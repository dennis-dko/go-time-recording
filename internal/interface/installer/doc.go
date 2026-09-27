// Package installer serves the first screen of a brand new installation: the
// one that decides which database everything else lives in.
//
// # Why this exists before the application does
//
// Every other setting an installation has - the administrator's password, the
// timezone, the instance title, the directory bind - is stored in the database.
// So the database cannot be one of them. It has to be settled before there is
// anywhere to settle anything, which means before the application starts, which
// means before there is an account to sign in with.
//
// That is the whole reason this is a separate server rather than another step of
// the in-application wizard. Run afterwards, choosing a database would point the
// application at an empty one and abandon everything configured so far -
// including the changed administrator password, so the installation would come
// back up reachable with the initial password from the documentation.
//
// # How it hands over
//
// Serve listens, waits for a connection that has been proven to work, writes it
// to configs/datasource.json and returns. The caller then starts the application
// normally in the same process. No restart is involved, which is what makes this
// work in a container that would otherwise be considered crashed.
//
// # Why it asks for a token
//
// There is no database, so there is nobody to authenticate. Whoever reaches this
// screen decides where the installation keeps its data, and an installation
// exposed for the minutes before it is configured would otherwise be anyone's to
// claim. The token is printed to the log, which only somebody who can already
// see the process can read.
package installer
