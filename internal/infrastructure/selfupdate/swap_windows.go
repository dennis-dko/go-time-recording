package selfupdate

import (
	"fmt"
	"os"
)

// swap puts the staged binary in place of the running one.
//
// Windows will not let a running executable be deleted or written over, and it
// will let it be *renamed* - which is the whole trick. The running file is moved
// aside, the new one takes its name, and the process carries on from the file it
// already has open under its new name until somebody restarts it.
//
// The one left behind cannot be deleted yet, for the same reason: it is still
// running. removeLeftovers clears it on the next start, when it is not.
func swap(self, staged string) error {
	old := self + ".old"

	// From a previous update, and by now not running. If this fails the rename
	// below will too, and that error is the one worth reporting.
	_ = os.Remove(old)

	if err := os.Rename(self, old); err != nil {
		return fmt.Errorf("cannot move the running binary aside: %w", err)
	}

	if err := os.Rename(staged, self); err != nil {
		// Put it back, or the installation has no binary at its own path and the
		// next start finds nothing.
		_ = os.Rename(old, self)

		return fmt.Errorf("cannot put the new binary in place: %w", err)
	}

	return markPending(self)
}

// removeLeftovers clears the note that an update is waiting, now that this
// process is the version it was waiting for.
//
// The binary moved aside by the swap stays, and not only because Windows would
// not have let it go earlier. Starting is not serving: a version can come up far
// enough to run this line and still fail on the migration, on the port, on the
// certificate - and clearing it here would throw the way back away at the one
// moment it starts to be needed. It costs one file, and the next update removes
// it as its first act.
func removeLeftovers(self string) {
	_ = os.Remove(self + ".pending")
}
