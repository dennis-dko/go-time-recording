//go:build integration

package integration

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// internal/infrastructure/directory is the one package here that talks to
// something this project does not control, and until now nothing ran it. The
// synchronisation tests drive a fake directory, which proves the rules and
// nothing about the LDAP: not the bind, not the filter substitution, not the
// attribute names, and above all not whether the stable identifier comes back in
// a form this application can store.
//
// That last one is the reason this file exists. The identifier is what tells a
// renamed account from a departed one, and getting it wrong deletes people
// together with every hour they recorded. It cannot be checked against a fake,
// because a fake returns whatever the test told it to.
//
//	docker compose --profile ldap up -d --wait          (from test/)
//	GTR_TEST_LDAP=localhost:5389 go test -tags integration ./test/integration
//
// Or `task test:ldap`, which does both. The seed these cases rely on is
// test/ldap/01-seed.ldif, and each entry in it is there for a case below.

const ldapEnv = "GTR_TEST_LDAP"

// The seed's own values. Repeated here rather than shared with the compose file,
// because a test that reads its expectations from the thing under test proves
// only that it can read.
const (
	ldapBaseDN   = "dc=example,dc=com"
	ldapPeopleDN = "ou=people,dc=example,dc=com"
	ldapBindDN   = "cn=admin,dc=example,dc=com"
	ldapBindPass = "gtr-test-password"
)

// requireLDAP skips unless a directory is configured and answering.
func requireLDAP(t *testing.T) (host string, port int) {
	t.Helper()

	address := os.Getenv(ldapEnv)
	if address == "" {
		t.Skipf("%s is not set; start test/docker-compose.yml's ldap profile to run this", ldapEnv)
	}

	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("%s must be host:port, got %q: %v", ldapEnv, address, err)
	}

	port, err = strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("%s has a port that is not a number: %q", ldapEnv, rawPort)
	}

	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Skipf("%s is %q but nothing answers there: %v", ldapEnv, address, err)
	}

	_ = conn.Close()

	return host, port
}

// ldapSettings is the payload the Settings screen sends, with the seed's values.
func ldapSettings(host string, port int, baseDN string) map[string]any {
	return map[string]any{
		"enabled":        true,
		"host":           host,
		"port":           port,
		"startTls":       false,
		"useTls":         false,
		"bindDn":         ldapBindDN,
		"bindPassword":   ldapBindPass,
		"baseDn":         baseDN,
		"userFilter":     "(|(uid=%s)(mail=%s))",
		"nameAttribute":  "cn",
		"emailAttribute": "mail",
		"idAttribute":    "entryUUID",
		"defaultRole":    "employee",
	}
}

// configureLDAP points the running instance at the directory. It takes effect at
// once - the handler reconfigures the client on save - so no restart is needed.
func configureLDAP(t *testing.T, admin *client, host string, port int, baseDN string) {
	t.Helper()

	admin.must(admin.api(http.MethodPut, "/settings/ldap", ldapSettings(host, port, baseDN)),
		http.StatusOK)
}

// The probe behind the "Test connection" button, against a real server. It binds
// and searches, so a pass means the settings are usable rather than merely
// well-formed.
func TestTheDirectoryConnectionTestReachesARealServer(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var outcome struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}

	admin.must(admin.api(http.MethodPost, "/settings/ldap/test",
		ldapSettings(host, port, ldapBaseDN)), http.StatusCreated, http.StatusOK).Data(t, &outcome)

	if !outcome.OK {
		t.Errorf("the connection test failed against the seeded directory: %s", outcome.Message)
	}

	// And a bind that cannot work has to fail, or the button says yes to
	// anything and is worse than no button.
	wrong := ldapSettings(host, port, ldapBaseDN)
	wrong["bindPassword"] = "not-the-password"

	admin.must(admin.api(http.MethodPost, "/settings/ldap/test", wrong),
		http.StatusCreated, http.StatusOK).Data(t, &outcome)

	if outcome.OK {
		t.Error("the connection test passed with a wrong bind password")
	}
}

