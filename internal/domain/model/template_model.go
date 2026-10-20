package model

// EntryTemplate is a booking somebody makes often, kept to fill the booking form
// with: a project, hours and a description, each of them optional, and no day.
//
// Only ever a way to fill the form. Nothing is booked from a template on its own -
// time is recorded by the person who worked it, with the hours they worked - so it
// carries no schedule and no repetition, and the project it names is asked about
// again when the booking is made, as any booking's is.
type EntryTemplate struct {
	ID            uint
	UserID        uint
	ProjectID     *uint
	DurationHours *float64
	Description   *string
}
