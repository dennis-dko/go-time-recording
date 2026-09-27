package service

import (
	"context"
	"strconv"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// RequireVisible refuses a project the viewer is not allowed to know about.
//
// A not-found rather than a refusal, so a private project's existence is not
// revealed by the difference between the two status codes.
//
// A viewer id of zero means authentication is switched off, which is the local
// trial case and sees everything.
//
// Exported, and in a file of its own, because the application layer decides the
// same thing about the same projects. It had its own copy, word for word, whose
// comment observed that it took "the same reading" as this one - which is true
// and is the problem: a rule agreed upon in two places is a rule that holds
// until somebody widens one of them. There is one of it now, so widening it is
// one edit and every caller follows.
func RequireVisible(project *model.Project, viewerID uint) error {
	if viewerID == 0 || project.VisibleTo(viewerID) {
		return nil
	}

	return apperror.NotFound("project", strconv.FormatUint(uint64(project.ID), 10))
}

// RequireVisible refuses a scope that names a project the viewer may not see.
//
// An evaluation of somebody's own time is filtered to their own entries, so a
// foreign project in its scope totals nothing of anybody else's - and was let
// through for that reason by the statistics while the report beside them refused
// it. The rule is about the id rather than the total: it is something the caller
// supplies, and both evaluations answer it the same way through this one check.
//
// A scope naming no project - every project, or none - passes.
func (s ProjectScope) RequireVisible(
	ctx context.Context, projects repository.ProjectRepository, viewerID uint,
) error {
	if s.ProjectID == 0 {
		return nil
	}

	project, err := projects.GetByID(ctx, s.ProjectID)
	if err != nil {
		return err
	}

	return RequireVisible(project, viewerID)
}
