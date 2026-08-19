//go:build integration

package integration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/test/harness"
)

// Every refusal this API can give names itself.
//
// The point of a code is that something other than an English reader can act on
// it: the interface looks up a sentence in the reader's own language, and support
// has a stable name for a thing rather than a paraphrase of a message that has
// since been reworded. A refusal without one is a refusal that arrives as
// somebody's English and stays that way.
//
// Six of these had none. They were the ones that do not go through the layer
// where refusals are annotated - a session check in a middleware, a rate limiter,
// the cross-site check, GoFr's own not-found - so each was written where it
// happened, in prose, and nothing tied them together.
//
// This provokes them for real, against a running instance, and reads what comes
// back. Written as a table rather than as a claim, because "all of them" is only
// worth saying if something checks it.
func TestEveryErrorTheAPICanGiveIsNamed(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	stranger := a.newClient()

	cases := []struct {
		what   string
		want   string
		status int
		do     func() response
	}{
		{
			what:   "no session at all",
			want:   apperror.CodeUnauthenticated,
			status: http.StatusUnauthorized,
			do:     func() response { return stranger.api(http.MethodGet, "/me", nil) },
		},
		{
			what:   "signed in, and not allowed",
			want:   "missingPermission",
			status: http.StatusForbidden,
			do: func() response {
				worker := a.signInAsUser(admin, "Wera", "wera@example.com")

				return worker.api(http.MethodGet, "/users", nil)
			},
		},
		{
			what:   "something that is not there",
			want:   apperror.CodeNotFound,
			status: http.StatusNotFound,
			do: func() response {
				return admin.api(http.MethodGet, "/users/999999", nil)
			},
		},
		{
			what:   "a body that is not JSON",
			want:   "bodyNotJSON",
			status: http.StatusBadRequest,
			do: func() response {
				// An endpoint this account may actually post to: the built-in
				// administrator records no time and owns no projects, so a
				// malformed body sent there is refused for the wrong reason.
				return admin.raw(http.MethodPost, "/api/v1/users", []byte("{not json"))
			},
		},
		{
			what:   "fields that are not acceptable",
			want:   apperror.CodeInvalidFields,
			status: http.StatusBadRequest,
			do: func() response {
				return admin.api(http.MethodPost, "/users",
					map[string]any{"name": "", "email": "", "role": "user"})
			},
		},
		{
			what:   "a body far larger than anything legitimate",
			want:   apperror.CodeBodyTooLarge,
			status: http.StatusRequestEntityTooLarge,
			do: func() response {
				// Three megabytes of nothing, to an endpoint that expects a name and
				// an address. Nothing bounded this: the body was decoded into a
				// struct with no cap, and http.Server bounds only the headers - so
				// the number here was whatever the caller felt like naming, on
				// /auth/login as readily as anywhere else.
				return admin.raw(http.MethodPost, "/api/v1/users",
					append([]byte(`{"name":"`), append(
						bytes.Repeat([]byte("a"), 3<<20), []byte(`"}`)...)...))
			},
		},
		{
			what:   "a request that cannot prove where it came from",
			want:   apperror.CodeCSRFRejected,
			status: http.StatusForbidden,
			do: func() response {
				return admin.withoutCSRF(http.MethodPost, "/api/v1/users",
					map[string]any{"name": "Anything"})
			},
		},
	}

	for _, one := range cases {
		t.Run(one.what, func(t *testing.T) {
			got := one.do()

			if got.Status != one.status {
				t.Fatalf("answered %d, want %d\n%s", got.Status, one.status, got.Body)
			}

			code := errorCode(t, got)

			if code == "" {
				t.Fatalf("answered %d with no code at all, so nothing can say this "+
					"in the reader's language:\n%s", got.Status, got.Body)
			}

			if code != one.want {
				t.Errorf("named itself %q, want %q", code, one.want)
			}
		})
	}
}

