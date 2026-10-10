package daily

import (
	"slices"
	"time"
)

// The kinds of move a game records. The six facts share their names
// with the API's "kind" field.
const (
	KindNext     = "next"
	KindLength   = "length"
	KindRating   = "rating"
	KindGenre    = "genre"
	KindDecade   = "decade"
	KindYears    = "years"
	KindDirector = "director"
	KindGuess    = "guess"
	KindReveal   = "reveal"
)

// factCost is what each fact "buy" sells costs.
var factCost = map[string]int{
	KindLength:   LengthCost,
	KindRating:   RatingCost,
	KindGenre:    GenreCost,
	KindDecade:   DecadeCost,
	KindYears:    YearsCost,
	KindDirector: DirectorCost,
}

// IsFact is whether kind is one of the facts "buy" sells. The old game's
// clues (actor, genres, year, story) are not, and are refused as bad
// like any kind that never was.
func IsFact(kind string) bool {
	_, ok := factCost[kind]
	return ok
}

// Slots is how many of the cast a game shows: six, sixth-billed up to
// the star.
const Slots = 6

// How a slot came to be showing: at Play, by Next name, by a wrong guess
// (either filled in because the guess credits them, or shown after it as
// the next name), or only by the game ending.
const (
	ViaStart = "start"
	ViaNext  = "next"
	ViaGuess = "guess"
	ViaEnd   = "end"
)

// Move is one recorded move: what was done, to what, and what it cost.
type Move struct {
	// Seq counts the game's moves from one; Key is the page's own name
	// for the request that made it, so a retry is recognised.
	Seq  int
	Key  string
	Kind string
	// Arg is the movie guessed.
	Arg  string
	Cost int
	// Guess is what a wrong guess learned, worked out when it was made,
	// so a replay never needs the live catalog. Nil for anything else,
	// a right guess included.
	Guess *Guessed
	At    time.Time
}

// Guessed is a wrongly guessed movie and what guessing it told the
// player: its title and year, which of the six it credits, and whether
// it shares the answer's decade and a genre. Never which genre, nor
// whether the answer is newer, older or better rated: those would give
// the bought ranges away for free.
type Guessed struct {
	Title string `json:"title"`
	// Year is 0 for a movie the catalog has no year for.
	Year int `json:"year"`
	// Shared are the slots of the six it credits, in slot order, showing
	// or not. Directors are not slots and never count.
	Shared      []int `json:"shared"`
	SameDecade  bool  `json:"sameDecade"`
	SharesGenre bool  `json:"sharesGenre"`
}

