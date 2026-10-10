package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"cinedikt/internal/daily"
)

// gamesOn gives each player a game of p started at at, in UTC, with the
// next name bought a minute in: a game with a move, for a re-pick to
// delete or to leave alone.
func gamesOn(t *testing.T, s *Store, p *daily.Puzzle, at time.Time, players ...daily.Player) {
	t.Helper()
	ctx := context.Background()
	for _, pl := range players {
		if _, err := s.StartDailyGame(ctx, pl.ID, p.No, at, time.UTC); err != nil {
			t.Fatal(err)
		}
		r := daily.Request{Key: "next-" + pl.Name[:3], Seq: 0, Kind: daily.KindNext}
		if _, err := s.DailyAct(ctx, pl.ID, p, r, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
}

// dailyCounts is how many games and moves are kept of puzzle No. no,
// and how many players there are.
func dailyCounts(t *testing.T, s *Store, no int) (games, moves, players int) {
	t.Helper()
	if err := s.pool.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM meta.daily_games WHERE no = $1),
		       (SELECT count(*) FROM meta.daily_moves m JOIN meta.daily_games g ON g.id = m.game WHERE g.no = $1),
		       (SELECT count(*) FROM meta.daily_players)`, no).Scan(&games, &moves, &players); err != nil {
		t.Fatal(err)
	}
	return games, moves, players
}

func twoPlayers(t *testing.T, s *Store) []daily.Player {
	t.Helper()
	var out []daily.Player
	for i, name := range []string{"Trinity Kimble", "Ellen Gump"} {
		p, err := s.CreateDailyPlayer(context.Background(), daily.TokenHash(daily.NewToken()), name, 100+i)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// finish gives player's game of p up, a move after the one gamesOn
// made, so it is on the boards with nothing.
func finish(t *testing.T, s *Store, p *daily.Puzzle, player daily.Player) {
	t.Helper()
	r := daily.Request{Key: "reveal-" + player.Name[:3], Seq: 1, Kind: daily.KindReveal}
	if _, err := s.DailyAct(context.Background(), player.ID, p, r, oct8.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

// TestADealIsAMovieOfOnesOwn: development's Play again deals one player
// another answer for the day, under its number on its day, exactly as
// the job would have dealt that candidate's board, and never the day's
// own answer nor another day's. Their game of it goes; nobody else's,
// and nobody else's movie, changes. Dealt again and again, it never
// deals back the movie it is replacing.
func TestADealIsAMovieOfOnesOwn(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	today := todaysPuzzle(t, s)
	yesterday, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	players := twoPlayers(t, s)
	gamesOn(t, s, yesterday, oct8.AddDate(0, 0, -1), players...)
	gamesOn(t, s, today, oct8, players...)
	me, them := players[0], players[1]

	if _, err := s.DailyDeal(ctx, me.ID, today.No); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deal before any: %v, want ErrNotFound", err)
	}
	if err := s.DealDailyPuzzle(ctx, me.ID, today.No); err != nil {
		t.Fatal(err)
	}
	got, err := s.DailyDeal(ctx, me.ID, today.No)
	if err != nil {
		t.Fatal(err)
	}
	if got.No != today.No || !got.Day.Equal(today.Day) {
		t.Errorf("the deal is No. %d on %s, want No. %d on %s", got.No, daily.DayString(got.Day), today.No, daily.DayString(today.Day))
	}
	if got.Answer.ID == today.Answer.ID || got.Answer.ID == yesterday.Answer.ID || !strings.HasPrefix(got.Answer.ID, "tt99001") {
		t.Errorf("the deal's answer is %s; the day's is %s, and yesterday's is %s", got.Answer.ID, today.Answer.ID, yesterday.Answer.ID)
	}
	// The puzzle is the one the job makes from that answer, cast,
	// directors, sheets and all.
	cands, err := s.dailyCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(cands, func(c daily.Candidate) bool { return c.ID == got.Answer.ID })
	if i < 0 {
		t.Fatalf("%s is not a candidate", got.Answer.ID)
	}
	want, err := s.dailyPuzzleOf(ctx, nil, today.No, today.Day, cands[i])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the deal kept\n%+v\nwhere the job deals\n%+v", got, want)
	}

	// Everyone else's movie and game are as they were; only mine of
	// today went.
	for _, p := range []*daily.Puzzle{today, yesterday} {
		if again, err := s.DailyPuzzleNo(ctx, p.No); err != nil || !reflect.DeepEqual(again, p) {
			t.Errorf("puzzle %d changed: %+v, %v", p.No, again, err)
		}
	}
	if _, err := s.DailyDeal(ctx, them.ID, today.No); !errors.Is(err, ErrNotFound) {
		t.Errorf("the other player's deal: %v, want ErrNotFound", err)
	}
	for _, c := range []struct {
		who   daily.Player
		p     *daily.Puzzle
		moves int
	}{{me, today, -1}, {them, today, 1}, {me, yesterday, 1}, {them, yesterday, 1}} {
		rec, err := s.DailyGame(ctx, c.who.ID, c.p.No)
		if err != nil {
			t.Fatal(err)
		}
		if (c.moves < 0) != (rec == nil) || (rec != nil && len(rec.Moves) != c.moves) {
			t.Errorf("%s's game of %d after the deal: %+v", c.who.Name, c.p.No, rec)
		}
	}

	deals := []string{got.Answer.ID}
	for range 12 {
		was := got.Answer.ID
		if err := s.DealDailyPuzzle(ctx, me.ID, today.No); err != nil {
			t.Fatal(err)
		}
		if got, err = s.DailyDeal(ctx, me.ID, today.No); err != nil {
			t.Fatal(err)
		}
		if got.Answer.ID == was || got.Answer.ID == today.Answer.ID || got.Answer.ID == yesterday.Answer.ID {
			t.Fatalf("a deal in place of %s dealt %s; the day's is %s and yesterday's %s", was, got.Answer.ID, today.Answer.ID, yesterday.Answer.ID)
		}
		deals = append(deals, got.Answer.ID)
	}
	// Any order fixed in advance deals the first two of it in turn,
	// since each deal leaves out only the one it replaces. Drawn afresh,
	// each has at least two others to choose from in the fixture, so
	// thirteen deals that only swap two movies back and forth would
	// happen about once in four thousand runs.
	swapped := true
	for i := 2; i < len(deals); i++ {
		swapped = swapped && deals[i] == deals[i-2]
	}
	if swapped {
		t.Errorf("thirteen deals only swapped two movies: %v", deals)
	}
}

// TestADealWithNoOtherAnswerChangesNothing: with every candidate an
// answer within ninety days, there is no other movie for the day, and
// the player is dealt nothing and keeps their game. A puzzle that does
// not exist is not found.
func TestADealWithNoOtherAnswerChangesNothing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	// Launched on the 7th, with the 16th added the next day: the ten
	// candidates are the ten days' answers.
	now := oct8.AddDate(0, 0, -1)
	job := dailyJob(s, &now)
	for _, at := range []time.Time{now, oct8} {
		now = at
		if err := job.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	today, err := s.DailyPuzzle(ctx, oct8)
	if err != nil {
		t.Fatal(err)
	}
	players := twoPlayers(t, s)
	gamesOn(t, s, today, oct8, players...)
	if err := s.DealDailyPuzzle(ctx, players[0].ID, today.No); !errors.Is(err, ErrNoOtherAnswer) {
		t.Fatalf("a deal with every candidate used: %v, want ErrNoOtherAnswer", err)
	}
	if _, err := s.DailyDeal(ctx, players[0].ID, today.No); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deal that found nothing kept one: %v", err)
	}
	if games, moves, _ := dailyCounts(t, s, today.No); games != 2 || moves != 2 {
		t.Errorf("after a deal that found nothing today has %d games and %d moves", games, moves)
	}
	if err := s.DealDailyPuzzle(ctx, players[0].ID, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deal of No. 99: %v, want ErrNotFound", err)
	}
}

// TestAPracticeGameIsOnNoBoardCountOrFigure: a game played on a movie
// of one's own says nothing of the day's. The title screen's count, both
// tabs of the board, the week's standing, the Telegram figures and the
// players a later board lists read only the game played on the day's own
// movie, and the practice player is nobody on them.
func TestAPracticeGameIsOnNoBoardCountOrFigure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	today := todaysPuzzle(t, s)
	players := twoPlayers(t, s)
	real, practice := players[0], players[1]
	if err := s.DealDailyPuzzle(ctx, practice.ID, today.No); err != nil {
		t.Fatal(err)
	}
	own, err := s.DailyDeal(ctx, practice.ID, today.No)
	if err != nil {
		t.Fatal(err)
	}
	gamesOn(t, s, today, oct8, real)
	gamesOn(t, s, own, oct8, practice)
	finish(t, s, today, real)
	finish(t, s, own, practice)

	if n, err := s.DailyPlayed(ctx, today.No); err != nil || n != 1 {
		t.Errorf("played today: %d, %v; want the one real game", n, err)
	}
	for _, tab := range []string{daily.TabToday, daily.TabWeek} {
		b, err := s.DailyBoard(ctx, today, tab, real.ID)
		if err != nil {
			t.Fatal(err)
		}
		if b.Total != 1 || b.You == nil || len(b.Rows) != 1 || b.Rows[0].Name != real.Name {
			t.Errorf("%s's board for the real player: %+v", tab, b)
		}
		// The practice player sees the board as a stranger does: a board
		// lists those who have played before and whoever is looking, and
		// they are neither, their game being no game of the day's.
		mine, err := s.DailyBoard(ctx, today, tab, practice.ID)
		if err != nil {
			t.Fatal(err)
		}
		nobody, err := s.DailyBoard(ctx, today, tab, 0)
		if err != nil {
			t.Fatal(err)
		}
		if mine.You != nil || len(mine.Rows) != 0 || !reflect.DeepEqual(mine, nobody) {
			t.Errorf("%s's board for the practice player:\n%+v\nfor nobody:\n%+v", tab, mine, nobody)
		}
		if tab == daily.TabToday {
			sum := 0
			for _, n := range mine.Chart {
				sum += n
			}
			if sum != 1 {
				t.Errorf("today's chart counts %d games, want the one real game: %v", sum, mine.Chart)
			}
		}
	}
	if w, err := s.DailyStanding(ctx, today, practice.ID, true); err != nil || w != nil {
		t.Errorf("the practice player's week: %+v, %v", w, err)
	}
	d, err := s.DailyDayStats(ctx, oct8)
	if err != nil || d == nil || d.Played != 1 || d.Finished != 1 {
		t.Errorf("the day's figures: %+v, %v", d, err)
	}
	// The next puzzle's board lists those who finished an earlier one:
	// the practice game is none.
	var listed []int64
	rows, err := s.pool.Query(ctx, `WITH `+eligibleSQL+` SELECT player FROM eligible ORDER BY player`, today.No+1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		listed = append(listed, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(listed, []int64{real.ID}) {
		t.Errorf("listed after today: %v, want only %d", listed, real.ID)
	}
}

// TestPlayAgainKeepsItsCandidatesUntilTheCatalogMoves: the read Play
// again deals from, the whole of a press's wait, is kept: a second press
// deals from the same list; a new catalog published, or the list grown
// older than DealCandidatesLife, reads it again; and opening the Daily
// reads it in the background, so the first press need not wait.
func TestPlayAgainKeepsItsCandidatesUntilTheCatalogMoves(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	publish := func() {
		t.Helper()
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO meta.generation (id, files, row_counts, imported_at) VALUES (1, '{}', '{}', clock_timestamp())
			ON CONFLICT (id) DO UPDATE SET imported_at = clock_timestamp()`); err != nil {
			t.Fatal(err)
		}
	}
	publish()
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM meta.generation`) })
	read := func() []daily.Candidate {
		t.Helper()
		cands, err := s.dealCandidates(ctx)
		if err != nil || len(cands) == 0 {
			t.Fatalf("candidates: %d, %v", len(cands), err)
		}
		return cands
	}
	same := func(a, b []daily.Candidate) bool { return &a[0] == &b[0] }

	first := read()
	if again := read(); !same(first, again) {
		t.Error("a second press read the candidates again")
	}
	publish()
	fresh := read()
	if same(first, fresh) {
		t.Error("a new catalog kept the old candidates")
	}
	s.deals.mu.Lock()
	s.deals.read = time.Now().Add(-DealCandidatesLife)
	s.deals.mu.Unlock()
	if later := read(); same(fresh, later) {
		t.Errorf("candidates older than %v were kept", DealCandidatesLife)
	}

	s.deals.mu.Lock()
	s.deals.cands = nil
	s.deals.mu.Unlock()
	s.WarmDailyDeals()
	deadline := time.Now().Add(30 * time.Second)
	for {
		s.deals.mu.Lock()
		warm := s.deals.cands != nil && s.deals.busy == nil
		s.deals.mu.Unlock()
		if warm {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the candidates were never read in the background")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestADealTakesAMovieWithItsColourFirst: a candidate whose poster colour
// is not known has its poster fetched to work it out, and a poster that
// does not answer held a press for the fetch's whole deadline. So a deal
// draws from the coloured first: here every other candidate's poster
// hangs, and each deal is the one coloured movie left, at once.
func TestADealTakesAMovieWithItsColourFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	today := todaysPuzzle(t, s)
	yesterday, err := s.DailyPuzzle(ctx, oct8.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	var keep string
	for g := 1; g <= dailyCandidates; g++ {
		id := fmt.Sprintf("tt99001%02d", g)
		if id != today.Answer.ID && id != yesterday.Answer.ID {
			keep = id
			break
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * DealColourFetch)
	}))
	t.Cleanup(srv.Close)
	if _, err := s.pool.Exec(ctx, `UPDATE meta.posters SET colour = NULL, poster_url = $1 || '/' || tconst || '.jpg'
		WHERE tconst LIKE 'tt99001%' AND tconst <> $2`, srv.URL, keep); err != nil {
		t.Fatal(err)
	}
	for i, player := range twoPlayers(t, s) {
		began := time.Now()
		if err := s.DealDailyPuzzle(ctx, player.ID, today.No); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(began); took > DealColourFetch {
			t.Errorf("deal %d took %v, waiting on a poster", i+1, took)
		}
		got, err := s.DailyDeal(ctx, player.ID, today.No)
		if err != nil {
			t.Fatal(err)
		}
		if got.Answer.ID != keep {
			t.Errorf("deal %d dealt %s, want %s, the one with its colour", i+1, got.Answer.ID, keep)
		}
	}
}
