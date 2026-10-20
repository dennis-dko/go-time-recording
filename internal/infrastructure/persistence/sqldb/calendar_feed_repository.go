package sqldb

import (
	"context"
	"strconv"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
)

const calendarFeedColumns = "id, user_id, key_hash, prefix, created_at, last_read_at"

// CalendarFeedRepository keeps each person's calendar feed in a SQL database.
type CalendarFeedRepository struct {
	base
}

// NewCalendarFeedRepository creates a calendar feed repository for the given dialect.
func NewCalendarFeedRepository(db DB, dialect string) *CalendarFeedRepository {
	return &CalendarFeedRepository{base{db: db, dialect: dialect}}
}

var _ repository.CalendarFeedRepository = (*CalendarFeedRepository)(nil)

func (r *CalendarFeedRepository) ForUser(ctx context.Context, userID uint) (*model.CalendarFeed, error) {
	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT "+calendarFeedColumns+" FROM calendar_feeds WHERE user_id = ?"), userID)

	feed, err := scanCalendarFeed(row)
	if problem := problemReading(err, "feed", strconv.FormatUint(uint64(userID), 10)); problem != nil {
		return nil, problem
	}

	return feed, nil
}

func (r *CalendarFeedRepository) ByHash(ctx context.Context, keyHash string) (*model.CalendarFeed, error) {
	row := r.db.QueryRowContext(ctx,
		r.rebind("SELECT "+calendarFeedColumns+" FROM calendar_feeds WHERE key_hash = ?"), keyHash)

	feed, err := scanCalendarFeed(row)
	if problem := problemReading(err, "feed", ""); problem != nil {
		return nil, problem
	}

	return feed, nil
}

// Replace stores the feed over the one its owner had - one statement in each
// engine's own words, as a planned week is saved - and forgets when the old one
// was last read, which says nothing about the new key.
func (r *CalendarFeedRepository) Replace(ctx context.Context, feed *model.CalendarFeed) error {
	query := `INSERT INTO calendar_feeds (user_id, key_hash, prefix, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET key_hash = EXCLUDED.key_hash, prefix = EXCLUDED.prefix,
			created_at = EXCLUDED.created_at, last_read_at = NULL`

	if r.dialect == DialectMySQL {
		query = `INSERT INTO calendar_feeds (user_id, key_hash, prefix, created_at) VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE key_hash = VALUES(key_hash), prefix = VALUES(prefix),
				created_at = VALUES(created_at), last_read_at = NULL`
	}

	if _, err := r.exec(ctx, query, feed.UserID, feed.KeyHash, feed.Prefix, feed.CreatedAt); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (r *CalendarFeedRepository) Delete(ctx context.Context, userID uint) (bool, error) {
	affected, err := r.exec(ctx, "DELETE FROM calendar_feeds WHERE user_id = ?", userID)
	if err != nil {
		return false, apperror.Internal(err)
	}

	return affected > 0, nil
}

func (r *CalendarFeedRepository) TouchLastRead(ctx context.Context, userID uint, at time.Time) error {
	if _, err := r.exec(ctx, "UPDATE calendar_feeds SET last_read_at = ? WHERE user_id = ?", at, userID); err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func scanCalendarFeed(s scanner) (*model.CalendarFeed, error) {
	var (
		feed       model.CalendarFeed
		createdAt  dateTime
		lastReadAt dateTime
	)

	if err := s.Scan(&feed.ID, &feed.UserID, &feed.KeyHash, &feed.Prefix, &createdAt, &lastReadAt); err != nil {
		return nil, err
	}

	feed.CreatedAt = createdAt.Time
	feed.LastReadAt = ptr(lastReadAt)

	return &feed, nil
}
