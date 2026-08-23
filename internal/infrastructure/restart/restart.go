// Package restart replaces the running process with a fresh one, so settings
// that are only read at start-up can be applied without access to the server.
//
// The database connection, the log level, the metrics port and the trace
// exporter are all read while the application starts. Administering them from a
// screen and then asking somebody to find a shell is most of the way to not
// having administered them at all.
//
// The mechanism is deliberately not "exit and let something else start us
// again". That works under Docker with a restart policy and under systemd with
// Restart=, and it turns the button into an off switch everywhere else -
// including a binary started by hand, which is how the README says to run it.
// Replacing the process image instead needs nothing outside the process, so
// there is no arrangement in which pressing it leaves the installation down.
package restart

import "errors"

// ErrUnsupported is returned where a process cannot replace itself.
var ErrUnsupported = errors.New("this platform cannot restart the application from inside it")

// What pressing the button does, which differs by where this is running.
//
// Named rather than described, because the screen has to say the right sentence
// in the reader's language and a sentence written here would arrive in English.
const (
	// ModeProcess: this process replaces itself and the installation is never
	// not running.
	ModeProcess = "process"

	// ModeContainer: this process stops, and the container manager starts a new
	// container from the image. Which it only does if it was told to - see Now.
	ModeContainer = "container"
)