// Signing in with a directory account, which is the whole point of the feature:
// the password is checked against the directory and the account appears locally
// on first success, carrying the name and address the directory holds.
func TestADirectoryAccountCanSignInAndIsCreatedLocally(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	alice := a.newClient()
	alice.signIn("alice@example.com", "alice-password")

	var me struct {
		User struct {
			Name       string `json:"name"`
			Email      string `json:"email"`
			Role       string `json:"role"`
			IsExternal bool   `json:"isExternal"`
		} `json:"user"`
	}

	alice.must(alice.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	// Straight out of the directory: cn and mail, not something derived from
	// what was typed into the sign-in form.
	if me.User.Name != "Alice Anderson" {
		t.Errorf("the name came back as %q, want it read from the directory's cn", me.User.Name)
	}

	if me.User.Email != "alice@example.com" {
		t.Errorf("the address came back as %q", me.User.Email)
	}

	if me.User.Role != "employee" {
		t.Errorf("the account got the role %q, want the configured default", me.User.Role)
	}

	// Marked as the directory's, which is what stops the interface offering to
	// set a password that would never be checked.
	if !me.User.IsExternal {
		t.Error("the account is not marked as coming from the directory")
	}
}

// The filter substitutes the login name into every %s, so the same person can
// sign in by uid or by mail address.
func TestADirectoryAccountCanSignInByUidOrByMail(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	byUID := a.newClient()
	byUID.signIn("bob", "bob-password")
	byUID.must(byUID.api(http.MethodGet, "/me", nil), http.StatusOK)

	byMail := a.newClient()
	byMail.signIn("bob@example.com", "bob-password")
	byMail.must(byMail.api(http.MethodGet, "/me", nil), http.StatusOK)
}

// A wrong password has to be refused by the directory rather than quietly
// falling back to a local check that would let a stale local password in.
func TestAWrongDirectoryPasswordIsRefused(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	refused := a.newClient()

	response := refused.api(http.MethodPost, "/auth/login", map[string]string{
		"email": "alice@example.com", "password": "not-alice-password",
	})

	if response.Status == http.StatusOK || response.Status == http.StatusCreated {
		t.Fatalf("a wrong directory password was accepted: %s", response.Body)
	}
}

// The base DN decides who the directory even contains, and getting it wrong is
// invisible until somebody cannot sign in. Dave sits outside ou=people on
// purpose, so narrowing the base DN has to make him disappear.
func TestTheBaseDNDecidesWhoTheDirectoryContains(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	// The whole tree: the contractor is found.
	configureLDAP(t, admin, host, port, ldapBaseDN)

	dave := a.newClient()
	dave.signIn("dave@example.com", "dave-password")
	dave.must(dave.api(http.MethodGet, "/me", nil), http.StatusOK)

	// Narrowed to ou=people: the same account is now outside it. A fresh
	// instance, because the first one has already created him locally and a
	// local account would answer without the directory being asked.
	b := start(t)
	narrowed := b.signInAsAdmin("a-much-better-password")
	configureLDAP(t, narrowed, host, port, ldapPeopleDN)

	response := b.newClient().api(http.MethodPost, "/auth/login", map[string]string{
		"email": "dave@example.com", "password": "dave-password",
	})

	if response.Status == http.StatusOK || response.Status == http.StatusCreated {
		t.Errorf("an account outside the base DN could sign in: %s", response.Body)
	}
}

// An entry with no mail address cannot be keyed on anything, so it is refused
// rather than given the login name as a substitute.
//
// Carol is in the seed for exactly this. Substituting the login name used to
// produce an account that signed in perfectly well and that the synchronisation
// then read as a departure - see the deletion test below, which is the harm this
// one prevents. The refusal says what is wrong, because whoever hits it has
// already proved their password and can do nothing about it themselves.
func TestAnEntryWithoutAMailAddressIsRefusedRatherThanGuessedAt(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	response := a.newClient().api(http.MethodPost, "/auth/login", map[string]string{
		"email": "carol", "password": "carol-password",
	})

	if response.Status == http.StatusOK || response.Status == http.StatusCreated {
		t.Errorf("an entry with no mail address was signed in: %s", response.Body)
	}

	// The answer itself stays vague on purpose - a caller who is not signed in
	// is told nothing about why. The reason belongs in the log, where the
	// administrator who has to fix the attribute will look, and without it a
	// misconfigured directory is indistinguishable from a mistyped password.
	if !eventually(func() bool { return strings.Contains(a.log(), "mail") }) {
		t.Errorf("nothing in the log says why the sign-in could not be completed:\n%s",
			truncate(a.log(), 1500))
	}

	// And no account was left behind by the attempt.
	var list struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK).Data(t, &list)

	for _, user := range list.Items {
		if user.Name == "Carol Carpenter" {
			t.Error("an account was created for an entry with no mail address")
		}
	}
}

