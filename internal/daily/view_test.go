package daily

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

var live = Live{
	Posters: map[string]string{
		"tt0133093": "https://img.example/matrix.jpg",
		"tt0067433": "https://img.example/man-and-boy.jpg",
		"tt0111257": "https://img.example/speed.jpg",
		"tt0234215": "https://img.example/reloaded.jpg",
	},
	Photos: map[string]string{"nm0000206": "https://image.tmdb.org/t/p/w185/keanu.jpg"},
}

func rendered(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// hidden fails the test when body names the answer, or names any card
// the game has face down: by id or by title, as a JSON string, so "The
// Matrix" is not mistaken for "The Matrix Reloaded".
func hidden(t *testing.T, p *Puzzle, s *State, body, when string) {
	t.Helper()
	for _, secret := range []string{p.Answer.ID, p.Answer.Title} {
		if strings.Contains(body, `"`+secret+`"`) {
			t.Errorf("%s: the answer's %q is in %s", when, secret, body)
		}
	}
	for _, c := range p.Cards {
		if s != nil && s.Up(c.ID) {
			continue
		}
		if s == nil && slices.Contains(p.Start, c.ID) {
			continue
		}
		for _, secret := range []string{c.Film, c.Title} {
			if strings.Contains(body, `"`+secret+`"`) {
				t.Errorf("%s: face-down card %s's %q is in the body", when, c.ID, secret)
			}
		}
	}
}

// yearSaid is everywhere body says year other than as a movie's own:
// every number or string that is the year, by its path, unless it is
// the "year" of an object with an "id", which is a card or a movie
// saying its own year, as every card does and every guess may. The year
// clue's entry has no id.
func yearSaid(t *testing.T, body string, year int) []string {
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
				if !movie || k != "year" {
					walk(path+"."+k, e)
				}
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		case float64:
			if x == float64(year) {
				out = append(out, path)
			}
		case string:
			if x == strconv.Itoa(year) {
				out = append(out, path)
			}
		}
	}
	walk("", v)
	slices.Sort(out)
	return out
}

// yearHidden fails the test when a game says the answer's year anywhere
// but the year clue's entry, or says it there before the clue is bought.
func yearHidden(t *testing.T, p *Puzzle, game Game, when string) {
	t.Helper()
	var want []string
	for i, e := range game.Log {
		if e.Type == EntryYear {
			want = append(want, fmt.Sprintf(".log[%d].year", i))
		}
	}
	if got := yearSaid(t, rendered(t, game), p.Answer.Year); !slices.Equal(got, want) {
		t.Errorf("%s: the answer's year is said at %v, want %v", when, got, want)
	}
}

// TestNothingBeforeTheEndNamesTheAnswerOrAFaceDownCard is the fairness
// rule: at load, and after every kind of move short of the end, nothing
// the page is sent names the answer or a card it has not turned over,
// and nothing says the answer's year but the year clue once it is
// bought. Only the starting cards are sent in full at load.
func TestNothingBeforeTheEndNamesTheAnswerOrAFaceDownCard(t *testing.T) {
	p := matrix()
	load := rendered(t, map[string]any{"cards": p.Faces(), "start": p.Opened(live), "clues": p.Clues()})
	hidden(t, p, nil, load, "at load")
	if got := yearSaid(t, load, p.Answer.Year); got != nil {
		t.Errorf("at load the answer's year is said at %v", got)
	}
	for _, id := range []string{"tt0067433", "tt0108065", "tt0109190"} {
		if !strings.Contains(load, `"`+id+`"`) {
			t.Errorf("starting card %s is not sent in full at load", id)
		}
	}

	// Two games between them make every kind of move, each kept short
	// of spending its last point. The second guesses a movie from the
	// answer's year, which says its own year and "same", as a guess
	// earns, before it buys the year.
	for _, moves := range [][]struct{ kind, arg string }{
		{{KindFlip, "c7"}, {KindDirector, ""}, {KindActor, ""}, {KindGenres, ""},
			{KindGuess, "tt0111257"}, {KindGuess, "tt0034583"}, {KindFlip, "c1"}},
		{{KindGuess, "tt9000001"}, {KindGuess, "tt9000002"}, {KindActor, ""}, {KindYear, ""}, {KindGuess, "tt0209144"}},
	} {
		g := play(t, p)
		hidden(t, p, g.state(), rendered(t, Render(p, g.record(), live)), "after Play")
		yearHidden(t, p, Render(p, g.record(), live), "after Play")
		for _, m := range moves {
			s := g.do(m.kind, m.arg)
			if s.Done {
				t.Fatalf("%s %s ended the game; the test means to stop short of the end", m.kind, m.arg)
			}
			game := Render(p, g.record(), live)
			hidden(t, p, s, rendered(t, game), "after "+m.kind+" "+m.arg)
			yearHidden(t, p, game, "after "+m.kind+" "+m.arg)
		}
	}

	g := play(t, p)
	g.do(KindFlip, "c7")
	s := g.do(KindGuess, "tt0133093")
	body := rendered(t, Render(p, g.record(), live))
	if !strings.Contains(body, `"tt0133093"`) || !strings.Contains(body, `"The Matrix"`) {
		t.Errorf("the end does not name the answer: %s", body)
	}
	if !s.Done {
		t.Fatal("the right guess did not end the game")
	}
}

