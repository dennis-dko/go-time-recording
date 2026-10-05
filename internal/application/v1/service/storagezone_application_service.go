package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
)

// StorageZoneService answers one question at start-up on a database that keeps a
// moment without its zone: is this process in the zone the stored moments were
// written in.
//
// That database is MySQL. GoFr opens it with loc=Local, and a moment is kept
// there in a DATETIME, so it is stored as the clock of the writing process and
// read back through the zone of the reading one. A day - midnight UTC to the
// application - written by a process in UTC was read by one in Europe/Berlin as
// the day before, and that day's totals did not find it: measured. Setting TZ on
// a container is all it takes, and nothing in the stored moment says it was
// written elsewhere, so the zone is recorded by the first start and a start in
// another refuses rather than moving every entry. PostgreSQL keeps the moment in
// a TIMESTAMPTZ and SQLite in text with its offset, so main asks this of MySQL
// alone.
type StorageZoneService struct {
	settings repository.SettingsRepository
	zone     *time.Location
	name     string
}

// NewStorageZoneService is given the zone this process reads its clock in, and
// what to call it in a sentence: Go does not name the system's zone.
func NewStorageZoneService(
	settings repository.SettingsRepository,
	zone *time.Location,
	name string,
) *StorageZoneService {
	return &StorageZoneService{settings: settings, zone: zone, name: name}
}

// Verify reports whether this process is in the zone the database's moments were
// written in, and records it the first time.
//
// The first time is also the first start of this version on a database written
// by an older one, which recorded nothing: its zone is taken to be the one it is
// started in, which is right unless it was moved before the update.
func (s *StorageZoneService) Verify(ctx context.Context) error {
	stored, err := s.settings.Get(ctx, model.SettingStorageZone)
	if err != nil {
		return err
	}

	here := zoneIdentity(s.zone)

	if stored == "" {
		return s.settings.Set(ctx, model.SettingStorageZone, here+" "+s.name)
	}

	written, name, _ := strings.Cut(stored, " ")
	if written == here {
		return nil
	}

	return fmt.Errorf("this MySQL database's times were written in %s (%s) and this process runs in %s (%s); "+
		"MySQL keeps a time without its zone, so every entry would be read as another day - "+
		"start the application in %s, and see deploy/OPERATIONS.md if the database has to move",
		name, written, s.name, here, name)
}

// zoneIdentity is what a zone is taken to be: its offsets from UTC in the middle
// of January and of July of a year that is over, written as "+01:00/+02:00".
//
// A year that is over, because a new release of the zone database changes the
// rules still to come and almost never those that have been, so the same zone
// gives the same answer after an update. And the offsets rather than a name,
// because two zones that agree on them store every day alike, whatever each is
// called.
func zoneIdentity(zone *time.Location) string {
	offset := func(month time.Month) string {
		return time.Date(2025, month, 15, 12, 0, 0, 0, time.UTC).In(zone).Format("-07:00")
	}

	return offset(time.January) + "/" + offset(time.July)
}
