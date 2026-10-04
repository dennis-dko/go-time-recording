package service_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// stubTokens keeps tokens in memory and counts how often a last use is written.
type stubTokens struct {
	mu      sync.Mutex
	byID    map[uint]*model.APIToken
	touched int
}

func (s *stubTokens) Save(_ context.Context, token *model.APIToken) (*model.APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	saved := *token
	saved.ID = uint(len(s.byID) + 1)
	s.byID[saved.ID] = &saved

	return &saved, nil
}

func (s *stubTokens) GetByHash(_ context.Context, tokenHash string) (*model.APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, token := range s.byID {
		if token.TokenHash == tokenHash {
			found := *token

			return &found, nil
		}
	}

	return nil, apperror.NotFound("token", tokenHash)
}

func (s *stubTokens) ListForUser(_ context.Context, userID uint) ([]*model.APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*model.APIToken

	for _, token := range s.byID {
		if token.UserID == userID {
			found := *token
			out = append(out, &found)
		}
	}

	return out, nil
}

func (s *stubTokens) Delete(_ context.Context, id, _ uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[id]; !ok {
		return apperror.NotFound("token", strconv.FormatUint(uint64(id), 10))
	}

	delete(s.byID, id)

	return nil
}

func (s *stubTokens) TouchLastUsed(_ context.Context, id uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.byID[id].LastUsedAt = &now
	s.touched++

	return nil
}

// lastUsedAgo moves a token's recorded last use into the past.
func (s *stubTokens) lastUsedAgo(id uint, ago time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	then := time.Now().Add(-ago)
	s.byID[id].LastUsedAt = &then
}

func (s *stubTokens) writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.touched
}

// A request made with a token writes down that the token was used at most once a
// minute, as a request made with a session does.
//
// Every one wrote it, and a script is what a token is for: measured on SQLite,
// three hundred requests took 470 to 520 microseconds each without that write and
// 3.3 to 6.3 milliseconds with it - a write and its commit on every read, for a
// time the tokens card shows to the minute.
func TestATokenRecordsItsLastUseAtMostOnceAMinute(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	tokens := &stubTokens{byID: map[uint]*model.APIToken{}}
	svc := service.NewAPITokenService(tokens, f.userRepo, f.auth)

	// Past the initial password, which a token is refused on.
	user, err := f.userRepo.GetByID(ctx, f.userID)
	if err != nil {
		t.Fatal(err)
	}

	user.MustChangePassword = false

	if _, err := f.userRepo.Update(ctx, user); err != nil {
		t.Fatal(err)
	}

	issued, err := svc.Create(ctx, f.userID, "ci", 0)
	if err != nil {
		t.Fatal(err)
	}

	for range 5 {
		if _, err := svc.Resolve(ctx, issued.Secret); err != nil {
			t.Fatalf("the token was refused: %v", err)
		}
	}

	if got := tokens.writes(); got != 1 {
		t.Errorf("five requests within a minute wrote the token's last use %d times, want once", got)
	}

	// A minute on, it is written again: the card goes on telling an unused token
	// from one in use.
	tokens.lastUsedAgo(issued.Token.ID, 2*time.Minute)
	before := tokens.writes()

	if _, err := svc.Resolve(ctx, issued.Secret); err != nil {
		t.Fatalf("the token was refused: %v", err)
	}

	if tokens.writes() != before+1 {
		t.Error("a request two minutes after the recorded use left it as it was")
	}
}
