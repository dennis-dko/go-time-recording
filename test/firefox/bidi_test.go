//go:build firefox

// Package firefox drives the interface in a second engine.
//
// The chromedp suite proves the interface works in Blink, which covers Chrome
// and Edge, because Edge is Chromium. It cannot say anything about Gecko or
// WebKit, and the difference is not academic: bulk deletion was invisible in
// Firefox for as long as it existed, while every Chrome test passed. One CSS
// rule, `input { width: 100% }`, reaching a checkbox inside a table cell that had
// been asked to be as narrow as its content. Firefox resolved that percentage
// against a cell with no width yet and made the box 0px wide; Chrome resolved it
// the other way and drew it correctly. Nothing was wrong with the markup, the
// script, or the API - the box was simply not there, in one engine, for one
// browser's users.
//
// No engine can be checked by reasoning about the other. So this runs the same
// application in Firefox and looks.
//
// It speaks WebDriver BiDi to Firefox directly over a WebSocket. Firefox opens
// that port itself with --remote-debugging-port, so there is no geckodriver to
// install and nothing to keep in step with the browser version.
//
//	task test:firefox
//	go test -tags firefox ./test/firefox
//
// Needs Firefox. Set FIREFOX_PATH if it is somewhere unusual.
package firefox

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// callTimeout bounds one command. A cold browser start lands inside it.
const callTimeout = 60 * time.Second

// browser is a headless Firefox and the one page it has open.
type browser struct {
	t    *testing.T
	conn *websocket.Conn

	// context is the browsing context - BiDi's name for the tab.
	context string

	// profile is where this Firefox keeps everything it decided, including which
	// icon it adopted for a page. That is the only place the answer exists: what a
	// tab draws is not in the document.
	profile string

	// next numbers the commands. Every reply carries the id it answers, so
	// replies can be told apart from the events that arrive between them.
	next int
}

