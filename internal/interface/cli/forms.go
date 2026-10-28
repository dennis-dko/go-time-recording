package cli

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// projectRows is how many projects a form shows at once before the list
// scrolls, so that a long list still fits under the stopwatch.
const projectRows = 7

// answers are what a form has been told.
type answers struct {
	project     uint
	description string
	hours       string
	day         string
	stop        bool
}

// formFor is the form for a change, writing into the screen's answers.
func (m screen) formFor(doing string) *huh.Form {
	a, l := m.asked, m.s.lang
	running := l.nameOf(m.shown.projects, m.shown.clock.ProjectID)

	var group *huh.Group

	switch doing {
	case "start":
		group = huh.NewGroup(m.projectField(&a.project), m.describe(&a.description)).
			Title(l.text("track.form.start", "Start the stopwatch"))
	case "switch":
		group = huh.NewGroup(m.projectField(&a.project), m.describe(&a.description)).
			Title(l.text("track.form.switch", "Switch to another project")).
			Description(l.text("track.form.switchNote", "What has run so far is booked on {0}.", running))
	case "stop":
		group = huh.NewGroup(huh.NewConfirm().Key("stop").
			Title(l.text("track.form.stopQuestion", "Book {0} on {1}?", hoursAndMinutes(m.elapsed().Hours()), running)).
			Affirmative(l.text("track.form.stopYes", "Stop and book")).Negative(l.text("track.form.stopNo", "Keep it running")).Value(&a.stop)).
			Title(l.text("track.form.stop", "Stop the stopwatch"))
	default:
		group = huh.NewGroup(
			huh.NewInput().Key("hours").Title(l.text("field.hours", "Hours")).Placeholder(l.text("track.form.hoursHint", "1.5, 1,5 or 1:30")).
				Validate(l.readableHours).Value(&a.hours),
			m.projectField(&a.project),
			huh.NewInput().Key("day").Title(l.text("field.day", "Day")).Placeholder(l.text("track.form.dayHint", "YYYY-MM-DD")).
				Validate(l.readableDay).Value(&a.day),
			m.describe(&a.description),
		).Title(l.text("track.form.book", "Book time by hand"))
	}

	// Escape puts a form away; ctrl+c, which huh would use, closes the screen.
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", l.text("track.key.cancel", "cancel")))
	l.keyHelp(keys)

	// The screen's answer to whether the terminal is dark rather than the form's
	// own: the terminal says so once, before any form is open, and is not asked
	// again.
	dark := m.dark

	return huh.NewForm(group).
		WithTheme(huh.ThemeFunc(func(bool) *huh.Styles { return formTheme(dark) })).
		WithKeyMap(keys).
		WithShowHelp(true).
		WithWidth(m.contentWidth())
}

// projectField picks one of the account's active projects, or none.
func (m screen) projectField(into *uint) *huh.Select[uint] {
	l := m.s.lang
	options := []huh.Option[uint]{huh.NewOption(l.text("ts.noProject", "No project"), uint(0))}

	for _, p := range m.shown.projects {
		if p.Status == "active" {
			options = append(options, huh.NewOption(p.Name, p.ID))
		}
	}

	field := huh.NewSelect[uint]().Key("project").Title(l.text("field.project", "Project")).Options(options...).Value(into)

	// The height counts the title's line.
	if len(options) > projectRows {
		field = field.Height(projectRows + 1)
	}

	return field
}

// describe asks what the time went on, which may be nothing.
func (m screen) describe(into *string) *huh.Input {
	l := m.s.lang

	return huh.NewInput().Key("description").Title(l.text("field.description", "Description")).Placeholder(l.text("track.form.optional", "optional")).
		CharLimit(model.MaxDescriptionLength).Value(into)
}

// readableHours refuses hours written in none of the ways parseHours reads.
func (l language) readableHours(raw string) error {
	_, err := l.parseHours(strings.TrimSpace(raw))

	return err
}

