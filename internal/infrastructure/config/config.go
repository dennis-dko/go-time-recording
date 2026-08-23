// Package config exposes the application-specific settings that sit on top of
// the configuration GoFr already reads for itself (ports, database, log level,
// tracing). Only settings this application acts on belong here - or, for the
// metrics endpoint and the trace exporter, reports on: those two are GoFr's to
// act on, and are read here so the Settings screen can show what this process is
// actually doing rather than leaving it to be guessed from a file.
package config

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// Provider is the subset of GoFr's config.Config this package needs, kept
// narrow so tests can supply a plain map instead of a full GoFr container.
type Provider interface {
	Get(key string) string
	GetOrDefault(key, fallback string) string
}

// Config holds the settings this application interprets itself.
type Config struct {
	// Dialect mirrors DB_DIALECT. Repositories need it because placeholder
	// syntax and "insert returning id" differ per dialect.
	Dialect string

	// UpdateCheck asks the release feed whether a newer version exists, and is
	// what makes the update card on the administration screen say anything.
	//
	// On by default: an installation that never learns a fix exists is not safer
	// for it. Off is for a deployment that must not reach the internet at all,
	// where the check would fail on every visit to that screen and say nothing
	// useful anyway - the same reasoning as the framework's own call home, and
	// the same switch shape.
	UpdateCheck bool

	// UpdateFeed is where to ask. Its own setting so an installation behind a
	// proxy, or one running its own fork, can point it somewhere reachable
	// rather than having to switch the whole thing off.
	UpdateFeed string

	// UpdateToken identifies this installation to the release feed. Optional, and
	// empty on almost every installation: checking for a release needs no
	// credentials.
	//
	// It exists because the limit is counted per address rather than per
	// installation - sixty checks an hour, shared by everything behind one office
	// connection - and running out answers 403, which reads as a permission
	// problem nobody can fix by changing a permission.
	UpdateToken string

	// SecretKey encrypts the values the database holds that the application has
	// to read back: a TOTP secret, and the directory's bind password.
	//
	// Base64, thirty-two bytes. Empty on an installation that has not set one,
	// which keeps working and stores those two in the clear - so this is opt-in,
	// and the cost of opting in is that losing the key costs every enrolled second
	// factor.
	//
	// It is protection against a dump: a backup on a laptop, a snapshot copied
	// somewhere with weaker access, a managed database read by somebody who should
	// only have reached the application. It is not protection against somebody who
	// has the machine, because they have this too.
	SecretKey string

	// UIEnabled serves the embedded web interface. Turn it off to run the
	// binary as a headless API.
	UIEnabled bool

	// AppName labels the instance, and is what authenticator apps show next to
	// a two-factor account.
	AppName string

	// AuthRequired turns on sign-in and role enforcement. With it off every
	// request is treated as fully privileged, which suits a local trial only.
	AuthRequired bool

	// SessionLifetime is how long a sign-in lasts before the user has to
	// authenticate again.
	SessionLifetime time.Duration

	// SessionIdle ends a session that has gone unused for this long, whatever is
	// left of its lifetime. Zero is no idle timeout at all.
	SessionIdle time.Duration

	// TLS serves HTTPS with automatically obtained Let's Encrypt certificates.
	TLSEnabled bool
	TLSDomains []string

	// TLSCertFile and TLSKeyFile point at a certificate this installation
	// already has, in PEM. With both set nothing is requested from a certificate
	// authority - which is the only way to serve HTTPS on a name Let's Encrypt
	// cannot reach, and that is most installations of this: an office network, a
	// hostname that resolves nowhere outside it, no public name at all.
	TLSCertFile string
	TLSKeyFile  string
	TLSEmail    string
	TLSCacheDir string
	TLSPort     int
	HTTPPort    int
	TLSStaging  bool

	// HSTSMaxAge tells browsers to refuse plain HTTP for this long. It is only
	// sent over connections that already are HTTPS.
	HSTSMaxAge time.Duration

	// RateLimit bounds sign-in and API-token requests per client per window.
	RateLimit       int
	RateLimitWindow time.Duration

	// LDAPSyncSchedule runs the directory reconciliation. Empty disables the
	// scheduled run; the administrator can still trigger one by hand.
	//
	// A run deletes accounts the directory no longer holds, together with
	// their recorded hours, so it is off by default.
	LDAPSyncSchedule string

	// LDAPSyncMaxDeleteRatio refuses a run that would remove more than this
	// share of the directory-backed accounts. It is the guard against a
	// truncated or misfiltered directory answer reading as a mass departure.
	LDAPSyncMaxDeleteRatio float64

	// MaxDailyHours caps the total hours a user may book on a single day.
	MaxDailyHours float64

	// Telemetry is what GoFr read for the metrics endpoint and the trace
	// exporter, and therefore what this process is serving and exporting.
	Telemetry Telemetry
}

