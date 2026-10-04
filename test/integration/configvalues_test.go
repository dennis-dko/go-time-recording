//go:build integration

package integration

import (
	"strings"
	"testing"
)

// A setting written so it cannot be used is said at start, beside the default
// that applies instead - rather than replaced without a word, which left whoever
// set it to work out why it did not apply.
func TestASettingThatCannotBeUsedIsSaidAtStart(t *testing.T) {
	t.Parallel()

	a := start(t, "RATE_LIMIT=lots")

	if !eventually(func() bool {
		return strings.Contains(a.log(), `configuration: RATE_LIMIT is \"lots\"`) ||
			strings.Contains(a.log(), `configuration: RATE_LIMIT is "lots"`)
	}) {
		t.Errorf("RATE_LIMIT=lots fell back without a word:\n%s", truncate(a.log(), 1500))
	}
}
