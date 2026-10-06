package installer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// What the environment supplied of the connection goes to whoever holds the
// token, and to nobody else.
//
// The state route answers without a token, because the page needs the
// application's name before one has been typed - and it carried the prefill as
// well: the database's host, its port, its name and the account that opens it,
// to anybody who could reach the port, for as long as nobody had answered the
// installer. Not the password. But the installer runs on the port the
// application will have, before there is an account to sign in with, and those
// four are what somebody needs to know to start trying one.
func TestTheInstallerNamesTheConnectionOnlyToWhoeverHoldsTheToken(t *testing.T) {
	s := &server{cfg: Config{
		AppName: "Time Recording",
		Version: "v1.2.3",
		Token:   "the-token-from-the-log",
		Prefill: config.Datasource{
			Dialect: "postgres", Host: "db.internal", Port: "5432",
			Name: "hours", User: "gtr", Password: "never-sent", SSLMode: "require",
		},
	}}

	ask := func(token string) (string, stateResponse) {
		t.Helper()

		request := httptest.NewRequest(http.MethodGet, "/install/state", nil)
		if token != "" {
			request.Header.Set("X-Setup-Token", token)
		}

		answer := httptest.NewRecorder()
		s.state(answer, request)

		var state stateResponse

		if err := json.Unmarshal(answer.Body.Bytes(), &state); err != nil {
			t.Fatalf("the state is not readable: %v\n%s", err, answer.Body.String())
		}

		return answer.Body.String(), state
	}

	for _, token := range []string{"", "not-the-token"} {
		body, state := ask(token)

		// The page is labelled before anybody has typed anything.
		if state.AppName != "Time Recording" || state.Version != "v1.2.3" {
			t.Errorf("with the token %q the page is not told what it is setting up: %s", token, body)
		}

		for _, part := range []string{"db.internal", "hours", "gtr", "5432", "postgres"} {
			if strings.Contains(body, part) {
				t.Errorf("with the token %q the state names %q of the connection: %s", token, part, body)
			}
		}
	}

	body, state := ask("the-token-from-the-log")

	if state.Datasource == nil || state.Datasource.Host != "db.internal" || state.Datasource.User != "gtr" ||
		state.Datasource.Name != "hours" {
		t.Errorf("the token's holder is not offered what the environment supplied: %s", body)
	}

	if strings.Contains(body, "never-sent") {
		t.Errorf("the state carries the password: %s", body)
	}
}
