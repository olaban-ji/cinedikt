package daily

import (
	"encoding/json"
	"slices"
	"time"
)

// What the page is shown of a game. Everything here is drawn from a
// replay, and a replay decides what may be seen: until a game ends
// nothing below names the answer or says its year, rating, length,
// genres, poster or directors, nothing names a cast member the player
// has not been shown, and a fact not bought is not there at all. The
// poster's colour is the one exception, sent from the start, since it
// fills the hidden card. The tests in view_test.go hold every response
// to that.

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

// Person is one of the answer's people the page may name, with their hue
// and their photo where there is one.
type Person struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Hue   int    `json:"hue"`
	Photo string `json:"photo,omitempty"`
}

// Ref is a movie the page is given by name only: the wrong guess that
// filled a slot in.
type Ref struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// SlotView is one of the six as the page draws it. A hidden one is only
// its slot; a shown one is who they are, their "Also in" movie, how
// they came to be showing, and the guess that filled them in, when one
// did.
type SlotView struct {
	Slot   int
	Shown  bool
	Person *Person
	Also   *Also
	Via    string
	From   *Ref
}

// MarshalJSON writes a hidden slot as {"slot":n,"shown":false} and
// nothing more, so no field of it can ever carry a hidden name.
func (v SlotView) MarshalJSON() ([]byte, error) {
	if !v.Shown {
		return json.Marshal(struct {
			Slot  int  `json:"slot"`
			Shown bool `json:"shown"`
		}{v.Slot, false})
	}
	return json.Marshal(struct {
		Slot   int     `json:"slot"`
		Shown  bool    `json:"shown"`
		Person *Person `json:"person"`
		Also   *Also   `json:"also,omitempty"`
		Via    string  `json:"via"`
		From   *Ref    `json:"from,omitempty"`
	}{v.Slot, true, v.Person, v.Also, v.Via, v.From})
}

// Facts are the facts bought, each present only once it is: the length
// and rating as bands (LengthBand, RatingBand), the genres, the decade
// and five-year range as their first years, and the directors.
type Facts struct {
	Length   *int      `json:"length,omitempty"`
	Rating   *int      `json:"rating,omitempty"`
	Genre    *[]string `json:"genre,omitempty"`
	Decade   *int      `json:"decade,omitempty"`
	Years    *int      `json:"years,omitempty"`
	Director *[]Person `json:"director,omitempty"`
}

// GuessView is a wrong guess as the page draws it.
type GuessView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Year        int    `json:"year"`
	Cost        int    `json:"cost"`
	Shared      []int  `json:"shared"`
	SameDecade  bool   `json:"sameDecade"`
	SharesGenre bool   `json:"sharesGenre"`
	Warmth      int    `json:"warmth"`
}

// The entries in a game's log: one per move, and an "out" after the
// wrong guess that ended it.
const (
	EntryNext    = "next"
	EntryFact    = "fact"
	EntryOverlap = "overlap"
	EntryGuess   = "guess"
	EntryWin     = "win"
	EntryGaveUp  = "gaveup"
	EntryOut     = "out"
)

// Entry is one line of the log. Which fields it carries depends on its
// Type, and MarshalJSON writes only those.
type Entry struct {
	Type string
	Cost int
	// Slot is the slot a Next name showed; Kind the fact bought; Person
	// the person an overlap added; Guess what a wrong guess said.
	Slot   int
	Kind   string
	Person string
	Guess  *GuessView
}

// MarshalJSON writes an entry in its type's shape.
func (e Entry) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case EntryNext:
		return json.Marshal(struct {
			Type string `json:"type"`
			Cost int    `json:"cost"`
			Slot int    `json:"slot"`
		}{e.Type, e.Cost, e.Slot})
	case EntryFact:
		return json.Marshal(struct {
			Type string `json:"type"`
			Kind string `json:"kind"`
			Cost int    `json:"cost"`
		}{e.Type, e.Kind, e.Cost})
	case EntryOverlap:
		return json.Marshal(struct {
			Type   string `json:"type"`
			Person string `json:"person"`
			Cost   int    `json:"cost"`
		}{e.Type, e.Person, e.Cost})
	case EntryGuess:
		return json.Marshal(struct {
			Type  string     `json:"type"`
			Cost  int        `json:"cost"`
			Guess *GuessView `json:"guess"`
		}{e.Type, e.Cost, e.Guess})
	default:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{e.Type})
	}
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
	// response's own "now" is, so the page reads all three alike. There
	// is no clock in the game: they are only when.
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	Won        bool       `json:"won"`
	GaveUp     bool       `json:"gaveUp"`
	// NextCost is what the next wrong guess costs.
	NextCost int        `json:"nextCost"`
	Slots    []SlotView `json:"slots"`
	Facts    Facts      `json:"facts"`
	Overlaps []string   `json:"overlaps"`
	Log      []Entry    `json:"log"`
	End      *End       `json:"end"`
}

