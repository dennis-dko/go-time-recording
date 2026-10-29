package cli

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// refreshEvery is how often the screen reads the day again while it is open.
// The stopwatch is the one the browser shows, so it may have been stopped
// there; half a minute is soon enough to notice that, and rare enough to cost
// the installation nothing.
const refreshEvery = 30 * time.Second

// screen is the client on a terminal: the stopwatch running live, what today has
// booked, and a form for each change the commands make.
//
// It makes those changes through the same session as the commands, so it can
// do nothing they cannot, and it says what it did in their words. It keeps the
// context it was opened with because Bubble Tea hands Update none, and every
// request the screen sends has to end when the program does.
type screen struct {
	ctx  context.Context
	s    *session
	host string

	width int
	dark  bool

	shown  snapshot
	readAt time.Time
	now    time.Time
	loaded bool

	// reading is a read of the day on its way, so the second hand does not ask
	// again every second while an installation is slow to answer. askedAt is
	// when the day was last asked for, answered or not: the half minute is
	// counted from there, because counted from the last answer it ran out for
	// good once an installation stopped answering, and from then on the screen
	// asked with every second.
	reading bool
	askedAt time.Time

	// busy is a change on its way, or one whose outcome has not been read yet.
	// Until it has, the screen shows the day as it was before the change, and a
	// key that acts on that would act on something that is no longer true.
	busy bool

	// news and failure are what the last change did, or why it was refused;
	// stale is why the last read failed, which leaves the day shown as it was.
	news    string
	failure string
	stale   string

	// form is the one open, if any, and doing says which change it asks about.
	// asked holds its answers behind a pointer, because the form writes into it
	// while the screen itself is copied on every update.
	form  *huh.Form
	doing string
	asked *answers
}

// loaded is the day as the installation answered, and the moment it was read.
type loaded struct {
	shown snapshot
	at    time.Time
	err   error
}

// done is what became of a change, in the sentence its command would print.
type done struct {
	said string
	err  error
}

// tick is the second hand.
type tick time.Time

// newScreen is the screen for one session, before anything has been read.
func newScreen(ctx context.Context, s *session) screen {
	host := s.api.base
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Host
	}

	return screen{ctx: ctx, s: s, host: host, reading: true, askedAt: time.Now()}
}

// show runs the screen until it is closed.
func (s *session) show(ctx context.Context) error {
	ended, err := s.program(ctx).Run()

	return s.leave(ctx, ended, err)
}

// program is the screen as a program drawing on the session's output. The
// options are the tests' way in, since they have no terminal to give it.
func (s *session) program(ctx context.Context, options ...tea.ProgramOption) *tea.Program {
	options = append([]tea.ProgramOption{tea.WithContext(ctx), tea.WithOutput(s.out)}, options...)

	return tea.NewProgram(newScreen(ctx, s), options...)
}

// leave leaves a line in the terminal saying how the stopwatch was left: the
// screen takes its picture with it when it goes, and whoever looks back at the
// shell should still see that.
func (s *session) leave(ctx context.Context, ended tea.Model, err error) error {
	switch {
	case ctx.Err() != nil, errors.Is(err, tea.ErrInterrupted):
		return nil
	case err != nil:
		return fmt.Errorf("%s: %w", s.lang.text("track.screenFailed", "the screen could not be opened"), err)
	}

	if last, ok := ended.(screen); ok && last.loaded {
		s.say("%s", last.farewell(time.Now()))
	}

	return nil
}

// Init reads the day, starts the second hand, and asks the terminal whether it
// is dark, which decides the colours.
func (m screen) Init() tea.Cmd {
	return tea.Batch(m.load(), second(), tea.RequestBackgroundColor)
}

// ask reads the day again, and remembers when it was asked.
func (m screen) ask(now time.Time) (screen, tea.Cmd) {
	m.reading, m.askedAt = true, now

	return m, m.load()
}

