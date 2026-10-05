package test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// The temporary directory the workspace hands Go comes with every clone.
//
// .vscode/settings.json points GOTMPDIR at a directory in the workspace rather
// than at the Taskfile's bin/gotmp, and says why. Go refuses to start when that
// directory does not exist - "go: creating work dir: ... cannot find the file
// specified", measured - and it existed on one machine, ignored there through
// .git/info/exclude: in a fresh clone every test started from the editor failed
// before compiling anything, and once somebody created the directory, what the
// tests left in it showed up as work.
func TestTheWorkspacesTemporaryDirectoryComesWithTheClone(t *testing.T) {
	t.Parallel()

	named := regexp.MustCompile(`"GOTMPDIR": "\$\{workspaceFolder\}/([^"]+)"`)
	dirs := map[string]bool{}

	for _, file := range []string{"settings.json", "launch.json", "tasks.json"} {
		raw, err := os.ReadFile(filepath.Join("..", ".vscode", file))
		if err != nil {
			t.Fatal(err)
		}

		for _, match := range named.FindAllSubmatch(raw, -1) {
			dirs[string(match[1])] = true
		}
	}

	if len(dirs) == 0 {
		t.Fatal("the workspace names no GOTMPDIR, so this case checks nothing")
	}

	ignored := patternsIn(t, filepath.Join("..", ".gitignore"))

	for dir := range dirs {
		if _, err := os.Stat(filepath.Join("..", dir, ".gitkeep")); err != nil {
			t.Errorf("%s is the workspace's GOTMPDIR and does not come with the repository: %v", dir, err)
		}

		for _, pattern := range []string{dir + "/*", "!" + dir + "/.gitkeep"} {
			if !slices.Contains(ignored, pattern) {
				t.Errorf(".gitignore does not say %q, so what the tests leave in %s shows up as work", pattern, dir)
			}
		}
	}
}
