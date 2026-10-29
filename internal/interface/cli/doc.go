// Package cli is go-time-recording-client, the terminal client of an
// installation, for whoever would rather book from the shell than open the
// browser. It is a binary of its own, built from cmd/client, rather than a
// command of the web binary: somebody booking from a terminal needs neither the
// server nor its database drivers on the machine they book from.
//
// It asks the installation's own API with a personal token and nothing else, so
// it can do exactly what the token's owner may do and is held to every rule the
// API keeps - a booking made here is a booking made in the form. It keeps no
// state of its own between runs: the stopwatch it starts is the one the browser
// shows, running on the server, and stopping it anywhere stops it everywhere.
// What it knows of that API - the routes, the fields, the codes a refusal
// carries and how the page writes their values - is what the release it was
// built from knew, which is why the client to use is the one from the
// installation's own release.
//
// On a terminal, the client with no command opens a screen drawn with Bubble
// Tea and huh: the stopwatch running, today's entries, and a form for each
// change the commands make. The commands stay, because a script reads lines and
// a screen reader reads lines too, where a drawn screen serves neither; both
// make their changes through the same functions and say what they did in the
// same words.
//
// Those words are English, or another language the binary carries a dictionary
// for. Its own sentences are the dictionaries' track.* entries, named after the
// command it was before it was a binary, and what the web interface says as
// well - a refusal, a status, the unit of an hour - it says in the web
// interface's entries, so the terminal and the browser never put one thing two
// ways. The language is the one asked for with --lang or GTR_LANG, otherwise
// the one the account chose in the browser, otherwise the system's.
package cli
