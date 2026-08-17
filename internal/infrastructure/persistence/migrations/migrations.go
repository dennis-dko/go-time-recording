// Package migrations defines the database schema as GoFr migrations, so a
// fresh binary provisions its own database on first start and no external
// schema tooling is needed for deployment.
package migrations

import (
	"encoding/json"
	"fmt"
	"strings"

	"gofr.dev/pkg/gofr/migration"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
)

// All returns the migration set for the given dialect, keyed by version.
//
// The dialect is passed in rather than detected because GoFr's
// migration.Datasource exposes only query methods, not the dialect, and the
// DDL for auto-increment keys and timestamps genuinely differs per engine.
func All(dialect string) map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20260730120000: {UP: func(d migration.Datasource) error {
			return execAll(d, createUsers(dialect), createProjects(dialect), createTimesheets(dialect))
		}},
		20260730120100: {UP: func(d migration.Datasource) error {
			return execAll(d,
				"CREATE INDEX idx_timesheets_user_date ON timesheets (user_id, date)",
				"CREATE INDEX idx_timesheets_project ON timesheets (project_id)",
			)
		}},
		20260731010000: {UP: func(d migration.Datasource) error {
			return addRoleBasedAccess(d, dialect)
		}},
		20260731020000: {UP: func(d migration.Datasource) error {
			return addSessionsAndPreferences(d, dialect)
		}},
		20260801010000: {UP: func(d migration.Datasource) error {
			return makeProjectOptional(d, dialect)
		}},
		20260801010100: {UP: func(d migration.Datasource) error {
			return createSettings(d, dialect)
		}},
		20260801020000: {UP: func(d migration.Datasource) error {
			return addPrivateProjects(d, dialect)
		}},
		20260801030000: {UP: func(d migration.Datasource) error {
			return createAPITokens(d, dialect)
		}},
		20260801040000: {UP: func(d migration.Datasource) error {
			return addExternalIdentity(d, dialect)
		}},
		20260801050000: {UP: func(d migration.Datasource) error {
			return addUserTimezone(d, dialect)
		}},
		20260802010000: {UP: func(d migration.Datasource) error {
			return addUserTourSeen(d, dialect)
		}},
		20260803010000: {UP: func(d migration.Datasource) error {
			return createPasskeys(d, dialect)
		}},
		20260805010000: {UP: func(d migration.Datasource) error {
			return createRunningTimers(d, dialect)
		}},
		20260805020000: {UP: func(d migration.Datasource) error {
			return separateSystemAdministration(d, dialect)
		}},
		20260810010000: {UP: func(d migration.Datasource) error {
			return retireTheReviewPath(d, dialect)
		}},
		20260810020000: {UP: func(d migration.Datasource) error {
			return repairAccountsWithoutARole(d, dialect)
		}},
		20260810030000: {UP: func(d migration.Datasource) error {
			return retireTheSeparateReportRight(d, dialect)
		}},
		20260810040000: {UP: func(d migration.Datasource) error {
			return handWorkingTimesToTheirOwners(d, dialect)
		}},
		20260810050000: {UP: func(d migration.Datasource) error {
			return giveEveryProjectAnOwner(d, dialect)
		}},
		20260810060000: {UP: func(d migration.Datasource) error {
			return separateAdministeringFromWorking(d, dialect)
		}},
		20260811010000: {UP: func(d migration.Datasource) error {
			return retireReadingEverybodysTime(d, dialect)
		}},
		20260812010000: {UP: func(d migration.Datasource) error {
			return callTheEverydayRoleUser(d, dialect)
		}},
		20260813010000: {UP: func(d migration.Datasource) error {
			return letAdministratorsReachTheSettings(d, dialect)
		}},
		20260814010000: {UP: func(d migration.Datasource) error {
			return clearTheAdministratorsWorkingDay(d, dialect)
		}},
		20260818010000: {UP: func(d migration.Datasource) error {
			return moveTheAppearanceOntoTheAccount(d, dialect)
		}},
	}
}

// clearTheAdministratorsWorkingDay takes the daily target off the built-in
// administrator.
//
// It was seeded with the default eight, from when that account recorded time
// like anybody else. It does not: it cannot book an hour, read a figure or open
// the working-times card, which is hidden for it. So the figure was read by
// nothing - and shown in exactly one place, the account table, where every other
// row said "default" and this one said 8.0 for no reason a reader could work
// out.
//
// Zero is not "no target" but "follow the instance default", which is what every
// account starts on. For this one it is the honest answer: it has no working day
// to have a target for, and if it ever gained one the instance default is what a
// new account would get anyway.
func clearTheAdministratorsWorkingDay(d migration.Datasource, dialect string) error {
	if _, err := d.SQL.Exec(sqldb.Rebind(dialect,
		"UPDATE users SET daily_target_hours = 0 WHERE is_system = ?"), true); err != nil {
		return fmt.Errorf("clearing the built-in administrator's daily target: %w", err)
	}

	return nil
}

// letAdministratorsReachTheSettings grants settings:manage to every role that
// already administers the accounts.
//
// Until now the Settings screen answered to one account: the built-in one, checked
// by name rather than by right. So the combined role - somebody who works here and
// also administers - held the accounts and the roles and could not open the
// database, directory or maintenance screens at all. The way round it was to sign
// in as the built-in account, which is the one account whose actions cannot be
// attributed to a person.
//
// Keyed on users:write rather than on a role name, because a role's name is a
// thing installations change and this has to find the ones that exist rather than
// the ones this application shipped. Whoever already decides who may sign in is
// not gaining reach they did not have.
//
// What this costs is written down where it is enforced, in
// Authorizer.RequireInstallationAdmin: a right that can be granted is a right that
// can be granted to yourself, if you hold roles:write.
func letAdministratorsReachTheSettings(d migration.Datasource, dialect string) error {
	return grantToAllRolesHolding(d, dialect, model.PermUserWrite, model.PermSettingsManage)
}

// callTheEverydayRoleUser renames employee to user, and employee-admin to user-admin.
//
// The word said more than this application knows. It holds accounts, and whether the
// person behind one is employed here, contracted, a volunteer or the only person in the
// company is not something it records, checks or needs - what it knows is that they use
// it. The role is also what a fresh installation puts everybody in, including whoever
// installed it, and calling that person an employee of themselves reads as a mistake.
//
// A rename rather than a new role beside the old one. Every account points at a role by
// id, so nobody is moved and nobody loses anything; what changes is the name the
// application looks the role up by, which is why this has to happen in the database
// rather than only in the model.
//
// Three things carry the name and all three are handled here: the roles table, the
// description that goes with each shipped role - which the migration that retired the
// review path could not update on an installation still using the old names - and the
// directory configuration, where the name of the role given to accounts provisioned
// from LDAP is stored inside a JSON blob.
func callTheEverydayRoleUser(d migration.Datasource, dialect string) error {
	for _, renaming := range []struct{ from, to string }{
		{"employee", model.RoleUser},
		{"employee-admin", model.RoleUserAdmin},
	} {
		if err := renameRole(d, dialect, renaming.from, renaming.to); err != nil {
			return err
		}
	}

	// The descriptions of the roles that ship, now that they can be found by the names
	// the model uses. This is also the repair for an installation that upgraded through
	// the review-path migration while still calling the role employee: that one matched
	// on today's names and updated nothing.
	describe := sqldb.Rebind(dialect, "UPDATE roles SET description = ? WHERE name = ?")

	for _, role := range model.DefaultRoles() {
		if _, err := d.SQL.Exec(describe, role.Description, role.Name); err != nil {
			return fmt.Errorf("describing %q: %w", role.Name, err)
		}
	}

	return pointTheDirectoryAtTheRenamedRole(d, dialect)
}

