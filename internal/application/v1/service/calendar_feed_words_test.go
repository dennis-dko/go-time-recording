package service

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/i18n"
)

// The feed calls a thing what the screen calls it, in every language: the hour,
// an entry without a project, and every reason a day can be away.
//
// Its words are the interface's own, from the dictionaries every part of the
// application reads - all but the English, which the interface keeps in its
// script as the fallbacks of its lookups and the feed writes out beside its own.
// So this reads those fallbacks out of app.js and holds the feed's English to
// them, and every other language to its dictionary.
func TestTheFeedCallsThingsWhatTheScreenCallsThem(t *testing.T) {
	script, err := os.ReadFile("../../../interface/web/asset/app.js")
	if err != nil {
		t.Fatalf("read the interface's script: %v", err)
	}

	english := func(key string) string {
		t.Helper()

		found := regexp.MustCompile(`t\('` + regexp.QuoteMeta(key) + `', '([^']*)'`).FindSubmatch(script)
		if found == nil {
			t.Fatalf("the interface never looks %s up with an English fallback", key)
		}

		return string(found[1])
	}

	same := func(what, feed, screen string) {
		t.Helper()

		if feed != screen {
			t.Errorf("%s: the feed writes %q where the screen writes %q", what, feed, screen)
		}
	}

	unprojected := &model.Timesheet{DurationHours: 1}

	for _, language := range i18n.Languages() {
		words := feedWords{i18n.For(language)}
		says := func(key string) string {
			if language == i18n.English {
				return english(key)
			}

			return i18n.Dictionary(language)[key]
		}

		same("the hour in "+language, words.hoursOf(1), words.words.Number(1)+" "+says("unit.hours"))
		same("no project in "+language, words.entryLine(unprojected, nil), words.hoursOf(1)+" · "+says("ts.noProject"))

		for _, kind := range model.AbsenceKinds() {
			same(kind+" in "+language, words.absenceName(kind), says("absence."+kind))
		}
	}
}

// What the feed says that the screen does not - a day's total - is in every
// dictionary, under calendarFeed.*, and nothing there is a sentence the feed no
// longer says.
func TestTheFeedsOwnSentencesAreInEveryDictionary(t *testing.T) {
	source, err := os.ReadFile("calendar_feed_application_service.go")
	if err != nil {
		t.Fatal(err)
	}

	said := map[string]bool{}
	for _, key := range regexp.MustCompile(`"(calendarFeed\.[A-Za-z.]+)"`).FindAllSubmatch(source, -1) {
		said[string(key[1])] = true
	}

	if len(said) == 0 {
		t.Fatal("the feed says nothing of its own; this case is reading nothing")
	}

	for _, language := range i18n.Languages()[1:] {
		words := i18n.Dictionary(language)

		for key := range said {
			if words[key] == "" {
				t.Errorf("the feed says %s and %s has no words for it", key, language)
			}
		}

		for key := range words {
			if strings.HasPrefix(key, "calendarFeed.") && !said[key] {
				t.Errorf("%s has %s, which the feed no longer says", language, key)
			}
		}
	}

	if got := (feedWords{i18n.For("de")}).booked(7.5); got != "7,50 Std. gebucht" {
		t.Errorf("a German day's total reads %q", got)
	}
}
