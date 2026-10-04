package logsink

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// The log comes back in the order it was written.
//
// GoFr writes ERROR and FATAL to standard error and everything else to standard
// output, and Capture reads the two pipes in two goroutines. A line was numbered
// when its goroutine reached the ring, so a failing request - its error, then at
// once its own request line - came back the other way round about two times in
// five, measured before the fix. The time column shows whole seconds, so the
// swap was invisible as well as wrong.
//
// Written as it happens: an ERROR line and straight after it an INFO line, then
// a pause, a hundred times.
func TestTheLogComesBackInTheOrderItWasWritten(t *testing.T) {
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	drained := make(chan struct{}, 2)

	go func() { _, _ = io.Copy(io.Discard, outRead); drained <- struct{}{} }()
	go func() { _, _ = io.Copy(io.Discard, errRead); drained <- struct{}{} }()

	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outWrite, errWrite

	sink := New(1000)

	restore, err := sink.Capture()
	if err != nil {
		os.Stdout, os.Stderr = originalOut, originalErr

		t.Fatalf("Capture: %v", err)
	}

	capturedOut, capturedErr := os.Stdout, os.Stderr

	const pairs = 100

	written := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)

	for i := range pairs {
		at := written.Add(time.Duration(i) * 10 * time.Millisecond)

		_, _ = fmt.Fprintf(capturedErr, `{"level":"ERROR","time":%q,"message":"failed %d"}`+"\n",
			at.Format(time.RFC3339Nano), i)
		_, _ = fmt.Fprintf(capturedOut, `{"level":"INFO","time":%q,"message":"request %d"}`+"\n",
			at.Add(time.Microsecond).Format(time.RFC3339Nano), i)

		time.Sleep(2 * time.Millisecond)
	}

	// The records arrive through two goroutines, so this waits for them.
	for range 200 {
		if len(sink.Query(Query{}).Records) == 2*pairs {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	restore()

	os.Stdout, os.Stderr = originalOut, originalErr

	_ = outWrite.Close()
	_ = errWrite.Close()
	<-drained
	<-drained

	records := sink.Query(Query{}).Records
	if len(records) != 2*pairs {
		t.Fatalf("%d records were kept of the %d written", len(records), 2*pairs)
	}

	for k := 1; k < len(records); k++ {
		if records[k].Time.Before(records[k-1].Time) {
			t.Errorf("%q came back before %q, which was written after it",
				records[k-1].Message, records[k].Message)
		}
	}
}
