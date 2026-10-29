package rest

import (
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dennis-dko/go-time-recording/internal/application/v1/service"
	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// CalendarFeedPath is where a feed is read, followed by its key: the route
// RegisterRoutes gives Read, written out there as every route is. The integration
// suite reads a feed at the address built from this, which is what holds the two
// together.
const CalendarFeedPath = "/api/v1/calendar-feed/"

// calendarContentType is what a calendar program is told it is reading.
const calendarContentType = "text/calendar; charset=utf-8"

// CalendarFeedHandler serves the address somebody's calendar program reads their
// recorded days from, and lets them make it, see it and take it back.
//
// Making one asks for the right to read one's own entries, because that is what
// the address hands out. Taking it back asks for nothing beyond being signed in:
// somebody whose role lost that right is exactly who should still be able to
// withdraw an address, which reads nothing for them by then anyway.
type CalendarFeedHandler struct {
	feeds *service.CalendarFeedService
	authz *Authorizer
}

// NewCalendarFeedHandler creates the handler.
func NewCalendarFeedHandler(feeds *service.CalendarFeedService, authz *Authorizer) *CalendarFeedHandler {
	return &CalendarFeedHandler{feeds: feeds, authz: authz}
}

// CalendarFeedResponse describes somebody's feed, without its key.
type CalendarFeedResponse struct {
	// Active says whether there is a feed at all; the rest is empty when not.
	Active bool `json:"active"`

	Prefix     string     `json:"prefix,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty"`
	LastReadAt *time.Time `json:"lastReadAt,omitempty"`

	// Path is where the feed is read, its key included, relative to this
	// installation's own address. Only in the answer that created it: the key
	// exists there and nowhere else, ever again.
	Path *string `json:"path,omitempty"`

	// QR is the whole address as a code, an SVG data URI, for the phone a calendar
	// is usually on - which is where an address of this length is otherwise
	// typed. It carries the key as Path does, so it is in the same answer and
	// nowhere else.
	QR *string `json:"qr,omitempty"`
}

func newCalendarFeedResponse(feed *model.CalendarFeed, path *string) CalendarFeedResponse {
	if feed == nil {
		return CalendarFeedResponse{}
	}

	answer := CalendarFeedResponse{
		Active:    true,
		Prefix:    feed.Prefix,
		CreatedAt: momentOrNil(feed.CreatedAt),
		Path:      path,
	}

	if feed.LastReadAt != nil {
		answer.LastReadAt = momentOrNil(*feed.LastReadAt)
	}

	return answer
}

// Get handles GET /api/v1/me/calendar-feed.
func (h *CalendarFeedHandler) Get(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	feed, err := h.feeds.Feed(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return newCalendarFeedResponse(feed, nil), nil
}

// Create handles POST /api/v1/me/calendar-feed: a new address, in place of the one
// there was, whose key stops working the moment this answers.
func (h *CalendarFeedHandler) Create(c *gofr.Context) (any, error) {
	principal, err := h.authz.Require(c, model.PermTimesheetReadOwn)
	if err != nil {
		return nil, err
	}

	issued, err := h.feeds.Create(c, principal.User.ID)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// With ".ics" on the end, which no calendar program needs and some older ones
	// look for before they believe an address is a calendar. Read takes the key
	// with it or without.
	path := CalendarFeedPath + issued.Key + ".ics"

	made := newCalendarFeedResponse(issued.Feed, &path)

	// The address the page writes beside it, so the code and the text cannot
	// differ. One that failed to render costs the address nothing.
	if request := requestOf(c); request != nil {
		if code, err := qrcode.SVGDataURI(addressAsSeen(request) + path); err == nil {
			made.QR = &code
		} else {
			c.Logger.Errorf("could not render the calendar address's QR code: %v", err)
		}
	}

	return made, nil
}

// Remove handles DELETE /api/v1/me/calendar-feed.
func (h *CalendarFeedHandler) Remove(c *gofr.Context) (any, error) {
	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if err := h.feeds.Remove(c, principal.User.ID); err != nil {
		return nil, toHTTPError(err)
	}

	return map[string]string{"status": "deleted"}, nil
}

// Read handles GET /api/v1/calendar-feed/{key}, which a calendar program asks with
// nothing but the address: no session and no token, only the key in the path.
//
// Registered as a route rather than served by middleware, and that is what keeps
// the key out of two of the three places the address would otherwise be written:
// GoFr names a request's trace span and its metrics by the route's template, which
// is "{key}", and only by the path itself when no route matched. The third, the
// request log, takes the address as it was asked, and the process log hides the
// key there - see logsink.Sink.Conceal.
//
// A key that opens nothing answers 404 and says nothing more, whatever the reason.
func (h *CalendarFeedHandler) Read(c *gofr.Context) (any, error) {
	calendar, err := h.feeds.Read(c, c.PathParam("key"))
	if err != nil {
		return nil, toHTTPError(err)
	}

	return response.File{Content: calendar, ContentType: calendarContentType}, nil
}