// renameRole renames one role, and does nothing if there is nothing to rename.
//
// The interesting case is a collision: an installation that built its own role called
// user, which is a perfectly reasonable thing to have done while the shipped one was
// called employee. The shipped name has to win, because the application looks this role
// up by it - the LDAP default, the account that arrives without a role, the fallback in
// the migration that first moved roles into the database. So the one standing in the way
// is moved aside with a suffix and keeps everything else: its rights, its description,
// and every account on it.
//
// Renamed rather than merged. Merging would move people between roles, which is a
// decision about who may do what, and no migration should make that quietly. Two roles
// with similar names is a tidying job an administrator can see and do; an account that
// silently gained or lost rights is not visible at all.
func renameRole(d migration.Datasource, dialect string, from, to string) error {
	taken, err := firstRoleThatExists(d, dialect, from)
	if err != nil {
		return err
	}

	if taken == "" {
		// Nothing to rename: a fresh installation was seeded with the new name from
		// the start, because the seeding migration builds the roles from the model.
		return nil
	}

	inTheWay, err := firstRoleThatExists(d, dialect, to)
	if err != nil {
		return err
	}

	if inTheWay != "" {
		free, err := aFreeRoleName(d, dialect, to)
		if err != nil {
			return err
		}

		if err := setRoleName(d, dialect, to, free); err != nil {
			return fmt.Errorf("moving the existing %q role aside: %w", to, err)
		}
	}

	return setRoleName(d, dialect, from, to)
}

// aFreeRoleName finds a name nothing is using, by counting up from name-2.
//
// Starting at 2 rather than 1 because the one already there is the first of them, and
// bounded so a database that answers strangely cannot spin here for ever.
func aFreeRoleName(d migration.Datasource, dialect, name string) (string, error) {
	for suffix := 2; suffix < 100; suffix++ {
		candidate := fmt.Sprintf("%s-%d", name, suffix)

		taken, err := firstRoleThatExists(d, dialect, candidate)
		if err != nil {
			return "", err
		}

		if taken == "" {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no free name near %q after 98 attempts", name)
}

func setRoleName(d migration.Datasource, dialect, from, to string) error {
	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "UPDATE roles SET name = ? WHERE name = ?"), to, from); err != nil {
		return fmt.Errorf("renaming the %q role to %q: %w", from, to, err)
	}

	return nil
}

// pointTheDirectoryAtTheRenamedRole updates the role name stored in the LDAP settings.
//
// It lives inside a JSON document in the settings table, so this reads it, changes the
// one field and writes it back rather than editing the text - a string replacement would
// depend on how the encoder happened to space the document out.
//
// Getting this wrong would not be visible until somebody signed in from the directory
// for the first time after the upgrade: the name would match no role, and the account
// would be refused at the one moment it is meant to be created.
func pointTheDirectoryAtTheRenamedRole(d migration.Datasource, dialect string) error {
	var stored string

	err := d.SQL.QueryRow(
		sqldb.Rebind(dialect, "SELECT value FROM settings WHERE key_name = ?"),
		"ldap.config").Scan(&stored)
	if err != nil || strings.TrimSpace(stored) == "" {
		// No directory configured, which is the ordinary case. A missing row is
		// sql.ErrNoRows here and not a problem worth failing an upgrade over; a
		// corrupt one is left alone for the same reason the settings screen leaves
		// it alone - it has to stay repairable from the interface.
		return nil
	}

	var config map[string]any

	if err := json.Unmarshal([]byte(stored), &config); err != nil {
		return nil
	}

	// The field has no JSON tag on the model, so it is spelled as the Go field is.
	current, ok := config["DefaultRole"].(string)
	if !ok {
		return nil
	}

	switch current {
	case "employee":
		config["DefaultRole"] = model.RoleUser
	case "employee-admin":
		config["DefaultRole"] = model.RoleUserAdmin
	default:
		// A role this installation named itself, which keeps its name.
		return nil
	}

	patched, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("rewriting the directory configuration: %w", err)
	}

	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "UPDATE settings SET value = ? WHERE key_name = ?"),
		string(patched), "ldap.config"); err != nil {
		return fmt.Errorf("storing the directory configuration: %w", err)
	}

	return nil
}

// retireReadingEverybodysTime withdraws timesheets:read:all and timesheets:write:all
// from every role.
//
// They were the last of the manager. One opened everybody's entries, balances, totals
// and exports; the other let somebody book and change time in another person's name.
// No default role has held either since the review path was retired, and by then the
// four screens that asked "which person" had been narrowed to a dropdown with one
// entry, so ticking one changed nothing anybody could see - while the API went on
// answering every question about every colleague.
//
// Each role keeps the half of what it held that is still a right: whoever could read
// everybody's time could read their own, and whoever could write everybody's could
// write their own. Granted before the wider one is withdrawn, in that order, so no
// role is left granting nothing - a role that grants nothing is one somebody assigns
// and then wonders about, because its holder signs in to an interface with no screen
// on it.
//
// The two names are literals. This migration is the reason the constants no longer
// exist, and a finished migration that referred to them would have to be edited every
// time the model moves on.
func retireReadingEverybodysTime(d migration.Datasource, dialect string) error {
	for _, right := range []struct{ all, own string }{
		{"timesheets:read:all", model.PermTimesheetReadOwn},
		{"timesheets:write:all", model.PermTimesheetWriteOwn},
	} {
		if err := grantToAllRolesHolding(d, dialect, right.all, right.own); err != nil {
			return err
		}

		if _, err := d.SQL.Exec(
			sqldb.Rebind(dialect, "DELETE FROM role_permissions WHERE permission = ?"),
			right.all); err != nil {
			return fmt.Errorf("withdrawing %q: %w", right.all, err)
		}
	}

	return nil
}

