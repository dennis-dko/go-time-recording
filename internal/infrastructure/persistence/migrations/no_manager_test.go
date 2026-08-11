package migrations_test

import (
	"database/sql"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Nobody comes out of the upgrade able to read or write somebody else's time.
//
// Two rights used to grant it: timesheets:read:all opened everybody's entries,
// balances, totals and exports, and timesheets:write:all let one account book and
// change time in another person's name. No default role held either by the end, but a
// role an installation built for itself could, and the role editor went on offering
// both - so an administrator could tick one, see nothing change on any screen, and
// hand out a capability the API answered in full.
//
// What has to be true afterwards: the rights are gone from every role, and no role is
// left granting nothing. The second half is the one worth testing. Withdrawing a right
// from a role whose only right it was would leave an empty role, and whoever holds an
// empty role signs in to an interface with no screen on it - which reads as a broken
// installation rather than as a decision somebody made.

// theRetirement is the migration that withdraws the two rights.
const theRetirement = int64(20260811010000)

// permissionsOf reads what a role grants, sorted so a comparison is stable.
func permissionsOf(t *testing.T, db *sql.DB, role string) []string {
	t.Helper()

	rows, err := db.Query(`SELECT permission FROM role_permissions
		WHERE role_id = (SELECT id FROM roles WHERE name = ?)`, role)
	if err != nil {
		t.Fatalf("reading what %q grants: %v", role, err)
	}

	defer func() { _ = rows.Close() }()

	var held []string

	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			t.Fatalf("reading a permission of %q: %v", role, err)
		}

		held = append(held, permission)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading what %q grants: %v", role, err)
	}

	sort.Strings(held)

	return held
}

// roleHolding creates a role granting exactly what it is given.
//
// Written straight into the tables, because the point is what the migration does to
// rows that are already there - and because the application refuses to create some of
// these now, which is the whole reason the migration exists.
func roleHolding(t *testing.T, db *sql.DB, name string, permissions ...string) {
	t.Helper()

	if _, err := db.Exec(`INSERT INTO roles (name, description, is_system)
		VALUES (?, ?, 0)`, name, "built by this installation"); err != nil {
		t.Fatalf("creating the role %q: %v", name, err)
	}

	for _, permission := range permissions {
		if _, err := db.Exec(`INSERT INTO role_permissions (role_id, permission)
			SELECT id, ? FROM roles WHERE name = ?`, permission, name); err != nil {
			t.Fatalf("granting %q to %q: %v", permission, name, err)
		}
	}
}

func TestTheRightsOverEverybodysTimeAreWithdrawn(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, theRetirement-1)

	// Three shapes an installation could actually have. The first two are the ones
	// that would be emptied by a plain delete; the third has to come through with the
	// rest of what it grants untouched.
	roleHolding(t, db, "observer", "timesheets:read:all")
	roleHolding(t, db, "bookings-clerk", "timesheets:write:all")
	roleHolding(t, db, "our-own-admin",
		"users:read", "users:write", "timesheets:read:all", "timesheets:write:all")

	migrate(t, db, theRetirement-1, latest(t))

	for _, role := range []string{"observer", "bookings-clerk", "our-own-admin"} {
		held := permissionsOf(t, db, role)

		for _, gone := range []string{"timesheets:read:all", "timesheets:write:all"} {
			for _, permission := range held {
				if permission == gone {
					t.Errorf("%q still grants %q", role, gone)
				}
			}
		}

		// The half of it that is still a right: whoever could read everybody's time
		// could read their own, and that is what remains.
		if len(held) == 0 {
			t.Errorf("%q grants nothing at all now, so whoever holds it signs in to "+
				"an interface with no screen on it", role)
		}
	}

	if got := strings.Join(permissionsOf(t, db, "observer"), ","); got != "timesheets:read:own" {
		t.Errorf("the reading role now grants %q, want timesheets:read:own", got)
	}

	if got := strings.Join(permissionsOf(t, db, "bookings-clerk"), ","); got != "timesheets:write:own" {
		t.Errorf("the booking role now grants %q, want timesheets:write:own", got)
	}

	// And a role that held other things keeps them. A migration that tidied a right
	// away and took an installation's own arrangement with it would be worse than the
	// right staying.
	want := "timesheets:read:own,timesheets:write:own,users:read,users:write"
	if got := strings.Join(permissionsOf(t, db, "our-own-admin"), ","); got != want {
		t.Errorf("the administrator role an installation built now grants %q, want %q",
			got, want)
	}
}

// A role that already had the own-scoped right does not end up with it twice.
//
// The grant is written as an insert-where-not-exists, and getting that wrong would
// either violate the unique constraint - failing the whole upgrade - or leave a
// duplicate row that shows up as the same permission listed twice in the role editor.
func TestTheResidueIsNotGrantedTwice(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, theRetirement-1)

	roleHolding(t, db, "both-ways",
		"timesheets:read:all", "timesheets:read:own", "timesheets:write:own")

	migrate(t, db, theRetirement-1, latest(t))

	want := "timesheets:read:own,timesheets:write:own"
	if got := strings.Join(permissionsOf(t, db, "both-ways"), ","); got != want {
		t.Errorf("the role now grants %q, want %q", got, want)
	}
}

// An ordinary upgrade, where no role held either right, changes nothing.
//
// The seeded roles have not held them since the review path was retired, so this is
// what almost every real installation runs - and a migration that quietly added a
// right to the user role on the way past would be a hole nobody looked for.
func TestTheRetirementChangesNothingOnAnOrdinaryUpgrade(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, theRetirement-1)

	before := map[string][]string{}
	for _, role := range []string{"admin", "user", "user-admin"} {
		before[role] = permissionsOf(t, db, role)
	}

	migrate(t, db, theRetirement-1, latest(t))

	for role, was := range before {
		is := permissionsOf(t, db, role)

		if strings.Join(is, ",") != strings.Join(was, ",") {
			t.Errorf("the seeded role %q changed from %v to %v", role, was, is)
		}
	}
}