// Nothing generic is emitted that is not in the catalogue.
//
// The other half of the property. A closed set is only closed if joining it is
// deliberate, and the failure this prevents is the quiet one: a handler that
// writes its own code, which then has no sentence anywhere and shows English to
// everybody while looking perfectly annotated in the source.
func TestTheGenericCodesAreTheOnesDeclared(t *testing.T) {
	t.Parallel()

	a := start(t)
	stranger := a.newClient()

	got := errorCode(t, stranger.api(http.MethodGet, "/me", nil))

	if !slices.Contains(apperror.GenericCodes, got) {
		t.Errorf("an endpoint answered with %q, which is not one of the declared "+
			"reasons: %v", got, apperror.GenericCodes)
	}
}

// Something that failed underneath says so generically, and keeps the original.
//
// The connection test is the one place a failure from a driver can be provoked
// on demand, and it is also the screen where this mattered most: the values being
// complained about are in the fields directly above it.
func TestAFailureFromUnderneathIsGenericAndKeepsItsDetail(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	got := admin.api(http.MethodPost, "/settings/datasource/test", map[string]any{
		"dialect":  "postgres",
		"host":     "127.0.0.1",
		"port":     "1",
		"name":     "gtr",
		"user":     "gtr",
		"password": "gtr",
	})

	// A probe answers successfully with the reason inside it: a database that
	// cannot be reached is information about what somebody typed, not a fault
	// here. 201 rather than 200 because it is a POST, and that is what GoFr makes
	// of one.
	if got.Status != http.StatusOK && got.Status != http.StatusCreated {
		t.Fatalf("the probe answered %d\n%s", got.Status, got.Body)
	}

	var body struct {
		Data struct {
			OK    bool `json:"ok"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Detail  string `json:"detail"`
			} `json:"error"`
		} `json:"data"`
	}

	if err := json.Unmarshal(got.Body, &body); err != nil {
		t.Fatalf("the probe answered something unreadable: %v\n%s", err, got.Body)
	}

	if body.Data.OK {
		t.Fatal("a connection to a closed port reported success")
	}

	if body.Data.Error.Code != apperror.CodeProbeFailed {
		t.Errorf("the failure is named %q, want %q",
			body.Data.Error.Code, apperror.CodeProbeFailed)
	}

	// The sentence is this application's, not the driver's.
	if strings.Contains(body.Data.Error.Message, "dial tcp") {
		t.Errorf("the message is the driver's own: %q", body.Data.Error.Message)
	}

	// And the driver's words are kept, because they are the only text that says
	// what actually happened.
	if !strings.Contains(body.Data.Error.Detail, "127.0.0.1:1") {
		t.Errorf("the original wording was lost: %q", body.Data.Error.Detail)
	}
}

// errorCode reads the code out of an error response.
func errorCode(t *testing.T, res response) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(res.Body, &body); err != nil {
		t.Fatalf("the refusal is not JSON: %v\n%s", err, res.Body)
	}

	return body.Error.Code
}

// The roles a fresh installation ships with cannot be deleted.
//
// Two different reasons, and they are worth telling apart. The admin role is
// fixed outright: an installation that loses it loses the way back into its own
// settings, so it cannot be renamed, stripped or removed.
//
// The other two are ordinary roles - what they grant is this installation's
// business and can be edited - and they are also the furniture. One is what every
// new account is given; both are what the directory synchronisation assigns by
// default. Deleting one leaves those without an answer, and putting it back means
// reproducing a list of permissions exactly from memory.
func TestTheShippedRolesCannotBeDeleted(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var roles struct {
		Items []struct {
			ID        uint   `json:"id"`
			Name      string `json:"name"`
			IsSystem  bool   `json:"isSystem"`
			IsDefault bool   `json:"isDefault"`
		} `json:"items"`
	}

	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &roles)

	if len(roles.Items) < 3 {
		t.Fatalf("a fresh installation has %d role(s), want the three it ships with",
			len(roles.Items))
	}

	shipped := 0

	for _, role := range roles.Items {
		if !role.IsDefault {
			continue
		}

		shipped++

		got := admin.api(http.MethodDelete, fmt.Sprintf("/roles/%d", role.ID), nil)

		if got.Status != http.StatusConflict {
			t.Errorf("deleting the shipped role %q answered %d, want %d\n%s",
				role.Name, got.Status, http.StatusConflict, got.Body)
		}

		// Named, so the interface can say which role and why in the reader's
		// language rather than showing the English sentence.
		want := "defaultRoleUndeletable"
		if role.IsSystem {
			want = "systemRoleUndeletable"
		}

		if code := errorCode(t, got); code != want {
			t.Errorf("refusing to delete %q is named %q, want %q", role.Name, code, want)
		}
	}

	if shipped != 3 {
		t.Errorf("%d of the roles are marked as shipped, want 3", shipped)
	}

	// A role somebody made is still deletable, which is the half that must not
	// have been broken by the half above.
	created := admin.api(http.MethodPost, "/roles", map[string]any{
		"name": "Aushilfe", "description": "Temporary", "permissions": []string{"timesheets:read:own"},
	})

	var made struct {
		ID uint `json:"id"`
	}

	admin.must(created, http.StatusCreated).Data(t, &made)

	deleted := admin.api(http.MethodDelete, fmt.Sprintf("/roles/%d", made.ID), nil)

	if deleted.Status != http.StatusNoContent && deleted.Status != http.StatusOK {
		t.Errorf("a role somebody created answered %d when deleted\n%s",
			deleted.Status, deleted.Body)
	}
}

// The shipped roles cannot be renamed or have their rights changed either.
//
// Undeletable came first, on the reasoning that what these grant is an
// installation's own business. What that misses is that they are also the answer
// to questions asked elsewhere: the role every new account is given, the one the
// directory synchronisation assigns, and the pair the interface names in its own
// words. A shipped role renamed or emptied breaks those quietly - the
// installation is left with a role called something else, granting something
// else, under a name the interface still translates.
func TestTheShippedRolesCannotBeEdited(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	var roles struct {
		Items []struct {
			ID        uint     `json:"id"`
			Name      string   `json:"name"`
			IsDefault bool     `json:"isDefault"`
			Rights    []string `json:"permissions"`
		} `json:"items"`
	}

	admin.must(admin.api(http.MethodGet, "/roles", nil), http.StatusOK).Data(t, &roles)

	checked := 0

	for _, role := range roles.Items {
		if !role.IsDefault {
			continue
		}

		checked++

		renamed := admin.api(http.MethodPut, fmt.Sprintf("/roles/%d", role.ID),
			map[string]any{"name": role.Name + "-neu"})

		if renamed.Status != http.StatusConflict {
			t.Errorf("renaming the shipped role %q answered %d, want %d\n%s",
				role.Name, renamed.Status, http.StatusConflict, renamed.Body)
		}

		stripped := admin.api(http.MethodPut, fmt.Sprintf("/roles/%d", role.ID),
			map[string]any{"permissions": []string{"timesheets:read:own"}})

		if stripped.Status != http.StatusConflict {
			t.Errorf("changing the rights of the shipped role %q answered %d, want %d\n%s",
				role.Name, stripped.Status, http.StatusConflict, stripped.Body)
		}
	}

	if checked != 3 {
		t.Errorf("%d shipped roles were checked, want 3", checked)
	}

	// And the description, which was the one part still open. The reasoning was
	// that a description belongs to the installation rather than to the
	// application - but these three are the words the interface translates, keyed
	// on the name, so an installation that edited one got a description in one
	// language that the interface then overrode in another. That reads as the
	// change not having been saved.
	for _, role := range roles.Items {
		if !role.IsDefault {
			continue
		}

		described := admin.api(http.MethodPut, fmt.Sprintf("/roles/%d", role.ID),
			map[string]any{"description": "Von uns beschrieben"})

		if described.Status != http.StatusConflict {
			t.Errorf("describing the shipped role %q answered %d, want %d\n%s",
				role.Name, described.Status, http.StatusConflict, described.Body)
		}

		break
	}
}

// An account the directory owns is not edited or deleted here.
//
// Both used to be allowed and both are worse than being refused, because each
// one looks like it worked:
//
// The name and the address are copied from the entry on every synchronisation,
// so changing them here lasts until the next run and then silently reverts.
//
// Deleting removes a person and, with a purge, everything they recorded - and
// then the next synchronisation creates the account again from the entry that is
// still in the directory. The hours are gone, the account is back, and nothing
// about the directory has changed.
//
// The role is the exception, because the directory has no opinion about it: it is
// decided here, for somebody the directory only says exists.
func TestAnAccountFromTheDirectoryIsEditedInTheDirectory(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	created := admin.api(http.MethodPost, "/users", map[string]any{
		"name": "Dirk", "email": "dirk@example.com",
		"role": "user", "password": "dirk-password-1",
	})

	var made struct {
		ID uint `json:"id"`
	}

	admin.must(created, http.StatusCreated).Data(t, &made)

	// Made into a directory account the way the synchronisation makes one. Through
	// the database because there is no endpoint that does this - the
	// synchronisation is the only thing that creates them, and standing an LDAP
	// server up for one flag would be testing the directory rather than this rule.
	markAsDirectoryAccount(t, a, made.ID)

	// The name is refused.
	renamed := admin.api(http.MethodPut, fmt.Sprintf("/users/%d", made.ID),
		map[string]any{"name": "Dirk Neu"})

	if renamed.Status != http.StatusConflict {
		t.Errorf("renaming a directory account answered %d, want %d\n%s",
			renamed.Status, http.StatusConflict, renamed.Body)
	}

	if code := errorCode(t, renamed); code != "directoryAccountReadOnly" {
		t.Errorf("the refusal is named %q", code)
	}

	// And so is the address, which is what the synchronisation matches on.
	moved := admin.api(http.MethodPut, fmt.Sprintf("/users/%d", made.ID),
		map[string]any{"email": "dirk.neu@example.com"})

	if moved.Status != http.StatusConflict {
		t.Errorf("changing a directory account's address answered %d, want %d",
			moved.Status, http.StatusConflict)
	}

	// Deleting is refused whether or not the hours go with it.
	for _, path := range []string{"", "?purge=true"} {
		removed := admin.api(http.MethodDelete,
			fmt.Sprintf("/users/%d%s", made.ID, path), nil)

		if removed.Status != http.StatusConflict {
			t.Errorf("deleting a directory account%s answered %d, want %d\n%s",
				path, removed.Status, http.StatusConflict, removed.Body)
		}

		if code := errorCode(t, removed); code != "directoryAccountUndeletable" {
			t.Errorf("the refusal is named %q", code)
		}
	}

	// The role is still this installation's to decide.
	role := admin.api(http.MethodPut, fmt.Sprintf("/users/%d/role", made.ID),
		map[string]any{"role": "user-admin"})

	if role.Status != http.StatusOK {
		t.Errorf("changing a directory account's role answered %d\n%s",
			role.Status, role.Body)
	}

	// And the account says where it comes from, so a screen can explain why two
	// of its buttons are missing.
	var listed struct {
		Items []struct {
			ID         uint `json:"id"`
			IsExternal bool `json:"isExternal"`
		} `json:"items"`
	}

	admin.must(admin.api(http.MethodGet, "/users", nil), http.StatusOK).Data(t, &listed)

	found := false

	for _, one := range listed.Items {
		if one.ID == made.ID {
			found = one.IsExternal
		}
	}

	if !found {
		t.Error("the account does not report that it comes from the directory")
	}
}

// markAsDirectoryAccount makes an account look like one the synchronisation
// created.
//
// Through the database because nothing else does it: the synchronisation is the
// only thing that creates these, and standing an LDAP server up for one flag
// would be testing the directory rather than the rule under test.
//
// The placeholders differ by dialect and the application's own queries are
// written with ?, so this rewrites them the way the harness does.
func markAsDirectoryAccount(t *testing.T, a *app, id uint) {
	t.Helper()

	db := a.DB(t)
	if db == nil {
		t.Fatal("this instance has no database")
	}

	query := "UPDATE users SET is_external = ?, external_id = ? WHERE id = ?"

	// The placeholders differ by dialect and the application's own queries are
	// written with ?, so PostgreSQL gets its own numbering.
	if strings.Contains(os.Getenv(harness.DSNEnv), "postgres") {
		query = "UPDATE users SET is_external = $1, external_id = $2 WHERE id = $3"
	}

	if _, err := db.Exec(query, true, fmt.Sprintf("uid=%d", id), id); err != nil {
		t.Fatalf("marking the account as the directory's: %v", err)
	}
}

// A logo is a PNG or a JPEG, and nothing else.
//
// This used to take any image a browser would encode, SVG included, and SVG is
// where it went wrong: the same file that renders perfectly in the header and on
// the sign-in screen can be refused as a tab icon, silently, by an engine with
// its own rules about what it will rasterise. Nothing in the response says so -
// the icon is served, fetched, and then not used, which is a long way to go to
// find out.
func TestALogoMustBeARasterImage(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	refused := admin.api(http.MethodPut, "/settings/branding", map[string]any{
		"title": "Zeiterfassung",
		"logo": "data:image/svg+xml;base64," +
			base64.StdEncoding.EncodeToString([]byte(
				`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"/>`)),
	})

	if refused.Status != http.StatusBadRequest {
		t.Fatalf("an SVG logo answered %d, want %d\n%s",
			refused.Status, http.StatusBadRequest, refused.Body)
	}

	if code := errorCode(t, refused); code != "logoNotRaster" {
		t.Errorf("the refusal is named %q", code)
	}

	// And the two that work everywhere are taken.
	for name, logo := range map[string]string{
		"a PNG":  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
		"a JPEG": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AKp//2Q==",
	} {
		accepted := admin.api(http.MethodPut, "/settings/branding", map[string]any{
			"title": "Zeiterfassung", "logo": logo,
		})

		if accepted.Status != http.StatusOK {
			t.Errorf("%s was refused: %d\n%s", name, accepted.Status, accepted.Body)
		}
	}
}