// Telemetry is the metrics and tracing configuration in force in this process.
//
// Reported, not acted on: GoFr owns both. The values are resolved with GoFr's own
// rules rather than sensible ones, because a screen that shows what should have
// happened instead of what did is worse than no screen.
type Telemetry struct {
	// LogLevel is how much this process is writing. Resolved the way GoFr does
	// it, which means anything it could not read shows as INFO - because that is
	// what it silently became.
	LogLevel string

	// MetricsPort is where /metrics is served, or 0 when the endpoint is off.
	MetricsPort int

	// TraceExporter is empty when spans go nowhere.
	TraceExporter string

	// TracerURL is the collector address, as host:port.
	TracerURL string

	// TracerRatio is the share of traces sampled.
	TracerRatio float64
}

// MetricsPath is where GoFr serves the metrics on the metrics port.
//
// Beside them, on the same port, it serves Go's profiling endpoints under
// /debug/pprof/ - with no authentication and outside the middleware chain, so
// none of this application's own protections apply to either. Anywhere that port
// is reachable, so is a heap dump.
const MetricsPath = "/metrics"

// MetricsServed reports whether this process serves the metrics endpoint.
func (t Telemetry) MetricsServed() bool { return t.MetricsPort > 0 }

// TracingEnabled reports whether spans are being exported.
func (t Telemetry) TracingEnabled() bool { return t.TraceExporter != "" }

// defaultMetricsPort is GoFr's own, used when METRICS_PORT is absent or
// unreadable. Kept here so the screen can name the port without waiting for GoFr
// to log it.
const defaultMetricsPort = 2121

// logLevel resolves LOG_LEVEL the way GoFr's GetLevelFromString does: any name
// it does not recognise, including an empty one, becomes INFO.
//
// Reported rather than corrected, because that is what the process is actually
// doing. An administrator who typed "verbose" and sees INFO here has been told
// something true; showing "verbose" back would not be.
func logLevel(raw string) string {
	level := strings.ToUpper(strings.TrimSpace(raw))
	if slices.Contains(model.SupportedLogLevels(), level) {
		return level
	}

	return "INFO"
}

// metricsPort resolves METRICS_PORT exactly as GoFr's initMetricsServer does:
// the literal "0" switches the endpoint off, anything it cannot read as a
// positive number falls back to the default, and 0 is returned for "off".
func metricsPort(raw string) int {
	if raw == "0" {
		return 0
	}

	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 {
		return defaultMetricsPort
	}

	return port
}

// traceRatio resolves TRACER_RATIO the way GoFr does, which is worth spelling
// out because the failure is silent: GoFr reports a parse error and then carries
// on with the zero value, so an unreadable ratio samples nothing rather than
// falling back to everything. The sampler then clamps whatever is left into
// 0..1.
func traceRatio(raw string) float64 {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}

	// ParseFloat reads "NaN" happily, and a sampler compares false against it and
	// records nothing - so nothing is the honest report. It also has to be caught
	// before the bounds below, which NaN passes both of: this value is encoded
	// into the settings response, and JSON has no way to write it, so one hand-
	// edited configuration file would leave the screen unable to render at all.
	if math.IsNaN(value) || value < 0 {
		return 0
	}

	if value > 1 {
		return 1
	}

	return value
}

const (
	defaultDialect            = "sqlite"
	defaultAutoCloseAfterDays = 14
	defaultMaxDailyHours      = 24
	defaultSessionLifetime    = 12 * time.Hour

	defaultTLSCacheDir      = "configs/certs"
	defaultTLSPort          = 443
	defaultHTTPRedirectPort = 80

	// A year is the value browsers expect before they will preload a host.
	defaultHSTSMaxAge = 365 * 24 * time.Hour

	// Generous enough for a script polling the API, tight enough that
	// guessing a password or a token is hopeless.
	defaultRateLimit       = 30
	defaultRateLimitWindow = time.Minute

	// Half the directory-backed accounts disappearing in one run is far more
	// likely to be a broken filter than a real mass departure.
	defaultSyncMaxDeleteRatio = 0.5
)

