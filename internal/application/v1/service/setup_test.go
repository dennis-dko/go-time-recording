package service_test

import (
	"context"
	"sync"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The wizard's whole value is that it tells the truth about what is
// outstanding. Two ways it could lie, and both are tested here: reporting a
// step done because the wizard once showed it, and staying dismissed while
// something required has come undone.

// stubSettings is an in-memory settings store.
type stubSettings struct {
	mu     sync.RWMutex
	values map[string]string
}

func newStubSettings() *stubSettings {
	return &stubSettings{values: map[string]string{}}
}

func (s *stubSettings) Get(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.values[key], nil
}

func (s *stubSettings) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.values[key] = value

	return nil
}

func (s *stubSettings) GetAll(_ context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]string, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}

	return out, nil
}

type setupFixture struct {
	*fixture

	store *stubSettings
	setup *service.SetupService
}

func newSetupFixture(t *testing.T, dialect string) *setupFixture {
	t.Helper()

	f := newFixture(t)
	store := newStubSettings()
	settings := service.NewSettingsService(store, f.roleRepo)

	return &setupFixture{
		fixture: f,
		store:   store,
		setup:   service.NewSetupService(settings, f.userRepo, dialect),
	}
}

// stepByID finds one step, failing loudly if the wizard stopped offering it.
func stepByID(t *testing.T, state model.SetupState, id string) model.SetupStep {
	t.Helper()

	for _, step := range state.Steps {
		if step.ID == id {
			return step
		}
	}

	t.Fatalf("the wizard no longer offers the %q step", id)

	return model.SetupStep{}
}

func TestFreshInstallationHasEverythingOutstanding(t *testing.T) {
	f := newSetupFixture(t, "sqlite")

	if _, err := f.auth.EnsureSystemUser(context.Background()); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	state, err := f.setup.State(context.Background())
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if state.Completed {
		t.Error("a fresh installation cannot have completed the wizard")
	}

	for _, step := range state.Steps {
		if step.Done {
			t.Errorf("%s should be outstanding on a fresh installation", step.ID)
		}
	}

	// Exactly these three are worth blocking on. Making everything mandatory
	// trains people to click past the wizard, and then the step that mattered
	// goes past too.
	required := map[string]bool{}
	for _, step := range state.Steps {
		if step.Required {
			required[step.ID] = true
		}
	}

	want := []string{model.SetupStepDatabase, model.SetupStepPassword, model.SetupStepTimezone}
	if len(required) != len(want) {
		t.Errorf("expected %v to be required, got %v", want, required)
	}

	for _, id := range want {
		if !required[id] {
			t.Errorf("%s should be a required step", id)
		}
	}
}

// The database has to be settled before anything else, because everything else
// is stored in it. Switching later leaves the password change, the timezone and
// the title behind in the old database, and start-up recreates the
// administrator with the documented initial password - so an installation that
// looked configured comes back up reachable with a password anyone can look up.
func TestTheDatabaseIsTheFirstStep(t *testing.T) {
	f := newSetupFixture(t, "sqlite")

	state, err := f.setup.State(context.Background())
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if len(state.Steps) == 0 {
		t.Fatal("the wizard offers no steps")
	}

	if state.Steps[0].ID != model.SetupStepDatabase {
		t.Errorf("the database must come first, got %q", state.Steps[0].ID)
	}

	if !state.Steps[0].Required {
		t.Error("the database step must be required")
	}
}

// Staying on SQLite is a legitimate answer, so it has to be expressible -
// otherwise the required step could never be completed by anyone who wants it,
// and a required step nobody can complete is a wizard that never goes away.
func TestKeepingTheCurrentDatabaseSettlesTheStep(t *testing.T) {
	f := newSetupFixture(t, "sqlite")
	ctx := context.Background()

	before, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if stepByID(t, before, model.SetupStepDatabase).Done {
		t.Fatal("the database step must start outstanding on SQLite")
	}

	if err := f.setup.KeepDatabase(ctx); err != nil {
		t.Fatalf("keep database: %v", err)
	}

	after, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if !stepByID(t, after, model.SetupStepDatabase).Done {
		t.Error("confirming the current database must settle the step")
	}
}

// Done-ness is read from what is configured, not from a record of the wizard
// having been through.
func TestStepsFollowWhatIsActuallyConfigured(t *testing.T) {
	f := newSetupFixture(t, "sqlite")
	ctx := context.Background()

	if _, err := f.auth.EnsureSystemUser(ctx); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	user, err := f.userRepo.GetByEmail(ctx, service.SystemUserEmail)
	if err != nil {
		t.Fatalf("read the administrator: %v", err)
	}

	user.MustChangePassword = false

	if _, err := f.userRepo.Update(ctx, user); err != nil {
		t.Fatalf("change the password: %v", err)
	}

	if err := f.store.Set(ctx, model.SettingTimezone, "Europe/Berlin"); err != nil {
		t.Fatalf("set the timezone: %v", err)
	}

	if err := f.store.Set(ctx, model.SettingAppTitle, "Acme Time"); err != nil {
		t.Fatalf("set the title: %v", err)
	}

	state, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	for _, id := range []string{model.SetupStepPassword, model.SetupStepTimezone, model.SetupStepBranding} {
		if !stepByID(t, state, id).Done {
			t.Errorf("%s should be done", id)
		}
	}

	for _, id := range []string{model.SetupStepDatabase, model.SetupStepDirectory} {
		if stepByID(t, state, id).Done {
			t.Errorf("%s should still be outstanding", id)
		}
	}
}

