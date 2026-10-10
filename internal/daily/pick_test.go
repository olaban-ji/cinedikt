package daily

import (
	"errors"
	"fmt"
	"reflect"
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

// TestAnOrderDrawnFromAnotherGeneratorKeepsTheRules: development's Play
// again shuffles with a generator of its own rather than the day's, so
// two draws differ from the day's order and from each other, while the
// week's mix still comes first and a recent answer is still left out.
func TestAnOrderDrawnFromAnotherGeneratorKeepsTheRules(t *testing.T) {
	cands := candidates()
	recent := Recent{Answers: map[string]bool{"tt0000100": true}, Eras: map[int]bool{1960: true}, Genres: map[string]bool{"Drama": true}}
	ids := func(cs []Candidate) string {
		var out []string
		for _, c := range cs {
			out = append(out, c.ID)
		}
		return strings.Join(out, " ")
	}
	day := ids(Order(oct8, cands, recent))
	if same := ids(OrderBy(Seeded(DayString(oct8)), cands, recent)); same != day {
		t.Errorf("the day's own generator ordered %s, Order %s", same, day)
	}
	one, two := OrderBy(Seeded("one draw"), cands, recent), OrderBy(Seeded("another"), cands, recent)
	if ids(one) == day || ids(two) == day || ids(one) == ids(two) {
		t.Errorf("the day ordered %s; two draws %s and %s", day, ids(one), ids(two))
	}
	for _, got := range [][]Candidate{one, two} {
		if len(got) != len(cands)-1 || strings.Contains(ids(got), "tt0000100") {
			t.Errorf("a draw kept %s", ids(got))
		}
		stale := false
		for _, c := range got {
			old := recent.Eras[c.Era] || recent.Genres[c.Genres[0]]
			if !old && stale {
				t.Errorf("a fresh candidate comes after a used era or genre in %s", ids(got))
			}
			stale = stale || old
		}
	}
}

// billed is a candidate's billed cast in billing order, the star first:
// seven, so the six are the first six and the seventh is left out.
var billed = []Named{
	{ID: "nm0000206", Name: "Keanu Reeves"},
	{ID: "nm0000401", Name: "Laurence Fishburne"},
	{ID: "nm0005251", Name: "Carrie-Anne Moss"},
	{ID: "nm0915989", Name: "Hugo Weaving"},
	{ID: "nm0287825", Name: "Gloria Foster"},
	{ID: "nm0001592", Name: "Joe Pantoliano"},
	{ID: "nm0324658", Name: "Marcus Chong"},
}

var wachowskis = []Named{{ID: "nm0905154", Name: "Lana Wachowski"}, {ID: "nm0905152", Name: "Lilly Wachowski"}}

var answer = Candidate{
	ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331, Length: 136, Colour: "#26382d",
	Poster: "https://img.example/matrix.jpg", Era: 1995, Genres: []string{"Action", "Sci-Fi"}, Votes: 2000000,
}

// sheets are the six's movies as the catalog reads them: the answer,
// crediting everyone; a sequel, a close relative and the most voted of
// all; Keanu's John Wick and Speed, and a Keanu movie both Wachowskis
// made, a close relative too; a movie Moss and Pantoliano share, which
// is neither one's "Also in"; two of Weaving's with the same votes;
// Foster's one, through the seventh-billed as well; nothing of
// Fishburne's own, only the sequel and three he made with Foster; two
// Pantoliano made, one with Weaving and one with Moss; and the crowd.
// So each of the six has MinSheet movies besides the answer and the
// crowd, and no more.
func sheets() []MapFilm {
	return append([]MapFilm{
		{ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331, Genres: []string{"Action", "Sci-Fi"}, Votes: 2000000,
			People: []string{"nm0905154", "nm0905152", "nm0000206", "nm0000401", "nm0005251", "nm0915989", "nm0287825", "nm0001592", "nm0324658"}},
		{ID: "tt0234215", Title: "The Matrix Reloaded", Year: 2003, Rating: 7.2, Genres: []string{"Action", "Sci-Fi"}, Votes: 9000000,
			People: []string{"nm0905154", "nm0905152", "nm0000206", "nm0000401", "nm0005251", "nm0915989"}},
		{ID: "tt2911666", Title: "John Wick", Year: 2014, Rating: 7.4, Genres: []string{"Action", "Thriller"}, Votes: 750000,
			People: []string{"nm0000206"}},
		{ID: "tt0111257", Title: "Speed", Year: 1994, Rating: 7.3, Genres: []string{"Action", "Thriller"}, Votes: 400000,
			People: []string{"nm0000206"}},
		{ID: "tt9000001", Title: "A Wachowski Keanu Movie", Year: 2020, Rating: 6.1, Votes: 5000000,
			People: []string{"nm0000206", "nm0905154", "nm0905152"}},
		{ID: "tt0209144", Title: "Memento", Year: 2000, Rating: 8.4, Genres: []string{"Mystery", "Thriller"}, Votes: 1400000,
			People: []string{"nm0005251", "nm0001592"}},
		{ID: "tt0241303", Title: "Chocolat", Year: 2000, Rating: 7.2, Votes: 200000, People: []string{"nm0005251"}},
		{ID: "tt0106977", Title: "The Fugitive", Year: 1993, Rating: 7.8, Votes: 330000, People: []string{"nm0001592"}},
		{ID: "tt0434409", Title: "V for Vendetta", Year: 2005, Rating: 8.1, Votes: 1200000, People: []string{"nm0915989"}},
		{ID: "tt0120737", Title: "The Lord of the Rings", Year: 2001, Rating: 8.9, Votes: 1200000, People: []string{"nm0915989"}},
		{ID: "tt0067433", Title: "Man and Boy", Year: 1971, Rating: 5.5, Votes: 900, People: []string{"nm0287825", "nm0324658"}},
		{ID: "tt9000003", Title: "A Fishburne Foster Movie", Year: 1985, Rating: 6.2, Votes: 500, People: []string{"nm0000401", "nm0287825"}},
		{ID: "tt9000004", Title: "Another Fishburne Foster Movie", Year: 1986, Rating: 6.4, Votes: 500, People: []string{"nm0000401", "nm0287825"}},
		{ID: "tt9000005", Title: "A Third Fishburne Foster Movie", Year: 1987, Rating: 6.6, Votes: 500, People: []string{"nm0287825", "nm0000401"}},
		{ID: "tt9000006", Title: "A Pantoliano Weaving Movie", Year: 1988, Rating: 5.9, Votes: 500, People: []string{"nm0001592", "nm0915989"}},
		{ID: "tt9000007", Title: "A Pantoliano Moss Movie", Year: 1989, Rating: 6.1, Votes: 500, People: []string{"nm0001592", "nm0005251"}},
		// Only the seventh-billed and a director: on no one's sheet.
		{ID: "tt9000002", Title: "Nobody's", Year: 2010, Rating: 6.0, Votes: 100, People: []string{"nm0324658", "nm0905154"}},
	}, crowd()...)
}

// crowd are three movies all six made in the 1990s, rated 8.0 or more,
// so inside The Matrix's decade and its rating band: as many as MinCrowd
// asks of every one of the six's sheets, and the only ones Foster,
// Weaving, Moss and Fishburne have there. Crediting all six, none is
// anyone's "Also in".
func crowd() []MapFilm {
	six := func() []string {
		return []string{"nm0000206", "nm0000401", "nm0005251", "nm0915989", "nm0287825", "nm0001592"}
	}
	return []MapFilm{
		{ID: "tt9100001", Title: "The Six in 1990", Year: 1990, Rating: 8.0, Genres: []string{"Drama"}, Votes: 50000, People: six()},
		{ID: "tt9100002", Title: "The Six in 1994", Year: 1994, Rating: 8.4, Genres: []string{"Drama"}, Votes: 50000, People: six()},
		{ID: "tt9100003", Title: "The Six in 1998", Year: 1998, Rating: 9.0, Genres: []string{"Drama"}, Votes: 50000, People: six()},
	}
}

var oct9 = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

// TestBuildKeepsTheSixInRevealOrder: the first six billed, the star
// last, each with their billing; the directors in crew order; and the
// answer's length, colour and genres.
func TestBuildKeepsTheSixInRevealOrder(t *testing.T) {
	p, err := Build(143, oct9.Add(15*time.Hour), answer, billed, wachowskis, sheets())
	if err != nil {
		t.Fatal(err)
	}
	if p.No != 143 || !p.Day.Equal(oct9) || p.Era != 1995 || p.Genre != "Action" {
		t.Errorf("puzzle = %+v", p)
	}
	if a := p.Answer; a.ID != "tt0133093" || a.Length != 136 || a.Colour != "#26382d" || a.Rating != 8.7 || !slices.Equal(a.Genres, []string{"Action", "Sci-Fi"}) {
		t.Errorf("answer = %+v", a)
	}
	var who []string
	for i, b := range p.Cast {
		who = append(who, fmt.Sprintf("%s %d", b.Name, b.Billing))
		if p.SlotOf(b.ID) != i {
			t.Errorf("%s is in slot %d, SlotOf says %d", b.Name, i, p.SlotOf(b.ID))
		}
	}
	want := []string{"Joe Pantoliano 6", "Gloria Foster 5", "Hugo Weaving 4", "Carrie-Anne Moss 3", "Laurence Fishburne 2", "Keanu Reeves 1"}
	if !slices.Equal(who, want) {
		t.Errorf("cast = %v, want %v", who, want)
	}
	if p.SlotOf("nm0324658") != -1 || p.SlotOf("nm0905154") != -1 {
		t.Error("the seventh-billed or a director has a slot")
	}
	if !slices.Equal(p.DirectorIDs(), []string{"nm0905154", "nm0905152"}) {
		t.Errorf("directors = %v", p.Directors)
	}
	again, err := Build(143, oct9, answer, billed, wachowskis, sheets())
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Errorf("the same candidate built twice differs: %v", err)
	}
}

