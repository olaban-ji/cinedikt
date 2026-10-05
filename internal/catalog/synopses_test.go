package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"cinedikt/internal/config"
	"cinedikt/internal/notify"
	"cinedikt/internal/omdb"
	"cinedikt/internal/tmdb"
)

// clearSynopses empties everything the synopsis paths write. The tests
// share one database, and meta outlives every generation.
func clearSynopses(t *testing.T, s *Store) {
	t.Helper()
	for _, table := range []string{"meta.synopses", "meta.synopsis_queue"} {
		if _, err := s.pool.Exec(context.Background(), `DELETE FROM `+table); err != nil {
			t.Fatal(err)
		}
	}
}

// synopsisRow reads one stored synopsis. ok is false when there is no
// row; text is empty for a null overview.
func synopsisRow(t *testing.T, s *Store, tconst string) (text, source string, ok bool) {
	t.Helper()
	var overview *string
	err := s.pool.QueryRow(context.Background(),
		`SELECT overview, source FROM meta.synopses WHERE tconst = $1`, tconst).Scan(&overview, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	if overview != nil {
		text = *overview
	}
	return text, source, true
}

// omdbAnswered reports whether OMDb has answered for a title, whatever
// text the row shows.
func omdbAnswered(t *testing.T, s *Store, tconst string) bool {
	t.Helper()
	var answered bool
	err := s.pool.QueryRow(context.Background(),
		`SELECT omdb_at IS NOT NULL FROM meta.synopses WHERE tconst = $1`, tconst).Scan(&answered)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return answered
}

// posterPassHas sets meta.posters to exactly these titles, each with
// the status given, as if the OMDb poster pass had got that far. The
// synopsis job asks only about titles the pass has answered.
func posterPassHas(t *testing.T, s *Store, statuses map[string]string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.posters`); err != nil {
		t.Fatal(err)
	}
	for id, status := range statuses {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO meta.posters (tconst, status, fetched_at) VALUES ($1, $2, now())`, id, status); err != nil {
			t.Fatal(err)
		}
	}
}

// ageSynopsis moves a row's stamp back, as if it had been fetched that
// long ago.
func ageSynopsis(t *testing.T, s *Store, tconst string, by time.Duration) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`UPDATE meta.synopses SET fetched_at = now() - $2::interval WHERE tconst = $1`,
		tconst, by.String()); err != nil {
		t.Fatal(err)
	}
}

