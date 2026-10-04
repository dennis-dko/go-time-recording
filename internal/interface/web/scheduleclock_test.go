package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// The directory card says which clock its schedule runs on.
//
// A schedule is read by the scheduler GoFr builds, on the process's own clock,
// which in the container this application ships is UTC - and not in the instance
// timezone the same Settings screen administers. "0 4 * * *" typed in Los Angeles
// for four in the morning runs at eight the evening before. The run it starts
// deletes accounts, so when it runs is not a detail, and nothing on the card or in
// either manual said which clock it was.
func TestTheDirectoryScheduleSaysWhichClockItRunsOn(t *testing.T) {
	english := regexp.MustCompile(`(?s)data-i18n="sync.scheduleHint">([^<]*)<`).
		FindStringSubmatch(asset(t, "/"))
	if english == nil {
		t.Fatal("the directory card no longer carries sync.scheduleHint")
	}

	for language, hint := range map[string]string{
		"English": english[1],
		"German":  dictionaries(t)["de"]["sync.scheduleHint"],
	} {
		if !strings.Contains(hint, "UTC") {
			t.Errorf("the %s hint beside the schedule does not say it runs in UTC in the "+
				"shipped container: %s", language, hint)
		}
	}
}
