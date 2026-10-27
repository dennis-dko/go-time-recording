package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// watched is the screen running as a program the test types into, without a
// terminal: keys arrive through Send, and what the screen has come to is asked
// through the program's filter, which runs between two messages - so the test
// reads the screen only where nothing is writing it.
type watched struct {
	t     *testing.T
	p     *tea.Program
	out   *bytes.Buffer
	ended chan struct{}
	err   error
}

// look asks the filter whether the screen has come to something.
type look struct {
	ok     func(screen) bool
	answer chan seen
}

// seen is the filter's answer, with what the screen showed.
type seen struct {
	ok    bool
	shows string
}

// watch opens the screen against an installation.
func watch(t *testing.T, base, token string) *watched {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	w := &watched{t: t, out: &bytes.Buffer{}, ended: make(chan struct{})}
	s := &session{api: newClient(base, token), out: w.out}

	w.p = s.program(ctx, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithWindowSize(100, 40),
		tea.WithoutSignals(), tea.WithFilter(func(m tea.Model, msg tea.Msg) tea.Msg {
			l, asked := msg.(look)
			if !asked {
				return msg
			}

			now := m.(screen)
			l.answer <- seen{ok: l.ok(now), shows: ansi.Strip(now.View().Content)}

			return nil
		}))

	go func() {
		defer close(w.ended)

		ended, err := w.p.Run()
		w.err = s.leave(ctx, ended, err)
	}()

	t.Cleanup(func() {
		cancel()
		<-w.ended
	})

	return w
}

