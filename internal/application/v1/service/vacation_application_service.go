package service

import (
	"context"
	"strconv"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// VacationService keeps how many days of leave a year gives somebody, and works out
// what is left of it.
//
// Each person's own, like the absences it is counted from.
type VacationService struct {
	entitlements repository.VacationRepository
	absences     repository.AbsenceRepository
	users        repository.UserRepository
	schedules    repository.WorkScheduleRepository
	regions      RegionSource
	today        Today
}

// NewVacationService needs what decides whether a recorded day of leave is a
// working day - the planned week and the holidays - and which day today is, which
// is what tells leave taken from leave planned.
func NewVacationService(
	entitlements repository.VacationRepository,
	absences repository.AbsenceRepository,
	users repository.UserRepository,
	schedules repository.WorkScheduleRepository,
	regions RegionSource,
	today Today,
) *VacationService {
	return &VacationService{
		entitlements: entitlements, absences: absences, users: users, schedules: schedules,
		regions: regions, today: today,
	}
}

// VacationAccount is a year of somebody's leave, and the entitlements it comes from.
type VacationAccount struct {
	model.VacationYear

	Entitlements []model.VacationEntitlement
}

// Year works out somebody's leave in a year.
//
// What a year leaves over is carried into the next, back to the first year an
// entitlement was recorded for: leave not taken is not lost here, and leave
// overdrawn is owed. Only leave on a day that is worked counts - a day the week
// planned for it asks something of, and no public holiday - which recording leave
// already keeps to, and which is asked again here because a plan or a state chosen
// later can make a recorded day one that is not worked.
func (s *VacationService) Year(ctx context.Context, userID uint, year int) (*VacationAccount, error) {
	if year < 1900 || year > 9999 {
		return nil, apperror.InvalidFields("year")
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	entitlements, err := s.entitlements.ForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	account := &VacationAccount{VacationYear: model.VacationYear{Year: year}, Entitlements: entitlements}

	// No account before the first year anything was given.
	if len(entitlements) == 0 || year < entitlements[0].FromYear {
		return account, nil
	}

	first := entitlements[0].FromYear
	from := time.Date(first, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)

	away, err := s.absences.Between(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}

	schedules, err := s.schedules.ForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	holidays, err := holidaysOf(ctx, s.regions, user, from, to)
	if err != nil {
		return nil, err
	}

	before := model.DefaultWeek(user.EffectiveDailyTarget())
	today := todayOf(ctx, s.today, user)

	taken, planned := map[int]float64{}, map[int]float64{}

	for _, absence := range away {
		days := absence.VacationDays()
		if days == 0 {
			continue
		}

		if _, holiday := holidays[absence.Day]; holiday || model.TargetOn(absence.Day, schedules, before) <= 0 {
			continue
		}

		if absence.Day.After(today) {
			planned[absence.Day.Year()] += days
		} else {
			taken[absence.Day.Year()] += days
		}
	}

	carried := 0.0

	for y := first; y < year; y++ {
		given, _ := model.EntitlementIn(y, entitlements)
		carried = given + carried - taken[y] - planned[y]
	}

	account.Entitlement, _ = model.EntitlementIn(year, entitlements)
	account.CarriedOver = settled(carried)
	account.Taken = settled(taken[year])
	account.Planned = settled(planned[year])

	return account, nil
}

// Entitle records how many days of leave a year gives somebody from a year on,
// replacing what an entitlement from that same year said.
func (s *VacationService) Entitle(ctx context.Context, userID uint, fromYear int, days float64) error {
	var invalid []string

	if fromYear < 1900 || fromYear > 9999 {
		invalid = append(invalid, "fromYear")
	}

	// Inside the range rather than outside it, so NaN is refused: see
	// validateTimesheet.
	if !(days >= 0 && days <= model.MaxEntitlementDays) {
		invalid = append(invalid, "days")
	}

	if len(invalid) > 0 {
		return apperror.InvalidFields(invalid...)
	}

	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return err
	}

	return s.entitlements.Save(ctx, model.VacationEntitlement{UserID: userID, FromYear: fromYear, Days: days})
}

// Withdraw takes back the entitlement from a year, handing its years back to the
// one before - or to none, where there is no other.
func (s *VacationService) Withdraw(ctx context.Context, userID uint, fromYear int) error {
	found, err := s.entitlements.Delete(ctx, userID, fromYear)
	if err != nil {
		return err
	}

	if !found {
		return apperror.NotFound("entitlement", strconv.Itoa(fromYear))
	}

	return nil
}
