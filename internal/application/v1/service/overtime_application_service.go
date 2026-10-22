package service

import (
	"context"
	"slices"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// OvertimeDay is the balance for one calendar day.
type OvertimeDay struct {
	Date    time.Time
	Booked  float64
	Target  float64
	Balance float64 // Booked - Target

	// Absence and Holiday say why the day asks less than its week plans, where
	// something does: the kind of absence recorded on it, or the holiday it is.
	Absence string
	Holiday string
}

// OvertimeBalance summarises a user's overtime across a period.
type OvertimeBalance struct {
	UserID   uint
	UserName string
	From     time.Time
	To       time.Time

	DailyTarget float64
	Days        []OvertimeDay

	// EveryDayCountsFrom is the day from which every working day counts, nil
	// while only the days booked on do.
	EveryDayCountsFrom *time.Time

	TotalBooked  float64
	TotalTarget  float64
	TotalBalance float64
}

// Day is one calendar day of somebody's: what was booked on it and what it asked.
type Day struct {
	Date   time.Time
	Booked float64
	Target float64

	// Absence is the kind of absence recorded on the day, or empty.
	Absence string

	// Holiday is the public holiday the day is where this person works, or empty.
	Holiday string

	// Counts says whether the day counts towards the balance - see dayRules.
	Counts bool
}

// MaxDays is the longest range Days answers: fifty-three weeks, which is what a
// year drawn as whole weeks from Monday to Sunday can span.
const MaxDays = 371

// dayRange reads a range of days, refusing one that ends before it begins or
// spans more than MaxDays - the bounds every answer given a day at a time keeps.
func dayRange(from, to time.Time) (first, last time.Time, err error) {
	first, last = model.CalendarDay(from), model.CalendarDay(to)

	if last.Before(first) {
		return first, last, apperror.Invalidf("'to' must not be before 'from'").WithCode("rangeInverted")
	}

	if span := int(last.Sub(first).Hours()/24) + 1; span > MaxDays {
		return first, last, apperror.Invalidf("a range of days may span at most %d days", MaxDays).
			WithCode("rangeTooLong", MaxDays)
	}

	return first, last, nil
}

// Today answers which calendar day it is for somebody, in their own zone.
type Today func(ctx context.Context, user *model.User) time.Time

// OvertimeService computes overtime balances.
type OvertimeService struct {
	timesheets repository.TimesheetRepository
	users      repository.UserRepository
	schedules  repository.WorkScheduleRepository
	absences   repository.AbsenceRepository
	regions    RegionSource
	today      Today
}

// NewOvertimeService needs the accounts, their planned weeks, their absences and
// the installation's holidays as well as their hours, because what a day asks is
// each account's own - and which day today is, in each account's own zone,
// because a working day nobody booked counts once it has been.
func NewOvertimeService(
	timesheets repository.TimesheetRepository,
	users repository.UserRepository,
	schedules repository.WorkScheduleRepository,
	absences repository.AbsenceRepository,
	regions RegionSource,
	today Today,
) *OvertimeService {
	return &OvertimeService{
		timesheets: timesheets, users: users, schedules: schedules, absences: absences,
		regions: regions, today: today,
	}
}

// dayRules decides a day: what it asks, and whether it counts towards the balance.
//
// Two rules, and which one holds is each person's choice of a day. Before it, or
// with none chosen, a day counts only if something was booked on it and asks what
// its week plans: the rule from before there was a holiday calendar, kept so that a
// month already behind somebody does not turn into a deficit the moment holidays
// and absences become known. From that day on, every working day up to today
// counts, booked or not, and asks what its week plans less what a public holiday
// or an absence takes off it. A day after today counts once something is booked on
// it, as it always did: a balance does not charge for a day nobody has had yet.
type dayRules struct {
	from      *time.Time
	today     time.Time
	schedules []model.WorkSchedule
	before    model.WeekHours
	holidays  map[time.Time]model.Holiday
	absent    map[time.Time]model.Absence
}

// of decides one day, given what was booked on it.
func (r dayRules) of(day time.Time, booked float64) (target float64, counts bool) {
	planned := model.TargetOn(day, r.schedules, r.before)

	if r.from == nil || day.Before(*r.from) {
		return planned, booked > 0
	}

	target = planned

	if _, holiday := r.holidays[day]; holiday {
		target = 0
	} else if absence, away := r.absent[day]; away {
		target = absence.Target(planned)
	}

	return target, booked > 0 || (target > 0 && !day.After(r.today))
}

// expectsWork says whether somebody was expected to work on a day at all: their
// week plans hours for it, it is no public holiday where they work, and they were
// not away - except for half a day of leave, which leaves half a day of work. A day
// off against overtime is away, though it asks its whole target of the balance;
// that is what takes it off the balance, and nobody is meant to book on it.
//
// Not of, which answers a different question: what a day asks of the balance, by
// a rule that before somebody's chosen day knows no holidays at all.
func (r dayRules) expectsWork(day time.Time) bool {
	if model.TargetOn(day, r.schedules, r.before) <= 0 {
		return false
	}

	if _, holiday := r.holidays[day]; holiday {
		return false
	}

	if absence, away := r.absent[day]; away {
		return absence.Kind == model.AbsenceHalfVacation
	}

	return true
}

// rulesFor gathers what decides somebody's days between two calendar days.
func (s *OvertimeService) rulesFor(ctx context.Context, user *model.User, first, last time.Time) (dayRules, error) {
	schedules, err := s.schedules.ForUser(ctx, user.ID)
	if err != nil {
		return dayRules{}, err
	}

	away, err := s.absences.Between(ctx, user.ID, first, last)
	if err != nil {
		return dayRules{}, err
	}

	absent := make(map[time.Time]model.Absence, len(away))
	for _, absence := range away {
		absent[absence.Day] = absence
	}

	holidays, err := holidaysOf(ctx, s.regions, user, first, last)
	if err != nil {
		return dayRules{}, err
	}

	return dayRules{
		from:      user.EveryDayCountsFrom,
		today:     todayOf(ctx, s.today, user),
		schedules: schedules,
		before:    model.DefaultWeek(user.EffectiveDailyTarget()),
		holidays:  holidays,
		absent:    absent,
	}, nil
}

// todayOf is today in somebody's own zone, or in UTC for a service built without
// a clock.
func todayOf(ctx context.Context, clock Today, user *model.User) time.Time {
	if clock == nil {
		return model.CalendarDay(time.Now().UTC())
	}

	return model.CalendarDay(clock(ctx, user))
}

// Balance computes the overtime balance for one user over a date range.
//
// The days that count are the ones dayRules says count: every booked day, and from
// the day somebody chose, every working day up to today. A booked day is measured
// against the week in force on it - see model.TargetOn - so a day the week leaves
// free counts every hour booked on it as overtime. DailyTarget stays the
// account's own figure, which is what that week is made of where no plan has been
// made.
func (s *OvertimeService) Balance(
	ctx context.Context,
	userID uint,
	from, to time.Time,
) (*OvertimeBalance, error) {
	if userID == 0 {
		return nil, apperror.InvalidFields("userId")
	}

	if to.Before(from) {
		return nil, apperror.Invalidf("'to' must not be before 'from'").WithCode("rangeInverted")
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// A range of days, whose end the repository decides; see OverWholeDays.
	start := startOfDay(from)
	end := startOfDay(to)

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &start,
		EndDate:   &end,
	})
	if err != nil {
		return nil, err
	}

	bookedPerDay := make(map[time.Time]float64)

	// Keyed by the calendar day rather than by the value in the column, because a
	// database that has been in use has both shapes in it: every stopwatch entry
	// written before BookingDay was corrected carries the zone it was recorded in.
	// Grouping by the raw value puts those in a day of their own, beside the same
	// day typed by hand, and charges a full target to each.
	for _, entry := range entries {
		bookedPerDay[model.CalendarDay(entry.Date)] += entry.DurationHours
	}

	first, last := model.CalendarDay(from), model.CalendarDay(to)

	rules, err := s.rulesFor(ctx, user, first, last)
	if err != nil {
		return nil, err
	}

	// The days worth asking about: every one booked on, and every one from the day
	// somebody counts every working day from up to today. Bounded by what happened
	// rather than by the range asked about, which may be decades wide.
	candidates := make(map[time.Time]bool, len(bookedPerDay))
	for day := range bookedPerDay {
		candidates[day] = true
	}

	if rules.from != nil {
		for day := latest(first, *rules.from); !day.After(earliest(last, rules.today)); day = day.AddDate(0, 0, 1) {
			candidates[day] = true
		}
	}

	balance := &OvertimeBalance{
		UserID:             user.ID,
		UserName:           user.Name,
		From:               start,
		To:                 startOfDay(to),
		DailyTarget:        user.EffectiveDailyTarget(),
		EveryDayCountsFrom: user.EveryDayCountsFrom,
		Days:               make([]OvertimeDay, 0, len(candidates)),
	}

	// Settled, every figure that is a sum or a difference: see settled.
	for day := range candidates {
		booked := bookedPerDay[day]

		target, counts := rules.of(day, booked)
		if !counts {
			continue
		}

		balance.Days = append(balance.Days, OvertimeDay{
			Date:    day,
			Booked:  settled(booked),
			Target:  target,
			Balance: settled(booked - target),
			Absence: rules.absent[day].Kind,
			Holiday: rules.holidays[day].Name,
		})
	}

	// Map iteration order is random; sort so the result is stable.
	slices.SortFunc(balance.Days, func(a, b OvertimeDay) int {
		return a.Date.Compare(b.Date)
	})

	for _, day := range balance.Days {
		balance.TotalBooked += day.Booked
		balance.TotalTarget += day.Target
		balance.TotalBalance += day.Balance
	}

	balance.TotalBooked = settled(balance.TotalBooked)
	balance.TotalTarget = settled(balance.TotalTarget)
	balance.TotalBalance = settled(balance.TotalBalance)

	return balance, nil
}

