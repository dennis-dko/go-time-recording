package service_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/service"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// definition finds a declaration of the visibility rule, whatever it is called.
var definition = regexp.MustCompile(`func [Rr]equireVisible\(`)

// The rule that keeps a private project secret is one rule, so it has to have
// one implementation.
//
// It was written out twice - once here and once in the application service -
// with the second copy's comment noting that it took "the same reading". Two
// copies of a sentence are not the same as one rule: widening this for an
// auditor role who may see everything would leave the other copy quietly
// refusing, and no test would have failed, because each package exercised the
// copy it could see.
//
// Read out of the source rather than asserted about behaviour, because
// behaviour is exactly what cannot tell two identical copies apart.
func TestTheVisibilityRuleHasOneImplementation(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	found := []string{}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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

		if definition.Match(source) {
			found = append(found, filepath.ToSlash(path))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking the source: %v", err)
	}

	if len(found) == 0 {
		t.Fatal("no definition of the visibility rule found; this test is no longer reading the source")
	}

	if len(found) > 1 {
		t.Errorf("the visibility rule is implemented %d times, and one of them will be the one "+
			"somebody forgets to change: %v", len(found), found)
	}
}

// A viewer of zero is an installation running without authentication, which
// sees everything rather than nothing.
func TestNobodyInParticularSeesEverything(t *testing.T) {
	t.Parallel()

	owner := uint(7)
	project := &model.Project{ID: 3, OwnerID: &owner}

	if err := service.RequireVisible(project, 0); err != nil {
		t.Errorf("an unauthenticated installation was refused its own data: %v", err)
	}
}

// The owner sees their own category.
func TestTheOwnerSeesTheirOwnProject(t *testing.T) {
	t.Parallel()

	owner := uint(7)
	project := &model.Project{ID: 3, OwnerID: &owner}

	if err := service.RequireVisible(project, 7); err != nil {
		t.Errorf("the owner was refused their own project: %v", err)
	}
}

// And anybody else is told it is not there, rather than that they may not look
// at it - the difference between the two answers is the project's existence.
func TestSomebodyElsesProjectIsNotFoundRatherThanForbidden(t *testing.T) {
	t.Parallel()

	owner := uint(7)
	project := &model.Project{ID: 3, OwnerID: &owner}

	err := service.RequireVisible(project, 8)
	if err == nil {
		t.Fatal("a private project was shown to somebody it does not belong to")
	}

	if kind := apperror.KindOf(err); kind != apperror.KindNotFound {
		t.Errorf("a private project's existence was revealed by the status: want a not-found, got %v", kind)
	}
}
