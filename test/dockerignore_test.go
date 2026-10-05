package test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Nothing git keeps out of the repository reaches an image built from the work
// tree.
//
// CI builds the image from a fresh checkout, which by construction holds none of
// what .gitignore names. A build on a developer's machine - task image, task
// stage - sends the work tree instead, and whatever lies in cmd/configs is copied
// into the image: measured, an image built beside a datasource.json carried its
// password in a layer, skipped its installer and connected to the database the
// file named, and loaded the personal .local.env GoFr reads by itself. So
// .dockerignore is .gitignore, line by line and in its order, written the way
// Docker reads a pattern.
func TestNothingGitKeepsOutReachesALocalImageBuild(t *testing.T) {
	t.Parallel()

	// The one thing a build leaves out that git does not: the repository itself.
	// It was 28 of the 32 MB a build sent, every commit changed it and so undid
	// the cache of the copy the binary is built from, and a remote written with
	// its credentials would have gone with it. Nothing reads the stamp go build
	// takes from it - the version is passed in, and the release binaries are
	// built without one.
	want := []string{".git"}

	for _, pattern := range patternsIn(t, filepath.Join("..", ".gitignore")) {
		want = append(want, asDockerPattern(pattern))
	}

	if got := patternsIn(t, filepath.Join("..", ".dockerignore")); !slices.Equal(got, want) {
		t.Errorf(".dockerignore leaves out\n\t%s\nwhere .gitignore says\n\t%s",
			strings.Join(got, "\n\t"), strings.Join(want, "\n\t"))
	}
}

// asDockerPattern writes a .gitignore pattern the way .dockerignore reads it.
// Git matches a pattern with no slash but a trailing one at any depth, Docker
// every pattern from the root of the context, so that kind gains a **/; and
// Docker has no mark for a directory, so the trailing slash goes.
func asDockerPattern(pattern string) string {
	negated := strings.HasPrefix(pattern, "!")
	pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "!"), "/")

	if strings.Contains(pattern, "/") {
		pattern = strings.TrimPrefix(pattern, "/")
	} else {
		pattern = "**/" + pattern
	}

	if negated {
		return "!" + pattern
	}

	return pattern
}

// patternsIn reads the patterns of an ignore file, leaving out its comments and
// blank lines.
func patternsIn(t *testing.T, file string) []string {
	t.Helper()

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	var patterns []string

	for line := range strings.Lines(string(raw)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}

	return patterns
}
