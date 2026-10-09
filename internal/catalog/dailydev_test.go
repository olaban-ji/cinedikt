package catalog

import (
	"context"
	"errors"
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

// TestARepickDealsTheDayAgainFromAnotherAnswer: development's Play
// again keeps the puzzle's number and day and deals it from another
// candidate, never the answer it had nor another day's, exactly as the
// job would have dealt that candidate's board; every game of it goes
// with its moves, and the players, the day before and its games stay
// as they were. Pressed again and again, it never deals back the answer
// it is replacing.
func TestARepickDealsTheDayAgainFromAnotherAnswer(t *testing.T) {
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
	if games, moves, _ := dailyCounts(t, s, today.No); games != 2 || moves != 2 {
		t.Fatalf("before the re-pick today has %d games and %d moves", games, moves)
	}

	if err := s.RepickDailyPuzzle(ctx, today.No); err != nil {
		t.Fatal(err)
	}
	got, err := s.DailyPuzzleNo(ctx, today.No)
	if err != nil {
		t.Fatal(err)
	}
	if got.No != today.No || !got.Day.Equal(today.Day) {
		t.Errorf("the re-pick is No. %d on %s, want No. %d on %s", got.No, daily.DayString(got.Day), today.No, daily.DayString(today.Day))
	}
	if got.Answer.ID == today.Answer.ID || got.Answer.ID == yesterday.Answer.ID || !strings.HasPrefix(got.Answer.ID, "tt99001") {
		t.Errorf("the re-pick's answer is %s; it was %s, and yesterday's is %s", got.Answer.ID, today.Answer.ID, yesterday.Answer.ID)
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
		t.Errorf("the re-pick kept\n%+v\nwhere the job deals\n%+v", got, want)
	}
	if len(got.Movies) != dailySheet || len(got.Cast) != daily.Slots || !slices.ContainsFunc(got.Movies, func(m daily.Movie) bool { return m.ID == got.Answer.ID }) {
		t.Errorf("the re-pick's puzzle: %d movies, %d cast", len(got.Movies), len(got.Cast))
	}

	if games, moves, people := dailyCounts(t, s, today.No); games != 0 || moves != 0 || people != 2 {
		t.Errorf("after the re-pick today has %d games and %d moves, and there are %d players", games, moves, people)
	}
	if games, moves, _ := dailyCounts(t, s, yesterday.No); games != 2 || moves != 2 {
		t.Errorf("after the re-pick yesterday has %d games and %d moves", games, moves)
	}
	if again, err := s.DailyPuzzleNo(ctx, yesterday.No); err != nil || !reflect.DeepEqual(again, yesterday) {
		t.Errorf("yesterday's puzzle changed: %+v, %v", again, err)
	}
	if rec, err := s.DailyGame(ctx, players[0].ID, yesterday.No); err != nil || rec == nil || len(rec.Moves) != 1 {
		t.Errorf("yesterday's game after the re-pick: %+v, %v", rec, err)
	}

	deals := []string{got.Answer.ID}
	for range 12 {
		was := got.Answer.ID
		if err := s.RepickDailyPuzzle(ctx, today.No); err != nil {
			t.Fatal(err)
		}
		if got, err = s.DailyPuzzleNo(ctx, today.No); err != nil {
			t.Fatal(err)
		}
		if got.Answer.ID == was || got.Answer.ID == yesterday.Answer.ID {
			t.Fatalf("a re-pick of %s dealt %s; yesterday's is %s", was, got.Answer.ID, yesterday.Answer.ID)
		}
		deals = append(deals, got.Answer.ID)
	}
	// Any order fixed in advance, the day's or another, deals the first
	// two of it in turn, since each press leaves out only the answer it
	// replaces. Drawn afresh, each press has at least three others to
	// choose from in the fixture, so thirteen deals that only swap two
	// movies back and forth would happen about once in 170,000 runs.
	swapped := true
	for i := 2; i < len(deals); i++ {
		swapped = swapped && deals[i] == deals[i-2]
	}
	if swapped {
		t.Errorf("thirteen re-picks only swapped two movies: %v", deals)
	}
}

// TestARepickWithNoOtherAnswerChangesNothing: with every candidate an
// answer within ninety days, there is no other movie for the day, and
// the puzzle and its games are as they were. A puzzle that does not
// exist is not found.
func TestARepickWithNoOtherAnswerChangesNothing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	dailyFixture(t, s)
	now := oct8
	if err := dailyJob(s, &now).Run(ctx); err != nil {
		t.Fatal(err)
	}
	today, err := s.DailyPuzzle(ctx, oct8)
	if err != nil {
		t.Fatal(err)
	}
	gamesOn(t, s, today, oct8, twoPlayers(t, s)...)
	if err := s.RepickDailyPuzzle(ctx, today.No); !errors.Is(err, ErrNoOtherAnswer) {
		t.Fatalf("a re-pick with every candidate used: %v, want ErrNoOtherAnswer", err)
	}
	if again, err := s.DailyPuzzleNo(ctx, today.No); err != nil || !reflect.DeepEqual(again, today) {
		t.Errorf("the puzzle changed: %+v, %v", again, err)
	}
	if games, moves, _ := dailyCounts(t, s, today.No); games != 2 || moves != 2 {
		t.Errorf("after a re-pick that found nothing today has %d games and %d moves", games, moves)
	}
	if err := s.RepickDailyPuzzle(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("a re-pick of No. 99: %v, want ErrNotFound", err)
	}
}
