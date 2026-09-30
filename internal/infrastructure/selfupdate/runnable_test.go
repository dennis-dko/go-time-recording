package selfupdate

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

// A download that cannot be started keeps the reason it could not.
//
// The refusal was written with %v, so the words survived and the error did not:
// a caller asking whether the file was simply not there got no for an answer.
func TestADownloadThatCannotStartKeepsItsCause(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-downloaded"+exeSuffix())

	err := runnable(context.Background(), missing, "v9.9.9")
	if err == nil {
		t.Fatal("a file that is not there was taken as runnable")
	}

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refusal lost its cause: %v", err)
	}
}
