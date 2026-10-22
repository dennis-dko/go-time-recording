package web_test

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The project form's figures stop where the server does.
//
// A budget and a rate are numbers somebody types, and the server refuses one past
// its bound by the field's name. A form that lets a larger one be typed has
// somebody press Save to be told what the box could have said; one that stops
// short makes a figure the server takes impossible to type.
func TestTheProjectFormStopsWhereTheServerDoes(t *testing.T) {
	html := asset(t, "/")

	for field, bound := range map[string]int{
		"budgetHours": model.MaxBudgetHours,
		"hourlyRate":  model.MaxHourlyRate,
	} {
		match := regexp.MustCompile(`name="` + field + `"[^>]*max="(\d+)"`).FindStringSubmatch(html)
		if match == nil {
			t.Errorf("the %s field has no max, so the form offers what the server refuses", field)

			continue
		}

		if written, _ := strconv.Atoi(match[1]); written != bound {
			t.Errorf("the %s field stops at %d and the server at %d", field, written, bound)
		}
	}

	if !regexp.MustCompile(`name="currency"[^>]*maxlength="3"`).MatchString(html) {
		t.Error("the currency field takes more than the three letters of a code")
	}
}
