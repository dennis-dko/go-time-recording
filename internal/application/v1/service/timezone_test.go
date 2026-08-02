package service_test

import (
	"context"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// A zone is stored so the application can decide which calendar day hours
// belong to. A name that does not resolve would leave every such decision
// falling back to UTC, silently, so it is rejected rather than accepted and
// worked around later.

func TestSetTimezoneStoresAValidZone(t *testing.T) {
	f, sessions := newSessionFixture(t, nil)

	if err := sessions.SetTimezone(context.Background(), f.userID, "Pacific/Auckland"); err != nil {
		t.Fatalf("set timezone: %v", err)
	}

	user, err := f.userRepo.GetByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if user.Timezone != "Pacific/Auckland" {
		t.Errorf("expected the zone to be stored, got %q", user.Timezone)
	}
}

// Clearing it is the normal way back to following the instance, so an empty
// value has to be accepted rather than treated as a missing field.
func TestClearingTheTimezoneFollowsTheInstanceAgain(t *testing.T) {
	f, sessions := newSessionFixture(t, nil)

	if err := sessions.SetTimezone(context.Background(), f.userID, "Pacific/Auckland"); err != nil {
		t.Fatalf("set timezone: %v", err)
	}

	if err := sessions.SetTimezone(context.Background(), f.userID, ""); err != nil {
		t.Fatalf("clearing the zone must be allowed: %v", err)
	}

	user, err := f.userRepo.GetByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if user.Timezone != "" {
		t.Errorf("expected an empty zone, got %q", user.Timezone)
	}

	if got := user.TimezoneOf("Europe/Berlin").String(); got != "Europe/Berlin" {
		t.Errorf("expected the account to follow the instance, got %q", got)
	}
}

func TestSetTimezoneRejectsAnUnknownZone(t *testing.T) {
	f, sessions := newSessionFixture(t, nil)

	// Reads like a real zone, is not one.
	if err := sessions.SetTimezone(context.Background(), f.userID, "Europe/Munich"); err == nil {
		t.Fatal("an unknown zone must be refused rather than stored")
	}

	user, err := f.userRepo.GetByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if user.Timezone != "" {
		t.Errorf("nothing should have been stored, got %q", user.Timezone)
	}
}

// Whitespace around a pasted zone name is the sort of thing that turns a valid
// choice into a silent fallback to UTC.
func TestSetTimezoneTrimsWhitespace(t *testing.T) {
	f, sessions := newSessionFixture(t, nil)

	if err := sessions.SetTimezone(context.Background(), f.userID, "  Europe/Berlin  "); err != nil {
		t.Fatalf("a padded but valid zone should be accepted: %v", err)
	}

	user, err := f.userRepo.GetByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if user.Timezone != "Europe/Berlin" {
		t.Errorf("expected the trimmed name, got %q", user.Timezone)
	}
}

// The zone must survive a sign-in: reconciling against a directory rewrites the
// account, and dropping the zone there would move someone's hours a day.
func TestSignInKeepsThePersonalTimezone(t *testing.T) {
	f, sessions := newSessionFixture(t, &service.ExternalUser{
		ID: "uuid-1", Email: "abroad@example.com", Name: "Abroad",
	})

	id := externalUser(t, f, "abroad@example.com")

	if err := sessions.SetTimezone(context.Background(), id, "Pacific/Auckland"); err != nil {
		t.Fatalf("set timezone: %v", err)
	}

	if _, err := sessions.Login(context.Background(), "abroad@example.com", "anything", ""); err != nil {
		t.Fatalf("login: %v", err)
	}

	user, err := f.userRepo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if user.Timezone != "Pacific/Auckland" {
		t.Errorf("the zone must survive a directory sign-in, got %q", user.Timezone)
	}
}

// The reason any of this exists, stated as a test: at one instant, two people
// are on different days, and their bookings must land accordingly.
func TestBookingDayDependsOnTheZone(t *testing.T) {
	instant := time.Date(2026, 8, 1, 22, 30, 0, 0, time.UTC)

	cases := map[string]string{
		"Europe/Berlin":     "2026-08-02",
		"America/New_York":  "2026-08-01",
		"Pacific/Auckland":  "2026-08-02",
		"America/Anchorage": "2026-08-01",
	}

	for zone, want := range cases {
		t.Run(zone, func(t *testing.T) {
			user := &model.User{Timezone: zone}

			got := instant.In(user.TimezoneOf(model.DefaultTimezone)).Format("2006-01-02")
			if got != want {
				t.Errorf("at %s UTC, %s is on %s, expected %s",
					instant.Format("15:04"), zone, got, want)
			}
		})
	}
}
