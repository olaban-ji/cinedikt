package catalog

import (
	"context"
	"reflect"
	"testing"
	"time"

	"cinedikt/internal/tmdb"
)

// TestTheBackstopForgetsTMDbIDsAndPostersButNeverOMDbs is TMDb's six
// months for the id map and the backup posters: a match past
// tmdbForgetDays is deleted, whatever became of its title, and a poster
// row that old loses what TMDb gave it and nothing else. An address of
// OMDb's, and its status, stay. Both titles are then asked about again.
func TestTheBackstopForgetsTMDbIDsAndPostersButNeverOMDbs(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	for _, stmt := range []string{
		`DELETE FROM meta.tmdb`,
		`DELETE FROM meta.tmdb_queue`,
		`DELETE FROM meta.posters`,
		// Past the backstop: a match whose title search can still offer,
		// and one whose title has left the catalog, which no re-ask
		// would ever reach.
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt0111161', 278, now() - interval '176 days')`,
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt7777777', 777, now() - interval '176 days')`,
		// Due for a re-ask, but not yet for the backstop.
		`INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at) VALUES ('tt0133093', 603, now() - interval '174 days')`,

		// Shawshank shows a picture of TMDb's, past the backstop.
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at, source, tmdb_at, votes, colour)
		 VALUES ('tt0111161', 'https://image.tmdb.org/t/p/w780/shawshank.jpg', 'ok', now(), 'tmdb', now() - interval '176 days', 2900000, '#112233')`,
		// Reloaded's own address has died, and TMDb had nothing for it.
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at, tmdb_at, votes)
		 VALUES ('tt0234215', 'https://m.media-amazon.com/images/M/reloaded.jpg', 'dead', now(), now() - interval '176 days', 604321)`,
		// The Matrix shows OMDb's picture. TMDb was asked when it once
		// failed to load, and had nothing.
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at, tmdb_at, votes, colour)
		 VALUES ('tt0133093', 'https://m.media-amazon.com/images/M/matrix.jpg', 'ok', now(), now() - interval '176 days', 2081234, '#445566')`,
		// An address marked as OMDb's own.
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at, source, tmdb_at, votes)
		 VALUES ('tt0000001', 'https://m.media-amazon.com/images/M/unrated.jpg', 'ok', now(), 'omdb', now() - interval '176 days', 0)`,
		// A picture of TMDb's that is due for a re-ask, not the backstop.
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at, source, tmdb_at, votes)
		 VALUES ('tt0000002', 'https://image.tmdb.org/t/p/w780/doc.jpg', 'ok', now(), 'tmdb', now() - interval '174 days', 0)`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	gone, err := s.forgetDueTMDbData(ctx, tmdbForgetDays)
	if err != nil {
		t.Fatal(err)
	}
	if gone.ids != 2 || gone.posters != 4 {
		t.Errorf("cleared %d ids and %d poster rows, want 2 and 4", gone.ids, gone.posters)
	}

	for _, id := range []string{"tt0111161", "tt7777777"} {
		if _, ok := tmdbRow(t, s, id); ok {
			t.Errorf("%s: a match past the backstop was kept", id)
		}
	}
	if id := tmdbID(t, s, "tt0133093"); id != 603 {
		t.Errorf("a match due only for a re-ask was dropped: id %d", id)
	}

	for _, want := range []struct {
		tconst, url, status, source string
		asked                       bool
	}{
		// TMDb's picture goes, and the title has none until it is asked
		// again. The status is OMDb's, and stays.
		{"tt0111161", "", "ok", "", false},
		// OMDb's addresses stay, dead or alive; only the stamp goes.
		{"tt0234215", "https://m.media-amazon.com/images/M/reloaded.jpg", "dead", "", false},
		{"tt0133093", "https://m.media-amazon.com/images/M/matrix.jpg", "ok", "", false},
		{"tt0000001", "https://m.media-amazon.com/images/M/unrated.jpg", "ok", "omdb", false},
		// Not yet due for the backstop.
		{"tt0000002", "https://image.tmdb.org/t/p/w780/doc.jpg", "ok", "tmdb", true},
	} {
		url, status, source, _, asked := posterRow(t, s, want.tconst)
		if url != want.url || status != want.status || source != want.source || asked != want.asked {
			t.Errorf("%s = %q, %s, source %q, asked %v; want %q, %s, source %q, asked %v",
				want.tconst, url, status, source, asked, want.url, want.status, want.source, want.asked)
		}
	}

	// TMDb's picture takes the colour worked out from it along; OMDb's
	// keeps its own.
	if c := posterColour(t, s, "tt0111161"); c != "" {
		t.Errorf("shawshank kept the colour %s of TMDb's picture", c)
	}
	if c := posterColour(t, s, "tt0133093"); c != "#445566" {
		t.Errorf("matrix colour = %q; the backstop touched OMDb's picture", c)
	}

	// And both are asked about again. The matcher's refill queues
	// Shawshank afresh, as it does any title with no match; the Matrix's
	// match stands, so it is not queued.
	if err := s.refillTMDBQueue(ctx); err != nil {
		t.Fatal(err)
	}
	queued := queueRows(t, s)
	if _, ok := queued["tt0111161"]; !ok {
		t.Errorf("queue = %v; a title whose match was deleted was not queued again", queued)
	}
	if _, ok := queued["tt0133093"]; ok {
		t.Error("a title with a match was queued")
	}
	// The poster fallback's queue has the two titles with no working
	// picture again, best known first.
	wanted, err := s.tmdbWanted(ctx, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0234215"}; !reflect.DeepEqual(wanted, want) {
		t.Errorf("poster fallback's queue = %v, want %v", wanted, want)
	}
}

// TestAReleaseDateOfTMDbsGoesWithTMDbsAnswer is TMDb's six months for
// the one field of a poster row the two services share. A date TMDb
// filled in where OMDb had none is TMDb's: a later answer replaces it,
// "nothing" takes it away, and so does the backstop. OMDb's date is
// never touched by any of them, and a date OMDb writes is OMDb's.
func TestAReleaseDateOfTMDbsGoesWithTMDbsAnswer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	for _, stmt := range []string{
		`DELETE FROM meta.posters`,
		// Shawshank has no date from OMDb; the Matrix has OMDb's.
		`INSERT INTO meta.posters (tconst, status, fetched_at, votes)
		 VALUES ('tt0111161', 'ok', now(), 2900000)`,
		`INSERT INTO meta.posters (tconst, released, status, fetched_at, votes)
		 VALUES ('tt0133093', '1999-03-31', 'ok', now(), 2081234)`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	check := func(when, tconst, want string, tmdbs bool) {
		t.Helper()
		var released *time.Time
		var flag bool
		if err := s.pool.QueryRow(ctx, `
			SELECT released, released_tmdb FROM meta.posters WHERE tconst = $1`, tconst).Scan(&released, &flag); err != nil {
			t.Fatal(err)
		}
		got := ""
		if released != nil {
			got = released.Format("2006-01-02")
		}
		if got != want || flag != tmdbs {
			t.Errorf("%s: %s released %q (TMDb's %v), want %q (TMDb's %v)", when, tconst, got, flag, want, tmdbs)
		}
	}
	found := func(date string) tmdb.Found {
		return tmdb.Found{ID: 1, Poster: "https://image.tmdb.org/t/p/w780/x.jpg", Released: day(date)}
	}

	for _, id := range []string{"tt0111161", "tt0133093"} {
		if err := s.saveTMDbPoster(ctx, id, found("1994-09-23")); err != nil {
			t.Fatal(err)
		}
	}
	check("first answer", "tt0111161", "1994-09-23", true)
	check("first answer", "tt0133093", "1999-03-31", false)

	// A re-ask replaces TMDb's date with TMDb's new one.
	if err := s.saveTMDbPoster(ctx, "tt0111161", found("1994-10-14")); err != nil {
		t.Fatal(err)
	}
	check("re-ask", "tt0111161", "1994-10-14", true)

	// "Nothing" takes TMDb's date away with its picture, and leaves OMDb's.
	for _, id := range []string{"tt0111161", "tt0133093"} {
		if err := s.saveTMDbPoster(ctx, id, tmdb.Found{ID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	check("nothing", "tt0111161", "", false)
	check("nothing", "tt0133093", "1999-03-31", false)

	// The backstop does the same for an answer 176 days old.
	for _, id := range []string{"tt0111161", "tt0133093"} {
		if err := s.saveTMDbPoster(ctx, id, found("1994-09-23")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE meta.posters SET tmdb_at = now() - interval '176 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.forgetDueTMDbData(ctx, tmdbForgetDays); err != nil {
		t.Fatal(err)
	}
	check("backstop", "tt0111161", "", false)
	check("backstop", "tt0133093", "1999-03-31", false)

	// A date OMDb writes over TMDb's is OMDb's from then on.
	if err := s.saveTMDbPoster(ctx, "tt0111161", found("1994-09-23")); err != nil {
		t.Fatal(err)
	}
	if err := s.writePosters(ctx, Live, []Poster{{TConst: "tt0111161", Released: day("1994-10-14"), OK: true}}); err != nil {
		t.Fatal(err)
	}
	check("OMDb's answer", "tt0111161", "1994-10-14", false)
}

// TestTheBackstopForgetsPeoplesPhotos is TMDb's six months for people's
// photos: an answer past tmdbForgetDays is deleted, whatever became of
// its person, and a younger one is kept. The deleted person is swept
// afresh, and a map opened with them on it marks them again.
func TestTheBackstopForgetsPeoplesPhotos(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s,
		`INSERT INTO meta.people (nconst, tmdb_id, profile_path, asked_at) VALUES
		    ('nm0000206', 6384, '/keanu.jpg', now() - interval '176 days'),
		    ('nm7777777', 99, NULL, now() - interval '176 days'),
		    ('nm0000401', 530, '/moss.jpg', now() - interval '174 days')`,
	)
	gone, err := s.forgetDueTMDbData(ctx, tmdbForgetDays)
	if err != nil {
		t.Fatal(err)
	}
	if gone.people != 2 {
		t.Errorf("deleted %d people's photos, want 2", gone.people)
	}
	for _, id := range []string{"nm0000206", "nm7777777"} {
		if _, ok := personRow(t, s, id); ok {
			t.Errorf("%s: a photo past the backstop was kept", id)
		}
	}
	if path, ok := personRow(t, s, "nm0000401"); !ok || path != "/moss.jpg" {
		t.Errorf("a photo due only for a re-ask was dropped: %q (row %v)", path, ok)
	}
	if err := s.refillPeopleQueue(ctx, 0); err != nil {
		t.Fatal(err)
	}
	var queued bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM meta.people_queue WHERE nconst = 'nm0000206')`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Error("a person whose photo was deleted was not queued again")
	}
}
