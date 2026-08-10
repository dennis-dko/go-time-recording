package migrations_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// Every project comes out of the upgrade belonging to somebody.
//
// There were two kinds: a shared project everybody could see and book on, and a
// private category for organising your own day. Each account is its own world now, so
// the second is the only kind - which leaves the question of who owns the first.
//
// Copied, not handed over. A shared project two colleagues booked on becomes a project
// each, keeping the name, and each person's entries move to their own copy. Handing it
// to one of them would have left the other's hours attached to a project they could no
// longer see: present in the database, invisible on screen, and impossible to account
// for.

// theSplit is the migration that gives every project an owner.
const theSplit = int64(20260810050000)

// bookedOn records a time entry, straight into the table: this is about what the
// migration does to rows, so the service layer's rules are beside the point.
func bookedOn(t *testing.T, db *sql.DB, userID, projectID uint, day string, hours float64) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO timesheets (user_id, project_id, date, duration_hours)
		VALUES (?, ?, ?, ?)`, userID, projectID, day, hours)
	if err != nil {
		t.Fatalf("booking %v hours for user %d: %v", hours, userID, err)
	}
}

// person creates an account and returns its id.
func person(t *testing.T, db *sql.DB, name, email string) uint {
	t.Helper()

	_, err := db.Exec(`INSERT INTO users
		(name, email, role_id, password_hash, daily_target_hours, max_daily_hours)
		VALUES (?, ?, (SELECT id FROM roles WHERE name = 'employee'), 'x', 0, 0)`,
		name, email)
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}

	var id uint

	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatalf("reading %s back: %v", name, err)
	}

	return id
}

func TestASharedProjectBecomesOneProjectPerPersonWhoBookedOnIt(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, theSplit-1)

	anna := person(t, db, "Anna", "anna@example.test")
	bert := person(t, db, "Bert", "bert@example.test")

	// One shared project, which is what an owner of NULL meant.
	if _, err := db.Exec(`INSERT INTO projects (name, description, start_date, status)
		VALUES ('Roof', 'tiles', '2026-08-01', 'active')`); err != nil {
		t.Fatalf("creating the shared project: %v", err)
	}

	var roof uint

	if err := db.QueryRow(`SELECT id FROM projects WHERE name = 'Roof'`).Scan(&roof); err != nil {
		t.Fatalf("reading the project back: %v", err)
	}

	bookedOn(t, db, anna, roof, "2026-08-03", 6)
	bookedOn(t, db, anna, roof, "2026-08-04", 2)
	bookedOn(t, db, bert, roof, "2026-08-03", 3)

	migrate(t, db, theSplit-1, latest(t))

	// Two projects called Roof, one each.
	for _, who := range []struct {
		name  string
		id    uint
		hours float64
	}{{"Anna", anna, 8}, {"Bert", bert, 3}} {
		var projects int

		err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE name = 'Roof' AND owner_id = ?`,
			who.id).Scan(&projects)
		if err != nil {
			t.Fatalf("counting %s's copies: %v", who.name, err)
		}

		if projects != 1 {
			t.Errorf("%s has %d project(s) called Roof, want 1", who.name, projects)
		}

		// And their hours are on their own copy, all of them.
		var hours float64

		err = db.QueryRow(`SELECT COALESCE(SUM(t.duration_hours), 0) FROM timesheets t
			JOIN projects p ON p.id = t.project_id
			WHERE t.user_id = ? AND p.owner_id = ?`, who.id, who.id).Scan(&hours)
		if err != nil {
			t.Fatalf("totalling %s's hours: %v", who.name, err)
		}

		if hours != who.hours {
			t.Errorf("%s has %v hours on their own project, want %v", who.name, hours, who.hours)
		}
	}

	// Nothing is left pointing at a project its owner cannot see, which is the whole
	// point of copying rather than handing over.
	var stranded int

	err := db.QueryRow(`SELECT COUNT(*) FROM timesheets t
		JOIN projects p ON p.id = t.project_id
		WHERE p.owner_id IS NULL OR p.owner_id <> t.user_id`).Scan(&stranded)
	if err != nil {
		t.Fatalf("looking for stranded entries: %v", err)
	}

	if stranded != 0 {
		t.Errorf("%d entry(s) are booked on a project that is not the booker's", stranded)
	}

	// And no hour went missing on the way.
	var total float64

	if err := db.QueryRow(`SELECT SUM(duration_hours) FROM timesheets`).Scan(&total); err != nil {
		t.Fatalf("totalling every entry: %v", err)
	}

	if total != 11 {
		t.Errorf("the entries total %v hours after the split, want 11", total)
	}
}

