package announce

import (
	"testing"
	"time"
)

// Everybody connected hears it, not just the first one.
func TestAnAnnouncementReachesEverySubscriber(t *testing.T) {
	hub := New()

	first, closeFirst := hub.Subscribe()
	defer closeFirst()

	second, closeSecond := hub.Subscribe()
	defer closeSecond()

	hub.Publish(Installing, "v1.2.3")

	for name, stream := range map[string]<-chan Announcement{
		"the first": first, "the second": second,
	} {
		select {
		case got := <-stream:
			if got.Kind != Installing || got.Version != "v1.2.3" {
				t.Errorf("%s connection heard %+v", name, got)
			}
		case <-time.After(time.Second):
			t.Errorf("%s connection heard nothing", name)
		}
	}
}

// Somebody who connects a moment too late is still told.
//
// This is the ordinary case rather than an edge one. The restart drops every
// connection there is, and every browser reconnects a second or two later - so
// the announcement that matters most is the one made just before nobody was
// listening.
func TestConnectingAfterwardsStillHearsTheLastThing(t *testing.T) {
	hub := New()

	hub.Publish(Restarting, "v1.2.3")

	stream, done := hub.Subscribe()
	defer done()

	select {
	case got := <-stream:
		if got.Kind != Restarting {
			t.Errorf("a connection made afterwards heard %+v", got)
		}
	case <-time.After(time.Second):
		t.Error("a connection made after the announcement heard nothing at all")
	}
}

// And once it is over, it stops being told to newcomers.
func TestWhatIsForgottenIsNotRepeated(t *testing.T) {
	hub := New()

	hub.Publish(Cancelled, "v1.2.3")
	hub.Forget()

	stream, done := hub.Subscribe()
	defer done()

	select {
	case got := <-stream:
		t.Errorf("a fresh connection was handed %+v, which is over", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// A browser that has stopped reading does not hold up the update.
//
// The failure this prevents: a laptop shut mid-afternoon leaves a connection
// that accepts nothing, and a publisher that waits for it waits for ever - with
// the update, and everybody else's warning, behind it.
func TestASubscriberThatStoppedReadingIsNotWaitedFor(t *testing.T) {
	hub := New()

	stuck, done := hub.Subscribe()
	defer done()

	// Past the buffer, several times over.
	finished := make(chan struct{})

	go func() {
		for range 50 {
			hub.Publish(Installing, "v1.2.3")
		}

		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("publishing blocked on a connection that had stopped reading")
	}

	// It kept what it could hold rather than being dropped: the connection may
	// come back, and the last thing said is what it needs.
	if len(stuck) == 0 {
		t.Error("the stuck connection was left with nothing at all")
	}
}

// Closing a stream takes it out, so nothing is written to it afterwards.
func TestClosingAStreamRemovesIt(t *testing.T) {
	hub := New()

	_, done := hub.Subscribe()

	if got := hub.Subscribers(); got != 1 {
		t.Fatalf("%d connection(s) after subscribing, want 1", got)
	}

	done()

	if got := hub.Subscribers(); got != 0 {
		t.Errorf("%d connection(s) after closing, want 0", got)
	}

	// Twice is not a panic. The stream handler closes on the way out of a
	// request, and a request can end more than one way.
	done()
}