// ratioOr reads a share between 0 and 1. Zero disables the check, which the
// caller must opt into explicitly.
func ratioOr(raw string, fallback float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v < 0 || v > 1 {
		return fallback
	}

	return v
}

// splitList reads a comma-separated setting, dropping blanks.
func splitList(raw string) []string {
	var out []string

	for part := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

// Load reads the application settings, applying defaults that let the binary
// run with no configuration at all.
func Load(p Provider) Config {
	return Config{
		Dialect:     strings.ToLower(p.GetOrDefault("DB_DIALECT", defaultDialect)),
		AppName:     p.GetOrDefault("APP_NAME", "Time Recording"),
		UIEnabled:   boolOr(p.GetOrDefault("UI_ENABLED", "true"), true),
		UpdateCheck: boolOr(p.GetOrDefault("UPDATE_CHECK", "true"), true),
		UpdateFeed:  p.Get("UPDATE_FEED"),
		UpdateToken: p.Get("UPDATE_TOKEN"),
		SecretKey:   p.Get("SECRET_KEY"),
		// Defaults to on: an instance that quietly serves everyone full
		// administrative rights should be a deliberate choice, not an oversight.
		AuthRequired:    boolOr(p.GetOrDefault("AUTH_ENABLED", "true"), true),
		SessionLifetime: durationOr(p.GetOrDefault("SESSION_LIFETIME", ""), defaultSessionLifetime),
		SessionIdle:     durationOr(p.GetOrDefault("SESSION_IDLE", ""), 0),

		TLSEnabled:  boolOr(p.GetOrDefault("TLS_ENABLED", "false"), false),
		TLSDomains:  splitList(p.Get("TLS_DOMAINS")),
		TLSCertFile: p.Get("TLS_CERT_FILE"),
		TLSKeyFile:  p.Get("TLS_KEY_FILE"),
		TLSEmail:    p.Get("TLS_EMAIL"),
		TLSCacheDir: p.GetOrDefault("TLS_CACHE_DIR", defaultTLSCacheDir),
		TLSPort:     intOr(p.GetOrDefault("TLS_PORT", ""), defaultTLSPort),
		HTTPPort:    intOr(p.GetOrDefault("TLS_REDIRECT_PORT", ""), defaultHTTPRedirectPort),
		TLSStaging:  boolOr(p.GetOrDefault("TLS_STAGING", "false"), false),

		HSTSMaxAge:      durationOr(p.GetOrDefault("HSTS_MAX_AGE", ""), defaultHSTSMaxAge),
		RateLimit:       intOr(p.GetOrDefault("RATE_LIMIT", ""), defaultRateLimit),
		RateLimitWindow: durationOr(p.GetOrDefault("RATE_LIMIT_WINDOW", ""), defaultRateLimitWindow),

		// Empty by default: a scheduled run deletes people and their hours,
		// which must be a deliberate choice.
		LDAPSyncSchedule: p.Get("LDAP_SYNC_SCHEDULE"),
		LDAPSyncMaxDeleteRatio: ratioOr(p.GetOrDefault("LDAP_SYNC_MAX_DELETE_RATIO", ""),
			defaultSyncMaxDeleteRatio),
		MaxDailyHours: floatOr(p.GetOrDefault("MAX_DAILY_HOURS", ""), defaultMaxDailyHours),

		Telemetry: Telemetry{
			LogLevel:      logLevel(p.Get("LOG_LEVEL")),
			MetricsPort:   metricsPort(p.Get("METRICS_PORT")),
			TraceExporter: strings.ToLower(strings.TrimSpace(p.Get("TRACE_EXPORTER"))),
			TracerURL:     strings.TrimSpace(p.Get("TRACER_URL")),
			// GoFr's own default, which it applies whenever the value is absent.
			TracerRatio: traceRatio(p.GetOrDefault("TRACER_RATIO", "1")),
		},
	}
}

// AuthEnabled reports whether sign-in and role checks are enforced.
func (c Config) AuthEnabled() bool {
	return c.AuthRequired
}

func durationOr(raw string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return fallback
	}

	return v
}

func boolOr(raw string, fallback bool) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}

	return v
}

func intOr(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return fallback
	}

	return v
}

func floatOr(raw string, fallback float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v <= 0 {
		return fallback
	}

	return v
}
