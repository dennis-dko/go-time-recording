package rest

import (
	"testing"
	"time"
)

// Asking by hand exists because the automatic answer can be six hours old.
//
// Somebody who has just published a release opens the card and is told the
// version before it is the newest one, which is true of what the feed said this
// morning and no longer true of the feed. There was no way to ask again: the
// answer is reused while it is fresh, and nothing bypassed that.
//
// The button that does bypass it needs a limit of its own, or it hands back the
// problem the cache was built to solve - sixty unauthenticated requests an hour
// per address is all GitHub allows, and a card with a button on it can be
// pressed by every administrator looking at it.
func TestAManualCheckMayBypassTheCacheButNotTheRateLimit(t *testing.T) {
	t.Run("the first ask goes through", func(t *testing.T) {
		h := &UpdateHandler{}

		if _, ok := h.mayCheckNow(); !ok {
			t.Error("an installation that has never been asked by hand refuses the first ask")
		}
	})

	t.Run("asking again straight away is refused, and says for how long", func(t *testing.T) {
		h := &UpdateHandler{checkedManuallyAt: time.Now()}

		wait, ok := h.mayCheckNow()
		if ok {
			t.Fatal("the feed can be asked as fast as the button can be pressed")
		}

		if wait <= 0 || wait > manualCheckEvery {
			t.Errorf("the wait has to be a usable number of seconds, got %v", wait)
		}
	})

	t.Run("the wait ends", func(t *testing.T) {
		h := &UpdateHandler{checkedManuallyAt: time.Now().Add(-manualCheckEvery - time.Second)}

		if _, ok := h.mayCheckNow(); !ok {
			t.Error("the limit never lifts, so the button never works again")
		}
	})

	// The whole point: a manual ask is not turned away by the six-hour answer it
	// is trying to get past.
	t.Run("a fresh cached answer does not block asking by hand", func(t *testing.T) {
		h := &UpdateHandler{checkedManuallyAt: time.Time{}}

		if _, ok := h.mayCheckNow(); !ok {
			t.Error("the manual ask is gated on the automatic cache, which makes it pointless")
		}
	})
}