// TestEachOfTheSixIsAlsoInTheirMostVotedMovieOfTheirOwn: never one that
// credits another of the six, never the answer, never a close relative,
// however well known; ties fall to the lower id, and someone with
// nothing of their own has no "Also in".
func TestEachOfTheSixIsAlsoInTheirMostVotedMovieOfTheirOwn(t *testing.T) {
	p, err := Build(143, oct9, answer, billed, wachowskis, sheets())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, b := range p.Cast {
		if b.Also != nil {
			got[b.Name] = fmt.Sprintf("%s %s %d", b.Also.ID, b.Also.Title, b.Also.Year)
		}
	}
	want := map[string]string{
		// John Wick, not the Wachowskis' movie, a close relative with
		// more votes, nor the sequel, nor the answer.
		"Keanu Reeves": "tt2911666 John Wick 2014",
		// Chocolat, not Memento, which Pantoliano is in too.
		"Carrie-Anne Moss": "tt0241303 Chocolat 2000",
		"Joe Pantoliano":   "tt0106977 The Fugitive 1993",
		// The Lord of the Rings and V for Vendetta tie, and tt0120737
		// sorts first.
		"Hugo Weaving": "tt0120737 The Lord of the Rings 2001",
		// The seventh-billed is not one of the six.
		"Gloria Foster": "tt0067433 Man and Boy 1971",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("also = %v\nwant   %v", got, want)
	}
	if fish := p.Cast[p.SlotOf("nm0000401")]; fish.Also != nil {
		t.Errorf("Fishburne, with nothing of his own, is also in %+v", fish.Also)
	}
}

