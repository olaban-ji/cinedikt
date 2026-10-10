package catalog

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image/color"
	imagepng "image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cinedikt/internal/daily"
)

// resetDaily empties Cinedikt Daily's tables. The tests share one
// database, so each one that reads them starts from nothing.
func resetDaily(t *testing.T, s *Store) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM meta.daily_deals`,
		`DELETE FROM meta.daily_moves`,
		`DELETE FROM meta.daily_games`,
		`DELETE FROM meta.daily_players`,
		`DELETE FROM meta.daily_puzzles`,
	} {
		if _, err := s.pool.Exec(context.Background(), stmt); err != nil {
			t.Fatal(err)
		}
	}
}

// The daily fixture's people: a director and seven billed actors, on
// every candidate, the six a game shows being the first six actors.
var (
	dailyDirector = "nm9900001"
	dailyActors   = []string{"nm9900002", "nm9900003", "nm9900004", "nm9900005", "nm9900006", "nm9900007", "nm9900008"}
)

// dailyCandidates is how many candidates the fixture has: one for each
// day a full pass keeps picked, UTC yesterday to eight days ahead.
const dailyCandidates = DailyBehind + 1 + DailyAhead

// dailySheet is how many movies each candidate's Movies sheets hold: the
// ten candidates and their crowd, daily.MinCrowd each, every one of them
// all six's, and the forty-six fillers through one of the six; not the
// fillers through the director or the seventh-billed alone, nor the
// unrated movie.
const dailySheet = 10 + 10*daily.MinCrowd + 46

// crowdN is daily.MinCrowd as the fixture's SQL writes it, and crowdOf
// the id of candidate 1's kth crowd movie, tt990401k.
var crowdN = fmt.Sprint(daily.MinCrowd)

func crowdOf(k int) string { return fmt.Sprintf("tt990401%d", k) }

// dailyFixture adds to the published fixture what a day's puzzle needs,
// straight into the live tables: ten candidates, well past
// daily.MinVotes and none in the first-run pool, one in each of the
// opening screen's eight eras and a second in two of them, each with a
// runtime, a poster and its colour, all eight people on every one of
// them, and half with only TMDb's overview and half with no synopsis at
// all, since an answer needs none; each candidate's crowd, MinCrowd
// movies the six made in its year at its rating, so every sheet keeps
// daily.MinCrowd inside its decade and band, Crowd g.k for candidate g;
// sixty more movies, each through one of those people, rated and voted
// from the fewest up, Filler g through the director when g is a multiple
// of eight and otherwise through actor g mod 8, the seventh-billed when
// that is 7; and one unrated movie that must be on no sheet. None but
// the candidates has a poster, so none but they can be one.
func dailyFixture(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	publishFixture(t, s)
	resetDaily(t, s)
	t.Cleanup(func() {
		resetDaily(t, s)
		for _, stmt := range []string{
			`DELETE FROM meta.posters WHERE tconst LIKE 'tt99%'`,
			`DELETE FROM meta.synopses WHERE tconst LIKE 'tt99%'`,
			`DELETE FROM meta.people WHERE nconst LIKE 'nm99%'`,
		} {
			_, _ = s.pool.Exec(context.Background(), stmt)
		}
		publishFixture(t, s)
	})
	for _, stmt := range []string{
		`INSERT INTO ` + Live + `.names (nconst, primary_name) VALUES
		    ('nm9900001', 'Dee Rector'), ('nm9900002', 'Ava First'), ('nm9900003', 'Bo Second'),
		    ('nm9900004', 'Cy Third'), ('nm9900005', 'Di Fourth'), ('nm9900006', 'Ed Fifth'),
		    ('nm9900007', 'Flo Sixth'), ('nm9900008', 'Gus Seventh')`,
		// The candidates: tt9900101 to tt9900110.
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres, runtime_minutes)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 'Candidate ' || g, 'Candidate ' || g, false,
		        (ARRAY[1931, 1965, 1984, 1999, 2008, 2015, 2021, 2024, 1997, 2016])[g],
		        CASE WHEN g % 2 = 0 THEN ARRAY['Drama', 'Romance'] ELSE ARRAY['Action', 'Sci-Fi'] END,
		        85 + 10 * g
		 FROM generate_series(1, 10) g`,
		`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 7.0 + g / 10.0, 500000 + g * 1000 FROM generate_series(1, 10) g`,
		`INSERT INTO ` + Live + `.directors (tconst, nconst, ordering)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 'nm9900001', 0 FROM generate_series(1, 10) g`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), a, 'nm990000' || (a + 1), 'actor', 'Hero ' || a
		 FROM generate_series(1, 10) g, generate_series(1, 7) a`,
		`INSERT INTO meta.synopses (tconst, overview, source, fetched_at)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'),
		        'A stranger arrives in town number ' || g || '. Nothing is the same after.', 'tmdb', now()
		 FROM generate_series(1, 10) g WHERE g % 2 = 0`,
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at, colour)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 'https://img.example/' || g || '.jpg', 'ok', now(), '#2' || (g - 1) || '382d'
		 FROM generate_series(1, 10) g`,
		// The crowd: tt9904g1 to tt9904gN for candidate g, N daily.MinCrowd.
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres, runtime_minutes)
		 SELECT 'tt9904' || lpad(g::text, 2, '0') || k, 'Crowd ' || g || '.' || k, 'Crowd ' || g || '.' || k, false,
		        t.start_year, ARRAY['Drama'], 100
		 FROM generate_series(1, 10) g CROSS JOIN generate_series(1, ` + crowdN + `) k
		 JOIN ` + Live + `.titles t ON t.tconst = 'tt99001' || lpad(g::text, 2, '0')`,
		`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
		 SELECT 'tt9904' || lpad(g::text, 2, '0') || k, 7.0 + g / 10.0, 1000 FROM generate_series(1, 10) g, generate_series(1, ` + crowdN + `) k`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt9904' || lpad(g::text, 2, '0') || k, a, 'nm990000' || (a + 1), 'actor', NULL
		 FROM generate_series(1, 10) g, generate_series(1, ` + crowdN + `) k, generate_series(1, 6) a`,
		// The sixty: tt9900201 to tt9900260, each through one person, the
		// director's through the crew.
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres, runtime_minutes)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 'Filler ' || g, 'Filler ' || g, false, 1960 + g, ARRAY['Drama'], 95
		 FROM generate_series(1, 60) g`,
		`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 4.0 + (g % 50) / 10.0, 100 + g * 10 FROM generate_series(1, 60) g`,
		`INSERT INTO ` + Live + `.directors (tconst, nconst, ordering)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 'nm9900001', 0 FROM generate_series(1, 60) g WHERE g % 8 = 0`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 1, 'nm990000' || (g % 8 + 1), 'actor', NULL
		 FROM generate_series(1, 60) g WHERE g % 8 <> 0`,
		// Unrated, through the star.
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres)
		 VALUES ('tt9900299', 'Nobody Rated It', 'Nobody Rated It', false, 2001, ARRAY['Drama'])`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 VALUES ('tt9900299', 1, 'nm9900002', 'actor', NULL)`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
}

