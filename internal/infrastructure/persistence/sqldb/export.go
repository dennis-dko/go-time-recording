package sqldb

import (
	"context"
	"fmt"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// accountExport is what an account's export reads: a statement for every table
// PurgeUser erases from, in the same order, and nothing else - test/export_test.go
// holds the two lists together.
//
// Every column is named rather than taken with *, because what is left out is the
// point as much as what is put in: a password hash, a second factor's seed, the
// hashes of tokens, sessions and calendar addresses, and a passkey's key material
// are the account's, and a file of them lying in somebody's downloads folder is a
// way in. What is handed over about each of those is that it exists, when it was
// made and when it was last used.
var accountExport = []struct {
	file  string
	query string
}{
	{"account", "SELECT u.id, u.name, u.email, COALESCE(r.name, '') AS role, u.language, u.theme, " +
		"u.timezone, u.holiday_region, u.daily_target_hours, u.max_daily_hours, u.every_day_counts_from, " +
		"u.balance_upper_limit, u.balance_lower_limit, u.skip_unbooked_reminder, u.timer_alert_hours, " +
		"u.is_external, u.external_id, u.totp_enabled, u.must_change_password, u.tour_seen " +
		"FROM users u LEFT JOIN roles r ON r.id = u.role_id WHERE u.id = ?"},
	{"stopwatch", "SELECT project_id, description, started_at FROM running_timers WHERE user_id = ?"},
	{"entry-history", "SELECT id, timesheet_id, date, duration_hours, project_id, description, replaced_at, " +
		"replaced_by " +
		"FROM timesheet_revisions WHERE user_id = ? ORDER BY timesheet_id, replaced_at"},
	{"time-entries", "SELECT id, project_id, date, duration_hours, description, created_at, updated_at, " +
		"recorded_by, updated_by " +
		"FROM timesheets WHERE user_id = ? ORDER BY date, id"},
	{"projects", "SELECT id, name, description, start_date, end_date, status, budget_hours " +
		"FROM projects WHERE owner_id = ? ORDER BY id"},
	{"api-tokens", "SELECT id, name, prefix, created_at, expires_at, last_used_at " +
		"FROM api_tokens WHERE user_id = ? ORDER BY id"},
	{"passkeys", "SELECT id, name, attestation_type, transports, sign_count, backup_eligible, backed_up, " +
		"created_at, last_used_at FROM passkeys WHERE user_id = ? ORDER BY id"},
	{"sessions", "SELECT created_at, expires_at, last_seen_at FROM sessions WHERE user_id = ? ORDER BY created_at"},
	{"planned-weeks", "SELECT valid_from, monday, tuesday, wednesday, thursday, friday, saturday, sunday " +
		"FROM work_schedules WHERE user_id = ? ORDER BY valid_from"},
	{"entry-templates", "SELECT id, project_id, duration_hours, description " +
		"FROM entry_templates WHERE user_id = ? ORDER BY id"},
	{"absences", "SELECT day, kind FROM absences WHERE user_id = ? ORDER BY day"},
	{"leave-entitlements", "SELECT from_year, days FROM vacation_entitlements WHERE user_id = ? ORDER BY from_year"},
	{"calendar-subscription", "SELECT prefix, created_at, last_read_at FROM calendar_feeds WHERE user_id = ?"},
	{"openid-links", "SELECT issuer, subject, created_at FROM openid_identities WHERE user_id = ? ORDER BY id"},
	{"working-times", "SELECT day, start_minute, end_minute, break_minutes " +
		"FROM work_days WHERE user_id = ? ORDER BY day"},
	{"managers", "SELECT manager_id FROM managed_accounts WHERE user_id = ? ORDER BY manager_id"},
	{"managed-accounts", "SELECT user_id FROM managed_accounts WHERE manager_id = ? ORDER BY user_id"},
}

// ExportUser reads everything held about an account, a part per table, each row
// as its columns.
//
// Columns as they are stored rather than as a model would put them: an export is
// an answer to "what do you keep about me", and the stored column is the honest
// answer to it. Only the shapes a driver hands back are evened out - bytes become
// text, a moment becomes RFC 3339 - so the same installation answers the same way
// on all three databases.
func (r *UserRepository) ExportUser(ctx context.Context, userID uint) ([]repository.ExportedPart, error) {
	parts := make([]repository.ExportedPart, 0, len(accountExport))

	for _, part := range accountExport {
		rows, err := r.exportRows(ctx, part.query, userID)
		if err != nil {
			return nil, apperror.Internal(fmt.Errorf("exporting the %s of user %d: %w", part.file, userID, err))
		}

		parts = append(parts, repository.ExportedPart{Name: part.file, Rows: rows})
	}

	return parts, nil
}

// exportRows runs one export statement and reads every row as column and value.
func (r *UserRepository) exportRows(ctx context.Context, query string, userID uint) ([]map[string]any, error) {
	rows, err := r.db.QueryContext(ctx, r.rebind(query), userID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	read := []map[string]any{}

	for rows.Next() {
		values := make([]any, len(columns))
		into := make([]any, len(columns))

		for i := range values {
			into[i] = &values[i]
		}

		if err := rows.Scan(into...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(columns))

		for i, column := range columns {
			switch value := values[i].(type) {
			case []byte:
				row[column] = string(value)
			case time.Time:
				row[column] = value.UTC().Format(time.RFC3339)
			default:
				row[column] = value
			}
		}

		read = append(read, row)
	}

	return read, rows.Err()
}