// separateAdministeringFromWorking takes the working day off the built-in account and
// offers it as a role instead.
//
// The account that exists on every installation before anybody has chosen anything is
// how you get in, not somebody's working day. It used to book and read its own hours
// "like anybody else who works here"; whoever does work here has an account of their
// own, and if that person also administers, they get a role that says so rather than a
// second sign-in.
//
// Two halves, in this order. The role is seeded first, so an installation that had
// somebody using the built-in account for both jobs has somewhere to move them before
// the rights come off it - and this migration deliberately does not move anybody. Who
// works here is not something a database can work out, and guessing would either
// invent a colleague or take an administrator's hours away.
//
// What it does instead: the built-in administrator keeps its recorded entries, its
// projects and its figures in the tables. They stop being reachable through the
// interface, because the rights that reach them are gone - and they come back the
// moment somebody assigns it the combined role, which is the honest way to undo this
// if an installation wants the old arrangement.
func separateAdministeringFromWorking(d migration.Datasource, dialect string) error {
	// The role that spans both jobs, so there is somewhere to put whoever needs it.
	//
	// Under either name, because this migration runs before the one that renames it -
	// and looking for one name only would seed a second copy of a role that is already
	// there under the other.
	combined, err := theCombinedRoleName(d, dialect)
	if err != nil {
		return err
	}

	if combined == "" {
		if err := seedRole(d, dialect, shippedRole(model.RoleUserAdmin)); err != nil {
			return err
		}
	}

	// And the working day comes off the built-in administrator's own role. Only that
	// one: a role somebody built for themselves is their business, and this is about
	// what arrives with the software.
	revoke := sqldb.Rebind(dialect,
		`DELETE FROM role_permissions
		  WHERE permission = ?
		    AND role_id = (SELECT id FROM roles WHERE name = ? AND is_system = TRUE)`)

	// TRUE is spelled differently on SQLite, which stores it as 1.
	if dialect == sqldb.DialectSQLite {
		revoke = strings.Replace(revoke, "is_system = TRUE", "is_system = 1", 1)
	}

	for _, permission := range []string{
		"projects:read", "projects:write", "projects:archive", "projects:delete",
		"timesheets:read:own", "timesheets:write:own",
		"reports:read:own", "settings:write:own",
	} {
		if _, err := d.SQL.Exec(revoke, permission, model.RoleAdmin); err != nil {
			return fmt.Errorf("taking %q off the built-in administrator: %w", permission, err)
		}
	}

	return nil
}

// giveEveryProjectAnOwner turns the shared projects into personal ones.
//
// There were two kinds: a shared project everybody could see and book on, and a
// private category for organising your own day. Each account is its own world now, so
// the second is the only kind - which leaves the question of who owns the first.
//
// Copied rather than handed to one person. A shared project two colleagues booked on
// becomes a project each, keeping the name, and each person's entries move to their
// own copy. Nobody loses an hour and nobody loses sight of one, which handing the
// project to the largest booker would have done to everybody else: their entries would
// have stayed attached to a project they could no longer see.
//
// The cost is duplicate names, and it is the honest one: two people who worked on the
// same thing now each have their own record of it, which is exactly what "no view
// across accounts" means.
//
// A shared project with no bookings at all is deleted. Nobody has a claim on it, and
// leaving it without an owner would leave a row no reader can reach - which is worse
// than removing it, because it would sit in the table failing the promise that every
// project belongs to somebody. What is lost is a name and a date range that nothing
// was ever recorded against.
//
// projects:write:own goes at the same time, into projects:write. Two rights told the
// two kinds apart; with one kind, one of them would grant nothing.
func giveEveryProjectAnOwner(d migration.Datasource, dialect string) error {
	shared, err := sharedProjectIDs(d)
	if err != nil {
		return err
	}

	for _, projectID := range shared {
		if err := splitProjectPerBooker(d, dialect, projectID); err != nil {
			return err
		}
	}

	// Whoever could keep a private category may now keep a project, because that is
	// the same thing - and keeping one means finishing it and removing it as well,
	// which the own-project right already allowed for your own. Granted before the old
	// right is withdrawn, so nobody is left holding neither.
	for _, permission := range []string{
		model.PermProjectWrite, model.PermProjectArchive, model.PermProjectDelete,
	} {
		if err := grantToAllRolesHolding(d, dialect, "projects:write:own", permission); err != nil {
			return err
		}
	}

	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "DELETE FROM role_permissions WHERE permission = ?"),
		"projects:write:own"); err != nil {
		return fmt.Errorf("withdrawing the own-project right: %w", err)
	}

	return nil
}

// sharedProjectIDs lists the projects that belong to nobody.
func sharedProjectIDs(d migration.Datasource) ([]uint, error) {
	rows, err := d.SQL.Query("SELECT id FROM projects WHERE owner_id IS NULL ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("looking for projects without an owner: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var ids []uint

	for rows.Next() {
		var id uint
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading a project id: %w", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the projects without an owner: %w", err)
	}

	return ids, nil
}

// splitProjectPerBooker gives one shared project to each person who booked on it.
func splitProjectPerBooker(d migration.Datasource, dialect string, projectID uint) error {
	bookers, err := bookersOf(d, dialect, projectID)
	if err != nil {
		return err
	}

	if len(bookers) == 0 {
		// Nobody's, and nothing recorded against it.
		if _, err := d.SQL.Exec(
			sqldb.Rebind(dialect, "DELETE FROM projects WHERE id = ?"), projectID); err != nil {
			return fmt.Errorf("removing an unclaimed project: %w", err)
		}

		return nil
	}

	// The first becomes the owner of the original, so one person's entries need not
	// move at all and the id they already point at stays valid.
	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "UPDATE projects SET owner_id = ? WHERE id = ?"),
		bookers[0], projectID); err != nil {
		return fmt.Errorf("giving project %d an owner: %w", projectID, err)
	}

	for _, booker := range bookers[1:] {
		copyID, err := copyProjectTo(d, dialect, projectID, booker)
		if err != nil {
			return err
		}

		if _, err := d.SQL.Exec(sqldb.Rebind(dialect,
			"UPDATE timesheets SET project_id = ? WHERE project_id = ? AND user_id = ?"),
			copyID, projectID, booker); err != nil {
			return fmt.Errorf("moving the entries of user %d: %w", booker, err)
		}
	}

	return nil
}

// bookersOf lists everybody who recorded time against a project, oldest account
// first so the choice of who keeps the original is not left to the engine.
func bookersOf(d migration.Datasource, dialect string, projectID uint) ([]uint, error) {
	rows, err := d.SQL.Query(sqldb.Rebind(dialect,
		"SELECT DISTINCT user_id FROM timesheets WHERE project_id = ? ORDER BY user_id"),
		projectID)
	if err != nil {
		return nil, fmt.Errorf("looking for who booked on project %d: %w", projectID, err)
	}

	defer func() { _ = rows.Close() }()

	var ids []uint

	for rows.Next() {
		var id uint
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading a user id: %w", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading who booked on project %d: %w", projectID, err)
	}

	return ids, nil
}

