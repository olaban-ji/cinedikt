package omdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func clientFor(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("key", WithBaseURL(srv.URL), WithRateLimit(1000, 1000))
}

func TestLookupReadsPosterAndDate(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "key" {
			t.Error("the key was not sent")
		}
		w.Write([]byte(`{"Response":"True","Poster":"https://m.media-amazon.com/images/M/x.jpg","Released":"31 Mar 1999"}`))
	})
	got, err := c.Lookup(context.Background(), "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if got.Poster != "https://m.media-amazon.com/images/M/x.jpg" {
		t.Errorf("poster = %q", got.Poster)
	}
	if want := time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC); !got.Released.Equal(want) {
		t.Errorf("released = %v, want %v", got.Released, want)
	}
}

func TestLookupRefusesAPosterCarryingTheKey(t *testing.T) {
	// img.omdbapi.com puts the key in the address. A page using one of
	// those as an <img src> publishes the key to every reader.
	for _, poster := range []string{
		"https://img.omdbapi.com/?apikey=SECRET&i=tt0133093",
		"http://www.omdbapi.com/?apikey=SECRET",
		"N/A",
		"",
		"ftp://elsewhere/x.jpg",
	} {
		c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"Response":"True","Poster":"` + poster + `","Released":"31 Mar 1999"}`))
		})
		got, err := c.Lookup(context.Background(), "tt1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Poster != "" {
			t.Errorf("poster %q was stored as %q", poster, got.Poster)
		}
	}
}

func TestAnAnsweredLookupWithNothingInItIsStillAnAnswer(t *testing.T) {
	// Otherwise this title is asked for again every night for ever.
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Response":"True","Poster":"N/A","Released":"N/A"}`))
	})
	got, err := c.Lookup(context.Background(), "tt1")
	if err != nil {
		t.Fatalf("an empty answer was read as a failure: %v", err)
	}
	if got.Poster != "" || !got.Released.IsZero() {
		t.Errorf("title = %+v", got)
	}
}

func TestParseReleased(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want bool
	}{
		{"31 Mar 1999", true},
		{"01 Jan 2020", true},
		{"N/A", false},
		{"", false},
		{"1999", false}, // a year alone stores no date; month-day stays 0
		{"Mar 1999", false},
		{"garbage", false},
	} {
		got, ok := ParseReleased(c.raw)
		if ok != c.want {
			t.Errorf("ParseReleased(%q) ok = %v, want %v", c.raw, ok, c.want)
		}
		if !c.want && !got.IsZero() {
			t.Errorf("ParseReleased(%q) gave %v", c.raw, got)
		}
	}
}

// TestLookupReadsEveryWayOMDbSaysNo is the rule the poster backfill
// stands on: a definite "I have nothing for this id" is an answer, so
// it is stored and the title is never asked about again. Anything else
// is a fault worth retrying.
//
// The case that matters is "Error getting data." — what OMDb actually
// returns for an id it does not hold. Read as a fault, it put 14% of
// the catalog into a retry loop that re-asked every twenty minutes for
// as long as the process ran.
func TestLookupReadsEveryWayOMDbSaysNo(t *testing.T) {
	for _, message := range []string{
		"Movie not found!",
		"Error getting data.",
		// Said of an id straight out of IMDb's dump, this is OMDb
		// telling us it has no such title.
		"Incorrect IMDb ID.",
		// Theirs to reword, so the match is not case-sensitive.
		"ERROR GETTING DATA.",
	} {
		c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"Response":"False","Error":"` + message + `"}`))
		})
		if _, err := c.Lookup(context.Background(), "tt0000001"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q gave %v, want ErrNotFound", message, err)
		}
	}
}

// And a fault is still a fault: something worth asking about again.
func TestLookupKeepsARealFailureRetryable(t *testing.T) {
	for _, message := range []string{"Invalid API key!", "Something went wrong"} {
		c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"Response":"False","Error":"` + message + `"}`))
		})
		_, err := c.Lookup(context.Background(), "tt0000001")
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrQuota) {
			t.Errorf("%q gave %v, want a plain error the caller will retry", message, err)
		}
	}

	// And an id this app should never have sent stays a fault, however
	// OMDb words it.
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Response":"False","Error":"Incorrect IMDb ID."}`))
	})
	if _, err := c.Lookup(context.Background(), "603"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("a malformed id gave %v, want an error that is not an answer", err)
	}
}

func TestOptionsDoNotDependOnTheirOrder(t *testing.T) {
	// WithHTTPTimeout used to replace the whole http.Client, so giving
	// it after WithConnections silently threw the pool away and left
	// the backfill on two connections.
	for _, c := range []struct {
		name string
		opts []Option
	}{
		{"connections then timeout", []Option{WithConnections(24), WithHTTPTimeout(9 * time.Second)}},
		{"timeout then connections", []Option{WithHTTPTimeout(9 * time.Second), WithConnections(24)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			client := New("key", c.opts...)
			if client.http.Timeout != 9*time.Second {
				t.Errorf("timeout = %v", client.http.Timeout)
			}
			tr, ok := client.http.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport = %T, want the pooled one", client.http.Transport)
			}
			if tr.MaxIdleConnsPerHost != 24 {
				t.Errorf("MaxIdleConnsPerHost = %d, want 24", tr.MaxIdleConnsPerHost)
			}
		})
	}
}

// A refused key is its own answer: the backfill stops on it and says
// so, instead of recording three-quarters of a million lookups as
// failed ones worth retrying tomorrow.
func TestARefusedKeyIsErrKey(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Response":"False","Error":"Invalid API key!"}`))
	})
	if _, err := c.Lookup(context.Background(), "tt0000001"); !errors.Is(err, ErrKey) {
		t.Errorf("Lookup gave %v, want ErrKey", err)
	}
}

func TestLookupAsksForTheFullPlotAndTrimsIt(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("plot"); got != "full" {
			t.Errorf("plot = %q, want full", got)
		}
		w.Write([]byte(`{"Response":"True","Poster":"N/A","Released":"31 Mar 1999","Plot":"  When a beautiful stranger leads computer hacker Neo to a forbidding underworld, he discovers the shocking truth.\n"}`))
	})
	got, err := c.Lookup(context.Background(), "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if want := "When a beautiful stranger leads computer hacker Neo to a forbidding underworld, he discovers the shocking truth."; got.Plot != want {
		t.Errorf("plot = %q, want %q", got.Plot, want)
	}
}

func TestParsePlot(t *testing.T) {
	for raw, want := range map[string]string{
		"N/A":              "",
		" N/A ":            "",
		"":                 "",
		"   ":              "",
		" A plot. ":        "A plot.",
		"N/A is not alone": "N/A is not alone",
	} {
		if got := ParsePlot(raw); got != want {
			t.Errorf("ParsePlot(%q) = %q, want %q", raw, got, want)
		}
	}
}
