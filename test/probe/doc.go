// Command probe checks that a database and a directory are reachable and
// usable, using the same drivers and the same queries the application does.
//
// The point of sharing the drivers is that a pass here means something. A
// generic port check, or psql from a shell, proves a socket answers; it does
// not prove that this binary's PostgreSQL driver can authenticate with these
// settings, or that the LDAP filter returns exactly one entry, or - the one
// that actually bites - that the directory hands out the stable identifier the
// synchronisation matches accounts on.
//
//	go run ./test/probe --db postgres --dsn "postgres://gtr:...@localhost:55432/..."
//	go run ./test/probe --ldap ldap://localhost:5389 --base-dn dc=example,dc=com
package main