// The preview is what an administrator looks at before running a synchronisation
// that deletes people. It has to describe the real directory, and it must never
// delete anything itself.
func TestTheSynchronisationPreviewDescribesTheRealDirectory(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	preview := syncPreview(t, admin)

	if preview.Aborted != "" {
		t.Fatalf("the preview refused to run: %s", preview.Aborted)
	}

	// The seeded accounts that have a mail address are the ones it can create.
	// Carol has none and must not appear.
	for _, want := range []string{"alice@example.com", "bob@example.com", "dave@example.com"} {
		if !contains(preview.Created, want) {
			t.Errorf("the preview does not list %s, which the directory holds", want)
		}
	}

	if contains(preview.Created, "carol") {
		t.Error("the preview lists an entry that has no mail address")
	}

	// The built-in administrator is local and must never be proposed for
	// deletion: it is the way back into an installation.
	for _, user := range preview.Candidates {
		if user.Email == adminEmail {
			t.Error("the preview proposes deleting the built-in administrator")
		}
	}
}

// SyncPreview mirrors the preview response.
type SyncPreview struct {
	DirectoryUsers int    `json:"directoryUsers"`
	LocalExternal  int    `json:"localExternal"`
	DryRun         bool   `json:"dryRun"`
	Aborted        string `json:"aborted"`

	// Candidates are the accounts a real run would delete, with the number of
	// time entries that would go with each.
	Candidates []struct {
		Email      string `json:"email"`
		Timesheets int    `json:"timesheets"`
	} `json:"candidates"`

	Created []string `json:"created"`
}

func syncPreview(t *testing.T, admin *client) SyncPreview {
	t.Helper()

	var preview SyncPreview

	admin.must(admin.api(http.MethodPost, "/settings/ldap/sync/preview", nil),
		http.StatusCreated, http.StatusOK).Data(t, &preview)

	return preview
}

// The one that matters most, and the reason the two code paths above have to
// agree with each other.
//
// Signing in creates a local account from a directory entry. The
// synchronisation then compares the local accounts against the directory's own
// list and deletes the ones the directory no longer holds - together with every
// hour recorded against them. So an entry that can sign in but does not appear
// in that list is an account that is created and then deleted by the next run,
// and whoever recorded time against it has no way to know.
func TestAnAccountThatCanSignInIsNeverProposedForDeletion(t *testing.T) {
	host, port := requireLDAP(t)

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")
	configureLDAP(t, admin, host, port, ldapBaseDN)

	// Everyone in the seed, including the entry with no mail address. Whether
	// each one gets in is not the assertion here - the assertion is that
	// whoever did get in is not then proposed for deletion, and an account that
	// cannot sign in at all trivially satisfies it.
	for _, who := range []struct{ login, password string }{
		{"alice@example.com", "alice-password"},
		{"bob@example.com", "bob-password"},
		{"dave@example.com", "dave-password"},
		{"carol", "carol-password"},
	} {
		a.newClient().api(http.MethodPost, "/auth/login", map[string]string{
			"email": who.login, "password": who.password,
		})
	}

	preview := syncPreview(t, admin)

	if preview.Aborted != "" {
		t.Fatalf("the preview refused to run: %s", preview.Aborted)
	}

	for _, candidate := range preview.Candidates {
		t.Errorf("%q signed in from the directory and the next synchronisation would delete it "+
			"together with %d time entries", candidate.Email, candidate.Timesheets)
	}
}
