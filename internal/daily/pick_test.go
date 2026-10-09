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
// Fishburne's own, only the sequel and three he made with Foster; and
// two Pantoliano made, one with Weaving and one with Moss. So each of
// the six has MinSheet movies besides the answer, and no more.
func sheets() []MapFilm {
	return []MapFilm{
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
		"1993 tt0106977 [0] [0] false",
		"1994 tt0111257 [5] [5] false",
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
// of Pantoliano's, the least voted of his, leave his fifteen fewest off
// his sheet, though Foster is on the ten fewest and her sheet, far from
// full, has them. On his they would be cards past his cap with only him
// showing on them, each a sign that someone hidden is in it, and his
// sheet run past MaxSheet would say that such cards were there to find.
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
	joe := p.SheetWants(0)
	if len(joe) != MaxSheet || !slices.Contains(joe, answer.ID) {
		t.Errorf("Pantoliano's sheet has %d movies, the answer among them %v; want %d", len(joe), slices.Contains(joe, answer.ID), MaxSheet)
	}
	for _, id := range joe {
		if n := extra(id); n >= 1 && n <= 15 {
			t.Errorf("Extra %d, among the fifteen least voted of his, is on Pantoliano's sheet", n)
		}
	}
	// Hers: the answer, Man and Boy, the three with Fishburne and the ten.
	gloria := p.SheetWants(1)
	for n := 1; n <= 10; n++ {
		if !slices.Contains(gloria, fmt.Sprintf("tt97%05d", n)) {
			t.Errorf("Extra %d, which Foster is on, is not on her sheet", n)
		}
	}
	if len(gloria) != 15 {
		t.Errorf("Foster's sheet has %d movies, want 15: %v", len(gloria), gloria)
	}
	for _, m := range p.Movies {
		if m.ID == answer.ID && !slices.Equal(m.Sheets, []int{0, 1, 2, 3, 4, 5}) {
			t.Errorf("the answer, the least voted, is on the sheets of %v", m.Sheets)
		}
		if n := extra(m.ID); n >= 1 && n <= 10 && (!slices.Equal(m.Cast, []int{0, 1}) || !slices.Equal(m.Sheets, []int{1})) {
			t.Errorf("Extra %d credits %v and is on the sheets of %v, want [0 1] and [1]", n, m.Cast, m.Sheets)
		}
		if n := extra(m.ID); n >= 11 && n <= 15 {
			t.Errorf("Extra %d, on nobody's sheet, is kept", n)
		}
	}

	// Once Foster shows, her sheet lights Pantoliano on the ten, and his
	// stays at MaxSheet without them.
	g := play(t, p)
	g.do(KindNext, "")
	hers, ok := p.SheetOf(g.state(), 1, Live{})
	if !ok || len(hers) != 15 {
		t.Fatalf("Foster's sheet with her showing: %d movies, %v", len(hers), ok)
	}
	for _, m := range hers {
		if n := extra(m.ID); n >= 1 && n <= 10 && !slices.Equal(m.On, []int{0, 1}) {
			t.Errorf("Extra %d on Foster's sheet lights %v, want [0 1]", n, m.On)
		}
	}
	if his, _ := p.SheetOf(g.state(), 0, Live{}); len(his) != MaxSheet {
		t.Errorf("Pantoliano's sheet with Foster showing has %d movies, want %d", len(his), MaxSheet)
	}
	if also := p.Cast[0].Also; also == nil || also.ID != "tt0106977" {
		t.Errorf("Pantoliano is also in %+v, want The Fugitive", also)
	}
}

// TestBuildRefusesACastMemberWithTooFewOtherMovies: a sheet is free to
// open, so one with fewer than MinSheet movies besides the answer would
// give the answer away by elimination. Pantoliano's, the sixth-billed's
// and open from Play, without The Fugitive, or with nothing but the
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
		{"without The Fugitive", without("tt0106977"), "3 other movies, fewer than 4"},
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
