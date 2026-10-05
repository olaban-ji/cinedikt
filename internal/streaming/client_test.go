package streaming

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fakeKey = "test-key-not-real"

// matrixShow is the shape of a /shows answer, cut down to what is read.
const matrixShow = `{
  "itemType": "show", "showType": "movie", "id": "82", "imdbId": "tt0133093", "title": "The Matrix",
  "streamingOptions": {
    "us": [
      {"service": {"id": "netflix", "name": "Netflix", "homePage": "https://www.netflix.com/",
        "themeColorCode": "#E50914",
        "imageSet": {"lightThemeImage": "https://img/netflix-light.svg", "darkThemeImage": "https://img/netflix-dark.svg",
          "whiteImage": "https://img/netflix-white.svg"}},
       "type": "subscription", "link": "https://www.netflix.com/title/20557937", "quality": "hd",
       "audios": [{"language": "eng"}], "subtitles": [], "expiresSoon": true, "expiresOn": 1793491200,
       "availableSince": 1700000000},
      {"service": {"id": "prime", "name": "Prime Video", "imageSet": {"lightThemeImage": "l", "darkThemeImage": "d"}},
       "type": "rent", "link": "https://www.primevideo.com/detail/x",
       "price": {"amount": "3.99", "currency": "USD", "formatted": "3.99 USD"}, "expiresSoon": false}
    ]
  }
}`

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(fakeKey, WithBaseURL(srv.URL), WithRate(1000))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestShowAsksForOneCountryWithTheKeyInAHeader(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shows/tt0133093" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("country") != "us" || q.Get("series_granularity") != "show" || q.Get("output_language") != "en" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if strings.Contains(r.URL.String(), fakeKey) {
			t.Error("the key travelled in the address")
		}
		if r.Header.Get("X-API-Key") != fakeKey {
			t.Errorf("X-API-Key = %q", r.Header.Get("X-API-Key"))
		}
		_, _ = w.Write([]byte(matrixShow))
	})
	options, err := c.Show(context.Background(), "tt0133093", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 {
		t.Fatalf("options = %+v", options)
	}
	n := options[0]
	if n.Type != "subscription" || n.Service.ID != "netflix" || n.Service.ImageSet.DarkThemeImage != "https://img/netflix-dark.svg" ||
		n.Link != "https://www.netflix.com/title/20557937" || n.ExpiresOn != 1793491200 {
		t.Errorf("netflix = %+v", n)
	}
	p := options[1]
	if p.Price == nil || !p.Price.Amount.OK || p.Price.Amount.Value != 3.99 || p.Price.Formatted != "3.99 USD" {
		t.Errorf("prime = %+v", p)
	}
}

// TestAShowNobodyCarriesIsDefinite: a 404 is the API's answer that the
// movie is on nothing, and the caller can keep it.
func TestAShowNobodyCarriesIsDefinite(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"show not found"}`, http.StatusNotFound)
	})
	if _, err := c.Show(context.Background(), "tt0000001", "us"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	// A country the API has nothing for, on a show it knows, is an empty
	// list.
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"streamingOptions": {}}`))
	})
	options, err := c.Show(context.Background(), "tt0133093", "ng")
	if err != nil || len(options) != 0 {
		t.Fatalf("options = %+v, err = %v", options, err)
	}
}

// TestARefusedKeyIsItsOwnError: only a person can fix it, and asking
// again changes nothing, so it is not retried.
func TestARefusedKeyIsItsOwnError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		var calls atomic.Int32
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			http.Error(w, `{"message":"invalid api key"}`, status)
		})
		_, err := c.Show(context.Background(), "tt0133093", "us")
		if !errors.Is(err, ErrKey) {
			t.Errorf("%d: err = %v, want ErrKey", status, err)
		}
		if calls.Load() != 1 {
			t.Errorf("%d: asked %d times", status, calls.Load())
		}
	}
}

