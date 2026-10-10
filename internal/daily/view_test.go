package daily

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

var live = Live{
	Posters: map[string]string{
		"tt0133093": "https://img.example/matrix.jpg",
		"tt0111257": "https://img.example/speed.jpg",
		"tt0106977": "https://img.example/fugitive.jpg",
		"tt0115736": "https://img.example/bound.jpg",
		"tt0209144": "https://img.example/memento.jpg",
	},
	Photos: map[string]string{
		"nm0000206": "https://image.tmdb.org/t/p/w185/keanu.jpg",
		"nm0001592": "https://image.tmdb.org/t/p/w185/joe.jpg",
		"nm0905154": "https://image.tmdb.org/t/p/w185/lana.jpg",
	},
}

func rendered(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// said is every place body says value, by its path: a string equal to
// it, or a number equal to it. A value an object with an "id" says as
// its own "year" is skipped when skipOwnYear is set: a guess or an
// "Also in" saying its own year, as a movie may.
func said(t *testing.T, body string, value any, skipOwnYear bool) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			_, movie := x["id"]
			for k, e := range x {
				if !(skipOwnYear && movie && k == "year") {
					walk(path+"."+k, e)
				}
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		case float64:
			if n, ok := value.(float64); ok && x == n {
				out = append(out, path)
			}
		case string:
			if s, ok := value.(string); ok && x == s {
				out = append(out, path)
			}
		}
	}
	walk("", v)
	slices.Sort(out)
	return out
}

// secretsKept fails the test when a game's body, before the end, says
// anything it may not: the answer's id, title, year, rating, length,
// poster or genres, a director before the Director fact, a hidden cast
// member's id or name, or a fact not bought. What it may say is checked
// too: exactly the facts bought, each where the facts are.
func secretsKept(t *testing.T, p *Puzzle, s *State, game Game, when string) {
	t.Helper()
	body := rendered(t, game)
	a := p.Answer
	for _, secret := range []any{a.ID, a.Title, live.Posters[a.ID], a.Rating, float64(a.Length)} {
		if got := said(t, body, secret, false); got != nil {
			t.Errorf("%s: the answer's %v is said at %v", when, secret, got)
		}
	}
	if got := said(t, body, float64(a.Year), true); got != nil {
		t.Errorf("%s: the answer's year is said at %v", when, got)
	}
	for _, g := range a.Genres {
		got := said(t, body, g, false)
		var want []string
		if s.Facts[KindGenre] {
			want = []string{fmt.Sprintf(".facts.genre[%d]", slices.Index(a.Genres, g))}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: the genre %s is said at %v, want %v", when, g, got, want)
		}
	}
	for i, d := range p.Directors {
		var want []string
		if s.Facts[KindDirector] {
			want = []string{fmt.Sprintf(".facts.director[%d].name", i)}
		}
		if got := said(t, body, d.Name, false); !slices.Equal(got, want) {
			t.Errorf("%s: %s is said at %v, want %v", when, d.Name, got, want)
		}
	}
	for i, b := range p.Cast {
		if s.Shown(i) {
			continue
		}
		for _, secret := range []string{b.ID, b.Name} {
			if got := said(t, body, secret, false); got != nil {
				t.Errorf("%s: hidden slot %d's %q is said at %v", when, i, secret, got)
			}
		}
		if b.Also != nil && !s.Guessed(b.Also.ID) {
			if got := said(t, body, b.Also.ID, false); got != nil {
				t.Errorf("%s: hidden slot %d's Also in is said at %v", when, i, got)
			}
		}
	}
	var facts map[string]any
	if err := json.Unmarshal([]byte(rendered(t, game.Facts)), &facts); err != nil {
		t.Fatal(err)
	}
	have := slices.Sorted(maps.Keys(facts))
	var bought []string
	for _, kind := range factKinds {
		if s.Facts[kind] {
			bought = append(bought, map[string]string{KindLength: "length", KindRating: "rating", KindGenre: "genre",
				KindDecade: "decade", KindDirector: "director"}[kind])
		}
	}
	slices.Sort(bought)
	if !slices.Equal(have, bought) {
		t.Errorf("%s: the facts say %v, want only the ones bought, %v", when, have, bought)
	}
	if game.End != nil || strings.Contains(body, `"end":{`) {
		t.Errorf("%s: a game in play has an end", when)
	}
}

