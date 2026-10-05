package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// newTestClient wires a client to handler with no rate limiting and no sleeps.
func newTestClient(t *testing.T, auth Auth, handler http.HandlerFunc, opts ...Option) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	opts = append([]Option{WithBaseURL(srv.URL), WithRateLimit(rate.Inf, 1)}, opts...)
	c := New(auth, opts...)
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c, srv
}

func TestAuthAPIKeyQuery(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k3y"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/movie" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("api_key"); got != "k3y" {
			t.Errorf("api_key = %q, want k3y", got)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("an Authorization header was sent with an API key alone")
		}
		w.Write([]byte(`{"results":[{"id":603,"title":"The Matrix"}]}`))
	})
	sr, err := c.SearchMovies(context.Background(), "the matrix")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(sr.Results) != 1 || sr.Results[0].ID != 603 {
		t.Errorf("results = %+v", sr.Results)
	}
}

func TestAuthBearerToken(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "unused", AccessToken: "tok"}, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		if r.URL.Query().Has("api_key") {
			t.Error("api_key sent alongside bearer token")
		}
		if got := r.URL.Query().Get("external_source"); got != "imdb_id" {
			t.Errorf("external_source = %q, want imdb_id", got)
		}
		w.Write([]byte(`{"movie_results":[{"id":603}]}`))
	})
	got, err := c.FindByIMDb(context.Background(), "tt0133093")
	if err != nil {
		t.Fatalf("FindByIMDb: %v", err)
	}
	if got.ID != 603 {
		t.Errorf("found = %+v", got)
	}
}

func TestRetryOn429(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"status_message":"slow down"}`))
			return
		}
		w.Write([]byte(`{"results":[{"id":603,"title":"The Matrix"}]}`))
	})
	sr, err := c.SearchMovies(context.Background(), "the matrix")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(sr.Results) != 1 || hits.Load() != 3 {
		t.Errorf("results = %+v after %d hits", sr.Results, hits.Load())
	}
}

// TestGivesUpAfterMaxAttempts, with no wait after the last of them: a
// wait there would only delay the failure.
func TestGivesUpAfterMaxAttempts(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	_, err := c.SearchMovies(context.Background(), "the matrix")
	if err == nil || err.Error() != "tmdb: giving up after 4 attempts: tmdb: HTTP 502" {
		t.Fatalf("error = %v after persistent 502", err)
	}
	if hits.Load() != maxAttempts || len(waits) != maxAttempts-1 {
		t.Errorf("hits = %d and waits %v, want %d hits and a wait between each", hits.Load(), waits, maxAttempts)
	}
}

// TestADateRetryAfterIsHonoured: Retry-After may be a date as well as a
// number of seconds.
func TestADateRetryAfterIsHonoured(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"results":[]}`))
	})
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	if _, err := c.SearchMovies(context.Background(), "the matrix"); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] < 59*time.Minute {
		t.Errorf("waits = %v, want the hour the date asked for", waits)
	}
}

// TestAWaitLongerThanTheDeadlineIsNotTaken: a reader waiting on a lookup
// gives TMDb seconds, not the minute a Retry-After may ask for.
func TestAWaitLongerThanTheDeadlineIsNotTaken(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	slept := false
	c.sleep = func(context.Context, time.Duration) error {
		slept = true
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.SearchMovies(ctx, "the matrix")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusTooManyRequests {
		t.Errorf("err = %v", err)
	}
	if slept || hits.Load() != 1 {
		t.Errorf("slept = %v after %d hits; the wait outlasted the deadline", slept, hits.Load())
	}
}

func TestNotFound(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status_message":"The resource you requested could not be found."}`))
	})
	_, err := c.FindByIMDb(context.Background(), "tt0133093")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestUnauthorizedIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "bad"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status_message":"Invalid API key"}`))
	})
	_, err := c.SearchMovies(context.Background(), "the matrix")
	if err == nil || hits.Load() != 1 {
		t.Fatalf("error = %v after %d hits; want one failed attempt", err, hits.Load())
	}
	if want := "Invalid API key"; err != nil && !contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
	// A refused key is ErrKey, so the catalog can tell it from a fault
	// without reading the text, and the status still travels with it.
	var se *StatusError
	if !errors.Is(err, ErrKey) || !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Errorf("error %v is not ErrKey with status 401", err)
	}
}

