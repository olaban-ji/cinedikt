package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"cinedikt/internal/daily"
)

// todaysPuzzle picks UTC yesterday's and today's puzzles from the daily
// fixture, and returns today's, 8 October's.
func todaysPuzzle(t *testing.T, s *Store) *daily.Puzzle {
	t.Helper()
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
	return p
}

func TestAPlayerIsTheirCookieAndTheirNameIsTheirOwn(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	resetDaily(t, s)
	t.Cleanup(func() { resetDaily(t, s) })

	token := daily.TokenHash(daily.NewToken())
	p, err := s.CreateDailyPlayer(ctx, token, "Trinity Kimble", 205)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || p.Name != "Trinity Kimble" || p.Hue != 205 {
		t.Errorf("player = %+v", p)
	}
	got, ok, err := s.DailyPlayer(ctx, token)
	if err != nil || !ok || got != p {
		t.Errorf("by cookie: %+v, %v, %v", got, ok, err)
	}
	if _, ok, err := s.DailyPlayer(ctx, daily.TokenHash(daily.NewToken())); err != nil || ok {
		t.Errorf("a cookie nobody has: %v, %v", ok, err)
	}
	// A second request with the same new cookie gets the player the
	// first made, whatever name it brought.
	again, err := s.CreateDailyPlayer(ctx, token, "Neo Ripley", 7)
	if err != nil || again != p {
		t.Errorf("the same cookie again: %+v, %v", again, err)
	}
	other := daily.TokenHash(daily.NewToken())
	if _, err := s.CreateDailyPlayer(ctx, other, "Trinity Kimble", 9); !errors.Is(err, ErrNameTaken) {
		t.Errorf("a name somebody has: %v, want ErrNameTaken", err)
	}
	second, err := s.CreateDailyPlayer(ctx, other, "Neo Ripley", 9)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenameDailyPlayer(ctx, second.ID, "Trinity Kimble"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("renamed to a name somebody has: %v, want ErrNameTaken", err)
	}
	if err := s.RenameDailyPlayer(ctx, second.ID, "Clarice Gump"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.DailyPlayer(ctx, other); got.Name != "Clarice Gump" {
		t.Errorf("after the rename: %+v", got)
	}
}

// TestAMoveIsRecordedOnceAndInOrder: a game starts with every point and
// the clock running; a move is checked and recorded; the same key again
// is the same game, not a second charge; a move made from an old point
// is stale and comes back with the game; and the right guess ends it,
// with its time on the row the boards read.
func TestAMoveIsRecordedOnceAndInOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := todaysPuzzle(t, s)
	player, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Trinity Kimble", 205)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "before-play", Kind: daily.KindReveal}, oct8); !errors.Is(err, daily.ErrNoGame) {
		t.Errorf("a move before Play: %v, want no-game", err)
	}
	if rec, err := s.DailyGame(ctx, player.ID, p.No); err != nil || rec != nil {
		t.Errorf("before Play: %+v, %v", rec, err)
	}
	started := oct8.Add(time.Minute)
	rec, err := s.StartDailyGame(ctx, player.ID, p.No, started, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Started.Equal(started) || len(rec.Moves) != 0 || rec.Finished != nil || rec.Zone != "UTC" {
		t.Errorf("a new game = %+v", rec)
	}
	if again, err := s.StartDailyGame(ctx, player.ID, p.No, started.Add(time.Hour), daily.Zone("Asia/Tokyo")); err != nil ||
		!again.Started.Equal(started) || again.Zone != "UTC" {
		t.Errorf("Play twice restarted the clock or moved the zone: %+v, %v", again, err)
	}

	card := p.Cards[0]
	for _, c := range p.Cards {
		if !slices.Contains(p.Start, c.ID) && !c.Relative() {
			card = c
			break
		}
	}
	flip := daily.Request{Key: "flip-0001", Seq: 0, Kind: daily.KindFlip, Arg: card.ID}
	rec, err = s.DailyAct(ctx, player.ID, p, flip, started.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Moves) != 1 || rec.Moves[0].Seq != 1 || rec.Moves[0].Cost != card.Cost() {
		t.Errorf("after a flip: %+v", rec.Moves)
	}
	// The same request again, a retry after a dropped answer.
	rec, err = s.DailyAct(ctx, player.ID, p, flip, started.Add(20*time.Second))
	if err != nil || len(rec.Moves) != 1 {
		t.Errorf("a retry: %d moves, %v", len(rec.Moves), err)
	}
	var pts, moves int
	if err := s.pool.QueryRow(ctx, `SELECT pts, moves FROM meta.daily_games WHERE player = $1`, player.ID).Scan(&pts, &moves); err != nil {
		t.Fatal(err)
	}
	if pts != daily.Start-card.Cost() || moves != 1 {
		t.Errorf("the row says %d points after %d moves, want %d after 1", pts, moves, daily.Start-card.Cost())
	}
	// Another tab, still at seq 0.
	rec, err = s.DailyAct(ctx, player.ID, p, daily.Request{Key: "other-tab", Seq: 0, Kind: daily.KindGenres}, started.Add(30*time.Second))
	if !errors.Is(err, daily.ErrStale) || rec == nil || len(rec.Moves) != 1 {
		t.Errorf("a stale move: %v, with %+v", err, rec)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "genres-01", Seq: 1, Kind: daily.KindGenres}, started.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "genres-02", Seq: 2, Kind: daily.KindGenres}, started.Add(45*time.Second)); !errors.Is(err, daily.ErrKnown) {
		t.Errorf("genres twice: %v, want known", err)
	}

	won := started.Add(3*time.Minute + 21*time.Second + 400*time.Millisecond)
	rec, err = s.DailyAct(ctx, player.ID, p, daily.Request{Key: "the-answer", Seq: 2, Kind: daily.KindGuess, Arg: p.Answer.ID}, won)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Finished == nil || !rec.Finished.Equal(won) || len(rec.Moves) != 3 {
		t.Errorf("after the right guess: %+v", rec)
	}
	var finished *time.Time
	var isWon, gaveUp bool
	var ms *int
	if err := s.pool.QueryRow(ctx, `SELECT pts, moves, finished_at, won, gave_up, ms FROM meta.daily_games WHERE player = $1`, player.ID).
		Scan(&pts, &moves, &finished, &isWon, &gaveUp, &ms); err != nil {
		t.Fatal(err)
	}
	if pts != daily.Start-card.Cost()-daily.GenresCost || moves != 3 || finished == nil || !isWon || gaveUp || ms == nil || *ms != 201400 {
		t.Errorf("the finished row: %d points, %d moves, finished %v, won %v, gave up %v, ms %v", pts, moves, finished, isWon, gaveUp, ms)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "too-late", Seq: 3, Kind: daily.KindReveal}, won); !errors.Is(err, daily.ErrDone) {
		t.Errorf("a move after the end: %v, want done", err)
	}
	// Read back cold, the game replays to the same end.
	cold, err := s.DailyGame(ctx, player.ID, p.No)
	if err != nil {
		t.Fatal(err)
	}
	if state := daily.Replay(p, cold.Moves); !state.Won || state.Pts != pts {
		t.Errorf("replayed from the database: %+v", state)
	}
}

