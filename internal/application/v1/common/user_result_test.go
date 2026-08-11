package common_test

import (
	"reflect"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/common"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The staff list is where an administrator decides who to edit or remove, so
// what it leaves out matters as much as what it shows. A directory-backed
// account displayed as local invites two useless actions: setting a password
// that is never checked, and a deletion the next synchronisation undoes.
//
// This was a real gap - the list carried neither the external flag nor the
// language or timezone - so the test walks every field rather than only the
// one that was missing.
func TestUserResultCarriesWhatTheListNeedsToShow(t *testing.T) {
	user := &model.User{
		ID:                 7,
		Name:               "Bob Builder",
		Email:              "bob@example.com",
		RoleID:             3,
		RoleName:           "user",
		IsSystem:           false,
		MustChangePassword: true,
		IsExternal:         true,
		ExternalID:         "uuid-1",
		TOTPEnabled:        true,
		Language:           model.LanguageGerman,
		Timezone:           "Pacific/Auckland",
		DailyTargetHours:   7.5,
		MaxDailyHours:      10,
	}

	results := common.NewUserResultFromModel(user)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}

	got := results[0]

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"ID", got.ID, uint(7)},
		{"Name", got.Name, "Bob Builder"},
		{"Email", got.Email, "bob@example.com"},
		{"RoleID", got.RoleID, uint(3)},
		{"Role", got.Role, "user"},
		{"IsSystem", got.IsSystem, false},
		{"MustChangePassword", got.MustChangePassword, true},
		{"IsExternal", got.IsExternal, true},
		{"TOTPEnabled", got.TOTPEnabled, true},
		{"Language", got.Language, model.LanguageGerman},
		{"Timezone", got.Timezone, "Pacific/Auckland"},
		{"DailyTargetHours", got.DailyTargetHours, 7.5},
		{"MaxDailyHours", got.MaxDailyHours, 10.0},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: expected %v, got %v", c.field, c.want, c.got)
		}
	}
}

// No password material may travel outwards, whatever else is added later.
func TestUserResultCarriesNoPasswordMaterial(t *testing.T) {
	results := common.NewUserResultFromModel(&model.User{
		Email:        "someone@example.com",
		PasswordHash: "$2a$10$averysecrethash",
		TOTPSecret:   "JBSWY3DPEHPK3PXP",
	})

	// A struct with no such field cannot leak one; this asserts the shape stays
	// that way, since the compiler is what enforces it.
	got := results[0]

	for _, field := range []string{"PasswordHash", "TOTPSecret"} {
		if hasField(got, field) {
			t.Errorf("UserResult must not carry %s", field)
		}
	}
}

// An account with no stored language reports both: nothing chosen, and English
// applying.
//
// This used to report only the resolved value, and that is what stopped the
// interface adopting the browser's language. It offers that once, on a first
// sign-in, and the only way to know it is a first sign-in is a language that is not
// there - so every account arrived looking like it had chosen English, and the
// browser was never asked. The timezone beside it has carried the pair for exactly
// this reason all along.
func TestUserResultReportsAnUnchosenLanguageAsUnchosen(t *testing.T) {
	results := common.NewUserResultFromModel(&model.User{Email: "a@example.com"})

	if results[0].Language != "" {
		t.Errorf("expected no stored language, got %q", results[0].Language)
	}

	if results[0].EffectiveLanguage != model.DefaultLanguage {
		t.Errorf("expected the default to apply, got %q", results[0].EffectiveLanguage)
	}
}

// And an account that did choose reports the choice in both places, so nothing
// that only wants "which language" has to know about the pair.
func TestUserResultReportsAChosenLanguageInBoth(t *testing.T) {
	results := common.NewUserResultFromModel(&model.User{Email: "a@example.com", Language: "de"})

	if results[0].Language != "de" || results[0].EffectiveLanguage != "de" {
		t.Errorf("expected de in both, got %q and %q",
			results[0].Language, results[0].EffectiveLanguage)
	}
}

// An empty timezone is passed through unresolved on purpose: the caller has to
// tell "follows the instance" apart from a deliberate choice, and only the
// interface layer knows what the instance setting is.
func TestUserResultLeavesAnUnsetTimezoneEmpty(t *testing.T) {
	results := common.NewUserResultFromModel(&model.User{Email: "a@example.com"})

	if results[0].Timezone != "" {
		t.Errorf("expected an empty timezone to stay empty, got %q", results[0].Timezone)
	}
}

func TestNewUserResultFromModelHandlesNoInput(t *testing.T) {
	if got := common.NewUserResultFromModel(); got != nil {
		t.Errorf("expected nil for no input, got %v", got)
	}
}

// hasField reports whether the struct declares a field of that name.
func hasField(v any, name string) bool {
	_, found := reflect.TypeOf(v).Elem().FieldByName(name)

	return found
}
