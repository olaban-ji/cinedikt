package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"cinedikt/internal/daily"
)

// resetDaily empties Cinedikt Daily's tables. The tests share one
// database, so each one that reads them starts from nothing.
func resetDaily(t *testing.T, s *Store) {
	t.Helper()
	for _, stmt := range []string{
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

// The daily fixture's people: one director and four billed actors, on
// every candidate. Their slots are 0 to 4, director first.
var dailyPeople = []string{"nm9900001", "nm9900002", "nm9900003", "nm9900004", "nm9900005"}

// dailyCandidates is how many candidates the fixture has: one for each
// day a full pass keeps picked, UTC yesterday to eight days ahead.
const dailyCandidates = DailyBehind + 1 + DailyAhead

// dailyFixture adds to the published fixture what a day's puzzle needs,
// straight into the live tables: ten candidates in the first-run pool,
// one in each of the opening screen pool's eight eras and a second in
// two of them, each with a poster, all five people on every one of
// them, and half with only TMDb's overview and half with no synopsis at
// all, since an answer needs none; sixty more movies each through one
// of those people, rated and voted from the fewest up, so every
// candidate's map holds 69 rated cards (the other nine candidates as
// close relatives); and one unrated movie that must stay off every
// board.
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
		    ('nm9900004', 'Cy Third'), ('nm9900005', 'Di Fourth')`,
		// The candidates: tt9900101 to tt9900110.
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 'Candidate ' || g, 'Candidate ' || g, false,
		        (ARRAY[1931, 1965, 1984, 1999, 2008, 2015, 2021, 2024, 1997, 2016])[g],
		        CASE WHEN g % 2 = 0 THEN ARRAY['Drama', 'Romance'] ELSE ARRAY['Action', 'Sci-Fi'] END
		 FROM generate_series(1, 10) g`,
		`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 7.0 + g / 10.0, 500000 + g * 1000 FROM generate_series(1, 10) g`,
		`INSERT INTO ` + Live + `.first_run (era, tconst, num_votes)
		 SELECT (ARRAY[1920, 1960, 1980, 1995, 2005, 2013, 2019, 2024, 1995, 2013])[g], 'tt99001' || lpad(g::text, 2, '0'), 500000 + g * 1000
		 FROM generate_series(1, 10) g`,
		`INSERT INTO ` + Live + `.directors (tconst, nconst, ordering)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 'nm9900001', 0 FROM generate_series(1, 10) g`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), a, 'nm990000' || (a + 1), 'actor', 'Hero ' || a
		 FROM generate_series(1, 10) g, generate_series(1, 4) a`,
		`INSERT INTO meta.synopses (tconst, overview, source, fetched_at)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'),
		        'A stranger arrives in town number ' || g || '. Nothing is the same after.', 'tmdb', now()
		 FROM generate_series(1, 10) g WHERE g % 2 = 0`,
		`INSERT INTO meta.posters (tconst, poster_url, status, fetched_at)
		 SELECT 'tt99001' || lpad(g::text, 2, '0'), 'https://img.example/' || g || '.jpg', 'ok', now()
		 FROM generate_series(1, 10) g`,
		// The sixty: tt9900201 to tt9900260, each through one person, the
		// director's through the crew.
		`INSERT INTO ` + Live + `.titles (tconst, primary_title, original_title, is_adult, start_year, genres)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 'Filler ' || g, 'Filler ' || g, false, 1960 + g, ARRAY['Drama']
		 FROM generate_series(1, 60) g`,
		`INSERT INTO ` + Live + `.ratings (tconst, average_rating, num_votes)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 4.0 + (g % 50) / 10.0, 100 + g * 10 FROM generate_series(1, 60) g`,
		`INSERT INTO ` + Live + `.directors (tconst, nconst, ordering)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 'nm9900001', 0 FROM generate_series(1, 60) g WHERE g % 5 = 0`,
		`INSERT INTO ` + Live + `.principals (tconst, ordering, nconst, category, character)
		 SELECT 'tt99002' || lpad(g::text, 2, '0'), 1, 'nm990000' || (g % 5 + 1), 'actor', NULL
		 FROM generate_series(1, 60) g WHERE g % 5 <> 0`,
		// Unrated, through the first actor.
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
		if again.Answer.ID != p.Answer.ID || len(again.Cards) != len(p.Cards) {
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

// TestADailyPuzzleIsTheMapWithTheAnswerHidden: the board is the
// answer's map, rated movies only, the answer off it; the people are in
// slot order with the director first; and the starting cards are the
// least known through three different people.
func TestADailyPuzzleIsTheMapWithTheAnswerHidden(t *testing.T) {
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
	if !strings.HasPrefix(p.Answer.ID, "tt99001") || p.Answer.Rating < 7 || p.Answer.Year == 0 || len(p.Answer.Genres) != 2 {
		t.Errorf("answer = %+v", p.Answer)
	}
	if p.Genre != p.Answer.Genres[0] {
		t.Errorf("genre = %q, want the first of %v", p.Genre, p.Answer.Genres)
	}
	var ids []string
	for _, sl := range p.People {
		ids = append(ids, sl.ID)
	}
	if !slices.Equal(ids, dailyPeople) || p.People[0].Role != daily.RoleDirector || p.People[0].Name != "Dee Rector" {
		t.Errorf("people = %+v", p.People)
	}
	if len(p.Cards) != 69 {
		t.Errorf("%d cards, want 69", len(p.Cards))
	}
	relatives := 0
	for i, c := range p.Cards {
		if c.ID != fmt.Sprintf("c%d", i+1) {
			t.Errorf("card %d has id %s", i, c.ID)
		}
		if c.Film == p.Answer.ID || c.Film == "tt9900299" {
			t.Errorf("%s is on the board", c.Film)
		}
		if c.Relative() {
			relatives++
		}
		if c.Title == "" || c.Votes == 0 || c.Rating == 0 {
			t.Errorf("card %s = %+v", c.ID, c)
		}
	}
	if relatives != 9 {
		t.Errorf("%d close relatives, want the other nine candidates", relatives)
	}
	var films []string
	for _, id := range p.Start {
		for _, c := range p.Cards {
			if c.ID == id {
				films = append(films, c.Film)
			}
		}
	}
	// Fillers 1, 2 and 3 have the fewest votes, through the first three
	// actors.
	if want := []string{"tt9900201", "tt9900202", "tt9900203"}; !slices.Equal(films, want) {
		t.Errorf("starting cards = %v, want %v", films, want)
	}

	if _, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("tomorrow, not picked: %v, want ErrNotFound", err)
	}
	if _, err := s.DailyPuzzleNo(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("No. 99: %v, want ErrNotFound", err)
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
// too, so a pass says which days it picked and how big their boards
// are, at every level, and never what any candidate is.
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
	if !strings.Contains(logs.String(), `msg="daily puzzle picked" day=2026-10-07 no=1 cards=69`) {
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
