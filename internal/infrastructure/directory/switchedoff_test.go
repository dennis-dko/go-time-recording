package directory

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// adTime writes a moment the way Active Directory counts it: 100-nanosecond steps
// since the first of January 1601.
func adTime(t time.Time) string {
	return strconv.FormatInt((t.Unix()+11_644_473_600)*10_000_000, 10)
}

// An entry Active Directory has switched off reaches a sign-in as switched off.
//
// A password sign-in is refused by the bind for such an account. A ticket sign-in
// reads only the entry, so the entry has to say it - the account's disabled flag,
// or an expiry already past. A directory carrying neither attribute, which is
// every one but Active Directory, reads as on.
func TestAnEntryActiveDirectoryHasSwitchedOffArrivesSwitchedOff(t *testing.T) {
	config := model.LDAPConfig{EmailAttribute: "mail", IDAttribute: "objectGUID", NameAttribute: "cn"}

	for _, c := range []struct {
		what  string
		attrs map[string][]string
		off   bool
	}{
		{"an ordinary account", map[string][]string{"userAccountControl": {"512"}}, false},
		{"a disabled account", map[string][]string{"userAccountControl": {"514"}}, true},
		{"an account that expired", map[string][]string{
			"userAccountControl": {"512"}, "accountExpires": {adTime(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))},
		}, true},
		{"an account that will expire", map[string][]string{
			"accountExpires": {adTime(time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC))},
		}, false},
		{"an account that never expires", map[string][]string{"accountExpires": {"9223372036854775807"}}, false},
		{"an account with no expiry set", map[string][]string{"accountExpires": {"0"}}, false},
		{"a directory that carries neither", map[string][]string{}, false},
	} {
		attrs := map[string][]string{"mail": {"jdoe@example.com"}, "cn": {"Jane Doe"}}
		for name, values := range c.attrs {
			attrs[name] = values
		}

		user, err := signedIn(ldap.NewEntry("cn=jdoe,dc=example,dc=com", attrs), config, "jdoe")
		if err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}

		if user.Disabled != c.off {
			t.Errorf("%s arrived with Disabled=%v, want %v", c.what, user.Disabled, c.off)
		}
	}
}

// A sign-in asks the directory for the two attributes that say so.
//
// An entry carries only what the search asked for, so a check of attributes
// nobody requested would hold in every case built from a made-up entry and never
// once in a real one.
func TestASignInAsksForWhatSaysAnAccountIsSwitchedOff(t *testing.T) {
	asked := signInAttributes(model.LDAPConfig{EmailAttribute: "mail", IDAttribute: "objectGUID", NameAttribute: "cn"})

	for _, name := range []string{"userAccountControl", "accountExpires"} {
		if !slices.Contains(asked, name) {
			t.Errorf("a sign-in does not ask the directory for %s: %v", name, asked)
		}
	}
}
