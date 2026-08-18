//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
)

// swap puts the staged binary in place of the running one, keeping the old.
//
// A rename over the top would be enough to install and is not enough to undo.
// Unix lets a running binary be replaced - the process keeps the inode it was
// started from and carries on - so the obvious version of this simply overwrote
// the file, and with it the only copy of the version that was known to work. If
// the new one then failed to start, the screen that could have put the old one
// back had gone with it.
//
// So the old one is moved aside first, exactly as Windows is forced to do, and
// kept. That makes the two platforms behave the same way, gives Rollback
// something to work with, and costs one file until the next update.
func swap(self, staged, version string) error {
	old := self + ".old"

	// Whatever the previous update left. Removed rather than kept, because the
	// version worth being able to go back to is the one that was running a
	// moment ago.
	_ = os.Remove(old)

	if err := os.Rename(self, old); err != nil {
		return fmt.Errorf("cannot move the running binary aside: %w", err)
	}

	if err := os.Rename(staged, self); err != nil {
		// Back where it was, or the installation has no binary at its own path
		// and the next start finds nothing.
		_ = os.Rename(old, self)

		return fmt.Errorf("cannot put the new binary in place: %w", err)
	}

	return markPending(self, version)
}

// removeLeftovers clears the note that an update is waiting, now that this
// process is the version it was waiting for.
//
// The previous binary is deliberately not among the things cleared. Starting is
// not serving: a version can come up far enough to run this line and still fail
// on the migration, on the port, on the certificate - and clearing it here would
// throw the way back away at the one moment it starts to be needed. It costs one
// file, and the next update removes it as its first act.
func removeLeftovers(self string) {
	_ = os.Remove(self + ".pending")
}
