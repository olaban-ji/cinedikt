package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"cinedikt/internal/tmdb"
)

func TestTMDbIDsAreMatchedBestKnownFirstAndOnlyOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb_queue`); err != nil {
		t.Fatal(err)
	}

	find := &fakeFinder{
		answers: map[string]tmdb.Found{
			"tt0111161": {ID: 278},
			"tt0133093": {ID: 603},
		},
		errs: map[string]error{
			"tt0000001": errors.New("tmdb down"),
		},
	}
	job := &TMDbIDJob{Store: s, Client: find, Logger: quietLogger(), Batch: 10}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked := find.askedFor()
	// Shawshank, then The Matrix, then Reloaded. The unrated film is
	// last and fails, so it stays on the queue. A documentary and an
	// adult title are never a search hit, so they are never asked.
	if len(asked) != 4 || asked[0] != "tt0111161" || asked[1] != "tt0133093" || asked[2] != "tt0234215" || asked[3] != "tt0000001" {
		t.Fatalf("asked %v", asked)
	}
	if id := tmdbID(t, s, "tt0111161"); id != 278 {
		t.Errorf("shawshank tmdb id = %d", id)
	}
	if id := tmdbID(t, s, "tt0133093"); id != 603 {
		t.Errorf("matrix tmdb id = %d", id)
	}
	if id, ok := tmdbRow(t, s, "tt0234215"); !ok || id != 0 {
		t.Errorf("reloaded stored (%d, asked %v), want asked with no id", id, ok)
	}
	if _, asked := tmdbRow(t, s, "tt0000001"); asked {
		t.Error("a failed lookup was stamped; it will not be retried")
	}

	find.errs = nil
	find.answers["tt0000001"] = tmdb.Found{ID: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	again := find.askedFor()
	if len(again) != 5 || again[4] != "tt0000001" {
		t.Fatalf("second pass asked %v", again)
	}
	if id := tmdbID(t, s, "tt0000001"); id != 1 {
		t.Errorf("unrated tmdb id = %d", id)
	}

	// Caught up: nothing left to spend a request on.
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if more := find.askedFor(); len(more) != len(again) {
		t.Fatalf("caught up, but asked %v", more)
	}
}

func TestRefillingTheTMDbQueueWritesOnlyWhatChanged(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb_queue`); err != nil {
		t.Fatal(err)
	}
	if err := s.refillTMDBQueue(ctx); err != nil {
		t.Fatal(err)
	}
	first := queueRows(t, s)
	if len(first) != 4 {
		t.Fatalf("queued %v, want the four films a search can offer", first)
	}

	// Nothing has changed, so nothing may be written: no new version of
	// any row, and no lock on one either, which dirties the page all the
	// same.
	if err := s.refillTMDBQueue(ctx); err != nil {
		t.Fatal(err)
	}
	for id, row := range queueRows(t, s) {
		if row != first[id] || row.xmax != "0" {
			t.Errorf("%s was written with nothing to change: %+v, was %+v", id, row, first[id])
		}
	}

	// A publish moves Reloaded's votes and brings Shawshank, which the
	// queue has not seen. Those two are written, and only those.
	if _, err := s.pool.Exec(ctx, `
		UPDATE `+Live+`.ratings SET num_votes = 700000 WHERE tconst = 'tt0234215'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb_queue WHERE tconst = 'tt0111161'`); err != nil {
		t.Fatal(err)
	}
	if err := s.refillTMDBQueue(ctx); err != nil {
		t.Fatal(err)
	}
	after := queueRows(t, s)
	if got := after["tt0234215"]; got.votes != 700000 || got.xmin == first["tt0234215"].xmin {
		t.Errorf("reloaded = %+v, want its new votes", got)
	}
	if got, ok := after["tt0111161"]; !ok || got.votes != 2900000 {
		t.Errorf("shawshank = %+v (queued %v), want it back with its votes", got, ok)
	}
	for _, id := range []string{"tt0133093", "tt0000001"} {
		if row := after[id]; row != first[id] || row.xmax != "0" {
			t.Errorf("%s was written with nothing to change: %+v, was %+v", id, row, first[id])
		}
	}
}