// copyProjectTo duplicates a project for one person and returns the copy's id.
//
// Inserted from a select so every column comes along without this having to name them
// twice, then read back by owner and name rather than by a last-inserted id: that is
// available on two of the three engines this runs on and not on the third.
func copyProjectTo(
	d migration.Datasource,
	dialect string,
	projectID uint,
	owner uint,
) (uint, error) {
	if _, err := d.SQL.Exec(sqldb.Rebind(dialect,
		`INSERT INTO projects (name, description, start_date, end_date, status, owner_id)
		 SELECT name, description, start_date, end_date, status, ? FROM projects WHERE id = ?`),
		owner, projectID); err != nil {
		return 0, fmt.Errorf("copying project %d for user %d: %w", projectID, owner, err)
	}

	var copyID uint

	err := d.SQL.QueryRow(sqldb.Rebind(dialect,
		`SELECT id FROM projects WHERE owner_id = ?
		   AND name = (SELECT name FROM projects WHERE id = ?)
		 ORDER BY id DESC`), owner, projectID).Scan(&copyID)
	if err != nil {
		return 0, fmt.Errorf("finding the copy of project %d for user %d: %w",
			projectID, owner, err)
	}

	return copyID, nil
}

// grantToAllRolesHolding gives a permission to every role that holds another one.
func grantToAllRolesHolding(d migration.Datasource, dialect, holds, grant string) error {
	if _, err := d.SQL.Exec(sqldb.Rebind(dialect,
		`INSERT INTO role_permissions (role_id, permission)
		 SELECT DISTINCT rp.role_id, ? FROM role_permissions rp
		  WHERE rp.permission = ?
		    AND NOT EXISTS (
			  SELECT 1 FROM role_permissions other
			   WHERE other.role_id = rp.role_id AND other.permission = ?)`),
		grant, holds, grant); err != nil {
		return fmt.Errorf("granting %q to every role holding %q: %w", grant, holds, err)
	}

	return nil
}

// handWorkingTimesToTheirOwners withdraws settings:write:other from every role.
//
// A daily target and a daily ceiling are time figures, and everything to do with time
// belongs to the person it is about. The right existed so this installation's
// administrator could set them for somebody else, and the reason that does not work is
// the arrangement itself: the administrator cannot read that person's entries, their
// balance or their figures. Setting a number whose effect is invisible to you is not
// administration, and the number's only consumer is an overtime balance nobody but its
// owner may see.
//
// It was not the lock it looked like either. The same two fields were writable through
// PUT /users/{id} and through the spreadsheet import, both of which check only
// users:write - so the right guarded one of three doors. All three are closed now, and
// the one that remains asks whose account it is.
//
// Nothing is lost by withdrawing it. The instance-wide default under Settings is what
// a new account gets, and its owner changes it from there.
func handWorkingTimesToTheirOwners(d migration.Datasource, dialect string) error {
	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "DELETE FROM role_permissions WHERE permission = ?"),
		"settings:write:other"); err != nil {
		return fmt.Errorf("withdrawing the right to set somebody else's working times: %w", err)
	}

	return nil
}

// retireTheSeparateReportRight withdraws reports:read from every role.
//
// Whether somebody may see another person's recorded time is one question, and
// timesheets:read:all is the one right that answers it. reports:read was a second
// answer to the same question, asked of a total rather than of a list, and it
// belonged to the role that reviewed other people's hours. That role is gone.
//
// What it left behind was worse than untidy: no role held it, and a whole screen was
// gated on it, so the project report was unreachable on every installation - while
// anybody who did grant it would have got a per-colleague breakdown of hours, which
// is exactly what nobody is meant to see. The report now covers the caller's own
// hours, and only the one right widens that.
//
// Nothing replaces it. A role that held it keeps timesheets:read:all if it had that,
// and an installation that wants somebody to see everybody's time grants that one.
func retireTheSeparateReportRight(d migration.Datasource, dialect string) error {
	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "DELETE FROM role_permissions WHERE permission = ?"),
		"reports:read"); err != nil {
		return fmt.Errorf("withdrawing the separate report right: %w", err)
	}

	return nil
}

// repairAccountsWithoutARole puts back a role for every account left without one,
// and makes sure there is one to put back.
//
// The migration before this one moves whoever was a manager to the employee role.
// It does that with a subselect on the role name, and a subselect that matches
// nothing yields NULL rather than nothing - so on an installation that had renamed
// or deleted the default employee role, every ex-manager's role_id became NULL.
//
// That is worse than it sounds. The users table is read into a struct whose role id
// is a plain uint, so a NULL in that column is not a user with no rights: it is a
// scan error. The affected person cannot sign in, and every listing of accounts
// fails for everybody, because one unreadable row fails the whole query. An
// administrator would see an empty user list and no reason for it.
//
// Written as a new migration rather than as a correction to the one that caused it.
// That one has shipped; an installation that already ran it has the NULL rows now,
// and editing it would repair nothing while quietly changing history for anyone who
// has not upgraded yet. This runs in the same start-up either way.
//
// What it cannot do is guess which role a renamed one was meant to be. An
// installation that renamed the default employee role gets the default back,
// alongside whatever they renamed it to, and their ex-managers land in it: two roles
// where there was one, which is a tidying job for an administrator rather than an
// account nobody can sign in to.
func repairAccountsWithoutARole(d migration.Datasource, dialect string) error {
	// A destination has to exist before anybody can be pointed at it, under whichever
	// name this installation calls it - and seeded only if it calls it neither, which
	// is the broken case this repair is for. An ordinary upgrade changes nothing here.
	ordinary, err := theOrdinaryRoleName(d, dialect)
	if err != nil {
		return err
	}

	if ordinary == "" {
		ordinary = model.RoleUser

		if err := seedRole(d, dialect, shippedRole(model.RoleUser)); err != nil {
			return err
		}
	}

	// Everybody who has no role, whatever left them that way.
	adopt := sqldb.Rebind(dialect, `UPDATE users SET role_id =
		(SELECT id FROM roles WHERE name = ?) WHERE role_id IS NULL`)

	if _, err := d.SQL.Exec(adopt, ordinary); err != nil {
		return fmt.Errorf("giving roleless accounts the %q role: %w", ordinary, err)
	}

	return nil
}

