package test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The temporary directory the workspace hands Go is made where it is used, and
// nothing in it reaches the repository.
//
// .vscode/settings.json points GOTMPDIR at a directory in the workspace rather
// than at the Taskfile's bin/gotmp, and says why. Go refuses to start when that
// directory does not exist - "go: creating work dir: ... cannot find the file
// specified", measured - so in a fresh clone every test started from the editor
// failed before compiling anything. For a while the directory came with the
// repository as an empty .gitkeep; it is local now, like bin/, and the workspace
// makes it: when the folder is opened, because the tests the Go extension starts
// run nothing first, and before every launch and task that hands it to Go,
// because an editor that does not run tasks on its own never runs that one.
func TestTheWorkspacesTemporaryDirectoryIsMadeWhereItIsUsed(t *testing.T) {
	t.Parallel()

	var settings struct {
		TestEnv map[string]string `json:"go.testEnvVars"`
	}

	var launch struct {
		Configurations []struct {
			Name          string
			PreLaunchTask string
			Env           map[string]string
		}
	}

	var tasks struct {
		Tasks []workspaceTask
	}

	readWorkspace(t, "settings.json", &settings)
	readWorkspace(t, "launch.json", &launch)
	readWorkspace(t, "tasks.json", &tasks)

	inWorkspace := func(env map[string]string) (string, bool) {
		return strings.CutPrefix(env["GOTMPDIR"], "${workspaceFolder}/")
	}

	dirs := map[string]bool{}

	if dir, ok := inWorkspace(settings.TestEnv); ok {
		dirs[dir] = true
	}

	for _, config := range launch.Configurations {
		if dir, ok := inWorkspace(config.Env); ok {
			dirs[dir] = true

			if !madeBy(tasks.Tasks, config.PreLaunchTask, dir) {
				t.Errorf("the launch %q hands Go %s and runs nothing first that makes it", config.Name, dir)
			}
		}
	}

	for _, task := range tasks.Tasks {
		if dir, ok := inWorkspace(task.Options.Env); ok {
			dirs[dir] = true

			if !madeBy(tasks.Tasks, task.Label, dir) {
				t.Errorf("the task %q hands Go %s and depends on nothing that makes it", task.Label, dir)
			}
		}
	}

	if len(dirs) == 0 {
		t.Fatal("the workspace names no GOTMPDIR, so this case checks nothing")
	}

	ignored := patternsIn(t, filepath.Join("..", ".gitignore"))

	for _, dir := range sortedKeys(dirs) {
		opened := slices.ContainsFunc(tasks.Tasks, func(task workspaceTask) bool {
			return task.RunOptions.RunOn == "folderOpen" && madeBy(tasks.Tasks, task.Label, dir)
		})

		if !opened {
			t.Errorf("no task makes %s when the folder is opened, so in a fresh clone the tests "+
				"the Go extension starts fail before compiling", dir)
		}

		if !slices.Contains(ignored, dir+"/") {
			t.Errorf(".gitignore does not say %q, so what the tests leave in %s shows up as work", dir+"/", dir)
		}

		for _, pattern := range ignored {
			if strings.HasPrefix(pattern, "!"+dir+"/") {
				t.Errorf(".gitignore takes %s back into the repository; %s is local, like bin/, "+
					"and the workspace makes it where it is used", pattern[1:], dir)
			}
		}
	}
}

// workspaceTask is what this file reads of a task in .vscode/tasks.json.
type workspaceTask struct {
	Label     string
	Command   string
	DependsOn taskLabels

	// What the task runs instead on one platform.
	Windows, Linux, OSX struct{ Command string }

	RunOptions struct{ RunOn string }
	Options    struct{ Env map[string]string }
}

// makes reports whether the task makes dir on every platform it runs on.
func (task workspaceTask) makes(dir string) bool {
	commands := []string{task.Command}

	for _, instead := range []string{task.Windows.Command, task.Linux.Command, task.OSX.Command} {
		if instead != "" {
			commands = append(commands, instead)
		}
	}

	return !slices.ContainsFunc(commands, func(command string) bool {
		return !makesADirectory.MatchString(command) || !strings.Contains(command, dir)
	})
}

// makesADirectory is how the shells a task can run in make a directory that may
// already be there.
var makesADirectory = regexp.MustCompile(`\bmkdir -p\b|\bNew-Item -ItemType Directory -Force\b`)

// taskLabels is what a task depends on: one task named as a string, or several
// as a list.
type taskLabels []string

// UnmarshalJSON takes either shape.
func (labels *taskLabels) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*labels = taskLabels{one}

		return nil
	}

	return json.Unmarshal(raw, (*[]string)(labels))
}

// madeBy reports whether running the task labelled so makes dir, by itself or
// through what it depends on.
func madeBy(tasks []workspaceTask, label, dir string) bool {
	seen := map[string]bool{}

	var reaches func(label string) bool
	reaches = func(label string) bool {
		if seen[label] {
			return false
		}

		seen[label] = true

		return slices.ContainsFunc(tasks, func(task workspaceTask) bool {
			return task.Label == label && (task.makes(dir) || slices.ContainsFunc(task.DependsOn, reaches))
		})
	}

	return reaches(label)
}

// readWorkspace reads one of the files in .vscode into v. VS Code lets them
// carry comments and trailing commas, which encoding/json refuses, so both are
// taken out first - everywhere but inside a string, where a URL is not a
// comment.
func readWorkspace(t *testing.T, name string, v any) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", ".vscode", name))
	if err != nil {
		t.Fatal(err)
	}

	var plain []byte

	comma := -1 // where in plain a comma stands that nothing has followed yet

	for i := 0; i < len(raw); i++ {
		switch c := raw[i]; {
		case c == '"':
			end := i + 1
			for end < len(raw) && raw[end] != '"' {
				if raw[end] == '\\' {
					end++
				}

				end++
			}

			plain = append(plain, raw[i:min(end+1, len(raw))]...)
			i, comma = end, -1

		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i+1 < len(raw) && raw[i+1] != '\n' {
				i++
			}

		case c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			end := bytes.Index(raw[i+2:], []byte("*/"))
			if end < 0 {
				t.Fatalf("%s: a comment is never closed", name)
			}

			i += end + 3

		case c == '}' || c == ']':
			if comma >= 0 {
				plain[comma] = ' '
			}

			plain, comma = append(plain, c), -1

		case c == ',':
			comma = len(plain)
			plain = append(plain, c)

		default:
			plain = append(plain, c)

			if !strings.ContainsRune(" \t\r\n", rune(c)) {
				comma = -1
			}
		}
	}

	if err := json.Unmarshal(plain, v); err != nil {
		t.Fatalf("reading %s: %v", name, err)
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
