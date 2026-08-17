package model_test

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The tab is named separately where an installation says so, and by the title
// where it does not.
//
// The fallback is what every installation that has never opened this setting
// relies on: the field arrived after they were all configured, so it is empty
// everywhere, and an empty field that meant an empty tab would rename every one
// of them to nothing.
func TestTheTabIsNamedSeparatelyOrByTheTitle(t *testing.T) {
	for name, tc := range map[string]struct {
		branding model.Branding
		want     string
	}{
		"nothing of its own": {
			branding: model.Branding{Title: "Zeiterfassung der Beispiel GmbH"},
			want:     "Zeiterfassung der Beispiel GmbH",
		},
		"its own name": {
			branding: model.Branding{Title: "Zeiterfassung der Beispiel GmbH", TabTitle: "Zeiterfassung"},
			want:     "Zeiterfassung",
		},
		// Spaces are not a name. Somebody clearing the field leaves whatever the
		// keyboard left behind, and a tab titled " " is a tab titled nothing.
		"blank": {
			branding: model.Branding{Title: "Zeiterfassung", TabTitle: "   "},
			want:     "Zeiterfassung",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.branding.TabName(); got != tc.want {
				t.Errorf("the tab is called %q, want %q", got, tc.want)
			}
		})
	}
}
