package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/sync/singleflight"

	"cinedikt/internal/catalog"
	"cinedikt/internal/tmdb"
)

// CatalogReader is what the API needs from the catalog. Everything here
// reads; nothing a request does can write the catalog it is reading.
type CatalogReader interface {
	Grid(ctx context.Context, tconst string) (*catalog.Grid, error)
	Films(ctx context.Context, anchor string, ids []string) ([]catalog.Movie, error)
	Search(ctx context.Context, query string, limit int) ([]catalog.Hit, error)
	// ByTMDB resolves TMDb movie ids already matched to this catalog,
	// in the order given. An id with no row is left out.
	ByTMDB(ctx context.Context, ids []int) ([]catalog.Hit, error)
	FirstRun(ctx context.Context, pool int) ([]catalog.Hit, error)
	LiveReady(ctx context.Context) (bool, error)
	Ping(ctx context.Context) error
}

// CatalogServer serves maps out of the catalog.
type CatalogServer struct {
	Catalog CatalogReader
	Logger  *slog.Logger
	// outside is asked when a title search finds nothing here. Nil means
	// an empty catalog result is the answer.
	outside OutsideSearch
	// posters is asked when a picture the reader has will not load.
	// Nil means there is no stand-in, and the card keeps the one it has.
	posters PosterLookup
	// keeper writes a stand-in down so the next read does not ask again.
	keeper PosterKeeper
	flight singleflight.Group
	// trailerStore holds the trailer job's answers; nil serves "no
	// trailer" for every film. trailersLooked is whether the job has the
	// credentials to find one for a film it has not reached yet.
	trailerStore   TrailerStore
	trailersLooked bool
	// photoStore holds the people job's answers; nil serves "no photo"
	// for everyone. photosLooked is whether the job has the credentials
	// to find one for a person it has not reached yet.
	photoStore   PhotoStore
	photosLooked bool
}

// PosterLookup is TMDb's mapping from an IMDb title to a picture.
type PosterLookup interface {
	FindByIMDb(ctx context.Context, imdbID string) (tmdb.Found, error)
}

// PosterKeeper records that answer. A picture replaces the address that
// failed; no picture is still an answer, and the address already stored
// is left alone.
type PosterKeeper interface {
	KeepTMDbPoster(ctx context.Context, tconst string, got tmdb.Found) error
}

// WithPosterStandIn asks TMDb for a replacement when a poster 404s.
func (s *CatalogServer) WithPosterStandIn(lookup PosterLookup, keep PosterKeeper) {
	s.posters = lookup
	s.keeper = keep
}

// NewCatalogServer builds the read API.
func NewCatalogServer(c CatalogReader, logger *slog.Logger) *CatalogServer {
	return &CatalogServer{Catalog: c, Logger: logger}
}

// MaxDetailIDs bounds one detail request.
const MaxDetailIDs = 200

// SearchHits is how many results a reader is offered.
const SearchHits = 10

// movieGrid returns a whole map: the searched movie, its people, and
// every card's place. There is no second request for the shape of it.
func (s *CatalogServer) movieGrid(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validTConst(id) {
		writeError(w, http.StatusBadRequest, "id must be an IMDb title id, such as tt0133093")
		return
	}
	if !s.ready(w, r) {
		return
	}
	grid, err := s.Catalog.Grid(r.Context(), id)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no map for that id")
		return
	}
	if err != nil {
		if gone(r) {
			return
		}
		s.Logger.Error("grid", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not build that map")
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, grid)
}

// movieGridFilms is what the cards on screen say, asked for by id.
func (s *CatalogServer) movieGridFilms(w http.ResponseWriter, r *http.Request) {
	anchor := r.PathValue("id")
	if !validTConst(anchor) {
		writeError(w, http.StatusBadRequest, "id must be an IMDb title id, such as tt0133093")
		return
	}
	raw := r.URL.Query().Get("ids")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	ids := strings.Split(raw, ",")
	if len(ids) > MaxDetailIDs {
		writeError(w, http.StatusBadRequest, "too many ids")
		return
	}
	for _, id := range ids {
		if !validTConst(id) {
			writeError(w, http.StatusBadRequest, "ids must be IMDb title ids")
			return
		}
	}
	if !s.ready(w, r) {
		return
	}
	films, err := s.Catalog.Films(r.Context(), anchor, ids)
	if err != nil {
		if gone(r) {
			return
		}
		s.Logger.Error("grid films", "anchor", anchor, "err", err)
		writeError(w, http.StatusInternalServerError, "could not read those films")
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"films": films})
}

// firstRun is the cold screen: films to open a map from, a different
// set each visit, one from each era.
func (s *CatalogServer) firstRun(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w, r) {
		return
	}
	hits, err := s.Catalog.FirstRun(r.Context(), 0)
	if err != nil && gone(r) {
		return
	}
	if err != nil {
		s.Logger.Error("first run", "err", err)
		// An empty screen is better than an error on the way in.
		hits = nil
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"results": hits})
}

// searchMovies finds a movie to open a map from, out of the catalog.
//
// An empty result is asked of TMDb when a fallback is configured. That
// call can fail, or know a film this catalog cannot map; either way the
// field gets the empty list it would have had, not an error.
func (s *CatalogServer) searchMovies(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < catalog.MinQuery {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	if !s.ready(w, r) {
		return
	}
	hits, err := s.Catalog.Search(r.Context(), q, SearchHits)
	if err != nil {
		if gone(r) {
			return
		}
		s.Logger.Error("search", "err", err)
		writeError(w, http.StatusInternalServerError, "could not search")
		return
	}
	if len(hits) == 0 {
		if found := s.fallbackSearch(r.Context(), q); len(found) > 0 {
			hits = found
		}
	}
	if hits == nil {
		hits = []catalog.Hit{}
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"results": hits})
}

// ready refuses to answer before a catalog has ever been published.
// There is nothing to serve, and saying so is better than an empty map.
func (s *CatalogServer) ready(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.Catalog.LiveReady(r.Context())
	if err != nil {
		if gone(r) {
			return false
		}
		s.Logger.Error("readiness", "err", err)
		writeError(w, http.StatusServiceUnavailable, "the catalog is unavailable")
		return false
	}
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "the catalog is still being built")
		return false
	}
	return true
}

// gone reports whether the reader stopped waiting before the answer was
// ready. The page cancels a request it no longer needs: every keystroke
// in the search field replaces the last search, and leaving the opening
// screen early drops its films. A closed tab does the same. The query
// then fails with context.Canceled, which says nothing about the
// catalog, so it is not logged as an error and nothing is written back,
// because nobody is there to read it. A request that ran out of time
// ends with DeadlineExceeded instead, and that is still an error.
func gone(r *http.Request) bool {
	return errors.Is(r.Context().Err(), context.Canceled)
}

// validTConst is IMDb's title id: "tt" and at least seven digits, though
// the length has grown over the years and is not assumed here.
func validTConst(id string) bool {
	if len(id) < 3 || len(id) > 20 || !strings.HasPrefix(id, "tt") {
		return false
	}
	for i := 2; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

// noStore keeps a map out of caches. It is built from a catalog that is
// swapped underneath, and a stale one is a map of a different day.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
