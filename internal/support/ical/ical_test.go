package ical

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func sample() Calendar {
	return Calendar{
		Name:  "Zeiterfassung",
		Stamp: time.Date(2026, time.October, 20, 9, 30, 0, 0, time.UTC),
		Events: []Event{{
			UID:         "20261019-booked@example",
			Day:         time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC),
			Summary:     "7,50 Std. gebucht",
			Description: "2,50 Std. · Portal\n5,00 Std. · Review",
		}},
	}
}

// Every line ends in a carriage return and a line feed, which is what the format
// asks for and what a strict reader refuses a feed without.
func TestEveryLineEndsTheWayTheFormatAsks(t *testing.T) {
	written := string(Write(sample()))

	if !strings.HasSuffix(written, "END:VCALENDAR\r\n") {
		t.Fatalf("the feed does not end with the calendar closed by CRLF:\n%q", written)
	}

	if bare := strings.Count(written, "\n") - strings.Count(written, "\r\n"); bare != 0 {
		t.Errorf("%d lines end in a bare line feed", bare)
	}
}

// A day is written as a whole day: a date with no time, ending the next day,
// which is how every calendar program shows something that has no hour.
func TestADayIsAWholeDay(t *testing.T) {
	written := string(Write(sample()))

	for _, want := range []string{
		"DTSTART;VALUE=DATE:20261019\r\n",
		"DTEND;VALUE=DATE:20261020\r\n",
		"DTSTAMP:20261020T093000Z\r\n",
		"UID:20261019-booked@example\r\n",
		"TRANSP:TRANSPARENT\r\n",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the feed has no %q in:\n%s", want, written)
		}
	}
}

// What somebody wrote is text, and four characters mean something else in this
// format: a comma and a semicolon separate values, a backslash escapes, and a line
// break ends the property. Each is written the way the format spells it.
func TestTextIsEscaped(t *testing.T) {
	calendar := sample()
	calendar.Events[0].Summary = `Review; Login, Teil 1 \ 2`
	calendar.Events[0].Description = "erste Zeile\r\nzweite Zeile"

	written := unfold(string(Write(calendar)))

	if want := `SUMMARY:Review\; Login\, Teil 1 \\ 2` + "\r\n"; !strings.Contains(written, want) {
		t.Errorf("the summary is not escaped, want %q in:\n%s", want, written)
	}

	if want := `DESCRIPTION:erste Zeile\nzweite Zeile` + "\r\n"; !strings.Contains(written, want) {
		t.Errorf("the line break is not escaped, want %q in:\n%s", want, written)
	}
}

// A line longer than 75 octets is folded, and never inside a character: a reader
// that unfolds it gets back exactly what was written.
//
// Every character of the description is two octets wide and the property name
// before it twelve, so each character starts on an even octet and the 75th falls
// inside one - a fold made where the octets run out, rather than where a
// character does, is caught on the first line. A sentence of mixed letters let
// that version pass, because no fold happened to land on an umlaut.
func TestALongLineIsFoldedBetweenCharacters(t *testing.T) {
	calendar := sample()
	calendar.Events[0].Description = strings.Repeat("ä", 200)

	written := string(Write(calendar))

	for _, line := range strings.Split(strings.TrimSuffix(written, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("a line of %d octets is not folded: %q", len(line), line)
		}

		if !utf8.ValidString(line) {
			t.Errorf("a fold splits a character: %q", line)
		}
	}

	if want := "DESCRIPTION:" + calendar.Events[0].Description + "\r\n"; !strings.Contains(unfold(written), want) {
		t.Errorf("unfolded, the description is not what was written")
	}
}

// Characters the format does not allow in text - every control character but the
// tab - are left out rather than written, because a reader refuses the whole feed
// over one of them.
func TestControlCharactersAreLeftOut(t *testing.T) {
	calendar := sample()
	calendar.Events[0].Summary = "Portal\x00\x07\tfertig"

	if written := string(Write(calendar)); !strings.Contains(written, "SUMMARY:Portal\tfertig\r\n") {
		t.Errorf("the control characters were written:\n%q", written)
	}
}

// unfold joins folded lines back together, the way a reader does.
func unfold(written string) string {
	return strings.ReplaceAll(written, "\r\n ", "")
}