func TestThePosterPassKeepsThePlotWithEveryAnswer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.posters`); err != nil {
		t.Fatal(err)
	}
	clearSynopses(t, s)

	fake := &fakeOMDb{
		answers: map[string]omdb.Title{
			"tt0133093": {Poster: "https://m.media-amazon.com/matrix.jpg", Plot: "Neo learns the truth."},
			"tt0234215": {Poster: "https://m.media-amazon.com/reloaded.jpg"},
		},
		errs: map[string]error{
			"tt0111161": omdb.ErrNotFound,
			"tt0000001": errors.New("omdb down"),
		},
	}
	job := &PosterJob{Store: s, Client: fake, Logger: quietLogger(), Batch: 100, Workers: 1}
	if err := job.Run(ctx, Live); err != nil {
		t.Fatal(err)
	}
	if text, source, ok := synopsisRow(t, s, "tt0133093"); !ok || text != "Neo learns the truth." || source != "omdb" {
		t.Errorf("matrix = %q from %q (row %v)", text, source, ok)
	}
	if !omdbAnswered(t, s, "tt0133093") {
		t.Error("the poster pass's answer did not count as OMDb having answered")
	}
	// Answers with no plot are answers: nobody has one, and the title
	// is not asked again.
	for _, id := range []string{"tt0234215", "tt0111161"} {
		if text, source, ok := synopsisRow(t, s, id); !ok || text != "" || source != "omdb" {
			t.Errorf("%s = %q from %q (row %v), want OMDb's null", id, text, source, ok)
		}
	}
	// A failed lookup says nothing about the synopsis.
	if _, _, ok := synopsisRow(t, s, "tt0000001"); ok {
		t.Error("a failed lookup was written down as no synopsis")
	}
}

// TestOMDbsPlotComesBeforeTMDbsOverview is the precedence both writes
// keep between them, whichever arrives first.
func TestOMDbsPlotComesBeforeTMDbsOverview(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)

	omdbSays := func(id string, plot string) {
		t.Helper()
		if err := s.writeOMDbSynopses(ctx, []string{id}, []*string{textOrNull(plot)}); err != nil {
			t.Fatal(err)
		}
	}
	tmdbSays := func(id string, overview string) {
		t.Helper()
		if err := s.keepTMDbOverview(ctx, id, overview); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(id, text, source string) {
		t.Helper()
		gotText, gotSource, ok := synopsisRow(t, s, id)
		if !ok || gotText != text || gotSource != source {
			t.Errorf("%s = %q from %q (row %v), want %q from %q", id, gotText, gotSource, ok, text, source)
		}
	}

	// An OMDb plot is never replaced by TMDb.
	omdbSays("tt0133093", "OMDb's plot.")
	tmdbSays("tt0133093", "TMDb's overview.")
	expect("tt0133093", "OMDb's plot.", "omdb")

	// TMDb fills in first, and OMDb's plot replaces it when it comes.
	tmdbSays("tt0234215", "TMDb's overview.")
	expect("tt0234215", "TMDb's overview.", "tmdb")
	omdbSays("tt0234215", "OMDb's plot.")
	expect("tt0234215", "OMDb's plot.", "omdb")

	// TMDb's overview is not OMDb's answer, so OMDb is still to be asked.
	tmdbSays("tt0111161", "TMDb's overview.")
	if omdbAnswered(t, s, "tt0111161") {
		t.Error("TMDb's overview counted as OMDb having answered")
	}
	// OMDb having nothing does not throw away a fresh overview, but it
	// is an answer all the same, and OMDb is not asked again.
	omdbSays("tt0111161", "")
	expect("tt0111161", "TMDb's overview.", "tmdb")
	if !omdbAnswered(t, s, "tt0111161") {
		t.Error("OMDb's \"none\" beside a fresh overview was not recorded")
	}
	// ...but one past its 150 days is dropped for OMDb's "none".
	ageSynopsis(t, s, "tt0111161", TMDbRefreshAfter+24*time.Hour)
	omdbSays("tt0111161", "")
	expect("tt0111161", "", "omdb")

	// Where OMDb has nothing, TMDb's overview fills the gap, and OMDb's
	// answer still stands.
	tmdbSays("tt0111161", "TMDb's overview.")
	expect("tt0111161", "TMDb's overview.", "tmdb")
	if !omdbAnswered(t, s, "tt0111161") {
		t.Error("TMDb's overview erased OMDb's answer")
	}

	// And TMDb having nothing writes nothing: a row would keep the
	// synopsis job from asking OMDb.
	tmdbSays("tt0000001", "")
	if _, _, ok := synopsisRow(t, s, "tt0000001"); ok {
		t.Error("an empty TMDb overview was written down")
	}
}

// TestTheTMDbJobsKeepTheOverviewForFree: every place a FindByIMDb answer
// is already saved keeps the overview that came on it.
func TestTheTMDbJobsKeepTheOverviewForFree(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	for _, table := range []string{"meta.tmdb", "meta.tmdb_queue"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table); err != nil {
			t.Fatal(err)
		}
	}
	// The Matrix already has OMDb's plot, which the matcher's answer
	// must not replace.
	if err := s.writeOMDbSynopses(ctx, []string{"tt0133093"}, []*string{textOrNull("OMDb's plot.")}); err != nil {
		t.Fatal(err)
	}
	find := &fakeFinder{answers: map[string]tmdb.Found{
		"tt0111161": {ID: 278, Overview: "Two imprisoned men bond."},
		"tt0133093": {ID: 603, Overview: "TMDb's overview."},
	}}
	job := &TMDbIDJob{Store: s, Client: find, Logger: quietLogger(), Batch: 10}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if text, source, _ := synopsisRow(t, s, "tt0111161"); text != "Two imprisoned men bond." || source != "tmdb" {
		t.Errorf("shawshank = %q from %q", text, source)
	}
	if omdbAnswered(t, s, "tt0111161") {
		t.Error("the matcher's overview counted as OMDb having answered")
	}
	if text, source, _ := synopsisRow(t, s, "tt0133093"); text != "OMDb's plot." || source != "omdb" {
		t.Errorf("matrix = %q from %q; TMDb replaced OMDb", text, source)
	}

	// The poster stand-in, and anything else that keeps a lookup.
	if err := s.KeepTMDbPoster(ctx, "tt0234215", tmdb.Found{ID: 604, Overview: "Neo fights on."}); err != nil {
		t.Fatal(err)
	}
	if text, _, _ := synopsisRow(t, s, "tt0234215"); text != "Neo fights on." {
		t.Errorf("reloaded = %q", text)
	}
}

// TestTheSynopsisJobServesDemandFirst is its queue order: a title a
// reader has met, whatever its votes; then the well known, most voted
// first; and only then TMDb's overviews that have come due.
func TestTheSynopsisJobServesDemandFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)

	// Reloaded's overview is TMDb's, and old enough to be due.
	if err := s.keepTMDbOverview(ctx, "tt0234215", "An old overview."); err != nil {
		t.Fatal(err)
	}
	ageSynopsis(t, s, "tt0234215", TMDbRefreshAfter+24*time.Hour)
	// A reader has met the unrated film, which no sweep would reach.
	if err := s.markSynopsesWanted(ctx, []string{"tt0000001"}); err != nil {
		t.Fatal(err)
	}
	posterPassHas(t, s, map[string]string{
		"tt0000001": "ok", "tt0111161": "ok", "tt0133093": "ok", "tt0234215": "ok",
		"tt0000002": "ok", "tt0000003": "ok",
	})

	fake := &fakeOMDb{answers: map[string]omdb.Title{
		"tt0000001": {Plot: "An unrated plot."},
		"tt0111161": {Plot: "Hope."},
		"tt0133093": {Plot: "Red pill."},
	}}
	var sink recordingSink
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: 1_000_000, Batch: 10, Workers: 1, Notify: &sink}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// The documentary and the adult title are below the floor and
	// nobody met them; Reloaded's overview is due, so it goes last.
	want := []string{"tt0000001", "tt0111161", "tt0133093", "tt0234215"}
	if got := fake.asked; !reflect.DeepEqual(got, want) {
		t.Fatalf("asked %v, want %v", got, want)
	}
	if text, source, _ := synopsisRow(t, s, "tt0000001"); text != "An unrated plot." || source != "omdb" {
		t.Errorf("unrated = %q from %q", text, source)
	}
	// OMDb had nothing for Reloaded, so the stale overview is dropped.
	if text, source, _ := synopsisRow(t, s, "tt0234215"); text != "" || source != "omdb" {
		t.Errorf("reloaded = %q from %q, want OMDb's null", text, source)
	}
	if fin := sink.of(notify.Finished); len(fin) != 1 || fin[0].Job != notify.JobSynopses || fin[0].Done != 3 || fin[0].None != 1 {
		t.Errorf("finished = %+v", fin)
	}

	// Caught up: nothing left to spend a request on.
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.asked) != len(want) {
		t.Errorf("caught up, but asked %v", fake.asked[len(want):])
	}
}

// TestFilmsCarryTheSynopsis is the read: the card's detail says what the
// film is about, and says nothing when nobody knows.
func TestFilmsCarryTheSynopsis(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	if err := s.writeOMDbSynopses(ctx, []string{"tt0133093", "tt0234215"}, []*string{textOrNull("Red pill."), nil}); err != nil {
		t.Fatal(err)
	}
	films, err := s.Films(ctx, "tt0133093", []string{"tt0133093", "tt0234215", "tt0111161"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range films {
		got[f.ID] = f.Synopsis
	}
	if got["tt0133093"] != "Red pill." || got["tt0234215"] != "" || got["tt0111161"] != "" {
		t.Errorf("synopses = %v", got)
	}
	grid, err := s.Grid(ctx, "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	if grid.Anchor.Synopsis != "Red pill." {
		t.Errorf("anchor synopsis = %q", grid.Anchor.Synopsis)
	}
}

func TestTheSynopsisJobStopsOnTheDailyLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	posterPassHas(t, s, map[string]string{
		"tt0000001": "ok", "tt0111161": "ok", "tt0133093": "ok", "tt0234215": "ok",
	})
	fake := &fakeOMDb{
		answers: map[string]omdb.Title{"tt0111161": {Plot: "Hope."}},
		errs:    map[string]error{"tt0133093": omdb.ErrQuota},
	}
	var sink recordingSink
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: 0, Batch: 10, Workers: 1, Notify: &sink}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0133093"}; !reflect.DeepEqual(fake.asked, want) {
		t.Errorf("asked %v, want %v and nothing after the limit", fake.asked, want)
	}
	if paused := sink.of(notify.Paused); len(paused) != 1 || paused[0].Cause != notify.DailyLimit || paused[0].Job != notify.JobSynopses {
		t.Errorf("paused = %+v", paused)
	}
	// What it learned before the limit is kept; the rest waits.
	if text, _, _ := synopsisRow(t, s, "tt0111161"); text != "Hope." {
		t.Errorf("shawshank = %q", text)
	}
	if _, _, ok := synopsisRow(t, s, "tt0133093"); ok {
		t.Error("a title the limit stopped was written down")
	}
}

// fiveVotes gives the fixture's unrated film five votes, the kind of
// film only a sweep with no floor reaches.
func fiveVotes(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO `+Live+`.ratings (tconst, average_rating, num_votes) VALUES ('tt0000001', 6.0, 5)`); err != nil {
		t.Fatal(err)
	}
}

