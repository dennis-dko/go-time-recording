package logsink

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// The log viewer's own polls reach the console and stay out of the ring it reads.
//
// Each poll is a request, and GoFr writes a line for every request at INFO - so
// a viewer left open wrote one every few seconds into the five thousand it shows,
// and whoever read it saw mostly themselves reading it while the lines they had
// come for were pushed out. The console is the operator's and keeps them, and a
// poll that failed is kept in both, because a failing viewer is worth finding.
func TestTheViewersOwnPollsStayOutOfTheRing(t *testing.T) {
	s := New(16)
	s.QuietRequestsTo("/api/v1/admin/logs")

	request := func(level, uri string, status int) string {
		return fmt.Sprintf(`{"level":%q,"time":"2026-10-09T10:11:12Z","message":`+
			`{"trace_id":"t","method":"GET","uri":%q,"response":%d,"response_time":900,`+
			`"ip":"127.0.0.1"}}`, level, uri, status)
	}

	var console bytes.Buffer

	s.drain(strings.NewReader(strings.Join([]string{
		request("INFO", "/api/v1/admin/logs?since=4&epoch=x", 200),
		request("INFO", "/api/v1/timesheets", 200),
		request("ERROR", "/api/v1/admin/logs?since=5", 500),
	}, "\n")+"\n"), &console)

	if got := strings.Count(console.String(), "\n"); got != 3 {
		t.Errorf("the console was given %d lines, want all 3:\n%s", got, console.String())
	}

	var kept []string
	for _, record := range s.Query(Query{}).Records {
		kept = append(kept, record.Message)
	}

	if len(kept) != 2 || !strings.Contains(kept[0], "/api/v1/timesheets") ||
		!strings.Contains(kept[1], "/api/v1/admin/logs?since=5 500") {
		t.Errorf("the ring kept %q, want the other request and the poll that failed", kept)
	}
}
