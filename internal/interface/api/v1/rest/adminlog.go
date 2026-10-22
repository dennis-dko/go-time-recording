package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// administrativeChanges is every change the administration log records: the route
// it arrives on, the code the log names it by, and the one field of the request
// worth keeping beside it. test/adminlog_test.go holds this list against the
// router, so a new route that changes accounts, roles or settings is either here
// or named there as one that changes nothing.
//
// A field is kept only where it says what the change was and names no person: the
// role an account was given, a role's own name, whether maintenance went on.
var administrativeChanges = []struct {
	method, route, action, keeps string
}{
	{http.MethodPost, "/api/v1/users", "user.created", ""},
	{http.MethodPut, "/api/v1/users/{id}", "user.updated", ""},
	{http.MethodDelete, "/api/v1/users/{id}", "user.deleted", ""},
	{http.MethodPut, "/api/v1/users/{id}/role", "user.role", "role"},
	{http.MethodPut, "/api/v1/users/{id}/password", "user.password", ""},
	{http.MethodPost, "/api/v1/users/import", "users.imported", ""},
	{http.MethodPost, "/api/v1/roles", "role.created", "name"},
	{http.MethodPut, "/api/v1/roles/{id}", "role.updated", "name"},
	{http.MethodDelete, "/api/v1/roles/{id}", "role.deleted", ""},
	{http.MethodPost, "/api/v1/roles/import", "roles.imported", ""},
	{http.MethodPut, "/api/v1/settings/maintenance", "settings.maintenance", "enabled"},
	{http.MethodPut, "/api/v1/settings/branding", "settings.branding", ""},
	{http.MethodPut, "/api/v1/settings/ldap", "settings.ldap", ""},
	{http.MethodPut, "/api/v1/settings/openid", "settings.openid", ""},
	{http.MethodPost, "/api/v1/settings/ldap/sync", "directory.synchronised", ""},
	{http.MethodPut, "/api/v1/settings/operational", "settings.operational", ""},
	{http.MethodPut, "/api/v1/settings/timezone", "settings.timezone", ""},
	{http.MethodPut, "/api/v1/settings/holidays", "settings.holidays", ""},
	{http.MethodPut, "/api/v1/settings/datasource", "settings.datasource", ""},
	{http.MethodPut, "/api/v1/settings/telemetry", "settings.telemetry", ""},
	{http.MethodPost, "/api/v1/settings/restart", "installation.restart", ""},
	{http.MethodPost, "/api/v1/settings/update", "installation.update", ""},
	{http.MethodPost, "/api/v1/setup/complete", "installation.setup", ""},
}

// loggedRoute is one of administrativeChanges, ready to be matched.
type loggedRoute struct {
	method, action, keeps string
	path                  *regexp.Regexp
}

// loggedRoutes compiles the table once: "{id}" becomes the number it stands for.
func loggedRoutes() []loggedRoute {
	routes := make([]loggedRoute, 0, len(administrativeChanges))

	for _, change := range administrativeChanges {
		pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(change.route), `\{id\}`, `(\d+)`) + "$"
		routes = append(routes, loggedRoute{
			method: change.method, action: change.action, keeps: change.keeps,
			path: regexp.MustCompile(pattern),
		})
	}

	return routes
}

// errorLogger is the part of a logger the administration log needs.
type errorLogger interface {
	Errorf(format string, args ...any)
}

// AdministrationLog records every administrative change that succeeded, with who
// made it - a middleware rather than a call in each handler, so a handler cannot
// forget it, and after authentication, which is what says who is asking.
//
// A refused request changes nothing and is not recorded; a change that went
// through and could not be recorded is logged as an error and still answered as
// the success it was, because the change has happened either way.
func AdministrationLog(log *service.AdminLogService, logger errorLogger) func(http.Handler) http.Handler {
	routes := loggedRoutes()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, target, logged := matchLogged(routes, r)
			principal, signedIn := principalFromContext(r.Context())

			if !logged || !signedIn || principal.User == nil {
				next.ServeHTTP(w, r)

				return
			}

			var sent []byte

			if route.keeps != "" && r.Body != nil {
				read, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "the request could not be read", http.StatusBadRequest)

					return
				}

				sent = read
				r.Body = io.NopCloser(bytes.NewReader(read))
			}

			answer := &answerRecorder{ResponseWriter: w, status: http.StatusOK, keep: target == 0}
			next.ServeHTTP(answer, r)

			if answer.status < http.StatusOK || answer.status >= http.StatusMultipleChoices {
				return
			}

			if target == 0 && strings.Contains(route.action, ".created") {
				target = createdID(answer.body.Bytes())
			}

			err := log.Record(r.Context(), principal.User.ID, route.action, target, keptValue(sent, route.keeps))
			if err != nil {
				logger.Errorf("the administration log could not keep %s by account %d: %v",
					route.action, principal.User.ID, err)
			}
		})
	}
}

