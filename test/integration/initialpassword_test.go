//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Every start says so while the documented password still opens the built-in
// administrator, not only the start that created it.
//
// The password is in the README, so until somebody signs in and changes it,
// anybody who can reach the installation can sign in as its administrator. The
// start that created the account said so, once; every later start said nothing -
// while the other states that open an installation up, authentication switched
// off, plain HTTP and no SECRET_KEY, are said on every start. An operator reading
// the log of a restart saw those, and nothing about the one that hands out the
// administrator.
func TestEveryStartSaysWhileTheDocumentedPasswordStillOpensTheAdministrator(t *testing.T) {
	t.Parallel()

	if os.Getenv(harness.DSNEnv) != "" {
		t.Skip("this test shares a SQLite file between instances")
	}

	const warning = "still opens with the documented initial password"

	// GoFr runs the start hooks, which is where the account is looked at, before
	// it starts its server - so once this line is in a log, the hooks have spoken
	// and a missing warning is an answer rather than a race. It is written at
	// INFO, which the harness's default level leaves out, hence the level below.
	started := func(a *app) string {
		t.Helper()

		if !eventuallyWithin(30*time.Second, func() bool {
			return strings.Contains(a.log(), "Starting server on port")
		}) {
			t.Fatalf("the instance never said it was serving:\n%s", a.log())
		}

		return a.log()
	}

	shared := harness.SharedDatabase(t)

	started(start(t, "DB_NAME="+shared, "LOG_LEVEL=INFO"))

	again := start(t, "DB_NAME="+shared, "LOG_LEVEL=INFO")
	if log := started(again); !strings.Contains(log, warning) {
		t.Errorf("a start whose administrator still has the documented password said "+
			"nothing about it:\n%s", log)
	}

	again.signInAsAdmin("a-much-better-password")

	if log := started(start(t, "DB_NAME="+shared, "LOG_LEVEL=INFO")); strings.Contains(log, warning) {
		t.Errorf("a start after the password was changed still warns about it:\n%s", log)
	}
}
