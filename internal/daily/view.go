package daily

import (
	"encoding/json"
	"time"
)

// What the page is shown of a game. Everything here is drawn from a
// replay, and a replay decides what may be seen: until a game ends
// nothing below names the answer, nor says its year until the year is
// bought, and a card face down is only ever its id, year and rating
// (Faces). The tests in view_test.go hold every response to that.

// Record is a game as it is kept: when it started, when it ended, the
// time zone it was started in, and its moves in order.
type Record struct {
	Started  time.Time
	Finished *time.Time
	// Zone is the IANA name of the zone Play was pressed in ("UTC" for
	// none): the game is played on its puzzle's day there, and abandoned
	// at its midnight. Read it with Zone.
	Zone  string
	Moves []Move
}

// Live is what is read fresh for every answer rather than kept with the
// puzzle: posters, which the poster jobs mend and replace, and people's
// photos, which TMDb's terms say may only be shown for 175 days after
// they were asked for. Both by id; a missing one is left out.
type Live struct {
	Posters map[string]string
	Photos  map[string]string
}

// Film is a movie the page may name.
type Film struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Year is 0 and Rating null for a guessed movie the catalog has
	// neither for. Every card and the answer have both.
	Year   int      `json:"year"`
	Rating *float64 `json:"rating"`
	MD     int      `json:"md"`
	Poster string   `json:"poster,omitempty"`
}

// Person is one of the answer's people the page may name, with the
// cards they are on, so it can mark them.
type Person struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Slot  int    `json:"slot"`
	Photo string `json:"photo,omitempty"`
	// Cards are the board cards they are on, in card id order.
	Cards []string `json:"cards"`
}

// Relation is what a close relative's face says instead of what it is.
type Relation struct {
	Shared int `json:"shared"`
}

// The entries in a game's log, one per move after "start", and an "out"
// after the wrong guess that spent the last point.
const (
	EntryStart  = "start"
	EntryFlip   = "flip"
	EntryPerson = "person"
	EntryGenres = "genres"
	EntryYear   = "year"
	EntryGuess  = "guess"
	EntryWin    = "win"
	EntryGaveUp = "gaveup"
	EntryOut    = "out"
)

// Entry is one line of the log. Which fields it carries depends on its
// Type, and MarshalJSON writes only those, with null where the API says
// a value may be absent.
type Entry struct {
	Type string
	// Card is the card turned over, or the card a guessed movie is.
	Card string
	Cost int
	// Film is what a card turned over is, or the movie guessed. A close
	// relative turned over has Relative instead.
	Film     *Film
	Relative *Relation
	// Role and People are a bought clue: "director" or "actor", and who
	// it revealed.
	Role   string
	People []Person
	Genres []string
	// Year is the year clue: the answer's year, which nothing else the
	// page is sent says before the game ends. A card from the same year
	// says its own year, as every card does, but nothing marks it out.
	Year int
	// Shared, YearHint and RatingHint are what a wrong guess said: who
	// it shares with the answer, and where the answer sits from it.
	Shared     []Person
	YearHint   string
	RatingHint string
}

// MarshalJSON writes an entry in its type's shape.
func (e Entry) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case EntryFlip:
		return json.Marshal(struct {
			Type     string    `json:"type"`
			Card     string    `json:"card"`
			Cost     int       `json:"cost"`
			Film     *Film     `json:"film,omitempty"`
			Relative *Relation `json:"relative,omitempty"`
		}{e.Type, e.Card, e.Cost, e.Film, e.Relative})
	case EntryPerson:
		return json.Marshal(struct {
			Type   string   `json:"type"`
			Role   string   `json:"role"`
			Cost   int      `json:"cost"`
			People []Person `json:"people"`
		}{e.Type, e.Role, e.Cost, nonNil(e.People)})
	case EntryGenres:
		return json.Marshal(struct {
			Type   string   `json:"type"`
			Cost   int      `json:"cost"`
			Genres []string `json:"genres"`
		}{e.Type, e.Cost, nonNilStrings(e.Genres)})
	case EntryYear:
		return json.Marshal(struct {
			Type string `json:"type"`
			Cost int    `json:"cost"`
			Year int    `json:"year"`
		}{e.Type, e.Cost, e.Year})
	case EntryGuess:
		return json.Marshal(struct {
			Type   string   `json:"type"`
			Cost   int      `json:"cost"`
			Film   *Film    `json:"film"`
			Card   *string  `json:"card"`
			Shared []Person `json:"shared"`
			Year   *string  `json:"year"`
			Rating *string  `json:"rating"`
		}{e.Type, e.Cost, e.Film, orNull(e.Card), nonNil(e.Shared), orNull(e.YearHint), orNull(e.RatingHint)})
	default:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{e.Type})
	}
}