// TestAnAlsoInNeverSharesTheAnswersTitle: a franchise movie that shares
// only one person is no close relative, but shown as someone's "Also in"
// it names the answer. So it is passed over, however well known, for
// their next most voted.
func TestAnAlsoInNeverSharesTheAnswersTitle(t *testing.T) {
	films := append(sheets(),
		MapFilm{ID: "tt9000008", Title: "The Matrix: Pantoliano's Story", Year: 2004, Rating: 6.0, Votes: 990000,
			People: []string{"nm0001592"}},
		MapFilm{ID: "tt9000009", Title: "Matrices", Year: 2006, Rating: 5.5, Votes: 980000,
			People: []string{"nm0005251"}},
	)
	p, err := Build(143, oct9, answer, billed, wachowskis, films)
	if err != nil {
		t.Fatal(err)
	}
	if also := p.Cast[p.SlotOf("nm0001592")].Also; also == nil || also.ID != "tt0106977" {
		t.Errorf("Pantoliano is also in %+v, want The Fugitive", also)
	}
	// "Matrices" is another word, not the answer's title.
	if also := p.Cast[p.SlotOf("nm0005251")].Also; also == nil || also.ID != "tt9000009" {
		t.Errorf("Moss is also in %+v, want Matrices", also)
	}
}

