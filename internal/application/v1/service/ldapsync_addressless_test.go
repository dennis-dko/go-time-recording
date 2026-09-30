package service_test

import (
	"context"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
)

// An entry without an address keeps the account its identifier names.
//
// The directory lists such an entry because its identifier still says the
// person is there; this is the other half, which holds that the run believes it.
func TestAnEntryWithoutAnAddressKeepsItsAccount(t *testing.T) {
	f := newSyncFixture(t, 0)
	kept := externalUserWithID(t, f.fixture, "mailbox-removed@example.com", "id-1")

	f.directory.users = []service.ExternalUser{
		{ID: "id-1"},
		{ID: "id-2", Email: "somebody@example.com"},
	}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	for _, candidate := range report.Candidates {
		if candidate.UserID == kept {
			t.Errorf("the account whose identifier is in the answer was proposed for deletion: %+v", candidate)
		}
	}

	if len(f.purger.purged) != 0 {
		t.Errorf("nothing may be deleted, but %d account(s) were", len(f.purger.purged))
	}
}

// An answer in which no entry carries an address deletes nobody.
//
// Entries without an address used to be dropped before they reached the run, so
// a mail attribute that names nothing - a typing mistake in the settings - came
// back as an empty answer, and the run refused it. Listed by their identifiers
// now, they no longer make the answer empty, and the refusal has to be asked of
// the entries that have an address: an answer where nobody has one is the same
// broken configuration, and what it would condemn are the accounts kept by
// their address alone.
func TestAnAnswerWithoutAnyAddressDeletesNobody(t *testing.T) {
	f := newSyncFixture(t, 0)
	externalUser(t, f.fixture, "kept-by-address@example.com")

	f.directory.users = []service.ExternalUser{{ID: "someone-else"}}

	report, err := f.sync.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if report.AbortCode != "syncDirectoryAnsweredEmpty" {
		t.Errorf("the run was not refused as an empty answer: aborted %q, code %q",
			report.Aborted, report.AbortCode)
	}

	if len(f.purger.purged) != 0 {
		t.Errorf("nothing may be deleted, but %d account(s) were", len(f.purger.purged))
	}
}
