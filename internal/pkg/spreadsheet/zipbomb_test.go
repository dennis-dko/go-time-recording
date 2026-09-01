package spreadsheet

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// An upload's size says nothing about what it unpacks to.
//
// An .xlsx is a zip. The import endpoint accepts 32 MB, and that bound was the
// only one: excelize was opened without options, so what those 32 MB were allowed
// to become was its default of 16 GB - a number this application never chose.
//
// Measured on the shape a sheet actually has, a run of identical cells, deflate
// reaches 408:1. A 32 MB upload therefore expands to about 13 GB, which is *under*
// excelize's default and so would not have been refused by it either. Worksheet
// XML past UnzipXMLSizeLimit is written to the temporary directory rather than
// held in memory, so the cost lands on the disk - and this is run on machines
// whose disk is an SD card.
//
// The right to do it is the ordinary one: importing time entries needs
// timesheets:write:own, which every account has.
//
// The bomb is streamed into the zip writer rather than built in memory, so the
// case that proves a 128 MB expansion is refused does not itself allocate 128 MB.
func TestAWorkbookThatUnpacksTooLargeIsRefused(t *testing.T) {
	bomb := oversizedWorkbook(t, maxUnzippedBytes+(8<<20))

	t.Logf("the crafted file is %.0f KB compressed", float64(len(bomb))/1024)

	if len(bomb) > 32<<20 {
		t.Fatalf("the crafted file is %d bytes, which the endpoint would refuse on "+
			"size alone; this case has to get past that to mean anything", len(bomb))
	}

	_, _, err := Read(bytes.NewReader(bomb))
	if err == nil {
		t.Fatal("a workbook that unpacks past the limit was accepted")
	}

	t.Logf("refused with: %v", err)

	// The reason matters, not just that there was one. Without the bound this
	// file is still refused - it is unpacked in full first, all 136 MB of it, and
	// only then rejected as having no readable sheet. A case that accepted any
	// error would have passed against the very thing it exists to catch.
	if !strings.Contains(err.Error(), "unzip size exceeds") {
		t.Errorf("refused for the wrong reason: %v; the file was unpacked before "+
			"being rejected, which is what the limit is for", err)
	}
}

// And an ordinary workbook still reads, so the bound is not simply refusing
// everything.
func TestAnOrdinaryWorkbookIsStillReadAfterTheBound(t *testing.T) {
	book, err := Write([]Row{{
		Date:        time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		User:        "Nils",
		Hours:       2,
		Description: "ordinary",
	}})
	if err != nil {
		t.Fatal(err)
	}

	rows, problems, err := Read(bytes.NewReader(book))
	if err != nil {
		t.Fatalf("an ordinary workbook was refused: %v", err)
	}

	if len(rows) != 1 || len(problems) != 0 {
		t.Fatalf("read %d row(s) and %d problem(s), want 1 and 0", len(rows), len(problems))
	}
}

// oversizedWorkbook is a real workbook whose sheet has been replaced by a very
// large, very compressible one.
//
// Built from a workbook this package wrote, so every other part is genuine and
// the file is refused for its size rather than for being malformed.
func oversizedWorkbook(t *testing.T, unpacked int) []byte {
	t.Helper()

	genuine, err := Write([]Row{{
		Date: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), Hours: 1,
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

	var swapped bool

	for _, file := range source.File {
		target, createErr := writer.Create(file.Name)
		if createErr != nil {
			t.Fatal(createErr)
		}

		if strings.HasPrefix(file.Name, "xl/worksheets/") && !swapped {
			swapped = true

			writeRepeatedly(t, target, `<c r="A1" t="s"><v>0</v></c>`, unpacked)

			continue
		}

		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}

		if _, err := io.Copy(target, reader); err != nil {
			t.Fatal(err)
		}

		_ = reader.Close()
	}

	if !swapped {
		t.Fatal("the written workbook has no worksheet part to replace")
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

// writeRepeatedly streams chunk until at least total bytes have gone in, so the
// caller never holds the expanded form.
func writeRepeatedly(t *testing.T, to io.Writer, chunk string, total int) {
	t.Helper()

	block := strings.Repeat(chunk, 4096)

	for written := 0; written < total; written += len(block) {
		if _, err := io.WriteString(to, block); err != nil {
			t.Fatal(err)
		}
	}
}
