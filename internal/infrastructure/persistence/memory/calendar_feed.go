package memory

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

// CalendarFeedRepository is an in-memory repository.CalendarFeedRepository.
type CalendarFeedRepository struct {
	mu     sync.RWMutex
	feeds  map[uint]model.CalendarFeed
	nextID uint
}

// NewCalendarFeedRepository creates an empty one.
func NewCalendarFeedRepository() *CalendarFeedRepository {
	return &CalendarFeedRepository{feeds: make(map[uint]model.CalendarFeed)}
}

var _ repository.CalendarFeedRepository = (*CalendarFeedRepository)(nil)

func (r *CalendarFeedRepository) ForUser(_ context.Context, userID uint) (*model.CalendarFeed, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	feed, found := r.feeds[userID]
	if !found {
		return nil, apperror.NotFound("feed", strconv.FormatUint(uint64(userID), 10))
	}

	return &feed, nil
}

func (r *CalendarFeedRepository) ByHash(_ context.Context, keyHash string) (*model.CalendarFeed, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, feed := range r.feeds {
		if feed.KeyHash == keyHash {
			return &feed, nil
		}
	}

	return nil, apperror.NotFound("feed", "")
}

func (r *CalendarFeedRepository) Replace(_ context.Context, feed *model.CalendarFeed) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++

	stored := *feed
	stored.ID = r.nextID
	stored.LastReadAt = nil
	r.feeds[feed.UserID] = stored

	return nil
}

func (r *CalendarFeedRepository) Delete(_ context.Context, userID uint) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, found := r.feeds[userID]
	delete(r.feeds, userID)

	return found, nil
}

func (r *CalendarFeedRepository) TouchLastRead(_ context.Context, userID uint, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if feed, found := r.feeds[userID]; found {
		feed.LastReadAt = &at
		r.feeds[userID] = feed
	}

	return nil
}
