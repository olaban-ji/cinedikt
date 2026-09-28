package catalog

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/tmdb"
)

// fakeFinder answers from a table and records what it was asked.
type fakeFinder struct {
	mu      sync.Mutex
	answers map[string]tmdb.Found
	errs    map[string]error
	asked   []string
}

func (f *fakeFinder) FindByIMDb(_ context.Context, id string) (tmdb.Found, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	if err, bad := f.errs[id]; bad {
		return tmdb.Found{}, err
	}
	got, ok := f.answers[id]
	if !ok {
		return tmdb.Found{}, tmdb.ErrNotFound
	}
	return got, nil
}

func (f *fakeFinder) askedFor() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// blankPosters gives every title in the fixture a poster row with no
// picture, so the whole fixture is fair game for the fallback.
//
// It inserts rather than updates. meta outlives every generation and is
// never reset by publishFixture, so an update would only touch rows
// some other test happened to leave behind — which made these tests
// pass as a package and fail under `-run`.
func blankPosters(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO meta.posters (tconst, poster_url, released, status, fetched_at)
		SELECT tconst, NULL, NULL, 'ok', now() FROM `+Live+`.titles
		ON CONFLICT (tconst) DO UPDATE
		SET poster_url = NULL, released = NULL, status = 'ok',
		    source = NULL, tmdb_at = NULL, wanted_at = NULL`)
	if err != nil {
		t.Fatal(err)
	}
}

func posterRow(t *testing.T, s *Store, tconst string) (url, status, source string, wanted, asked bool) {
	t.Helper()
	var u, src *string
	var w, a *time.Time
	err := s.pool.QueryRow(context.Background(), `
		SELECT poster_url, status, source, wanted_at, tmdb_at
		FROM meta.posters WHERE tconst = $1`, tconst).Scan(&u, &status, &src, &w, &a)
	if err != nil {
		t.Fatal(err)
	}
	if u != nil {
		url = *u
	}
	if src != nil {
		source = *src
	}
	return url, status, source, w != nil, a != nil
}

// posterColour is the opening screen's colour for a title, or "" when
// it has none to work out yet.
func posterColour(t *testing.T, s *Store, tconst string) string {
	t.Helper()
	var c *string
	if err := s.pool.QueryRow(context.Background(),
		`SELECT colour FROM meta.posters WHERE tconst = $1`, tconst).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c == nil {
		return ""
	}
	return *c
}

func TestTMDbFallbackTakesWhatAReaderWantedFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)

	// Shawshank is the most voted in the fixture, so a sweep would
	// reach it first. The reader wants the unrated 1930 film instead.
	if err := s.markWanted(ctx, []string{"tt0000001"}); err != nil {
		t.Fatal(err)
	}
	find := &fakeFinder{answers: map[string]tmdb.Found{
		"tt0000001": {Poster: "https://image.tmdb.org/t/p/w780/a.jpg"},
	}}
	// One title, so the order is the whole of what is being checked.
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), Batch: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked := find.askedFor()
	if len(asked) == 0 || asked[0] != "tt0000001" {
		t.Fatalf("asked %v first, want the wanted title", asked)
	}

	url, status, source, wanted, checked := posterRow(t, s, "tt0000001")
	if url != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Errorf("poster_url = %q", url)
	}
	if status != "ok" || source != "tmdb" {
		t.Errorf("status/source = %q/%q, want ok/tmdb", status, source)
	}
	if wanted {
		t.Error("the demand mark survived a repair; it will be asked about again")
	}
	if !checked {
		t.Error("tmdb_at was not stamped")
	}
}

// TestAFloorOfZeroTakesTheWholeTail is what TMDB_SWEEP_MIN_VOTES=0
// means: no vote predicate at all, so a title nobody has ever rated is
// in the queue alongside the rest.
func TestAFloorOfZeroTakesTheWholeTail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)
	// The fixture's unrated 1930 film has no ratings row at all, which
	// is the case a coalesce has to cover.
	if _, err := s.pool.Exec(ctx, `UPDATE meta.posters SET votes = NULL`); err != nil {
		t.Fatal(err)
	}

	atZero, err := s.tmdbWanted(ctx, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(atZero) == 0 {
		t.Fatal("a floor of 0 found nothing; the whole tail should be in the queue")
	}

	// And a floor keeps the unrated out.
	atHundred, err := s.tmdbWanted(ctx, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(atHundred) != 0 {
		t.Errorf("a floor of 100 returned %d unrated titles", len(atHundred))
	}

	// A title somebody opened is in the queue at any floor: demand is
	// not a vote count.
	if err := s.markWanted(ctx, []string{atZero[0]}); err != nil {
		t.Fatal(err)
	}
	wanted, err := s.tmdbWanted(ctx, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(wanted) != 1 || wanted[0] != atZero[0] {
		t.Errorf("wanted = %v, want just the title a reader opened", wanted)
	}
}

func TestTMDbFallbackLeavesTheLongTailAlone(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)

	// Nothing wanted, and a floor above everything in the fixture.
	find := &fakeFinder{}
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), MinVotes: 10_000_000}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked := find.askedFor(); len(asked) != 0 {
		t.Errorf("swept %v; a film nobody has opened is not worth a second service's time", asked)
	}
}

func TestTMDbFallbackAsksEachTitleOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)

	// TMDb has nothing for any of them, which is still an answer.
	find := &fakeFinder{}
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), MinVotes: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	first := len(find.askedFor())
	if first == 0 {
		t.Fatal("the sweep asked about nothing")
	}
	// A second pass has nothing left: a definite "no" is stored, and
	// that is what stops the queue coming round until it comes due.
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if again := len(find.askedFor()); again != first {
		t.Errorf("asked %d then %d; a stored answer was asked again", first, again)
	}
}

func TestTMDbFallbackRetriesAFault(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)

	find := &fakeFinder{errs: map[string]error{"tt0111161": errors.New("dial tcp: refused")}}
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), MinVotes: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, checked := posterRow(t, s, "tt0111161")
	if checked {
		t.Error("a failed lookup was stamped as answered; it will never be tried again")
	}
}

func TestDeadPosterBecomesWorkForTheFallback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)
	// A title with a picture is not in the queue.
	if _, err := s.pool.Exec(ctx, `
		UPDATE meta.posters SET poster_url = 'https://x/gone.jpg' WHERE tconst = 'tt0133093'`); err != nil {
		t.Fatal(err)
	}
	find := &fakeFinder{}
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), MinVotes: 10_000_000}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked := find.askedFor(); len(asked) != 0 {
		t.Fatalf("a title with a poster was queued: %v", asked)
	}

	// Until the host says the address is gone.
	if err := s.MarkPosterDead(ctx, "tt0133093"); err != nil {
		t.Fatal(err)
	}
	_, status, _, wanted, _ := posterRow(t, s, "tt0133093")
	if status != "dead" || !wanted {
		t.Fatalf("status/wanted = %q/%v, want dead and wanted", status, wanted)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked := find.askedFor(); len(asked) != 1 || asked[0] != "tt0133093" {
		t.Errorf("asked %v, want the dead one", asked)
	}
}

func TestWantingAPosterIgnoresTitlesThatHaveOne(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)
	if _, err := s.pool.Exec(ctx, `
		UPDATE meta.posters SET poster_url = 'https://x/p.jpg' WHERE tconst = 'tt0133093'`); err != nil {
		t.Fatal(err)
	}
	// By the time a mark is written the picture may have arrived, and
	// marking that row would put a perfectly good poster in the queue.
	if err := s.markWanted(ctx, []string{"tt0133093", "tt0111161"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, wanted, _ := posterRow(t, s, "tt0133093"); wanted {
		t.Error("a title with a poster was marked as wanted")
	}
	if _, _, _, wanted, _ := posterRow(t, s, "tt0111161"); !wanted {
		t.Error("a title with no poster was not marked")
	}
}

// TestTMDbPosterAnswersAreAskedAgainLastOnceTheyComeDue is TMDb's six
// months for the backup posters. An answer older than 150 days is asked
// again, after what a reader wants and what the sweep has left, oldest
// first: a picture of TMDb's, and TMDb's "nothing" for a title that still
// has no picture. A new picture replaces the old in place; TMDb no longer
// having one takes its own away. An answer inside the 150 days, and
// TMDb's "nothing" for a title whose own picture works, are left alone.
func TestTMDbPosterAnswersAreAskedAgainLastOnceTheyComeDue(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	// meta outlives every test, and a stamp another test left behind
	// could be due.
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.posters`); err != nil {
		t.Fatal(err)
	}
	blankPosters(t, s)
	for _, stmt := range []string{
		// A reader has just met the unrated film, which has no picture.
		`UPDATE meta.posters SET wanted_at = now() WHERE tconst = 'tt0000001'`,
		// The documentary is well known enough for the sweep.
		`UPDATE meta.posters SET votes = 5000 WHERE tconst = 'tt0000002'`,
		// Shawshank shows a picture of TMDb's, 160 days old, and the
		// opening screen has its colour.
		`UPDATE meta.posters
		 SET poster_url = 'https://image.tmdb.org/t/p/w780/old.jpg', source = 'tmdb', tmdb_at = now() - interval '160 days',
		     colour = '#112233'
		 WHERE tconst = 'tt0111161'`,
		// TMDb had nothing for Reloaded 151 days ago, and it still has
		// no picture.
		`UPDATE meta.posters SET tmdb_at = now() - interval '151 days' WHERE tconst = 'tt0234215'`,
		// The Matrix's picture of TMDb's is 149 days old, and coloured.
		`UPDATE meta.posters
		 SET poster_url = 'https://image.tmdb.org/t/p/w780/matrix.jpg', source = 'tmdb', tmdb_at = now() - interval '149 days',
		     colour = '#445566'
		 WHERE tconst = 'tt0133093'`,
		// A title with a picture of OMDb's that works, for which TMDb
		// once had nothing.
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at, tmdb_at)
		 VALUES ('tt0000009', 'https://m.media-amazon.com/images/M/fine.jpg', 'ok', now(), now() - interval '170 days')`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	find := &fakeFinder{answers: map[string]tmdb.Found{
		"tt0000001": {ID: 1, Poster: "https://image.tmdb.org/t/p/w780/unrated.jpg"},
		"tt0111161": {ID: 278, Poster: "https://image.tmdb.org/t/p/w780/new.jpg"},
		"tt0234215": {ID: 604, Poster: "https://image.tmdb.org/t/p/w780/reloaded.jpg"},
		// TMDb has since lost the Matrix's picture.
		"tt0133093": {ID: 603},
	}}
	// One title a batch, so the order is the whole of what is checked.
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), MinVotes: 1000, Batch: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0000001", "tt0000002", "tt0111161", "tt0234215"}; !reflect.DeepEqual(find.askedFor(), want) {
		t.Fatalf("asked %v, want %v", find.askedFor(), want)
	}
	for _, want := range []struct {
		tconst, url, source string
	}{
		{"tt0111161", "https://image.tmdb.org/t/p/w780/new.jpg", "tmdb"},
		{"tt0234215", "https://image.tmdb.org/t/p/w780/reloaded.jpg", "tmdb"},
		{"tt0133093", "https://image.tmdb.org/t/p/w780/matrix.jpg", "tmdb"},
		{"tt0000009", "https://m.media-amazon.com/images/M/fine.jpg", ""},
	} {
		if url, _, source, _, _ := posterRow(t, s, want.tconst); url != want.url || source != want.source {
			t.Errorf("%s = %q from %q, want %q from %q", want.tconst, url, source, want.url, want.source)
		}
	}
	// The same answer renews the id match.
	if id := tmdbID(t, s, "tt0111161"); id != 278 {
		t.Errorf("shawshank tmdb id = %d", id)
	}
	// The old picture's colour went with it, for the colour job to work
	// out afresh. The Matrix's picture did not change, and keeps its own.
	if c := posterColour(t, s, "tt0111161"); c != "" {
		t.Errorf("shawshank's new picture kept the old one's colour %s", c)
	}
	if c := posterColour(t, s, "tt0133093"); c != "#445566" {
		t.Errorf("matrix colour = %q before its picture came due", c)
	}

	// Caught up. Then the Matrix's picture comes due, and TMDb no longer
	// has one: its terms leave nothing to keep.
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(find.askedFor()); n != 4 {
		t.Fatalf("caught up, but asked %v", find.askedFor()[4:])
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE meta.posters SET tmdb_at = now() - interval '151 days' WHERE tconst = 'tt0133093'`); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if asked := find.askedFor(); len(asked) != 5 || asked[4] != "tt0133093" {
		t.Fatalf("asked %v, want the Matrix last", asked)
	}
	url, status, source, _, checked := posterRow(t, s, "tt0133093")
	if url != "" || source != "" || status != "ok" || !checked {
		t.Errorf("matrix = %q, %s, from %q, asked %v; want no picture, and TMDb's answer stamped", url, status, source, checked)
	}
	if c := posterColour(t, s, "tt0133093"); c != "" {
		t.Errorf("matrix kept the colour %s of a picture it no longer has", c)
	}
}

