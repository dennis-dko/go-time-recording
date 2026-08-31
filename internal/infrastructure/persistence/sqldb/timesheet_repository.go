package sqldb

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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

// Save writes a new entry, stamping when it was recorded.
//
// The moment is taken here rather than by the caller so that every write path gets
// it - the form, the stopwatch and the spreadsheet import all arrive through a
// repository, and only one of them would have remembered. UTC because the column
// on MySQL keeps no zone, and a server that is moved would otherwise re-read its
// own history an hour out.
func (r *TimesheetRepository) Save(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	now := time.Now().UTC()

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
	now := time.Now().UTC()

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

	query := "SELECT " + timesheetColumns + " FROM timesheets"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY date DESC, id DESC"

	return r.query(ctx, r.rebind(query), args...)
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
	now := time.Now().UTC()

	found, err := r.update(ctx, "timesheets",
		"UPDATE timesheets SET user_id = ?, project_id = ?, date = ?, duration_hours = ?, "+
			"description = ?, updated_at = ? WHERE id = ?",
		timesheet.ID,
		timesheet.UserID, timesheet.ProjectID, timesheet.Date, timesheet.DurationHours,
		timesheet.Description, now, timesheet.ID)
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

func (r *TimesheetRepository) Delete(ctx context.Context, id uint) error {
	affected, err := r.exec(ctx, "DELETE FROM timesheets WHERE id = ?", id)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("timesheet", strconv.FormatUint(uint64(id), 10))
	}

	return nil
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