// TestAGameEndsAtMidnightInTheZoneItWasStartedIn: 8 October's puzzle,
// started in Tokyo, can be played until midnight there, 15:00 UTC, and
// not a moment after, though it is still the 8th in Los Angeles for
// sixteen hours more; a retry of a move made in time is still answered.
// A game started in Los Angeles then plays on. A game kept before games
// had zones is on UTC's day.
func TestAGameEndsAtMidnightInTheZoneItWasStartedIn(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := todaysPuzzle(t, s)
	tokyo, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Trinity Kimble", 205)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.StartDailyGame(ctx, tokyo.ID, p.No, oct8, daily.Zone("Asia/Tokyo"))
	if err != nil || rec.Zone != "Asia/Tokyo" {
		t.Fatalf("started in Tokyo: %+v, %v", rec, err)
	}
	midnight := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	if _, err := s.DailyAct(ctx, tokyo.ID, p, daily.Request{Key: "in-time-1", Seq: 0, Kind: daily.KindGenres}, midnight.Add(-time.Second)); err != nil {
		t.Fatalf("a second before midnight in Tokyo: %v", err)
	}
	if _, err := s.DailyAct(ctx, tokyo.ID, p, daily.Request{Key: "too-late", Seq: 1, Kind: daily.KindReveal}, midnight); !errors.Is(err, daily.ErrDay) {
		t.Errorf("at midnight in Tokyo: %v, want day", err)
	}
	if rec, err := s.DailyAct(ctx, tokyo.ID, p, daily.Request{Key: "in-time-1", Seq: 0, Kind: daily.KindGenres}, midnight.Add(time.Hour)); err != nil || len(rec.Moves) != 1 {
		t.Errorf("a retry past midnight: %+v, %v", rec, err)
	}
	var moves int
	var finished *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT moves, finished_at FROM meta.daily_games WHERE player = $1`, tokyo.ID).Scan(&moves, &finished); err != nil {
		t.Fatal(err)
	}
	if moves != 1 || finished != nil {
		t.Errorf("after the refusal the row says %d moves, finished %v", moves, finished)
	}

	la, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Neo Ripley", 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartDailyGame(ctx, la.ID, p.No, midnight, daily.Zone("America/Los_Angeles")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DailyAct(ctx, la.ID, p, daily.Request{Key: "la-reveal", Seq: 0, Kind: daily.KindReveal}, midnight.Add(time.Hour)); err != nil {
		t.Errorf("08:00 on the 8th in Los Angeles: %v", err)
	}

	old, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Clarice Gump", 9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO meta.daily_games (player, no, pts, started_at) VALUES ($1, $2, 1000, $3)`,
		old.ID, p.No, oct8); err != nil {
		t.Fatal(err)
	}
	if rec, err := s.DailyGame(ctx, old.ID, p.No); err != nil || rec.Zone != "UTC" {
		t.Errorf("a game kept without a zone: %+v, %v", rec, err)
	}
}

