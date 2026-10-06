package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// unreadableSessions is a session store whose database does not answer.
type unreadableSessions struct {
	*stubSessions
}

func (unreadableSessions) Get(context.Context, string) (*model.Session, error) {
	return nil, apperror.Internal(errors.New("the database went away"))
}

// A session that could not be read is not a session that is gone.
//
// The two were one answer, "no session", and the middleware clears the cookie of
// a session that is gone - so a database that did not answer for a moment, which
// a restart of its container is enough for, signed out everybody whose request
// arrived in that moment, with sessions that were perfectly good.
func TestASessionThatCannotBeReadIsNotReportedAsGone(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	unreadable := service.NewSessionService(f.userRepo, f.roleRepo,
		unreadableSessions{newStubSessions()}, f.auth, time.Hour)

	_, err := unreadable.Resolve(ctx, "a-token")
	if hasCode(err, "noSession") || apperror.KindOf(err) != apperror.KindInternal {
		t.Errorf("a session the database could not read was answered %v, want the failure itself", err)
	}

	// And one that is not there is still answered as gone.
	gone := service.NewSessionService(f.userRepo, f.roleRepo, newStubSessions(), f.auth, time.Hour)

	if _, err := gone.Resolve(ctx, "a-token"); !hasCode(err, "noSession") {
		t.Errorf("a session that does not exist was answered %v, want noSession", err)
	}
}
