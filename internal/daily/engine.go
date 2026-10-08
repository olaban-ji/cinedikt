package daily

import (
	"math"
	"slices"
	"time"
)

// The kinds of move a game records. The four clues a player can buy
// share their names with the API's "kind" field.
const (
	KindFlip     = "flip"
	KindDirector = "director"
	KindActor    = "actor"
	KindGenres   = "genres"
	KindStory    = "story"
	KindGuess    = "guess"
	KindReveal   = "reveal"
)

// Move is one recorded move: what was done, to what, and what it cost.
type Move struct {
	// Seq counts the game's moves from one; Key is the page's own name
	// for the request that made it, so a retry is recognised.
	Seq  int
	Key  string
	Kind string
	// Arg is the card turned over, or the movie guessed.
	Arg  string
	Cost int
	// Guess is what a wrong guess learned, worked out when it was made,
	// so a replay never needs the live catalog. Nil for anything else,
	// a right guess included.
	Guess *Guessed
	At    time.Time
}

// Guessed is a wrongly guessed movie and what guessing it told the
// player.
type Guessed struct {
	Title  string   `json:"title"`
	Year   int      `json:"year"`
	Rating *float64 `json:"rating"`
	MD     int      `json:"md"`
	// Shared are the answer's people credited on it, as slots.
	Shared []int `json:"shared"`
	// YearHint is where the answer sits from it in time: "older",
	// "newer" or "same", or empty when the guess has no year.
	// RatingHint is the same for the rating: "higher", "lower" or
	// "same", or empty for an unrated guess.
	YearHint   string `json:"year_hint,omitempty"`
	RatingHint string `json:"rating_hint,omitempty"`
}

// Request is a move the page asks for. Seq is how many moves it has seen
// recorded: a move can only follow the game as it stands.
type Request struct {
	Key  string
	Seq  int
	Kind string
	Arg  string
}

// Looked is what the catalog says about a guessed movie, read when the
// guess is made: its facts, and which of the answer's people it credits
// as actor, actress or director, by id.
type Looked struct {
	Title    string
	Year     int
	Rating   *float64
	MD       int
	Credited []string
}

// State is a game worked out from its moves.
type State struct {
	Pts int
	// Bought and Found are the people learned by buying and by wrong
	// guesses, as slots, in the order learned. Nobody is in both.
	Bought []int
	Found  []int
	Genres bool
	Story  bool
	// Wrong is how many wrong guesses there have been.
	Wrong int
	// Done is a game that has ended, Won by naming the answer, GaveUp by
	// asking for it, and Out by running out of points.
	Done, Won, GaveUp, Out bool

	up      map[string]bool
	known   map[int]bool
	guessed map[string]bool
	// steps is each move with the people it revealed, for the log.
	steps []step
}

type step struct {
	move   Move
	people []int
}

// Replay works a game out from its moves, in the order recorded. The
// moves were checked when they were made, so they are taken as they
// are.
func Replay(p *Puzzle, moves []Move) *State {
	s := &State{
		Pts:     Start,
		up:      make(map[string]bool, len(p.Start)+len(moves)),
		known:   map[int]bool{},
		guessed: map[string]bool{},
	}
	for _, id := range p.Start {
		s.up[id] = true
	}
	for _, m := range moves {
		s.Step(p, m)
	}
	return s
}

// Step takes one move.
func (s *State) Step(p *Puzzle, m Move) {
	var people []int
	switch m.Kind {
	case KindFlip:
		s.Pts -= m.Cost
		s.up[m.Arg] = true
	case KindDirector, KindActor:
		s.Pts -= m.Cost
		people = s.next(p, m.Kind)
		for _, i := range people {
			s.known[i] = true
		}
		s.Bought = append(s.Bought, people...)
	case KindGenres:
		s.Pts -= m.Cost
		s.Genres = true
	case KindStory:
		s.Pts -= m.Cost
		s.Story = true
	case KindGuess:
		s.guessed[m.Arg] = true
		if m.Arg == p.Answer.ID {
			s.Done, s.Won = true, true
			break
		}
		s.Wrong++
		// A wrong guess is never refused for points: one that cannot be
		// paid for in full takes the game to nothing.
		s.Pts = max(0, s.Pts-m.Cost)
		if m.Guess != nil {
			for _, i := range m.Guess.Shared {
				if !s.known[i] {
					s.known[i] = true
					s.Found = append(s.Found, i)
				}
			}
		}
		// A movie on the board that is guessed by name turns face up,
		// and shows what it is even when it is a close relative: the
		// player named it.
		if c := p.cardOf(m.Arg); c != nil {
			s.up[c.ID] = true
		}
		if s.Pts == 0 {
			s.Done, s.Out = true, true
		}
	case KindReveal:
		s.Done, s.GaveUp = true, true
	}
	if s.Done && !s.Won {
		s.Pts = 0
	}
	s.steps = append(s.steps, step{move: m, people: people})
}

