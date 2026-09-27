// Package browser drives the real interface in a real browser.
//
// The integration tests prove the API answers correctly. They cannot prove
// anyone can *use* it: whether the sign-in screen actually goes away, whether a
// tab switch shows the right panel, whether a stylesheet rule quietly beats the
// hidden attribute. Those failures leave the API perfectly healthy and the
// application unusable.
//
// That is not hypothetical here. This project shipped a sign-in form that
// authenticated correctly and then left the overlay on screen, because
// `display: flex` on .login-screen won over the browser's own
// `[hidden] { display: none }`. Every API check passed. Only opening it in a
// browser showed it.
//
//	task test:browser
//	go test -tags browser ./test/browser
//
// Needs Chrome, Chromium or Edge. chromedp finds it; set CHROME_PATH if it is
// somewhere unusual.
package browser
