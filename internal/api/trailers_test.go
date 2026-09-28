package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cinedikt/internal/catalog"
	"cinedikt/internal/tmdb"
)

// fakeTrailerStore holds one title's row and records what is kept.
type fakeTrailerStore struct {
	mu      sync.Mutex
	row     catalog.TrailerRow
	err     error
	kept    []string
	keptAny bool
	finds   []tmdb.Found
}

func (f *fakeTrailerStore) Trailer(context.Context, string) (catalog.TrailerRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.row, f.err
}

func (f *fakeTrailerStore) KeepTrailer(_ context.Context, _ string, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kept = append(f.kept, key)
	f.keptAny = true
	return nil
}

func (f *fakeTrailerStore) KeepTMDbFind(_ context.Context, _ string, got tmdb.Found) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finds = append(f.finds, got)
	return nil
}

// fakeTrailerTMDb answers FindByIMDb and Videos from fixed values.
type fakeTrailerTMDb struct {
	found     tmdb.Found
	findErr   error
	videos    []tmdb.Video
	videosErr error
	finds     atomic.Int32
	lists     atomic.Int32
	// gate, when set, holds every Videos call until it is closed.
	gate chan struct{}
}

func (f *fakeTrailerTMDb) FindByIMDb(context.Context, string) (tmdb.Found, error) {
	f.finds.Add(1)
	return f.found, f.findErr
}

func (f *fakeTrailerTMDb) Videos(context.Context, int) ([]tmdb.Video, error) {
	f.lists.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	return f.videos, f.videosErr
}

// embedExcept is a checker that refuses the keys it holds.
type embedExcept map[string]bool

func (e embedExcept) Embeddable(_ context.Context, key string) (bool, error) {
	return !e[key], nil
}

func askTrailer(t *testing.T, s *CatalogServer, id string) (*httptest.ResponseRecorder, *string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/trailers/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.trailerFor(rec, req)
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body map[string]*string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	key, ok := body["key"]
	if !ok {
		t.Fatalf("body %s has no key field", rec.Body)
	}
	return rec, key
}

func trailerServer(lookup TrailerLookup, store TrailerStore, check embedExcept) *CatalogServer {
	s := NewCatalogServer(nil, nil)
	s.WithTrailers(lookup, store, check)
	return s
}

func clip(key string) tmdb.Video {
	return tmdb.Video{Key: key, Site: "YouTube", Type: "Trailer", Official: true, Language: "en"}
}

func TestATrailerAlreadyStoredIsTheAnswer(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, Key: "vKQi3bBA1y8", AskedAt: time.Now()}}
	look := &fakeTrailerTMDb{}
	_, key := askTrailer(t, trailerServer(look, store, nil), "tt0133093")
	if key == nil || *key != "vKQi3bBA1y8" {
		t.Fatalf("key = %v", key)
	}
	if look.finds.Load()+look.lists.Load() != 0 || store.keptAny {
		t.Error("a stored answer was asked again")
	}
}

func TestATrailerIsLookedUpOnceAndKept(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true}}
	look := &fakeTrailerTMDb{
		found:  tmdb.Found{ID: 603, Overview: "Neo learns the truth."},
		videos: []tmdb.Video{clip("blocked"), clip("playable")},
	}
	_, key := askTrailer(t, trailerServer(look, store, embedExcept{"blocked": true}), "tt0133093")
	if key == nil || *key != "playable" {
		t.Fatalf("key = %v, want the first one YouTube will embed", key)
	}
	// No TMDb id was known, so it was found and remembered, overview
	// and all.
	if look.finds.Load() != 1 || len(store.finds) != 1 || store.finds[0].ID != 603 || store.finds[0].Overview == "" {
		t.Errorf("finds = %d, kept %+v", look.finds.Load(), store.finds)
	}
	if len(store.kept) != 1 || store.kept[0] != "playable" {
		t.Errorf("kept %v", store.kept)
	}
}

func TestATrailerUsesTheTMDbIDAlreadyKnown(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, TMDbAsked: true, TMDbID: 603}}
	look := &fakeTrailerTMDb{videos: []tmdb.Video{clip("playable")}}
	if _, key := askTrailer(t, trailerServer(look, store, nil), "tt0133093"); key == nil || *key != "playable" {
		t.Fatalf("key = %v", key)
	}
	if look.finds.Load() != 0 {
		t.Error("TMDb was asked for an id meta.tmdb already had")
	}
}

