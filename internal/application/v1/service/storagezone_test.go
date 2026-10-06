package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// The zone the first start records is the one every later start is held to, by
// what it does to a stored moment rather than by what it is called.
func TestALaterStartIsHeldToTheZoneTheFirstOneRecorded(t *testing.T) {
	ctx := context.Background()
	store := newStubSettings()

	verify := func(name string) error {
		zone, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}

		return service.NewStorageZoneService(store, zone, name).Verify(ctx)
	}

	if err := verify("Europe/Berlin"); err != nil {
		t.Fatalf("the first start was refused: %v", err)
	}

	if got, want := store.values[model.SettingStorageZone], "+01:00/+02:00 Europe/Berlin"; got != want {
		t.Errorf("the first start recorded %q, want %q", got, want)
	}

	// The same zone, and one that stores every day exactly as it does.
	for _, same := range []string{"Europe/Berlin", "Europe/Paris"} {
		if err := verify(same); err != nil {
			t.Errorf("a start in %s was refused: %v", same, err)
		}
	}

	// UTC moves every day written in Berlin; London agrees with UTC in winter and
	// with Berlin in neither season.
	for _, other := range []string{"UTC", "Europe/London", "America/New_York"} {
		err := verify(other)
		if err == nil {
			t.Errorf("a start in %s was let through on a database written in Europe/Berlin", other)

			continue
		}

		for _, said := range []string{"Europe/Berlin", "+01:00/+02:00", other} {
			if !strings.Contains(err.Error(), said) {
				t.Errorf("the refusal of %s does not say %q: %v", other, said, err)
			}
		}
	}

	if got := store.values[model.SettingStorageZone]; got != "+01:00/+02:00 Europe/Berlin" {
		t.Errorf("a refused start wrote over what was recorded: %q", got)
	}
}
