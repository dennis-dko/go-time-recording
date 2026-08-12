// Package test holds checks on the test environment itself.
//
// Not the application: the compose files, and whether the services they start
// are the ones a deployment runs. Nothing here needs a build tag, so it runs in
// the ordinary unit job rather than only where Docker exists - reading YAML
// needs no daemon, and a check that only runs in the slow job is a check nobody
// waits for.
package test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A backing service in the test environment is the same version a deployment
// runs.
//
// The point of testing against PostgreSQL rather than SQLite, or against a real
// collector rather than a fake, is that the thing under test is the thing that
// will be running. A version that has drifted quietly undoes that: a trace that
// renders in testing and not on the server is a version difference, and a
// version difference is the last thing anybody thinks to check.
//
// A comment used to ask for this, in deploy/compose.tracing.yaml, beside a
// version pinned inside a FROM in a Dockerfile where nothing could compare it.
// Both are gone; this is what replaced them.
func TestTheBackingServicesMatchWhatADeploymentRuns(t *testing.T) {
	deployed := imagesIn(t, filepath.Join("..", "deploy"))
	if len(deployed) == 0 {
		t.Fatal("no images found in deploy/; this test is no longer reading them")
	}

	tested := imagesIn(t, ".")
	if len(tested) == 0 {
		t.Fatal("no images found in the test compose file")
	}

	// Only the ones both sides run. The test environment also starts MySQL and a
	// directory browser, which no deployment has and which nothing here can
	// compare against - and the deployment starts the application's own image,
	// which the test environment builds from source rather than pulling.
	shared := 0

	for name, deployedTag := range deployed {
		testedTag, both := tested[name]
		if !both {
			continue
		}

		shared++

		if testedTag != deployedTag {
			t.Errorf("%s is pinned to %s here and %s in deploy/ - the test "+
				"environment is not exercising what a deployment runs",
				name, testedTag, deployedTag)
		}
	}

	// Otherwise a rename on either side would empty the comparison and pass.
	if shared < 2 {
		t.Errorf("only %d service is run by both, which is fewer than there were "+
			"when this was written - a renamed image makes this test pass by "+
			"comparing nothing", shared)
	}
}

// imagesIn collects image:tag pairs from every compose file in a directory.
//
// By text rather than by parsing the YAML: a parser would be a dependency and a
// schema to keep up with, for a line that has looked the same since compose
// files existed. Images whose tag is interpolated are skipped - the
// application's own is ${GTR_VERSION}, which is a deployment's choice and not a
// version this repository pins.
func imagesIn(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	line := regexp.MustCompile(`(?m)^\s*image:\s*([^\s#]+)`)
	found := map[string]string{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		source, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Fatalf("reading %s: %v", name, readErr)
		}

		for _, match := range line.FindAllStringSubmatch(string(source), -1) {
			reference := match[1]
			if strings.Contains(reference, "${") {
				continue
			}

			image, tag, tagged := strings.Cut(reference, ":")
			if !tagged {
				t.Errorf("%s pins %q with no tag, so it follows latest and the two "+
					"environments cannot be compared at all", name, reference)

				continue
			}

			found[image] = tag
		}
	}

	return found
}
