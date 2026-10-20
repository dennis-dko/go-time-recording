// Package ical writes a calendar in the format calendar programs subscribe to,
// iCalendar (RFC 5545), for the feed somebody adds to Outlook, Google or their
// telephone to see their recorded days beside their appointments.
//
// It writes and never reads: a feed is published, and nothing here accepts a
// calendar from anybody. That is why it is this small - the format has recurrence,
// time zones, alarms and attendees, and a day somebody booked hours on needs none
// of them.
//
// Every event is a whole day. A booking is a number of hours on a calendar day
// with no time of day in it, and an absence is a day, so a date is the honest
// shape for both; a time would claim an hour nobody recorded.
package ical
