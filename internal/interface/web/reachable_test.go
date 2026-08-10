package web_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// Every screen can be reached by somebody a fresh installation has.
//
// A data-perm decides whether an element exists on screen at all, and it names a
// permission rather than a role - so nothing in the markup says whether anybody
// actually holds it. The Reports tab and the team-overtime card were both gated on
// reports:read, which belonged to the role that reviewed other people's hours. When
// that role went, the right stayed and no role held it, so both were hidden on every
// installation there is. Nobody noticed, because a hidden element looks exactly like
// an element somebody else is meant to see.
//
// This is the check that would have said so on the day the role was removed.

// permGate finds the permission requirements in the markup.
var permGate = regexp.MustCompile(`data-perm="([^"]+)"`)

// grantedByDefault is every permission at least one seeded role holds.
func grantedByDefault(t *testing.T) map[string]string {
	t.Helper()

	granted := map[string]string{}

	for _, role := range model.DefaultRoles() {
		for _, permission := range role.Permissions {
			// The first role to hold it names it, which is enough for the message.
			if _, seen := granted[permission]; !seen {
				granted[permission] = role.Name
			}
		}
	}

	if len(granted) == 0 {
		t.Fatal("no seeded role grants anything")
	}

	return granted
}

func TestEveryGatedElementIsReachableBySomeSeededRole(t *testing.T) {
	html := asset(t, "/")
	granted := grantedByDefault(t)

	var unreachable []string

	for _, match := range permGate.FindAllStringSubmatch(html, -1) {
		// A comma-separated list is "any of these", the way can() reads it.
		wanted := strings.Split(match[1], ",")

		reachable := false

		for _, permission := range wanted {
			if _, held := granted[strings.TrimSpace(permission)]; held {
				reachable = true

				break
			}
		}

		if !reachable {
			unreachable = append(unreachable, match[1])
		}
	}

	sort.Strings(unreachable)

	if len(unreachable) > 0 {
		t.Errorf("%d element(s) require a permission no seeded role holds, so they are "+
			"hidden on every installation: %v\n\nEither a role should hold it, or the "+
			"element and the endpoint behind it should go - a screen nobody can reach "+
			"looks exactly like one somebody else is meant to see",
			len(unreachable), unreachable)
	}
}

// And the reverse: a permission the role editor offers that nothing checks.
//
// Granting one of those changes nothing, which is worse than it not existing: an
// administrator ticks a box to give somebody a right and the application carries on
// refusing them. The comment on the permission list already promises this - "a
// permission that exists only in the database would grant nothing" - and nothing
// held it to that promise.
//
// Checked by constant name rather than by the string, because that is how the Go
// source refers to them: the string itself appears only in the declaration.
func TestEveryPermissionIsCheckedSomewhere(t *testing.T) {
	js := asset(t, "/app.js")
	html := asset(t, "/")

	declarations := permissionConstants(t)
	source := goSourceOutside(t, filepath.Join("model", "permission_model.go"))

	var unused []string

	for _, permission := range model.AllPermissions() {
		constant, declared := declarations[permission]
		if !declared {
			t.Errorf("%q is in AllPermissions() but declared under no constant", permission)

			continue
		}

		switch {
		// Enforced in Go, which is where most of them live.
		case strings.Contains(source, constant),
			// Or read by the interface, by name.
			strings.Contains(js, "'"+permission+"'"),
			strings.Contains(html, permission):
		default:
			unused = append(unused, permission)
		}
	}

	sort.Strings(unused)

	if len(unused) > 0 {
		t.Errorf("%d permission(s) are offered in the role editor and checked nowhere, so "+
			"granting one changes nothing: %v", len(unused), unused)
	}
}

// permissionConstants maps each permission to the constant it is declared under.
func permissionConstants(t *testing.T) map[string]string {
	t.Helper()

	path := filepath.Join("..", "..", "domain", "model", "permission_model.go")

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the permission list: %v", err)
	}

	found := map[string]string{}

	for _, match := range regexp.MustCompile(`(Perm\w+)\s*=\s*"([^"]+)"`).
		FindAllStringSubmatch(string(source), -1) {
		found[match[2]] = match[1]
	}

	if len(found) == 0 {
		t.Fatal("no permission constants found; the declaration shape changed and this " +
			"guard no longer guards anything")
	}

	return found
}

