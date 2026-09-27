package spreadsheet

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// A cell may point at a shared string that does not exist, and one of the two
// places excelize looks it up does not check for a negative index.
//
// Two advisories, one for each lookup, and only the second is this case's.
// GO-2026-6452 (CVE-2026-59162, GHSA-fx5j-qcqg-grpf) is the in-memory one, fixed
// in v2.11.0. The spilled one below is GHSA-wcg2-648h-mhxq, fixed upstream in
// f98df08 on 2026-07-28 - after v2.11.0, in no release yet, and published in no
// database - so the v2.11.0 in go.mod still panics on it. The two were read as one
// until the database began listing v2.11.0 as the fix; see
// TestExcelizeStillPanicsOnASpilledNegativeSharedString. Read in the
// dependency's own source rather than taken from the advisory, because the two
// lookups differ and only one of them is the hole. cell.go's getValueFrom does
// check - `if xlsxSI < 0 || xlsxSI >= len(d.SI)` - but it only reaches that check
// when the shared strings are in memory. When they are not, it calls
// getFromStringItem, and rows.go tests the upper bound alone:
//
//	if len(f.sharedStringItem) <= index {   // 5 <= -1 is false, so it falls through
//	        return strconv.Itoa(index)
//	}
//	offsetRange := f.sharedStringItem[index]   // and a negative index panics here
//
// So the file has to be large to be dangerous, which is why this case builds a
// big one. Shared strings go to a temporary file once that part passes
// UnzipXMLSizeLimit, which rowsOf leaves unset, so excelize derives it as
// min(UnzipSizeLimit, StreamChunkSize) = 16 MiB. Under that threshold the
// workbook takes the guarded path and nothing happens; over it, one cell reading
// `<v>-1</v>` is enough.
//
// The right to do it is the ordinary one: importing time entries needs
// timesheets:write:own, which every account has.
//
// What this costs without the guard is not a crash of the process - GoFr recovers
// a panic raised inside a handler - but the request dies as a logged panic with a
// stack trace instead of telling the person their file cannot be read. The guard
// is in rowsOf rather than here, and it is the reason this case asserts an error
// rather than asserting no panic: a recovered panic that returned nil would leave
// the caller reading an empty workbook as an empty import.
func TestAWorkbookPointingAtANegativeSharedStringIsRefused(t *testing.T) {
	crafted := workbookWithNegativeSharedString(t)

	t.Logf("the crafted file is %.0f KB compressed", float64(len(crafted))/1024)

	if len(crafted) > 32<<20 {
		t.Fatalf("the crafted file is %d bytes, which the endpoint would refuse on "+
			"size alone; this case has to get past that to mean anything", len(crafted))
	}

	rows, problems, err := Read(bytes.NewReader(crafted))
	if err == nil {
		t.Fatalf("a workbook with a negative shared-string index was accepted: "+
			"%d rows, %d problems", len(rows), len(problems))
	}

	t.Logf("refused with: %v", err)

	// The reason matters, not only that there was one. A file refused because the
	// heading row did not match would pass an err != nil check while proving
	// nothing about the lookup this case is about.
	if !errors.Is(err, ErrUnreadableWorkbook) {
		t.Errorf("refused for the wrong reason: %v; the parse was expected to give "+
			"way and be caught, not the file to be rejected on its contents", err)
	}

	if rows != nil {
		t.Errorf("the reader returned %d rows beside the error; a file that could "+
			"not be parsed has no rows to hand back", len(rows))
	}
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

// The recover() in rowsOf still has its reason, and this is what says so.
//
// GO-2026-6452 was carried in ci.yml's advisory register. On 2026-09-24 the
// database began listing v2.11.0 - the version go.mod requires - as fixed, which
// is true of that advisory: it is the in-memory lookup in cell.go. A spilled
// shared-string table goes through getFromStringItem in rows.go instead, a second
// flaw - GHSA-wcg2-648h-mhxq, fixed upstream in f98df08 after v2.11.0 and in no
// release or database yet - so it still panics. govulncheck therefore reports
// nothing, the register had to drop the entry, and nothing else would notice the
// day excelize ships that fix and the guard becomes the kind of recover()
// CLAUDE.md forbids: one with no reason left.
//
// So it asks excelize directly, with no guard of ours in the way, and fails once
// excelize stops panicking - which should be the first release carrying f98df08.
// That is the moment to look again at the guard, at the case above and at
// CLAUDE.md's paragraph about them.
func TestExcelizeStillPanicsOnASpilledNegativeSharedString(t *testing.T) {
	crafted := workbookWithNegativeSharedString(t)

	panicked := func() (panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()

		f, err := excelize.OpenReader(bytes.NewReader(crafted))
		if err != nil {
			t.Fatalf("the crafted workbook could not be opened at all, so this asks "+
				"nothing about the lookup: %v", err)
		}
		defer func() { _ = f.Close() }()

		// The rows are not the question; whether reading them panics is.
		_, _ = f.GetRows(f.GetSheetName(0))

		return false
	}()

	if !panicked {
		t.Error("excelize no longer panics on a spilled negative shared-string index, " +
			"so the recover() in rowsOf has lost the reason it was written for: look " +
			"again at it, at TestAWorkbookPointingAtANegativeSharedStringIsRefused and " +
			"at CLAUDE.md's paragraph about the one recover()")
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
