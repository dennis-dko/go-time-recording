// Package common holds the result types that both the command and the query
// paths return, so a project just written and a project just read come back in
// one shape rather than in two that drift apart.
//
// These are not the wire format, and the difference matters when changing one.
// internal/interface/api/v1/rest converts them into its own response types -
// newProjectResponse and the two beside it - before anything is marshalled, so
// a field renamed here is an internal change. The contract a browser already
// reads, the one that is immutable once public, belongs to that package rather
// than to this one.
//
// They are built from internal/domain/model and never the other way round.
package common
