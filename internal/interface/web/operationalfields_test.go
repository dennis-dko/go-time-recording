package web_test

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// Every limit the form can set is one the server describes.
//
// The card shows two things about each field beside the box: what is currently
// in force, and - as the placeholder - what the configuration file supplies if
// the box is left empty. Both come from OperationalLimits, which is the same type
// on the wire for the effective values and the defaults.
//
// A field with a box and no entry in that type gets neither. sessionIdleMinutes
// was exactly that: in model.Limits, in the markup, in the list the form saves
// from, and missing from the wire - so it alone had an empty placeholder, saying
// nothing about what leaving it blank would do, and the "Currently in force" line
// named the other five.
//
// Checked in both directions. A limit the server sends and the form has no box
// for is the same disagreement seen from the other side.
func TestEveryOperationalLimitTravelsBothWays(t *testing.T) {
	js := asset(t, "/app.js")
	source := readSource(t, filepath.Join("..", "api", "v1", "rest", "settings_operational.go"))

	list := regexp.MustCompile(`(?s)const OPERATIONAL_FIELDS = \[(.*?)\];`).
		FindStringSubmatch(js)
	if list == nil {
		t.Fatal("app.js no longer declares OPERATIONAL_FIELDS; this test is reading nothing")
	}

	inForm := map[string]bool{}

	for _, match := range regexp.MustCompile(`'([a-zA-Z]+)'`).
		FindAllStringSubmatch(list[1], -1) {
		inForm[match[1]] = true
	}

	block := regexp.MustCompile(`(?s)type OperationalLimits struct \{(.*?)\n\}`).
		FindStringSubmatch(source)
	if block == nil {
		t.Fatal("settings_operational.go no longer declares OperationalLimits")
	}

	onWire := map[string]bool{}

	for _, match := range regexp.MustCompile(`json:"([a-zA-Z]+)"`).
		FindAllStringSubmatch(block[1], -1) {
		onWire[match[1]] = true
	}

	// Only against a pattern that has stopped matching. A short wire list is the
	// defect this exists for, so counting it here would report the finding as a
	// broken test - which is how the first version of this hid it.
	if len(inForm) < 6 {
		t.Fatalf("read %d fields out of OPERATIONAL_FIELDS, which is fewer than the "+
			"six limits this card has: %v", len(inForm), sorted(inForm))
	}

	if len(onWire) == 0 {
		t.Fatal("read no json tags out of OperationalLimits; the pattern has stopped " +
			"matching")
	}

	for _, field := range sorted(inForm) {
		if onWire[field] {
			continue
		}

		t.Errorf("the form can set %s and OperationalLimits does not carry it, so "+
			"that box has no placeholder saying what leaving it empty will do, and "+
			"the line naming what is in force cannot name it", field)
	}

	for _, field := range sorted(onWire) {
		if inForm[field] {
			continue
		}

		t.Errorf("the server sends %s among the limits and the form has no box for "+
			"it, so it can be read and never changed here", field)
	}
}

// sorted returns the keys of a set, in a fixed order so a failure reads the same
// way twice.
func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))

	for key := range set {
		out = append(out, key)
	}

	sort.Strings(out)

	return out
}

// A field's maxlength is what the server enforces, and not only what the form
// suggests.
//
// Every one of these was a number in the markup alone. The API took whatever it
// was sent, which makes a maxlength a hint to whoever fills in the form and no
// limit at all to whoever calls the endpoint - and the title and the banner are
// read by everybody who opens the sign-in page, before there is a session.
//
// Both directions matter. A markup limit above the server's is a form that lets
// somebody type a title, press Save and be told the title is invalid; one below
// it is a limit nobody can reach, which is the kind of number that stays wrong
// because nothing ever trips over it.
func TestTheFormLimitsAreTheOnesTheServerEnforces(t *testing.T) {
	html := asset(t, "/")

	for field, limit := range map[string]int{
		"title":       model.MaxTitleLength,
		"tabTitle":    model.MaxTabTitleLength,
		"banner":      model.MaxBannerLength,
		"footerText":  model.MaxFooterTextLength,
		"legalNotice": model.MaxLegalNoticeLength,
		"companyName": model.MaxCompanyNameLength,
		"message":     model.MaintenanceMessageLimit,

		// The account form bounded the name and not the address beside it, and
		// the server bounds both - so a long name was caught while typing and a
		// long address only on pressing Save.
		"email": model.MaxEmailLength,
	} {
		t.Run(field, func(t *testing.T) {
			pattern := regexp.MustCompile(
				`name="` + regexp.QuoteMeta(field) + `"[^>]*maxlength="(\d+)"`)

			match := pattern.FindStringSubmatch(html)
			if match == nil {
				t.Fatalf("the %s field has no maxlength, so the form offers to type "+
					"what the server will refuse", field)
			}

			written, err := strconv.Atoi(match[1])
			if err != nil {
				t.Fatalf("maxlength %q is not a number", match[1])
			}

			if written != limit {
				t.Errorf("the form allows %d characters and the server allows %d",
					written, limit)
			}
		})
	}
}
