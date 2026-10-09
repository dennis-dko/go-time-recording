package logsink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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
		return Record{Time: time.Now(), Level: "INFO", Message: line, unlevelled: true}
	}

	var e entry
	if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
		if level, ok := levelOfACutLine(trimmed); ok {
			return Record{Time: time.Now(), Level: level, Message: line}
		}

		return Record{Time: time.Now(), Level: "INFO", Message: line, unlevelled: true}
	}

	text, traceID, asked := messageText(e.Message)

	record := Record{
		Time:    e.Time,
		Level:   strings.ToUpper(strings.TrimSpace(e.Level)),
		Message: text,
		TraceID: e.TraceID,
		request: asked,
	}

	// The request log carries its trace inside the message rather than beside
	// it, and so does what the framework writes for a handler that failed. The
	// readable line made of the message no longer says it, so it is kept here,
	// which is where Query looks when somebody searches for a request by its
	// trace. A statement has none to lift: the framework logs those without the
	// request they ran for.
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

// levelOfACutLine reads the level of a line GoFr wrote and readLine cut, which is
// no longer JSON once cut.
//
// GoFr writes the request log at ERROR for an answer of 500 and up, with the
// whole URI in it, and a URI may be up to a megabyte long - longer than a line
// is kept. Left unlevelled, such a line was filed under INFO and was missing
// from the filter somebody chooses to see what failed. GoFr encodes its entry
// with the level first, so the start of the line still says it.
func levelOfACutLine(trimmed string) (string, bool) {
	const opening = `{"level":"`

	if !strings.HasSuffix(trimmed, truncationNote) || !strings.HasPrefix(trimmed, opening) {
		return "", false
	}

	rest := trimmed[len(opening):]

	end := strings.IndexByte(rest, '"')
	if end <= 0 {
		return "", false
	}

	level := strings.ToUpper(rest[:end])
	if _, known := severity[level]; !known {
		return "", false
	}

	return level, true
}

// structured is the union of the two object-shaped messages summarised here:
// the request log from GoFr's HTTP middleware, and the query log from its SQL
// datasource. Both are frequent enough that leaving them as raw JSON would make
// a log viewer unreadable for the two things most worth reading.
//
// They are not the only objects it logs. A handler that failed is one, with its
// trace inside; it keeps its JSON and still has the trace read out of it, which
// is why TraceID is looked for whatever the shape.
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
// mentions if it mentions one, and the request when it is a request log.
//
// A string is used as it is. A recognised object becomes a readable summary. An
// unrecognised one keeps its JSON: wrong but complete beats a guess that drops
// the field somebody needed.
func messageText(raw json.RawMessage) (text, traceID string, asked requestLine) {
	if len(raw) == 0 {
		return "", "", requestLine{}
	}

	if err := json.Unmarshal(raw, &text); err == nil {
		return text, "", requestLine{}
	}

	var s structured
	if err := json.Unmarshal(raw, &s); err != nil {
		return strings.TrimSpace(string(raw)), "", requestLine{}
	}

	switch {
	case s.Method != "":
		// GoFr reports response_time in microseconds.
		line := fmt.Sprintf("%s %s %d %s", s.Method, s.URI, s.Response, micros(s.ResponseTime))
		if s.IP != "" {
			line += " from " + s.IP
		}

		return line, s.TraceID, requestLine{method: s.Method, uri: s.URI, status: s.Response}
	case s.Query != "":
		return fmt.Sprintf("%s %s %s", s.Type, micros(s.Duration), collapse(s.Query)), s.TraceID, requestLine{}
	default:
		return strings.TrimSpace(string(raw)), s.TraceID, requestLine{}
	}
}

// withoutValues is a line as the framework wrote it, less the values of the
// statement it logs.
//
// GoFr logs every SQL statement at DEBUG with its arguments, and this
// application runs it at DEBUG always and applies the level on the way out - so
// choosing DEBUG, which the Settings screen offers, sent every value every
// statement carried to the console: a second factor's secret as it was
// enrolled, the directory's bind password as it was saved, both in clear text
// without SECRET_KEY, and everybody's hours and notes. The console is the
// container's log or the journal, read by whoever runs the machine.
//
// Each value is replaced by its kind, so the line still says what ran and with
// how many values of which sort, which is what a query log is for. Only a query
// log is touched; every other line passes as the framework wrote it. Without
// capture there is nothing here to do this, and the framework then runs at the
// configured level rather than at DEBUG.
func withoutValues(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") || !strings.Contains(trimmed, `"args"`) {
		return line
	}

	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &outer); err != nil {
		return line
	}

	var message map[string]json.RawMessage
	if err := json.Unmarshal(outer["message"], &message); err != nil {
		return line
	}

	var args []json.RawMessage
	if _, isQuery := message["query"]; !isQuery || json.Unmarshal(message["args"], &args) != nil {
		return line
	}

	kinds := make([]string, len(args))
	for i, arg := range args {
		kinds[i] = kindOf(arg)
	}

	message["args"] = plainJSON(kinds)
	outer["message"] = plainJSON(message)

	return string(plainJSON(outer))
}

// plainJSON encodes without escaping <, > and &, which json.Marshal does for the
// sake of HTML: a statement comparing with < would otherwise reach the console
// as an escape sequence. It cannot fail on what it is given here - strings and
// raw JSON that has just been decoded.
func plainJSON(v any) json.RawMessage {
	var b bytes.Buffer

	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(v)

	return bytes.TrimRight(b.Bytes(), "\n")
}

// kindOf names what sort of JSON value a raw one is, without its content.
func kindOf(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "<empty>"
	}

	switch trimmed[0] {
	case '"':
		return "<string>"
	case 't', 'f':
		return "<bool>"
	case 'n':
		return "<null>"
	case '[':
		return "<array>"
	case '{':
		return "<object>"
	default:
		return "<number>"
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