var oct8 = time.Date(2026, 10, 8, 10, 30, 0, 0, time.UTC)

func dailyJob(s *Store, now *time.Time) *DailyJob {
	return &DailyJob{Store: s, Logger: quietLogger(), Now: func() time.Time { return *now }}
}

// TestTheDailyJobKeepsYesterdayToEightDaysAheadPicked, once: a pass
// picks every day from UTC yesterday, which readers west of UTC are
// still on, to eight days past UTC today, numbered from the first, each
// a different answer, and the next pass changes nothing. Every one of
// the ten candidates is an answer, though none has an overview from
// OMDb and half have none at all.
func TestTheDailyJobKeepsYesterdayToEightDaysAheadPicked(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8
	job := dailyJob(s, &now)
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}

	answers := map[string]int{}
	var eras []int
	var picked []*daily.Puzzle
	from := daily.Today(oct8).AddDate(0, 0, -1)
	for i := range dailyCandidates {
		day := from.AddDate(0, 0, i)
		p, err := s.DailyPuzzle(ctx, day)
		if err != nil {
			t.Fatalf("%s: %v", daily.DayString(day), err)
		}
		if p.No != i+1 || !p.Day.Equal(day) {
			t.Errorf("%s is No. %d on %s", daily.DayString(day), p.No, daily.DayString(p.Day))
		}
		answers[p.Answer.ID]++
		eras = append(eras, p.Era)
		picked = append(picked, p)
	}
	if len(answers) != dailyCandidates {
		t.Errorf("answers = %v, want ten different", answers)
	}
	if daily.DayString(picked[0].Day) != "2026-10-07" || daily.DayString(picked[len(picked)-1].Day) != "2026-10-16" {
		t.Errorf("picked %s to %s, want 7 to 16 October", daily.DayString(picked[0].Day), daily.DayString(picked[len(picked)-1].Day))
	}
	for _, day := range []time.Time{from.AddDate(0, 0, -1), from.AddDate(0, 0, dailyCandidates)} {
		if _, err := s.DailyPuzzle(ctx, day); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s, outside the window: %v, want ErrNotFound", daily.DayString(day), err)
		}
	}
	// A week uses seven eras before one comes round again.
	week := slices.Clone(eras[:7])
	slices.Sort(week)
	if len(slices.Compact(week)) != 7 {
		t.Errorf("eras = %v, want the first seven days in seven eras", eras)
	}

	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range picked {
		again, err := s.DailyPuzzleNo(ctx, p.No)
		if err != nil {
			t.Fatal(err)
		}
		if again.Answer.ID != p.Answer.ID || len(again.Movies) != len(p.Movies) {
			t.Errorf("No. %d changed from %s to %s", p.No, p.Answer.ID, again.Answer.ID)
		}
	}
	var rows int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM meta.daily_puzzles`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != dailyCandidates {
		t.Errorf("%d puzzles after two passes, want %d", rows, dailyCandidates)
	}
}

// TestTheJobNeverPicksADayBeforeTheFirst: a database whose first puzzle
// is UTC today's, as one that picked before readers had zones is, keeps
// its numbers; yesterday is left without a puzzle rather than made
// No. 0.
func TestTheJobNeverPicksADayBeforeTheFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8.AddDate(0, 0, 1)
	job := dailyJob(s, &now)
	job.Days = 2
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	now, job.Days = oct8, 0
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("the day before the first: %v, want ErrNotFound", err)
	}
	for i, want := range []int{1, 2, 3, 9} {
		day := oct8.AddDate(0, 0, []int{0, 1, 2, 8}[i])
		p, err := s.DailyPuzzle(ctx, day)
		if err != nil || p.No != want {
			t.Errorf("%s: %+v, %v; want No. %d", daily.DayString(day), p, err, want)
		}
	}
}

// TestADailyPuzzleIsTheCastAndTheirMovies: the answer with its runtime
// and colour; its director; the first six billed in reveal order, the
// star last and the seventh left out, each also in the most voted movie
// of their own; and the sheets, every rated movie of the six's, the
// answer among them crediting everyone, every candidate's crowd too,
// and nothing that is only the director's or the seventh's, nor
// unrated.
func TestADailyPuzzleIsTheCastAndTheirMovies(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8
	job := dailyJob(s, &now)
	job.Days = 2
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.DailyPuzzle(ctx, oct8)
	if err != nil {
		t.Fatal(err)
	}
	a := p.Answer
	g := 0
	if _, err := fmt.Sscanf(a.ID, "tt99001%02d", &g); err != nil || g < 1 || g > 10 {
		t.Fatalf("answer = %+v", a)
	}
	if a.Rating < 7 || a.Year == 0 || len(a.Genres) != 2 || a.Length != 85+10*g || a.Colour != fmt.Sprintf("#2%d382d", g-1) {
		t.Errorf("answer = %+v", a)
	}
	if p.Genre != a.Genres[0] {
		t.Errorf("genre = %q, want the first of %v", p.Genre, a.Genres)
	}
	if !reflect.DeepEqual(p.Directors, []daily.Named{{ID: dailyDirector, Name: "Dee Rector"}}) {
		t.Errorf("directors = %+v", p.Directors)
	}
	var cast []string
	for _, b := range p.Cast {
		also := "none"
		if b.Also != nil {
			also = fmt.Sprintf("%s %d", b.Also.Title, b.Also.Year)
		}
		cast = append(cast, fmt.Sprintf("%s %s %d, also %s", b.ID, b.Name, b.Billing, also))
	}
	// The most voted filler of each actor's: Filler g, for the highest g
	// of theirs up to sixty.
	want := []string{
		"nm9900007 Flo Sixth 6, also Filler 54 2014",
		"nm9900006 Ed Fifth 5, also Filler 53 2013",
		"nm9900005 Di Fourth 4, also Filler 60 2020",
		"nm9900004 Cy Third 3, also Filler 59 2019",
		"nm9900003 Bo Second 2, also Filler 58 2018",
		"nm9900002 Ava First 1, also Filler 57 2017",
	}
	if !slices.Equal(cast, want) {
		t.Errorf("cast =\n%s\nwant\n%s", strings.Join(cast, "\n"), strings.Join(want, "\n"))
	}
	if len(p.Movies) != dailySheet {
		t.Errorf("%d movies on the sheets, want %d", len(p.Movies), dailySheet)
	}
	for _, m := range p.Movies {
		var n int
		switch {
		case strings.HasPrefix(m.ID, "tt99001"):
			if !slices.Equal(m.Cast, []int{0, 1, 2, 3, 4, 5}) || !m.Dir {
				t.Errorf("candidate %s = %+v, want all six and the director", m.ID, m)
			}
		case strings.HasPrefix(m.ID, "tt99002"):
			fmt.Sscanf(m.ID, "tt99002%02d", &n)
			if slot := 6 - n%8; n%8 == 0 || n%8 == 7 || !slices.Equal(m.Cast, []int{slot}) || m.Dir {
				t.Errorf("filler %d = %+v, want slot %d alone", n, m, slot)
			}
		case strings.HasPrefix(m.ID, "tt9904"):
			if !slices.Equal(m.Cast, []int{0, 1, 2, 3, 4, 5}) || m.Dir {
				t.Errorf("crowd %s = %+v, want all six and no director", m.ID, m)
			}
		default:
			t.Errorf("%s is on the sheets", m.ID)
		}
		if m.Title == "" || m.Rating == 0 || m.Year == 0 || m.Genres == nil {
			t.Errorf("movie %s = %+v", m.ID, m)
		}
	}
	if !slices.IsSortedFunc(p.Movies, func(x, y daily.Movie) int { return cmp.Or(cmp.Compare(x.Year, y.Year), cmp.Compare(x.ID, y.ID)) }) {
		t.Error("the sheets are not in year order")
	}

	if _, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("tomorrow, not picked: %v, want ErrNotFound", err)
	}
	if _, err := s.DailyPuzzleNo(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("No. 99: %v, want ErrNotFound", err)
	}
}

// TestEachOfTheSixHasTheirFourHundredMostVoted on their sheet, as a map
// caps its spine: four hundred and ten more movies of the star's, the
// least voted of hers but the answer, leave the fifty-eight fewest off
// her sheet, though the fifth-billed is on the ten fewest and they are
// on his; and the answer, made the least voted of hers with no more
// than daily.MinVotes, is on both still. On hers, those ten would be
// cards past her cap that only someone else of the six let in, each a
// sign that a hidden name is in it.
func TestEachOfTheSixHasTheirFourHundredMostVoted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8
	job := dailyJob(s, &now)
	job.Days = 2
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := s.DailyPuzzle(ctx, oct8)
	if err != nil {
		t.Fatal(err)
	}
	resetDaily(t, s)
	for _, stmt := range []string{
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres)
		 SELECT 'tt97' || lpad(g::text, 5, '0'), 'Extra ' || g, 'Extra ' || g, false, 1950 + g % 70, ARRAY['Drama']
		 FROM generate_series(1, 410) g`,
		`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
		 SELECT 'tt97' || lpad(g::text, 5, '0'), 6.0, $1::int + g FROM generate_series(1, 410) g`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt97' || lpad(g::text, 5, '0'), 1, 'nm9900002', 'actress', NULL FROM generate_series(1, 410) g`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt97' || lpad(g::text, 5, '0'), 2, 'nm9900006', 'actor', NULL FROM generate_series(1, 10) g`,
		// Votes play no part in the day's order, so the same answer is
		// picked again, now the least voted movie the star is in: the
		// fillers and the crowd raised past it, and it left with just
		// enough to be a candidate.
		`UPDATE ` + Live + `.ratings SET num_votes = num_votes + $1::int WHERE tconst LIKE 'tt99002%' OR tconst LIKE 'tt9904%'`,
		`UPDATE ` + Live + `.ratings SET num_votes = $1::int WHERE tconst = '` + first.Answer.ID + `'`,
	} {
		var args []any
		if strings.Contains(stmt, "$1") {
			args = append(args, daily.MinVotes)
		}
		if _, err := s.pool.Exec(ctx, stmt, args...); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.DailyPuzzle(ctx, oct8)
	if err != nil || p.Answer.ID != first.Answer.ID {
		t.Fatalf("picked again: %+v, %v; want %s", p, err, first.Answer.ID)
	}
	extra := func(id string) int {
		n := 0
		fmt.Sscanf(id, "tt97%05d", &n)
		return n
	}
	// Once a game is over every card on a sheet is readable, and its
	// poster read: the whole sheet.
	done := daily.Replay(p, []daily.Move{{Seq: 1, Key: "k", Kind: daily.KindReveal}})
	star := p.SheetWants(done, 5)
	// Hers besides the extras: the answer, the nine other candidates,
	// every candidate's crowd and her eight fillers. The extras fill the
	// rest of her MaxSheet, the most voted first, and the rest of the 410
	// are cut.
	hers := 1 + 9 + 10*daily.MinCrowd + 8
	cut := 410 - (daily.MaxSheet - hers)
	extras := 0
	for _, id := range star {
		if n := extra(id); n > 0 {
			extras++
			if n <= cut {
				t.Errorf("Extra %d, among the least voted of hers, is on the star's sheet", n)
			}
		}
	}
	if len(star) != daily.MaxSheet || extras != daily.MaxSheet-hers || !slices.Contains(star, p.Answer.ID) {
		t.Errorf("the star's sheet has %d movies, %d of them extras, the answer among them %v",
			len(star), extras, slices.Contains(star, p.Answer.ID))
	}
	// His: the answer, the nine other candidates, the crowd, his seven
	// fillers and the ten he shares with her.
	ed := p.SheetWants(done, 1)
	for n := 1; n <= 10; n++ {
		if !slices.Contains(ed, fmt.Sprintf("tt97%05d", n)) {
			t.Errorf("Extra %d, which the fifth-billed is on, is not on his sheet", n)
		}
	}
	if len(ed) != 1+9+10*daily.MinCrowd+7+10 || !slices.Contains(ed, p.Answer.ID) {
		t.Errorf("the fifth-billed's sheet has %d movies: %v", len(ed), ed)
	}
	// Eleven up to the cut are on nobody's sheet, so not kept.
	for _, m := range p.Movies {
		if n := extra(m.ID); n >= 11 && n <= cut {
			t.Errorf("Extra %d, on nobody's sheet, is kept", n)
		}
	}
	if !slices.ContainsFunc(p.Movies, func(m daily.Movie) bool { return m.ID == p.Answer.ID && len(m.Cast) == 6 && len(m.Sheets) == 6 }) {
		t.Error("the answer, the least voted, is not on every sheet")
	}
	// Played as the API serves it: the sheet at the end, everyone
	// showing, holds no more than MaxSheet.
	if movies, ok := p.SheetOf(done, 5, daily.Live{}); !ok || len(movies) != daily.MaxSheet {
		t.Errorf("the star's sheet once it is over: %d movies, %v", len(movies), ok)
	}
}

// TestEachOfTheSixNeedsMoviesBesideTheAnswer: a sheet is free to open,
// and the sixth-billed's is open from Play, so a newcomer in that place
// with nothing else in the catalog, whose sheet would be the answer
// alone, leaves the one candidate with a runtime no answer for the day;
// with daily.MinSheet movies of their own, of the answer's 1990s and
// rated in its band, it is the answer.
func TestEachOfTheSixNeedsMoviesBesideTheAnswer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	for _, stmt := range []string{
		`UPDATE ` + Live + `.titles SET runtime_minutes = NULL WHERE tconst LIKE 'tt99001%' AND tconst <> 'tt9900104'`,
		`INSERT INTO ` + Live + `.names (nconst, primary_name) VALUES ('nm9900009', 'Hal Newcomer')`,
		`UPDATE ` + Live + `.principals SET nconst = 'nm9900009' WHERE tconst = 'tt9900104' AND ordering = 6`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	now := oct8
	job := dailyJob(s, &now)
	job.Days = 1
	if err := job.Run(ctx); err == nil || !strings.Contains(err.Error(), "candidates can be the daily answer for 2026-10-07") {
		t.Errorf("a sixth-billed with no other movie: %v, want the day said to have no answer", err)
	}
	for _, stmt := range []string{
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres, runtime_minutes)
		 SELECT 'tt99003' || lpad(g::text, 2, '0'), 'Newcomer ' || g, 'Newcomer ' || g, false, 1990 + g, ARRAY['Drama'], 90
		 FROM generate_series(1, $1::int) g`,
		`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
		 SELECT 'tt99003' || lpad(g::text, 2, '0'), 7.5, 1000 FROM generate_series(1, $1::int) g`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt99003' || lpad(g::text, 2, '0'), 1, 'nm9900009', 'actor', NULL FROM generate_series(1, $1::int) g`,
	} {
		if _, err := s.pool.Exec(ctx, stmt, daily.MinSheet); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("with %d movies of the newcomer's: %v", daily.MinSheet, err)
	}
	done := daily.Replay(p, []daily.Move{{Seq: 1, Key: "k", Kind: daily.KindReveal}})
	if p.Answer.ID != "tt9900104" || p.Cast[0].ID != "nm9900009" || len(p.SheetWants(done, 0)) != daily.MinSheet+1 {
		t.Errorf("with %d movies of the newcomer's: %+v", daily.MinSheet, p)
	}
}

