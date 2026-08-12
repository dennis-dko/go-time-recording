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

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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

// SupportedDialects are the databases this application can open, in the order
// they are offered.
//
// One list rather than a set of literals repeated per switch, because the same
// three names also have to appear in two dropdowns - the installer's and the
// Settings screen's - and nothing about a fourth being added would make anyone
// remember all four places. A test compares the markup against this.
func SupportedDialects() []string {
	return []string{"sqlite", "postgres", "mysql"}
}

// Validate reports why the connection cannot be used, if it cannot.
//
// The complaints name themselves rather than being prose. This is the one
// validation somebody meets constantly - it fires on every Test connection with
// a field still empty - and it used to answer "missing: host, user" in English,
// on a screen that was otherwise entirely German, beside labels that call those
// fields something else. A field this names is one the interface can mark and
// translate, because it is the name the payload uses.
func (d Datasource) Validate() error {
	switch strings.ToLower(d.Dialect) {
	case "sqlite":
		if strings.TrimSpace(d.Name) == "" {
			return apperror.InvalidFields("name")
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

		if d.Port != "" {
			if _, err := strconv.Atoi(d.Port); err != nil {
				missing = append(missing, "port")
			}
		}

		// All of them at once. Being told about the host, filling it in, and
		// then being told about the user is being told half of what is wrong -
		// and the port used to be checked only once the other three were
		// already right, so a form with an empty host and a port of "prod"
		// needed two attempts to learn both.
		if len(missing) > 0 {
			return apperror.InvalidFields(missing...)
		}

		return nil
	default:
		return apperror.Invalidf("unsupported dialect %q; use %s",
			d.Dialect, strings.Join(SupportedDialects(), ", ")).
			WithCode("unsupportedDialect", d.Dialect, strings.Join(SupportedDialects(), ", "))
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

// driverDSN builds the driver name and connection string for a probe.
//
// The port comes from DefaultPortFor rather than being defaulted per branch here,
// which is what lets this claim to probe the connection GoFr will open: the same
// function fills it in for GoFr, in ApplyDatasource.
func driverDSN(ds Datasource) (driver, dsn string, err error) {
	port := DefaultPortFor(ds.Dialect, ds.Port)

	switch strings.ToLower(ds.Dialect) {
	case "sqlite":
		return "sqlite", fmt.Sprintf("file:%s.db", strings.TrimSuffix(ds.Name, ".db")), nil
	case "postgres":
		sslMode := ds.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}

		return "postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			ds.Host, port, ds.User, ds.Password, ds.Name, sslMode), nil
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			ds.User, ds.Password, ds.Host, port, ds.Name)

		if tls := mysqlTLSParam(ds.SSLMode); tls != "" {
			dsn += "&" + tls
		}

		return "mysql", dsn, nil
	default:
		return "", "", fmt.Errorf("unsupported dialect %q", ds.Dialect)
	}
}

// mysqlTLSParam maps a stored SSL mode onto the DSN parameter the MySQL driver
// reads, the way GoFr does.
//
// The second half of the same mistake DefaultPortFor fixes. DB_SSL_MODE reads
// like a PostgreSQL setting and the comment beside it used to say so, but GoFr
// appends a tls= parameter for MySQL as well - so a MySQL deployment asking for
// TLS was proven over a plaintext connection here and then opened over TLS by
// GoFr. A check that connects differently from the thing it is checking is not a
// check.
//
// verify-ca and verify-full are the one case this cannot mirror exactly: GoFr
// registers a TLS config built from DB_TLS_CA_CERT under its own name, and
// reproducing that here would mean loading and parsing the operator's CA a
// second time, in a probe, to answer a question GoFr will answer properly a
// moment later. The probe uses skip-verify for those, which proves the server is
// reachable, speaks TLS and accepts the credentials - everything except the
// certificate's provenance, which is left to the start-up that actually enforces
// it.
func mysqlTLSParam(sslMode string) string {
	switch strings.ToLower(strings.TrimSpace(sslMode)) {
	case "preferred":
		return "tls=preferred"
	case "require", "true", "skip-verify", "verify-ca", "verify-full":
		return "tls=skip-verify"
	default:
		// "disable", "false", empty, and anything GoFr does not recognise - all
		// of which leave GoFr's DSN without a tls parameter too.
		return ""
	}
}

// DefaultPortFor fills in the port a dialect is served on, when none was given.
//
// It exists because GoFr and this package disagreed, silently and in the worst
// possible direction. GoFr defaults DB_PORT to 3306 for *every* dialect - the
// 5432 it knows about is applied only to the "supabase" dialect - while the
// pre-flight probe here defaulted PostgreSQL to 5432 and its own comment claimed
// to mirror GoFr. So a PostgreSQL deployment that set everything but the port
// passed the probe on 5432 and GoFr then dialled 3306, which is exactly the
// opaque mid-migration failure the probe was written to prevent: the check that
// exists to explain the problem was the one thing that said it was fine.
//
// One answer for both, resolved here and exported to GoFr by ApplyDatasource, so
// the port that was proven is the port that gets used.
func DefaultPortFor(dialect, port string) string {
	if strings.TrimSpace(port) != "" {
		return port
	}

	switch strings.ToLower(dialect) {
	case "postgres":
		return "5432"
	case "mysql":
		return "3306"
	default:
		// SQLite has no port, and an unsupported dialect is refused by Validate
		// long before anything dials anywhere.
		return ""
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

	// Filled in rather than passed through: an empty port here means GoFr falls
	// back to 3306 whatever the dialect, so leaving it out is how a PostgreSQL
	// deployment ends up dialling MySQL's port. See DefaultPortFor.
	if port := DefaultPortFor(ds.Dialect, ds.Port); port != "" {
		values["DB_PORT"] = port
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
