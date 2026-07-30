// Package config exposes the application-specific settings that sit on top of
// the configuration GoFr already reads for itself (ports, database, log level,
// tracing). Only settings this application acts on belong here.
package config

import (
	"strconv"
	"strings"
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

	// UIEnabled serves the embedded web interface. Turn it off to run the
	// binary as a headless API.
	UIEnabled bool

	// BasicAuthUser/BasicAuthPassword protect every route when both are set.
	BasicAuthUser     string
	BasicAuthPassword string

	// AutoCloseSchedule is the cron expression for sweeping stale open
	// timesheets. Empty disables the job.
	AutoCloseSchedule string

	// AutoCloseAfterDays is how old an open timesheet must be before the
	// sweep submits it.
	AutoCloseAfterDays int

	// MaxDailyHours caps the total hours a user may book on a single day.
	MaxDailyHours float64
}

const (
	defaultDialect            = "sqlite"
	defaultAutoCloseSchedule  = "0 2 * * *" // 02:00 daily
	defaultAutoCloseAfterDays = 14
	defaultMaxDailyHours      = 24
)

// Load reads the application settings, applying defaults that let the binary
// run with no configuration at all.
func Load(p Provider) Config {
	return Config{
		Dialect:            strings.ToLower(p.GetOrDefault("DB_DIALECT", defaultDialect)),
		UIEnabled:          boolOr(p.GetOrDefault("UI_ENABLED", "true"), true),
		BasicAuthUser:      p.Get("BASIC_AUTH_USER"),
		BasicAuthPassword:  p.Get("BASIC_AUTH_PASSWORD"),
		AutoCloseSchedule:  p.GetOrDefault("AUTO_CLOSE_SCHEDULE", defaultAutoCloseSchedule),
		AutoCloseAfterDays: intOr(p.GetOrDefault("AUTO_CLOSE_AFTER_DAYS", ""), defaultAutoCloseAfterDays),
		MaxDailyHours:      floatOr(p.GetOrDefault("MAX_DAILY_HOURS", ""), defaultMaxDailyHours),
	}
}

// AuthEnabled reports whether basic auth should be installed. Both halves of
// the credential must be present; a username with no password would otherwise
// silently accept every request.
func (c Config) AuthEnabled() bool {
	return c.BasicAuthUser != "" && c.BasicAuthPassword != ""
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