// A shared project nobody ever booked on is removed.
//
// Nobody has a claim on it, and leaving it without an owner would leave a row no reader
// can reach - sitting in the table failing the promise that every project belongs to
// somebody. What is lost is a name and a date range nothing was recorded against.
func TestASharedProjectWithNoBookingsIsRemoved(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, theSplit-1)

	if _, err := db.Exec(`INSERT INTO projects (name, start_date, status)
		VALUES ('Nobody booked this', '2026-08-01', 'active')`); err != nil {
		t.Fatalf("creating the project: %v", err)
	}

	migrate(t, db, theSplit-1, latest(t))

	var left int

	err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE name = 'Nobody booked this'`).
		Scan(&left)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}

	if left != 0 {
		t.Errorf("an unclaimed project survived the split, invisible to everybody")
	}
}

// Not one project without an owner, whatever the installation looked like before.
//
// The invariant the model now rests on: VisibleTo answers "no" for a project with no
// owner, so one left behind is a row nobody can reach.
func TestNoProjectIsLeftWithoutAnOwner(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, theSplit-1)

	anna := person(t, db, "Anna", "anna@example.test")

	// A mix: one booked on, one not, and one that was already somebody's category.
	for _, statement := range []string{
		`INSERT INTO projects (name, start_date, status) VALUES ('Booked', '2026-08-01', 'active')`,
		`INSERT INTO projects (name, start_date, status) VALUES ('Empty', '2026-08-01', 'active')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("creating a project: %v", err)
		}
	}

	if _, err := db.Exec(`INSERT INTO projects (name, start_date, status, owner_id)
		VALUES ('Mine already', '2026-08-01', 'active', ?)`, anna); err != nil {
		t.Fatalf("creating the owned project: %v", err)
	}

	var booked uint

	if err := db.QueryRow(`SELECT id FROM projects WHERE name = 'Booked'`).Scan(&booked); err != nil {
		t.Fatalf("reading it back: %v", err)
	}

	bookedOn(t, db, anna, booked, "2026-08-03", 4)

	migrate(t, db, theSplit-1, latest(t))

	var ownerless int

	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE owner_id IS NULL`).
		Scan(&ownerless); err != nil {
		t.Fatalf("counting projects without an owner: %v", err)
	}

	if ownerless != 0 {
		t.Errorf("%d project(s) have no owner, so nobody can see them", ownerless)
	}

	// The one that was already hers is untouched, so this cannot pass by having
	// deleted everything.
	var mine int

	err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE name = 'Mine already' AND owner_id = ?`,
		anna).Scan(&mine)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}

	if mine != 1 {
		t.Errorf("the project that already had an owner is gone")
	}
}

// The right to keep a project of your own survives under its new name.
//
// projects:write:own and projects:write were two rights for two kinds of project. With
// one kind, one of them would grant nothing - so whoever held the old one holds the
// remaining one, and nobody is left holding neither.
func TestWhoeverCouldKeepACategoryCanStillKeepAProject(t *testing.T) {
	t.Parallel()

	db := freshDB(t)
	migrate(t, db, 0, theSplit-1)

	// A role an installation built for itself, with only the own-project right.
	if _, err := db.Exec(`INSERT INTO roles (name, description, is_system)
		VALUES ('categories-only', 'keeps their own categories', 0)`); err != nil {
		t.Fatalf("creating the role: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO role_permissions (role_id, permission)
		SELECT id, 'projects:write:own' FROM roles WHERE name = 'categories-only'`); err != nil {
		t.Fatalf("granting the old right: %v", err)
	}

	migrate(t, db, theSplit-1, latest(t))

	var writes int

	err := db.QueryRow(`SELECT COUNT(*) FROM role_permissions
		WHERE permission = 'projects:write'
		  AND role_id = (SELECT id FROM roles WHERE name = 'categories-only')`).Scan(&writes)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}

	if writes != 1 {
		t.Error("a role that could keep its own categories can no longer keep a project")
	}

	// And the old right is gone everywhere, or the role editor would offer one that
	// grants nothing.
	var old int

	if err := db.QueryRow(`SELECT COUNT(*) FROM role_permissions
		WHERE permission = 'projects:write:own'`).Scan(&old); err != nil {
		t.Fatalf("counting: %v", err)
	}

	if old != 0 {
		t.Errorf("%d role(s) still hold projects:write:own, which grants nothing", old)
	}
}