// firefoxPath is where Firefox is, or an empty string if this machine has none.
func firefoxPath() string {
	if set := os.Getenv("FIREFOX_PATH"); set != "" {
		return set
	}

	candidates := map[string][]string{
		"windows": {
			`C:\Program Files\Mozilla Firefox\firefox.exe`,
			`C:\Program Files (x86)\Mozilla Firefox\firefox.exe`,
		},
		"darwin": {"/Applications/Firefox.app/Contents/MacOS/firefox"},
		"linux":  {"/usr/bin/firefox", "/usr/local/bin/firefox", "/snap/bin/firefox"},
	}

	for _, path := range candidates[runtime.GOOS] {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Whatever is on PATH, which is how a CI runner usually has it.
	if path, err := exec.LookPath("firefox"); err == nil {
		return path
	}

	return ""
}

// openBrowser starts Firefox and connects to it.
func openBrowser(t *testing.T) *browser {
	t.Helper()

	exe := firefoxPath()
	if exe == "" {
		t.Skip("no Firefox on this machine; set FIREFOX_PATH to run this suite")
	}

	b := &browser{t: t}
	port := freePort(t)

	// Its own profile, thrown away with the test: a shared one carries cookies,
	// a remembered window size and a session store, and this suite is about what
	// a browser does with a fresh page.
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profile, 0o750); err != nil {
		t.Fatal(err)
	}

	b.profile = profile

	cmd := exec.Command(exe, "--headless",
		"--remote-debugging-port", fmt.Sprint(port),
		"--profile", profile,
		// Pinned, for the same reason the Chrome suite pins it: a figure written
		// 1,11 by a German machine and 1.11 by an American one is otherwise a
		// test that passes depending on who runs it.
		"--lang", "en-US",
		"--window-size", "1280,900",
		"about:blank")

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting Firefox: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Firefox opens the port a moment after the process exists.
	deadline := time.Now().Add(45 * time.Second)
	for b.conn == nil {
		if time.Now().After(deadline) {
			t.Fatal("Firefox never opened its remote port")
		}

		conn, _, err := websocket.DefaultDialer.Dial(
			fmt.Sprintf("ws://127.0.0.1:%d/session", port), nil)
		if err == nil {
			b.conn = conn

			break
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Cleanup(func() { _ = b.conn.Close() })

	b.call("session.new", map[string]any{"capabilities": map[string]any{}})
	b.context = b.firstContext()

	return b
}

// freePort asks the operating system for one nobody is using, so two packages
// running at once do not collide on a hard-coded number.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port
}

// call sends one command and returns the reply to it.
func (b *browser) call(method string, params map[string]any) map[string]any {
	b.t.Helper()

	b.next++
	id := b.next

	if err := b.conn.WriteJSON(map[string]any{
		"id": id, "method": method, "params": params,
	}); err != nil {
		b.t.Fatalf("%s: %v", method, err)
	}

	if err := b.conn.SetReadDeadline(time.Now().Add(callTimeout)); err != nil {
		b.t.Fatal(err)
	}

	// Events and replies share the socket, so anything that is not the answer to
	// this command is skipped rather than mistaken for it.
	for {
		var msg map[string]any

		if err := b.conn.ReadJSON(&msg); err != nil {
			b.t.Fatalf("%s: %v", method, err)
		}

		got, ok := msg["id"].(float64)
		if !ok || int(got) != id {
			continue
		}

		if msg["type"] == "error" {
			body, _ := json.Marshal(msg)
			b.t.Fatalf("%s: %.500s", method, body)
		}

		return msg
	}
}

// firstContext is the tab Firefox opened at start-up.
func (b *browser) firstContext() string {
	b.t.Helper()

	tree := b.call("browsingContext.getTree", map[string]any{})

	result, _ := tree["result"].(map[string]any)
	contexts, _ := result["contexts"].([]any)

	if len(contexts) == 0 {
		b.t.Fatal("Firefox has no tab open")
	}

	first, _ := contexts[0].(map[string]any)
	context, _ := first["context"].(string)

	return context
}

// goTo loads a page and waits for it to finish loading.
func (b *browser) goTo(url string) {
	b.t.Helper()

	b.call("browsingContext.navigate", map[string]any{
		"context": b.context, "url": url, "wait": "complete",
	})
}

// eval runs an expression in the page and returns what it produced.
//
// Promises are awaited, so an async expression can be written as one.
func (b *browser) eval(expression string) any {
	b.t.Helper()

	out := b.call("script.evaluate", map[string]any{
		"expression":      expression,
		"target":          map[string]any{"context": b.context},
		"awaitPromise":    true,
		"resultOwnership": "none",
	})

	result, _ := out["result"].(map[string]any)

	if result["type"] == "exception" {
		body, _ := json.Marshal(result["exceptionDetails"])
		b.t.Fatalf("the page threw: %.500s\n\nwhile evaluating:\n%s", body, expression)
	}

	value, _ := result["result"].(map[string]any)

	return value["value"]
}

// evalString is eval where a string is expected.
func (b *browser) evalString(expression string) string {
	b.t.Helper()

	got, _ := b.eval(expression).(string)

	return got
}

// evalJSON runs an expression that returns JSON and unpacks it.
//
// BiDi hands back structured values, and walking a map[string]any of them in Go
// is far less readable than the thing being asserted deserves. The page stringifies,
// this unpacks, and the test reads like a test.
func (b *browser) evalJSON(expression string, into any) {
	b.t.Helper()

	raw := b.evalString(expression)

	if err := json.Unmarshal([]byte(raw), into); err != nil {
		b.t.Fatalf("reading the page's answer: %v\n\n%.400s", err, raw)
	}
}

// settle waits for the interface to catch up with whatever was just asked of it.
//
// The interface answers requests to draw itself, so there is nothing to wait for
// that is true of every case. Kept as one call so the waiting is named rather
// than scattered as bare sleeps.
//
// Where there *is* something to wait for, waitFor is the one to use: a fixed
// sleep is a guess about somebody else's machine, and this suite failed on a
// loaded runner for exactly that reason - the sign-in had not finished inside the
// guess, and the case reported it as a sign-in that does not work.
func (b *browser) settle() {
	time.Sleep(1200 * time.Millisecond)
}

// waitFor blocks until an expression in the page is true.
//
// Sixty seconds, which is long next to anything this waits for and short next to
// a case that hangs. It was ten, then thirty, and both were guesses about the
// machine running it: every case here starts its own instance, so a sign-in on a
// loaded runner includes a first migration - and the case then reported a sign-in
// that had not finished as a sign-in that does not work.
//
// A deadline on a condition costs nothing when the condition is met, which is
// the argument for making it generous rather than tight.
func (b *browser) waitFor(expression, complaint string) {
	b.t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		if got, _ := b.eval(expression).(bool); got {
			return
		}

		if time.Now().After(deadline) {
			b.t.Fatalf("%s (waiting for %s)", complaint, expression)
		}

		time.Sleep(150 * time.Millisecond)
	}
}
