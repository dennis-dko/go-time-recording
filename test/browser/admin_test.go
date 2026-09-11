//go:build browser

package browser

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The directory synchronisation is not offered to an administrator who may not
// perform one.
//
// Running it deletes every account the directory no longer holds along with
// everything those people recorded, so it belongs to the built-in administrator
// alone - but the Settings tab is opened by anybody holding settings:manage, and
// the card sat in the middle of it with two buttons the server refuses. A
// data-perm cannot express this: it names a permission, and this is about which
// account it is, so only a browser can answer whether the card is there.
func TestAGrantedAdministratorIsNotOfferedTheDirectoryRun(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createAccount(t, "bothe@example.com", "both-jobs-password-1", "user-admin")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("bothe@example.com", "both-jobs-password-1")
	p.waitGone("#login-screen")
	p.settleWelcome()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-ldap", chromedp.ByID))

	// Waited for the screen to be filled, not for it to exist. Every card here is
	// in index.html, so the wait above returns before a single request has
	// answered - and the card this case is about starts on screen and is taken
	// away once the directory settings arrive. Reading before that finds it
	// present and reports a right that was never granted.
	//
	// This line is the last thing the screen writes, and it is never empty: it
	// says either when the synchronisation is scheduled or that it runs only when
	// the button is pressed.
	p.waitForFilled("#sync-schedule-active")

	if p.visible("#sync-card") {
		t.Error("an administrator who may not synchronise the directory is offered " +
			"the buttons that do it")
	}

	// The connection card above it is theirs to edit, so this is the one card
	// missing rather than the screen having failed to load.
	if !p.visible("#form-ldap") {
		t.Error("the directory connection is missing too, so the screen did not load")
	}
}

// The Settings screen loads its cards in one unbroken chain of awaits, so a card
// whose request fails does not fail alone: every card after it stays blank while
// the API answers perfectly and nothing else notices. The metrics and tracing
// card went into the middle of that chain, which makes the card after it as much
// the point of this test as the new one is.
func TestTheSettingsScreenFillsTheTelemetryCardAndTheOnesAfterIt(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-telemetry", chromedp.ByID))

	// Waited for the card to be filled, not for the card to exist.
	//
	// Every one of these forms is in index.html, so the wait above returns the
	// moment the screen is shown - before a single request has answered. Reading
	// straight after it therefore asks "is the chain finished" of a chain that
	// has not started, which is a question with a different answer on a busy
	// machine than on a quiet one. It passed here and failed on CI, saying the
	// screen was broken when it was merely still loading.
	//
	// The LDAP filter is the last of the three read below, so waiting for it
	// waits for all of them - and a chain that really did break never fills it,
	// which is what this case is about.
	p.waitForValue(`#form-ldap input[name="userFilter"]`)

	// This line is written by the same response that fills the fields, so an
	// empty one means the card never loaded at all.
	active := p.text("#telemetry-active")
	if active == "" {
		t.Error("the telemetry card says nothing about what this process is doing")
	}

	// The metrics endpoint is the one thing on this screen somebody wants to
	// copy, so it has to be there in full rather than as a port number to
	// assemble by hand.
	if !strings.Contains(active, "/metrics") {
		t.Errorf("the metrics endpoint is not named in %q", active)
	}

	// The card after the new one in the chain. Its user filter has a default, so
	// an empty field here means the telemetry request threw and took the rest of
	// the screen down with it.
	if filter := p.value(`#form-ldap input[name="userFilter"]`); filter == "" {
		t.Error("the LDAP card is empty, so loading the cards stopped before it")
	}
}

