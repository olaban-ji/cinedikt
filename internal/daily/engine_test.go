package daily

import (
	"errors"
	"slices"
	"testing"
)

func TestEachWrongGuessCostsFiftyMoreThanTheLast(t *testing.T) {
	for wrong, want := range []int{100, 150, 200, 250} {
		if got := NextWrong(wrong); got != want {
			t.Errorf("NextWrong(%d) = %d, want %d", wrong, got, want)
		}
	}
}

// TestAGameStartsWithAThousandAndTheSixthBilledShowing, and nobody else.
func TestAGameStartsWithAThousandAndTheSixthBilledShowing(t *testing.T) {
	s := Replay(matrix(), nil)
	if s.Pts != Start || s.Done || s.Wrong != 0 || len(s.Facts) != 0 || len(s.Overlaps) != 0 {
		t.Errorf("state = %+v", s)
	}
	if got := shownSlots(s); !slices.Equal(got, []int{0}) || s.Seen() != 1 || s.FirstHidden() != 1 {
		t.Errorf("showing %v, seen %d, first hidden %d; want the sixth-billed alone", got, s.Seen(), s.FirstHidden())
	}
	for _, slot := range []int{-1, 6} {
		if s.Shown(slot) {
			t.Errorf("slot %d, which is none, is showing", slot)
		}
	}
}

// TestNextShowsTheFirstHiddenNameForAHundred, up to the star, and then
// there is no next name to buy.
func TestNextShowsTheFirstHiddenNameForAHundred(t *testing.T) {
	g := play(t, matrix())
	for i := 1; i < Slots; i++ {
		s := g.do(KindNext, "")
		next := i + 1
		if next == Slots {
			next = -1
		}
		if s.Pts != Start-NameCost*i || !s.Shown(i) || s.via[i] != ViaNext || s.FirstHidden() != next {
			t.Errorf("after %d next names: %d points, showing %v, first hidden %d", i, s.Pts, shownSlots(s), s.FirstHidden())
		}
	}
	if m := g.moves[0]; m.Cost != 100 || m.Arg != "" || m.Kind != KindNext {
		t.Errorf("a next name was recorded as %+v", m)
	}
	if err := g.try(KindNext, ""); !errors.Is(err, ErrKnown) {
		t.Errorf("a next name with all six showing: %v, want known", err)
	}
}

// TestEachFactIsBoughtOnceAtItsPrice: length and rating for fifty, the
// genre, the decade and the five years for a hundred, the director for
// two hundred and fifty. The five years only after the decade, and the
// old game's clues are no moves at all.
func TestEachFactIsBoughtOnceAtItsPrice(t *testing.T) {
	g := play(t, matrix())
	if err := g.try(KindYears, ""); !errors.Is(err, ErrBad) {
		t.Errorf("the five years before the decade: %v, want bad", err)
	}
	spent := 0
	for _, c := range []struct {
		kind string
		cost int
	}{
		{KindLength, 50}, {KindRating, 50}, {KindGenre, 100}, {KindDecade, 100}, {KindYears, 100}, {KindDirector, 250},
	} {
		s := g.do(c.kind, "x")
		spent += c.cost
		if s.Pts != Start-spent || !s.Facts[c.kind] {
			t.Errorf("after %s: %d points, facts %v", c.kind, s.Pts, s.Facts)
		}
		if m := g.moves[len(g.moves)-1]; m.Cost != c.cost || m.Arg != "" {
			t.Errorf("%s was recorded as %+v", c.kind, m)
		}
	}
	for _, kind := range factKinds {
		if err := g.try(kind, ""); !errors.Is(err, ErrKnown) {
			t.Errorf("%s again: %v, want known", kind, err)
		}
	}
	for _, kind := range []string{"flip", "actor", "genres", "year", "story", "trailer", ""} {
		if err := g.try(kind, ""); !errors.Is(err, ErrBad) {
			t.Errorf("%q: %v, want bad", kind, err)
		}
	}
	if s := g.state(); s.Pts != 350 || shownSlots(s)[0] != 0 || len(shownSlots(s)) != 1 {
		t.Errorf("facts changed who is showing: %+v", s)
	}
}

