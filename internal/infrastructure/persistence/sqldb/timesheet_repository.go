package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

const timesheetColumns = "id, user_id, project_id, date, duration_hours, description, " +
	"created_at, updated_at"

// TimesheetRepository stores time entries in a SQL database.
type TimesheetRepository struct {
	base
}

// NewTimesheetRepository creates a timesheet repository for the given dialect.
func NewTimesheetRepository(db DB, dialect string) *TimesheetRepository {
	return &TimesheetRepository{base{db: db, dialect: dialect}}
}

var _ repository.TimesheetRepository = (*TimesheetRepository)(nil)

// recordedNow is the moment a write is stamped with.
//
// Truncated to the second, and that is the whole of what makes the value this
// returns the value that is stored. The three dialects keep three different
// resolutions - PostgreSQL's TIMESTAMPTZ microseconds, MySQL's DATETIME whole
// seconds, SQLite whatever the driver wrote - so a nanosecond moment handed back
// in the creation response was a moment no database had. Correcting the entry then
// read the stored one back and the two disagreed in the sixth decimal place, which
// is how the PostgreSQL leg of the integration suite found this and the SQLite leg
// did not.
//
// One second is the resolution this records anyway: it answers when a figure was
// written down and whether it has moved since, and MySQL could not have kept more
// without changing the column type on a table that already exists.
//
// UTC because MySQL's column keeps no zone, and a server that is moved would
// otherwise re-read its own history an hour out.
func recordedNow() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// Save writes a new entry, stamping when it was recorded.
//
// The moment is taken here rather than by the caller so that every write path gets
// it - the form, the stopwatch and the spreadsheet import all arrive through a
// repository, and only one of them would have remembered.
func (r *TimesheetRepository) Save(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	now := recordedNow()

	id, err := r.insert(ctx,
		"INSERT INTO timesheets (user_id, project_id, date, duration_hours, description, "+
			"created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		timesheet.UserID, timesheet.ProjectID, timesheet.Date,
		timesheet.DurationHours, timesheet.Description, now, now)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	created := *timesheet
	created.ID = id
	created.CreatedAt, created.UpdatedAt = now, now

	return &created, nil
}

// SaveMany writes several entries inside one transaction.
//
// For the spreadsheet import: a file that landed half way would leave nobody able
// to say which half, or which entries came from it. See base.withTx.
func (r *TimesheetRepository) SaveMany(ctx context.Context, entries []*model.Timesheet) error {
	if len(entries) == 0 {
		return nil
	}

	// One moment for the whole file rather than one per row: they were recorded by
	// a single act, and rows differing by a few milliseconds would invite somebody
	// to read an order into them that the spreadsheet never had.
	now := recordedNow()

	return r.withTx(ctx, func(tx base) error {
		for _, entry := range entries {
			// Through the transaction's own base, not the repository's: reaching
			// past it would take a second connection, and on SQLite that means
			// waiting for a lock this transaction is holding.
			if _, err := tx.db.ExecContext(ctx, tx.rebind(
				"INSERT INTO timesheets (user_id, project_id, date, duration_hours, "+
					"description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"),
				entry.UserID, entry.ProjectID, entry.Date,
				entry.DurationHours, entry.Description, now, now); err != nil {
				return apperror.Internal(err)
			}
		}

		return nil
	})
}

func (r *TimesheetRepository) GetByID(ctx context.Context, id uint) (*model.Timesheet, error) {
	row := r.db.QueryRowContext(ctx, r.rebind("SELECT "+timesheetColumns+" FROM timesheets WHERE id = ?"), id)

	timesheet, err := scanTimesheet(row)
	if problem := problemReading(err, "timesheet", strconv.FormatUint(uint64(id), 10)); problem != nil {
		return nil, problem
	}

	return timesheet, nil
}

// GetByFilter returns the time entries matching filter. Zero-valued filter
// fields are omitted, so an empty filter returns everything.
func (r *TimesheetRepository) GetByFilter(
	ctx context.Context,
	filter repository.TimesheetFilter,
) ([]*model.Timesheet, error) {
	// A range of days, before it is compared to anything. See OverWholeDays: the
	// ends arrive carrying the reader's zone and the stored dates carry none.
	filter = filter.OverWholeDays()

	conditions, args := timesheetConditions(filter)

	query := "SELECT " + timesheetColumns + " FROM timesheets" + where(conditions)

	// The order is what makes paging mean anything: without it two pages can show
	// one entry twice and never show another, and nothing about the result says so.
	query += " ORDER BY date DESC, id DESC"

	// Offset without limit is ignored rather than translated, because SQL wants the
	// limit first and inventing one here would answer a question nobody asked.
	if filter.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, filter.Limit, filter.Offset)
	}

	return r.query(ctx, r.rebind(query), args...)
}

