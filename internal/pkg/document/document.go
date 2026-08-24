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

import (
	"bytes"
	"fmt"
	"image/png"
	"strconv"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// The page, in millimetres, and the room left around it.
//
// A4 portrait: this is filed and printed in the countries this application is
// written for, and a table of hours has no need of landscape.
const (
	pageWidth   = 210.0
	pageMargin  = 18.0
	contentWide = pageWidth - 2*pageMargin

	// Enough to sit under a heading without touching it, and to keep a row of
	// figures readable at a glance.
	lineHeight = 6.0
	rowHeight  = 6.5
)

// Type sizes. One family throughout, in two weights: the document is a record,
// not a design.
const (
	titleSize   = 18.0
	headingSize = 12.0
	bodySize    = 9.5
	captionSize = 8.5
)

// Table is a set of figures as the screen showed them.
//
// Numeric marks the columns to set flush right, and comes from the interface
// because the interface already knows: its own headings carry a class saying
// so. Working it out here - by trying to parse every cell - would get it wrong
// for exactly the columns that matter, the ones where a row reads "-" because
// nothing was booked.
type Table struct {
	Columns []string   `json:"columns"`
	Numeric []bool     `json:"numeric"`
	Rows    [][]string `json:"rows"`
}

// Section is one part of a document: a heading, optionally a chart, optionally
// a table. Both are optional because the three evaluations differ - the
// statistics screen is two charts and no table, and overtime is neither.
type Section struct {
	Heading string
	Caption string

	// Chart is a PNG as the browser drew it. Empty means this section has none.
	Chart []byte

	Table *Table
}

// Line is one figure with its name, for the summary at the end.
type Line struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Document is a whole evaluation.
type Document struct {
	Title    string
	Subtitle string
	Sections []Section
	Summary  []Line

	// Footer names the installation and the moment. It is set on this side,
	// because it is the one thing in here that is not the screen's to say.
	Footer string

	// Written is the moment the document says it was made. Given by the caller
	// rather than read from the clock here, so that the footer and the file's
	// own creation date agree with each other and with whatever the caller has
	// already recorded about the request.
	Written time.Time
}

// Write lays the document out and returns the PDF.
func Write(doc Document) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")

	// The Go fonts rather than fpdf's built-in ones. Those are Latin-1: they
	// cover German and turn everything else into question marks without saying
	// so, and a project name is whatever somebody typed. These arrive with
	// golang.org/x/image, which this module already depends on, so they cost no
	// new dependency and no font file to ship.
	pdf.AddUTF8FontFromBytes("go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("go", "B", gobold.TTF)

	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, pageMargin)
	pdf.SetTitle(doc.Title, true)
	pdf.SetCreationDate(doc.Written)

	writeFooter(pdf, doc.Footer, doc.Written)

	pdf.AddPage()
	writeHeading(pdf, doc)

	for i, section := range doc.Sections {
		if err := writeSection(pdf, i, section); err != nil {
			return nil, err
		}
	}

	writeSummary(pdf, doc.Summary)

	var out bytes.Buffer

	if err := pdf.Output(&out); err != nil {
		return nil, fmt.Errorf("writing the document: %w", err)
	}

	return out.Bytes(), nil
}

// writeFooter puts the installation and the moment at the foot of every page.
//
// Registered before the first page rather than drawn after the last: a document
// that runs to three pages has to say on all three where it came from, and a
// page number is only of use on the page it counts.
func writeFooter(pdf *fpdf.Fpdf, footer string, written time.Time) {
	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		pdf.SetFont("go", "", captionSize)
		pdf.SetTextColor(120, 120, 120)

		left := strings.TrimSpace(footer)
		if !written.IsZero() {
			left = strings.TrimSpace(left + "  ·  " + written.Format("2006-01-02 15:04"))
		}

		pdf.CellFormat(contentWide/2, 5, left, "", 0, "L", false, 0, "")
		pdf.CellFormat(contentWide/2, 5, strconv.Itoa(pdf.PageNo()), "", 0, "R", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})
}

