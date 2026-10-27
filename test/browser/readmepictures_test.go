//go:build browser

package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
	"github.com/dennis-dko/go-time-recording/test/oidctest"
)

// readmePictures names the directory the README's pictures are written to.
const readmePictures = "GTR_README_PICTURES"

// defaultName is what cmd/configs/.env calls an installation, in place of the
// name the harness gives each instance to tell it from the others.
const defaultName = "APP_NAME=Time Recording"

// The README's pictures are taken from the application rather than drawn.
//
// A screenshot in a README is a claim about what the application looks like, and
// it goes stale the way a comment does: silently, while everything around it
// moves on. So none of them is edited by hand. This books a year of somebody's
// working time through the API the screens read - five projects, days away, the
// times each recent day began and ended, a stopwatch running - and photographs
// the screens that show it, so a changed screen is one command away from a new
// picture: task readme:pictures.
//
// It is a camera rather than a check, so it runs only when asked and skips
// everywhere else, CI included. What it would assert is asserted screen by
// screen by the cases beside it.
func TestTheReadmePicturesAreTakenFromTheApplication(t *testing.T) {
	into := os.Getenv(readmePictures)
	if into == "" {
		t.Skip("takes the README's pictures only when asked: task readme:pictures")
	}

	if err := os.MkdirAll(into, 0o750); err != nil {
		t.Fatalf("cannot create %s: %v", into, err)
	}

	zone := afternoon(time.Now())
	provider := oidctest.Start(t)

	// INFO, so the live log has the request lines an administrator would see, and
	// the name an installation is given when nobody chooses one.
	p := openWith(t, "LOG_LEVEL=INFO", defaultName)
	p.run("a desktop in the afternoon", chromedp.EmulateViewport(1280, 800),
		emulation.SetTimezoneOverride(zone))

	p.readyAdmin()
	p.awaitJSON(fmt.Sprintf(theInstallation, strconv.Quote(zone), strconv.Quote(provider.URL),
		strconv.Quote(oidctest.ClientID), strconv.Quote(oidctest.ClientSecret)), nil)
	p.reload()
	p.settled()

	p.showView("admin")
	p.shootCard(into, "log", "#log-card")

	p.run("sign out", chromedp.Click("#logout", chromedp.ByID),
		chromedp.WaitVisible("#form-login", chromedp.ByID))
	p.shootCard(into, "signin", "#form-login")

	p.signIn("robin@example.com", "robin-password-1")
	p.waitGone("#login-screen")
	p.settled()
	p.settleWelcome()
	p.chooseTheme("light")

	p.awaitJSON(demoRecord, nil)
	p.reload()
	p.settled()

	p.showView("calendar")
	p.shoot(into, "calendar")
	p.shootCard(into, "workday", "#workday-card")

	p.showView("week")
	p.shootCard(into, "week", "#week-card")

	p.showView("timesheets")
	p.shootCard(into, "stopwatch", "#timer-card")
	p.run("search this month's meetings",
		chromedp.SendKeys("#filter-ts-q", "#meeting", chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const field = document.querySelector('#filter-ts-from');
			setDateField(field, todayISO().slice(0, 8) + '01');
			field.dispatchEvent(new Event('change', {bubbles: true}));
		})()`, nil))
	p.shootCard(into, "entries", "#view-timesheets .card:has(#table-timesheets)")

	p.showView("overtime")
	p.shootCard(into, "activities", "#statistics-card",
		`#statistics-card h3[data-i18n="stats.perDay"]`, ".chart-wrap:has(#chart-days)")
	p.shootCard(into, "year", "#year-card", "#year-card .table-wrap")
	p.run("calculate the balance", p.click(`#form-overtime button[type="submit"]`))
	p.waitShown("#overtime-result")
	p.shootCard(into, "overtime", "#overtime-result")

	p.showView("report")
	p.run("evaluate", p.click(`#form-report button[type="submit"]`))
	p.waitShown("#report-result")
	p.run("as a pie", p.click(`#report-chart-switch [data-chart="pie"]`))
	p.shootCard(into, "report", "#report-result")
	p.run("work out the month's statement", p.click(`#form-statement button[type="submit"]`))
	p.waitShown("#statement-result")
	p.shootCard(into, "statement", "#statement-result")

	p.showView("projects")
	p.shootCard(into, "projects", "#view-projects .card:has(#table-projects)")

	p.chooseTheme("dark")
	p.showView("calendar")
	p.shoot(into, "calendar-dark")
	p.chooseTheme("light")

	p.chooseLanguage("de")
	p.shoot(into, "calendar-de")
	p.chooseLanguage("en")

	// Opened at a desktop's width, because a telephone folds the tabs away.
	p.showView("timesheets")
	p.run("a telephone", chromedp.EmulateViewport(390, 844, chromedp.EmulateScale(2)))
	p.shoot(into, "phone")

	// The same day from a terminal, through track and a token of its own.
	var token struct {
		Secret string `json:"secret"`
	}

	p.awaitJSON(`api('/me/tokens', {method: 'POST', body: JSON.stringify({name: 'Terminal', expiresInDays: 0})})`, &token)

	term := openTerminal(t, p.app.BaseURL(), token.Secret)
	term.await("Website relaunch", "Today")
	keep(t, into, "terminal", term.photograph())

	term.press("w")
	term.await("Switch to another project")
	term.press("jj")
	term.await("> Mobile app")
	keep(t, into, "terminal-switch", term.photograph())

	photographTheInstaller(t, into)
}

