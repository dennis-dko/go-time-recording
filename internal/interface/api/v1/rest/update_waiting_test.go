package rest

import (
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
)

// A press for a version already downloaded is told so, and everybody else goes on
// being told what is true.
//
// Apply announces an install before it asks for one, and the install then turns
// out to be one that had already happened. Answered like any other failure, the
// announcement was withdrawn as cancelled - on a screen where the version is in
// fact in place and waiting for the restart.
func TestAPressForAVersionAlreadyWaitingSaysSo(t *testing.T) {
	hub := announce.New()
	h := &UpdateHandler{hub: hub}

	hub.Publish(announce.Installing, "v9.9.9")

	err := h.afterAFailedInstall(selfupdate.ErrAlreadyInstalled, "v9.9.9")

	conflict, ok := err.(conflictError)
	if !ok {
		t.Fatalf("answered %T (%v), want a conflict", err, err)
	}

	if code := conflict.Response()["code"]; code != "updateAlreadyInstalled" {
		t.Errorf("answered with code %v", code)
	}

	last, standing := hub.Last()
	if !standing || last.Kind != announce.Pending || last.Version != "v9.9.9" {
		t.Errorf("the notice standing is %+v (%v), want v9.9.9 waiting for a restart",
			last, standing)
	}
}
