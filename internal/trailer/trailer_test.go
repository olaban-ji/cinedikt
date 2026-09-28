package trailer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"cinedikt/internal/tmdb"
)

func day(d int) time.Time { return time.Date(2020, 1, d, 0, 0, 0, 0, time.UTC) }

func keys(vs []tmdb.Video) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Key
	}
	return out
}

// TestRankOrdersTheCandidates is the README's order, each rule breaking
// only the ties the one before it left: a trailer before a teaser, then
// the studio's own, then English, then the newest.
func TestRankOrdersTheCandidates(t *testing.T) {
	got := Rank([]tmdb.Video{
		{Key: "teaserOfficialEn", Site: "YouTube", Type: "Teaser", Official: true, Language: "en", Published: day(9)},
		{Key: "trailerFanEn", Site: "YouTube", Type: "Trailer", Official: false, Language: "en", Published: day(8)},
		{Key: "trailerOfficialFr", Site: "YouTube", Type: "Trailer", Official: true, Language: "fr", Published: day(7)},
		{Key: "trailerOfficialEnOld", Site: "YouTube", Type: "Trailer", Official: true, Language: "en", Published: day(1)},
		{Key: "trailerOfficialEnNew", Site: "YouTube", Type: "Trailer", Official: true, Language: "en", Published: day(5)},
		{Key: "teaserFanFr", Site: "YouTube", Type: "Teaser", Official: false, Language: "fr", Published: day(2)},
		// Not trailers, not on YouTube, or not a key that can go in an
		// address: none of these is ever offered.
		{Key: "featurette", Site: "YouTube", Type: "Featurette", Official: true, Language: "en", Published: day(9)},
		{Key: "vimeoTrailer", Site: "Vimeo", Type: "Trailer", Official: true, Language: "en", Published: day(9)},
		{Key: "bad key\"", Site: "YouTube", Type: "Trailer", Official: true, Language: "en", Published: day(9)},
	})
	want := []string{
		"trailerOfficialEnNew",
		"trailerOfficialEnOld",
		"trailerOfficialFr",
		"trailerFanEn",
		"teaserOfficialEn",
		"teaserFanFr",
	}
	if !reflect.DeepEqual(keys(got), want) {
		t.Errorf("rank = %v\nwant   %v", keys(got), want)
	}
}

type fakeVideos struct {
	videos []tmdb.Video
	err    error
}

func (f fakeVideos) Videos(context.Context, int) ([]tmdb.Video, error) { return f.videos, f.err }

// oembedServer answers each key with the status in codes, 200 when it is
// not listed, and records the order it was asked in.
func oembedServer(t *testing.T, codes map[string]int) (*OEmbed, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("format = %q", r.URL.Query().Get("format"))
		}
		watch := r.URL.Query().Get("url")
		key, ok := strings.CutPrefix(watch, "https://www.youtube.com/watch?v=")
		if !ok {
			t.Errorf("url = %q", watch)
		}
		mu.Lock()
		asked = append(asked, key)
		mu.Unlock()
		if code, ok := codes[key]; ok {
			w.WriteHeader(code)
			return
		}
		w.Write([]byte(`{"type":"video"}`))
	}))
	t.Cleanup(srv.Close)
	return NewOEmbed(WithEndpoint(srv.URL), WithCheckRate(rate.Inf, 1)), &asked
}

func trailers(ks ...string) []tmdb.Video {
	out := make([]tmdb.Video, len(ks))
	for i, k := range ks {
		// Newest first, so the given order is Rank's order.
		out[i] = tmdb.Video{Key: k, Site: "YouTube", Type: "Trailer", Official: true, Language: "en", Published: day(20 - i)}
	}
	return out
}