// TestAGuessIsJudgedOnTheLiveCatalog: who it shares with the answer is
// read from its credits when it is made, kept with the move, and a movie
// the catalog does not have is unknown.
func TestAGuessIsJudgedOnTheLiveCatalog(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := todaysPuzzle(t, s)
	player, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Trinity Kimble", 205)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartDailyGame(ctx, player.ID, p.No, oct8, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "nothing-01", Seq: 0, Kind: daily.KindGuess, Arg: "tt9999999"}, oct8); !errors.Is(err, daily.ErrUnknown) {
		t.Errorf("a movie the catalog lacks: %v, want unknown", err)
	}
	// Filler 7 is through the second actor (slot 2), from 1967, rated
	// 4.7: older and lower than any candidate.
	rec, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "filler-07", Seq: 0, Kind: daily.KindGuess, Arg: "tt9900207"}, oct8.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	g := rec.Moves[0].Guess
	if g == nil || g.Title != "Filler 7" || g.Year != 1967 || g.Rating == nil || *g.Rating != 4.7 ||
		!slices.Equal(g.Shared, []int{2}) || g.YearHint != "newer" || g.RatingHint != "higher" {
		t.Errorf("the guess learned %+v", g)
	}
	if rec.Moves[0].Cost != daily.WrongCost {
		t.Errorf("cost %d", rec.Moves[0].Cost)
	}
	// Another candidate shares all five people.
	other := "tt9900101"
	if other == p.Answer.ID {
		other = "tt9900102"
	}
	rec, err = s.DailyAct(ctx, player.ID, p, daily.Request{Key: "candidate", Seq: 1, Kind: daily.KindGuess, Arg: other}, oct8.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if g := rec.Moves[1].Guess; !slices.Equal(g.Shared, []int{0, 1, 2, 3, 4}) || rec.Moves[1].Cost != 150 {
		t.Errorf("guessing a relative learned %+v for %d", g, rec.Moves[1].Cost)
	}
	cold, err := s.DailyGame(ctx, player.ID, p.No)
	if err != nil {
		t.Fatal(err)
	}
	if g := cold.Moves[0].Guess; g == nil || g.Title != "Filler 7" || !slices.Equal(g.Shared, []int{2}) {
		t.Errorf("read back, the guess says %+v", g)
	}
	if state := daily.Replay(p, cold.Moves); len(state.Known()) != 5 || state.Pts != daily.Start-250 {
		t.Errorf("replayed: %+v", state)
	}
}

