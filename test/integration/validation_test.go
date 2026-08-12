//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

// Validation is checked in several places already, one rule at a time and close
// to the rule. What none of those cover is the shape of the whole surface: that
// every endpoint which writes something refuses what it cannot use, and refuses
// it as the caller's mistake rather than as a server fault.
//
// The distinction is the point. A 400 tells the interface which field to mark
// and the caller what to change; a 500 tells them the application is broken, and
// a 201 tells them nothing at all while storing something nobody meant. So this
// walks the write endpoints with values a form should never have sent - and would
// not, which is exactly why the server has to be the one that says no.
//
// It is not a fuzz test. Every case here is a value somebody actually sends: an
// empty field, a pasted address with spaces, a negative number, a date typed the
// American way, an enum from an older version of the interface.

// badRequest is one call that must be refused.
type badRequest struct {
	what   string
	method string
	path   string
	body   any
}

// The statuses that count as a refusal the caller can act on. 422 is included
// because the framework answers a payload it cannot bind with one.
func refused(status int) bool {
	return status >= http.StatusBadRequest && status < http.StatusInternalServerError
}

// check runs the calls and reports the ones that were not refused, saying which
// way each failed - the two failures need different fixes.
func checkRefusals(t *testing.T, c *client, cases []badRequest) {
	t.Helper()

	for _, tc := range cases {
		response := c.api(tc.method, tc.path, tc.body)

		switch {
		case refused(response.Status):
			// Refused, and the caller is told which field. A refusal with no
			// message leaves the interface with nothing to show.
			if response.Message() == "" {
				t.Errorf("%s: refused with %d but said nothing", tc.what, response.Status)
			}
		case response.Status >= http.StatusInternalServerError:
			t.Errorf("%s: answered %d - the value reached something that could not handle it, "+
				"and the caller is told the application is broken rather than what to correct: %s",
				tc.what, response.Status, truncate(string(response.Body), 200))
		default:
			t.Errorf("%s: accepted with %d, storing something nobody meant: %s",
				tc.what, response.Status, truncate(string(response.Body), 200))
		}
	}
}

// tooLong is longer than any name, label or description is allowed to be.
var tooLong = strings.Repeat("x", 5000)

func TestCreatingAnAccountRefusesWhatCannotBeStored(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	checkRefusals(t, admin, []badRequest{
		{"an account with no name", http.MethodPost, "/users",
			map[string]any{"name": "", "email": "a@example.com", "role": "user", "password": "a-password-1"}},
		{"an account with no address", http.MethodPost, "/users",
			map[string]any{"name": "A", "email": "", "role": "user", "password": "a-password-1"}},
		{"an address that is not one", http.MethodPost, "/users",
			map[string]any{"name": "A", "email": "not-an-address", "role": "user", "password": "a-password-1"}},
		{"an address with a space in it", http.MethodPost, "/users",
			map[string]any{"name": "A", "email": "a b@example.com", "role": "user", "password": "a-password-1"}},
		{"a role nobody has", http.MethodPost, "/users",
			map[string]any{"name": "A", "email": "b@example.com", "role": "sorcerer", "password": "a-password-1"}},
		{"a password too short to be one", http.MethodPost, "/users",
			map[string]any{"name": "A", "email": "c@example.com", "role": "user", "password": "x"}},
		{"a name longer than any column", http.MethodPost, "/users",
			map[string]any{"name": tooLong, "email": "d@example.com", "role": "user", "password": "a-password-1"}},
	})

	// The working times moved out of account creation: they belong to the person they
	// are about, so they are no longer part of the body that creates an account and
	// sending them there changes nothing. The rule that a target must be a possible
	// number of hours did not move - it lives at the door that still sets them, and
	// this is where it is checked.
	var me struct {
		User userResponse `json:"user"`
	}

	admin.must(admin.api(http.MethodGet, "/me", nil), http.StatusOK).Data(t, &me)

	own := path("/users/", me.User.ID, "/working-times")

	checkRefusals(t, admin, []badRequest{
		{"a negative daily target", http.MethodPut, own,
			map[string]any{"dailyTargetHours": -8}},
		{"a daily target longer than a day", http.MethodPut, own,
			map[string]any{"dailyTargetHours": 48}},
		{"a negative daily maximum", http.MethodPut, own,
			map[string]any{"maxDailyHours": -1}},
	})
}

func TestBookingTimeRefusesWhatCannotBeStored(t *testing.T) {
	_, _, worker := startWithWorker(t)

	checkRefusals(t, worker, []badRequest{
		{"no date at all", http.MethodPost, "/timesheets",
			map[string]any{"durationHours": 2}},
		{"a date the American way", http.MethodPost, "/timesheets",
			map[string]any{"date": "08/01/2026", "durationHours": 2}},
		{"a date that is not one", http.MethodPost, "/timesheets",
			map[string]any{"date": "yesterday", "durationHours": 2}},
		{"a day that does not exist", http.MethodPost, "/timesheets",
			map[string]any{"date": "2026-02-31", "durationHours": 2}},
		{"negative hours", http.MethodPost, "/timesheets",
			map[string]any{"date": "2026-08-01", "durationHours": -2}},
		{"more hours than a day has", http.MethodPost, "/timesheets",
			map[string]any{"date": "2026-08-01", "durationHours": 25}},
		{"a project that does not exist", http.MethodPost, "/timesheets",
			map[string]any{"date": "2026-08-01", "durationHours": 2, "projectId": 99999}},
		{"a description longer than any column", http.MethodPost, "/timesheets",
			map[string]any{"date": "2026-08-01", "durationHours": 2, "description": tooLong}},
	})
}

