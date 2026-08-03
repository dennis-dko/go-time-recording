package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gofrconfig "gofr.dev/pkg/gofr/config"
	"gofr.dev/pkg/gofr/logging"

	// Registered for the connection test only. GoFr opens the application's
	// own connection; these let the settings screen probe a target before
	// anyone restarts into it. They are the same drivers GoFr uses, so a
	// successful probe means the real connection will work too.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// DatasourceFile is where the administered database connection is stored.
//
// It is a file, not a row in the database: the settings describe which
// database to open, so storing them inside that database would make it
// impossible to point the application somewhere else.
const DatasourceFile = "configs/datasource.json"

// Datasource is the administered database connection.
type Datasource struct {
	Dialect  string `json:"dialect"`
	Name     string `json:"name"`
	Host     string `json:"host,omitempty"`
	Port     string `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	SSLMode  string `json:"sslMode,omitempty"`
}

// Validate reports why the connection cannot be used, if it cannot.
func (d Datasource) Validate() error {
	switch strings.ToLower(d.Dialect) {
	case "sqlite":
		if strings.TrimSpace(d.Name) == "" {
			return fmt.Errorf("a database name is required")
		}

		return nil
	case "postgres", "mysql":
		var missing []string

		if strings.TrimSpace(d.Host) == "" {
			missing = append(missing, "host")
		}

		if strings.TrimSpace(d.Name) == "" {
			missing = append(missing, "name")
		}

		if strings.TrimSpace(d.User) == "" {
			missing = append(missing, "user")
		}

		if len(missing) > 0 {
			return fmt.Errorf("missing: %s", strings.Join(missing, ", "))
		}

		if d.Port != "" {
			if _, err := strconv.Atoi(d.Port); err != nil {
				return fmt.Errorf("port must be a number")
			}
		}

		return nil
	default:
		return fmt.Errorf("unsupported dialect %q; use sqlite, postgres or mysql", d.Dialect)
	}
}

// ConfigLocation is where GoFr looks for its .env files, and therefore where
// DatabaseConfigured has to look too.
const ConfigLocation = "./configs"

// DatabaseConfigured reports whether a database is configured anywhere GoFr
// would read one from.
//
// It answers the question that decides whether this binary serves its installer
// or the application, so it has to see exactly what GoFr will: .env, then the
// stage's own file, then real environment variables. GoFr's own reader is used
// rather than a second implementation of that precedence, because the two
// disagreeing would mean an installation that runs the installer and then
// connects somewhere else - or the reverse, which is worse.
//
// The logger discards: this runs before the application has said anything, and
// "Loaded config from file" twice in the first two lines invites the reader to
// look for a bug that is not there.
func DatabaseConfigured() bool {
	return strings.TrimSpace(gofrConfig().Get("DB_DIALECT")) != ""
}

// DatasourceFromEnvironment reads the connection GoFr would assemble from its
// configuration, password included.
//
// Used to probe it before GoFr does. GoFr's failure here is a fatal from inside
// its migration step - "failed to create gofr_migration table, err: dial tcp" -
// which names the symptom and not the cause, and arrives through the pipe the
// log viewer reads from, moments before the process exits. Checking first turns
// that into a sentence naming the host and the actual refusal.
//
// The second return value is false when no dialect is configured, which is the
// case that sends the binary to its installer instead.
func DatasourceFromEnvironment() (Datasource, bool) {
	cfg := gofrConfig()

	dialect := strings.TrimSpace(cfg.Get("DB_DIALECT"))
	if dialect == "" {
		return Datasource{}, false
	}

	return Datasource{
		Dialect:  dialect,
		Name:     strings.TrimSpace(cfg.Get("DB_NAME")),
		Host:     strings.TrimSpace(cfg.Get("DB_HOST")),
		Port:     strings.TrimSpace(cfg.Get("DB_PORT")),
		User:     strings.TrimSpace(cfg.Get("DB_USER")),
		Password: cfg.Get("DB_PASSWORD"),
		SSLMode:  strings.TrimSpace(cfg.Get("DB_SSL_MODE")),
	}, true
}

// Installer is what the first-run screen needs before GoFr exists.
type Installer struct {
	// HTTPPort is the port the application would have used, so the installer
	// answers where the operator already looked.
	HTTPPort string

	// AppName labels the page.
	AppName string

	// SetupToken is SETUP_TOKEN, empty when one should be generated.
	SetupToken string

	// Prefill is whatever the environment already supplied. An operator who set
	// DB_HOST in a compose file but forgot DB_DIALECT should not retype the
	// rest.
	Prefill Datasource
}

// InstallerSettings reads the few settings the first-run screen needs.
//
// Separate from Load because that takes GoFr's config, and GoFr cannot be
// constructed before the database question is answered - which is the question
// the installer exists to answer.
func InstallerSettings() Installer {
	cfg := gofrConfig()

	return Installer{
		HTTPPort:   valueOr(cfg, "HTTP_PORT", "8000"),
		AppName:    valueOr(cfg, "APP_NAME", "Time Recording"),
		SetupToken: strings.TrimSpace(cfg.Get("SETUP_TOKEN")),
		Prefill: Datasource{
			Dialect: strings.TrimSpace(cfg.Get("DB_DIALECT")),
			Name:    strings.TrimSpace(cfg.Get("DB_NAME")),
			Host:    strings.TrimSpace(cfg.Get("DB_HOST")),
			Port:    strings.TrimSpace(cfg.Get("DB_PORT")),
			User:    strings.TrimSpace(cfg.Get("DB_USER")),
			SSLMode: strings.TrimSpace(cfg.Get("DB_SSL_MODE")),
		},
	}
}

func valueOr(cfg Provider, key, fallback string) string {
	if value := strings.TrimSpace(cfg.Get(key)); value != "" {
		return value
	}

	return fallback
}

// gofrConfig reads the .env layering the way GoFr will, with a logger that
// discards: this runs before the application has said anything, and "Loaded
// config from file" twice in the first two lines invites the reader to look for
// a bug that is not there.
func gofrConfig() Provider {
	location := ""
	if _, err := os.Stat(ConfigLocation); err == nil {
		location = ConfigLocation
	}

	return gofrconfig.NewEnvFile(location, logging.NewFileLogger(""))
}

// LoadDatasource reads the administered connection, if one has been saved.
func LoadDatasource(path string) (Datasource, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Datasource{}, false
	}

	var ds Datasource
	if err := json.Unmarshal(raw, &ds); err != nil {
		return Datasource{}, false
	}

	if ds.Dialect == "" {
		return Datasource{}, false
	}

	return ds, true
}

// SaveDatasource writes the connection for the next start.
func SaveDatasource(path string, ds Datasource) error {
	if err := ds.Validate(); err != nil {
		return err
	}

	// SQLite is a local file and uses none of the server fields. Clearing them
	// keeps a password from a previous server connection from lingering in the
	// file long after it stopped being used.
	if strings.EqualFold(ds.Dialect, "sqlite") {
		ds.Host, ds.Port, ds.User, ds.Password, ds.SSLMode = "", "", "", "", ""
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}

	// 0600: the file holds a database password.
	return os.WriteFile(path, raw, 0o600)
}

// TestDatasource opens the connection and runs a trivial query, so the
// administrator learns whether the settings work before restarting into them.
//
// The connection is opened and closed here rather than handed to the
// application: this is a probe, not a switch.
func TestDatasource(ctx context.Context, ds Datasource) error {
	if err := ds.Validate(); err != nil {
		return err
	}

	driver, dsn, err := driverDSN(ds)
	if err != nil {
		return err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("cannot open the connection: %w", err)
	}

	defer func() { _ = db.Close() }()

	probeCtx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	if err := db.PingContext(probeCtx); err != nil {
		return fmt.Errorf("cannot reach the database: %w", err)
	}

	// A ping can succeed against a server while the named database is missing
	// or unreadable, so a real statement is issued too.
	if _, err := db.ExecContext(probeCtx, "SELECT 1"); err != nil {
		return fmt.Errorf("connected, but the database is not usable: %w", err)
	}

	return nil
}

// testTimeout keeps an unreachable host from holding the request open.
const testTimeout = 8 * time.Second

// driverDSN builds the driver name and connection string for a probe. It
// mirrors what GoFr assembles internally from the same settings.
func driverDSN(ds Datasource) (driver, dsn string, err error) {
	port := ds.Port

	switch strings.ToLower(ds.Dialect) {
	case "sqlite":
		return "sqlite", fmt.Sprintf("file:%s.db", strings.TrimSuffix(ds.Name, ".db")), nil
	case "postgres":
		if port == "" {
			port = "5432"
		}

		sslMode := ds.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}

		return "postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			ds.Host, port, ds.User, ds.Password, ds.Name, sslMode), nil
	case "mysql":
		if port == "" {
			port = "3306"
		}

		return "mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			ds.User, ds.Password, ds.Host, port, ds.Name), nil
	default:
		return "", "", fmt.Errorf("unsupported dialect %q", ds.Dialect)
	}
}

// ApplyDatasource exports the connection as the environment variables GoFr
// reads when it builds its own configuration.
//
// It must run *before* gofr.New(). GoFr lets real environment variables win
// over the values in its .env files, which is what makes this override work
// without touching GoFr's API.
func ApplyDatasource(ds Datasource) error {
	values := map[string]string{
		"DB_DIALECT": strings.ToLower(ds.Dialect),
		"DB_NAME":    ds.Name,
	}

	if ds.Host != "" {
		values["DB_HOST"] = ds.Host
	}

	if ds.Port != "" {
		values["DB_PORT"] = ds.Port
	}

	if ds.User != "" {
		values["DB_USER"] = ds.User
	}

	if ds.Password != "" {
		values["DB_PASSWORD"] = ds.Password
	}

	if ds.SSLMode != "" {
		values["DB_SSL_MODE"] = ds.SSLMode
	}

	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return nil
}
