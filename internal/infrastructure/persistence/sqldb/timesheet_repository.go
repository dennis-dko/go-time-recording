package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

const timesheetColumns = "id, user_id, project_id, date, duration_hours, description"

// TimesheetRepository stores time entries in a SQL database.
type TimesheetRepository struct {
	base
}

// NewTimesheetRepository creates a timesheet repository for the given dialect.
func NewTimesheetRepository(db DB, dialect string) *TimesheetRepository {
	return &TimesheetRepository{base{db: db, dialect: dialect}}
}

var _ repository.TimesheetRepository = (*TimesheetRepository)(nil)

func (r *TimesheetRepository) Save(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	id, err := r.insert(ctx,
		"INSERT INTO timesheets (user_id, project_id, date, duration_hours, description) "+
			"VALUES (?, ?, ?, ?, ?)",
		timesheet.UserID, timesheet.ProjectID, timesheet.Date,
		timesheet.DurationHours, timesheet.Description)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	created := *timesheet
	created.ID = id

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

	return r.withTx(ctx, func(tx base) error {
		for _, entry := range entries {
			// Through the transaction's own base, not the repository's: reaching
			// past it would take a second connection, and on SQLite that means
			// waiting for a lock this transaction is holding.
			if _, err := tx.db.ExecContext(ctx, tx.rebind(
				"INSERT INTO timesheets (user_id, project_id, date, duration_hours, "+
					"description) VALUES (?, ?, ?, ?, ?)"),
				entry.UserID, entry.ProjectID, entry.Date,
				entry.DurationHours, entry.Description); err != nil {
				return apperror.Internal(err)
			}
		}

		return nil
	})
}

func (r *TimesheetRepository) GetByID(ctx context.Context, id uint) (*model.Timesheet, error) {
	row := r.db.QueryRowContext(ctx, r.rebind("SELECT "+timesheetColumns+" FROM timesheets WHERE id = ?"), id)

	timesheet, err := scanTimesheet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("timesheet", strconv.FormatUint(uint64(id), 10))
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	return timesheet, nil
}

// GetByFilter returns the time entries matching filter. Zero-valued filter
// fields are omitted, so an empty filter returns everything.
func (r *TimesheetRepository) GetByFilter(
	ctx context.Context,
	filter repository.TimesheetFilter,
) ([]*model.Timesheet, error) {
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

func (r *TimesheetRepository) Update(ctx context.Context, timesheet *model.Timesheet) (*model.Timesheet, error) {
	found, err := r.update(ctx, "timesheets",
		"UPDATE timesheets SET user_id = ?, project_id = ?, date = ?, duration_hours = ?, "+
			"description = ? WHERE id = ?",
		timesheet.ID,
		timesheet.UserID, timesheet.ProjectID, timesheet.Date, timesheet.DurationHours,
		timesheet.Description, timesheet.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if !found {
		return nil, apperror.NotFound("timesheet", strconv.FormatUint(uint64(timesheet.ID), 10))
	}

	updated := *timesheet

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
	)

	err := s.Scan(&timesheet.ID, &timesheet.UserID, &timesheet.ProjectID, &date,
		&timesheet.DurationHours, &timesheet.Description)
	if err != nil {
		return nil, err
	}

	timesheet.Date = date.Time

	return &timesheet, nil
}
