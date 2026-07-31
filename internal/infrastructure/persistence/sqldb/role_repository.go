package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// RoleRepository stores roles and their permissions in a SQL database.
type RoleRepository struct {
	base
}

// NewRoleRepository creates a role repository for the given dialect.
func NewRoleRepository(db DB, dialect string) *RoleRepository {
	return &RoleRepository{base{db: db, dialect: dialect}}
}

var _ repository.RoleRepository = (*RoleRepository)(nil)

func (r *RoleRepository) Save(ctx context.Context, role *model.Role) (*model.Role, error) {
	id, err := r.insert(ctx,
		"INSERT INTO roles (name, description, is_system) VALUES (?, ?, ?)",
		role.Name, role.Description, role.IsSystem)
	if err != nil {
		return nil, translateRoleErr(err, role.Name)
	}

	if err := r.replacePermissions(ctx, id, role.Permissions); err != nil {
		return nil, err
	}

	return r.GetByID(ctx, id)
}

func (r *RoleRepository) GetByID(ctx context.Context, id uint) (*model.Role, error) {
	return r.getBy(ctx, "id = ?", id, strconv.FormatUint(uint64(id), 10))
}

func (r *RoleRepository) GetByName(ctx context.Context, name string) (*model.Role, error) {
	return r.getBy(ctx, "name = ?", name, name)
}

func (r *RoleRepository) getBy(ctx context.Context, where string, arg any, label string) (*model.Role, error) {
	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT id, name, description, is_system FROM roles WHERE "+where), arg)

	var role model.Role

	err := row.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("role", label)
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	role.Permissions, err = r.permissionsOf(ctx, role.ID)
	if err != nil {
		return nil, err
	}

	return &role, nil
}

func (r *RoleRepository) GetAll(ctx context.Context) ([]*model.Role, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, name, description, is_system FROM roles ORDER BY id")
	if err != nil {
		return nil, apperror.Internal(err)
	}
	defer rows.Close()

	roles := make([]*model.Role, 0)

	for rows.Next() {
		var role model.Role
		if scanErr := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem); scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		roles = append(roles, &role)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	// Permissions are loaded after the rows are closed: holding a second query
	// open on the same connection while iterating the first is not portable.
	for _, role := range roles {
		role.Permissions, err = r.permissionsOf(ctx, role.ID)
		if err != nil {
			return nil, err
		}
	}

	return roles, nil
}

func (r *RoleRepository) Update(ctx context.Context, role *model.Role) (*model.Role, error) {
	affected, err := r.exec(ctx,
		"UPDATE roles SET name = ?, description = ? WHERE id = ?",
		role.Name, role.Description, role.ID)
	if err != nil {
		return nil, translateRoleErr(err, role.Name)
	}

	if affected == 0 {
		return nil, apperror.NotFound("role", strconv.FormatUint(uint64(role.ID), 10))
	}

	if err := r.replacePermissions(ctx, role.ID, role.Permissions); err != nil {
		return nil, err
	}

	return r.GetByID(ctx, role.ID)
}

func (r *RoleRepository) Delete(ctx context.Context, id uint) error {
	if _, err := r.exec(ctx, "DELETE FROM role_permissions WHERE role_id = ?", id); err != nil {
		return apperror.Internal(err)
	}

	affected, err := r.exec(ctx, "DELETE FROM roles WHERE id = ?", id)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("role", strconv.FormatUint(uint64(id), 10))
	}

	return nil
}

func (r *RoleRepository) CountUsers(ctx context.Context, roleID uint) (int, error) {
	var count int

	err := r.db.QueryRowContext(ctx,
		r.rebind("SELECT COUNT(*) FROM users WHERE role_id = ?"), roleID).Scan(&count)
	if err != nil {
		return 0, apperror.Internal(err)
	}

	return count, nil
}

func (r *RoleRepository) permissionsOf(ctx context.Context, roleID uint) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		r.rebind("SELECT permission FROM role_permissions WHERE role_id = ? ORDER BY permission"), roleID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	defer rows.Close()

	permissions := make([]string, 0)

	for rows.Next() {
		var permission string
		if scanErr := rows.Scan(&permission); scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		permissions = append(permissions, permission)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return permissions, nil
}

// replacePermissions makes the stored grants match the given set exactly.
func (r *RoleRepository) replacePermissions(ctx context.Context, roleID uint, permissions []string) error {
	if _, err := r.exec(ctx, "DELETE FROM role_permissions WHERE role_id = ?", roleID); err != nil {
		return apperror.Internal(err)
	}

	for _, permission := range permissions {
		_, err := r.exec(ctx,
			"INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)", roleID, permission)
		if err != nil {
			return apperror.Internal(err)
		}
	}

	return nil
}

func translateRoleErr(err error, name string) error {
	if isUniqueViolation(err) {
		return apperror.Conflictf("a role named %q already exists", name)
	}

	return apperror.Internal(err)
}
