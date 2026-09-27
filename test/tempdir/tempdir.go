package tempdir

import (
	"os"
	"testing"
	"time"
)

// New is a t.TempDir whose removal is retried briefly before testing.T's own.
//
// Registered after t.TempDir, so it runs before it - cleanups run last first -
// and after whatever the test registers later, such as stopping the program
// that held the files.
func New(t testing.TB) string {
	t.Helper()

	dir := t.TempDir()

	t.Cleanup(func() { Remove(dir) })

	return dir
}

// Remove deletes dir, retrying for a few seconds while Windows still holds it.
//
// Retried rather than slept through, and the outcome is ignored: if it still
// cannot be removed, t.TempDir reports it, which is the behaviour without this.
func Remove(dir string) {
	if dir == "" {
		return
	}

	deadline := time.Now().Add(5 * time.Second)

	for os.RemoveAll(dir) != nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}
