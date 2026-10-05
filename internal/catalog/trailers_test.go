package catalog

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"cinedikt/internal/config"
	"cinedikt/internal/notify"
	"cinedikt/internal/tmdb"
)

// TestReaskTrailerOnlyForARecentFilmsOldNull is the recent-film rule: a
// "no trailer" older than a week, for a film out in the last twelve
// months, is asked again. Nothing else is, by this rule.
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

// TestADueTrailerIsARecentFilmsNullOrAnythingPast150Days is what the
// endpoint marks for the job while it serves the answer it has.
func TestADueTrailerIsARecentFilmsNullOrAnythingPast150Days(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(-5, 0, 0)
	for _, c := range []struct {
		name string
		row  TrailerRow
		want bool
	}{
		{"never asked", TrailerRow{Title: true}, false},
		{"a fresh key", TrailerRow{Title: true, Asked: true, Key: "k", AskedAt: now.Add(-time.Hour), Released: old}, false},
		{"a key 151 days old", TrailerRow{Title: true, Asked: true, Key: "k", AskedAt: now.Add(-151 * 24 * time.Hour), Released: old}, true},
		{"a null 151 days old", TrailerRow{Title: true, Asked: true, AskedAt: now.Add(-151 * 24 * time.Hour), Released: old}, true},
		{"an old film's week-old null", TrailerRow{Title: true, Asked: true, AskedAt: now.Add(-8 * 24 * time.Hour), Released: old}, false},
		{"a recent film's week-old null", TrailerRow{Title: true, Asked: true, AskedAt: now.Add(-8 * 24 * time.Hour), Released: now.AddDate(0, -1, 0)}, true},
	} {
		if got := c.row.Due(now); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}

// fakeClips is TMDb for the trailer job. Each TMDb id in keys has one
// trailer, any other id no clips at all; an IMDb id in found is that
// movie, any other is not one TMDb has. It records what it was asked,
// in order.
type fakeClips struct {
	mu    sync.Mutex
	keys  map[int]string
	err   map[int]error
	found map[string]tmdb.Found
	asked []int
	finds []string
	// onVideos, when set, runs as each list is asked for, before it is
	// answered.
	onVideos func(id int)
}

func (f *fakeClips) FindByIMDb(_ context.Context, imdbID string) (tmdb.Found, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finds = append(f.finds, imdbID)
	got, ok := f.found[imdbID]
	if !ok {
		return tmdb.Found{}, tmdb.ErrNotFound
	}
	return got, nil
}

func (f *fakeClips) Videos(_ context.Context, id int) ([]tmdb.Video, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	if f.onVideos != nil {
		f.onVideos(id)
	}
	if err := f.err[id]; err != nil {
		return nil, err
	}
	key, ok := f.keys[id]
	if !ok {
		return nil, nil
	}
	return []tmdb.Video{{Key: key, Site: "YouTube", Type: "Trailer", Official: true, Language: "en"}}, nil
}

func (f *fakeClips) seen() (asked []int, finds []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.asked...), append([]string(nil), f.finds...)
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

// tmdbMatch reads what meta.tmdb says of a title: ok is false when TMDb
// was never asked, and id is zero for "no movie".
func tmdbMatch(t *testing.T, s *Store, tconst string) (id int, ok bool) {
	t.Helper()
	var got *int
	err := s.pool.QueryRow(context.Background(),
		`SELECT tmdb_id FROM meta.tmdb WHERE tconst = $1`, tconst).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		id = *got
	}
	return id, true
}

// trailerFixture publishes the fixture catalog with nothing known about
// trailers, then runs the setup statements.
func trailerFixture(t *testing.T, s *Store, stmts ...string) {
	t.Helper()
	publishFixture(t, s)
	for _, stmt := range append([]string{
		`DELETE FROM meta.trailers`,
		`DELETE FROM meta.trailer_queue`,
		`DELETE FROM meta.tmdb`,
		`DELETE FROM meta.tmdb_queue`,
		`DELETE FROM meta.posters`,
		`DELETE FROM meta.synopses`,
	}, stmts...) {
		if _, err := s.pool.Exec(context.Background(), stmt); err != nil {
			t.Fatal(err)
		}
	}
}

