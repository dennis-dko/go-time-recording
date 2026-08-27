package service

import (
	"strconv"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
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