// hookedOMDb is fakeOMDb with something that happens as a title is asked
// about, the way a reader's mark can land while a pass is running.
type hookedOMDb struct {
	*fakeOMDb
	on map[string]func()
}

func (f hookedOMDb) Lookup(ctx context.Context, id string) (omdb.Title, error) {
	if hook := f.on[id]; hook != nil {
		hook()
	}
	return f.fakeOMDb.Lookup(ctx, id)
}

// pausingOMDb is fakeOMDb with the real client's daily-limit pause: the
// lookup that is told the day's requests are spent pauses the client,
// and the jobs look at PausedUntil before they ask anything.
type pausingOMDb struct {
	*fakeOMDb
	mu    sync.Mutex
	until time.Time
}

func (f *pausingOMDb) Lookup(ctx context.Context, id string) (omdb.Title, error) {
	got, err := f.fakeOMDb.Lookup(ctx, id)
	if errors.Is(err, omdb.ErrQuota) {
		f.mu.Lock()
		f.until = time.Now().Add(time.Hour)
		f.mu.Unlock()
	}
	return got, err
}

func (f *pausingOMDb) PausedUntil() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.until
}

// reset is the next day: the limit is back.
func (f *pausingOMDb) reset() {
	f.mu.Lock()
	f.until = time.Time{}
	f.mu.Unlock()
	f.fakeOMDb.mu.Lock()
	f.fakeOMDb.errs = nil
	f.fakeOMDb.mu.Unlock()
}