// retireTheReviewPath removes the entry status and the role that reviewed it.
//
// An entry travelled open -> submitted -> approved, and an approved one was locked
// against every later change. That needs somebody to do the approving, and there is
// nobody: everyone keeps their own hours and the built-in administrator runs the
// installation rather than reading other people's work. A review path with no
// reviewer is a step that only ever stands between a person and the time they
// recorded.
//
// So the column goes, and with it the manager role, the approve right, and the
// nightly sweep that moved stale entries along. Whoever was a manager becomes an
// ordinary account, which now carries everything about its own work - including its
// own projects.
func retireTheReviewPath(d migration.Datasource, dialect string) error {
	// Whichever name this installation calls the everyday role. Read once, up here,
	// because the first thing this migration does is point people at it - and a
	// subselect that matches no row yields NULL rather than nothing, which is how an
	// earlier version of this left accounts unable to sign in at all.
	ordinary, err := theOrdinaryRoleName(d, dialect)
	if err != nil {
		return err
	}

	// The people first, while the role still exists to be moved away from. An
	// account left pointing at a role that has gone cannot sign in.
	moveUsers := sqldb.Rebind(dialect, `UPDATE users SET role_id =
		(SELECT id FROM roles WHERE name = ?)
		WHERE role_id IN (SELECT id FROM roles WHERE name = ?)`)

	if _, err := d.SQL.Exec(moveUsers, ordinary, "manager"); err != nil {
		return fmt.Errorf("moving managers to %q: %w", ordinary, err)
	}

	// Then the role, permissions first: role_permissions references roles(id), and
	// on the engines that enforce it the delete would be refused.
	for _, statement := range []string{
		`DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE name = ?)`,
		`DELETE FROM roles WHERE name = ?`,
	} {
		if _, err := d.SQL.Exec(sqldb.Rebind(dialect, statement), "manager"); err != nil {
			return fmt.Errorf("removing the manager role: %w", err)
		}
	}

	// The approve right is gone from the application, so any role still holding it
	// holds a permission no line of code reads.
	if _, err := d.SQL.Exec(
		sqldb.Rebind(dialect, "DELETE FROM role_permissions WHERE permission = ?"),
		"timesheets:approve"); err != nil {
		return fmt.Errorf("revoking the approve permission: %w", err)
	}

	// What an ordinary account is now: its own time, and its own projects.
	grant := sqldb.Rebind(dialect, `INSERT INTO role_permissions (role_id, permission)
		SELECT id, ? FROM roles WHERE name = ?
		  AND NOT EXISTS (
			SELECT 1 FROM role_permissions rp
			WHERE rp.role_id = roles.id AND rp.permission = ?)`)

	for _, permission := range []string{
		model.PermProjectWrite, model.PermProjectArchive, model.PermProjectDelete,
		model.PermTimesheetTransfer,
	} {
		if _, err := d.SQL.Exec(grant, permission, ordinary, permission); err != nil {
			return fmt.Errorf("granting %q to %q: %w", permission, ordinary, err)
		}
	}

	// The description on screen, which would otherwise still promise submitting.
	//
	// Matched on today's names, so on an installation that still calls the everyday
	// role employee this updates nothing - and the migration that renames it writes the
	// descriptions again afterwards, which is where that case is caught.
	describe := sqldb.Rebind(dialect, "UPDATE roles SET description = ? WHERE name = ?")

	for _, role := range model.DefaultRoles() {
		if _, err := d.SQL.Exec(describe, role.Description, role.Name); err != nil {
			return fmt.Errorf("describing %q: %w", role.Name, err)
		}
	}

	// And the column itself. Supported by all three engines in the versions this
	// application requires - SQLite has taken DROP COLUMN since 3.35, which predates
	// the driver in go.mod.
	if _, err := d.SQL.Exec("ALTER TABLE timesheets DROP COLUMN status"); err != nil {
		return fmt.Errorf("dropping the timesheet status: %w", err)
	}

	return nil
}

// separateSystemAdministration narrows the administrator role to the installation.
//
// The role was seeded holding every permission, which made the one account that
// exists on every installation also the account that could read, edit and approve
// everybody's recorded hours. Administering the installation and reading what
// people recorded in it are different jobs; see model.SystemAdminPermissions.
//
// Changing the seed only helps installations yet to be created, so the rights are
// revoked here for the ones that already ran it. The report right moves to the
// manager role in the same step, or it would be held by nobody at all.
//
// Only the permissions this migration is about are touched. An installation that
// has since added its own to either role keeps them: this is a correction of what
// was seeded, not a reset to what the seed says today.
func separateSystemAdministration(d migration.Datasource, dialect string) error {
	revoke := sqldb.Rebind(dialect, `DELETE FROM role_permissions
		WHERE permission = ?
		  AND role_id IN (SELECT id FROM roles WHERE name = ?)`)

	// What running the work is, as opposed to running the installation.
	// Literals rather than the model's constants throughout: a migration records
	// what was done at the time, and every name in this list has since been removed
	// from the model - the review path, the role that used it, and finally the two
	// rights that opened everybody's time at once. Referring to a constant would tie
	// a finished migration to a definition that is allowed to change.
	for _, permission := range []string{
		"timesheets:read:all",
		"timesheets:write:all",
		"timesheets:approve",
		"reports:read",
		model.PermTimesheetTransfer,
		model.PermProjectWrite,
		model.PermProjectDelete,
		model.PermProjectArchive,
	} {
		if _, err := d.SQL.Exec(revoke, permission, model.RoleAdmin); err != nil {
			return fmt.Errorf("revoking %q from %q: %w", permission, model.RoleAdmin, err)
		}
	}

	// NOT EXISTS rather than a bare insert: an installation that already granted
	// these to the manager role would otherwise get a duplicate row, and the
	// pair is the primary key.
	grant := sqldb.Rebind(dialect, `INSERT INTO role_permissions (role_id, permission)
		SELECT id, ? FROM roles WHERE name = ?
		  AND NOT EXISTS (
			SELECT 1 FROM role_permissions rp
			WHERE rp.role_id = roles.id AND rp.permission = ?)`)

	// PermProjectDelete as well: it was the administrator's alone, and a project
	// with entries is refused, so what this deletes is an empty project created by
	// mistake rather than anybody's recorded time.
	for _, permission := range []string{"reports:read", model.PermProjectDelete} {
		if _, err := d.SQL.Exec(grant, permission, "manager", permission); err != nil {
			return fmt.Errorf("granting %q to %q: %w", permission, "manager", err)
		}
	}

	// The descriptions are on screen in the role list, where "Full administrative
	// access" would now be describing something the role no longer is.
	describe := sqldb.Rebind(dialect, "UPDATE roles SET description = ? WHERE name = ?")

	for _, role := range model.DefaultRoles() {
		if role.Name != model.RoleAdmin && role.Name != "manager" {
			continue
		}

		if _, err := d.SQL.Exec(describe, role.Description, role.Name); err != nil {
			return fmt.Errorf("describing %q: %w", role.Name, err)
		}
	}

	return nil
}

