package service

import (
	"sort"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/spreadsheet"
)

// SheetRow is one row of a file as a preview shows it.
//
// Cells as text, in the order of the sheet's own columns. The time-entry import
// has a typed response of its own, from when it was the only one; projects and
// people share this because their previews differ only in what the columns are
// called, and three preview tables in the interface to show the same three things
// - what a row would do, and why it cannot - would be two too many.
type SheetRow struct {
	// Number is the line in the spreadsheet, so somebody can go and look. The
	// heading is row 1.
	Number int

	Cells []string

	// Problem is empty for a row that would be written, and in English otherwise -
	// which is what a log wants, and the fallback for a client that cannot do
	// better.
	Problem string

	// Code names which complaint it is and Values are what its sentence
	// interpolated, so the interface can put the same thing in the reader's
	// language.
	//
	// This used to be Problem alone, on the grounds that what is wrong with row 47
	// of somebody's file is not a fixed set of reasons that code could translate.
	// It is: there are about a dozen of them, they are all written in this package
	// and in the reader, and the preview they land in translates its headings and
	// its cells - so the one column that stayed English was the column explaining
	// why the file was refused.
	Code   string
	Values []any
}

// problemRow builds a rejected row from a complaint that names itself.
func problemRow(number int, cells []string, err error) SheetRow {
	code, values := spreadsheet.ProblemOf(err)

	return SheetRow{
		Number: number, Cells: cells, Problem: err.Error(), Code: code, Values: values,
	}
}

// SheetPlan is a whole file, understood.
type SheetPlan struct {
	// Columns are the headings the preview shows, already in the reader's
	// language, so the preview and the file they exported say the same words.
	Columns []string

	Rows []SheetRow

	Writable int
	Rejected int
}

func (p *SheetPlan) add(row SheetRow) {
	if row.Problem == "" {
		p.Writable++
	} else {
		p.Rejected++
	}

	p.Rows = append(p.Rows, row)
}

// sorted orders a plan's rows by their line in the file.
//
// The unreadable rows are collected separately from the readable ones, so without
// this the preview would list row 47 above row 3 and nobody could find anything in
// it.
func (p *SheetPlan) sorted() {
	sort.SliceStable(p.Rows, func(i, j int) bool { return p.Rows[i].Number < p.Rows[j].Number })
}

// refuseRejected is the answer to applying a plan that has rows in it that cannot
// be written.
//
// Refused as a whole rather than partly: somebody who has been shown "3 of 64
// rows are wrong" and presses import again means "I fixed them", not "do the other
// 61 and let me guess which".
func refuseRejected(plan *SheetPlan) error {
	if plan == nil || len(plan.Rows) == 0 {
		return apperror.Invalidf("there is nothing to import").WithCode("importEmpty")
	}

	if plan.Rejected > 0 {
		return apperror.Conflictf(
			"%d of %d rows cannot be imported; nothing was written",
			plan.Rejected, len(plan.Rows)).
			WithCode("importHasRejectedRows", plan.Rejected, len(plan.Rows))
	}

	return nil
}
