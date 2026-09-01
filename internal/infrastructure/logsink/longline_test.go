package logsink

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// One enormous line must not take the log, and then the process, with it.
//
// Capture replaces os.Stdout and os.Stderr with pipes and reads them in two
// goroutines. drain read those with a bufio.Scanner bounded at maxLineBytes, and
// a Scanner does not truncate an over-long line - it stops, with ErrTooLong. The
// loop was `for scanner.Scan()`, so the goroutine simply returned.
//
// Two things follow, and the second is the serious one. Nothing is captured or
// forwarded after that point: no console output, no log screen, no record of
// whatever happens next - and the process log is where this application's
// installer token is read from. Then, once the pipe's buffer fills with the
// output nobody is reading any more, every write to stdout blocks, and the
// application stops in whatever it was doing when it next tried to log.
//
// Measured before the fix rather than reasoned about: writing after the long
// line blocked, and zero records were kept.
//
// maxLineBytes' own comment said what should happen - "something megabytes long
// is a runaway, and truncating beats holding it all" - and truncating is the one
// thing a Scanner will not do.
func TestAnOverLongLineDoesNotStopTheCapture(t *testing.T) {
	// A console that is always drained, so nothing here can block on it and a
	// failure means what it says.
	consoleRead, consoleWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	drained := make(chan struct{})

	go func() { defer close(drained); _, _ = io.Copy(io.Discard, consoleRead) }()

	original := os.Stdout
	os.Stdout = consoleWrite

	sink := New(50)

	restore, err := sink.Capture()
	if err != nil {
		os.Stdout = original

		t.Fatalf("Capture: %v", err)
	}

	captured := os.Stdout

	// Deliberately past the bound, and then ordinary logging - enough of it to
	// outrun any pipe buffer, because the failure this covers is the writes
	// blocking rather than the first one failing.
	long := strings.Repeat("x", maxLineBytes+1024)
	after := `{"level":"ERROR","time":"2026-08-03T10:00:00Z","message":"still logging"}`

	written := make(chan error, 1)

	go func() {
		_, writeErr := captured.WriteString(long + "\n")

		for i := 0; i < 20000 && writeErr == nil; i++ {
			_, writeErr = captured.WriteString(after + "\n")
		}

		written <- writeErr
	}()

	// The bound is what makes a regression report the reason instead of hanging
	// the suite until the whole package times out.
	select {
	case writeErr := <-written:
		if writeErr != nil {
			t.Errorf("writing after an over-long line failed: %v", writeErr)
		}
	case <-time.After(20 * time.Second):
		t.Error("writing to stdout blocked after an over-long line: the drain " +
			"stopped reading and the pipe filled, which stops the application")
	}

	// The records arrive through a goroutine, so this waits for them rather than
	// assuming they are already there.
	kept := 0

	for range 100 {
		kept = len(sink.Query(Query{}).Records)
		if kept > 0 {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	restore()

	os.Stdout = original

	_ = consoleWrite.Close()
	<-drained
	_ = consoleRead.Close()

	if kept == 0 {
		t.Fatal("nothing was captured after the over-long line; the drain stopped")
	}
}

// And the over-long line is cut rather than dropped, because a runaway line is
// itself the evidence about whatever produced it.
//
// Tested against readLine directly rather than through Capture: the sink is a
// ring buffer, so a case that writes enough output to prove the pipe does not
// block has by then evicted the line it wanted to look at. Two properties, two
// cases.
func TestAnOverLongLineIsCutAndSaysSo(t *testing.T) {
	const limit = 64

	long := strings.Repeat("x", limit*4)

	reader := bufio.NewReaderSize(strings.NewReader(long+"\nthe next line\n"), 16)

	first, err := readLine(reader, limit)
	if err != nil {
		t.Fatalf("reading the long line: %v", err)
	}

	if !strings.HasSuffix(first, truncationNote) {
		t.Errorf("the cut line does not say it was cut: %q", first)
	}

	if body := strings.TrimSuffix(first, truncationNote); len(body) != limit {
		t.Errorf("kept %d bytes of the line, want %d", len(body), limit)
	}

	// The tail of the long line is dropped rather than left to arrive as lines of
	// its own, which is the difference between one runaway line and hundreds.
	second, err := readLine(reader, limit)
	if err != nil {
		t.Fatalf("reading the line after it: %v", err)
	}

	if second != "the next line" {
		t.Errorf("the line after the long one read %q; the tail of the long one "+
			"was left in the stream", second)
	}
}
