package selfupdate

import (
	"fmt"
	"os"
)

// swap puts the staged binary in place of the running one, keeping the old.
//
// One implementation for both platforms, and that is the point rather than an
// accident of it. Windows will not let a running executable be deleted or
// written over, and it will let it be *renamed* - which is the whole trick: the
// running file is moved aside, the new one takes its name, and the process
// carries on from the file it already has open until somebody restarts it.
//
// Unix has no such rule. A rename over the top would be enough to install there,
// and is not enough to undo: the obvious version simply overwrote the file, and
// with it the only copy of the version that was known to work. If the new one
// then failed to start, the screen that could have put the old one back had gone
// with it. So Unix does what Windows is forced to do, deliberately - the old one
// is moved aside first, and kept.
//
// That convergence is why this used to be two files behind a build tag with the
// same body in each. The bodies had already been made identical on purpose, so
// what the split was still buying was a second copy to keep in step, each one
// invisible to a lint, vet or test run on the other platform - which is exactly
// the arrangement in which two things stop agreeing without anyone seeing it.
//
// The one left behind cannot be deleted here on Windows, because it is still
// running. removeLeftovers does not clear it on the next start either, and that
// is deliberate too - see there.
func swap(self, staged, version string) error {
	old := self + ".old"

	// Whatever the previous update left. Removed rather than kept, because the
	// version worth being able to go back to is the one that was running a
	// moment ago. If this fails the rename below will too, and that error is the
	// one worth reporting.
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
//
// On Windows it could not be cleared here in any case, since it is the file this
// process is running from. The reason above is the one that decides it, and it
// holds on both platforms.
func removeLeftovers(self string) {
	_ = os.Remove(self + ".pending")
}
