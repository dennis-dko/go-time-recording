package model_test

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// What an installation calls itself, in the language that is reading.
func TestTheInstanceIsNamedInTheLanguageThatAsked(t *testing.T) {
	t.Parallel()

	branding := model.Branding{
		Title: "Time Recording",
		Translations: map[string]model.BrandingText{
			"de": {Title: "Zeiterfassung"},
			"en": {Title: "Time Recording"},
		},
	}

	if got := branding.TitleIn("de"); got != "Zeiterfassung" {
		t.Errorf("a German reader is shown %q", got)
	}

	// A language with no name of its own falls back to the untranslated one
	// rather than to nothing.
	if got := branding.TitleIn("fr"); got != "Time Recording" {
		t.Errorf("an untranslated language is shown %q", got)
	}

	// And a translation that exists but is empty is not a name.
	empty := model.Branding{
		Title:        "Time Recording",
		Translations: map[string]model.BrandingText{"de": {Title: "   "}},
	}

	if got := empty.TitleIn("de"); got != "Time Recording" {
		t.Errorf("a blank translation was used as a name: %q", got)
	}
}
