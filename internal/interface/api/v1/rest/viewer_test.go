package rest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// viewerDefinition finds a declaration of "whose eyes is this read through".
var viewerDefinition = regexp.MustCompile(`func \([^)]*\) viewerID\(`)

// One question, asked on nearly every read in this package, so it has one
// answer.
//
// It was a method on three separate handlers, all with the same name and the
// same signature, and the three had already drifted: one of them guarded
// against a nil principal and two dereferenced it. Nothing had gone wrong yet,
// because every call site follows a successful authorization - but that is a
// property of the callers, not of the copies, and it is not a property anybody
// checks when adding the fourth.
func TestTheViewerRuleHasOneImplementation(t *testing.T) {
	t.Parallel()

	found := []string{}

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		if viewerDefinition.Match(source) {
			found = append(found, filepath.ToSlash(path))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	if len(found) == 0 {
		t.Fatal("no definition of the viewer rule found; this test is no longer reading the source")
	}

	if len(found) > 1 {
		t.Errorf("the viewer rule is implemented %d times: %v", len(found), found)
	}
}

// The guard the surviving copy has to keep.
//
// Two of the three dereferenced the principal without looking, so a caller that
// had not established one would have taken the process down rather than reading
// nothing. Unreachable through today's handlers and cheap to be sure of.
func TestAViewerWithoutAPrincipalIsNobody(t *testing.T) {
	t.Parallel()

	authz := NewAuthorizer(nil, true)

	if id := authz.viewerID(nil); id != 0 {
		t.Errorf("a request with no principal was read as somebody: got %d", id)
	}

	if id := authz.viewerID(&service.Principal{}); id != 0 {
		t.Errorf("a principal with no user was read as somebody: got %d", id)
	}
}

// With enforcement off there is nobody to be, and zero is what the services
// read as "no narrowing" - a local trial sees everything.
func TestWithoutEnforcementTheViewerIsNobody(t *testing.T) {
	t.Parallel()

	authz := NewAuthorizer(nil, false)
	principal := &service.Principal{User: &model.User{ID: 9}}

	if id := authz.viewerID(principal); id != 0 {
		t.Errorf("an unauthenticated installation was narrowed to a user: got %d", id)
	}
}

// And with enforcement on, it is whoever is asking.
func TestTheViewerIsWhoeverIsAsking(t *testing.T) {
	t.Parallel()

	authz := NewAuthorizer(nil, true)
	principal := &service.Principal{User: &model.User{ID: 9}}

	if id := authz.viewerID(principal); id != 9 {
		t.Errorf("the caller was not read as themselves: got %d", id)
	}
}