func TestTMDbIDsAreNotAskedForTitlesSearchCanNoLongerOffer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb_queue`); err != nil {
		t.Fatal(err)
	}
	if err := s.refillTMDBQueue(ctx); err != nil {
		t.Fatal(err)
	}

	// Since the queue was filled, a publish has dropped a title from the
	// catalog and made Reloaded a documentary. Both are still queued,
	// the dropped one at the head.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.tmdb_queue (tconst, votes) VALUES ('tt7777777', 5000000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE `+Live+`.titles SET genres = array_append(genres, 'Documentary')
		WHERE tconst = 'tt0234215'`); err != nil {
		t.Fatal(err)
	}

	find := &fakeFinder{answers: map[string]tmdb.Found{
		"tt0111161": {ID: 278},
		"tt0133093": {ID: 603},
		"tt0234215": {ID: 604},
	}}
	job := &TMDbIDJob{Store: s, Client: find, Logger: quietLogger(), Batch: 10}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	asked := find.askedFor()
	if len(asked) != 3 || asked[0] != "tt0111161" || asked[1] != "tt0133093" || asked[2] != "tt0000001" {
		t.Fatalf("asked %v, want only the films a search can still offer", asked)
	}
	if left := queueRows(t, s); len(left) != 0 {
		t.Errorf("queue still holds %v", left)
	}
	// Dropped, not answered: an answer would keep Reloaded from being
	// asked about when it is a film again.
	for _, id := range []string{"tt7777777", "tt0234215"} {
		if _, asked := tmdbRow(t, s, id); asked {
			t.Errorf("%s was recorded as asked", id)
		}
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE `+Live+`.titles SET genres = array_remove(genres, 'Documentary')
		WHERE tconst = 'tt0234215'`); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if again := find.askedFor(); len(again) != 4 || again[3] != "tt0234215" {
		t.Fatalf("second pass asked %v, want Reloaded back", again)
	}
	if id := tmdbID(t, s, "tt0234215"); id != 604 {
		t.Errorf("reloaded tmdb id = %d", id)
	}
}

// TestTMDbIDsAreAskedAgainOnceTheyComeDue is TMDb's six months for the
// id map: a match older than 150 days is asked again, after every title
// never asked, oldest first, and its row is renewed in place. One inside
// the 150 days is left alone, and so is a due match for a title search
// cannot offer, which is left to the backstop.
func TestTMDbIDsAreAskedAgainOnceTheyComeDue(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	for _, stmt := range []string{
		`DELETE FROM meta.tmdb`,
		`DELETE FROM meta.tmdb_queue`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt0111161', 278, now() - interval '151 days')`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt0234215', NULL, now() - interval '160 days')`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt0133093', 603, now() - interval '149 days')`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt0000002', 99, now() - interval '170 days')`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	find := &fakeFinder{answers: map[string]tmdb.Found{
		"tt0000001": {ID: 1},
		// Since they were asked, TMDb has renumbered Shawshank and found
		// a movie for Reloaded.
		"tt0111161": {ID: 2780},
		"tt0234215": {ID: 604},
		"tt0133093": {ID: 603},
	}}
	// One title a batch, so the order is the whole of what is checked.
	job := &TMDbIDJob{Store: s, Client: find, Logger: quietLogger(), Batch: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// The unrated film has never been asked, so it goes first. Then the
	// due matches, oldest first. The documentary is not a film search
	// can offer, so its match waits for the backstop.
	if want := []string{"tt0000001", "tt0234215", "tt0111161"}; !reflect.DeepEqual(find.askedFor(), want) {
		t.Fatalf("asked %v, want %v", find.askedFor(), want)
	}
	for id, want := range map[string]int{"tt0111161": 2780, "tt0234215": 604, "tt0133093": 603, "tt0000002": 99} {
		if got := tmdbID(t, s, id); got != want {
			t.Errorf("%s tmdb id = %d, want %d", id, got, want)
		}
	}
	for id, fresh := range map[string]bool{"tt0111161": true, "tt0234215": true, "tt0133093": false, "tt0000002": false} {
		if got := tmdbAskedToday(t, s, id); got != fresh {
			t.Errorf("%s asked today = %v, want %v", id, got, fresh)
		}
	}

	// Caught up: a renewed match is not due again for another 150 days.
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if more := find.askedFor(); len(more) != 3 {
		t.Fatalf("caught up, but asked %v", more[3:])
	}
}

