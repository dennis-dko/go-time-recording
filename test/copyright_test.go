package test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// The copyright year has to include this one, wherever it is written down.
//
// There is no mechanism that keeps it current, and that is the point of this
// test. Two files carry a year that a person typed: the LICENCE and the Windows
// resource that fills in the .exe's Properties tab. Nothing reads the clock for
// either, so on the first of January they are both quietly a year out - the kind
// of wrong that nobody notices for twelve months and then notices in a customer's
// file dialog.
//
// The interface's own copyright line is a different mechanism and needs no test
// here: it is written as {year} and resolved when the page is drawn, so it is
// right whenever somebody is looking at it. That is also the answer for anybody
// wondering which to use - a typed year in the footer text goes stale exactly
// like these two.
//
// This fails on 1 January. That is the intent: a build that starts a new year
// stops until the year is written in, which takes a minute and is a minute
// somebody is being told about rather than a year nobody is.
func TestTheCopyrightYearIncludesThisOne(t *testing.T) {
	year := time.Now().Year()

	for _, carrier := range []struct {
		file    string
		pattern *regexp.Regexp
	}{
		{
			file: filepath.Join("..", "LICENSE"),
			// "Copyright (c) 2025-2026 Name" or "Copyright (c) 2026 Name".
			pattern: regexp.MustCompile(`Copyright \(c\) (\d{4})(?:\s*-\s*(\d{4}))?`),
		},
		{
			file:    filepath.Join("..", "build", "versioninfo.json.in"),
			pattern: regexp.MustCompile(`Copyright \(c\) (\d{4})(?:\s*-\s*(\d{4}))?`),
		},
	} {
		t.Run(filepath.Base(carrier.file), func(t *testing.T) {
			body, err := os.ReadFile(carrier.file)
			if err != nil {
				t.Fatalf("reading %s: %v", carrier.file, err)
			}

			match := carrier.pattern.FindSubmatch(body)
			if match == nil {
				t.Fatalf("%s carries no copyright line this test can read; if the "+
					"wording changed, this test has to change with it", carrier.file)
			}

			// The later of the two where it is a range, the only one where it is not.
			written := string(match[1])
			if len(match[2]) > 0 {
				written = string(match[2])
			}

			latest, err := strconv.Atoi(written)
			if err != nil {
				t.Fatalf("%s says %q, which is not a year", carrier.file, written)
			}

			if latest < year {
				t.Errorf("%s stops at %d and it is %d - the copyright is a year "+
					"behind wherever this file is read", carrier.file, latest, year)
			}
		})
	}
}
