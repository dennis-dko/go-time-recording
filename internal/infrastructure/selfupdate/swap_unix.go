//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
)

// swap puts the staged binary in place of the running one.
//
// A rename over the top, which unix allows while the file is open: the running
// process keeps the inode it was started from and carries on untroubled, and the
// next start reads the new one. That is also what makes this atomic - there is no
// moment at which the path holds half a binary.
func swap(self, staged string) error {
	if err := os.Rename(staged, self); err != nil {
		return fmt.Errorf("cannot put the new binary in place: %w", err)
	}

	return markPending(self)
}

// removeLeftovers is what the next start calls once it is running as the new
// version. Nothing to do here: the rename above left nothing behind.
func removeLeftovers(self string) {
	_ = os.Remove(self + ".pending")
}
