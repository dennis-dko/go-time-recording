// Package kerberostest issues the Kerberos tickets the tests present, without a
// KDC.
//
// A KDC's part in a sign-in is to seal a ticket with the service's key. With the
// key in a keytab built here, a test can do the same, and what it presents is a
// real SPNEGO token the service checks exactly as it checks a browser's - so the
// package that accepts tickets, the integration suite and the browser suite all
// test against real tokens, and none of them needs a directory server that also
// runs a KDC. One place, because three copies of how a ticket is made would be
// three chances to test against a token no browser sends.
package kerberostest
