package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"cinedikt/internal/catalog"
	"cinedikt/internal/tmdb"
)

func TestSearchUsesTheCatalogWhenItHasAMatch(t *testing.T) {
	out := &tmdbStub{results: []tmdb.Movie{{ID: 1}}}
	body := searchBody(t, catalogStub{
		hits: []catalog.Hit{{ID: "tt0133093", Title: "The Matrix", Year: 1999}},
	}, out, "matrix")
	if len(body.Results) != 1 || body.Results[0].ID != "tt0133093" {
		t.Fatalf("results = %+v", body.Results)
	}
	if out.searches.Load() != 0 {
		t.Fatalf("tmdb was asked %d times", out.searches.Load())
	}
}

func TestSearchFallsBackToTMDbWhenTheCatalogIsEmpty(t *testing.T) {
	out := &tmdbStub{results: []tmdb.Movie{
		{ID: 11},  // not in the catalog
		{ID: 603}, // The Matrix
		{ID: 604}, // not in the catalog
		{ID: 278}, // The Shawshank Redemption
	}}
	cat := catalogStub{byTMDB: map[int]catalog.Hit{
		278: {ID: "tt0111161", Title: "The Shawshank Redemption", Year: 1994},
		603: {ID: "tt0133093", Title: "The Matrix", Year: 1999, Poster: "https://img/m.jpg"},
	}}
	body := searchBody(t, cat, out, "la matrice")
	if out.searches.Load() != 1 {
		t.Fatalf("tmdb searches = %d", out.searches.Load())
	}
	if len(body.Results) != 2 {
		t.Fatalf("results = %+v", body.Results)
	}
	// TMDb's order, not the catalog's vote order. The two it has not
	// matched are gone.
	if body.Results[0].ID != "tt0133093" || body.Results[1].ID != "tt0111161" {
		t.Fatalf("results = %+v", body.Results)
	}
	if body.Results[0].Poster != "https://img/m.jpg" || body.Results[0].Year != 1999 {
		t.Fatalf("first = %+v", body.Results[0])
	}
}

func TestSearchFallbackKeepsAnEmptyResultWhenTMDbFails(t *testing.T) {
	out := &tmdbStub{searchErr: errors.New("tmdb down")}
	body := searchBody(t, catalogStub{}, out, "zzzz")
	if body.Results == nil || len(body.Results) != 0 {
		t.Fatalf("results = %+v, want an empty list", body.Results)
	}
}

func TestSearchFallbackDropsAnIDTheCatalogHasNotMatched(t *testing.T) {
	out := &tmdbStub{results: []tmdb.Movie{{ID: 1}, {ID: 2}}}
	body := searchBody(t, catalogStub{}, out, "matrix")
	if len(body.Results) != 0 {
		t.Fatalf("results = %+v", body.Results)
	}
	if out.searches.Load() != 1 {
		t.Fatalf("tmdb searches = %d", out.searches.Load())
	}
}

func TestSearchFallbackKeepsTMDbOrderAndStopsAtTen(t *testing.T) {
	results := make([]tmdb.Movie, 25)
	byTMDB := map[int]catalog.Hit{}
	for i := range results {
		id := i + 1
		results[i] = tmdb.Movie{ID: id}
		tt := fmt.Sprintf("tt%07d", id)
		byTMDB[id] = catalog.Hit{ID: tt, Title: tt, Year: 2000}
	}
	out := &tmdbStub{results: results}
	body := searchBody(t, catalogStub{byTMDB: byTMDB}, out, "films")
	if out.searches.Load() != 1 {
		t.Fatalf("tmdb searches = %d", out.searches.Load())
	}
	if len(body.Results) != SearchHits {
		t.Fatalf("results = %d, want %d", len(body.Results), SearchHits)
	}
	if body.Results[0].ID != "tt0000001" {
		t.Fatalf("first = %s", body.Results[0].ID)
	}
}

func TestSearchWithoutAFallbackStaysEmpty(t *testing.T) {
	body := searchBody(t, catalogStub{}, nil, "zzzz")
	if len(body.Results) != 0 {
		t.Fatalf("results = %+v", body.Results)
	}
}

type searchPayload struct {
	Results []catalog.Hit `json:"results"`
}

func searchBody(t *testing.T, cat CatalogReader, out OutsideSearch, q string) searchPayload {
	t.Helper()
	s := NewCatalogServer(cat, discardLogger())
	if out != nil {
		s.WithSearchFallback(out)
	}
	req := httptest.NewRequest(http.MethodGet, "/search/movies?q="+url.QueryEscape(q), nil)
	rec := httptest.NewRecorder()
	s.searchMovies(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body searchPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Results == nil {
		t.Fatal("results was null")
	}
	return body
}

type catalogStub struct {
	hits   []catalog.Hit
	byTMDB map[int]catalog.Hit
}

func (catalogStub) Grid(context.Context, string) (*catalog.Grid, error) { return nil, nil }
func (catalogStub) Films(context.Context, string, []string) ([]catalog.Movie, error) {
	return nil, nil
}
func (c catalogStub) Search(context.Context, string, int) ([]catalog.Hit, error) {
	return c.hits, nil
}
func (c catalogStub) ByTMDB(_ context.Context, ids []int) ([]catalog.Hit, error) {
	out := make([]catalog.Hit, 0, len(ids))
	seen := map[int]struct{}{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if h, ok := c.byTMDB[id]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}
func (catalogStub) FirstRun(context.Context) ([]catalog.Hit, error) { return nil, nil }
func (catalogStub) LiveReady(context.Context) (bool, error)         { return true, nil }

type tmdbStub struct {
	results   []tmdb.Movie
	searchErr error
	searches  atomic.Int32
}

func (t *tmdbStub) SearchMovies(context.Context, string) (*tmdb.SearchResults, error) {
	t.searches.Add(1)
	if t.searchErr != nil {
		return nil, t.searchErr
	}
	return &tmdb.SearchResults{Results: t.results}, nil
}
