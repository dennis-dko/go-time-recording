//go:build !unix

package restart

// Windows has no execve. A process cannot replace its own image, so the nearest
// equivalent would be to start a second one and exit - which leaves a window in
// which the old process still holds the port and the new one cannot bind it, and
// which ends with no application running if the new one fails to start for any
// reason at all.
//
// That is the failure this whole package exists to avoid, so the button is not
// offered here and the screen says why. The deployment target is a Linux
// container; this is the developer's machine.

// Supported reports whether this process can replace itself.
func Supported() bool { return false }

// Code names the refusal, so a screen can say it in the reader's own language.
//
// Why() is written in English at the point the limitation is decided, which is
// right for a log and wrong for the person reading the screen - and there is more
// than one refusal, so one translated sentence for all of them would tell a Linux
// reader they are on Windows.
func Code() string { return "noExecve" }

// Why explains the refusal, for a screen that has to say more than "no".
func Why() string {
	return "restarting from inside the application needs execve, which Windows does not have; " +
		"restart the application the way it was started"
}

// Now always fails here.
func Now() error { return ErrUnsupported }
