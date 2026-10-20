package service

import (
	"os"
	"regexp"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The feed calls a thing what the screen calls it, in both languages: the hour,
// an entry without a project, and every reason a day can be away.
//
// The feed's words are a second copy of the interface's, kept on the server
// because the feed is written there. Two copies agreeing today is how they come
// to disagree, so this reads the interface's dictionary and its English
// fallbacks out of app.js and holds the feed's table to them.
func TestTheFeedCallsThingsWhatTheScreenCallsThem(t *testing.T) {
	script, err := os.ReadFile("../../../interface/web/assets/app.js")
	if err != nil {
		t.Fatalf("read the interface's script: %v", err)
	}

	german := func(key string) string {
		t.Helper()

		found := regexp.MustCompile(`'` + regexp.QuoteMeta(key) + `': '([^']*)'`).FindSubmatch(script)
		if found == nil {
			t.Fatalf("the interface has no German for %s", key)
		}

		return string(found[1])
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

	en, de := feedLanguages[model.LanguageEnglish], feedLanguages[model.LanguageGerman]

	same("the hour in English", en.hours, english("unit.hours"))
	same("the hour in German", de.hours, german("unit.hours"))
	same("no project in English", en.noProject, english("ts.noProject"))
	same("no project in German", de.noProject, german("ts.noProject"))

	for _, kind := range model.AbsenceKinds() {
		key := "absence." + kind

		same(kind+" in English", en.absenceName(kind), english(key))
		same(kind+" in German", de.absenceName(kind), german(key))
	}

	for _, language := range model.SupportedLanguages() {
		if _, written := feedLanguages[language]; !written {
			t.Errorf("the interface is offered in %q and no feed is written in it", language)
		}
	}
}
