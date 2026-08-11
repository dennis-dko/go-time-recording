package migrations_test

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The everyday role is called user, whatever it was called before.
//
// It was employee, and its combined one employee-admin. The word said more than this
// application knows: it holds accounts, and whether the person behind one is employed
// here, contracted, a volunteer or the only person in the company is not something it
// records or needs.
//
// A rename is not a cosmetic change here, because the application finds this role by
// name in four places: the role given to an account provisioned from the directory, the
// role an account without one is adopted into, the fallback in the migration that first
// moved roles into the database, and the seed itself. A rename that missed one of them
// would not fail - it would find no row, do nothing, and look exactly like success until
// somebody signed in from LDAP for the first time and was refused.
//
// So what these cases check is not only that the roles come out with the new names, but
// that everything which stored the old one was brought along.

// theRename is the migration that does it.
const theRename = int64(20260812010000)

// asAnOldInstallation takes a database to just before the rename, having spent the
// second half of the chain under the old names.
//
// Necessary because the chain cannot reproduce the old state by itself: the migration
// that first seeds roles builds them from the model, so replaying it today names the
// roles as the model names them today. Every case about an installation which predates
// the rename has to write that state by hand.
//
// Where the renaming happens matters more than it looks. An installation old enough to
// be renamed is also old enough not to have run the migrations that retired the review
// path, gave every project an owner and separated administering from working - so it
// runs all of those with its roles still called employee and employee-admin. Those
// migrations look the everyday role up by name, and a fixture that renamed the roles
// after them would leave that untested: they would have run against today's names, which
// is the one case a real upgrade of an old installation never has.
func asAnOldInstallation(t *testing.T, db *sql.DB) {
	t.Helper()

	// Just before the batch that reshaped the roles, which is where an installation
	// upgrading from the previous release starts from.
	migrate(t, db, 0, retirement-1)

	for _, renaming := range []struct{ from, to string }{
		{model.RoleUser, "employee"},
		{model.RoleUserAdmin, "employee-admin"},
	} {
		if _, err := db.Exec(`UPDATE roles SET name = ? WHERE name = ?`,
			renaming.to, renaming.from); err != nil {
			t.Fatalf("putting the old name %q back: %v", renaming.to, err)
		}
	}

	// And through that batch under the old names, stopping short of the rename so a
	// case can still set up the state it is about.
	migrate(t, db, retirement-1, theRename-1)
}

// grants reports what a role grants, as one comparable string.
func grants(t *testing.T, db *sql.DB, role string) string {
	t.Helper()

	return strings.Join(permissionsOf(t, db, role), ",")
}

// asTheModelHasIt is what a role should grant, in the same shape.
func asTheModelHasIt(name string) string {
	for _, role := range model.DefaultRoles() {
		if role.Name != name {
			continue
		}

		want := append([]string{}, role.Permissions...)
		sort.Strings(want)

		return strings.Join(want, ",")
	}

	return ""
}