// TestThePickWaitsForACatalogWithRuntimes: a live catalog imported
// before titles kept runtimes picks nothing, and the pass says why once,
// rather than calling every candidate unfit. With the column back it
// picks as usual.
func TestThePickWaitsForACatalogWithRuntimes(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	if _, err := s.pool.Exec(ctx, `ALTER TABLE `+Live+`.titles DROP COLUMN runtime_minutes`); err != nil {
		t.Fatal(err)
	}
	now := oct8
	job := dailyJob(s, &now)
	err := job.Run(ctx)
	if !errors.Is(err, ErrNoRuntimes) || err.Error() != "catalog: the catalog predates runtimes; the next import adds them" {
		t.Errorf("a pass over a catalog with no runtimes: %v", err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM meta.daily_puzzles`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d puzzles picked, %v", n, err)
	}
	if _, err := s.pool.Exec(ctx, `ALTER TABLE `+Live+`.titles ADD COLUMN runtime_minutes int;
		UPDATE `+Live+`.titles SET runtime_minutes = 100`); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx); err != nil {
		t.Errorf("with runtimes: %v", err)
	}
}

// TestACatalogWithoutRuntimesIsImportedAgain: the hour's check imports
// a live catalog that predates runtimes from the very files it was made
// from, rather than leave the Daily waiting for IMDb's next set; one with
// them, or a first run, is left to the stamps.
func TestACatalogWithoutRuntimesIsImportedAgain(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	published := Generation{Files[0]: {}}
	for _, c := range []struct {
		drop      bool
		published Generation
		want      string
	}{
		{false, published, ""},
		{false, nil, ""},
		{true, nil, ""},
		{true, published, "the live catalog predates runtimes"},
	} {
		if c.drop {
			if _, err := s.pool.Exec(ctx, `ALTER TABLE `+Live+`.titles DROP COLUMN IF EXISTS runtime_minutes`); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.liveBehind(ctx, c.published)
		if err != nil || got != c.want {
			t.Errorf("runtimes dropped %v, %d published: %q, %v; want %q", c.drop, len(c.published), got, err, c.want)
		}
	}
}

// TestAnAnswerNeedsARuntimeAndSixBilledCast: with only one candidate
// that has a runtime it is the answer; with five billed cast it is none,
// and the pass says the day has no answer.
func TestAnAnswerNeedsARuntimeAndSixBilledCast(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	if _, err := s.pool.Exec(ctx, `UPDATE `+Live+`.titles SET runtime_minutes = NULL WHERE tconst LIKE 'tt99001%' AND tconst <> 'tt9900104'`); err != nil {
		t.Fatal(err)
	}
	now := oct8
	job := dailyJob(s, &now)
	job.Days = 1
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1)); err != nil || p.Answer.ID != "tt9900104" || p.Answer.Length != 125 {
		t.Errorf("the one candidate with a runtime: %+v, %v", p, err)
	}
	resetDaily(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM `+Live+`.principals WHERE tconst = 'tt9900104' AND ordering >= 6`); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx); err == nil || !strings.Contains(err.Error(), "candidates can be the daily answer for 2026-10-07") {
		t.Errorf("five billed cast: %v, want the day said to have no answer", err)
	}
}

