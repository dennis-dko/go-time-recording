package service_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// gatedDirectory holds its first listing until it is released, so a second run
// can be started while the first is inside it.
type gatedDirectory struct {
	users   []service.ExternalUser
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (d *gatedDirectory) Enabled() bool { return true }

func (d *gatedDirectory) ListUsers(context.Context) ([]service.ExternalUser, error) {
	if d.calls.Add(1) == 1 {
		close(d.entered)
		<-d.release
	}

	return d.users, nil
}

// A run that starts while another is running is turned away, not run beside it.
//
// The schedule and the button share one service, and two administrators can
// press the button at once. Two runs read the same directory and the same
// accounts, so they choose the same departures - and the second to reach one
// finds it gone, is refused with "user not found", and stops part-way through,
// having deleted whatever it reached first. Nothing is lost, since every
// deletion is its own transaction, but whoever pressed the button is told a
// run failed with a sentence about an account it never touched. Turned away at
// the start, the second run says what is actually the case.
func TestASynchronisationThatStartsWhileAnotherRunsIsTurnedAway(t *testing.T) {
	f := newFixture(t)
	externalUser(t, f, "staying@example.com")
	externalUser(t, f, "leaving@example.com")

	directory := &gatedDirectory{
		users:   []service.ExternalUser{{Email: "staying@example.com"}},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}

	purger := &recordingPurger{users: f.userRepo}
	sync := service.NewLDAPSyncService(directory, f.userRepo, f.roleRepo,
		f.timesheetRepo, purger, nil, 0.9, model.RoleUser)

	first := make(chan error, 1)

	go func() {
		_, err := sync.Sync(context.Background())
		first <- err
	}()

	<-directory.entered

	_, err := sync.Sync(context.Background())
	if apperror.KindOf(err) != apperror.KindConflict || !hasCode(err, "syncAlreadyRunning") {
		t.Errorf("a run started during another answered %v, want it turned away as already running", err)
	}

	// A preview changes nothing, so it may look while a run is under way.
	if _, err := sync.Preview(context.Background()); err != nil && hasCode(err, "syncAlreadyRunning") {
		t.Error("a preview was turned away while a run was under way")
	}

	close(directory.release)

	if err := <-first; err != nil {
		t.Fatalf("the first run failed: %v", err)
	}

	if len(purger.purged) != 1 {
		t.Errorf("the departed account was purged %d times, want once", len(purger.purged))
	}

	// And once it has finished, the next one runs.
	if _, err := sync.Sync(context.Background()); err != nil {
		t.Errorf("a run after the first had finished was refused: %v", err)
	}
}

func hasCode(err error, code string) bool {
	detail, ok := apperror.Detail(err)

	return ok && detail.Code == code
}
