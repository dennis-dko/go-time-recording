//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// The statistics total the activities the descriptions name, most hours first,
// beside the hours that name none.
func TestTheStatisticsTotalTheActivitiesTheDescriptionsName(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	for description, hours := range map[string]float64{
		"Planning #meeting":          1.5,
		"#Meeting about the #review": 0.5,
		"Parser #review, see abc#12": 3,
		"Quiet work on ticket #123":  2,
	} {
		worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
			"date": "2026-08-04", "durationHours": hours, "description": description,
		}), http.StatusCreated, http.StatusOK)
	}

	var stats struct {
		TotalHours    float64 `json:"totalHours"`
		UntaggedHours float64 `json:"untaggedHours"`
		Tags          []struct {
			Tag   string  `json:"tag"`
			Hours float64 `json:"hours"`
		} `json:"tags"`
	}

	worker.must(worker.api(http.MethodGet, "/me/statistics?from=2026-08-03&to=2026-08-09", nil),
		http.StatusOK).Data(t, &stats)

	if len(stats.Tags) != 2 || stats.Tags[0].Tag != "review" || stats.Tags[0].Hours != 3.5 ||
		stats.Tags[1].Tag != "meeting" || stats.Tags[1].Hours != 2 {
		t.Errorf("the activities read %+v, want review 3.5 and meeting 2", stats.Tags)
	}

	if stats.UntaggedHours != 2 || stats.TotalHours != 7 {
		t.Errorf("%.2f of %.2f hours name no activity, want 2 of 7", stats.UntaggedHours, stats.TotalHours)
	}
}
