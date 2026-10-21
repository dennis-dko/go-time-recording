package web_test

import (
	"regexp"
	"slices"
	"testing"
)

// The directory synchronisation is the card under the LDAP connection.
//
// The two are one subject - the connection, and the run that reads through it -
// and the tour's step on the LDAP card says the reconciliation is below it. For
// a while it was two cards below, with the sign-in provider between them, so an
// administrator who had just tested the connection scrolled past an unrelated
// card to preview the run. The administration screen is one column, so the order
// of the markup is the order on screen.
func TestTheDirectorySynchronisationIsTheCardUnderTheLDAPConnection(t *testing.T) {
	cards := cardsInOrder(readAsset(t, "index.html"))

	at := slices.Index(cards, "form-ldap")
	if at < 0 {
		t.Fatal("there is no LDAP card in the markup")
	}

	// The note standing in for the card, for an account the run is not offered
	// to, comes first and is hidden for everybody else.
	want := []string{"sync-elsewhere", "sync-card"}

	if got := cards[at+1 : min(at+1+len(want), len(cards))]; !slices.Equal(got, want) {
		t.Errorf("under the LDAP card come %q, want %q", got, want)
	}
}

// cardsInOrder returns the id of every card in the markup, in the order they
// are drawn.
func cardsInOrder(markup string) []string {
	var ids []string

	for _, tag := range cardOpening.FindAllString(markup, -1) {
		if id := elementID.FindStringSubmatch(tag); id != nil {
			ids = append(ids, id[1])
		}
	}

	return ids
}

// cardOpening matches the opening tag of an element drawn as a card, and
// elementID the id inside one.
var (
	cardOpening = regexp.MustCompile(`<[a-z]+(?:\s[^>]*)?\sclass="(?:[^"]*\s)?card(?:\s[^"]*)?"[^>]*>`)
	elementID   = regexp.MustCompile(`\sid="([^"]+)"`)
)