func TestBusyAndFailingAnswersAreRetriedHonouringRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "7")
			http.Error(w, "slow down", http.StatusTooManyRequests)
		case 2:
			http.Error(w, "oops", http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(matrixShow))
		}
	}))
	defer srv.Close()
	var waits []time.Duration
	c := New(fakeKey, WithBaseURL(srv.URL), WithRate(1000))
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	if _, err := c.Show(context.Background(), "tt0133093", "us"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(waits) != 2 || waits[0] != 7*time.Second {
		t.Errorf("calls = %d, waits = %v; want 3 calls and Retry-After honoured first", calls.Load(), waits)
	}

	// Three failures is the end, and the last of them is the reason.
	calls.Store(0)
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"message":"down"}`, http.StatusServiceUnavailable)
	})
	_, err := c.Show(context.Background(), "tt0133093", "us")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusServiceUnavailable || calls.Load() != maxAttempts {
		t.Errorf("err = %v after %d calls", err, calls.Load())
	}
}

// TestAWaitLongerThanTheDeadlineIsNotTaken: a reader waiting on a first
// ask gives the API seconds, not the minute a Retry-After may ask for.
func TestAWaitLongerThanTheDeadlineIsNotTaken(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	})
	slept := false
	c.sleep = func(context.Context, time.Duration) error {
		slept = true
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Show(ctx, "tt0133093", "us")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusTooManyRequests {
		t.Errorf("err = %v", err)
	}
	if slept || calls.Load() != 1 {
		t.Errorf("slept = %v after %d calls; the wait outlasted the deadline", slept, calls.Load())
	}
}

// TestTheKeyIsInNoError: whatever goes wrong, the error a caller logs
// does not carry the key.
func TestTheKeyIsInNoError(t *testing.T) {
	for _, h := range []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", http.StatusUnauthorized) },
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad", http.StatusBadRequest) },
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) },
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusInternalServerError) },
	} {
		c := testClient(t, h)
		_, err := c.Show(context.Background(), "tt0133093", "us")
		if err == nil {
			t.Fatal("no error")
		}
		if strings.Contains(err.Error(), fakeKey) {
			t.Errorf("error %q carries the key", err)
		}
	}
	// And a server that is not there at all.
	c := New(fakeKey, WithBaseURL("http://127.0.0.1:1"), WithRate(1000))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := c.Show(context.Background(), "tt0133093", "us"); err == nil || strings.Contains(err.Error(), fakeKey) {
		t.Errorf("err = %v", err)
	}
}

// TestTheKeyNeverFollowsARedirect: Go carries a custom header such as
// X-API-Key across a redirect to any host, so a redirect is not followed
// at all, and the host it names never sees the key.
func TestTheKeyNeverFollowsARedirect(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		if r.Header.Get("X-API-Key") != "" {
			t.Errorf("another host was sent the key")
		}
		_, _ = w.Write([]byte(matrixShow))
	}))
	t.Cleanup(other.Close)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusFound)
	})
	_, err := c.Show(context.Background(), "tt0133093", "us")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusFound {
		t.Errorf("err = %v, want the redirect as a StatusError", err)
	}
	if elsewhere.Load() != 0 {
		t.Errorf("the redirect was followed %d times", elsewhere.Load())
	}
}

func TestShowRefusesWhatIsNotAnIdOrACountry(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("asked about %s", r.URL)
	})
	for _, c2 := range [][2]string{{"nm0000206", "us"}, {"tt0133093", "US"}, {"tt0133093", "usa"}, {"tt0133093", ""}} {
		if _, err := c.Show(context.Background(), c2[0], c2[1]); err == nil {
			t.Errorf("%v was asked", c2)
		}
	}
}

func TestCountriesAreLowercasedAndNamed(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/countries" || r.Header.Get("X-API-Key") != fakeKey {
			t.Errorf("%s with key %q", r.URL.Path, r.Header.Get("X-API-Key"))
		}
		_, _ = w.Write([]byte(`{
		  "us": {"countryCode": "us", "name": "United States", "services": [{"id": "netflix"}]},
		  "gb": {"countryCode": "GB", "name": "United Kingdom", "services": []},
		  "xx": {"countryCode": "", "name": ""},
		  "bad": {"countryCode": "abc", "name": "Nowhere"}
		}`))
	})
	got, err := c.Countries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Country{{"gb", "United Kingdom"}, {"us", "United States"}, {"xx", "XX"}}
	if len(got) != len(want) {
		t.Fatalf("countries = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("countries[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
