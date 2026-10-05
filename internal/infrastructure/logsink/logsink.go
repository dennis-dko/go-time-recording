package logsink

import (
	"bufio"
	"crypto/rand"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Record is one captured log line.
type Record struct {
	// Seq is a monotonic identifier. Within one process it never repeats and
	// never goes backwards, which is what lets a client ask for "everything
	// after what I already have" without comparing timestamps - several lines
	// routinely share a millisecond. It starts again with the process, which is
	// what Query.Epoch is for.
	Seq uint64

	Time    time.Time
	Level   string
	Message string

	// TraceID ties a line to the request that produced it, when there was one.
	TraceID string

	// unlevelled marks a line that carried no level of its own - a panic trace,
	// a driver writing to stderr, the framework's start-up banner. Level says
	// INFO for those because something has to, and the threshold below must not
	// act on a level nobody claimed: a stack trace dropped because somebody set
	// WARN is exactly the line they were about to need.
	unlevelled bool
}

// Levels are the levels GoFr emits, most to least severe. Exported so the
// interface offers exactly the set that can actually appear rather than a
// hard-coded guess.
var Levels = []string{"FATAL", "ERROR", "WARN", "NOTICE", "INFO", "DEBUG"}

// Sink holds the most recent records in a fixed-size ring.
//
// A ring rather than a growing slice: this runs for the lifetime of the
// process, and an unbounded log buffer is a memory leak with a plausible
// excuse.
type Sink struct {
	mu      sync.RWMutex
	ring    []Record
	next    int
	filled  bool
	lastSeq uint64

	// renderer turns a record back into the line written to the real console.
	// nil means the captured bytes are forwarded verbatim.
	renderer func(Record) string

	// threshold is the least severe level that is kept and forwarded. Zero
	// means everything, which is what an uncaptured or unconfigured sink does.
	threshold int

	// epoch names this sink among every one there has been, which is to say this
	// process among its predecessors. See Query.Epoch.
	epoch string
}

// severity ranks the levels GoFr emits. Anything not in here is unranked, and
// unranked lines are always kept - see Record.unlevelled.
var severity = map[string]int{
	"DEBUG": 1, "INFO": 2, "NOTICE": 3, "WARN": 4, "ERROR": 5, "FATAL": 6,
}

// SetLevel decides what is written and kept from now on.
//
// This is what makes the log level administrable while the application runs.
// It was built here rather than handed to the framework because GoFr's
// ChangeLevel was a bare assignment to a field every request goroutine reads
// without synchronisation, and a data race is not a reasonable price for
// saving a restart. That was true up to v1.59.0. From v1.60.0 the field is
// atomic - read in its logging/logger.go - so the race is no longer what
// stands between the framework and the level.
//
// The framework is left at its most verbose and the decision is made on the
// way out, once, by whichever of the two goroutines drains the pipe the line
// came down. What the console receives is unchanged: the lines below the
// threshold never reach it. What it costs is that the framework formats a line
// that is then dropped - measured on a fast desktop processor, under four
// microseconds and fifteen allocations for the pipe and the parse alone, three
// to six times a request at INFO, where every statement is such a line.
//
// An empty or unrecognised level means no filtering, which is the safe
// direction: showing too much is a nuisance, and hiding a line somebody needed
// is the failure this package exists to prevent.
func (s *Sink) SetLevel(level string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.threshold = severity[strings.ToUpper(strings.TrimSpace(level))]
}

// Level reports the threshold in force, for the screen that sets it.
func (s *Sink) Level() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for name, rank := range severity {
		if rank == s.threshold {
			return name
		}
	}

	return ""
}

// isAbsentEnvFile reports whether a record is the framework complaining that
// there is no .env beside the binary.
//
// Running without one is a supported way to run this - a single file, no
// directory beside it - and every value such a file would carry is already the
// built-in default. deploy/.env.binary.example exists to say exactly that: its
// lines are the defaults written out, so a binary with no .env behaves like a
// binary with that file.
//
// What made the warning worse than useless is the path it names. GoFr looks for
// a ./configs directory and, finding none, joins the empty string to "/.env" -
// so it reports "/.env", at the root of the filesystem, which nobody configured
// and which could not have worked if it had existed. Somebody reading their
// first start-up goes looking for a file that was never part of the design.
//
// Matched on that exact shape rather than on the sentence. A ./configs
// directory that exists with no .env in it produces the same complaint about a
// real path, and that one is kept: somebody made the directory, so the missing
// file is a mistake rather than a decision. Only the rootless form is silenced,
// and only at WARN - the framework raises a fatal for a file it cannot parse,
// which is a different thing and must still be heard.
func isAbsentEnvFile(r Record) bool {
	return r.Level == "WARN" &&
		strings.HasPrefix(r.Message, "Failed to load config from file: /.env")
}

