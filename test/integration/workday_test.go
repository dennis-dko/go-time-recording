//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

type workDays struct {
	Days []struct {
		Date         string  `json:"date"`
		Recorded     bool    `json:"recorded"`
		Start        string  `json:"start"`
		End          string  `json:"end"`
		BreakMinutes int     `json:"breakMinutes"`
		WorkedHours  float64 `json:"workedHours"`
		BookedHours  float64 `json:"bookedHours"`
		Notices      []struct {
			Rule    string `json:"rule"`
			Minutes int    `json:"minutes"`
		} `json:"notices"`
	} `json:"days"`
	TotalWorked float64 `json:"totalWorked"`
	TotalBooked float64 `json:"totalBooked"`
}

// A working day is kept as the clock read it and read beside the hours booked on
// it, on every database - with a day only booked on among them - and taken back.
func TestAWorkingDayIsKeptBesideTheHoursBookedOnIt(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	worker.must(worker.api(http.MethodPut, "/me/workdays/2026-08-03", map[string]any{
		"start": "08:00", "end": "16:30", "breakMinutes": 30,
	}), http.StatusOK)

	worker.must(worker.api(http.MethodPut, "/me/workdays/2026-08-05", map[string]any{
		"start": "20:00", "end": "24:00",
	}), http.StatusOK)

	for day, hours := range map[string]float64{"2026-08-03": 2.5, "2026-08-04": 3} {
		worker.must(worker.api(http.MethodPost, "/timesheets", map[string]any{
			"date": day, "durationHours": hours,
		}), http.StatusCreated, http.StatusOK)
	}

	var month workDays

	worker.must(worker.api(http.MethodGet, "/me/workdays?from=2026-08-01&to=2026-08-31", nil),
		http.StatusOK).Data(t, &month)

	if len(month.Days) != 3 {
		t.Fatalf("August reads %+v, want the two recorded days and the one only booked", month.Days)
	}

	first, booked, evening := month.Days[0], month.Days[1], month.Days[2]

	if first.Date != "2026-08-03" || !first.Recorded || first.Start != "08:00" || first.End != "16:30" ||
		first.BreakMinutes != 30 || first.WorkedHours != 8 || first.BookedHours != 2.5 {
		t.Errorf("the 3rd reads %+v", first)
	}

	if booked.Date != "2026-08-04" || booked.Recorded || booked.BookedHours != 3 {
		t.Errorf("the 4th reads %+v, want three hours booked and nothing recorded", booked)
	}

	if evening.End != "24:00" || evening.WorkedHours != 4 {
		t.Errorf("the evening to midnight reads %+v", evening)
	}

	if month.TotalWorked != 12 || month.TotalBooked != 5.5 {
		t.Errorf("August adds up to %.2f worked and %.2f booked, want 12 and 5.5", month.TotalWorked, month.TotalBooked)
	}

	refused := worker.api(http.MethodPut, "/me/workdays/2026-08-06", map[string]any{"start": "09:00", "end": "25:00"})
	if refused.Status != http.StatusBadRequest || !strings.Contains(string(refused.Body), `"end"`) {
		t.Errorf("an end after the day answered %d: %s", refused.Status, refused.Body)
	}

	worker.must(worker.api(http.MethodDelete, "/me/workdays/2026-08-03", nil), http.StatusOK, http.StatusNoContent)

	if got := worker.api(http.MethodDelete, "/me/workdays/2026-08-03", nil).Status; got != http.StatusNotFound {
		t.Errorf("taking back a day with nothing recorded answered %d, want 404", got)
	}
}

// An account's working days go with it, on every database: PostgreSQL refuses the
// deletion outright if one is left pointing at the account.
func TestDeletingAnAccountTakesItsWorkingDaysWithIt(t *testing.T) {
	t.Parallel()

	a, admin, worker := startWithWorker(t)

	var me struct {
		User userResponse `json:"user"`
	}

	worker.must(worker.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	worker.must(worker.api(http.MethodPut, "/me/workdays/2026-08-03", map[string]any{
		"start": "08:00", "end": "16:00",
	}), http.StatusOK)

	admin.must(admin.api(http.MethodDelete, path("/users/", me.User.ID), nil), http.StatusOK, http.StatusNoContent)

	if left := a.Rows(t, "work_days", "user_id = ?", me.User.ID); left != 0 {
		t.Errorf("%d working day(s) outlived the account they belonged to", left)
	}
}

// What the Working Hours Act says about a day travels with it: a day of thirteen
// hours, and the eight hours of rest before the next one, each under the day the
// shift began - and nothing under a day inside every limit.
func TestWhatTheWorkingHoursActSaysTravelsWithTheDay(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	for day, times := range map[string]map[string]any{
		"2026-08-10": {"start": "09:00", "end": "23:00", "breakMinutes": 60},
		"2026-08-11": {"start": "07:00", "end": "15:00", "breakMinutes": 30},
		"2026-08-13": {"start": "08:00", "end": "16:30", "breakMinutes": 30},
	} {
		worker.must(worker.api(http.MethodPut, "/me/workdays/"+day, times), http.StatusOK)
	}

	var month workDays

	worker.must(worker.api(http.MethodGet, "/me/workdays?from=2026-08-01&to=2026-08-31", nil),
		http.StatusOK).Data(t, &month)

	if len(month.Days) != 3 {
		t.Fatalf("August reads %+v, want the three recorded days", month.Days)
	}

	long, short, ordinary := month.Days[0], month.Days[1], month.Days[2]

	if len(long.Notices) != 1 || long.Notices[0].Rule != "longDay" || long.Notices[0].Minutes != 780 {
		t.Errorf("the 10th says %+v, want thirteen hours worked", long.Notices)
	}

	if len(short.Notices) != 1 || short.Notices[0].Rule != "shortRest" || short.Notices[0].Minutes != 480 {
		t.Errorf("the 11th says %+v, want eight hours of rest", short.Notices)
	}

	if ordinary.Notices == nil || len(ordinary.Notices) != 0 {
		t.Errorf("the 13th says %+v, want an empty list rather than none", ordinary.Notices)
	}
}
