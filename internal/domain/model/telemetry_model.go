package model

import (
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
)

// Telemetry is what an administrator may say about the metrics endpoint and
// trace exporting.
//
// Neither can be switched while the application runs. GoFr binds the metrics
// port during start-up, on a listener of its own that never reaches the
// middleware chain, and it reads the tracing settings inside gofr.New() - so by
// the time there is a screen to administer them from, both decisions have been
// made. What is stored here is therefore applied at the *next* start, the same
// way the administered database connection is, and the screen says so.
//
// The three tracing fields are pointers, and nil means "whatever the
// configuration file configured". The distinction is not cosmetic: an empty
// exporter is a deliberate "off" that has to beat a TRACE_EXPORTER left in the
// file, while a nil exporter must leave that value alone. A plain string could
// not tell the two apart, and the first save from the screen would silently
// switch tracing off for every installation that had configured it in a file.
//
// What is deliberately *not* here is the metrics port. GoFr checks it at
// start-up and calls Fatalf when something already holds it, so a port that was
// free when it was saved and taken by the next start kills the process - and the
// screen that would correct it dies with it. It stays a deployment fact in the
// file, where a wrong value can be fixed without the application running.
type Telemetry struct {
	// MetricsOff stops the metrics endpoint being served at all. It is worth
	// being able to stop: the port carries no authentication, is not covered by
	// TLS, and serves Go's profiling endpoints beside the metrics.
	//
	// A plain bool rather than a pointer, because unlike the tracing fields there
	// is nothing here to tell apart. Switching the endpoint off is the only thing
	// this screen can promise: the port itself comes from the configuration file,
	// which has not been read yet at the moment these settings are applied, so
	// "on" could only mean "leave the file alone" - which is what false already
	// means.
	MetricsOff bool `json:"metricsOff,omitempty"`

	// TraceExporter is where spans go: empty for nowhere, otherwise one of the
	// supported exporters.
	TraceExporter *string `json:"traceExporter,omitempty"`

	// TracerURL is the collector's address as host:port. Not a URL despite the
	// name, which is GoFr's: the exporter speaks OTLP over gRPC and hands the
	// value to a gRPC dialer, which reads a scheme as part of the host and
	// resolves nothing.
	TracerURL *string `json:"tracerUrl,omitempty"`

	// TracerRatio is the share of traces sampled, between 0 and 1.
	TracerRatio *float64 `json:"tracerRatio,omitempty"`

	// LogLevel is how much the process writes, from DEBUG to FATAL.
	//
	// Here rather than left in the file because it is the one of these three an
	// administrator needs while something is wrong, and the log viewer already
	// in this application can only show what the process was started willing to
	// write. Applied at the next start like the rest: GoFr can change a level
	// while running, but it does so by assigning to a field that every request
	// goroutine reads without synchronisation, and a data race is not a
	// reasonable price for saving a restart.
	LogLevel *string `json:"logLevel,omitempty"`
}

// SupportedLogLevels are the levels GoFr emits, least to most severe.
//
// Anything else is not refused by GoFr but silently read as INFO, which is the
// worst of both: an administrator who typed "verbose" gets no more logging and
// no complaint. logsink.Levels lists the same six for the viewer's filter, and a
// test holds the two together.
func SupportedLogLevels() []string {
	return []string{"DEBUG", "INFO", "NOTICE", "WARN", "ERROR", "FATAL"}
}

// The exporters offered.
//
// GoFr also accepts "zipkin", which it warns is deprecated and to be replaced by
// "otlp", and "gofr", which posts every span to a service run by the framework's
// authors and defaults to doing so with no configuration at all. Neither is
// offered: the first is on its way out, and the second is not a decision anybody
// should be able to make by picking an entry from a list.
const (
	TraceExporterOTLP   = "otlp"
	TraceExporterJaeger = "jaeger"
)

// TracingOff is the stored exporter that means "export nowhere", as opposed to a
// nil exporter, which means the configuration file still decides.
const TracingOff = ""

// SupportedTraceExporters are the exporters offered, in the order they appear on
// the screen.
//
// A list rather than literals in a switch, because the same names also have to
// appear as options in the markup, and a test compares the two - an exporter the
// screen offers and the server refuses is a 400 nobody can act on, and one the
// server accepts but the screen never offers cannot be chosen at all.
func SupportedTraceExporters() []string {
	return []string{TraceExporterOTLP, TraceExporterJaeger}
}