// And the screen behind that button does not open for somebody who may not have
// it.
//
// The tabs are hidden by data-perm, and most ways to a screen were already
// covered by that: the address bar and the remembered screen go through
// startingView, which only accepts a view whose tab is visible, and the tour
// drops steps whose permission the reader lacks. Checked, rather than assumed -
// the first version of this case asserted the address bar was a way in, and it
// was not.
//
// The banner's button was the one that was not covered. It calls switchView
// directly, so hiding the tab did nothing about it.
//
// The data was never at risk: every request behind that screen is refused by the
// server on its own. What was at risk is the sentence this is meant to be
// describable in - somebody who may not administer this installation should not
// be looking at the screen that administers it.
func TestAnOrdinaryAccountCannotOpenTheAdministrationScreen(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.becomeWorker()

	// The call the banner's button makes, which is the way in that was open.
	p.run("ask for it the way the banner does", chromedp.Evaluate(
		`(() => { switchView('admin'); return 1; })()`, nil))

	if p.visible("#view-admin") {
		t.Error("the administration screen opened for an account without " +
			"settings:manage, through switchView")
	}
}

// An installation configured through the environment sees what it is connected
// to, and is told that saving the form would take over from that.
//
// The card is filled from the file the installer or the card itself writes. A
// deployment that sets DB_* has no such file, so every field was blank - under a
// first line reading "currently connected via sqlite". It looked unconfigured on
// an installation that plainly was not.
//
// The reason it matters past appearances: that file wins over the environment.
// Somebody filling in the blank form would silently override the deployment at
// the next start.
//
// The harness starts its instances from the environment, so this is that case.
func TestTheDatabaseCardSaysWhereAConnectionWithoutASavedSettingComesFrom(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))

	// The form is in index.html and therefore on screen before any answer has
	// arrived; the answer is what fills it.
	p.waitForFilled("#datasource-active")
	p.waitShown("#datasource-source")

	// The database name, because this instance runs on SQLite: the server fields
	// are put away for a file, and the file's name is the whole connection.
	if got := p.attr(`#form-datasource [name="name"]`, "placeholder"); got == "" {
		t.Error("the database field offers no placeholder, so the card still shows " +
			"nothing of the connection its first line says is in force")
	}

	// And it is a placeholder, not a value: leaving the field alone has to keep
	// meaning "leave the connection alone", or the next save writes a file that
	// overrides the environment.
	var value string

	p.run("read the stored value", chromedp.Evaluate(
		`document.querySelector('#form-datasource [name="name"]').value`, &value))

	if value != "" {
		t.Errorf("the database field holds %q on an installation that has stored "+
			"nothing; the running connection is being presented as saved", value)
	}
}

// The connection card names what is running and invents nothing.
//
// Reported from a container installation. The type stood empty while the boxes
// beneath it showed placeholders for a connection the card would not name - and
// the port was not a placeholder at all but a real 3306, because nothing chosen
// was read as "a server, and not PostgreSQL". A real value in one box and
// placeholders in the rest.
//
// Reloading made it worse rather than better: there is no empty option to go
// back to, so the browser restored the form onto the first one and the card came
// up claiming SQLite.
func TestTheConnectionCardNamesWhatIsRunningAndInventsNothing(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")
	p.settled()

	var state struct {
		Active string `json:"active"`
		Stored bool   `json:"stored"`
	}

	// What the server says this process opened, which is what the card has to
	// agree with. Read with the answer awaited, which evalJSON does not do.
	var raw string

	p.run("ask what is running", chromedp.Evaluate(`
		(async () => {
			const r = await fetch('/api/v1/settings/datasource', { credentials: 'same-origin' });
			const body = await r.json();

			return JSON.stringify({
				active: body?.data?.active ?? '',
				stored: body?.data?.stored ?? false,
			});
		})()`, &raw, awaitPromise))

	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("reading what is running: %v; %s", err, raw)
	}

	if state.Stored {
		t.Fatal("this instance has a stored connection, so it is not the case that " +
			"was reported")
	}

	shown := func() string { return p.value(`#form-datasource select[name="dialect"]`) }

	if got := shown(); got != state.Active {
		t.Errorf("the card shows the type %q while the process is connected via %q",
			got, state.Active)
	}

	// And a reload keeps it, along with the placeholders it belongs to.
	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.waitGone("#login-screen")
	p.settleWizard()
	p.settled()

	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")

	if got := shown(); got != state.Active {
		t.Errorf("the card shows the type %q after a reload; the process is "+
			"connected via %q", got, state.Active)
	}

	if got := p.attr(`#form-datasource [name="name"]`, "placeholder"); got == "" {
		t.Error("the placeholders are gone after a reload, so the card is blank " +
			"again under a line saying what it is connected to")
	}

	// And nothing chosen is not a server: no invented port, no server fields.
	var empty string

	p.run("choose nothing at all", chromedp.Evaluate(`
		(() => {
			const form = document.querySelector('#form-datasource');

			form.elements.port.value = '';
			form.elements.dialect.value = '';
			form.elements.dialect.dispatchEvent(new Event('change', { bubbles: true }));

			return JSON.stringify({
				port: form.elements.port.value,
				serverFieldsHidden: document.querySelector('#ds-server-fields').hidden,
			});
		})()`, &empty))

	if !strings.Contains(empty, `"port":""`) {
		t.Errorf("a card with no type chosen filled the port in by itself: %s", empty)
	}

	if !strings.Contains(empty, `"serverFieldsHidden":true`) {
		t.Errorf("a card with no type chosen offers the fields of a server: %s", empty)
	}
}