// latest is the later of two days.
func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// earliest is the earlier of two days.
func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}

// Days returns every day of a range with what was booked on it and what it asked.
//
// Every day, the empty ones included, which is the difference from Balance: a
// picture of a week has to show the Tuesday nobody booked on as well. Each says
// whether it counts towards the balance, by the same rule the balance keeps, so
// that a picture summing them comes to the figure the balance gives.
//
// Bounded to MaxDays, so the range is a picture's worth and not a history's.
func (s *OvertimeService) Days(ctx context.Context, userID uint, from, to time.Time) ([]Day, error) {
	if userID == 0 {
		return nil, apperror.InvalidFields("userId")
	}

	first, last, err := dayRange(from, to)
	if err != nil {
		return nil, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	entries, err := s.timesheets.GetByFilter(ctx, repository.TimesheetFilter{
		UserID:    userID,
		StartDate: &first,
		EndDate:   &last,
	})
	if err != nil {
		return nil, err
	}

	booked := make(map[time.Time]float64)
	for _, entry := range entries {
		booked[model.CalendarDay(entry.Date)] += entry.DurationHours
	}

	rules, err := s.rulesFor(ctx, user, first, last)
	if err != nil {
		return nil, err
	}

	var days []Day

	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		target, counts := rules.of(d, booked[d])

		days = append(days, Day{
			Date:    d,
			Booked:  settled(booked[d]),
			Target:  target,
			Absence: rules.absent[d].Kind,
			Holiday: rules.holidays[d].Name,
			Counts:  counts,
		})
	}

	return days, nil
}

