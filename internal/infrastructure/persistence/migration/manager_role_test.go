package migration_test

import (
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// beforeTheManagerShipped is the last version an installation can have run before
// the manager role was part of what it is given.
const beforeTheManagerShipped = 20261022050000

// An installation running before the manager role shipped gets it, holding what the
// model says - and a role of its own that happens to be called manager is moved
// aside with its people and its rights rather than becoming the shipped one, which
// would hand those people the team rights nobody chose for them.
func TestAnInstallationsOwnManagerRoleIsMovedAsideForTheShippedOne(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, beforeTheManagerShipped)

	// Seeded from today's model, the chain gave this database a manager at its start,
	// which the retirement of the review path then removed - removed again here, so
	// the case does not depend on that. An installation that ran the chain before the
	// role shipped had none, and had made one of its own instead.
	for _, statement := range []string{
		`DELETE FROM role_permissions WHERE role_id = (SELECT id FROM roles WHERE name = 'manager')`,
		`DELETE FROM roles WHERE name = 'manager'`,
		`INSERT INTO roles (name, description, is_system) VALUES ('manager', 'Our own', 0)`,
		`INSERT INTO role_permissions (role_id, permission)
			SELECT id, 'projects:read' FROM roles WHERE name = 'manager'`,
		`INSERT INTO users (name, email, role_id, password_hash)
			SELECT 'Mona', 'mona@example.com', id, '' FROM roles WHERE name = 'manager'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("making the installation's own manager role: %v", err)
		}
	}

	migrate(t, db, beforeTheManagerShipped, latest(t))

	if got := strings.Join(roleNames(t, db), ","); got != "admin,manager,manager-2,user,user-admin" {
		t.Fatalf("the roles are %q, want the installation's own moved to manager-2 beside the shipped one", got)
	}

	if got := grants(t, db, "manager-2"); got != "projects:read" {
		t.Errorf("the installation's own role grants %q, want what it granted before", got)
	}

	var on string
	if err := db.QueryRow(`SELECT r.name FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.email = 'mona@example.com'`).Scan(&on); err != nil || on != "manager-2" {
		t.Errorf("Mona is on %q (%v), want the role she was given, under its new name", on, err)
	}

	if got, want := grants(t, db, model.RoleManager), asTheModelHasIt(model.RoleManager); got != want {
		t.Errorf("the shipped manager grants %q, want %q", got, want)
	}
}

// An installation that already has the shipped role keeps exactly one: running the
// migration over a fresh chain, which seeded it from the start, changes nothing.
func TestTheShippedManagerRoleIsNotShippedTwice(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, latest(t))

	if got, want := grants(t, db, model.RoleManager), asTheModelHasIt(model.RoleManager); got != want {
		t.Errorf("the manager role of a fresh installation grants %q, want %q", got, want)
	}

	if names := strings.Join(roleNames(t, db), ","); strings.Contains(names, "manager-") {
		t.Errorf("a fresh installation has the roles %q, with the shipped one moved aside for itself", names)
	}
}
