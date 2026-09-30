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
package kerberos