// keeps reports whether a record passes the threshold.
func (s *Sink) keeps(r Record) bool {
	if r.unlevelled {
		return true
	}

	s.mu.RLock()
	threshold := s.threshold
	s.mu.RUnlock()

	rank, known := severity[r.Level]

	return !known || rank >= threshold
}

// DefaultCapacity is about an hour of ordinary chatter, and a few minutes of a
// tight error loop. Roughly a couple of megabytes at typical line lengths, and
// never more than about forty, because maxKeptBytes bounds each line.
const DefaultCapacity = 5000

// New creates a sink holding at most capacity records.
func New(capacity int) *Sink {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}

	// Random rather than the time it was made: two sinks made in the same tick of
	// a coarse clock would answer to one name, which is the one thing a name here
	// is for - measured on Windows, where two made in a row did.
	return &Sink{ring: make([]Record, capacity), epoch: rand.Text()}
}

// Epoch names the sink a sequence number was counted by.
func (s *Sink) Epoch() string { return s.epoch }

// SetPassthroughRenderer changes what is written to the real console.
//
// Capture forwards the framework's own bytes by default, which is JSON. A
// development build hands in a renderer here to get readable lines back on a
// terminal. It affects only the console; the records kept for the viewer are
// the same either way.
func (s *Sink) SetPassthroughRenderer(render func(Record) string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.renderer = render
}

