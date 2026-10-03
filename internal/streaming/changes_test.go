package streaming

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// changesPage is the shape of a /changes answer, cut down to what is
// read, with a show for each way the feed can carry one.
const changesPage = `{
  "changes": [
    {"changeType": "new", "itemType": "show", "showId": "82", "showType": "movie", "season": null, "episode": null,
     "service": {"id": "netflix", "name": "Netflix"}, "streamingOptionType": "subscription", "addon": null,
     "timestamp": 1790000000, "link": "https://www.netflix.com/title/20557937"},
    {"changeType": "new", "itemType": "show", "showId": "83", "showType": "movie", "timestamp": 1790000100},
    {"changeType": "new", "itemType": "show", "showId": "84", "showType": "movie", "timestamp": null},
    {"changeType": "new", "itemType": "show", "showId": "85", "showType": "movie", "timestamp": 1790000200},
    {"changeType": "new", "itemType": "show", "showId": "86", "showType": "movie", "timestamp": 1790000300},
    {"changeType": "new", "itemType": "show", "showId": "87", "showType": "movie", "timestamp": 1790000400}
  ],
  "shows": {
    "82": {"itemType": "show", "showType": "movie", "id": "82", "imdbId": "tt0133093", "title": "The Matrix",
      "streamingOptions": {"us": [
        {"service": {"id": "netflix", "name": "Netflix", "imageSet": {"lightThemeImage": "l", "darkThemeImage": "d"}},
         "type": "subscription", "link": "https://www.netflix.com/title/20557937", "expiresOn": 1793491200}
      ], "gb": []}},
    "83": {"id": "83", "imdbId": "tt0111161", "title": "The Shawshank Redemption"},
    "84": {"id": "84", "imdbId": "tt0234215", "streamingOptions": {"us": "not a list"}},
    "85": {"id": "85", "imdbId": "not-an-imdb-id", "streamingOptions": {"us": []}},
    "86": {"id": "86", "imdbId": "tt0000001", "streamingOptions": null},
    "87": "not a show"
  },
  "hasMore": true,
  "nextCursor": "1790000400:2378449044"
}`

// TestChangesAsksForOnePageOfMovieChanges: whole movies, oldest first,
// inside the window, with the key in a header, and the shows read one by
// one.
func TestChangesAsksForOnePageOfMovieChanges(t *testing.T) {
	from := time.Unix(1789900000, 0)
	to := time.Unix(1790003600, 0)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/changes" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		for k, want := range map[string]string{
			"country": "us", "change_type": "updated", "item_type": "show", "show_type": "movie",
			"order_direction": "asc", "output_language": "en",
			"from": "1789900000", "to": "1790003600", "cursor": "1789999999:1",
		} {
			if q.Get(k) != want {
				t.Errorf("%s = %q, want %q", k, q.Get(k), want)
			}
		}
		if strings.Contains(r.URL.String(), fakeKey) || r.Header.Get("X-API-Key") != fakeKey {
			t.Errorf("the key travelled wrongly: %s, header %q", r.URL, r.Header.Get("X-API-Key"))
		}
		_, _ = w.Write([]byte(changesPage))
	})
	page, err := c.Changes(context.Background(), ChangesQuery{
		Country: "us", Type: ChangeUpdated, From: from, To: to, Cursor: "1789999999:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || page.NextCursor != "1790000400:2378449044" {
		t.Errorf("hasMore %v, nextCursor %q", page.HasMore, page.NextCursor)
	}
	if len(page.Changes) != 6 {
		t.Fatalf("changes = %+v", page.Changes)
	}
	first := page.Changes[0]
	if first.Type != ChangeNew || first.ShowID != "82" || !first.At.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("first = %+v", first)
	}
	if !page.Changes[2].At.IsZero() {
		t.Errorf("a change with no time = %v", page.Changes[2].At)
	}

	// A show carrying its options: written straight from the feed.
	matrix := page.Shows["82"]
	if matrix.IMDbID != "tt0133093" {
		t.Errorf("82 = %+v", matrix)
	}
	options, ok := matrix.Options("us")
	if !ok || len(options) != 1 || options[0].Service.ID != "netflix" || options[0].ExpiresOn != 1793491200 {
		t.Errorf("82 in us = %+v, %v", options, ok)
	}
	// An empty list the feed did send is an answer: on nothing there.
	if options, ok := matrix.Options("gb"); !ok || options == nil || len(options) != 0 {
		t.Errorf("82 in gb = %#v, %v; want an empty list", options, ok)
	}
	// A country the feed left out says nothing.
	if _, ok := matrix.Options("de"); ok {
		t.Error("82 in de was taken for an answer")
	}

	// Without the field, with a list that cannot be read, or with null,
	// the show says nothing about where it streams.
	for _, id := range []string{"83", "84", "86"} {
		show, found := page.Shows[id]
		if !found || show.IMDbID == "" {
			t.Errorf("%s was not read: %+v", id, show)
		}
		if options, ok := show.Options("us"); ok {
			t.Errorf("%s in us = %+v, taken for an answer", id, options)
		}
	}
	// A show with no IMDb id, or that is not a show, matches nothing.
	for _, id := range []string{"85", "87"} {
		if show, found := page.Shows[id]; found {
			t.Errorf("%s = %+v, want it left out", id, show)
		}
	}
}

