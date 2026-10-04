package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// The sentence beside the restart button in a container names the restart
// policies under which pressing it is a restart, and the one under which it is
// a stop.
//
// In a container the button ends the process cleanly, and what starts it again
// is the container manager. "always" and "unless-stopped" do; "on-failure"
// restarts only a container that failed, which a deliberate stop is not. The
// sentence said the manager starts it again "if it was told to restart the
// container" - which somebody running under on-failure has every reason to read
// as themselves, and for them the button was an off switch.
func TestTheContainerRestartSentenceNamesThePolicies(t *testing.T) {
	// The fallback is several literals joined across lines.
	english := regexp.MustCompile(
		`t\('restart\.modeContainer',\s*('(?:[^'\\]|\\.)*'(?:\s*\+\s*'(?:[^'\\]|\\.)*')*)`).
		FindStringSubmatch(asset(t, "/app.js"))
	if english == nil {
		t.Fatal("app.js no longer words the container restart through restart.modeContainer")
	}

	for language, sentence := range map[string]string{
		"English": english[1],
		"German":  dictionaries(t)["de"]["restart.modeContainer"],
	} {
		for _, policy := range []string{"always", "unless-stopped", "on-failure"} {
			if !strings.Contains(sentence, policy) {
				t.Errorf("the %s sentence beside the button does not name %q: %s",
					language, policy, sentence)
			}
		}
	}
}
