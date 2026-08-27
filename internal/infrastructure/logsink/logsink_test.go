package logsink

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// append is a shorthand for the tests, which care about level and message and
// never about the clock.
func (s *Sink) appendLine(level, message string) {
	s.Append(Record{Time: time.Now(), Level: level, Message: message})
}

func messages(records []Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.Message)
	}

	return out
}

func equal(t *testing.T, got, want []string) {
	t.Helper()

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The ring is the whole reason this can run for months without growing, so it
// has to actually discard rather than merely stop appending.
func TestTheRingKeepsTheNewestAndDropsTheOldest(t *testing.T) {
	s := New(3)

	for _, m := range []string{"one", "two", "three", "four", "five"} {
		s.appendLine("INFO", m)
	}

	result := s.Query(Query{})

	equal(t, messages(result.Records), []string{"three", "four", "five"})

	if result.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", result.LastSeq)
	}
}

// Sequence numbers are what a polling client uses to ask for "only what is
// new". If they restarted or repeated, a viewer would either miss lines or
// show them twice.
func TestSequenceNumbersNeverRepeatEvenAfterTheRingWraps(t *testing.T) {
	s := New(2)

	seen := map[uint64]bool{}

	for range 10 {
		s.appendLine("INFO", "line")
	}

	for _, r := range s.Query(Query{}).Records {
		if seen[r.Seq] {
			t.Fatalf("sequence %d appeared twice", r.Seq)
		}

		seen[r.Seq] = true
	}

	if got := s.Query(Query{}).LastSeq; got != 10 {
		t.Errorf("LastSeq = %d, want 10", got)
	}
}

func TestSinceReturnsOnlyWhatIsNewer(t *testing.T) {
	s := New(10)

	s.appendLine("INFO", "first")
	s.appendLine("INFO", "second")

	after := s.Query(Query{}).LastSeq

	s.appendLine("INFO", "third")

	equal(t, messages(s.Query(Query{Since: after}).Records), []string{"third"})
}

// A client that polls with Since is told when the ring discarded records it
// had not fetched yet. Presenting a gap as continuity would be worse than
// admitting it.
func TestDroppedReportsWhatTheClientMissed(t *testing.T) {
	s := New(3)

	s.appendLine("INFO", "one")

	after := s.Query(Query{}).LastSeq // 1

	for _, m := range []string{"two", "three", "four", "five"} {
		s.appendLine("INFO", m)
	}

	// The ring now holds 3,4,5. Record 2 was never seen by this client.
	result := s.Query(Query{Since: after})

	if result.Dropped != 1 {
		t.Errorf("Dropped = %d, want 1", result.Dropped)
	}

	equal(t, messages(result.Records), []string{"three", "four", "five"})
}

func TestNothingIsReportedDroppedWhileTheRingStillHoldsEverything(t *testing.T) {
	s := New(10)

	s.appendLine("INFO", "one")
	s.appendLine("INFO", "two")

	if got := s.Query(Query{Since: 1}).Dropped; got != 0 {
		t.Errorf("Dropped = %d, want 0", got)
	}
}

func TestFilteringByLevel(t *testing.T) {
	s := New(10)

	s.appendLine("DEBUG", "noisy")
	s.appendLine("INFO", "ordinary")
	s.appendLine("WARN", "worrying")
	s.appendLine("ERROR", "broken")

	equal(t, messages(s.Query(Query{Levels: []string{"WARN", "ERROR"}}).Records),
		[]string{"worrying", "broken"})

	// Lower case from a query string has to work: the interface sends what the
	// checkbox value says, not what this package spells internally.
	equal(t, messages(s.Query(Query{Levels: []string{"error"}}).Records),
		[]string{"broken"})
}

func TestSearchIgnoresCase(t *testing.T) {
	s := New(10)

	s.appendLine("INFO", "Directory sync removed alice@example.com")
	s.appendLine("INFO", "session opened")

	equal(t, messages(s.Query(Query{Search: "ALICE"}).Records),
		[]string{"Directory sync removed alice@example.com"})
}

