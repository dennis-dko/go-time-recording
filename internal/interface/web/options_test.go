package web_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// Some of the dropdowns in this interface are filled by the server - the roles,
// the users, the projects - and cannot drift, because there is one list and the
// markup holds none of it.
//
// Two are not. The database dialects and the trace exporters are closed sets the
// server owns and validates, written out a second time as <option> elements
// because the pages that offer them are static. That duplication is fine as long
// as something notices when the two stop agreeing, and this is that something.
//
// Both directions matter and fail differently. An option the screen offers and
// the server rejects is a saved form answered with a 400 naming a field the
// administrator did choose. A value the server accepts but no option offers is a
// supported database nobody can select.

// optionValues reads the value="" of every option inside the named select.
func optionValues(t *testing.T, html, selectName string) []string {
	t.Helper()

	// The select, up to its closing tag. Matching to the end of the document
	// instead would pull in every later option on the page.
	block := regexp.MustCompile(
		`(?s)<select[^>]*name="` + regexp.QuoteMeta(selectName) + `"[^>]*>(.*?)</select>`)

	found := block.FindStringSubmatch(html)
	if found == nil {
		t.Fatalf("no <select name=%q> in the markup", selectName)
	}

	var values []string

	for _, m := range regexp.MustCompile(`value="([^"]*)"`).FindAllStringSubmatch(found[1], -1) {
		values = append(values, m[1])
	}

	return values
}

// missing reports which of want is absent from got.
func missing(want, got []string) []string {
	var absent []string

	for _, value := range want {
		if !contains(got, value) {
			absent = append(absent, value)
		}
	}

	return absent
}

func contains(values []string, want string) bool {
	return slices.Contains(values, want)
}

// The log level select carries the same "follow the configuration file" option
// as the exporter, and for the same reason.
func TestTheLogLevelsOfferedAreTheOnesGoFrEmits(t *testing.T) {
	offered := optionValues(t, asset(t, "/"), "logLevel")
	supported := model.SupportedLogLevels()

	if absent := missing(supported, offered); len(absent) > 0 {
		t.Errorf("the Settings screen offers no option for %v, so a level the process can emit "+
			"cannot be chosen", absent)
	}

	for _, value := range offered {
		if value == "" {
			continue
		}

		if !contains(supported, value) {
			// Worth catching rather than shrugging at: GoFr does not refuse a
			// level it cannot read, it quietly uses INFO instead.
			t.Errorf("the Settings screen offers the level %q, which GoFr would read as INFO", value)
		}
	}
}

func TestTheDatabasesOfferedAreTheOnesTheServerAccepts(t *testing.T) {
	offered := optionValues(t, asset(t, "/"), "dialect")
	supported := config.SupportedDialects()

	if absent := missing(supported, offered); len(absent) > 0 {
		t.Errorf("the Settings screen offers no option for %v, so a supported database "+
			"cannot be chosen there", absent)
	}

	for _, value := range offered {
		if !contains(supported, value) {
			t.Errorf("the Settings screen offers %q, which the server refuses on save", value)
		}
	}
}

// The exporter select carries two options that are not exporters: following the
// configuration file, and switching tracing off. Both are states the server
// stores rather than values it validates against the list, so they are named
// here instead of being read as drift.
func TestTheTraceExportersOfferedAreTheOnesTheServerAccepts(t *testing.T) {
	const (
		followTheFile = ""
		switchedOff   = "off"
	)

	offered := optionValues(t, asset(t, "/"), "traceExporter")
	supported := model.SupportedTraceExporters()

	if absent := missing(supported, offered); len(absent) > 0 {
		t.Errorf("the Settings screen offers no option for %v, so a supported exporter "+
			"cannot be chosen there", absent)
	}

	for _, value := range offered {
		if value == followTheFile || value == switchedOff {
			continue
		}

		if !contains(supported, value) {
			t.Errorf("the Settings screen offers the exporter %q, which the server refuses on save",
				value)
		}
	}

	// The two states, checked rather than assumed: without them the screen can
	// express "off" only by picking an exporter, and the difference between an
	// administered off and following the file is what the whole setting turns on.
	for _, state := range []string{followTheFile, switchedOff} {
		if !contains(offered, state) {
			t.Errorf("the exporter select has no %q option", state)
		}
	}
}
