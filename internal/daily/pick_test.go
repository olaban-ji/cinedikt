package daily

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

var oct8 = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

func candidates() []Candidate {
	var out []Candidate
	for i, era := range []int{1920, 1960, 1980, 1995, 2005, 2013, 2019, 1960, 2024, 1980, 1995, 2005} {
		genre := []string{"Drama", "Comedy", "Action"}[i%3]
		out = append(out, Candidate{ID: fmt.Sprintf("tt%07d", 100+i), Title: fmt.Sprintf("Movie %d", i), Era: era, Genres: []string{genre, "Romance"}})
	}
	return out
}

// TestTheOrderIsTheDaysOwn: the same day orders the same candidates the
// same way whatever order they arrive in, and another day differently.
func TestTheOrderIsTheDaysOwn(t *testing.T) {
	ids := func(cs []Candidate) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.ID)
		}
		return out
	}
	cands := candidates()
	first := ids(Order(oct8, cands, Recent{}))
	reversed := slices.Clone(cands)
	slices.Reverse(reversed)
	if again := ids(Order(oct8, reversed, Recent{})); !slices.Equal(first, again) {
		t.Errorf("the same day ordered %v, then %v", first, again)
	}
	if next := ids(Order(oct8.AddDate(0, 0, 1), cands, Recent{})); slices.Equal(first, next) {
		t.Errorf("two days ordered their candidates the same: %v", first)
	}
	if len(first) != len(cands) {
		t.Errorf("%d of %d candidates kept", len(first), len(cands))
	}
}

// TestTheOrderMixesTheWeek: a fresh era and a fresh first genre first,
// then a fresh era in any genre, then the rest; and a recent answer not
// at all.
func TestTheOrderMixesTheWeek(t *testing.T) {
	cands := candidates()
	recent := Recent{
		Answers: map[string]bool{"tt0000100": true},
		Eras:    map[int]bool{1960: true, 1980: true, 1995: true, 2005: true, 2013: true, 2019: true},
		Genres:  map[string]bool{"Drama": true, "Comedy": true},
	}
	got := Order(oct8, cands, recent)
	tier := func(c Candidate) int {
		t := 0
		if recent.Eras[c.Era] {
			t += 2
		}
		if recent.Genres[c.Genres[0]] {
			t++
		}
		return t
	}
	for i := 1; i < len(got); i++ {
		if tier(got[i-1]) > tier(got[i]) {
			t.Errorf("%s (tier %d) comes before %s (tier %d)", got[i].ID, tier(got[i]), got[i-1].ID, tier(got[i-1]))
		}
	}
	for _, c := range got {
		if c.ID == "tt0000100" {
			t.Error("an answer from the last ninety days is offered again")
		}
	}
	if got[0].Era != 2024 || got[0].Genres[0] != "Action" {
		t.Errorf("first = %+v, want the one candidate in a fresh era and genre", got[0])
	}
}

// matrixPeople are slots for a candidate: one director and four cast.
var matrixPeople = []Slot{
	{ID: "nm1", Name: "Director One", Role: RoleDirector},
	{ID: "nm2", Name: "Actor Two", Role: RoleCast},
	{ID: "nm3", Name: "Actor Three", Role: RoleCast},
	{ID: "nm4", Name: "Actor Four", Role: RoleCast},
	{ID: "nm5", Name: "Actor Five", Role: RoleCast},
}

var answer = Candidate{
	ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331, Era: 1995,
	Genres: []string{"Action", "Sci-Fi"}, Votes: 2000000,
}

// mapOf is a map of n rated movies, each through one person in turn, the
// answer itself first as the spine has it, two unrated movies and a
// close relative.
func mapOf(n int) []MapFilm {
	r := func(v float64) *float64 { return &v }
	films := []MapFilm{{ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: r(8.7), Votes: 2000000, People: []int{0, 1, 2, 3, 4}}}
	for i := range n {
		films = append(films, MapFilm{
			ID: fmt.Sprintf("tt1%06d", i), Title: fmt.Sprintf("Linked %d", i), Year: 1980 + i%40,
			Rating: r(5 + float64(i%40)/10), Votes: 1000 * (i + 1), People: []int{i % 5},
		})
	}
	films = append(films,
		MapFilm{ID: "tt2000001", Title: "Unrated One", Year: 2020, Votes: 3, People: []int{1}},
		MapFilm{ID: "tt2000002", Title: "Unrated Two", Year: 2021, Votes: 4, People: []int{2}},
		MapFilm{ID: "tt0234215", Title: "The Matrix Reloaded", Year: 2003, Rating: r(7.2), Votes: 2, People: []int{4, 0, 1, 2}},
	)
	return films
}