// TestChangesReadsShowsListedInsteadOfKeyed: the documentation keys the
// shows by id; a list of them, each naming its id, reads the same.
func TestChangesReadsShowsListedInsteadOfKeyed(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"changes": [{"changeType": "removed", "showId": "82", "timestamp": 1790000000}],
		  "shows": [{"id": "82", "imdbId": "tt0133093", "streamingOptions": {"us": []}}], "hasMore": false}`))
	})
	page, err := c.Changes(context.Background(), ChangesQuery{Country: "us", Type: ChangeRemoved})
	if err != nil {
		t.Fatal(err)
	}
	if options, ok := page.Shows["82"].Options("us"); !ok || len(options) != 0 || page.HasMore || page.NextCursor != "" {
		t.Errorf("page = %+v", page)
	}
}

// TestChangesFindsAShowByItsOwnIdWhenKeyedOtherwise: the documentation
// keys the shows by the id a change names; a map keyed by anything else
// still finds each show by the id it carries, without taking the place of
// a show that is keyed by that id.
func TestChangesFindsAShowByItsOwnIdWhenKeyedOtherwise(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"changes": [
		    {"changeType": "updated", "showId": "1", "timestamp": 1790000000},
		    {"changeType": "updated", "showId": "82", "timestamp": 1790000100}],
		  "shows": {
		    "movie/603": {"id": "1", "imdbId": "tt0133093", "streamingOptions": {"us": []}},
		    "82": {"id": "82", "imdbId": "tt0111161"},
		    "movie/82": {"id": "82", "imdbId": "tt0234215"}},
		  "hasMore": false}`))
	})
	page, err := c.Changes(context.Background(), ChangesQuery{Country: "us", Type: ChangeUpdated})
	if err != nil {
		t.Fatal(err)
	}
	if show, ok := page.Shows["1"]; !ok || show.IMDbID != "tt0133093" {
		t.Errorf("show 1 = %+v, %v; want it found by its own id", show, ok)
	}
	if options, ok := page.Shows["1"].Options("us"); !ok || len(options) != 0 {
		t.Errorf("show 1 in us = %+v, %v", options, ok)
	}
	if show := page.Shows["82"]; show.IMDbID != "tt0111161" {
		t.Errorf("show 82 = %+v, want the one keyed by its id", show)
	}
}

// TestChangesFailsLikeEveryOtherAsk: a refused key is ErrKey and not
// retried, a busy feed is retried, and a query that could never be
// answered is not sent.
func TestChangesFailsLikeEveryOtherAsk(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"message":"invalid api key"}`, http.StatusUnauthorized)
	})
	if _, err := c.Changes(context.Background(), ChangesQuery{Country: "us", Type: ChangeNew}); !errors.Is(err, ErrKey) || calls != 1 {
		t.Errorf("err = %v after %d calls, want ErrKey after one", err, calls)
	}

	calls = 0
	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"message":"down"}`, http.StatusServiceUnavailable)
	})
	_, err := c.Changes(context.Background(), ChangesQuery{Country: "us", Type: ChangeNew})
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusServiceUnavailable || calls != maxAttempts {
		t.Errorf("err = %v after %d calls", err, calls)
	}

	c = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("asked %s", r.URL)
	})
	for _, q := range []ChangesQuery{
		{Country: "US", Type: ChangeNew},
		{Country: "", Type: ChangeNew},
		{Country: "us", Type: "expiring"},
		{Country: "us", Type: ""},
	} {
		if _, err := c.Changes(context.Background(), q); err == nil {
			t.Errorf("%+v was asked", q)
		}
	}
}
