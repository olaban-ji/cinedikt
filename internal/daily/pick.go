package daily

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Choosing a day's puzzle. The catalog reads the candidates, each one's
// people and their movies; what makes a movie a fair answer, and what its
// puzzle holds, is decided here.

// Candidate is a movie that could be a day's answer, with what the pick
// needs to know before it reads the movie's people.
type Candidate struct {
	ID     string
	Title  string
	Year   int
	Rating float64
	MD     int
	// Length is IMDb's runtime in minutes, 0 when it has none.
	Length int
	// Colour is what the poster averages to, "#rrggbb", or empty while
	// the colour job has not reached it; Poster is the poster's address,
	// which the pick works the colour out from then.
	Colour string
	Poster string
	// Era is the movie's era in the opening screen's pool, the most
	// voted 250 of each: the lower year of its band.
	Era    int
	Genres []string
	Votes  int
}

// Recent is what the days around a pick have already used.
type Recent struct {
	// Answers are the answers within RepeatDays of the day, either
	// side: one picked ahead counts as much as one already played.
	Answers map[string]bool
	// Eras are the eras of the EraDays before it, and Genres the first
	// genres of the GenreDays before it.
	Eras   map[int]bool
	Genres map[string]bool
}

// RepeatDays is how long a movie waits before it can be the answer
// again: long enough that nobody is handed one they remember solving.
const RepeatDays = 90

// EraDays and GenreDays are how far back a pick looks to mix the week:
// a week of seven days uses seven eras of the pool's eight before one
// comes round again, and a genre is not used two days running.
const (
	EraDays   = 6
	GenreDays = 2
)

// Order is the order a day tries its candidates in: shuffled by a
// generator seeded from the day, so a re-run tries them in the same
// order, then those of an era the last EraDays have not used and a
// first genre the last GenreDays have not used, then any genre, then
// any era. Answers too close to the day are left out altogether.
//
// The candidates are sorted by id before the shuffle, so the order the
// database happened to return them in changes nothing.
func Order(day time.Time, cands []Candidate, recent Recent) []Candidate {
	return OrderBy(Seeded(DayString(day)), cands, recent)
}

// OrderBy is Order with the shuffle drawn from r rather than from the
// day, every other rule the same. Only development's Play again uses
// it, to deal a day afresh: seeded from the day, the movie it replaced,
// no longer that day's answer, would head the list again, and pressing
// it twice would only swap the same two movies back and forth.
func OrderBy(r *Rand, cands []Candidate, recent Recent) []Candidate {
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if !recent.Answers[c.ID] {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Candidate) int { return cmp.Compare(a.ID, b.ID) })
	Shuffle(r, out)
	tier := func(c Candidate) int {
		t := 0
		if recent.Eras[c.Era] {
			t += 2
		}
		if len(c.Genres) > 0 && recent.Genres[c.Genres[0]] {
			t++
		}
		return t
	}
	slices.SortStableFunc(out, func(a, b Candidate) int { return cmp.Compare(tier(a), tier(b)) })
	return out
}

// MapFilm is one movie of the six's, as the catalog reads it for the
// Movies sheets: what a card says, how well known it is, and which of
// the answer's people it credits, by id. Every one is rated and passes
// the map's film test.
type MapFilm struct {
	ID     string
	Title  string
	Year   int
	Rating float64
	MD     int
	Genres []string
	Votes  int
	// People are the answer's people it credits, cast and directors and
	// not only the six, as actor, actress or director: enough to tell a
	// close relative.
	People []string
}

// What a candidate must have to be a day's answer.
const (
	// MinDirectors is enough for the Director fact to sell somebody.
	MinDirectors = 1
	// MinCast is the six the game shows: a movie with fewer billed cast
	// has no star to work up to.
	MinCast = Slots
	// MaxSheet caps each of the six's movies on the Movies sheets at
	// their most voted, as MaxSpine caps a map: a career is wider than a
	// screen, and the most voted survive. The answer always makes it.
	MaxSheet = 400
	// MinSheet is how many movies besides the answer each of the six's
	// sheet must hold. A sheet is free to open, and the sixth-billed's
	// is open from Play: one holding only the answer, or the answer and
	// a movie or two, would hand it over by elimination, where it is
	// meant to sit among the cards unmarked.
	MinSheet = 4
)