// load reads the day again.
func (m screen) load() tea.Cmd {
	ctx, s := m.ctx, m.s

	return func() tea.Msg {
		shown, err := s.snapshot(ctx)

		return loaded{shown: shown, at: time.Now(), err: err}
	}
}

// second waits for the next second on the wall clock, so the stopwatch turns
// over when the clock on the wall does.
func second() tea.Cmd {
	return tea.Every(time.Second, func(at time.Time) tea.Msg { return tick(at) })
}

// Update answers a key, a second, or what the installation said.
func (m screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// From anywhere, a form included: escape is what puts a form away.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.form == nil {
			return m.press(msg)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
	case tick:
		m.now = time.Time(msg)

		if !m.reading && m.now.Sub(m.askedAt) >= refreshEvery {
			next, read := m.ask(m.now)

			return next, tea.Batch(second(), read)
		}

		return m, second()
	case loaded:
		return m.read(msg), nil
	case done:
		m.news, m.failure = msg.said, ""
		if msg.err != nil {
			m.news, m.failure = "", msg.err.Error()
		}

		return m.ask(time.Now())
	}

	if m.form != nil {
		return m.fillIn(msg)
	}

	return m, nil
}

// read takes in the day as the installation answered. A failure keeps the day
// that was shown, under a line saying why: an installation briefly out of reach
// should not blank the screen of somebody looking at it.
func (m screen) read(msg loaded) screen {
	m.reading, m.busy = false, false

	if msg.err != nil {
		m.stale = msg.err.Error()

		return m
	}

	m.shown, m.readAt, m.loaded, m.stale = msg.shown, msg.at, true, ""

	if msg.at.After(m.now) {
		m.now = msg.at
	}

	return m
}

// offer is a key that opens the form for a change, and what the legend calls the
// change. The keys stay the same in every language, so a hand that knows them
// does not have to learn them again.
type offer struct{ key, doing, called string }

// offers are the changes the stopwatch can take as it stands, in the order the
// legend names them. A key for a change it cannot take - stopping one that is
// not running - is not offered, and does nothing.
func (m screen) offers() []offer {
	l := m.s.lang

	if m.shown.clock.Running {
		return []offer{{"w", "switch", l.text("track.key.switch", "switch")}, {"x", "stop", l.text("track.key.stop", "stop")}, {"b", "book", l.text("track.key.book", "book")}}
	}

	return []offer{{"s", "start", l.text("track.key.start", "start")}, {"b", "book", l.text("track.key.book", "book")}}
}

// press answers a key while no form is open.
func (m screen) press(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "r":
		if m.reading {
			return m, nil
		}

		return m.ask(time.Now())
	}

	if !m.loaded || m.busy {
		return m, nil
	}

	for _, o := range m.offers() {
		if o.key == k.String() {
			return m.open(o.doing)
		}
	}

	return m, nil
}

// open puts up the form for a change, with what the last one said cleared away.
func (m screen) open(doing string) (tea.Model, tea.Cmd) {
	m.doing, m.news, m.failure = doing, "", ""
	m.asked = &answers{day: m.readAt.In(m.shown.zone).Format(time.DateOnly), stop: true}
	m.form = m.formFor(doing)

	return m, m.form.Init()
}

// fillIn hands a message to the open form, and sends the change once the form
// is answered.
func (m screen) fillIn(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	m.form = form.(*huh.Form)

	switch m.form.State {
	case huh.StateAborted:
		m.form = nil

		return m, nil
	case huh.StateCompleted:
		m.form = nil

		change := m.request()
		if change == nil {
			return m, nil
		}

		m.busy = true

		return m, change
	}

	return m, cmd
}

// elapsed is how long the stopwatch has run: what the installation measured
// when the day was read, and the time since on this machine's clock - so a
// clock that is set wrong here moves the second hand and not the hours.
func (m screen) elapsed() time.Duration {
	return time.Duration(m.shown.clock.ElapsedHours*float64(time.Hour)) + max(m.now.Sub(m.readAt), 0)
}

