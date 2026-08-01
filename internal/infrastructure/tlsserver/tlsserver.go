// Package tlsserver obtains and renews Let's Encrypt certificates and serves
// the application over HTTPS.
//
// GoFr owns its own HTTP listener and only accepts a certificate file pair, so
// automatic certificates are handled here: this package terminates TLS and
// forwards to GoFr's plain listener on localhost.
package tlsserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Config describes how to serve HTTPS.
type Config struct {
	// Domains the certificate should cover. Required: Let's Encrypt issues
	// only for names it can reach.
	Domains []string

	// Email receives expiry warnings from the certificate authority.
	Email string

	// CacheDir stores issued certificates so a restart does not request new
	// ones and run into the rate limits.
	CacheDir string

	// HTTPSPort is where TLS is served; HTTPPort answers the ACME challenge
	// and redirects everything else.
	HTTPSPort int
	HTTPPort  int

	// Backend is the plain address GoFr listens on, e.g. "127.0.0.1:8000".
	Backend string

	// Staging uses Let's Encrypt's test authority, whose certificates are not
	// trusted by browsers but whose rate limits are far looser. Use it while
	// setting a deployment up.
	Staging bool
}

// Logger is the small part of GoFr's logger this package needs.
type Logger interface {
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
}

// stagingDirectory is Let's Encrypt's test endpoint.
const stagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"

// Start serves HTTPS in the background and returns once the listeners are up.
//
// The returned function shuts both servers down.
func Start(cfg Config, logger Logger) (stop func(context.Context) error, err error) {
	if len(cfg.Domains) == 0 {
		return nil, fmt.Errorf("at least one domain is required for a certificate")
	}

	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		// HostPolicy is what stops the process from being used to request
		// certificates for names it does not serve.
		HostPolicy: autocert.HostWhitelist(cfg.Domains...),
		Cache:      autocert.DirCache(cfg.CacheDir),
		Email:      cfg.Email,
	}

	if cfg.Staging {
		manager.Client = &acme.Client{DirectoryURL: stagingDirectory}
	}

	backend, err := url.Parse("http://" + cfg.Backend)
	if err != nil {
		return nil, fmt.Errorf("invalid backend address %q: %w", cfg.Backend, err)
	}

	// Rewrite rather than the deprecated Director: it drops any inbound
	// X-Forwarded-* headers before setting its own, so a client cannot forge
	// the address the rate limiter and the cookie logic rely on.
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(backend)
			pr.SetXForwarded()

			// The application decides on Secure cookies and HSTS from this,
			// so the original scheme has to survive the hop to the plain
			// listener.
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
			pr.Out.Host = pr.In.Host
		},
	}

	httpsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPSPort),
		Handler: proxy,
		TLSConfig: &tls.Config{
			GetCertificate: manager.GetCertificate,
			MinVersion:     tls.VersionTLS12,
			NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
		},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Port 80 must stay reachable: it answers the HTTP-01 challenge, and
	// redirects everything else so no one is served over plain HTTP.
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           manager.HTTPHandler(redirectToHTTPS(cfg.HTTPSPort)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Infof("serving HTTPS on :%d for %s", cfg.HTTPSPort, strings.Join(cfg.Domains, ", "))

		if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			logger.Errorf("HTTPS server stopped: %v", err)
		}
	}()

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("HTTP redirect server stopped: %v", err)
		}
	}()

	return func(ctx context.Context) error {
		httpErr := httpServer.Shutdown(ctx)

		if err := httpsServer.Shutdown(ctx); err != nil {
			return err
		}

		return httpErr
	}, nil
}

// redirectToHTTPS sends every plain request to the encrypted address.
func redirectToHTTPS(httpsPort int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}

		if httpsPort != 443 {
			host = fmt.Sprintf("%s:%d", host, httpsPort)
		}

		target := "https://" + host + r.URL.RequestURI()

		// 308 keeps the method and body, so a redirected POST is not silently
		// turned into a GET.
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}
