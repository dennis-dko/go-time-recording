package service_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// A run the deletion limit refuses shows a share that is above the limit.
//
// Both percentages were rounded to whole numbers, so a share just over the limit
// came out equal to it: 126 of 250 accounts is 50.4%, and the refusal read
// "would remove 126 of 250 directory accounts (50%), above the 50% safety
// limit" - wrong on its face, on the one screen where somebody is deciding
// whether to let a run delete people. And a limit set to 12.5% was shown as 13%.
func TestARefusedRunShowsAShareAboveTheLimit(t *testing.T) {
	for _, c := range []struct {
		name      string
		limit     float64
		accounts  int
		departing int
		wantLimit float64
	}{
		{"just over a whole limit", 0.5, 250, 126, 50},
		{"just over a limit that is not whole", 0.125, 1000, 126, 12.5},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newSyncFixture(t, c.limit)

			staying := make([]service.ExternalUser, 0, c.accounts-c.departing)

			// Straight into the repository: a thousand accounts made through the
			// service would each pay for a password hash this case has no use for.
			for i := range c.accounts {
				email := fmt.Sprintf("person%d@example.com", i)

				if _, err := f.userRepo.Save(context.Background(),
					&model.User{Name: email, Email: email, IsExternal: true}); err != nil {
					t.Fatalf("seed %s: %v", email, err)
				}

				if i >= c.departing {
					staying = append(staying, service.ExternalUser{Email: email})
				}
			}

			f.directory.users = staying

			report, err := f.sync.Preview(context.Background())
			if err != nil {
				t.Fatalf("preview: %v", err)
			}

			if report.AbortCode != "syncWouldRemoveTooMany" || len(report.AbortValues) != 4 {
				t.Fatalf("the limit did not refuse the run: %q %v", report.Aborted, report.AbortValues)
			}

			share, limit := number(t, report.AbortValues[2]), number(t, report.AbortValues[3])

			if share <= limit {
				t.Errorf("the refusal shows a share of %v%% beside a limit of %v%%, and says it is "+
					"above it: %s", share, limit, report.Aborted)
			}

			if limit != c.wantLimit {
				t.Errorf("a limit of %v is shown as %v%%: %s", c.limit, limit, report.Aborted)
			}

			if strings.Contains(report.Aborted, fmt.Sprintf("(%v%%), above the %v%%", limit, limit)) {
				t.Errorf("the sentence calls a share equal to the limit above it: %s", report.Aborted)
			}
		})
	}
}

// number reads a figure a refusal carries, whichever numeric type it went in as.
func number(t *testing.T, value any) float64 {
	t.Helper()

	switch v := value.(type) {
	case int:
		return float64(v)
	case float64:
		return v
	}

	t.Fatalf("%v (%T) is not a figure", value, value)

	return 0
}
