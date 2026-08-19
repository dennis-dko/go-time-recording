package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// No form may fall back to GET, because GET puts what was typed in the URL.
//
// Every form here is submitted by script: a listener calls preventDefault and
// sends the values as JSON. So the method attribute is never used - until the
// one time it is, and that is exactly the time it matters.
//
// A form with no method is a GET form. Submitted natively - because the script
// did not load, or threw before it wired that form, or an old cached copy is
// missing a listener a newer page expects - the browser serialises every named
// field into the query string. Five of these hold a password field: signing in,
// changing a password, creating a user, the database connection, the directory
// bind. Measured, before this was fixed, against a running instance:
//
//	uri: /?email=dennis%40example.com&password=hunter2-the-secret
//
// That is the address bar, the browser's history, the application's own log at
// INFO - which is the default level, and which the log viewer inside the
// application shows to administrators - any reverse proxy's access log, and,
// where tracing is on, the traces.
//
// POST puts them in a body instead. Nothing here answers a POST to those paths,
// so a native submit becomes a refusal rather than a disclosure.
func TestNoFormCanFallBackToGet(t *testing.T) {
	markup := asset(t, "/")

	forms := regexp.MustCompile(`<form([^>]*)>`).FindAllStringSubmatch(markup, -1)
	if len(forms) == 0 {
		t.Fatal("no forms found in the markup, so this guard is checking nothing")
	}

	for _, form := range forms {
		attrs := form[1]

		if !strings.Contains(attrs, `method="post"`) {
			name := "a form"
			if id := regexp.MustCompile(`id="([^"]+)"`).FindStringSubmatch(attrs); id != nil {
				name = id[1]
			}

			t.Errorf("%s has no method, so it is a GET form: submitted without its "+
				"script it would put every field it holds into the URL", name)
		}
	}
}
