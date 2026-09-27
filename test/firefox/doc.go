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
