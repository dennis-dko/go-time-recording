//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The framework measures the machinery: a histogram per HTTP request, one per
// SQL query, a gauge of goroutines. None of it says whether the application is
// doing its job - a deployment can serve every request in milliseconds while
// nobody has been able to book time since the directory changed.
//
// So these check the four this application records itself, on the endpoint an
// operator actually scrapes. Reading them back from there rather than asserting
// on the call is the point: a metric that is recorded but never registered, or
// registered under a name nothing exports, is indistinguishable from a working
// one until somebody writes an alert against it.

// scrape reads the metrics endpoint of the instance under test.
//
// The port is the one the harness gave this instance, which is not the one the
// application serves its API on - metrics live on a listener of their own,
// outside the middleware chain, which is also why no session is needed here.
func scrape(t *testing.T, a *app) string {
	t.Helper()

	return get(t, fmt.Sprintf("http://localhost:%d/metrics", a.MetricsPort()))
}

// declares reports whether the scrape carries the metric at all, which is a
// different question from what its value is.
func declares(scraped, metric string) bool {
	return strings.Contains(scraped, metric)
}

// Registering a metric is not publishing it, and this pins the difference so
// nobody writes an alert on the assumption it works the other way.
//
// The registration creates the instrument; the exporter emits a series only once
// it has a value. So a fresh installation publishes none of these, and "nothing
// has gone wrong yet" and "this metric does not exist" are the same empty query
// result - which an alert has to treat as absent rather than as a healthy zero.
//
// This test exists because the opposite was assumed while writing them, and the
// endpoint said otherwise.
func TestAMetricIsPublishedOnlyOnceItHasAValue(t *testing.T) {
	a := start(t)

	if scraped := scrape(t, a); declares(scraped, "gtr_signin_failures_total") {
		t.Error("a counter with no value is published after all - which would be better, " +
			"and means the comment in registerBusinessMetrics is now wrong")
	}

	refused := a.newClient()
	refused.api(http.MethodPost, "/auth/login", map[string]string{
		"email": adminEmail, "password": "not-the-password",
	})

	if !eventually(func() bool {
		return declares(scrape(t, a), "gtr_signin_failures_total")
	}) {
		t.Error("the counter is still absent after the event it counts")
	}
}

// The one the installation exists to record.
func TestBookingTimeIsCounted(t *testing.T) {
	a, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
		"date": "2026-08-03", "durationHours": 6,
	}), http.StatusCreated, http.StatusOK)

	scraped := scrape(t, a)

	// The histogram's sum carries the hours and its count carries the entries,
	// so both questions - how much was recorded, and in how many pieces - are
	// answered by the one metric.
	if !declares(scraped, "gtr_timesheet_hours_booked_sum") {
		t.Fatalf("the hours histogram has no sum:\n%s", metricLines(scraped, "gtr_timesheet"))
	}

	if !declares(scraped, "gtr_timesheet_hours_booked_count") {
		t.Errorf("the hours histogram has no count:\n%s", metricLines(scraped, "gtr_timesheet"))
	}

}

// Either somebody is working through a password list, or the directory has
// stopped answering and is turning away people whose passwords are fine. The
// label is what tells those apart.
func TestARefusedSignInIsCountedWithItsReason(t *testing.T) {
	a := start(t)

	// No session needed, and deliberately so: this is the endpoint anybody can
	// reach, which is why counting what it refuses is worth doing.
	refused := a.newClient()
	refused.api(http.MethodPost, "/auth/login", map[string]string{
		"email": adminEmail, "password": "not-the-password",
	})

	if !eventually(func() bool {
		return strings.Contains(scrape(t, a), `reason="credentials"`)
	}) {
		t.Errorf("a refused sign-in was not counted under its reason:\n%s",
			metricLines(scrape(t, a), "gtr_signin"))
	}
}

// metricLines pulls the matching lines out of a scrape, so a failure shows what
// was published instead of the whole exposition.
func metricLines(scraped, prefix string) string {
	var found []string

	for _, line := range strings.Split(scraped, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}

	if len(found) == 0 {
		return "(nothing published under " + prefix + ")"
	}

	return strings.Join(found, "\n")
}
