package daily

import (
	"cmp"
	"encoding/json"
	"math"
	"slices"
	"time"
)

// What the page is shown of a game. Everything here is drawn from a
// replay, and a replay decides what may be seen: until a game ends
// nothing below names the answer or says its year, rating, length,
// genres, poster or directors, nothing names a cast member the player
// has not been shown, and a fact not bought is not there at all. The
// poster's colour is one exception, sent from the start, since it fills
// the hidden card. The Movies sheet is the other: it carries the answer
// as one card among the rest, rationed in its own way (below), and a
// game reads only the one it opened.
// The tests in view_test.go hold every response to that.

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
	EntryNext   = "next"
	EntryFact   = "fact"
	EntrySheet  = "sheet"
	EntryGuess  = "guess"
	EntryWin    = "win"
	EntryGaveUp = "gaveup"
	EntryOut    = "out"
)

// Entry is one line of the log. Which fields it carries depends on its
// Type, and MarshalJSON writes only those.
type Entry struct {
	Type string
	Cost int
	// Slot is the slot a Next name showed; Kind the fact bought; Person
	// whose sheet was opened, by id; Guess what a wrong guess said.
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
	case EntrySheet:
		// No cost: opening the sheet is free, so it is never among what
		// the player paid for.
		return json.Marshal(struct {
			Type   string `json:"type"`
			Person string `json:"person"`
		}{e.Type, e.Person})
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
	// Sheet is whose Movies sheet the player opened, by id, null until
	// they open one. It names someone they were shown when they did.
	Sheet *string `json:"sheet"`
	Log   []Entry `json:"log"`
	End   *End    `json:"end"`
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
		Log:       []Entry{},
	}
	if s.Sheet != "" {
		sheet := s.Sheet
		g.Sheet = &sheet
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
	case m.Kind == KindSheet:
		return []Entry{{Type: EntrySheet, Person: m.Arg}}
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

// The Movies sheet: one showing person's movies on a small Cinedikt map,
// today's among them and unmarked. Its cards are the one place a game in
// play is sent the answer's title, year and rating, so what a card says
// is rationed. Every card used to be readable, with which of the slots
// showing it credited, and once two or three names were showing the one
// card they all shared was the answer: matched across two people's
// sheets by year and exact rating it gave itself away, whatever the
// page drew. Now a card is readable only inside the ranges the player
// has bought, and every other card is blank, only where it sits on the
// map, its rating no finer than the half point the map places it by.
// And a game in play reads one sheet, the one it opened: inside a range
// or two, two sheets' readable titles almost never share more than
// today's.

// Opens is whether the player may read the Movies sheet of the cast
// member in slot as the game stands: until the end, only the one they
// opened, who was showing when they did (Apply); once it is over,
// anyone's in the cast. Before they open one, no one's.
func (p *Puzzle) Opens(s *State, slot int) bool {
	if !s.Shown(slot) {
		return false
	}
	return s.Done || (s.Sheet != "" && p.Cast[slot].ID == s.Sheet)
}

// readable is whether a movie on the sheets may be read as the game
// stands. Once a range is bought (the decade or the five years, the
// rating band, the genre) a movie is readable when it is inside every
// one bought: its year in the decade and in the five years, its rating
// in the band, its genres holding every one of the answer's. Before one
// is, nothing is. Once the game is over, everything is. The ranges are
// the answer's own, so the answer is readable from the first range
// bought and blank until then, as any card inside them would be: nothing
// in the rule marks it out. Length is no range here: other movies'
// runtimes are not on a map. The rating band is RatingBand's, compared
// in tenths, so a movie is in it from its floor up to, and not
// including, its ceiling, as the page draws it.
func (p *Puzzle) readable(s *State, m Movie) bool {
	if s.Done {
		return true
	}
	a, ranged := p.Answer, false
	if s.Facts[KindDecade] {
		if Decade(m.Year) != Decade(a.Year) {
			return false
		}
		ranged = true
	}
	if s.Facts[KindYears] {
		if Years(m.Year) != Years(a.Year) {
			return false
		}
		ranged = true
	}
	if s.Facts[KindRating] {
		if RatingBand(m.Rating) != RatingBand(a.Rating) {
			return false
		}
		ranged = true
	}
	if s.Facts[KindGenre] {
		for _, g := range a.Genres {
			if !slices.Contains(m.Genres, g) {
				return false
			}
		}
		ranged = true
	}
	return ranged
}

// halfPoint is a rating as a blank card is placed by it: to the nearest
// half point, so 7.2 is 7.0, 7.3 is 7.5 and 7.8 is 8.0. IMDb rates in
// tenths, so no rating sits exactly between two half points.
func halfPoint(rating float64) float64 {
	return math.Round(rating*2) / 2
}

// SheetCard is one movie on a Movies sheet as the page draws it. A
// readable card is the movie: its id, title, year, rating and genres,
// and its poster when every readable card on the sheet has one. A blank
// card is where a movie sits on the map and nothing more: its year, and
// At, its rating to the nearest half point. SheetOf never puts anything
// else of a blank card's movie into it, so no field can carry what the
// card hides, and MarshalJSON writes it as {year, at} whatever it holds.
type SheetCard struct {
	Readable bool
	ID       string
	Title    string
	Year     int
	Rating   float64
	Genres   []string
	Poster   string
	At       float64
}

// MarshalJSON writes a readable card as {id, title, year, rating,
// genres, poster?} and a blank one as {year, at}: the page tells them
// apart by the id.
func (c SheetCard) MarshalJSON() ([]byte, error) {
	if !c.Readable {
		return json.Marshal(struct {
			Year int     `json:"year"`
			At   float64 `json:"at"`
		}{c.Year, c.At})
	}
	return json.Marshal(struct {
		ID     string   `json:"id"`
		Title  string   `json:"title"`
		Year   int      `json:"year"`
		Rating float64  `json:"rating"`
		Genres []string `json:"genres"`
		Poster string   `json:"poster,omitempty"`
	}{c.ID, c.Title, c.Year, c.Rating, nonNilStrings(c.Genres), c.Poster})
}

// SheetOf is the Movies sheet of the cast member in slot, as the game
// stands: every movie on their sheet, the answer among them, each
// readable or blank as readable says. Blank cards are kept, so the map
// keeps its shape, and the list is ordered by year, then by At, then
// readable before blank, then by id, so where a card sits in the list
// says nothing its face does not: in the puzzle's order, by id within a
// year, a blank card's place among its neighbours would say which ids it
// fell between, and two sheets' blank cards could be lined up by it.
// ok is false when the sheet is not one the player may read (Opens):
// while the game is on, anyone's but the one they opened, and a slot
// that is none.
//
// Every readable card gets its poster, or none does: the answer always
// has one, since a movie without one is never an answer, so a sheet where
// only some readable cards had theirs would mark it out as one of those.
// A blank card never has one. SheetWants is what to read live's posters
// for.
func (p *Puzzle) SheetOf(s *State, slot int, live Live) ([]SheetCard, bool) {
	if !p.Opens(s, slot) {
		return nil, false
	}
	theirs := p.sheet(slot)
	out := make([]SheetCard, len(theirs))
	all := true
	for i, m := range theirs {
		if !p.readable(s, m) {
			out[i] = SheetCard{Year: m.Year, At: halfPoint(m.Rating)}
			continue
		}
		out[i] = SheetCard{Readable: true, ID: m.ID, Title: m.Title, Year: m.Year, Rating: m.Rating,
			Genres: nonNilStrings(slices.Clone(m.Genres)), At: halfPoint(m.Rating)}
		all = all && live.Posters[m.ID] != ""
	}
	for i := range out {
		if all && out[i].Readable {
			out[i].Poster = live.Posters[out[i].ID]
		}
	}
	blank := func(c SheetCard) int {
		if c.Readable {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(out, func(x, y SheetCard) int {
		return cmp.Or(cmp.Compare(x.Year, y.Year), cmp.Compare(x.At, y.At), cmp.Compare(blank(x), blank(y)), cmp.Compare(x.ID, y.ID))
	})
	return out, true
}

// SheetWants are the movies on slot's sheet that are readable as the game
// stands, whose posters it reads: none before a range is bought, and
// none for a sheet the player may not read (Opens). Once the game is
// over it is the whole sheet.
func (p *Puzzle) SheetWants(s *State, slot int) []string {
	if !p.Opens(s, slot) {
		return nil
	}
	var out []string
	for _, m := range p.sheet(slot) {
		if p.readable(s, m) {
			out = append(out, m.ID)
		}
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
