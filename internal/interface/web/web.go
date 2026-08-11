// Package web serves the user interface. The assets are compiled into the
// binary with go:embed so a deployment is a single file, with no asset
// directory to ship alongside it.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
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
	tags := buildETags(sub)

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

			if servedFromCache(w, r, tags) {
				return
			}

			fileServer.ServeHTTP(w, r)
		})
	}
}

// etags is the content hash of every embedded asset, by request path.
//
// Built once at start-up, which is possible precisely because the assets are
// compiled in: they cannot change while the process runs, so there is nothing to
// invalidate and nothing to stat per request.
type etags map[string]string

// buildETags hashes every embedded file.
func buildETags(sub fs.FS) etags {
	tags := etags{}

	_ = fs.WalkDir(sub, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil //nolint:nilerr // an asset that cannot be read is simply not tagged
		}

		content, readErr := fs.ReadFile(sub, name)
		if readErr != nil {
			return nil //nolint:nilerr // same: no tag rather than no start-up
		}

		sum := sha256.Sum256(content)
		tags["/"+name] = `"` + hex.EncodeToString(sum[:16]) + `"`

		return nil
	})

	// The single-page fallback answers "/" with index.html, so it needs the tag
	// under the path it is actually requested by.
	if tag, ok := tags["/index.html"]; ok {
		tags["/"] = tag
	}

	return tags
}

// servedFromCache answers a repeat request with 304 where the browser already has
// the file, and reports whether it did.
//
// Every asset used to be sent in full on every page load. http.FileServer would
// have handled this by itself, but it works from the modification time, and a file
// compiled into the binary has none - embed reports the zero time, so no
// Last-Modified and no ETag were ever emitted and nothing could be revalidated.
//
// no-cache rather than a long max-age, and that is the honest choice here: the
// asset URLs are not fingerprinted - /app.js is /app.js in every release - so a
// browser told to hold one for a year would still be running last release's
// interface against this release's API, with no way to tell it otherwise. no-cache
// does not mean "do not cache": it means "ask first", and asking is one request
// that ends in 304 with no body.
func servedFromCache(w http.ResponseWriter, r *http.Request, tags etags) bool {
	tag, known := tags[path(r)]
	if !known {
		return false
	}

	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "no-cache")

	// Any of the tags the browser offers, which is what the header allows even
	// though this only ever sends one.
	for _, offered := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimSpace(offered) == tag {
			w.WriteHeader(http.StatusNotModified)

			return true
		}
	}

	return false
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
