package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// This is the logic that decides whether the binary serves its installer or the
// application, and what it connects to if it serves the application. Getting it
// wrong has two shapes, both bad: an installation that runs the installer over a
// database it was already using, or one that quietly connects somewhere nobody
// chose.

func TestValidateAcceptsWhatEachDialectActuallyNeeds(t *testing.T) {
	valid := []config.Datasource{
		// SQLite is a file. There is no server, so there is nothing else to ask.
		{Dialect: "sqlite", Name: "go-time-recording"},
		{Dialect: "SQLite", Name: "go-time-recording"},
		{Dialect: "postgres", Name: "gtr", Host: "db", User: "gtr"},
		{Dialect: "postgres", Name: "gtr", Host: "db", User: "gtr", Port: "5432"},
		{Dialect: "mysql", Name: "gtr", Host: "db", User: "root"},
	}

	for _, ds := range valid {
		if err := ds.Validate(); err != nil {
			t.Errorf("%+v should be valid: %v", ds, err)
		}
	}
}

func TestValidateRefusesWhatCannotWork(t *testing.T) {
	cases := map[string]config.Datasource{
		"no dialect at all":      {Name: "gtr"},
		"a dialect nobody has":   {Dialect: "oracle", Name: "gtr"},
		"sqlite without a file":  {Dialect: "sqlite"},
		"sqlite with only space": {Dialect: "sqlite", Name: "   "},
		"postgres without host":  {Dialect: "postgres", Name: "gtr", User: "gtr"},
		"postgres without user":  {Dialect: "postgres", Name: "gtr", Host: "db"},
		"postgres without name":  {Dialect: "postgres", Host: "db", User: "gtr"},
		"a port that is a word":  {Dialect: "postgres", Name: "gtr", Host: "db", User: "gtr", Port: "prod"},
		"mysql without user":     {Dialect: "mysql", Name: "gtr", Host: "db"},
	}

	for name, ds := range cases {
		if err := ds.Validate(); err == nil {
			t.Errorf("%s should be refused: %+v", name, ds)
		}
	}
}

// A password left in the file after switching to SQLite would sit there
// indefinitely, describing a server nothing connects to any more.
func TestSavingSQLiteClearsTheServerFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datasource.json")

	err := config.SaveDatasource(path, config.Datasource{
		Dialect: "sqlite", Name: "gtr",
		Host: "db", Port: "5432", User: "gtr", Password: "secret", SSLMode: "require",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	for _, gone := range []string{"secret", "5432", "require"} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("%q is still in the saved file:\n%s", gone, raw)
		}
	}

	loaded, ok := config.LoadDatasource(path)
	if !ok {
		t.Fatal("the saved connection could not be read back")
	}

	if loaded.Dialect != "sqlite" || loaded.Name != "gtr" {
		t.Errorf("read back %+v, want the SQLite connection", loaded)
	}
}

// An invalid connection must not reach the file. The process reads it at the next
// start and would then be unable to serve either the application or the screen
// that could fix it.
func TestSavingRefusesAnInvalidConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datasource.json")

	if err := config.SaveDatasource(path, config.Datasource{Dialect: "sqlite"}); err == nil {
		t.Error("an invalid connection was saved")
	}

	if _, err := os.Stat(path); err == nil {
		t.Error("the file was written despite the refusal")
	}
}

func TestLoadingReportsAbsenceRatherThanGuessing(t *testing.T) {
	dir := t.TempDir()

	if _, ok := config.LoadDatasource(filepath.Join(dir, "missing.json")); ok {
		t.Error("a missing file was reported as a configured connection")
	}

	// Not JSON at all - a half-written file, or something edited by hand.
	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, ok := config.LoadDatasource(broken); ok {
		t.Error("an unparseable file was reported as a configured connection")
	}

	// Valid JSON with no dialect describes nothing, and must not be mistaken for
	// a decision - that is what sends the binary to its installer.
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"name":"gtr"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, ok := config.LoadDatasource(empty); ok {
		t.Error("a connection with no dialect was reported as configured")
	}
}

// DatabaseConfigured is the switch between installer and application, so what it
// reads matters more than most things here.
func TestDatabaseConfiguredFollowsTheEnvironment(t *testing.T) {
	// No configs/ directory here, so only the real environment can answer -
	// which is what the test wants to vary.
	t.Chdir(t.TempDir())

	t.Setenv("DB_DIALECT", "")

	if config.DatabaseConfigured() {
		t.Error("no dialect anywhere should mean no database is configured")
	}

	if _, ok := config.DatasourceFromEnvironment(); ok {
		t.Error("no dialect anywhere should yield no connection")
	}

	t.Setenv("DB_DIALECT", "postgres")
	t.Setenv("DB_NAME", "gtr")
	t.Setenv("DB_HOST", "db.example")
	t.Setenv("DB_USER", "gtr")
	t.Setenv("DB_PASSWORD", "secret")

	if !config.DatabaseConfigured() {
		t.Error("a dialect in the environment should mean a database is configured")
	}

	ds, ok := config.DatasourceFromEnvironment()
	if !ok {
		t.Fatal("a configured environment yielded no connection")
	}

	if ds.Dialect != "postgres" || ds.Name != "gtr" || ds.Host != "db.example" {
		t.Errorf("read %+v, want the environment's connection", ds)
	}

	// The password has to come through: this connection is probed before GoFr
	// uses it, and probing without the password would report a failure that is
	// not there.
	if ds.Password != "secret" {
		t.Error("the password did not come through, so the pre-flight probe would fail spuriously")
	}
}

// Whitespace is what a hand-edited .env file leaves behind, and " " is not a
// configured database.
func TestABlankDialectIsNotAConfiguredDatabase(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DB_DIALECT", "   ")

	if config.DatabaseConfigured() {
		t.Error("whitespace should not count as a configured dialect")
	}
}
