//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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
