// Package announce tells every open browser something, at once.
//
// Everything else this application says to a browser is an answer to a question
// the browser asked. That is the right shape for almost all of it - and it is the
// wrong shape for exactly one thing: the binary underneath is being replaced, and
// the people using it have a minute's notice at most.
//
// A poll cannot carry that. The permission notice polls once a minute, which is
// fine for what it is - the server enforces the change immediately whatever the
// interface believes, so the only cost of being late is a stale button. An update
// is not like that. Being told forty seconds after the restart began is being
// told nothing.
//
// So this is the other direction: a connection the browser opens and leaves open,
// and a line written down it when there is something to say.
package announce
