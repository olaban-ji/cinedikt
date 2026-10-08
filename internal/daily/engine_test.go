package daily

import (
	"errors"
	"slices"
	"testing"
)

// TestFlipCostFollowsTheRating is the price table: twenty for the
// worst rated, eighty for the best, in fives between, with a half
// rounded up as the prototype's Math.round does it.
func TestFlipCostFollowsTheRating(t *testing.T) {
	for _, c := range []struct {
		rating float64
		want   int
	}{
		{1.0, 20}, {4.0, 20}, {4.6, 20}, {4.7, 25}, {5.0, 25}, {5.1, 30}, {5.5, 35},
		{6.3, 45}, {6.9, 50}, {7.0, 55}, {7.3, 55}, {7.4, 60}, {8.1, 65}, {8.2, 70},
		{8.6, 75}, {8.9, 75}, {9.0, 80}, {9.9, 80},
	} {
		if got := FlipCost(c.rating); got != c.want {
			t.Errorf("FlipCost(%.1f) = %d, want %d", c.rating, got, c.want)
		}
	}
}

func TestEachWrongGuessCostsFiftyMoreThanTheLast(t *testing.T) {
	for wrong, want := range []int{100, 150, 200, 250} {
		if got := NextWrong(wrong); got != want {
			t.Errorf("NextWrong(%d) = %d, want %d", wrong, got, want)
		}
	}
}

func TestAGameStartsWithAThousandAndTheStartingCardsUp(t *testing.T) {
	s := Replay(matrix(), nil)
	if s.Pts != Start || s.Done {
		t.Errorf("state = %+v", s)
	}
	for _, id := range []string{"c2", "c3", "c4"} {
		if !s.Up(id) {
			t.Errorf("starting card %s is face down", id)
		}
	}
	if s.Up("c1") {
		t.Error("a card that is not a starting card is face up")
	}
}

// TestACardIsTurnedOverOnceAndOnlyWhenPaidFor: its price is its
// rating's, a card already up cannot be bought again, a card that is
// not on the board is not a move, and one the points cannot cover is
// refused without changing anything.
func TestACardIsTurnedOverOnceAndOnlyWhenPaidFor(t *testing.T) {
	g := play(t, matrix())
	s := g.do(KindFlip, "c8")
	if s.Pts != Start-75 || !s.Up("c8") {
		t.Errorf("after turning over an 8.9: %d points, up %v", s.Pts, s.Up("c8"))
	}
	for _, c := range []struct {
		arg  string
		want error
	}{
		{"c8", ErrKnown}, {"c2", ErrKnown}, {"c99", ErrBad}, {"tt0133093", ErrBad},
	} {
		if err := g.try(KindFlip, c.arg); !errors.Is(err, c.want) {
			t.Errorf("flip %s: %v, want %v", c.arg, err, c.want)
		}
	}
	// The close relative costs what its rating costs, like any card.
	if s := g.do(KindFlip, "c1"); s.Pts != Start-75-55 {
		t.Errorf("after the relative: %d points", s.Pts)
	}

	poor := play(t, matrix())
	poor.do(KindYear, "")
	poor.do(KindDirector, "")
	poor.do(KindActor, "")
	poor.do(KindActor, "")
	poor.do(KindActor, "")
	poor.do(KindGuess, "tt0034583")
	if s := poor.state(); s.Pts != 100 {
		t.Fatalf("set-up spent to %d, want 100", s.Pts)
	}
	poor.do(KindGenres, "")
	if err := poor.try(KindFlip, "c7"); !errors.Is(err, ErrPoints) {
		t.Errorf("a 70-point card with 20 left: %v, want points", err)
	}
	if s := poor.do(KindFlip, "c10"); s.Pts != 0 || s.Done {
		t.Errorf("spending the last point on a card: %+v; the game goes on until a guess", s)
	}
}

func TestADirectorBuysEveryDirectorStillUnknown(t *testing.T) {
	g := play(t, matrix())
	s := g.do(KindDirector, "")
	if s.Pts != Start-DirectorCost || !slices.Equal(s.Bought, []int{0, 1}) {
		t.Errorf("bought %v for %d", s.Bought, Start-s.Pts)
	}
	if err := g.try(KindDirector, ""); !errors.Is(err, ErrKnown) {
		t.Errorf("a second director: %v, want known", err)
	}
}