// Unfit is why a candidate cannot be a day's answer. It never names the
// candidate: whoever reads the logs may want to play it.
type Unfit string

func (u Unfit) Error() string { return "daily: " + string(u) }

// Build makes a candidate's puzzle from its people and their movies, or
// says why it cannot be one. cast is the answer's billed cast in billing
// order, the star first, and directors its directors in crew order; a
// person who both acted in it and directed it is a director, as the
// app's chip row has them, so the Director fact never names a hidden
// cast member. films are the six's movies (MapFilm).
//
// The six are the first six of cast, kept in reveal order: sixth-billed
// first, the star last. Each one's sheet is their own MaxSheet most
// voted, the answer first and ties to the lower id, as sheetFilms orders
// them, and must hold MinSheet movies besides the answer. Each one's
// "Also in" movie is the most voted of theirs that credits none of the
// other five, is not the answer, is not a close relative and does not
// share the answer's title (sharesTitle), any of which would give the
// answer away; ties fall to the lower id, so a pick made twice keeps the
// same one. The movies kept are those on someone's
// sheet, sorted by year then id, so where a movie sits in the list says
// nothing about how well known it is.
func Build(no int, day time.Time, a Candidate, cast, directors []Named, films []MapFilm) (*Puzzle, error) {
	if len(directors) < MinDirectors {
		return nil, Unfit("it has no director")
	}
	if len(cast) < MinCast {
		return nil, Unfit(fmt.Sprintf("it has %d billed cast, fewer than %d", len(cast), MinCast))
	}
	if a.Length <= 0 {
		return nil, Unfit("it has no runtime")
	}
	if !ColourOK(a.Colour) {
		return nil, Unfit("its poster has no colour")
	}
	six := make([]Billed, MinCast)
	for i, c := range cast[:MinCast] {
		six[MinCast-1-i] = Billed{ID: c.ID, Name: c.Name, Billing: i + 1}
	}
	slot := make(map[string]int, len(six))
	for i, b := range six {
		slot[b.ID] = i
	}
	directed := make(map[string]bool, len(directors))
	for _, d := range directors {
		directed[d.ID] = true
	}
	// from is the film each movie was made from, by index, nil for an
	// answer made from the candidate.
	movies := make([]Movie, 0, len(films)+1)
	from := make([]*MapFilm, 0, len(films)+1)
	answer := false
	for i := range films {
		f := &films[i]
		m := Movie{ID: f.ID, Title: f.Title, Year: f.Year, Rating: f.Rating, MD: f.MD,
			Genres: nonNilStrings(slices.Clone(f.Genres)), Cast: []int{}, Sheets: []int{}}
		for _, id := range f.People {
			if s, ok := slot[id]; ok {
				m.Cast = append(m.Cast, s)
			}
			m.Dir = m.Dir || directed[id]
		}
		slices.Sort(m.Cast)
		m.Cast = slices.Compact(m.Cast)
		if len(m.Cast) == 0 {
			continue
		}
		answer = answer || f.ID == a.ID
		movies = append(movies, m)
		from = append(from, f)
	}
	// The answer is on every one of the six's sheets. The catalog's read
	// always has it; were it ever missing, its card is made from the
	// candidate, crediting all six, so no sheet lacks it.
	if !answer {
		movies = append(movies, Movie{ID: a.ID, Title: a.Title, Year: a.Year, Rating: a.Rating, MD: a.MD,
			Genres: nonNilStrings(slices.Clone(a.Genres)), Cast: []int{0, 1, 2, 3, 4, 5}, Sheets: []int{}, Dir: true})
		from = append(from, nil)
	}
	// Each one's sheet is their own MaxSheet most voted. The catalog
	// reads every one's top MaxSheet together, so a movie of someone's
	// that is below their own cap can be there because another of the
	// six is on it and their cap let it in: it goes on that one's sheet
	// only. On the first's, with only them showing on its card, it would
	// say that someone hidden is in it, and a sheet run past MaxSheet
	// would say that such cards were there to find.
	first := func(i int) int {
		if movies[i].ID == a.ID {
			return 0
		}
		return 1
	}
	votes := func(i int) int {
		if from[i] == nil {
			return 0
		}
		return from[i].Votes
	}
	for s := range six {
		var theirs []int
		for i, m := range movies {
			if slices.Contains(m.Cast, s) {
				theirs = append(theirs, i)
			}
		}
		slices.SortFunc(theirs, func(x, y int) int {
			return cmp.Or(cmp.Compare(first(x), first(y)), cmp.Compare(votes(y), votes(x)), cmp.Compare(movies[x].ID, movies[y].ID))
		})
		for _, i := range theirs[:min(len(theirs), MaxSheet)] {
			movies[i].Sheets = append(movies[i].Sheets, s)
		}
	}
	// A movie on nobody's sheet is never shown, so it is not kept.
	kept, keptFrom := make([]Movie, 0, len(movies)), make([]*MapFilm, 0, len(movies))
	for i, m := range movies {
		if len(m.Sheets) > 0 {
			kept, keptFrom = append(kept, m), append(keptFrom, from[i])
		}
	}
	movies, from = kept, keptFrom
	for s := range six {
		n := 0
		for _, m := range movies {
			if m.ID != a.ID && slices.Contains(m.Sheets, s) {
				n++
			}
		}
		if n < MinSheet {
			return nil, Unfit(fmt.Sprintf("one of the six has %d other movies, fewer than %d", n, MinSheet))
		}
	}
	also := make([]*MapFilm, len(six))
	for i, m := range movies {
		f := from[i]
		if f == nil || f.ID == a.ID || len(m.Cast) != 1 || len(f.People) >= RelativeShared || sharesTitle(f.Title, a.Title) {
			continue
		}
		if b := also[m.Cast[0]]; b == nil || f.Votes > b.Votes || (f.Votes == b.Votes && f.ID < b.ID) {
			also[m.Cast[0]] = f
		}
	}
	slices.SortFunc(movies, func(x, y Movie) int { return cmp.Or(cmp.Compare(x.Year, y.Year), cmp.Compare(x.ID, y.ID)) })
	for i, f := range also {
		if f != nil {
			six[i].Also = &Also{ID: f.ID, Title: f.Title, Year: f.Year}
		}
	}
	genre := ""
	if len(a.Genres) > 0 {
		genre = a.Genres[0]
	}
	return &Puzzle{
		No:  no,
		Day: Today(day),
		Answer: Answer{
			ID: a.ID, Title: a.Title, Year: a.Year, Rating: a.Rating, MD: a.MD,
			Length: a.Length, Colour: a.Colour, Genres: nonNilStrings(slices.Clone(a.Genres)),
		},
		Directors: slices.Clone(directors),
		Cast:      six,
		Movies:    movies,
		Era:       a.Era,
		Genre:     genre,
	}, nil
}

