package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAskPosterTreats404AsMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s, want HEAD", r.Method)
		}
		switch r.URL.Path {
		case "/missing.jpg":
			http.NotFound(w, r)
		case "/gone.jpg":
			w.WriteHeader(http.StatusGone)
		case "/live.jpg":
			w.WriteHeader(http.StatusOK)
		case "/down.jpg":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	missing, known := askPoster(context.Background(), srv.URL+"/missing.jpg")
	if !missing || !known {
		t.Errorf("404 = missing %v known %v, want true true", missing, known)
	}
	missing, known = askPoster(context.Background(), srv.URL+"/gone.jpg")
	if !missing || !known {
		t.Errorf("410 = missing %v known %v, want true true", missing, known)
	}
	missing, known = askPoster(context.Background(), srv.URL+"/live.jpg")
	if missing || !known {
		t.Errorf("200 = missing %v known %v, want false true", missing, known)
	}
	missing, known = askPoster(context.Background(), srv.URL+"/down.jpg")
	if missing || known {
		t.Errorf("503 = missing %v known %v, want false false", missing, known)
	}
}

func TestRememberPosterGoneKeepsADefiniteAnswer(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	raw := srv.URL + "/cached.jpg"
	if missing, _ := rememberPosterGone(context.Background(), raw); !missing {
		t.Fatal("a 404 was kept")
	}
	if missing, _ := rememberPosterGone(context.Background(), raw); !missing {
		t.Fatal("the remembered 404 was kept the second time")
	}
	if hits.Load() != 1 {
		t.Errorf("the host was asked %d times, want 1", hits.Load())
	}
}

func TestAMissIsNotGoneUntilItOutlastsTheErrorCache(t *testing.T) {
	var mode atomic.Int32 // 0 = 404, 1 = 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 0 {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	now := time.Now()
	prev := posterNow
	posterNow = func() time.Time { return now }
	t.Cleanup(func() { posterNow = prev })

	raw := srv.URL + "/flaky.jpg"
	missing, gone := rememberPosterGone(context.Background(), raw)
	if !missing || gone {
		t.Fatalf("first 404 = missing %v gone %v, want true false", missing, gone)
	}
	missing, gone = rememberPosterGone(context.Background(), raw)
	if !missing || gone {
		t.Fatalf("inside the window = missing %v gone %v, want true false", missing, gone)
	}

	now = now.Add(posterMissTTL + time.Second)
	missing, gone = rememberPosterGone(context.Background(), raw)
	if !missing || !gone {
		t.Fatalf("a 404 that survived the window = missing %v gone %v, want true true", missing, gone)
	}

	// A picture that comes back is a picture. The earlier miss does not stick.
	live := srv.URL + "/back.jpg"
	now = time.Now()
	if _, gone := rememberPosterGone(context.Background(), live); gone {
		t.Fatal("the first miss was recorded as gone")
	}
	mode.Store(1)
	now = now.Add(posterMissTTL + time.Second)
	missing, gone = rememberPosterGone(context.Background(), live)
	if missing || gone {
		t.Fatalf("after the picture returned = missing %v gone %v, want false false", missing, gone)
	}
}

func TestFirstLiveSkipsAPosterThatIsGone(t *testing.T) {
	prev := posterGone
	posterGone = func(_ context.Context, raw string) (bool, bool) {
		dead := raw == "https://img.test/gone.jpg"
		return raw == "" || dead, dead
	}
	t.Cleanup(func() { posterGone = prev })

	// The broken poster is listed first, which is what a random draw
	// does when it lands on one. The era should still be filled.
	groups := [][]pick{
		{
			{hit: Hit{ID: "tt0133093", Poster: "https://img.test/gone.jpg"}, era: 1995},
			{hit: Hit{ID: "tt0234215", Poster: "https://img.test/live.jpg"}, era: 1995},
		},
		{
			{hit: Hit{ID: "tt0111161", Poster: ""}, era: 1980},
		},
		{
			{hit: Hit{ID: "tt0000099", Poster: "https://img.test/live.jpg"}, era: 2013},
		},
	}
	got, gone := firstLive(context.Background(), groups)
	// The set, not the sequence: the eras are shuffled so that a
	// window with room for half of them does not always get the same
	// half. Which two came back is the question here.
	ids := map[string]bool{}
	for _, h := range got {
		ids[h.ID] = true
	}
	if len(got) != 2 || !ids["tt0234215"] || !ids["tt0000099"] {
		t.Fatalf("got %+v, want the two films whose posters exist", got)
	}
	// The dead address is reported so it can be written down and
	// repaired. The film with no address at all is not: there is
	// nothing there to have stopped working.
	if len(gone) != 1 || gone[0] != "tt0133093" {
		t.Errorf("gone = %v, want only the 404", gone)
	}
}
