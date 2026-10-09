package test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// The workspace runs the race detector the way CLAUDE.md says to.
//
// The detector needs cgo, which the machine this is developed on does not have,
// so both run it in a container, and every detail of that command was found by
// the command failing without it. CLAUDE.md gained the fourth, GOTOOLCHAIN=auto,
// when go.mod moved to a patch release the image's minor tag did not carry yet;
// the editor's task did not, and answered "go.mod requires go >= 1.27.1 (running
// go 1.27.0; GOTOOLCHAIN=local)" without testing a single package - measured.
// Flags may stand between -race and the packages, as -count=1 does, because both
// are compared whole below.
func TestTheWorkspaceRunsTheRaceDetectorTheWayCLAUDEmdSays(t *testing.T) {
	t.Parallel()

	documented := commandsIn(t, filepath.Join("..", "CLAUDE.md"),
		regexp.MustCompile(`(?m)^\s*(MSYS_NO_PATHCONV=1 docker run .* go test -race( -\S+)* \./\.\.\.)$`))
	tasked := commandsIn(t, filepath.Join("..", ".vscode", "tasks.json"),
		regexp.MustCompile(`"command": ("MSYS_NO_PATHCONV=1 docker run .* go test -race( -\S+)* \./\.\.\.")`))

	if len(documented) != 1 || len(tasked) != 1 {
		t.Fatalf("found %d race commands in CLAUDE.md and %d in the workspace's tasks, want one each",
			len(documented), len(tasked))
	}

	task, err := strconv.Unquote(tasked[0])
	if err != nil {
		t.Fatal(err)
	}

	// The one difference meant to be there: CLAUDE.md spells the work tree out,
	// and the task asks the shell for it.
	mount := regexp.MustCompile(`-v \S+:/app `)
	asRun := func(command string) string { return mount.ReplaceAllString(command, "-v <the work tree>:/app ") }

	if asRun(task) != asRun(documented[0]) {
		t.Errorf("the workspace runs the race detector as\n\t%s\nwhere CLAUDE.md says\n\t%s", task, documented[0])
	}
}

// commandsIn returns what the first group of a pattern finds in a file, every
// time it finds it.
func commandsIn(t *testing.T, file string, in *regexp.Regexp) []string {
	t.Helper()

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	var commands []string

	for _, match := range in.FindAllSubmatch(raw, -1) {
		commands = append(commands, string(match[1]))
	}

	return commands
}
