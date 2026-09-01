package rest

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"time"
	"unicode"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
	"github.com/dennis-dko/go-time-recording/internal/pkg/document"
)

// pdfContentType is what a browser needs to be told before it will hand the
// file to a reader rather than show it as text.
const pdfContentType = "application/pdf"

// What one request may contain.
//
// Nothing here is looked up, so nothing here is bounded by the size of anything
// real - the whole document is described by the caller. LimitRequestBody caps
// the request at two megabytes, which stops the outrageous case; these numbers
// stop the merely unreasonable one, where a body inside the cap still asks for
// a document nobody wants and a lot of work to build it.
//
// They are chosen from what the screens actually send. The longest evaluation
// is a year of daily figures, which is 366 rows; the widest table on any of
// these screens has four columns; and a chart drawn at twice its size on screen
// comes to a few hundred kilobytes.
const (
	maxSections   = 8
	maxRows       = 2000
	maxColumns    = 12
	maxSummary    = 32
	maxTextLength = 500
	maxChartBytes = 1 << 20
	chartDataURI  = "data:image/png;base64,"
)

// DocumentHandler turns what an evaluation screen is showing into a PDF.
//
// It reads nothing. Every word and every figure in the answer arrived in the
// request, because the charts are drawn in the browser and what was asked for
// is the chart as chosen and as shown - see the document package for why that
// decides the rest.
//
// What it still needs a session for: the document is a file this installation
// puts its name on, and an endpoint that will lay out anybody's text under that
// name, for anybody, is a document nobody should trust. So it is behind the
// same door as every other screen - and behind nothing more, because there is
// no data here to be allowed or refused.
type DocumentHandler struct {
	authz *Authorizer

	// instanceName is what the foot of every page calls this installation.
	instanceName func(c *gofr.Context) string
}

// NewDocumentHandler creates the handler.
func NewDocumentHandler(authz *Authorizer, instanceName func(c *gofr.Context) string) *DocumentHandler {
	return &DocumentHandler{authz: authz, instanceName: instanceName}
}

// DocumentTableRequest is one table as the screen showed it.
type DocumentTableRequest struct {
	Columns []string   `json:"columns"`
	Numeric []bool     `json:"numeric"`
	Rows    [][]string `json:"rows"`
}

// DocumentSectionRequest is one heading, with a chart and a table under it.
type DocumentSectionRequest struct {
	Heading string `json:"heading"`
	Caption string `json:"caption"`

	// Chart is a data URI holding a PNG, which is what a canvas produces.
	Chart string `json:"chart"`

	Table *DocumentTableRequest `json:"table"`
}

// DocumentRequest is a whole evaluation, as it stands on the screen.
type DocumentRequest struct {
	Title    string                   `json:"title"`
	Subtitle string                   `json:"subtitle"`
	Sections []DocumentSectionRequest `json:"sections"`
	Summary  []document.Line          `json:"summary"`

	// Colours is what the screen is drawn in, so the document is drawn in it
	// too. Absent falls back to a plain document.
	Colours document.Palette `json:"colours"`
}

// Export handles POST /api/v1/exports/document.
//
// No filename is set here. GoFr's responder owns the headers and keeps its
// writer private, so Content-Disposition is not reachable from a handler; the
// interface names the file when it saves it - the same arrangement the
// spreadsheet exports are under.
func (h *DocumentHandler) Export(c *gofr.Context) (any, error) {
	var req DocumentRequest

	principal, err := h.authz.Principal(c)
	if err != nil {
		return nil, err
	}

	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	doc, err := req.document()
	if err != nil {
		return nil, toHTTPError(err)
	}

	doc.Language = language(c)
	doc.Footer = strings.TrimSpace(h.instanceName(c))
	if principal != nil && principal.User != nil {
		doc.Footer = strings.TrimSpace(doc.Footer + "  ·  " + principal.User.Name)
	}

	doc.Written = time.Now()

	pdf, err := document.Write(doc)
	if err != nil {
		return nil, toHTTPError(apperror.Internal(err))
	}

	return response.File{Content: pdf, ContentType: pdfContentType}, nil
}

