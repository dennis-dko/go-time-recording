package logsink

import "testing"

// Searching for a request's trace finds the lines that request wrote.
//
// The trace is lifted out of the message so the line can say something a person
// reads instead of a wall of JSON - which also takes it out of what the search
// looks at. The code and its test both said the lifting was what made one
// request's lines findable, and the search never looked where it had been
// lifted to: somebody pasting the identifier a failed response carried was
// shown an empty viewer over a buffer holding every line of that request.
func TestSearchingForATraceFindsTheLinesOfThatRequest(t *testing.T) {
	s := New(10)

	const failed, other = "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7a3ce929d0e0e4736"

	// The request log carries its trace inside the message, a line written
	// through the request's own logger beside it, and a statement carries none.
	for _, line := range []string{
		`{"level":"DEBUG","time":"2026-08-03T10:11:12Z","message":` +
			`{"type":"INSERT","query":"INSERT INTO timesheets (hours) VALUES (?)","duration":90,"args":[8]}}`,
		`{"level":"ERROR","time":"2026-08-03T10:11:12Z",` +
			`"message":"saving the entry: database is locked","trace_id":"` + failed + `"}`,
		`{"level":"INFO","time":"2026-08-03T10:11:12Z","message":` +
			`{"trace_id":"` + failed + `","method":"POST","uri":"/api/v1/timesheets","response":500,"response_time":1500}}`,
		`{"level":"INFO","time":"2026-08-03T10:11:13Z","message":` +
			`{"trace_id":"` + other + `","method":"GET","uri":"/api/v1/me","response":200,"response_time":300}}`,
	} {
		s.Append(parse(line))
	}

	// In capitals, because the search ignores case everywhere else.
	found := s.Query(Query{Search: "4BF92F3577B34DA6A3CE929D0E0E4736"}).Records

	if len(found) != 2 || found[0].Level != "ERROR" || found[1].Level != "INFO" {
		t.Errorf("a search for the trace of a failed request found %v; "+
			"want the error it logged and its line in the request log", messages(found))
	}

	// A part of a trace is not a search for it. Thirty-two hexadecimal digits
	// that the line does not show would otherwise answer any short run of
	// digits: both requests here end alike, and neither says so anywhere a
	// reader could see why it was handed the line.
	if part := s.Query(Query{Search: "a3ce929d0e0e4736"}).Records; len(part) != 0 {
		t.Errorf("a search for part of a trace found %v, which say it nowhere", messages(part))
	}
}
