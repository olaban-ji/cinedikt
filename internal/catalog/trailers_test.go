package catalog

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"cinedikt/internal/notify"
	"cinedikt/internal/tmdb"
)

// TestReaskTrailerOnlyForARecentFilmsOldNull is the rule the endpoint
// applies to a stored answer: a "no trailer" older than a week, for a
// film out in the last twelve months, is asked again. Nothing else is.
func TestReaskTrailerOnlyForARecentFilmsOldNull(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	recent := now.AddDate(0, -3, 0)
	for _, c := range []struct {
		name     string
		key      string
		askedAt  time.Time
		released time.Time
		want     bool
	}{
		{"recent film, null asked 8 days ago", "", now.Add(-8 * 24 * time.Hour), recent, true},
		{"recent film, null asked 6 days ago", "", now.Add(-6 * 24 * time.Hour), recent, false},
		{"recent film, a key is never re-asked", "vKQi3bBA1y8", now.Add(-60 * 24 * time.Hour), recent, false},
		{"film from 13 months ago", "", now.Add(-30 * 24 * time.Hour), now.AddDate(0, -13, 0), false},
		{"film not out yet", "", now.Add(-8 * 24 * time.Hour), now.AddDate(0, 2, 0), false},
		{"film out today", "", now.Add(-8 * 24 * time.Hour), now, true},
		{"release unknown", "", now.Add(-30 * 24 * time.Hour), time.Time{}, false},
	} {
		if got := ReaskTrailer(c.key, c.askedAt, c.released, now); got != c.want {
			t.Errorf("%s: re-ask = %v, want %v", c.name, got, c.want)
		}
	}
}

// fakeClips answers each TMDb id in keys with one trailer, any other id
// with no clips at all, and records the order it was asked in.
type fakeClips struct {
	mu    sync.Mutex
	keys  map[int]string
	asked []int
	err   map[int]error
}

func (f *fakeClips) Videos(_ context.Context, id int) ([]tmdb.Video, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	if err := f.err[id]; err != nil {
		return nil, err
	}
	key, ok := f.keys[id]
	if !ok {
		return nil, nil
	}
	return []tmdb.Video{{Key: key, Site: "YouTube", Type: "Trailer", Official: true, Language: "en"}}, nil
}

// embedAll says yes to every video but the ones in refused.
type embedAll struct{ refused map[string]bool }

func (e embedAll) Embeddable(_ context.Context, key string) (bool, error) {
	return !e.refused[key], nil
}

func trailerRow(t *testing.T, s *Store, tconst string) (key string, ok bool) {
	t.Helper()
	var k *string
	err := s.pool.QueryRow(context.Background(),
		`SELECT youtube_key FROM meta.trailers WHERE tconst = $1`, tconst).Scan(&k)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	if k != nil {
		key = *k
	}
	return key, true
}