func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNil(p []Person) []Person {
	if p == nil {
		return []Person{}
	}
	return p
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// The two phases a game the page can see is in. A puzzle nobody has
// pressed Play on has no game at all.
const (
	PhasePlay = "play"
	PhaseDone = "done"
)

// Game is a game as the page draws it.
type Game struct {
	Phase string `json:"phase"`
	Pts   int    `json:"pts"`
	// Seq is how many moves are recorded, which the page sends back with
	// its next move.
	Seq int `json:"seq"`
	// StartedAt and FinishedAt are in UTC to the millisecond, as the
	// response's own "now" is, so the page reads all three alike.
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	// Secs is the time from Play to the end, in whole seconds, once
	// there is an end.
	Secs     *int     `json:"secs"`
	Won      bool     `json:"won"`
	GaveUp   bool     `json:"gaveUp"`
	NextCost int      `json:"nextCost"`
	Log      []Entry  `json:"log"`
	Known    []Person `json:"known"`
	End      *End     `json:"end"`
}

// End is everything kept back until the game is over: the answer, what
// every card is, and everyone.
type End struct {
	Answer AnswerFilm `json:"answer"`
	Cards  []EndCard  `json:"cards"`
	People []Person   `json:"people"`
}

// AnswerFilm is the answer with its genres.
type AnswerFilm struct {
	Film
	Genres []string `json:"genres"`
}

// EndCard is a card as the finished map draws it: what it is, and its
// people as slots.
type EndCard struct {
	ID     string `json:"id"`
	Film   Film   `json:"film"`
	People []int  `json:"people"`
}

// Instant is a moment as the API writes it: in UTC, to the millisecond.
// Postgres keeps microseconds, and the server's own zone is nobody's
// business.
func Instant(t time.Time) time.Time {
	return t.UTC().Truncate(time.Millisecond)
}

// Secs is a game's time in whole seconds, rounded as the clock on the
// page rounds it.
func Secs(ms int64) int {
	return int((ms + 500) / 1000)
}

// Render is a recorded game as the page may see it, with live's posters
// and photos.
func Render(p *Puzzle, rec *Record, live Live) Game {
	s := Replay(p, rec.Moves)
	g := Game{
		Phase:     PhasePlay,
		Pts:       s.Pts,
		Seq:       len(rec.Moves),
		StartedAt: Instant(rec.Started),
		Won:       s.Won,
		GaveUp:    s.GaveUp,
		NextCost:  s.NextCost(),
		Log:       []Entry{{Type: EntryStart}},
		Known:     p.persons(s.Known(), live),
	}
	if s.Done {
		g.Phase = PhaseDone
		if rec.Finished != nil {
			at := Instant(*rec.Finished)
			g.FinishedAt = &at
			// The time is taken from the moments as kept, the way the
			// board's ms was, so the two never round apart.
			secs := Secs(rec.Finished.Sub(rec.Started).Milliseconds())
			g.Secs = &secs
		}
	}
	for i, st := range s.steps {
		g.Log = append(g.Log, p.entries(s, st, i == len(s.steps)-1, live)...)
	}
	if s.Done {
		g.End = p.end(live)
	}
	return g
}

// entries is the log's lines for one move; last is whether it is the
// game's latest.
func (p *Puzzle) entries(s *State, st step, last bool, live Live) []Entry {
	m := st.move
	switch m.Kind {
	case KindFlip:
		e := Entry{Type: EntryFlip, Card: m.Arg, Cost: m.Cost}
		c := p.card(m.Arg)
		if c == nil {
			return []Entry{e}
		}
		// A close relative stays nameless until the player names it or
		// the game ends: its title would give the answer away.
		if c.Relative() && !s.Done && !s.guessed[c.Film] {
			e.Relative = &Relation{Shared: len(c.People)}
		} else {
			f := c.film(live)
			e.Film = &f
		}
		return []Entry{e}
	case KindDirector, KindActor:
		return []Entry{{Type: EntryPerson, Role: m.Kind, Cost: m.Cost, People: p.persons(st.people, live)}}
	case KindGenres:
		return []Entry{{Type: EntryGenres, Cost: m.Cost, Genres: p.Answer.Genres}}
	case KindYear:
		return []Entry{{Type: EntryYear, Cost: m.Cost, Year: p.Answer.Year}}
	case KindGuess:
		if m.Arg == p.Answer.ID {
			return []Entry{{Type: EntryWin}}
		}
		e := Entry{Type: EntryGuess, Cost: m.Cost}
		f := Film{ID: m.Arg}
		if g := m.Guess; g != nil {
			f.Title, f.Year, f.Rating, f.MD = g.Title, g.Year, g.Rating, g.MD
			e.Shared = p.persons(g.Shared, live)
			e.YearHint, e.RatingHint = g.YearHint, g.RatingHint
		}
		f.Poster = live.Posters[m.Arg]
		e.Film = &f
		if c := p.cardOf(m.Arg); c != nil {
			e.Card = c.ID
		}
		out := []Entry{e}
		// The guess that spent the last point is the one that ended it,
		// and the only one a replay can see doing so.
		if s.Out && last {
			out = append(out, Entry{Type: EntryOut})
		}
		return out
	case KindReveal:
		return []Entry{{Type: EntryGaveUp}}
	}
	return nil
}

// film is what a card is.
func (c Card) film(live Live) Film {
	r := c.Rating
	return Film{ID: c.Film, Title: c.Title, Year: c.Year, Rating: &r, MD: c.MD, Poster: live.Posters[c.Film]}
}

// persons are slots as the page draws them.
func (p *Puzzle) persons(slots []int, live Live) []Person {
	out := make([]Person, 0, len(slots))
	for _, i := range slots {
		if i < 0 || i >= len(p.People) {
			continue
		}
		s := p.People[i]
		out = append(out, Person{ID: s.ID, Name: s.Name, Role: s.Role, Slot: i, Photo: live.Photos[s.ID], Cards: p.cardsOf(i)})
	}
	return out
}

// cardsOf is the cards a slot is on, in card id order.
func (p *Puzzle) cardsOf(slot int) []string {
	out := []string{}
	for _, c := range p.Cards {
		for _, i := range c.People {
			if i == slot {
				out = append(out, c.ID)
				break
			}
		}
	}
	return out
}

// end is what the page is given once the game is over.
func (p *Puzzle) end(live Live) *End {
	r := p.Answer.Rating
	e := &End{
		Answer: AnswerFilm{
			Film:   Film{ID: p.Answer.ID, Title: p.Answer.Title, Year: p.Answer.Year, Rating: &r, MD: p.Answer.MD, Poster: live.Posters[p.Answer.ID]},
			Genres: nonNilStrings(p.Answer.Genres),
		},
		Cards: make([]EndCard, len(p.Cards)),
	}
	for i, c := range p.Cards {
		people := c.People
		if people == nil {
			people = []int{}
		}
		e.Cards[i] = EndCard{ID: c.ID, Film: c.film(live), People: people}
	}
	all := make([]int, len(p.People))
	for i := range all {
		all[i] = i
	}
	e.People = p.persons(all, live)
	return e
}

// Wants are the movies and people whose posters and photos a game shows
// as it stands, for one read of each: the starting cards, the cards
// turned over or guessed, and the movies guessed, and the people known;
// everything once the game is over. A nil rec is a puzzle nobody has
// started, which shows only the starting cards.
func (p *Puzzle) Wants(rec *Record) (films, people []string) {
	films = append(films, cardFilms(p, p.Start)...)
	if rec == nil {
		return films, nil
	}
	s := Replay(p, rec.Moves)
	if s.Done {
		films = append(films, p.Answer.ID)
		for _, c := range p.Cards {
			films = append(films, c.Film)
		}
		return films, p.PeopleIDs()
	}
	for _, m := range rec.Moves {
		switch m.Kind {
		case KindFlip:
			films = append(films, cardFilms(p, []string{m.Arg})...)
		case KindGuess:
			films = append(films, m.Arg)
		}
	}
	for _, i := range s.Known() {
		people = append(people, p.People[i].ID)
	}
	return films, people
}

func cardFilms(p *Puzzle, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if c := p.card(id); c != nil {
			out = append(out, c.Film)
		}
	}
	return out
}