// until waits for the screen to come to what ok asks, and answers with what it
// shows then.
func (w *watched) until(what string, ok func(screen) bool) string {
	w.t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for {
		answer := make(chan seen, 1)

		var got seen

		w.p.Send(look{ok: ok, answer: answer})

		select {
		case got = <-answer:
		case <-w.ended:
			w.t.Fatalf("the screen closed before %s", what)
		}

		if got.ok {
			return got.shows
		}

		if time.Now().After(deadline) {
			w.t.Fatalf("the screen never came to %s; it shows:\n%s", what, got.shows)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// press sends keys as a keyboard does: each a character, or a named key.
func (w *watched) press(keys ...string) {
	named := map[string]rune{"enter": tea.KeyEnter, "down": tea.KeyDown, "backspace": tea.KeyBackspace, "esc": tea.KeyEscape}

	for _, k := range keys {
		if code, ok := named[k]; ok {
			w.p.Send(tea.KeyPressMsg{Code: code})

			continue
		}

		for _, r := range k {
			w.p.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
}

// close quits the screen with q and waits for it to go.
func (w *watched) close() {
	w.t.Helper()

	w.press("q")

	select {
	case <-w.ended:
	case <-time.After(10 * time.Second):
		w.t.Fatal("the screen did not close on q")
	}

	if w.err != nil {
		w.t.Fatalf("the screen closed with %v", w.err)
	}
}

// asking is a condition on the field the open form is asking about.
func asking(field string) func(screen) bool {
	return func(m screen) bool { return m.form != nil && m.form.GetFocusedField().GetKey() == field }
}

// settled is a screen with no form open and nothing on its way.
func settled(m screen) bool { return m.loaded && m.form == nil && !m.busy && !m.reading }

// The screen shows what the stopwatch runs on, since when and for how long,
// ticking on by itself, with today's entries under it and only the keys that do
// something now.
func TestTheScreenShowsTheStopwatchRunningAndTodaysEntries(t *testing.T) {
	f, server := newFakeInstallation(t)

	m := newScreen(context.Background(), &session{api: newClient(server.URL, f.token), out: io.Discard})

	read, _ := m.load()().(loaded)
	if read.err != nil {
		t.Fatalf("reading the day: %v", read.err)
	}

	next, _ := m.Update(read)
	m = next.(screen)
	shows := ansi.Strip(m.View().Content)

	for _, want := range []string{"Dana Example", "1:30:00", "Consulting · review", "since 09:12",
		"Today", "3.00 h", "2.50 h", "review", "no project", "w switch", "x stop", "b book"} {
		if !strings.Contains(shows, want) {
			t.Errorf("the screen does not show %q:\n%s", want, shows)
		}
	}

	if strings.Contains(shows, "s start") {
		t.Errorf("a running stopwatch is offered a start:\n%s", shows)
	}

	next, _ = m.Update(tick(read.at.Add(75 * time.Second)))

	if shows := ansi.Strip(next.(screen).View().Content); !strings.Contains(shows, "1:31:15") {
		t.Errorf("75 seconds on, the stopwatch does not show 1:31:15:\n%s", shows)
	}
}

// Starting from the screen sends the project picked and the description typed,
// and the screen then shows the stopwatch running - read back from the
// installation, not assumed.
func TestStartingFromTheScreenStartsTheStopwatchOnTheProjectPicked(t *testing.T) {
	f, server := newFakeInstallation(t)
	f.stopped = true

	w := watch(t, server.URL, f.token)
	w.until("the day is read", func(m screen) bool { return m.loaded && !m.shown.clock.Running })

	w.press("s")
	w.until("the project is asked", asking("project"))

	// No project, Website, Webshop, Consulting: the archived one is not offered.
	w.press("down", "down", "down", "enter")
	w.until("the description is asked", asking("description"))

	w.press("review", "enter")
	shows := w.until("the start is read back", func(m screen) bool { return settled(m) && m.shown.clock.Running })

	sent := f.sentTo("POST /me/timer")
	if len(sent) != 1 || sent[0]["projectId"] != float64(4) || sent[0]["description"] != "review" {
		t.Errorf("the installation was sent %v, want the stopwatch started on Consulting for the review", sent)
	}

	if !strings.Contains(shows, "Started at 09:12 on Consulting.") || !strings.Contains(shows, "since 09:12") {
		t.Errorf("after the start the screen shows:\n%s", shows)
	}

	w.close()

	if left := w.out.String(); !strings.Contains(left, "Running since 09:12 on Consulting") {
		t.Errorf("closing the screen left %q in the terminal, want how the stopwatch was left", left)
	}
}

// Stopping asks first, and keeping the stopwatch running sends nothing.
func TestStoppingFromTheScreenAsksFirst(t *testing.T) {
	f, server := newFakeInstallation(t)

	w := watch(t, server.URL, f.token)
	w.until("the day is read", settled)

	w.press("x")
	w.until("the stop is asked about", asking("stop"))
	w.press("n")
	w.until("the question is put away", settled)

	if sent := f.sentTo("POST /me/timer/stop"); len(sent) != 0 {
		t.Fatalf("keeping the stopwatch running sent %d stop(s)", len(sent))
	}

	w.press("x")
	w.until("the stop is asked about again", asking("stop"))
	w.press("y")

	shows := w.until("the stop is read back", func(m screen) bool { return settled(m) && !m.shown.clock.Running })

	if sent := f.sentTo("POST /me/timer/stop"); len(sent) != 1 {
		t.Errorf("stopping sent %d stop(s), want one", len(sent))
	}

	if !strings.Contains(shows, "Booked 1.50 h on Consulting.") || !strings.Contains(shows, "s start") {
		t.Errorf("after the stop the screen shows:\n%s", shows)
	}

	w.close()
}

// Booking by hand refuses hours it cannot read where they are typed, and sends
// the day it offers - today in the account's zone - with what was picked.
func TestBookingFromTheScreenRefusesHoursItCannotRead(t *testing.T) {
	f, server := newFakeInstallation(t)

	w := watch(t, server.URL, f.token)
	w.until("the day is read", settled)

	w.press("b")
	w.until("the hours are asked", asking("hours"))

	w.press("abc", "enter")
	shows := w.until("the hours are refused", func(m screen) bool {
		return asking("hours")(m) && m.form.GetFocusedField().Error() != nil
	})

	if !strings.Contains(shows, `"abc" is not a number of hours`) {
		t.Errorf("hours that cannot be read are refused without saying so:\n%s", shows)
	}

	w.press("backspace", "backspace", "backspace", "1:30", "enter")
	w.until("the project is asked", asking("project"))
	w.press("down", "down", "down", "enter")
	w.until("the day is asked", asking("day"))
	w.press("enter")
	w.until("the description is asked", asking("description"))
	w.press("call", "enter")
	w.until("the booking is read back", func(m screen) bool { return settled(m) && m.news != "" })

	today := time.Now().In(mustZone(t, "Europe/Berlin")).Format(time.DateOnly)
	sent := f.sentTo("POST /timesheets")

	if len(sent) != 1 || sent[0]["durationHours"] != 1.5 || sent[0]["date"] != today ||
		sent[0]["projectId"] != float64(4) || sent[0]["description"] != "call" {
		t.Errorf("the installation was sent %v, want 1.5 hours on Consulting today (%s) for the call", sent, today)
	}

	w.close()
}

// A refusal is said under the stopwatch in the installation's own words, and the
// screen goes on showing the day.
func TestARefusedChangeIsSaidOnTheScreen(t *testing.T) {
	f, server := newFakeInstallation(t)
	f.refuseAs = "the project is closed for booking"

	w := watch(t, server.URL, f.token)
	w.until("the day is read", settled)

	w.press("x")
	w.until("the stop is asked about", asking("stop"))
	w.press("y")

	shows := w.until("the refusal is said", func(m screen) bool { return settled(m) && m.failure != "" })

	if !strings.Contains(shows, "✗ the project is closed for booking") || !strings.Contains(shows, "Today") {
		t.Errorf("after a refusal the screen shows:\n%s", shows)
	}

	w.close()
}

// An installation that cannot be reached is asked again every half minute: not
// with every second the clock ticks once a read has failed, and not never when
// the very first one failed.
func TestAnInstallationOutOfReachIsAskedAgainEveryHalfMinute(t *testing.T) {
	asked := func(m screen, msg tea.Msg) (screen, bool) {
		next, _ := m.Update(msg)

		return next.(screen), next.(screen).reading
	}

	opened := func() screen {
		return newScreen(context.Background(), &session{api: newClient("http://127.0.0.1:1", "gtr_x"), out: io.Discard})
	}

	// Taken after both screens are opened, each noting when it first asked: a
	// start taken before would put "thirty seconds on" a few nanoseconds short
	// of the half minute, on a clock fine enough to tell - Linux's is, and this
	// case failed there while it passed on Windows.
	neverRead, readOnce := opened(), opened()
	start := time.Now()
	out := errors.New("could not reach the installation")

	m, _ := asked(neverRead, loaded{err: out, at: start.Add(time.Second)})

	if _, again := asked(m, tick(start.Add(31*time.Second))); !again {
		t.Error("after a first read that failed, half a minute on, nothing asks again")
	}

	m, _ = asked(readOnce, loaded{shown: snapshot{zone: time.UTC}, at: start})
	m, _ = asked(m, tick(start.Add(30*time.Second)))
	m, _ = asked(m, loaded{err: out, at: start.Add(31 * time.Second)})

	if _, again := asked(m, tick(start.Add(32*time.Second))); again {
		t.Error("a second after a read failed, the installation is asked again")
	}

	if _, again := asked(m, tick(start.Add(61*time.Second))); !again {
		t.Error("half a minute after a read failed, nothing asks again")
	}
}

// Why the day cannot be read is said whole, over as many lines as it takes: the
// reason comes at the end of the sentence, which is where a cut falls.
func TestWhyTheDayCannotBeReadIsSaidWhole(t *testing.T) {
	m := newScreen(context.Background(), &session{api: newClient("http://127.0.0.1:9", "gtr_x"), out: io.Discard})

	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	next, _ = next.(screen).Update(loaded{at: time.Now(), err: errors.New(`could not reach http://127.0.0.1:9: ` +
		`Get "http://127.0.0.1:9/api/v1/me": dial tcp 127.0.0.1:9: connect: connection refused`)})

	shows := strings.Join(strings.Fields(ansi.Strip(next.(screen).View().Content)), " ")

	if !strings.Contains(shows, "connection refused") {
		t.Errorf("the screen does not say why the day cannot be read:\n%s", ansi.Strip(next.(screen).View().Content))
	}

	// Nor does it go on saying it is reading, once it has stopped.
	if strings.Contains(shows, "Reading ") {
		t.Errorf("after the read failed, the screen still says it is reading:\n%s", ansi.Strip(next.(screen).View().Content))
	}
}

// mustZone loads a zone the test depends on.
func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()

	zone, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}

	return zone
}
