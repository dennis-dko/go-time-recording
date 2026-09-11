package rest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/announce"
	"github.com/dennis-dko/go-time-recording/internal/infrastructure/imageupdate"
)

// An image update that changed nothing takes back the restart it announced.
//
// The press announces a restart to every open screen, because a successful
// update replaces this container and the people using it need the notice. The
// updater answers "none" when the registry holds nothing newer, and in that case
// this container goes on running - but nothing read the answer, so the
// announcement stood. Every screen said the application was restarting, the hub
// handed that to every screen that connected afterwards, and the error toasts a
// restart suppresses stayed suppressed, until the process restarted for some
// other reason.
func TestAnImageUpdateThatFoundNothingNewerTakesTheRestartBack(t *testing.T) {
	h, hub, dir := imageUpdateUnderTest(t)

	stream, stop := hub.Subscribe()
	defer stop()

	if _, err := h.askForTheImage(requestContext(), "v9.9.9"); err != nil {
		t.Fatalf("asking for the image: %v", err)
	}

	answerAsTheUpdater(t, dir, imageupdate.ResultNothing)
	awaitAnnouncement(t, stream, announce.Cancelled)

	if last, standing := hub.Last(); standing {
		t.Errorf("%q is still handed to every screen that connects, after an update "+
			"that changed nothing", last.Kind)
	}
}

// A failed one is taken back as well, and the card is told what went wrong.
//
// deploy/OPERATIONS.md already promised the second half - "the card says what
// went wrong" - while nothing read the updater's words at all.
func TestAFailedImageUpdateIsTakenBackAndTheCardSaysWhy(t *testing.T) {
	h, hub, dir := imageUpdateUnderTest(t)

	stream, stop := hub.Subscribe()
	defer stop()

	c := requestContext()

	if _, err := h.askForTheImage(c, "v9.9.9"); err != nil {
		t.Fatalf("asking for the image: %v", err)
	}

	answerAsTheUpdater(t, dir, "failed: pull access denied for ghcr.io/example")
	awaitAnnouncement(t, stream, announce.Cancelled)

	raw, err := json.Marshal(h.describe(c))
	if err != nil {
		t.Fatalf("encoding the card: %v", err)
	}

	var card map[string]any
	if err := json.Unmarshal(raw, &card); err != nil {
		t.Fatalf("decoding the card: %v", err)
	}

	if card["imageOutcome"] != "failed" {
		t.Errorf("the card is told %v about the last image update, want \"failed\"",
			card["imageOutcome"])
	}

	if card["imageProblem"] != "pull access denied for ghcr.io/example" {
		t.Errorf("the card is given %v as the reason, want the updater's own words",
			card["imageProblem"])
	}
}

// A second press before the updater has answered the first is turned away.
//
// Between the request being written and the updater taking it there are up to
// three seconds in which nothing on disk says an update is under way, so a
// second press was accepted - and a second press is a second request and a
// second wait for the same answer.
func TestASecondPressBeforeTheUpdaterAnswersIsTurnedAway(t *testing.T) {
	h, hub, dir := imageUpdateUnderTest(t)

	stream, stop := hub.Subscribe()
	defer stop()

	c := requestContext()

	if _, err := h.askForTheImage(c, "v9.9.9"); err != nil {
		t.Fatalf("the first press: %v", err)
	}

	if _, err := h.askForTheImage(c, "v9.9.9"); err == nil ||
		!strings.Contains(err.Error(), "already running") {
		t.Errorf("a second press before the updater answered came back %v, want it "+
			"turned away as already running", err)
	}

	// The first press is still waiting; answer it, so its wait ends with the case.
	answerAsTheUpdater(t, dir, imageupdate.ResultNothing)
	awaitAnnouncement(t, stream, announce.Cancelled)
}

// A press while the updater is at work leaves the restart it announced standing.
//
// The binary path learnt this in afterAFailedInstall: a second press means the
// first one's update is still running and its restart is still coming, so the
// press that got there second has nothing to take back. The image path took it
// back anyway - it announced an install, heard "busy" and then announced that
// nothing would happen, while the updater went on to replace the container
// underneath every screen that had just been told to stop waiting.
func TestAPressWhileTheUpdaterWorksLeavesTheRestartStanding(t *testing.T) {
	h, hub, dir := imageUpdateUnderTest(t)

	// What the updater writes while it works, and what the first press said.
	if err := os.WriteFile(filepath.Join(dir, "running"), nil, 0o600); err != nil {
		t.Fatalf("cannot mark the updater as working: %v", err)
	}

	hub.Publish(announce.Restarting, "v9.9.9")

	if _, err := h.askForTheImage(requestContext(), "v9.9.9"); err == nil {
		t.Error("a press while the updater works was accepted")
	}

	last, standing := hub.Last()
	if !standing {
		t.Fatal("the second press took the restart back: every screen now expects " +
			"nothing, and the updater replaces the container anyway")
	}

	if last.Kind != announce.Restarting {
		t.Errorf("the notice now says %q, want %q - the first press's update is still "+
			"running", last.Kind, announce.Restarting)
	}
}

// imageUpdateUnderTest is a handler whose image updater is a directory the case
// can answer in, and the hub it announces on.
func imageUpdateUnderTest(t *testing.T) (*UpdateHandler, *announce.Hub, string) {
	t.Helper()

	dir := t.TempDir()
	hub := announce.New()

	return &UpdateHandler{hub: hub, images: imageupdate.New(dir)}, hub, dir
}

// requestContext is a gofr.Context carrying the one thing these handlers use
// from it outside a request's own data: a logger.
func requestContext() *gofr.Context {
	return &gofr.Context{
		Context:   context.Background(),
		Container: &container.Container{Logger: logging.NewMockLogger(logging.ERROR)},
	}
}

// answerAsTheUpdater does what deploy/update/updater.sh does with a request, in
// the order it does it: takes the request and the last outcome away, marks
// itself working, and clears that mark before the new outcome can be read.
func answerAsTheUpdater(t *testing.T, dir, outcome string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(dir, "request")); err != nil {
		t.Fatalf("there is no request to answer: %v", err)
	}

	for _, name := range []string{"request", "result"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("taking %s: %v", name, err)
		}
	}

	running := filepath.Join(dir, "running")

	if err := os.WriteFile(running, nil, 0o600); err != nil {
		t.Fatalf("marking the updater as working: %v", err)
	}

	if err := os.Remove(running); err != nil {
		t.Fatalf("clearing the working mark: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "result"), []byte(outcome), 0o600); err != nil {
		t.Fatalf("writing the outcome: %v", err)
	}
}

// awaitAnnouncement reads the stream until the wanted announcement arrives, and
// fails the case when it has not after a few of the watch's looks.
func awaitAnnouncement(t *testing.T, stream <-chan announce.Announcement, want announce.Kind) {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for {
		select {
		case got, open := <-stream:
			if !open {
				t.Fatalf("the stream closed before %q arrived", want)
			}

			if got.Kind == want {
				return
			}
		case <-deadline:
			t.Fatalf("no %q within five seconds: the restart stays announced to every "+
				"screen", want)
		}
	}
}