// farewell is the line the screen leaves in the terminal when it closes.
func (m screen) farewell(now time.Time) string {
	if now.After(m.now) {
		m.now = now
	}

	return m.shown.clockLine(m.s.lang, m.elapsed().Hours())
}

// tones are the web interface's colours, light or dark, so that the terminal
// and the browser look like one application: app.css holds the same ones.
type tones struct {
	accent, onAccent, muted, danger, green, line color.Color
}

// tonesFor is the colours for a dark terminal or a light one.
func tonesFor(dark bool) tones {
	pick := lipgloss.LightDark(dark)

	return tones{
		accent:   pick(lipgloss.Color("#2f6feb"), lipgloss.Color("#5b8dfa")),
		onAccent: pick(lipgloss.Color("#ffffff"), lipgloss.Color("#0d1117")),
		muted:    pick(lipgloss.Color("#626d78"), lipgloss.Color("#98a2ad")),
		danger:   pick(lipgloss.Color("#c0392b"), lipgloss.Color("#e06c5c")),
		green:    lipgloss.Color("#2e9e5b"),
		line:     pick(lipgloss.Color("#dfe3e8"), lipgloss.Color("#2c333b")),
	}
}

// palette is the styles the screen draws with.
type palette struct {
	title, strong, muted, ok, failed, key lipgloss.Style

	idle, running color.Color
}

// paletteFor is the palette for a dark terminal or a light one.
func paletteFor(dark bool) palette {
	c := tonesFor(dark)

	return palette{
		title:   lipgloss.NewStyle().Bold(true).Foreground(c.accent),
		strong:  lipgloss.NewStyle().Bold(true),
		muted:   lipgloss.NewStyle().Foreground(c.muted),
		ok:      lipgloss.NewStyle().Foreground(c.green),
		failed:  lipgloss.NewStyle().Foreground(c.danger),
		key:     lipgloss.NewStyle().Bold(true).Foreground(c.accent),
		idle:    c.muted,
		running: c.green,
	}
}

// contentWidth is how wide the screen draws: the window less its margins, and
// no wider than a line can be read across.
func (m screen) contentWidth() int {
	if m.width == 0 {
		return 76
	}

	return min(max(m.width-4, 40), 88)
}

// View draws the screen: who and where, the stopwatch, and under it today's
// entries or the form that is open.
func (m screen) View() tea.View {
	look := paletteFor(m.dark)
	width := m.contentWidth()

	parts := []string{m.header(look, width), m.clockCard(look, width)}

	switch {
	case m.form != nil:
		parts = append(parts, m.form.View())
	case m.loaded:
		parts = append(parts, m.todayList(look, width))
	}

	// Wrapped rather than cut short: a reason comes at the end of its sentence.
	if line := m.statusLine(look); line != "" {
		parts = append(parts, lipgloss.NewStyle().Width(width).Render(line))
	}

	parts = append(parts, m.legend(look))

	v := tea.NewView(lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(parts, "\n\n")))
	v.AltScreen = true
	v.WindowTitle = m.windowTitle()

	return v
}

// header names the application, the installation and the account.
func (m screen) header(look palette, width int) string {
	left := look.title.Render("go-time-recording") + look.muted.Render("  "+m.host)

	return spread(width, left, look.muted.Render(m.shown.name))
}

// clockCard is the stopwatch: what it runs on, since when, and for how long.
func (m screen) clockCard(look palette, width int) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(look.idle).
		Padding(0, 1).Width(width)

	l := m.s.lang

	switch {
	case !m.loaded && m.reading:
		return box.Render(look.muted.Render(l.text("track.reading", "Reading {0} …", m.host)))
	case !m.loaded:
		return box.Render(look.muted.Render(l.text("track.nothingRead", "Nothing read from {0} yet.", m.host)))
	}

	clock := m.shown.clock
	if !clock.Running || clock.StartedAt == nil {
		return box.Render(look.muted.Render("○  ") + l.text("track.notRunning", "The stopwatch is not running."))
	}

	on := l.nameOf(m.shown.projects, clock.ProjectID)
	if clock.Description != nil && *clock.Description != "" {
		on += look.muted.Render(" · " + *clock.Description)
	}

	left := look.ok.Render("●  ") + look.strong.Render(clockFace(m.elapsed())) + "   " + on
	right := look.muted.Render(l.text("track.since", "since {0}", clock.StartedAt.In(m.shown.zone).Format("15:04")))

	return box.BorderForeground(look.running).Render(spread(width-4, left, right))
}

