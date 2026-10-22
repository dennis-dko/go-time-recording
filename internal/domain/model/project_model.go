package model

import (
	"math"
	"time"
)

const (
	// ProjectStatusActive is the state a project starts in: it accepts new
	// time entries.
	ProjectStatusActive    = "active"
	ProjectStatusArchived  = "archived"
	ProjectStatusCompleted = "completed"
)

// Project model
//
// Every project belongs to one person, and to nobody else. There used to be two
// kinds - a shared project everyone could see and book on, and a private category
// for splitting up your own day - and the second was an exception to the first.
// Now the exception is the whole rule: each account is its own world, so a project
// is one person's way of organising their own hours.
//
// Two people working on the same thing therefore have a project each, with the same
// name if they like. Nothing adds them together, because nothing is meant to: there
// are no teams here. A manager keeps the projects of the accounts assigned to them,
// and does it as those accounts - the project is still the owner's.
type Project struct {
	ID          uint
	Name        string
	Description *string
	StartDate   time.Time
	EndDate     *time.Time
	Status      string

	// OwnerID is whose project it is.
	//
	// A pointer because the column is nullable and was, for a while, genuinely
	// empty: that was how a shared project was stored. A migration gave every
	// existing one an owner, so a nil here is not "shared" - it is a row nobody
	// can reach, which is what VisibleTo says about it.
	//
	// One path still produces one: with AUTH_ENABLED=false there is no "whose" to
	// record, so a project created then has no owner. That is consistent while
	// enforcement is off, because RequireVisible short-circuits on a viewer id of
	// zero and sees everything. It stops being consistent the moment somebody
	// turns authentication on - those rows are then invisible to every account,
	// including the one that made them, and there is no screen that can reach them
	// to fix it. deploy/OPERATIONS.md says so under "Things that will surprise
	// you"; it is the reason that mode is documented as a throwaway trial.
	OwnerID *uint

	// BudgetHours is how many hours its owner means to spend on it, nil for no
	// such figure. Theirs alone and nobody's sign-off: a project is still somebody's
	// way of sorting their own hours, and this is them saying how many they expect.
	BudgetHours *float64

	// Billable says whether its hours are charged to somebody, which is what a
	// statement of work lists. HourlyRate is what an hour is charged at, in
	// hundredths of Currency - cents for euros - and nil until there is one.
	// Hundredths rather than a float, because a float cannot hold 0.10, and money
	// added up a hundredth wrong is the one kind of wrong a statement may not be.
	Billable   bool
	HourlyRate *int64
	Currency   string
}

// MaxBudgetHours bounds a budget: far beyond anything one person books on one
// project, near enough that a mistyped figure is refused rather than kept.
const MaxBudgetHours = 100000

// MaxHourlyRate bounds a rate, in whole units of its currency: far beyond what
// anybody charges for an hour, near enough that a mistyped figure is refused.
const MaxHourlyRate = 100000

// DefaultCurrency is what a project is charged in until its owner says otherwise.
const DefaultCurrency = "EUR"

// Charge is what hours on the project come to, in hundredths of its currency, and
// false where nothing can be charged: it is not billable, or has no rate yet.
// Rounded once, on the hours together, so a statement's lines are what a
// calculator makes of the same hours rather than a sum of roundings.
func (p *Project) Charge(hours float64) (int64, bool) {
	if !p.Billable || p.HourlyRate == nil {
		return 0, false
	}

	return int64(math.Round(hours * float64(*p.HourlyRate))), true
}

// VisibleTo reports whether the user may see the project, which is to say whether
// it is theirs.
func (p *Project) VisibleTo(userID uint) bool {
	return p.OwnerID != nil && *p.OwnerID == userID
}
