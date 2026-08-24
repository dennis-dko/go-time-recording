package imageupdate_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/imageupdate"
)

// Nothing is available where nothing was configured, which is every deployment
// that has not added the overlay.
//
// The default matters more than it looks. This decides whether the version card
// offers to replace the image or to swap the binary, and a wrong "yes" is a
// button that writes a file nobody reads and then waits for a restart that
// never comes.
func TestAnUpdaterThatWasNeverDeployedIsNotAvailable(t *testing.T) {
	t.Parallel()

	if imageupdate.New("").Available() {
		t.Error("an installation that configured no updater reports one")
	}

	if imageupdate.New(filepath.Join(t.TempDir(), "not-there")).Available() {
		t.Error("a directory that does not exist reports an updater behind it")
	}
}

// A file is not a directory, and neither is an updater.
func TestAFileWhereTheDirectoryShouldBeIsNotAnUpdater(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "requests")

	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("cannot write the test file: %v", err)
	}

	if imageupdate.New(path).Available() {
		t.Error("a plain file is taken for the channel to an updater")
	}
}

// Asking leaves the request where the updater looks, and nowhere else.
func TestAskingLeavesARequestTheUpdaterCanFind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	updater := imageupdate.New(dir)

	if !updater.Available() {
		t.Fatal("a writable directory does not report an updater")
	}

	if err := updater.Ask(); err != nil {
		t.Fatalf("asking for an update: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "request")); err != nil {
		t.Errorf("the request is not where the updater looks for it: %v", err)
	}

	// And nothing half-written is left beside it. The request is moved into
	// place rather than created there, because the updater polls the directory
	// and would otherwise be able to find a file mid-write.
	if _, err := os.Stat(filepath.Join(dir, "request.tmp")); err == nil {
		t.Error("the staging file was left behind, so the updater can find a " +
			"request that is still being written")
	}
}

// Asking twice while one is running is refused, and refused as its own thing.
//
// Not a failure: the second press is somebody who did not see the first one
// take, and telling them it broke would send them looking for a fault. The
// screen needs to tell "already running" from "could not ask", so they are
// different errors.
func TestAskingWhileOneIsRunningIsRefusedAsBusy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	updater := imageupdate.New(dir)

	// What the updater writes while it works.
	if err := os.WriteFile(filepath.Join(dir, "running"), nil, 0o600); err != nil {
		t.Fatalf("cannot mark one as running: %v", err)
	}

	if !updater.Running() {
		t.Fatal("an update that is running is not reported as running")
	}

	err := updater.Ask()

	if !errors.Is(err, imageupdate.ErrBusy) {
		t.Errorf("a second request during an update failed as %v rather than as busy", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "request")); err == nil {
		t.Error("the refused request was written anyway, so the updater will run twice")
	}
}

// Asking where there is no updater says so rather than pretending.
func TestAskingWithNoUpdaterSaysSo(t *testing.T) {
	t.Parallel()

	err := imageupdate.New(filepath.Join(t.TempDir(), "absent")).Ask()

	if !errors.Is(err, imageupdate.ErrUnavailable) {
		t.Errorf("asking an updater that is not there failed as %v", err)
	}
}

// The outcome is readable, and stays readable.
//
// Cleared by the next request rather than by reading it: a screen that asks
// twice - and this one polls - would otherwise have the first answer taken by
// whichever poll got there first.
func TestTheOutcomeIsReadableMoreThanOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	updater := imageupdate.New(dir)

	if _, ok := updater.Result(); ok {
		t.Error("an updater that has done nothing reports an outcome")
	}

	if err := os.WriteFile(filepath.Join(dir, "result"),
		[]byte(imageupdate.ResultDone+"\n"), 0o600); err != nil {
		t.Fatalf("cannot write the outcome: %v", err)
	}

	for i := range 2 {
		got, ok := updater.Result()

		if !ok {
			t.Fatalf("reading the outcome the %d time: nothing there", i+1)
		}

		if got != imageupdate.ResultDone {
			t.Errorf("the outcome reads %q", got)
		}
	}
}