// todayList is what today has booked, entry by entry, under its total.
func (m screen) todayList(look palette, width int) string {
	l := m.s.lang
	entries := m.shown.today
	day := l.weekday(m.readAt.In(m.shown.zone))

	heading := spread(width, look.strong.Render(l.text("cal.today", "Today"))+look.muted.Render("  "+day),
		look.strong.Render(l.hours(total(entries))))

	if len(entries) == 0 {
		return heading + "\n" + look.muted.Render(l.text("track.nothingYet", "Nothing booked yet."))
	}

	names := make([]string, len(entries))
	column := 0

	for i, e := range entries {
		names[i] = l.nameOf(m.shown.projects, e.ProjectID)
		column = max(column, lipgloss.Width(names[i]))
	}

	column = min(column, width/2)
	rows := []string{heading}
	hoursWidth := hoursColumn(l.hoursOf(entries)...)

	for i, e := range entries {
		name := ansi.Truncate(names[i], column, "…")
		row := fmt.Sprintf("   %*s  %s", hoursWidth, l.hours(e.DurationHours),
			name+strings.Repeat(" ", column-lipgloss.Width(name)))

		if e.Description != nil && *e.Description != "" {
			row += "  " + look.muted.Render(*e.Description)
		}

		rows = append(rows, ansi.Truncate(row, width, "…"))
	}

	return strings.Join(rows, "\n")
}

// statusLine says what the last change did, or why the day shown may be old.
func (m screen) statusLine(look palette) string {
	switch {
	case m.stale != "":
		return look.failed.Render("✗ " + m.stale)
	case m.failure != "":
		return look.failed.Render("✗ " + m.failure)
	case m.news != "":
		return look.ok.Render("✓ ") + m.news
	case m.busy:
		return look.muted.Render(m.s.lang.text("track.sending", "Sending …"))
	}

	return ""
}

// legend names the keys that do something now, and only those.
func (m screen) legend(look palette) string {
	l := m.s.lang

	var keys []offer

	switch {
	case m.form != nil:
		keys = []offer{{"esc", "", l.text("track.key.cancel", "cancel")}, {"ctrl+c", "", l.text("track.key.quit", "quit")}}
	default:
		if m.loaded && !m.busy {
			keys = m.offers()
		}

		keys = append(keys, offer{"r", "", l.text("track.key.refresh", "refresh")}, offer{"q", "", l.text("track.key.quit", "quit")})
	}

	named := make([]string, len(keys))
	for i, k := range keys {
		named[i] = look.key.Render(k.key) + " " + look.muted.Render(k.called)
	}

	return strings.Join(named, look.muted.Render("  ·  "))
}

// windowTitle puts the stopwatch in the terminal's title, where it can be seen
// from another tab.
func (m screen) windowTitle() string {
	if m.loaded && m.shown.clock.Running {
		return clockFace(m.elapsed()) + " · " + m.s.lang.nameOf(m.shown.projects, m.shown.clock.ProjectID)
	}

	return "go-time-recording"
}

// spread sets left and right on one line of width, cutting the left short
// rather than letting the line wrap.
func spread(width int, left, right string) string {
	if room := width - lipgloss.Width(right) - 2; lipgloss.Width(left) > room {
		left = ansi.Truncate(left, max(room, 0), "…")
	}

	return left + strings.Repeat(" ", max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right
}

// clockFace writes a duration as a stopwatch shows it: 1:23:45.
func clockFace(d time.Duration) string {
	seconds := int(d.Truncate(time.Second) / time.Second)

	return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