// TestAnyMovieWithMinVotesCanBeTheAnswer, in the first-run pool or not:
// the fixture's candidates are none of them in it, and each is in the
// era its year falls in. With only Candidate 4 left with a runtime, it
// is the day's answer with daily.MinVotes votes, and with one vote fewer
// the day has none.
func TestAnyMovieWithMinVotesCanBeTheAnswer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	var pooled int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM `+Live+`.first_run WHERE tconst LIKE 'tt99%'`).Scan(&pooled); err != nil || pooled != 0 {
		t.Fatalf("%d of the fixture's movies are in the first-run pool, %v", pooled, err)
	}
	cands, err := s.dailyCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cands {
		got = append(got, fmt.Sprintf("%s %d %d", c.ID, c.Year, c.Era))
	}
	slices.Sort(got)
	want := []string{
		"tt9900101 1931 1920", "tt9900102 1965 1960", "tt9900103 1984 1980", "tt9900104 1999 1995", "tt9900105 2008 2005",
		"tt9900106 2015 2013", "tt9900107 2021 2019", "tt9900108 2024 2024", "tt9900109 1997 1995", "tt9900110 2016 2013",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the candidates are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if _, err := s.pool.Exec(ctx, `UPDATE `+Live+`.titles SET runtime_minutes = NULL WHERE tconst LIKE 'tt99001%' AND tconst <> 'tt9900104'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE `+Live+`.ratings SET num_votes = $1 WHERE tconst = 'tt9900104'`, daily.MinVotes-1); err != nil {
		t.Fatal(err)
	}
	now := oct8
	job := dailyJob(s, &now)
	job.Days = 1
	if err := job.Run(ctx); err == nil || !strings.Contains(err.Error(), "none of 0 candidates can be the daily answer for 2026-10-07") {
		t.Errorf("with %d votes: %v, want the day said to have no answer", daily.MinVotes-1, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE `+Live+`.ratings SET num_votes = $1 WHERE tconst = 'tt9900104'`, daily.MinVotes); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatalf("with %d votes: %v", daily.MinVotes, err)
	}
	if p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1)); err != nil || p.Answer.ID != "tt9900104" || p.Era != 1995 {
		t.Errorf("with %d votes: %+v, %v; want Candidate 4, of the 1995 era", daily.MinVotes, p, err)
	}
}