func TestCreatingAProjectRefusesWhatCannotBeStored(t *testing.T) {
	_, _, worker := startWithWorker(t)

	checkRefusals(t, worker, []badRequest{
		{"a project with no name", http.MethodPost, "/projects",
			map[string]any{"name": "", "startDate": "2026-01-01"}},
		{"a name longer than any column", http.MethodPost, "/projects",
			map[string]any{"name": tooLong, "startDate": "2026-01-01"}},
		{"a start date that is not one", http.MethodPost, "/projects",
			map[string]any{"name": "P", "startDate": "whenever"}},
		{"an end date before the start", http.MethodPost, "/projects",
			map[string]any{"name": "P", "startDate": "2026-06-01", "endDate": "2026-01-01"}},
	})
}

func TestChangingOwnSettingsRefusesWhatCannotBeStored(t *testing.T) {
	_, _, worker := startWithWorker(t)

	checkRefusals(t, worker, []badRequest{
		{"a new password too short to be one", http.MethodPut, "/me/password",
			map[string]any{"currentPassword": "a-much-better-password", "newPassword": "x"}},
		{"a language nobody speaks here", http.MethodPut, "/me/language",
			map[string]any{"language": "xx"}},
		{"a timezone that does not exist", http.MethodPut, "/me/timezone",
			map[string]any{"timezone": "Mars/Olympus_Mons"}},
		{"a token with no label", http.MethodPost, "/me/tokens",
			map[string]any{"name": ""}},
		{"a token label longer than any column", http.MethodPost, "/me/tokens",
			map[string]any{"name": tooLong}},
		{"a token that expires in the past", http.MethodPost, "/me/tokens",
			map[string]any{"name": "t", "expiresInDays": -5}},
	})
}

func TestAdministeringRolesRefusesWhatCannotBeStored(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	checkRefusals(t, admin, []badRequest{
		{"a role with no name", http.MethodPost, "/roles",
			map[string]any{"name": "", "permissions": []string{}}},
		{"a permission the server does not enforce", http.MethodPost, "/roles",
			map[string]any{"name": "auditor", "permissions": []string{"everything:always"}}},
		{"a name longer than any column", http.MethodPost, "/roles",
			map[string]any{"name": tooLong, "permissions": []string{}}},
	})
}

// The settings that decide where the data lives and who may sign in. These are
// the ones where a value that is merely stored, rather than refused, is only
// discovered at the next start - which is the worst moment to discover it.
func TestAdministeringTheInstanceRefusesWhatCannotBeStored(t *testing.T) {
	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	checkRefusals(t, admin, []badRequest{
		{"a database dialect nobody has", http.MethodPut, "/settings/datasource",
			map[string]any{"dialect": "oracle", "name": "gtr"}},
		{"a SQLite connection with no file", http.MethodPut, "/settings/datasource",
			map[string]any{"dialect": "sqlite", "name": ""}},
		{"a PostgreSQL connection with no host", http.MethodPut, "/settings/datasource",
			map[string]any{"dialect": "postgres", "name": "gtr", "user": "gtr"}},
		{"a database port that is a word", http.MethodPut, "/settings/datasource",
			map[string]any{"dialect": "postgres", "name": "gtr", "host": "db", "user": "u", "port": "prod"}},
		{"a directory enabled with no host", http.MethodPut, "/settings/ldap",
			map[string]any{"enabled": true, "baseDn": "dc=example,dc=com",
				"userFilter": "(uid=%s)", "port": 389, "defaultRole": "user"}},
		{"a user filter with nothing to substitute", http.MethodPut, "/settings/ldap",
			map[string]any{"enabled": true, "host": "ldap", "baseDn": "dc=example,dc=com",
				"userFilter": "(uid=fixed)", "port": 389, "defaultRole": "user"}},
		{"a directory port outside the range", http.MethodPut, "/settings/ldap",
			map[string]any{"enabled": true, "host": "ldap", "baseDn": "dc=example,dc=com",
				"userFilter": "(uid=%s)", "port": 70000, "defaultRole": "user"}},
		{"a default role nobody has", http.MethodPut, "/settings/ldap",
			map[string]any{"enabled": true, "host": "ldap", "baseDn": "dc=example,dc=com",
				"userFilter": "(uid=%s)", "port": 389, "defaultRole": "sorcerer"}},
		{"an instance timezone that does not exist", http.MethodPut, "/settings/timezone",
			map[string]any{"timezone": "Mars/Olympus_Mons"}},

		// The labelling. Each of these carries a maxlength in the form and the
		// endpoint took whatever it was sent, so the limit applied to whoever used
		// the screen and to nobody else - and the title and the banner are in the
		// one response this application hands out before there is a session.
		{"a title longer than the form allows", http.MethodPut, "/settings/branding",
			map[string]any{"title": tooLong}},
		{"a banner longer than the form allows", http.MethodPut, "/settings/branding",
			map[string]any{"title": "Fine", "banner": tooLong}},
		{"a footer longer than the form allows", http.MethodPut, "/settings/branding",
			map[string]any{"title": "Fine", "footerText": tooLong}},
		{"a legal notice longer than the form allows", http.MethodPut, "/settings/branding",
			map[string]any{"title": "Fine", "legalNotice": tooLong}},
		{"a company name longer than the form allows", http.MethodPut, "/settings/branding",
			map[string]any{"title": "Fine", "companyName": tooLong}},
	})

	// And what the form does allow is stored, so the limits are the form's rather
	// than something stricter nobody can reach.
	admin.must(admin.api(http.MethodPut, "/settings/branding", map[string]any{
		"title":       strings.Repeat("t", 80),
		"banner":      strings.Repeat("b", 300),
		"footerText":  strings.Repeat("f", 200),
		"legalNotice": strings.Repeat("l", 200),
		"companyName": strings.Repeat("c", 120),
	}), http.StatusOK)
}
