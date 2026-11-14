package web_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The directory card offers, as the role every account from the directory starts
// with, only roles the server will store - and the server refuses a role holding
// any of the rights that amount to the installation. The page has no way to ask
// which those are, so directoryRoleChoices keeps a copy of them, and this holds
// the copy to model.AdministeringPermissions in both directions: a right missing
// from it is a role offered and then refused on save, which is how users:write
// was left out on the page as it was on the server.
func TestTheDirectorysRoleChoicesLeaveOutWhatTheServerRefuses(t *testing.T) {
	script := asset(t, "/app.js")

	found := regexp.MustCompile(`function directoryRoleChoices\(\) \{\s*const administering = \[([^\]]*)\]`).
		FindStringSubmatch(script)
	if found == nil {
		t.Fatal("directoryRoleChoices no longer opens with the rights it leaves out; this test is reading nothing")
	}

	var listed []string
	for _, right := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(found[1], -1) {
		listed = append(listed, right[1])
	}

	want := slices.Clone(model.AdministeringPermissions())

	slices.Sort(listed)
	slices.Sort(want)

	if !slices.Equal(listed, want) {
		t.Errorf("the directory card leaves out roles holding %v, and the server refuses roles holding %v",
			listed, want)
	}
}