// goSourceOutside is every non-test Go file under internal/, except the one named -
// the declaration itself is not a use of what it declares.
func goSourceOutside(t *testing.T, except string) string {
	t.Helper()

	var all strings.Builder

	root := filepath.Join("..", "..", "..", "internal")

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, except) {
			return nil
		}

		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		all.Write(source)

		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	return all.String()
}

// Every permission the interface names is one the application enforces.
//
// The other direction from TestEveryPermissionIsCheckedSomewhere, and it catches the
// opposite mistake: a right that was removed from the application but left behind in a
// can() call or a data-perm. That reads as "nobody may do this" for ever, silently -
// which is what happened to a step of the guided tour. It asked for reports:read after
// that right was withdrawn, so the step was dropped for everybody and the walk simply
// got shorter, with nothing to say it had.
func TestEveryPermissionTheInterfaceNamesExists(t *testing.T) {
	js := asset(t, "/app.js")
	html := asset(t, "/")

	enforced := map[string]bool{}
	for _, permission := range model.AllPermissions() {
		enforced[permission] = true
	}

	// Only live references: what can() is asked, what a tour step or a spreadsheet card
	// declares, and what data-perm gates on. Prose is left alone deliberately - a
	// comment explaining why a right was removed has to be able to name it, and a guard
	// that forbade that would fight the way this codebase explains itself.
	live := []*regexp.Regexp{
		regexp.MustCompile(`can\(\s*'([a-z:,' ]+)'`),
		regexp.MustCompile(`(?:permission|read|write):\s*'([a-z:,]+)'`),
		regexp.MustCompile(`data-perm="([a-z:,]+)"`),
	}

	var unknown []string
	seen := map[string]bool{}

	for _, source := range []string{js, html} {
		for _, pattern := range live {
			for _, match := range pattern.FindAllStringSubmatch(source, -1) {
				// A comma-separated list is "any of these", so every name in it counts.
				// can() also takes them as separate arguments, which the pattern above
				// catches together - hence the quotes and spaces in the split set.
				for _, name := range strings.FieldsFunc(match[1], func(r rune) bool {
					return r == ',' || r == '\'' || r == ' '
				}) {
					if enforced[name] || seen[name] {
						continue
					}

					seen[name] = true
					unknown = append(unknown, name)
				}
			}
		}
	}

	sort.Strings(unknown)

	if len(unknown) > 0 {
		t.Errorf("%d name(s) in the interface look like permissions the application does "+
			"not enforce, so whatever depends on them is off for everybody: %v\n\n"+
			"Either the name is stale and should go with what it gated, or it is a typo",
			len(unknown), unknown)
	}
}

// Every role the application ships says what it is for, in the reader's language.
//
// A role is chosen from a dropdown by somebody deciding what a colleague may do, and
// "employee-admin" against "employee" is a difference you can only infer from the
// name - the difference being whether that person can administer the installation.
// The description carries it, and for the roles that ship it is translated rather
// than left as the English sentence the seed wrote into the database.
//
// Read from the model rather than restated here, so a fourth role cannot arrive with
// nothing to explain it.
func TestEverySeededRoleSaysWhatItIsFor(t *testing.T) {
	dict, ok := dictionaries(t)["de"]
	if !ok {
		t.Fatal("no German dictionary")
	}

	shipped := map[string]bool{}

	for _, role := range model.DefaultRoles() {
		shipped[role.Name] = true

		// The seed writes an English sentence into the database, which is the fallback
		// and has to be there too: a custom role has nothing else.
		if strings.TrimSpace(role.Description) == "" {
			t.Errorf("the seeded role %q has no description at all, so a custom-role "+
				"reader would see an empty explanation", role.Name)
		}

		if _, translated := dict["role.desc."+role.Name]; !translated {
			t.Errorf("the seeded role %q has no German description, so a German reader "+
				"chooses it from the dropdown by guessing", role.Name)
		}
	}

	// And nothing left over: a role that was removed leaves its sentence behind, and a
	// sentence nobody shows is a sentence nobody notices is wrong.
	for key := range dict {
		name, isRole := strings.CutPrefix(key, "role.desc.")
		if !isRole {
			continue
		}

		if !shipped[name] {
			t.Errorf("%q explains a role this application does not ship", key)
		}
	}
}

