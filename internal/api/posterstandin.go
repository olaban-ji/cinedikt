package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cinedikt/internal/imdbid"
	"cinedikt/internal/tmdb"
)

// standInBudget is how long a card will wait on a replacement. Past
// that it should try the address it already has, not keep the frame blank.
const standInBudget = 4 * time.Second

// posterStandIn is GET /posters/{id}: a picture from TMDb for a title
// whose own poster just failed to load.
//
// A miss here is not an error the card should surface. It falls back
// to the address it already had. A picture is written down, so the next
// read does not have to ask.
func (s *CatalogServer) posterStandIn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !imdbid.Title(id) {
		writeError(w, http.StatusBadRequest, "id must be an IMDb title id, such as tt0133093")
		return
	}
	if s.posters == nil {
		writeError(w, http.StatusNotFound, "no poster")
		return
	}
	v, err, _ := s.flight.Do(id, func() (any, error) {
		return s.lookupStandIn(id)
	})
	if err != nil {
		s.warnPoster(err)
		writeError(w, http.StatusBadGateway, "could not fetch a poster")
		return
	}
	poster, _ := v.(string)
	if poster == "" {
		writeError(w, http.StatusNotFound, "no poster")
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]string{"poster": poster})
}

// lookupStandIn asks once. The singleflight around it is what keeps a
// grid of the same miss from becoming a grid of the same request.
//
// The call is not tied to one reader's context: the first card to miss
// may leave while others are waiting on the same answer.
func (s *CatalogServer) lookupStandIn(id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), standInBudget)
	defer cancel()
	got, err := s.posters.FindByIMDb(ctx, id)
	switch {
	case errors.Is(err, tmdb.ErrNotFound):
		s.keepStandIn(ctx, id, tmdb.Found{})
		return "", nil
	case err != nil:
		return "", err
	case got.Poster == "":
		s.keepStandIn(ctx, id, got)
		return "", nil
	}
	s.keepStandIn(ctx, id, got)
	return got.Poster, nil
}

// keepStandIn writes the answer down. A failure to write does not take
// the picture away from the reader who is waiting on it.
func (s *CatalogServer) keepStandIn(ctx context.Context, id string, got tmdb.Found) {
	if s.keeper == nil {
		return
	}
	if err := s.keeper.KeepTMDbPoster(ctx, id, got); err != nil {
		s.warnPoster(err)
	}
}

func (s *CatalogServer) warnPoster(err error) {
	if err == nil || s.Logger == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	s.Logger.Warn("poster stand-in", "err", err)
}