// TestTheSynopsisSweepReachesEveryFilmBehindWhatReadersMeet is the
// default floor of zero: the sweep asks about every film a map can draw,
// a five-vote one included, most voted first. A reader's mark that lands
// mid-sweep is looked at before the next batch, so it goes ahead of the
// rest of the sweep however long that is.
func TestTheSynopsisSweepReachesEveryFilmBehindWhatReadersMeet(t *testing.T) {
	if config.DefaultSynopsisSweepMinVotes != 0 {
		t.Fatalf("the synopsis sweep's default floor is %d, want 0", config.DefaultSynopsisSweepMinVotes)
	}
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	fiveVotes(t, s)
	posterPassHas(t, s, map[string]string{
		"tt0000001": "ok", "tt0111161": "ok", "tt0133093": "ok", "tt0234215": "ok",
		"tt0000002": "ok", "tt0000003": "ok",
	})
	fake := hookedOMDb{
		fakeOMDb: &fakeOMDb{answers: map[string]omdb.Title{
			"tt0000001": {Plot: "Five votes' worth."},
			"tt0111161": {Plot: "Hope."},
			"tt0133093": {Plot: "Red pill."},
			"tt0234215": {Plot: "More pills."},
		}},
		// While Shawshank is asked about, a reader is shown Reloaded.
		on: map[string]func(){"tt0111161": func() {
			if err := s.markSynopsesWanted(ctx, []string{"tt0234215"}); err != nil {
				t.Error(err)
			}
		}},
	}
	// One title a batch, so the order is the whole of what is checked.
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: config.DefaultSynopsisSweepMinVotes, Batch: 1, Workers: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// Reloaded goes ahead of the Matrix, which has more votes, and the
	// five-vote film is reached last. The documentary and the adult
	// title are not films a map draws.
	want := []string{"tt0111161", "tt0234215", "tt0133093", "tt0000001"}
	if !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want %v", fake.asked, want)
	}
	if text, source, _ := synopsisRow(t, s, "tt0000001"); text != "Five votes' worth." || source != "omdb" {
		t.Errorf("five-vote film = %q from %q", text, source)
	}
}

