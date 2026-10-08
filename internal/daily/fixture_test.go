package daily

import (
	"fmt"
	"testing"
	"time"
)

// matrix is a small puzzle with the shapes that matter: two directors,
// six cast, a close relative, a card only a director is on, cards at
// both ends of the price range, and three starting cards each through
// a different actor.
//
// Slots: 0 Lana and 1 Lilly Wachowski, directors; 2 Keanu Reeves,
// 3 Laurence Fishburne, 4 Carrie-Anne Moss, 5 Hugo Weaving, 6 Gloria
// Foster, 7 Joe Pantoliano, cast.
func matrix() *Puzzle {
	return &Puzzle{
		No:  142,
		Day: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC),
		Answer: Answer{
			ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331,
			Genres: []string{"Action", "Sci-Fi"},
		},
		People: []Slot{
			{ID: "nm0905154", Name: "Lana Wachowski", Role: RoleDirector},
			{ID: "nm0905152", Name: "Lilly Wachowski", Role: RoleDirector},
			{ID: "nm0000206", Name: "Keanu Reeves", Role: RoleCast},
			{ID: "nm0000401", Name: "Laurence Fishburne", Role: RoleCast},
			{ID: "nm0005251", Name: "Carrie-Anne Moss", Role: RoleCast},
			{ID: "nm0915989", Name: "Hugo Weaving", Role: RoleCast},
			{ID: "nm0287825", Name: "Gloria Foster", Role: RoleCast},
			{ID: "nm0001592", Name: "Joe Pantoliano", Role: RoleCast},
		},
		Cards: []Card{
			{ID: "c1", Film: "tt0234215", Title: "The Matrix Reloaded", Year: 2003, Rating: 7.2, MD: 515, Votes: 600000, People: []int{0, 1, 2, 3, 4, 5, 6}},
			{ID: "c2", Film: "tt0067433", Title: "Man and Boy", Year: 1971, Rating: 5.5, MD: 0, Votes: 900, People: []int{6}},
			{ID: "c3", Film: "tt0108065", Title: "Searching for Bobby Fischer", Year: 1993, Rating: 7.3, MD: 811, Votes: 40000, People: []int{3}},
			{ID: "c4", Film: "tt0109190", Title: "Baby's Day Out", Year: 1994, Rating: 6.3, MD: 701, Votes: 60000, People: []int{7}},
			{ID: "c5", Film: "tt0111257", Title: "Speed", Year: 1994, Rating: 7.3, MD: 610, Votes: 400000, People: []int{2}},
			{ID: "c6", Film: "tt0115736", Title: "Bound", Year: 1996, Rating: 7.3, MD: 913, Votes: 80000, People: []int{0, 1, 7}},
			{ID: "c7", Film: "tt0209144", Title: "Memento", Year: 2000, Rating: 8.4, MD: 1011, Votes: 1400000, People: []int{4, 7}},
			{ID: "c8", Film: "tt0120737", Title: "The Lord of the Rings: The Fellowship of the Ring", Year: 2001, Rating: 8.9, MD: 1219, Votes: 2100000, People: []int{5}},
			{ID: "c9", Film: "tt1371111", Title: "Cloud Atlas", Year: 2012, Rating: 7.4, MD: 1026, Votes: 380000, People: []int{0, 1}},
			{ID: "c10", Film: "tt0090000", Title: "A Cheap Thriller", Year: 1988, Rating: 4.1, MD: 0, Votes: 1200, People: []int{3}},
		},
		Start: []string{"c2", "c3", "c4"},
		Era:   1995,
		Genre: "Action",
	}
}

// films is what the catalog would say about the movies a test guesses.
var films = map[string]Looked{
	"tt0234215": {Title: "The Matrix Reloaded", Year: 2003, Rating: ptr(7.2), MD: 515,
		Credited: []string{"nm0905154", "nm0905152", "nm0000206", "nm0000401", "nm0005251", "nm0915989", "nm0287825"}},
	"tt0111257": {Title: "Speed", Year: 1994, Rating: ptr(7.3), MD: 610, Credited: []string{"nm0000206"}},
	"tt0034583": {Title: "Casablanca", Year: 1942, Rating: ptr(8.5), MD: 1126},
	"tt0209144": {Title: "Memento", Year: 2000, Rating: ptr(8.4), MD: 1011, Credited: []string{"nm0005251", "nm0001592"}},
	"tt9000001": {Title: "Nobody Rated This", Year: 1999},
	"tt9000002": {Title: "Nor This, Nor When"},
}

func ptr[T any](v T) *T { return &v }

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
	return &Record{Started: time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC), Moves: g.moves}
}