// TestTheTrailerJobWalksItsQueuesInOrder: films never asked, most voted
// first; then a recent film's week-old "no trailer"; then answers old
// enough that TMDb's terms want them asked again. A film not out yet
// keeps its "no trailer" until it is.
func TestTheTrailerJobWalksItsQueuesInOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
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
	)
	clips := &fakeClips{keys: map[int]string{278: "shawshankKey", 603: "matrixKey", 604: "reloadedKey", 2: "tooSoonKey"}}
	var sink recordingSink
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 1_000_000, Batch: 10, Notify: &sink}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked, finds := clips.seen()
	if want := []int{278, 603, 1, 604}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked %v, want %v", asked, want)
	}
	if len(finds) != 0 {
		t.Errorf("matched %v, all of which had TMDb ids", finds)
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
	if asked, _ := clips.seen(); len(asked) != 4 {
		t.Errorf("caught up, but asked %v", asked[4:])
	}
}

// TestTheTrailerJobServesWantedFilmsFirst: films a reader has opened go
// before the sweep, newest first, whatever their votes and whatever
// kind of film they are, and so does a wanted answer that has come due.
func TestTheTrailerJobServesWantedFilmsFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()),
		    ('tt0234215', 604, now()), ('tt0000001', 1, now())`,
		// Reloaded's answer is past its 150 days.
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at)
		 VALUES ('tt0234215', 'oldReloaded', now() - interval '151 days')`,
	)
	// Opened in this order, so the documentary, which no sweep reaches
	// and nothing has matched, is the newest want.
	for _, id := range []string{"tt0234215", "tt0000001", "tt0000002"} {
		if err := s.markTrailersWanted(ctx, []string{id}); err != nil {
			t.Fatal(err)
		}
	}
	clips := &fakeClips{
		keys:  map[int]string{1: "unratedKey", 99: "docKey", 278: "shawshankKey", 603: "matrixKey", 604: "reloadedKey"},
		found: map[string]tmdb.Found{"tt0000002": {ID: 99}},
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 1_000_000, Batch: 10}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked, finds := clips.seen()
	if want := []int{99, 1, 604, 278, 603}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked %v, want the wanted films, newest first, then the sweep: %v", asked, want)
	}
	if want := []string{"tt0000002"}; !reflect.DeepEqual(finds, want) {
		t.Errorf("matched %v, want %v", finds, want)
	}
	for id, want := range map[string]string{"tt0000002": "docKey", "tt0000001": "unratedKey", "tt0234215": "reloadedKey"} {
		if got, _ := trailerRow(t, s, id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	// An answer takes the title out of the queue, want and all.
	var left int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM meta.trailer_queue`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d titles still queued after every one was answered", left)
	}
}

// TestAMarkQueuesOnlyWhatNeedsAsking: a film with no answer, or with one
// that has come due, is marked; a fresh answer, which the job may have
// given a moment before the mark was written, is not, and nor is a
// title the catalog does not hold.
func TestAMarkQueuesOnlyWhatNeedsAsking(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at) VALUES
		    ('tt0133093', 'matrixKey', now()),
		    ('tt0234215', 'oldReloaded', now() - interval '151 days')`,
	)
	if err := s.markTrailersWanted(ctx, []string{"tt0133093", "tt0234215", "tt0000001", "tt7777777"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.tconsts(ctx, `SELECT tconst FROM meta.trailer_queue WHERE wanted_at IS NOT NULL ORDER BY tconst`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0000001", "tt0234215"}; !reflect.DeepEqual(got, want) {
		t.Errorf("wanted %v, want %v", got, want)
	}

	// A refill keeps the due answer's want: it is the answer waiting to
	// be replaced, not one that has been.
	if err := s.refillTrailerQueue(ctx, 0); err != nil {
		t.Fatal(err)
	}
	got, err = s.tconsts(ctx, `SELECT tconst FROM meta.trailer_queue WHERE wanted_at IS NOT NULL ORDER BY tconst`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0000001", "tt0234215"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after a refill, wanted %v, want %v", got, want)
	}
}

// TestTheTrailerSweepReachesEveryFilm: at the default floor of zero the
// sweep takes every film the id matcher would match, the unrated one
// included, whatever TMDb has said about each. A known id is asked for
// its clips; "TMDb has no movie" is "no trailer" at no cost; a film
// nothing has matched is matched here, once, and the match is kept.
func TestTheTrailerSweepReachesEveryFilm(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0133093', 603, now()), ('tt0111161', NULL, now())`,
	)
	clips := &fakeClips{
		keys: map[int]string{603: "matrixKey", 604: "reloadedKey", 1: "unratedKey"},
		// Reloaded's clips fail the first time, after its match.
		err: map[int]error{604: errors.New("tmdb down")},
		found: map[string]tmdb.Found{
			"tt0234215": {ID: 604, Overview: "Neo returns."},
			"tt0000001": {ID: 1},
		},
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: config.DefaultTrailerSweepMinVotes}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked, finds := clips.seen()
	// Most voted first: Shawshank costs nothing, and the documentary and
	// the adult title are not films the sweep takes.
	if want := []int{603, 604, 1}; !reflect.DeepEqual(asked, want) {
		t.Errorf("asked %v, want %v", asked, want)
	}
	if want := []string{"tt0234215", "tt0000001"}; !reflect.DeepEqual(finds, want) {
		t.Errorf("matched %v, want %v", finds, want)
	}
	for id, want := range map[string]string{"tt0133093": "matrixKey", "tt0000001": "unratedKey", "tt0111161": ""} {
		if got, ok := trailerRow(t, s, id); !ok || got != want {
			t.Errorf("%s = %q (row %v), want %q", id, got, ok, want)
		}
	}
	if _, ok := trailerRow(t, s, "tt0234215"); ok {
		t.Error("a failed lookup was stored as an answer")
	}
	for _, id := range []string{"tt0000002", "tt0000003"} {
		if _, ok := trailerRow(t, s, id); ok {
			t.Errorf("%s was swept", id)
		}
	}
	// The matches are kept the way the id matcher keeps them, overview
	// and all, the failed lookup's included.
	if id, ok := tmdbMatch(t, s, "tt0234215"); !ok || id != 604 {
		t.Errorf("reloaded's match = %d (row %v), want 604", id, ok)
	}
	if id, ok := tmdbMatch(t, s, "tt0000001"); !ok || id != 1 {
		t.Errorf("unrated's match = %d (row %v), want 1", id, ok)
	}
	if text, source, _ := synopsisRow(t, s, "tt0234215"); text != "Neo returns." || source != "tmdb" {
		t.Errorf("reloaded's overview = %q from %q", text, source)
	}

	// TMDb is back. The next pass asks for Reloaded's clips by the id it
	// kept, and does not match it again.
	clips.mu.Lock()
	clips.err = nil
	clips.mu.Unlock()
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked, finds = clips.seen()
	if want := []int{603, 604, 1, 604}; !reflect.DeepEqual(asked, want) {
		t.Errorf("asked %v, want %v", asked, want)
	}
	if len(finds) != 2 {
		t.Errorf("matched %v, want each film matched once", finds)
	}
	if got, _ := trailerRow(t, s, "tt0234215"); got != "reloadedKey" {
		t.Errorf("reloaded = %q", got)
	}
}

