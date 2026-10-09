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
				KindDecade: "decade", KindYears: "years", KindDirector: "director"}[kind])
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
// between them make every kind of move: one buys every fact, one adds
// overlaps and asks for names, and one guesses movies from the answer's
// decade and genre, which say their own years.
func TestNothingBeforeTheEndSaysWhatIsHidden(t *testing.T) {
	p := matrix()
	for _, moves := range [][]struct{ kind, arg string }{
		{{KindLength, ""}, {KindRating, ""}, {KindGenre, ""}, {KindDecade, ""}, {KindYears, ""}, {KindDirector, ""}},
		{{KindOverlap, "nm0001592"}, {KindNext, ""}, {KindOverlap, "nm0287825"}, {KindNext, ""}, {KindNext, ""}},
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
	g.do(KindOverlap, "nm0287825")
	g.do(KindGuess, "tt0111257")
	g.do(KindDirector, "")
	g.do(KindGuess, "tt0034583")
	g.do(KindGuess, "tt0076759")
	game := Render(p, g.record(), live)
	var lines []string
	for _, e := range game.Log {
		lines = append(lines, rendered(t, e))
	}
	want := []string{
		`{"type":"next","cost":100,"slot":1}`,
		`{"type":"fact","kind":"decade","cost":100}`,
		`{"type":"overlap","person":"nm0287825","cost":250}`,
		`{"type":"guess","cost":100,"guess":{"id":"tt0111257","title":"Speed","year":1994,"cost":100,"shared":[5],"sameDecade":true,"sharesGenre":true,"warmth":2}}`,
		`{"type":"fact","kind":"director","cost":250}`,
		`{"type":"guess","cost":150,"guess":{"id":"tt0034583","title":"Casablanca","year":1942,"cost":150,"shared":[],"sameDecade":false,"sharesGenre":false,"warmth":0}}`,
		`{"type":"guess","cost":200,"guess":{"id":"tt0076759","title":"Star Wars","year":1977,"cost":200,"shared":[],"sameDecade":false,"sharesGenre":true,"warmth":1}}`,
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
// the genres, the decade and the five years as their first years, and
// the directors by name with their hues and photos.
func TestEachFactIsSaidAsTheGameSellsIt(t *testing.T) {
	p := matrix()
	g := play(t, p)
	for _, kind := range factKinds {
		g.do(kind, "")
	}
	got := rendered(t, Render(p, g.record(), live).Facts)
	want := `{"length":2,"rating":3,"genre":["Action","Sci-Fi"],"decade":1990,"years":1995,"director":[` +
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
	for _, want := range []string{`"end":null`, `"finishedAt":null`, `"overlaps":[]`, `"log":[]`, `"facts":{}`, `"nextCost":100`, `"phase":"play"`} {
		if !strings.Contains(body, want) {
			t.Errorf("a game in play lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, "secs") {
		t.Errorf("a game has a clock: %s", body)
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
func sheetJSON(t *testing.T, movies []SheetMovie) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(rendered(t, movies)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestASheetShowsOnlyWhatTheGameShows: only a slot showing has one; each
// movie lists the slots showing it credits, never one still hidden; a
// director is marked only once Director is bought; and the answer is a
// card like the rest, with the same fields.
func TestASheetShowsOnlyWhatTheGameShows(t *testing.T) {
	p := matrix()
	g := play(t, p)
	for _, slot := range []int{1, 5, -1, 6} {
		if _, ok := p.SheetOf(g.state(), slot, live); ok {
			t.Errorf("slot %d, not showing, has a sheet", slot)
		}
	}
	joe, ok := p.SheetOf(g.state(), 0, live)
	if !ok {
		t.Fatal("the sixth-billed has no sheet")
	}
	var got []string
	for _, m := range joe {
		got = append(got, fmt.Sprintf("%s %v %v", m.ID, m.On, m.Dir))
	}
	// Memento credits Moss too, and the answer everyone, but only
	// Pantoliano is showing; Bound's directors are not bought.
	if want := []string{"tt0106977 [0] false", "tt0115736 [0] false", "tt0133093 [0] false", "tt0209144 [0] false"}; !slices.Equal(got, want) {
		t.Errorf("Pantoliano's sheet = %v, want %v", got, want)
	}
	keys := func(m map[string]any) string { return strings.Join(slices.Sorted(maps.Keys(m)), " ") }
	cards := sheetJSON(t, joe)
	for _, c := range cards {
		if keys(c) != keys(cards[0]) {
			t.Errorf("%v has fields %s, %v has %s", c["id"], keys(c), cards[0]["id"], keys(cards[0]))
		}
		if _, dir := c["dir"]; dir {
			t.Errorf("%v says dir before Director is bought", c["id"])
		}
	}

	g.do(KindGuess, "tt0209144")
	g.do(KindDirector, "")
	joe, _ = p.SheetOf(g.state(), 0, live)
	got = nil
	for _, m := range joe {
		got = append(got, fmt.Sprintf("%s %v %v", m.ID, m.On, m.Dir))
	}
	if want := []string{"tt0106977 [0] false", "tt0115736 [0] true", "tt0133093 [0 1 3] true", "tt0209144 [0 3] false"}; !slices.Equal(got, want) {
		t.Errorf("Pantoliano's sheet after Memento and Director = %v, want %v", got, want)
	}
	if moss, ok := p.SheetOf(g.state(), 3, live); !ok || len(moss) != 4 {
		t.Errorf("Moss, filled in, has %d movies, %v", len(moss), ok)
	}

	// Once it is over, anyone in the cast has one, crediting everyone.
	g.do(KindReveal, "")
	keanu, ok := p.SheetOf(g.state(), 5, live)
	if !ok || len(keanu) != 4 || !slices.Equal(keanu[1].On, []int{0, 1, 2, 3, 4, 5}) || keanu[1].ID != "tt0133093" {
		t.Errorf("Keanu's sheet at the end = %+v, %v", keanu, ok)
	}
}

// TestASheetHasEveryPosterOrNone: the answer always has a poster, so a
// sheet where only some cards had theirs would mark it out. Pantoliano's
// four all have one in live, and are sent with them; Keanu's do not,
// and none is sent, the answer's included.
func TestASheetHasEveryPosterOrNone(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindReveal, "")
	if !slices.Equal(p.SheetWants(0), []string{"tt0106977", "tt0115736", "tt0133093", "tt0209144"}) {
		t.Errorf("Pantoliano's sheet wants %v", p.SheetWants(0))
	}
	joe, _ := p.SheetOf(g.state(), 0, live)
	for _, m := range joe {
		if m.Poster == "" || m.Poster != live.Posters[m.ID] {
			t.Errorf("%s has poster %q with every poster there", m.ID, m.Poster)
		}
	}
	keanu, _ := p.SheetOf(g.state(), 5, live)
	for _, m := range keanu {
		if m.Poster != "" {
			t.Errorf("%s has poster %q though John Wick has none", m.ID, m.Poster)
		}
	}
	if body := rendered(t, keanu); strings.Contains(body, "poster") || strings.Contains(body, "matrix.jpg") {
		t.Errorf("Keanu's sheet says %s", body)
	}
}

// TestASheetIsOnlyTheMoviesOnIt: Memento, on Moss's sheet but below
// Pantoliano's cap, is on hers and lights him there once both show, and
// is not on his. A puzzle kept before sheets were, with none, has every
// movie crediting a slot on that slot's sheet, as it did then.
func TestASheetIsOnlyTheMoviesOnIt(t *testing.T) {
	p := matrix()
	memento := slices.IndexFunc(p.Movies, func(m Movie) bool { return m.ID == "tt0209144" })
	p.Movies[memento].Sheets = []int{3}
	g := play(t, p)
	g.do(KindGuess, "tt0209144")
	ids := func(movies []SheetMovie) []string {
		var out []string
		for _, m := range movies {
			out = append(out, m.ID)
		}
		return out
	}
	joe, _ := p.SheetOf(g.state(), 0, live)
	if want := []string{"tt0106977", "tt0115736", "tt0133093"}; !slices.Equal(ids(joe), want) || !slices.Equal(p.SheetWants(0), want) {
		t.Errorf("Pantoliano's sheet = %v, wants %v; want %v", ids(joe), p.SheetWants(0), want)
	}
	moss, ok := p.SheetOf(g.state(), 3, live)
	i := slices.IndexFunc(moss, func(m SheetMovie) bool { return m.ID == "tt0209144" })
	if !ok || i < 0 || !slices.Equal(moss[i].On, []int{0, 3}) {
		t.Errorf("Moss's sheet = %+v, %v; want Memento lighting [0 3]", moss, ok)
	}

	var kept []Movie
	if err := json.Unmarshal([]byte(`[{"id":"tt0209144","title":"Memento","year":2000,"rating":8.4,"md":1011,`+
		`"genres":["Mystery","Thriller"],"cast":[0,3],"dir":false}]`), &kept); err != nil {
		t.Fatal(err)
	}
	p.Movies = kept
	for _, slot := range []int{0, 3} {
		if got := p.SheetWants(slot); !slices.Equal(got, []string{"tt0209144"}) {
			t.Errorf("kept before sheets were, slot %d's sheet wants %v", slot, got)
		}
	}
	if got := p.SheetWants(1); len(got) != 0 {
		t.Errorf("kept before sheets were, Foster's sheet wants %v", got)
	}
}