func TestLevelAndSearchApplyTogether(t *testing.T) {
	s := New(10)

	s.appendLine("ERROR", "database unreachable")
	s.appendLine("INFO", "database migrated")
	s.appendLine("ERROR", "ldap unreachable")

	equal(t, messages(s.Query(Query{Levels: []string{"ERROR"}, Search: "database"}).Records),
		[]string{"database unreachable"})
}

// The newest lines are the ones worth keeping when more match than were asked
// for - a viewer showing the oldest hundred of a thousand-line error storm
// would be useless.
func TestLimitKeepsTheNewest(t *testing.T) {
	s := New(10)

	for _, m := range []string{"one", "two", "three", "four"} {
		s.appendLine("INFO", m)
	}

	equal(t, messages(s.Query(Query{Limit: 2}).Records), []string{"three", "four"})
}

func TestAnEmptySinkAnswersWithoutRecords(t *testing.T) {
	result := New(5).Query(Query{})

	if len(result.Records) != 0 || result.LastSeq != 0 || result.Dropped != 0 {
		t.Errorf("got %+v, want an empty result", result)
	}
}

// ------------------------------------------------------------------- parsing

func TestParsingGofrsJSON(t *testing.T) {
	record := parse(`{"level":"WARN","time":"2026-08-03T10:11:12.5Z",` +
		`"message":"directory sync refused","trace_id":"abc123","gofrVersion":"1.58.0"}`)

	if record.Level != "WARN" {
		t.Errorf("Level = %q, want WARN", record.Level)
	}

	if record.Message != "directory sync refused" {
		t.Errorf("Message = %q", record.Message)
	}

	if record.TraceID != "abc123" {
		t.Errorf("TraceID = %q, want abc123", record.TraceID)
	}

	if record.Time.Year() != 2026 || record.Time.Minute() != 11 {
		t.Errorf("Time = %s, want the timestamp from the line", record.Time)
	}
}

// The request log's message is an object, not a string. Left as raw JSON every
// request line in the viewer would be a wall of braces, which is most of what
// there is to read.
func TestARequestLogBecomesAReadableLine(t *testing.T) {
	record := parse(`{"level":"INFO","time":"2026-08-03T10:11:12Z","message":` +
		`{"trace_id":"deadbeef","method":"GET","uri":"/api/v1/me","response":200,` +
		`"response_time":1500,"ip":"127.0.0.1"}}`)

	for _, want := range []string{"GET", "/api/v1/me", "200", "1.5ms", "127.0.0.1"} {
		if !strings.Contains(record.Message, want) {
			t.Errorf("message %q does not mention %q", record.Message, want)
		}
	}

	// Without lifting the trace out of the message, searching for one request's
	// lines would be impossible.
	if record.TraceID != "deadbeef" {
		t.Errorf("TraceID = %q, want it lifted out of the message", record.TraceID)
	}
}

// The query log is the other object shape, and the repositories here write
// multi-line SQL - which would otherwise arrive as one line full of runs of
// spaces.
func TestAQueryLogBecomesAReadableLine(t *testing.T) {
	record := parse(`{"level":"DEBUG","time":"2026-08-03T10:11:12Z","message":` +
		`{"type":"QueryRowContext","duration":2000,"query":"SELECT id\n  FROM users\n  WHERE id = ?"}}`)

	if !strings.Contains(record.Message, "SELECT id FROM users WHERE id = ?") {
		t.Errorf("message %q: the statement was not collapsed onto one line", record.Message)
	}

	for _, want := range []string{"QueryRowContext", "2ms"} {
		if !strings.Contains(record.Message, want) {
			t.Errorf("message %q does not mention %q", record.Message, want)
		}
	}
}

// An object this package does not recognise keeps its JSON. Dropping the fields
// it did not expect would hide exactly the unusual thing worth looking at.
func TestAnUnrecognisedObjectKeepsItsJSON(t *testing.T) {
	record := parse(`{"level":"INFO","time":"2026-08-03T10:11:12Z",` +
		`"message":{"somethingNew":"and its value"}}`)

	for _, want := range []string{"somethingNew", "and its value"} {
		if !strings.Contains(record.Message, want) {
			t.Errorf("message %q does not mention %q", record.Message, want)
		}
	}
}

