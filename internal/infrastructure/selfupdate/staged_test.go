package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennis-dko/go-time-recording/test/tempdir"
)

// A download that could not be put in place does not stay beside the binary.
//
// A refused checksum and a file that will not run both removed what they had
// staged, and TestAFailedInstallLeavesTheOldBinaryInPlace holds the first. A
// swap that failed did not: a verified copy of the release, some fifty
// megabytes, stayed beside the binary until the next install wrote over it.
func TestAnInstallThatCannotSwapLeavesNoDownloadBehind(t *testing.T) {
	const version = "v9.9.9"

	body := workingProgram(t, version)
	sum := sha256.Sum256(body)

	source, release, _ := stagedRelease(t, version, hex.EncodeToString(sum[:]), body)

	self := filepath.Join(tempdir.New(t), "go-time-recording"+exeSuffix())
	running := []byte("the running version")

	if err := os.WriteFile(self, running, 0o755); err != nil {
		t.Fatal(err)
	}

	// Where the running binary is moved aside, a directory with something in it:
	// it can neither be removed nor renamed onto, on either platform, so the swap
	// fails at its first step and only after the download has been checked.
	if err := os.MkdirAll(filepath.Join(self+".old", "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := source.InstallOver(context.Background(), release, self)
	if err == nil {
		t.Fatal("an install whose swap could not happen reported success")
	}

	if !strings.Contains(err.Error(), "cannot move the running binary aside") {
		t.Fatalf("the install failed before the swap, so this measures nothing: %v", err)
	}

	if _, err := os.Stat(self + ".new"); err == nil {
		t.Error("the download that could not be put in place is still lying beside the binary")
	}

	got, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("the installation has no binary at its own path: %v", err)
	}

	if string(got) != string(running) {
		t.Errorf("the path holds %q, want the version that was running", got)
	}
}