// writeHeading writes the title and the period it covers.
func writeHeading(pdf *fpdf.Fpdf, doc Document) {
	pdf.SetFont("go", "B", titleSize)
	pdf.MultiCell(contentWide, lineHeight+3, doc.Title, "", "L", false)

	if subtitle := strings.TrimSpace(doc.Subtitle); subtitle != "" {
		pdf.SetFont("go", "", bodySize)
		pdf.SetTextColor(90, 90, 90)
		pdf.MultiCell(contentWide, lineHeight, subtitle, "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}

	pdf.Ln(4)
}

// writeSection writes one heading, its chart and its table.
//
// The number is the section's place in the document, and is there only to name
// the chart: fpdf keeps registered images in a map, so two sections whose
// headings happen to match would have shared one picture - and the statistics
// screen sends two charts at once.
func writeSection(pdf *fpdf.Fpdf, number int, section Section) error {
	if heading := strings.TrimSpace(section.Heading); heading != "" {
		pdf.SetFont("go", "B", headingSize)
		pdf.MultiCell(contentWide, lineHeight, heading, "", "L", false)
		pdf.Ln(1)
	}

	if caption := strings.TrimSpace(section.Caption); caption != "" {
		pdf.SetFont("go", "", captionSize)
		pdf.SetTextColor(90, 90, 90)
		pdf.MultiCell(contentWide, lineHeight-1, caption, "", "L", false)
		pdf.SetTextColor(0, 0, 0)
		pdf.Ln(1)
	}

	if len(section.Chart) > 0 {
		if err := writeChart(pdf, number, section.Chart); err != nil {
			return err
		}
	}

	if section.Table != nil {
		writeTable(pdf, *section.Table)
	}

	pdf.Ln(4)

	return nil
}

// writeChart places the picture the browser drew, at the width of the text.
//
// Its own proportions rather than a fixed height, so a chart of thirty projects
// is not squashed into the space three needed. One that will not fit under what
// is already on the page moves to the next one whole: half a chart at the foot
// of a page says less than a gap does.
func writeChart(pdf *fpdf.Fpdf, number int, chart []byte) error {
	config, err := png.DecodeConfig(bytes.NewReader(chart))
	if err != nil {
		return fmt.Errorf("the chart is not a readable PNG: %w", err)
	}

	if config.Width <= 0 || config.Height <= 0 {
		return fmt.Errorf("the chart has no size: %dx%d", config.Width, config.Height)
	}

	name := "chart-" + strconv.Itoa(number)

	pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "PNG"},
		bytes.NewReader(chart))

	if pdf.Err() {
		return fmt.Errorf("the chart could not be placed: %s", pdf.Error())
	}

	height := contentWide * float64(config.Height) / float64(config.Width)

	_, pageHeight := pdf.GetPageSize()
	if pdf.GetY()+height > pageHeight-pageMargin {
		pdf.AddPage()
	}

	pdf.ImageOptions(name, pageMargin, pdf.GetY(), contentWide, height, true,
		fpdf.ImageOptions{ImageType: "PNG"}, 0, "")

	pdf.Ln(3)

	return nil
}

// writeTable sets the figures out in columns.
//
// The heading row is repeated on every page the table runs onto, because a
// column of numbers with nothing above it is a column nobody can read.
func writeTable(pdf *fpdf.Fpdf, table Table) {
	if len(table.Columns) == 0 {
		return
	}

	widths := columnWidths(pdf, table)

	header := func() {
		pdf.SetFont("go", "B", bodySize)
		pdf.SetFillColor(238, 238, 238)

		for i, column := range table.Columns {
			pdf.CellFormat(widths[i], rowHeight, fit(pdf, column, widths[i]), "B", 0,
				alignOf(table, i), true, 0, "")
		}

		pdf.Ln(-1)
		pdf.SetFont("go", "", bodySize)
	}

	header()

	_, pageHeight := pdf.GetPageSize()

	for _, row := range table.Rows {
		if pdf.GetY()+rowHeight > pageHeight-pageMargin {
			pdf.AddPage()
			header()
		}

		for i := range table.Columns {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}

			pdf.CellFormat(widths[i], rowHeight, fit(pdf, cell, widths[i]), "B", 0,
				alignOf(table, i), false, 0, "")
		}

		pdf.Ln(-1)
	}
}

// columnWidths shares the width out by what each column has to hold.
//
// Measured rather than divided equally: a table of "Period" and "Hours" reads
// badly when a date and a two-digit number are given the same room. What is
// measured is the widest thing in each column, so a column of project names
// takes the space a column of figures does not need.
func columnWidths(pdf *fpdf.Fpdf, table Table) []float64 {
	widths := make([]float64, len(table.Columns))

	pdf.SetFont("go", "B", bodySize)

	for i, column := range table.Columns {
		widths[i] = pdf.GetStringWidth(column)
	}

	pdf.SetFont("go", "", bodySize)

	for _, row := range table.Rows {
		for i := range table.Columns {
			if i >= len(row) {
				continue
			}

			if w := pdf.GetStringWidth(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	// Room to breathe on both sides of every cell.
	total := 0.0

	for i := range widths {
		widths[i] += 6
		total += widths[i]
	}

	if total <= 0 {
		return widths
	}

	// Scaled to the page: up where there is room to spare, down where there is
	// not. Down is the case that matters - one long description would otherwise
	// push the last column off the paper.
	for i := range widths {
		widths[i] = widths[i] * contentWide / total
	}

	return widths
}

// alignOf puts figures on the right and everything else on the left.
func alignOf(table Table, column int) string {
	if column < len(table.Numeric) && table.Numeric[column] {
		return "R"
	}

	return "L"
}

// fit shortens a cell that will not fit its column, ending it with an ellipsis
// so a reader can see that something was cut rather than wonder.
func fit(pdf *fpdf.Fpdf, cell string, width float64) string {
	room := width - 4
	if room <= 0 || pdf.GetStringWidth(cell) <= room {
		return cell
	}

	runes := []rune(cell)

	for len(runes) > 1 {
		runes = runes[:len(runes)-1]

		if pdf.GetStringWidth(string(runes)+"…") <= room {
			return string(runes) + "…"
		}
	}

	return string(runes)
}

// writeSummary sets out the figures that stand on their own - a total, or the
// three numbers an overtime calculation comes to.
func writeSummary(pdf *fpdf.Fpdf, summary []Line) {
	if len(summary) == 0 {
		return
	}

	pdf.Ln(2)

	for _, line := range summary {
		pdf.SetFont("go", "", bodySize)
		pdf.CellFormat(contentWide*0.6, rowHeight, line.Label, "", 0, "L", false, 0, "")
		pdf.SetFont("go", "B", bodySize)
		pdf.CellFormat(contentWide*0.4, rowHeight, line.Value, "", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}
}