// End is everything kept back until the game is over: the answer, with
// every fact exact, and its directors.
type End struct {
	Answer    EndAnswer `json:"answer"`
	Directors []Person  `json:"directors"`
}

// EndAnswer is the answer as the end shows it.
type EndAnswer struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Year   int      `json:"year"`
	Rating float64  `json:"rating"`
	Length int      `json:"length"`
	Genres []string `json:"genres"`
	Colour string   `json:"colour"`
	Poster string   `json:"poster,omitempty"`
}

// Instant is a moment as the API writes it: in UTC, to the millisecond.
// Postgres keeps microseconds, and the server's own zone is nobody's
// business.
func Instant(t time.Time) time.Time {
	return t.UTC().Truncate(time.Millisecond)
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
		Slots:     make([]SlotView, len(p.Cast)),
		Facts:     p.facts(s, live),
		Overlaps:  append([]string{}, s.Overlaps...),
		Log:       []Entry{},
	}
	if s.Done {
		g.Phase = PhaseDone
		if rec.Finished != nil {
			at := Instant(*rec.Finished)
			g.FinishedAt = &at
		}
	}
	for i := range p.Cast {
		g.Slots[i] = p.slotView(s, i, live)
	}
	for i, st := range s.steps {
		g.Log = append(g.Log, p.entries(s, st, i == len(s.steps)-1)...)
	}
	if s.Done {
		g.End = p.end(live)
	}
	return g
}

// slotView is slot i as the page may see it: who they are once they are
// showing, and "end" for anyone the game ended before the player saw.
func (p *Puzzle) slotView(s *State, i int, live Live) SlotView {
	if !s.Shown(i) {
		return SlotView{Slot: i}
	}
	b := p.Cast[i]
	v := SlotView{
		Slot:   i,
		Shown:  true,
		Person: &Person{ID: b.ID, Name: b.Name, Hue: p.CastHue(i), Photo: live.Photos[b.ID]},
		Also:   b.Also,
		Via:    s.via[i],
	}
	if v.Via == "" {
		v.Via = ViaEnd
	}
	if from := s.from[i]; from.ID != "" {
		v.From = &from
	}
	return v
}

// facts are the facts bought, worked out from the answer.
func (p *Puzzle) facts(s *State, live Live) Facts {
	var f Facts
	a := p.Answer
	if s.Facts[KindLength] {
		band := LengthBand(a.Length)
		f.Length = &band
	}
	if s.Facts[KindRating] {
		band := RatingBand(a.Rating)
		f.Rating = &band
	}
	if s.Facts[KindGenre] {
		genres := nonNilStrings(slices.Clone(a.Genres))
		f.Genre = &genres
	}
	if s.Facts[KindDecade] {
		decade := Decade(a.Year)
		f.Decade = &decade
	}
	if s.Facts[KindYears] {
		years := Years(a.Year)
		f.Years = &years
	}
	if s.Facts[KindDirector] {
		directors := p.directors(live)
		f.Director = &directors
	}
	return f
}

// directors are the answer's directors as the page draws them.
func (p *Puzzle) directors(live Live) []Person {
	out := make([]Person, len(p.Directors))
	for i, d := range p.Directors {
		out[i] = Person{ID: d.ID, Name: d.Name, Hue: p.DirectorHue(i), Photo: live.Photos[d.ID]}
	}
	return out
}

// entries is the log's lines for one move; last is whether it is the
// game's latest.
func (p *Puzzle) entries(s *State, st step, last bool) []Entry {
	m := st.move
	switch {
	case m.Kind == KindNext:
		return []Entry{{Type: EntryNext, Cost: m.Cost, Slot: st.slot}}
	case IsFact(m.Kind):
		return []Entry{{Type: EntryFact, Kind: m.Kind, Cost: m.Cost}}
	case m.Kind == KindOverlap:
		return []Entry{{Type: EntryOverlap, Person: m.Arg, Cost: m.Cost}}
	case m.Kind == KindGuess:
		if m.Arg == p.Answer.ID {
			return []Entry{{Type: EntryWin}}
		}
		g := &GuessView{ID: m.Arg, Cost: m.Cost, Shared: []int{}}
		if r := m.Guess; r != nil {
			g.Title, g.Year, g.SameDecade, g.SharesGenre, g.Warmth = r.Title, r.Year, r.SameDecade, r.SharesGenre, r.Warmth()
			g.Shared = append(g.Shared, r.Shared...)
		}
		out := []Entry{{Type: EntryGuess, Cost: m.Cost, Guess: g}}
		// The guess the points could not cover is the one that ended it,
		// and the only one a replay can see doing so.
		if s.Out && last {
			out = append(out, Entry{Type: EntryOut})
		}
		return out
	case m.Kind == KindReveal:
		return []Entry{{Type: EntryGaveUp}}
	}
	return nil
}