// TestTheCandidatesAreTheOnesBuildFindsCrowded: dailyCandidates asks
// daily.Build's crowd rule of every movie at once, and must ask it as
// Build does, which dailyPuzzleOf asks again of what it reads. Candidate
// 1, of 1931 rated 7.1, has only its own crowd in its decade and band.
// It keeps its place while they are inside the 1930s and from 7.0 to
// 7.9, and loses it, dailyPuzzleOf refusing it alike, when one moves
// just outside either, when one of the six is taken out of one, or when
// the star is its director too, and so no one of the six. And
// the star's cap is counted as Build counts it, with the answer moved
// first: given the answer and its crowd as the least voted of hers, and
// just enough more voted movies of hers to fill her sheet, its crowd are
// the last on it, and with one more the last of them is past it.
//
// The one part of the rule the query leaves to Build is someone's own
// "Also in", which Build sets aside: so it may keep a candidate Build
// refuses, never the other way. The sixth-billed, taken out of one of
// the crowd and given one of her own, the most voted of hers and so
// her "Also in", keeps the candidate in the query, and dailyPuzzleOf
// refuses it; less voted than her fillers, it is no "Also in", and they
// agree again.
func TestTheCandidatesAreTheOnesBuildFindsCrowded(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	exec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, stmt, args...); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	cands, err := s.dailyCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(cands, func(c daily.Candidate) bool { return c.ID == "tt9900101" })
	if len(cands) != dailyCandidates || i < 0 {
		t.Fatalf("%d candidates, Candidate 1 among them %v", len(cands), i >= 0)
	}
	one := cands[i]
	agree := func(when string, want bool) {
		t.Helper()
		cands, err := s.dailyCandidates(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if in := slices.ContainsFunc(cands, func(c daily.Candidate) bool { return c.ID == one.ID }); in != want {
			t.Errorf("%s: Candidate 1 is a candidate %v, want %v", when, in, want)
		}
		_, err = s.dailyPuzzleOf(ctx, nil, 1, oct8, one)
		if fit := err == nil; fit != want || !fit && !strings.Contains(err.Error(), "other movies in the answer's decade and rating band") {
			t.Errorf("%s: dailyPuzzleOf says %v, want fit %v", when, err, want)
		}
	}
	agree("as it is", true)
	for _, c := range []struct {
		year   int
		rating float64
		in     bool
	}{
		{1930, 7.0, true}, {1939, 7.9, true}, {1929, 7.1, false}, {1940, 7.1, false}, {1935, 6.9, false}, {1935, 8.0, false},
	} {
		exec(`UPDATE `+Live+`.titles SET start_year = $1 WHERE tconst = $2`, c.year, crowdOf(1))
		exec(`UPDATE `+Live+`.ratings SET average_rating = $1 WHERE tconst = $2`, c.rating, crowdOf(1))
		agree(fmt.Sprintf("one of the crowd in %d at %v", c.year, c.rating), c.in)
	}
	exec(`UPDATE `+Live+`.titles SET start_year = 1931 WHERE tconst = $1`, crowdOf(1))
	exec(`UPDATE `+Live+`.ratings SET average_rating = 7.1 WHERE tconst = $1`, crowdOf(1))
	exec(`DELETE FROM `+Live+`.principals WHERE tconst = $1 AND nconst = 'nm9900006'`, crowdOf(2))
	agree("the fifth-billed out of one of the crowd", false)
	exec(`INSERT INTO `+Live+`.principals (tconst, ordering, nconst, category) VALUES ($1, 5, 'nm9900006', 'actor')`, crowdOf(2))
	agree("the fifth-billed back", true)
	// The star credited as its director too, as peopleOn has her, is a
	// director and no one of the six, and the seventh-billed, with no
	// crowd, takes her place: by the crew's directors or by a credit.
	exec(`INSERT INTO ` + Live + `.directors (tconst, nconst, ordering) VALUES ('tt9900101', 'nm9900002', 1)`)
	agree("the star in its crew's directors", false)
	exec(`DELETE FROM ` + Live + `.directors WHERE tconst = 'tt9900101' AND nconst = 'nm9900002'`)
	exec(`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category) VALUES ('tt9900101', 9, 'nm9900002', 'director')`)
	agree("the star credited as its director", false)
	exec(`DELETE FROM ` + Live + `.principals WHERE tconst = 'tt9900101' AND ordering = 9`)
	agree("the star only its star again", true)

	last, own := crowdOf(daily.MinCrowd), crowdOf(daily.MinCrowd+1)
	exec(`DELETE FROM `+Live+`.principals WHERE tconst = $1 AND nconst = 'nm9900007'`, last)
	agree("the sixth-billed out of one of the crowd", false)
	exec(`INSERT INTO `+Live+`.titles (tconst, primary_title, original_title, is_adult, start_year, genres, runtime_minutes)
	      VALUES ($1, 'Crowd 1.own', 'Crowd 1.own', false, 1931, ARRAY['Drama'], 100)`, own)
	exec(`INSERT INTO `+Live+`.ratings (tconst, average_rating, num_votes) VALUES ($1, 7.1, 1000000)`, own)
	exec(`INSERT INTO `+Live+`.principals (tconst, ordering, nconst, category) VALUES ($1, 1, 'nm9900007', 'actress')`, own)
	cands, err = s.dailyCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(cands, func(c daily.Candidate) bool { return c.ID == one.ID }) {
		t.Error("with one of her own, her Also in: Candidate 1 is no candidate, want it kept for Build to refuse")
	}
	short := fmt.Sprintf("%d other movies in the answer's decade and rating band besides their Also in", daily.MinCrowd-1)
	if _, err := s.dailyPuzzleOf(ctx, nil, 1, oct8, one); err == nil || !strings.Contains(err.Error(), short) {
		t.Errorf("with one of her own, her Also in: dailyPuzzleOf says %v, want it refused", err)
	}
	exec(`UPDATE `+Live+`.ratings SET num_votes = 1 WHERE tconst = $1`, own)
	agree("one of her own, and not her Also in", true)
	for _, table := range []string{"principals", "ratings", "titles"} {
		exec(`DELETE FROM `+Live+`.`+table+` WHERE tconst = $1`, own)
	}
	exec(`INSERT INTO `+Live+`.principals (tconst, ordering, nconst, category) VALUES ($1, 6, 'nm9900007', 'actor')`, last)
	agree("the sixth-billed back", true)

	// Hers, by votes: the nine other candidates, their crowd and her
	// eight fillers, raised past everything below, then the extras, then
	// the answer's crowd, then the answer. Before the extras that is
	// 9 + 9×MinCrowd + 8 above them and MinCrowd + 1 below, so the extras
	// that fill her sheet to MaxSheet number MaxSheet − 18 − 10×MinCrowd.
	// The votes are set from MinVotes: the raised ten times it, the
	// extras about twice, the answer's crowd just over it and the answer
	// at it, so the order holds however MinVotes is tuned.
	exec(`UPDATE `+Live+`.ratings SET num_votes = num_votes + $1::int WHERE tconst LIKE 'tt99002%' OR tconst LIKE 'tt9904%'`, 10*daily.MinVotes)
	exec(`UPDATE `+Live+`.ratings SET num_votes = $1::int + 1 WHERE tconst LIKE 'tt990401%'`, daily.MinVotes)
	exec(`UPDATE `+Live+`.ratings SET num_votes = $1::int WHERE tconst = 'tt9900101'`, daily.MinVotes)
	one.Votes = daily.MinVotes
	extras := func(from, to int) {
		t.Helper()
		exec(`INSERT INTO `+Live+`.titles (tconst, primary_title, original_title, is_adult, start_year, genres)
		      SELECT 'tt97' || lpad(g::text, 5, '0'), 'Extra ' || g, 'Extra ' || g, false, 1950, ARRAY['Drama']
		      FROM generate_series($1::int, $2::int) g`, from, to)
		exec(`INSERT INTO `+Live+`.ratings (tconst, average_rating, num_votes)
		      SELECT 'tt97' || lpad(g::text, 5, '0'), 6.0, $3::int + g FROM generate_series($1::int, $2::int) g`, from, to, 2*daily.MinVotes)
		exec(`INSERT INTO `+Live+`.principals (tconst, ordering, nconst, category)
		      SELECT 'tt97' || lpad(g::text, 5, '0'), 1, 'nm9900002', 'actress' FROM generate_series($1::int, $2::int) g`, from, to)
	}
	fill := daily.MaxSheet - 18 - 10*daily.MinCrowd
	extras(1, fill)
	agree("her crowd the last on her sheet", true)
	extras(fill+1, fill+1)
	agree("the last of her crowd past her cap", false)
}

