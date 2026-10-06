//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/support/spreadsheet"
)

// A project given no start date begins on the day it was created in its owner's
// zone, whichever way it was created.
//
// The form fills the start with the reader's own today. A request without one -
// the field cleared, a script, an imported row whose start cell is empty - left
// it to the service, which took the date of the server's clock: in the evening
// west of UTC a project began tomorrow, and one given an end date of today was
// refused as ending before it began.
func TestAProjectWithoutAStartBeginsOnItsOwnersDay(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	zone, today := aZoneWhoseDayIsNotTheServers(t)

	worker.must(worker.api(http.MethodPut, "/me/timezone",
		map[string]string{"timezone": zone}), http.StatusOK)

	var typed struct {
		StartDate string `json:"startDate"`
	}

	worker.must(worker.api(http.MethodPost, "/projects", map[string]any{
		"name": "Typed without a start", "endDate": today,
	}), http.StatusCreated, http.StatusOK).Data(t, &typed)

	if typed.StartDate != today {
		t.Errorf("a project created on %s in %s, with no start, begins on %s", today, zone, typed.StartDate)
	}

	book, err := spreadsheet.WriteProjects("", []spreadsheet.ProjectRow{
		{Name: "Imported without a start", Status: "active"},
	})
	if err != nil {
		t.Fatalf("building the workbook: %v", err)
	}

	if result, r := importSheet(t, worker, "/projects/import", book, "false"); !accepted(r.Status) || result.Imported != 1 {
		t.Fatalf("importing: status %d, %d imported, body %.300q", r.Status, result.Imported, r.Body)
	}

	var listed listOf[struct {
		Name      string `json:"name"`
		StartDate string `json:"startDate"`
	}]

	worker.must(worker.api(http.MethodGet, "/projects", nil), http.StatusOK).Data(t, &listed)

	for _, project := range listed.Items {
		if project.Name == "Imported without a start" && project.StartDate != today {
			t.Errorf("a project imported on %s in %s, with no start, begins on %s", today, zone, project.StartDate)
		}
	}
}

// aZoneWhoseDayIsNotTheServers names a zone whose date is not the server's at
// this moment, and that date. The server runs on this machine and reads its clock
// in the same zone as this process; of fourteen hours ahead of UTC and eleven
// behind, one always has another date than any zone in between.
func aZoneWhoseDayIsNotTheServers(t *testing.T) (string, string) {
	t.Helper()

	server := time.Now().Format(time.DateOnly)

	for _, name := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		zone, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}

		if day := time.Now().In(zone).Format(time.DateOnly); day != server {
			return name, day
		}
	}

	t.Fatal("neither zone has another date than the server's, so this case would prove nothing")

	return "", ""
}
