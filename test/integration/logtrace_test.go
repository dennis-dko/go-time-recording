//go:build integration

package integration

import (
	"strings"
	"testing"
)

// The identifier a response carries finds what its request wrote in the log.
//
// Every response names its request in X-Correlation-ID, and the log keeps the
// same identifier beside each line that request wrote. Somebody holding one -
// from a failed call, from the browser's network tab - pasted it into the
// search and was shown nothing: the search looked at the text of a line, and
// the readable line the request log is turned into does not say it.
func TestTheCorrelationIDOfAResponseFindsItsLineInTheLog(t *testing.T) {
	t.Parallel()

	a := start(t, "LOG_LEVEL=INFO")
	admin := a.signInAsAdmin("a-much-better-password")

	correlationOf := func(path string) string {
		t.Helper()

		response, err := admin.http.Get(a.BaseURL() + "/api/v1" + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}

		_ = response.Body.Close()

		return response.Header.Get("X-Correlation-ID")
	}

	wanted, another := correlationOf("/me"), correlationOf("/me")
	if wanted == "" || wanted == another {
		t.Fatalf("two requests were named %q and %q; want two names", wanted, another)
	}

	// Its own line in the request log, and not the other request's. Searched for
	// more than once because that line is written after the response has gone.
	// The searches find each other too - the request log records the address
	// each was asked at, which contains what it searched for - and those are
	// lines of other requests, told apart here by the trace they carry.
	var own []string

	if !eventually(func() bool {
		own = own[:0]

		for _, record := range admin.logs(t, "?search="+strings.ToUpper(wanted)).Records {
			if record.TraceID == another {
				t.Errorf("a search for one request found a line of another: %q", record.Message)
			}

			if record.TraceID == wanted {
				own = append(own, record.Message)
			}
		}

		return len(own) > 0
	}) {
		t.Fatalf("a search for the identifier a response carried found none of its lines\n\napplication log:\n%s",
			truncate(a.log(), 2000))
	}

	if len(own) != 1 || !strings.Contains(own[0], "GET /api/v1/me ") {
		t.Errorf("the request left %q under its identifier; want its one line in the request log", own)
	}
}