// TestTheTrailerJobWalksItsThreeQueuesInOrder: well-known films never
// asked, most voted first; then a recent film's week-old "no trailer";
// then answers old enough that TMDb's terms want them asked again. A
// film not out yet keeps its "no trailer" until it is.
func TestTheTrailerJobWalksItsThreeQueuesInOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	for _, stmt := range []string{
		`DELETE FROM meta.trailers`,
		`DELETE FROM meta.trailer_queue`,
		`DELETE FROM meta.tmdb`,
		`DELETE FROM meta.posters`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()),
		    ('tt0234215', 604, now()), ('tt0000001', 1, now()),
		    ('tt0000002', 2, now()), ('tt0000003', NULL, now())`,
		// The unrated film came out a month ago, and was told "no
		// trailer" eight days ago: due.
		`INSERT INTO meta.posters (tconst, status, fetched_at, released)
		 VALUES ('tt0000001', 'ok', now(), current_date - 30)`,
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at)
		 VALUES ('tt0000001', NULL, now() - interval '8 days')`,
		// The documentary comes out in two months: its eight-day-old
		// "no trailer" stands until then.
		`INSERT INTO meta.posters (tconst, status, fetched_at, released)
		 VALUES ('tt0000002', 'ok', now(), current_date + 60)`,
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at)
		 VALUES ('tt0000002', NULL, now() - interval '8 days')`,
		// Reloaded's answer is 151 days old: due, last.
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at)
		 VALUES ('tt0234215', 'oldReloaded', now() - interval '151 days')`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	clips := &fakeClips{keys: map[int]string{278: "shawshankKey", 603: "matrixKey", 604: "reloadedKey", 2: "tooSoonKey"}}
	var sink recordingSink
	job := &TrailerJob{Store: s, Videos: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 1_000_000, Batch: 10, Notify: &sink}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []int{278, 603, 1, 604}; !reflect.DeepEqual(clips.asked, want) {
		t.Fatalf("asked %v, want %v", clips.asked, want)
	}
	for id, want := range map[string]string{
		"tt0111161": "shawshankKey",
		"tt0133093": "matrixKey",
		"tt0234215": "reloadedKey",
	} {
		if got, _ := trailerRow(t, s, id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	// Still nothing for the unrated film, and that answer is kept.
	if key, ok := trailerRow(t, s, "tt0000001"); !ok || key != "" {
		t.Errorf("unrated = %q (row %v), want a stored null", key, ok)
	}
	if key, ok := trailerRow(t, s, "tt0000002"); !ok || key != "" {
		t.Errorf("a film not out yet = %q (row %v), want its null left alone", key, ok)
	}
	if fin := sink.of(notify.Finished); len(fin) != 1 || fin[0].Job != notify.JobTrailers || fin[0].Done != 3 || fin[0].None != 1 {
		t.Errorf("finished = %+v", fin)
	}

	// Every answer is fresh now, the null it just stored included, so a
	// second pass asks nothing.
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(clips.asked) != 4 {
		t.Errorf("caught up, but asked %v", clips.asked[4:])
	}
}

// TestTheTrailerJobStoresANullAndKeepsAFaultRetryable: nothing YouTube
// will embed is an answer and is kept; a failed lookup is not.
func TestTheTrailerJobStoresANullAndKeepsAFaultRetryable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	for _, stmt := range []string{
		`DELETE FROM meta.trailers`,
		`DELETE FROM meta.trailer_queue`,
		`DELETE FROM meta.tmdb`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now())`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	clips := &fakeClips{
		keys: map[int]string{278: "blockedKey"},
		err:  map[int]error{603: errors.New("tmdb down")},
	}
	job := &TrailerJob{Store: s, Videos: clips, Check: embedAll{refused: map[string]bool{"blockedKey": true}}, Logger: quietLogger(), MinVotes: 0}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if key, ok := trailerRow(t, s, "tt0111161"); !ok || key != "" {
		t.Errorf("shawshank = %q (row %v), want a stored null", key, ok)
	}
	if _, ok := trailerRow(t, s, "tt0133093"); ok {
		t.Error("a failed lookup was stored as an answer")
	}
	// Asked once in the pass, not over and over.
	if len(clips.asked) != 2 {
		t.Errorf("asked %v", clips.asked)
	}
}

// TestATrailerPassEndsOnABatchOfFailures: a failed lookup stays at the
// head of its kind, and a head made of nothing but this pass's failures
// ends the pass. A TMDb or a YouTube that is down costs one batch of
// lookups, not one for every title queued.
func TestATrailerPassEndsOnABatchOfFailures(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	for _, stmt := range []string{
		`DELETE FROM meta.trailers`,
		`DELETE FROM meta.trailer_queue`,
		`DELETE FROM meta.tmdb`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()), ('tt0234215', 604, now())`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	down := errors.New("tmdb down")
	clips := &fakeClips{err: map[int]error{278: down, 603: down, 604: down}}
	job := &TrailerJob{Store: s, Videos: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 0, Batch: 2}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []int{278, 603}; !reflect.DeepEqual(clips.asked, want) {
		t.Errorf("asked %v, want only the head batch %v", clips.asked, want)
	}
}

func TestTheTrailerRowHasEverythingTheEndpointNeeds(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	for _, stmt := range []string{
		`DELETE FROM meta.trailers`,
		`DELETE FROM meta.tmdb`,
		`DELETE FROM meta.posters`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt0133093', 603, now())`,
		`INSERT INTO meta.posters (tconst, status, fetched_at, released) VALUES ('tt0133093', 'ok', now(), '1999-03-31')`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	row, err := s.Trailer(ctx, "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if !row.Title || row.Asked || !row.TMDbAsked || row.TMDbID != 603 {
		t.Errorf("row = %+v", row)
	}
	if want := time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC); !row.Released.Equal(want) {
		t.Errorf("released = %v, want %v", row.Released, want)
	}
	if err := s.KeepTrailer(ctx, "tt0133093", "vKQi3bBA1y8"); err != nil {
		t.Fatal(err)
	}
	row, err = s.Trailer(ctx, "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if !row.Asked || row.Key != "vKQi3bBA1y8" || time.Since(row.AskedAt) > time.Minute {
		t.Errorf("after keeping: %+v", row)
	}
	// Reloaded has no date of its own, so its year stands in.
	row, err = s.Trailer(ctx, "tt0234215")
	if err != nil {
		t.Fatal(err)
	}
	if !row.Title || row.TMDbAsked || !row.Released.Equal(time.Date(2003, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("reloaded = %+v", row)
	}
	// And a title the catalog does not hold says so.
	row, err = s.Trailer(ctx, "tt9999999")
	if err != nil {
		t.Fatal(err)
	}
	if row.Title {
		t.Errorf("an unknown title = %+v", row)
	}
}
