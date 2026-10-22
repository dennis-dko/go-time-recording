package model_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// october is a calendar day in October 2026.
func october(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

// worked is a recorded day: from start to end on the clock, in minutes after
// midnight, with breaks minutes of breaks.
func worked(on time.Time, start, end, breaks int) model.WorkDay {
	return model.WorkDay{UserID: 1, Day: on, StartMinute: start, EndMinute: end, BreakMinutes: breaks}
}

// What the Working Hours Act says about a day is read from what was recorded, and
// it is the act's three limits that a record can show: no more than ten hours
// (section 3), a break that grows with the hours (section 4) and eleven hours of
// rest before the next shift (section 5). A day inside all three says nothing.
func TestTheActIsReadFromTheRecordedDays(t *testing.T) {
	cases := []struct {
		name string
		days []model.WorkDay
		want map[time.Time][]model.WorkingTimeNotice
	}{
		{
			name: "an ordinary day",
			days: []model.WorkDay{worked(october(20), 8*60, 16*60+30, 30)},
			want: map[time.Time][]model.WorkingTimeNotice{},
		},
		{
			name: "more than ten hours",
			days: []model.WorkDay{worked(october(16), 7*60+30, 19*60, 45)},
			want: map[time.Time][]model.WorkingTimeNotice{
				october(16): {{Rule: model.NoticeLongDay, Minutes: 645}},
			},
		},
		{
			name: "exactly ten hours is allowed",
			days: []model.WorkDay{worked(october(16), 7*60, 17*60+45, 45)},
			want: map[time.Time][]model.WorkingTimeNotice{},
		},
		{
			name: "past six hours a break of thirty minutes",
			days: []model.WorkDay{worked(october(17), 8*60, 14*60+25, 20)},
			want: map[time.Time][]model.WorkingTimeNotice{
				october(17): {{Rule: model.NoticeShortBreak, Minutes: 30}},
			},
		},
		{
			name: "six hours exactly needs no break",
			days: []model.WorkDay{worked(october(17), 8*60, 14*60, 0)},
			want: map[time.Time][]model.WorkingTimeNotice{},
		},
		{
			name: "past nine hours a break of forty-five minutes",
			days: []model.WorkDay{worked(october(17), 8*60, 18*60, 30)},
			want: map[time.Time][]model.WorkingTimeNotice{
				october(17): {{Rule: model.NoticeShortBreak, Minutes: 45}},
			},
		},
		{
			name: "fewer than eleven hours between two shifts",
			days: []model.WorkDay{
				worked(october(16), 9*60, 23*60, 60),
				worked(october(17), 7*60, 15*60, 30),
			},
			want: map[time.Time][]model.WorkingTimeNotice{
				october(16): {{Rule: model.NoticeLongDay, Minutes: 780}},
				october(17): {{Rule: model.NoticeShortRest, Minutes: 480}},
			},
		},
		{
			name: "a day off in between is rest enough",
			days: []model.WorkDay{
				worked(october(16), 12*60, 20*60, 30),
				worked(october(18), 6*60, 14*60, 30),
			},
			want: map[time.Time][]model.WorkingTimeNotice{},
		},
		{
			// A night is recorded as the two days it falls on, and it is one shift:
			// eight hours without a break, not two short days with no rest between.
			name: "a night over midnight is one shift",
			days: []model.WorkDay{
				worked(october(17), 22*60, model.MinutesPerDay, 0),
				worked(october(18), 0, 6*60, 0),
			},
			want: map[time.Time][]model.WorkingTimeNotice{
				october(17): {{Rule: model.NoticeShortBreak, Minutes: 30}},
			},
		},
		{
			name: "every limit at once, in the order of the act",
			days: []model.WorkDay{
				worked(october(19), 10*60, 22*60, 0),
				worked(october(20), 6*60, 18*60, 15),
			},
			want: map[time.Time][]model.WorkingTimeNotice{
				october(19): {{Rule: model.NoticeLongDay, Minutes: 720}, {Rule: model.NoticeShortBreak, Minutes: 45}},
				october(20): {
					{Rule: model.NoticeLongDay, Minutes: 705},
					{Rule: model.NoticeShortBreak, Minutes: 45},
					{Rule: model.NoticeShortRest, Minutes: 480},
				},
			},
		},
		{
			// Given out of order, as nothing promises they arrive sorted.
			name: "the days are read in calendar order",
			days: []model.WorkDay{
				worked(october(17), 7*60, 15*60, 30),
				worked(october(16), 9*60, 23*60, 60),
			},
			want: map[time.Time][]model.WorkingTimeNotice{
				october(16): {{Rule: model.NoticeLongDay, Minutes: 780}},
				october(17): {{Rule: model.NoticeShortRest, Minutes: 480}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.WorkingTimeNotices(tc.days)

			if len(got) != len(tc.want) {
				t.Fatalf("notices on %d day(s), want %d: %+v", len(got), len(tc.want), got)
			}

			for day, want := range tc.want {
				if !slices.Equal(got[day], want) {
					t.Errorf("%s says %+v, want %+v", day.Format(time.DateOnly), got[day], want)
				}
			}
		})
	}
}