// Anything unparseable is kept rather than dropped: a panic trace is precisely
// what somebody opening a log viewer is looking for.
func TestUnparseableLinesAreKeptVerbatim(t *testing.T) {
	for _, line := range []string{
		"panic: runtime error: invalid memory address",
		"",
		"{not actually json",
	} {
		record := parse(line)

		if record.Message != line {
			t.Errorf("parse(%q).Message = %q, want it kept as it was", line, record.Message)
		}

		if record.Level != "INFO" {
			t.Errorf("parse(%q).Level = %q, want INFO", line, record.Level)
		}
	}
}

func TestAJSONLineWithoutALevelOrTimeStillGetsBoth(t *testing.T) {
	record := parse(`{"message":"something happened"}`)

	if record.Level != "INFO" {
		t.Errorf("Level = %q, want INFO", record.Level)
	}

	if record.Time.IsZero() {
		t.Error("Time is zero; a record with no timestamp is unsortable")
	}
}

// ------------------------------------------------------------------- capture

// The console must keep receiving everything. A log viewer that swallowed the
// output of `docker logs` would be a poor trade.
func TestCaptureForwardsToTheConsoleAndKeepsACopy(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("cannot create a pipe: %v", err)
	}

	// Stand in for the real console, so the test can read what was forwarded.
	original := os.Stdout
	os.Stdout = write

	s := New(10)

	restore, err := s.Capture()
	if err != nil {
		os.Stdout = original

		t.Fatalf("Capture: %v", err)
	}

	line := `{"level":"ERROR","time":"2026-08-03T10:00:00Z","message":"it broke"}`

	if _, err := os.Stdout.WriteString(line + "\n"); err != nil {
		t.Fatalf("writing to the captured stdout: %v", err)
	}

	restore()

	os.Stdout = original

	_ = write.Close()

	forwarded := make([]byte, 4096)
	n, _ := read.Read(forwarded)

	_ = read.Close()

	if !strings.Contains(string(forwarded[:n]), "it broke") {
		t.Errorf("the console got %q, which does not contain the line", string(forwarded[:n]))
	}

	records := s.Query(Query{}).Records
	if len(records) != 1 {
		t.Fatalf("the sink kept %d records, want 1", len(records))
	}

	if records[0].Level != "ERROR" || records[0].Message != "it broke" {
		t.Errorf("kept %+v, want the parsed ERROR line", records[0])
	}
}

// Restoring has to put the real files back, or every later write in the
// process disappears into a closed pipe.
func TestRestorePutsTheOriginalFilesBack(t *testing.T) {
	stdout, stderr := os.Stdout, os.Stderr

	restore, err := New(4).Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if os.Stdout == stdout {
		t.Error("Capture did not replace os.Stdout")
	}

	restore()

	if os.Stdout != stdout || os.Stderr != stderr {
		t.Error("restore did not put the original files back")
	}
}

// The viewer's filter and the log level setting describe the same six levels
// from two different places: this one, which is what the process can emit, and
// model.SupportedLogLevels, which is what an administrator may choose.
//
// They are kept apart on purpose - the domain must not reach into infrastructure
// for a list - so something has to hold them together, or the Settings screen
// ends up offering a level that never appears in the viewer beneath it.
func TestTheLevelsOfferedAndTheLevelsEmittedAreTheSame(t *testing.T) {
	emitted := map[string]bool{}
	for _, level := range Levels {
		emitted[level] = true
	}

	offered := map[string]bool{}
	for _, level := range model.SupportedLogLevels() {
		offered[level] = true
	}

	for level := range emitted {
		if !offered[level] {
			t.Errorf("%q can be emitted but cannot be chosen under Settings", level)
		}
	}

	for level := range offered {
		if !emitted[level] {
			t.Errorf("%q can be chosen under Settings but is not a level the viewer knows", level)
		}
	}
}