// TestNothingBeforeTheEndSaysWhatIsHidden is the fairness rule: after
// Play, and after every kind of move short of the end, nothing a game is
// rendered as names the answer or says its year, rating, length, poster
// or genres, names a director before they are bought, or anyone in the
// cast still hidden, and every fact not bought is absent. Three games
// between them make every kind of move: one opens a sheet and buys
// every fact, one asks for names, and one guesses movies from the
// answer's decade and genre, which say their own years.
func TestNothingBeforeTheEndSaysWhatIsHidden(t *testing.T) {
	p := matrix()
	for _, moves := range [][]struct{ kind, arg string }{
		{{KindSheet, "nm0001592"}, {KindLength, ""}, {KindRating, ""}, {KindGenre, ""}, {KindDecade, ""}, {KindDirector, ""}},
		{{KindNext, ""}, {KindNext, ""}, {KindNext, ""}, {KindNext, ""}, {KindNext, ""}},
		{{KindGuess, "tt0120601"}, {KindGuess, "tt0115736"}, {KindGuess, "tt9000002"}, {KindGuess, "tt0209144"}},
	} {
		g := play(t, p)
		secretsKept(t, p, g.state(), Render(p, g.record(), live), "after Play")
		for _, m := range moves {
			s := g.do(m.kind, m.arg)
			if s.Done {
				t.Fatalf("%s %s ended the game; the test means to stop short of the end", m.kind, m.arg)
			}
			secretsKept(t, p, s, Render(p, g.record(), live), "after "+m.kind+" "+m.arg)
		}
	}

	g := play(t, p)
	g.do(KindNext, "")
	g.do(KindGuess, "tt0133093")
	body := rendered(t, Render(p, g.record(), live))
	for _, want := range []string{`"tt0133093"`, `"The Matrix"`, `"Lana Wachowski"`, `"Keanu Reeves"`, `"length":136`, `"rating":8.7`} {
		if !strings.Contains(body, want) {
			t.Errorf("the end does not say %s: %s", want, body)
		}
	}
}

// TestAHiddenSlotIsOnlyItsNumber, field for field, and a shown one says
// who, their "Also in", how they came to show, and the guess that filled
// them in.
func TestAHiddenSlotIsOnlyItsNumber(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindGuess, "tt0209144")
	slots := Render(p, g.record(), live).Slots
	want := []string{
		`{"slot":0,"shown":true,"person":{"id":"nm0001592","name":"Joe Pantoliano","hue":205,"photo":"https://image.tmdb.org/t/p/w185/joe.jpg"},"also":{"id":"tt0106977","title":"The Fugitive","year":1993},"via":"start"}`,
		`{"slot":1,"shown":true,"person":{"id":"nm0287825","name":"Gloria Foster","hue":78},"also":{"id":"tt0067433","title":"Man and Boy","year":1971},"via":"guess"}`,
		`{"slot":2,"shown":false}`,
		`{"slot":3,"shown":true,"person":{"id":"nm0005251","name":"Carrie-Anne Moss","hue":345},"also":{"id":"tt0241303","title":"Chocolat","year":2000},"via":"guess","from":{"id":"tt0209144","title":"Memento"}}`,
		`{"slot":4,"shown":false}`,
		`{"slot":5,"shown":false}`,
	}
	if len(slots) != Slots {
		t.Fatalf("%d slots", len(slots))
	}
	for i, w := range want {
		if got := rendered(t, slots[i]); got != w {
			t.Errorf("slot %d = %s\nwant     %s", i, got, w)
		}
	}
	// Someone with no "Also in" says none.
	p.Cast[2].Also = nil
	g.do(KindNext, "")
	if got := rendered(t, Render(p, g.record(), live).Slots[2]); strings.Contains(got, "also") || !strings.Contains(got, `"via":"next"`) {
		t.Errorf("Weaving, with no Also in, = %s", got)
	}
}

// TestTheLogHasAnEntryPerMove, in the API's shapes, the wrong guess the
// points could not cover followed by "out".
func TestTheLogHasAnEntryPerMove(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindNext, "")
	g.do(KindDecade, "")
	g.do(KindGuess, "tt0111257")
	g.do(KindDirector, "")
	g.do(KindGuess, "tt0034583")
	g.do(KindGuess, "tt0076759")
	g.do(KindGuess, "tt0120601")
	game := Render(p, g.record(), live)
	var lines []string
	for _, e := range game.Log {
		lines = append(lines, rendered(t, e))
	}
	want := []string{
		`{"type":"next","cost":100,"slot":1}`,
		`{"type":"fact","kind":"decade","cost":100}`,
		`{"type":"guess","cost":100,"guess":{"id":"tt0111257","title":"Speed","year":1994,"cost":100,"shared":[5],"sameDecade":true,"sharesGenre":true,"warmth":2}}`,
		`{"type":"fact","kind":"director","cost":250}`,
		`{"type":"guess","cost":150,"guess":{"id":"tt0034583","title":"Casablanca","year":1942,"cost":150,"shared":[],"sameDecade":false,"sharesGenre":false,"warmth":0}}`,
		`{"type":"guess","cost":200,"guess":{"id":"tt0076759","title":"Star Wars","year":1977,"cost":200,"shared":[],"sameDecade":false,"sharesGenre":true,"warmth":1}}`,
		`{"type":"guess","cost":250,"guess":{"id":"tt0120601","title":"Being John Malkovich","year":1999,"cost":250,"shared":[],"sameDecade":true,"sharesGenre":false,"warmth":1}}`,
		`{"type":"out"}`,
	}
	if !slices.Equal(lines, want) {
		t.Errorf("log =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if game.Phase != PhaseDone || game.Pts != 0 || game.Won || game.GaveUp || game.Seq != 7 {
		t.Errorf("game = %+v", game)
	}

	won := play(t, p)
	won.do(KindGuess, "tt0133093")
	if log := rendered(t, Render(p, won.record(), live).Log); log != `[{"type":"win"}]` {
		t.Errorf("a win logs %s", log)
	}
	gave := play(t, p)
	gave.do(KindReveal, "")
	if log := rendered(t, Render(p, gave.record(), live).Log); log != `[{"type":"gaveup"}]` {
		t.Errorf("giving up logs %s", log)
	}
	if log := rendered(t, Render(p, play(t, p).record(), live).Log); log != `[]` {
		t.Errorf("a new game logs %s", log)
	}
}

