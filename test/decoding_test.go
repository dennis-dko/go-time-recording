package test

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Everything that decodes compressed input has to bound what it decodes to.
//
// One audit found three of these, in three packages, and they were the same
// mistake each time: the application bounded what *arrives* and nothing bounded
// what it *becomes*. Compression ratios are not incidental to that - they are the
// whole of it, and every figure below was measured rather than estimated.
//
//   - A workbook is a zip. The import endpoint accepts 32 MB; deflate reaches
//     408:1 on the shape a worksheet actually has, so that is about 13 GB.
//   - A logo is capped at 256 KB. A flat PNG compresses about 1250:1, so 8000
//     square arrives inside the cap and decodes to 244 MB - three times per save.
//   - A chart is capped at 1 MB. A canvas produces RGBA, whose alpha has to be
//     split from the colour, which means inflating it all: 260 KB in, 782 MB out.
//
// None of the three was visible to a linter, a race detector or any suite: there
// is nothing wrong with the line, and every test that runs fast enough to keep
// uses an input small enough to hide it.
//
// So this is a list rather than a rule that could be inferred. Each call site
// below is one somebody has read and bounded, and a new one fails until it is
// either bounded and added here, or shown not to need it.
func TestEveryDecoderOfCompressedInputIsBounded(t *testing.T) {
	// Read, bounded, and why - so the next person adding one has the shape of the
	// answer rather than only the rule.
	bounded := map[string]string{
		"internal/pkg/spreadsheet/book.go": "excelize.Options{UnzipSizeLimit: maxUnzippedBytes}, 128 MB",

		"internal/pkg/imaging/imaging.go": "refuseTooManyPixels and PixelsIn, against MaxPixels (16 MP)",

		"internal/pkg/document/document.go": "MaxChartPixels (16 MP), checked on the DecodeConfig already there",

		"internal/interface/api/v1/rest/document_handler.go": "document.MaxChartPixels, so the refusal is coded and translated",
	}

	found := map[string][]int{}

	for _, root := range []string{"internal", "cmd"} {
		walk(t, filepath.Join("..", root), func(path, body string) {
			for i, line := range strings.Split(body, "\n") {
				if decodesCompressedInput.MatchString(line) {
					shown := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../"))
					found[shown] = append(found[shown], i+1)
				}
			}
		})
	}

	if len(found) == 0 {
		t.Fatal("no decoding call sites found at all; this test is reading nothing")
	}

	var files []string
	for file := range found {
		files = append(files, file)
	}

	sort.Strings(files)

	for _, file := range files {
		if _, known := bounded[file]; !known {
			at := make([]string, 0, len(found[file]))
			for _, line := range found[file] {
				at = append(at, strconv.Itoa(line))
			}

			t.Errorf("%s decodes compressed input at line(s) %s and is not on the "+
				"bounded list. What arrives says nothing about what it becomes: read "+
				"the header first (image.DecodeConfig, or the library's own size "+
				"option), refuse what is too large, and add it here with the bound "+
				"you chose and why", file, strings.Join(at, ", "))
		}
	}

	for file := range bounded {
		if _, still := found[file]; !still {
			t.Errorf("%s is on the bounded list and no longer decodes anything; drop "+
				"it, or the list stops describing the tree", file)
		}
	}

	// Named so a failure can be read without opening the file.
	if t.Failed() {
		t.Logf("bounded today: %s", strings.Join(sortedKeys(bounded), ", "))
	}
}

// The calls that turn a small input into a large one. base64 and JSON are not
// here on purpose: both are bounded by their input within a small constant, and
// every JSON body already passes through LimitRequestBody.
var decodesCompressedInput = regexp.MustCompile(
	`\b(?:image|png|jpeg|gif)\.Decode(?:Config)?\(|\b(?:zip|gzip|flate|tar)\.NewReader\(|excelize\.OpenReader\(`)

func walk(t *testing.T, root string, each func(path, body string)) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		each(path, read(t, path))

		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}
