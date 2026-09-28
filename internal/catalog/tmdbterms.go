package catalog

// How long anything learned from TMDb is kept.
//
// TMDb's terms ask that data cached from it be refreshed within six
// months. The jobs re-ask a synopsis or a trailer of TMDb's after
// tmdbRefreshDays, as their lowest priority. That only works while a
// re-ask can happen and succeeds, so a backstop deletes whatever is
// older still, whatever became of its title.

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// tmdbRefreshDays is how long anything learned from TMDb is kept before
// it is asked again or dropped. 150 days leaves a margin inside the six
// months TMDb's terms allow.
const tmdbRefreshDays = 150

// TMDbRefreshAfter is tmdbRefreshDays as a duration.
const TMDbRefreshAfter = tmdbRefreshDays * 24 * time.Hour

// tmdbForgetDays is how old a row learned from TMDb may get before it is
// deleted, whether or not asking again has worked. It is the backstop for
// a re-ask that keeps failing, a title that has left the catalog, or a
// runner with no credentials to re-ask with, and it is still inside the
// roughly 182 days of TMDb's six months.
const tmdbForgetDays = 175

// forgetDueTMDbData deletes TMDb overviews older than overviewDays and
// trailers older than tmdbForgetDays.
//
// Neither statement looks at the catalog or at meta.tmdb, so a row goes
// on time whatever became of its title. A deleted trailer is looked up
// again the next time somebody opens the film, or by the trailer job's
// queue; a deleted overview leaves the film to OMDb's answer, or to none.
func (s *Store) forgetDueTMDbData(ctx context.Context, overviewDays int) (overviews, trailers int64, err error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM meta.synopses
		WHERE source = 'tmdb' AND fetched_at < now() - make_interval(days => $1)`, overviewDays)
	if err != nil {
		return 0, 0, fmt.Errorf("catalog: drop due tmdb overviews: %w", err)
	}
	overviews = tag.RowsAffected()
	tag, err = s.pool.Exec(ctx, `
		DELETE FROM meta.trailers
		WHERE asked_at < now() - make_interval(days => $1)`, tmdbForgetDays)
	if err != nil {
		return overviews, 0, fmt.Errorf("catalog: drop due trailers: %w", err)
	}
	return overviews, tag.RowsAffected(), nil
}

// forgetTMDbDataWhenDue runs forgetDueTMDbData every SynopsisRest for as
// long as the process does, whichever credentials it has.
//
// overviewDays is tmdbRefreshDays when there is no OMDb key, because
// then nothing asks OMDb to replace an overview and each is dropped the
// day it comes due. With a key, the synopsis job re-asks at 150 days and
// this waits until tmdbForgetDays.
func forgetTMDbDataWhenDue(ctx context.Context, store *Store, logger *slog.Logger, overviewDays int) {
	for {
		overviews, trailers, err := store.forgetDueTMDbData(ctx, overviewDays)
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Warn("drop due tmdb data", "err", err)
		case overviews > 0 || trailers > 0:
			logger.Info("dropped tmdb data past its time", "overviews", overviews, "trailers", trailers)
		}
		if !waitFor(ctx, nil, SynopsisRest) {
			return
		}
	}
}
