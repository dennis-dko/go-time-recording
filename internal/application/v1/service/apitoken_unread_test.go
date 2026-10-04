package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// unreadableTokens is a token store whose database does not answer.
type unreadableTokens struct {
	*stubTokens
}

func (unreadableTokens) GetByHash(context.Context, string) (*model.APIToken, error) {
	return nil, apperror.Internal(errors.New("the database went away"))
}

// A token that could not be looked up is not a token that is invalid, as a
// session that could not be read is not one that is gone: a script told its
// token is invalid while the database restarts is told to replace a token that
// works.
func TestATokenThatCannotBeLookedUpIsNotReportedAsInvalid(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	unreadable := service.NewAPITokenService(unreadableTokens{&stubTokens{byID: map[uint]*model.APIToken{}}},
		f.userRepo, f.auth)

	_, err := unreadable.Resolve(ctx, service.APITokenPrefix+"something")
	if hasCode(err, "invalidToken") || apperror.KindOf(err) != apperror.KindInternal {
		t.Errorf("a token the database could not look up was answered %v, want the failure itself", err)
	}

	// And one that does not exist is still invalid.
	plain := service.NewAPITokenService(&stubTokens{byID: map[uint]*model.APIToken{}}, f.userRepo, f.auth)

	if _, err := plain.Resolve(ctx, service.APITokenPrefix+"something"); !hasCode(err, "invalidToken") {
		t.Errorf("a token that does not exist was answered %v, want invalidToken", err)
	}
}
