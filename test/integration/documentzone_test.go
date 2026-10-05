//go:build integration

package integration

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

// A document is dated in its reader's zone.
//
// The foot of every page says when it was made, and the file's own creation
// date says the same. Both were the server's clock read in the server's zone,
// with no zone named: an evaluation made at half past eight in the evening in
// Berlin, by the image, which runs in UTC, said half past six, and one made
// shortly after midnight said yesterday. The creation date is read here because
// it is the one place the moment stands uncompressed; the footer is written from
// the same value.
func TestADocumentIsDatedInItsReadersZone(t *testing.T) {
	t.Parallel()

	_, _, worker := startWithWorker(t)

	// Fourteen hours ahead of UTC, so its clock is never the server's.
	const zone = "Pacific/Kiritimati"

	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}

	worker.must(worker.api(http.MethodPut, "/me/timezone",
		map[string]string{"timezone": zone}), http.StatusOK)

	before := time.Now().In(location).Format("20060102150405")
	r := worker.api(http.MethodPost, "/exports/document", map[string]any{"title": "Evaluation"})
	after := time.Now().In(location).Format("20060102150405")

	if !accepted(r.Status) {
		t.Fatalf("the document was refused with %d: %.200q", r.Status, r.Body)
	}

	if stamp := creationDateOf(t, r.Body); stamp < before || stamp > after {
		t.Errorf("a document made between %s and %s in %s says it was made at %s", before, after, zone, stamp)
	}
}

// creationDateOf reads the moment a PDF says it was made, as the digits fpdf
// writes - a text string, which it may write in UTF-16 with a byte order mark.
func creationDateOf(t *testing.T, pdf []byte) string {
	t.Helper()

	_, rest, found := bytes.Cut(pdf, []byte("/CreationDate ("))
	if !found {
		t.Fatalf("the document names no creation date: %.200q", pdf)
	}

	raw, _, _ := bytes.Cut(rest, []byte(")"))
	raw = bytes.TrimPrefix(raw, []byte{0xfe, 0xff})
	raw = bytes.ReplaceAll(raw, []byte{0}, nil)

	stamp, ok := bytes.CutPrefix(raw, []byte("D:"))
	if !ok || len(stamp) < len("20060102150405") {
		t.Fatalf("the creation date reads %q", raw)
	}

	return string(stamp[:len("20060102150405")])
}
