//go:build unix

package restart

import (
	"fmt"
	"os"
	"syscall"

	"github.com/dennis-dko/go-time-recording/internal/pkg/hosting"
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

// Supported reports whether this process can restart itself, by either means.
func Supported() bool {
	return hosting.InContainer() || executableErr == nil
}

// Code names the refusal. Empty when there is none: restarting works here unless
// the running binary cannot be located.
func Code() string {
	if !Supported() {
		return "executableUnknown"
	}

	return ""
}

// Why explains a refusal, for a screen that has to say more than "no".
func Why() string {
	if !Supported() {
		return fmt.Sprintf("the running binary could not be located: %v", executableErr)
	}

	return ""
}

// Mode says what pressing the button will actually do, which is not the same
// thing everywhere.
//
// Outside a container this process replaces itself and the installation is never
// not running. Inside one it stops instead, and what starts a new container is
// the restart policy - which is a thing this process cannot see. The screen says
// which of the two it is rather than letting somebody find out.
func Mode() string {
	if hosting.InContainer() {
		return ModeContainer
	}

	return ModeProcess
}

// Now replaces this process with a fresh one, or stops it so something else
// starts one, and does not return.
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
	// In a container, stopping is the restart.
	//
	// execve would work here too, and for a long time it was what happened - but
	// it keeps the environment, and in a container the environment is most of the
	// configuration. Everything ApplyTelemetry and ApplyDatasource exported is
	// inherited by the replacement, so a setting cleared back to "follow the
	// configuration file" came back as the value the previous process had
	// exported. The manual said so, as a consequence to know about; from a
	// screen whose whole promise is that the next start uses what is stored, it
	// is the promise not being kept.
	//
	// Exiting gives a container built from the image and the compose file again,
	// with nothing carried over. What starts it is the restart policy - the
	// deployment here sets unless-stopped, which restarts whatever the exit
	// status - and a container run without one stays down, which is why Mode
	// exists and the screen says which kind of restart this is.
	//
	// Status zero, because this is a deliberate stop rather than a failure. It is
	// also the one status "on-failure" does not restart on, which is the case the
	// screen warns about.
	if hosting.InContainer() {
		os.Exit(0)
	}

	if executableErr != nil {
		return fmt.Errorf("%w: %w", ErrUnsupported, executableErr)
	}

	// Only returns on failure; on success this process is gone.
	return syscall.Exec(executablePath, os.Args, os.Environ())
}