// TestTheLogHasAnEntryPerMove, in the API's shapes: "start" first, then
// one per move, the guess that spent the last point followed by "out".
func TestTheLogHasAnEntryPerMove(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindFlip, "c5")
	g.do(KindDirector, "")
	g.do(KindYear, "")
	g.do(KindActor, "")
	g.do(KindActor, "")
	g.do(KindGenres, "")
	g.do(KindGuess, "tt0034583")
	g.do(KindGuess, "tt9000001")

	game := Render(p, g.record(), live)
	var types []string
	for _, e := range game.Log {
		types = append(types, e.Type)
	}
	want := []string{"start", "flip", "person", "year", "person", "person", "genres", "guess", "guess", "out"}
	if !slices.Equal(types, want) {
		t.Errorf("log = %v, want %v", types, want)
	}
	if game.Phase != PhaseDone || game.Pts != 0 || game.Won || game.Seq != 8 {
		t.Errorf("game = %+v", game)
	}

	var log []map[string]any
	if err := json.Unmarshal([]byte(rendered(t, game.Log)), &log); err != nil {
		t.Fatal(err)
	}
	flip := log[1]
	if flip["card"] != "c5" || flip["cost"] != 55.0 || flip["film"].(map[string]any)["title"] != "Speed" {
		t.Errorf("flip = %v", flip)
	}
	if poster := flip["film"].(map[string]any)["poster"]; poster != "https://img.example/speed.jpg" {
		t.Errorf("flip poster = %v", poster)
	}
	person := log[2]
	people := person["people"].([]any)
	if person["role"] != "director" || person["cost"] != 150.0 || len(people) != 2 {
		t.Errorf("person = %v", person)
	}
	if lana := people[0].(map[string]any); lana["name"] != "Lana Wachowski" || lana["slot"] != 0.0 ||
		!slices.Equal(anyStrings(lana["cards"]), []string{"c1", "c6", "c9"}) {
		t.Errorf("Lana = %v", lana)
	}
	if year := rendered(t, game.Log[3]); year != `{"type":"year","cost":200,"year":1999}` {
		t.Errorf("year = %s", year)
	}
	if genres := log[6]; !slices.Equal(anyStrings(genres["genres"]), []string{"Action", "Sci-Fi"}) {
		t.Errorf("genres = %v", genres)
	}
	// Casablanca is off the board and shares nobody: card null, shared
	// empty, and the hints the guess earned.
	guess := log[7]
	if guess["card"] != nil || len(guess["shared"].([]any)) != 0 || guess["year"] != "newer" || guess["rating"] != "higher" {
		t.Errorf("guess = %v", guess)
	}
	if film := guess["film"].(map[string]any); film["title"] != "Casablanca" || film["year"] != 1942.0 || film["rating"] != 8.5 {
		t.Errorf("guessed film = %v", film)
	}
}

func anyStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// TestAGuessedMovieWithNothingKnownSaysNull: no year is 0, no rating is
// null, and no hint is null rather than a guess at one.
func TestAGuessedMovieWithNothingKnownSaysNull(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindGuess, "tt9000002")
	body := rendered(t, Render(p, g.record(), live).Log[1])
	want := `{"type":"guess","cost":100,"film":{"id":"tt9000002","title":"Nor This, Nor When","year":0,"rating":null,"md":0},` +
		`"card":null,"shared":[],"year":null,"rating":null}`
	if body != want {
		t.Errorf("entry = %s\nwant    %s", body, want)
	}
}

// TestACloseRelativeIsNamelessUntilNamed: turned over, it says only how
// many people it shares; guessed by name, or at the end, it is a card
// like any other.
func TestACloseRelativeIsNamelessUntilNamed(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindFlip, "c1")
	entry := rendered(t, Render(p, g.record(), live).Log[1])
	if want := `{"type":"flip","card":"c1","cost":55,"relative":{"shared":7}}`; entry != want {
		t.Errorf("relative = %s, want %s", entry, want)
	}
	g.do(KindGuess, "tt0234215")
	game := Render(p, g.record(), live)
	if f := game.Log[1].Film; f == nil || f.Title != "The Matrix Reloaded" || game.Log[1].Relative != nil {
		t.Errorf("guessed by name, the relative's flip says %+v", game.Log[1])
	}
	if guess := game.Log[2]; guess.Card != "c1" || len(guess.Shared) != 7 {
		t.Errorf("the guess of the relative = %+v", guess)
	}

	over := play(t, p)
	over.do(KindFlip, "c1")
	over.do(KindReveal, "")
	if f := Render(p, over.record(), live).Log[1].Film; f == nil || f.ID != "tt0234215" {
		t.Errorf("at the end the relative's flip says %+v", f)
	}
}

