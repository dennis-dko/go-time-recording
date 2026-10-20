package ical

import (
	"strings"
	"time"
	"unicode/utf8"
)

// productID names what wrote the feed, which the format asks of every calendar.
const productID = "-//Go-Time-Recording//Calendar feed//EN"

// refresh is how often a calendar program is asked to read the feed again: an
// hour, so a day booked in the morning is beside the afternoon's appointments.
// A hint and no more - Google reads a subscription when it chooses to - but
// Outlook and Apple's calendars follow it, under both of its names.
const refresh = "PT1H"

// maxLineOctets is where the format folds a line.
const maxLineOctets = 75

// Calendar is a feed: what it is called, when it was written, and its days.
type Calendar struct {
	// Name is what the calendar program lists it as.
	Name string

	// Stamp is when the feed was written, which every event carries.
	Stamp time.Time

	Events []Event
}

// Event is one whole day in the calendar.
type Event struct {
	// UID is what a calendar program recognises the event by from one reading of
	// the feed to the next, so it has to stay the same for the same day.
	UID string

	// Day is a calendar day as the domain keeps one, midnight UTC.
	Day time.Time

	Summary     string
	Description string
}

// Write renders the calendar.
//
// Every event is transparent: it says what was recorded, it is not an
// appointment, and a day of booked hours must not make somebody look busy to
// whoever reads their free time.
func Write(calendar Calendar) []byte {
	var out strings.Builder

	add := func(line string) {
		out.WriteString(fold(line))
		out.WriteString("\r\n")
	}

	add("BEGIN:VCALENDAR")
	add("VERSION:2.0")
	add("PRODID:" + productID)
	add("CALSCALE:GREGORIAN")
	add("METHOD:PUBLISH")

	if calendar.Name != "" {
		add("NAME:" + text(calendar.Name))
		add("X-WR-CALNAME:" + text(calendar.Name))
	}

	add("REFRESH-INTERVAL;VALUE=DURATION:" + refresh)
	add("X-PUBLISHED-TTL:" + refresh)

	stamp := calendar.Stamp.UTC().Format("20060102T150405Z")

	for _, event := range calendar.Events {
		add("BEGIN:VEVENT")
		add("UID:" + text(event.UID))
		add("DTSTAMP:" + stamp)
		add("DTSTART;VALUE=DATE:" + event.Day.Format("20060102"))
		add("DTEND;VALUE=DATE:" + event.Day.AddDate(0, 0, 1).Format("20060102"))
		add("SUMMARY:" + text(event.Summary))

		if event.Description != "" {
			add("DESCRIPTION:" + text(event.Description))
		}

		add("TRANSP:TRANSPARENT")
		add("END:VEVENT")
	}

	add("END:VCALENDAR")

	return []byte(out.String())
}

// text writes a value of the format's text type.
//
// A comma and a semicolon separate values, a backslash escapes and a line break
// ends the property, so each is written the way the format spells it. Every
// other control character but the tab is left out: the format does not allow
// them in text, and a strict reader refuses the whole feed over one.
func text(value string) string {
	var out strings.Builder

	for _, r := range strings.ReplaceAll(value, "\r\n", "\n") {
		switch {
		case r == '\\':
			out.WriteString(`\\`)
		case r == ';':
			out.WriteString(`\;`)
		case r == ',':
			out.WriteString(`\,`)
		case r == '\n':
			out.WriteString(`\n`)
		case r == '\t':
			out.WriteRune(r)
		case r < 0x20 || r == 0x7f:
		default:
			out.WriteRune(r)
		}
	}

	return out.String()
}

// fold cuts a line longer than the format allows into pieces, each after the first
// starting with the space that tells a reader it continues the one before.
//
// Between characters, never inside one. The format counts octets and only asks a
// reader to cope with a split character; a description in German is where one
// would land, and a reader that does not cope shows the person a broken umlaut.
func fold(line string) string {
	if len(line) <= maxLineOctets {
		return line
	}

	var out strings.Builder

	// The first piece may be the whole width; a continuation loses one octet to its
	// leading space.
	limit := maxLineOctets

	for len(line) > limit {
		cut := limit
		for !utf8.RuneStart(line[cut]) {
			cut--
		}

		out.WriteString(line[:cut])
		out.WriteString("\r\n ")

		line = line[cut:]
		limit = maxLineOctets - 1
	}

	out.WriteString(line)

	return out.String()
}
