package cli

import (
	"fmt"
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
	a := m.asked
	running := nameOf(m.shown.projects, m.shown.clock.ProjectID)

	var group *huh.Group

	switch doing {
	case "start":
		group = huh.NewGroup(m.projectField(&a.project), describe(&a.description)).
			Title("Start the stopwatch")
	case "switch":
		group = huh.NewGroup(m.projectField(&a.project), describe(&a.description)).
			Title("Switch to another project").
			Description(fmt.Sprintf("What has run so far is booked on %s.", running))
	case "stop":
		group = huh.NewGroup(huh.NewConfirm().Key("stop").
			Title(fmt.Sprintf("Book %s on %s?", hoursAndMinutes(m.elapsed().Hours()), running)).
			Affirmative("Stop and book").Negative("Keep it running").Value(&a.stop)).
			Title("Stop the stopwatch")
	default:
		group = huh.NewGroup(
			huh.NewInput().Key("hours").Title("Hours").Placeholder("1.5, 1,5 or 1:30").
				Validate(readableHours).Value(&a.hours),
			m.projectField(&a.project),
			huh.NewInput().Key("day").Title("Day").Placeholder("YYYY-MM-DD").
				Validate(readableDay).Value(&a.day),
			describe(&a.description),
		).Title("Book time by hand")
	}

	// Escape puts a form away; ctrl+c, which huh would use, closes the screen.
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))

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
	options := []huh.Option[uint]{huh.NewOption("No project", uint(0))}

	for _, p := range m.shown.projects {
		if p.Status == "active" {
			options = append(options, huh.NewOption(p.Name, p.ID))
		}
	}

	field := huh.NewSelect[uint]().Key("project").Title("Project").Options(options...).Value(into)

	// The height counts the title's line.
	if len(options) > projectRows {
		field = field.Height(projectRows + 1)
	}

	return field
}

// describe asks what the time went on, which may be nothing.
func describe(into *string) *huh.Input {
	return huh.NewInput().Key("description").Title("Description").Placeholder("optional").
		CharLimit(model.MaxDescriptionLength).Value(into)
}

// readableHours refuses hours written in none of the ways parseHours reads.
func readableHours(raw string) error {
	_, err := parseHours(strings.TrimSpace(raw))

	return err
}

// request is the change an answered form asks for, as it will be sent; nil
// where the answer was to change nothing.
func (m screen) request() tea.Cmd {
	ctx, s, a, shown := m.ctx, m.s, *m.asked, m.shown

	var picked *uint
	if a.project != 0 {
		picked = &a.project
	}

	name := nameOf(shown.projects, picked)
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
		worked, err := parseHours(strings.TrimSpace(a.hours))
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