// TestEachFactIsSaidAsTheGameSellsIt: the length and rating as bands,
// the genres, the decade as its first year, and the directors by name
// with their hues and photos.
func TestEachFactIsSaidAsTheGameSellsIt(t *testing.T) {
	p := matrix()
	g := play(t, p)
	for _, kind := range factKinds {
		g.do(kind, "")
	}
	got := rendered(t, Render(p, g.record(), live).Facts)
	want := `{"length":2,"rating":3,"genre":["Action","Sci-Fi"],"decade":1990,"director":[` +
		`{"id":"nm0905154","name":"Lana Wachowski","hue":118,"photo":"https://image.tmdb.org/t/p/w185/lana.jpg"},` +
		`{"id":"nm0905152","name":"Lilly Wachowski","hue":255}]}`
	if got != want {
		t.Errorf("facts = %s\nwant    %s", got, want)
	}
	// A band that is 0 is still said, and a movie with no genres says so.
	p.Answer.Length, p.Answer.Rating, p.Answer.Genres = 85, 5.4, nil
	if got := rendered(t, Render(p, g.record(), live).Facts); !strings.HasPrefix(got, `{"length":0,"rating":0,"genre":[],`) {
		t.Errorf("the lowest bands and no genres = %s", got)
	}
	if got := rendered(t, Render(p, play(t, p).record(), live).Facts); got != `{}` {
		t.Errorf("no facts bought = %s", got)
	}
}