// CountByFilter counts the matching entries without reading them.
func (r *TimesheetRepository) CountByFilter(
	ctx context.Context,
	filter repository.TimesheetFilter,
) (uint, error) {
	filter = filter.OverWholeDays()

	conditions, args := timesheetConditions(filter)

	var total uint

	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT COUNT(*) FROM timesheets"+where(conditions)), args...)
	if err := row.Scan(&total); err != nil {
		return 0, apperror.Internal(err)
	}

	return total, nil
}

// where renders the collected conditions, or nothing at all.
func where(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}

	return " WHERE " + strings.Join(conditions, " AND ")
}

func (r *TimesheetRepository) GetAll(ctx context.Context) ([]*model.Timesheet, error) {
	return r.GetByFilter(ctx, repository.TimesheetFilter{})
}

// Update writes the corrected entry and moves the correction moment.
//
// created_at is deliberately absent from the statement: when the figure was first
// written down does not change because it was later found to be wrong, and that
// difference is the whole point of keeping both.
func (r *TimesheetRepository) Update(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	now := recordedNow()

	found := true

	// What the entry said, read in the same transaction as the correction and -
	// where the engine can - locked by it, so that two corrections at once cannot
	// both keep the same earlier version and lose the one between.
	err := r.withTx(ctx, func(tx base) error {
		read := "SELECT " + timesheetColumns + " FROM timesheets WHERE id = ?"
		if r.dialect != DialectSQLite {
			read += " FOR UPDATE"
		}

		before, err := scanTimesheet(tx.db.QueryRowContext(ctx, tx.rebind(read), timesheet.ID))
		if errors.Is(err, sql.ErrNoRows) {
			found = false

			return nil
		}

		if err != nil {
			return err
		}

		if !before.Says(timesheet) {
			kept := model.RevisionOf(before, now)

			if _, err := tx.exec(ctx,
				"INSERT INTO timesheet_revisions (timesheet_id, user_id, date, duration_hours, project_id, "+
					"description, replaced_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
				kept.TimesheetID, kept.UserID, kept.Date, kept.DurationHours, kept.ProjectID,
				kept.Description, kept.ReplacedAt); err != nil {
				return err
			}
		}

		_, err = tx.exec(ctx,
			"UPDATE timesheets SET user_id = ?, project_id = ?, date = ?, duration_hours = ?, "+
				"description = ?, updated_at = ? WHERE id = ?",
			timesheet.UserID, timesheet.ProjectID, timesheet.Date, timesheet.DurationHours,
			timesheet.Description, now, timesheet.ID)

		return err
	})
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if !found {
		return nil, apperror.NotFound("timesheet", strconv.FormatUint(uint64(timesheet.ID), 10))
	}

	updated := *timesheet
	updated.UpdatedAt = now

	return &updated, nil
}

// Delete removes the entry and its history in one change: the history points at
// the entry, so it goes first, and an entry that could not be deleted keeps it.
func (r *TimesheetRepository) Delete(ctx context.Context, id uint) error {
	var affected int64

	err := r.withTx(ctx, func(tx base) error {
		if _, err := tx.exec(ctx, "DELETE FROM timesheet_revisions WHERE timesheet_id = ?", id); err != nil {
			return err
		}

		var err error
		affected, err = tx.exec(ctx, "DELETE FROM timesheets WHERE id = ?", id)

		return err
	})
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("timesheet", strconv.FormatUint(uint64(id), 10))
	}

	return nil
}

