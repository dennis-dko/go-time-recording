// Package web serves the user interface. The assets are compiled into the
// binary with go:embed so a deployment is a single file, with no asset
// directory to ship alongside it.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// assets holds the UI. all: is not needed here because no file starts with
// '_' or '.', but the pattern must keep matching every file added later.
//
//go:embed assets
var assets embed.FS

// apiPrefixes are the paths the UI must never shadow. Anything under them is
// passed to the next handler even if a file of that name happened to exist.
var apiPrefixes = []string{
	"/api/",
	"/.well-known/",
	"/health",
	"/alive",
	"/metrics",
	"/favicon.ico",
	"/swagger",
}

// Handler returns middleware that serves the embedded UI.
//
// GoFr's AddStaticFiles only serves a directory from disk, which would defeat
// the single-binary goal, so the assets are served here instead. The signature
// matches gofr.dev/pkg/gofr/http.Middleware.
func Handler() func(http.Handler) http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		// Only reachable if the embed directive and this path disagree,
		// which is a build-time mistake rather than a runtime condition.
		panic("web: embedded assets missing: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !servesUI(r) {
				next.ServeHTTP(w, r)

				return
			}

			// /api-docs is a second page, not part of the single-page app, so
			// it is resolved to its own file rather than the SPA fallback.
			if p := path(r); p == "/api-docs" || p == "/api-docs/" {
				r = r.Clone(r.Context())
				r.URL.Path = "/api-docs.html"
			}

			// The UI is a single page: unknown paths must render the app
			// rather than 404, so deep links work on reload.
			if _, statErr := fs.Stat(sub, strings.TrimPrefix(path(r), "/")); statErr != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}

			fileServer.ServeHTTP(w, r)
		})
	}
}

// servesUI reports whether the request should be answered from the embedded
// assets rather than passed down to the API routes.
func servesUI(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	p := path(r)
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(p, prefix) {
			return false
		}
	}

	return true
}

func path(r *http.Request) string {
	p := r.URL.Path
	if p == "" {
		return "/"
	}

	return p
}
