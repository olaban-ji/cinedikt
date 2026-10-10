package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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

// TestAMoveIsRecordedOnceAndInOrder: a game starts with every point; a
// move is checked and recorded; the same key again is the same game, not
// a second charge; a move made from an old point is stale and comes back
// with the game; and the right guess ends it, on the row the boards
// read.
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
		t.Errorf("Play twice restarted the game or moved the zone: %+v, %v", again, err)
	}

	next := daily.Request{Key: "next-0001", Seq: 0, Kind: daily.KindNext}
	rec, err = s.DailyAct(ctx, player.ID, p, next, started.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Moves) != 1 || rec.Moves[0].Seq != 1 || rec.Moves[0].Cost != daily.NameCost || rec.Moves[0].Arg != "" {
		t.Errorf("after a next name: %+v", rec.Moves)
	}
	// The same request again, a retry after a dropped answer.
	rec, err = s.DailyAct(ctx, player.ID, p, next, started.Add(20*time.Second))
	if err != nil || len(rec.Moves) != 1 {
		t.Errorf("a retry: %d moves, %v", len(rec.Moves), err)
	}
	var pts, moves int
	if err := s.pool.QueryRow(ctx, `SELECT pts, moves FROM meta.daily_games WHERE player = $1`, player.ID).Scan(&pts, &moves); err != nil {
		t.Fatal(err)
	}
	if pts != daily.Start-daily.NameCost || moves != 1 {
		t.Errorf("the row says %d points after %d moves, want 900 after 1", pts, moves)
	}
	// Another tab, still at seq 0.
	rec, err = s.DailyAct(ctx, player.ID, p, daily.Request{Key: "other-tab", Seq: 0, Kind: daily.KindGenre}, started.Add(30*time.Second))
	if !errors.Is(err, daily.ErrStale) || rec == nil || len(rec.Moves) != 1 {
		t.Errorf("a stale move: %v, with %+v", err, rec)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "genre-01", Seq: 1, Kind: daily.KindGenre}, started.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "genre-02", Seq: 2, Kind: daily.KindGenre}, started.Add(45*time.Second)); !errors.Is(err, daily.ErrKnown) {
		t.Errorf("the genre twice: %v, want known", err)
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
	if err := s.pool.QueryRow(ctx, `SELECT pts, moves, finished_at, won, gave_up FROM meta.daily_games WHERE player = $1`, player.ID).
		Scan(&pts, &moves, &finished, &isWon, &gaveUp); err != nil {
		t.Fatal(err)
	}
	if pts != 800 || moves != 3 || finished == nil || !isWon || gaveUp {
		t.Errorf("the finished row: %d points, %d moves, finished %v, won %v, gave up %v", pts, moves, finished, isWon, gaveUp)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "too-late", Seq: 3, Kind: daily.KindReveal}, won); !errors.Is(err, daily.ErrDone) {
		t.Errorf("a move after the end: %v, want done", err)
	}
	// Read back cold, the game replays to the same end.
	cold, err := s.DailyGame(ctx, player.ID, p.No)
	if err != nil {
		t.Fatal(err)
	}
	if state := daily.Replay(p, cold.Moves); !state.Won || state.Pts != pts || state.Seen() != 2 {
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
	if _, err := s.DailyAct(ctx, tokyo.ID, p, daily.Request{Key: "in-time-1", Seq: 0, Kind: daily.KindGenre}, midnight.Add(-time.Second)); err != nil {
		t.Fatalf("a second before midnight in Tokyo: %v", err)
	}
	if _, err := s.DailyAct(ctx, tokyo.ID, p, daily.Request{Key: "too-late", Seq: 1, Kind: daily.KindReveal}, midnight); !errors.Is(err, daily.ErrDay) {
		t.Errorf("at midnight in Tokyo: %v, want day", err)
	}
	if rec, err := s.DailyAct(ctx, tokyo.ID, p, daily.Request{Key: "in-time-1", Seq: 0, Kind: daily.KindGenre}, midnight.Add(time.Hour)); err != nil || len(rec.Moves) != 1 {
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

// TestAGuessIsJudgedOnTheLiveCatalog: which of the six it credits is
// read from its credits when it is made, with whether it shares the
// answer's decade and a genre, and kept with the move; directors and the
// seventh-billed are not the six and never count; and a movie the
// catalog does not have is unknown.
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
	// Filler 2 is through the second-billed, Bo, in slot 4: from 1962, a
	// drama.
	rec, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "filler-02", Seq: 0, Kind: daily.KindGuess, Arg: "tt9900202"}, oct8.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	g := rec.Moves[0].Guess
	decade, drama := p.Answer.Year/10 == 196, slices.Contains(p.Answer.Genres, "Drama")
	if g == nil || g.Title != "Filler 2" || g.Year != 1962 || !slices.Equal(g.Shared, []int{4}) || g.SameDecade != decade || g.SharesGenre != drama {
		t.Errorf("the guess learned %+v; want slot 4, same decade %v, a genre %v", g, decade, drama)
	}
	if rec.Moves[0].Cost != daily.WrongCost {
		t.Errorf("cost %d", rec.Moves[0].Cost)
	}
	// Filler 8 is the director's alone, and Filler 7 the seventh-billed's.
	for i, id := range []string{"tt9900208", "tt9900207"} {
		rec, err = s.DailyAct(ctx, player.ID, p, daily.Request{Key: "not-six-" + id, Seq: 1 + i, Kind: daily.KindGuess, Arg: id}, oct8.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if g := rec.Moves[1+i].Guess; len(g.Shared) != 0 || g.Shared == nil {
			t.Errorf("%s shares %v", id, g.Shared)
		}
	}
	// Another candidate shares all six.
	other := "tt9900101"
	if other == p.Answer.ID {
		other = "tt9900102"
	}
	rec, err = s.DailyAct(ctx, player.ID, p, daily.Request{Key: "candidate", Seq: 3, Kind: daily.KindGuess, Arg: other}, oct8.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if g := rec.Moves[3].Guess; !slices.Equal(g.Shared, []int{0, 1, 2, 3, 4, 5}) || g.Warmth() != 2 || rec.Moves[3].Cost != 250 {
		t.Errorf("guessing a relative learned %+v for %d", g, rec.Moves[3].Cost)
	}
	cold, err := s.DailyGame(ctx, player.ID, p.No)
	if err != nil {
		t.Fatal(err)
	}
	if g := cold.Moves[0].Guess; g == nil || g.Title != "Filler 2" || !slices.Equal(g.Shared, []int{4}) || g.SameDecade != decade || g.SharesGenre != drama {
		t.Errorf("read back, the guess says %+v", g)
	}
	if state := daily.Replay(p, cold.Moves); state.Seen() != 6 || state.Pts != daily.Start-100-150-200-250 {
		t.Errorf("replayed: %d seen, %d points", state.Seen(), state.Pts)
	}
	var pts int
	if err := s.pool.QueryRow(ctx, `SELECT pts FROM meta.daily_games WHERE player = $1`, player.ID).Scan(&pts); err != nil || pts != 300 {
		t.Errorf("the row says %d points, %v", pts, err)
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
// read, each finished game with its points.
func boardFixture(t *testing.T, s *Store) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	resetDaily(t, s)
	t.Cleanup(func() { resetDaily(t, s) })
	for no := 1; no <= 4; no++ {
		if kept, err := s.putDailyPuzzle(ctx, boardPuzzle(no, time.Date(2026, 10, 4+no, 0, 0, 0, 0, time.UTC))); err != nil || !kept {
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
	game := func(name string, no, pts int, won bool) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at, won)
			VALUES ($1, $2, $3, 1, now(), now(), $4)`, players[name], no, pts, won); err != nil {
			t.Fatal(err)
		}
	}
	// Everyone but New has finished the three earlier puzzles.
	for _, name := range []string{"Ava", "Bo", "Cy", "Di", "Ed", "Flo", "Gus"} {
		for no := 1; no <= 3; no++ {
			game(name, no, 500, true)
		}
	}
	// Today: Ava and Bo tie on points; Gus missed it; New is on their
	// first game; Flo has started and not finished.
	game("Ava", 4, 900, true)
	game("Bo", 4, 900, true)
	game("Cy", 4, 700, true)
	game("Di", 4, 650, true)
	game("Ed", 4, 600, true)
	game("Gus", 4, 0, false)
	game("New", 4, 800, true)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.daily_games (player, no, pts, started_at) VALUES ($1, 4, 1000, now())`, players["Flo"]); err != nil {
		t.Fatal(err)
	}
	return players
}

// boardPuzzle is a puzzle for the boards, which read nothing of it but
// its number and day.
func boardPuzzle(no int, day time.Time) *daily.Puzzle {
	return &daily.Puzzle{No: no, Day: day,
		Answer: daily.Answer{ID: fmt.Sprintf("tt000000%d", no), Title: "Answer", Year: 1999, Rating: 8, Length: 100, Colour: "#26382d"}}
}

// boardRows is a board's rows as "place name pts", "=" before a shared
// place and "[]" around the reader.
func boardRows(b daily.Board) []string {
	var out []string
	for _, r := range b.Rows {
		place := fmt.Sprint(r.Place)
		if r.Tied {
			place = "=" + place
		}
		name := strings.TrimSuffix(r.Name, " Player")
		if r.You {
			name = "[" + name + "]"
		}
		out = append(out, fmt.Sprintf("%s %s %d", place, name, r.Pts))
	}
	return out
}

// TestTodaysBoardIsThePlayersAroundYourScore, placed by points alone:
// two just above, the reader, one on their score and one just below,
// never the top. A player on their first game is placed where they would
// sit, on their own board only; a game not finished is on no board, and
// a reader with no finished game sees no rows, only the board's size and
// today's figures.
func TestTodaysBoardIsThePlayersAroundYourScore(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	players := boardFixture(t, s)
	p, err := s.DailyPuzzleNo(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	board := func(name string) daily.Board {
		t.Helper()
		b, err := s.DailyBoard(ctx, p, daily.TabToday, players[name])
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	anyone := board("nobody")
	if anyone.Rows == nil || len(anyone.Rows) != 0 || anyone.Total != 6 || anyone.You != nil || anyone.Beat != nil {
		t.Errorf("without a player: %v, total %d, you %+v", boardRows(anyone), anyone.Total, anyone.You)
	}
	// Six of seven finished games were won, and they fell in these bars.
	if anyone.Solved == nil || *anyone.Solved != 86 || !slices.Equal(anyone.Chart, []int{1, 0, 0, 0, 0, 0, 2, 1, 1, 2, 0}) {
		t.Errorf("solved = %v, chart = %v", anyone.Solved, anyone.Chart)
	}

	for name, want := range map[string][]string{
		// New, unlisted, is placed among the listed on their own board.
		"New": {"=1 Ava 900", "=1 Bo 900", "3 [New] 800", "4 Cy 700"},
		"Ava": {"=1 [Ava] 900", "=1 Bo 900", "3 Cy 700"},
		"Cy":  {"=1 Ava 900", "=1 Bo 900", "3 [Cy] 700", "4 Di 650"},
		"Gus": {"4 Di 650", "5 Ed 600", "6 [Gus] 0"},
		"Flo": nil,
	} {
		b := board(name)
		if got := boardRows(b); !slices.Equal(got, want) {
			t.Errorf("%s's board = %v, want %v", name, got, want)
		}
		for _, r := range b.Rows {
			if r.Days != nil {
				t.Errorf("%s's board has days on today's tab: %+v", name, r)
			}
		}
	}
	fresh := board("New")
	if y := fresh.You; y == nil || y.Place != 3 || y.Tied || y.Pts != 800 || y.Days != nil || fresh.Total != 7 {
		t.Errorf("New = %+v of %d", fresh.You, fresh.Total)
	}
	// Four of the seven finished games scored less than New's 800.
	if fresh.Beat == nil || *fresh.Beat != 57 {
		t.Errorf("beat = %v, want 57", fresh.Beat)
	}
	if ava := board("Ava"); ava.You == nil || ava.You.Place != 1 || !ava.You.Tied || ava.Total != 6 {
		t.Errorf("Ava = %+v of %d", ava.You, ava.Total)
	}
	if gus := board("Gus"); gus.Beat == nil || *gus.Beat != 0 {
		t.Errorf("Gus beat = %v", gus.Beat)
	}
	if flo := board("Flo"); flo.You != nil || flo.Beat != nil || flo.Total != 6 {
		t.Errorf("a game in play: you %+v, beat %v, total %d", flo.You, flo.Beat, flo.Total)
	}
}

// TestTheWeeksBoardIsTwoAheadAndTwoBehind by each listed player's points
// over Monday to this puzzle's day, with each day's points, 0 for a day
// not played, and no chart or figures.
func TestTheWeeksBoardIsTwoAheadAndTwoBehind(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	players := boardFixture(t, s)
	p, err := s.DailyPuzzleNo(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Ava and Bo have 2,400; Cy 2,200, Di 2,150, Ed 2,100; Flo and Gus
	// 1,500, Flo's open game counting nothing; and New 800.
	for name, want := range map[string][]string{
		"New": {"=6 Flo 1500", "=6 Gus 1500", "8 [New] 800"},
		"Ava": {"=1 [Ava] 2400", "=1 Bo 2400", "3 Cy 2200"},
		"Di":  {"=1 Bo 2400", "3 Cy 2200", "4 [Di] 2150", "5 Ed 2100", "=6 Flo 1500"},
	} {
		week, err := s.DailyBoard(ctx, p, daily.TabWeek, players[name])
		if err != nil {
			t.Fatal(err)
		}
		if got := boardRows(week); !slices.Equal(got, want) {
			t.Errorf("%s's week = %v, want %v", name, got, want)
		}
		if week.Beat != nil || week.Solved != nil || week.Chart != nil {
			t.Errorf("the week's tab has beat %v, solved %v and chart %v", week.Beat, week.Solved, week.Chart)
		}
	}
	week, err := s.DailyBoard(ctx, p, daily.TabWeek, players["New"])
	if err != nil {
		t.Fatal(err)
	}
	if week.Total != 8 || week.You == nil || week.You.Place != 8 || !slices.Equal(week.You.Days, []int{0, 0, 0, 800}) {
		t.Errorf("New = %+v of %d", week.You, week.Total)
	}
	if flo := week.Rows[0]; !slices.Equal(flo.Days, []int{500, 500, 500, 0}) {
		t.Errorf("Flo's days = %v", flo.Days)
	}
	body, err := json.Marshal(week)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"you":{"place":8,"tied":false,"pts":800,"days":[0,0,0,800]}`) || !strings.Contains(string(body), `"chart":null`) {
		t.Errorf("the week's tab says %s", body)
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
	monday := boardPuzzle(8, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
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
		{`INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at, won, zone)
		  VALUES ($1, 8, 990, 1, '2026-10-11T23:00Z', '2026-10-11T23:30Z', true, 'Asia/Tokyo')`, players["Cy"]},
	} {
		if _, err := s.pool.Exec(ctx, c.sql, c.player); err != nil {
			t.Fatalf("%v\n%s", err, c.sql)
		}
	}
	thursday, err := s.DailyPuzzleNo(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	week, err := s.DailyBoard(ctx, thursday, daily.TabWeek, players["Bo"])
	if err != nil {
		t.Fatal(err)
	}
	if got := boardRows(week); !slices.Equal(got, []string{"=1 Ava 2400", "=1 [Bo] 2400", "3 Cy 2200", "4 Di 2150"}) {
		t.Errorf("Thursday's week = %v", got)
	}
	if ava := week.Rows[0]; !slices.Equal(ava.Days, []int{500, 500, 500, 900}) {
		t.Errorf("Ava's days = %v", ava.Days)
	}
	if !slices.Equal(week.You.Days, []int{500, 500, 500, 900}) {
		t.Errorf("Bo's days = %v", week.You.Days)
	}
	next, err := s.DailyBoard(ctx, monday, daily.TabWeek, players["Cy"])
	if err != nil {
		t.Fatal(err)
	}
	if got := boardRows(next); !slices.Equal(got, []string{"1 [Cy] 990"}) || next.You == nil || !slices.Equal(next.You.Days, []int{990}) {
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

// TestEveryFactIsKeptAsAMove: each bought once at its price, on the row
// the boards read, and replayed cold to the facts the page is shown; the
// old game's clues are no moves, and neither is the overlap, for anyone,
// nor the five years, before the decade or after it.
func TestEveryFactIsKeptAsAMove(t *testing.T) {
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
	for _, r := range []daily.Request{
		{Key: "the-years", Kind: "years"}, {Key: "the-story", Kind: "story"}, {Key: "the-year", Kind: "year"},
		{Key: "a-flip", Kind: "flip", Arg: "c1"}, {Key: "an-actor", Kind: "actor"},
		{Key: "an-overlap", Kind: "overlap", Arg: p.Cast[0].ID}, {Key: "a-hidden-overlap", Kind: "overlap", Arg: p.Cast[5].ID},
	} {
		if _, err := s.DailyAct(ctx, player.ID, p, r, oct8); !errors.Is(err, daily.ErrBad) {
			t.Errorf("%s %s: %v, want bad", r.Kind, r.Arg, err)
		}
	}
	for i, r := range []daily.Request{
		{Key: "the-decade", Kind: daily.KindDecade}, {Key: "the-rating", Kind: daily.KindRating},
		{Key: "the-length", Kind: daily.KindLength},
	} {
		r.Seq = i
		if _, err := s.DailyAct(ctx, player.ID, p, r, oct8.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("%s: %v", r.Kind, err)
		}
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "decade-again", Seq: 3, Kind: daily.KindDecade}, oct8.Add(time.Hour)); !errors.Is(err, daily.ErrKnown) {
		t.Errorf("the decade twice: %v, want known", err)
	}
	if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "years-after", Seq: 3, Kind: "years"}, oct8.Add(time.Hour)); !errors.Is(err, daily.ErrBad) {
		t.Errorf("the five years after the decade: %v, want bad", err)
	}
	var pts int
	if err := s.pool.QueryRow(ctx, `SELECT pts FROM meta.daily_games WHERE player = $1`, player.ID).Scan(&pts); err != nil || pts != 800 {
		t.Errorf("the row says %d points, %v", pts, err)
	}
	cold, err := s.DailyGame(ctx, player.ID, p.No)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, m := range cold.Moves {
		kinds = append(kinds, m.Kind)
		if m.Arg != "" || m.Guess != nil {
			t.Errorf("%s was kept as %+v", m.Kind, m)
		}
	}
	if !slices.Equal(kinds, []string{daily.KindDecade, daily.KindRating, daily.KindLength}) {
		t.Errorf("the moves kept are %v", kinds)
	}
	game := daily.Render(p, cold, daily.Live{})
	f := game.Facts
	if f.Decade == nil || *f.Decade != p.Answer.Year/10*10 || f.Rating == nil || *f.Rating != daily.RatingBand(p.Answer.Rating) ||
		f.Length == nil || *f.Length != daily.LengthBand(p.Answer.Length) || f.Genre != nil || game.Pts != 800 {
		t.Errorf("read back, the game says %+v with %d points", f, game.Pts)
	}
}

// TestOneSheetIsOpenedAGameUnderItsRow: six requests at once, from the
// same point, each opening another of the six's sheets, take turns on
// the game's row, so one is recorded and the other five are stale. It
// is kept with whose sheet it is and no cost, read back cold as the
// game's sheet; a retry of it naming someone else is the game as it is;
// and a second, from the game as it stands, is known.
func TestOneSheetIsOpenedAGameUnderItsRow(t *testing.T) {
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
	for _, who := range []string{p.Cast[1].ID, p.Directors[0].ID} {
		if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "too-soon-" + who, Kind: daily.KindSheet, Arg: who}, oct8); !errors.Is(err, daily.ErrBad) {
			t.Errorf("a sheet for %s, not showing: %v, want bad", who, err)
		}
	}
	seq := 0
	for ; seq < daily.Slots-1; seq++ {
		if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: fmt.Sprintf("next-%d", seq), Seq: seq, Kind: daily.KindNext}, oct8); err != nil {
			t.Fatal(err)
		}
	}

	errs := make([]error, len(p.Cast))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, c := range p.Cast {
		wg.Go(func() {
			<-start
			_, errs[i] = s.DailyAct(ctx, player.ID, p, daily.Request{Key: fmt.Sprintf("other-tab-%d", i), Seq: seq, Kind: daily.KindSheet, Arg: c.ID}, oct8.Add(time.Minute))
		})
	}
	close(start)
	wg.Wait()
	opened := -1
	for i, err := range errs {
		switch {
		case err == nil && opened < 0:
			opened = i
		case errors.Is(err, daily.ErrStale):
		default:
			t.Errorf("the tab opening slot %d's sheet: %v", i, err)
		}
	}
	if opened < 0 {
		t.Fatal("no tab opened a sheet")
	}
	who := p.Cast[opened].ID
	seq++

	cold, err := s.DailyGame(ctx, player.ID, p.No)
	if err != nil {
		t.Fatal(err)
	}
	if len(cold.Moves) != seq {
		t.Fatalf("%d moves kept, want %d", len(cold.Moves), seq)
	}
	if m := cold.Moves[seq-1]; m.Kind != daily.KindSheet || m.Arg != who || m.Cost != 0 || m.Guess != nil || m.Key != fmt.Sprintf("other-tab-%d", opened) {
		t.Errorf("the sheet was kept as %+v", m)
	}
	if g := daily.Render(p, cold, daily.Live{}); g.Sheet == nil || *g.Sheet != who || g.Pts != daily.Start-(daily.Slots-1)*daily.NameCost {
		t.Errorf("read back, the game says sheet %v with %d points", g.Sheet, g.Pts)
	}
	var pts, moves int
	if err := s.pool.QueryRow(ctx, `SELECT pts, moves FROM meta.daily_games WHERE player = $1`, player.ID).Scan(&pts, &moves); err != nil ||
		pts != daily.Start-(daily.Slots-1)*daily.NameCost || moves != seq {
		t.Errorf("the row says %d points and %d moves, %v", pts, moves, err)
	}

	other := p.Cast[(opened+1)%daily.Slots].ID
	rec, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: fmt.Sprintf("other-tab-%d", opened), Seq: seq - 1, Kind: daily.KindSheet, Arg: other}, oct8.Add(time.Hour))
	if err != nil {
		t.Fatalf("a retry naming someone else: %v", err)
	}
	if len(rec.Moves) != seq || daily.Replay(p, rec.Moves).Sheet != who {
		t.Errorf("a retry naming someone else is answered with %d moves and the sheet %q", len(rec.Moves), daily.Replay(p, rec.Moves).Sheet)
	}
	for _, again := range []string{who, other} {
		if _, err := s.DailyAct(ctx, player.ID, p, daily.Request{Key: "again-" + again, Seq: seq, Kind: daily.KindSheet, Arg: again}, oct8.Add(time.Hour)); !errors.Is(err, daily.ErrKnown) {
			t.Errorf("a second sheet, for %s: %v, want known", again, err)
		}
	}
}

// The tables Point Blank made, as its meta.sql left them: the shape a
// database that ran it has, which meta.sql must bring to Name Drop's.
const pointBlankSQL = `
	CREATE TABLE meta.daily_puzzles (
	    no int PRIMARY KEY, day date UNIQUE NOT NULL, answer text NOT NULL, title text NOT NULL,
	    year int NOT NULL, rating numeric(3,1) NOT NULL, md int NOT NULL, people jsonb NOT NULL,
	    genres text[] NOT NULL, cards jsonb NOT NULL, start text[] NOT NULL, era int NOT NULL,
	    genre text NOT NULL, picked_at timestamptz NOT NULL DEFAULT now());
	CREATE TABLE meta.daily_players (
	    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, token bytea UNIQUE NOT NULL,
	    name text UNIQUE NOT NULL, hue smallint NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
	CREATE TABLE meta.daily_games (
	    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	    player bigint NOT NULL REFERENCES meta.daily_players (id) ON DELETE CASCADE,
	    no int NOT NULL REFERENCES meta.daily_puzzles (no), pts int NOT NULL, moves int NOT NULL DEFAULT 0,
	    started_at timestamptz NOT NULL, finished_at timestamptz, won boolean NOT NULL DEFAULT false,
	    gave_up boolean NOT NULL DEFAULT false, ms int, zone text NOT NULL DEFAULT 'UTC', UNIQUE (player, no));
	CREATE INDEX daily_games_board ON meta.daily_games (no, pts DESC, ms) WHERE finished_at IS NOT NULL;
	CREATE INDEX daily_games_no ON meta.daily_games (no);
	CREATE TABLE meta.daily_moves (
	    game bigint NOT NULL REFERENCES meta.daily_games (id) ON DELETE CASCADE, seq int NOT NULL,
	    key text NOT NULL,
	    kind text NOT NULL CHECK (kind IN ('flip', 'director', 'actor', 'genres', 'year', 'guess', 'reveal')),
	    arg text, cost int NOT NULL, detail jsonb, at timestamptz NOT NULL,
	    PRIMARY KEY (game, seq), UNIQUE (game, key));
	INSERT INTO meta.daily_puzzles (no, day, answer, title, year, rating, md, people, genres, cards, start, era, genre)
	VALUES (1, '2026-10-08', 'tt0133093', 'The Matrix', 1999, 8.7, 331, '[]', '{Action}', '[]', '{}', 1995, 'Action');
	INSERT INTO meta.daily_players (token, name, hue) VALUES ('\x01', 'Trinity Kimble', 205);
	INSERT INTO meta.daily_games (player, no, pts, started_at) SELECT id, 1, 945, now() FROM meta.daily_players;
	INSERT INTO meta.daily_moves (game, seq, key, kind, arg, cost, at)
	SELECT id, 1, 'flip-0001', 'flip', 'c4', 55, now() FROM meta.daily_games`

// dailyShape is the daily tables as the catalog has them: each one's
// columns, the moves' kind check, and the board's index.
func dailyShape(t *testing.T, s *Store) string {
	t.Helper()
	var shape string
	if err := s.pool.QueryRow(context.Background(), `
		SELECT (SELECT string_agg(table_name || '.' || column_name || ' ' || data_type, ', ' ORDER BY table_name, ordinal_position)
		        FROM information_schema.columns WHERE table_schema = 'meta' AND table_name LIKE 'daily\_%')
		    || ' | ' || (SELECT pg_get_constraintdef(oid) FROM pg_constraint
		                 WHERE conrelid = 'meta.daily_moves'::regclass AND conname = 'daily_moves_kind_check')
		    || ' | ' || (SELECT pg_get_indexdef('meta.daily_games_board'::regclass))`).Scan(&shape); err != nil {
		t.Fatal(err)
	}
	return shape
}

// TestMetaBringsPointBlanksTablesToNameDrop: meta.sql applied twice to a
// database with no daily tables, twice to one with Point Blank's, and
// twice to one already in Name Drop's leaves the same shape every time:
// puzzles without cards, games without a clock, and a kind check that
// takes Name Drop's moves and refuses Point Blank's. Point Blank's
// puzzles, games and moves go and its players stay; Name Drop's are
// kept as they are.
func TestMetaBringsPointBlanksTablesToNameDrop(t *testing.T) {
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
	count := func(table string) int {
		t.Helper()
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM meta.`+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	t.Cleanup(func() { resetDaily(t, s) })
	drop := `DROP TABLE meta.daily_moves, meta.daily_games, meta.daily_players, meta.daily_puzzles`

	exec(drop, metaSQL, metaSQL)
	fresh := dailyShape(t, s)
	for _, want := range []string{"daily_puzzles.billed jsonb", "daily_puzzles.movies jsonb", "daily_puzzles.colour character",
		"daily_puzzles.length integer", "daily_puzzles.directors jsonb", "'next'", "'decade'", "'sheet'", "'guess'", "(no, pts DESC)"} {
		if !strings.Contains(fresh, want) {
			t.Errorf("a fresh database lacks %s: %s", want, fresh)
		}
	}
	for _, gone := range []string{"daily_puzzles.cards", "daily_puzzles.start ", "daily_puzzles.people", "daily_games.ms ",
		"'flip'", "'actor'", "'genres'", "'year'", "'overlap'", "'years'"} {
		if strings.Contains(fresh, gone) {
			t.Errorf("a fresh database has %s: %s", gone, fresh)
		}
	}

	exec(drop, pointBlankSQL)
	if old := dailyShape(t, s); !strings.Contains(old, "cards") || count("daily_moves") != 1 {
		t.Fatalf("the set-up did not make Point Blank's tables: %s", old)
	}
	exec(metaSQL, metaSQL)
	if got := dailyShape(t, s); got != fresh {
		t.Errorf("over Point Blank's tables:\n%s\nwant\n%s", got, fresh)
	}
	if count("daily_puzzles") != 0 || count("daily_games") != 0 || count("daily_moves") != 0 || count("daily_players") != 1 {
		t.Errorf("Point Blank's rows left: %d puzzles, %d games, %d moves, %d players",
			count("daily_puzzles"), count("daily_games"), count("daily_moves"), count("daily_players"))
	}

	if kept, err := s.putDailyPuzzle(ctx, boardPuzzle(1, oct8)); err != nil || !kept {
		t.Fatalf("a Name Drop puzzle: %v, %v", kept, err)
	}
	exec(`INSERT INTO meta.daily_games (player, no, pts, started_at) SELECT id, 1, 900, now() FROM meta.daily_players`)
	move := `INSERT INTO meta.daily_moves (game, seq, key, kind, cost, at) SELECT id, $1, $2, $3, 100, now() FROM meta.daily_games`
	if _, err := s.pool.Exec(ctx, move, 1, "next-0001", daily.KindNext); err != nil {
		t.Errorf("a next name: %v", err)
	}
	var pgErr *pgconn.PgError
	if _, err := s.pool.Exec(ctx, move, 2, "flip-0002", "flip"); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("a flip: %v, want the check to refuse it", err)
	}
	exec(metaSQL, metaSQL)
	if got := dailyShape(t, s); got != fresh {
		t.Errorf("over Name Drop's tables:\n%s\nwant\n%s", got, fresh)
	}
	if count("daily_puzzles") != 1 || count("daily_games") != 1 || count("daily_moves") != 1 || count("daily_players") != 1 {
		t.Error("meta.sql changed a database already in Name Drop's shape")
	}
	if p, err := s.DailyPuzzleNo(ctx, 1); err != nil || p.Answer.Colour != "#26382d" || p.Answer.Length != 100 || p.Cast == nil || p.Movies == nil {
		t.Errorf("the Name Drop puzzle read back: %+v, %v", p, err)
	}
}

// The moves table as Name Drop's first meta.sql made it, its kind check
// taking the overlap: the shape a database that ran it has, which
// meta.sql must bring to the one without.
const overlapMovesSQL = `
	CREATE TABLE meta.daily_moves (
	    game   bigint NOT NULL REFERENCES meta.daily_games (id) ON DELETE CASCADE,
	    seq    int NOT NULL,
	    key    text NOT NULL,
	    kind   text NOT NULL CHECK (kind IN ('next', 'length', 'rating', 'genre', 'decade', 'years',
	                                         'director', 'overlap', 'guess', 'reveal')),
	    arg    text, cost int NOT NULL, detail jsonb, at timestamptz NOT NULL,
	    PRIMARY KEY (game, seq), UNIQUE (game, key))`

// TestMetaTakesTheOverlapOutOfTheKindCheck: meta.sql applied twice to a
// database whose kind check still takes the overlap, with a game that
// bought one, leaves the shape a fresh database has: a check that
// refuses the overlap and takes every other move, the sheet among them.
// The game that bought one goes, its moves with it, since it cannot be
// replayed without it; the other game, its moves and both players stay
// as they were.
func TestMetaTakesTheOverlapOutOfTheKindCheck(t *testing.T) {
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
	count := func(sql string) int {
		t.Helper()
		var n int
		if err := s.pool.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	t.Cleanup(func() { resetDaily(t, s) })
	drop := `DROP TABLE meta.daily_moves, meta.daily_games, meta.daily_players, meta.daily_puzzles`

	exec(drop, metaSQL, metaSQL)
	fresh := dailyShape(t, s)
	exec(`DROP TABLE meta.daily_moves`, overlapMovesSQL)
	if old := dailyShape(t, s); !strings.Contains(old, "'overlap'") {
		t.Fatalf("the set-up did not make the moves table that takes the overlap: %s", old)
	}
	if kept, err := s.putDailyPuzzle(ctx, boardPuzzle(1, oct8)); err != nil || !kept {
		t.Fatalf("a puzzle: %v, %v", kept, err)
	}
	var games [2]int64
	for i, name := range []string{"Trinity Kimble", "Morpheus Vane"} {
		player, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), name, 205)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO meta.daily_games (player, no, pts, moves, started_at) VALUES ($1, 1, $2, $3, now())
			RETURNING id`, player.ID, []int{600, 800}[i], []int{3, 2}[i]).Scan(&games[i]); err != nil {
			t.Fatal(err)
		}
	}
	move := `INSERT INTO meta.daily_moves (game, seq, key, kind, arg, cost, at) VALUES ($1, $2, $3, $4, nullif($5, ''), $6, now())`
	for _, m := range []struct {
		game      int64
		seq       int
		kind, arg string
		cost      int
	}{
		{games[0], 1, daily.KindNext, "", 100}, {games[0], 2, "overlap", "nm9900007", 250}, {games[0], 3, daily.KindLength, "", 50},
		{games[1], 1, daily.KindNext, "", 100}, {games[1], 2, daily.KindDecade, "", 100},
	} {
		if _, err := s.pool.Exec(ctx, move, m.game, m.seq, fmt.Sprintf("key-%d-%d", m.game, m.seq), m.kind, m.arg, m.cost); err != nil {
			t.Fatalf("%s: %v", m.kind, err)
		}
	}

	exec(metaSQL, metaSQL)
	if got := dailyShape(t, s); got != fresh {
		t.Errorf("over the check that takes the overlap:\n%s\nwant\n%s", got, fresh)
	}
	if n := count(`SELECT count(*) FROM meta.daily_moves WHERE kind = 'overlap'`); n != 0 {
		t.Errorf("%d overlap moves are left", n)
	}
	if n := count(`SELECT count(*) FROM meta.daily_games`); n != 1 || count(`SELECT count(*) FROM meta.daily_players`) != 2 ||
		count(`SELECT count(*) FROM meta.daily_puzzles`) != 1 {
		t.Errorf("%d games are left, want the one that bought no overlap", n)
	}
	var kept int64
	var moves int
	if err := s.pool.QueryRow(ctx, `SELECT g.id, (SELECT count(*) FROM meta.daily_moves m WHERE m.game = g.id) FROM meta.daily_games g`).
		Scan(&kept, &moves); err != nil || kept != games[1] || moves != 2 {
		t.Errorf("the game kept is %d with %d moves, %v; want %d with 2", kept, moves, err, games[1])
	}
	var pgErr *pgconn.PgError
	if _, err := s.pool.Exec(ctx, move, games[1], 3, "an-overlap", "overlap", "nm9900007", 250); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("an overlap: %v, want the check to refuse it", err)
	}
	for i, kind := range []string{daily.KindNext, daily.KindLength, daily.KindRating, daily.KindGenre, daily.KindDecade,
		daily.KindDirector, daily.KindSheet, daily.KindGuess, daily.KindReveal} {
		if _, err := s.pool.Exec(ctx, move, games[1], 3+i, "move-"+kind, kind, "", 0); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}

// The moves table as Name Drop's second meta.sql made it, its kind check
// taking neither the overlap nor the sheet: the shape a database that ran
// it has, which meta.sql must bring to the one that takes the sheet.
const sheetlessMovesSQL = `
	CREATE TABLE meta.daily_moves (
	    game   bigint NOT NULL REFERENCES meta.daily_games (id) ON DELETE CASCADE,
	    seq    int NOT NULL,
	    key    text NOT NULL,
	    kind   text NOT NULL CHECK (kind IN ('next', 'length', 'rating', 'genre', 'decade', 'years',
	                                         'director', 'guess', 'reveal')),
	    arg    text, cost int NOT NULL, detail jsonb, at timestamptz NOT NULL,
	    PRIMARY KEY (game, seq), UNIQUE (game, key))`

// TestMetaAddsTheSheetToTheKindCheck: meta.sql applied twice to a
// database whose kind check takes neither the overlap nor the sheet,
// with games of every other kind of move, finished and not, leaves the
// shape a fresh database has: a check that takes the sheet and refuses
// the overlap. Nothing that database holds is a move the new check
// refuses, so every game, move and player stays as it was, and the game
// still in play can open its sheet.
func TestMetaAddsTheSheetToTheKindCheck(t *testing.T) {
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
	// held is every game and its moves, in order, and the players.
	held := func() string {
		t.Helper()
		var got string
		if err := s.pool.QueryRow(ctx, `
			SELECT coalesce((SELECT string_agg(g.id || ':' || g.pts || ':' || g.moves || ':' || (g.finished_at IS NOT NULL) || ' ' ||
			                   coalesce((SELECT string_agg(m.seq || m.kind || coalesce(m.arg, '') || m.cost, ',' ORDER BY m.seq)
			                             FROM meta.daily_moves m WHERE m.game = g.id), ''), ' | ' ORDER BY g.id)
			                 FROM meta.daily_games g), '')
			    || ' / ' || (SELECT count(*) FROM meta.daily_players)`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Cleanup(func() { resetDaily(t, s) })
	drop := `DROP TABLE meta.daily_moves, meta.daily_games, meta.daily_players, meta.daily_puzzles`

	exec(drop, metaSQL, metaSQL)
	fresh := dailyShape(t, s)
	exec(`DROP TABLE meta.daily_moves`, sheetlessMovesSQL)
	if old := dailyShape(t, s); strings.Contains(old, "'sheet'") || strings.Contains(old, "'overlap'") || old == fresh {
		t.Fatalf("the set-up did not make the moves table that takes no sheet: %s", old)
	}
	if kept, err := s.putDailyPuzzle(ctx, boardPuzzle(1, oct8)); err != nil || !kept {
		t.Fatalf("a puzzle: %v, %v", kept, err)
	}
	var games [2]int64
	for i, name := range []string{"Trinity Kimble", "Morpheus Vane"} {
		player, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), name, 205)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at) VALUES ($1, 1, $2, $3, now(), $4)
			RETURNING id`, player.ID, []int{750, 0}[i], []int{3, 4}[i], []*time.Time{nil, &oct8}[i]).Scan(&games[i]); err != nil {
			t.Fatal(err)
		}
	}
	move := `INSERT INTO meta.daily_moves (game, seq, key, kind, arg, cost, at) VALUES ($1, $2, $3, $4, nullif($5, ''), $6, now())`
	for _, m := range []struct {
		game      int64
		seq       int
		kind, arg string
		cost      int
	}{
		{games[0], 1, daily.KindNext, "", 100}, {games[0], 2, daily.KindDecade, "", 100}, {games[0], 3, daily.KindLength, "", 50},
		{games[1], 1, daily.KindRating, "", 50}, {games[1], 2, daily.KindGenre, "", 100}, {games[1], 3, daily.KindGuess, "tt9900099", 100},
		{games[1], 4, daily.KindReveal, "", 0},
	} {
		if _, err := s.pool.Exec(ctx, move, m.game, m.seq, fmt.Sprintf("key-%d-%d", m.game, m.seq), m.kind, m.arg, m.cost); err != nil {
			t.Fatalf("%s: %v", m.kind, err)
		}
	}
	var pgErr *pgconn.PgError
	if _, err := s.pool.Exec(ctx, move, games[0], 4, "a-sheet", daily.KindSheet, "nm9900002", 0); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("a sheet before meta.sql: %v, want the old check to refuse it", err)
	}
	before := held()

	exec(metaSQL, metaSQL)
	if got := dailyShape(t, s); got != fresh {
		t.Errorf("over the check that takes no sheet:\n%s\nwant\n%s", got, fresh)
	}
	if got := held(); got != before {
		t.Errorf("meta.sql changed what was held:\n%s\nwant\n%s", got, before)
	}
	if _, err := s.pool.Exec(ctx, move, games[0], 4, "a-sheet", daily.KindSheet, "nm9900002", 0); err != nil {
		t.Errorf("a sheet: %v", err)
	}
	if _, err := s.pool.Exec(ctx, move, games[0], 5, "an-overlap", "overlap", "nm9900003", 250); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("an overlap: %v, want the check to refuse it", err)
	}
	// And once more, now holding a sheet: nothing changes, the check
	// itself not even made again.
	check := func() (oid uint32) {
		t.Helper()
		if err := s.pool.QueryRow(ctx, `SELECT oid FROM pg_constraint
			WHERE conrelid = 'meta.daily_moves'::regclass AND conname = 'daily_moves_kind_check'`).Scan(&oid); err != nil {
			t.Fatal(err)
		}
		return oid
	}
	before, made := held(), check()
	exec(metaSQL)
	if got := dailyShape(t, s); got != fresh || held() != before || check() != made {
		t.Errorf("meta.sql over its own shape, holding a sheet, changed it: %s\n%s", got, held())
	}
}

// The moves table as Name Drop's third meta.sql made it, its kind check
// taking the sheet and the five years: the shape a database that ran it
// has, which meta.sql must bring to the one without the years.
const yearsMovesSQL = `
	CREATE TABLE meta.daily_moves (
	    game   bigint NOT NULL REFERENCES meta.daily_games (id) ON DELETE CASCADE,
	    seq    int NOT NULL,
	    key    text NOT NULL,
	    kind   text NOT NULL CHECK (kind IN ('next', 'length', 'rating', 'genre', 'decade', 'years',
	                                         'director', 'sheet', 'guess', 'reveal')),
	    arg    text, cost int NOT NULL, detail jsonb, at timestamptz NOT NULL,
	    PRIMARY KEY (game, seq), UNIQUE (game, key))`

// TestMetaTakesTheYearsOutOfTheKindCheck: meta.sql applied twice to a
// database whose kind check still takes the five years, with a game that
// bought them after the decade and opened a sheet, and one that bought
// the decade and the rating and ended, leaves the shape a fresh database
// has: a check that refuses the years and takes every other move. The
// game that bought them goes, its moves with it, since it cannot be
// replayed without them; the other game, its moves and both players
// stay as they were. Applied again, now in the new shape, it changes
// nothing, the check itself not even made again.
func TestMetaTakesTheYearsOutOfTheKindCheck(t *testing.T) {
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
	// held is every game and its moves, in order, and the players.
	held := func() string {
		t.Helper()
		var got string
		if err := s.pool.QueryRow(ctx, `
			SELECT coalesce((SELECT string_agg(g.id || ':' || g.pts || ':' || g.moves || ':' || (g.finished_at IS NOT NULL) || ' ' ||
			                   coalesce((SELECT string_agg(m.seq || m.kind || coalesce(m.arg, '') || m.cost, ',' ORDER BY m.seq)
			                             FROM meta.daily_moves m WHERE m.game = g.id), ''), ' | ' ORDER BY g.id)
			                 FROM meta.daily_games g), '')
			    || ' / ' || (SELECT count(*) FROM meta.daily_players)`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	check := func() (oid uint32) {
		t.Helper()
		if err := s.pool.QueryRow(ctx, `SELECT oid FROM pg_constraint
			WHERE conrelid = 'meta.daily_moves'::regclass AND conname = 'daily_moves_kind_check'`).Scan(&oid); err != nil {
			t.Fatal(err)
		}
		return oid
	}
	t.Cleanup(func() { resetDaily(t, s) })
	drop := `DROP TABLE meta.daily_moves, meta.daily_games, meta.daily_players, meta.daily_puzzles`

	exec(drop, metaSQL, metaSQL)
	fresh := dailyShape(t, s)
	exec(`DROP TABLE meta.daily_moves`, yearsMovesSQL)
	if old := dailyShape(t, s); !strings.Contains(old, "'years'") || !strings.Contains(old, "'sheet'") || old == fresh {
		t.Fatalf("the set-up did not make the moves table that takes the years: %s", old)
	}
	if kept, err := s.putDailyPuzzle(ctx, boardPuzzle(1, oct8)); err != nil || !kept {
		t.Fatalf("a puzzle: %v, %v", kept, err)
	}
	var games [2]int64
	for i, name := range []string{"Trinity Kimble", "Morpheus Vane"} {
		player, err := s.CreateDailyPlayer(ctx, daily.TokenHash(daily.NewToken()), name, 205)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at) VALUES ($1, 1, $2, $3, now(), $4)
			RETURNING id`, player.ID, []int{800, 0}[i], []int{3, 4}[i], []*time.Time{nil, &oct8}[i]).Scan(&games[i]); err != nil {
			t.Fatal(err)
		}
	}
	move := `INSERT INTO meta.daily_moves (game, seq, key, kind, arg, cost, at) VALUES ($1, $2, $3, $4, nullif($5, ''), $6, now())`
	for _, m := range []struct {
		game      int64
		seq       int
		kind, arg string
		cost      int
	}{
		{games[0], 1, daily.KindDecade, "", 100}, {games[0], 2, "years", "", 100}, {games[0], 3, daily.KindSheet, "nm9900007", 0},
		{games[1], 1, daily.KindDecade, "", 100}, {games[1], 2, daily.KindRating, "", 50}, {games[1], 3, daily.KindGuess, "tt9900099", 100},
		{games[1], 4, daily.KindReveal, "", 0},
	} {
		if _, err := s.pool.Exec(ctx, move, m.game, m.seq, fmt.Sprintf("key-%d-%d", m.game, m.seq), m.kind, m.arg, m.cost); err != nil {
			t.Fatalf("%s, before meta.sql: %v", m.kind, err)
		}
	}
	// The other game's moves as they are: what must be left.
	var want string
	if err := s.pool.QueryRow(ctx, `SELECT string_agg(seq || kind || coalesce(arg, '') || cost, ',' ORDER BY seq)
		FROM meta.daily_moves WHERE game = $1`, games[1]).Scan(&want); err != nil {
		t.Fatal(err)
	}

	exec(metaSQL, metaSQL)
	if got := dailyShape(t, s); got != fresh {
		t.Errorf("over the check that takes the years:\n%s\nwant\n%s", got, fresh)
	}
	var left, players int
	var kept int64
	var moves string
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM meta.daily_games), (SELECT count(*) FROM meta.daily_players),
		       (SELECT min(id) FROM meta.daily_games),
		       (SELECT string_agg(seq || kind || coalesce(arg, '') || cost, ',' ORDER BY seq) FROM meta.daily_moves)`).
		Scan(&left, &players, &kept, &moves); err != nil {
		t.Fatal(err)
	}
	if left != 1 || kept != games[1] || moves != want || players != 2 {
		t.Errorf("%d games left, %d with moves %s, and %d players; want game %d alone with %s, and 2 players",
			left, kept, moves, players, games[1], want)
	}
	var pgErr *pgconn.PgError
	if _, err := s.pool.Exec(ctx, move, games[1], 5, "the-years", "years", "", 100); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("the years: %v, want the check to refuse them", err)
	}
	for i, kind := range []string{daily.KindNext, daily.KindLength, daily.KindRating, daily.KindGenre, daily.KindDecade,
		daily.KindDirector, daily.KindSheet, daily.KindGuess, daily.KindReveal} {
		if _, err := s.pool.Exec(ctx, move, games[1], 5+i, "move-"+kind, kind, "", 0); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	before, made := held(), check()
	exec(metaSQL)
	if got := dailyShape(t, s); got != fresh || held() != before || check() != made {
		t.Errorf("meta.sql over its own shape changed it: %s\n%s", got, held())
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
		INSERT INTO meta.daily_games (player, no, pts, moves, started_at, finished_at, gave_up)
		VALUES ($1, 4, 0, 1, now(), now(), true)`, zed.ID); err != nil {
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
	// share first; Gus scored nothing today and shares sixth with Flo,
	// whose game today is not finished; New, on their first game, is
	// placed eighth on a board of eight that only they see, as the tab
	// places them.
	for name, want := range map[string]*daily.Week{
		"Ava": {Rank: 1, Players: 7},
		"Bo":  {Rank: 1, Players: 7},
		"Gus": {Rank: 6, Players: 7},
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
		if got != nil && (tab.You == nil || tab.You.Place != got.Rank || tab.Total != got.Players) {
			t.Errorf("%s: the standing says %+v and the This week tab %+v of %d", name, got, tab.You, tab.Total)
		}
	}

	// Before today's game, Monday to Wednesday: the seven who played
	// them have 1,500 each, a tie all the way down, so all share first.
	// Today's points count for nobody, Ava's 900 included, and nobody
	// with nothing before today has a standing.
	for name, want := range map[string]*daily.Week{
		"Ava": {Rank: 1, Players: 7},
		"Flo": {Rank: 1, Players: 7},
		"Gus": {Rank: 1, Players: 7},
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
	if got := standing(monday, "Bo", true); !same(got, &daily.Week{Rank: 1, Players: 7}) {
		t.Errorf("Bo after Monday's game: %+v", got)
	}
	if w, err := s.DailyStanding(ctx, thursday, 0, true); err != nil || w != nil {
		t.Errorf("no player: %+v, %v", w, err)
	}
}
