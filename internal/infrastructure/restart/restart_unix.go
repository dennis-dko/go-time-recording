//go:build unix

package restart

import (
	"fmt"
	"os"
	"syscall"
)

// The binary to re-execute, resolved when the package is initialised rather
// than when the button is pressed.
//
// Early on purpose. os.Executable resolves /proc/self/exe on Linux, and a
// deployment that replaces the binary underneath a running process - which is
// what an upgrade does - would otherwise be re-executed as whatever is at that
// path by then. Restarting into a different build than the one that was running
// is a surprise nobody asked the button for.
var executablePath, executableErr = os.Executable()

// Supported reports whether this process can replace itself.
func Supported() bool {
	return executableErr == nil
}

// Code names the refusal. Empty when there is none: restarting works here unless
// the running binary cannot be located.
func Code() string {
	if executableErr != nil {
		return "executableUnknown"
	}

	return ""
}

// Why explains a refusal, for a screen that has to say more than "no".
func Why() string {
	if executableErr != nil {
		return fmt.Sprintf("the running binary could not be located: %v", executableErr)
	}

	return ""
}

// Now replaces this process with a fresh one, and does not return.
//
// execve keeps the process id, the working directory - which is where GoFr looks
// for ./configs - and the open file descriptors that are not close-on-exec. Go
// marks sockets close-on-exec, so the listeners are released by the kernel as
// the image is replaced, and the new process binds the same ports rather than
// finding them held by a predecessor that no longer exists.
//
// The environment is passed through as it is now, which includes everything
// ApplyDatasource and ApplyTelemetry exported. That is harmless: the new process
// reads the same settings out of the same database and exports them again, and
// where it disagrees with this one - because the stored settings changed - the
// value it exports is the one that wins.
func Now() error {
	if executableErr != nil {
		return fmt.Errorf("%w: %w", ErrUnsupported, executableErr)
	}

	// Only returns on failure; on success this process is gone.
	return syscall.Exec(executablePath, os.Args, os.Environ())
}
