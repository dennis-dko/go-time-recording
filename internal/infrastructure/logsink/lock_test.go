package logsink

import (
	"strings"
	"testing"
	"time"
)

// A query of the log does not hold up the lines being written into it.
//
// The pipes Capture puts in front of the process's output are drained by two
// goroutines, and each line they read is stored under the sink's lock. Query
// held that lock for its whole scan - the copy of the ring, a lowercased copy of
// every message for a search, the sort - and a writer waiting on a read lock
// also holds back every reader behind it, so both goroutines stopped draining.
// Once the pipe's buffer was full, every write to standard output blocked, and
// with it every request that logs. The lines that make the scan slow can be
// forced from outside: the framework logs every request with its URI, and the
// ring keeps eight kilobytes of each. Measured with the ring full of those and a
// search, a query held the lock for 114 ms and a line waited 113 ms of it.
func TestALogQueryDoesNotHoldUpTheLinesBeingWritten(t *testing.T) {
	s := New(DefaultCapacity)
	long := strings.Repeat("a", maxKeptBytes)

	for i := range DefaultCapacity {
		s.Append(Record{Time: time.Now(), Level: "INFO", Message: long + string(rune('A'+i%26))})
	}

	var longestQuery, longestWait time.Duration

	// Repeated, so that some writes arrive while a query is under way rather than
	// before it has begun.
	for range 20 {
		started := make(chan struct{})
		took := make(chan time.Duration)

		go func() {
			close(started)

			begin := time.Now()
			s.Query(Query{Search: "nowhere", Limit: 300})
			took <- time.Since(begin)
		}()

		<-started
		time.Sleep(100 * time.Microsecond)

		begin := time.Now()
		s.Append(Record{Time: time.Now(), Level: "INFO", Message: "written meanwhile"})

		longestWait = max(longestWait, time.Since(begin))
		longestQuery = max(longestQuery, <-took)
	}

	t.Logf("a line waited up to %v during queries of up to %v", longestWait, longestQuery)

	if longestWait > longestQuery/4 {
		t.Errorf("a line waited up to %v for a query that took up to %v; the query held the "+
			"lock for its scan rather than for its copy", longestWait, longestQuery)
	}
}
