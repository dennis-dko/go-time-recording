// Package cli is the binary's way in from a terminal: go-time-recording track, a
// client against an installation for whoever would rather book from the shell
// than open the browser.
//
// It asks the installation's own API with a personal token and nothing else, so
// it can do exactly what the token's owner may do and is held to every rule the
// API keeps - a booking made here is a booking made in the form. It keeps no
// state of its own between runs: the stopwatch it starts is the one the browser
// shows, running on the server, and stopping it anywhere stops it everywhere.
package cli
