package spreadsheet

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// A cell may point at a shared string that does not exist, and a workbook built
// to do that imports nothing.
//
// It used to bring excelize down. There are two places excelize looks a shared
// string up and an advisory for each: GO-2026-6452 is the in-memory lookup, fixed
// in v2.11.0, and GHSA-wcg2-648h-mhxq the spilled one, where rows.go tested the
// upper bound alone - `if len(f.sharedStringItem) <= index` let -1 through to
// `f.sharedStringItem[index]`. That one was fixed upstream in f98df08 and carried
// by no release; go.mod has required a commit after it since the advisories of
// 2026-10-09, and until then the recover() in rowsOf turned the panic into a
// refusal.
//
// So the file has to be large to reach that lookup, which is why this case builds
// a big one. Shared strings go to a temporary file once that part passes
// UnzipXMLSizeLimit, which rowsOf leaves unset, so excelize derives it as
// min(UnzipSizeLimit, StreamChunkSize) = 16 MiB. Under that threshold the workbook
// takes the in-memory path; over it, one cell reading `<v>-1</v>` was enough. The
// right to try is the ordinary one: importing time entries needs
// timesheets:write:own, which every account has.
//
// Refused as unreadable or read with the broken cell empty, either is safe. What
// must not happen is a panic escaping the reader, or a row coming out of the
// broken reference.
func TestAWorkbookPointingAtANegativeSharedStringImportsNothing(t *testing.T) {
	crafted := workbookWithNegativeSharedString(t)

	t.Logf("the crafted file is %.0f KB compressed", float64(len(crafted))/1024)

	if len(crafted) > 32<<20 {
		t.Fatalf("the crafted file is %d bytes, which the endpoint would refuse on "+
			"size alone; this case has to get past that to mean anything", len(crafted))
	}

	rows, problems, err := Read(bytes.NewReader(crafted))
	if err != nil {
		t.Logf("refused with: %v", err)

		return
	}

	if len(rows) != 0 {
		t.Errorf("a cell pointing at no shared string came out as %d rows", len(rows))
	}

	t.Logf("read as %d rows and %d problems", len(rows), len(problems))
}

// And an ordinary workbook whose shared strings are large but sane still reads,
// so the guard is not simply refusing every file that takes the temporary-file
// path. Without this, a guard that turned the whole branch into an error would
// pass the case above and break every large import.
func TestALargeWorkbookWithSaneSharedStringsStillReads(t *testing.T) {
	ordinary := workbookWithSharedStrings(t, `<c r="A2" t="s"><v>0</v></c>`)

	rows, problems, err := Read(bytes.NewReader(ordinary))
	if err != nil {
		t.Fatalf("a large but valid workbook was refused: %v", err)
	}

	t.Logf("read %d rows and %d problems", len(rows), len(problems))

	// The crafted row is one shared-string cell rather than a complete time entry,
	// so it comes back as a problem rather than as a row. Either one proves what
	// this case is for: the reader reached row 2 of a workbook whose shared strings
	// were spilled to a temporary file, instead of the guard turning that whole
	// branch into an error.
	if len(rows)+len(problems) == 0 {
		t.Error("the large workbook produced neither a row nor a problem, so the " +
			"reader never reached the shared-string lookup and this case proves " +
			"nothing about the guard leaving valid files alone")
	}
}

// workbookWithNegativeSharedString is the crafted file: one cell pointing at
// shared string -1.
func workbookWithNegativeSharedString(t *testing.T) []byte {
	t.Helper()

	return workbookWithSharedStrings(t, `<c r="A2" t="s"><v>-1</v></c>`)
}

// workbookWithSharedStrings rebuilds a genuine workbook with a shared-string
// part big enough to be spilled to a temporary file, and one worksheet cell of
// the caller's choosing in the row below the heading.
//
// Built from this package's own writer rather than from a fixture, so the parts
// this case does not care about - the content types, the relationships, the
// heading row excelize needs to find a sheet at all - are whatever the current
// version of the writer produces.
func workbookWithSharedStrings(t *testing.T, cell string) []byte {
	t.Helper()

	genuine, err := Write([]Row{{
		Date:        time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		User:        "Nils",
		Hours:       2,
		Description: "ordinary",
	}})
	if err != nil {
		t.Fatal(err)
	}

	source, err := zip.NewReader(bytes.NewReader(genuine), int64(len(genuine)))
	if err != nil {
		t.Fatal(err)
	}

	out := new(bytes.Buffer)
	writer := zip.NewWriter(out)

	var sheetSwapped, stringsSwapped bool

	for _, file := range source.File {
		target, createErr := writer.Create(file.Name)
		if createErr != nil {
			t.Fatal(createErr)
		}

		switch {
		case strings.HasPrefix(file.Name, "xl/worksheets/") && !sheetSwapped:
			sheetSwapped = true

			writeSheet(t, target, cell)

		case file.Name == "xl/sharedStrings.xml":
			stringsSwapped = true

			writeSharedStrings(t, target)

		default:
			reader, openErr := file.Open()
			if openErr != nil {
				t.Fatal(openErr)
			}

			if _, err := io.Copy(target, reader); err != nil {
				t.Fatal(err)
			}

			_ = reader.Close()
		}
	}

	if !sheetSwapped {
		t.Fatal("the written workbook has no worksheet part to replace")
	}

	// The writer is what decides whether there is a shared-string part at all,
	// and the whole case rests on there being one that is large. Saying so here
	// means a writer that stopped emitting shared strings fails loudly instead of
	// turning this into a case that exercises the in-memory path and passes.
	if !stringsSwapped {
		t.Fatal("the written workbook has no xl/sharedStrings.xml, so this case " +
			"cannot reach the temporary-file lookup it is about")
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

// writeSheet writes a minimal but well-formed worksheet: a heading row the
// reader can recognise, and the caller's cell under it.
//
// Well-formed matters here in a way it does not for the zip-bomb case next door.
// That one is refused on size before anything parses it, so it can write
// fragments; this one has to be read far enough to reach a shared-string lookup.
func writeSheet(t *testing.T, to io.Writer, cell string) {
	t.Helper()

	var row strings.Builder

	row.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	row.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	row.WriteString(`<sheetData><row r="1">`)

	for i, heading := range timesheets.Headings {
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			t.Fatal(err)
		}

		row.WriteString(`<c r="` + name + `1" t="inlineStr"><is><t>` + heading + `</t></is></c>`)
	}

	row.WriteString(`</row><row r="2">` + cell + `</row></sheetData></worksheet>`)

	if _, err := io.WriteString(to, row.String()); err != nil {
		t.Fatal(err)
	}
}

// writeSharedStrings writes a shared-string part past UnzipXMLSizeLimit, so
// excelize spills it to a temporary file and looks entries up through
// getFromStringItem rather than from memory.
func writeSharedStrings(t *testing.T, to io.Writer) {
	t.Helper()

	const past = (16 << 20) + (1 << 20)

	if _, err := io.WriteString(to,
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`); err != nil {
		t.Fatal(err)
	}

	entry := `<si><t>padding</t></si>`
	block := strings.Repeat(entry, 4096)

	for written := 0; written < past; written += len(block) {
		if _, err := io.WriteString(to, block); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := io.WriteString(to, `</sst>`); err != nil {
		t.Fatal(err)
	}
}