// TestAnActorIsTheNextInBillingNobodyKnows: a guess that found Keanu
// leaves him unsold, and the buy moves on to Fishburne.
func TestAnActorIsTheNextInBillingNobodyKnows(t *testing.T) {
	g := play(t, matrix())
	g.do(KindGuess, "tt0111257")
	s := g.do(KindActor, "")
	if !slices.Equal(s.Found, []int{2}) || !slices.Equal(s.Bought, []int{3}) {
		t.Errorf("found %v, bought %v; want Keanu found and Fishburne bought", s.Found, s.Bought)
	}
	for range 4 {
		g.do(KindActor, "")
	}
	if err := g.try(KindActor, ""); !errors.Is(err, ErrKnown) {
		t.Errorf("an actor with none left: %v, want known", err)
	}
	// Buying never sells a director as an actor.
	if s := g.state(); slices.Contains(s.Bought, 0) || slices.Contains(s.Bought, 1) {
		t.Errorf("bought %v, which has a director in it", s.Bought)
	}
}

// TestGenresAndTheYearAreBoughtOnce, the year for 200 points, and
// "story", the clue the year replaced, is no move at all.
func TestGenresAndTheYearAreBoughtOnce(t *testing.T) {
	g := play(t, matrix())
	s := g.do(KindGenres, "")
	s = g.do(KindYear, "")
	if !s.Genres || !s.Year || s.Pts != Start-80-200 {
		t.Errorf("state = %+v", s)
	}
	if m := g.moves[1]; m.Kind != KindYear || m.Cost != 200 || m.Arg != "" {
		t.Errorf("the year was recorded as %+v", m)
	}
	for _, kind := range []string{KindGenres, KindYear} {
		if err := g.try(kind, ""); !errors.Is(err, ErrKnown) {
			t.Errorf("%s again: %v, want known", kind, err)
		}
	}
	for _, kind := range []string{"trailer", "story"} {
		if err := g.try(kind, ""); !errors.Is(err, ErrBad) {
			t.Errorf("%s: %v, want bad", kind, err)
		}
	}
}

// TestTheYearNeedsTwoHundredPoints: with exactly 200 left it is bought
// and spends the last of them; with 170 it is refused, and nothing
// changes.
func TestTheYearNeedsTwoHundredPoints(t *testing.T) {
	g := play(t, matrix())
	g.do(KindDirector, "")
	for range 3 {
		g.do(KindActor, "")
	}
	for _, card := range []string{"c8", "c7", "c5"} {
		g.do(KindFlip, card)
	}
	if s := g.state(); s.Pts != 200 {
		t.Fatalf("set-up spent to %d, want 200", s.Pts)
	}
	if s := g.do(KindYear, ""); !s.Year || s.Pts != 0 || s.Done {
		t.Errorf("the year with 200 left: %+v", s)
	}

	poor := play(t, matrix())
	poor.do(KindDirector, "")
	for range 4 {
		poor.do(KindActor, "")
	}
	poor.do(KindGenres, "")
	if err := poor.try(KindYear, ""); !errors.Is(err, ErrPoints) {
		t.Errorf("the year with 170 left: %v, want points", err)
	}
	if s := poor.state(); s.Year || s.Pts != 170 || len(poor.moves) != 6 {
		t.Errorf("after the refusal: %+v, %d moves", s, len(poor.moves))
	}
}

// TestAKindTheEngineDoesNotKnowIsTakenAsNothing: a game holding a move
// of a kind that is no longer one, as "story" no longer is, replays as
// though the move cost nothing and showed nothing, with no line in the
// log, and its seq still counts it, so the game can go on. No game was
// ever kept with one; this is the clean refusal, not a reader of old
// games.
func TestAKindTheEngineDoesNotKnowIsTakenAsNothing(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindGenres, "")
	g.moves = append(g.moves, Move{Seq: 2, Key: "key-story", Kind: "story", Cost: 300})
	s := g.do(KindFlip, "c9")
	if s.Pts != Start-80-60 || s.Done || s.Year {
		t.Errorf("replayed: %+v", s)
	}
	game := Render(p, g.record(), live)
	var types []string
	for _, e := range game.Log {
		types = append(types, e.Type)
	}
	if !slices.Equal(types, []string{"start", "genres", "flip"}) || game.Seq != 3 || game.Pts != Start-80-60 {
		t.Errorf("log %v, seq %d, %d points", types, game.Seq, game.Pts)
	}
}

