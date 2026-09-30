//go:build unix

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/dennis-dko/go-time-recording/internal/infrastructure/config"
)

// A save that cannot be finished leaves the connection that was working.
//
// The file was written in place, and os.WriteFile empties a file before it
// writes: a save that failed part way - a full disk - left the working
// connection cut off in the middle, JSON that no longer loads, and the next
// start served the installer as if nothing had ever been configured. Measured on
// a 64 KB tmpfs: 4096 bytes of the file left, and LoadDatasource refusing them.
//
// A file size limit stands in for the full disk, because a test cannot fill one:
// past RLIMIT_FSIZE a write fails with EFBIG, and a Go program takes no action
// on the SIGXFSZ that comes with it, as os/signal documents. Unix only, because
// the limit is. It is the whole process's limit, so this test does not run in
// parallel and puts it back before it asserts anything.
func TestASaveThatCannotFinishLeavesTheWorkingConnection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "configs")
	path := filepath.Join(dir, "datasource.json")
	working := config.Datasource{Dialect: "postgres", Name: "gtr", Host: "db", User: "gtr", Password: "secret"}

	if err := config.SaveDatasource(path, working); err != nil {
		t.Fatal(err)
	}

	var before syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &before); err != nil {
		t.Fatal(err)
	}

	limited := before
	limited.Cur = 1024

	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Skipf("cannot set a file size limit here: %v", err)
	}

	bigger := working
	bigger.Password = strings.Repeat("x", 4096)

	err := config.SaveDatasource(path, bigger)

	if restore := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &before); restore != nil {
		t.Fatalf("putting the file size limit back: %v", restore)
	}

	if !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("the save should have failed on the file size limit, and returned %v", err)
	}

	if got, ok := config.LoadDatasource(path); !ok || got != working {
		t.Errorf("after a save that could not finish the file reads as %+v (loads: %v), "+
			"want the connection that was working", got, ok)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.Name() != "datasource.json" {
			t.Errorf("the save that could not finish left %s behind", entry.Name())
		}
	}
}