// TestAPosterWithNoColourIsColouredFromThePicture as the colour job
// would, and the colour kept for it; a candidate whose poster cannot be
// read is passed over, and nothing the pass says names it.
func TestAPosterWithNoColourIsColouredFromThePicture(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	var png bytes.Buffer
	if err := imagepng.Encode(&png, flat(color.RGBA{0x30, 0x50, 0xa0, 0xff})); err != nil {
		t.Fatal(err)
	}
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path != "/tt9900107.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png.Bytes())
	}))
	t.Cleanup(srv.Close)
	if _, err := s.pool.Exec(ctx, `UPDATE meta.posters SET colour = NULL, poster_url = $1 || '/' || tconst || '.png'
		WHERE tconst LIKE 'tt99001%'`, srv.URL); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	now := oct8
	job := &DailyJob{Store: s, Client: srv.Client(), Days: 1, Now: func() time.Time { return now },
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1))
	want := averageColour(flat(color.RGBA{0x30, 0x50, 0xa0, 0xff}))
	if err != nil || p.Answer.ID != "tt9900107" || p.Answer.Colour != want {
		t.Fatalf("the one candidate with a poster to read: %+v, %v; want %s", p, err, want)
	}
	var kept string
	if err := s.pool.QueryRow(ctx, `SELECT colour FROM meta.posters WHERE tconst = 'tt9900107'`).Scan(&kept); err != nil || kept != want {
		t.Errorf("the colour kept is %q, %v", kept, err)
	}
	if asked.Load() < 2 {
		t.Errorf("the poster host was asked %d times", asked.Load())
	}
	if strings.Contains(logs.String(), "tt99001") || strings.Contains(logs.String(), srv.URL) {
		t.Errorf("the logs name a candidate:\n%s", logs.String())
	}
}

