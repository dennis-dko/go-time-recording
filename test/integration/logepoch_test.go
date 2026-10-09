//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// follow asks for the log after a position, under the name of the process that
// counted it. An empty name is a client that does not say.
func (c *client) follow(t *testing.T, since uint64, epoch string) logPage {
	t.Helper()

	query := url.Values{"since": {strconv.FormatUint(since, 10)}, "limit": {"1000"}}
	if epoch != "" {
		query.Set("epoch", epoch)
	}

	return c.logs(t, "?"+query.Encode())
}

// A log position counted by another process is answered from the beginning of
// the running one, and the answer says so.
//
// The sink's own cases hold the rule; this holds that the route hands it both
// halves - the name going out with every answer and coming back with the
// position - which is the part a client depends on and no unit case sees.
func TestALogPositionFromAnotherProcessIsAnsweredFromTheBeginning(t *testing.T) {
	t.Parallel()

	a := start(t, "LOG_LEVEL=INFO")
	admin := a.signInAsAdmin("a-much-better-password")

	first := admin.follow(t, 0, "")
	if first.Epoch == "" || len(first.Records) == 0 || first.Restarted {
		t.Fatalf("a first page came back as %d lines, named %q, restarted %v",
			len(first.Records), first.Epoch, first.Restarted)
	}

	// Following on in the same process: only what is new, and nothing said.
	if own := admin.follow(t, first.LastSeq, first.Epoch); own.Restarted {
		t.Error("a position of this process's own was reported as another's")
	}

	// The same position under another process's name: everything, and said.
	other := admin.follow(t, first.LastSeq, "another-process")
	if !other.Restarted || len(other.Records) < len(first.Records) {
		t.Errorf("a position from another process was answered with %d lines, restarted %v; "+
			"want the log from its beginning - at least the %d a first page held",
			len(other.Records), other.Restarted, len(first.Records))
	}

	if other.Epoch != first.Epoch {
		t.Errorf("the process named itself %q and then %q", first.Epoch, other.Epoch)
	}
}

// Somebody following the log across a real restart is handed what the new
// process wrote while it started.
//
// The case above hands the route a name that is not its own; this one gets the
// name the honest way, from a process that is then replaced. It waits until the
// new process has counted past the position held from the old one, because that
// is the state the number alone cannot settle - before it, a position beyond
// the end of the log gives the restart away without any name - and it is where
// a viewer left open ends up within a minute of any restart.
func TestFollowingTheLogAcrossARestartStartsAtTheNewBeginning(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("a process cannot replace its own image on Windows")
	}

	a := start(t, "LOG_LEVEL=INFO")
	admin := a.signInAsAdmin("a-much-better-password")

	held := admin.follow(t, 0, "")
	before := restartState(t, admin)

	admin.must(admin.api(http.MethodPost, "/settings/restart", nil),
		http.StatusCreated, http.StatusOK)

	if !eventuallyWithin(45*time.Second, func() bool {
		state, ok := admin.tryRestartState()

		return ok && state.StartedAt != "" && state.StartedAt != before.StartedAt
	}) {
		t.Fatalf("the application did not come back as a different process\n\napplication log:\n%s",
			truncate(a.log(), 2000))
	}

	// Every request is a line but a poll of the log itself, which is kept out of
	// what the viewer shows - so a request beside each poll is what moves the new
	// log on.
	var fresh logPage

	for range 200 {
		admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK)

		fresh = admin.follow(t, 0, "")
		if fresh.LastSeq > held.LastSeq {
			break
		}
	}

	if fresh.LastSeq <= held.LastSeq {
		t.Fatalf("the new process is at line %d and never passed the %d held from the old one",
			fresh.LastSeq, held.LastSeq)
	}

	if fresh.Epoch == held.Epoch {
		t.Fatalf("the process that replaced the first answers to the same name, %q", fresh.Epoch)
	}

	followed := admin.follow(t, held.LastSeq, held.Epoch)

	if !followed.Restarted {
		t.Error("a position held from the process before this one was taken for one of its own")
	}

	if len(followed.Records) == 0 || followed.Records[0].Seq != 1 {
		t.Errorf("following on from line %d of the old process was answered with %d lines of the new one, "+
			"and not from its first", held.LastSeq, len(followed.Records))
	}
}
