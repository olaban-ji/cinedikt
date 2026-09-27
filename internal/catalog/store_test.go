package catalog

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore opens the database named by CATALOG_TEST_URL, or skips.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("CATALOG_TEST_URL")
	if url == "" {
		t.Skip("set CATALOG_TEST_URL to run the Postgres tests")
	}
	s, err := Open(context.Background(), url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// A small set of films standing in for the real dump, with the shapes
// that matter: a two-director film, a documentary, an adult title, an
// unrated film, and television that must not survive.
const (
	basicsRows = basicsHeader + "\n" +
		"tt0133093\tmovie\tThe Matrix\tThe Matrix\t0\t1999\t\\N\t136\tAction,Sci-Fi\n" +
		"tt0234215\tmovie\tThe Matrix Reloaded\tThe Matrix Reloaded\t0\t2003\t\\N\t138\tAction,Sci-Fi\n" +
		"tt0111161\tmovie\tThe Shawshank Redemption\tThe Shawshank Redemption\t0\t1994\t\\N\t142\tDrama\n" +
		"tt0000001\tmovie\tAn Unrated Film\tAn Unrated Film\t0\t1930\t\\N\t\\N\tDrama\n" +
		"tt0000002\tmovie\tA Documentary\tA Documentary\t0\t2011\t\\N\t88\tDocumentary\n" +
		"tt0000003\tmovie\tAdult Film\tAdult Film\t1\t2011\t\\N\t70\tAdult\n" +
		"tt9999001\ttvEpisode\tSome Episode\tSome Episode\t0\t2015\t\\N\t44\tDrama\n" +
		"tt9999002\ttvSeries\tSome Series\tSome Series\t0\t2015\t2019\t44\tDrama\n" +
		"tt9999003\tshort\tA Short\tA Short\t0\t1910\t\\N\t12\tComedy\n" +
		"tt9999004\tvideo\tA Video\tA Video\t0\t2001\t\\N\t60\tMusic"

	principalsRows = principalsHeader + "\n" +
		"tt0133093\t1\tnm0000206\tactor\t\\N\t[\"Neo\"]\n" +
		"tt0133093\t2\tnm0000401\tactress\t\\N\t[\"Trinity\"]\n" +
		"tt0133093\t3\tnm0915989\tactor\t\\N\t[\"Morpheus\"]\n" +
		"tt0133093\t4\tnm0001570\twriter\t\\N\t\\N\n" +
		"tt0234215\t1\tnm0000206\tactor\t\\N\t[\"Neo\"]\n" +
		"tt0111161\t1\tnm0000209\tactor\t\\N\t[\"Andy Dufresne\"]\n" +
		"tt0111161\t2\tnm0000151\tactor\t\\N\t[\"Ellis Boyd 'Red' Redding\"]\n" +
		"tt0000001\t1\tnm0000209\tactor\t\\N\t\\N\n" +
		"tt9999001\t1\tnm7777777\tactor\t\\N\t[\"TV Person\"]\n" +
		"tt9999002\t1\tnm7777777\tactor\t\\N\t[\"TV Person\"]"

	crewRows = "tconst\tdirectors\twriters\n" +
		"tt0133093\tnm0905154,nm0905152\tnm0905154\n" +
		"tt0234215\tnm0905154,nm0905152\tnm0905154\n" +
		"tt0111161\tnm0001104\tnm0000175\n" +
		"tt9999001\tnm8888888\t\\N\n" +
		"tt0000002\t\\N\t\\N"

	ratingsRows = "tconst\taverageRating\tnumVotes\n" +
		"tt0133093\t8.7\t2081234\n" +
		"tt0234215\t7.2\t604321\n" +
		"tt0111161\t9.3\t2900000\n" +
		"tt0000001\t\\N\t\\N\n" +
		"tt9999001\t8.1\t5000"

	namesRows = "nconst\tprimaryName\tbirthYear\tdeathYear\tprimaryProfession\tknownForTitles\n" +
		"nm0000206\tKeanu Reeves\t1964\t\\N\tactor\ttt0133093\n" +
		"nm0000401\tCarrie-Anne Moss\t1967\t\\N\tactress\ttt0133093\n" +
		"nm0915989\tLaurence Fishburne\t1961\t\\N\tactor\ttt0133093\n" +
		"nm0905154\tLana Wachowski\t1965\t\\N\tdirector\ttt0133093\n" +
		"nm0905152\tLilly Wachowski\t1967\t\\N\tdirector\ttt0133093\n" +
		"nm0000209\tTim Robbins\t1958\t\\N\tactor\ttt0111161\n" +
		"nm0000151\tMorgan Freeman\t1937\t\\N\tactor\ttt0111161\n" +
		"nm0001104\tFrank Darabont\t1959\t\\N\tdirector\ttt0111161\n" +
		"nm7777777\tTV Only\t1970\t\\N\tactor\ttt9999001\n" +
		"nm8888888\tTV Director\t1960\t\\N\tdirector\ttt9999001"
)

// resetLive puts the database back to "nothing has ever been published",
// which is the state the first-run paths are about. The tests share one
// database, so a test that cares has to say so.
func resetLive(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`DROP SCHEMA IF EXISTS ` + Live + ` CASCADE`,
		`DROP SCHEMA IF EXISTS ` + retired + ` CASCADE`,
		`DELETE FROM meta.generation`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
}

// loadFixture runs a whole import of the rows above into catalog_next.
func loadFixture(t *testing.T, s *Store) Counts {
	t.Helper()
	ctx := context.Background()
	if err := s.ResetStaging(ctx); err != nil {
		t.Fatal(err)
	}
	kept, _, err := s.LoadTitles(ctx, quietLogger(), gzipped(t, basicsRows))
	if err != nil {
		t.Fatal(err)
	}
	credited := make(NConsts)
	if _, err := s.LoadPrincipals(ctx, quietLogger(), gzipped(t, principalsRows), kept, credited); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadDirectors(ctx, quietLogger(), gzipped(t, crewRows), kept, credited); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRatings(ctx, quietLogger(), gzipped(t, ratingsRows), kept); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadNames(ctx, quietLogger(), gzipped(t, namesRows), credited); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, quietLogger()); err != nil {
		t.Fatal(err)
	}
	counts, err := s.CountStaging(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestLoadKeepsOnlyMoviesAndTheirPeople(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	counts := loadFixture(t, s)

	// Six movies; the episode, series, short and video are gone.
	if counts.Titles != 6 {
		t.Errorf("titles = %d, want 6", counts.Titles)
	}
	// Seven kept credits — three on The Matrix, one on Reloaded, two on
	// Shawshank, one on the unrated film. The writer and the two
	// television rows go.
	if counts.Principals != 7 {
		t.Errorf("principals = %d, want 7", counts.Principals)
	}
	// Two two-director films plus one single: five rows. This is the
	// case a source that advanced per line would have lost.
	if counts.Directors != 5 {
		t.Errorf("directors = %d, want 5", counts.Directors)
	}
	// Three rated movies: the null rating and the episode are not rows.
	if counts.Ratings != 3 {
		t.Errorf("ratings = %d, want 3", counts.Ratings)
	}
	// Eight credited people; the two known only from television are not
	// stored even though name.basics lists them.
	if counts.Names != 8 {
		t.Errorf("names = %d, want 8", counts.Names)
	}

	var tvPeople int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM catalog_next.names WHERE nconst IN ('nm7777777','nm8888888')`).Scan(&tvPeople); err != nil {
		t.Fatal(err)
	}
	if tvPeople != 0 {
		t.Errorf("%d people known only from television were stored", tvPeople)
	}
}

func TestBothDirectorsOfAFilmSurviveInOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	loadFixture(t, s)

	rows, err := s.pool.Query(ctx,
		`SELECT nconst, ordering FROM catalog_next.directors WHERE tconst = 'tt0133093' ORDER BY ordering`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		var order int
		if err := rows.Scan(&id, &order); err != nil {
			t.Fatal(err)
		}
		if order != len(got) {
			t.Errorf("%s has ordering %d at position %d", id, order, len(got))
		}
		got = append(got, id)
	}
	if len(got) != 2 || got[0] != "nm0905154" || got[1] != "nm0905152" {
		t.Errorf("directors = %v, want both Wachowskis in the listed order", got)
	}
}

func TestCheckRejectsAMixedGeneration(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	counts := loadFixture(t, s)

	if share, err := s.Check(ctx, counts); err != nil {
		t.Errorf("a clean load was rejected at %.4f: %v", share, err)
	}

	// Credits naming titles that are not stored is what a principals
	// file from a different day looks like.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO catalog_next.principals (tconst, ordering, nconst, category)
		SELECT 'tt' || g, 1, 'nm0000206', 'actor' FROM generate_series(1000, 2000) g`); err != nil {
		t.Fatal(err)
	}
	counts, err := s.CountStaging(ctx)
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.Check(ctx, counts)
	if err == nil {
		t.Fatalf("a mixed generation was accepted at %.4f", share)
	}
	if share >= MinIntegrity {
		t.Errorf("integrity reported as %.4f, which should have passed", share)
	}
	// The notifier is told the share, not handed the sentence.
	var ie *IntegrityError
	if !errors.As(err, &ie) || ie.Share != share {
		t.Errorf("err = %v, want an IntegrityError carrying %.4f", err, share)
	}
}

func TestCheckRejectsAnEmptyTable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	loadFixture(t, s)
	if _, err := s.pool.Exec(ctx, `TRUNCATE catalog_next.ratings`); err != nil {
		t.Fatal(err)
	}
	counts, err := s.CountStaging(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Check(ctx, counts)
	if err == nil {
		t.Fatal("a load with an empty table was accepted")
	}
	var ie *IntegrityError
	if !errors.As(err, &ie) || err.Error() != "catalog: no ratings loaded" {
		t.Errorf("err = %v, want an IntegrityError with the text it always had", err)
	}
}

func TestPublishSwapsAndKeepsTheOldOneUntilNextRun(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	resetLive(t, s)

	// Nothing published yet.
	if ready, err := s.LiveReady(ctx); err != nil || ready {
		t.Fatalf("ready=%v err=%v before any publish", ready, err)
	}

	gen := genAt(at(24, 0, 42))
	counts := loadFixture(t, s)
	if err := s.Publish(ctx, gen, counts); err != nil {
		t.Fatal(err)
	}
	if ready, err := s.LiveReady(ctx); err != nil || !ready {
		t.Fatalf("ready=%v err=%v after publishing", ready, err)
	}

	var titles int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM catalog.titles`).Scan(&titles); err != nil {
		t.Fatal(err)
	}
	if titles != 6 {
		t.Errorf("live titles = %d", titles)
	}

	// The stamps came back, so the next hour has something to compare.
	published, importedAt, err := s.Published(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if importedAt.IsZero() {
		t.Error("imported_at was not recorded")
	}
	if ok, why := Ready(published, gen, Files); ok {
		t.Errorf("the generation just published would import again: %s", why)
	}
	if ok, _ := Ready(published, genAt(at(25, 0, 42)), Files); !ok {
		t.Error("tomorrow's generation would not import")
	}

	// A second publish retires the first, and the first is still there
	// for readers that have not finished with it.
	counts = loadFixture(t, s)
	if err := s.Publish(ctx, genAt(at(25, 0, 42)), counts); err != nil {
		t.Fatal(err)
	}
	var old bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'catalog_old')`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if !old {
		t.Error("the previous generation was dropped inside the swap")
	}
	if err := s.DropRetired(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'catalog_old')`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if old {
		t.Error("the previous generation was not dropped on the next run")
	}
}

// TestDropRetiredKeepsItsLockTimeoutToItself is the timeout doing its
// job on the drop and nowhere else: a reader still on the old
// generation makes the drop give up rather than wait, and afterwards no
// connection in the pool carries the timeout into the import's own long
// statements.
func TestDropRetiredKeepsItsLockTimeoutToItself(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`DROP SCHEMA IF EXISTS ` + retired + ` CASCADE`,
		`CREATE SCHEMA ` + retired,
		`CREATE TABLE ` + retired + `.titles (tconst text)`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	reader, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Exec(ctx, `LOCK TABLE `+retired+`.titles IN ACCESS SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	// Bounded, so a drop that waits forever fails the test rather than
	// hanging it.
	waitCtx, cancel := context.WithTimeout(ctx, 6*PublishLockTimeout)
	err = s.DropRetired(waitCtx)
	cancel()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Errorf("err = %v, want the drop to give up on a lock timeout", err)
	}
	if err := reader.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DropRetired(ctx); err != nil {
		t.Fatal(err)
	}

	// Holding every connection the pool can have at once is the only
	// way to be sure each one is looked at, whichever ran the drop.
	conns := make([]*pgxpool.Conn, s.pool.Config().MaxConns)
	for i := range conns {
		if conns[i], err = s.pool.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
		defer conns[i].Release()
	}
	for i, c := range conns {
		var setting, reset string
		if err := c.QueryRow(ctx,
			`SELECT setting, reset_val FROM pg_settings WHERE name = 'lock_timeout'`).Scan(&setting, &reset); err != nil {
			t.Fatal(err)
		}
		if setting != reset {
			t.Errorf("connection %d kept lock_timeout = %sms, want its default %sms", i, setting, reset)
		}
	}
}

// TestTheNotifierRemembersThroughTheStore is the Memory half of the
// Store: what it saves is what it reads back, and a database it has
// never written to reads as nothing rather than an error.
func TestTheNotifierRemembersThroughTheStore(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.notify`); err != nil {
		t.Fatal(err)
	}
	raw, err := s.LoadNotifyState(ctx)
	if err != nil || raw != nil {
		t.Fatalf("a fresh database read %q, %v; want nothing and no error", raw, err)
	}
	for _, want := range []string{`{"v": 1, "board_id": 77}`, `{"v": 1, "board_id": 78}`} {
		if err := s.SaveNotifyState(ctx, []byte(want)); err != nil {
			t.Fatal(err)
		}
		got, err := s.LoadNotifyState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("read back %s, want %s", got, want)
		}
	}
}

func TestPublishedFilmsIsTheLiveCount(t *testing.T) {
	s := testStore(t)
	publishFixture(t, s)
	n, err := s.PublishedFilms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("the live catalog has films, and PublishedFilms says none")
	}
}
