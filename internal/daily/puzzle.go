package daily

import (
	"slices"
	"time"
)

// Puzzle is one day's game, as it was picked. Everything a game needs is
// copied in at pick time and never read from the catalog again: ratings
// and votes move with every nightly import, a title can be withdrawn,
// and someone's filmography grows, so a live read would change the
// Movies sheets, and who shares what, halfway through a day.
type Puzzle struct {
	No  int
	Day time.Time
	// Answer is the hidden movie.
	Answer Answer
	// Directors are the answer's directors in IMDb's crew order: the
	// Director fact, and the Movies sheet's director chip.
	Directors []Named
	// Cast is the six billed cast the game shows, in reveal order:
	// sixth-billed first, the star last. A cast member's slot is their
	// index here, which is who they are everywhere in a game.
	Cast []Billed
	// Movies are the Movies sheets: each of the six's own MaxSheet most
	// voted, the answer among them and unmarked, every movie saying whose
	// sheets it is on.
	Movies []Movie
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
	// Length is the runtime in minutes, IMDb's: the Length fact.
	Length int
	// Colour is what the poster averages to, "#rrggbb". It is the one
	// thing about the answer the page has from the start: it fills the
	// hidden card, as it fills the opening screen's frames.
	Colour string
	// Genres are IMDb's, in IMDb's order: the Genre fact.
	Genres []string
}

// Named is someone the puzzle keeps by name: a director.
type Named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Billed is one of the six cast. Billing is their place among the
// answer's billed cast, the star being 1, which is also where their
// colour comes from. Also is another movie they are in that none of the
// other five is, nil when they have none.
type Billed struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Billing int    `json:"billing"`
	Also    *Also  `json:"also,omitempty"`
}

// Also is a cast member's "Also in" movie.
type Also struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Year  int    `json:"year"`
}

// Movie is one movie on the Movies sheets. Cast are the slots of the six
// it credits, in slot order, and Dir is whether any of the answer's
// directors is credited on it. The answer is one of them, and nothing
// marks it out: it credits all six and a director, as a close relative
// may.
//
// Sheets are the slots whose own sheet it is on, those it is among the
// MaxSheet most voted of, in slot order. Cast can name more: a movie on
// one's sheet may credit another of the six whose cap it fell below,
// and once they show it lights for them too, but it is not on their
// sheet. A puzzle kept before sheets were has none (nil), and its
// sheets are every movie crediting the slot, as they were then.
type Movie struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Year   int      `json:"year"`
	Rating float64  `json:"rating"`
	MD     int      `json:"md"`
	Genres []string `json:"genres"`
	Cast   []int    `json:"cast"`
	Sheets []int    `json:"sheets"`
	Dir    bool     `json:"dir"`
}

// SlotOf is the slot of the cast member with that id, or -1 for anyone
// not among the six.
func (p *Puzzle) SlotOf(nconst string) int {
	return slices.IndexFunc(p.Cast, func(b Billed) bool { return b.ID == nconst })
}

// CastSlots are the slots of the people among nconsts, in slot order:
// what a guessed movie shares with the answer, from the people credited
// on it. Directors are not slots, so they never count.
func (p *Puzzle) CastSlots(nconsts []string) []int {
	out := []int{}
	for i, b := range p.Cast {
		if slices.Contains(nconsts, b.ID) {
			out = append(out, i)
		}
	}
	return out
}

// CastIDs are the six's ids, in slot order.
func (p *Puzzle) CastIDs() []string {
	ids := make([]string, len(p.Cast))
	for i, b := range p.Cast {
		ids[i] = b.ID
	}
	return ids
}

// DirectorIDs are the directors' ids, in crew order.
func (p *Puzzle) DirectorIDs() []string {
	ids := make([]string, len(p.Directors))
	for i, d := range p.Directors {
		ids[i] = d.ID
	}
	return ids
}

// CastHue is the hue of the cast member in slot: their billing, the star
// first.
func (p *Puzzle) CastHue(slot int) int {
	return HueAt(p.Cast[slot].Billing - 1)
}

// DirectorHue is the hue of the director at index i in crew order: after
// every place in the cast.
func (p *Puzzle) DirectorHue(i int) int {
	return HueAt(len(p.Cast) + i)
}
