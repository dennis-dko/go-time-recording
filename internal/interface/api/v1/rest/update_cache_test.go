package rest

import (
	"errors"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/selfupdate"
)

// The release feed is asked rarely, and a refusal does not erase what it said.
//
// It used to be asked at most once a minute, which is sixty times an hour - and
// GitHub allows an unauthenticated caller sixty an hour per address. Every
// administrator's sign-in starts a check and every open tab repeats it hourly,
// all from one address, so an installation with a few administrators spent its
// allowance and got a 403 that reads on screen as a permission problem.
//
// The two rules that follow from that are tested here rather than through the
// handler, because both are about time passing and neither is about HTTP.
func TestTheReleaseAnswerIsKeptAndTheFeedIsNotHammered(t *testing.T) {
	known := selfupdate.Release{Version: "v1.2.3"}

	t.Run("a fresh answer is reused", func(t *testing.T) {
		h := &UpdateHandler{cached: known, cachedAt: time.Now()}

		if !h.hasFreshAnswer() {
			t.Error("an answer taken moments ago is asked for again")
		}
	})

	t.Run("a stale answer is asked again", func(t *testing.T) {
		h := &UpdateHandler{cached: known, cachedAt: time.Now().Add(-updateCacheFor - time.Minute)}

		if h.hasFreshAnswer() {
			t.Error("an answer older than the cache is still being reused")
		}
	})

	t.Run("a refusal is waited out", func(t *testing.T) {
		h := &UpdateHandler{
			cached:   known,
			cachedAt: time.Now().Add(-updateCacheFor - time.Minute),
			cacheErr: errors.New("403"),
			failedAt: time.Now(),
		}

		if !h.waitingAfterFailure() {
			t.Error("a refusal is answered by asking again, which is how a rate " +
				"limit becomes a rate limit that never clears")
		}

		// And what it says meanwhile is still the version it knows.
		if h.cached.Version != known.Version {
			t.Error("the last good answer was thrown away with the failed attempt")
		}
	})

	t.Run("the wait ends", func(t *testing.T) {
		h := &UpdateHandler{
			cacheErr: errors.New("403"),
			failedAt: time.Now().Add(-updateRetryAfter - time.Minute),
		}

		if h.waitingAfterFailure() {
			t.Error("the feed is never asked again after one refusal")
		}
	})

	// Four requests a day rather than sixty an hour, however many people are
	// looking at the screen.
	if perDay := int(24 * time.Hour / updateCacheFor); perDay > 8 {
		t.Errorf("the feed would be asked %d times a day per instance", perDay)
	}
}
