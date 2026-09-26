// Package tempdir hands a test a directory that is removed only once Windows has
// let go of it.
//
// A test that runs a program - an instance of the application, a downloaded
// binary asked for its version, the updater's shell script - leaves files that
// Windows keeps open for a while after the process has exited. t.TempDir removes
// its directory straight away and then fails a test that had passed, with "The
// directory is not empty" or "being used by another process". The harness, the
// self-update tests and the updater tests each met it separately, and each had
// grown or was about to grow its own copy of the same retry; this is the one
// place it lives. CI runs on Linux, where an open file does not stop a removal,
// so only a Windows run ever shows it.
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
