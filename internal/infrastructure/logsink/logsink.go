// Package logsink keeps the most recent log lines in memory so an
// administrator can read them from the running application.
//
// # Why it intercepts the process output
//
// The interesting lines are not the ones this code writes. They are the ones
// the framework writes: the request log, a failing database statement, what
// happened during the migrations. GoFr builds its logger inside gofr.New()
// with os.Stdout captured at construction, and exposes no way to supply a
// writer, so the only place to see everything it emits is the process's own
// output.
//
// So Capture replaces os.Stdout and os.Stderr with pipes, reads the lines,
// forwards each one to the real console unchanged, and keeps a copy. Nothing
// downstream changes: a container still gets identical output on its stdout.
//
// # What this costs
//
// GoFr decides between pretty-printed and JSON output by asking whether its
// output is a terminal. A pipe is not, so with capture installed the console
// gets JSON even when a person is watching. That is the deliberate trade: JSON
// is what the log viewer needs to show a level and a timestamp rather than a
// wall of text, it is what a log collector wants in production anyway, and
// SetPassthroughRenderer exists for the development case that wants readable
// lines back.
//
// # The one thing that can be lost
//
// A Fatal writes and then calls os.Exit immediately. The bytes reach the
// kernel's pipe buffer, but this package's reader may not be scheduled before
// the process is gone, so a fatal line can be missing from the console. That
// matters because a fatal is exactly the message an operator needs, which is
// why the application pre-flights the failures it can predict - a taken port,
// an unusable database - and reports those itself rather than letting the
// framework exit on them.
package logsink

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Record is one captured log line.
type Record struct {
	// Seq is a monotonic identifier. It never repeats and never goes
	// backwards, which is what lets a client ask for "everything after what I
	// already have" without comparing timestamps - several lines routinely
	// share a millisecond.
	Seq uint64

	Time    time.Time
	Level   string
	Message string

	// TraceID ties a line to the request that produced it, when there was one.
	TraceID string
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
}

// DefaultCapacity is about an hour of ordinary chatter, and a few minutes of a
// tight error loop. Roughly a couple of megabytes at typical line lengths.
const DefaultCapacity = 5000

// New creates a sink holding at most capacity records.
func New(capacity int) *Sink {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}

	return &Sink{ring: make([]Record, capacity)}
}

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
func (s *Sink) Append(r Record) {
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
	// without regard to case.
	Search string

	// Limit caps how many records come back, keeping the newest. Zero means no
	// cap beyond the ring itself.
	Limit int
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
}

// Query returns the matching records, oldest first.
func (s *Sink) Query(q Query) Result {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := Result{LastSeq: s.lastSeq}

	wanted := make(map[string]bool, len(q.Levels))
	for _, level := range q.Levels {
		wanted[strings.ToUpper(strings.TrimSpace(level))] = true
	}

	search := strings.ToLower(strings.TrimSpace(q.Search))

	// A client that asked for everything after a record the ring has already
	// discarded is missing lines. Say so.
	if oldest := s.oldestSeqLocked(); q.Since > 0 && oldest > q.Since+1 {
		result.Dropped = oldest - q.Since - 1
	}

	for _, r := range s.orderedLocked() {
		if r.Seq <= q.Since {
			continue
		}

		if len(wanted) > 0 && !wanted[r.Level] {
			continue
		}

		if search != "" && !strings.Contains(strings.ToLower(r.Message), search) {
			continue
		}

		result.Records = append(result.Records, r)
	}

	// Trim from the front: when more matched than asked for, the newest are
	// the ones worth having.
	if q.Limit > 0 && len(result.Records) > q.Limit {
		result.Records = result.Records[len(result.Records)-q.Limit:]
	}

	return result
}

