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

	// CertFile and KeyFile are a certificate this installation already has, in
	// PEM. Given both, nothing is requested from anybody: no domain, no
	// challenge on port 80, no certificate authority.
	//
	// This is what makes HTTPS possible where Let's Encrypt cannot go, which is
	// most installations of this application: an office network, a hostname that
	// resolves nowhere outside it, an address with no public name at all. Those
	// have a certificate from a company authority or one made for the purpose,
	// and until now had no way to use it.
	CertFile string
	KeyFile  string
}

// usesOwnCertificate reports whether this installation brought its own.
func (c Config) usesOwnCertificate() bool {
	return strings.TrimSpace(c.CertFile) != "" && strings.TrimSpace(c.KeyFile) != ""
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
	own := cfg.usesOwnCertificate()

	if !own && len(cfg.Domains) == 0 {
		return nil, fmt.Errorf(
			"TLS needs either TLS_DOMAINS, for a certificate from Let's Encrypt, or " +
				"TLS_CERT_FILE and TLS_KEY_FILE, for one this installation already has")
	}

	var (
		manager     *autocert.Manager
		certificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	)

	if own {
		// Loaded once, here, so a file that is missing or does not parse stops
		// the start rather than every request: a certificate read lazily is a
		// certificate whose mistakes are found by the first visitor.
		pair, loadErr := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if loadErr != nil {
			return nil, fmt.Errorf("cannot read the certificate and key: %w", loadErr)
		}

		certificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &pair, nil }
	} else {
		manager = &autocert.Manager{
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

		certificate = manager.GetCertificate
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
			GetCertificate: certificate,
			MinVersion:     tls.VersionTLS12,
			NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
		},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// The plain listener redirects, so nobody is served over HTTP.
	//
	// With Let's Encrypt it also answers the HTTP-01 challenge, which is why port
	// 80 has to stay reachable from outside. With a certificate this installation
	// already has there is no challenge, so this is only the redirect - and an
	// installation that does not want a plain listener at all can point
	// TLS_REDIRECT_PORT somewhere harmless.
	redirect := http.Handler(redirectToHTTPS(cfg.HTTPSPort))

	if manager != nil {
		redirect = manager.HTTPHandler(redirect)
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           redirect,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Bound here rather than inside the goroutine, and this is the difference
	// between an installation that says it serves HTTPS and one that does.
	//
	// ListenAndServeTLS binds and serves in one call, so starting it in a
	// goroutine meant a refused bind - port 443 without the privilege to take
	// it, or something already holding it - became a line in the log that
	// nothing was reading. Start returned a shutdown function either way, the
	// caller took that as success, and the installation came up serving plain
	// HTTP under a setting that says otherwise.
	//
	// It is also what makes the answer trustworthy for everything downstream:
	// once Start returns without an error, the encrypted listener is bound, and
	// the plain port can be closed to the network on the strength of it.
	httpsListener, err := net.Listen("tcp", httpsServer.Addr)
	if err != nil {
		return nil, fmt.Errorf("cannot bind %s for HTTPS: %w", httpsServer.Addr, err)
	}

	// The redirect is not held to the same standard, deliberately. It is a
	// courtesy to whoever typed http://, and with a certificate obtained over
	// TLS-ALPN-01 nothing depends on it - so a port 80 that cannot be bound
	// costs that courtesy rather than the encrypted listener that did bind.
	httpListener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		logger.Errorf("cannot bind %s to redirect plain requests: %v; "+
			"HTTPS is unaffected", httpServer.Addr, err)
	}

	go func() {
		logger.Infof("serving HTTPS on :%d for %s", cfg.HTTPSPort, strings.Join(cfg.Domains, ", "))

		if err := httpsServer.ServeTLS(httpsListener, "", ""); err != nil && err != http.ErrServerClosed {
			logger.Errorf("HTTPS server stopped: %v", err)
		}
	}()

	if httpListener != nil {
		go func() {
			if err := httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
				logger.Errorf("HTTP redirect server stopped: %v", err)
			}
		}()
	}

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

// KeepThePlainPortLocal stops GoFr's plain listener from serving the network
// while the HTTPS front end in this process is up.
//
// GoFr binds its port on every interface and offers no way to say otherwise, so
// terminating TLS in front of it left the unencrypted port open beside the
// encrypted one. Documented, in two places, as something for the operator to
// firewall - which is a real answer and a fragile one: it is a second step,
// taken on a different machine, by somebody who has just been told the
// installation is finished.
//
// What that open port gave away was not only the traffic. Anything arriving on
// it could also claim to be a proxy: the rate limiter keys on X-Forwarded-For,
// so a caller inventing a fresh one per attempt had no limit on sign-in
// guesses at all, and X-Forwarded-Proto decided whether cookies were marked
// Secure.
//
// So while the front end is serving, this port answers the machine it runs on -
// which is the front end, dialling loopback - and sends everybody else to the
// encrypted address. The condition is the point: when the HTTPS listener did
// not come up, this steps aside entirely rather than redirecting an
// installation to a port with nothing behind it.
func KeepThePlainPortLocal(frontEndIsServing func() bool, httpsPort int) func(http.Handler) http.Handler {
	toHTTPS := redirectToHTTPS(httpsPort)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS != nil || !frontEndIsServing() || fromThisMachine(r) {
				next.ServeHTTP(w, r)

				return
			}

			toHTTPS.ServeHTTP(w, r)
		})
	}
}

// fromThisMachine reports whether the connection was opened over the loopback
// interface.
//
// The address the connection came from, not any header on it. Everything a
// caller can write is exactly what this is deciding whether to believe.
func fromThisMachine(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	address := net.ParseIP(strings.Trim(host, "[]"))

	return address != nil && address.IsLoopback()
}
