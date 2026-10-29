package test

import (
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// changesNothing are the routes under accounts, roles, settings and the setup that
// the administration log leaves out, each because it changes nothing about how the
// installation is run.
var changesNothing = map[string]string{
	"POST /api/v1/settings/ldap/test":         "tries a connection and keeps nothing",
	"POST /api/v1/settings/openid/test":       "reads a provider's discovery document and keeps nothing",
	"POST /api/v1/settings/ldap/sync/preview": "reports what a run would do, without doing it",
	"POST /api/v1/settings/datasource/test":   "tries a connection and keeps nothing",
	"POST /api/v1/settings/update/check":      "asks whether a newer release exists",
	"POST /api/v1/setup/claim":                "signs in the browser that answered the installer, which is a sign-in",
	"PUT /api/v1/users/{id}/working-times":    "a person's own figures, which only their owner sets",
}

// Every route that changes accounts, roles or settings is in the administration
// log, or named above as one that changes nothing.
//
// The log is a middleware with a table of routes, so a handler cannot forget to
// record its change - and the table is what can be forgotten instead. A route
// added to the router and not to the table would change the installation without
// a trace, which is the one thing the log exists to prevent, and nothing about the
// route itself would say so.
func TestEveryAdministrativeChangeIsInTheAdministrationLog(t *testing.T) {
	router := read(t, filepath.Join("..", "internal", "interface", "api", "v1", "router.go"))
	table := read(t, filepath.Join("..", "internal", "interface", "api", "v1", "rest", "adminlog.go"))

	var served []string

	for _, match := range regexp.MustCompile(`app\.(POST|PUT|DELETE)\(base\+"(/(?:users|roles|settings|setup)[^"]*)"`).
		FindAllStringSubmatch(router, -1) {
		served = append(served, match[1]+" /api/v1"+match[2])
	}

	var logged []string

	for _, match := range regexp.MustCompile(`\{http\.Method(Post|Put|Delete), "([^"]+)", "[^"]+", "[^"]*"\}`).
		FindAllStringSubmatch(table, -1) {
		method := map[string]string{"Post": "POST", "Put": "PUT", "Delete": "DELETE"}[match[1]]
		logged = append(logged, method+" "+match[2])
	}

	if len(served) < 20 || len(logged) < 20 {
		t.Fatalf("read %d changing routes and %d logged ones, so a scan has stopped reading", len(served), len(logged))
	}

	for _, route := range served {
		if _, nothing := changesNothing[route]; !nothing && !slices.Contains(logged, route) {
			t.Errorf("%s changes how the installation is run and the administration log does not record it", route)
		}
	}

	for _, route := range logged {
		if !slices.Contains(served, route) {
			t.Errorf("the administration log records %s, which the router does not serve", route)
		}
	}

	for route := range changesNothing {
		if !slices.Contains(served, route) {
			t.Errorf("%s is named as changing nothing and the router does not serve it; drop the row", route)
		}
	}
}

// Every change the administration log records is shown as a sentence.
//
// The screen words a change by its code, from a table of its own; a code the
// table does not know is shown as the code, which is how "settings.telemetry"
// would come to stand in the middle of a German screen. So every code in the
// log's table has its sentence, and the screen's table holds no code the log
// never records.
func TestEveryRecordedChangeHasItsSentence(t *testing.T) {
	table := read(t, filepath.Join("..", "internal", "interface", "api", "v1", "rest", "adminlog.go"))
	script := read(t, filepath.Join("..", "internal", "interface", "web", "asset", "app.js"))

	var recorded []string

	for _, match := range regexp.MustCompile(`\{http\.Method\w+, "[^"]+", "([^"]+)", "[^"]*"\}`).
		FindAllStringSubmatch(table, -1) {
		recorded = append(recorded, match[1])
	}

	sentences := regexp.MustCompile(`(?s)const CHANGE_SENTENCES = \{(.*?)\n\};`).FindStringSubmatch(script)
	if sentences == nil || len(recorded) < 20 {
		t.Fatalf("found %d recorded changes and the sentences %v, so a scan has stopped reading", len(recorded), sentences != nil)
	}

	var worded []string

	for _, match := range regexp.MustCompile(`'([\w.]+)': \(\) =>`).FindAllStringSubmatch(sentences[1], -1) {
		worded = append(worded, match[1])
	}

	for _, action := range recorded {
		if !slices.Contains(worded, action) {
			t.Errorf("the administration log records %s and the screen has no sentence for it", action)
		}
	}

	for _, action := range worded {
		if !slices.Contains(recorded, action) {
			t.Errorf("the screen has a sentence for %s, which the administration log never records", action)
		}
	}
}
