package service

import (
	"context"
	"time"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// RegionSource answers which state's public holidays the installation keeps,
// empty for none. The settings answer it; a service built without one keeps
// none, which is what an installation that never chose anything keeps too.
type RegionSource func(ctx context.Context) (string, error)

// holidaysOf returns the public holidays somebody keeps between two days, keyed
// by their day: their own state's, or the installation's where they follow it.
//
// One place for it, because the days a balance counts and the days an absence is
// recorded on have to agree about which days are holidays.
func holidaysOf(
	ctx context.Context,
	installation RegionSource,
	user *model.User,
	from, to time.Time,
) (map[time.Time]model.Holiday, error) {
	var instance string

	if installation != nil {
		region, err := installation(ctx)
		if err != nil {
			return nil, err
		}

		instance = region
	}

	return model.HolidaysBetween(user.HolidayRegionOf(instance), from, to), nil
}