// afternoon names a zone in which it is early afternoon right now.
//
// The pictures show a day in progress - a morning booked, a stopwatch running
// since lunch - and which day that is, and what the clock reads, comes from the
// zone the installation keeps. Taken in Europe at one in the morning, the
// stopwatch had been running since the evening before. The fixed offsets of the
// tz database cover every hour, so whenever this runs there is one in which the
// record reads as an ordinary working afternoon. Their names are POSIX's, whose
// sign is the other way round: Etc/GMT-2 is two hours ahead of UTC.
func afternoon(now time.Time) string {
	offset := 14 - now.UTC().Hour()

	switch {
	case offset == 0:
		return "Etc/GMT"
	case offset > 0:
		return fmt.Sprintf("Etc/GMT-%d", offset)
	default:
		return fmt.Sprintf("Etc/GMT+%d", -offset)
	}
}

// chooseTheme sets the appearance the way the picker in the bar does. Left on
// its default it follows the time of day, which at night is dark.
func (p *page) chooseTheme(theme string) {
	p.t.Helper()

	p.run("choose "+theme, chromedp.SetValue("#theme-picker", theme, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('#theme-picker').dispatchEvent(new Event('change'))`, nil))
	p.waitEvaluates("the theme", `document.documentElement.dataset.theme`, theme)
}

// showView opens a screen from its tab and waits for it to be drawn.
func (p *page) showView(view string) {
	p.t.Helper()

	p.run("open "+view, p.click(`.tab[data-view="`+view+`"]`))
	p.waitShown("#view-" + view)
	p.settled()
}

// still waits for the screen to stop changing - nothing loading, no toast, no
// pointer over a control and no control focused - and puts it at the top, where
// the bar does not cover anything below it.
func (p *page) still() {
	p.t.Helper()

	p.atRest()
	p.waitFor("the toasts to go", func() bool { return p.count("#toast > *") == 0 })
	p.run("put the pointer away",
		chromedp.Evaluate(`document.activeElement?.blur(); window.scrollTo(0, 0)`, nil),
		chromedp.MouseEvent(input.MouseMoved, 1, 1))

	// The charts and the bar's highlight ease into place.
	time.Sleep(700 * time.Millisecond)
}

// shoot saves what the window shows as name.png in into.
func (p *page) shoot(into, name string) {
	p.t.Helper()

	p.still()

	var picture []byte

	p.run("photograph "+name, chromedp.CaptureScreenshot(&picture))
	keep(p.t, into, name, picture)
}

// shootCard saves one card as name.png in into, with a margin of the page around
// it, and without the parts of it hide names: the year's tiles are the picture,
// and the table of months under them would make it twice as tall.
func (p *page) shootCard(into, name, card string, hide ...string) {
	p.t.Helper()

	for _, part := range hide {
		p.run("put away "+part, chromedp.Evaluate(fmt.Sprintf(
			`document.querySelector(%q).style.display = 'none'`, part), nil))
	}

	p.still()

	var picture []byte

	p.run("photograph "+name, photograph(card, &picture))
	keep(p.t, into, name, picture)

	for _, part := range hide {
		p.run("bring back "+part, chromedp.Evaluate(fmt.Sprintf(
			`document.querySelector(%q).style.display = ''`, part), nil))
	}
}

// photographTheInstaller saves the screen a new installation opens on.
func photographTheInstaller(t *testing.T, into string) {
	t.Helper()

	app := harness.StartUnconfigured(t, defaultName)

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), launch("en-US")...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 90*time.Second)
	defer cancelTimeout()

	var picture []byte

	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(app.BaseURL()),
		chromedp.WaitVisible("#heading", chromedp.ByID),
		chromedp.Sleep(time.Second),
		photograph("main", &picture),
	); err != nil {
		t.Fatalf("photographing the installer: %v", err)
	}

	keep(t, into, "installer", picture)
}

// photograph captures the element the selector finds, with a margin of the page
// around it - beyond the window if it reaches further than the window does.
func photograph(selector string, picture *[]byte) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var box struct{ X, Y, Width, Height float64 }

		if err := chromedp.Evaluate(fmt.Sprintf(`(() => {
			const r = document.querySelector(%q).getBoundingClientRect();
			return {X: r.left + scrollX, Y: r.top + scrollY, Width: r.width, Height: r.height};
		})()`, selector), &box).Do(ctx); err != nil {
			return err
		}

		const margin = 16

		shot, err := cdppage.CaptureScreenshot().
			WithCaptureBeyondViewport(true).
			WithClip(&cdppage.Viewport{
				X: box.X - margin, Y: box.Y - margin,
				Width: box.Width + 2*margin, Height: box.Height + 2*margin,
				Scale: 1,
			}).Do(ctx)
		*picture = shot

		return err
	})
}

// keep writes a picture.
func keep(t *testing.T, into, name string, picture []byte) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(into, name+".png"), picture, 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", name, err)
	}

	t.Logf("%s.png: %d KB", name, len(picture)/1024)
}

// theInstallation is what the built-in administrator settles before anybody
// works: the zone, the public holidays of a state, a provider to sign in through,
// and the account whose year the pictures show.
const theInstallation = `(async () => {
	const put = (path, body) => api(path, {method: 'PUT', body: JSON.stringify(body)});
	await put('/settings/timezone', {timezone: %s});
	await put('/settings/holidays', {region: 'BY'});
	await put('/settings/openid', {enabled: true, issuer: %s, clientId: %s, clientSecret: %s, label: ''});
	await api('/users', {method: 'POST', body: JSON.stringify({
		name: 'Robin Keller', email: 'robin@example.com', role: 'user', password: 'robin-password-1'})});
	return true;
})()`

// demoRecord is somebody's last twelve months: five projects, three of them
// charged by the hour, three or four entries a working day with an activity in
// each, leave and a few days off sick, public holidays kept free, when each of
// the last two weeks' days began and ended, and a stopwatch running since
// shortly before one.
//
// The budgets are chosen to land one project past its budget and one in the
// last fifth of it, which is where the project list marks them; the activities
// are seven that keep seven different colours, where the charts would otherwise
// draw two of them alike.
const demoRecord = `(async () => {
	const pad = n => String(n).padStart(2, '0');
	const iso = d => d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
	const clock = minutes => pad(Math.floor(minutes / 60)) + ':' + pad(minutes % 60);
	const today = new Date();
	today.setHours(12, 0, 0, 0);
	const ago = n => { const d = new Date(today); d.setDate(d.getDate() - n); return d; };
	const post = (path, body) => api(path, {method: 'POST', body: JSON.stringify(body)});
	const put = (path, body) => api(path, {method: 'PUT', body: JSON.stringify(body)});
	const back = 365;

	const project = async (name, description, budgetHours, hourlyRate = null) => (await post('/projects', {
		name, description, budgetHours, startDate: iso(ago(back + 30)), status: 'active',
		billable: hourlyRate !== null, hourlyRate, currency: 'EUR'})).id;

	const web = await project('Website relaunch', 'New shop front and checkout', 800, 95);
	const app = await project('Mobile app', 'Release 2.4 for iOS and Android', 1000, 110);
	const support = await project('Customer support', 'Second-level tickets', null, 70);
	const tooling = await project('Internal tooling', 'Build pipeline and deployment', 160);
	const training = await project('Training', 'Courses and conferences', 100);

	const days = [
		[[web, 3.5, '#design Landing page layout'], [app, 2.5, '#review Release 2.4 pull requests'],
			[support, 1.5, '#support Ticket queue'], [tooling, 0.5, '#meeting Stand-up']],
		[[app, 4, '#development Offline sync'], [web, 2, '#meeting Sprint planning'],
			[support, 2.25, '#support Escalations']],
		[[web, 5, '#development Checkout flow'], [training, 1.5, '#ux Accessibility workshop'],
			[tooling, 1, '#devops CI pipeline']],
		[[app, 3, '#review Code review'], [support, 3, '#support Ticket queue'], [web, 2.75, '#design Icon set']],
		[[tooling, 2.5, '#devops Deployment scripts'], [app, 3.5, '#development Push notifications'],
			[web, 1.5, '#meeting Stakeholder demo'], [support, 1, '#support Ticket queue']],
	];

	const monday = (today.getDay() + 6) % 7;
	const away = new Set();
	for (const [first, length, kind] of [
		[monday + 4, 2, 'vacation'], [monday + 14, 1, 'sick'], [monday + 77, 12, 'vacation'],
		[monday + 133, 1, 'sick'], [monday + 196, 5, 'vacation'],
	]) {
		await post('/me/absences', {from: iso(ago(first)), to: iso(ago(first - length + 1)), kind});
		for (let i = 0; i < length; i++) away.add(iso(ago(first - i)));
	}

	const span = await api('/me/days?from=' + iso(ago(back)) + '&to=' + iso(ago(1)));
	for (const day of span.days) {
		if (day.holiday) away.add(day.date);
	}

	for (let n = back; n >= 1; n--) {
		const d = ago(n);
		if (d.getDay() === 0 || d.getDay() === 6 || away.has(iso(d))) continue;
		const plan = days[n % days.length];
		for (const [projectId, durationHours, description] of plan) {
			await post('/timesheets', {projectId, date: iso(d), durationHours, description});
		}
		if (n <= 14) {
			const begin = 8 * 60 + 15 + (n % 4) * 10;
			const end = begin + Math.round(plan.reduce((sum, e) => sum + e[1], 0) * 60) + 30;
			await put('/me/workdays/' + iso(d), {start: clock(begin), end: clock(end), breakMinutes: 30});
		}
	}

	for (const [projectId, durationHours, description] of [
		[tooling, 0.5, '#meeting Stand-up'], [support, 1.5, '#support Ticket queue'], [app, 2, '#review Code review'],
	]) {
		await post('/timesheets', {projectId, date: iso(today), durationHours, description});
	}

	await post('/me/templates', {projectId: tooling, durationHours: 0.5, description: '#meeting Stand-up'});
	await put('/me/vacation', {fromYear: today.getFullYear() - 1, days: 30});

	await post('/me/timer', {projectId: web, description: '#design Hero section'});
	await put('/me/timer', {startedAt: new Date(Date.now() - 80 * 60000).toISOString()});

	return true;
})()`
