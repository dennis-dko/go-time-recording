package logsink

import (
	"bytes"
	"strings"
	"testing"
)

// A statement's values are not written out, at any level.
//
// GoFr logs every SQL statement at DEBUG together with its arguments, and this
// application runs the framework at DEBUG always and applies the level here, on
// the way out. So an administrator choosing DEBUG - which the Settings screen
// offers, and which is what somebody diagnosing a fault reaches for - sent every
// value every statement carried to the console: a second factor's secret as it
// was enrolled, the directory's bind password as it was saved, both in clear
// text on an installation without SECRET_KEY, and everybody's hours and notes.
// The console is the container's log or the journal, read by whoever operates
// the machine, who need not be anybody this application lets see any of that.
//
// The statement stays, and so does how many values it had and of which kind:
// that is what a query log is for. The values are not.
func TestAStatementsValuesAreNotWrittenOut(t *testing.T) {
	t.Parallel()

	const secret = "JBSWY3DPEHPK3PXP"

	s := New(10)
	s.SetLevel("DEBUG")

	line := `{"level":"DEBUG","time":"2026-09-29T10:11:12Z","message":{"type":"ExecContext",` +
		`"duration":900,"query":"UPDATE users SET totp_secret = ?, totp_enabled = ? WHERE id = ? AND n < 2",` +
		`"args":["` + secret + `",true,7]},"trace_id":"abc"}`

	var console bytes.Buffer

	s.drain(strings.NewReader(line+"\n"), &console)

	written := console.String()

	if strings.Contains(written, secret) {
		t.Errorf("the console was given the statement's values: %s", written)
	}

	for _, want := range []string{"UPDATE users SET totp_secret", "AND n < 2",
		`"<string>"`, `"<bool>"`, `"<number>"`} {
		if !strings.Contains(written, want) {
			t.Errorf("the console line %s does not say %s", written, want)
		}
	}

	for _, record := range s.Query(Query{}).Records {
		if strings.Contains(record.Message, secret) {
			t.Errorf("the viewer keeps the statement's values: %s", record.Message)
		}
	}
}

// Every other line reaches the console as the framework wrote it.
func TestOnlyAQueryLogIsRewritten(t *testing.T) {
	t.Parallel()

	s := New(10)

	lines := []string{
		`{"level":"INFO","time":"2026-09-29T10:11:12Z","message":"args are not a statement's here"}`,
		`{"level":"INFO","time":"2026-09-29T10:11:12Z","message":{"method":"GET","uri":"/x?args=1","response":200}}`,
		`panic: not JSON at all, "args" and all`,
	}

	var console bytes.Buffer

	s.drain(strings.NewReader(strings.Join(lines, "\n")+"\n"), &console)

	if got := strings.TrimRight(console.String(), "\n"); got != strings.Join(lines, "\n") {
		t.Errorf("lines that are not a query log were changed:\n%s", got)
	}
}
