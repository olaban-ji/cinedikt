package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cinedikt/internal/catalog"
	"cinedikt/internal/tmdb"
	"cinedikt/internal/trailer"
)

// trailerBudget is how long the first open of a film waits on its
// trailer lookup. Past that the row says there is none this time, and
// the next open asks again.
const trailerBudget = 4 * time.Second

// TrailerLookup is what a trailer needs from TMDb: the id for a title,
// and that movie's clips.
type TrailerLookup interface {
	FindByIMDb(ctx context.Context, imdbID string) (tmdb.Found, error)
	trailer.Lister
}

// TrailerStore is where answers are read and kept.
type TrailerStore interface {
	Trailer(ctx context.Context, tconst string) (catalog.TrailerRow, error)
	KeepTrailer(ctx context.Context, tconst, key string) error
	KeepTMDbFind(ctx context.Context, tconst string, got tmdb.Found) error
}

// WithTrailers answers GET /trailers/{id}. lookup may be nil when there
// are no TMDb credentials: stored answers are still served, and anything
// else is "no trailer". check is YouTube's oEmbed, shared with the
// trailer job; nil builds one for the server alone.
func (s *CatalogServer) WithTrailers(lookup TrailerLookup, store TrailerStore, check trailer.Checker) {
	s.trailers = lookup
	s.trailerStore = store
	if check == nil {
		check = trailer.NewOEmbed()
	}
	s.oembed = check
}

// trailerAnswer is the body: a YouTube key, or null for none.
type trailerAnswer struct {
	Key *string `json:"key"`
}

// trailerFor is GET /trailers/{id}: the YouTube trailer a film plays in
// place, or null when there is none that can be embedded.
//
// A stored answer is used when there is one. Otherwise TMDb is asked,
// once however many readers open the film at the same moment, and the
// answer is kept. A lookup that fails is a 502 and is not kept, so the
// next open asks again.
func (s *CatalogServer) trailerFor(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validTConst(id) {
		writeError(w, http.StatusBadRequest, "id must be an IMDb title id, such as tt0133093")
		return
	}
	var key string
	if s.trailerStore != nil {
		v, err, _ := s.trailerFlight.Do(id, func() (any, error) {
			return s.lookupTrailer(id)
		})
		if err != nil {
			s.warnTrailer(err)
			writeError(w, http.StatusBadGateway, "could not look up a trailer")
			return
		}
		key, _ = v.(string)
	}
	var body trailerAnswer
	if key != "" {
		body.Key = &key
	}
	noStore(w)
	writeJSON(w, http.StatusOK, body)
}

// lookupTrailer answers for one title. The call is not tied to one
// reader's context: the first to open the film may leave while others
// wait on the same answer.
func (s *CatalogServer) lookupTrailer(id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), trailerBudget)
	defer cancel()
	row, err := s.trailerStore.Trailer(ctx, id)
	if err != nil {
		return "", err
	}
	// Not a film this catalog holds: nothing to play, and nothing worth
	// a request or a row.
	if !row.Title {
		return "", nil
	}
	if row.Asked && !catalog.ReaskTrailer(row.Key, row.AskedAt, row.Released, time.Now()) {
		return row.Key, nil
	}
	if s.trailers == nil {
		return row.Key, nil
	}
	tmdbID := row.TMDbID
	if !row.TMDbAsked {
		got, err := s.trailers.FindByIMDb(ctx, id)
		switch {
		case errors.Is(err, tmdb.ErrNotFound):
			got = tmdb.Found{}
		case err != nil:
			return "", err
		}
		// Remembered so no one asks TMDb for this id again, with the
		// overview that came on the same answer.
		if err := s.trailerStore.KeepTMDbFind(ctx, id, got); err != nil {
			s.warnTrailer(err)
		}
		tmdbID = got.ID
	}
	key := ""
	if tmdbID > 0 {
		if key, err = trailer.Pick(ctx, s.trailers, s.oembed, tmdbID); err != nil {
			return "", err
		}
	}
	// A failed write does not take the answer away from the reader
	// waiting on it; the next open asks again.
	if err := s.trailerStore.KeepTrailer(ctx, id, key); err != nil {
		s.warnTrailer(err)
	}
	return key, nil
}

func (s *CatalogServer) warnTrailer(err error) {
	if err == nil || s.Logger == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	s.Logger.Warn("trailer lookup", "err", err)
}
