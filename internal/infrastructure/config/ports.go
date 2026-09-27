package config

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// PortInUse reports the first port GoFr is about to claim that something is
// already answering on.
//
// GoFr checks both ports itself, the served one when the first route is
// registered and the metrics one inside gofr.New(), and answers a port in use
// with a Fatal. A Fatal goes through the captured output and the process exits
// before the reader forwards it - see the logsink package - so a second copy
// started by mistake exited 1 having said nothing about why. This asks the same
// question the same way, before gofr.New(), so the refusal can be said by the
// application on the console instead.
//
// The same way matters more than the right way. GoFr calls a port blocked when a
// connection to it succeeds, and a bind test answers differently on some
// platforms - so anything but GoFr's own predicate would let through a port
// GoFr then refuses, or refuse one GoFr would have taken.
//
// Read after the administered telemetry has been exported, because a stored
// metrics port or "metrics off" is only visible from then on.
func PortInUse(ctx context.Context) error {
	p := gofrConfig()

	served := portOr(p.Get("HTTP_PORT"), defaultHTTPPort)
	if answers(ctx, served) {
		return fmt.Errorf("port %d, where the application is served, is already in use", served)
	}

	// "0" switches the metrics endpoint off, and then there is nothing to claim.
	if raw := p.Get("METRICS_PORT"); raw != "0" {
		metrics := portOr(raw, defaultMetricPort)
		if answers(ctx, metrics) {
			return fmt.Errorf("port %d, where the metrics are served, is already in use", metrics)
		}
	}

	return nil
}

// defaultHTTPPort and defaultMetricPort are GoFr's own defaults, which it
// applies to a value that is missing, unreadable or not positive.
const (
	defaultHTTPPort   = 8000
	defaultMetricPort = 2121
)

// portCheckTimeout is how long GoFr waits for its own check to connect.
const portCheckTimeout = 2 * time.Second

// portOr reads a port the way GoFr does.
func portOr(raw string, fallback int) int {
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 {
		return fallback
	}

	return port
}

// answers reports whether something accepts a connection on the port, which is
// what GoFr takes to mean the port is blocked.
func answers(ctx context.Context, port int) bool {
	dialer := net.Dialer{Timeout: portCheckTimeout}

	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}

	_ = conn.Close()

	return true
}