// TestAPurchaseMustLeaveAPoint: a buy is refused when the points left
// are no more than it costs, a hundred for the next name with a hundred
// left among them; a wrong guess with no more than its price left ends
// the game at nothing instead.
func TestAPurchaseMustLeaveAPoint(t *testing.T) {
	g := play(t, matrix())
	for _, kind := range factKinds {
		g.do(kind, "")
	}
	if s := g.state(); s.Pts != 350 {
		t.Fatalf("set-up spent to %d, want 350", s.Pts)
	}
	g.do(KindNext, "")
	if err := g.try(KindOverlap, "nm0001592"); !errors.Is(err, ErrPoints) {
		t.Errorf("an overlap for 250 with 250 left: %v, want points", err)
	}
	g.do(KindNext, "")
	if s := g.do(KindNext, ""); s.Pts != 50 || s.Done {
		t.Errorf("a next name with 150 left: %+v", s)
	}
	if err := g.try(KindNext, ""); !errors.Is(err, ErrPoints) {
		t.Errorf("a next name with 50 left: %v, want points", err)
	}
	if s := g.state(); s.Pts != 50 || len(g.moves) != 9 {
		t.Errorf("after the refusals: %+v, %d moves", s, len(g.moves))
	}

	exact := play(t, matrix())
	for _, kind := range factKinds {
		exact.do(kind, "")
	}
	exact.do(KindOverlap, "nm0001592")
	if s := exact.state(); s.Pts != 100 {
		t.Fatalf("set-up spent to %d, want 100", s.Pts)
	}
	if err := exact.try(KindNext, ""); !errors.Is(err, ErrPoints) {
		t.Errorf("a next name with exactly 100 left: %v, want points", err)
	}
	s := exact.do(KindGuess, "tt0034583")
	if !s.Done || !s.Out || s.Won || s.Pts != 0 {
		t.Errorf("a 100-point wrong guess with 100 left: %+v", s)
	}
	// It still showed the next name, as any wrong guess does: seen.
	if s.Seen() != 2 || s.via[1] != ViaGuess {
		t.Errorf("the guess that ran out showed %d names, Foster by %q", s.Seen(), s.via[1])
	}
	if m := exact.moves[len(exact.moves)-1]; m.Cost != 100 || m.Guess == nil {
		t.Errorf("the guess that ran out was recorded as %+v", m)
	}
	if err := exact.try(KindGuess, "tt0133093"); !errors.Is(err, ErrDone) {
		t.Errorf("a guess after the end: %v, want done", err)
	}
}

// TestAnOverlapIsOncePerPersonShowing: only for one of the six who is
// showing, never a director nor anyone hidden, and once each.
func TestAnOverlapIsOncePerPersonShowing(t *testing.T) {
	g := play(t, matrix())
	for _, who := range []string{"nm0287825", "nm0905154", "nm9999999", ""} {
		if err := g.try(KindOverlap, who); !errors.Is(err, ErrBad) {
			t.Errorf("an overlap for %q: %v, want bad", who, err)
		}
	}
	s := g.do(KindOverlap, "nm0001592")
	if s.Pts != Start-OverlapCost || !s.Overlapped("nm0001592") || !slices.Equal(s.Overlaps, []string{"nm0001592"}) {
		t.Errorf("after an overlap: %+v", s)
	}
	if err := g.try(KindOverlap, "nm0001592"); !errors.Is(err, ErrKnown) {
		t.Errorf("the same overlap again: %v, want known", err)
	}
	g.do(KindNext, "")
	if s := g.do(KindOverlap, "nm0287825"); !slices.Equal(s.Overlaps, []string{"nm0001592", "nm0287825"}) || s.Pts != 400 {
		t.Errorf("a second overlap, once she shows: %+v", s)
	}
}