// RunningBalance is somebody's balance from the first day that counts up to today,
// read against the bounds they set.
type RunningBalance struct {
	Hours float64

	// Since is where the balance begins, nil while nothing counts yet.
	Since *time.Time

	Upper, Lower *float64

	// Outside is model.BalanceAbove or model.BalanceBelow while the balance lies
	// past one of the bounds, and empty otherwise.
	Outside string
}

// Notices is what about somebody's own time deserves a banner.
type Notices struct {
	Balance RunningBalance

	// UnbookedDay is the last day before today somebody was expected to work and
	// booked nothing on, nil when that day was booked or they asked not to be told.
	UnbookedDay *time.Time
}

// Notices answers what about somebody's own time deserves a banner.
func (s *OvertimeService) Notices(ctx context.Context, userID uint) (*Notices, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	running, err := s.running(ctx, user)
	if err != nil {
		return nil, err
	}

	unbooked, err := s.unbookedDay(ctx, user)
	if err != nil {
		return nil, err
	}

	return &Notices{Balance: running, UnbookedDay: unbooked}, nil
}

// reminderLookBack is how far back the last working day is looked for: two weeks,
// which reaches past a fortnight of leave and the public holidays around it.
const reminderLookBack = 14

// unbookedDay is the last day before today somebody was expected to work, when
// nothing is booked on it.
//
// Only that one day. Somebody who books a week at a time on Friday is told about
// Thursday on Friday morning and about nothing on Monday, and an earlier gap is
// the business of the day it was - a banner listing every unbooked day of a month
// would be one nobody reads. Nothing is said about a day before the balance
// begins, which belongs to a time before the account was used.
func (s *OvertimeService) unbookedDay(ctx context.Context, user *model.User) (*time.Time, error) {
	if user.SkipUnbookedReminder {
		return nil, nil
	}

	begins, found, err := s.beginning(ctx, user)
	if err != nil || !found {
		return nil, err
	}

	today := todayOf(ctx, s.today, user)
	last := today.AddDate(0, 0, -1)
	first := today.AddDate(0, 0, -reminderLookBack)

	rules, err := s.rulesFor(ctx, user, first, last)
	if err != nil {
		return nil, err
	}

	for day := last; !day.Before(first) && !day.Before(begins); day = day.AddDate(0, 0, -1) {
		if !rules.expectsWork(day) {
			continue
		}

		booked, err := s.timesheets.CountByFilter(ctx, repository.TimesheetFilter{
			UserID: user.ID, StartDate: &day, EndDate: &day,
		})
		if err != nil || booked > 0 {
			return nil, err
		}

		return &day, nil
	}

	return nil, nil
}

