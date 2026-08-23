// Package installer serves the first screen of a brand new installation: the
// one that decides which database everything else lives in.
//
// # Why this exists before the application does
//
// Every other setting an installation has - the administrator's password, the
// timezone, the instance title, the directory bind - is stored in the database.
// So the database cannot be one of them. It has to be settled before there is
// anywhere to settle anything, which means before the application starts, which
// means before there is an account to sign in with.
//
// That is the whole reason this is a separate server rather than another step of
// the in-application wizard. Run afterwards, choosing a database would point the
// application at an empty one and abandon everything configured so far -
// including the changed administrator password, so the installation would come
// back up reachable with the initial password from the documentation.
//
// # How it hands over
//
// Serve listens, waits for a connection that has been proven to work, writes it
// to configs/datasource.json and returns. The caller then starts the application
// normally in the same process. No restart is involved, which is what makes this
// work in a container that would otherwise be considered crashed.
//
// # Why it asks for a token
//
// There is no database, so there is nobody to authenticate. Whoever reaches this
// screen decides where the installation keeps its data, and an installation
// exposed for the minutes before it is configured would otherwise be anyone's to
// claim. The token is printed to the log, which only somebody who can already
// see the process can read.
package installer

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	appconfig "github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

//go:embed assets/install.html
var assets embed.FS

// TokenEnv supplies the token instead of one being generated.
//
// For an unattended install: a compose file or a provisioning script can set it,
// print nothing, and drive this screen without a human reading a log. An empty
// value means one is generated and logged.
const TokenEnv = "SETUP_TOKEN"

// Config is what Serve needs.
type Config struct {
	// Addr is where to listen, in the form ":8000".
	Addr string

	// AppName labels the page.
	AppName string

	// Version is shown in the page footer, so it is possible to tell which
	// build somebody is looking at before it has an interface.
	Version string

	// Token is required in every request that changes anything. Generated and
	// logged when empty.
	Token string

	// DatasourceFile is where the accepted connection is written.
	DatasourceFile string

	// Prefill is what the environment already supplied, offered in the form so
	// an operator who configured half of it in a compose file does not retype
	// it. The password is never sent back.
	Prefill appconfig.Datasource

	// Logf reports progress. Required: this screen is the only thing the
	// process is doing, and a silent one would look hung.
	Logf func(format string, args ...any)
}

// Serve blocks until a working database connection has been saved, and returns
// it.
//
// The returned connection has been probed with the application's own drivers, so
// the caller can rely on it rather than discovering a bad password during
// migrations.
func Serve(ctx context.Context, cfg Config) (appconfig.Datasource, error) {
	if cfg.Logf == nil {
		return appconfig.Datasource{}, errors.New("installer: Logf is required")
	}

	if cfg.DatasourceFile == "" {
		cfg.DatasourceFile = appconfig.DatasourceFile
	}

	generated := cfg.Token == ""
	if generated {
		token, err := newToken()
		if err != nil {
			return appconfig.Datasource{}, err
		}

		cfg.Token = token
	}

	s := &server{cfg: cfg, done: make(chan appconfig.Datasource, 1)}

	mux := http.NewServeMux()
	mux.HandleFunc("/install/state", s.state)
	mux.HandleFunc("/install/test", s.test)
	mux.HandleFunc("/install/save", s.save)
	// Everything else is the page, so a bookmarked deep link into the
	// application still lands somewhere that explains itself.
	mux.HandleFunc("/", s.page)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return appconfig.Datasource{}, err
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cfg.Logf("installer stopped serving: %v", err)
		}
	}()

	s.announce(listener.Addr(), generated)

	var (
		accepted appconfig.Datasource
		waitErr  error
	)

	select {
	case accepted = <-s.done:
	case <-ctx.Done():
		waitErr = ctx.Err()
	}

	// The application is about to bind the same port, so the listener has to be
	// gone before Serve returns rather than shortly after.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = httpServer.Shutdown(shutdownCtx)

	return accepted, waitErr
}

type server struct {
	cfg  Config
	done chan appconfig.Datasource

	// once guards the handover: two browser tabs submitting at the same moment
	// must not both be told they succeeded, and must not write the file twice.
	once sync.Once
}

func (s *server) announce(addr net.Addr, generated bool) {
	where := "http://localhost" + portOf(addr)

	s.cfg.Logf("no database is configured, so %s is serving its installer instead", s.cfg.AppName)
	s.cfg.Logf("open %s to choose one - the application will not start until you do", where)

	if generated {
		s.cfg.Logf("setup token: %s", s.cfg.Token)
		s.cfg.Logf("or open %s/?token=%s to fill it in automatically", where, s.cfg.Token)
		s.cfg.Logf("set %s to choose the token yourself and skip this message", TokenEnv)
	}
}

