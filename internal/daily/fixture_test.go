package daily

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// matrix is a small puzzle with the shapes that matter: two directors,
// the six in reveal order, a close relative on the sheets, a movie two
// of the six share, a movie only a director shares with one of them,
// and an "Also in" for each of the six.
//
// Slots, sixth-billed first: 0 Joe Pantoliano, 1 Gloria Foster, 2 Hugo
// Weaving, 3 Carrie-Anne Moss, 4 Laurence Fishburne, 5 Keanu Reeves,
// the star.
func matrix() *Puzzle {
	p := &Puzzle{
		No:  143,
		Day: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC),
		Answer: Answer{
			ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331, Length: 136, Colour: "#26382d",
			Genres: []string{"Action", "Sci-Fi"},
		},
		Directors: []Named{{ID: "nm0905154", Name: "Lana Wachowski"}, {ID: "nm0905152", Name: "Lilly Wachowski"}},
		Cast: []Billed{
			{ID: "nm0001592", Name: "Joe Pantoliano", Billing: 6, Also: &Also{ID: "tt0106977", Title: "The Fugitive", Year: 1993}},
			{ID: "nm0287825", Name: "Gloria Foster", Billing: 5, Also: &Also{ID: "tt0067433", Title: "Man and Boy", Year: 1971}},
			{ID: "nm0915989", Name: "Hugo Weaving", Billing: 4, Also: &Also{ID: "tt0434409", Title: "V for Vendetta", Year: 2005}},
			{ID: "nm0005251", Name: "Carrie-Anne Moss", Billing: 3, Also: &Also{ID: "tt0241303", Title: "Chocolat", Year: 2000}},
			{ID: "nm0000401", Name: "Laurence Fishburne", Billing: 2, Also: &Also{ID: "tt0078788", Title: "Apocalypse Now", Year: 1979}},
			{ID: "nm0000206", Name: "Keanu Reeves", Billing: 1, Also: &Also{ID: "tt2911666", Title: "John Wick", Year: 2014}},
		},
		Movies: []Movie{
			{ID: "tt0067433", Title: "Man and Boy", Year: 1971, Rating: 5.5, Genres: []string{"Drama"}, Cast: []int{1}},
			{ID: "tt0078788", Title: "Apocalypse Now", Year: 1979, Rating: 8.4, MD: 815, Genres: []string{"Drama", "War"}, Cast: []int{4}},
			{ID: "tt0106977", Title: "The Fugitive", Year: 1993, Rating: 7.8, MD: 806, Genres: []string{"Action", "Crime"}, Cast: []int{0}},
			{ID: "tt0111257", Title: "Speed", Year: 1994, Rating: 7.3, MD: 610, Genres: []string{"Action", "Thriller"}, Cast: []int{5}},
			{ID: "tt0115736", Title: "Bound", Year: 1996, Rating: 7.3, MD: 913, Genres: []string{"Crime", "Thriller"}, Cast: []int{0}, Dir: true},
			{ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331, Genres: []string{"Action", "Sci-Fi"}, Cast: []int{0, 1, 2, 3, 4, 5}, Dir: true},
			{ID: "tt0209144", Title: "Memento", Year: 2000, Rating: 8.4, MD: 1011, Genres: []string{"Mystery", "Thriller"}, Cast: []int{0, 3}},
			{ID: "tt0241303", Title: "Chocolat", Year: 2000, Rating: 7.2, MD: 1215, Genres: []string{"Drama", "Romance"}, Cast: []int{3}},
			{ID: "tt0120737", Title: "The Lord of the Rings: The Fellowship of the Ring", Year: 2001, Rating: 8.9, MD: 1219, Genres: []string{"Adventure", "Fantasy"}, Cast: []int{2}},
			{ID: "tt0234215", Title: "The Matrix Reloaded", Year: 2003, Rating: 7.2, MD: 515, Genres: []string{"Action", "Sci-Fi"}, Cast: []int{1, 2, 3, 4, 5}, Dir: true},
			{ID: "tt0434409", Title: "V for Vendetta", Year: 2005, Rating: 8.1, MD: 317, Genres: []string{"Action", "Drama"}, Cast: []int{2}},
			{ID: "tt1371111", Title: "Cloud Atlas", Year: 2012, Rating: 7.4, MD: 1026, Genres: []string{"Drama", "Sci-Fi"}, Cast: []int{2}, Dir: true},
			{ID: "tt2911666", Title: "John Wick", Year: 2014, Rating: 7.4, MD: 1024, Genres: []string{"Action", "Thriller"}, Cast: []int{5}},
		},
		Era:   1995,
		Genre: "Action",
	}
	// None of the six is near their cap, so every movie is on the sheet
	// of everyone it credits.
	for i := range p.Movies {
		p.Movies[i].Sheets = slices.Clone(p.Movies[i].Cast)
	}
	return p
}