// TestNoTrailerIsAnAnswerAndIsKept: nothing YouTube will embed, or no
// TMDb movie at all, is null, and stored so it is not asked again.
func TestNoTrailerIsAnAnswerAndIsKept(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, TMDbAsked: true, TMDbID: 603}}
	look := &fakeTrailerTMDb{videos: []tmdb.Video{clip("blocked")}}
	_, key := askTrailer(t, trailerServer(look, store, embedExcept{"blocked": true}), "tt0133093")
	if key != nil {
		t.Fatalf("key = %q, want null", *key)
	}
	if len(store.kept) != 1 || store.kept[0] != "" {
		t.Errorf("kept %v, want one null", store.kept)
	}

	// TMDb has no movie for the title: null, kept, and no list asked.
	store = &fakeTrailerStore{row: catalog.TrailerRow{Title: true}}
	look = &fakeTrailerTMDb{findErr: tmdb.ErrNotFound}
	if _, key := askTrailer(t, trailerServer(look, store, nil), "tt0133093"); key != nil {
		t.Fatalf("key = %q, want null", *key)
	}
	if look.lists.Load() != 0 || len(store.kept) != 1 || store.kept[0] != "" || len(store.finds) != 1 {
		t.Errorf("lists %d, kept %v, finds %v", look.lists.Load(), store.kept, store.finds)
	}
}

// TestAStoredNullIsAskedAgainOnlyForARecentFilm: trailers are often
// added after release.
func TestAStoredNullIsAskedAgainOnlyForARecentFilm(t *testing.T) {
	now := time.Now()
	weekOld := now.Add(-8 * 24 * time.Hour)

	recent := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, AskedAt: weekOld,
		Released: now.AddDate(0, -2, 0), TMDbAsked: true, TMDbID: 1}}
	look := &fakeTrailerTMDb{videos: []tmdb.Video{clip("added")}}
	if _, key := askTrailer(t, trailerServer(look, recent, nil), "tt0000001"); key == nil || *key != "added" {
		t.Fatalf("recent film: key = %v, want the trailer added since", key)
	}

	old := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, AskedAt: weekOld,
		Released: now.AddDate(-5, 0, 0), TMDbAsked: true, TMDbID: 1}}
	look = &fakeTrailerTMDb{videos: []tmdb.Video{clip("added")}}
	if _, key := askTrailer(t, trailerServer(look, old, nil), "tt0000001"); key != nil {
		t.Fatalf("old film: key = %q, want the stored null", *key)
	}
	if look.lists.Load() != 0 {
		t.Error("an old film's null was asked again")
	}
}

func TestAFailedTrailerLookupIs502AndNotKept(t *testing.T) {
	for name, look := range map[string]*fakeTrailerTMDb{
		"find":   {findErr: errors.New("tmdb down")},
		"videos": {found: tmdb.Found{ID: 603}, videosErr: errors.New("tmdb down")},
	} {
		store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true}}
		rec, _ := askTrailer(t, trailerServer(look, store, nil), "tt0133093")
		if rec.Code != http.StatusBadGateway {
			t.Errorf("%s: status = %d, want 502", name, rec.Code)
		}
		if store.keptAny {
			t.Errorf("%s: a failed lookup was kept as an answer", name)
		}
	}
	store := &fakeTrailerStore{err: errors.New("database down")}
	if rec, _ := askTrailer(t, trailerServer(&fakeTrailerTMDb{}, store, nil), "tt0133093"); rec.Code != http.StatusBadGateway {
		t.Errorf("a failed read: status = %d, want 502", rec.Code)
	}
}

func TestATrailerForAnUnknownTitleIsNullAndCostsNothing(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{}}
	look := &fakeTrailerTMDb{}
	if _, key := askTrailer(t, trailerServer(look, store, nil), "tt9999999"); key != nil {
		t.Fatalf("key = %q, want null", *key)
	}
	if look.finds.Load() != 0 || store.keptAny {
		t.Error("an unknown title was asked about or stored")
	}
	if rec, _ := askTrailer(t, trailerServer(look, store, nil), "nope"); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad id: status = %d, want 400", rec.Code)
	}
}

func TestWithoutTMDbOnlyStoredTrailersAreServed(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, Key: "stored", AskedAt: time.Now()}}
	if _, key := askTrailer(t, trailerServer(nil, store, nil), "tt0133093"); key == nil || *key != "stored" {
		t.Fatalf("key = %v", key)
	}
	store = &fakeTrailerStore{row: catalog.TrailerRow{Title: true}}
	if _, key := askTrailer(t, trailerServer(nil, store, nil), "tt0133093"); key != nil || store.keptAny {
		t.Fatalf("key = %v, kept %v; want null and nothing kept", key, store.kept)
	}
}

// TestReadersOpeningTheSameFilmShareOneLookup: a grid of readers opening
// one film at the same moment is one request to TMDb.
func TestReadersOpeningTheSameFilmShareOneLookup(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, TMDbAsked: true, TMDbID: 603}}
	look := &fakeTrailerTMDb{videos: []tmdb.Video{clip("playable")}, gate: make(chan struct{})}
	s := trailerServer(look, store, nil)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/trailers/tt0133093", nil)
			req.SetPathValue("id", "tt0133093")
			s.trailerFor(httptest.NewRecorder(), req)
		}()
	}
	// Let the five pile up behind the first before it answers.
	for look.lists.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(look.gate)
	wg.Wait()
	if n := look.lists.Load(); n != 1 {
		t.Errorf("TMDb was asked %d times for one film", n)
	}
}