func TestForbiddenIsARefusedKeyToo(t *testing.T) {
	c, _ := newTestClient(t, Auth{AccessToken: "revoked"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := c.FindByIMDb(context.Background(), "tt0133093"); !errors.Is(err, ErrKey) {
		t.Errorf("error = %v, want ErrKey", err)
	}
}

func TestAServerFaultIsAStatusErrorToo(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	_, err := c.FindByIMDb(context.Background(), "tt0133093")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusBadRequest {
		t.Fatalf("error %v is not a *StatusError with status 400", err)
	}
	if errors.Is(err, ErrKey) {
		t.Error("a bad request read as a refused key")
	}
	if err.Error() != "tmdb: HTTP 400" {
		t.Errorf("error text = %q, want the text it always had", err)
	}
}

func TestSearchMovies(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/movie" || r.URL.Query().Get("query") != "the matrix" {
			t.Errorf("request = %s", r.URL)
		}
		w.Write([]byte(`{"page":1,"results":[{"id":603,"title":"The Matrix","release_date":"1999-03-31"}],"total_results":1}`))
	})
	sr, err := c.SearchMovies(context.Background(), "the matrix")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(sr.Results) != 1 || sr.Results[0].ID != 603 {
		t.Errorf("results = %+v", sr.Results)
	}
}

func TestFindByIMDbReturnsTheTMDBID(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/find/tt0133093" || r.URL.Query().Get("external_source") != "imdb_id" {
			t.Errorf("request = %s", r.URL)
		}
		w.Write([]byte(`{"movie_results":[{"id":603,"poster_path":"/m.jpg","release_date":"1999-03-31"}]}`))
	})
	got, err := c.FindByIMDb(context.Background(), "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 603 {
		t.Errorf("id = %d", got.ID)
	}
	if got.Poster == "" || got.Released.IsZero() {
		t.Errorf("found = %+v", got)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestFindByIMDbKeepsTheOverviewTrimmed(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"movie_results":[{"id":603,"overview":"  A hacker learns the truth.\n "}]}`))
	})
	got, err := c.FindByIMDb(context.Background(), "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if got.Overview != "A hacker learns the truth." {
		t.Errorf("overview = %q", got.Overview)
	}
}

func TestVideosReadsEveryClip(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/603/videos" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"id":603,"results":[
			{"key":"vKQi3bBA1y8","site":"YouTube","type":"Trailer","official":true,"iso_639_1":"EN","published_at":"2014-10-02T19:00:25.000Z","name":"Trailer"},
			{"key":"abc","site":"Vimeo","type":"Teaser","official":false,"iso_639_1":"fr","published_at":"","name":"Teaser"}
		]}`))
	})
	got, err := c.Videos(context.Background(), 603)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("videos = %+v", got)
	}
	first := got[0]
	if first.Key != "vKQi3bBA1y8" || first.Site != "YouTube" || first.Type != "Trailer" || !first.Official || first.Language != "en" {
		t.Errorf("first = %+v", first)
	}
	if want := time.Date(2014, 10, 2, 19, 0, 25, 0, time.UTC); !first.Published.Equal(want) {
		t.Errorf("published = %v, want %v", first.Published, want)
	}
	if !got[1].Published.IsZero() {
		t.Errorf("an empty date was read as %v", got[1].Published)
	}
	if _, err := c.Videos(context.Background(), 0); err == nil {
		t.Error("a zero id was sent to TMDb")
	}
}

