// Package imageupdate asks something else to replace this container.
//
// A container deployment updates by pulling a new image and recreating the
// container from it. This process cannot do that: pulling and recreating need
// the Docker socket, and holding that socket is root on the host - not root in
// a container, root on the machine, because anything with it can start a
// container that mounts the host's filesystem. An application with a sign-in
// form is the last thing that should hold it.
//
// So the deployment can be given a second container that holds it instead,
// whose entire vocabulary is one sentence, and this is how that sentence is
// said: a file appears in a directory both can see. There is no argument to it.
// This side cannot name an image, cannot name a container and cannot pass a
// flag - it writes an empty file, and the updater does the one thing it does.
//
// See deploy/compose.update.yaml, which is where the privilege is granted and
// where the reasoning is written for the person granting it.
//
// Absent by default. Without the overlay there is no directory, this reports
// itself unavailable, and the version card offers what it offered before: the
// binary swapped inside the running container, which lasts until the container
// is recreated.
package imageupdate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The three files the two sides pass between them. Named here and in
// deploy/update/updater.sh, which is the other half of the protocol.
const (
	requestFile = "request"
	runningFile = "running"
	resultFile  = "result"
)

// Outcomes the updater reports. Anything else is a failure and carries its own
// words.
const (
	// ResultDone: a new image was pulled and the container recreated from it.
	//
	// Rarely read by the process that asked. A successful update replaces the
	// container that would have read it; the browser finds out by watching the
	// version come back different, the way it does after any restart.
	ResultDone = "ok"

	// ResultNothing: the registry had nothing newer than what is running.
	ResultNothing = "none"
)

// ErrUnavailable is returned where no updater is part of this deployment.
var ErrUnavailable = errors.New("no image updater is available")

// ErrBusy is returned where one is already running.
var ErrBusy = errors.New("an update is already running")

// Updater talks to the container that holds the socket.
type Updater struct {
	// dir is the shared directory. Empty where the overlay is not deployed.
	dir string
}

// New reads where the requests go, from the environment.
//
// The variable is the directory rather than a flag beside it: the flag and the
// channel are then the same fact and cannot come to disagree - an installation
// that says an updater is present but cannot write to it would report itself
// ready and fail at the press.
func New(dir string) *Updater {
	return &Updater{dir: strings.TrimSpace(dir)}
}

// Available reports whether an updater is part of this deployment.
//
// Asked of the directory rather than of a setting: the volume is mounted by the
// same overlay that starts the updater, so a directory that is there and
// writable is an updater that is there. Checked on every call rather than once
// at start-up, because the overlay can be added to a deployment without
// rebuilding anything, and an application that decided this once would go on
// denying it until somebody restarted the very thing they had just enabled.
func (u *Updater) Available() bool {
	if u.dir == "" {
		return false
	}

	info, err := os.Stat(u.dir)
	if err != nil || !info.IsDir() {
		return false
	}

	// Writable, which is the part that matters: a directory this cannot write
	// into is an updater that will never hear anything.
	probe := filepath.Join(u.dir, ".writable")

	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return false
	}

	_ = os.Remove(probe)

	return true
}

// Running reports whether an update is under way.
func (u *Updater) Running() bool {
	if u.dir == "" {
		return false
	}

	_, err := os.Stat(filepath.Join(u.dir, runningFile))

	return err == nil
}

// Result is what the last update came to, and whether there is one to read.
//
// Cleared by the next request rather than by reading, so a screen that asks
// twice gets the same answer twice instead of the first reader taking it.
func (u *Updater) Result() (string, bool) {
	if u.dir == "" {
		return "", false
	}

	raw, err := os.ReadFile(filepath.Join(u.dir, resultFile))
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(raw)), true
}

// Ask leaves the request. It does not wait: what happens next is that this
// container stops existing.
func (u *Updater) Ask() error {
	if !u.Available() {
		return ErrUnavailable
	}

	if u.Running() {
		return ErrBusy
	}

	// Written whole and moved into place. The updater polls for this file, and a
	// file that is being created is a file it can find half-written - which for
	// an empty request would not matter, and is the sort of thing that stops
	// being true the first time somebody puts something in it.
	staging := filepath.Join(u.dir, requestFile+".tmp")

	if err := os.WriteFile(staging, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing the update request: %w", err)
	}

	if err := os.Rename(staging, filepath.Join(u.dir, requestFile)); err != nil {
		return fmt.Errorf("placing the update request: %w", err)
	}

	return nil
}
