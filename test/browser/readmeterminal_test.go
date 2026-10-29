//go:build browser

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/dennis-dko/go-time-recording/test/harness"
)

// terminalColumns and terminalRows are the size of the terminal the README's
// pictures of the client are taken in: narrow enough that the letters can still
// be read where the README shows two pictures side by side, wide enough that
// the stopwatch's line is not cut short, and tall enough for the longest form.
// Rows left empty below are not photographed.
const terminalColumns, terminalRows = 76, 34

// terminalAddress is the installation as the pictures name it. The screen's
// header shows the address the client was given, and the port this instance
// happened to get is not something a reader would type.
const terminalAddress = "http://time.example.com"

// terminal is go-time-recording-client running in a pseudo-terminal, and the
// emulator that keeps what it draws.
//
// The pictures of the terminal are taken the way the browser's are: from the
// program rather than drawn. The client the suite built runs in a real
// pseudo-terminal against the instance the browser pictures come from, a
// terminal emulator keeps the cells it draws, and a browser photographs those
// cells. Nothing on the way knows what the screen is meant to look like, so a
// changed screen is a changed picture.
type terminal struct {
	t      *testing.T
	pty    xpty.Pty
	cmd    *exec.Cmd
	screen *vt.SafeEmulator
	fg, bg color.Color

	mu    sync.Mutex
	title string
}

// openTerminal starts the client for the account token belongs to.
//
// The terminal is light - the light interface's text on its surface - and not
// only to match the README's other pictures. The screen asks the terminal for
// its background and picks its colours by the answer, and through Windows' own
// pseudo-console no answer arrives, so there the screen keeps the light colours
// it starts with. Measured: on a dark terminal the same screen came out in its
// light colours on Windows and its dark ones on Linux. On a light one both draw
// alike, so the picture does not depend on where it was taken. The language is
// English, as the README is, rather than whatever the machine taking the
// pictures is set to.
//
// The instance is reached through the proxy setting, which the client honours,
// so that the client can be given terminalAddress and still arrive at base.
func openTerminal(t *testing.T, base, token string) *terminal {
	t.Helper()

	pty, err := xpty.NewPty(terminalColumns, terminalRows)
	if err != nil {
		t.Fatalf("opening a pseudo-terminal: %v", err)
	}

	term := &terminal{t: t, pty: pty, screen: vt.NewSafeEmulator(terminalColumns, terminalRows),
		fg: color.RGBA{0x1c, 0x21, 0x26, 0xff}, bg: color.RGBA{0xff, 0xff, 0xff, 0xff}}

	fg, bg := term.fg, term.bg
	term.screen.SetDefaultForegroundColor(fg)
	term.screen.SetDefaultBackgroundColor(bg)
	term.screen.SetCallbacks(vt.Callbacks{Title: func(title string) {
		term.mu.Lock()
		term.title = title
		term.mu.Unlock()
	}})

	term.cmd = exec.Command(harness.Client())
	term.cmd.Env = append(os.Environ(), "GTR_URL="+terminalAddress, "GTR_TOKEN="+token, "GTR_LANG=en",
		"HTTP_PROXY="+base, "NO_PROXY=", "TERM=xterm-256color", "COLORTERM=truecolor")

	if err := pty.Start(term.cmd); err != nil {
		_ = pty.Close()
		t.Fatalf("starting the client in the pseudo-terminal: %v", err)
	}

	// What the program draws goes to the emulator, and what the emulator says
	// back - the keys pressed, and its answers to the program's questions about
	// it - goes to the program.
	go func() { _, _ = io.Copy(term.screen, pty) }()
	go func() { _, _ = io.Copy(pty, term.screen) }()

	t.Cleanup(term.close)

	return term
}

// press types text, as somebody at the terminal would.
func (term *terminal) press(text string) {
	term.screen.SendText(text)
}

// await waits until the screen shows every one of words, and a moment longer,
// so that what is photographed is a frame drawn whole.
func (term *terminal) await(words ...string) {
	term.t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		shown := term.snapshot().String()

		missing := ""

		for _, word := range words {
			if !strings.Contains(shown, word) {
				missing = word

				break
			}
		}

		if missing == "" {
			break
		}

		if time.Now().After(deadline) {
			term.t.Fatalf("the terminal never showed %q; it shows:\n%s", missing, shown)
		}

		time.Sleep(100 * time.Millisecond)
	}

	time.Sleep(time.Second)
}

// snapshot is the screen as it stands, every cell of it taken at one moment.
func (term *terminal) snapshot() uv.ScreenBuffer {
	buffer := uv.NewScreenBuffer(terminalColumns, terminalRows)
	term.screen.Draw(buffer, buffer.Bounds())

	return buffer
}

// close asks the client to quit, waits for it a little, and lets the terminal go.
//
// Ctrl-C rather than q, because q is a letter like any other while a form is
// open. And the wait is bounded here rather than by the context WaitProcess
// takes, which it honours only on Windows: elsewhere it waits for the process
// however long that is, and a client still at its form never ends.
func (term *terminal) close() {
	term.press("\x03")

	ended := make(chan struct{})

	go func() {
		_ = xpty.WaitProcess(context.Background(), term.cmd)

		close(ended)
	}()

	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		_ = term.cmd.Process.Kill()

		<-ended
	}

	_ = term.pty.Close()
	_ = term.screen.Close()
}

