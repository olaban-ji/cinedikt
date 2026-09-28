package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/catalog"
	"cinedikt/internal/tmdb"
)

// fakeTrailerStore holds one title's row and records the marks made.
type fakeTrailerStore struct {
	mu     sync.Mutex
	row    catalog.TrailerRow
	err    error
	wanted []string
}

func (f *fakeTrailerStore) Trailer(context.Context, string) (catalog.TrailerRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.row, f.err
}

func (f *fakeTrailerStore) WantTrailer(tconst string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wanted = append(f.wanted, tconst)
}

func (f *fakeTrailerStore) marks() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.wanted...)
}

// forbiddenTMDb is a TMDb that fails the test if anything asks it.
type forbiddenTMDb struct{ t *testing.T }

func (f forbiddenTMDb) FindByIMDb(context.Context, string) (tmdb.Found, error) {
	f.t.Error("the trailer route asked TMDb for a film")
	return tmdb.Found{}, errors.New("not here")
}

func (f forbiddenTMDb) SearchMovies(context.Context, string) (*tmdb.SearchResults, error) {
	f.t.Error("the trailer route searched TMDb")
	return nil, errors.New("not here")
}

// forbiddenNetwork fails the test on any request that leaves the
// process. YouTube's oEmbed and TMDb's client both go out through it.
type forbiddenNetwork struct{ t *testing.T }

func (f forbiddenNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("the trailer route called %s", r.URL.Host)
	return nil, errors.New("no network in this test")
}

type trailerReply struct {
	code    int
	key     *string
	pending bool
}

func askTrailer(t *testing.T, s *CatalogServer, id string) trailerReply {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/trailers/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.trailerFor(rec, req)
	if rec.Code != http.StatusOK {
		return trailerReply{code: rec.Code}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	raw, ok := body["key"]
	if !ok {
		t.Fatalf("body %s has no key field", rec.Body)
	}
	reply := trailerReply{code: rec.Code}
	if err := json.Unmarshal(raw, &reply.key); err != nil {
		t.Fatalf("key %s: %v", raw, err)
	}
	if raw, ok := body["pending"]; ok {
		if err := json.Unmarshal(raw, &reply.pending); err != nil || !reply.pending {
			t.Errorf("pending = %s; it is sent only when true", raw)
		}
	}
	return reply
}

func trailerServer(store TrailerStore, looked bool) *CatalogServer {
	s := NewCatalogServer(nil, nil)
	s.WithTrailers(store, looked)
	return s
}

func TestATrailerAlreadyStoredIsTheAnswer(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, Key: "vKQi3bBA1y8", AskedAt: time.Now()}}
	got := askTrailer(t, trailerServer(store, true), "tt0133093")
	if got.key == nil || *got.key != "vKQi3bBA1y8" || got.pending {
		t.Fatalf("reply = %+v", got)
	}
	if m := store.marks(); len(m) != 0 {
		t.Errorf("a fresh answer was marked for the job: %v", m)
	}

	// "No trailer" is an answer too, not a wait.
	store = &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, AskedAt: time.Now()}}
	if got := askTrailer(t, trailerServer(store, true), "tt0133093"); got.key != nil || got.pending {
		t.Fatalf("reply = %+v, want a definite null", got)
	}
}

// TestAFilmNobodyHasAskedAboutIsPendingAndMarked: the reader is told the
// answer is on its way, and the job is asked to find it.
func TestAFilmNobodyHasAskedAboutIsPendingAndMarked(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true}}
	got := askTrailer(t, trailerServer(store, true), "tt0133093")
	if got.key != nil || !got.pending {
		t.Fatalf("reply = %+v, want a pending null", got)
	}
	if m := store.marks(); len(m) != 1 || m[0] != "tt0133093" {
		t.Errorf("marks = %v, want the film", m)
	}
}