// TestTitlesAreSharedWordForWord: a sequel, a subtitle, a dropped
// article or a plural is the same title; a longer word that only starts
// the same way is not.
func TestTitlesAreSharedWordForWord(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"Halloween", "Halloween III: Season of the Witch", true},
		{"The Matrix", "The Matrix Reloaded", true},
		{"The Matrix", "Matrix", true},
		{"Alien", "Aliens", true},
		{"Toy Story 3", "Toy Story", true},
		{"The Godfather Part II", "THE GODFATHER", true},
		{"Up", "Up in the Air", true},
		{"Heat", "Heathers", false},
		{"Carrie", "Fried Green Tomatoes", false},
		{"Glass", "Glass Onion", true},
		{"Grass", "Glass", false},
		{"", "Anything", false},
	} {
		if got := sharesTitle(c.a, c.b); got != c.want {
			t.Errorf("sharesTitle(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestTheSheetsAreTheSixsMoviesWithTheAnswerUnmarked: each movie says
// which of the six it credits as slots, whose sheets it is on, here
// everyone it credits, and whether a director is on it; the answer is
// one of them, crediting everyone, as the sequel nearly does; a movie
// none of the six is on is on no sheet; and the order is by year, so
// nothing in it says which movie is best known.
func TestTheSheetsAreTheSixsMoviesWithTheAnswerUnmarked(t *testing.T) {
	p, err := Build(143, oct9, answer, billed, wachowskis, sheets())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range p.Movies {
		got = append(got, fmt.Sprintf("%d %s %v %v %v", m.Year, m.ID, m.Cast, m.Sheets, m.Dir))
	}
	want := []string{
		"1971 tt0067433 [1] [1] false",
		"1985 tt9000003 [1 4] [1 4] false",
		"1986 tt9000004 [1 4] [1 4] false",
		"1987 tt9000005 [1 4] [1 4] false",
		"1988 tt9000006 [0 2] [0 2] false",
		"1989 tt9000007 [0 3] [0 3] false",
		"1990 tt9100001 [0 1 2 3 4 5] [0 1 2 3 4 5] false",
		"1993 tt0106977 [0] [0] false",
		"1994 tt0111257 [5] [5] false",
		"1994 tt9100002 [0 1 2 3 4 5] [0 1 2 3 4 5] false",
		"1998 tt9100003 [0 1 2 3 4 5] [0 1 2 3 4 5] false",
		"1999 tt0133093 [0 1 2 3 4 5] [0 1 2 3 4 5] true",
		"2000 tt0209144 [0 3] [0 3] false",
		"2000 tt0241303 [3] [3] false",
		"2001 tt0120737 [2] [2] false",
		"2003 tt0234215 [2 3 4 5] [2 3 4 5] true",
		"2005 tt0434409 [2] [2] false",
		"2014 tt2911666 [5] [5] false",
		"2020 tt9000001 [5] [5] true",
	}
	if !slices.Equal(got, want) {
		t.Errorf("movies =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, m := range p.Movies {
		if m.Genres == nil || m.Cast == nil || m.Sheets == nil {
			t.Errorf("%s has a null list: %+v", m.ID, m)
		}
	}
	// Without the answer in what the catalog read, it is made from the
	// candidate, so every sheet still has it.
	without := slices.DeleteFunc(sheets(), func(f MapFilm) bool { return f.ID == answer.ID })
	q, err := Build(143, oct9, answer, billed, wachowskis, without)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(q.Movies, func(m Movie) bool { return m.ID == answer.ID })
	if i < 0 || !slices.Equal(q.Movies[i].Cast, []int{0, 1, 2, 3, 4, 5}) || !slices.Equal(q.Movies[i].Sheets, []int{0, 1, 2, 3, 4, 5}) ||
		!q.Movies[i].Dir || q.Movies[i].Rating != 8.7 {
		t.Errorf("the answer made from the candidate = %+v", q.Movies)
	}
}

// TestEachSheetIsItsOwnFourHundredMostVoted: four hundred and ten more
// of Pantoliano's, the least voted of his, leave his eighteen fewest off
// his sheet, though Foster is on the ten fewest and her sheet, far from
// full, has them. On his they would be cards past his cap, there only
// because someone else of the six is in them, and his sheet run past
// MaxSheet would say that such cards were there to find.
// The other five are on nobody's sheet and are not kept; and the answer,
// made the least voted of all, is on every sheet still.
func TestEachSheetIsItsOwnFourHundredMostVoted(t *testing.T) {
	more := sheets()
	more[0].Votes = 0
	for n := 1; n <= MaxSheet+10; n++ {
		people := []string{"nm0001592"}
		if n <= 10 {
			people = append(people, "nm0287825")
		}
		more = append(more, MapFilm{ID: fmt.Sprintf("tt97%05d", n), Title: fmt.Sprintf("Extra %d", n), Year: 1950 + n%70,
			Rating: 6, Genres: []string{"Drama"}, Votes: n, People: people})
	}
	p, err := Build(143, oct9, answer, billed, wachowskis, more)
	if err != nil {
		t.Fatal(err)
	}
	extra := func(id string) int {
		n := 0
		fmt.Sscanf(id, "tt97%05d", &n)
		return n
	}
	// Once it is over, every card on a sheet is readable: the whole sheet.
	over := Replay(p, []Move{{Seq: 1, Key: "k", Kind: KindReveal}})
	joe := p.SheetWants(over, 0)
	if len(joe) != MaxSheet || !slices.Contains(joe, answer.ID) {
		t.Errorf("Pantoliano's sheet has %d movies, the answer among them %v; want %d", len(joe), slices.Contains(joe, answer.ID), MaxSheet)
	}
	for _, id := range joe {
		if n := extra(id); n >= 1 && n <= 18 {
			t.Errorf("Extra %d, among the eighteen least voted of his, is on Pantoliano's sheet", n)
		}
	}
	// Hers: the answer, Man and Boy, the three with Fishburne, the crowd
	// and the ten.
	gloria := p.SheetWants(over, 1)
	for n := 1; n <= 10; n++ {
		if !slices.Contains(gloria, fmt.Sprintf("tt97%05d", n)) {
			t.Errorf("Extra %d, which Foster is on, is not on her sheet", n)
		}
	}
	if len(gloria) != 18 {
		t.Errorf("Foster's sheet has %d movies, want 18: %v", len(gloria), gloria)
	}
	for _, m := range p.Movies {
		if m.ID == answer.ID && !slices.Equal(m.Sheets, []int{0, 1, 2, 3, 4, 5}) {
			t.Errorf("the answer, the least voted, is on the sheets of %v", m.Sheets)
		}
		if n := extra(m.ID); n >= 1 && n <= 10 && (!slices.Equal(m.Cast, []int{0, 1}) || !slices.Equal(m.Sheets, []int{1})) {
			t.Errorf("Extra %d credits %v and is on the sheets of %v, want [0 1] and [1]", n, m.Cast, m.Sheets)
		}
		if n := extra(m.ID); n >= 11 && n <= 18 {
			t.Errorf("Extra %d, on nobody's sheet, is kept", n)
		}
	}

	// Played, Foster's sheet, opened once she is showing, is her
	// eighteen, the ten among them; his, opened in a game of its own,
	// stays at MaxSheet without them.
	g := play(t, p)
	g.do(KindNext, "")
	g.do(KindSheet, "nm0287825")
	hers, ok := p.SheetOf(g.state(), 1, Live{})
	if !ok || len(hers) != 18 {
		t.Fatalf("Foster's sheet, opened with her showing: %d movies, %v", len(hers), ok)
	}
	him := play(t, p)
	him.do(KindNext, "")
	him.do(KindSheet, "nm0001592")
	if his, _ := p.SheetOf(him.state(), 0, Live{}); len(his) != MaxSheet {
		t.Errorf("Pantoliano's sheet, opened with Foster showing, has %d movies, want %d", len(his), MaxSheet)
	}
	if also := p.Cast[0].Also; also == nil || also.ID != "tt0106977" {
		t.Errorf("Pantoliano is also in %+v, want The Fugitive", also)
	}
}

// TestBuildRefusesACastMemberWithTooFewOtherMovies: a sheet is free to
// open, so one with fewer than MinSheet movies besides the answer would
// give the answer away by elimination. Pantoliano's, the sixth-billed's
// and open from Play, with nothing but the crowd, or nothing but the
// answer, is refused, and the refusal names neither the movie nor him.
func TestBuildRefusesACastMemberWithTooFewOtherMovies(t *testing.T) {
	without := func(ids ...string) []MapFilm {
		return slices.DeleteFunc(sheets(), func(f MapFilm) bool { return slices.Contains(ids, f.ID) })
	}
	for _, c := range []struct {
		name  string
		films []MapFilm
		why   string
	}{
		{"with only the crowd", without("tt0106977", "tt0209144", "tt9000006", "tt9000007"), "3 other movies, fewer than 4"},
		{"with only the answer", []MapFilm{sheets()[0]}, "0 other movies, fewer than 4"},
	} {
		_, err := Build(1, oct9, answer, billed, wachowskis, c.films)
		var unfit Unfit
		if !errors.As(err, &unfit) || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v, want a refusal saying %q", c.name, err, c.why)
			continue
		}
		for _, name := range []string{"Matrix", answer.ID, "Pantoliano", "nm0001592"} {
			if strings.Contains(err.Error(), name) {
				t.Errorf("%s: the refusal %q names %s", c.name, err, name)
			}
		}
	}
	if _, err := Build(1, oct9, answer, billed, wachowskis, sheets()); err != nil {
		t.Errorf("with MinSheet movies each besides the answer: %v", err)
	}
}

// TestEachOfTheSixNeedsACrowdInTheAnswersDecadeAndBand: a player who has
// bought the decade and the rating reads only the cards inside both, so
// every one of the six's sheets must keep MinCrowd there besides the
// answer. Pantoliano, left out of one of the crowd, has two, and the
// candidate is refused, saying nothing of who or what; given one more
// of his own in the 1990s at 8.0 or more, he has three, and it is a
// puzzle. A movie of his that only someone else's cap let in counts for
// nothing on his sheet, and nor do the answer, the 1990s below 8.0 and
// 8.0 or more outside the 1990s.
func TestEachOfTheSixNeedsACrowdInTheAnswersDecadeAndBand(t *testing.T) {
	two := sheets()
	crowded := slices.IndexFunc(two, func(f MapFilm) bool { return f.ID == "tt9100002" })
	two[crowded].People = slices.DeleteFunc(two[crowded].People, func(id string) bool { return id == "nm0001592" })
	_, err := Build(1, oct9, answer, billed, wachowskis, two)
	var unfit Unfit
	why := "one of the six has 2 other movies in the answer's decade and rating band besides their Also in, fewer than 3"
	if !errors.As(err, &unfit) || err.Error() != "daily: "+why {
		t.Fatalf("Pantoliano with two: %v, want a refusal saying %q", err, why)
	}
	for _, name := range []string{"Matrix", answer.ID, "Pantoliano", "nm0001592", "The Six"} {
		if strings.Contains(err.Error(), name) {
			t.Errorf("the refusal %q names %s", err, name)
		}
	}
	if err := Fit(answer, billed, wachowskis, two); err == nil || err.Error() != "daily: "+why {
		t.Errorf("Fit, with two: %v, want the same refusal", err)
	}
	three := append(slices.Clone(two), MapFilm{ID: "tt9100004", Title: "Pantoliano in 1996", Year: 1996, Rating: 8.1,
		Votes: 400, People: []string{"nm0001592"}})
	p, err := Build(1, oct9, answer, billed, wachowskis, three)
	if err != nil {
		t.Fatalf("Pantoliano with three: %v", err)
	}
	if fourth := p.Movies[slices.IndexFunc(p.Movies, func(m Movie) bool { return m.ID == "tt9100004" })]; !slices.Equal(fourth.Sheets, []int{0}) {
		t.Errorf("Pantoliano's own third is on the sheets of %v", fourth.Sheets)
	}
	// None of these is a third: the 1990s at 7.9, 8.0 or more in 2000,
	// and, past his cap, one he made with Foster.
	for _, f := range []MapFilm{
		{ID: "tt9100005", Title: "Pantoliano at 7.9", Year: 1996, Rating: 7.9, Votes: 400, People: []string{"nm0001592"}},
		{ID: "tt9100006", Title: "Pantoliano in 2000", Year: 2000, Rating: 8.8, Votes: 400, People: []string{"nm0001592"}},
	} {
		if _, err := Build(1, oct9, answer, billed, wachowskis, append(slices.Clone(two), f)); err == nil || err.Error() != "daily: "+why {
			t.Errorf("with %s: %v, want the refusal", f.Title, err)
		}
	}
	capped := slices.Clone(two)
	capped = append(capped, MapFilm{ID: "tt9100007", Title: "Pantoliano and Foster", Year: 1996, Rating: 8.1, Votes: 1,
		People: []string{"nm0001592", "nm0287825"}})
	for n := 1; n <= MaxSheet; n++ {
		capped = append(capped, MapFilm{ID: fmt.Sprintf("tt97%05d", n), Title: fmt.Sprintf("Extra %d", n), Year: 1950,
			Rating: 6, Votes: 1000 + n, People: []string{"nm0001592"}})
	}
	if _, err := Build(1, oct9, answer, billed, wachowskis, capped); err == nil || err.Error() != "daily: "+why {
		t.Errorf("with a third past his cap, on Foster's sheet alone: %v, want the refusal", err)
	}
}

// TestACastMembersOwnAlsoInIsNoPartOfTheirCrowd: it is named beside them
// from the moment they show, so on their sheet it is a card the player
// already knows is not today's. Pantoliano, left out of one of the
// crowd, is given a third of his own in the 1990s at 8.0 or more, the
// most voted of his own and so his "Also in": with only two others to
// weigh, the candidate is refused, saying nothing of who or what. One
// more of his there, less voted, makes three besides it, and it is a
// puzzle; and an "Also in" outside the decade or the band takes nothing
// from the three.
func TestACastMembersOwnAlsoInIsNoPartOfTheirCrowd(t *testing.T) {
	two := sheets()
	crowded := slices.IndexFunc(two, func(f MapFilm) bool { return f.ID == "tt9100002" })
	two[crowded].People = slices.DeleteFunc(two[crowded].People, func(id string) bool { return id == "nm0001592" })
	named := MapFilm{ID: "tt9100004", Title: "Pantoliano in 1996", Year: 1996, Rating: 8.1, Votes: 500000, People: []string{"nm0001592"}}
	alsoIn := func(p *Puzzle) string {
		if p.Cast[0].Also == nil {
			return ""
		}
		return p.Cast[0].Also.ID
	}

	_, err := Build(1, oct9, answer, billed, wachowskis, append(slices.Clone(two), named))
	why := "one of the six has 2 other movies in the answer's decade and rating band besides their Also in, fewer than 3"
	var unfit Unfit
	if !errors.As(err, &unfit) || err.Error() != "daily: "+why {
		t.Fatalf("his third his own Also in: %v, want a refusal saying %q", err, why)
	}
	for _, name := range []string{"Matrix", answer.ID, "Pantoliano", "nm0001592", "tt9100004", "The Six"} {
		if strings.Contains(err.Error(), name) {
			t.Errorf("the refusal %q names %s", err, name)
		}
	}
	if err := Fit(answer, billed, wachowskis, append(slices.Clone(two), named)); err == nil || err.Error() != "daily: "+why {
		t.Errorf("Fit, with his third his own Also in: %v, want the same refusal", err)
	}
	// Less voted than The Fugitive, the same movie is not his "Also in",
	// and counts.
	quiet := named
	quiet.Votes = 400
	if p, err := Build(1, oct9, answer, billed, wachowskis, append(slices.Clone(two), quiet)); err != nil {
		t.Errorf("his third less voted than The Fugitive: %v", err)
	} else if got := alsoIn(p); got != "tt0106977" {
		t.Errorf("his third less voted than The Fugitive, Pantoliano is also in %s", got)
	}

	fourth := MapFilm{ID: "tt9100005", Title: "Pantoliano in 1997", Year: 1997, Rating: 8.2, Votes: 300, People: []string{"nm0001592"}}
	p, err := Build(1, oct9, answer, billed, wachowskis, append(slices.Clone(two), named, fourth))
	if err != nil {
		t.Fatalf("three besides his own Also in: %v", err)
	}
	if got := alsoIn(p); got != named.ID {
		t.Errorf("Pantoliano is also in %s, want %s", got, named.ID)
	}

	for _, outside := range []MapFilm{
		{ID: "tt9100006", Title: "Pantoliano at 7.9", Year: 1996, Rating: 7.9, Votes: 600000, People: []string{"nm0001592"}},
		{ID: "tt9100007", Title: "Pantoliano in 2000", Year: 2000, Rating: 8.8, Votes: 600000, People: []string{"nm0001592"}},
	} {
		p, err := Build(1, oct9, answer, billed, wachowskis, append(slices.Clone(two), quiet, outside))
		if err != nil {
			t.Errorf("with %s his Also in: %v", outside.Title, err)
			continue
		}
		if got := alsoIn(p); got != outside.ID {
			t.Errorf("with %s, Pantoliano is also in %s", outside.Title, got)
		}
	}
}

// TestTheCrowdIsTheDecadeAndTheBandTheFactsSell, at their edges: for an
// answer of 1999 rated 7.4, a movie of 1990 or 1999 is in its decade and
// one of 1989 or 2000 is not, and one rated 7.0 or 7.9 is in its band and
// one rated 6.9 or 8.0 is not. The crowd here is two inside both and a
// third moved to each edge: inside, the candidate is a puzzle, and just
// outside, it is refused.
func TestTheCrowdIsTheDecadeAndTheBandTheFactsSell(t *testing.T) {
	a := answer
	a.Rating = 7.4
	for _, c := range []struct {
		year   int
		rating float64
		fit    bool
	}{
		{1995, 7.5, true},
		{1990, 7.5, true}, {1999, 7.5, true}, {1989, 7.5, false}, {2000, 7.5, false},
		{1995, 7.0, true}, {1995, 0.1 + 0.2 + 6.7, true}, {1995, 7.9, true}, {1995, 6.9, false}, {1995, 8.0, false},
	} {
		films := sheets()
		for i := range films {
			if strings.HasPrefix(films[i].ID, "tt91") {
				films[i].Year, films[i].Rating = 1995, 7.5
			}
			if films[i].ID == "tt9100003" {
				films[i].Year, films[i].Rating = c.year, c.rating
			}
		}
		_, err := Build(1, oct9, a, billed, wachowskis, films)
		if fit := err == nil; fit != c.fit {
			t.Errorf("the third in %d at %v: %v, want fit %v", c.year, c.rating, err, c.fit)
		}
		if err != nil && !strings.Contains(err.Error(), "2 other movies in the answer's decade and rating band") {
			t.Errorf("the third in %d at %v: %v", c.year, c.rating, err)
		}
	}
}

// TestBuildRefusesWhatIsNoGame: no director, fewer than six billed cast,
// no runtime, or no poster colour, each said without naming the movie.
func TestBuildRefusesWhatIsNoGame(t *testing.T) {
	noRuntime, noColour, badColour := answer, answer, answer
	noRuntime.Length = 0
	noColour.Colour = ""
	badColour.Colour = "#26382D"
	for _, c := range []struct {
		name      string
		a         Candidate
		cast, dir []Named
		why       string
	}{
		{"no director", answer, billed, nil, "no director"},
		{"five billed cast", answer, billed[:5], wachowskis, "5 billed cast, fewer than 6"},
		{"no runtime", noRuntime, billed, wachowskis, "no runtime"},
		{"no colour", noColour, billed, wachowskis, "no colour"},
		{"a colour the job never writes", badColour, billed, wachowskis, "no colour"},
	} {
		_, err := Build(1, oct9, c.a, c.cast, c.dir, sheets())
		var unfit Unfit
		if !errors.As(err, &unfit) || !strings.Contains(err.Error(), c.why) || strings.Contains(err.Error(), "Matrix") {
			t.Errorf("%s: %v, want a refusal saying %q", c.name, err, c.why)
		}
		// Fit asks every rule but the colour, as Build does.
		fit := Fit(c.a, c.cast, c.dir, sheets())
		if c.why == "no colour" && fit != nil || c.why != "no colour" && (fit == nil || fit.Error() != err.Error()) {
			t.Errorf("%s: Fit says %v, where Build says %v", c.name, fit, err)
		}
	}
	if err := Fit(answer, billed, wachowskis, sheets()); err != nil {
		t.Errorf("Fit, with everything: %v", err)
	}
}

func TestAColourIsSevenCharactersOfLowerCaseHex(t *testing.T) {
	for c, want := range map[string]bool{
		"#26382d": true, "#000000": true, "#ffffff": true,
		"": false, "#26382": false, "26382d0": false, "#26382D": false, "#26382g": false, "#26382d ": false,
	} {
		if got := ColourOK(c); got != want {
			t.Errorf("ColourOK(%q) = %v, want %v", c, got, want)
		}
	}
}
