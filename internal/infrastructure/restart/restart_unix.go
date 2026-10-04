//go:build unix

package restart

import (
	"fmt"
	"os"
	"syscall"

	"github.com/dennis-dko/go-time-recording/internal/support/hosting"
)

// The binary to re-execute, resolved when the package is initialised rather
// than when the button is pressed.
//
// Early on purpose. os.Executable reads /proc/self/exe on Linux, which names the
// file this process is running from wherever that file is now - and an update
// moves it aside before putting the new one in its place. Asked at the press,
// the restart that is meant to start the installed version would run the one
// just replaced.
var executablePath, executableErr = os.Executable()

// startEnvironment is the environment to hand on, taken when the package is
// initialised as executablePath is: by the time the button is pressed it is no
// longer the one this process was started with.
//
// GoFr writes every key of the configuration file into it, the stored settings
// are exported into it, and main widens the log level in it. Handed on, each of
// those reaches the next process as a real environment variable, and a real
// variable beats the file. So an edited file lost to its own earlier value, on
// that restart and on every one after it; a setting cleared back to "follow the
// configuration file" came back as the value that had been exported, and the
// restart card then went quiet about it; a deleted connection file did not bring
// the installer back; and an installation with no stored log level logged at
// DEBUG from its first restart on. What the next process starts on is what a
// cold start would be given.
var startEnvironment = os.Environ()

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
// The environment it hands on is startEnvironment, and not the one this process
// has by now.
func Now() error {
	// In a container, stopping is the restart.
	//
	// execve would work here too, and for a long time it was what happened. It
	// was given up while it still handed on the environment as it stood, and in a
	// container the environment is most of the configuration: everything
	// ApplyTelemetry and ApplyDatasource exported was inherited by the
	// replacement, so a setting cleared back to "follow the configuration file"
	// came back as the value the previous process had exported. From a screen
	// whose whole promise is that the next start uses what is stored, that was
	// the promise not being kept.
	//
	// startEnvironment has since taken that fault out of the other path too, so
	// it no longer tells the two apart. What this path still has for it is that
	// the screens and the manual describe it.
	//
	// What starts the container again is the restart policy - the deployment
	// here sets unless-stopped, which restarts whatever the exit status - and a
	// container run without one stays down, which is why Mode exists and the
	// screen says which kind of restart this is.
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
	return syscall.Exec(executablePath, os.Args, startEnvironment)
}