// TestADueAnswerIsServedAtOnceAndMarked: a recent film's week-old "no
// trailer", or any answer past TMDb's 150 days, is still the answer
// now, and the job is asked to look again. Anything else is left alone.
func TestADueAnswerIsServedAtOnceAndMarked(t *testing.T) {
	now := time.Now()
	weekOld := now.Add(-8 * 24 * time.Hour)
	for _, c := range []struct {
		name string
		row  catalog.TrailerRow
		key  string
		mark bool
	}{
		{"a recent film's week-old null", catalog.TrailerRow{Title: true, Asked: true, AskedAt: weekOld,
			Released: now.AddDate(0, -2, 0)}, "", true},
		{"an old film's week-old null", catalog.TrailerRow{Title: true, Asked: true, AskedAt: weekOld,
			Released: now.AddDate(-5, 0, 0)}, "", false},
		{"a key past 150 days", catalog.TrailerRow{Title: true, Asked: true, Key: "oldKey",
			AskedAt: now.Add(-151 * 24 * time.Hour), Released: now.AddDate(-5, 0, 0)}, "oldKey", true},
		{"a key 149 days old", catalog.TrailerRow{Title: true, Asked: true, Key: "freshKey",
			AskedAt: now.Add(-149 * 24 * time.Hour), Released: now.AddDate(-5, 0, 0)}, "freshKey", false},
	} {
		store := &fakeTrailerStore{row: c.row}
		got := askTrailer(t, trailerServer(store, true), "tt0000001")
		if got.pending {
			t.Errorf("%s: a stored answer was sent as pending", c.name)
		}
		if (got.key == nil) != (c.key == "") || (got.key != nil && *got.key != c.key) {
			t.Errorf("%s: key = %v, want %q", c.name, got.key, c.key)
		}
		if marked := len(store.marks()) == 1; marked != c.mark {
			t.Errorf("%s: marked = %v, want %v", c.name, marked, c.mark)
		}
	}
}

func TestATrailerForAnUnknownTitleIsNullAndMarksNothing(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{}}
	if got := askTrailer(t, trailerServer(store, true), "tt9999999"); got.key != nil || got.pending {
		t.Fatalf("reply = %+v, want a definite null", got)
	}
	if m := store.marks(); len(m) != 0 {
		t.Errorf("an unknown title was marked: %v", m)
	}
	if got := askTrailer(t, trailerServer(store, true), "nope"); got.code != http.StatusBadRequest {
		t.Errorf("a bad id: status = %d, want 400", got.code)
	}
}

func TestAFailedTrailerReadIs500AndMarksNothing(t *testing.T) {
	store := &fakeTrailerStore{err: errors.New("database down")}
	if got := askTrailer(t, trailerServer(store, true), "tt0133093"); got.code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", got.code)
	}
	if m := store.marks(); len(m) != 0 {
		t.Errorf("a failed read was marked: %v", m)
	}
}

// TestWithoutTMDbNothingIsPending: with no credentials there is no job
// to find a trailer, so a film it never reached is "no trailer" rather
// than a wait that never ends. Stored answers are still served.
func TestWithoutTMDbNothingIsPending(t *testing.T) {
	store := &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, Key: "stored", AskedAt: time.Now()}}
	if got := askTrailer(t, trailerServer(store, false), "tt0133093"); got.key == nil || *got.key != "stored" {
		t.Fatalf("reply = %+v", got)
	}
	store = &fakeTrailerStore{row: catalog.TrailerRow{Title: true}}
	if got := askTrailer(t, trailerServer(store, false), "tt0133093"); got.key != nil || got.pending {
		t.Fatalf("reply = %+v, want a definite null", got)
	}
	store = &fakeTrailerStore{row: catalog.TrailerRow{Title: true, Asked: true, Key: "old",
		AskedAt: time.Now().Add(-151 * 24 * time.Hour)}}
	askTrailer(t, trailerServer(store, false), "tt0133093")
	if m := store.marks(); len(m) != 0 {
		t.Errorf("marked %v with nothing to answer the marks", m)
	}
}

// TestTheTrailerRouteNeverAsksTMDbOrYouTube: whatever the row says, the
// answer is read, never looked up. The server's own TMDb client, which
// search and the poster stand-in use, fails the test if it is asked,
// and so does any request that leaves the process.
func TestTheTrailerRouteNeverAsksTMDbOrYouTube(t *testing.T) {
	was := http.DefaultTransport
	http.DefaultTransport = forbiddenNetwork{t}
	t.Cleanup(func() { http.DefaultTransport = was })

	now := time.Now()
	for _, row := range []catalog.TrailerRow{
		{},
		{Title: true},
		{Title: true, Asked: true, Key: "stored", AskedAt: now},
		{Title: true, Asked: true, AskedAt: now.Add(-8 * 24 * time.Hour), Released: now.AddDate(0, -1, 0)},
		{Title: true, Asked: true, Key: "old", AskedAt: now.Add(-200 * 24 * time.Hour)},
	} {
		s := trailerServer(&fakeTrailerStore{row: row}, true)
		s.WithSearchFallback(forbiddenTMDb{t})
		s.WithPosterStandIn(forbiddenTMDb{t}, nil)
		if got := askTrailer(t, s, "tt0133093"); got.code != http.StatusOK {
			t.Errorf("row %+v: status = %d", row, got.code)
		}
	}
}