// TestAMarkMidRoundPutsTheWantedFilmNext: a reader who opens a film while
// the sweep is part way through a round waits on the lookup in hand, not
// on the rest of the round.
func TestAMarkMidRoundPutsTheWantedFilmNext(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()),
		    ('tt0234215', 604, now()), ('tt0000001', 1, now()),
		    ('tt0000002', 2, now())`,
	)
	wanted := make(chan struct{}, 1)
	clips := &fakeClips{}
	clips.onVideos = func(id int) {
		if id != 278 {
			return
		}
		// While Shawshank is being asked about, a reader opens the
		// documentary: the mark is written, then the job is woken.
		if err := s.markTrailersWanted(ctx, []string{"tt0000002"}); err != nil {
			t.Error(err)
		}
		wanted <- struct{}{}
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 0, Batch: 10, Wanted: wanted}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked, _ := clips.seen()
	if want := []int{278, 2, 603, 604, 1}; !reflect.DeepEqual(asked, want) {
		t.Errorf("asked %v, want the wanted film straight after the lookup in hand: %v", asked, want)
	}
}

// TestAReadersMarkIsWrittenAndWakesTheTrailerJob is the endpoint's half:
// WantTrailer returns at once, and the buffer writes the mark and sends
// the signal the runner's listener turns into the job's wake.
func TestAReadersMarkIsWrittenAndWakesTheTrailerJob(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s)

	conn, err := pgx.Connect(ctx, os.Getenv("CATALOG_TEST_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "LISTEN "+NotifyTrailerWanted); err != nil {
		t.Fatal(err)
	}

	s.WantTrailer("tt0000001")

	wait, cancel := context.WithTimeout(ctx, 3*wantFlush)
	defer cancel()
	note, err := conn.WaitForNotification(wait)
	if err != nil {
		t.Fatalf("no %s signal: %v", NotifyTrailerWanted, err)
	}
	if note.Channel != NotifyTrailerWanted {
		t.Errorf("signal on %q", note.Channel)
	}
	var wantedAt *time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT wanted_at FROM meta.trailer_queue WHERE tconst = 'tt0000001'`).Scan(&wantedAt); err != nil {
		t.Fatal(err)
	}
	if wantedAt == nil {
		t.Error("the mark was written without a want")
	}
}