// portOf renders the ":8000" part of a listening address. Taken from the
// listener rather than the configured address, because ":0" means the operating
// system picked one and only it knows which.
func portOf(addr net.Addr) string {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return ":" + strconv.Itoa(tcp.Port)
	}

	return ""
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	page, err := assets.ReadFile("assets/install.html")
	if err != nil {
		http.Error(w, "the installer page is missing from this build", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Nothing here should be cached: the next request to this URL is meant to
	// be answered by the application, not by a stale installer page.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	_, _ = w.Write(page)
}

// stateResponse labels the page and pre-fills what is already known.
type stateResponse struct {
	AppName    string         `json:"appName"`
	Version    string         `json:"version"`
	Datasource *prefillFields `json:"datasource,omitempty"`
	Dialects   []string       `json:"dialects"`
}

// prefillFields is deliberately not appconfig.Datasource: that carries a
// password, and this travels to a browser that has not authenticated.
type prefillFields struct {
	Dialect string `json:"dialect,omitempty"`
	Name    string `json:"name,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    string `json:"port,omitempty"`
	User    string `json:"user,omitempty"`
	SSLMode string `json:"sslMode,omitempty"`
}

// state answers GET /install/state. Unauthenticated, and carries nothing worth
// protecting: the application name and version are on the sign-in screen too,
// and the page needs it before a token has been typed.
func (s *server) state(w http.ResponseWriter, _ *http.Request) {
	response := stateResponse{
		AppName:  s.cfg.AppName,
		Version:  s.cfg.Version,
		Dialects: []string{"sqlite", "postgres", "mysql"},
	}

	if p := s.cfg.Prefill; p.Dialect != "" || p.Name != "" || p.Host != "" {
		response.Datasource = &prefillFields{
			Dialect: p.Dialect,
			Name:    p.Name,
			Host:    p.Host,
			Port:    p.Port,
			User:    p.User,
			SSLMode: p.SSLMode,
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *server) test(w http.ResponseWriter, r *http.Request) {
	ds, ok := s.accept(w, r)
	if !ok {
		return
	}

	if err := appconfig.TestDatasource(r.Context(), ds); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) save(w http.ResponseWriter, r *http.Request) {
	ds, ok := s.accept(w, r)
	if !ok {
		return
	}

	// Probed before it is written, not after. A saved connection that does not
	// work would leave the process unable to start and unable to serve the
	// screen that could fix it - the one state this design must not reach.
	if err := appconfig.TestDatasource(r.Context(), ds); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	if err := appconfig.SaveDatasource(s.cfg.DatasourceFile, ds); err != nil {
		writeError(w, http.StatusInternalServerError,
			fmt.Errorf("cannot save the connection: %w", err))

		return
	}

	s.cfg.Logf("database configured: %s, saved to %s", ds.Dialect, s.cfg.DatasourceFile)

	// Answer before handing over, so the browser learns it succeeded rather
	// than seeing the connection drop as the port changes hands.
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	if flusher, canFlush := w.(http.Flusher); canFlush {
		flusher.Flush()
	}

	s.once.Do(func() { s.done <- ds })
}

// accept checks the method and the token and decodes the body.
func (s *server) accept(w http.ResponseWriter, r *http.Request) (appconfig.Datasource, bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))

		return appconfig.Datasource{}, false
	}

	// Constant time, so a wrong token cannot be narrowed down by how long the
	// rejection took.
	given := strings.TrimSpace(r.Header.Get("X-Setup-Token"))
	if subtle.ConstantTimeCompare([]byte(given), []byte(s.cfg.Token)) != 1 {
		// Coded, because it is the refusal somebody meets most: the token is a
		// hex string copied out of a log, and copying it wrongly is easy.
		writeError(w, http.StatusUnauthorized,
			apperror.Invalidf("the setup token is wrong - it is printed in this "+
				"process's log").WithCode("wrongSetupToken"))

		return appconfig.Datasource{}, false
	}

	var ds appconfig.Datasource

	// Bounded: this endpoint is unauthenticated until the token is checked, and
	// the check has already happened, but a connection description is a few
	// hundred bytes and there is no reason to read more.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&ds); err != nil {
		writeError(w, http.StatusBadRequest,
			errors.New("the request is not a connection description"))

		return appconfig.Datasource{}, false
	}

	if err := ds.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)

		return appconfig.Datasource{}, false
	}

	return ds, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(body)
}

// writeError answers a refusal, with whatever of it the page can translate.
//
// The message is English prose written where the rule is enforced, which is
// right for a log and wrong for the only screen this server has - and that
// screen had no choice but to print it. So "invalid field(s): name" was shown to
// somebody whose installer was otherwise entirely German, naming a field by the
// key the payload uses rather than by the label above it.
//
// The same arrangement the application makes: a field list, or a code and the
// values its sentence interpolated, beside the English. This page has its own
// small dictionary and renders whichever of the three it can.
func writeError(w http.ResponseWriter, status int, err error) {
	body := map[string]any{"error": err.Error()}

	if detail, ok := errors.AsType[*apperror.Error](err); ok {
		if len(detail.Fields) > 0 {
			body["param"] = detail.Fields
		}

		if detail.Code != "" {
			body["code"] = detail.Code

			if len(detail.Values) > 0 {
				body["values"] = detail.Values
			}
		}
	}

	writeJSON(w, status, body)
}

// newToken returns a token short enough to retype and long enough that guessing
// it is not a strategy.
func newToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return hex.EncodeToString(raw), nil
}