// Append stores a record, assigning it the next sequence number.
//
// A line longer than maxKeptBytes is kept cut, and cut into a string of its own:
// a slice of the original would hold every byte of it in memory for as long as
// the record stays in the ring, which is the thing the cut is for.
func (s *Sink) Append(r Record) {
	if len(r.Message) > maxKeptBytes {
		cut := maxKeptBytes
		for cut > 0 && !utf8.RuneStart(r.Message[cut]) {
			cut--
		}

		r.Message = r.Message[:cut] + truncationNote
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastSeq++
	r.Seq = s.lastSeq

	s.ring[s.next] = r
	s.next = (s.next + 1) % len(s.ring)

	if s.next == 0 {
		s.filled = true
	}
}

// Query selects records to return.
type Query struct {
	// Since returns only records newer than this sequence number. Zero means
	// from the oldest still held.
	Since uint64

	// Levels restricts the result to these levels. Empty means every level.
	Levels []string

	// Search keeps only records whose message contains this text, compared
	// without regard to case - or whose trace is this text, the whole of it,
	// which is how the lines of one request are found.
	Search string

	// Limit caps how many records come back, keeping the newest. Zero means no
	// cap beyond the ring itself.
	Limit int

	// Epoch is the sink Since was counted by, as a previous Result named it.
	// Empty means the client does not say.
	Epoch string
}

// Result is a page of records plus where the log now ends.
type Result struct {
	Records []Record

	// LastSeq is the newest sequence number in the sink, filtered or not. A
	// client passes it back as Query.Since; using the last returned record
	// instead would re-scan everything a filter excluded on every poll.
	LastSeq uint64

	// Dropped reports how many records fell out of the ring before the client
	// asked for them, so the interface can say that lines are missing rather
	// than quietly presenting a gap as continuity.
	Dropped uint64

	// Skipped reports how many records matched a follow-on query and were left
	// out by its Limit. LastSeq moves past them all the same, so without this a
	// burst between two polls was the gap Dropped exists to admit, only unsaid:
	// the records were still in the ring and the client never asked again.
	Skipped uint64

	// Epoch names this sink, for the client to send back with Since.
	Epoch string

	// Restarted says the client was following another sink - a process that has
	// since been replaced - and was answered from the beginning of this one.
	// Dropped and Skipped then count from that beginning: a client that comes
	// back late is following on into a log that may already have rolled over.
	Restarted bool
}

// Query returns the matching records, oldest first.
//
// The lock is held for a copy of what is newer than Since, and the matching is
// done once it is let go. The goroutines draining the process's output store each
// line under it, and a writer waiting on a read lock holds back every reader
// behind it as well - so while Query held it for its whole scan both stopped, the
// pipe filled, and every write to standard output blocked. Measured, a search
// over a ring of eight-kilobyte lines, which anybody who can reach the port can
// fill, held it for 114 ms a poll.
func (s *Sink) Query(q Query) Result {
	s.mu.RLock()

	result := Result{LastSeq: s.lastSeq, Epoch: s.epoch}

	// A position another sink counted says nothing about this one's lines: the
	// numbers start again with the process. Taken at its word, the first answer
	// after a restart held nothing, moved the client on to where the new log
	// ended, and the lines the process wrote while starting were never shown.
	//
	// Named by the client where it can, and also recognised by a number this
	// sink has not reached - which catches a client that does not say, and
	// cannot catch a position this sink has already passed, which is why the
	// name exists.
	following := q.Since > 0

	if (q.Epoch != "" && q.Epoch != s.epoch) || q.Since > s.lastSeq {
		q.Since = 0
		result.Restarted = true
	}

	// A client that asked for everything after a record the ring has already
	// discarded is missing lines. Say so - and a client following on into a new
	// process is owed every line of it, so there the count starts at none.
	if oldest := s.oldestSeqLocked(); following && oldest > q.Since+1 {
		result.Dropped = oldest - q.Since - 1
	}

	held := s.heldAfterLocked(q.Since)

	s.mu.RUnlock()

	wanted := make(map[string]bool, len(q.Levels))
	for _, level := range q.Levels {
		wanted[strings.ToUpper(strings.TrimSpace(level))] = true
	}

	search := strings.ToLower(strings.TrimSpace(q.Search))

	for _, r := range held {
		if len(wanted) > 0 && !wanted[r.Level] {
			continue
		}

		// The text of the line, or the whole of the trace it was written under.
		// Not a part of one: a trace is thirty-two hexadecimal digits the line
		// does not show, so somebody searching for 500 would be handed one line
		// in about a hundred and forty that says 500 nowhere.
		if search != "" && !strings.Contains(strings.ToLower(r.Message), search) &&
			!strings.EqualFold(r.TraceID, search) {
			continue
		}

		result.Records = append(result.Records, r)
	}

	// Trim from the front: when more matched than asked for, the newest are
	// the ones worth having. Counted when the client is following on from a
	// line it holds, because then what is trimmed is a gap in what it shows.
	if q.Limit > 0 && len(result.Records) > q.Limit {
		if following {
			result.Skipped = uint64(len(result.Records) - q.Limit)
		}

		result.Records = result.Records[len(result.Records)-q.Limit:]
	}

	// In the order the lines were written, which is not the order they reached
	// the ring. GoFr writes ERROR and FATAL to standard error and the rest to
	// standard output, two pipes read by two goroutines, and a line is numbered
	// when its goroutine gets to the lock: measured, an error and the request
	// line written straight after it came back the other way round about two
	// times in five. Equal times, and the lines that carry no time of their own,
	// keep the order they arrived in. A client follows on from LastSeq, not from
	// the last record it was handed, so nothing is fetched twice for it.
	slices.SortStableFunc(result.Records, func(a, b Record) int {
		return a.Time.Compare(b.Time)
	})

	return result
}

// heldAfterLocked copies the held records newer than since, oldest first. A copy
// and never a slice of the ring, because Query reads it after letting the lock
// go, while Append goes on writing into the ring.
func (s *Sink) heldAfterLocked(since uint64) []Record {
	parts := [][]Record{s.ring[:s.next]}
	if s.filled {
		parts = [][]Record{s.ring[s.next:], s.ring[:s.next]}
	}

	var out []Record

	for _, part := range parts {
		for _, r := range part {
			if r.Seq > since {
				out = append(out, r)
			}
		}
	}

	return out
}

// oldestSeqLocked is the sequence number of the oldest record still held, or
// zero when the sink is empty.
func (s *Sink) oldestSeqLocked() uint64 {
	if !s.filled {
		if s.next == 0 {
			return 0
		}

		return s.ring[0].Seq
	}

	return s.ring[s.next].Seq
}

// Capture redirects the process output through the sink.
//
// It must be called before gofr.New(), because GoFr's logger captures
// os.Stdout when it is constructed and keeps it for the life of the process.
//
// The returned function restores the original files and stops capturing. It
// waits briefly for the readers to drain so the last lines still reach the
// console.
func (s *Sink) Capture() (restore func(), err error) {
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	errRead, errWrite, err := os.Pipe()
	if err != nil {
		_ = outRead.Close()
		_ = outWrite.Close()

		return nil, err
	}

	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outWrite, errWrite

	var wg sync.WaitGroup

	wg.Add(2)

	go func() { defer wg.Done(); s.drain(outRead, originalOut) }()
	go func() { defer wg.Done(); s.drain(errRead, originalErr) }()

	return func() {
		os.Stdout, os.Stderr = originalOut, originalErr

		// Closing the write ends ends the readers, which ends the goroutines.
		_ = outWrite.Close()
		_ = errWrite.Close()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			// A blocked console write must not hold up shutdown.
		}

		_ = outRead.Close()
		_ = errRead.Close()
	}, nil
}

