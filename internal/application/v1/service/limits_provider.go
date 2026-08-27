package service

import (
	"context"
	"sync"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// limitsTTL is how stale a cached value may get.
//
// The point of administering these from the interface is that a change takes
// effect without a restart, so the window has to be short enough that an
// administrator sees their change work. It also has to be long enough that the
// rate limiter, which asks on every request, does not turn into a query per
// request.
const limitsTTL = 10 * time.Second

// LimitsProvider answers "what are the limits right now" cheaply.
//
// The values live in the database so they survive a restart and apply to every
// process reading that database, but several callers need them on a hot path -
// the rate limiter runs before every request. Reading through a short-lived
// cache keeps both true: administered centrally, and cheap to ask.
type LimitsProvider struct {
	settings *SettingsService

	// fallback is what the environment configured, used for any field the
	// administrator has not overridden and whenever the database cannot be
	// read at all.
	fallback model.Limits

	mu      sync.RWMutex
	cached  model.Limits
	fetched time.Time
}

// NewLimitsProvider creates a provider over the environment's values.
func NewLimitsProvider(settings *SettingsService, fallback model.Limits) *LimitsProvider {
	return &LimitsProvider{
		settings: settings,
		fallback: fallback,
		cached:   fallback,
	}
}

// Fallback reports the environment's values, ignoring anything administered.
func (p *LimitsProvider) Fallback() model.Limits {
	return p.fallback
}

// Limits returns the values in force.
//
// A failed read keeps serving the last good answer rather than surfacing an
// error: these decide whether a request is allowed through and how long a
// session lasts, and a database hiccup must not turn into an outage.
func (p *LimitsProvider) Limits(ctx context.Context) model.Limits {
	p.mu.RLock()
	cached, fetched := p.cached, p.fetched
	p.mu.RUnlock()

	if time.Since(fetched) < limitsTTL {
		return cached
	}

	operational, err := p.settings.Operational(ctx)
	if err != nil {
		// Mark the attempt so a database that is down is not re-queried on
		// every single request while it recovers.
		p.mu.Lock()
		p.fetched = time.Now()
		p.mu.Unlock()

		return cached
	}

	resolved := operational.Resolve(p.fallback)

	p.mu.Lock()
	p.cached = resolved
	p.fetched = time.Now()
	p.mu.Unlock()

	return resolved
}

// Invalidate drops the cache so the next read sees a just-saved change without
// waiting out the TTL.
func (p *LimitsProvider) Invalidate() {
	p.mu.Lock()
	p.fetched = time.Time{}
	p.mu.Unlock()
}

// SessionLifetime is how long a newly opened session lasts.
func (p *LimitsProvider) SessionLifetime(ctx context.Context) time.Duration {
	hours := p.Limits(ctx).SessionLifetimeHours
	if hours <= 0 {
		hours = p.fallback.SessionLifetimeHours
	}

	return time.Duration(hours * float64(time.Hour))
}

// SessionIdle is how long a session may go unused before it ends. Zero is no
// idle timeout, which is what an installation has until somebody sets one.
func (p *LimitsProvider) SessionIdle(ctx context.Context) time.Duration {
	minutes := p.Limits(ctx).SessionIdleMinutes
	if minutes <= 0 {
		return 0
	}

	return time.Duration(minutes * float64(time.Minute))
}

// RateLimit reports the request budget and the window it applies to.
func (p *LimitsProvider) RateLimit(ctx context.Context) (limit int, window time.Duration) {
	current := p.Limits(ctx)

	return current.RateLimit, time.Duration(current.RateLimitWindowSeconds) * time.Second
}