// TestClientsGivenOneLimiterShareIt is the rule every process now keeps:
// TMDb counts requests per address, so two clients with a limiter each
// would spend twice the budget between them. With one limiter, what one
// client spends the other cannot.
func TestClientsGivenOneLimiterShareIt(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"movie_results":[{"id":603}]}`))
	}))
	t.Cleanup(srv.Close)
	// Two tokens, and the next one an hour away.
	shared := rate.NewLimiter(rate.Every(time.Hour), 2)
	a := New(Auth{APIKey: "k"}, WithBaseURL(srv.URL), WithLimiter(shared))
	b := New(Auth{APIKey: "k"}, WithBaseURL(srv.URL), WithLimiter(shared))
	if a.Limiter() != shared || b.Limiter() != shared {
		t.Fatal("a client did not keep the limiter it was given")
	}
	if _, err := a.FindByIMDb(context.Background(), "tt0133093"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.FindByIMDb(context.Background(), "tt0234215"); err != nil {
		t.Fatal(err)
	}
	// Both tokens are spent, by two different clients. A third call from
	// either has to wait an hour, which the deadline refuses at once.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := a.FindByIMDb(ctx, "tt0111161"); err == nil {
		t.Error("the first client still had a budget of its own")
	}
	if hits.Load() != 2 {
		t.Errorf("TMDb was asked %d times, want 2", hits.Load())
	}
}

func TestTheDefaultLimiterIsTheProcessBudget(t *testing.T) {
	l := NewLimiter(0)
	if l.Limit() != DefaultRatePerSecond || l.Burst() != DefaultBurst {
		t.Errorf("default limiter = %v/s burst %d, want %v/s burst %d", l.Limit(), l.Burst(), DefaultRatePerSecond, DefaultBurst)
	}
	if l := NewLimiter(7); l.Limit() != 7 || l.Burst() != DefaultBurst {
		t.Errorf("limiter = %v/s burst %d, want 7/s burst %d", l.Limit(), l.Burst(), DefaultBurst)
	}
	if c := New(Auth{APIKey: "k"}, WithLimiter(nil)); c.Limiter() == nil {
		t.Error("a nil limiter took the client's own away")
	}
}

func TestFindPersonByIMDbReturnsTheIDAndThePhotoPath(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/find/nm0000206" || r.URL.Query().Get("external_source") != "imdb_id" {
			t.Errorf("request = %s", r.URL)
		}
		w.Write([]byte(`{"movie_results":[],"person_results":[{"id":6384,"name":"Keanu Reeves","profile_path":" /keanu.jpg ","adult":false}]}`))
	})
	got, err := c.FindPersonByIMDb(context.Background(), "nm0000206")
	if err != nil {
		t.Fatal(err)
	}
	// The bare path is kept, trimmed; an address is built from it at the
	// width it is served at.
	if got.ID != 6384 || got.Profile != "/keanu.jpg" {
		t.Errorf("found = %+v", got)
	}
	if url := PosterURL(got.Profile, ProfileWidth); url != "https://image.tmdb.org/t/p/w185/keanu.jpg" {
		t.Errorf("address = %q", url)
	}
}

// TestAPersonTMDbDoesNotHaveIsNotFound: an empty result is an answer, and
// the caller stops asking.
func TestAPersonTMDbDoesNotHaveIsNotFound(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"movie_results":[],"person_results":[],"tv_results":[]}`))
	})
	_, err := c.FindPersonByIMDb(context.Background(), "nm9999999")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// TestAPersonWithNoPhotoIsAnAnswer: TMDb has the person, and says so with
// a null profile_path. That is a definite "no photo", with the id kept.
func TestAPersonWithNoPhotoIsAnAnswer(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"person_results":[{"id":1234,"profile_path":null,"adult":false}]}`))
	})
	got, err := c.FindPersonByIMDb(context.Background(), "nm0000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 1234 || got.Profile != "" {
		t.Errorf("found = %+v, want the id and no photo", got)
	}
}

// TestAnAdultPersonKeepsTheIDButNotThePhoto: TMDb's adult flag keeps a
// photo off a chip row that sits beside ordinary movies.
func TestAnAdultPersonKeepsTheIDButNotThePhoto(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"person_results":[{"id":777,"profile_path":"/x.jpg","adult":true}]}`))
	})
	got, err := c.FindPersonByIMDb(context.Background(), "nm0000002")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 777 || got.Profile != "" {
		t.Errorf("found = %+v, want the id and no photo", got)
	}
}

// TestOnlyANameIDIsSentToThePersonLookup: "nm" and digits goes out; a
// title id, or anything else, never reaches TMDb.
func TestOnlyANameIDIsSentToThePersonLookup(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"person_results":[{"id":1,"profile_path":"/a.jpg"}]}`))
	})
	if _, err := c.FindPersonByIMDb(context.Background(), "nm0000206"); err != nil {
		t.Errorf("a name id was refused: %v", err)
	}
	for _, bad := range []string{"tt0133093", "nm", "nm12a4", "NM0000206", "nm0000206/../x", ""} {
		if _, err := c.FindPersonByIMDb(context.Background(), bad); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%q: error = %v, want it refused", bad, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("TMDb was asked %d times, want once", hits.Load())
	}
	// And the movie lookup refuses a name id the same way.
	if _, err := c.FindByIMDb(context.Background(), "nm0000206"); err == nil {
		t.Error("a name id was sent to the movie lookup")
	}
}

// TestARefusedKeyOnThePersonLookupIsErrKey, so the people job can tell
// a key only a person can fix from a fault that will pass.
func TestARefusedKeyOnThePersonLookupIsErrKey(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "bad"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status_message":"Invalid API key"}`))
	})
	_, err := c.FindPersonByIMDb(context.Background(), "nm0000206")
	if !errors.Is(err, ErrKey) {
		t.Fatalf("error %v is not ErrKey", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("a refused key read as no such person")
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want one attempt", hits.Load())
	}
}
