// Package kerberos accepts a browser's Kerberos ticket, so somebody signed in to
// their domain is signed in here without typing a password.
//
// It checks the SPNEGO token - the "Negotiate" a browser sends to an address it
// has been told to trust - against this service's own key, read from a keytab the
// operator places on the server, and says whose ticket it was. Who that is here
// is not decided in this package: the principal is looked up in the directory,
// which is what makes the account the same one a directory password sign-in
// reaches. A keytab is a secret on a par with a private key, which is why it is a
// file on the server and never passes through the settings screen.
//
// gokrb5 starts one goroutine of its own, the first time a ticket is checked:
// the cleaner of its replay cache, a process-wide singleton that sleeps for the
// clock skew between sweeps and never ends. It is not in the inventory CLAUDE.md
// keeps, which counts this repository's go statements, and nothing here could
// stop it - it keeps the cache that refuses a ticket presented twice, which has to
// outlive every request.
package kerberos
