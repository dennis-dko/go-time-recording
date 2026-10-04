// Package restart replaces the running process with a fresh one, so settings
// that are only read at start-up can be applied without access to the server.
//
// The database connection, the log level, the metrics port and the trace
// exporter are all read while the application starts. Administering them from a
// screen and then asking somebody to find a shell is most of the way to not
// having administered them at all.
//
// Outside a container the mechanism is deliberately not "exit and let something
// else start us again". That works under systemd with Restart=, and it turns the
// button into an off switch everywhere else - including a binary started by
// hand, which is how the README says to run it. Replacing the process image
// instead needs nothing outside the process, so there pressing it cannot leave
// the installation down.
//
// Inside a container it is exactly that, and Now says how it came to be: a
// container run without a restart policy stays down, which is why Mode exists
// and the screen says which of the two it is offering before anybody presses.
//
// Either way the new process starts on what a stop and a start would give it,
// and nothing of what this one had exported into its own environment.
package restart
