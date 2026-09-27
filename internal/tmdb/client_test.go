package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		if r.URL.Path != "/movie/603" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("api_key"); got != "k3y" {
			t.Errorf("api_key = %q, want k3y", got)
		}
		if got := r.URL.Query().Get("append_to_response"); got != "credits" {
			t.Errorf("append_to_response = %q, want credits", got)
		}
		w.Write([]byte(`{"id":603,"title":"The Matrix","release_date":"1999-03-31","poster_path":"/m.jpg","vote_average":8.2,"vote_count":26000,"imdb_id":"tt0133093","credits":{"cast":[{"id":6384,"name":"Keanu Reeves","character":"Neo","order":0}],"crew":[{"id":525,"name":"Lana Wachowski","job":"Director","department":"Directing"}]}}`))
	})
	m, err := c.Movie(context.Background(), 603)
	if err != nil {
		t.Fatalf("Movie: %v", err)
	}
	if m.Title != "The Matrix" || m.Credits == nil || len(m.Credits.Cast) != 1 || m.Credits.Cast[0].Character != "Neo" {
		t.Errorf("Movie = %+v", m)
	}
	if len(m.Credits.Crew) != 1 || m.Credits.Crew[0].Job != JobDirector || m.Credits.Crew[0].ID != 525 {
		t.Errorf("Movie crew = %+v", m.Credits.Crew)
	}
	if m.PosterPath != "/m.jpg" || m.VoteAverage != 8.2 || m.VoteCount != 26000 || m.IMDbID != "tt0133093" {
		t.Errorf("Movie metadata = %+v", m)
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
		if got := r.URL.Query().Get("append_to_response"); got != "movie_credits" {
			t.Errorf("append_to_response = %q, want movie_credits", got)
		}
		w.Write([]byte(`{"id":6384,"name":"Keanu Reeves","popularity":40.5,"known_for_department":"Acting","movie_credits":{"cast":[{"id":603,"title":"The Matrix","character":"Neo"}],"crew":[{"id":603,"title":"The Matrix","job":"Director"}]}}`))
	})
	p, err := c.Person(context.Background(), 6384)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if p.Name != "Keanu Reeves" || p.Popularity != 40.5 || p.MovieCredits == nil || len(p.MovieCredits.Cast) != 1 {
		t.Errorf("Person = %+v", p)
	}
	if len(p.MovieCredits.Crew) != 1 || p.MovieCredits.Crew[0].Job != JobDirector {
		t.Errorf("Person crew = %+v", p.MovieCredits.Crew)
	}
}

func TestCacheServesRepeats(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"id":1,"name":"X","movie_credits":{"cast":[{"id":603,"title":"The Matrix"}]}}`))
	}, WithCache(newMemCache()))

	for i := 0; i < 3; i++ {
		p, err := c.Person(context.Background(), 1)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if p.MovieCredits == nil || len(p.MovieCredits.Cast) != 1 {
			t.Fatalf("call %d: credits = %+v", i, p.MovieCredits)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1", hits.Load())
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
		w.Write([]byte(`{"id":603,"title":"The Matrix"}`))
	})
	m, err := c.Movie(context.Background(), 603)
	if err != nil {
		t.Fatalf("Movie: %v", err)
	}
	if m.Title != "The Matrix" || hits.Load() != 3 {
		t.Errorf("title = %q after %d hits", m.Title, hits.Load())
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	var hits atomic.Int32
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := c.Movie(context.Background(), 603); err == nil {
		t.Fatal("want error after persistent 502")
	}
	if hits.Load() != maxAttempts {
		t.Errorf("hits = %d, want %d", hits.Load(), maxAttempts)
	}
}

func TestNotFound(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status_message":"The resource you requested could not be found."}`))
	})
	_, err := c.Movie(context.Background(), 0)
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
	_, err := c.Movie(context.Background(), 603)
	if err == nil || hits.Load() != 1 {
		t.Fatalf("error = %v after %d hits; want one failed attempt", err, hits.Load())
	}
	if want := "Invalid API key"; err != nil && !contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
	// The status travels as a value, so the catalog can tell a refused
	// key from a fault without reading the text.
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Errorf("error %v is not a *StatusError with status 401", err)
	}
}

func TestAServerFaultIsAStatusErrorToo(t *testing.T) {
	c, _ := newTestClient(t, Auth{APIKey: "k"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	_, err := c.Movie(context.Background(), 603)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusBadRequest {
		t.Fatalf("error %v is not a *StatusError with status 400", err)
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

type memCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemCache() *memCache {
	return &memCache{m: map[string][]byte{}}
}

func (c *memCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[key]
	return b, ok
}

func (c *memCache) Set(key string, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = append([]byte(nil), body...)
	return nil
}