// TestASpentDailyLimitPausesTheSweepAndItResumes: a sweep of every film
// can outlast a day's OMDb requests. When the limit is spent the pass
// pauses, keeping what it learned and losing nothing it did not; while
// the client is paused a pass asks nothing, whatever woke it; and once
// the limit resets, the next pass starts with what a reader has met and
// then the title the limit stopped.
func TestASpentDailyLimitPausesTheSweepAndItResumes(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	fiveVotes(t, s)
	posterPassHas(t, s, map[string]string{
		"tt0000001": "ok", "tt0111161": "ok", "tt0133093": "ok", "tt0234215": "ok",
	})
	fake := &pausingOMDb{fakeOMDb: &fakeOMDb{
		answers: map[string]omdb.Title{
			"tt0000001": {Plot: "Five votes' worth."},
			"tt0111161": {Plot: "Hope."},
			"tt0133093": {Plot: "Red pill."},
			"tt0234215": {Plot: "More pills."},
		},
		errs: map[string]error{"tt0133093": omdb.ErrQuota},
	}}
	var sink recordingSink
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: config.DefaultSynopsisSweepMinVotes, Batch: 1, Workers: 1, Notify: &sink}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0133093"}; !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want %v and nothing after the limit", fake.asked, want)
	}
	paused := sink.of(notify.Paused)
	if len(paused) != 1 || paused[0].Cause != notify.DailyLimit || !paused[0].NextTry.Equal(fake.PausedUntil()) {
		t.Errorf("paused = %+v, want the daily limit until %v", paused, fake.PausedUntil())
	}
	if text, _, _ := synopsisRow(t, s, "tt0111161"); text != "Hope." {
		t.Errorf("shawshank = %q; what was learned before the limit was lost", text)
	}
	if _, _, ok := synopsisRow(t, s, "tt0133093"); ok {
		t.Error("the title the limit stopped was written down, and will not be asked again")
	}

	// A reader is shown the five-vote film while the limit is spent. The
	// mark wakes the job, and it asks nothing.
	if err := s.markSynopsesWanted(ctx, []string{"tt0000001"}); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.asked) != 2 {
		t.Fatalf("asked %v while the limit was spent", fake.asked[2:])
	}

	// The next day.
	fake.reset()
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0133093", "tt0000001", "tt0133093", "tt0234215"}; !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want %v", fake.asked, want)
	}
	for id, want := range map[string]string{"tt0133093": "Red pill.", "tt0234215": "More pills.", "tt0000001": "Five votes' worth."} {
		if text, _, _ := synopsisRow(t, s, id); text != want {
			t.Errorf("%s = %q, want %q", id, text, want)
		}
	}
	if fin := sink.of(notify.Finished); len(fin) != 1 || fin[0].Done != 3 {
		t.Errorf("finished = %+v, want the resumed pass's three", fin)
	}
}

