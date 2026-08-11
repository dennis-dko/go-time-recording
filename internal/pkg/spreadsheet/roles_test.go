package spreadsheet_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// The rights a role holds survive the trip out and back.
//
// A column per permission is the whole point of this sheet, and the thing that can
// go wrong with it is silent: a right read out of the wrong column grants something
// nobody asked for, and looks exactly like a right that was granted on purpose.
func TestRolesRoundTrip(t *testing.T) {
	t.Parallel()

	permissions := []string{"users:read", "users:write", "projects:read", "settings:manage"}

	written, err := spreadsheet.WriteRoles("de", permissions, []spreadsheet.RoleRow{
		{Name: "user", Description: "everyday", Granted: []string{"projects:read"}},
		{Name: "admin", Description: "the installation", System: true,
			Granted: []string{"users:read", "users:write", "settings:manage"}},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	rows, problems, err := spreadsheet.ReadRoles(bytes.NewReader(written), permissions)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	if len(problems) != 0 {
		t.Fatalf("a file this package wrote came back with problems: %v", problems)
	}

	if len(rows) != 2 {
		t.Fatalf("read %d row(s), want 2", len(rows))
	}

	for _, want := range []struct {
		name    string
		granted []string
	}{
		{"user", []string{"projects:read"}},
		{"admin", []string{"users:read", "users:write", "settings:manage"}},
	} {
		index := slices.IndexFunc(rows, func(r spreadsheet.RoleRow) bool { return r.Name == want.name })
		if index < 0 {
			t.Errorf("%q did not come back at all", want.name)

			continue
		}

		got := slices.Clone(rows[index].Granted)
		slices.Sort(got)
		slices.Sort(want.granted)

		if strings.Join(got, ",") != strings.Join(want.granted, ",") {
			t.Errorf("%q came back holding %v, want %v", want.name, got, want.granted)
		}
	}
}

// A file exported before a right existed still reads, and reads correctly.
//
// This is why the permission columns are matched by heading rather than by
// position. By position, a file one column short would shift every right after the
// gap onto its neighbour - so every role in it would come back granting something
// adjacent to what it was meant to.
func TestRolesReadByHeadingNotByPosition(t *testing.T) {
	t.Parallel()

	older := []string{"users:read", "projects:read"}

	written, err := spreadsheet.WriteRoles("", older, []spreadsheet.RoleRow{
		{Name: "reader", Granted: []string{"projects:read"}},
	})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	// The application has gained a right since, and it sits between the two.
	current := []string{"users:read", "users:write", "projects:read"}

	rows, problems, err := spreadsheet.ReadRoles(bytes.NewReader(written), current)
	if err != nil {
		t.Fatalf("reading an older export: %v", err)
	}

	if len(problems) != 0 || len(rows) != 1 {
		t.Fatalf("read %d row(s) with problems %v", len(rows), problems)
	}

	if got := strings.Join(rows[0].Granted, ","); got != "projects:read" {
		t.Errorf("the older export came back granting %q, want projects:read", got)
	}
}

// A heading naming something this application does not enforce stops the file.
//
// It means the file came from another installation, or somebody typed a column in
// by hand. Reading it anyway would quietly ignore a whole column of intent.
func TestRolesRefuseAnUnknownPermissionColumn(t *testing.T) {
	t.Parallel()

	written, err := spreadsheet.WriteRoles("", []string{"users:read", "moons:harvest"},
		[]spreadsheet.RoleRow{{Name: "odd", Granted: []string{"moons:harvest"}}})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}

	_, _, err = spreadsheet.ReadRoles(bytes.NewReader(written), []string{"users:read"})
	if err == nil {
		t.Fatal("a column naming an unknown permission was accepted")
	}

	if !strings.Contains(err.Error(), "moons:harvest") {
		t.Errorf("the refusal does not name the column: %v", err)
	}
}
