// Package service holds the domain rules - the answers that stay the same
// whoever is asking and however the question arrived.
//
// It is deliberately small. Most of what happens between a request and a row is
// orchestration, and that lives in internal/application; what is here is the
// handful of decisions it would be wrong to make twice. RequireVisible is both
// the example and the reason the package exists: the application layer had its
// own copy of it, word for word, and a rule agreed upon in two places is one
// that holds only until somebody widens one of them.
//
// The rule to read before changing anything else here: a project the viewer may
// not see is not found, never refused. The difference between 403 and 404 is
// itself the leak - answering the wrong one says that a project exists and that
// somebody has it.
package service
