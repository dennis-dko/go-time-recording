// Package document writes an evaluation to PDF: what was on the screen, on a
// page somebody can file or send on.
//
// Everything in a document comes from the interface rather than from the
// database, which is the opposite of how the spreadsheet exports work and is
// deliberate.
//
// The charts are the reason. They are drawn in the browser, in SVG, by hand -
// there is no chart library, because the Content-Security-Policy allows no
// external origin - and what was asked for is the chart as chosen and as shown,
// down to which of bars, columns or pie somebody picked. Re-drawing that here
// would mean a second chart implementation in a second language, kept in step
// with the first by nothing but attention.
//
// Once the picture comes from the screen, so should the words beside it. A
// document whose chart is the screen's but whose headings were translated again
// on this side would need a dictionary saying the same things as the one in
// app.js - which is the defect this repository keeps finding, in a new place.
//
// What that costs is that this package cannot vouch for the figures it sets
// out. That is acceptable because of who is at both ends: the document is built
// from one person's own screen and handed straight back to them, so there is
// nothing in it they could not already read. Nothing is looked up, so there is
// nothing to leak.
package document
