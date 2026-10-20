package repository

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// CalendarFeedRepository keeps the calendar feed each person may have.
type CalendarFeedRepository interface {
	// ForUser returns somebody's feed, or a not-found error when they have none.
	ForUser(ctx context.Context, userID uint) (*model.CalendarFeed, error)

	// ByHash returns the feed whose key hashes to this, or a not-found error. It
	// is what every reading of a feed goes through.
	ByHash(ctx context.Context, keyHash string) (*model.CalendarFeed, error)

	// Replace stores somebody's feed in place of the one they had, which from then
	// on reads nothing.
	Replace(ctx context.Context, feed *model.CalendarFeed) error

	// Delete removes somebody's feed, and reports whether there was one.
	Delete(ctx context.Context, userID uint) (bool, error)

	// TouchLastRead records when the feed was last read.
	TouchLastRead(ctx context.Context, userID uint, at time.Time) error
}
