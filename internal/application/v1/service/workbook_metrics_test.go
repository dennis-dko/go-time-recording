package service_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/persistence/memory"
)

// histogramSpy keeps every value recorded in a histogram, by name.
type histogramSpy struct {
	mu     sync.Mutex
	values map[string][]float64
}

func (*histogramSpy) IncrementCounter(context.Context, string, ...string) {}

func (h *histogramSpy) RecordHistogram(_ context.Context, name string, value float64, _ ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.values[name] = append(h.values[name], value)
}

// An imported entry counts among the hours booked, as one typed in does.
//
// The metric's own documentation says its sum is what was recorded. The import
// writes its entries through the repository in one transaction, past the
// booking that records the metric, so an installation that brings its hours in
// from a workbook showed a fraction of them.
func TestImportedHoursCountAmongTheHoursBooked(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	spy := &histogramSpy{values: map[string][]float64{}}

	entries := service.NewTimesheetApplicationService(f.timesheetRepo, f.userRepo,
		memory.NewProjectRepository(), 24).WithMetrics(spy)
	workbook := service.NewWorkbookService(f.timesheetRepo, f.userRepo, memory.NewProjectRepository(), entries)

	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	imported, err := workbook.Apply(ctx, &service.ImportPlan{
		Rows: []service.PlannedRow{
			{Number: 2, Date: day, UserID: f.userID, ProjectID: &f.projectID, Hours: 3},
			{Number: 3, Date: day.AddDate(0, 0, 1), UserID: f.userID, ProjectID: &f.projectID, Hours: 5},
		},
		Writable: 2,
	})
	if err != nil || imported != 2 {
		t.Fatalf("the import wrote %d entries: %v", imported, err)
	}

	got := spy.values[service.MetricHoursBooked]
	slices.Sort(got)

	if !slices.Equal(got, []float64{3, 5}) {
		t.Errorf("two imported entries of 3 and 5 hours were recorded as booked hours %v", got)
	}
}
