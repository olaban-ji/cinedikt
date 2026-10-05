package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/catalog"
)

// fakePhotoStore holds each person's row and records the marks made.
type fakePhotoStore struct {
	mu     sync.Mutex
	rows   map[string]catalog.PhotoRow
	err    error
	asked  [][]string
	wanted []string
}

func (f *fakePhotoStore) PeoplePhotos(_ context.Context, ids []string) (map[string]catalog.PhotoRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, append([]string(nil), ids...))
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]catalog.PhotoRow, len(ids))
	for _, id := range ids {
		if row, ok := f.rows[id]; ok {
			out[id] = row
		}
	}
	return out, nil
}

func (f *fakePhotoStore) WantPeople(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wanted = append(f.wanted, ids...)
}

func (f *fakePhotoStore) marks() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.wanted...)
	sort.Strings(out)
	return out
}

type photosReply struct {
	code    int
	photos  map[string]*string
	pending []string
}

func askPhotos(t *testing.T, s *CatalogServer, ids string) photosReply {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/people/photos?ids="+ids, nil)
	rec := httptest.NewRecorder()
	s.peoplePhotos(rec, req)
	if rec.Code != http.StatusOK {
		return photosReply{code: rec.Code}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Photos  map[string]*string `json:"photos"`
		Pending *[]string          `json:"pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	if body.Photos == nil || body.Pending == nil {
		t.Fatalf("body %s is missing photos or pending; both are always sent", rec.Body)
	}
	pending := append([]string(nil), *body.Pending...)
	sort.Strings(pending)
	return photosReply{code: rec.Code, photos: body.Photos, pending: pending}
}

func photosServer(store PhotoStore, looked bool) *CatalogServer {
	s := NewCatalogServer(nil, nil)
	s.WithPeoplePhotos(store, looked)
	return s
}

const keanuPhoto = "https://image.tmdb.org/t/p/w185/keanu.jpg"

// photoRows is a map's people as the store holds them: Keanu with a
// photo, Moss with TMDb's "none", Fishburne with a photo whose answer has
// come due, Lana never asked, and an id the catalog does not hold.
func photoRows() map[string]catalog.PhotoRow {
	now := time.Now()
	return map[string]catalog.PhotoRow{
		"nm0000206": {Person: true, Asked: true, Photo: keanuPhoto, AskedAt: now.Add(-time.Hour)},
		"nm0000401": {Person: true, Asked: true, AskedAt: now.Add(-time.Hour)},
		"nm0915989": {Person: true, Asked: true, Photo: "https://image.tmdb.org/t/p/w185/fishburne.jpg",
			AskedAt: now.Add(-151 * 24 * time.Hour)},
		"nm0905154": {Person: true},
		"nm9999999": {},
	}
}

const mapIDs = "nm0000206,nm0000401,nm0915989,nm0905154,nm9999999"

// TestPhotosAreReadAndTheMissingAreMarked: a stored photo is the answer,
// and so is "none"; a person the job has not reached is pending and
// marked, and so is an answer that has come due, which is still served.
// A person the catalog does not hold is "none", and marked for nothing.
func TestPhotosAreReadAndTheMissingAreMarked(t *testing.T) {
	store := &fakePhotoStore{rows: photoRows()}
	got := askPhotos(t, photosServer(store, true), mapIDs)
	want := map[string]string{
		"nm0000206": keanuPhoto,
		"nm0000401": "",
		"nm0915989": "https://image.tmdb.org/t/p/w185/fishburne.jpg",
		"nm0905154": "",
		"nm9999999": "",
	}
	if len(got.photos) != len(want) {
		t.Errorf("photos = %v, want an entry for each of %d people", got.photos, len(want))
	}
	for id, url := range want {
		photo, ok := got.photos[id]
		switch {
		case !ok:
			t.Errorf("%s is missing from the answer", id)
		case url == "" && photo != nil:
			t.Errorf("%s = %q, want null", id, *photo)
		case url != "" && (photo == nil || *photo != url):
			t.Errorf("%s = %v, want %q", id, photo, url)
		}
	}
	if want := []string{"nm0905154"}; !reflect.DeepEqual(got.pending, want) {
		t.Errorf("pending = %v, want %v", got.pending, want)
	}
	if m, want := store.marks(), []string{"nm0905154", "nm0915989"}; !reflect.DeepEqual(m, want) {
		t.Errorf("marks = %v, want the unanswered and the due: %v", m, want)
	}
}

// TestNothingPendingIsEmptyNotNull: a reader whose people are all
// answered is told nothing is pending, as a list it can read.
func TestNothingPendingIsEmptyNotNull(t *testing.T) {
	store := &fakePhotoStore{rows: photoRows()}
	req := httptest.NewRequest(http.MethodGet, "/people/photos?ids=nm0000206", nil)
	rec := httptest.NewRecorder()
	photosServer(store, true).peoplePhotos(rec, req)
	if !strings.Contains(rec.Body.String(), `"pending":[]`) {
		t.Errorf("body = %s, want an empty pending list", rec.Body)
	}
	if m := store.marks(); len(m) != 0 {
		t.Errorf("a fresh answer was marked: %v", m)
	}
}

// TestARepeatedIDIsAskedAndAnsweredOnce: a request that names somebody
// twice reads them once, answers them once and marks them once.
func TestARepeatedIDIsAskedAndAnsweredOnce(t *testing.T) {
	store := &fakePhotoStore{rows: photoRows()}
	got := askPhotos(t, photosServer(store, true), "nm0905154,nm0905154,nm0000206")
	if len(got.photos) != 2 || !reflect.DeepEqual(got.pending, []string{"nm0905154"}) {
		t.Errorf("reply = %+v", got)
	}
	if len(store.asked) != 1 || !reflect.DeepEqual(store.asked[0], []string{"nm0905154", "nm0000206"}) {
		t.Errorf("the store was asked %v", store.asked)
	}
	if m := store.marks(); !reflect.DeepEqual(m, []string{"nm0905154"}) {
		t.Errorf("marks = %v", m)
	}
}

// TestPhotoIDsAreCheckedBeforeAnythingIsRead: every id must be a name id,
// and there may be at most MaxPhotoIDs of them.
func TestPhotoIDsAreCheckedBeforeAnythingIsRead(t *testing.T) {
	store := &fakePhotoStore{rows: photoRows()}
	s := photosServer(store, true)
	many := make([]string, MaxPhotoIDs+1)
	for i := range many {
		many[i] = "nm0000206"
	}
	for _, ids := range []string{
		"",
		"tt0133093",
		"nm0000206,tt0133093",
		"nm0000206,",
		"nm",
		"nm12x4",
		"NM0000206",
		strings.Join(many, ","),
	} {
		if got := askPhotos(t, s, ids); got.code != http.StatusBadRequest {
			t.Errorf("ids=%q: status = %d, want 400", ids, got.code)
		}
	}
	if len(store.asked) != 0 || len(store.marks()) != 0 {
		t.Errorf("a refused request read %v and marked %v", store.asked, store.marks())
	}
	// Exactly MaxPhotoIDs is allowed.
	if got := askPhotos(t, s, strings.Join(many[:MaxPhotoIDs], ",")); got.code != http.StatusOK {
		t.Errorf("%d ids: status = %d, want 200", MaxPhotoIDs, got.code)
	}
}

func TestAFailedPhotoReadIs500AndMarksNothing(t *testing.T) {
	store := &fakePhotoStore{err: errors.New("database down")}
	if got := askPhotos(t, photosServer(store, true), "nm0000206"); got.code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", got.code)
	}
	if m := store.marks(); len(m) != 0 {
		t.Errorf("a failed read was marked: %v", m)
	}
}

// TestWithoutTMDbNoPhotoIsPending: with no credentials there is no job to
// find a photo, so a person it never reached has none rather than one on
// its way that never comes. Stored photos are still served, and nothing
// is marked.
func TestWithoutTMDbNoPhotoIsPending(t *testing.T) {
	store := &fakePhotoStore{rows: photoRows()}
	got := askPhotos(t, photosServer(store, false), mapIDs)
	if len(got.pending) != 0 {
		t.Errorf("pending = %v with nothing to answer it", got.pending)
	}
	if p := got.photos["nm0000206"]; p == nil || *p != keanuPhoto {
		t.Errorf("a stored photo was not served: %v", p)
	}
	if got.photos["nm0905154"] != nil {
		t.Errorf("an unanswered person has a photo: %v", *got.photos["nm0905154"])
	}
	if m := store.marks(); len(m) != 0 {
		t.Errorf("marked %v with nothing to answer the marks", m)
	}
}

// TestThePhotosRouteNeverAsksTMDb: whatever the rows say, the answer is
// read, never looked up. The server's own TMDb client, which search and
// the poster stand-in use, fails the test if it is asked, and so does
// any request that leaves the process. The route is reached through the
// server's own router, as a reader reaches it.
func TestThePhotosRouteNeverAsksTMDb(t *testing.T) {
	was := http.DefaultTransport
	http.DefaultTransport = forbiddenNetwork{t}
	t.Cleanup(func() { http.DefaultTransport = was })

	store := &fakePhotoStore{rows: photoRows()}
	cat := photosServer(store, true)
	cat.WithSearchFallback(forbiddenTMDb{t})
	cat.WithPosterStandIn(forbiddenTMDb{t}, nil)
	h := New(cat, discardLogger()).Handler()
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/people/photos?ids="+mapIDs, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
	}
	if len(store.asked) != 3 {
		t.Errorf("the store was read %d times, want 3", len(store.asked))
	}
}
