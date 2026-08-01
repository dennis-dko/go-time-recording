package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