// ColourOK is whether a colour is one the poster colour job writes:
// "#rrggbb", in lower case.
func ColourOK(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}

// nonNilStrings is a list as the API and the database want it: empty
// rather than null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// sharesTitle says whether either title holds the other whole, word for
// word: Halloween and Halloween III: Season of the Witch, The Matrix and
// The Matrix Reloaded, Alien and Aliens. A sequel or a remake shown as
// someone's "Also in" names the answer as surely as the answer itself,
// and a franchise often shares only one person with it, too few to be a
// close relative. Words are compared without case, punctuation, a
// leading article or a plural s, so Aliens is still Alien. A short
// answer title rules out more than it needs to (Up, every movie with "up"
// in its name), which only means someone's next most voted is shown.
func sharesTitle(a, b string) bool {
	x, y := titleWords(a), titleWords(b)
	if len(x) == 0 || len(y) == 0 {
		return false
	}
	if len(x) < len(y) {
		x, y = y, x
	}
	// Whether the shorter run of words, y, sits whole inside x.
	for i := 0; i+len(y) <= len(x); i++ {
		if slices.Equal(x[i:i+len(y)], y) {
			return true
		}
	}
	return false
}

// titleWords is a title as the words sharesTitle compares: lower case,
// split at anything that is not a letter or a digit, without a leading
// "the", "a" or "an", and with a plural s taken off words longer than
// three letters.
func titleWords(title string) []string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) > 1 && (words[0] == "the" || words[0] == "a" || words[0] == "an") {
		words = words[1:]
	}
	for i, w := range words {
		if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			words[i] = strings.TrimSuffix(w, "s")
		}
	}
	return words
}
