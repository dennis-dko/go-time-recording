package test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// endsInSOnPurpose are the directory names that end in an s and may, each with
// its reason. Not a list of exceptions to grow: a new plural is renamed instead.
var endsInSOnPurpose = map[string]string{
	"configs":   "GoFr reads ./configs from the working directory, and every installation keeps its configuration there",
	"workflows": "GitHub looks for workflows in .github/workflows and nowhere else",
	"kerberos":  "a name, not a plural",
}

// Every directory is named in the singular - locale, migration, asset, image -
// as the maintainer decided on 2026-10-29, when the five plural ones were
// renamed.
//
// Read from the names themselves: one ending in an s, but not in ss - harness,
// progress - is taken for a plural unless endsInSOnPurpose says why it is not,
// so a new directory called helpers or fixtures is reported here rather than
// found standing beside the renamed ones. Irregular plurals get past it; a rule
// that needed a dictionary to apply would not be applied.
func TestEveryDirectoryIsNamedInTheSingular(t *testing.T) {
	root := ".."
	looked := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() || path == root {
			return nil
		}

		name := entry.Name()

		// Tools' directories and what builds leave behind are not the project's
		// structure. .github is the one hidden directory the project writes.
		if (strings.HasPrefix(name, ".") && name != ".github") || name == "bin" || name == "dist" {
			return filepath.SkipDir
		}

		looked++

		if _, allowed := endsInSOnPurpose[name]; allowed {
			// What is inside configs is GoFr's to name as well - its TLS cache
			// is configs/certs.
			if name == "configs" {
				return filepath.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(name, "s") && !strings.HasSuffix(name, "ss") {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s is named in the plural; this tree names its directories in the singular",
				filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	if looked < 40 {
		t.Fatalf("only %d directories were looked at; the walk has stopped reading the tree", looked)
	}
}