// next is who a buy of kind would reveal: every director nobody knows
// yet, or the first cast member in billing order nobody knows yet, so a
// person found by a guess is never sold again.
func (s *State) next(p *Puzzle, kind string) []int {
	var out []int
	for i, sl := range p.People {
		if s.known[i] {
			continue
		}
		switch {
		case kind == KindDirector && sl.Role == RoleDirector:
			out = append(out, i)
		case kind == KindActor && sl.Role != RoleDirector:
			return []int{i}
		}
	}
	return out
}

// Up is whether a card is face up: a starting card, one turned over, or
// one guessed by name.
func (s *State) Up(card string) bool { return s.up[card] }

// Known is everyone the player knows, bought or found, in slot order.
func (s *State) Known() []int {
	out := make([]int, 0, len(s.known))
	for i := range s.known {
		out = append(out, i)
	}
	slices.Sort(out)
	return out
}

// Guessed is whether a movie has been guessed.
func (s *State) Guessed(tconst string) bool { return s.guessed[tconst] }

// NextCost is what the next wrong guess costs.
func (s *State) NextCost() int { return NextWrong(s.Wrong) }

// Apply checks a move against the game as it stands and returns it
// priced, ready to record and Step, or the refusal it earns. looked is
// the catalog's answer for a guessed movie, nil when the catalog does
// not have it; it is not read for a guess of the answer itself.
func Apply(p *Puzzle, s *State, r Request, looked *Looked) (Move, error) {
	if s.Done {
		return Move{}, ErrDone
	}
	m := Move{Key: r.Key, Kind: r.Kind, Arg: r.Arg}
	switch r.Kind {
	case KindFlip:
		c := p.card(r.Arg)
		if c == nil {
			return Move{}, ErrBad
		}
		if s.up[c.ID] {
			return Move{}, ErrKnown
		}
		m.Cost = c.Cost()
	case KindDirector, KindActor:
		m.Arg = ""
		if len(s.next(p, r.Kind)) == 0 {
			return Move{}, ErrKnown
		}
		m.Cost = DirectorCost
		if r.Kind == KindActor {
			m.Cost = ActorCost
		}
	case KindGenres:
		m.Arg = ""
		if s.Genres {
			return Move{}, ErrKnown
		}
		m.Cost = GenresCost
	case KindStory:
		m.Arg = ""
		if s.Story {
			return Move{}, ErrKnown
		}
		m.Cost = StoryCost
	case KindGuess:
		if s.guessed[r.Arg] {
			return Move{}, ErrKnown
		}
		if r.Arg == p.Answer.ID {
			return m, nil
		}
		if looked == nil {
			return Move{}, ErrUnknown
		}
		m.Cost = s.NextCost()
		m.Guess = &Guessed{
			Title:      looked.Title,
			Year:       looked.Year,
			Rating:     looked.Rating,
			MD:         looked.MD,
			Shared:     p.Slots(looked.Credited),
			YearHint:   YearHint(looked.Year, p.Answer.Year),
			RatingHint: RatingHint(looked.Rating, p.Answer.Rating),
		}
		return m, nil
	case KindReveal:
		m.Arg = ""
		return m, nil
	default:
		return Move{}, ErrBad
	}
	if s.Pts < m.Cost {
		return Move{}, ErrPoints
	}
	return m, nil
}

// YearHint is where the answer's year sits from a guess's: "newer" when
// the answer came out later, "older" when earlier, "same", or empty when
// the guess has no year to compare.
func YearHint(guess, answer int) string {
	switch {
	case guess == 0:
		return ""
	case guess < answer:
		return "newer"
	case guess > answer:
		return "older"
	}
	return "same"
}

// RatingHint is the same for the rating: "higher" when the answer is
// rated higher than the guess, "lower", "same", or empty for an unrated
// guess. Ratings are compared in tenths, which is all IMDb gives.
func RatingHint(guess *float64, answer float64) string {
	if guess == nil {
		return ""
	}
	g, a := math.Round(*guess*10), math.Round(answer*10)
	switch {
	case g < a:
		return "higher"
	case g > a:
		return "lower"
	}
	return "same"
}