// TestAWrongGuessFillsInTheCastItSharesThenShowsTheNextName: Memento
// credits Pantoliano, already showing, and Moss, slot 3, who is filled
// in through it; then the first name still hidden, Foster, shows. It
// says how warm it was, and the next wrong guess costs fifty more.
func TestAWrongGuessFillsInTheCastItSharesThenShowsTheNextName(t *testing.T) {
	p := matrix()
	g := play(t, p)
	s := g.do(KindGuess, "tt0209144")
	if s.Pts != Start-100 || s.Wrong != 1 || s.NextCost() != 150 || s.Done {
		t.Errorf("after one wrong guess: %d points, %d wrong, next %d", s.Pts, s.Wrong, s.NextCost())
	}
	if got := shownSlots(s); !slices.Equal(got, []int{0, 1, 3}) {
		t.Errorf("showing %v, want Pantoliano, Foster and Moss", got)
	}
	if s.via[3] != ViaGuess || s.from[3] != (Ref{ID: "tt0209144", Title: "Memento"}) {
		t.Errorf("Moss shows by %q from %+v", s.via[3], s.from[3])
	}
	if s.via[1] != ViaGuess || s.from[1] != (Ref{}) {
		t.Errorf("Foster shows by %q from %+v, want the next name after a guess", s.via[1], s.from[1])
	}
	m := g.moves[0]
	want := Guessed{Title: "Memento", Year: 2000, Shared: []int{0, 3}}
	if m.Guess == nil || m.Cost != 100 || m.Guess.Title != want.Title || m.Guess.Year != want.Year ||
		!slices.Equal(m.Guess.Shared, want.Shared) || m.Guess.SameDecade || m.Guess.SharesGenre || m.Guess.Warmth() != 2 {
		t.Errorf("recorded %+v, %+v", m, m.Guess)
	}

	// Speed shares the star, and the decade and a genre; Casablanca
	// nothing at all, and shows the next name all the same.
	s = g.do(KindGuess, "tt0111257")
	if got := shownSlots(s); !slices.Equal(got, []int{0, 1, 2, 3, 5}) || s.from[5].Title != "Speed" || s.Pts != 750 {
		t.Errorf("after Speed: showing %v, %d points", got, s.Pts)
	}
	if sp := g.moves[1].Guess; !sp.SameDecade || !sp.SharesGenre || !slices.Equal(sp.Shared, []int{5}) || sp.Warmth() != 2 {
		t.Errorf("Speed learned %+v", sp)
	}
	s = g.do(KindGuess, "tt0034583")
	if got := shownSlots(s); !slices.Equal(got, []int{0, 1, 2, 3, 4, 5}) || s.Pts != 550 || s.NextCost() != 250 {
		t.Errorf("after Casablanca: showing %v, %d points", got, s.Pts)
	}
	if c := g.moves[2].Guess; c.SameDecade || c.SharesGenre || len(c.Shared) != 0 || c.Warmth() != 0 {
		t.Errorf("Casablanca learned %+v", c)
	}
	// With everyone showing, a wrong guess still costs, and shows nobody.
	if s := g.do(KindGuess, "tt0076759"); s.Pts != 300 || s.Seen() != 6 {
		t.Errorf("a guess with everyone showing: %+v", s)
	}
	if err := g.try(KindGuess, "tt0111257"); !errors.Is(err, ErrKnown) {
		t.Errorf("Speed again: %v, want known", err)
	}
	if err := g.try(KindGuess, "tt0000404"); !errors.Is(err, ErrUnknown) {
		t.Errorf("a movie the catalog lacks: %v, want unknown", err)
	}
}

// TestHowWarmAGuessWasIsTheCastFirst: two for each of the six it shares,
// one for the decade and one for a genre; none cold, one or two warm,
// three or more hot. A guess with no year shares no decade.
func TestHowWarmAGuessWasIsTheCastFirst(t *testing.T) {
	for _, c := range []struct {
		g    Guessed
		want int
	}{
		{Guessed{}, 0},
		{Guessed{SameDecade: true}, 1},
		{Guessed{SharesGenre: true}, 1},
		{Guessed{SameDecade: true, SharesGenre: true}, 1},
		{Guessed{Shared: []int{4}}, 1},
		{Guessed{Shared: []int{4}, SharesGenre: true}, 2},
		{Guessed{Shared: []int{1, 4}}, 2},
	} {
		if got := c.g.Warmth(); got != c.want {
			t.Errorf("%+v is %d, want %d", c.g, got, c.want)
		}
	}
	g := play(t, matrix())
	g.do(KindGuess, "tt0120601")
	g.do(KindGuess, "tt9000002")
	if bjm := g.moves[0].Guess; !bjm.SameDecade || bjm.SharesGenre || bjm.Warmth() != 1 {
		t.Errorf("a 1999 comedy learned %+v", bjm)
	}
	if none := g.moves[1].Guess; none.SameDecade || none.SharesGenre || none.Year != 0 || none.Warmth() != 0 {
		t.Errorf("a movie with no year or genres learned %+v", none)
	}
}

func TestTheRightGuessWinsWithThePointsLeft(t *testing.T) {
	g := play(t, matrix())
	g.do(KindNext, "")
	g.do(KindDecade, "")
	s := g.do(KindGuess, "tt0133093")
	if !s.Done || !s.Won || s.Pts != 800 || s.Out || s.Seen() != 2 {
		t.Errorf("after the right guess: %+v", s)
	}
	if m := g.moves[2]; m.Cost != 0 || m.Guess != nil {
		t.Errorf("the right guess was recorded as %+v", m)
	}
	// Once it is over everyone is shown, though only two were seen.
	if got := shownSlots(s); len(got) != Slots {
		t.Errorf("at the end showing %v", got)
	}
}

