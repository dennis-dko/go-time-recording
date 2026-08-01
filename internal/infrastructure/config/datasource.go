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
