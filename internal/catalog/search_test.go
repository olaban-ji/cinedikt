package catalog

import (
	"context"
	"testing"
)

func TestSearchPutsTheOneTheyMeantFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)

	// "matrix" matches both Matrix films. The one with the votes wins,
	// which is the whole reason ranking is done here rather than taken
	// from whatever order a title match happens to return.
	got, err := s.Search(ctx, "matrix", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("hits = %d: %+v", len(got), got)
	}
	if got[0].ID != "tt0133093" {
		t.Errorf("first hit = %s (%s), want The Matrix", got[0].ID, got[0].Title)
	}
	if got[0].Votes <= got[1].Votes {
		t.Errorf("hits are not in vote order: %d then %d", got[0].Votes, got[1].Votes)
	}
}

func TestSearchPrefersAWholeTitleOverAMentionOfIt(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)

	// Shawshank has far more votes, so only the exact-title rule can
	// put a smaller film above it.
	got, err := s.Search(ctx, "the matrix", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Title != "The Matrix" {
		t.Errorf("first hit = %+v, want the exact title", got)
	}
}

func TestSearchOffersOnlyWhatOpens(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)

	// A documentary and an adult title are in the catalog and are never
	// mapped, so offering either would be a dead end.
	for _, q := range []string{"documentary", "adult"} {
		got, err := s.Search(ctx, q, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("%q offered %+v", q, got)
		}
	}
	// And a film with nobody billed has no map to draw.
	for _, h := range mustSearch(t, s, "a ") {
		if h.ID == "tt0000002" {
			t.Error("a film with no people was offered")
		}
	}
}

func TestByTMDBKeepsTheOrderAndDropsWhatCannotOpen(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb`); err != nil {
		t.Fatal(err)
	}

	if got, err := s.ByTMDB(ctx, nil); err != nil || got != nil {
		t.Fatalf("empty ids = %+v, %v", got, err)
	}

	// The ids are TMDb's, stored ahead of the search. Asked for in an
	// order that is not vote order. A documentary, an adult title, an
	// id nobody matched, and a repeated one are not offered.
	for _, row := range []struct {
		tconst string
		id     int
	}{
		{"tt0111161", 278},
		{"tt0000002", 2},
		{"tt0000003", 3},
		{"tt0133093", 603},
		{"tt0000001", 1},
	} {
		if err := s.rememberTMDB(ctx, row.tconst, row.id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ByTMDB(ctx, []int{278, 2, 3, 404, 603, 603})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "tt0111161" || got[1].ID != "tt0133093" {
		t.Fatalf("hits = %+v", got)
	}
	if got[0].Title != "The Shawshank Redemption" || got[1].Year != 1999 {
		t.Fatalf("hits = %+v", got)
	}

	// Unrated is still a film.
	got, err = s.ByTMDB(ctx, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "tt0000001" {
		t.Fatalf("hits = %+v", got)
	}
}

func TestSearchIgnoresAQueryTooShortToMeanAnything(t *testing.T) {
	s := testStore(t)
	got, err := s.Search(context.Background(), "a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a one-character query returned %+v", got)
	}
}

func mustSearch(t *testing.T, s *Store, q string) []Hit {
	t.Helper()
	got, err := s.Search(context.Background(), q, 20)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// assumePostersShow keeps a first-run test off the network. The
// fixture's addresses are not real pictures.
func assumePostersShow(t *testing.T) {
	t.Helper()
	prev := posterGone
	posterGone = func(context.Context, string) (bool, bool) { return false, false }
	t.Cleanup(func() { posterGone = prev })
}

func TestFirstRunOffersOneMovieAnEra(t *testing.T) {
	s := testStore(t)
	assumePostersShow(t)
	ctx := context.Background()
	publishFixture(t, s)

	// The fixture's movies need a poster before the cold screen will
	// offer them: a tile with no picture is a grey box.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.posters (tconst, poster_url, status, fetched_at)
		SELECT tconst, 'https://m.media-amazon.com/images/M/x.jpg', 'ok', now()
		FROM catalog.titles
		ON CONFLICT (tconst) DO UPDATE SET poster_url = EXCLUDED.poster_url`); err != nil {
		t.Fatal(err)
	}

	got, err := s.FirstRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("the cold screen was offered nothing")
	}
	// One per era, so no era is represented twice and no movie repeats.
	seen := map[string]bool{}
	for _, h := range got {
		if seen[h.ID] {
			t.Errorf("%s was offered twice", h.ID)
		}
		seen[h.ID] = true
		if h.Poster == "" {
			t.Errorf("%s was offered with no poster", h.ID)
		}
	}
	if len(got) > len(eras) {
		t.Errorf("offered %d, want at most one for each of the %d eras", len(got), len(eras))
	}
}

// TestFirstRunDoesNotAlwaysOpenOnTheOldest is why the eras are
// shuffled rather than sorted.
//
// In era order the screen had the same shape every visit — oldest film
// top left — and a window with room for only four tiles got the four
// oldest eras every single time, because the client takes as many as
// fit from the front of the list.
func TestFirstRunDoesNotAlwaysOpenOnTheOldest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	assumePostersShow(t)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.posters (tconst, poster_url, status, fetched_at)
		SELECT tconst, 'https://m.media-amazon.com/images/M/x.jpg', 'ok', now()
		FROM catalog.titles
		ON CONFLICT (tconst) DO UPDATE SET poster_url = EXCLUDED.poster_url`); err != nil {
		t.Fatal(err)
	}

	// The fixture is small, so this asks the same question many times
	// and looks at what came first.
	firsts := map[string]int{}
	rounds := 0
	for i := 0; i < 60; i++ {
		got, err := s.FirstRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) < 2 {
			continue
		}
		rounds++
		firsts[got[0].ID]++
	}
	if rounds == 0 {
		t.Skip("the fixture has fewer than two eras with posters")
	}
	if len(firsts) < 2 {
		t.Errorf("the same film opened the screen every time: %v", firsts)
	}
}

func TestFirstRunOffersNothingBeforeThePostersArrive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.posters`); err != nil {
		t.Fatal(err)
	}
	got, err := s.FirstRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// An empty screen, not eight grey boxes.
	if len(got) != 0 {
		t.Errorf("offered %+v before any poster was known", got)
	}
}

func TestFirstRunSkipsABlankOrMissingPoster(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)

	prev := posterGone
	posterGone = func(_ context.Context, raw string) (bool, bool) {
		dead := raw == "https://img.test/gone.jpg"
		return dead, dead
	}
	t.Cleanup(func() { posterGone = prev })

	// Reloaded is the only picture in its era that still exists. The
	// Matrix has an empty address, and Shawshank's address 404s. Neither
	// should be offered, and the era should not come up empty while a
	// later candidate is fine.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.posters (tconst, poster_url, status, fetched_at)
		SELECT tconst,
		       CASE tconst
		           WHEN 'tt0234215' THEN 'https://img.test/live.jpg'
		           WHEN 'tt0133093' THEN ''
		           ELSE 'https://img.test/gone.jpg'
		       END,
		       'ok', now()
		FROM catalog.titles
		ON CONFLICT (tconst) DO UPDATE SET poster_url = EXCLUDED.poster_url`); err != nil {
		t.Fatal(err)
	}

	got, err := s.FirstRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "tt0234215" {
		t.Fatalf("got %+v, want only the film whose poster still exists", got)
	}
}