// TestARefusedTMDbKeyEndsThePass: a 401 is the key, not the title, and
// every title after it would be told the same.
func TestARefusedTMDbKeyEndsThePass(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	blankPosters(t, s)
	refused := &tmdb.StatusError{Status: 401, Message: "Invalid API key"}
	find := &fakeFinder{errs: map[string]error{}}
	for _, id := range []string{"tt0111161", "tt0133093", "tt0234215", "tt0000001", "tt0000002"} {
		find.errs[id] = refused
	}
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), MinVotes: 0}
	err := job.Run(ctx)
	var key *KeyError
	if !errors.As(err, &key) || key.Provider != "TMDb" {
		t.Fatalf("err = %v, want a KeyError for TMDb", err)
	}
	if n := len(find.askedFor()); n != 1 {
		t.Errorf("asked %d titles after the key was refused, want 1", n)
	}
}

// TestEveryTMDbLookupFailingIsAFailure: fifty faults and not one answer
// is TMDb, or the way there, and the pass says so instead of finishing.
func TestEveryTMDbLookupFailingIsAFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// meta outlives every test, and a row another test left wanted is
	// in the queue at any floor.
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.posters`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.posters (tconst, status, fetched_at, votes)
		SELECT 'tt99' || lpad(g::text, 5, '0'), 'ok', now(), 1000000 + g
		FROM generate_series(1, 60) g`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM meta.posters WHERE votes > 1000000`)
	})
	find := &fakeFinder{errs: map[string]error{}}
	for g := 1; g <= 60; g++ {
		find.errs[fmt.Sprintf("tt99%05d", g)] = errors.New("tmdb: HTTP 502")
	}
	job := &TMDbJob{Store: s, Client: find, Logger: quietLogger(), MinVotes: 1000001}
	err := job.Run(ctx)
	var all *LookupsFailedError
	// The pass ends when a batch comes back all tried, so it sees the
	// first page of the queue, not all of it; that is already enough.
	if !errors.As(err, &all) || all.Provider != "TMDb" || all.Count < failedLookups {
		t.Fatalf("err = %v, want a LookupsFailedError for at least %d", err, failedLookups)
	}
}
