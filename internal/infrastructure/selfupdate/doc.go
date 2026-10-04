// Package selfupdate finds out whether a newer release exists, and where it can,
// installs it.
//
// # Why "where it can"
//
// This ships four ways, and this package can update only one of them. A single
// binary can fetch its successor, prove it is the one the release published, and
// put it in its own place. A container cannot: it has no business talking to the
// runtime that started it, and a binary swapped inside a container is undone by
// the next `docker compose up` - which is the one moment somebody is certain the
// update took. Offering a button there would be offering an update that silently
// reverts.
//
// So the check runs everywhere and the install does not. Where it does not, the
// screen says what to run instead. That is not a gap in the feature; it is what
// updating a container is.
//
// A container can still be updated from the running application where the
// deployment adds the updater beside it, and that is the imageupdate package's
// business rather than this one's: there the image is replaced, not the binary.
//
// # What is verified
//
// The release publishes a SHA256SUMS beside its binaries. Nothing downloaded here
// is written into place until it hashes to what that file says, and the file is
// read from the same release as the binary. This is code that will be executed as
// the application on the next start: a download that is merely "probably fine" is
// not a standard worth having.
package selfupdate
