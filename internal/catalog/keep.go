package catalog

import (
	"strconv"
	"strings"
)

// MovieType is the only titleType stored. A series, an episode, a short,
// a video and a made-for-TV film all sit in the same table as a feature,
// and all of them are dropped as the file is read. Nine of every ten
// rows in title.basics is a television episode.
const MovieType = "movie"

// The categories title.principals uses for the people a map is built
// from. Writers, composers and the rest of the crew are not kept.
const (
	CategoryActor    = "actor"
	CategoryActress  = "actress"
	CategoryDirector = "director"
)

// Title is a kept row of title.basics.
type Title struct {
	TConst    string
	Primary   string
	Original  string
	IsAdult   bool
	StartYear int // 0 when IMDb has none
	Genres    []string
}

// ReadTitle reads the current row as a Title, and reports false for any
// row that is not a movie. The caller keeps the returned TConst values
// as the allow-list the other four files are filtered against.
func ReadTitle(r *Reader) (Title, bool) {
	if r.Text("titleType") != MovieType {
		return Title{}, false
	}
	id := r.Text("tconst")
	if id == "" {
		return Title{}, false
	}
	year, _ := r.Int("startYear")
	return Title{
		TConst:    id,
		Primary:   r.Text("primaryTitle"),
		Original:  r.Text("originalTitle"),
		IsAdult:   r.Bool("isAdult"),
		StartYear: year,
		Genres:    r.List("genres"),
	}, true
}

// IsCast reports whether a principals category is a performance. IMDb
// splits these by gender; the map does not, so both are cast.
func IsCast(category string) bool {
	return category == CategoryActor || category == CategoryActress
}

// IsKeptCategory reports whether a principals row is worth storing.
func IsKeptCategory(category string) bool {
	return IsCast(category) || category == CategoryDirector
}

// Principal is a kept row of title.principals: someone billed on a movie
// that survived title.basics.
type Principal struct {
	TConst    string
	Ordering  int
	NConst    string
	Category  string
	Character string // empty when the row has none
}

// ReadPrincipal reads the current row, and reports false when the title
// was not kept or the credit is not one a map draws.
func ReadPrincipal(r *Reader, kept func(tconst string) bool) (Principal, bool) {
	id := r.Text("tconst")
	if id == "" || !kept(id) {
		return Principal{}, false
	}
	category := r.Text("category")
	if !IsKeptCategory(category) {
		return Principal{}, false
	}
	person := r.Text("nconst")
	if person == "" {
		return Principal{}, false
	}
	ordering, ok := r.Int("ordering")
	if !ok {
		return Principal{}, false
	}
	return Principal{
		TConst:    id,
		Ordering:  ordering,
		NConst:    person,
		Category:  category,
		Character: FirstCharacter(r.Text("characters")),
	}, true
}

// FirstCharacter reads the first name out of the JSON array string that
// title.principals stores, such as `["Neo"]`. Only the first is kept: a
// card has room for one part, and the rest are usually the same person
// under another name.
//
// It is read by hand rather than unmarshalled. The field is one shape,
// it appears on every credit of nine hundred thousand films, and a JSON
// decoder per row is a cost with nothing to show for it.
func FirstCharacter(field string) string {
	if field == "" || field == Null {
		return ""
	}
	open := strings.IndexByte(field, '"')
	if open < 0 {
		return ""
	}
	var out strings.Builder
	for i := open + 1; i < len(field); i++ {
		switch field[i] {
		case '\\':
			// A quote or a backslash inside the name is escaped; take
			// the next byte as it stands.
			if i+1 < len(field) {
				i++
				out.WriteByte(field[i])
			}
		case '"':
			return strings.TrimSpace(out.String())
		default:
			out.WriteByte(field[i])
		}
	}
	// No closing quote: the field is malformed, so it holds no name.
	return ""
}

// Director is one row of the exploded title.crew director list.
type Director struct {
	TConst string
	NConst string
}

// ReadDirectors explodes the current title.crew row. Directors are taken
// from here as well as from principals, because a film's director is
// often absent from the billed principals and a map without its director
// is missing the person who most defines it.
//
// Writers are ignored. The order of the returned slice is the order IMDb
// lists them, which is the order the chips are shown in.
func ReadDirectors(r *Reader, kept func(tconst string) bool) []Director {
	id := r.Text("tconst")
	if id == "" || !kept(id) {
		return nil
	}
	people := r.List("directors")
	if len(people) == 0 {
		return nil
	}
	out := make([]Director, 0, len(people))
	seen := make(map[string]bool, len(people))
	for _, person := range people {
		// A person listed twice on one film is still one director.
		if person == "" || seen[person] {
			continue
		}
		seen[person] = true
		out = append(out, Director{TConst: id, NConst: person})
	}
	return out
}

// Rating is a kept row of title.ratings.
type Rating struct {
	TConst  string
	Average float64
	Votes   int
}

// ReadRating reads the current row, and reports false when the title was
// not kept or the file has no usable score.
func ReadRating(r *Reader, kept func(tconst string) bool) (Rating, bool) {
	id := r.Text("tconst")
	if id == "" || !kept(id) {
		return Rating{}, false
	}
	average, ok := ParseRating(r.Text("averageRating"))
	if !ok {
		return Rating{}, false
	}
	votes, _ := r.Int("numVotes")
	return Rating{TConst: id, Average: average, Votes: votes}, true
}

// ParseRating reads IMDb's one-decimal score. A rating of zero is not a
// score anyone gave: the file writes `\N` for an unrated title, and a
// literal 0.0 would put an unrated film at the bottom of the axis
// instead of in the unrated column where it belongs.
func ParseRating(field string) (float64, bool) {
	if field == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(field, 64)
	if err != nil || v <= 0 || v > 10 {
		return 0, false
	}
	return v, true
}

// Name is a kept row of name.basics: someone with at least one credit on
// a movie that survived.
type Name struct {
	NConst  string
	Primary string
}

// ReadName reads the current row, and reports false for anyone whose id
// did not survive in principals or directors. name.basics is read last
// for exactly this reason: most of the people in it are known only from
// television.
func ReadName(r *Reader, credited func(nconst string) bool) (Name, bool) {
	id := r.Text("nconst")
	if id == "" || !credited(id) {
		return Name{}, false
	}
	return Name{NConst: id, Primary: r.Text("primaryName")}, true
}
