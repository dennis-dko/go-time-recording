// Package security holds the primitives every credential here is made from:
// password hashes, the session and API tokens and the digests they are stored
// as, the one-time codes of the second factor, and the sealing that keeps a
// secret the application must read back - a TOTP secret, the directory's bind
// password - unreadable in a copy of the database.
//
// Each choice is made once, in this package: the hash, the token length, the
// cipher. A caller picks a primitive by what it protects, never an algorithm,
// so none of them can choose a weaker one by accident.
package security
