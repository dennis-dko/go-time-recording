package document

import (
	"testing"
	"time"
)

// The foot of the page is the one line the server writes itself, so it is the
// one line that can be in the wrong language.
//
// It printed ISO order for everybody. That is unambiguous and it is not how a
// German page is dated, so a German evaluation ended on a line no German
// document ends on - and the name beside it was in English for the same reason.
func TestTheMomentIsWrittenTheWayTheLanguageWritesIt(t *testing.T) {
	t.Parallel()

	written := time.Date(2026, time.August, 27, 23, 40, 0, 0, time.UTC)

	if got := moment(written, "de"); got != "27.08.2026 23:40" {
		t.Errorf("a German page is dated %q", got)
	}

	if got := moment(written, "en"); got != "2026-08-27 23:40" {
		t.Errorf("an English page is dated %q", got)
	}

	// A language nobody has translations for still gets a date rather than a
	// blank, and gets the one the rest of the interface falls back to.
	if got := moment(written, ""); got != "2026-08-27 23:40" {
		t.Errorf("an unnamed language is dated %q", got)
	}
}
