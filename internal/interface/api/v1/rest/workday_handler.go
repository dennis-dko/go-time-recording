package rest

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// WorkDayHandler serves somebody's own record of when they worked: read with the
// right to read one's own time and kept with the right to book it, like the
// absences beside it.
type WorkDayHandler struct {
	work     *service.WorkDayService
	authz    *Authorizer
	timezone InstanceTimezoneFunc
}

// NewWorkDayHandler creates the handler.
func NewWorkDayHandler(work *service.WorkDayService, authz *Authorizer, timezone InstanceTimezoneFunc) *WorkDayHandler {
	return &WorkDayHandler{work: work, authz: authz, timezone: timezone}
}

// WorkDayResponse is a day: its times as the clock read them, and the hours worked
// and booked on it.
type WorkDayResponse struct {
	Date Date `json:"date"`

	// Recorded is false for a day booked on with no times recorded, which comes
	// without start and end.
	Recorded     bool    `json:"recorded"`
	Start        string  `json:"start,omitempty"`
	End          string  `json:"end,omitempty"`
	BreakMinutes int     `json:"breakMinutes"`
	WorkedHours  float64 `json:"workedHours"`
	BookedHours  float64 `json:"bookedHours"`

	// Notices is what the Working Hours Act says about the shift that began on this
	// day, and an empty list rather than none when it says nothing.
	Notices []WorkingTimeNoticeResponse `json:"notices"`
}

// WorkingTimeNoticeResponse is one limit of the Working Hours Act a shift went
// past; see model.WorkingTimeNotice for what Minutes is the minutes of.
type WorkingTimeNoticeResponse struct {
	Rule    string `json:"rule"`
	Minutes int    `json:"minutes"`
}

// WorkDaysResponse is a range of days and what they add up to.
type WorkDaysResponse struct {
	From        Date              `json:"from"`
	To          Date              `json:"to"`
	Days        []WorkDayResponse `json:"days"`
	TotalWorked float64           `json:"totalWorked"`
	TotalBooked float64           `json:"totalBooked"`
}

// WorkDayRequest records a day's times, as a clock shows them: "08:00", and
// "24:00" for a day that ends at midnight.
type WorkDayRequest struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	BreakMinutes int    `json:"breakMinutes"`
}

// List handles GET /api/v1/me/workdays, by default the month today falls in, in the
// caller's own zone.
func (h *WorkDayHandler) List(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	from, to, err := monthRange(c, principal.User.TimezoneOf(h.timezone.resolve(c)))
	if err != nil {
		return nil, toHTTPError(err)
	}

	days, err := h.work.Between(c, principal.User.ID, from, to)
	if err != nil {
		return nil, toHTTPError(err)
	}

	answer := WorkDaysResponse{
		From: Date{Time: model.CalendarDay(from)}, To: Date{Time: model.CalendarDay(to)},
		Days: make([]WorkDayResponse, 0, len(days)),
	}

	// Worked as whole minutes and divided once, so the total is the hours the
	// minutes come to rather than the sum of a month of rounded thirds.
	workedMinutes := 0

	for _, day := range days {
		described := WorkDayResponse{
			Date: Date{Time: day.Day}, Recorded: day.Recorded, BookedHours: day.BookedHours,
			Notices: make([]WorkingTimeNoticeResponse, 0, len(day.Notices)),
		}

		for _, notice := range day.Notices {
			described.Notices = append(described.Notices,
				WorkingTimeNoticeResponse{Rule: notice.Rule, Minutes: notice.Minutes})
		}

		if day.Recorded {
			described.Start = clockOf(day.StartMinute)
			described.End = clockOf(day.EndMinute)
			described.BreakMinutes = day.BreakMinutes
			described.WorkedHours = float64(day.WorkedMinutes()) / 60
			workedMinutes += day.WorkedMinutes()
		}

		answer.TotalBooked += described.BookedHours
		answer.Days = append(answer.Days, described)
	}

	answer.TotalWorked = float64(workedMinutes) / 60
	answer.TotalBooked = math.Round(answer.TotalBooked*100) / 100

	return answer, nil
}

// Record handles PUT /api/v1/me/workdays/{date}, in place of what the day had.
func (h *WorkDayHandler) Record(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	day, err := time.Parse(time.DateOnly, strings.TrimSpace(c.PathParam("date")))
	if err != nil {
		return nil, toHTTPError(apperror.InvalidFields("date"))
	}

	var req WorkDayRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	start, ok := minuteOf(req.Start)
	if !ok {
		return nil, toHTTPError(apperror.InvalidFields("start"))
	}

	end, ok := minuteOf(req.End)
	if !ok {
		return nil, toHTTPError(apperror.InvalidFields("end"))
	}

	if err := h.work.Record(c, model.WorkDay{
		UserID: principal.User.ID, Day: day, StartMinute: start, EndMinute: end, BreakMinutes: req.BreakMinutes,
	}); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "recorded"}, nil
}

// Remove handles DELETE /api/v1/me/workdays/{date}.
func (h *WorkDayHandler) Remove(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetWriteOwn)
	if err != nil {
		return nil, err
	}

	day, err := time.Parse(time.DateOnly, strings.TrimSpace(c.PathParam("date")))
	if err != nil {
		return nil, toHTTPError(apperror.InvalidFields("date"))
	}

	if err := h.work.Remove(c, principal.User.ID, day); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// monthRange reads the from/to parameters, defaulting to the month today falls in -
// today in the caller's own zone, for the reason weekRange gives.
func monthRange(c *gofr.Context, location *time.Location) (from, to time.Time, err error) {
	fromParam, err := queryDate(c, "from")
	if err != nil {
		return from, to, err
	}

	toParam, err := queryDate(c, "to")
	if err != nil {
		return from, to, err
	}

	today := time.Now().In(location)
	from = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
	to = from.AddDate(0, 1, -1)

	if fromParam != nil {
		from = *fromParam
	}

	if toParam != nil {
		to = *toParam
	}

	return from, to, nil
}

// clockOf writes minutes after midnight as a clock shows them.
func clockOf(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

// minuteOf reads a clock's "HH:MM" as minutes after midnight, "24:00" included.
func minuteOf(clock string) (int, bool) {
	hours, minutes, found := strings.Cut(strings.TrimSpace(clock), ":")
	if !found || len(hours) != 2 || len(minutes) != 2 {
		return 0, false
	}

	h, errHours := strconv.Atoi(hours)
	m, errMinutes := strconv.Atoi(minutes)

	if errHours != nil || errMinutes != nil || h < 0 || m < 0 || m > 59 {
		return 0, false
	}

	return h*60 + m, h*60+m <= model.MinutesPerDay
}
