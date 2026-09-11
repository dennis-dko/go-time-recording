package rest

import (
	"gofr.dev/pkg/gofr"

	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// DatasourceRequest is the administered database connection.
type DatasourceRequest struct {
	Dialect  string `json:"dialect"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"sslMode"`
}

// DatasourceResponse reports the stored connection without its password.
type DatasourceResponse struct {
	DatasourceRequest

	HasPassword bool `json:"hasPassword"`

	// Active is what the running process is actually connected to, which
	// differs from the stored settings until the next restart.
	Active string `json:"active"`

	// Stored says whether the fields above came from anywhere. False on an
	// installation configured through the environment, where the screen has
	// nothing of its own to show and used to show nothing at all.
	Stored bool `json:"stored"`

	// Running is the connection this process opened, so a screen with nothing
	// stored can show what is in force instead of an empty form. No password:
	// the screen never receives one, and this is not the place to start.
	Running DatasourceRequest `json:"running"`

	// RestartRequired is always true after a change: GoFr opens the database
	// at start-up, and swapping it under running requests is not safe.
	RestartRequired bool `json:"restartRequired"`
}

// Datasource handles GET /api/v1/settings/datasource.
func (h *SettingsHandler) Datasource(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile)

	resp := DatasourceResponse{
		Active: h.activeDialect,
		Stored: ok,
		Running: DatasourceRequest{
			Dialect: h.running.Dialect,
			Name:    h.running.Name,
			Host:    h.running.Host,
			Port:    h.running.Port,
			User:    h.running.User,
			SSLMode: h.running.SSLMode,
		},
	}

	if ok {
		resp.DatasourceRequest = DatasourceRequest{
			Dialect: stored.Dialect,
			Name:    stored.Name,
			Host:    stored.Host,
			Port:    stored.Port,
			User:    stored.User,
			SSLMode: stored.SSLMode,
		}
		resp.HasPassword = stored.Password != ""
	}

	return resp, nil
}

// SaveDatasource handles PUT /api/v1/settings/datasource.
//
// The connection is written to a file and takes effect on the next start; it
// is deliberately not swapped at run time.
func (h *SettingsHandler) SaveDatasource(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req DatasourceRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	ds := appconfig.Datasource{
		Dialect:  req.Dialect,
		Name:     req.Name,
		Host:     req.Host,
		Port:     req.Port,
		User:     req.User,
		Password: req.Password,
		SSLMode:  req.SSLMode,
	}

	// Keep the stored password when the client sends none back.
	if ds.Password == "" {
		if stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile); ok {
			ds.Password = stored.Password
		}
	}

	if err := ds.Validate(); err != nil {
		return nil, toHTTPError(apperror.Invalidf("%v", err).WithCode("datasourceInvalid"))
	}

	if err := appconfig.SaveDatasource(appconfig.DatasourceFile, ds); err != nil {
		return nil, toHTTPError(apperror.Internal(err))
	}

	// No message. There used to be one, written in English here, and the interface
	// showed it in preference to its own translated sentence - so the one screen
	// that is otherwise entirely German answered a successful save in English.
	// What to call this is the interface's business; what happened is this one's.
	return map[string]any{
		"status":          "saved",
		"restartRequired": true,
	}, nil
}

// TestDatasource handles POST /api/v1/settings/datasource/test.
//
// It probes the connection without saving or switching to it, so the
// administrator can find a typo before restarting into a broken setting.
func (h *SettingsHandler) TestDatasource(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req DatasourceRequest
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	ds := appconfig.Datasource{
		Dialect:  req.Dialect,
		Name:     req.Name,
		Host:     req.Host,
		Port:     req.Port,
		User:     req.User,
		Password: req.Password,
		SSLMode:  req.SSLMode,
	}

	// An empty password means "use the stored one", the same as on save.
	if ds.Password == "" {
		if stored, ok := appconfig.LoadDatasource(appconfig.DatasourceFile); ok {
			ds.Password = stored.Password
		}
	}

	if err := appconfig.TestDatasource(c, ds); err != nil {
		// A failed probe is information, not a server fault - so it comes back
		// as a 200 with the reason in it rather than as an error status.
		//
		// The reason travels the way a refusal does, because half of these are
		// refusals: a field left empty is a fixed complaint the interface can
		// name and translate, and only what the driver says back - "connection
		// refused", "password authentication failed" - is prose nobody can
		// anticipate. Both arrive here; the client shows whichever it can.
		return map[string]any{"ok": false, "error": probeFailure(err)}, nil
	}

	// No message on the way out. What a success is called is the interface's
	// business and it has a translated sentence for it; this one was written
	// here in English and shown in preference to it, so a German screen
	// answered "connection established". A failure still carries its own text,
	// because what went wrong is not a fixed set of sentences code could
	// translate.
	return map[string]any{"ok": true}, nil
}