// TestThePosterPassAndTheSynopsisJobShareOnePause is why the synopsis
// job is handed the poster pass's client rather than one of its own:
// OMDb's daily limit is per key, and when it is spent both have to stop.
func TestThePosterPassAndTheSynopsisJobShareOnePause(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"Response":"False","Error":"Request limit reached!"}`))
	}))
	t.Cleanup(srv.Close)
	client := omdb.New("key", omdb.WithBaseURL(srv.URL), omdb.WithRateLimit(1000, 1000))
	if _, err := client.Lookup(context.Background(), "tt0133093"); !errors.Is(err, omdb.ErrQuota) {
		t.Fatalf("lookup = %v, want the daily limit", err)
	}
	// Neither job touches the store, or OMDb, while the client is
	// paused. A nil store would panic if either did.
	posters := &PosterJob{Client: client, Logger: quietLogger()}
	synopses := &SynopsisJob{Client: client, Logger: quietLogger()}
	if err := posters.Run(context.Background(), Live); err != nil {
		t.Fatal(err)
	}
	if err := synopses.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Errorf("OMDb was asked %d times, want only the one that spent the limit", hits.Load())
	}

	// And the runner really does hand both jobs the one client.
	r := &Runner{OMDbKey: "key", Logger: quietLogger()}
	_, p, sy := r.build()
	if p == nil || sy == nil {
		t.Fatal("an OMDb key built neither job")
	}
	if p.Client != sy.Client {
		t.Error("the poster pass and the synopsis job were given different clients")
	}
}

// TestTheRunnersTMDbClientWaitsOnTheProcessLimiter: the jobs' client and
// the request path's must draw on the same budget.
func TestTheRunnersTMDbClientWaitsOnTheProcessLimiter(t *testing.T) {
	limiter := tmdb.NewLimiter(20)
	r := &Runner{TMDbAuth: tmdb.Auth{APIKey: "k"}, TMDbLimiter: limiter, Logger: quietLogger()}
	if c := r.tmdbClient(); c == nil || c.Limiter() != limiter {
		t.Error("the runner's TMDb client has a limiter of its own")
	}
}

// TestDueTMDbDataIsDroppedAndOMDbsIsKept is the backstop for TMDb's six
// months: overviews past the cutoff the runner gives, and trailers past
// tmdbForgetDays, go whatever became of their titles. OMDb's plots stay.
func TestDueTMDbDataIsDroppedAndOMDbsIsKept(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	if err := s.keepTMDbOverview(ctx, "tt0111161", "Due."); err != nil {
		t.Fatal(err)
	}
	if err := s.keepTMDbOverview(ctx, "tt0234215", "Fresh."); err != nil {
		t.Fatal(err)
	}
	if err := s.keepTMDbOverview(ctx, "tt0000001", "Long overdue."); err != nil {
		t.Fatal(err)
	}
	if err := s.writeOMDbSynopses(ctx, []string{"tt0133093"}, []*string{textOrNull("Kept.")}); err != nil {
		t.Fatal(err)
	}
	ageSynopsis(t, s, "tt0111161", TMDbRefreshAfter+24*time.Hour)
	ageSynopsis(t, s, "tt0000001", (tmdbForgetDays+1)*24*time.Hour)
	ageSynopsis(t, s, "tt0133093", 3*TMDbRefreshAfter)
	for _, stmt := range []string{
		`DELETE FROM meta.trailers`,
		// Due for a re-ask, but not yet for the backstop.
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at) VALUES ('tt0133093', 'matrixKey', now() - interval '151 days')`,
		// Past the backstop: one whose title is still here, and one whose
		// title has left the catalog, which no re-ask would ever reach.
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at) VALUES ('tt0111161', NULL, now() - interval '176 days')`,
		`INSERT INTO meta.trailers (tconst, youtube_key, asked_at) VALUES ('tt7777777', 'goneKey', now() - interval '176 days')`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	// With an OMDb key, overviews wait for the backstop too.
	gone, err := s.forgetDueTMDbData(ctx, tmdbForgetDays)
	if err != nil {
		t.Fatal(err)
	}
	if gone.overviews != 1 || gone.trailers != 2 {
		t.Errorf("dropped %d overviews and %d trailers, want 1 and 2", gone.overviews, gone.trailers)
	}
	if _, _, ok := synopsisRow(t, s, "tt0000001"); ok {
		t.Error("a TMDb overview past the backstop was kept")
	}
	if text, _, _ := synopsisRow(t, s, "tt0111161"); text != "Due." {
		t.Errorf("an overview due for a re-ask was dropped before the backstop: %q", text)
	}
	for _, id := range []string{"tt0111161", "tt7777777"} {
		if _, ok := trailerRow(t, s, id); ok {
			t.Errorf("%s: a trailer past the backstop was kept", id)
		}
	}
	if key, ok := trailerRow(t, s, "tt0133093"); !ok || key != "matrixKey" {
		t.Errorf("a trailer due only for a re-ask was dropped: %q (row %v)", key, ok)
	}

	// With no OMDb key, nothing would re-ask an overview, so each goes
	// the day it comes due.
	if gone, err = s.forgetDueTMDbData(ctx, tmdbRefreshDays); err != nil {
		t.Fatal(err)
	}
	if gone.overviews != 1 {
		t.Errorf("dropped %d overviews, want 1", gone.overviews)
	}
	if _, _, ok := synopsisRow(t, s, "tt0111161"); ok {
		t.Error("a TMDb overview past 150 days was kept")
	}
	if text, _, _ := synopsisRow(t, s, "tt0234215"); text != "Fresh." {
		t.Errorf("a fresh overview was dropped: %q", text)
	}
	if text, _, _ := synopsisRow(t, s, "tt0133093"); text != "Kept." {
		t.Errorf("an OMDb plot was dropped: %q", text)
	}
}

// TestATMDbOverviewDoesNotStopOMDbBeingAsked: TMDb's overview arriving
// first is shown until OMDb answers, and does not stand in for that
// answer. OMDb's plot then replaces it; OMDb having none leaves a fresh
// overview showing, and is not asked again.
func TestATMDbOverviewDoesNotStopOMDbBeingAsked(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	posterPassHas(t, s, map[string]string{"tt0111161": "ok", "tt0234215": "ok", "tt0133093": "ok"})
	// The Matrix's plot is OMDb's already; the other two have only the
	// overview the TMDb jobs saved on their way past.
	if err := s.writeOMDbSynopses(ctx, []string{"tt0133093"}, []*string{textOrNull("Red pill.")}); err != nil {
		t.Fatal(err)
	}
	for id, overview := range map[string]string{"tt0111161": "TMDb on Shawshank.", "tt0234215": "TMDb on Reloaded."} {
		if err := s.keepTMDbOverview(ctx, id, overview); err != nil {
			t.Fatal(err)
		}
	}
	films, err := s.Films(ctx, "tt0133093", []string{"tt0111161"})
	if err != nil {
		t.Fatal(err)
	}
	if len(films) != 1 || films[0].Synopsis != "TMDb on Shawshank." {
		t.Errorf("before OMDb answers, films = %+v", films)
	}

	fake := &fakeOMDb{answers: map[string]omdb.Title{"tt0111161": {Plot: "OMDb on Shawshank."}}}
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: 1000, Batch: 10, Workers: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0234215"}; !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want %v: an overview of TMDb's kept OMDb from being asked", fake.asked, want)
	}
	if text, source, _ := synopsisRow(t, s, "tt0111161"); text != "OMDb on Shawshank." || source != "omdb" {
		t.Errorf("shawshank = %q from %q, want OMDb's plot", text, source)
	}
	if text, source, _ := synopsisRow(t, s, "tt0234215"); text != "TMDb on Reloaded." || source != "tmdb" {
		t.Errorf("reloaded = %q from %q, want TMDb's fresh overview kept", text, source)
	}

	// Both have OMDb's answer now, so neither is asked again.
	job.refilled = time.Time{}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fake.asked) != 2 {
		t.Errorf("asked again: %v", fake.asked[2:])
	}
}

// TestTheSynopsisJobLeavesUnansweredTitlesToThePosterPass: a title the
// poster pass has not answered yet, or whose lookup failed and is
// waiting to be retried, is the pass's to ask, and it keeps the plot
// from that answer. Asking here too would spend two of the day's OMDb
// requests on one title. A reader's mark waits in the queue meanwhile.
//
// It is also the refill's timing: a pass the rest interval did not
// bring works from the queue as it stands, without scanning the catalog.
func TestTheSynopsisJobLeavesUnansweredTitlesToThePosterPass(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	// Shawshank is answered, the Matrix's lookup failed, and Reloaded
	// and the unrated film have not been reached.
	posterPassHas(t, s, map[string]string{"tt0111161": "ok", "tt0133093": "missing"})
	if err := s.markSynopsesWanted(ctx, []string{"tt0000001"}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeOMDb{answers: map[string]omdb.Title{
		"tt0111161": {Plot: "Hope."},
		"tt0000001": {Plot: "Unrated."},
		"tt0234215": {Plot: "Reloaded."},
	}}
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: 0, Batch: 10, Workers: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161"}; !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want only %v", fake.asked, want)
	}

	// The pass answers the unrated film and Reloaded. The reader's mark
	// was waiting, so the next pass asks about it straight away; Reloaded
	// is not in the queue until the rest interval brings a refill.
	for _, id := range []string{"tt0000001", "tt0234215"} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO meta.posters (tconst, status, fetched_at) VALUES ($1, 'ok', now())`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0000001"}; !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want %v", fake.asked, want)
	}
	job.refilled = time.Now().Add(-SynopsisRest)
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0000001", "tt0234215"}; !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want %v", fake.asked, want)
	}
}

