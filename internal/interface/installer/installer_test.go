package installer

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// The wait for the application ends, whoever is answering.
//
// The two-minute limit stood behind the return an installer still answering
// takes. So when the process went away after a save and came back as an
// installer - the connection file lost with the container it was written in -
// the page waited for as long as the tab stayed open, under a line saying the
// application was starting. Read from the page rather than driven: two minutes
// of waiting is what the case would otherwise be.
func TestTheWaitForTheApplicationEndsWhoeverAnswers(t *testing.T) {
	page, err := assets.ReadFile("assets/install.html")
	if err != nil {
		t.Fatalf("reading the installer page: %v", err)
	}

	markup := string(page)

	start := strings.Index(markup, "function waitForTheApplication()")
	if start < 0 {
		t.Fatal("the installer no longer waits for the application at all")
	}

	loop := markup[start:]

	limit := strings.Index(loop, "attempts > 120")
	asks := strings.Index(loop, "await whoAnswers()")

	if limit < 0 || asks < 0 {
		t.Fatalf("the wait loop no longer has a limit (%d) or no longer asks who "+
			"answers (%d); this case is reading nothing", limit, asks)
	}

	if limit > asks {
		t.Error("the limit stands behind the question of who answers, so an installer " +
			"that goes on answering is waited on for ever")
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

// The page says nothing its dictionary cannot say in German.
//
// Two shapes, because both had happened on this page: a lookup whose key the
// dictionary does not hold, which renders the English and looks translated in
// the source; and a label written straight from a literal - the password
// button's name was "Show the password" on an otherwise German screen, for
// everybody who reads a screen by its accessible names.
func TestTheInstallerPageSaysNothingItCannotTranslate(t *testing.T) {
	raw, err := assets.ReadFile("assets/install.html")
	if err != nil {
		t.Fatalf("reading the installer page: %v", err)
	}

	page := string(raw)

	// A whole key, followed by its fallback. The two lookups built from a prefix
	// - a refusal's code, a field's name - are held by the case that knows which
	// codes and fields the server sends.
	looked := regexp.MustCompile(`\bt\('([a-zA-Z.]+)',`).FindAllStringSubmatch(page, -1)
	if len(looked) < 10 {
		t.Fatalf("found %d lookups in the page; this case is reading nothing", len(looked))
	}

	for _, lookup := range looked {
		if !strings.Contains(page, "'"+lookup[1]+"':") {
			t.Errorf("the page looks up %q and its German dictionary has no such entry", lookup[1])
		}
	}

	for _, bare := range regexp.MustCompile(
		`(?:\.title\s*=|setAttribute\('aria-label',|\.textContent\s*=|\.placeholder\s*=)\s*'[^']+'`).
		FindAllString(page, -1) {
		t.Errorf("the page writes a label from a literal, in English whatever the reader "+
			"asked for: %s", bare)
	}
}

// Once a connection has been saved, a second answer is refused rather than
// written.
//
// Two browser tabs answering at nearly the same moment - or a second answer in
// the moment before the listener closes - each passed the token check and the
// probe, each wrote the connection file and each was told it had worked, and only
// then did the handover pick the first. The process went on with the first
// connection and the file held the second, so the next start opened a different
// database: every account, project and hour from the first start was simply not
// there. The comment on the handover already said neither may be told it
// succeeded and the file must not be written twice; this holds it to that.
func TestASecondAnswerIsRefusedOnceAConnectionIsSaved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "datasource.json")

	s := &server{
		cfg:  Config{Token: "the-token", DatasourceFile: file, Logf: t.Logf},
		done: make(chan config.Datasource, 1),
	}

	answer := func(name string) int {
		body := fmt.Sprintf(`{"dialect":"sqlite","name":%q}`, filepath.ToSlash(filepath.Join(dir, name)))
		req := httptest.NewRequest(http.MethodPost, "/install/save", strings.NewReader(body))
		req.Header.Set("X-Setup-Token", "the-token")

		rec := httptest.NewRecorder()
		s.save(rec, req)

		return rec.Code
	}

	if got := answer("first"); got != http.StatusOK {
		t.Fatalf("the first answer was refused with %d", got)
	}

	if got := answer("second"); got != http.StatusConflict {
		t.Errorf("a second answer after the connection was saved got %d, want %d",
			got, http.StatusConflict)
	}

	saved, ok := config.LoadDatasource(file)
	if !ok || !strings.HasSuffix(filepath.ToSlash(saved.Name), "/first") {
		t.Errorf("the file holds %q, not the connection that was handed over", saved.Name)
	}

	if handed := <-s.done; !strings.HasSuffix(filepath.ToSlash(handed.Name), "/first") {
		t.Errorf("the connection handed over is %q", handed.Name)
	}
}

// The connection handed to the application is the one the next start reads.
//
// The application goes on in this process with what the installer hands over,
// and the next start reads the file - and the restart card compares the two to
// say whether a restart is waiting. Saving keeps no server fields for SQLite, so
// an answer that carried some - the page sends none, a request written by hand
// can - ran with a password the file did not have, and the card called a change
// of password pending from the first minute of an installation it would not
// change at all.
func TestTheConnectionHandedOverIsTheOneTheNextStartReads(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "datasource.json")

	s := &server{
		cfg:  Config{Token: "the-token", DatasourceFile: file, Logf: t.Logf},
		done: make(chan config.Datasource, 1),
	}

	body := fmt.Sprintf(`{"dialect":"sqlite","name":%q,"host":"db","port":"5432","user":"gtr",`+
		`"password":"left-over","sslMode":"disable"}`, filepath.ToSlash(filepath.Join(dir, "gtr")))
	req := httptest.NewRequest(http.MethodPost, "/install/save", strings.NewReader(body))
	req.Header.Set("X-Setup-Token", "the-token")

	rec := httptest.NewRecorder()
	s.save(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the answer was refused with %d: %s", rec.Code, rec.Body)
	}

	saved, ok := config.LoadDatasource(file)
	if !ok {
		t.Fatal("nothing was saved")
	}

	if handed := <-s.done; handed != saved {
		t.Errorf("the application goes on with %+v while the next start reads %+v", handed, saved)
	}
}
