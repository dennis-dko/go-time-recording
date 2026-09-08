package test

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Nothing was comparing what the binary reads with what the operator is told.
//
// deploy/OPERATIONS.md is described in CLAUDE.md as the operational contract -
// "a change to how the binary is configured, started or shut down belongs in it
// in the same commit" - and that sentence was the whole of the mechanism. A key
// added to the code and to nothing else is invisible: the application runs, the
// default applies, and the person deploying has no way to learn the knob exists
// except by reading the source they were given a document to avoid.
//
// The idea is taken from the envcheck script in a sibling repository, where the
// same class had gone wrong. The implementation is not: that one resolves
// component constructors transitively because its variables are derived from
// them, while here they are read by name, which makes the derivation a search
// rather than an analysis.
//
// Both directions are checked, because each catches a different mistake. A
// derived key nobody documented is a knob the operator cannot find; a documented
// key nothing derives is a knob that does nothing, which is worse - it is
// followed, set, and then quietly ignored.

// configRead matches a configuration lookup by name. The three-character floor
// keeps ordinary Get calls with a short constant out of it.
var configRead = regexp.MustCompile(`\.Get(?:OrDefault)?\("([A-Z][A-Z_0-9]{2,})"`)

// assignedKey matches a variable set in an env file.
var assignedKey = regexp.MustCompile(`(?m)^([A-Z][A-Z_0-9]+)=`)

// configDocs are the places an operator looks: the configuration GoFr reads, the
// two examples a deployment is started from, and the operational contract.
var configDocs = []string{
	"cmd/configs/.env",
	"deploy/.env.example",
	"deploy/.env.binary.example",
	"deploy/OPERATIONS.md",
}

// notOurs lists the variables the example files set for somebody else to read,
// so their absence from this code is correct rather than a gap.
//
// GOFR_TELEMETRY and SHUTDOWN_GRACE_PERIOD are GoFr's own and are consumed
// inside the framework; GTR_VERSION is read by compose to choose the image tag
// and never reaches the process. Each is named here rather than matched by
// prefix, so a fourth one is a decision somebody writes down.
var notOurs = map[string]bool{
	"GOFR_TELEMETRY":        true,
	"GTR_VERSION":           true,
	"SHUTDOWN_GRACE_PERIOD": true,
}

// TestEveryConfigurationKeyIsDocumented checks the keys the code reads against
// the files an operator is pointed at.
func TestEveryConfigurationKeyIsDocumented(t *testing.T) {
	root := ".."

	derived := derivedKeys(t, root)

	// A derivation that quietly stopped matching would make this pass while
	// comparing nothing.
	if len(derived) < 20 {
		t.Fatalf("only %d configuration keys were found in the code; the search has "+
			"stopped matching the way they are read", len(derived))
	}

	var documented strings.Builder

	for _, name := range configDocs {
		documented.WriteString(read(t, filepath.Join(root, name)))
		documented.WriteString("\n")
	}

	text := documented.String()

	for _, key := range derived {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(key) + `\b`).MatchString(text) {
			t.Errorf("%s is read from the configuration and appears in none of %s. "+
				"An operator has no way to learn the knob exists", key,
				strings.Join(configDocs, ", "))
		}
	}
}

// TestEveryDocumentedKeyIsRead checks the other direction: a variable an example
// file sets, that nothing reads.
func TestEveryDocumentedKeyIsRead(t *testing.T) {
	root := ".."

	derived := map[string]bool{}
	for _, key := range derivedKeys(t, root) {
		derived[key] = true
	}

	for _, name := range []string{
		"cmd/configs/.env", "deploy/.env.example", "deploy/.env.binary.example",
	} {
		for _, m := range assignedKey.FindAllStringSubmatch(read(t, filepath.Join(root, name)), -1) {
			key := m[1]
			if derived[key] || notOurs[key] {
				continue
			}

			t.Errorf("%s sets %s and nothing in this repository reads it. Either the "+
				"code stopped reading it and the example was left behind, or it "+
				"belongs to GoFr or to compose and needs saying so", name, key)
		}
	}
}

// derivedKeys returns every configuration key the code reads, sorted the way the
// search finds them and with duplicates removed.
func derivedKeys(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	seen := map[string]bool{}

	for _, dir := range []string{"internal", "cmd"} {
		for _, path := range goFilesUnder(t, filepath.Join(root, dir)) {
			for _, m := range configRead.FindAllStringSubmatch(read(t, path), -1) {
				if seen[m[1]] {
					continue
				}

				seen[m[1]] = true

				out = append(out, m[1])
			}
		}
	}

	return out
}

// goFilesUnder returns every non-test Go file below root.
//
// Test files are left out on purpose: a case may name a key in order to set it
// for an instance it starts, and that is not the application deriving it.
func goFilesUnder(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		out = append(out, path)

		return nil
	})
	if err != nil {
		t.Fatalf("cannot walk %s: %v", root, err)
	}

	return out
}