// TestASynopsisPassEndsOnABatchOfFailures: a failed lookup stays at the
// head of the queue, and a head made of nothing but this pass's failures
// ends it. An OMDb that is down costs one batch of requests, not one for
// every title in the queue.
func TestASynopsisPassEndsOnABatchOfFailures(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	posterPassHas(t, s, map[string]string{
		"tt0000001": "ok", "tt0111161": "ok", "tt0133093": "ok", "tt0234215": "ok",
	})
	down := errors.New("omdb down")
	fake := &fakeOMDb{errs: map[string]error{
		"tt0000001": down, "tt0111161": down, "tt0133093": down, "tt0234215": down,
	}}
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: 0, Batch: 2, Workers: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0133093"}; !reflect.DeepEqual(fake.asked, want) {
		t.Errorf("asked %v, want only the head batch %v", fake.asked, want)
	}
	for _, id := range fake.asked {
		if _, _, ok := synopsisRow(t, s, id); ok {
			t.Errorf("%s: a failed lookup was written down", id)
		}
	}
}

// TestATimedOutSynopsisLookupIsAskedOncePerPass: the OMDb client's own
// timeout reads as a deadline, the way a stopping pass does, but the
// pass has not stopped. The title stays at the head of the queue, since
// nothing was written down for it, and the pass must go on past it
// rather than ask it again in every batch.
func TestATimedOutSynopsisLookupIsAskedOncePerPass(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	publishFixture(t, s)
	clearSynopses(t, s)
	fiveVotes(t, s)
	posterPassHas(t, s, map[string]string{
		"tt0000001": "ok", "tt0111161": "ok", "tt0133093": "ok", "tt0234215": "ok",
	})
	fake := &fakeOMDb{
		answers: map[string]omdb.Title{
			"tt0000001": {Plot: "Five votes' worth."},
			"tt0133093": {Plot: "Red pill."},
			"tt0234215": {Plot: "More pills."},
		},
		errs: map[string]error{"tt0111161": fmt.Errorf("omdb: GET x: %w", context.DeadlineExceeded)},
	}
	job := &SynopsisJob{Store: s, Client: fake, Logger: quietLogger(), MinVotes: 0, Batch: 2, Workers: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatalf("the pass did not end; asked %d times", fake.count())
	}
	if want := []string{"tt0111161", "tt0133093", "tt0234215", "tt0000001"}; !reflect.DeepEqual(fake.asked, want) {
		t.Fatalf("asked %v, want %v, the title that timed out once", fake.asked, want)
	}
	if _, _, ok := synopsisRow(t, s, "tt0111161"); ok {
		t.Error("a lookup that timed out was written down")
	}
}