// beginning is the first day that counts towards somebody's balance: the first day
// they booked on, or the day every working day counts from where that is earlier.
func (s *OvertimeService) beginning(ctx context.Context, user *model.User) (time.Time, bool, error) {
	first, found, err := s.timesheets.FirstDay(ctx, user.ID)
	if err != nil {
		return first, false, err
	}

	if from := user.EveryDayCountsFrom; from != nil && (!found || from.Before(first)) {
		return *from, true, nil
	}

	return first, found, nil
}

// running is somebody's balance from the first day that counts to today.
//
// The first day that counts is the first day booked on, or the day every working
// day counts from where that is earlier: from then on an unbooked working day is
// a deficit, so it belongs to the balance whether or not anything was booked
// before it. A booking ahead of today counts once its day has come, as it does
// on the overtime screen.
func (s *OvertimeService) running(ctx context.Context, user *model.User) (RunningBalance, error) {
	running := RunningBalance{Upper: user.BalanceUpperLimit, Lower: user.BalanceLowerLimit}

	first, found, err := s.beginning(ctx, user)
	if err != nil {
		return running, err
	}

	today := todayOf(ctx, s.today, user)

	if !found || first.After(today) {
		return running, nil
	}

	balance, err := s.Balance(ctx, user.ID, first, today)
	if err != nil {
		return running, err
	}

	running.Hours = balance.TotalBalance
	running.Since = &first
	running.Outside = user.BalanceOutside(running.Hours)

	return running, nil
}

// maxBalanceBound is the furthest from zero a bound may lie: some years of
// working days, which is further than anybody keeping a flexitime account means.
const maxBalanceBound = 10000

// SetBalanceLimits keeps the bounds of somebody's running balance, nil for none.
//
// An upper bound lies at zero or above and a lower one at zero or below. A bound on
// the wrong side of zero would put every balance outside it from the first day,
// which no one means and a banner would then say on every screen.
func (s *OvertimeService) SetBalanceLimits(ctx context.Context, userID uint, upper, lower *float64) error {
	if upper != nil && (*upper < 0 || *upper > maxBalanceBound) {
		return apperror.InvalidFields("upperBound")
	}

	if lower != nil && (*lower > 0 || *lower < -maxBalanceBound) {
		return apperror.InvalidFields("lowerBound")
	}

	return s.users.SetBalanceLimits(ctx, userID, upper, lower)
}