// keyHelp says in this language what huh's keys do, in the line under a form:
// the keys of the three kinds of field these forms have.
func (l language) keyHelp(keys *huh.KeyMap) {
	help := func(binding *key.Binding, doing string) {
		binding.SetHelp(binding.Help().Key, doing)
	}

	help(&keys.Input.AcceptSuggestion, l.text("track.huh.complete", "complete"))
	help(&keys.Input.Prev, l.text("track.huh.back", "back"))
	help(&keys.Input.Next, l.text("track.huh.next", "next"))
	help(&keys.Input.Submit, l.text("track.huh.submit", "submit"))

	help(&keys.Select.Prev, l.text("track.huh.back", "back"))
	help(&keys.Select.Next, l.text("track.huh.select", "select"))
	help(&keys.Select.Submit, l.text("track.huh.submit", "submit"))
	help(&keys.Select.Up, l.text("track.huh.up", "up"))
	help(&keys.Select.Down, l.text("track.huh.down", "down"))
	help(&keys.Select.Left, l.text("track.huh.left", "left"))
	help(&keys.Select.Right, l.text("track.huh.right", "right"))
	help(&keys.Select.Filter, l.text("track.huh.filter", "filter"))
	help(&keys.Select.SetFilter, l.text("track.huh.setFilter", "set filter"))
	help(&keys.Select.ClearFilter, l.text("track.huh.clearFilter", "clear filter"))
	help(&keys.Select.HalfPageUp, l.text("track.huh.halfPageUp", "½ page up"))
	help(&keys.Select.HalfPageDown, l.text("track.huh.halfPageDown", "½ page down"))
	help(&keys.Select.GotoTop, l.text("track.huh.goToStart", "go to start"))
	help(&keys.Select.GotoBottom, l.text("track.huh.goToEnd", "go to end"))

	help(&keys.Confirm.Prev, l.text("track.huh.back", "back"))
	help(&keys.Confirm.Next, l.text("track.huh.next", "next"))
	help(&keys.Confirm.Submit, l.text("track.huh.submit", "submit"))
	help(&keys.Confirm.Toggle, l.text("track.huh.toggle", "toggle"))
	help(&keys.Confirm.Accept, l.text("track.huh.yes", "Yes"))
	help(&keys.Confirm.Reject, l.text("track.huh.no", "No"))
}

// request is the change an answered form asks for, as it will be sent; nil
// where the answer was to change nothing.
func (m screen) request() tea.Cmd {
	ctx, s, a, shown := m.ctx, m.s, *m.asked, m.shown

	var picked *uint
	if a.project != 0 {
		picked = &a.project
	}

	name := s.lang.nameOf(shown.projects, picked)
	body := map[string]any{}

	if picked != nil {
		body["projectId"] = *picked
	}

	if text := strings.TrimSpace(a.description); text != "" {
		body["description"] = text
	}

	var change func() (string, error)

	switch m.doing {
	case "start":
		change = func() (string, error) { return s.startClock(ctx, body, name, shown.zone) }
	case "switch":
		change = func() (string, error) { return s.switchClock(ctx, body, name, shown.projects) }
	case "stop":
		if !a.stop {
			return nil
		}

		change = func() (string, error) { return s.stopClock(ctx, shown.projects) }
	case "book":
		worked, err := s.lang.parseHours(strings.TrimSpace(a.hours))
		if err != nil {
			return func() tea.Msg { return done{err: err} }
		}

		body["date"], body["durationHours"] = strings.TrimSpace(a.day), worked
		change = func() (string, error) { return s.bookHours(ctx, body, name) }
	default:
		return nil
	}

	return func() tea.Msg {
		said, err := change()

		return done{said: said, err: err}
	}
}

// formTheme is huh's base theme in the web interface's colours.
func formTheme(dark bool) *huh.Styles {
	c := tonesFor(dark)
	t := huh.ThemeBase(dark)

	t.Focused.Base = t.Focused.Base.BorderForeground(c.accent)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(c.accent).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(c.accent).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(c.muted)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(c.danger)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(c.danger)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(c.accent)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(c.accent)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(c.accent)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(c.accent)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(c.onAccent).Background(c.accent)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(c.muted).Background(c.line)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(c.accent)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(c.muted)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(c.accent)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = t.Blurred.Title.Foreground(c.muted)
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description

	return t
}