// UTC chosen deliberately and UTC by default look identical in the resolved
// setting, so the step reads the stored value instead. Otherwise an
// administrator who genuinely wants UTC could never complete the step.
func TestChoosingUTCCompletesTheTimezoneStep(t *testing.T) {
	f := newSetupFixture(t, "sqlite")
	ctx := context.Background()

	before, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if stepByID(t, before, model.SetupStepTimezone).Done {
		t.Fatal("the timezone step must start outstanding")
	}

	if err := f.store.Set(ctx, model.SettingTimezone, "UTC"); err != nil {
		t.Fatalf("set the timezone: %v", err)
	}

	after, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if !stepByID(t, after, model.SetupStepTimezone).Done {
		t.Error("choosing UTC deliberately must complete the step")
	}
}

// Running on PostgreSQL is itself the answer to the database step; there is
// nothing left to ask.
func TestRunningOnARealDatabaseCompletesThatStep(t *testing.T) {
	f := newSetupFixture(t, "postgres")

	state, err := f.setup.State(context.Background())
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if !stepByID(t, state, model.SetupStepDatabase).Done {
		t.Error("an installation already on PostgreSQL has nothing to configure here")
	}
}

// A connection saved from the Settings screen only applies at the next
// restart, but the decision has been made - the wizard should stop asking.
func TestASavedConnectionCompletesTheDatabaseStepBeforeTheRestart(t *testing.T) {
	f := newSetupFixture(t, "sqlite")
	ctx := context.Background()

	if err := f.store.Set(ctx, model.SettingDatasourceChosen, "true"); err != nil {
		t.Fatalf("mark the choice: %v", err)
	}

	state, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if !stepByID(t, state, model.SetupStepDatabase).Done {
		t.Error("a saved connection should settle the step even before it is applied")
	}
}

func TestCompleteDismissesTheWizard(t *testing.T) {
	f := newSetupFixture(t, "postgres")
	ctx := context.Background()

	if err := f.setup.Complete(ctx); err != nil {
		t.Fatalf("complete: %v", err)
	}

	state, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if !state.Completed {
		t.Error("the wizard should be recorded as dismissed")
	}
}

// The property that makes dismissing safe: it settles the optional steps, not
// the required ones. A restored database, or a setting cleared by hand, brings
// the wizard back rather than leaving an installation half-configured and
// quiet about it.
func TestDismissingDoesNotSuppressAnOutstandingRequiredStep(t *testing.T) {
	f := newSetupFixture(t, "postgres")
	ctx := context.Background()

	if _, err := f.auth.EnsureSystemUser(ctx); err != nil {
		t.Fatalf("ensure system user: %v", err)
	}

	if err := f.setup.Complete(ctx); err != nil {
		t.Fatalf("complete: %v", err)
	}

	state, err := f.setup.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	if !state.Completed {
		t.Fatal("expected the wizard to be dismissed")
	}

	// The administrator never changed the password, so that step is still
	// outstanding despite the dismissal.
	if state.OutstandingRequired() == 0 {
		t.Fatal("expected the password step to be outstanding")
	}

	if !state.ShouldShow() {
		t.Error("a dismissed wizard must still appear while a required step is undone")
	}
}

func TestShouldShowIsFalseOnlyWhenDismissedAndSettled(t *testing.T) {
	cases := []struct {
		name  string
		state model.SetupState
		want  bool
	}{
		{
			"fresh installation",
			model.SetupState{Steps: []model.SetupStep{{ID: "a", Required: true}}},
			true,
		},
		{
			"dismissed with a required step outstanding",
			model.SetupState{Completed: true, Steps: []model.SetupStep{{ID: "a", Required: true}}},
			true,
		},
		{
			"required steps done but never dismissed",
			model.SetupState{Steps: []model.SetupStep{{ID: "a", Required: true, Done: true}}},
			true,
		},
		{
			"dismissed and settled",
			model.SetupState{Completed: true, Steps: []model.SetupStep{{ID: "a", Required: true, Done: true}}},
			false,
		},
		{
			"an outstanding optional step does not bring it back",
			model.SetupState{Completed: true, Steps: []model.SetupStep{
				{ID: "a", Required: true, Done: true},
				{ID: "b", Required: false, Done: false},
			}},
			false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.ShouldShow(); got != tc.want {
				t.Errorf("expected ShouldShow to be %v, got %v", tc.want, got)
			}
		})
	}
}
