package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/security"
)

// racingSessions is a session store a change to the account lands in the middle
// of: it happens the moment before a sign-in's session is written.
type racingSessions struct {
	*stubSessions

	before func()
}

func (r racingSessions) Save(ctx context.Context, session *model.Session) error {
	if r.before != nil {
		r.before()
	}

	return r.stubSessions.Save(ctx, session)
}

// A sign-in that checked a password which was changed before its session was
// written does not keep that session.
//
// A password change puts the new password in place and then ends the account's
// other sessions. A sign-in with the old one checks it - slowly, by design - and
// writes its session afterwards, so one that wrote it after the change had ended
// the others kept a session opened with a password that no longer worked: the
// very session somebody changes a password to be rid of.
func TestASignInRacingAPasswordChangeKeepsNoSession(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	user, err := f.userRepo.GetByID(ctx, f.userID)
	if err != nil {
		t.Fatal(err)
	}

	if user.PasswordHash, err = security.HashPassword("the-old-password-1"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.userRepo.Update(ctx, user); err != nil {
		t.Fatal(err)
	}

	store := newStubSessions()

	// The owner changes the password and ends every other session, between the
	// sign-in's check and its write.
	changed := func() {
		owner, err := f.userRepo.GetByID(ctx, f.userID)
		if err != nil {
			t.Fatal(err)
		}

		if owner.PasswordHash, err = security.HashPassword("the-new-password-1"); err != nil {
			t.Fatal(err)
		}

		if _, err := f.userRepo.Update(ctx, owner); err != nil {
			t.Fatal(err)
		}

		if err := store.DeleteForUser(ctx, f.userID); err != nil {
			t.Fatal(err)
		}
	}

	sessions := service.NewSessionService(f.userRepo, f.roleRepo,
		racingSessions{stubSessions: store, before: changed}, f.auth, time.Hour)

	if _, err := sessions.Login(ctx, user.Email, "the-old-password-1", ""); !hasCode(err, "invalidCredentials") {
		t.Errorf("a sign-in with the old password, overtaken by the change, was answered %v; "+
			"want the refusal of a wrong password", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	for _, session := range store.items {
		if session.UserID == f.userID {
			t.Error("a session opened with the old password outlived the change that ended the others")
		}
	}
}
