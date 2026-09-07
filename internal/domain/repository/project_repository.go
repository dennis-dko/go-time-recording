// Package repository declares what the domain needs from storage, and nothing
// about how it is stored.
//
// The interfaces live here rather than beside their implementations because the
// dependency has to point inwards: internal/domain may import neither
// internal/infrastructure nor internal/interface, so the layer that owns the
// rules also owns the shape of the questions it asks. Two packages answer them
// - persistence/sqldb against a real database and persistence/memory for tests
// - and neither is visible from here, which is what lets a service be tested
// without either.
//
// Every method takes a context first and is expected to respect it. All of them
// do; one that does not is a request nobody can abort.
//
// One rule reaches past this package and is easy to miss from inside it: a new
// table with a foreign key to users has to be added to sqldb.PurgeUser in the
// same commit. An account and the hours recorded against it go together, and a
// missing entry does not fail loudly - on PostgreSQL and MySQL it makes the
// final delete impossible, and on SQLite, where foreign keys are not enforced
// unless asked for, it silently leaves rows pointing at an account that no
// longer exists. That has already happened once, with passkeys, which is why
// TestEveryTableReferencingAnAccountIsPurgedWithIt now reads the whole
// migration chain for both shapes.
package repository

import (
	"context"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// ProjectRepository repository functions for project
type ProjectRepository interface {
	Save(ctx context.Context, project *model.Project) (*model.Project, error)

	GetByID(ctx context.Context, id uint) (*model.Project, error)

	GetAll(ctx context.Context) ([]*model.Project, error)

	Update(ctx context.Context, project *model.Project) (*model.Project, error)

	Delete(ctx context.Context, id uint) error
}