// TestANewTitleIsMatchedAheadOfTheReasksStillToCome: the re-asks of a
// whole sweep's matches can run for hours, and a publish in the middle
// of them must not wait for the end. A pass refills its queue once it
// has run for tmdbIDRefillEvery, and a title the refill brings goes
// ahead of the re-asks still to come.
func TestANewTitleIsMatchedAheadOfTheReasksStillToCome(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	was := tmdbIDRefillEvery
	tmdbIDRefillEvery = 0
	t.Cleanup(func() { tmdbIDRefillEvery = was })
	for _, stmt := range []string{
		`DELETE FROM meta.tmdb`,
		`DELETE FROM meta.tmdb_queue`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES
		     ('tt0111161', 278, now() - interval '160 days'),
		     ('tt0133093', 603, now() - interval '155 days'),
		     ('tt0234215', 604, now() - interval '151 days'),
		     ('tt0000001', 1, now() - interval '10 days')`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	// While Shawshank is asked about again, a publish brings a new film.
	find := &hookedFinder{
		fakeFinder: &fakeFinder{answers: map[string]tmdb.Found{
			"tt0111161": {ID: 278}, "tt0133093": {ID: 603}, "tt0234215": {ID: 604}, "tt0000010": {ID: 10},
		}},
		on: map[string]func(){"tt0111161": func() {
			for _, stmt := range []string{
				`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres)
				 VALUES ('tt0000010', 'A New Film', 'A New Film', false, 2026, ARRAY['Drama'])`,
				`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category) VALUES ('tt0000010', 1, 'nm0000209', 'actor')`,
			} {
				if _, err := s.pool.Exec(ctx, stmt); err != nil {
					t.Error(err)
				}
			}
		}},
	}
	job := &TMDbIDJob{Store: s, Client: find, Logger: quietLogger(), Batch: 1}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0000010", "tt0133093", "tt0234215"}; !reflect.DeepEqual(find.askedFor(), want) {
		t.Fatalf("asked %v, want %v", find.askedFor(), want)
	}
	if id := tmdbID(t, s, "tt0000010"); id != 10 {
		t.Errorf("new film tmdb id = %d", id)
	}
}

// hookedFinder is fakeFinder with something that happens as a title is
// asked about.
type hookedFinder struct {
	*fakeFinder
	on map[string]func()
}

func (f *hookedFinder) FindByIMDb(ctx context.Context, id string) (tmdb.Found, error) {
	if hook := f.on[id]; hook != nil {
		hook()
	}
	return f.fakeFinder.FindByIMDb(ctx, id)
}

// tmdbAskedToday reports whether a title's match was stamped in the
// last day.
func tmdbAskedToday(t *testing.T, s *Store, tconst string) bool {
	t.Helper()
	var today bool
	err := s.pool.QueryRow(context.Background(), `
		SELECT asked_at > now() - interval '1 day' FROM meta.tmdb WHERE tconst = $1`, tconst).Scan(&today)
	if err != nil {
		t.Fatal(err)
	}
	return today
}

// queueRow is one row of the TMDb queue with the system columns that
// say whether a statement has written to it since it was last read.
type queueRow struct {
	votes      int
	xmin, xmax string
}

func queueRows(t *testing.T, s *Store) map[string]queueRow {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), `
		SELECT tconst, votes, xmin::text, xmax::text FROM meta.tmdb_queue`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]queueRow{}
	for rows.Next() {
		var id string
		var row queueRow
		if err := rows.Scan(&id, &row.votes, &row.xmin, &row.xmax); err != nil {
			t.Fatal(err)
		}
		out[id] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tmdbID(t *testing.T, s *Store, tconst string) int {
	t.Helper()
	id, _ := tmdbRow(t, s, tconst)
	return id
}

func tmdbRow(t *testing.T, s *Store, tconst string) (int, bool) {
	t.Helper()
	var id *int
	err := s.pool.QueryRow(context.Background(), `
		SELECT tmdb_id FROM meta.tmdb WHERE tconst = $1`, tconst).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	if id == nil {
		return 0, true
	}
	return *id, true
}
