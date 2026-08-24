package installer

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// The installer is the one screen that cannot be corrected from inside the
// application, because it is what runs when there is no application yet. A
// database it fails to offer is one nobody can choose without editing a file by
// hand - which is exactly the situation this screen exists to avoid.
//
// So the list it shows is checked against the list the server accepts, the same
// way the Settings screen's is. Read out of the embedded filesystem rather than
// from disk, so it is the markup that actually ships.

func TestTheInstallerOffersEveryDatabaseTheServerAccepts(t *testing.T) {
	page, err := assets.ReadFile("assets/install.html")
	if err != nil {
		t.Fatalf("the installer page is not embedded: %v", err)
	}

	block := regexp.MustCompile(`(?s)<select[^>]*id="dialect"[^>]*>(.*?)</select>`).
		FindStringSubmatch(string(page))
	if block == nil {
		t.Fatal(`no <select id="dialect"> in the installer page`)
	}

	var offered []string

	for _, m := range regexp.MustCompile(`value="([^"]*)"`).FindAllStringSubmatch(block[1], -1) {
		offered = append(offered, m[1])
	}

	supported := config.SupportedDialects()

	for _, want := range supported {
		if !contains(offered, want) {
			t.Errorf("the installer offers no option for %q, so a supported database cannot be "+
				"chosen on a first start", want)
		}
	}

	for _, value := range offered {
		if !contains(supported, value) {
			t.Errorf("the installer offers %q, which the server refuses - and refuses at the one "+
				"moment there is no other way in", value)
		}
	}
}

func contains(values []string, want string) bool {
	return slices.Contains(values, want)
}

// The installer's wait-for-the-application loop has to be able to tell the
// application apart from itself.
//
// Once the application has the port it serves the single-page app for every
// unknown path, /install/state included - so that path answers 200 with HTML, and
// a loop that only checks res.ok concludes the installer is still in charge and
// waits for ever. It did: the page never reloaded after the database was
// configured. The content type is what separates them.
func TestTheWaitLoopDistinguishesTheApplicationFromTheInstaller(t *testing.T) {
	page, err := assets.ReadFile("assets/install.html")
	if err != nil {
		t.Fatalf("reading the installer page: %v", err)
	}

	markup := string(page)

	if !strings.Contains(markup, "waitForTheApplication") {
		t.Fatal("the installer no longer waits for the application at all")
	}

	// The check that matters. Written out rather than matched loosely, because
	// the failure it prevents is silent: everything looks fine and the page just
	// never moves on.
	if !strings.Contains(markup, "res.ok && (res.headers.get('content-type') || '').includes('json')") {
		t.Error("the loop treats any 200 on /install/state as the installer still " +
			"being in charge; the application answers that path with the SPA")
	}

	if !strings.Contains(markup, "location.reload()") {
		t.Error("the loop never reloads the page")
	}
}

// The installer speaks the browser's language.
//
// It is the first screen anybody sees and it was English only, on a German
// machine with a German browser - which reads as software that was not meant for
// you. It cannot ask the server which language to use, because there is no
// database yet and no session; the browser's own preference is all there is.
func TestTheInstallerFollowsTheBrowserLanguage(t *testing.T) {
	page, err := assets.ReadFile("assets/install.html")
	if err != nil {
		t.Fatalf("reading the installer page: %v", err)
	}

	markup := string(page)

	if !strings.Contains(markup, "navigator.languages") {
		t.Error("the installer never looks at what language the browser asks for")
	}

	// Every piece of static text it shows has to be reachable by a key, or the
	// German pass leaves half the page in English - which is worse than all of it.
	for _, key := range []string{
		"title", "intro", "token.title", "token.text", "db.title", "db.text",
		"action.test", "action.save",
	} {
		if !strings.Contains(markup, `data-i18n="`+key+`"`) {
			t.Errorf("no element carries the key %q", key)
		}

		if !strings.Contains(markup, `'`+key+`':`) {
			t.Errorf("the German dictionary has no entry for %q", key)
		}
	}

	// And the messages it writes while working, which are not in the markup.
	for _, key := range []string{"msg.testing", "msg.works", "msg.saving", "msg.saved"} {
		if !strings.Contains(markup, `t('`+key+`'`) {
			t.Errorf("the message %q is not looked up", key)
		}
	}

	// English stays in the markup as the fallback, so a key nobody translated
	// still renders something.
	if !strings.Contains(markup, ">Set up Time Recording</h1>") {
		t.Error("the English original is gone from the markup, so there is no fallback")
	}
}
