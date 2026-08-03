package rest

import (
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/logsink"
)

// LogHandler serves the process log to the built-in administrator.
type LogHandler struct {
	sink  *logsink.Sink
	authz *Authorizer
}

// NewLogHandler creates the handler.
func NewLogHandler(sink *logsink.Sink, authz *Authorizer) *LogHandler {
	return &LogHandler{sink: sink, authz: authz}
}

// LogRecordResponse is one log line on the wire.
type LogRecordResponse struct {
	// Seq is what the client sends back as "since" to fetch only what is new.
	Seq uint64 `json:"seq"`

	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	TraceID string    `json:"traceId,omitempty"`
}

// LogResponse is a page of the log.
type LogResponse struct {
	Records []LogRecordResponse `json:"records"`

	// LastSeq is where the log ends now, whether or not the filter matched
	// anything. The client polls with this rather than with the last record it
	// received, or a filter that matches nothing would make it re-scan the
	// whole buffer every second.
	LastSeq uint64 `json:"lastSeq"`

	// Dropped is how many lines fell out of the buffer before being fetched -
	// the viewer says so rather than presenting a gap as continuity.
	Dropped uint64 `json:"dropped"`

	// Levels is every level that can appear, so the interface offers exactly
	// that set instead of a list copied by hand that drifts.
	Levels []string `json:"levels"`

	// Available reports whether capture is installed at all. Without it the
	// log is empty for a reason worth stating rather than looking broken.
	Available bool `json:"available"`
}

// maxLogPage bounds one response. The viewer polls, so a huge first page only
// delays the first thing it can show.
const maxLogPage = 1000

// defaultLogPage is what an unspecified limit means.
const defaultLogPage = 300

// Logs handles GET /api/v1/admin/logs.
//
// Query parameters:
//
//	since   only lines newer than this sequence number
//	levels  comma-separated, e.g. WARN,ERROR; absent means every level
//	search  case-insensitive substring of the message
//	limit   how many lines at most, newest kept
//
// The whole process log is readable here, which is why it is behind the
// built-in administrator: it carries email addresses, request paths and
// whatever a failing driver decided to print.
func (h *LogHandler) Logs(c *gofr.Context) (any, error) {
	if _, err := h.authz.RequireSystemAdmin(c); err != nil {
		return nil, err
	}

	response := LogResponse{
		Records:   []LogRecordResponse{},
		Levels:    logsink.Levels,
		Available: h.sink != nil,
	}

	if h.sink == nil {
		return response, nil
	}

	result := h.sink.Query(logsink.Query{
		Since:  uintParam(c, "since"),
		Levels: splitLevels(c.Param("levels")),
		Search: c.Param("search"),
		Limit:  pageLimit(c.Param("limit")),
	})

	response.LastSeq = result.LastSeq
	response.Dropped = result.Dropped

	for _, record := range result.Records {
		response.Records = append(response.Records, LogRecordResponse{
			Seq:     record.Seq,
			Time:    record.Time,
			Level:   record.Level,
			Message: record.Message,
			TraceID: record.TraceID,
		})
	}

	return response, nil
}

// uintParam reads a non-negative integer parameter, treating anything
// unparseable as absent - a client sending nonsense gets the unfiltered view
// rather than an error it cannot act on.
func uintParam(c *gofr.Context, name string) uint64 {
	value, err := strconv.ParseUint(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil {
		return 0
	}

	return value
}

// splitLevels turns "warn,error" into the levels to keep. Unknown names are
// dropped rather than rejected; an empty result means no filter, which is the
// same thing the absent parameter means.
func splitLevels(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var levels []string

	for _, part := range strings.Split(raw, ",") {
		name := strings.ToUpper(strings.TrimSpace(part))

		for _, known := range logsink.Levels {
			if name == known {
				levels = append(levels, name)

				break
			}
		}
	}

	return levels
}

func pageLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return defaultLogPage
	}

	if limit > maxLogPage {
		return maxLogPage
	}

	return limit
}