// TestKnownIsEveryoneBoughtOrFoundInSlotOrder, with their cards and the
// photo where there is one.
func TestKnownIsEveryoneBoughtOrFoundInSlotOrder(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindGuess, "tt0209144")
	g.do(KindActor, "")
	known := Render(p, g.record(), live).Known
	var slots []int
	for _, k := range known {
		slots = append(slots, k.Slot)
	}
	if !slices.Equal(slots, []int{2, 4, 7}) {
		t.Errorf("known slots = %v, want Keanu bought and Moss and Pantoliano found", slots)
	}
	if known[0].Photo != live.Photos["nm0000206"] || !slices.Equal(known[0].Cards, []string{"c1", "c5"}) {
		t.Errorf("Keanu = %+v", known[0])
	}
	if known[1].Photo != "" {
		t.Errorf("a person with no photo has %q", known[1].Photo)
	}
}

// TestTheEndShowsEverything: the answer with its genres and poster,
// every card with what it is and its people, and everyone, with the
// time in whole seconds.
func TestTheEndShowsEverything(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindFlip, "c9")
	g.do(KindGuess, "tt0133093")
	rec := g.record()
	done := rec.Started.Add(3*time.Minute + 41*time.Second + 600*time.Millisecond)
	rec.Finished = &done
	game := Render(p, rec, live)
	if game.Phase != PhaseDone || !game.Won || game.Pts != 940 || game.Secs == nil || *game.Secs != 222 {
		t.Errorf("game = %+v, secs %v", game, game.Secs)
	}
	if game.FinishedAt == nil || !game.FinishedAt.Equal(done) {
		t.Errorf("finishedAt = %v", game.FinishedAt)
	}
	if last := game.Log[len(game.Log)-1]; last.Type != EntryWin {
		t.Errorf("the right guess logs %q, want win", last.Type)
	}
	end := game.End
	if end == nil {
		t.Fatal("no end")
	}
	if end.Answer.ID != "tt0133093" || end.Answer.Poster != live.Posters["tt0133093"] || !slices.Equal(end.Answer.Genres, []string{"Action", "Sci-Fi"}) {
		t.Errorf("answer = %+v", end.Answer)
	}
	if len(end.Cards) != len(p.Cards) || end.Cards[0].Film.Title != "The Matrix Reloaded" || len(end.Cards[0].People) != 7 {
		t.Errorf("cards = %+v", end.Cards)
	}
	if len(end.People) != len(p.People) || end.People[7].Name != "Joe Pantoliano" {
		t.Errorf("people = %+v", end.People)
	}

	playing := Render(p, play(t, p).record(), live)
	if playing.End != nil || playing.FinishedAt != nil || playing.Secs != nil || playing.Phase != PhasePlay {
		t.Errorf("a game in play has %+v", playing)
	}
	if body := rendered(t, playing); !strings.Contains(body, `"end":null`) || !strings.Contains(body, `"secs":null`) {
		t.Errorf("a game in play says %s", body)
	}
}

// TestTimesAreWrittenInUTCToTheMillisecond, as the response's "now" is,
// whatever zone and precision the database handed them back in; the
// seconds are still taken from the moments as kept.
func TestTimesAreWrittenInUTCToTheMillisecond(t *testing.T) {
	p := matrix()
	g := play(t, p)
	g.do(KindReveal, "")
	rec := g.record()
	lagos := time.FixedZone("WAT", 3600)
	rec.Started = time.Date(2026, 10, 8, 10, 0, 0, 123456789, lagos)
	done := rec.Started.Add(95*time.Second + 400*time.Microsecond)
	rec.Finished = &done
	body := rendered(t, Render(p, rec, live))
	for _, want := range []string{`"startedAt":"2026-10-08T09:00:00.123Z"`, `"finishedAt":"2026-10-08T09:01:35.123Z"`, `"secs":95`} {
		if !strings.Contains(body, want) {
			t.Errorf("the game lacks %s: %s", want, body)
		}
	}
}

// TestWantsAreWhatTheGameShows: the posters and photos read for an
// answer are the ones it draws, and every one of them once it is over.
func TestWantsAreWhatTheGameShows(t *testing.T) {
	p := matrix()
	films, people := p.Wants(nil)
	if !slices.Equal(films, []string{"tt0067433", "tt0108065", "tt0109190"}) || people != nil {
		t.Errorf("before Play: %v, %v", films, people)
	}
	g := play(t, p)
	g.do(KindFlip, "c7")
	g.do(KindGuess, "tt0034583")
	g.do(KindDirector, "")
	films, people = p.Wants(g.record())
	if !slices.Equal(films, []string{"tt0067433", "tt0108065", "tt0109190", "tt0209144", "tt0034583"}) {
		t.Errorf("films = %v", films)
	}
	if !slices.Equal(people, []string{"nm0905154", "nm0905152"}) {
		t.Errorf("people = %v", people)
	}
	g.do(KindReveal, "")
	films, people = p.Wants(g.record())
	if !slices.Contains(films, "tt0133093") || len(films) != 3+1+len(p.Cards) || len(people) != len(p.People) {
		t.Errorf("at the end: %d films, %d people", len(films), len(people))
	}
}