// document checks the request over and turns it into what the writer takes.
//
// Every string is trimmed of control characters on the way through. They cannot
// hurt a PDF - the writer escapes what it writes - but a heading with a newline
// in it lays out as a heading somebody clearly did not type, and refusing it
// would be refusing a document over a stray character in a project name.
func (r DocumentRequest) document() (document.Document, error) {
	if strings.TrimSpace(r.Title) == "" {
		return document.Document{}, apperror.InvalidFields("title")
	}

	if len(r.Sections) > maxSections {
		return document.Document{}, apperror.Invalidf(
			"a document has at most %d sections; this one has %d",
			maxSections, len(r.Sections)).WithCode("documentTooLong")
	}

	if len(r.Summary) > maxSummary {
		return document.Document{}, apperror.Invalidf(
			"a document has at most %d summary lines; this one has %d",
			maxSummary, len(r.Summary)).WithCode("documentTooLong")
	}

	doc := document.Document{
		Title:    plain(r.Title),
		Subtitle: plain(r.Subtitle),
		Colours:  r.Colours,
		Sections: make([]document.Section, 0, len(r.Sections)),
		Summary:  make([]document.Line, 0, len(r.Summary)),
	}

	for _, line := range r.Summary {
		doc.Summary = append(doc.Summary,
			document.Line{Label: plain(line.Label), Value: plain(line.Value)})
	}

	for _, section := range r.Sections {
		converted, err := section.section()
		if err != nil {
			return document.Document{}, err
		}

		doc.Sections = append(doc.Sections, converted)
	}

	return doc, nil
}

// section checks one part of the document over.
func (s DocumentSectionRequest) section() (document.Section, error) {
	chart, err := decodeChart(s.Chart)
	if err != nil {
		return document.Section{}, err
	}

	out := document.Section{
		Heading: plain(s.Heading),
		Caption: plain(s.Caption),
		Chart:   chart,
	}

	if s.Table == nil {
		return out, nil
	}

	table, err := s.Table.table()
	if err != nil {
		return document.Section{}, err
	}

	out.Table = &table

	return out, nil
}

// table checks one set of figures over.
func (t DocumentTableRequest) table() (document.Table, error) {
	if len(t.Columns) > maxColumns {
		return document.Table{}, apperror.Invalidf(
			"a table has at most %d columns; this one has %d",
			maxColumns, len(t.Columns)).WithCode("documentTooLong")
	}

	if len(t.Rows) > maxRows {
		return document.Table{}, apperror.Invalidf(
			"a table has at most %d rows; this one has %d",
			maxRows, len(t.Rows)).WithCode("documentTooLong")
	}

	table := document.Table{
		Columns: make([]string, 0, len(t.Columns)),
		Numeric: t.Numeric,
		Rows:    make([][]string, 0, len(t.Rows)),
	}

	for _, column := range t.Columns {
		table.Columns = append(table.Columns, plain(column))
	}

	for _, row := range t.Rows {
		cells := make([]string, 0, len(row))

		for _, cell := range row {
			cells = append(cells, plain(cell))
		}

		table.Rows = append(table.Rows, cells)
	}

	return table, nil
}

// decodeChart reads the picture out of the data URI the canvas produced.
//
// Only PNG, and only as a data URI: a URL would be this server fetching
// whatever address a request named, which is a different and much worse thing
// than laying out a picture somebody sent.
func decodeChart(chart string) ([]byte, error) {
	chart = strings.TrimSpace(chart)
	if chart == "" {
		return nil, nil
	}

	encoded, found := strings.CutPrefix(chart, chartDataURI)
	if !found {
		return nil, apperror.Invalidf("a chart must be a PNG data URI").
			WithCode("chartNotAPicture")
	}

	picture, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, apperror.Invalidf("the chart is not readable: %v", err).
			WithCode("chartNotAPicture")
	}

	if len(picture) > maxChartBytes {
		return nil, apperror.Invalidf("a chart is at most %d bytes; this one is %d",
			maxChartBytes, len(picture)).WithCode("documentTooLong")
	}

	// And how large it is once read, which the byte count does not say. A PNG
	// compresses by orders of magnitude, and a canvas produces the kind that costs
	// most to read - colour type 6, whose alpha has to be split from the colour,
	// which means inflating the whole image. Measured: 8000 square arrives as
	// 260 KB, inside the limit above, and allocates 782 MB.
	//
	// Refused here as well as in the document package, and for the reason the logo
	// needed both: the package refuses so nothing can be made to allocate that
	// much whatever calls it, and this refuses so the person who asked for the
	// report is told why, in their own language, instead of meeting whatever a
	// failed render turns into.
	// png rather than image: the prefix above already required a PNG, and the
	// generic decoder would answer only because some other package in this binary
	// happens to have registered the format.
	config, err := png.DecodeConfig(bytes.NewReader(picture))
	if err != nil {
		return nil, apperror.Invalidf("the chart is not readable: %v", err).
			WithCode("chartNotAPicture")
	}

	if config.Width*config.Height > document.MaxChartPixels {
		return nil, apperror.Invalidf(
			"a chart is at most %d megapixels; this one is %dx%d",
			document.MaxChartPixels>>20, config.Width, config.Height).
			WithCode("chartTooManyPixels", document.MaxChartPixels>>20)
	}

	return picture, nil
}

// plain reduces a value to something that can be set on a page: no control
// characters, no runaway length.
func plain(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, value)

	cleaned = strings.TrimSpace(cleaned)

	if runes := []rune(cleaned); len(runes) > maxTextLength {
		return string(runes[:maxTextLength]) + "…"
	}

	return cleaned
}