// roleNames lists every role there is, sorted.
func roleNames(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT name FROM roles ORDER BY name`)
	if err != nil {
		t.Fatalf("listing the roles: %v", err)
	}

	defer func() { _ = rows.Close() }()

	var names []string

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading a role name: %v", err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("listing the roles: %v", err)
	}

	return names
}

// roleID returns the id of a role, or 0.
func roleID(t *testing.T, db *sql.DB, name string) uint {
	t.Helper()

	var id uint

	err := db.QueryRow(`SELECT id FROM roles WHERE name = ?`, name).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("looking for the %q role: %v", name, err)
	}

	return id
}

func TestTheEverydayRoleIsRenamedAndKeepsItsPeople(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	asAnOldInstallation(t, db)

	was := roleID(t, db, "employee")
	if was == 0 {
		t.Fatal("the fixture did not produce an employee role")
	}

	// Somebody on it, because the point of renaming rather than replacing is that
	// nobody moves. An account points at a role by id.
	person := person(t, db, "Ida", "ida@example.com")

	if _, err := db.Exec(`UPDATE users SET role_id = ? WHERE id = ?`, was, person); err != nil {
		t.Fatalf("putting Ida on the employee role: %v", err)
	}

	migrate(t, db, theRename-1, latest(t))

	if got := roleID(t, db, "employee"); got != 0 {
		t.Errorf("a role called employee is still there, as %d", got)
	}

	now := roleID(t, db, model.RoleUser)
	if now == 0 {
		t.Fatal("there is no role called user")
	}

	if now != was {
		t.Errorf("the role is a different row now (%d, was %d), so this replaced it "+
			"rather than renaming it - and everybody on the old one is pointing at a "+
			"role that no longer exists", now, was)
	}

	var on uint
	if err := db.QueryRow(`SELECT role_id FROM users WHERE id = ?`, person).Scan(&on); err != nil {
		t.Fatalf("reading Ida back: %v", err)
	}

	if on != was {
		t.Errorf("Ida is on role %d, want the renamed %d", on, was)
	}

	// The combined one too, which is the same rename and the one more easily forgotten.
	if got := roleID(t, db, "employee-admin"); got != 0 {
		t.Errorf("a role called employee-admin is still there, as %d", got)
	}

	if roleID(t, db, model.RoleUserAdmin) == 0 {
		t.Error("there is no role called user-admin")
	}

	// And the descriptions are the ones the application ships. An installation that
	// upgraded through the review-path migration while still calling the role employee
	// never got them, because that migration matched on today's names.
	for _, role := range model.DefaultRoles() {
		var described string

		if err := db.QueryRow(`SELECT description FROM roles WHERE name = ?`,
			role.Name).Scan(&described); err != nil {
			t.Fatalf("reading the description of %q: %v", role.Name, err)
		}

		if described != role.Description {
			t.Errorf("%q is described as %q, want %q", role.Name, described, role.Description)
		}
	}

	// Three roles and no more. A migration in the middle of the chain that looked the
	// everyday role up by today's name only would have found nothing on this
	// installation and seeded a second copy, which shows up here as a fourth role.
	if got := strings.Join(roleNames(t, db), ","); got != "admin,user,user-admin" {
		t.Errorf("this installation now has the roles %q, want admin,user,user-admin", got)
	}

	// And each grants what the model says. This is the assertion that catches the other
	// half of the same mistake: a migration that quietly did nothing because it was
	// looking for a name this installation did not have. The rights an ordinary account
	// gained when the review path was retired - its own projects, and moving an entry
	// between them - are handed out by exactly such a migration.
	for _, name := range []string{model.RoleUser, model.RoleUserAdmin, model.RoleAdmin} {
		if got, want := grants(t, db, name), asTheModelHasIt(name); got != want {
			t.Errorf("after the upgrade %q grants\n  %s\nwant\n  %s", name, got, want)
		}
	}
}

// An installation that already has a role called user keeps it, and the shipped one
// still gets the name.
//
// A perfectly reasonable thing to have built while the shipped role was called employee.
// The shipped name has to win, because the application looks this role up by it - so the
// one standing in the way is moved aside and keeps everything else it had.
//
// Moved rather than merged: merging would move people between roles, which is a decision
// about who may do what, and no migration should make that quietly. Two roles with
// similar names is a tidying job an administrator can see; an account that silently
// gained rights is not visible at all.
func TestARoleAlreadyCalledUserIsMovedAsideRatherThanMerged(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	asAnOldInstallation(t, db)

	roleHolding(t, db, model.RoleUser, "projects:read")

	squatter := roleID(t, db, model.RoleUser)
	shipped := roleID(t, db, "employee")

	// Somebody on the installation's own role, who must not be moved anywhere.
	theirs := person(t, db, "Ole", "ole@example.com")

	if _, err := db.Exec(`UPDATE users SET role_id = ? WHERE id = ?`, squatter, theirs); err != nil {
		t.Fatalf("putting Ole on their own role: %v", err)
	}

	migrate(t, db, theRename-1, latest(t))

	if got := roleID(t, db, model.RoleUser); got != shipped {
		t.Errorf("the role called user is %d, want the shipped %d: the name the "+
			"application looks this role up by has to be the shipped one", got, shipped)
	}

	moved := roleID(t, db, "user-2")
	if moved == 0 {
		t.Fatal("the installation's own role is nowhere; it was neither left in place " +
			"nor moved aside")
	}

	if moved != squatter {
		t.Errorf("user-2 is row %d, want the installation's own %d", moved, squatter)
	}

	// It keeps its rights, and Ole keeps it.
	if got := strings.Join(permissionsOf(t, db, "user-2"), ","); got != "projects:read" {
		t.Errorf("the moved role grants %q, want projects:read", got)
	}

	var on uint
	if err := db.QueryRow(`SELECT role_id FROM users WHERE id = ?`, theirs).Scan(&on); err != nil {
		t.Fatalf("reading Ole back: %v", err)
	}

	if on != squatter {
		t.Errorf("Ole is on role %d, want their own %d - a migration must not decide "+
			"that somebody's rights change", on, squatter)
	}
}

// The role name stored in the directory configuration is brought along.
//
// It lives inside a JSON document in the settings table. Missing it would not fail the
// upgrade: the name would simply match no role, and the first person to sign in from the
// directory after it would be refused at the one moment their account is meant to be
// created.
func TestTheDirectoryDefaultRoleIsRenamedToo(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	asAnOldInstallation(t, db)

	stored, err := json.Marshal(map[string]any{
		"Host": "ldap.example.com", "DefaultRole": "employee", "IDAttribute": "entryUUID",
	})
	if err != nil {
		t.Fatalf("building the stored configuration: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO settings (key_name, value, updated_at) VALUES (?, ?, '2026-08-12')`,
		"ldap.config", string(stored)); err != nil {
		t.Fatalf("storing the directory configuration: %v", err)
	}

	migrate(t, db, theRename-1, latest(t))

	var after string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key_name = ?`,
		"ldap.config").Scan(&after); err != nil {
		t.Fatalf("reading the configuration back: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(after), &config); err != nil {
		t.Fatalf("the configuration is no longer readable JSON: %v", err)
	}

	if got := config["DefaultRole"]; got != model.RoleUser {
		t.Errorf("the directory still provisions accounts into %q, which is a role that "+
			"no longer exists - the first person to sign in from the directory would be "+
			"refused", got)
	}

	// And the rest of the document survives, because this rewrites one field rather
	// than replacing the configuration.
	if got := config["Host"]; got != "ldap.example.com" {
		t.Errorf("the configured host is now %v; the rename overwrote settings it has "+
			"no business touching", got)
	}
}

// A role the installation named itself is left alone in the directory configuration.
func TestADirectoryDefaultThatIsNotAShippedRoleIsLeftAlone(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	asAnOldInstallation(t, db)

	if _, err := db.Exec(
		`INSERT INTO settings (key_name, value, updated_at) VALUES (?, ?, '2026-08-12')`,
		"ldap.config", `{"DefaultRole":"contractors"}`); err != nil {
		t.Fatalf("storing the directory configuration: %v", err)
	}

	migrate(t, db, theRename-1, latest(t))

	var after string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key_name = ?`,
		"ldap.config").Scan(&after); err != nil {
		t.Fatalf("reading the configuration back: %v", err)
	}

	if !strings.Contains(after, "contractors") {
		t.Errorf("the configuration is now %q; a role an installation named itself keeps "+
			"its name", after)
	}
}