// films is what the catalog would say about the movies a test guesses:
// who of the six each credits, by id, and its genres.
var films = map[string]Looked{
	"tt0234215": {Title: "The Matrix Reloaded", Year: 2003, Genres: []string{"Action", "Sci-Fi"},
		Credited: []string{"nm0287825", "nm0915989", "nm0005251", "nm0000401", "nm0000206"}},
	"tt0111257": {Title: "Speed", Year: 1994, Genres: []string{"Action", "Adventure", "Thriller"}, Credited: []string{"nm0000206"}},
	"tt0034583": {Title: "Casablanca", Year: 1942, Genres: []string{"Drama", "Romance", "War"}},
	"tt0209144": {Title: "Memento", Year: 2000, Genres: []string{"Mystery", "Thriller"}, Credited: []string{"nm0005251", "nm0001592"}},
	"tt0120601": {Title: "Being John Malkovich", Year: 1999, Genres: []string{"Comedy", "Drama", "Fantasy"}},
	"tt0076759": {Title: "Star Wars", Year: 1977, Genres: []string{"Action", "Adventure", "Fantasy"}},
	"tt0115736": {Title: "Bound", Year: 1996, Genres: []string{"Crime", "Thriller"}, Credited: []string{"nm0001592"}},
	"tt9000002": {Title: "Nor This, Nor When"},
}

// factKinds are the facts "buy" sells, in the order the page lists them.
var factKinds = []string{KindLength, KindRating, KindGenre, KindDecade, KindDirector}

// player plays a game the way the store does: each move checked against
// the game as it stands, priced, recorded and taken.
type player struct {
	t     *testing.T
	p     *Puzzle
	moves []Move
	key   int
}

func play(t *testing.T, p *Puzzle) *player { return &player{t: t, p: p} }

// try asks for a move and returns the refusal, if any.
func (g *player) try(kind, arg string) error {
	g.t.Helper()
	s := Replay(g.p, g.moves)
	var looked *Looked
	if kind == KindGuess {
		if l, ok := films[arg]; ok {
			looked = &l
		}
	}
	g.key++
	m, err := Apply(g.p, s, Request{Key: fmt.Sprintf("key-%04d", g.key), Seq: len(g.moves), Kind: kind, Arg: arg}, looked)
	if err != nil {
		return err
	}
	m.Seq = len(g.moves) + 1
	g.moves = append(g.moves, m)
	return nil
}

// do is try for a move the test expects to be taken.
func (g *player) do(kind, arg string) *State {
	g.t.Helper()
	if err := g.try(kind, arg); err != nil {
		g.t.Fatalf("%s %s: %v", kind, arg, err)
	}
	return g.state()
}

func (g *player) state() *State { return Replay(g.p, g.moves) }

func (g *player) record() *Record {
	return &Record{Started: time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC), Moves: g.moves}
}

// shownSlots are the slots a state shows, in slot order.
func shownSlots(s *State) []int {
	var out []int
	for i := range Slots {
		if s.Shown(i) {
			out = append(out, i)
		}
	}
	return out
}
