package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/sqldb"
)

// StoredTelemetry reads the administered metrics and tracing settings from the
// database, before GoFr has opened it.
//
// It has to be before: GoFr binds the metrics port and builds the trace exporter
// inside gofr.New(), so anything read afterwards is a start too late. That is why
// this goes around the application's own repositories rather than through them -
// those need the datasource GoFr has not created yet - and opens the same
// connection TestDatasource has just proved, with the same drivers.
//
// A read that fails is not an error worth stopping for. On a first start the
// settings table does not exist, because the migrations run after this; and a
// database that is genuinely unreachable was already caught by TestDatasource a
// moment ago. The caller reports it and carries on with the configuration file's
// values.
func StoredTelemetry(ctx context.Context, ds Datasource) (model.Telemetry, error) {
	driver, dsn, err := driverDSN(ds)
	if err != nil {
		return model.Telemetry{}, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return model.Telemetry{}, err
	}

	defer func() { _ = db.Close() }()

	readCtx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	// The application's own repository over a plain *sql.DB, which satisfies it.
	// Reading the row with a hand-written query instead would be a second place
	// that has to agree with the schema and with the dialect's placeholders.
	raw, err := sqldb.NewSettingsRepository(db, ds.Dialect).Get(readCtx, model.SettingTelemetry)
	if err != nil {
		return model.Telemetry{}, err
	}

	if raw == "" {
		return model.Telemetry{}, nil
	}

	var telemetry model.Telemetry
	if err := json.Unmarshal([]byte(raw), &telemetry); err != nil {
		// The same reading the settings service takes: a corrupt entry falls back
		// to the configuration file rather than stopping the application that
		// holds the screen to repair it.
		return model.Telemetry{}, nil
	}

	telemetry = telemetry.Normalise()

	// Checked again on the way out, not only on the way in. Everything saved
	// through the screen has passed this already, so what it catches is a row
	// edited by hand or written by a version that allowed something this one does
	// not - and the cost of exporting it anyway is GoFr accepting the value and
	// then dropping every span without a word.
	if invalid := telemetry.InvalidTelemetryFields(); len(invalid) > 0 {
		return model.Telemetry{}, fmt.Errorf("the stored settings are unusable: %s",
			strings.Join(invalid, ", "))
	}

	return telemetry, nil
}

// ApplyTelemetry exports the administered settings as the environment variables
// GoFr reads when it builds its own configuration.
//
// The same mechanism as ApplyDatasource, and it must run before gofr.New() for
// the same reason. What is worth knowing is why an *empty* value works as an
// override: GoFr snapshots the real environment before it loads its .env files
// and writes that snapshot back afterwards, and a variable set to an empty string
// is still in that snapshot. So exporting TRACE_EXPORTER="" is not the same as
// leaving it unset - it beats a value in the file, which is exactly what
// "switched off here" has to do.
//
// A nil field is left alone rather than cleared, so anything the administrator
// has not decided keeps coming from the file.
func ApplyTelemetry(telemetry model.Telemetry) error {
	// Normalised here as well as on the way into the database, so this function
	// does not depend on having been handed something already tidied: switching
	// tracing off is what clears the collector address, and a caller that skipped
	// that step would otherwise export an address with no exporter to use it.
	telemetry = telemetry.Normalise()

	values := map[string]string{}

	// "0" rather than "": GoFr reads this one with GetOrDefault, which treats an
	// empty value as absent and would fall back to its default port - switching
	// the endpoint on instead of off.
	if telemetry.MetricsOff {
		values["METRICS_PORT"] = "0"
	}

	if telemetry.TraceExporter != nil {
		values["TRACE_EXPORTER"] = *telemetry.TraceExporter

		// Cleared together with the exporter. Left behind, a collector address
		// from the file would meet an empty exporter, which GoFr reports as
		// "missing TRACE_EXPORTER config" on every start - an error line about a
		// setting somebody deliberately switched off.
		if *telemetry.TraceExporter == model.TracingOff {
			values["TRACER_URL"] = ""
		}
	}

	if telemetry.TracerURL != nil && strings.TrimSpace(*telemetry.TracerURL) != "" {
		values["TRACER_URL"] = strings.TrimSpace(*telemetry.TracerURL)
	}

	// An empty level would be read by GoFr as INFO rather than as "not set", so
	// it is treated here as the absence it means and the file keeps deciding.
	//
	// Still exported even though the level is now applied by the log sink rather
	// than by GoFr: the sink only exists where the output is captured, and where
	// it is not, this is what carries the setting. It is also what
	// EffectiveLogLevel reads back, so the two cannot answer differently.
	if telemetry.LogLevel != nil && *telemetry.LogLevel != "" {
		values["LOG_LEVEL"] = *telemetry.LogLevel
	}

	if telemetry.TracerRatio != nil {
		// 'f' with the shortest exact precision: GoFr parses this with
		// ParseFloat, which reads "1e-05" but not the comma a German locale would
		// produce, and reports a parse failure by sampling nothing at all.
		values["TRACER_RATIO"] = strconv.FormatFloat(*telemetry.TracerRatio, 'f', -1, 64)
	}

	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return nil
}

// TelemetryFromConfig is what the configuration file and the environment say,
// before anything stored in the database is applied over them.
//
// It has to be read before ApplyTelemetry, and that is the whole reason it
// exists. ApplyTelemetry works by setting environment variables, and a real
// environment variable beats the file it came from - so once it has run there is
// no reading the file's own answer back.
//
// What needs that answer is the question "would a restart change anything". A
// setting cleared back to "follow the configuration file" is a change, and
// comparing an absent stored value against the running process says nothing
// about it: the file's value is what the next start would use.
func TelemetryFromConfig() Telemetry {
	p := gofrConfig()

	return Telemetry{
		LogLevel:      logLevel(p.Get("LOG_LEVEL")),
		MetricsPort:   metricsPort(p.Get("METRICS_PORT")),
		TraceExporter: strings.ToLower(strings.TrimSpace(p.Get("TRACE_EXPORTER"))),
		TracerURL:     strings.TrimSpace(p.Get("TRACER_URL")),
		TracerRatio:   traceRatio(p.GetOrDefault("TRACER_RATIO", "1")),
	}
}

// EffectiveLogLevel is the level this process should actually emit at.
//
// The administered value where there is one, and whatever the configuration
// file says otherwise - resolved the same way GoFr resolves it, so an
// unrecognised name is INFO here exactly as it would be there.
//
// Called after ApplyTelemetry, which has already exported an administered level
// into the environment, so reading the environment answers both cases with one
// lookup rather than repeating the precedence rule in a second place.
func EffectiveLogLevel(telemetry model.Telemetry) string {
	if telemetry.LogLevel != nil && strings.TrimSpace(*telemetry.LogLevel) != "" {
		return logLevel(*telemetry.LogLevel)
	}

	return logLevel(gofrConfig().Get("LOG_LEVEL"))
}