// A fresh installation comes out with exactly three roles, named as the model names
// them.
//
// The case that catches the subtler half of this change. The migration that first seeds
// roles builds them from the model, so a fresh chain is seeded with today's names from
// the very beginning - and every migration in the middle that looks the everyday role up
// by name is therefore looking at the new one, while an existing installation has the
// old one. A migration that matched only one of the two would silently do nothing on
// half the installations there are.
//
// Counting the roles is what makes this bite: a middle migration that seeded a role
// because it could not find the name it expected would show up here as a fourth.
func TestAFreshInstallationHasTheThreeShippedRolesAndNoOthers(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, latest(t))

	want := "admin,user,user-admin"
	if got := strings.Join(roleNames(t, db), ","); got != want {
		t.Errorf("a fresh installation has the roles %q, want %q\n\nA name left over is "+
			"an old one that was not renamed; an extra one is a migration in the middle "+
			"of the chain seeding a role because it looked for a name this installation "+
			"never had", got, want)
	}

	// And each of them grants what the model says, so a middle migration cannot have
	// quietly handed one of them a right on the way past.
	for _, role := range model.DefaultRoles() {
		if got, want := grants(t, db, role.Name), asTheModelHasIt(role.Name); got != want {
			t.Errorf("the seeded role %q grants\n  %s\nwant\n  %s", role.Name, got, want)
		}
	}
}
