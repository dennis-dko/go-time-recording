// Package config exposes the application-specific settings that sit on top of
// the configuration GoFr already reads for itself (ports, database, log level,
// tracing). Only settings this application acts on belong here.
package config

import (
	"strconv"
	"strings"
	"time"
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

	// AppName labels the instance, and is what authenticator apps show next to
	// a two-factor account.
	AppName string

	// AuthRequired turns on sign-in and role enforcement. With it off every
	// request is treated as fully privileged, which suits a local trial only.
	AuthRequired bool

	// SessionLifetime is how long a sign-in lasts before the user has to
	// authenticate again.
	SessionLifetime time.Duration

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
	defaultSessionLifetime    = 12 * time.Hour
)

// Load reads the application settings, applying defaults that let the binary
// run with no configuration at all.
func Load(p Provider) Config {
	return Config{
		Dialect:   strings.ToLower(p.GetOrDefault("DB_DIALECT", defaultDialect)),
		AppName:   p.GetOrDefault("APP_NAME", "Zeiterfassung"),
		UIEnabled: boolOr(p.GetOrDefault("UI_ENABLED", "true"), true),
		// Defaults to on: an instance that quietly serves everyone full
		// administrative rights should be a deliberate choice, not an oversight.
		AuthRequired:       boolOr(p.GetOrDefault("AUTH_ENABLED", "true"), true),
		SessionLifetime:    durationOr(p.GetOrDefault("SESSION_LIFETIME", ""), defaultSessionLifetime),
		AutoCloseSchedule:  p.GetOrDefault("AUTO_CLOSE_SCHEDULE", defaultAutoCloseSchedule),
		AutoCloseAfterDays: intOr(p.GetOrDefault("AUTO_CLOSE_AFTER_DAYS", ""), defaultAutoCloseAfterDays),
		MaxDailyHours:      floatOr(p.GetOrDefault("MAX_DAILY_HOURS", ""), defaultMaxDailyHours),
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
