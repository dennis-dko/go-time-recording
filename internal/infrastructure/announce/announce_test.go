package announce

import (
	"slices"
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

// An update taken back is said to whoever is connected and kept for nobody.
//
// What a retraction takes back is the thing that was being remembered, so it
// leaves nothing to hand to a screen that connects afterwards - and it leaves
// nothing in one step. It was two once, Publish and then Forget, and between
// them the hub remembered the retraction itself: a screen connecting in that
// moment was told that an update it had never heard of had not been installed.
func TestAnUpdateTakenBackIsRememberedForNobody(t *testing.T) {
	hub := New()

	watching, stop := hub.Subscribe()
	defer stop()

	hub.Publish(Restarting, "v1.2.3")
	hub.Publish(Cancelled, "v1.2.3")

	for _, want := range []Kind{Restarting, Cancelled} {
		select {
		case got := <-watching:
			if got.Kind != want {
				t.Errorf("the connection that was open heard %q, want %q", got.Kind, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("the connection that was open never heard %q", want)
		}
	}

	if last, standing := hub.Last(); standing {
		t.Errorf("%q is still remembered once the update has been taken back", last.Kind)
	}

	// Subscribe writes what is remembered before it returns, so there is nothing
	// to wait for: the stream either holds it by now or never will.
	late, done := hub.Subscribe()
	defer done()

	select {
	case got := <-late:
		t.Errorf("a connection made afterwards was handed %+v, which is over", got)
	default:
	}
}

// What is said about maintenance does not displace what is said about an update.
//
// The hub remembered one thing, the last, whatever it was about. Saving the
// maintenance card while an image was being pulled therefore replaced "the
// application is restarting" with a notice that carries nothing, and a screen
// that connected - or reconnected - in the minutes the pull still had to run
// was neither warned of the restart nor reloaded once the new version answered:
// a screen reloads when the last update notice it heard was the restart.
func TestWhatIsSaidAboutOneThingDoesNotDisplaceTheOther(t *testing.T) {
	for name, c := range map[string]struct {
		said []Kind
		want []Kind
	}{
		"an update, then maintenance": {
			said: []Kind{Restarting, Maintenance},
			want: []Kind{Restarting, Maintenance},
		},
		"maintenance, then an update": {
			said: []Kind{Maintenance, Installing},
			want: []Kind{Maintenance, Installing},
		},
		"maintenance saved a second time": {
			said: []Kind{Maintenance, Restarting, Maintenance},
			want: []Kind{Restarting, Maintenance},
		},
		"an update moving on": {
			said: []Kind{Installing, Maintenance, Restarting},
			want: []Kind{Maintenance, Restarting},
		},
		"an update taken back": {
			said: []Kind{Installing, Maintenance, Cancelled},
			want: []Kind{Maintenance},
		},
	} {
		t.Run(name, func(t *testing.T) {
			hub := New()

			for _, kind := range c.said {
				hub.Publish(kind, "")
			}

			late, done := hub.Subscribe()
			defer done()

			// Subscribe writes what is remembered before it returns.
			var heard []Kind

			for range len(late) {
				heard = append(heard, (<-late).Kind)
			}

			if !slices.Equal(heard, c.want) {
				t.Errorf("a connection made afterwards heard %q, want %q", heard, c.want)
			}
		})
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

// Shutting down ends every stream rather than leaving them to be waited for.
//
// The property that makes this worth its own test: the connections this package
// hands out are the only ones in the application that never end by themselves.
// An HTTP server draining before it exits waits for exactly that, so without this
// a shutdown sits out its whole timeout on connections that are behaving as
// intended - and on the restart path that time is added to the time the
// application is down.
func TestShuttingDownEndsEveryStream(t *testing.T) {
	hub := New()

	first, closeFirst := hub.Subscribe()
	defer closeFirst()

	second, closeSecond := hub.Subscribe()
	defer closeSecond()

	hub.Close()

	for name, stream := range map[string]<-chan Announcement{
		"the first": first, "the second": second,
	} {
		select {
		case _, open := <-stream:
			if open {
				t.Errorf("%s stream carried something instead of ending", name)
			}
		case <-time.After(time.Second):
			t.Errorf("%s stream was left open by the shutdown", name)
		}
	}

	// And a request that arrives during the shutdown is not handed a stream that
	// would have to be waited for either.
	late, done := hub.Subscribe()
	defer done()

	select {
	case _, open := <-late:
		if open {
			t.Error("a stream opened during the shutdown is live")
		}
	case <-time.After(time.Second):
		t.Error("a stream opened during the shutdown would be waited for")
	}

	// Twice is not a panic: the signal handler and a test can both arrive here.
	hub.Close()
}