// TestTheEndShowsEverything: every slot, those the player never saw as
// shown by the end, the answer with every fact exact, its poster and
// colour, and the directors.
func TestTheEndShowsEverything(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindNext, "")
	g.do(KindGuess, "tt0133093")
	rec := g.record()
	done := rec.Started.Add(3*time.Minute + 41*time.Second)
	rec.Finished = &done
	game := Render(p, rec, live)
	if game.Phase != PhaseDone || !game.Won || game.Pts != 900 || game.FinishedAt == nil || !game.FinishedAt.Equal(done) {
		t.Errorf("game = %+v", game)
	}
	var via []string
	for _, sl := range game.Slots {
		if !sl.Shown || sl.Person == nil {
			t.Errorf("slot %d is hidden at the end", sl.Slot)
			continue
		}
		via = append(via, sl.Via)
	}
	if want := []string{"start", "next", "end", "end", "end", "end"}; !slices.Equal(via, want) {
		t.Errorf("at the end the slots show by %v, want %v", via, want)
	}
	end := rendered(t, game.End)
	want := `{"answer":{"id":"tt0133093","title":"The Matrix","year":1999,"rating":8.7,"length":136,"genres":["Action","Sci-Fi"],` +
		`"colour":"#26382d","poster":"https://img.example/matrix.jpg"},"directors":[` +
		`{"id":"nm0905154","name":"Lana Wachowski","hue":118,"photo":"https://image.tmdb.org/t/p/w185/lana.jpg"},` +
		`{"id":"nm0905152","name":"Lilly Wachowski","hue":255}]}`
	if end != want {
		t.Errorf("end = %s\nwant  %s", end, want)
	}

	playing := Render(p, play(t, p).record(), live)
	body := rendered(t, playing)
	for _, want := range []string{`"end":null`, `"finishedAt":null`, `"log":[]`, `"facts":{}`, `"sheet":null`, `"nextCost":100`, `"phase":"play"`} {
		if !strings.Contains(body, want) {
			t.Errorf("a game in play lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "secs") || strings.Contains(body, "overlaps") {
		t.Errorf("a game has a clock or overlaps: %s", body)
	}
}

// TestTimesAreWrittenInUTCToTheMillisecond, as the response's "now" is,
// whatever zone and precision the database handed them back in.
func TestTimesAreWrittenInUTCToTheMillisecond(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindReveal, "")
	rec := g.record()
	lagos := time.FixedZone("WAT", 3600)
	rec.Started = time.Date(2026, 10, 9, 10, 0, 0, 123456789, lagos)
	done := rec.Started.Add(95*time.Second + 400*time.Microsecond)
	rec.Finished = &done
	body := rendered(t, Render(p, rec, live))
	for _, want := range []string{`"startedAt":"2026-10-09T09:00:00.123Z"`, `"finishedAt":"2026-10-09T09:01:35.123Z"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the game lacks %s: %s", want, body)
		}
	}
}

// TestWantsAreWhatTheGameShows: the photos read for an answer are of
// the people it shows, and the directors once bought; the answer's
// poster and everyone once it is over; nothing before Play.
func TestWantsAreWhatTheGameShows(t *testing.T) {
	p := matrix()
	if films, people := p.Wants(nil); films != nil || people != nil {
		t.Errorf("before Play: %v, %v", films, people)
	}
	g := play(t, p)
	g.do(KindGuess, "tt0209144")
	films, people := p.Wants(g.record())
	if films != nil || !slices.Equal(people, []string{"nm0001592", "nm0287825", "nm0005251"}) {
		t.Errorf("after Memento: %v, %v", films, people)
	}
	g.do(KindDirector, "")
	if _, people = p.Wants(g.record()); !slices.Equal(people[3:], []string{"nm0905154", "nm0905152"}) {
		t.Errorf("after the director: %v", people)
	}
	g.do(KindReveal, "")
	films, people = p.Wants(g.record())
	if !slices.Equal(films, []string{"tt0133093"}) || len(people) != Slots+2 {
		t.Errorf("at the end: %v, %v", films, people)
	}
}

// sheetJSON is a sheet as the API sends it.
func sheetJSON(t *testing.T, cards []SheetCard) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(rendered(t, cards)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// readableIDs are the ids of a sheet's readable cards, in its order.
func readableIDs(cards []SheetCard) []string {
	var out []string
	for _, c := range cards {
		if c.Readable {
			out = append(out, c.ID)
		}
	}
	return out
}

// opened is a game in play with all six showing and slot's sheet
// opened: the one sheet it may read until the end.
func opened(t *testing.T, p *Puzzle, slot int) *player {
	t.Helper()
	g := play(t, p)
	for range Slots - 1 {
		g.do(KindNext, "")
	}
	g.do(KindSheet, p.Cast[slot].ID)
	return g
}

// TestOnlyTheSheetOpenedIsRead: before a sheet is opened nobody's is
// read, not even the sixth-billed's, showing from Play; once
// Pantoliano's is, only his, though Foster comes to show beside him; a
// slot that is none never has one; and once it is over anyone in the
// cast does, every card readable.
func TestOnlyTheSheetOpenedIsRead(t *testing.T) {
	p := matrix()
	g := play(t, p)
	closed := func(when string, slots ...int) {
		t.Helper()
		s := g.state()
		for _, slot := range slots {
			if _, ok := p.SheetOf(s, slot, live); ok || p.Opens(s, slot) {
				t.Errorf("%s, slot %d has a sheet", when, slot)
			}
			if got := p.SheetWants(s, slot); got != nil {
				t.Errorf("%s, slot %d wants %v", when, slot, got)
			}
		}
	}
	closed("before one is opened", -1, 0, 1, 5, 6)
	g.do(KindSheet, "nm0001592")
	g.do(KindNext, "")
	g.do(KindDecade, "")
	if joe, ok := p.SheetOf(g.state(), 0, live); !ok || len(joe) != 4 || len(readableIDs(joe)) != 3 {
		t.Errorf("the sheet opened, with the 1990s bought: %+v, %v", joe, ok)
	}
	if got := p.SheetWants(g.state(), 0); len(got) != 3 {
		t.Errorf("the sheet opened wants %v", got)
	}
	closed("with Pantoliano's opened and Foster showing", -1, 1, 2, 5, 6)
	g.do(KindReveal, "")
	for slot := range Slots {
		cards, ok := p.SheetOf(g.state(), slot, live)
		if !ok || len(cards) != len(p.sheet(slot)) || len(readableIDs(cards)) != len(cards) {
			t.Errorf("slot %d's sheet once it is over: %+v, %v", slot, cards, ok)
		}
	}
	closed("once it is over", -1, 6)
}

// TestBeforeAnyRangeEveryCardIsBlank: with neither the decade nor the
// rating bought, a sheet is only where its movies sit: each card exactly
// {year, at}, with no id, title, exact rating, genres or poster, today's
// movie like the rest. The genre, the length and the director are no
// ranges, and a movie guessed is no more readable for it.
func TestBeforeAnyRangeEveryCardIsBlank(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindSheet, "nm0001592")
	want := `[{"year":1993,"at":8},{"year":1996,"at":7.5},{"year":1999,"at":8.5},{"year":2000,"at":8.5}]`
	for _, move := range []struct{ kind, arg string }{{"", ""}, {KindGenre, ""}, {KindLength, ""}, {KindDirector, ""}, {KindGuess, "tt0209144"}} {
		if move.kind != "" {
			g.do(move.kind, move.arg)
		}
		joe, _ := p.SheetOf(g.state(), 0, live)
		body := rendered(t, joe)
		if body != want {
			t.Errorf("after %s, Pantoliano's sheet = %s\nwant %s", move.kind, body, want)
		}
		secrets := []string{`"rating"`, `"genres"`, `"poster"`, `"id"`, `"title"`, "tt"}
		for _, m := range p.Movies {
			secrets = append(secrets, m.Title)
		}
		for _, secret := range secrets {
			if strings.Contains(body, secret) {
				t.Errorf("after %s, Pantoliano's sheet says %s: %s", move.kind, secret, body)
			}
		}
		if got := p.SheetWants(g.state(), 0); len(got) != 0 {
			t.Errorf("after %s, Pantoliano's sheet wants posters for %v", move.kind, got)
		}
	}
	// Moss, filled in by Memento, has a sheet of blanks too, opened in a
	// game of its own.
	other := play(t, p)
	other.do(KindGuess, "tt0209144")
	other.do(KindSheet, "nm0005251")
	if moss, ok := p.SheetOf(other.state(), 3, live); !ok || len(readableIDs(moss)) != 0 || len(moss) != 4 {
		t.Errorf("Moss's sheet = %+v, %v", moss, ok)
	}
}

// TestACardIsReadableOnlyInsideEveryRangeBought: the decade and the
// rating band each narrow what the sheets read, together as much as the
// tighter; the genre, the length and the director never narrow them nor
// open them. Across all six sheets, each opened in a game of its own
// with everyone showing.
func TestACardIsReadableOnlyInsideEveryRangeBought(t *testing.T) {
	p := matrix()
	// The Fugitive, Speed, Bound and The Matrix are the 1990s'.
	nineties := []string{"tt0106977", "tt0111257", "tt0115736", "tt0133093"}
	// 8.0 or higher: Apocalypse Now, The Matrix, Memento, The Lord of the
	// Rings and V for Vendetta.
	eights := []string{"tt0078788", "tt0133093", "tt0209144", "tt0120737", "tt0434409"}
	for _, c := range []struct {
		facts []string
		want  []string
	}{
		{nil, nil},
		{[]string{KindLength, KindDirector}, nil},
		// The genre, Action and Sci-Fi, opens nothing, not even the two
		// movies that have both.
		{[]string{KindGenre}, nil},
		{[]string{KindLength, KindGenre, KindDirector}, nil},
		{[]string{KindDecade}, nineties},
		{[]string{KindDecade, KindGenre}, nineties},
		{[]string{KindRating}, eights},
		// Nor does it close anything: V for Vendetta and The Lord of the
		// Rings have neither of the answer's genres, Memento only one.
		{[]string{KindRating, KindGenre}, eights},
		{[]string{KindDecade, KindRating}, []string{"tt0133093"}},
		{[]string{KindDecade, KindRating, KindGenre, KindLength}, []string{"tt0133093"}},
	} {
		var got []string
		for slot := range Slots {
			g := opened(t, p, slot)
			for _, kind := range c.facts {
				g.do(kind, "")
			}
			s := g.state()
			cards, ok := p.SheetOf(s, slot, live)
			if !ok {
				t.Fatalf("%v: slot %d has no sheet", c.facts, slot)
			}
			for _, id := range readableIDs(cards) {
				if !slices.Contains(got, id) {
					got = append(got, id)
				}
			}
			if wants := p.SheetWants(s, slot); !slices.Equal(slices.Sorted(slices.Values(wants)), slices.Sorted(slices.Values(readableIDs(cards)))) {
				t.Errorf("%v: slot %d wants %v and reads %v", c.facts, slot, wants, readableIDs(cards))
			}
			if len(cards) != len(p.sheet(slot)) {
				t.Errorf("%v: slot %d has %d cards for %d movies", c.facts, slot, len(cards), len(p.sheet(slot)))
			}
		}
		slices.Sort(got)
		want := slices.Sorted(slices.Values(c.want))
		if !slices.Equal(got, want) {
			t.Errorf("with %v bought, the sheets read %v, want %v", c.facts, got, want)
		}
	}

	// Once it is over, everything is readable, whatever was bought.
	g := play(t, p)
	g.do(KindReveal, "")
	for slot := range Slots {
		cards, _ := p.SheetOf(g.state(), slot, live)
		if len(readableIDs(cards)) != len(cards) {
			t.Errorf("at the end, slot %d's sheet has blank cards: %+v", slot, cards)
		}
	}
}

// TestARangeHoldsFromItsFloorToBelowItsCeiling, as the page draws it: the
// decade takes its first and last years, and the rating band its floor
// and not its ceiling, compared in tenths so a 7.0 kept as 6.9999 is
// still 7.0. The genre bought, a movie's genres change nothing.
func TestARangeHoldsFromItsFloorToBelowItsCeiling(t *testing.T) {
	p := matrix()
	p.Answer.Rating = 7.5
	g := play(t, p)
	for _, kind := range []string{KindDecade, KindRating, KindGenre} {
		g.do(kind, "")
	}
	s := g.state()
	in := Movie{Year: 1997, Rating: 7.5, Genres: []string{"Action", "Sci-Fi"}}
	for _, c := range []struct {
		name string
		edit func(m *Movie)
		want bool
	}{
		{"inside them both", func(*Movie) {}, true},
		{"the decade's first year", func(m *Movie) { m.Year = 1990 }, true},
		{"its last", func(m *Movie) { m.Year = 1999 }, true},
		{"the year before it", func(m *Movie) { m.Year = 1989 }, false},
		{"the year after it", func(m *Movie) { m.Year = 2000 }, false},
		{"the band's floor", func(m *Movie) { m.Rating = 7.0 }, true},
		{"its floor a hair under", func(m *Movie) { m.Rating = 0.1 + 0.2 + 6.7 }, true},
		{"its top", func(m *Movie) { m.Rating = 7.9 }, true},
		{"its ceiling", func(m *Movie) { m.Rating = 8.0 }, false},
		{"below its floor", func(m *Movie) { m.Rating = 6.9 }, false},
		{"one more genre", func(m *Movie) { m.Genres = []string{"Action", "Adventure", "Sci-Fi"} }, true},
		{"one genre of the two", func(m *Movie) { m.Genres = []string{"Action", "Thriller"} }, true},
		{"none of them", func(m *Movie) { m.Genres = []string{"Romance"} }, true},
		{"no genres", func(m *Movie) { m.Genres = nil }, true},
	} {
		m := in
		m.Genres = slices.Clone(in.Genres)
		c.edit(&m)
		if got := p.readable(s, m); got != c.want {
			t.Errorf("%s (%+v): readable %v, want %v", c.name, m, got, c.want)
		}
	}
	// The decade alone holds its first and last years.
	decade := play(t, p)
	decade.do(KindDecade, "")
	for year, want := range map[int]bool{1989: false, 1990: true, 1999: true, 2000: false} {
		if got := p.readable(decade.state(), Movie{Year: year, Rating: 1}); got != want {
			t.Errorf("%d in the 1990s: readable %v, want %v", year, got, want)
		}
	}
}

// TestABlankCardIsPlacedToTheHalfPoint, and no finer: two sheets' blank
// cards can no longer be matched by year and exact rating.
func TestABlankCardIsPlacedToTheHalfPoint(t *testing.T) {
	for _, c := range []struct{ rating, want float64 }{
		{5.5, 5.5}, {7.2, 7}, {7.3, 7.5}, {7.7, 7.5}, {7.8, 8}, {8.4, 8.5}, {8.7, 8.5}, {8.8, 9}, {0.1 + 0.2 + 6.7, 7}, {10, 10},
	} {
		if got := halfPoint(c.rating); got != c.want {
			t.Errorf("halfPoint(%v) = %v, want %v", c.rating, got, c.want)
		}
	}
}

// TestTheAnswersCardIsLikeEveryOther: blank, it carries exactly {year,
// at}, as every blank card does; readable, it has the fields every
// readable card has and no more. Nothing in a sheet says which card is
// today's.
func TestTheAnswersCardIsLikeEveryOther(t *testing.T) {
	p := matrix()
	keys := func(m map[string]any) string { return strings.Join(slices.Sorted(maps.Keys(m)), " ") }
	for _, facts := range [][]string{nil, {KindLength}, {KindGenre}, {KindDecade}, {KindDecade, KindGenre}, {KindRating}, {KindDecade, KindRating, KindGenre}} {
		for slot := range Slots {
			g := opened(t, p, slot)
			for _, kind := range facts {
				g.do(kind, "")
			}
			cards, ok := p.SheetOf(g.state(), slot, live)
			if !ok || len(cards) == 0 {
				t.Fatalf("%v: slot %d's sheet, opened, = %+v, %v", facts, slot, cards, ok)
			}
			readable := ""
			for i, c := range sheetJSON(t, cards) {
				_, id := c["id"]
				if id != cards[i].Readable {
					t.Errorf("%v: slot %d's card %d is readable %v and says id %v", facts, slot, i, cards[i].Readable, id)
				}
				if !id {
					if keys(c) != "at year" {
						t.Errorf("%v: slot %d's blank card %d says %s", facts, slot, i, keys(c))
					}
					continue
				}
				if readable == "" {
					readable = keys(c)
				}
				if keys(c) != readable {
					t.Errorf("%v: slot %d has a readable card with %s and one with %s", facts, slot, readable, keys(c))
				}
			}
			if r := strings.Replace(readable, " poster", "", 1); readable != "" && r != "id rating title year" {
				t.Errorf("%v: slot %d's readable cards say %s", facts, slot, readable)
			}
		}
	}
}

// TestNoCardSaysItsGenres, readable or blank, in play or once it is
// over: the genre is no range, so a sheet read inside the decade and the
// band holds movies of every genre, and a card that said its own would
// let a player who bought Genre filter them by hand down to today's.
// Here Pantoliano's three 1990s movies are Action and Crime, Crime and
// Thriller, and Action and Sci-Fi: by their genres the answer's would be
// the only one left.
func TestNoCardSaysItsGenres(t *testing.T) {
	p := matrix()
	var genres []string
	for _, m := range p.Movies {
		for _, g := range m.Genres {
			if !slices.Contains(genres, g) {
				genres = append(genres, g)
			}
		}
	}
	says := func(when string, slot int, cards []SheetCard) {
		t.Helper()
		body := rendered(t, cards)
		for _, secret := range append([]string{"genres"}, genres...) {
			if strings.Contains(body, `"`+secret+`"`) {
				t.Errorf("%s, slot %d's sheet says %q: %s", when, slot, secret, body)
			}
		}
	}
	for _, facts := range [][]string{{KindDecade, KindGenre}, {KindRating, KindGenre}, {KindGenre, KindDecade, KindRating}} {
		for slot := range Slots {
			g := opened(t, p, slot)
			for _, kind := range facts {
				g.do(kind, "")
			}
			cards, ok := p.SheetOf(g.state(), slot, live)
			if !ok || len(readableIDs(cards)) == 0 {
				t.Fatalf("%v: slot %d's sheet reads nothing: %+v, %v", facts, slot, cards, ok)
			}
			says(fmt.Sprintf("with %v bought", facts), slot, cards)
		}
	}
	g := opened(t, p, 0)
	g.do(KindDecade, "")
	if joe, _ := p.SheetOf(g.state(), 0, live); !slices.Equal(readableIDs(joe), []string{"tt0106977", "tt0115736", "tt0133093"}) {
		t.Errorf("Pantoliano's sheet with the 1990s reads %v", readableIDs(joe))
	}
	g.do(KindGenre, "")
	g.do(KindReveal, "")
	for slot := range Slots {
		cards, _ := p.SheetOf(g.state(), slot, live)
		if len(readableIDs(cards)) != len(cards) {
			t.Fatalf("once it is over, slot %d's sheet has blank cards: %+v", slot, cards)
		}
		says("once it is over", slot, cards)
	}
}

// TestACardsPlaceSaysNothingItsFaceDoesNot: cards are listed by year,
// then by where they sit on the rating axis, readable before blank, so a
// blank card's place among the rest says nothing of its id. Chocolat and
// Memento, both of 2000 on Moss's sheet, come lower rated first, and
// giving every movie a new id changes nothing a blank sheet says.
func TestACardsPlaceSaysNothingItsFaceDoesNot(t *testing.T) {
	p := matrix()
	g := opened(t, p, 3)
	moss, _ := p.SheetOf(g.state(), 3, live)
	before := rendered(t, moss)
	if want := `[{"year":1999,"at":8.5},{"year":2000,"at":7},{"year":2000,"at":8.5},{"year":2003,"at":7}]`; before != want {
		t.Errorf("Moss's sheet = %s\nwant %s", before, want)
	}
	renamed := matrix()
	for i := range renamed.Movies {
		renamed.Movies[i].ID = fmt.Sprintf("tt%07d", 9999999-i)
	}
	renamed.Answer.ID = renamed.Movies[slices.IndexFunc(p.Movies, func(m Movie) bool { return m.ID == p.Answer.ID })].ID
	for slot := range Slots {
		o := opened(t, p, slot)
		was, _ := p.SheetOf(o.state(), slot, live)
		now, ok := renamed.SheetOf(Replay(renamed, o.moves), slot, Live{})
		if !ok || len(now) == 0 || rendered(t, was) != rendered(t, now) {
			t.Errorf("slot %d's blank sheet changed with the ids: %s, then %s", slot, rendered(t, was), rendered(t, now))
		}
	}
	// Readable, a year's cards come by where they sit, then by id.
	g.do(KindGenre, "")
	g.do(KindRating, "")
	g.do(KindReveal, "")
	moss, _ = p.SheetOf(g.state(), 3, live)
	var order []string
	for _, c := range moss {
		order = append(order, c.Title)
	}
	if want := []string{"The Matrix", "Chocolat", "Memento", "The Matrix Reloaded"}; !slices.Equal(order, want) {
		t.Errorf("Moss's sheet at the end is in the order %v, want %v", order, want)
	}
}

// TestASheetHasEveryReadablePosterOrNone: the answer always has a poster,
// so a sheet where only some readable cards had theirs would mark it out.
// Pantoliano's three 1990s movies all have one in live, and are sent
// them, while Memento, blank, is not; Weaving's three readable at 8.0 or
// higher, The Matrix, The Lord of the Rings and V for Vendetta, do not
// all, and none is sent one, the answer included. A blank card never
// has one, and no poster is read for it.
func TestASheetHasEveryReadablePosterOrNone(t *testing.T) {
	p := matrix()
	g := opened(t, p, 0)
	g.do(KindDecade, "")
	s := g.state()
	if got := p.SheetWants(s, 0); !slices.Equal(got, []string{"tt0106977", "tt0115736", "tt0133093"}) {
		t.Errorf("Pantoliano's sheet wants %v", got)
	}
	joe, _ := p.SheetOf(s, 0, live)
	for _, c := range joe {
		if c.Readable && (c.Poster == "" || c.Poster != live.Posters[c.ID]) {
			t.Errorf("%s has poster %q with every readable poster there", c.ID, c.Poster)
		}
		if !c.Readable && c.Poster != "" {
			t.Errorf("a blank card has poster %q", c.Poster)
		}
	}
	if body := rendered(t, joe); strings.Count(body, `"poster"`) != 3 || strings.Contains(body, "memento.jpg") {
		t.Errorf("Pantoliano's sheet with the 1990s = %s", body)
	}

	rating := opened(t, p, 2)
	rating.do(KindRating, "")
	hugo, _ := p.SheetOf(rating.state(), 2, live)
	if ids := readableIDs(hugo); !slices.Equal(ids, []string{"tt0133093", "tt0120737", "tt0434409"}) {
		t.Fatalf("Weaving's readable with the rating: %v", ids)
	}
	if body := rendered(t, hugo); strings.Contains(body, "poster") || strings.Contains(body, "matrix.jpg") {
		t.Errorf("Weaving's sheet with the rating says %s", body)
	}

	// At the end everything is readable: Pantoliano's all have one, and
	// Keanu's do not.
	g.do(KindReveal, "")
	joe, _ = p.SheetOf(g.state(), 0, live)
	if body := rendered(t, joe); strings.Count(body, `"poster"`) != 4 {
		t.Errorf("Pantoliano's sheet at the end = %s", body)
	}
	keanu, _ := p.SheetOf(g.state(), 5, live)
	if body := rendered(t, keanu); strings.Contains(body, "poster") {
		t.Errorf("Keanu's sheet at the end = %s", body)
	}
}

// TestASheetIsOnlyTheMoviesOnIt: Memento, on Moss's sheet but below
// Pantoliano's cap, is on hers and not on his. A puzzle kept before
// sheets were, with none, has every movie crediting a slot on that
// slot's sheet, as it did then.
func TestASheetIsOnlyTheMoviesOnIt(t *testing.T) {
	p := matrix()
	memento := slices.IndexFunc(p.Movies, func(m Movie) bool { return m.ID == "tt0209144" })
	p.Movies[memento].Sheets = []int{3}
	g := play(t, p)
	g.do(KindReveal, "")
	over := g.state()
	joe, _ := p.SheetOf(over, 0, live)
	if want := []string{"tt0106977", "tt0115736", "tt0133093"}; !slices.Equal(readableIDs(joe), want) || !slices.Equal(p.SheetWants(over, 0), want) {
		t.Errorf("Pantoliano's sheet = %v, wants %v; want %v", readableIDs(joe), p.SheetWants(over, 0), want)
	}
	moss, ok := p.SheetOf(over, 3, live)
	if !ok || !slices.Contains(readableIDs(moss), "tt0209144") {
		t.Errorf("Moss's sheet = %+v, %v; want Memento on it", moss, ok)
	}

	var kept []Movie
	if err := json.Unmarshal([]byte(`[{"id":"tt0209144","title":"Memento","year":2000,"rating":8.4,"md":1011,`+
		`"genres":["Mystery","Thriller"],"cast":[0,3],"dir":false}]`), &kept); err != nil {
		t.Fatal(err)
	}
	p.Movies = kept
	for _, slot := range []int{0, 3} {
		if got := p.SheetWants(over, slot); !slices.Equal(got, []string{"tt0209144"}) {
			t.Errorf("kept before sheets were, slot %d's sheet wants %v", slot, got)
		}
	}
	if got := p.SheetWants(over, 1); len(got) != 0 {
		t.Errorf("kept before sheets were, Foster's sheet wants %v", got)
	}
}
