package web

import (
	"sync"

	"github.com/dennis-dko/go-time-recording/internal/pkg/imaging"
)

// icons converts logos and remembers what it converted.
//
// Almost nothing reaches this any more: the three sizes are derived when the
// logo is saved and stored beside it, so what the tab is served is read rather
// than made. What is left for it is the installation whose logo predates that -
// there the stored icon is empty, and rather than serving a wordmark to a tab,
// the original is converted here and kept for as long as the process runs.
//
// Keyed by the fingerprint that is already in the icon's address, so a changed
// logo is a different key and there is nothing to invalidate.
type icons struct {
	mu   sync.Mutex
	key  string
	body []byte
}

// convert returns the icon for this logo, converting it at most once.
//
// A logo that cannot be converted answers nothing, and the caller serves the
// shipped mark instead: a tab with the wrong picture is a small wrong thing, and
// a tab with no picture is what this whole exercise was about.
func (c *icons) convert(logo string, decoded []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fingerprint(logo)

	if c.key == key {
		return c.body
	}

	converted, err := imaging.ToIcon(decoded)
	if err != nil {
		c.key, c.body = key, nil

		return nil
	}

	c.key, c.body = key, converted

	return converted
}