// The administered level decides what is written and kept, from the next line.
//
// This is what lets the log level be changed without a restart. The framework
// decides what to emit from a field every request goroutine reads without
// synchronisation, so changing that while requests are in flight is a data race;
// instead the framework is left at its most verbose and the level is applied
// here, in the one goroutine that drains the captured output.
func TestTheLevelDecidesWhatIsKept(t *testing.T) {
	s := New(100)
	s.SetLevel("WARN")

	for _, line := range []struct{ level, message string }{
		{"DEBUG", "a query"},
		{"INFO", "a request"},
		{"WARN", "something odd"},
		{"ERROR", "something wrong"},
	} {
		if !s.keeps(Record{Level: line.level}) {
			continue
		}

		s.appendLine(line.level, line.message)
	}

	equal(t, messages(s.Query(Query{}).Records),
		[]string{"something odd", "something wrong"})
}

// And it can be changed again, which is the whole point.
func TestTheLevelCanBeRaisedAndLoweredWhileRunning(t *testing.T) {
	s := New(100)

	s.SetLevel("ERROR")

	if s.keeps(Record{Level: "WARN"}) {
		t.Error("a WARN line is kept at ERROR")
	}

	s.SetLevel("DEBUG")

	if !s.keeps(Record{Level: "DEBUG"}) {
		t.Error("a DEBUG line is dropped at DEBUG, so raising the level did nothing")
	}

	if got := s.Level(); got != "DEBUG" {
		t.Errorf("the sink reports %q as the level in force, want DEBUG", got)
	}
}

// A line that claimed no level of its own is always kept.
//
// A panic trace, a driver writing to stderr, the framework's start-up banner:
// parse calls those INFO because the record needs something, and acting on that
// would drop a stack trace because somebody set WARN - which is exactly the line
// they were about to need.
func TestALineWithNoLevelOfItsOwnSurvivesAnyThreshold(t *testing.T) {
	s := New(100)
	s.SetLevel("FATAL")

	if !s.keeps(parse("panic: runtime error: invalid memory address")) {
		t.Error("a panic trace is dropped by the log level, which is where it is " +
			"least affordable")
	}

	if !s.keeps(parse(`{"level":"MYSTERY","message":"from somewhere else"}`)) {
		t.Error("a line at a level this does not rank is dropped rather than kept")
	}
}

// An empty or unrecognised level filters nothing, which is the safe direction:
// showing too much is a nuisance and hiding a line somebody needed is the
// failure this package exists to prevent.
func TestAnUnrecognisedLevelFiltersNothing(t *testing.T) {
	s := New(100)
	s.SetLevel("verbose")

	if !s.keeps(Record{Level: "DEBUG"}) {
		t.Error("a level nobody recognises hid something")
	}
}

// Running the binary with no .env beside it is a supported way to run it, so the
// framework's complaint about that is not news.
//
// GoFr looks for ./configs and, when there is no such directory, asks godotenv
// for the empty string joined to "/.env" - so the warning names "/.env", a path
// at the root of the filesystem that nobody configured and that could not have
// worked. Every value the file would have carried is already the built-in
// default; deploy/.env.binary.example exists to say so, and its lines are the
// defaults written out.
//
// A configs directory that exists and has no .env in it is a different case and
// keeps its warning: somebody made the directory, so the missing file is
// probably a mistake rather than a decision.
func TestTheFrameworksComplaintAboutAnAbsentEnvFileIsNotShown(t *testing.T) {
	t.Parallel()

	absent := Record{
		Level:   "WARN",
		Message: "Failed to load config from file: /.env, Err: open /.env: no such file or directory",
	}

	if !isAbsentEnvFile(absent) {
		t.Error("the binary is told off for not having a file it was never meant to need")
	}

	// The same complaint about a directory somebody actually made.
	deliberate := Record{
		Level:   "WARN",
		Message: "Failed to load config from file: ./configs/.env, Err: open ./configs/.env: no such file or directory",
	}

	if isAbsentEnvFile(deliberate) {
		t.Error("a configs directory with no .env in it is a mistake worth reporting, " +
			"and this swallows it")
	}

	// And nothing else is swallowed with it.
	for _, other := range []Record{
		{Level: "WARN", Message: "SECRET_KEY is not set"},
		{Level: "ERROR", Message: "Failed to load config from file: /.env"},
		{Level: "INFO", Message: "Loaded config from file: ./configs/.env"},
	} {
		if isAbsentEnvFile(other) {
			t.Errorf("this is not the framework's missing-env warning: %q", other.Message)
		}
	}
}
