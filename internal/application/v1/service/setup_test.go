package service_test

import (
	"context"
	"maps"
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
	maps.Copy(out, s.values)

	return out, nil
}

type setupFixture struct {
	*fixture

	store *stubSettings
	setup *service.SetupService
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()

	f := newFixture(t)
	store := newStubSettings()
	// Empty app name: these tests are about which steps are outstanding, and
	// the instance title only decides what the branding step falls back to.
	settings := service.NewSettingsService(store, f.roleRepo, "")

	return &setupFixture{
		fixture: f,
		store:   store,
		setup:   service.NewSetupService(settings, f.userRepo),
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
	f := newSetupFixture(t)

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

	// Exactly these two are worth blocking on. Making everything mandatory
	// trains people to click past the wizard, and then the step that mattered
	// goes past too. The database is not among them because it cannot be
	// outstanding here: the installer settles it before the application starts.
	required := map[string]bool{}
	for _, step := range state.Steps {
		if step.Required {
			required[step.ID] = true
		}
	}

	want := []string{model.SetupStepPassword, model.SetupStepTimezone}
	if len(required) != len(want) {
		t.Errorf("expected %v to be required, got %v", want, required)
	}

	for _, id := range want {
		if !required[id] {
			t.Errorf("%s should be a required step", id)
		}
	}
}

// Done-ness is read from what is configured, not from a record of the wizard
// having been through.
func TestStepsFollowWhatIsActuallyConfigured(t *testing.T) {
	f := newSetupFixture(t)
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

	if stepByID(t, state, model.SetupStepDirectory).Done {
		t.Error("the directory step should still be outstanding")
	}
}

// UTC chosen deliberately and UTC by default look identical in the resolved
// setting, so the step reads the stored value instead. Otherwise an
// administrator who genuinely wants UTC could never complete the step.
func TestChoosingUTCCompletesTheTimezoneStep(t *testing.T) {
	f := newSetupFixture(t)
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

func TestCompleteDismissesTheWizard(t *testing.T) {
	f := newSetupFixture(t)
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
	f := newSetupFixture(t)
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
