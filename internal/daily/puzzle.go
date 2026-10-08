package daily

import (
	"strconv"
	"time"
)

// Puzzle is one day's game, as it was picked. Everything a game needs is
// copied in at pick time and never read from the catalog again: ratings
// and votes move with every nightly import, a title can be withdrawn,
// and a card's price is its rating, so a live read would change the
// prices halfway through a day.
type Puzzle struct {
	No  int
	Day time.Time
	// Answer is the hidden movie.
	Answer Answer
	// People are the answer's people in slot order: its directors in
	// IMDb's crew order, then its cast in billing order, which is the
	// app's own chip row. A person's slot is who they are everywhere in a
	// game, and their colour on the page.
	People []Slot
	// Cards are the map: every movie on the answer's map that has a
	// rating, the answer left out, in card id order.
	Cards []Card
	// Start is the three cards face up from the beginning.
	Start []string
	// Era and Genre are the answer's era in the opening screen's pool and
	// its first IMDb genre, kept so the next days' picks can be told
	// apart from this one.
	Era   int
	Genre string
}

// On is whether p is the puzzle of the day at now for a reader in zone:
// whether Play may start a game of it there, and, in the zone a game was
// started in, whether the game may still be played. A game's zone is
// the one kept with it, never the one a later request names, so a tab
// cannot hop zones to keep a game going past its midnight.
func (p *Puzzle) On(now time.Time, zone *time.Location) bool {
	return p.Day.Equal(DayIn(now, zone))
}

// Answer is the hidden movie.
type Answer struct {
	ID     string
	Title  string
	Year   int
	Rating float64
	MD     int
	// Genres are IMDb's, in IMDb's order: the "Genres" clue.
	Genres []string
}

// Slot is one of the answer's people. Role is "director" or "cast": a
// person who both acted in it and directed it is its director, as the
// chip row has them.
type Slot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// The two roles a slot can have.
const (
	RoleDirector = "director"
	RoleCast     = "cast"
)

// Card is one movie on the board. Its id is opaque ("c17"): the page
// places a card by its year and rating alone, and is told what it is
// only once it is turned over.
type Card struct {
	ID     string  `json:"id"`
	Film   string  `json:"film"`
	Title  string  `json:"title"`
	Year   int     `json:"year"`
	Rating float64 `json:"rating"`
	MD     int     `json:"md"`
	// Votes is how well known it was at pick time, which chose the
	// starting cards.
	Votes int `json:"votes"`
	// People are the answer's people it shares, as slots, in slot order.
	People []int `json:"people"`
}

// Relative is whether a card is a close relative of the answer.
func (c Card) Relative() bool { return len(c.People) >= RelativeShared }

// Cost is what turning it over costs.
func (c Card) Cost() int { return FlipCost(c.Rating) }

// cardID is the id the nth card gets, counting from one.
func cardID(n int) string { return "c" + strconv.Itoa(n) }

// card is the card with that id, or nil.
func (p *Puzzle) card(id string) *Card {
	for i := range p.Cards {
		if p.Cards[i].ID == id {
			return &p.Cards[i]
		}
	}
	return nil
}

// cardOf is the card a movie is, or nil when it is not on the board.
func (p *Puzzle) cardOf(tconst string) *Card {
	for i := range p.Cards {
		if p.Cards[i].Film == tconst {
			return &p.Cards[i]
		}
	}
	return nil
}

// Slots are the slots of the people among nconsts, in slot order: what
// a guessed movie shares with the answer, from the people credited on
// it.
func (p *Puzzle) Slots(nconsts []string) []int {
	on := make(map[string]bool, len(nconsts))
	for _, id := range nconsts {
		on[id] = true
	}
	out := []int{}
	for i, s := range p.People {
		if on[s.ID] {
			out = append(out, i)
		}
	}
	return out
}

// PeopleIDs are the answer's people's ids, in slot order.
func (p *Puzzle) PeopleIDs() []string {
	ids := make([]string, len(p.People))
	for i, s := range p.People {
		ids[i] = s.ID
	}
	return ids
}

// Clues is how many of each kind of person the answer has, which is all
// the page is told about them until they are bought or found: enough to
// say "Directors" rather than "Director", and when the actors run out.
type Clues struct {
	Directors int `json:"directors"`
	Cast      int `json:"cast"`
}

// Clues counts the answer's people.
func (p *Puzzle) Clues() Clues {
	var c Clues
	for _, s := range p.People {
		if s.Role == RoleDirector {
			c.Directors++
		} else {
			c.Cast++
		}
	}
	return c
}

// Face is what the page is told about every card at load: where it goes.
type Face struct {
	ID     string  `json:"id"`
	Year   int     `json:"year"`
	Rating float64 `json:"rating"`
	MD     int     `json:"md"`
}

// Faces are every card's face, in card id order.
func (p *Puzzle) Faces() []Face {
	out := make([]Face, len(p.Cards))
	for i, c := range p.Cards {
		out[i] = Face{ID: c.ID, Year: c.Year, Rating: c.Rating, MD: c.MD}
	}
	return out
}

// Opened is a starting card, sent in full from the beginning.
type Opened struct {
	Card string `json:"card"`
	Film Film   `json:"film"`
}

// Opened are the three starting cards with what they are.
func (p *Puzzle) Opened(live Live) []Opened {
	out := make([]Opened, 0, len(p.Start))
	for _, id := range p.Start {
		if c := p.card(id); c != nil {
			out = append(out, Opened{Card: id, Film: c.film(live)})
		}
	}
	return out
}
