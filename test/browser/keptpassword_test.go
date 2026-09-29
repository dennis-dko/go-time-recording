//go:build browser

package browser

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"
)

// The database card shows its password as a filled, masked box, and the page
// never holds the password.
//
// The card shows the connection in force as values - a stored one, or the one
// a compose deployment set - and an empty password box beside them read as "no
// password". The password itself is never sent to the page: it would be one
// click on the eye away from whoever sits at the screen, and a way for an
// administrator, who by design reads nobody's hours, to open all of them in the
// database. So the box holds a stand-in, which is never sent: saving or testing
// the card as it stands sends no password, and the server keeps the one in
// force - for the same server and user only, which is why changing either of
// those empties the box.
//
// The server is played by the page's own api function with a PostgreSQL
// connection from the environment, because this suite runs on SQLite; what the
// server does with an untouched box is TestAnUntouchedPasswordKeepsTheOneTheEnvironmentGave.
func TestTheDatabasePasswordIsShownFilledAndNeverHeld(t *testing.T) {
	t.Parallel()

	p := open(t)
	p.readyAdmin()

	p.run("open Settings", p.click(`.tab[data-view="admin"]`),
		chromedp.WaitVisible("#form-datasource", chromedp.ByID))
	p.waitForFilled("#datasource-active")
	p.settled()

	p.run("play a compose deployment on PostgreSQL", chromedp.Evaluate(`(async () => {
		const server = api;

		window.sent = [];

		api = (path, options) => {
			if (path === '/settings/datasource' && (!options || !options.method)) {
				return Promise.resolve({
					dialect: '', name: '', host: '', port: '', user: '', sslMode: '',
					hasPassword: false, stored: false, active: 'postgres',
					running: { dialect: 'postgres', name: 'gtr', host: 'db', port: '5432',
						user: 'gtr', sslMode: 'disable' },
					runningHasPassword: true,
				});
			}

			if (path.startsWith('/settings/datasource')) {
				window.sent.push(JSON.parse(options.body));

				return Promise.resolve(path.endsWith('/test') ? { ok: true } : { status: 'saved' });
			}

			return server(path, options);
		};

		await loadAdmin();

		return true;
	})()`, nil, awaitPromise))

	var shown struct {
		Fields   map[string]string `json:"fields"`
		Type     string            `json:"type"`
		Filled   bool              `json:"filled"`
		EyeShut  bool              `json:"eyeShut"`
		Password string            `json:"password"`
	}

	read := func(step string) {
		var raw string

		p.run(step, chromedp.Evaluate(`(() => {
			const form = document.querySelector('#form-datasource');
			const box = form.elements.password;
			const eye = box.closest('.password-field')?.querySelector('button');

			return JSON.stringify({
				fields: Object.fromEntries(['dialect', 'name', 'host', 'port', 'user', 'sslMode']
					.map((name) => [name, form.elements[name].value])),
				type: box.type,
				filled: box.value !== '',
				eyeShut: !!eye && eye.disabled,
				password: box.value,
			});
		})()`, &raw))

		if err := json.Unmarshal([]byte(raw), &shown); err != nil {
			t.Fatalf("reading the card: %v; %s", err, raw)
		}
	}

	read("read the card")

	want := map[string]string{
		"dialect": "postgres", "name": "gtr", "host": "db", "port": "5432",
		"user": "gtr", "sslMode": "disable",
	}

	for name, value := range want {
		if shown.Fields[name] != value {
			t.Errorf("the %s field holds %q, want the running connection's %q",
				name, shown.Fields[name], value)
		}
	}

	if !shown.Filled || shown.Type != "password" {
		t.Errorf("the password box is filled=%v and of type %q, want a filled, masked box",
			shown.Filled, shown.Type)
	}

	if !shown.EyeShut {
		t.Error("the eye beside a password the page does not hold can still be pressed")
	}

	// Saved and tested as it stands, it sends no password.
	p.run("test and save as it stands", p.click("#datasource-test"),
		chromedp.Poll(`window.sent.length > 0`, nil),
		p.click(`#form-datasource button[type="submit"]`),
		chromedp.Poll(`window.sent.length > 1`, nil))

	var sent string

	p.run("what was sent", chromedp.Evaluate(`JSON.stringify(window.sent)`, &sent))

	var bodies []map[string]any

	if err := json.Unmarshal([]byte(sent), &bodies); err != nil {
		t.Fatalf("reading what was sent: %v; %s", err, sent)
	}

	for i, body := range bodies {
		if password, _ := body["password"].(string); password != "" {
			t.Errorf("request %d sent the password %q for a box nobody typed in", i+1, password)
		}
	}

	// Another server is not the one the kept password belongs to.
	p.run("name another host", chromedp.Evaluate(`(() => {
		const host = document.querySelector('#form-datasource').elements.host;

		host.value = 'elsewhere';
		host.dispatchEvent(new Event('input', { bubbles: true }));

		return true;
	})()`, nil))

	read("read the card again")

	if shown.Filled {
		t.Errorf("the password box still shows a password (%q) for a server it does not belong to",
			shown.Password)
	}
}
