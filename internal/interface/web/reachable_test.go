package web_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
