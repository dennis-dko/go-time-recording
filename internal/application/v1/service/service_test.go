package service

import "testing"

// An address whose domain is one label is an address.
//
// The rule required a dot in it, which reads as the obvious shape and is wrong
// on exactly the networks this application is most often installed on. "@local"
// and a bare host name are ordinary there - and the account the application
// creates for itself is admin@local, so the screen refused to create the kind of
// address it had already made.
//
// Still not a full validation, which no pattern can be: what is rejected here is
// input that is obviously not an address, and delivery is the real test.
func TestAnAddressMayHaveASingleLabelDomain(t *testing.T) {
	for _, address := range []string{
		"anna@local",
		"anna.weber@local",
		"admin@local",
		"anna@intranet",
		"anna@example.com",
		"anna@mail.example.co.uk",
	} {
		if !validEmail(address) {
			t.Errorf("%q was refused as an address", address)
		}
	}

	// And the obvious non-addresses still are.
	for _, address := range []string{
		"",
		"anna",
		"@local",
		"anna@",
		"anna@local.",
		"anna@.local",
		"anna weber@local",
		"anna@@local",
	} {
		if validEmail(address) {
			t.Errorf("%q was accepted as an address", address)
		}
	}
}
