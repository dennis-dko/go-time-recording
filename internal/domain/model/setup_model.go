package model

// SetupStepID names one step of the first-run wizard. They are constants
// because the interface renders them in this order and the server decides
// which are done; a step invented on one side only would silently never
// complete.
const (
	SetupStepPassword  = "password"
	SetupStepTimezone  = "timezone"
	SetupStepDatabase  = "database"
	SetupStepBranding  = "branding"
	SetupStepDirectory = "directory"
)

// SetupStep is one thing to settle before an installation is ready for use.
type SetupStep struct {
	ID string `json:"id"`

	// Done is worked out from what is actually configured, never from a "the
	// wizard was here" marker. A step that was completed and then undone -
	// someone clearing the timezone again - has to reappear as outstanding,
	// which a stored flag could not express.
	Done bool `json:"done"`

	// Required marks a step an installation should not run without. The
	// distinction is deliberate: calling everything mandatory trains people to
	// click past the wizard, and then the one step that mattered goes past too.
	Required bool `json:"required"`
}

// SetupState is what the first-run wizard renders.
type SetupState struct {
	// Completed reports whether the wizard has been dismissed. It is stored
	// rather than derived, because "I have seen this and the optional steps are
	// not for me" is a decision only a person can make.
	Completed bool `json:"completed"`

	Steps []SetupStep `json:"steps"`
}

// OutstandingRequired counts the required steps still to do.
func (s SetupState) OutstandingRequired() int {
	var count int

	for _, step := range s.Steps {
		if step.Required && !step.Done {
			count++
		}
	}

	return count
}

// ShouldShow reports whether the wizard belongs on screen.
//
// A required step that is not done brings it back even after it was dismissed:
// dismissing is a statement about the optional steps, not permission to run
// without a timezone.
func (s SetupState) ShouldShow() bool {
	return !s.Completed || s.OutstandingRequired() > 0
}