// The version card can be asked to look again.
//
// The card is drawn from an answer the server keeps for six hours, so somebody
// who has just published a release is told the version before it and, until this
// button existed, had no way to say "look again" short of restarting the
// application.
//
// Driven through the interface rather than through the endpoint, because what
// was missing was a button: the endpoint answering correctly and the card having
// nothing to press are the same failure to the person reading the screen.
func TestTheVersionCardCanBeAskedToLookAgain(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#update-card", chromedp.ByID))

	if !p.visible("#update-check") {
		t.Fatal("the version card offers no way to ask the release feed again")
	}

	// What it said before, so the press can be seen to have done something even
	// when the answer is the same one.
	before := strings.TrimSpace(p.text("#update-state"))

	// What the browser had already objected to before the press. A page loaded
	// before anybody signed in reports the 401 from /me, which is the interface
	// asking whether there is a session and being told there is not - counting it
	// against the button would make this case fail for something it did not do.
	quietBefore := len(p.complaints())

	// What it measures before anything is pressed.
	wasWide := p.pixels("#update-check", "offsetWidth")

	p.run("ask again", p.click("#update-check"))

	// The button comes back rather than staying disabled: it says "Looking …"
	// while the request is out, and a button left in that state is a card that
	// can only be asked once.
	deadline := time.Now().Add(waitPatience)
	for time.Now().Before(deadline) {
		if !p.disabled("#update-check") {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	if p.disabled("#update-check") {
		t.Error("the button never came back, so the card can be asked exactly once")
	}

	if after := strings.TrimSpace(p.text("#update-state")); after == "" {
		t.Errorf("the card lost its sentence after asking again; it said %q before", before)
	}

	// The button is the width it was. While it is working it says something else,
	// and the two labels are not the same width in any language - so swapping
	// them resized the button, moved the row and shifted the card, twice. The
	// width is held for the duration and released afterwards, which is what these
	// two assertions are: the same width, and nothing left pinning it.
	if width := p.pixels("#update-check", "offsetWidth"); width != wasWide {
		t.Errorf("the button is %dpx after asking and was %dpx before", width, wasWide)
	}

	if pinned := p.styleProperty("#update-check", "minWidth"); pinned != "" {
		t.Errorf("the button is still pinned to %q, so it can never be narrower again", pinned)
	}

	// And the press itself was quiet. The card is redrawn from the answer that
	// comes back, which is the part most likely to go wrong without showing.
	if raised := p.complaints()[quietBefore:]; len(raised) > 0 {
		t.Errorf("asking again made the browser complain %d time(s):\n%s",
			len(raised), strings.Join(raised, "\n"))
	}
}

// The directory's role picker has to offer the roles.
//
// It was empty, and the way there is ordinary: start filling in the directory
// card, reload the page for any of the reasons a page gets reloaded, and the
// draft comes back with the form marked as being edited. Every later refill is
// skipped to protect what was typed - including the call that puts the roles
// into the picker, which is not something anybody typed. The card came back with
// a chooser that offered nothing to choose.
func TestTheDirectoryRolePickerOffersTheRolesAfterADraftComesBack(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-ldap", chromedp.ByID))

	if p.count("#form-ldap select[name=defaultRole] option") == 0 {
		t.Fatal("the role picker is empty before anybody has even touched the form")
	}

	// Somebody starts configuring the directory, which is what marks the form.
	p.run("start filling it in",
		chromedp.SendKeys(`#form-ldap input[name="host"]`, "ldap.example.invalid",
			chromedp.ByQuery))

	// And the page is reloaded, for whatever reason pages get reloaded.
	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#who", chromedp.ByID))
	p.settled()

	p.run("open Settings again", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-ldap", chromedp.ByID))

	// The draft is back - that part is deliberate and stays.
	if host := p.value(`#form-ldap input[name="host"]`); host != "ldap.example.invalid" {
		t.Errorf("the draft was not restored, so this case is no longer about what it says: %q", host)
	}

	if n := p.count("#form-ldap select[name=defaultRole] option"); n == 0 {
		t.Error("the role picker came back empty, so no default role can be chosen for " +
			"accounts the directory creates")
	}
}

// The directory's role picker offers the roles and starts on the ordinary one.
//
// Reported twice as still empty after two attempts at it, so this asserts the
// requirement itself rather than one theory of what breaks it: there are options
// to choose from, nothing empty is selected, and what is selected before anybody
// touches it is the ordinary user role.
func TestTheDirectoryRolePickerIsNeverEmptyAndDefaultsToTheOrdinaryRole(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-ldap", chromedp.ByID))

	if n := p.count("#form-ldap select[name=defaultRole] option"); n == 0 {
		t.Fatal("the role picker offers nothing to choose")
	}

	// Nothing empty to choose, so the card cannot store an empty role again.
	if empty := p.count(`#form-ldap select[name=defaultRole] option[value=""]`); empty != 0 {
		t.Errorf("the picker offers %d empty option(s), which is what got stored last time", empty)
	}

	value := p.value("#form-ldap select[name=defaultRole]")
	if value == "" {
		t.Fatal("the picker has options but none of them is selected, which is what the " +
			"empty box on screen actually is")
	}

	if value != "user" {
		t.Errorf("a directory nobody has configured should provision ordinary users, "+
			"the picker starts on %q", value)
	}

	// And the selected option says something a person can read, rather than a
	// bare identifier.
	if label := strings.TrimSpace(p.text("#form-ldap select[name=defaultRole] option[value=user]")); label == "" {
		t.Error("the chosen role has no label, so the box looks empty even when it is not")
	}
}

// An account created here can be corrected here.
//
// A name typed with a typo, or somebody whose address changed, had one way out:
// delete the account - and every hour recorded in it - and make it again. The API
// has taken the change all along; nothing on the screen ever asked for it.
//
// Not every row, and the two exceptions are the ones that go wrong quietly. The
// built-in administrator is the way back into an installation, and the row
// somebody is reading their own name in is theirs - neither is somebody else's
// account to correct from the screen that administers accounts.
func TestAnAdminCorrectsALocalAccountFromTheTable(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("add somebody", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Wilma", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "wilma@example.com",
			chromedp.ByQuery),
		p.chooseOption(`#form-user select[name="role"]`, "user"),
		chromedp.SendKeys(`#form-user input[name="password"]`, "wilma-password-1",
			chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`))

	p.waitForText("#table-users tbody", "wilma@example.com")

	// Two rows, one of them the built-in administrator - which is also the row
	// whoever is signed in is reading their own name in. So exactly one account
	// here is somebody else's to correct.
	if got := p.count(`#table-users tbody button[data-action="edit"]`); got != 1 {
		t.Fatalf("the table offers %d accounts for editing, want 1 - the built-in "+
			"administrator is the way back in and must not be one of them", got)
	}

	p.run("open Wilma", p.click(`#table-users tbody button[data-action="edit"]`),
		chromedp.WaitVisible("#user-cancel", chromedp.ByID))

	if title := p.text("#user-form-title"); !strings.Contains(strings.ToLower(title), "edit") {
		t.Errorf("the form still says %q rather than that it is editing somebody", title)
	}

	if got := p.value(`#form-user input[name="name"]`); got != "Wilma" {
		t.Errorf("the form opened on %q rather than on the account that was picked", got)
	}

	// The role is not on this form while it is editing: the row has a control for
	// it already, and two places to answer one question is how they end up
	// disagreeing. Nor is a password - nobody sets somebody else's from here.
	if p.visible("#user-role-field") || p.visible("#user-password-field") {
		t.Error("editing an account offers the role and the password as well, which " +
			"are answered elsewhere")
	}

	p.run("correct the name",
		chromedp.SetValue(`#form-user input[name="name"]`, "Wilma Feuerstein",
			chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`))

	p.waitForText("#table-users tbody", "Wilma Feuerstein")

	if got := p.text("#table-users tbody"); strings.Contains(got, "wilma@example.com") == false {
		t.Errorf("the address went missing over the correction: %q", got)
	}

	// And the form is back to adding somebody, rather than sitting on the account
	// that was just saved - which is how the next new account gets written over an
	// existing one.
	if p.visible("#user-cancel") {
		t.Error("the form stayed in editing after saving")
	}

	if p.value(`#form-user input[name="name"]`) != "" {
		t.Error("the form kept the account it was editing after saving it")
	}

	if !p.visible("#user-role-field") || !p.visible("#user-password-field") {
		t.Error("the fields a new account needs did not come back")
	}
}

// A directory account is not offered for correction.
//
// Its name and address are copied from the entry on every synchronisation, so a
// change made here holds until the next run and then reverts - which looks
// exactly like it worked. The server refuses it for that reason, and a button
// that asks for a refusal teaches somebody the screen is broken.
//
// The account is supplied rather than synchronised: what is under test is what
// the table does with one, and standing up a directory to get one would be a
// different case about a different thing.
func TestADirectoryAccountIsNotOfferedForCorrection(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Users", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#table-users", chromedp.ByID))

	p.run("answer with one account, kept in a directory", chromedp.Evaluate(`
		(async () => {
			const server = api;
			api = (path, options) => path === '/users'
				? Promise.resolve({ items: [{
					id: 4711, name: 'Sven', email: 'sven@example.com', role: 'user',
					isSystem: false, isExternal: true,
					dailyTargetHours: 0, maxDailyHours: 0,
				}] })
				: server(path, options);

			await loadUsers();

			return 1;
		})()`, nil, awaitPromise))

	p.waitForText("#table-users tbody", "sven@example.com")

	for _, action := range []string{"edit", "reset-password"} {
		if got := p.count(`#table-users tbody button[data-action="` + action + `"]`); got != 0 {
			t.Errorf("a directory account is offered %q (%d buttons), which the server "+
				"refuses and the next synchronisation would undo", action, got)
		}
	}
}

// Letting somebody back in, from the table, without learning what they had.
//
// The administrator chooses the password rather than the application handing out
// the documented one, because the must-change flag does not stop signing in - it
// cannot, or nobody could ever get out of it - so a well-known password would
// leave a window in which anybody who knows the address takes the account over.
//
// And it can be generated, because a password a person invents under mild
// pressure is a password like the last one they invented. The field takes either.
func TestAnAdministratorResetsAPasswordFromTheTable(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("add somebody", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#form-user", chromedp.ByID),
		chromedp.SendKeys(`#form-user input[name="name"]`, "Wilma", chromedp.ByQuery),
		chromedp.SendKeys(`#form-user input[name="email"]`, "wilma@example.com",
			chromedp.ByQuery),
		p.chooseOption(`#form-user select[name="role"]`, "user"),
		chromedp.SendKeys(`#form-user input[name="password"]`, "wilma-password-1",
			chromedp.ByQuery),
		p.click(`#form-user button[type="submit"]`))

	p.waitForText("#table-users tbody", "wilma@example.com")

	// One row offers it. Not the built-in administrator's, which is also the row
	// whoever is signed in is reading their own name in - and their own password
	// is changed under My account, which asks for the one they have.
	offered := p.count(`#table-users tbody button[data-action="reset-password"]`)

	if offered != 1 {
		t.Fatalf("%d rows offer a password reset, want 1", offered)
	}

	p.run("open it", p.click(`#table-users tbody button[data-action="reset-password"]`),
		chromedp.WaitVisible("#reset-password-field", chromedp.ByID))

	// The question names the person, because an administrator with a table of
	// accounts open is one mis-click from resetting the wrong one.
	if asked := p.text(".confirm-card"); !strings.Contains(asked, "Wilma") {
		t.Errorf("the question does not say whose password it is about: %q", asked)
	}

	// Hidden to begin with, and readable through the control every other password
	// field on this screen already has - rather than a second one built here.
	if got := p.attr("#reset-password-field", "type"); got != "password" {
		t.Errorf("the field starts as %q, so the password is on screen before "+
			"anybody asked for it", got)
	}

	if p.count(".confirm-card .password-toggle") != 1 {
		t.Error("the field has no reveal button, or has grown a second one")
	}

	var generated string

	p.run("let it choose one", p.click("#reset-password-generate"),
		chromedp.Value("#reset-password-field", &generated, chromedp.ByID))

	if len(generated) < 8 {
		t.Fatalf("the generated password is %q, which the server would refuse",
			generated)
	}

	// Revealed by generating it. One that has to be read out or written down and
	// cannot be seen is worse than none.
	if got := p.attr("#reset-password-field", "type"); got != "text" {
		t.Errorf("the generated password stays hidden (type %q), so nobody can pass "+
			"it on", got)
	}

	p.run("reset it", p.click(".confirm-card button.confirm-proceed"))

	p.waitForText("#toast", "assword reset")

	// And it works: the account signs in with what was generated, and is made to
	// choose its own before it can do anything.
	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))

	p.signIn("wilma@example.com", generated)
	p.waitGone("#login-screen")

	if !p.visible("#password-banner") {
		t.Error("the account signed in on the reset password and was not asked to " +
			"replace it")
	}
}