// TestPostersAndPhotosAreReadLive, and a photo older than TMDb's 175
// days is not shown.
func TestPostersAndPhotosAreReadLive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.people (nconst, tmdb_id, profile_path, asked_at) VALUES
		    ('nm9900002', 1, '/ava.jpg', now() - interval '3 days'),
		    ('nm9900003', 2, '/bo.jpg', now() - interval '200 days'),
		    ('nm9900004', 3, NULL, now())`); err != nil {
		t.Fatal(err)
	}
	live, err := s.DailyLive(ctx, []string{"tt9900101", "tt9900201", "tt0000404"}, []string{"nm9900002", "nm9900003", "nm9900004", "nm9900005"})
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Posters) != 1 || live.Posters["tt9900101"] != "https://img.example/1.jpg" {
		t.Errorf("posters = %v", live.Posters)
	}
	if len(live.Photos) != 1 || live.Photos["nm9900002"] != "https://image.tmdb.org/t/p/w185/ava.jpg" {
		t.Errorf("photos = %v", live.Photos)
	}
}

// TestNamesComeFromTheCharactersOfWellKnownMovies, checked against the
// people who played them: the fixture's Matrix and Shawshank are well
// known, their writer is not an actor or director, and television never
// reached the catalog.
func TestNamesComeFromTheCharactersOfWellKnownMovies(t *testing.T) {
	s := testStore(t)
	publishFixture(t, s)
	credits, err := s.DailyNames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(credits.Characters)
	want := []string{"Andy Dufresne", "Ellis Boyd 'Red' Redding", "Morpheus", "Neo", "Trinity"}
	if !slices.Equal(credits.Characters, want) {
		t.Errorf("characters = %q, want %q", credits.Characters, want)
	}
	slices.Sort(credits.People)
	want = []string{"Carrie-Anne Moss", "Keanu Reeves", "Laurence Fishburne", "Morgan Freeman", "Tim Robbins"}
	if !slices.Equal(credits.People, want) {
		t.Errorf("people = %q, want %q", credits.People, want)
	}
}

// boardFixture is four puzzles, Monday 5 to Thursday 8 October, and
// players whose games are written straight into the rows the boards
// read, each finished game with points and a time.
func boardFixture(t *testing.T, s *Store) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	resetDaily(t, s)
	t.Cleanup(func() { resetDaily(t, s) })
	for no := 1; no <= 4; no++ {
		p := &daily.Puzzle{No: no, Day: time.Date(2026, 10, 4+no, 0, 0, 0, 0, time.UTC),
			Answer: daily.Answer{ID: fmt.Sprintf("tt000000%d", no), Title: "Answer", Year: 1999, Rating: 8},
			People: []daily.Slot{}, Cards: []daily.Card{}, Start: []string{}}
		if kept, err := s.putDailyPuzzle(ctx, p); err != nil || !kept {
			t.Fatalf("puzzle %d: %v, %v", no, kept, err)
		}
	}
	players := map[string]int64{}
	for i, name := range []string{"Ava", "Bo", "Cy", "Di", "Ed", "Flo", "Gus", "New"} {
		p, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), name+" Player", 10*i)
		if err != nil {
			t.Fatal(err)
		}
		players[name] = p.ID
	}
	game := func(name string, no, pts int, ms int, won bool) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at, won, ms)
			VALUES ($1, $2, $3, 1, now(), now(), $4, $5)`, players[name], no, pts, won, ms); err != nil {
			t.Fatal(err)
		}
	}
	// Everyone but New has finished the three earlier puzzles.
	for _, name := range []string{"Ava", "Bo", "Cy", "Di", "Ed", "Flo", "Gus"} {
		for no := 1; no <= 3; no++ {
			game(name, no, 500, 60000, true)
		}
	}
	// Today: Ava and Bo tie on points, Ava faster; Gus missed it; New is
	// on their first game; Flo has started and not finished.
	game("Ava", 4, 900, 100000, true)
	game("Bo", 4, 900, 120000, true)
	game("Cy", 4, 700, 90000, true)
	game("Di", 4, 650, 90000, true)
	game("Ed", 4, 600, 90000, true)
	game("Gus", 4, 0, 30000, false)
	game("New", 4, 800, 50000, true)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.daily_games (player, no, pts, started_at) VALUES ($1, 4, 1000, now())`, players["Flo"]); err != nil {
		t.Fatal(err)
	}
	return players
}

func boardNames(b daily.Board) []string {
	var out []string
	for _, r := range b.Rows {
		if r.Gap {
			out = append(out, "···")
			continue
		}
		out = append(out, fmt.Sprintf("%d %s", r.Rank, r.Name))
	}
	return out
}

// TestTodaysBoardRanksFinishedGamesByPointsThenTime: a player on their
// first game is ranked where they would sit and told they are not on the
// board yet, but nobody else sees them; a game not finished is on no
// board.
func TestTodaysBoardRanksFinishedGamesByPointsThenTime(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	players := boardFixture(t, s)
	p, err := s.DailyPuzzleNo(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}

	anyone, err := s.DailyBoard(ctx, p, daily.TabToday, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1 Ava Player", "2 Bo Player", "3 Cy Player", "4 Di Player", "5 Ed Player"}
	if got := boardNames(anyone); !slices.Equal(got, want) || anyone.Total != 6 || anyone.You != nil || anyone.Beat != nil {
		t.Errorf("without a player: %v, total %d, you %+v", got, anyone.Total, anyone.You)
	}
	// Six of seven finished games were won.
	if anyone.Solved == nil || *anyone.Solved != 86 {
		t.Errorf("solved = %v, want 86", anyone.Solved)
	}
	if anyone.Rows[0].Secs == nil || *anyone.Rows[0].Secs != 100 || anyone.Rows[0].Days != nil {
		t.Errorf("Ava's row = %+v", anyone.Rows[0])
	}

	fresh, err := s.DailyBoard(ctx, p, daily.TabToday, players["New"])
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"1 Ava Player", "2 Bo Player", "3 New Player", "4 Cy Player", "5 Di Player"}
	if got := boardNames(fresh); !slices.Equal(got, want) || fresh.Total != 7 {
		t.Errorf("New's board: %v, total %d", got, fresh.Total)
	}
	if y := fresh.You; y == nil || y.Rank != 3 || y.Pts != 800 || y.Secs != 50 || y.Listed {
		t.Errorf("New = %+v", fresh.You)
	}
	// Four of the seven finished games scored less than New's 800.
	if fresh.Beat == nil || *fresh.Beat != 57 {
		t.Errorf("beat = %v, want 57", fresh.Beat)
	}

	gus, err := s.DailyBoard(ctx, p, daily.TabToday, players["Gus"])
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"1 Ava Player", "2 Bo Player", "3 Cy Player", "4 Di Player", "5 Ed Player", "6 Gus Player"}
	if got := boardNames(gus); !slices.Equal(got, want) || gus.You == nil || !gus.You.Listed || !gus.Rows[5].You {
		t.Errorf("Gus's board: %v, you %+v", got, gus.You)
	}
	if gus.Beat == nil || *gus.Beat != 0 {
		t.Errorf("Gus beat = %v", gus.Beat)
	}

	flo, err := s.DailyBoard(ctx, p, daily.TabToday, players["Flo"])
	if err != nil {
		t.Fatal(err)
	}
	if flo.You != nil || flo.Beat != nil || flo.Total != 6 {
		t.Errorf("a game in play: you %+v, beat %v, total %d", flo.You, flo.Beat, flo.Total)
	}
}

// TestTheWeeksBoardAddsUpTheDaysSoFar: each listed player's points over
// Monday to this puzzle's day, with each day's points, then their time.
func TestTheWeeksBoardAddsUpTheDaysSoFar(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	players := boardFixture(t, s)
	p, err := s.DailyPuzzleNo(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	week, err := s.DailyBoard(ctx, p, daily.TabWeek, players["New"])
	if err != nil {
		t.Fatal(err)
	}
	// Ava and Bo have 2,400, Ava faster; Cy 2,200, Di 2,150, Ed 2,100;
	// Flo and Gus 1,500, Flo faster because her open game counts
	// nothing, time included; and New 800.
	want := []string{"1 Ava Player", "2 Bo Player", "3 Cy Player", "4 Di Player", "5 Ed Player", "6 Flo Player", "7 Gus Player", "8 New Player"}
	if got := boardNames(week); !slices.Equal(got, want) || week.Total != 8 {
		t.Errorf("week = %v, total %d", got, week.Total)
	}
	ava := week.Rows[0]
	if ava.Pts != 2400 || ava.Secs != nil || len(ava.Days) != 7 {
		t.Fatalf("Ava's week = %+v", ava)
	}
	for i, want := range []int{500, 500, 500, 900} {
		if ava.Days[i] == nil || *ava.Days[i] != want {
			t.Errorf("Ava's day %d = %v, want %d", i, ava.Days[i], want)
		}
	}
	for i := 4; i < 7; i++ {
		if ava.Days[i] != nil {
			t.Errorf("Ava's day %d = %d, a day not played yet", i, *ava.Days[i])
		}
	}
	if y := week.You; y == nil || y.Rank != 8 || y.Pts != 800 || y.Listed || y.Days[3] == nil || y.Days[0] != nil {
		t.Errorf("New = %+v", week.You)
	}
	if week.Beat != nil || week.Solved != nil {
		t.Errorf("the week's tab has beat %v and solved %v", week.Beat, week.Solved)
	}
}

// TestTheWeekIsMadeOfPuzzleDays, never of when games were played: a game
// counts in its puzzle's ISO week wherever and whenever it was finished,
// and a game of the next Monday's puzzle, finished on Sunday by the
// clock in UTC as a reader in Tokyo would, starts the next week.
func TestTheWeekIsMadeOfPuzzleDays(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	players := boardFixture(t, s)
	monday := &daily.Puzzle{No: 8, Day: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Answer: daily.Answer{ID: "tt0000008", Title: "Answer", Year: 1999, Rating: 8},
		People: []daily.Slot{}, Cards: []daily.Card{}, Start: []string{}}
	if kept, err := s.putDailyPuzzle(ctx, monday); err != nil || !kept {
		t.Fatalf("Monday's puzzle: %v, %v", kept, err)
	}
	for _, c := range []struct {
		sql    string
		player int64
	}{
		// Ava's Thursday game, stamped the next Monday; Bo's first game,
		// stamped the Sunday before the week began.
		{`UPDATE meta.daily_games SET started_at = '2026-10-12T03:00Z', finished_at = '2026-10-12T03:05Z' WHERE player = $1 AND no = 4`, players["Ava"]},
		{`UPDATE meta.daily_games SET started_at = '2026-10-04T20:00Z', finished_at = '2026-10-04T20:05Z' WHERE player = $1 AND no = 1`, players["Bo"]},
		// Cy's game of Monday's puzzle, finished at 23:30 UTC on Sunday.
		{`INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at, won, ms, zone)
		  VALUES ($1, 8, 990, 1, '2026-10-11T23:00Z', '2026-10-11T23:30Z', true, 1800000, 'Asia/Tokyo')`, players["Cy"]},
	} {
		if _, err := s.pool.Exec(ctx, c.sql, c.player); err != nil {
			t.Fatalf("%v\n%s", err, c.sql)
		}
	}
	thursday, err := s.DailyPuzzleNo(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	week, err := s.DailyBoard(ctx, thursday, daily.TabWeek, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1 Ava Player", "2 Bo Player", "3 Cy Player", "4 Di Player", "5 Ed Player"}
	if got := boardNames(week); !slices.Equal(got, want) || week.Rows[0].Pts != 2400 || week.Rows[1].Pts != 2400 || week.Rows[2].Pts != 2200 {
		t.Errorf("Thursday's week = %v, %+v", got, week.Rows)
	}
	if ava := week.Rows[0]; ava.Days[3] == nil || *ava.Days[3] != 900 {
		t.Errorf("Ava's Thursday = %v", ava.Days[3])
	}
	if bo := week.Rows[1]; bo.Days[0] == nil || *bo.Days[0] != 500 {
		t.Errorf("Bo's Monday = %v", bo.Days[0])
	}
	next, err := s.DailyBoard(ctx, monday, daily.TabWeek, players["Cy"])
	if err != nil {
		t.Fatal(err)
	}
	if got := boardNames(next); !slices.Equal(got, []string{"1 Cy Player"}) || next.You == nil || next.You.Pts != 990 ||
		next.You.Days[0] == nil || *next.You.Days[0] != 990 {
		t.Errorf("the next week = %v, you %+v", got, next.You)
	}
}

// TestTheStreakCountsConsecutivePuzzlesThatScored, and reads it as the
// page needs it before and after today's game.
func TestTheStreakCountsConsecutivePuzzlesThatScored(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	players := boardFixture(t, s)
	for name, want := range map[string]daily.Streak{
		"Ava": {Now: 4},
		"Gus": {Before: 3},
		"Flo": {Before: 3},
		"New": {Now: 1},
	} {
		got, err := s.DailyStreak(ctx, players[name], 4)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
	if got, err := s.DailyStreak(ctx, players["Ava"], 3); err != nil || got != (daily.Streak{Now: 3}) {
		t.Errorf("Ava on No. 3: %+v, %v", got, err)
	}
	played, err := s.DailyPlayed(ctx, 4)
	if err != nil || played != 8 {
		t.Errorf("played = %d, %v; want every game started today, finished or not", played, err)
	}
}

// TestTodaysCountIsReadFromAnIndex: the banner asks how many have played
// on every visit, and the count covers unfinished games too, so it needs
// an index of every game that leads with the puzzle, not the board's
// partial one or the unique one that leads with the player; without it
// each visit reads the whole history of games.
func TestTodaysCountIsReadFromAnIndex(t *testing.T) {
	s := testStore(t)
	var n int
	if err := s.pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace ns ON ns.oid = t.relnamespace
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE ns.nspname = 'meta' AND t.relname = 'daily_games' AND a.attname = 'no' AND i.indpred IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("meta.daily_games has no index of every game that leads with no")
	}
}

// TestTheYearIsKeptAsAMove: bought once for 200, on the row the boards
// read, and replayed cold to its entry with the answer's year; "story"
// is no move.
func TestTheYearIsKeptAsAMove(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := todaysPuzzle(t, s)
	player, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Trinity Kimble", 205)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartDailyGame(ctx, player.ID, p.No, oct8, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "the-story", Seq: 0, Kind: "story"}, oct8); !errors.Is(err, daily.ErrBad) {
		t.Errorf("buying the story: %v, want bad", err)
	}
	rec, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "the-year", Seq: 0, Kind: daily.KindYear}, oct8.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if m := rec.Moves[0]; m.Kind != daily.KindYear || m.Cost != daily.YearCost || m.Arg != "" {
		t.Errorf("the year was recorded as %+v", m)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "year-again", Seq: 1, Kind: daily.KindYear}, oct8.Add(2*time.Minute)); !errors.Is(err, daily.ErrKnown) {
		t.Errorf("the year twice: %v, want known", err)
	}
	var pts int
	if err := s.pool.QueryRow(ctx, `SELECT pts FROM meta.daily_games WHERE player = $1`, player.ID).Scan(&pts); err != nil {
		t.Fatal(err)
	}
	if pts != daily.Start-200 {
		t.Errorf("the row says %d points", pts)
	}
	cold, err := s.DailyGame(ctx, player.ID, p.No)
	if err != nil {
		t.Fatal(err)
	}
	game := daily.Render(p, cold, daily.Live{})
	if last := game.Log[len(game.Log)-1]; last.Type != daily.EntryYear || last.Year != p.Answer.Year || last.Cost != 200 || game.Pts != 800 {
		t.Errorf("read back, the year says %+v with %d points", last, game.Pts)
	}
}

// dailyShape is what meta.sql must leave the daily tables as, read from
// the catalog: whether the puzzles still have an opening column, and
// what the moves' kind check says.
func dailyShape(t *testing.T, s *Store) (opening bool, check string) {
	t.Helper()
	ctx := context.Background()
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_schema = 'meta' AND table_name = 'daily_puzzles' AND column_name = 'opening')`).Scan(&opening); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = 'meta.daily_moves'::regclass AND conname = 'daily_moves_kind_check'`).Scan(&check); err != nil {
		t.Fatal(err)
	}
	return opening, check
}

// TestMetaBringsTheFirstDailyTablesToTheYear: applied twice to a
// database with no daily tables, and twice to one whose tables were
// made before the year replaced "How it starts", with the opening
// column and a check that knows "story" and not "year", meta.sql leaves
// both the same: no opening, and a check that takes a year and refuses
// a story. A puzzle kept with an opening is still read.
func TestMetaBringsTheFirstDailyTablesToTheYear(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	exec := func(stmts ...string) {
		t.Helper()
		for _, stmt := range stmts {
			if _, err := s.pool.Exec(ctx, stmt); err != nil {
				t.Fatalf("%v\n%s", err, stmt)
			}
		}
	}
	want := func(when string) {
		t.Helper()
		opening, check := dailyShape(t, s)
		if opening || !strings.Contains(check, "'year'") || strings.Contains(check, "'story'") {
			t.Errorf("%s: opening column %v, kind check %s", when, opening, check)
		}
	}

	exec(`DROP TABLE meta.daily_moves, meta.daily_games, meta.daily_players, meta.daily_puzzles`)
	t.Cleanup(func() { resetDaily(t, s) })
	exec(metaSQL, metaSQL)
	want("on a fresh database")

	exec(
		`ALTER TABLE meta.daily_puzzles ADD COLUMN opening text NOT NULL`,
		`ALTER TABLE meta.daily_moves DROP CONSTRAINT daily_moves_kind_check`,
		`ALTER TABLE meta.daily_moves ADD CONSTRAINT daily_moves_kind_check
		     CHECK (kind IN ('flip', 'director', 'actor', 'genres', 'story', 'guess', 'reveal'))`,
		`INSERT INTO meta.daily_puzzles (no, day, answer, title, year, rating, md, people, genres, opening, cards, start, era, genre)
		 VALUES (1, '2026-10-08', 'tt0133093', 'The Matrix', 1999, 8.7, 331, '[]', '{Action}', 'Neo is the one.', '[]', '{}', 1995, 'Action')`,
	)
	if opening, _ := dailyShape(t, s); !opening {
		t.Fatal("the set-up did not put the opening column back")
	}
	exec(metaSQL, metaSQL)
	want("over the first tables")
	p, err := s.DailyPuzzleNo(ctx, 1)
	if err != nil || p.Answer.Year != 1999 {
		t.Errorf("the puzzle kept with an opening: %+v, %v", p, err)
	}

	player, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Trinity Kimble", 205)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartDailyGame(ctx, player.ID, 1, oct8, time.UTC); err != nil {
		t.Fatal(err)
	}
	move := `INSERT INTO meta.daily_moves (game, seq, key, kind, cost, at)
	         SELECT id, $2, $3, $4, 200, now() FROM meta.daily_games WHERE player = $1`
	if _, err := s.pool.Exec(ctx, move, player.ID, 1, "the-year", daily.KindYear); err != nil {
		t.Errorf("a year move: %v", err)
	}
	var pgErr *pgconn.PgError
	if _, err := s.pool.Exec(ctx, move, player.ID, 2, "the-story", "story"); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("a story move: %v, want the check to refuse it", err)
	}
}

// TestTheStandingIsYourPlaceOnTheWeeksBoard, the one the result's This
// week tab shows, rank and size alike: before today's game over the days
// before today, after it through today, ties falling as the tab breaks
// them, a player not yet listed placed and counted on their own board
// only, and nothing for a week with no points in it, a Monday before
// playing among them.
func TestTheStandingIsYourPlaceOnTheWeeksBoard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	players := boardFixture(t, s)
	thursday, err := s.DailyPuzzleNo(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	monday, err := s.DailyPuzzleNo(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Zed's only game is today's, given up: on the week's board, with
	// nothing.
	zed, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), "Zed Player", 300)
	if err != nil {
		t.Fatal(err)
	}
	players["Zed"] = zed.ID
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at, gave_up, ms)
		VALUES ($1, 4, 0, 1, now(), now(), true, 20000)`, zed.ID); err != nil {
		t.Fatal(err)
	}
	standing := func(p *daily.Puzzle, name string, finished bool) *daily.Week {
		t.Helper()
		w, err := s.DailyStanding(ctx, p, players[name], finished)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	same := func(got, want *daily.Week) bool {
		return (got == nil && want == nil) || (got != nil && want != nil && *got == *want)
	}

	// After today's game, through Thursday. Ava and Bo tie on 2,400 and
	// Ava was faster; Gus scored nothing today and is last of the seven
	// listed; New, on their first game, is placed eighth on a board of
	// eight that only they see, as the tab places them.
	for name, want := range map[string]*daily.Week{
		"Ava": {Rank: 1, Players: 7},
		"Bo":  {Rank: 2, Players: 7},
		"Gus": {Rank: 7, Players: 7},
		"New": {Rank: 8, Players: 8},
		"Zed": nil,
	} {
		got := standing(thursday, name, true)
		if !same(got, want) {
			t.Errorf("%s after today's game: %+v, want %+v", name, got, want)
		}
		tab, err := s.DailyBoard(ctx, thursday, daily.TabWeek, players[name])
		if err != nil {
			t.Fatal(err)
		}
		if got != nil && (tab.You == nil || tab.You.Rank != got.Rank || tab.Total != got.Players) {
			t.Errorf("%s: the standing says %+v and the This week tab %+v of %d", name, got, tab.You, tab.Total)
		}
	}

	// Before today's game, Monday to Wednesday: the seven who played
	// them have 1,500 in three minutes each, a tie all the way down,
	// which falls by who joined first, as the tab's does. Today's points
	// count for nobody, Ava's 900 included, and nobody with nothing
	// before today has a standing.
	for name, want := range map[string]*daily.Week{
		"Ava": {Rank: 1, Players: 7},
		"Flo": {Rank: 6, Players: 7},
		"Gus": {Rank: 7, Players: 7},
		"New": nil,
		"Zed": nil,
	} {
		if got := standing(thursday, name, false); !same(got, want) {
			t.Errorf("%s before today's game: %+v, want %+v", name, got, want)
		}
	}

	// A Monday before playing has no day to count; after Monday's game,
	// everyone is listed, there being no earlier puzzle.
	if got := standing(monday, "Ava", false); got != nil {
		t.Errorf("Monday before playing: %+v", got)
	}
	if got := standing(monday, "Bo", true); !same(got, &daily.Week{Rank: 2, Players: 7}) {
		t.Errorf("Bo after Monday's game: %+v", got)
	}
	if w, err := s.DailyStanding(ctx, thursday, 0, true); err != nil || w != nil {
		t.Errorf("no player: %+v, %v", w, err)
	}
}
