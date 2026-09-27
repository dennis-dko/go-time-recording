// Package spreadsheet writes and reads the workbooks the application exchanges.
//
// One package for both directions on purpose: the column order, the headings and
// how a date and an hour figure are written are the same knowledge either way, and
// an exporter and an importer that each held their own copy would drift until a
// file this application wrote could no longer be read back by it.
//
// Real .xlsx rather than comma-separated text. A CSV is what Excel mangles: the
// separator depends on the machine's locale, dates are re-interpreted on opening,
// and a description containing a semicolon quietly becomes two columns. None of
// that is recoverable at the point somebody has already saved over the file.
package spreadsheet