// Your own row says so, and says it before it says anything else.
//
// The built-in administrator's row already said "system account", and the row
// somebody reads their own name in said nothing at all - which is the one fact
// that matters to the person looking at the table, because it is the row whose
// delete button is not there.
func TestYourOwnRowIsMarkedAsYours(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()
	p.createOrdinaryAccount(t, "sven@example.com", "sven-password-1")

	// Reloaded, because the account above was created through the API rather than
	// through the form: the table is filled when the interface loads and refreshed
	// by what the form does afterwards.
	p.run("reload", chromedp.Reload(), chromedp.WaitVisible("#tabs", chromedp.ByID))
	p.settleWelcome()

	p.run("open the accounts", p.click(`.tab[data-view="users"]`),
		chromedp.WaitVisible("#table-users", chromedp.ByID))

	p.waitForText("#table-users tbody", "sven@example.com")

	var own struct {
		Label     string `json:"label"`
		HasDelete bool   `json:"hasDelete"`
	}

	p.evalJSON(`JSON.stringify((() => {
		const rows = [...document.querySelectorAll('#table-users tbody tr')];
		const mine = rows.find(row => row.textContent.includes('admin@local'));
		if (!mine) return { label: 'no row for the signed-in account', hasDelete: false };

		return {
			label: mine.querySelector('td.actions span.muted')?.textContent ?? '',
			hasDelete: Boolean(mine.querySelector('td.actions button.danger')),
		};
	})())`, &own)

	if own.Label != "Your account" {
		t.Errorf("the signed-in account's row is marked %q, want \"Your account\"",
			own.Label)
	}

	if own.HasDelete {
		t.Error("the row for the account doing the looking offers a delete, which " +
			"would end the session it was pressed from")
	}
}
