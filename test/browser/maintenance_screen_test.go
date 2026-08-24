//go:build browser

package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Maintenance switched on under somebody who is already working.
//
// The middleware refuses every request an ordinary account makes for as long as
// maintenance lasts, which is the point. What that left on the screen was not:
// the whole interface still standing, every card an error, every click another
// one - an application insisting somebody was working while nothing worked.
//
// It hands the screen back now, with the notice on it, which is the same thing
// an expired session does and for the same reason.
//
// Switched on through the database rather than through the screen, because this
// browser is signed in as the person it happens to. A second browser signed in
// as an administrator would be the honest way and costs a second Chrome per
// run; the settings row is the same row the screen writes, and the middleware
// reads it within its two-second cache either way.
func TestMaintenanceHandsBackTheScreenOfSomebodyAlreadySignedIn(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyWorker()

	// Working, before. Without this the assertion below could pass on a screen
	// that never came up at all.
	if !p.visible("#tabs") {
		t.Fatal("the interface is not up, so there is nothing to be taken away")
	}

	switchMaintenanceOn(t, p, "Restoring a backup")

	// The pollers ask every few seconds, so this is the ordinary path rather
	// than a click: the screen goes back on its own.
	p.waitShown("#login-screen")

	if !p.visible("#form-login") {
		t.Fatalf("the screen never went back to the sign-in form; it still shows:\n%s",
			truncateText(p.text("body"), 300))
	}

	// And it says why, in the administrator's own words. A sign-in form with no
	// explanation is indistinguishable from having been signed out for any other
	// reason - which is exactly the confusion this is here to end.
	p.waitForText("#maintenance-banner", "Restoring a backup")

	// The session is gone on the server too, not only forgotten here. Asked by
	// signing in again, which an installation in maintenance refuses for this
	// account - a session left standing would have let the screen come back by
	// itself the moment maintenance ended.
	p.signIn(workerEmail, workerPassword)

	// Given long enough to have got in if it were going to. Asked of the sign-in
	// screen rather than of the tab bar: the tabs are still in the page behind
	// the screen that covers them, so "the tabs are visible" is true either way
	// and answers a different question than the one being asked.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !p.visible("#login-screen") {
			t.Fatal("signing in during maintenance let an ordinary account back in")
		}

		time.Sleep(250 * time.Millisecond)
	}

	// And the refusal said why, rather than reading as a wrong password.
	if said := p.text("#login-screen"); !strings.Contains(said, "Restoring a backup") {
		t.Errorf("the sign-in screen does not say the installation is out of "+
			"service; it says: %q", truncateText(said, 200))
	}
}

// switchMaintenanceOn turns the installation out of service from outside this
// browser, as an administrator.
//
// Over HTTP rather than by writing the settings row directly, which is what the
// first version of this did. The row ends up the same either way - and writing
// it directly tells nobody. Every open screen learns about maintenance because
// SaveMaintenance announces it down the stream the browser already holds, so a
// case that skips the handler is a case about a feature with its point removed.
// This one failed exactly that way for as long as it took to notice.
//
// A second HTTP client rather than a second browser: this browser is signed in
// as the person it happens to, which is the whole case.
func switchMaintenanceOn(t *testing.T, p *page, message string) {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cannot hold the administrator's cookies: %v", err)
	}

	admin := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	base := p.app.BaseURL()

	// The visit a browser makes before anything else, which is where the CSRF
	// token comes from. Without it even signing in is refused - the token is
	// issued to whoever asked for the page, and this client had asked for
	// nothing.
	visit, err := admin.Get(base)
	if err != nil {
		t.Fatalf("cannot open the page as an administrator would: %v", err)
	}

	_ = visit.Body.Close()

	asAdmin(t, admin, base, http.MethodPost, "/auth/login", map[string]any{
		"email": harness.AdminEmail, "password": adminPassword,
	})

	asAdmin(t, admin, base, http.MethodPut, "/settings/maintenance", map[string]any{
		"enabled": true, "message": message,
	})
}