// TestAPosterIsFetchedOnlyForACandidateThatWillMakeAPuzzle: the colour
// is asked after every other rule (daily.Fit). Candidate 4, the only one
// with a runtime, its poster not yet coloured, with a newcomer as its
// sixth-billed whose three movies are all in its crowd, is crowded but
// short of daily.MinSheet, so the day has no answer and the poster host
// is never asked. With a fourth movie of the newcomer's it is the answer,
// coloured from its poster.
func TestAPosterIsFetchedOnlyForACandidateThatWillMakeAPuzzle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	var png bytes.Buffer
	if err := imagepng.Encode(&png, flat(color.RGBA{0x30, 0x50, 0xa0, 0xff})); err != nil {
		t.Fatal(err)
	}
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(png.Bytes())
	}))
	t.Cleanup(srv.Close)
	newcomer := func(from, to int) {
		t.Helper()
		for _, stmt := range []string{
			`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres, runtime_minutes)
			 SELECT 'tt99003' || lpad(g::text, 2, '0'), 'Newcomer ' || g, 'Newcomer ' || g, false, 1990 + g, ARRAY['Drama'], 90
			 FROM generate_series($1::int, $2::int) g`,
			`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
			 SELECT 'tt99003' || lpad(g::text, 2, '0'), 7.5, 1000 FROM generate_series($1::int, $2::int) g`,
			`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
			 SELECT 'tt99003' || lpad(g::text, 2, '0'), 1, 'nm9900009', 'actor', NULL FROM generate_series($1::int, $2::int) g`,
		} {
			if _, err := s.pool.Exec(ctx, stmt, from, to); err != nil {
				t.Fatalf("%v\n%s", err, stmt)
			}
		}
	}
	for _, stmt := range []string{
		`UPDATE ` + Live + `.titles SET runtime_minutes = NULL WHERE tconst LIKE 'tt99001%' AND tconst <> 'tt9900104'`,
		`INSERT INTO ` + Live + `.names (nconst, primary_name) VALUES ('nm9900009', 'Hal Newcomer')`,
		`UPDATE ` + Live + `.principals SET nconst = 'nm9900009' WHERE tconst = 'tt9900104' AND ordering = 6`,
		`UPDATE meta.posters SET colour = NULL, poster_url = '` + srv.URL + `/tt9900104.png' WHERE tconst = 'tt9900104'`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	newcomer(1, daily.MinCrowd)
	cands, err := s.dailyCandidates(ctx)
	if err != nil || len(cands) != 1 || cands[0].ID != "tt9900104" || cands[0].Colour != "" {
		t.Fatalf("the candidates are %+v, %v; want Candidate 4 alone, uncoloured", cands, err)
	}
	now := oct8
	job := &DailyJob{Store: s, Client: srv.Client(), Days: 1, Now: func() time.Time { return now }, Logger: quietLogger()}
	if err := job.Run(ctx); err == nil || !strings.Contains(err.Error(), "none of 1 candidates can be the daily answer") {
		t.Errorf("with %d movies of the newcomer's: %v, want the day said to have no answer", daily.MinCrowd, err)
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("the poster host was asked %d times for a candidate short of MinSheet", n)
	}
	newcomer(daily.MinCrowd+1, daily.MinSheet)
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	want := averageColour(flat(color.RGBA{0x30, 0x50, 0xa0, 0xff}))
	if p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1)); err != nil || p.Answer.ID != "tt9900104" || p.Answer.Colour != want || asked.Load() == 0 {
		t.Errorf("with %d movies of the newcomer's: %+v, %v, the host asked %d times", daily.MinSheet, p, err, asked.Load())
	}
}

