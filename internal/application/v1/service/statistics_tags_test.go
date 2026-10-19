package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/command"
	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	domainservice "github.com/dennis-dko/go-time-recording/internal/domain/service"
)

// The activities the descriptions name are totalled beside the projects: an entry
// naming two counts towards both, the most hours come first, and what names none
// is its own figure.
func TestTheStatisticsTotalTheActivities(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, booking := range []struct {
		hours       float64
		description string
	}{
		{1.5, "Sprint planning #meeting"},
		{0.5, "#Meeting and #review of the release"},
		{3, "Parser #review"},
		{2, "Quiet work"},
	} {
		description := booking.description

		if _, err := f.timesheets.CreateTimesheet(ctx, command.CreateTimesheetCommand{
			UserID: f.userID, Date: day(14), DurationHours: booking.hours, Description: &description,
		}); err != nil {
			t.Fatalf("book %q: %v", booking.description, err)
		}
	}

	stats, err := service.NewStatisticsService(f.timesheetRepo, f.projectRepo).
		Own(ctx, f.userID, day(13), day(19), domainservice.ProjectScope{})
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}

	want := []service.TagTotal{{Tag: "review", Hours: 3.5}, {Tag: "meeting", Hours: 2}}

	if len(stats.Tags) != len(want) || stats.Tags[0] != want[0] || stats.Tags[1] != want[1] {
		t.Errorf("the activities read %+v, want %+v", stats.Tags, want)
	}

	if stats.UntaggedHours != 2 || stats.TotalHours != 7 {
		t.Errorf("%.2f hours name no activity of %.2f, want 2 of 7", stats.UntaggedHours, stats.TotalHours)
	}
}
