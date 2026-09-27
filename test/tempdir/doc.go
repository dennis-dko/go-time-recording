// Package tempdir hands a test a directory that is removed only once Windows has
// let go of it.
//
// A test that runs a program - an instance of the application, a downloaded
// binary asked for its version, the updater's shell script - leaves files that
// Windows keeps open for a while after the process has exited. t.TempDir removes
// its directory straight away and then fails a test that had passed, with "The
// directory is not empty" or "being used by another process". The harness, the
// self-update tests and the updater tests each met it separately, and each had
// grown or was about to grow its own copy of the same retry; this is the one
// place it lives. CI runs on Linux, where an open file does not stop a removal,
// so only a Windows run ever shows it.
package tempdir