// TestADayWithNoFairAnswerFailsThePass, and says which day: every
// candidate was an answer within ninety days.
func TestADayWithNoFairAnswerFailsThePass(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8
	job := dailyJob(s, &now)
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	now = oct8.AddDate(0, 0, 1)
	err := job.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "2026-10-17") {
		t.Errorf("err = %v, want the new last day said to have no answer", err)
	}
	// The puzzles already picked are untouched, and keep their numbers.
	p, err := s.DailyPuzzle(ctx, now)
	if err != nil || p.No != 3 {
		t.Errorf("tomorrow = %v, %v", p, err)
	}
}

// TestTheFirstPuzzleIsNumberOne: a fresh database's first puzzle is UTC
// yesterday's, since readers west of UTC are still on it, and numbers
// count from it, so a later first pass still numbers from it.
func TestTheFirstPuzzleIsNumberOne(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8
	job := dailyJob(s, &now)
	job.Days = 1
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1)); err != nil || p.No != 1 {
		t.Fatalf("UTC yesterday: %+v, %v; want No. 1", p, err)
	}
	now = oct8.AddDate(0, 0, 3)
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.DailyPuzzle(ctx, now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if p.No != 4 {
		t.Errorf("three days after No. 1 is No. %d", p.No)
	}
}

// TestAMissedFirstDayKeepsItsNumber: a first pass that could not pick
// its first day, but kept the days after it, has no No. 1. The days it
// kept are numbered from the missing day, so the next pass fills that
// day as No. 1 while it is still in the window, and a new day past the
// end takes the next free number rather than one already taken.
func TestAMissedFirstDayKeepsItsNumber(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8
	job := dailyJob(s, &now)
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// What a first pass leaves when UTC yesterday's pick fails.
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.daily_puzzles WHERE no = 1`); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1)); err != nil || p.No != 1 {
		t.Fatalf("the missed first day: %+v, %v; want No. 1 again", p, err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.daily_puzzles WHERE no = 1`); err != nil {
		t.Fatal(err)
	}
	// A day on, the missed day has left the window, and 17 October is
	// the first new day: it is No. 11, counted from the missed day.
	now = oct8.AddDate(0, 0, 1)
	if err := job.Run(ctx); err != nil {
		t.Fatalf("the pass after a missed first day: %v", err)
	}
	if p, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, 9)); err != nil || p.No != 11 {
		t.Errorf("17 October: %+v, %v; want No. 11", p, err)
	}
}

// TestThePickNeverLogsTheAnswer: whoever reads the logs may want to play
// too, so a pass says which days it picked and how many movies their
// sheets hold, at every level, and never what any candidate is.
func TestThePickNeverLogsTheAnswer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	var logs bytes.Buffer
	job := &DailyJob{Store: s, Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now: func() time.Time { return oct8 }}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), fmt.Sprintf(`msg="daily puzzle picked" day=2026-10-07 no=1 movies=%d`, dailySheet)) {
		t.Errorf("the pass did not say what it picked:\n%s", logs.String())
	}
	for _, secret := range []string{"tt99001", "Candidate"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the logs name a candidate (%q):\n%s", secret, logs.String())
		}
	}
}

// TestAFailedPickNamesNoCandidate: the store's errors say which movie
// they were reading, and the error a pass returns, which reaches the
// logs and Telegram, says only that it was a candidate.
func TestAFailedPickNamesNoCandidate(t *testing.T) {
	err := error(unnamed{err: fmt.Errorf("catalog: spine for tt0133093: %w", context.DeadlineExceeded), id: "tt0133093"})
	if strings.Contains(err.Error(), "tt0133093") || err.Error() != "catalog: spine for a candidate: context deadline exceeded" {
		t.Errorf("err = %q", err)
	}
	if !errors.Is(errors.Join(err), context.DeadlineExceeded) {
		t.Error("the error no longer unwraps to its cause")
	}
}