func TestPickSkipsWhatYouTubeWillNotEmbed(t *testing.T) {
	// A 400 is how YouTube answers for many ids it no longer holds.
	check, asked := oembedServer(t, map[string]int{"blocked": 401, "private": 403, "gone": 404, "dead": 400})
	got, err := Pick(context.Background(), fakeVideos{videos: trailers("blocked", "private", "gone", "dead", "playable", "later")}, check, 603)
	if err != nil {
		t.Fatal(err)
	}
	if got != "playable" {
		t.Errorf("picked %q, want the first one YouTube will embed", got)
	}
	if want := []string{"blocked", "private", "gone", "dead", "playable"}; !reflect.DeepEqual(*asked, want) {
		t.Errorf("asked %v, want %v and no further", *asked, want)
	}
}

func TestPickWithNothingEmbeddableIsAnAnswer(t *testing.T) {
	check, _ := oembedServer(t, map[string]int{"a": 403, "b": 404})
	got, err := Pick(context.Background(), fakeVideos{videos: trailers("a", "b")}, check, 603)
	if err != nil || got != "" {
		t.Errorf("pick = %q, %v; want no trailer and no error", got, err)
	}
	// TMDb no longer having the movie is the same answer.
	got, err = Pick(context.Background(), fakeVideos{err: tmdb.ErrNotFound}, check, 603)
	if err != nil || got != "" {
		t.Errorf("pick = %q, %v; want no trailer and no error", got, err)
	}
	got, err = Pick(context.Background(), fakeVideos{videos: []tmdb.Video{{Key: "clip", Site: "YouTube", Type: "Clip"}}}, check, 603)
	if err != nil || got != "" {
		t.Errorf("a clip was offered as a trailer: %q, %v", got, err)
	}
}

// A check that fails outright says nothing about the video, so the pick
// fails rather than storing the next candidate for good.
func TestPickFailsWhenACheckFails(t *testing.T) {
	check, asked := oembedServer(t, map[string]int{"first": 503})
	got, err := Pick(context.Background(), fakeVideos{videos: trailers("first", "second")}, check, 603)
	var ce *CheckError
	if !errors.As(err, &ce) || ce.Status != 503 {
		t.Errorf("pick = %q, %v; want a CheckError with the 503", got, err)
	}
	if len(*asked) != 1 {
		t.Errorf("asked %v after a failed check", *asked)
	}
	if _, err := Pick(context.Background(), fakeVideos{err: errors.New("tmdb down")}, check, 603); err == nil {
		t.Error("a failed TMDb call was read as no trailer")
	}
}

func TestTheCheckerKeepsToItsOwnBudget(t *testing.T) {
	check, _ := oembedServer(t, nil)
	check.limiter = rate.NewLimiter(rate.Every(time.Hour), 1)
	if ok, err := check.Embeddable(context.Background(), "one"); err != nil || !ok {
		t.Fatalf("first check = %v, %v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := check.Embeddable(ctx, "two"); err == nil {
		t.Error("a second check went out with the budget spent")
	}
	if d := NewOEmbed(); d.limiter.Limit() != OEmbedRate || d.http.Timeout != OEmbedTimeout {
		t.Errorf("default checker = %v/s, timeout %v", d.limiter.Limit(), d.http.Timeout)
	}
}

// TestPickStopsAfterMaxCandidates: a studio that blocks embedding blocks
// every upload, and a long list checked one by one would outlast the
// budget a reader waits on.
func TestPickStopsAfterMaxCandidates(t *testing.T) {
	codes := map[string]int{}
	var ks []string
	for i := 0; i < MaxCandidates+3; i++ {
		k := "blocked" + string(rune('a'+i))
		ks = append(ks, k)
		codes[k] = 403
	}
	// The one YouTube would play is past the cap.
	delete(codes, ks[len(ks)-1])
	check, asked := oembedServer(t, codes)
	got, err := Pick(context.Background(), fakeVideos{videos: trailers(ks...)}, check, 603)
	if err != nil || got != "" {
		t.Errorf("pick = %q, %v; want none after %d refusals", got, err, MaxCandidates)
	}
	if len(*asked) != MaxCandidates {
		t.Errorf("checked %d, want %d", len(*asked), MaxCandidates)
	}
}
