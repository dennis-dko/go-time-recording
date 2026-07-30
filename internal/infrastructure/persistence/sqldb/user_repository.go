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

const userColumns = "id, name, email, role"

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
	id, err := r.insert(ctx, "INSERT INTO users (name, email, role) VALUES (?, ?, ?)",
		user.Name, user.Email, user.Role)
	if err != nil {
		return nil, translateUserErr(err, user.Email)
	}

	created := *user
	created.ID = id

	return &created, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id uint) (*model.User, error) {
	row := r.db.QueryRowContext(ctx, r.rebind("SELECT "+userColumns+" FROM users WHERE id = ?"), id)

	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("user", strconv.FormatUint(uint64(id), 10))
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	return user, nil
}

func (r *UserRepository) GetAll(ctx context.Context) ([]*model.User, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY id")
	if err != nil {
		return nil, apperror.Internal(err)
	}
	defer rows.Close()

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
	affected, err := r.exec(ctx, "UPDATE users SET name = ?, email = ?, role = ? WHERE id = ?",
		user.Name, user.Email, user.Role, user.ID)
	if err != nil {
		return nil, translateUserErr(err, user.Email)
	}

	if affected == 0 {
		return nil, apperror.NotFound("user", strconv.FormatUint(uint64(user.ID), 10))
	}

	updated := *user

	return &updated, nil
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

	if err := s.Scan(&user.ID, &user.Name, &user.Email, &user.Role); err != nil {
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
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") {
		return apperror.Conflictf("a user with email %q already exists", email)
	}

	return apperror.Internal(err)
}