// end is what the page is given once the game is over.
func (p *Puzzle) end(live Live) *End {
	a := p.Answer
	return &End{
		Answer: EndAnswer{ID: a.ID, Title: a.Title, Year: a.Year, Rating: a.Rating, Length: a.Length,
			Genres: nonNilStrings(slices.Clone(a.Genres)), Colour: a.Colour, Poster: live.Posters[a.ID]},
		Directors: p.directors(live),
	}
}

// Wants are the movies and people whose posters and photos a game shows
// as it stands, for one read of each: the people showing and the
// directors once bought, and, once it is over, the answer's poster and
// everyone. A nil rec is a puzzle nobody has started, which shows
// nobody: the hidden card is only its colour.
func (p *Puzzle) Wants(rec *Record) (films, people []string) {
	if rec == nil {
		return nil, nil
	}
	s := Replay(p, rec.Moves)
	if s.Done {
		films = append(films, p.Answer.ID)
	}
	for i, b := range p.Cast {
		if s.Shown(i) {
			people = append(people, b.ID)
		}
	}
	if s.Done || s.Facts[KindDirector] {
		people = append(people, p.DirectorIDs()...)
	}
	return films, people
}

// SheetMovie is one movie on a Movies sheet as the page draws it: what
// the card says, which of the slots showing it credits, and whether a
// director is on it once the Director fact is bought.
type SheetMovie struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Year   int      `json:"year"`
	Rating float64  `json:"rating"`
	Genres []string `json:"genres"`
	Poster string   `json:"poster,omitempty"`
	On     []int    `json:"on"`
	Dir    bool     `json:"dir,omitempty"`
}

// SheetOf is the Movies sheet of the cast member in slot, as the game
// stands: their movies, the answer among them, each with the slots
// showing it credits, never one still hidden, and Dir only once the
// Director fact is bought. ok is false when the slot is not showing, or
// is no slot at all; once the game is over everyone is.
//
// Every card gets its poster, or none does: the answer always has one,
// since a movie without one is never an answer, so a sheet where only
// some cards had theirs would mark it out as one of those. SheetWants is
// what to read live's posters for.
func (p *Puzzle) SheetOf(s *State, slot int, live Live) ([]SheetMovie, bool) {
	if !s.Shown(slot) {
		return nil, false
	}
	dir := s.Facts[KindDirector]
	theirs := p.sheet(slot)
	all := true
	for _, m := range theirs {
		all = all && live.Posters[m.ID] != ""
	}
	out := make([]SheetMovie, 0, len(theirs))
	for _, m := range theirs {
		v := SheetMovie{ID: m.ID, Title: m.Title, Year: m.Year, Rating: m.Rating,
			Genres: nonNilStrings(slices.Clone(m.Genres)), On: []int{}, Dir: dir && m.Dir}
		for _, i := range m.Cast {
			if s.Shown(i) {
				v.On = append(v.On, i)
			}
		}
		if all {
			v.Poster = live.Posters[m.ID]
		}
		out = append(out, v)
	}
	return out, true
}

// SheetWants are the movies on slot's sheet, whose posters it reads.
func (p *Puzzle) SheetWants(slot int) []string {
	theirs := p.sheet(slot)
	out := make([]string, len(theirs))
	for i, m := range theirs {
		out[i] = m.ID
	}
	return out
}

// sheet is slot's movies, in the puzzle's order: those on their own
// sheet, never one only another of the six's cap let in, so no sheet
// runs past MaxSheet nor holds a less voted movie of theirs because
// someone hidden is on it.
func (p *Puzzle) sheet(slot int) []Movie {
	var out []Movie
	for _, m := range p.Movies {
		sheets := m.Sheets
		if sheets == nil {
			sheets = m.Cast
		}
		if slices.Contains(sheets, slot) {
			out = append(out, m)
		}
	}
	return out
}
