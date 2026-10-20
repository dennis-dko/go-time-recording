package logsink

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// A calendar feed's key is the whole of its address, and the framework writes the
// address of every request into the log - so a reading of somebody's feed put the
// key to their recorded days in front of whoever reads the log, the administrator
// in the viewer included. Concealed, the line still says a feed was read, and
// which kind of key it was, without being the key.
func TestAConcealedKeyReachesNeitherTheConsoleNorTheRing(t *testing.T) {
	const key = "gtrcal_Q2FsZW5kYXIga2V5IGZvciB0ZXN0aW5nIG9ubHk-_x"

	line := fmt.Sprintf(`{"level":"INFO","time":"2026-10-20T10:11:12Z","message":`+
		`{"trace_id":"t","method":"GET","uri":"/api/v1/calendar-feed/%s.ics","response":200,`+
		`"response_time":900,"ip":"127.0.0.1"}}`, key)

	for _, rendered := range []bool{false, true} {
		s := New(16)
		s.Conceal("gtrcal_")

		if rendered {
			s.SetPassthroughRenderer(func(r Record) string { return r.Message })
		}

		var console bytes.Buffer

		s.drain(strings.NewReader(line+"\n"), &console)

		kept := s.Query(Query{}).Records
		if len(kept) != 1 {
			t.Fatalf("the ring kept %d records, want the one", len(kept))
		}

		for where, written := range map[string]string{"console": console.String(), "ring": kept[0].Message} {
			if strings.Contains(written, key[len("gtrcal_"):]) {
				t.Errorf("rendered %v, the %s has the key: %s", rendered, where, written)
			}

			if !strings.Contains(written, "/api/v1/calendar-feed/gtrcal_[hidden].ics") {
				t.Errorf("rendered %v, the %s does not say a key was read: %s", rendered, where, written)
			}
		}
	}
}

// What is concealed is what follows the prefix and nothing else, every time it
// occurs, and a line without it is written as it came.
func TestOnlyTheKeyIsConcealed(t *testing.T) {
	s := New(4)
	s.Conceal("gtrcal_")

	for line, want := range map[string]string{
		"two: gtrcal_abc/gtrcal_DEF-1_x and the rest": "two: gtrcal_[hidden]/gtrcal_[hidden] and the rest",
		"a prefix with nothing after it: gtrcal_ ":    "a prefix with nothing after it: gtrcal_ ",
		"nothing to hide here":                        "nothing to hide here",
	} {
		if got := s.concealed(line); got != want {
			t.Errorf("%q was written %q, want %q", line, got, want)
		}
	}
}
