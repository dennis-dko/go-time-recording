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

// Users are always read with their role name joined in, so callers can display
// a user without a second query.
const userSelect = `SELECT u.id, u.name, u.email, u.role_id, COALESCE(r.name, ''),
	u.password_hash, u.must_change_password, u.is_system,
	u.daily_target_hours, u.max_daily_hours,
	u.totp_secret, u.totp_enabled, u.language, u.is_external
	FROM users u LEFT JOIN roles r ON r.id = u.role_id`

// UserRepository stores users in a SQL database.
type UserRepository struct {
	base
}

// NewUserRepository creates a user repository for the given dialect.
func NewUserRepository(db DB, dialect string) *UserRepository {
	return &UserRepository{base{db: db, dialect: dialect}}
}

// compile-time check that the domain contract stays satisfied.
var _ repository.UserRepository = (*UserRepository)(nil)

func (r *UserRepository) Save(ctx context.Context, user *model.User) (*model.User, error) {
	id, err := r.insert(ctx,
		"INSERT INTO users (name, email, role_id, password_hash, must_change_password, "+
			"is_system, daily_target_hours, max_daily_hours, totp_secret, totp_enabled, "+
			"language, is_external) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		user.Name, user.Email, user.RoleID, user.PasswordHash, user.MustChangePassword,
		user.IsSystem, user.DailyTargetHours, user.MaxDailyHours,
		user.TOTPSecret, user.TOTPEnabled, user.Language, user.IsExternal)
	if err != nil {
		return nil, translateUserErr(err, user.Email)
	}

	// Read back so the caller receives the joined role name.
	return r.GetByID(ctx, id)
}

func (r *UserRepository) GetByID(ctx context.Context, id uint) (*model.User, error) {
	row := r.db.QueryRowContext(ctx, r.rebind(userSelect+" WHERE u.id = ?"), id)

	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("user", strconv.FormatUint(uint64(id), 10))
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	return user, nil
}

// GetByEmail looks a user up by their login identifier.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	row := r.db.QueryRowContext(ctx, r.rebind(userSelect+" WHERE u.email = ?"), strings.ToLower(email))

	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("user", email)
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	return user, nil
}

func (r *UserRepository) GetAll(ctx context.Context) ([]*model.User, error) {
	rows, err := r.db.QueryContext(ctx, userSelect+" ORDER BY u.id")
	if err != nil {
		return nil, apperror.Internal(err)
	}
	defer func() { _ = rows.Close() }()

	users := make([]*model.User, 0)

	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return users, nil
}

func (r *UserRepository) Update(ctx context.Context, user *model.User) (*model.User, error) {
	affected, err := r.exec(ctx,
		"UPDATE users SET name = ?, email = ?, role_id = ?, password_hash = ?, "+
			"must_change_password = ?, daily_target_hours = ?, max_daily_hours = ?, "+
			"totp_secret = ?, totp_enabled = ?, language = ?, is_external = ? WHERE id = ?",
		user.Name, user.Email, user.RoleID, user.PasswordHash, user.MustChangePassword,
		user.DailyTargetHours, user.MaxDailyHours,
		user.TOTPSecret, user.TOTPEnabled, user.Language, user.IsExternal, user.ID)
	if err != nil {
		return nil, translateUserErr(err, user.Email)
	}

	if affected == 0 {
		return nil, apperror.NotFound("user", strconv.FormatUint(uint64(user.ID), 10))
	}

	return r.GetByID(ctx, user.ID)
}

func (r *UserRepository) Delete(ctx context.Context, id uint) error {
	affected, err := r.exec(ctx, "DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("user", strconv.FormatUint(uint64(id), 10))
	}

	return nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*model.User, error) {
	var user model.User

	err := s.Scan(&user.ID, &user.Name, &user.Email, &user.RoleID, &user.RoleName,
		&user.PasswordHash, &user.MustChangePassword, &user.IsSystem,
		&user.DailyTargetHours, &user.MaxDailyHours,
		&user.TOTPSecret, &user.TOTPEnabled, &user.Language, &user.IsExternal)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// translateUserErr converts a driver-level unique violation into a conflict,
// so a duplicate email surfaces as a client error rather than a 500.
//
// The dialects report this differently and none of the supported drivers
// expose a shared typed error, so the check is on the message text.
func translateUserErr(err error, email string) error {
	if isUniqueViolation(err) {
		return apperror.Conflictf("a user with email %q already exists", email)
	}

	return apperror.Internal(err)
}

// isUniqueViolation reports whether err is a driver-level unique constraint
// violation. The dialects report this differently and none of the supported
// drivers expose a shared typed error, so the check is on the message text.
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