// Nothing on screen asks which person.
//
// Four controls used to: the booking form, the entry filter, the calendar and the
// overtime form each offered a dropdown of colleagues. They were built when a role
// could hold timesheets:read:all, and they never worked as they read even then - the
// account that administers colleagues does not read what they recorded, so it was
// offered every name and every choice but its own came back 403. The filter was worse:
// "All users" quietly showed only your own entries, because the server pins the scope
// and the label did not know.
//
// Now that whose time it is is settled by who is signed in, any such control could
// only refuse. This is the check that says so before somebody adds one back, because a
// dropdown that looks ordinary and answers 403 is a bug that reads as a permissions
// problem for as long as nobody tries it.
func TestNothingOnScreenAsksWhichPerson(t *testing.T) {
	html := asset(t, "/")

	for _, gone := range []struct{ markup, was string }{
		{`id="filter-ts-user"`, "the entry filter"},
		{`id="calendar-user"`, "the calendar"},
		{`name="userId"`, "the booking form and the overtime form"},
	} {
		if strings.Contains(html, gone.markup) {
			t.Errorf("%s is back in the markup (%s)\n\nWhose time an entry is is decided "+
				"by who is signed in, so a control that names somebody else can only be "+
				"refused - and one that looks ordinary and answers 403 reads as a "+
				"permissions problem rather than as a control that should not be there",
				gone.markup, gone.was)
		}
	}
}

// The width an empty table claims is the width it has.
//
// fillTable takes a column count and uses it as the colspan of the "nothing here"
// row. Nothing connects that number to the header it has to match, so removing a
// column leaves the empty message spanning further than the table goes - which is
// invisible for as long as the table has rows in it, and shows up as a stray cell
// sticking out on the day somebody has no entries. That is exactly what happened when
// the user column came out of the entry list.
//
// Read out of the two files rather than restated here, so a table added later is
// checked without anybody remembering to add it.
func TestAnEmptyTableSpansItsOwnColumns(t *testing.T) {
	js := asset(t, "/app.js")
	html := asset(t, "/")

	calls := regexp.MustCompile(`fillTable\(\$\('#(table-[a-z-]+) tbody'\), [^,]+, (\d+)`).
		FindAllStringSubmatch(js, -1)
	if len(calls) == 0 {
		t.Fatal("no fillTable calls found; this guard is no longer reading the source")
	}

	for _, call := range calls {
		table, claimed := call[1], call[2]

		markup := tableMarkup(t, html, table)
		if markup == "" {
			t.Errorf("app.js fills #%s, which is not in the markup", table)

			continue
		}

		// <th followed by a space or a close bracket, so <thead does not count as a
		// column - which it did on the first attempt, making every table in the
		// application look one column wider than it is.
		headers := len(regexp.MustCompile(`<th[\s>]`).FindAllString(markup, -1))

		if claimed != strconv.Itoa(headers) {
			t.Errorf("#%s has %d column(s) and its empty row spans %s: a message that "+
				"reaches past the last column shows as a stray cell the moment somebody "+
				"has nothing to list", table, headers, claimed)
		}
	}
}

// tableMarkup returns the head of one table, from its id to the end of its header row.
func tableMarkup(t *testing.T, html, table string) string {
	t.Helper()

	start := strings.Index(html, `id="`+table+`"`)
	if start < 0 {
		return ""
	}

	end := strings.Index(html[start:], "</thead>")
	if end < 0 {
		t.Fatalf("#%s has no header row, so its width cannot be checked", table)
	}

	return html[start : start+end]
}