func TestShowingTheAnswerScoresNothing(t *testing.T) {
	g := play(t, matrix())
	g.do(KindNext, "")
	s := g.do(KindReveal, "x")
	if !s.Done || !s.GaveUp || s.Won || s.Pts != 0 {
		t.Errorf("after showing the answer: %+v", s)
	}
	if m := g.moves[1]; m.Arg != "" || m.Cost != 0 {
		t.Errorf("the reveal was recorded as %+v", m)
	}
	for _, kind := range []string{KindNext, KindDirector, KindOverlap, KindGuess, KindReveal} {
		if err := g.try(kind, "nm0001592"); !errors.Is(err, ErrDone) {
			t.Errorf("%s after the end: %v, want done", kind, err)
		}
	}
}

// TestAKindTheEngineDoesNotKnowIsTakenAsNothing: a game holding a move
// of a kind that is no longer one replays as though the move cost
// nothing and showed nothing, with no line in the log, and its seq
// still counts it, so the game can go on. No game is kept with one;
// this is the clean refusal, not a reader of old games.
func TestAKindTheEngineDoesNotKnowIsTakenAsNothing(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindGenre, "")
	g.moves = append(g.moves, Move{Seq: 2, Key: "key-flip", Kind: "flip", Arg: "c7", Cost: 60})
	s := g.do(KindNext, "")
	if s.Pts != Start-100-100 || s.Done || !slices.Equal(shownSlots(s), []int{0, 1}) {
		t.Errorf("replayed: %+v", s)
	}
	game := Render(p, g.record(), Live{})
	var types []string
	for _, e := range game.Log {
		types = append(types, e.Type)
	}
	if !slices.Equal(types, []string{"fact", "next"}) || game.Seq != 3 || game.Pts != 800 {
		t.Errorf("log %v, seq %d, %d points", types, game.Seq, game.Pts)
	}
}

// TestTheBandsAreTheFactsRanges, at their edges, the rating compared in
// tenths so a sum that misses 7.0 by a hair is still 7.0.
func TestTheBandsAreTheFactsRanges(t *testing.T) {
	for minutes, want := range map[int]int{1: 0, 89: 0, 90: 1, 119: 1, 120: 2, 136: 2, 149: 2, 150: 3, 170: 3, 400: 3} {
		if got := LengthBand(minutes); got != want {
			t.Errorf("LengthBand(%d) = %d, want %d", minutes, got, want)
		}
	}
	for _, c := range []struct {
		rating float64
		want   int
	}{
		{1.0, 0}, {5.9, 0}, {6.0, 1}, {6.9, 1}, {7.0, 2}, {0.1 + 0.2 + 6.7, 2}, {7.9, 2}, {8.0, 3}, {8.7, 3}, {10, 3},
	} {
		if got := RatingBand(c.rating); got != c.want {
			t.Errorf("RatingBand(%v) = %d, want %d", c.rating, got, c.want)
		}
	}
	for year, want := range map[int][2]int{1999: {1990, 1995}, 1990: {1990, 1990}, 1994: {1990, 1990}, 1995: {1990, 1995}, 2000: {2000, 2000}, 2026: {2020, 2025}} {
		if Decade(year) != want[0] || Years(year) != want[1] {
			t.Errorf("%d is the %d decade and the %d years, want %v", year, Decade(year), Years(year), want)
		}
	}
}

// TestEveryoneKeepsTheColourOfTheirPlace: the star first, down the
// billing, then the directors in crew order.
func TestEveryoneKeepsTheColourOfTheirPlace(t *testing.T) {
	p := matrix()
	var cast []int
	for i := range p.Cast {
		cast = append(cast, p.CastHue(i))
	}
	if want := []int{205, 78, 150, 345, 28, 232}; !slices.Equal(cast, want) {
		t.Errorf("the six's hues in slot order = %v, want %v", cast, want)
	}
	if a, b := p.DirectorHue(0), p.DirectorHue(1); a != 118 || b != 255 {
		t.Errorf("the directors' hues = %d and %d", a, b)
	}
	if HueAt(12) != 232 || HueAt(13) != 28 {
		t.Error("the hues do not wrap after twelve")
	}
}
