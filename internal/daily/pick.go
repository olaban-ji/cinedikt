package daily

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

// Choosing a day's puzzle. The catalog reads the candidates and each
// one's map; what makes a movie a fair answer, and how its board is
// dealt, is decided here.

// Candidate is a movie that could be a day's answer, with what the pick
// needs to know before it reads the movie's map.
type Candidate struct {
	ID     string
	Title  string
	Year   int
	Rating float64
	MD     int
	// Era is the movie's era in the opening screen's pool, the most
	// voted 250 of each: the lower year of its band.
	Era    int
	Genres []string
	Votes  int
	// Overview is OMDb's plot, whose first sentence is "How it starts".
	Overview string
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
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if !recent.Answers[c.ID] {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Candidate) int { return cmp.Compare(a.ID, b.ID) })
	Shuffle(Seeded(DayString(day)), out)
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

// MapFilm is one movie on a candidate's map, as the app's own map has
// it, with its title and votes.
type MapFilm struct {
	ID     string
	Title  string
	Year   int
	Rating *float64
	MD     int
	Votes  int
	// People are the candidate's people on it, as slots.
	People []int
}

// The least a map must offer to be a day's board.
const (
	// MinCards is enough cards that the board is a search rather than a
	// guess between a handful.
	MinCards = 40
	// MinDirectors and MinCast are enough people that the Director and
	// Actor clues each have something to sell, and the three starting
	// cards can come through three different actors.
	MinDirectors = 1
	MinCast      = 3
	// StartCards is how many cards are face up from the beginning.
	StartCards = 3
)

// Unfit is why a candidate cannot be a day's answer.
type Unfit string

func (u Unfit) Error() string { return "daily: " + string(u) }

// Build deals a candidate's puzzle from its people and its map, or says
// why it cannot be one.
//
// The board is every movie on the map that has a rating, the answer
// left out. An unrated movie has no place on the rating axis the game
// narrows on, so it is left off rather than parked in a column of its
// own. The cards' ids are dealt after a shuffle seeded from the day, so
// a card's number says nothing about how well known it is.
func Build(no int, day time.Time, a Candidate, people []Slot, films []MapFilm) (*Puzzle, error) {
	opening := Opening(a.Overview)
	if opening == "" {
		return nil, Unfit("its overview has no opening sentence")
	}
	if Mentions(opening, a.Title) {
		return nil, Unfit("its opening sentence names it")
	}
	var directors, cast int
	for _, s := range people {
		if s.Role == RoleDirector {
			directors++
		} else {
			cast++
		}
	}
	if directors < MinDirectors {
		return nil, Unfit("it has no director")
	}
	if cast < MinCast {
		return nil, Unfit(fmt.Sprintf("it has %d billed cast, fewer than %d", cast, MinCast))
	}
	cards := make([]Card, 0, len(films))
	for _, f := range films {
		if f.ID == a.ID || f.Rating == nil {
			continue
		}
		on := slices.Clone(f.People)
		slices.Sort(on)
		if on == nil {
			on = []int{}
		}
		cards = append(cards, Card{Film: f.ID, Title: f.Title, Year: f.Year, Rating: *f.Rating, MD: f.MD, Votes: f.Votes, People: on})
	}
	if len(cards) < MinCards {
		return nil, Unfit(fmt.Sprintf("its map has %d rated movies, fewer than %d", len(cards), MinCards))
	}
	// Sorted first, so the deal depends on the day and the map alone.
	slices.SortFunc(cards, func(x, y Card) int { return cmp.Compare(x.Film, y.Film) })
	Shuffle(Seeded(DayString(day)+"/cards"), cards)
	for i := range cards {
		cards[i].ID = cardID(i + 1)
	}
	start := Starts(cards)
	if len(start) < StartCards {
		return nil, Unfit("its map has no three starting cards through three different people")
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
			Genres: nonNilStrings(slices.Clone(a.Genres)),
		},
		People:  people,
		Cards:   cards,
		Start:   start,
		Opening: opening,
		Era:     a.Era,
		Genre:   genre,
	}, nil
}

// Starts are the starting cards: the least known cards on the map that
// are not close relatives, each linked to the answer through people
// none of the others is. Walking the cards from the fewest votes up and
// taking each one whose people are all still unused does both: the
// three show three different ways in, and none of them is a movie
// everybody knows.
func Starts(cards []Card) []string {
	order := slices.Clone(cards)
	slices.SortStableFunc(order, func(x, y Card) int {
		return cmp.Or(cmp.Compare(x.Votes, y.Votes), cmp.Compare(x.Film, y.Film))
	})
	used := map[int]bool{}
	var out []string
	for _, c := range order {
		if c.Relative() || len(c.People) == 0 {
			continue
		}
		if slices.ContainsFunc(c.People, func(i int) bool { return used[i] }) {
			continue
		}
		for _, i := range c.People {
			used[i] = true
		}
		out = append(out, c.ID)
		if len(out) == StartCards {
			break
		}
	}
	return out
}