// TestTheTrailerJobStoresANullAndKeepsAFaultRetryable: nothing YouTube
// will embed is an answer and is kept; a failed lookup is not.
func TestTheTrailerJobStoresANullAndKeepsAFaultRetryable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now())`,
	)
	clips := &fakeClips{
		keys: map[int]string{278: "blockedKey"},
		err:  map[int]error{603: errors.New("tmdb down")},
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{refused: map[string]bool{"blockedKey": true}}, Logger: quietLogger(), MinVotes: 0}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if key, ok := trailerRow(t, s, "tt0111161"); !ok || key != "" {
		t.Errorf("shawshank = %q (row %v), want a stored null", key, ok)
	}
	if _, ok := trailerRow(t, s, "tt0133093"); ok {
		t.Error("a failed lookup was stored as an answer")
	}
	// Asked once in the pass, not over and over. The two films nothing
	// had matched are matched, found to have no movie, and cost no list.
	if asked, _ := clips.seen(); len(asked) != 2 {
		t.Errorf("asked %v", asked)
	}
}

// TestATrailerPassEndsOnABatchOfFailures: a failed lookup stays at the
// head of its kind, and a head made of nothing but this pass's failures
// gives way to the next kind, and at the last ends the pass. A TMDb or
// a YouTube that is down costs a round of lookups, not one for every
// title queued.
func TestATrailerPassEndsOnABatchOfFailures(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()), ('tt0234215', 604, now())`,
	)
	down := errors.New("tmdb down")
	clips := &fakeClips{err: map[int]error{278: down, 603: down, 604: down}}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 0, Batch: 2}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked, _ := clips.seen(); !reflect.DeepEqual(asked, []int{278, 603}) {
		t.Errorf("asked %v, want only the head batch [278 603]", asked)
	}
}

// TestAWantedFilmThatKeepsFailingDoesNotHoldUpTheSweep: a film a reader
// opened whose lookup fails is tried once a pass, and the sweep goes on
// behind it.
func TestAWantedFilmThatKeepsFailingDoesNotHoldUpTheSweep(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()), ('tt0000002', 2, now())`,
	)
	if err := s.markTrailersWanted(ctx, []string{"tt0000002"}); err != nil {
		t.Fatal(err)
	}
	clips := &fakeClips{
		keys: map[int]string{278: "shawshankKey", 603: "matrixKey"},
		err:  map[int]error{2: errors.New("tmdb down")},
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 1_000_000, Batch: 10}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked, _ := clips.seen(); !reflect.DeepEqual(asked, []int{2, 278, 603}) {
		t.Errorf("asked %v, want the wanted film once, then the sweep", asked)
	}
}

// TestAWantedFilmThatFailedIsAskedAgainWhenMarkedAgain: a reader still
// waiting on a film whose lookup failed asks again, and the new mark
// has the film looked up again in the same pass rather than after it.
func TestAWantedFilmThatFailedIsAskedAgainWhenMarkedAgain(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()), ('tt0000002', 2, now())`,
	)
	if err := s.markTrailersWanted(ctx, []string{"tt0000002"}); err != nil {
		t.Fatal(err)
	}
	wanted := make(chan struct{}, 1)
	clips := &fakeClips{
		keys: map[int]string{2: "docKey", 278: "shawshankKey", 603: "matrixKey"},
		err:  map[int]error{2: errors.New("tmdb down")},
	}
	clips.onVideos = func(id int) {
		if id != 278 {
			return
		}
		// The documentary has failed and the sweep has moved on. TMDb
		// is back, and the reader's page asks again, which marks the
		// film again and wakes the job. onVideos runs under the fake's
		// lock, so the error map is its to change.
		clips.err = nil
		if err := s.markTrailersWanted(ctx, []string{"tt0000002"}); err != nil {
			t.Error(err)
		}
		wanted <- struct{}{}
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 1_000_000, Batch: 10, Wanted: wanted}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked, _ := clips.seen(); !reflect.DeepEqual(asked, []int{2, 278, 2, 603}) {
		t.Errorf("asked %v, want the wanted film again straight after the new mark", asked)
	}
	if got, _ := trailerRow(t, s, "tt0000002"); got != "docKey" {
		t.Errorf("documentary = %q, want docKey", got)
	}
}