// orderedLocked returns the held records oldest first.
func (s *Sink) orderedLocked() []Record {
	if !s.filled {
		return s.ring[:s.next]
	}

	out := make([]Record, 0, len(s.ring))
	out = append(out, s.ring[s.next:]...)
	out = append(out, s.ring[:s.next]...)

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

		// Closing the write ends ends the scanners, which ends the goroutines.
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

// drain reads lines, forwards them to the real console and keeps a copy.
func (s *Sink) drain(from io.Reader, console io.Writer) {
	scanner := bufio.NewScanner(from)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		line := scanner.Text()

		record := parse(line)

		// The console first, always. Whatever this package does with the
		// record afterwards must not delay or endanger the output somebody is
		// watching.
		s.mu.RLock()
		render := s.renderer
		s.mu.RUnlock()

		out := line
		if render != nil {
			out = render(record)
		}

		_, _ = io.WriteString(console, out+"\n")

		s.Append(record)
	}
}

// entry is the shape GoFr encodes. Message is any: a string for an ordinary
// log call, an object for the request log.
type entry struct {
	Level   string          `json:"level"`
	Time    time.Time       `json:"time"`
	Message json.RawMessage `json:"message"`
	TraceID string          `json:"trace_id"`
}

// parse turns a captured line into a record.
//
// Anything that is not GoFr's JSON - a panic trace, a driver writing to stderr
// on its own, the framework's start-up banner - is kept verbatim at INFO. A
// log viewer that silently dropped what it could not parse would hide exactly
// the lines somebody is hunting for.
func parse(line string) Record {
	trimmed := strings.TrimSpace(line)

	if !strings.HasPrefix(trimmed, "{") {
		return Record{Time: time.Now(), Level: "INFO", Message: line}
	}

	var e entry
	if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
		return Record{Time: time.Now(), Level: "INFO", Message: line}
	}

	text, traceID := messageText(e.Message)

	record := Record{
		Time:    e.Time,
		Level:   strings.ToUpper(strings.TrimSpace(e.Level)),
		Message: text,
		TraceID: e.TraceID,
	}

	// The request and query logs carry the trace inside the message rather than
	// beside it. Lifting it out is what makes searching for one request's lines
	// possible at all.
	if record.TraceID == "" {
		record.TraceID = traceID
	}

	if record.Time.IsZero() {
		record.Time = time.Now()
	}

	if record.Level == "" {
		record.Level = "INFO"
	}

	return record
}

// structured is the union of the two message shapes GoFr logs as objects: the
// request log from its HTTP middleware, and the query log from its SQL
// datasource. Both are frequent enough that leaving them as raw JSON would make
// a log viewer unreadable for the two things most worth reading.
type structured struct {
	// The request log.
	Method       string `json:"method"`
	URI          string `json:"uri"`
	Response     int    `json:"response"`
	ResponseTime int64  `json:"response_time"`
	IP           string `json:"ip"`

	// The query log.
	Type     string `json:"type"`
	Query    string `json:"query"`
	Duration int64  `json:"duration"`

	TraceID string `json:"trace_id"`
}

// messageText renders the message field as one line, and reports the trace it
// mentions if it mentions one.
//
// A string is used as it is. A recognised object becomes a readable summary. An
// unrecognised one keeps its JSON: wrong but complete beats a guess that drops
// the field somebody needed.
func messageText(raw json.RawMessage) (text, traceID string) {
	if len(raw) == 0 {
		return "", ""
	}

	if err := json.Unmarshal(raw, &text); err == nil {
		return text, ""
	}

	var s structured
	if err := json.Unmarshal(raw, &s); err != nil {
		return strings.TrimSpace(string(raw)), ""
	}

	switch {
	case s.Method != "":
		// GoFr reports response_time in microseconds.
		line := fmt.Sprintf("%s %s %d %s", s.Method, s.URI, s.Response, micros(s.ResponseTime))
		if s.IP != "" {
			line += " from " + s.IP
		}

		return line, s.TraceID
	case s.Query != "":
		return fmt.Sprintf("%s %s %s", s.Type, micros(s.Duration), collapse(s.Query)), s.TraceID
	default:
		return strings.TrimSpace(string(raw)), s.TraceID
	}
}

// micros renders a microsecond duration the way a person reads it.
func micros(us int64) string {
	return (time.Duration(us) * time.Microsecond).String()
}

// collapse puts a statement on one line. GoFr logs the SQL as written, and the
// repositories here write multi-line queries.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
