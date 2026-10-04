//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// Every route the API document says needs a credential refuses a request that
// carries none.
//
// The document's global security list says what a caller must present, and an
// operation marked `"security": []` says it needs nothing. Both are claims, and
// the first is the one that matters: a route that forgot to ask who is calling
// answers anybody, and no case written for that route's own behaviour would
// notice, because every one of them signs in first. So this walks the document
// the server itself serves and sends each operation without a session - through
// a client that has fetched the page, so the CSRF token is there and it is the
// session the request lacks.
//
// Public operations are not asked anything: a sign-in refusing a wrong password
// answers 401 as well, so their answer says nothing about whether they check.
func TestEveryRouteTheDocumentGuardsRefusesACallerWithoutACredential(t *testing.T) {
	t.Parallel()

	a := start(t)
	visitor := a.newClient()

	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal(visitor.do(http.MethodGet, "/openapi.json", nil).Body, &document); err != nil {
		t.Fatalf("the server's API document does not parse: %v", err)
	}

	var asked []string

	for path, operations := range document.Paths {
		for method, raw := range operations {
			method = strings.ToUpper(method)
			if !documentedMethod(method) {
				continue
			}

			var operation struct {
				Security *[]any `json:"security"`
			}

			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatalf("%s %s does not parse: %v", method, path, err)
			}

			if operation.Security != nil && len(*operation.Security) == 0 {
				continue
			}

			var body any
			if method != http.MethodGet && method != http.MethodDelete {
				body = map[string]any{}
			}

			concrete := strings.ReplaceAll(path, "{id}", "1")
			if r := visitor.api(method, concrete, body); r.Status != http.StatusUnauthorized {
				t.Errorf("%s %s answered %d to a caller without a credential, where the document "+
					"says one is needed: %.200s", method, path, r.Status, r.Body)
			}

			asked = append(asked, method+" "+path)
		}
	}

	// A document that stopped parsing into paths would make this pass while
	// asking nothing.
	if len(asked) < 50 {
		sort.Strings(asked)
		t.Fatalf("only %d guarded operations were found in the document: %v", len(asked), asked)
	}
}

// documentedMethod reports an HTTP method, as opposed to the other keys an
// OpenAPI path item may carry.
func documentedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}

	return false
}
