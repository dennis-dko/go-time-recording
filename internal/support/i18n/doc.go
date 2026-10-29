// Package i18n is every language the application speaks besides English: one
// dictionary a language, in locale/<language>.json, compiled into the binary
// and read by everything that says something to somebody - the web interface,
// which fetches /i18n/<language>.json, the installer, the terminal client, the
// workbooks, the calendar feed and the foot of a PDF.
//
// English has no dictionary. It is the source, written where it is said - in the
// page's markup and in the fallback every lookup carries - so a key a dictionary
// lacks still says something true, in English. A new language is a new file with
// every key the German one has, and a line in model.SupportedLanguages; the tests
// hold the files and the list together.
//
// A dictionary maps a key to a sentence. The key says what the sentence is for
// and who says it: installer.* the installer, track.* the terminal client,
// workbook.* the spreadsheets, calendarFeed.* the calendar feed, the rest the web
// interface, whose words the others borrow where they say the same thing - and
// this package's own: language.name, number.* for how a figure is written, and
// layout.* for how a day and a time are, each a Go layout written with the
// reference day, 2 January 2006 at 15:04. Each reader's tests hold its own part
// of every dictionary to what it says. A sentence takes its values where it says
// {0}, {1} ...
package i18n
