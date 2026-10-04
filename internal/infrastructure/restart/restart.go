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

	// ModeContainer: this process stops, and the container manager starts the
	// container again. Which it only does under a restart policy that restarts a
	// container that ended without an error - see Now.
	ModeContainer = "container"
)
