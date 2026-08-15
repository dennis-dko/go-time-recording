// Package announce tells every open browser something, at once.
//
// Everything else this application says to a browser is an answer to a question
// the browser asked. That is the right shape for almost all of it - and it is the
// wrong shape for exactly one thing: the binary underneath is being replaced, and
// the people using it have a minute's notice at most.
//
// A poll cannot carry that. The permission notice polls once a minute, which is
// fine for what it is - the server enforces the change immediately whatever the
// interface believes, so the only cost of being late is a stale button. An update
// is not like that. Being told forty seconds after the restart began is being
// told nothing.
//
// So this is the other direction: a connection the browser opens and leaves open,
// and a line written down it when there is something to say.
package announce

import (
	"sync"
	"time"
)

// Kind is what happened. Small and closed on purpose: this is a channel for
// things the whole installation needs to know at the moment they happen, and
// keeping the list short is what stops it becoming a second, worse API.
type Kind string

const (
	// Installing: a new version is being downloaded and checked. The application
	// is working normally and will go on working normally - nothing about this
	// phase interrupts anybody. It is said out loud because what follows it does.
	Installing Kind = "update.installing"

	// Restarting: the new version is in place and this process is about to be
	// replaced by it. Seconds, not minutes, and requests in flight will fail.
	Restarting Kind = "update.restarting"

	// Pending: the new version is in place and cannot take effect until somebody
	// restarts the application by hand. Windows, where a process cannot replace
	// itself. Nothing is interrupted, and nothing changes until that happens.
	Pending Kind = "update.pending"

	// Cancelled: an update that was announced did not happen after all - the
	// download failed its checks, or it would not run. Said because the banner
	// raised by Installing would otherwise stay up for ever, promising a restart
	// that is not coming.
	Cancelled Kind = "update.cancelled"
)

// Announcement is one thing worth interrupting somebody for.
type Announcement struct {
	Kind Kind `json:"kind"`

	// Version being installed, where there is one. Shown, so nobody has to take
	// the application's word for what is happening to it.
	Version string `json:"version,omitempty"`

	// At is when it was said, so a browser that has been away can tell an
	// announcement it already acted on from a new one.
	At time.Time `json:"at"`
}

// Hub holds the open connections and writes to all of them.
//
// Safe for concurrent use: announcements come from a request handler and
// subscriptions from every other request handler.
type Hub struct {
	mu sync.Mutex

	// subscribers, by a number that only has to be unique.
	subscribers map[int]chan Announcement
	next        int

	// last is what was said most recently, handed to anybody who connects
	// afterwards. A browser that opened its connection one second after the
	// announcement would otherwise never hear it - and reconnecting is exactly
	// what every browser does when the restart drops the connection.
	last *Announcement
}

// New creates an empty hub.
func New() *Hub {
	return &Hub{subscribers: map[int]chan Announcement{}}
}

// Subscribe opens a stream. The returned function closes it and must be called.
func (h *Hub) Subscribe() (<-chan Announcement, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.next++
	id := h.next

	// Buffered, so Publish never waits on a browser. Four is generous for a
	// channel that carries one kind of message a few times a year.
	stream := make(chan Announcement, 4)

	if h.last != nil {
		stream <- *h.last
	}

	h.subscribers[id] = stream

	return stream, func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		if existing, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(existing)
		}
	}
}

// Publish says something to everybody connected, and remembers it for whoever
// connects next.
//
// Never blocks. A browser whose buffer is full is one that has stopped reading -
// a laptop that was shut, a connection that died without saying so - and holding
// up an update for it would be holding up the update for everybody.
func (h *Hub) Publish(kind Kind, version string) {
	announcement := Announcement{Kind: kind, Version: version, At: time.Now().UTC()}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.last = &announcement

	for _, stream := range h.subscribers {
		select {
		case stream <- announcement:
		default:
		}
	}
}

// Last is the most recent announcement, if there has been one.
func (h *Hub) Last() (Announcement, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.last == nil {
		return Announcement{}, false
	}

	return *h.last, true
}

// Forget drops the remembered announcement.
//
// Called when an update finishes or is abandoned, so a browser connecting an
// hour later is not handed a restart notice about something that is long over.
func (h *Hub) Forget() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.last = nil
}

// Subscribers is how many connections are open. For tests and for the operations
// log; nothing in the application behaves differently by it.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.subscribers)
}