// Revisions returns what an entry said before each correction, the most recently
// replaced first - by when, and by which was kept last for two in the same second.
func (r *TimesheetRepository) Revisions(ctx context.Context, timesheetID uint) ([]model.TimesheetRevision, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(
		"SELECT id, timesheet_id, user_id, date, duration_hours, project_id, description, replaced_at "+
			"FROM timesheet_revisions WHERE timesheet_id = ? ORDER BY replaced_at DESC, id DESC"), timesheetID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	var history []model.TimesheetRevision

	for rows.Next() {
		var (
			revision         model.TimesheetRevision
			date, replacedAt dateTime
		)

		if err := rows.Scan(&revision.ID, &revision.TimesheetID, &revision.UserID, &date,
			&revision.DurationHours, &revision.ProjectID, &revision.Description, &replacedAt); err != nil {
			return nil, apperror.Internal(err)
		}

		revision.Date, revision.ReplacedAt = date.Time, replacedAt.Time
		history = append(history, revision)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return history, nil
}

func (r *TimesheetRepository) query(ctx context.Context, query string, args ...any) ([]*model.Timesheet, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	defer func() { _ = rows.Close() }()

	timesheets := make([]*model.Timesheet, 0)

	for rows.Next() {
		timesheet, scanErr := scanTimesheet(rows)
		if scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		timesheets = append(timesheets, timesheet)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return timesheets, nil
}

func scanTimesheet(s scanner) (*model.Timesheet, error) {
	var (
		timesheet model.Timesheet
		date      dateTime
		// Null for every entry booked before the audit columns existed, which
		// leaves both zero - the model's word for "nobody recorded this".
		created, updated dateTime
	)

	err := s.Scan(&timesheet.ID, &timesheet.UserID, &timesheet.ProjectID, &date,
		&timesheet.DurationHours, &timesheet.Description, &created, &updated)
	if err != nil {
		return nil, err
	}

	timesheet.Date = date.Time
	timesheet.CreatedAt, timesheet.UpdatedAt = created.Time, updated.Time

	return &timesheet, nil
}

// timesheetConditions turns a filter into the WHERE clause both the listing and
// the count run.
//
// Shared rather than written twice, because a page and its total have to describe
// the same set: a count that narrows differently from the query it is reported
// beside is a number that tells the reader there is more when there is not.
// Expects a filter that has already been through OverWholeDays.
func timesheetConditions(filter repository.TimesheetFilter) ([]string, []any) {
	var (
		conditions []string
		args       []any
	)

	if filter.UserID != 0 {
		conditions = append(conditions, "user_id = ?")
		args = append(args, filter.UserID)
	}

	if filter.ProjectID != 0 {
		conditions = append(conditions, "project_id = ?")
		args = append(args, filter.ProjectID)
	}

	if filter.WithoutProject {
		conditions = append(conditions, "project_id IS NULL")
	}

	if filter.StartDate != nil {
		conditions = append(conditions, "date >= ?")
		args = append(args, *filter.StartDate)
	}

	if filter.EndDate != nil {
		conditions = append(conditions, "date <= ?")
		args = append(args, *filter.EndDate)
	}

	if filter.Search != "" {
		conditions = append(conditions, "LOWER(description) LIKE ? ESCAPE '!'")
		args = append(args, "%"+likeLiteral(strings.ToLower(filter.Search))+"%")
	}

	return conditions, args
}

// likeLiteral makes text match itself inside a LIKE pattern.
//
// The escape is '!' rather than the backslash two of the engines default to:
// SQLite has no default escape at all, and a backslash written into the ESCAPE
// clause is a different string to MySQL than to PostgreSQL. A character none of
// them treats specially inside a string literal gives one clause that means the
// same thing on all three.
func likeLiteral(text string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(text)
}

// HoursByProject totals somebody's hours on each project they booked on, in one
// grouped query rather than a listing per project.
func (r *TimesheetRepository) HoursByProject(ctx context.Context, userID uint) (map[uint]float64, error) {
	query := "SELECT project_id, SUM(duration_hours) FROM timesheets WHERE project_id IS NOT NULL"

	var args []any

	if userID != 0 {
		query += " AND user_id = ?"
		args = append(args, userID)
	}

	rows, err := r.db.QueryContext(ctx, r.rebind(query+" GROUP BY project_id"), args...)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	defer func() { _ = rows.Close() }()

	totals := map[uint]float64{}

	for rows.Next() {
		var (
			projectID uint
			hours     float64
		)

		if err := rows.Scan(&projectID, &hours); err != nil {
			return nil, apperror.Internal(err)
		}

		totals[projectID] = hours
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return totals, nil
}
