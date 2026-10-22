package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// AccountExportService hands somebody everything held about their account - the
// right of access and of portability (GDPR articles 15 and 20) - as a ZIP with a
// JSON file a part and a note on what it is.
//
// What it reads is decided where the tables are: the database's export reads a
// statement for every table the erasure deletes from, and leaves out the secrets.
// This only packs it.
type AccountExportService struct {
	exporter repository.AccountExporter
	now      func() time.Time
}

// NewAccountExportService needs the database's own reading of the account.
func NewAccountExportService(exporter repository.AccountExporter) *AccountExportService {
	return &AccountExportService{exporter: exporter, now: time.Now}
}

// exportLeftOut is what the note says the export does not hold, and why: each is
// a way into the account, and an export ends up in a downloads folder.
var exportLeftOut = []string{
	"the password's hash",
	"the second factor's seed",
	"the hashes of personal tokens, sessions and the calendar address",
	"a passkey's key material",
}

// exportNote is the archive's own description, written as about.json.
type exportNote struct {
	Application string    `json:"application"`
	CreatedAt   time.Time `json:"createdAt"`
	Parts       []string  `json:"parts"`
	LeftOut     []string  `json:"leftOut"`
}

// Export builds the archive: a part a file, each an array of rows - an empty one
// for a part that holds nothing, which is an answer too - and about.json.
func (s *AccountExportService) Export(ctx context.Context, userID uint) ([]byte, error) {
	parts, err := s.exporter.ExportUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	created := s.now().UTC().Truncate(time.Second)

	var archive bytes.Buffer

	packer := zip.NewWriter(&archive)
	note := exportNote{Application: "Go-Time-Recording", CreatedAt: created, LeftOut: exportLeftOut}

	for _, part := range parts {
		rows := part.Rows
		if rows == nil {
			rows = []map[string]any{}
		}

		if err := writeJSON(packer, part.Name+".json", created, rows); err != nil {
			return nil, err
		}

		note.Parts = append(note.Parts, part.Name)
	}

	if err := writeJSON(packer, "about.json", created, note); err != nil {
		return nil, err
	}

	if err := packer.Close(); err != nil {
		return nil, apperror.Internal(err)
	}

	return archive.Bytes(), nil
}

// writeJSON writes one file of the archive, indented for a person to read.
func writeJSON(packer *zip.Writer, name string, modified time.Time, value any) error {
	file, err := packer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
	if err != nil {
		return apperror.Internal(fmt.Errorf("adding %s to the export: %w", name, err))
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(value); err != nil {
		return apperror.Internal(fmt.Errorf("writing %s into the export: %w", name, err))
	}

	return nil
}