// Warmth is how close a wrong guess was: 0 cold, 1 warm, 2 hot. The cast
// it shares counts most, two each, then a point for the same decade and
// one for a shared genre: none is cold, one or two warm, three or more
// hot.
func (g Guessed) Warmth() int {
	score := 2 * len(g.Shared)
	if g.SameDecade {
		score++
	}
	if g.SharesGenre {
		score++
	}
	switch {
	case score >= 3:
		return 2
	case score >= 1:
		return 1
	}
	return 0
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
// guess is made: its title, year and genres, and which of the six it
// credits as actor, actress or director, by id.
type Looked struct {
	Title    string
	Year     int
	Genres   []string
	Credited []string
}

// State is a game worked out from its moves.
type State struct {
	Pts int
	// Facts are the facts bought, by kind.
	Facts map[string]bool
	// Wrong is how many wrong guesses there have been.
	Wrong int
	// Done is a game that has ended, Won by naming the answer, GaveUp by
	// asking for it, and Out by a wrong guess the points could not cover.
	Done, Won, GaveUp, Out bool

	// via is how each slot came to be showing, "" while it is hidden,
	// and from the wrong guess that filled it in, when one did.
	via  [Slots]string
	from [Slots]Ref
	// guessed are the movies guessed, and steps each move with the slot
	// a Next name showed, for the log.
	guessed map[string]bool
	steps   []step
}

type step struct {
	move Move
	slot int
}

// Replay works a game out from its moves, in the order recorded. The
// moves were checked when they were made, so they are taken as they
// are. The sixth-billed is showing from Play.
func Replay(p *Puzzle, moves []Move) *State {
	s := &State{Pts: Start, Facts: map[string]bool{}, guessed: map[string]bool{}}
	s.via[0] = ViaStart
	for _, m := range moves {
		s.Step(p, m)
	}
	return s
}

// Step takes one move.
func (s *State) Step(p *Puzzle, m Move) {
	slot := -1
	switch {
	case m.Kind == KindNext:
		s.Pts -= m.Cost
		if slot = s.FirstHidden(); slot >= 0 {
			s.via[slot] = ViaNext
		}
	case IsFact(m.Kind):
		s.Pts -= m.Cost
		s.Facts[m.Kind] = true
	case m.Kind == KindGuess:
		s.guessed[m.Arg] = true
		if m.Arg == p.Answer.ID {
			s.Done, s.Won = true, true
			break
		}
		s.Wrong++
		// Everyone hidden the movie credits is filled in through it, and
		// then the next hidden name shows, so the step after a lucky
		// guess is still someone new.
		if g := m.Guess; g != nil {
			for _, i := range g.Shared {
				if i >= 0 && i < Slots && s.via[i] == "" {
					s.via[i], s.from[i] = ViaGuess, Ref{ID: m.Arg, Title: g.Title}
				}
			}
		}
		if next := s.FirstHidden(); next >= 0 {
			s.via[next] = ViaGuess
		}
		// A wrong guess is never refused for points: one the points
		// cannot cover, leaving at least one, ends the game at nothing,
		// though the names it showed were still seen.
		if s.Pts <= m.Cost {
			s.Done, s.Out = true, true
			break
		}
		s.Pts -= m.Cost
	case m.Kind == KindReveal:
		s.Done, s.GaveUp = true, true
	default:
		// A kind this engine does not know is taken as nothing: it costs
		// nothing, shows nothing and has no line in the log. The game
		// still replays, and the move still counts in seq, so the page
		// can go on from it. None should ever be met: Apply refuses
		// them, the moves table's check refuses them, the old game's
		// moves went with its tables when meta.sql dropped them, and the
		// test games that bought an overlap, Name Drop's first way of
		// combining two names, went when meta.sql took it out of the
		// check.
		return
	}
	if s.Done && !s.Won {
		s.Pts = 0
	}
	s.steps = append(s.steps, step{move: m, slot: slot})
}

// FirstHidden is the first slot in reveal order nobody has been shown,
// or -1 once all six are showing.
func (s *State) FirstHidden() int {
	return slices.Index(s.via[:], "")
}

// Shown is whether the player has been shown the cast member in slot:
// always, once the game is over.
func (s *State) Shown(slot int) bool {
	if slot < 0 || slot >= Slots {
		return false
	}
	return s.Done || s.via[slot] != ""
}

// Seen is how many of the six the player saw before the game ended, or
// has seen so far.
func (s *State) Seen() int {
	n := 0
	for _, v := range s.via {
		if v != "" {
			n++
		}
	}
	return n
}

// Guessed is whether a movie has been guessed.
func (s *State) Guessed(tconst string) bool { return s.guessed[tconst] }

// NextCost is what the next wrong guess costs.
func (s *State) NextCost() int { return NextWrong(s.Wrong) }

// Apply checks a move against the game as it stands and returns it
// priced, ready to record and Step, or the refusal it earns. looked is
// the catalog's answer for a guessed movie, nil when the catalog does
// not have it; it is not read for a guess of the answer itself.
//
// Every purchase must leave at least a point: one that would spend the
// last is refused, so the only way to nothing is a wrong guess the
// points cannot cover, or asking for the answer.
func Apply(p *Puzzle, s *State, r Request, looked *Looked) (Move, error) {
	if s.Done {
		return Move{}, ErrDone
	}
	m := Move{Key: r.Key, Kind: r.Kind, Arg: r.Arg}
	switch {
	case r.Kind == KindNext:
		m.Arg = ""
		if s.FirstHidden() < 0 {
			return Move{}, ErrKnown
		}
		m.Cost = NameCost
	case IsFact(r.Kind):
		m.Arg = ""
		if s.Facts[r.Kind] {
			return Move{}, ErrKnown
		}
		if r.Kind == KindYears && !s.Facts[KindDecade] {
			return Move{}, ErrBad
		}
		m.Cost = factCost[r.Kind]
	case r.Kind == KindGuess:
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
			Title:       looked.Title,
			Year:        looked.Year,
			Shared:      p.CastSlots(looked.Credited),
			SameDecade:  looked.Year != 0 && Decade(looked.Year) == Decade(p.Answer.Year),
			SharesGenre: slices.ContainsFunc(looked.Genres, func(g string) bool { return slices.Contains(p.Answer.Genres, g) }),
		}
		return m, nil
	case r.Kind == KindReveal:
		m.Arg = ""
		return m, nil
	default:
		return Move{}, ErrBad
	}
	if s.Pts <= m.Cost {
		return Move{}, ErrPoints
	}
	return m, nil
}
