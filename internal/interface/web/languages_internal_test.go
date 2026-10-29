package web

import (
	"strings"
	"testing"
)

// Neither a language nor its name can leave the attribute it is written into,
// whatever they say: the tags are put together by hand, so the escaping is all
// that stands between a dictionary's words and the document around them.
func TestALanguagesNameStaysInItsAttribute(t *testing.T) {
	tags := languagesMeta([]string{"en", `x"><script>`}, func(language string) string {
		return `"><b onmouseover="alert(1)">` + language
	})

	if strings.Contains(tags, "<script>") || strings.Contains(tags, "<b ") {
		t.Errorf("markup got out of an attribute: %s", tags)
	}

	// Six quotes a tag, around its three attribute values, and none of its own
	// from the values.
	if quotes := strings.Count(tags, `"`); quotes != 6*2 {
		t.Errorf("the tags carry %d quotes, want 12 - a value's own quote was not escaped: %s", quotes, tags)
	}
}
