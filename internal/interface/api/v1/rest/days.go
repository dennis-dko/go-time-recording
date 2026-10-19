package rest

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// DayResponse is one day: what was booked on it and what it asked, and why it
// asks less where somebody is away.
type DayResponse struct {
	Date    Date    `json:"date"`
	Booked  float64 `json:"booked"`
	Target  float64 `json:"target"`
	Absence string  `json:"absence,omitempty"`
}

// DaysResponse is every day of a range, and what they add up to.
type DaysResponse struct {
	From        Date          `json:"from"`
	To          Date          `json:"to"`
	Days        []DayResponse `json:"days"`
	TotalBooked float64       `json:"totalBooked"`
	TotalTarget float64       `json:"totalTarget"`
}

// Days handles GET /api/v1/me/days: every day of a range, the empty ones
// included, with what was booked on it and what it asked.
//
// The caller's own, and needing nothing beyond reading their own entries - the
// same as the statistics, which this is the per-day half of.
func (h *MeHandler) Days(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	from, to, err := weekRange(c, h.locationFor(c, principal.User))
	if err != nil {
		return nil, toHTTPError(err)
	}

	days, err := h.overtime.Days(c, principal.User.ID, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	answer := DaysResponse{
		From: Date{Time: model.CalendarDay(from)},
		To:   Date{Time: model.CalendarDay(to)},
		Days: make([]DayResponse, 0, len(days)),
	}

	for _, day := range days {
		answer.Days = append(answer.Days, DayResponse{
			Date: Date{Time: day.Date}, Booked: day.Booked, Target: day.Target, Absence: day.Absence,
		})

		answer.TotalBooked += day.Booked
		answer.TotalTarget += day.Target
	}

	return answer, nil
}

// weekRange reads the from/to parameters, defaulting to the week - Monday to
// Sunday - that today falls in.
//
// Today in the caller's own zone, for the reason the overtime's month is: for
// somebody in Auckland on a Monday morning the server's own clock is still in
// last week.
func weekRange(c *gofr.Context, location *time.Location) (from, to time.Time, err error) {
	fromParam, err := queryDate(c, "from")
	if err != nil {
		return from, to, err
	}

	toParam, err := queryDate(c, "to")
	if err != nil {
		return from, to, err
	}

	today := time.Now().In(location)
	sinceMonday := (int(today.Weekday()) + 6) % 7

	from = time.Date(today.Year(), today.Month(), today.Day()-sinceMonday, 0, 0, 0, 0, location)
	to = from.AddDate(0, 0, 6)

	if fromParam != nil {
		from = *fromParam
	}

	if toParam != nil {
		to = *toParam
	}

	return from, to, nil
}
