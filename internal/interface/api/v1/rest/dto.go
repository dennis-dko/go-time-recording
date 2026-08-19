package rest

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

// Date carries a calendar day over the wire. It accepts either a bare
// "2006-01-02" or a full RFC 3339 timestamp on the way in, and always renders
// as "2006-01-02" on the way out, so clients never have to reason about the
// time-of-day component of a day-granular field.
type Date struct {
	time.Time
}

// UnmarshalJSON reads the three forms a date arrives in.
//
// A pointer receiver because it writes to the value, and this is the half of the
// pair that has to be one. See MarshalJSON for why the other half is not.
func (d *Date) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	for _, layout := range []string{time.DateOnly, time.RFC3339, time.RFC3339Nano} {
		t, err := time.Parse(layout, raw)
		if err == nil {
			d.Time = t

			return nil
		}
	}

	return apperror.Invalidf("date %q must be YYYY-MM-DD or RFC 3339", raw).WithCode("dateFormat", raw)
}

// MarshalJSON writes the day and nothing else.
//
// A value receiver, deliberately, and the only place in this package where one
// type carries both kinds.
//
// json.Marshal looks for the method on what it was handed. Every response holds
// a Date by value, so a pointer receiver here would not be found on them - and
// what happens then is quieter than an error: Date embeds time.Time, whose own
// MarshalJSON is promoted and answers instead. The field goes out as
// "2026-08-19T00:00:00Z" rather than "2026-08-19", on every date in the API, with
// nothing failing anywhere.
//
// So the rule about one receiver per type gives way to the rule encoding/json
// actually enforces.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}

	return json.Marshal(d.Format(time.DateOnly))
}

// pathID reads the ":id" path parameter as a positive integer.
func pathID(c *gofr.Context) (uint, error) {
	return parseUint(c.PathParam("id"), "id")
}

// queryUint reads an optional unsigned query parameter, returning 0 when it is
// absent so callers can treat 0 as "unfiltered".
func queryUint(c *gofr.Context, name string) (uint, error) {
	raw := strings.TrimSpace(c.Param(name))
	if raw == "" {
		return 0, nil
	}

	return parseUint(raw, name)
}

// queryInt reads an optional signed query parameter, defaulting when absent.
func queryInt(c *gofr.Context, name string, fallback int) int {
	raw := strings.TrimSpace(c.Param(name))
	if raw == "" {
		return fallback
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return v
}

// queryDate reads an optional date query parameter.
func queryDate(c *gofr.Context, name string) (*time.Time, error) {
	raw := strings.TrimSpace(c.Param(name))
	if raw == "" {
		return nil, nil //nolint:nilnil // absent parameter, not a failure
	}

	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return &t, nil
		}
	}

	return nil, apperror.InvalidFields(name)
}

func parseUint(raw, field string) (uint, error) {
	// Read at the size it is being put into, rather than at sixty-four and
	// checked afterwards.
	//
	// This returns a uint, which is sixty-four bits on everything this ships for
	// and thirty-two on something it does not - and there a value read at
	// sixty-four wraps on the way in, so an id past four billion arrives as a
	// different, valid-looking id and the request is answered about the wrong
	// row. strconv.IntSize is exactly the destination's width, so the refusal
	// happens in the reading and there is nothing left to check.
	//
	// The first attempt at this compared against math.MaxUint afterwards, which
	// is the same value as MaxUint64 on a sixty-four bit build: a comparison that
	// cannot be true, and so no bound at all. It read like a guard and was not
	// one.
	v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, strconv.IntSize)
	if err != nil || v == 0 {
		return 0, apperror.InvalidFields(field)
	}

	return uint(v), nil
}

// bind decodes the JSON request body, turning a malformed payload into a 400
// rather than letting it surface as an unexplained 500.
func bind(c *gofr.Context, target any) error {
	if err := c.Bind(target); err != nil {
		return apperror.Invalidf("request body is not valid JSON: %v", err).WithCode("bodyNotJSON")
	}

	return nil
}

// listResponse is the envelope for collection endpoints. GoFr wraps it again
// in its own {"data": ...} envelope.
type listResponse[T any] struct {
	Items      []T  `json:"items"`
	TotalCount uint `json:"totalCount"`
}
