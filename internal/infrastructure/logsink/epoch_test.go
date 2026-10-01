package logsink

import "testing"

// A position counted by another process is answered from the beginning of this
// one, and the answer says so.
//
// Sequence numbers start again with the process, and a viewer left open across
// a restart went on asking for "everything after" a number the old process had
// reached. The first answer after the restart held nothing - every line of the
// new process was at or below that number - and moved the viewer on to where
// the new log ended, so the lines a process writes while it starts, which are
// the ones somebody who has just restarted it is waiting to read, were never
// shown and nothing said so.
//
// The number alone cannot tell: once the new process has written more lines
// than the old position, the later ones come back and the earlier ones are still
// missing. So a sink names itself, and a client says which sink its number is
// from.
func TestAPositionFromAnotherProcessIsAnsweredFromTheBeginning(t *testing.T) {
	s := New(10)

	s.appendLine("INFO", "starting")
	s.appendLine("WARN", "a warning while starting")
	s.appendLine("INFO", "serving")

	all := []string{"starting", "a warning while starting", "serving"}

	// Past the end of this sink: no line here has that number yet.
	beyond := s.Query(Query{Since: 5000})
	equal(t, messages(beyond.Records), all)

	if !beyond.Restarted {
		t.Error("a position past the end of the log was answered without saying the log began again")
	}

	// Within this sink's numbers, and still from another one.
	other := s.Query(Query{Since: 2, Epoch: "another-process"})
	equal(t, messages(other.Records), all)

	if !other.Restarted {
		t.Error("a position from another process was taken for one of this process's own")
	}

	// The ordinary follow-on, which must stay what it was.
	own := s.Query(Query{Since: 2, Epoch: s.Epoch()})
	equal(t, messages(own.Records), []string{"serving"})

	if own.Restarted {
		t.Error("a position of this process's own was reported as another's")
	}

	if own.Epoch == "" || own.Epoch != beyond.Epoch {
		t.Errorf("the sink did not name itself the same way twice: %q and %q", own.Epoch, beyond.Epoch)
	}

	// Two sinks are two processes.
	if New(10).Epoch() == s.Epoch() {
		t.Error("two sinks answered to one name")
	}
}

// An answer from the beginning still admits what it leaves out.
//
// A viewer does not always come back at once: paused, or in a tab the browser
// put to sleep, it follows on into a process that has been writing for hours.
// By then the new log may hold more than a page or have rolled over, and the
// lines missing between what the viewer shows and what it is handed are a gap
// like any other - counted from the first line of this process, because every
// one of them is new to somebody who was following its predecessor.
func TestAnAnswerFromTheBeginningStillAdmitsWhatItLeavesOut(t *testing.T) {
	s := New(3)

	for _, m := range []string{"one", "two", "three", "four", "five"} {
		s.appendLine("INFO", m)
	}

	// The ring holds 3, 4 and 5; the page holds two of them.
	result := s.Query(Query{Since: 4, Epoch: "another-process", Limit: 2})

	equal(t, messages(result.Records), []string{"four", "five"})

	if !result.Restarted {
		t.Fatal("a position from another process was taken for one of this process's own")
	}

	if result.Dropped != 2 {
		t.Errorf("two lines of this process had left the ring and the answer says %d were dropped", result.Dropped)
	}

	if result.Skipped != 1 {
		t.Errorf("the page left out one line the ring still held and says it skipped %d", result.Skipped)
	}
}