// createRunningTimers holds the clock somebody has started and not yet stopped.
//
// A table of its own rather than columns on timesheets, because a running timer
// is not a time entry yet and cannot satisfy what a time entry has to: it has no
// duration until it stops, no status to be in, and no calendar day until the zone
// and the stop time are both known. Storing it as a timesheet would mean an entry
// of zero hours, which validation refuses and every total would have to learn to
// skip.
//
// The user is the primary key, so one person can have exactly one clock running.
// Two would be a question nobody has an answer for - which of them does the next
// stop belong to.
func createRunningTimers(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE running_timers (
		user_id %s PRIMARY KEY REFERENCES users(id),
		project_id %s REFERENCES projects(id),
		description TEXT,
		started_at %s NOT NULL
	)`, foreignKeyID(dialect), foreignKeyID(dialect), timestamp(dialect)))
}

// createPasskeys stores the WebAuthn credentials users register for signing in.
//
// Nothing here is worth stealing: a passkey's private half never leaves the
// device, so what is kept is the public key and the counters needed to verify a
// signature. That is also why there is no "revoke everything" concern - a
// credential is useless without the device that holds its other half.
//
// credential_id is unique across the installation because it is what a sign-in
// arrives with: the browser sends the credential, and the credential names its
// owner. That is what allows signing in without typing a username.
func createPasskeys(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE passkeys (
		%s,
		user_id %s NOT NULL REFERENCES users(id),
		name VARCHAR(120) NOT NULL,
		credential_id %s NOT NULL,
		public_key %s NOT NULL,
		attestation_type VARCHAR(32) NOT NULL DEFAULT '',
		transports VARCHAR(120) NOT NULL DEFAULT '',
		sign_count BIGINT NOT NULL DEFAULT 0,
		backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
		backed_up BOOLEAN NOT NULL DEFAULT FALSE,
		created_at %s NOT NULL,
		last_used_at %s
	)`, primaryKey(dialect), foreignKeyID(dialect),
		credentialID(dialect), blob(dialect),
		timestamp(dialect), timestamp(dialect)),
		"CREATE INDEX idx_passkeys_user ON passkeys (user_id)",
		"CREATE UNIQUE INDEX idx_passkeys_credential ON passkeys (credential_id)")
}

// credentialID is the type for the lookup key.
//
// MySQL cannot index a BLOB without a prefix length, so the identifier is
// stored as VARBINARY there - long enough for anything an authenticator
// produces, and indexable whole.
func credentialID(dialect string) string {
	if dialect == sqldb.DialectMySQL {
		return "VARBINARY(255)"
	}

	return blob(dialect)
}

// blob is the dialect's binary column type.
func blob(dialect string) string {
	switch dialect {
	case sqldb.DialectPostgres:
		return "BYTEA"
	case sqldb.DialectMySQL:
		return "BLOB"
	default:
		return "BLOB"
	}
}

// addUserTimezone lets an individual work in a zone other than the instance's.
//
// Empty means "follow the instance setting", which is what nearly everyone
// does; only someone working from another country needs their own. Storing the
// empty string rather than the resolved name matters, because it means changing
// the instance setting moves those people with it.
func addUserTimezone(d migration.Datasource, _ string) error {
	return execAll(d,
		"ALTER TABLE users ADD COLUMN timezone VARCHAR(64) NOT NULL DEFAULT ''",
	)
}

// addExternalIdentity gives directory-backed accounts a stable identifier.
//
// The synchronisation used to match on the mail address, which meant a renamed
// mailbox looked like one person leaving and another arriving - and the
// departure would have deleted their recorded hours.
//
// Existing rows keep an empty identifier and are matched by mail address until
// their next sign-in fills it in, so an upgrade loses nothing.
func addExternalIdentity(d migration.Datasource, dialect string) error {
	return execAll(d,
		"ALTER TABLE users ADD COLUMN external_id VARCHAR(128) NOT NULL DEFAULT ''",
		// Not UNIQUE: every local account carries the empty string, and most
		// engines would count those as duplicates.
		"CREATE INDEX idx_users_external_id ON users (external_id)",
	)
}

// addUserTourSeen remembers who has already been shown the guided tour.
//
// Existing accounts default to false and are therefore offered the tour once,
// which is the right outcome for an upgrade: they have not seen it either.
func addUserTourSeen(d migration.Datasource, _ string) error {
	return execAll(d,
		"ALTER TABLE users ADD COLUMN tour_seen BOOLEAN NOT NULL DEFAULT FALSE",
	)
}