// photograph takes the screen as it stands, in a window of its own carrying the
// title the program gave it.
func (term *terminal) photograph() []byte {
	term.t.Helper()

	term.mu.Lock()
	title := term.title
	term.mu.Unlock()

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), launch("en-US")...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var picture []byte

	// Transparent around the window, so the picture sits on the page it is
	// shown on whichever colours that page has.
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 1024),
		emulation.SetDefaultBackgroundColorOverride().WithColor(&cdp.RGBA{}),
		chromedp.Navigate("about:blank"),
		setContent(terminalPage(term.snapshot(), title, term.fg, term.bg)),
		chromedp.WaitVisible(".window", chromedp.ByQuery),
		photograph(".window", &picture),
	); err != nil {
		term.t.Fatalf("photographing the terminal: %v", err)
	}

	return picture
}

// setContent replaces the blank page's document with page, handed over as a
// JSON string because that is also a script's string.
func setContent(page string) chromedp.Action {
	literal, _ := json.Marshal(page)

	return chromedp.Evaluate(`document.open(); document.write(`+string(literal)+`); document.close(); true`, nil)
}

// terminalPage is a screen as a page a browser can photograph: one span for each
// run of cells that look alike, in the colours the program asked for, on the
// terminal's own. The rows below the last one drawn on are left out but one.
func terminalPage(screen uv.ScreenBuffer, title string, fg, bg color.Color) string {
	last := 0

	for y := range screen.Height() {
		if strings.TrimSpace(screen.Line(y).String()) != "" {
			last = y
		}
	}

	var rows strings.Builder

	for y := 0; y <= min(last+1, screen.Height()-1); y++ {
		var run strings.Builder

		style := ""

		flush := func() {
			if run.Len() > 0 {
				fmt.Fprintf(&rows, `<span style="%s">%s</span>`, style, html.EscapeString(run.String()))
				run.Reset()
			}
		}

		for x := 0; x < screen.Width(); x++ {
			cell := screen.CellAt(x, y)
			if cell != nil && cell.IsZero() {
				continue
			}

			content, look := " ", cellStyle(nil, fg, bg)
			if cell != nil {
				if cell.Content != "" {
					content = cell.Content
				}

				look = cellStyle(&cell.Style, fg, bg)
			}

			if look != style {
				flush()

				style = look
			}

			run.WriteString(content)
		}

		flush()
		rows.WriteString("\n")
	}

	bar := ""
	if title != "" {
		bar = `<div class="bar">` + html.EscapeString(title) + `</div>`
	}

	return fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><style>
html, body { margin: 0; background: transparent; }
body { padding: 24px; }
.window { display: inline-block; width: max-content; border-radius: 10px; overflow: hidden; background: %[1]s; color: %[2]s;
  box-shadow: 0 0 0 1px rgba(128, 128, 128, 0.35), 0 8px 24px rgba(0, 0, 0, 0.18); }
.bar { padding: 8px 16px; font: 13px/1.3 "Segoe UI", system-ui, sans-serif; text-align: center;
  opacity: 0.7; border-bottom: 1px solid rgba(128, 128, 128, 0.3); }
pre { margin: 0; padding: 4px 8px 8px; font: 15px/1.2 Consolas, "Cascadia Mono", "DejaVu Sans Mono", monospace; }
</style></head><body><div class="window">%[3]s<pre>%[4]s</pre></div></body></html>`,
		hexColor(bg), hexColor(fg), bar, rows.String())
}

// cellStyle is how one cell is written: its colours, falling back on the
// terminal's, and what of its attributes a page can show.
func cellStyle(style *uv.Style, fg, bg color.Color) string {
	if style == nil {
		return ""
	}

	front, back := fg, bg
	if style.Fg != nil {
		front = style.Fg
	}

	if style.Bg != nil {
		back = style.Bg
	}

	if style.Attrs&uv.AttrReverse != 0 {
		front, back = back, front
	}

	var css []string

	if hexColor(front) != hexColor(fg) {
		css = append(css, "color:"+hexColor(front))
	}

	if hexColor(back) != hexColor(bg) {
		css = append(css, "background:"+hexColor(back))
	}

	if style.Attrs&uv.AttrBold != 0 {
		css = append(css, "font-weight:700")
	}

	if style.Attrs&uv.AttrFaint != 0 {
		css = append(css, "opacity:0.6")
	}

	if style.Attrs&uv.AttrItalic != 0 {
		css = append(css, "font-style:italic")
	}

	switch {
	case style.Underline != 0 && style.Attrs&uv.AttrStrikethrough != 0:
		css = append(css, "text-decoration:underline line-through")
	case style.Underline != 0:
		css = append(css, "text-decoration:underline")
	case style.Attrs&uv.AttrStrikethrough != 0:
		css = append(css, "text-decoration:line-through")
	}

	return strings.Join(css, ";")
}

// hexColor writes a colour the way a style sheet does.
func hexColor(c color.Color) string {
	r, g, b, _ := c.RGBA()

	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