// maxLineBytes bounds one log line. A stack trace in a message can be long;
// something megabytes long is a runaway, and truncating beats holding it all.
const maxLineBytes = 512 * 1024

// maxKeptBytes bounds one line as the viewer keeps it, which is far below what
// the console is given.
//
// Bounding the lines by count alone let the ring hold DefaultCapacity lines of
// maxLineBytes each - two and a half gigabytes - and the lines are not all this
// application's own. The framework logs every request with its full URI, a
// refused one as much as an answered one, so anybody who could reach the port
// could pin half a megabyte of memory a request; measured, one anonymous
// request with a long query string was kept at 307 KB. The longest line an
// ordinary session keeps is under two hundred bytes, a collapsed query a few
// hundred more, so eight kilobytes leaves every real line whole and caps the
// ring at about forty megabytes.
const maxKeptBytes = 8 * 1024

// truncationNote is appended to a line that was cut, so it reads as a line that
// was cut rather than as a complete line ending oddly.
const truncationNote = " …[line truncated]"

// drain reads lines, forwards them to the real console and keeps a copy.
//
// Not a bufio.Scanner, and that is the whole of this function's history. A
// Scanner bounded at maxLineBytes does not truncate an over-long line: it stops,
// with ErrTooLong, and the loop was `for scanner.Scan()` - so one enormous line
// ended this goroutine. Nothing was captured or forwarded afterwards, which
// includes the console, and this application's installer token is read from the
// process log. Then the pipe that Capture put in front of os.Stdout filled with
// output nobody was reading any more, and every write to stdout blocked - so the
// process stopped in whatever it was doing when it next tried to log.
//
// Measured, not reasoned about: writing after the long line blocked, and zero
// records were kept. TestAnOverLongLineDoesNotStopTheCapture holds it.
//
// bufio.Reader.ReadLine is the shape that survives it. It hands back a long line
// in pieces rather than refusing it, so the reader never stops on the content it
// is reading - which is the property that matters here, since this goroutine
// ending is the failure.
func (s *Sink) drain(from io.Reader, console io.Writer) {
	reader := bufio.NewReaderSize(from, 64*1024)

	for {
		line, err := readLine(reader, maxLineBytes)

		// The pipe closing is the intended way out, and the only one. A partial
		// line before it is still a line somebody wrote.
		if line == "" && err != nil {
			return
		}

		record := parse(line)

		// Below the administered level: neither written nor kept. The framework
		// runs at its most verbose so that raising the level costs no restart,
		// which means this is the only place the level is actually applied - so
		// a line dropped here is a line the console never had either, exactly as
		// if the framework had suppressed it.
		if !s.keeps(record) {
			continue
		}

		// The framework telling the binary off for a file it was never meant to
		// need. Dropped here for the same reason and by the same means as the
		// level above: this is the only place the process's own output can be
		// edited before anybody sees it.
		if isAbsentEnvFile(record) {
			continue
		}

		// The console first, always. Whatever this package does with the
		// record afterwards must not delay or endanger the output somebody is
		// watching.
		s.mu.RLock()
		render := s.renderer
		s.mu.RUnlock()

		out := withoutValues(line)
		if render != nil {
			out = render(record)
		}

		_, _ = io.WriteString(console, out+"\n")

		s.Append(record)

		if err != nil {
			return
		}
	}
}

// readLine reads one line, cut to limit.
//
// The tail of an over-long line is read and dropped rather than left in the pipe,
// because what is left in the pipe is what the next read would see - a runaway
// line would otherwise arrive as a stream of nonsense lines instead of one.
func readLine(reader *bufio.Reader, limit int) (string, error) {
	var (
		built     strings.Builder
		truncated bool
	)

	for {
		chunk, more, err := reader.ReadLine()

		if room := limit - built.Len(); room > 0 {
			if len(chunk) > room {
				chunk, truncated = chunk[:room], true
			}

			built.Write(chunk)
		} else if len(chunk) > 0 {
			truncated = true
		}

		if err != nil {
			return built.String(), err
		}

		if !more {
			break
		}
	}

	if truncated {
		return built.String() + truncationNote, nil
	}

	return built.String(), nil
}
