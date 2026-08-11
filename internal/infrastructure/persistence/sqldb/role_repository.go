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

// Save stores a new role together with the permissions it grants.
//
// Both in one transaction, because to whoever filled in the form they are one
// thing. The row used to be committed on its own and the grants attempted
// afterwards in a transaction of their own, so a permission phase that failed -
// the same right listed twice is enough, the pair is the primary key - left a
// role behind that granted nothing and that nobody had asked for. That is the
// state replacePermissions' comment says a transaction is there to prevent,
// reached one level up.
//
// The read back happens after the commit, not inside it: GetByID runs on the
// repository's own datasource, which means a second connection, and asking for
// one while this transaction still holds the write lock is a wait that only the
// transaction doing the waiting could end.
func (r *RoleRepository) Save(ctx context.Context, role *model.Role) (*model.Role, error) {
	var id uint

	err := r.withTx(ctx, func(tx base) error {
		newID, err := tx.insert(ctx,
			"INSERT INTO roles (name, description, is_system) VALUES (?, ?, ?)",
			role.Name, role.Description, role.IsSystem)
		if err != nil {
			return translateRoleErr(err, role.Name)
		}

		id = newID

		return replacePermissions(ctx, tx, newID, role.Permissions)
	})
	if err != nil {
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
	defer func() { _ = rows.Close() }()

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

// Update rewrites a role and the set of permissions it grants.
//
// Same transaction, same reason as Save, with more to lose: here the failing
// half is a role that already had grants and users on it. The rename would stand
// while the permissions behind it were half replaced or gone.
func (r *RoleRepository) Update(ctx context.Context, role *model.Role) (*model.Role, error) {
	err := r.withTx(ctx, func(tx base) error {
		found, updateErr := tx.update(ctx, "roles",
			"UPDATE roles SET name = ?, description = ? WHERE id = ?",
			role.ID,
			role.Name, role.Description, role.ID)
		if updateErr != nil {
			return translateRoleErr(updateErr, role.Name)
		}

		if !found {
			return apperror.NotFound("role", strconv.FormatUint(uint64(role.ID), 10))
		}

		return replacePermissions(ctx, tx, role.ID, role.Permissions)
	})
	if err != nil {
		return nil, err
	}

	return r.GetByID(ctx, role.ID)
}

// Delete removes a role and its permissions.
//
// One transaction, because it is two tables. The order matters and so does the
// atomicity: permissions gone with the role still there is a role that grants
// nothing while everyone assigned to it keeps pointing at it.
func (r *RoleRepository) Delete(ctx context.Context, id uint) error {
	return r.withTx(ctx, func(tx base) error {
		if _, err := tx.exec(ctx, "DELETE FROM role_permissions WHERE role_id = ?", id); err != nil {
			return apperror.Internal(err)
		}

		affected, err := tx.exec(ctx, "DELETE FROM roles WHERE id = ?", id)
		if err != nil {
			return apperror.Internal(err)
		}

		if affected == 0 {
			return apperror.NotFound("role", strconv.FormatUint(uint64(id), 10))
		}

		return nil
	})
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
	defer func() { _ = rows.Close() }()

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
//
// The most dangerous few lines in this package, and the reason withTx exists: it
// deletes every permission the role has and then inserts the new set. A
// connection lost in between leaves the role holding fewer permissions than
// anybody asked for, or none - and every user on that role loses the access those
// permissions granted, immediately, with the only trace being an error the
// administrator saw once.
//
// It runs on the transaction it is handed instead of opening one, so that the
// role row and its grants share a single transaction with its callers. Opening
// one here would not nest: it would begin a second transaction on the
// repository's own datasource, and that means a second connection asking to
// write rows the first connection has already locked. On SQLite the two then
// wait for each other, and the one that could release the lock is the one
// blocked. Handing the transaction down is what keeps it one.
func replacePermissions(ctx context.Context, tx base, roleID uint, permissions []string) error {
	if _, err := tx.exec(ctx, "DELETE FROM role_permissions WHERE role_id = ?", roleID); err != nil {
		return apperror.Internal(err)
	}

	for _, permission := range permissions {
		_, err := tx.exec(ctx,
			"INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)", roleID, permission)
		if err != nil {
			return apperror.Internal(err)
		}
	}

	return nil
}

func translateRoleErr(err error, name string) error {
	if isUniqueViolation(err) {
		return apperror.Conflictf("a role named %q already exists", name).
			WithCode("roleNameTaken", name)
	}

	return apperror.Internal(err)
}
