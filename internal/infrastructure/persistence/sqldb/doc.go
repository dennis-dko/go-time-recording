// Package sqldb implements the domain repositories on top of a SQL database.
//
// One implementation serves every dialect GoFr supports rather than one
// package per engine: the queries are identical apart from placeholder syntax
// and how a generated id is read back, both of which are handled here.
package sqldb
