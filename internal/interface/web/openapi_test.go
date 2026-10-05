package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/interface/api/v1/rest"
)

// The published API description and the routes that exist are the same list.
//
// It had drifted in both directions by the time this was written: six endpoints
// nobody had written down - including a whole pair for moving spreadsheets in and
// out - and one described in detail that had been removed, so anybody following the
// documentation got a 404 for it.
//
// Neither kind of drift announces itself. The description is served to whoever
// clicks "API documentation", and it is read by people who cannot see the router.

// routeExpression finds the registrations in the router.
var routeExpression = regexp.MustCompile(`app\.(GET|POST|PUT|DELETE|PATCH)\(base\+"([^"]+)"`)

// registeredRoutes reads the routes out of the router source.
//
// From the source rather than by starting the application: a route table is what
// the file says, and reading it needs no database, no port and no configuration.
func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()

	// The test's own directory is internal/interface/web, and the router is two
	// levels up in api/v1.
	source, err := os.ReadFile(filepath.Join("..", "api", "v1", "router.go"))
	if err != nil {
		t.Fatalf("reading the router: %v", err)
	}

	found := map[string]bool{}

	for _, match := range routeExpression.FindAllStringSubmatch(string(source), -1) {
		found[match[1]+" "+match[2]] = true
	}

	if len(found) == 0 {
		t.Fatal("no routes found in the router; the registration shape changed and this " +
			"guard no longer guards anything")
	}

	return found
}

// documentedRoutes reads the operations out of the description that is served.
func documentedRoutes(t *testing.T) map[string]bool {
	t.Helper()

	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal([]byte(asset(t, "/openapi.json")), &document); err != nil {
		t.Fatalf("the served openapi.json is not valid JSON: %v", err)
	}

	if len(document.Paths) == 0 {
		t.Fatal("openapi.json describes no paths")
	}

	methods := map[string]bool{
		"get": true, "post": true, "put": true, "delete": true, "patch": true,
	}

	found := map[string]bool{}

	for path, operations := range document.Paths {
		for method := range operations {
			if methods[strings.ToLower(method)] {
				found[strings.ToUpper(method)+" "+path] = true
			}
		}
	}

	return found
}

func TestEveryRouteIsDescribedAndEveryDescriptionIsARoute(t *testing.T) {
	routes := registeredRoutes(t)
	described := documentedRoutes(t)

	var undocumented, imaginary []string

	for route := range routes {
		if !described[route] {
			undocumented = append(undocumented, route)
		}
	}

	for route := range described {
		if !routes[route] {
			imaginary = append(imaginary, route)
		}
	}

	sort.Strings(undocumented)
	sort.Strings(imaginary)

	if len(undocumented) > 0 {
		t.Errorf("%d route(s) the application serves are missing from openapi.json: %v",
			len(undocumented), undocumented)
	}

	if len(imaginary) > 0 {
		t.Errorf("%d operation(s) in openapi.json have no route, so anybody following "+
			"the documentation gets a 404: %v", len(imaginary), imaginary)
	}
}

// The description says 429 exactly where the rate limiter counts a request.
//
// The two are kept in different places - the limiter decides by path in the rest
// package, the description is a file somebody edits by hand - and the start of a
// passkey sign-in was counted for a while before anybody would have thought to
// write it down. Asked of the limiter itself rather than of a list, so a sign-in
// added to it is held to the description without being named here. Requests are
// sent without a token, which the limiter counts on every route.
func TestTheDescriptionSaysTooManyRequestsWhereverTheLimiterCounts(t *testing.T) {
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal([]byte(asset(t, "/openapi.json")), &document); err != nil {
		t.Fatalf("the served openapi.json is not valid JSON: %v", err)
	}

	methods := map[string]bool{
		"get": true, "post": true, "put": true, "delete": true, "patch": true,
	}

	parameter := regexp.MustCompile(`\{[^}]+\}`)
	counted := 0

	for path, entries := range document.Paths {
		for method, raw := range entries {
			// A path also carries what its operations share, the parameters.
			if !methods[strings.ToLower(method)] {
				continue
			}

			method = strings.ToUpper(method)

			var operation struct {
				Responses map[string]json.RawMessage `json:"responses"`
			}

			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatalf("%s %s is not an operation: %v", method, path, err)
			}

			limiter := rest.NewRateLimiter(1, time.Minute).Middleware()(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

			var last int

			for range 2 {
				r := httptest.NewRequest(method, "/api/v1"+parameter.ReplaceAllString(path, "1"), nil)
				r.RemoteAddr = "192.0.2.12:4000"

				w := httptest.NewRecorder()
				limiter.ServeHTTP(w, r)
				last = w.Code
			}

			limited := last == http.StatusTooManyRequests
			_, documented := operation.Responses["429"]

			if limited {
				counted++
			}

			switch {
			case limited && !documented:
				t.Errorf("%s %s is counted by the rate limiter and its description never says it can answer 429",
					method, path)
			case documented && !limited:
				t.Errorf("%s %s is described as answering 429, and the rate limiter does not count it",
					method, path)
			}
		}
	}

	if counted == 0 {
		t.Fatal("the rate limiter counted none of the described routes; the way it decides changed and this " +
			"guard no longer guards anything")
	}
}