// Normalise trims the collector address and drops what an exporter of "off"
// makes meaningless.
//
// Clearing rather than keeping, for the same reason SaveDatasource clears the
// server fields of a SQLite connection: a collector address left behind from a
// previous exporter reads, later, as a setting that is in force.
func (t Telemetry) Normalise() Telemetry {
	if t.TracerURL != nil {
		trimmed := strings.TrimSpace(*t.TracerURL)
		t.TracerURL = &trimmed
	}

	if t.TraceExporter != nil {
		trimmed := strings.ToLower(strings.TrimSpace(*t.TraceExporter))
		t.TraceExporter = &trimmed

		if trimmed == TracingOff {
			t.TracerURL, t.TracerRatio = nil, nil
		}
	}

	// Upper-cased rather than rejected for its case: GoFr reads the level
	// case-insensitively, so "debug" is not a mistake, and storing it as it will
	// be read keeps the screen and the process saying the same word.
	if t.LogLevel != nil {
		level := strings.ToUpper(strings.TrimSpace(*t.LogLevel))
		t.LogLevel = &level
	}

	return t
}

// TracingAdministered reports whether spans are to be exported, as opposed to
// switched off here or left to the configuration file.
func (t Telemetry) TracingAdministered() bool {
	return t.TraceExporter != nil && *t.TraceExporter != TracingOff
}

// Administered reports whether anything at all was decided here.
//
// It answers one question, at start-up: whether a read that failed lost
// something. Nothing administered is the ordinary state of a fresh installation
// and worth no mention; anything administered that did not reach the process is
// a screen showing settings the process is not running on, which is worth saying
// out loud.
func (t Telemetry) Administered() bool {
	return t.MetricsOff || t.LogLevel != nil ||
		t.TraceExporter != nil || t.TracerURL != nil || t.TracerRatio != nil
}

// InvalidTelemetryFields lists the fields whose values could not be used.
//
// Every rule here exists because GoFr fails these quietly. An exporter it does
// not recognise is logged once and then hands a nil exporter to the batch
// processor, which accepts it and drops every span; a collector address with a
// scheme in front of it produces "too many colons in address" on each export,
// inside the exporter, where nobody is looking; and a sampling ratio it cannot
// parse leaves the ratio at zero, which samples nothing. All three look exactly
// like working tracing from the screen that configured them.
func (t Telemetry) InvalidTelemetryFields() []string {
	var invalid []string

	if t.TraceExporter != nil &&
		*t.TraceExporter != TracingOff &&
		!slices.Contains(SupportedTraceExporters(), *t.TraceExporter) {
		invalid = append(invalid, "traceExporter")
	}

	// Required rather than optional once an exporter is chosen: GoFr refuses the
	// pair without it and logs "missing TRACER_URL config", so tracing would be
	// off while the screen showed an exporter.
	if t.TracingAdministered() && (t.TracerURL == nil || *t.TracerURL == "") {
		invalid = append(invalid, "tracerUrl")
	} else if t.TracerURL != nil && *t.TracerURL != "" && !isHostPort(*t.TracerURL) {
		invalid = append(invalid, "tracerUrl")
	}

	// NaN is checked before the bounds rather than left to them, because it
	// compares false against both and would sail through. It must not: encoding
	// it back out fails, so a single stored NaN would turn the screen that could
	// correct it into an error with no value on it.
	if t.TracerRatio != nil &&
		(math.IsNaN(*t.TracerRatio) || *t.TracerRatio < 0 || *t.TracerRatio > 1) {
		invalid = append(invalid, "tracerRatio")
	}

	// An empty level is allowed and means the same as an absent one, so that
	// clearing the field on the screen goes back to following the file rather
	// than storing a blank GoFr would read as INFO.
	if t.LogLevel != nil && *t.LogLevel != "" &&
		!slices.Contains(SupportedLogLevels(), *t.LogLevel) {
		invalid = append(invalid, "logLevel")
	}

	return invalid
}

// isHostPort reports whether the address is the bare host:port a gRPC dialer can
// use.
//
// A scheme or a path is the mistake worth catching, because it is the form every
// collector's own documentation shows: "http://jaeger:4317" is what an
// administrator will type, and it is exactly what does not work.
func isHostPort(address string) bool {
	if strings.Contains(address, "://") || strings.Contains(address, "/") {
		return false
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return false
	}

	number, err := strconv.Atoi(port)

	return err == nil && number > 0 && number <= MaxPort
}

// MaxPort is the highest port number a collector address may name.
const MaxPort = 65535