// matchLogged finds the logged route a request arrived on, and the id in its path.
func matchLogged(routes []loggedRoute, r *http.Request) (loggedRoute, uint, bool) {
	for _, route := range routes {
		if route.method != r.Method {
			continue
		}

		found := route.path.FindStringSubmatch(r.URL.Path)
		if found == nil {
			continue
		}

		var target uint

		if len(found) > 1 {
			id, err := strconv.ParseUint(found[1], 10, 64)
			if err != nil {
				return route, 0, false
			}

			target = uint(id)
		}

		return route, target, true
	}

	return loggedRoute{}, 0, false
}

// answerRecorder notes the status a handler answered with, and keeps the body of
// an answer that carries the id of what it created.
type answerRecorder struct {
	http.ResponseWriter

	status int
	keep   bool
	body   bytes.Buffer
}

func (a *answerRecorder) WriteHeader(status int) {
	a.status = status
	a.ResponseWriter.WriteHeader(status)
}

func (a *answerRecorder) Write(p []byte) (int, error) {
	if a.keep && a.body.Len() < 1<<16 {
		a.body.Write(p)
	}

	return a.ResponseWriter.Write(p)
}

// createdID reads the id out of an answer that created something.
func createdID(answer []byte) uint {
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}

	if json.Unmarshal(answer, &created) != nil {
		return 0
	}

	return created.Data.ID
}

// keptValue reads one top-level field of a request as text, or nothing.
func keptValue(sent []byte, field string) string {
	if field == "" || len(sent) == 0 {
		return ""
	}

	var fields map[string]any
	if json.Unmarshal(sent, &fields) != nil {
		return ""
	}

	value, found := fields[field]
	if !found || value == nil {
		return ""
	}

	kept := fmt.Sprint(value)
	if len(kept) > 255 {
		kept = kept[:255]
	}

	return kept
}

// AdminLogHandler serves the administration log.
type AdminLogHandler struct {
	log   *service.AdminLogService
	authz *Authorizer
}

// NewAdminLogHandler creates the handler.
func NewAdminLogHandler(log *service.AdminLogService, authz *Authorizer) *AdminLogHandler {
	return &AdminLogHandler{log: log, authz: authz}
}

// adminLogPage is how many changes the log answers at a time.
const adminLogPage = 50

// AdminLogEntryResponse is one change as the log shows it; a name is empty for an
// account or a role that is gone.
type AdminLogEntryResponse struct {
	ID     uint                  `json:"id"`
	At     time.Time             `json:"at"`
	Actor  AdminLogNameResponse  `json:"actor"`
	Action string                `json:"action"`
	Target *AdminLogNameResponse `json:"target,omitempty"`
	Detail string                `json:"detail,omitempty"`
}

// AdminLogNameResponse is an id and the name it carries now.
type AdminLogNameResponse struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// AdminLogResponse is a page of the log, newest first, and whether older changes
// remain.
type AdminLogResponse struct {
	Entries []AdminLogEntryResponse `json:"entries"`
	More    bool                    `json:"more"`
}

// Changes handles GET /api/v1/admin/changes, a page at a time: ?before= the id of
// the oldest change already shown.
func (h *AdminLogHandler) Changes(c *gofr.Context) (any, error) {
	if _, err := h.authz.Require(c, model.PermSettingsManage); err != nil {
		return nil, err
	}

	before, err := queryUint(c, "before")
	if err != nil {
		return nil, toHTTPError(err)
	}

	entries, more, err := h.log.Page(c, before, adminLogPage)
	if err != nil {
		return nil, toHTTPError(err)
	}

	answer := AdminLogResponse{Entries: make([]AdminLogEntryResponse, 0, len(entries)), More: more}

	for _, entry := range entries {
		shown := AdminLogEntryResponse{
			ID: entry.ID, At: entry.At.UTC(), Action: entry.Action, Detail: entry.Detail,
			Actor: AdminLogNameResponse{ID: entry.ActorID, Name: entry.ActorName},
		}

		if entry.TargetID != 0 {
			shown.Target = &AdminLogNameResponse{ID: entry.TargetID, Name: entry.TargetName}
		}

		answer.Entries = append(answer.Entries, shown)
	}

	return answer, nil
}
