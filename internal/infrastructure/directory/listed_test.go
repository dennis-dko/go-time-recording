package directory

import (
	"slices"
	"testing"

	"github.com/go-ldap/ldap/v3"

	appservice "github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// An entry without a mail address is listed by its identifier.
//
// The synchronisation keeps an account while its identifier is in the answer -
// that is what lets a renamed mailbox keep matching - and deletes it, with the
// hours recorded on it, once it is not. An entry that had lost its mail
// attribute was dropped here before its identifier could be looked at: a mailbox
// removed, or an attribute the bind account may no longer read, and the next run
// read a person the directory still held as a departure. No account is made for
// such an entry, since there is nothing to key one on, but it still says the
// person is there. Only an entry with neither an address nor an identifier says
// nothing at all.
func TestAnEntryWithoutAnAddressIsListedByItsIdentifier(t *testing.T) {
	config := model.LDAPConfig{
		EmailAttribute: "mail", IDAttribute: "entryUUID", NameAttribute: "cn",
		DefaultRole: model.RoleUser,
	}

	entries := []*ldap.Entry{
		ldap.NewEntry("uid=alice,ou=people,dc=example,dc=org", map[string][]string{
			"mail": {"Alice@Example.com"}, "entryUUID": {"a-1"}, "cn": {"Alice"},
		}),
		ldap.NewEntry("uid=carol,ou=people,dc=example,dc=org", map[string][]string{
			"entryUUID": {"c-1"}, "cn": {"Carol"},
		}),
		ldap.NewEntry("cn=service,dc=example,dc=org", map[string][]string{
			"cn": {"service"},
		}),
	}

	want := []appservice.ExternalUser{
		{ID: "a-1", Email: "alice@example.com", Name: "Alice", Role: model.RoleUser},
		{ID: "c-1", Name: "Carol", Role: model.RoleUser},
	}

	if got := listed(entries, config); !slices.Equal(got, want) {
		t.Errorf("the answer was listed as %+v, want %+v", got, want)
	}
}