// TestAWrongGuessFindsItsPeopleAndTurnsItsCardUp: Speed is on the board
// through Keanu, so guessing it finds him and turns its card over, and
// the next wrong guess costs fifty more.
func TestAWrongGuessFindsItsPeopleAndTurnsItsCardUp(t *testing.T) {
	g := play(t, matrix())
	s := g.do(KindGuess, "tt0111257")
	if s.Pts != Start-100 || s.Wrong != 1 || s.NextCost() != 150 {
		t.Errorf("after one wrong guess: %d points, %d wrong, next %d", s.Pts, s.Wrong, s.NextCost())
	}
	if !s.Up("c5") || !slices.Equal(s.Found, []int{2}) {
		t.Errorf("Speed up %v, found %v", s.Up("c5"), s.Found)
	}
	m := g.moves[0]
	if m.Guess == nil || m.Guess.YearHint != "newer" || m.Guess.RatingHint != "higher" || m.Cost != 100 {
		t.Errorf("recorded %+v, %+v", m, m.Guess)
	}
	// A movie off the board shares nobody and turns nothing over.
	s = g.do(KindGuess, "tt0034583")
	if s.Pts != Start-100-150 || len(s.Found) != 1 {
		t.Errorf("after Casablanca: %+v", s)
	}
	if err := g.try(KindGuess, "tt0111257"); !errors.Is(err, ErrKnown) {
		t.Errorf("Speed again: %v, want known", err)
	}
	if err := g.try(KindGuess, "tt0000404"); !errors.Is(err, ErrUnknown) {
		t.Errorf("a movie the catalog lacks: %v, want unknown", err)
	}
}

// TestAWrongGuessIsNeverRefusedForPoints: one that cannot be paid for
// takes the game to nothing and ends it.
func TestAWrongGuessIsNeverRefusedForPoints(t *testing.T) {
	g := play(t, matrix())
	g.do(KindYear, "")
	g.do(KindDirector, "")
	for range 4 {
		g.do(KindActor, "")
	}
	if s := g.state(); s.Pts != 50 {
		t.Fatalf("set-up spent to %d, want 50", s.Pts)
	}
	s := g.do(KindGuess, "tt0034583")
	if !s.Done || !s.Out || s.Won || s.Pts != 0 {
		t.Errorf("after a 100-point guess with 50 left: %+v", s)
	}
	if err := g.try(KindGuess, "tt0133093"); !errors.Is(err, ErrDone) {
		t.Errorf("a guess after the end: %v, want done", err)
	}
}

func TestTheRightGuessCostsNothingAndKeepsThePoints(t *testing.T) {
	g := play(t, matrix())
	g.do(KindFlip, "c9")
	s := g.do(KindGuess, "tt0133093")
	if !s.Done || !s.Won || s.Pts != Start-60 {
		t.Errorf("after the right guess: %+v", s)
	}
	if m := g.moves[1]; m.Cost != 0 || m.Guess != nil {
		t.Errorf("the right guess was recorded as %+v", m)
	}
}

func TestShowingTheAnswerScoresNothing(t *testing.T) {
	g := play(t, matrix())
	g.do(KindFlip, "c9")
	s := g.do(KindReveal, "")
	if !s.Done || !s.GaveUp || s.Won || s.Pts != 0 {
		t.Errorf("after showing the answer: %+v", s)
	}
	for _, kind := range []string{KindFlip, KindDirector, KindGenres, KindReveal} {
		if err := g.try(kind, "c1"); !errors.Is(err, ErrDone) {
			t.Errorf("%s after the end: %v, want done", kind, err)
		}
	}
}

// TestHintsSayWhereTheAnswerSits from the guess: a 1994 guess for a 1999
// answer says "newer", and a 7.3 for an 8.7 says "higher". Nothing to
// compare says nothing.
func TestHintsSayWhereTheAnswerSits(t *testing.T) {
	for _, c := range []struct {
		guess, answer int
		want          string
	}{
		{1994, 1999, "newer"}, {2003, 1999, "older"}, {1999, 1999, "same"}, {0, 1999, ""},
	} {
		if got := YearHint(c.guess, c.answer); got != c.want {
			t.Errorf("YearHint(%d, %d) = %q, want %q", c.guess, c.answer, got, c.want)
		}
	}
	for _, c := range []struct {
		guess  *float64
		answer float64
		want   string
	}{
		{ptr(7.3), 8.7, "higher"}, {ptr(8.9), 8.7, "lower"}, {ptr(8.7), 8.7, "same"},
		{ptr(0.1 + 0.2 + 6.9), 7.2, "same"}, {nil, 8.7, ""},
	} {
		if got := RatingHint(c.guess, c.answer); got != c.want {
			t.Errorf("RatingHint(%v, %.1f) = %q, want %q", c.guess, c.answer, got, c.want)
		}
	}
}
