package installer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// refused is as much of a refusal as these cases read.
type refused struct {
	Error  string   `json:"error"`
	Code   string   `json:"code"`
	Values []string `json:"values"`
}

func refusalOf(t *testing.T, rec *httptest.ResponseRecorder) refused {
	t.Helper()

	var answer refused
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("the refusal is not JSON: %v\n%s", err, rec.Body.String())
	}

	return answer
}

// A connection that cannot be opened is named for the page, and what the driver
// said is kept.
//
// It is the commonest thing to go wrong on this screen, and it travelled as the
// probe's own English and nothing else - so the page, which has a sentence for a
// wrong token and for an empty field, had none for this and printed the server's.
func TestAFailedProbeIsNamedAndKeepsWhatTheDriverSaid(t *testing.T) {
	s := &server{
		cfg:  Config{Token: "the-token", DatasourceFile: filepath.Join(t.TempDir(), "datasource.json"), Logf: t.Logf},
		done: make(chan config.Datasource, 1),
	}

	// Nothing listens on port 1, so the refusal is immediate.
	const nobody = `{"dialect":"postgres","name":"gtr","host":"127.0.0.1","port":"1","user":"gtr","password":"x"}`

	for path, handler := range map[string]http.HandlerFunc{
		"/install/test": s.test, "/install/save": s.save,
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(nobody))
		req.Header.Set("X-Setup-Token", "the-token")

		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d to a connection nothing answers, want %d",
				path, rec.Code, http.StatusBadRequest)
		}

		answer := refusalOf(t, rec)

		if answer.Code != "probeFailed" {
			t.Errorf("%s names the failure %q, so the page can only print the English: %s",
				path, answer.Code, answer.Error)
		}

		if len(answer.Values) != 1 || !strings.Contains(answer.Values[0], "127.0.0.1:1") {
			t.Errorf("%s carries %q for the sentence to hold, which has lost the address "+
				"the driver named", path, answer.Values)
		}
	}
}

// A connection that works and cannot be written down is named as well, and the
// installer goes on waiting for an answer it can keep.
//
// A configuration directory this process may not write to is how a first start
// in a container goes wrong, and the operating system's words for it are all the
// detail there is.
func TestAConnectionThatCannotBeSavedIsNamed(t *testing.T) {
	dir := t.TempDir()

	// A file where the directory should be, which no platform can write into.
	blocker := filepath.Join(dir, "configs")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("placing the obstacle: %v", err)
	}

	s := &server{
		cfg:  Config{Token: "the-token", DatasourceFile: filepath.Join(blocker, "datasource.json"), Logf: t.Logf},
		done: make(chan config.Datasource, 1),
	}

	body := `{"dialect":"sqlite","name":"` + filepath.ToSlash(filepath.Join(dir, "chosen")) + `"}`

	req := httptest.NewRequest(http.MethodPost, "/install/save", strings.NewReader(body))
	req.Header.Set("X-Setup-Token", "the-token")

	rec := httptest.NewRecorder()
	s.save(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a connection that could not be written was answered with %d", rec.Code)
	}

	answer := refusalOf(t, rec)

	if answer.Code != "cannotSaveConnection" {
		t.Errorf("the failure is named %q, so the page can only print the English: %s",
			answer.Code, answer.Error)
	}

	if len(answer.Values) != 1 || answer.Values[0] == "" {
		t.Errorf("the failure carries %q, which has lost the operating system's reason",
			answer.Values)
	}

	if s.saved {
		t.Error("the installer counts a connection it could not write as saved, so a " +
			"corrected answer would be refused")
	}
}
