package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// sessionStoreAnswering answers every lookup with one error.
type sessionStoreAnswering struct {
	err error
}

func (s sessionStoreAnswering) Get(context.Context, string) (*model.Session, error) {
	return nil, s.err
}

func (sessionStoreAnswering) Save(context.Context, *model.Session) error { return nil }

func (sessionStoreAnswering) Touch(context.Context, string, time.Time) error { return nil }

func (sessionStoreAnswering) Delete(context.Context, string) error { return nil }

func (sessionStoreAnswering) DeleteForUser(context.Context, uint) error { return nil }

func (sessionStoreAnswering) DeleteForUserExcept(context.Context, uint, string) error { return nil }

func (sessionStoreAnswering) DeleteExpired(context.Context) (int64, error) { return 0, nil }

// A session the database could not read keeps its cookie, and a handler that
// needs the caller says they could not be checked rather than that nobody is
// signed in.
//
// The cookie was cleared on every failed lookup, so a database that did not
// answer for a moment - a restart of its container is enough - signed out
// everybody whose request arrived in it, and the 401 that came with it put each
// of their screens on the sign-in form.
func TestASessionThatCannotBeReadIsNeitherClearedNorCalledSignedOut(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		lookup      error
		wantCleared bool
		wantStatus  int
	}{
		{"unreadable", apperror.Internal(errors.New("the database went away")), false, http.StatusInternalServerError},
		{"gone", apperror.NotFound("session", "a-token"), true, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			users := memory.NewUserRepository()
			roles := memory.NewRoleRepository(users)
			sessions := service.NewSessionService(users, roles, sessionStoreAnswering{err: tc.lookup},
				service.NewAuthService(users, roles), time.Hour)

			status := 0

			handler := SessionMiddleware(sessions)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, err := NewAuthorizer(true).Principal(&gofr.Context{
					Context: r.Context(),
					Request: gofrHTTP.NewRequest(r),
				})

				var coded interface{ StatusCode() int }
				if errors.As(err, &coded) {
					status = coded.StatusCode()
				}
			}))

			r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "a-token"})

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			cleared := strings.Contains(w.Header().Get("Set-Cookie"), SessionCookieName+"=")

			if cleared != tc.wantCleared {
				t.Errorf("the session cookie cleared: %v, want %v", cleared, tc.wantCleared)
			}

			if status != tc.wantStatus {
				t.Errorf("a handler asking for the caller was answered %d, want %d", status, tc.wantStatus)
			}
		})
	}
}