func TestBuildDealsTheBoard(t *testing.T) {
	p, err := Build(142, oct8.Add(15*time.Hour), answer, matrixPeople, mapOf(45))
	if err != nil {
		t.Fatal(err)
	}
	if p.No != 142 || !p.Day.Equal(oct8) || p.Answer.ID != "tt0133093" || p.Era != 1995 || p.Genre != "Action" {
		t.Errorf("puzzle = %+v", p)
	}
	// Forty-five linked movies and the relative: the answer and the two
	// unrated movies are left off.
	if len(p.Cards) != 46 {
		t.Errorf("%d cards, want 46", len(p.Cards))
	}
	seen := map[string]bool{}
	for i, c := range p.Cards {
		if c.ID != fmt.Sprintf("c%d", i+1) {
			t.Errorf("card %d is %s, want the ids in order", i, c.ID)
		}
		if c.Film == "tt0133093" || strings.HasPrefix(c.Film, "tt20000") {
			t.Errorf("%s (%s) is on the board", c.Film, c.Title)
		}
		seen[c.Film] = true
	}
	// The relative's people are sorted into slot order.
	for _, c := range p.Cards {
		if c.Film == "tt0234215" && (!slices.Equal(c.People, []int{0, 1, 2, 4}) || !c.Relative()) {
			t.Errorf("the relative = %+v", c)
		}
	}
	// The deal hides how well known a card is: c1 is not simply the
	// first movie the map listed.
	if p.Cards[0].Film == "tt1000000" && p.Cards[1].Film == "tt1000001" {
		t.Error("the card ids follow the map's own order")
	}
	again, err := Build(142, oct8, answer, matrixPeople, mapOf(45))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(p.Cards, again.Cards, func(a, b Card) bool { return a.ID == b.ID && a.Film == b.Film }) {
		t.Error("the same day dealt the same map two ways")
	}
}

// TestTheStartingCardsAreTheLeastKnownThroughDifferentPeople: the
// relative has the fewest votes of all and is passed over, and each
// card after the first shares nobody with those already taken.
func TestTheStartingCardsAreTheLeastKnownThroughDifferentPeople(t *testing.T) {
	p, err := Build(142, oct8, answer, matrixPeople, mapOf(45))
	if err != nil {
		t.Fatal(err)
	}
	var films []string
	for _, id := range p.Start {
		films = append(films, p.card(id).Film)
	}
	// Linked 0, 1 and 2 have the fewest votes, through slots 0, 1 and 2.
	if want := []string{"tt1000000", "tt1000001", "tt1000002"}; !slices.Equal(films, want) {
		t.Errorf("starting cards = %v, want %v", films, want)
	}

	cards := []Card{
		{ID: "c1", Film: "tt1", Votes: 10, People: []int{2, 3}},
		{ID: "c2", Film: "tt2", Votes: 20, People: []int{3}},
		{ID: "c3", Film: "tt3", Votes: 30, People: []int{4}},
		{ID: "c4", Film: "tt4", Votes: 5, People: []int{1, 5, 6}},
		{ID: "c5", Film: "tt5", Votes: 30, People: []int{5}},
		{ID: "c6", Film: "tt0", Votes: 30, People: []int{6}},
	}
	// c4 is a relative; c2 shares Fishburne with c1; c6 and c3 tie on
	// votes, and c6's id sorts first.
	if got := Starts(cards); !slices.Equal(got, []string{"c1", "c6", "c3"}) {
		t.Errorf("Starts = %v, want c1, c6, c3", got)
	}
}

func TestBuildRefusesAMapThatIsNoGame(t *testing.T) {
	thin := mapOf(38)
	noStarts := mapOf(45)
	for i := range noStarts {
		noStarts[i].People = []int{0}
	}
	for _, c := range []struct {
		name   string
		a      Candidate
		people []Slot
		films  []MapFilm
		why    string
	}{
		{"a thin map", answer, matrixPeople, thin, "39 rated movies"},
		{"no director", answer, matrixPeople[1:], mapOf(45), "no director"},
		{"too few cast", answer, matrixPeople[:3], mapOf(45), "2 billed cast"},
		{"one way in", answer, matrixPeople, noStarts, "no three starting cards"},
	} {
		_, err := Build(1, oct8, c.a, c.people, c.films)
		var unfit Unfit
		if !errors.As(err, &unfit) || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v, want a refusal saying %q", c.name, err, c.why)
		}
	}
}