// asAdmin sends one request, with the two headers this API insists on.
func asAdmin(t *testing.T, client *http.Client, base, method, path string, body any) {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("cannot encode the request: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), method,
		base+"/api/v1"+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("cannot build the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Same origin, because this API refuses a cross-origin write - and a request
	// with no Origin at all is what that looks like to it.
	req.Header.Set("Origin", base)

	// And the token that came back with the session, which the jar is holding.
	if parsed, err := url.Parse(base); err == nil {
		for _, cookie := range client.Jar.Cookies(parsed) {
			if cookie.Name == "gtr_csrf" {
				req.Header.Set("X-CSRF-Token", cookie.Value)
			}
		}
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	defer func() { _ = res.Body.Close() }()

	answer, _ := io.ReadAll(res.Body)

	if res.StatusCode >= http.StatusBadRequest {
		t.Fatalf("%s %s: got %d: %s", method, path, res.StatusCode, answer)
	}
}

// The connection card answers its own first line, even once somebody has
// touched it.
//
// The card says "currently connected via postgres" and then shows what that
// connection is - as placeholders, because on an installation configured
// through the environment those values are not the form's to save.
//
// All of that was skipped as one block whenever the form counted as being
// filled in, which is right for the values somebody typed and wrong for
// everything else: one touch and the card went on naming a connection above
// five empty boxes, with no note saying where it came from. A restored draft
// did it with nobody touching anything at all.
func TestTheConnectionCardKeepsSayingWhatIsRunningWhileBeingEdited(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	p.waitForFilled("#datasource-active")

	// The note is the plainest evidence that the card knows where its connection
	// came from, and it is shown only on an installation configured through the
	// environment - which the harness starts every instance as.
	if !p.visible("#datasource-source") {
		t.Fatalf("the card does not say where the connection came from; it shows %q",
			truncateText(p.text("#form-datasource"), 200))
	}

	before := p.placeholder(`#form-datasource [name="name"]`)

	if before == "" {
		t.Fatal("the database name is not offered as a placeholder, so the card " +
			"shows nothing of the connection it just named")
	}

	// Somebody starts typing. From here on the values are theirs.
	//
	// The name rather than the host: the host belongs to the server dialects,
	// and the harness runs on SQLite, where that field is not on screen at all.
	// The name is the one field every connection has.
	p.run("start typing",
		chromedp.SendKeys(`#form-datasource [name="name"]`, "somewhere-else.db",
			chromedp.ByQuery))

	// And something reloads the screen, which happens for reasons having nothing
	// to do with them. Choosing a language is the one that was reported.
	p.chooseLanguage("de")
	p.waitForText(`.tab[data-view="timesheets"]`, "Zeiteinträge")

	if got := p.value(`#form-datasource [name="name"]`); !strings.Contains(got, "somewhere-else.db") {
		t.Errorf("what was being typed was taken away: the name reads %q", got)
	}

	if !p.visible("#datasource-source") {
		t.Error("the card stopped saying where its connection came from as soon as " +
			"somebody touched the form")
	}

	if after := p.placeholder(`#form-datasource [name="name"]`); after != before {
		t.Errorf("the card stopped showing the running connection: the database "+
			"name offered %q before and %q after", before, after)
	}
}

// The traces are named at an address that exists from where the screen is read.
//
// It used to be http://127.0.0.1:16686, which is right on exactly one machine -
// the server. This screen is read from a browser, which is by definition
// somewhere else, so the sentence named the reader's own machine, where nothing
// is listening.
func TestTheTracingHintNamesThisInstallation(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	p.waitForFilled("#tel-tracing-hint")

	hint := p.text("#tel-tracing-hint")

	var host string

	p.run("read the host", chromedp.Evaluate(`window.location.hostname`, &host))

	if host == "" {
		t.Fatal("the page has no hostname, so the case cannot ask its question")
	}

	if !strings.Contains(hint, host+":16686") {
		t.Errorf("the hint does not name this installation's own address (%s:16686); "+
			"it says: %q", host, hint)
	}

	// And the tunnel it offers names the same host rather than a placeholder,
	// because the shipped overlay binds the trace browser to the server's
	// loopback and the command is the way in.
	if !strings.Contains(hint, "ssh -L 16686:127.0.0.1:16686 "+host) {
		t.Errorf("the hint does not say how to reach a loopback-bound trace "+
			"browser on this host; it says: %q", hint)
	}
}
