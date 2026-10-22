package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
)

// noErrors fails the case on anything the log could not keep.
type noErrors struct{ t *testing.T }

func (n noErrors) Errorf(format string, args ...any) { n.t.Errorf(format, args...) }

// A change is recorded once it has gone through, with the account that made it,
// what it was made to - the id in its path, or of what it created - and the one
// value worth keeping; the handler still reads the whole request. Nothing is
// recorded for a refusal, for a read, for a route that changes nothing about the
// installation, or for a caller nobody signed in as.
func TestAnAdministrativeChangeIsRecordedOnceItWentThrough(t *testing.T) {
	events := memory.NewAdminEventRepository()
	users := memory.NewUserRepository()
	log := service.NewAdminLogService(events, users, memory.NewRoleRepository(users))

	var handled string

	answers := map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/v1/users": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"id":9,"name":"Bea"}}`)
		},
		"PUT /api/v1/users/5/role": func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			handled = string(body)
			_, _ = io.WriteString(w, `{"data":{}}`)
		},
		"PUT /api/v1/roles/2": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		},
	}

	logged := AdministrationLog(log, noErrors{t})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if answer, found := answers[r.Method+" "+r.URL.Path]; found {
			answer(w, r)
		}
	}))

	ask := func(method, path, body string, signedIn bool) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if signedIn {
			r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{},
				&service.Principal{User: &model.User{ID: 3}}))
		}

		logged.ServeHTTP(httptest.NewRecorder(), r)
	}

	ask(http.MethodPost, "/api/v1/users", `{"name":"Bea"}`, true)
	ask(http.MethodPut, "/api/v1/users/5/role", `{"role":"user-admin"}`, true)
	ask(http.MethodPut, "/api/v1/roles/2", `{"name":"x"}`, true)
	ask(http.MethodPut, "/api/v1/settings/branding", `{}`, false)
	ask(http.MethodGet, "/api/v1/users", "", true)
	ask(http.MethodPut, "/api/v1/me/theme", `{"theme":"dark"}`, true)

	if handled != `{"role":"user-admin"}` {
		t.Errorf("the handler read %q, want the request whole", handled)
	}

	recorded, err := events.Before(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	if len(recorded) != 2 {
		t.Fatalf("the log holds %d entries, want the creation and the role: %+v", len(recorded), recorded)
	}

	role, created := recorded[0], recorded[1]

	if created.Action != "user.created" || created.ActorID != 3 || created.TargetID != 9 {
		t.Errorf("the creation reads %+v, want account 3 creating account 9", created)
	}

	if role.Action != "user.role" || role.TargetID != 5 || role.Detail != "user-admin" {
		t.Errorf("the role reads %+v, want account 5 given user-admin", role)
	}
}