// Saving the same settings twice is not an error.
//
// It is what somebody does when they are not sure the first press registered,
// and on MySQL it used to answer 500: the write was an UPDATE and, if that
// reported nothing changed, an INSERT - and MySQL counts *changed* rows rather
// than matched ones, so writing a value that was already there fell through to
// the INSERT and hit the primary key.
//
// The dialect that had the bug is the one this only fails on, so it is worth
// saying plainly that this case means nothing unless the suite is run against
// MySQL as well - which CI does.
func TestSavingTheSameSettingsTwiceIsFine(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	body := map[string]any{
		"title":       "Zeiterfassung",
		"banner":      "",
		"footerText":  "Gemacht in Osnabrück",
		"legalNotice": "",
	}

	for attempt := 1; attempt <= 2; attempt++ {
		saved := admin.api(http.MethodPut, "/settings/branding", body)

		if saved.Status != http.StatusOK {
			t.Fatalf("saving the appearance the %d time answered %d\n%s",
				attempt, saved.Status, saved.Body)
		}
	}

	// And what it holds afterwards is what was sent, rather than a row that was
	// written twice and read once.
	var branding struct {
		FooterText string `json:"footerText"`
	}

	admin.must(admin.api(http.MethodGet, "/branding", nil), http.StatusOK).Data(t, &branding)

	if branding.FooterText != "Gemacht in Osnabrück" {
		t.Errorf("the footer reads %q after being saved twice", branding.FooterText)
	}
}

// The part chosen for each place is stored and comes back.
func TestTheChosenPartOfTheLogoIsStored(t *testing.T) {
	t.Parallel()

	a := start(t)
	admin := a.signInAsAdmin("a-much-better-password")

	saved := admin.api(http.MethodPut, "/settings/branding", map[string]any{
		"title": "Zeiterfassung",
		"logo":  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
		"crops": map[string]any{
			"icon": map[string]float64{"x": 0, "y": 0, "w": 0.2, "h": 1},
		},
	})

	if saved.Status != http.StatusOK {
		t.Fatalf("saving answered %d\n%s", saved.Status, saved.Body)
	}

	var branding struct {
		Crops map[string]struct {
			X float64 `json:"x"`
			W float64 `json:"w"`
		} `json:"crops"`
	}

	admin.must(admin.api(http.MethodGet, "/branding", nil), http.StatusOK).Data(t, &branding)

	icon, ok := branding.Crops["icon"]
	if !ok {
		t.Fatalf("nothing came back for the tab's part: %+v", branding.Crops)
	}

	if icon.W != 0.2 {
		t.Errorf("the tab uses %v of the width, want 0.2", icon.W)
	}
}
