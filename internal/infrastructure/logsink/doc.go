// Package logsink keeps the most recent log lines in memory so an
// administrator can read them from the running application.
//
// # Why it intercepts the process output
//
// The interesting lines are not the ones this code writes. They are the ones
// the framework writes: the request log, a failing database statement, what
// happened during the migrations. GoFr builds its logger inside gofr.New()
// with os.Stdout captured at construction, and exposes no way to supply a
// writer, so the only place to see everything it emits is the process's own
// output.
//
// So Capture replaces os.Stdout and os.Stderr with pipes, reads the lines,
// forwards each one to the real console unchanged, and keeps a copy. Nothing
// downstream changes: a container still gets identical output on its stdout.
//
// # What this costs
//
// GoFr decides between pretty-printed and JSON output by asking whether its
// output is a terminal. A pipe is not, so with capture installed the console
// gets JSON even when a person is watching. That is the deliberate trade: JSON
// is what the log viewer needs to show a level and a timestamp rather than a
// wall of text, it is what a log collector wants in production anyway, and
// SetPassthroughRenderer exists for the development case that wants readable
// lines back.
//
// # What can be lost
//
// A Fatal writes and then calls os.Exit immediately. The bytes reach the
// kernel's pipe buffer, but this package's reader may not be scheduled before
// the process is gone, so a fatal line can be missing from the console. That
// matters because a fatal is exactly the message an operator needs, which is
// why the application pre-flights the failures it can predict - a taken port,
// an unusable database - and reports those itself rather than letting the
// framework exit on them.
//
// A restart has the same shape: restart.Now exits, or replaces the image, the
// moment after the line saying why. Releasing the capture first is not the
// remedy it looks like, because GoFr's logger keeps writing to the pipe it was
// handed, so a restart that then failed would report its failure into a closed
// pipe. Every other way the process ends lets the reader finish first: die and
// a stopped installer release the capture before they exit, and an ordinary
// stop returns through main, which releases it on the way out.
package logsink
