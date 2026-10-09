package imageupdate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The files the two sides pass between them. Named here and in
// deploy/update/updater.sh, which is the other half of the protocol.
const (
	requestFile = "request"
	runningFile = "running"
	resultFile  = "result"

	// aliveFile is rewritten by the updater on every round of its loop, and
	// holds the seconds between rounds.
	aliveFile = "alive"
)

// defaultRound is the updater's round when its sign of life does not say: the
// script's own default for GTR_UPDATE_POLL.
const defaultRound = 3 * time.Second

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

	// ResultFailed: the pull or the recreate did not work. The updater's own
	// words follow it.
	ResultFailed = "failed:"
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

// Available reports whether an updater is part of this deployment and is there
// to read a request.
//
// Asked of the directory rather than of a setting: the volume is mounted by the
// same overlay that starts the updater, so a directory that is there and
// writable is the channel to one. That it is listening is a second question,
// because the volume outlives the container: an updater stopped, crashed or
// taken out of the deployment leaves the directory mounted and writable, and a
// button offered over it wrote a request nobody read. So the updater says it is
// there - see heard - and where it has stopped saying so, the card names the
// command instead. Checked on every call rather than once at start-up, because
// the overlay can be added to a deployment without rebuilding anything, and an
// application that decided this once would go on denying it until somebody
// restarted the very thing they had just enabled.
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

	return u.heard()
}

// heard reports whether the updater has said recently enough that it is there.
//
// It rewrites the alive file on every round of its loop, naming the seconds
// between rounds, and is taken for there while that file is younger than three
// rounds and ten seconds more: one round lost to a busy machine is not a stopped
// updater, three in a row are. The round comes from the file rather than from a
// number here, because GTR_UPDATE_POLL sets it and a fixed window would take an
// updater on a one-minute round for stopped between two of its rounds. An update
// under way is no round at all - a pull and a recreate can take minutes - so it
// counts as being there too.
func (u *Updater) heard() bool {
	if u.Running() {
		return true
	}

	path := filepath.Join(u.dir, aliveFile)

	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	round := defaultRound

	if raw, err := os.ReadFile(path); err == nil {
		if seconds, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && seconds > 0 {
			round = time.Duration(seconds) * time.Second
		}
	}

	return time.Since(info.ModTime()) <= 3*round+10*time.Second
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
// twice gets the same answer twice instead of the first reader taking it. Ask
// clears it, and the updater clears it again when it takes the request.
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

	// The last outcome goes before the request is placed rather than when the
	// updater takes it, up to three seconds later: whoever waits for this
	// request's answer starts reading now, and would read the previous one's.
	err := os.Remove(filepath.Join(u.dir, resultFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clearing the last update's outcome: %w", err)
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
