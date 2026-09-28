package catalog

// How long anything learned from TMDb is kept.
//
// TMDb's terms ask that data cached from it be refreshed within six
// months. The jobs re-ask a synopsis, a trailer, an id match, a backup
// poster or a person's photo of TMDb's after tmdbRefreshDays, as their
// lowest priority. That only works while a re-ask can happen and
// succeeds, so a backstop deletes whatever is older still, whatever
// became of its title or its person.

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

// tmdbForgotten is what one run of the backstop took away: overviews,
// trailers, id matches and people's photos deleted, and poster rows
// whose TMDb answer was cleared.
type tmdbForgotten struct {
	overviews, trailers, ids, people, posters int64
}

// forgetDueTMDbData deletes TMDb overviews older than overviewDays, and
// trailers, id matches and people's photos older than tmdbForgetDays. It
// also clears the TMDb answer from every poster row whose answer is that
// old.
//
// None of the statements looks at the catalog, so a row goes on time
// whatever became of its title or its person. A deleted trailer is
// looked up again by the trailer job, in its sweep or as soon as
// somebody opens the film, and a deleted photo by the people job, in its
// sweep or as soon as somebody opens a map the person is on. A deleted
// id match leaves its title unmatched, so the id matcher's next refill
// queues it afresh if search can still offer it. A deleted overview
// leaves the film to OMDb's answer, or to none.
//
// On a poster row, only what TMDb gave goes. A picture of TMDb's is set
// back to none, with the colour worked out from it; a release date of
// TMDb's goes too (released_tmdb says which dates are); and the stamp that
// says TMDb was asked is cleared, so a title with no picture returns to
// the poster fallback's queue, to be asked again when a reader meets it
// or the sweep reaches it. An address or date of OMDb's, and the status,
// which is OMDb's, are left as they are.
func (s *Store) forgetDueTMDbData(ctx context.Context, overviewDays int) (tmdbForgotten, error) {
	var gone tmdbForgotten
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM meta.synopses
		WHERE source = 'tmdb' AND fetched_at < now() - make_interval(days => $1)`, overviewDays)
	if err != nil {
		return gone, fmt.Errorf("catalog: drop due tmdb overviews: %w", err)
	}
	gone.overviews = tag.RowsAffected()
	tag, err = s.pool.Exec(ctx, `
		DELETE FROM meta.trailers
		WHERE asked_at < now() - make_interval(days => $1)`, tmdbForgetDays)
	if err != nil {
		return gone, fmt.Errorf("catalog: drop due trailers: %w", err)
	}
	gone.trailers = tag.RowsAffected()
	tag, err = s.pool.Exec(ctx, `
		DELETE FROM meta.tmdb
		WHERE asked_at < now() - make_interval(days => $1)`, tmdbForgetDays)
	if err != nil {
		return gone, fmt.Errorf("catalog: drop due tmdb ids: %w", err)
	}
	gone.ids = tag.RowsAffected()
	tag, err = s.pool.Exec(ctx, `
		DELETE FROM meta.people
		WHERE asked_at < now() - make_interval(days => $1)`, tmdbForgetDays)
	if err != nil {
		return gone, fmt.Errorf("catalog: drop due people's photos: %w", err)
	}
	gone.people = tag.RowsAffected()
	tag, err = s.pool.Exec(ctx, `
		UPDATE meta.posters
		SET poster_url    = CASE WHEN source = 'tmdb' THEN NULL ELSE poster_url END,
		    colour        = CASE WHEN source = 'tmdb' THEN NULL ELSE colour END,
		    source        = CASE WHEN source = 'tmdb' THEN NULL ELSE source END,
		    released      = CASE WHEN released_tmdb THEN NULL ELSE released END,
		    released_tmdb = false,
		    tmdb_at       = NULL
		WHERE tmdb_at < now() - make_interval(days => $1)`, tmdbForgetDays)
	if err != nil {
		return gone, fmt.Errorf("catalog: clear due tmdb posters: %w", err)
	}
	gone.posters = tag.RowsAffected()
	return gone, nil
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
		gone, err := store.forgetDueTMDbData(ctx, overviewDays)
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Warn("drop due tmdb data", "err", err)
		case gone != tmdbForgotten{}:
			logger.Info("dropped tmdb data past its time",
				"overviews", gone.overviews, "trailers", gone.trailers, "ids", gone.ids,
				"people", gone.people, "posters", gone.posters)
		}
		if !waitFor(ctx, nil, SynopsisRest) {
			return
		}
	}
}