// TestAFilmThatFailedInTheSweepIsAskedAgainOnceWanted: a try made for
// the sweep holds back only the sweep. A reader who opens the film later
// in the pass has it looked up again at once, not once the pass, which
// can run for days, is over.
func TestAFilmThatFailedInTheSweepIsAskedAgainOnceWanted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		    ('tt0111161', 278, now()), ('tt0133093', 603, now()),
		    ('tt0234215', 604, now()), ('tt0000001', 1, now())`,
	)
	wanted := make(chan struct{}, 1)
	clips := &fakeClips{
		keys: map[int]string{278: "shawshankKey", 603: "matrixKey"},
		// Shawshank, the head of the sweep, fails.
		err: map[int]error{278: errors.New("tmdb down")},
	}
	clips.onVideos = func(id int) {
		if id != 603 {
			return
		}
		// While the Matrix is being asked about, TMDb is back and a
		// reader opens Shawshank. onVideos runs under the fake's lock,
		// so the error map is its to change.
		clips.err = nil
		if err := s.markTrailersWanted(ctx, []string{"tt0111161"}); err != nil {
			t.Error(err)
		}
		wanted <- struct{}{}
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 0, Batch: 10, Wanted: wanted}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked, _ := clips.seen(); !reflect.DeepEqual(asked, []int{278, 603, 278, 604, 1}) {
		t.Errorf("asked %v, want Shawshank again straight after the lookup in hand", asked)
	}
	if got, _ := trailerRow(t, s, "tt0111161"); got != "shawshankKey" {
		t.Errorf("shawshank = %q, want shawshankKey", got)
	}
}

// TestEveryTrailerRequestFailingIsAFailure: fifty faults and not one
// answer from TMDb is TMDb, or the way there, and the pass says so
// instead of finishing, even when a film TMDb has no movie for was
// answered along the way without a request.
func TestEveryTrailerRequestFailingIsAFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// The fixture holds too few films for a round of failures. These are
	// better known than any of its films, so a floor above the fixture's
	// votes leaves the sweep to them, and the best known of all is one
	// TMDb has no movie for: answered first, and for free.
	const films = `FROM generate_series(0, 60) g`
	const tconst = `'tt99' || lpad(g::text, 5, '0')`
	trailerFixture(t, s,
		`INSERT INTO `+Live+`.titles (tconst, primary_title, original_title, is_adult, start_year)
		 SELECT `+tconst+`, 'Film ' || g, 'Film ' || g, false, 2000 `+films,
		`INSERT INTO `+Live+`.principals (tconst, ordering, nconst, category)
		 SELECT `+tconst+`, 1, 'nm0000206', 'actor' `+films,
		`INSERT INTO `+Live+`.ratings (tconst, average_rating, num_votes)
		 SELECT `+tconst+`, 7.0, 6000000 - g `+films,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at)
		 SELECT `+tconst+`, nullif(900000 + g, 900000), now() `+films,
	)
	t.Cleanup(func() {
		for _, table := range []string{"meta.tmdb", "meta.trailer_queue", "meta.trailers", Live + ".titles", Live + ".principals", Live + ".ratings"} {
			_, _ = s.pool.Exec(context.Background(), `DELETE FROM `+table+` WHERE tconst LIKE 'tt9900%'`)
		}
	})
	clips := &fakeClips{err: map[int]error{}}
	for g := 1; g <= 60; g++ {
		clips.err[900000+g] = errors.New("tmdb: HTTP 502")
	}
	job := &TrailerJob{Store: s, TMDb: clips, Check: embedAll{}, Logger: quietLogger(), MinVotes: 5_000_000}
	err := job.Run(ctx)
	var all *LookupsFailedError
	if !errors.As(err, &all) || all.Provider != "TMDb" || all.Count < failedLookups {
		t.Fatalf("err = %v, want a LookupsFailedError for at least %d", err, failedLookups)
	}
	// The free answer was given, and still did not hide the outage.
	if key, ok := trailerRow(t, s, "tt9900000"); !ok || key != "" {
		t.Errorf("the film TMDb has no movie for = %q (row %v), want a stored null", key, ok)
	}
}

func TestTheTrailerRowHasEverythingTheEndpointNeeds(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	trailerFixture(t, s,
		`INSERT INTO meta.posters (tconst, status, fetched_at, released) VALUES ('tt0133093', 'ok', now(), '1999-03-31')`,
	)
	row, err := s.Trailer(ctx, "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if !row.Title || row.Asked {
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
	if !row.Asked || row.Key != "vKQi3bBA1y8" || time.Since(row.AskedAt) > time.Minute || row.Due(time.Now()) {
		t.Errorf("after keeping: %+v", row)
	}
	// Reloaded has no date of its own, so its year stands in.
	row, err = s.Trailer(ctx, "tt0234215")
	if err != nil {
		t.Fatal(err)
	}
	if !row.Title || row.Asked || !row.Released.Equal(time.Date(2003, 1, 1, 0, 0, 0, 0, time.UTC)) {
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
