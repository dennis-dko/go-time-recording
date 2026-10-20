package service

import (
	"sync"
	"time"
)

// pendingSweepInterval is how often the abandoned first halves are looked for,
// which is most of them: every sign-in somebody thinks better of leaves one.
//
// Not on every start, which is how it began: every start then visited every
// first half still waiting, so the cost of a start grew with the number started -
// measured, 158 microseconds a start with 10,000 waiting and 11.6 milliseconds
// with 60,000, and the start of a sign-in needs no session. At most once a
// minute, an abandoned one outlives its lifetime by a minute at most and the
// visit is paid once rather than by every start.
const pendingSweepInterval = time.Minute

// pending is the first half of a ceremony, waiting for its second.
type pending[T any] struct {
	data    T
	expires time.Time
}

// pendingStore keeps the first halves of ceremonies until their second half takes
// them or they expire - a passkey's challenge, a round trip through an OpenID
// provider. A map behind a lock, swept of what has expired at most once a
// pendingSweepInterval.
//
// In memory on purpose: a first half is worthless once its lifetime is over, and
// putting it in the database would mean writing a row for every sign-in attempt,
// the abandoned ones included. The cost is that a restart, or a second instance
// behind a load balancer, loses them - the person retries and it works.
//
// A plain map rather than a sync.Map, because when the last sweep was has to be
// kept with the map it describes, which is the case the sync package's own
// documentation sends to a map and a lock.
type pendingStore[T any] struct {
	// lifetime is how long a ceremony may take between its two halves.
	lifetime time.Duration

	mu        sync.Mutex
	byToken   map[string]pending[T]
	lastSweep time.Time
}

// put keeps a first half until its second half takes it or it expires.
func (c *pendingStore[T]) put(token string, data T, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if now.Sub(c.lastSweep) >= pendingSweepInterval {
		c.lastSweep = now

		for key, waiting := range c.byToken {
			if now.After(waiting.expires) {
				delete(c.byToken, key)
			}
		}
	}

	if c.byToken == nil {
		c.byToken = map[string]pending[T]{}
	}

	c.byToken[token] = pending[T]{data: data, expires: now.Add(c.lifetime)}
}

// take returns a first half and removes it, so one can never be used twice. One
// past its lifetime is refused whether or not a sweep has reached it yet.
func (c *pendingStore[T]) take(token string, now time.Time) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var none T

	waiting, ok := c.byToken[token]
	if !ok {
		return none, false
	}

	delete(c.byToken, token)

	if now.After(waiting.expires) {
		return none, false
	}

	return waiting.data, true
}