// createAPITokens adds the personal tokens used for API access.
//
// A token stores only a hash, plus a short readable prefix so its owner can
// tell their tokens apart in a list without the secret being present.
func createAPITokens(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE api_tokens (
		%s,
		user_id %s NOT NULL REFERENCES users(id),
		name VARCHAR(120) NOT NULL,
		token_hash VARCHAR(64) NOT NULL UNIQUE,
		prefix VARCHAR(32) NOT NULL,
		created_at %s NOT NULL,
		expires_at %s,
		last_used_at %s
	)`, primaryKey(dialect), foreignKeyID(dialect),
		timestamp(dialect), timestamp(dialect), timestamp(dialect)),
		"CREATE INDEX idx_api_tokens_user ON api_tokens (user_id)")
}

// addPrivateProjects lets every user keep personal categories.
//
// A project with an owner is private to that user; NULL keeps the existing
// shared projects exactly as they were, so nothing has to be migrated.
func addPrivateProjects(d migration.Datasource, dialect string) error {
	err := execAll(d,
		fmt.Sprintf("ALTER TABLE projects ADD COLUMN owner_id %s REFERENCES users(id)",
			foreignKeyID(dialect)),
		"CREATE INDEX idx_projects_owner ON projects (owner_id)",
	)
	if err != nil {
		return err
	}

	// Grant the new permission to every existing role, so upgrading an
	// installation does not silently take the feature away from its users.
	//
	// A literal, because the constant is gone: projects:write:own and projects:write
	// collapsed into one when every project became somebody's own. A finished
	// migration says what it did at the time, and must not be tied to a definition
	// that is allowed to change under it.
	return grantToAllRoles(d, dialect, "projects:write:own")
}

// grantToAllRoles adds a permission to every role that does not have it yet.
//
// The check is done per role in Go rather than with a single INSERT ... SELECT
// guarded by NOT EXISTS: referencing the target table inside an insert is not
// portable across the supported engines. Skipping existing rows matters
// because a freshly seeded admin role already holds every permission, and a
// duplicate insert would abort the migration.
func grantToAllRoles(d migration.Datasource, dialect, permission string) error {
	rows, err := d.SQL.Query("SELECT id FROM roles")
	if err != nil {
		return fmt.Errorf("reading roles: %w", err)
	}

	var roleIDs []int64

	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			_ = rows.Close()

			return fmt.Errorf("reading roles: %w", scanErr)
		}

		roleIDs = append(roleIDs, id)
	}

	err = rows.Err()
	_ = rows.Close()

	if err != nil {
		return fmt.Errorf("reading roles: %w", err)
	}

	countQuery := sqldb.Rebind(dialect,
		"SELECT COUNT(*) FROM role_permissions WHERE role_id = ? AND permission = ?")
	insertQuery := sqldb.Rebind(dialect,
		"INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)")

	for _, roleID := range roleIDs {
		var existing int

		if err := d.SQL.QueryRow(countQuery, roleID, permission).Scan(&existing); err != nil {
			return fmt.Errorf("checking %q on role %d: %w", permission, roleID, err)
		}

		if existing > 0 {
			continue
		}

		if _, err := d.SQL.Exec(insertQuery, roleID, permission); err != nil {
			return fmt.Errorf("granting %q to role %d: %w", permission, roleID, err)
		}
	}

	return nil
}

// makeProjectOptional lets a time entry be booked without a project, so hours
// can be recorded first and categorised later (or not at all).
//
// SQLite cannot drop a NOT NULL constraint in place, so there the table is
// rebuilt; the other engines alter the column directly.
func makeProjectOptional(d migration.Datasource, dialect string) error {
	switch dialect {
	case sqldb.DialectPostgres:
		return execAll(d, "ALTER TABLE timesheets ALTER COLUMN project_id DROP NOT NULL")
	case sqldb.DialectMySQL:
		return execAll(d, fmt.Sprintf("ALTER TABLE timesheets MODIFY project_id %s NULL",
			foreignKeyID(dialect)))
	default: // sqlite
		return execAll(d,
			fmt.Sprintf(`CREATE TABLE timesheets_new (
				%s,
				user_id %s NOT NULL REFERENCES users(id),
				project_id %s REFERENCES projects(id),
				date %s NOT NULL,
				duration_hours %s NOT NULL,
				description TEXT,
				status VARCHAR(32) NOT NULL
			)`, primaryKey(dialect), foreignKeyID(dialect), foreignKeyID(dialect),
				timestamp(dialect), float(dialect)),

			"INSERT INTO timesheets_new (id, user_id, project_id, date, duration_hours, description, status) "+
				"SELECT id, user_id, project_id, date, duration_hours, description, status FROM timesheets",

			// Dropping the table takes its indexes with it, so they are
			// recreated against the replacement below.
			"DROP TABLE timesheets",
			"ALTER TABLE timesheets_new RENAME TO timesheets",
			"CREATE INDEX idx_timesheets_user_date ON timesheets (user_id, date)",
			"CREATE INDEX idx_timesheets_project ON timesheets (project_id)",
		)
	}
}

// createSettings adds the key/value table behind the administration screen:
// branding, and the LDAP connection.
//
// The database connection itself is deliberately *not* stored here - it would
// live in the very database it configures - and goes to a file instead.
func createSettings(d migration.Datasource, dialect string) error {
	return execAll(d, fmt.Sprintf(`CREATE TABLE settings (
		key_name VARCHAR(64) PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at %s NOT NULL
	)`, timestamp(dialect)))
}

// addSessionsAndPreferences introduces real sign-in sessions, per-user
// two-factor authentication and the user's interface language.
func addSessionsAndPreferences(d migration.Datasource, dialect string) error {
	err := execAll(d,
		// The token hash is the primary key: lookups happen on every request,
		// and there is nothing else worth indexing.
		fmt.Sprintf(`CREATE TABLE sessions (
			token_hash VARCHAR(64) PRIMARY KEY,
			user_id %s NOT NULL REFERENCES users(id),
			created_at %s NOT NULL,
			expires_at %s NOT NULL
		)`, foreignKeyID(dialect), timestamp(dialect), timestamp(dialect)),

		"CREATE INDEX idx_sessions_user ON sessions (user_id)",
		"CREATE INDEX idx_sessions_expiry ON sessions (expires_at)",

		// The secret stays empty until the user completes enrolment, so a
		// half-finished setup never locks anyone out.
		"ALTER TABLE users ADD COLUMN totp_secret VARCHAR(64) NOT NULL DEFAULT ''",
		fmt.Sprintf("ALTER TABLE users ADD COLUMN totp_enabled %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
		"ALTER TABLE users ADD COLUMN language VARCHAR(8) NOT NULL DEFAULT ''",

		// Set by LDAP-provisioned accounts, which have no local password.
		fmt.Sprintf("ALTER TABLE users ADD COLUMN is_external %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
	)
	if err != nil {
		return err
	}

	// Only the built-in administrator may read everyone's aggregate reports,
	// so the permission is withdrawn from every other role.
	return execAll(d, fmt.Sprintf(
		"DELETE FROM role_permissions WHERE permission = '%s' "+
			"AND role_id <> (SELECT id FROM roles WHERE name = '%s')",
		"reports:read", model.RoleAdmin))
}

// addRoleBasedAccess introduces roles, credentials and per-user working times.
//
// Roles move from a free-text column on users into their own table so they can
// be administered at run time. Any role names already present are preserved,
// so an existing installation keeps its assignments.
func addRoleBasedAccess(d migration.Datasource, dialect string) error {
	err := execAll(d,
		fmt.Sprintf(`CREATE TABLE roles (
			%s,
			name VARCHAR(64) NOT NULL UNIQUE,
			description VARCHAR(255) NOT NULL DEFAULT '',
			is_system %s NOT NULL DEFAULT %s
		)`, primaryKey(dialect), boolean(dialect), falseLiteral(dialect)),

		fmt.Sprintf(`CREATE TABLE role_permissions (
			role_id %s NOT NULL REFERENCES roles(id),
			permission VARCHAR(64) NOT NULL,
			PRIMARY KEY (role_id, permission)
		)`, foreignKeyID(dialect)),

		// Nullable on purpose: the column is filled in below from the existing
		// role names, and only then would NOT NULL be satisfiable.
		fmt.Sprintf("ALTER TABLE users ADD COLUMN role_id %s", foreignKeyID(dialect)),
		"ALTER TABLE users ADD COLUMN password_hash VARCHAR(255) NOT NULL DEFAULT ''",
		fmt.Sprintf("ALTER TABLE users ADD COLUMN must_change_password %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
		fmt.Sprintf("ALTER TABLE users ADD COLUMN is_system %s NOT NULL DEFAULT %s",
			boolean(dialect), falseLiteral(dialect)),
		fmt.Sprintf("ALTER TABLE users ADD COLUMN daily_target_hours %s NOT NULL DEFAULT 0", float(dialect)),
		fmt.Sprintf("ALTER TABLE users ADD COLUMN max_daily_hours %s NOT NULL DEFAULT 0", float(dialect)),
	)
	if err != nil {
		return err
	}

	if err := seedRoles(d, dialect); err != nil {
		return err
	}

	// Carry existing users over to the new roles, then retire the old column.
	// Anyone whose role name no longer exists lands on the everyday role, the least
	// privileged one, rather than losing access entirely.
	//
	// The constant rather than a literal, unusually for a migration this old: this one
	// seeds the roles itself, a few lines up and from the model, so the name it should
	// fall back to is whatever the model calls that role today.
	return execAll(d,
		"UPDATE users SET role_id = (SELECT id FROM roles WHERE roles.name = users.role)",
		fmt.Sprintf("UPDATE users SET role_id = (SELECT id FROM roles WHERE name = '%s') WHERE role_id IS NULL",
			model.RoleUser),
		"ALTER TABLE users DROP COLUMN role",
	)
}

// theOrdinaryRoleName is the name the everyday role has in this database, and
// theCombinedRoleName the same for the one that works and administers.
//
// The everyday role was called employee and is called user; the combined one was
// employee-admin and is user-admin. The migration that renames them is at the end of
// this chain, so a migration in the middle of it can be looking at either name.
//
// Which one it sees depends on the installation, not on the position in the chain. The
// migration that first moved roles into the database builds them from the model, so a
// fresh installation is seeded with today's names from the very beginning; one that has
// been running since before the rename carries the old ones until the rename runs.
//
// Matching both is what keeps a migration in the middle honest. The constant alone
// would silently do nothing on every existing installation, and a literal alone would
// silently do nothing on every fresh one - and a migration that does nothing looks
// exactly like a migration that worked.
//
// An empty answer means neither is there, which is a repair rather than an upgrade: the
// caller seeds the role under today's name.
func theOrdinaryRoleName(d migration.Datasource, dialect string) (string, error) {
	return firstRoleThatExists(d, dialect, "employee", model.RoleUser)
}

func theCombinedRoleName(d migration.Datasource, dialect string) (string, error) {
	return firstRoleThatExists(d, dialect, "employee-admin", model.RoleUserAdmin)
}

func firstRoleThatExists(d migration.Datasource, dialect string, names ...string) (string, error) {
	count := sqldb.Rebind(dialect, "SELECT COUNT(*) FROM roles WHERE name = ?")

	for _, name := range names {
		var found int

		if err := d.SQL.QueryRow(count, name).Scan(&found); err != nil {
			return "", fmt.Errorf("looking for the %q role: %w", name, err)
		}

		if found > 0 {
			return name, nil
		}
	}

	return "", nil
}

// shippedRole is one of the roles a fresh installation gets, by name.
func shippedRole(name string) model.Role {
	for _, role := range model.DefaultRoles() {
		if role.Name == name {
			return role
		}
	}

	// Unreachable while name is one of the defaults, and a compile-time constant is
	// not enough to promise that at run time.
	return model.Role{Name: name, Description: "Keeps their own time"}
}

// seedRoles inserts the default roles and their permissions.
func seedRoles(d migration.Datasource, dialect string) error {
	for _, role := range model.DefaultRoles() {
		if err := seedRole(d, dialect, role); err != nil {
			return err
		}
	}

	return nil
}

// seedRole inserts one role and its permissions.
//
// Its own function because two places need it: first start, which writes every
// default role, and the repair above, which writes exactly one back.
func seedRole(d migration.Datasource, dialect string, role model.Role) error {
	insertRole := sqldb.Rebind(dialect,
		"INSERT INTO roles (name, description, is_system) VALUES (?, ?, ?)")

	if _, err := d.SQL.Exec(insertRole, role.Name, role.Description, role.IsSystem); err != nil {
		return fmt.Errorf("seeding role %q: %w", role.Name, err)
	}

	grant := sqldb.Rebind(dialect,
		"INSERT INTO role_permissions (role_id, permission) SELECT id, ? FROM roles WHERE name = ?")

	for _, permission := range role.Permissions {
		if _, err := d.SQL.Exec(grant, permission, role.Name); err != nil {
			return fmt.Errorf("granting %q to %q: %w", permission, role.Name, err)
		}
	}

	return nil
}

func execAll(d migration.Datasource, statements ...string) error {
	for _, stmt := range statements {
		if _, err := d.SQL.Exec(stmt); err != nil {
			return fmt.Errorf("migration statement failed (%s): %w", stmt, err)
		}
	}

	return nil
}

// primaryKey returns the dialect's auto-incrementing primary key declaration.
func primaryKey(dialect string) string {
	switch dialect {
	case sqldb.DialectPostgres:
		return "id SERIAL PRIMARY KEY"
	case sqldb.DialectMySQL:
		return "id INT AUTO_INCREMENT PRIMARY KEY"
	default: // sqlite
		return "id INTEGER PRIMARY KEY AUTOINCREMENT"
	}
}

// timestamp returns the dialect's type for a point in time.
func timestamp(dialect string) string {
	switch dialect {
	case sqldb.DialectPostgres:
		return "TIMESTAMPTZ"
	case sqldb.DialectMySQL:
		return "DATETIME"
	default: // sqlite
		return "TIMESTAMP"
	}
}

// foreignKeyID returns the column type for a foreign key, which must match the
// referenced primary key's type exactly on MySQL.
func foreignKeyID(dialect string) string {
	if dialect == sqldb.DialectMySQL {
		return "INT"
	}

	return "INTEGER"
}

// float returns the dialect's double-precision floating point type. MySQL
// rejects PostgreSQL's "DOUBLE PRECISION" spelling.
func float(dialect string) string {
	if dialect == sqldb.DialectMySQL {
		return "DOUBLE"
	}

	return "DOUBLE PRECISION"
}

// boolean returns the dialect's boolean type.
func boolean(dialect string) string {
	if dialect == sqldb.DialectSQLite {
		// SQLite has no real boolean type; the declaration is documentation
		// and the values are stored as 0/1.
		return "INTEGER"
	}

	return "BOOLEAN"
}

// falseLiteral returns how the dialect spells false in a DEFAULT clause.
func falseLiteral(dialect string) string {
	if dialect == sqldb.DialectSQLite {
		return "0"
	}

	return "FALSE"
}

func createUsers(dialect string) string {
	return fmt.Sprintf(`CREATE TABLE users (
		%s,
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL UNIQUE,
		role VARCHAR(32) NOT NULL
	)`, primaryKey(dialect))
}

func createProjects(dialect string) string {
	return fmt.Sprintf(`CREATE TABLE projects (
		%s,
		name VARCHAR(255) NOT NULL,
		description TEXT,
		start_date %s NOT NULL,
		end_date %s,
		status VARCHAR(32) NOT NULL
	)`, primaryKey(dialect), timestamp(dialect), timestamp(dialect))
}

func createTimesheets(dialect string) string {
	return fmt.Sprintf(`CREATE TABLE timesheets (
		%s,
		user_id %s NOT NULL REFERENCES users(id),
		project_id %s NOT NULL REFERENCES projects(id),
		date %s NOT NULL,
		duration_hours %s NOT NULL,
		description TEXT,
		status VARCHAR(32) NOT NULL
	)`, primaryKey(dialect), foreignKeyID(dialect), foreignKeyID(dialect), timestamp(dialect), float(dialect))
}

// moveTheAppearanceOntoTheAccount gives each account its own light or dark.
//
// It was a device setting, kept in the browser, which is right for one person
// with one laptop and wrong everywhere else: a shared machine handed the next
// person the last one's dark mode, on a screen with nothing else of theirs on
// it. Empty is what every existing account gets - follow the time of day, which
// is what a browser that has never been told anything does.
func moveTheAppearanceOntoTheAccount(d migration.Datasource, dialect string) error {
	_ = dialect

	return execAll(d,
		"ALTER TABLE users ADD COLUMN theme VARCHAR(8) NOT NULL DEFAULT ''")
}
