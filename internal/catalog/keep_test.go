package catalog

import "testing"

// row reads one data row against a header, the way an import does.
func row(t *testing.T, header, data string) *Reader {
	t.Helper()
	r, err := NewReader(gzipped(t, header, data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if !r.Next() {
		t.Fatal("no row")
	}
	return r
}

const basicsHeader = "tconst\ttitleType\tprimaryTitle\toriginalTitle\tisAdult\tstartYear\tendYear\truntimeMinutes\tgenres"

func TestOnlyMoviesAreKept(t *testing.T) {
	// The nine types that are not `movie` are most of the file.
	for _, kind := range []string{"tvEpisode", "tvSeries", "short", "video", "tvMovie", "videoGame", "tvMiniSeries", "tvSpecial", "tvShort"} {
		r := row(t, basicsHeader, "tt1\t"+kind+"\tSomething\tSomething\t0\t1999\t\\N\t90\tDrama")
		if _, ok := ReadTitle(r); ok {
			t.Errorf("%s was stored", kind)
		}
	}
	r := row(t, basicsHeader, "tt0133093\tmovie\tThe Matrix\tThe Matrix\t0\t1999\t\\N\t136\tAction,Sci-Fi")
	got, ok := ReadTitle(r)
	if !ok {
		t.Fatal("a movie was dropped")
	}
	if got.TConst != "tt0133093" || got.Primary != "The Matrix" || got.StartYear != 1999 {
		t.Errorf("title = %+v", got)
	}
	if len(got.Genres) != 2 {
		t.Errorf("genres = %v", got.Genres)
	}
}

// TestARuntimeIsKeptInMinutes, the Daily's Length fact, and none is
// nothing: IMDb's \N, a stray value, or a runtime of no minutes or less.
func TestARuntimeIsKeptInMinutes(t *testing.T) {
	for field, want := range map[string]int{"136": 136, "90": 90, "\\N": 0, "": 0, "abc": 0, "0": 0, "-5": 0} {
		r := row(t, basicsHeader, "tt0133093\tmovie\tThe Matrix\tThe Matrix\t0\t1999\t\\N\t"+field+"\tAction,Sci-Fi")
		got, ok := ReadTitle(r)
		if !ok || got.Runtime != want {
			t.Errorf("runtimeMinutes %q: %d, %v; want %d", field, got.Runtime, ok, want)
		}
	}
}

func TestAdultAndDocumentaryAreStoredNotDropped(t *testing.T) {
	// Both are movies, so they are loaded. The grid leaves them out when
	// it builds a map, and search excludes adult through its own index.
	r := row(t, basicsHeader, "tt2\tmovie\tA Documentary\tA Documentary\t0\t2011\t\\N\t88\tDocumentary")
	doc, ok := ReadTitle(r)
	if !ok {
		t.Fatal("a documentary was dropped at load")
	}
	if len(doc.Genres) != 1 || doc.Genres[0] != "Documentary" {
		t.Errorf("genres = %v", doc.Genres)
	}
	r = row(t, basicsHeader, "tt3\tmovie\tAdult Film\tAdult Film\t1\t2011\t\\N\t88\tAdult")
	adult, ok := ReadTitle(r)
	if !ok || !adult.IsAdult {
		t.Errorf("adult title = %+v, ok=%v", adult, ok)
	}
}

func TestATitleWithNoYearIsStillKept(t *testing.T) {
	// The year decides whether it lands on a map, not whether it loads.
	r := row(t, basicsHeader, "tt4\tmovie\tUndated\tUndated\t0\t\\N\t\\N\t\\N\t\\N")
	got, ok := ReadTitle(r)
	if !ok {
		t.Fatal("a movie with no year was dropped at load")
	}
	if got.StartYear != 0 || got.Genres != nil {
		t.Errorf("title = %+v", got)
	}
}

const principalsHeader = "tconst\tordering\tnconst\tcategory\tjob\tcharacters"

func TestPrincipalsKeepOnlyCastAndDirectorsOfKeptFilms(t *testing.T) {
	movies := func(id string) bool { return id == "tt0133093" }

	kept := map[string]bool{"actor": true, "actress": true, "director": true}
	for _, category := range []string{"actor", "actress", "director", "writer", "composer", "producer", "editor", "self", "cinematographer"} {
		r := row(t, principalsHeader, "tt0133093\t1\tnm1\t"+category+"\t\\N\t[\"Neo\"]")
		_, ok := ReadPrincipal(r, movies)
		if ok != kept[category] {
			t.Errorf("category %s kept=%v, want %v", category, ok, kept[category])
		}
	}

	// A credit on a title that did not survive title.basics goes too,
	// which is how the television bulk leaves this file.
	r := row(t, principalsHeader, "tt9999999\t1\tnm1\tactor\t\\N\t[\"Someone\"]")
	if _, ok := ReadPrincipal(r, movies); ok {
		t.Error("a credit on an unkept title was stored")
	}
}

func TestPrincipalCarriesOrderAndCharacter(t *testing.T) {
	all := func(string) bool { return true }
	r := row(t, principalsHeader, "tt0133093\t1\tnm0000206\tactor\t\\N\t[\"Neo\"]")
	got, ok := ReadPrincipal(r, all)
	if !ok {
		t.Fatal("dropped")
	}
	if got.Ordering != 1 || got.NConst != "nm0000206" || got.Character != "Neo" {
		t.Errorf("principal = %+v", got)
	}
	// Billing order is what the chip row is sorted by, so a row without
	// one cannot be placed and is not kept.
	r = row(t, principalsHeader, "tt0133093\t\\N\tnm1\tactor\t\\N\t\\N")
	if _, ok := ReadPrincipal(r, all); ok {
		t.Error("a credit with no ordering was stored")
	}
}

func TestFirstCharacter(t *testing.T) {
	for _, c := range []struct{ field, want string }{
		{`["Neo"]`, "Neo"},
		{`["Trinity","Trin"]`, "Trinity"},
		{`[]`, ""},
		{`\N`, ""},
		{"", ""},
		{`["Dr. King Schultz"]`, "Dr. King Schultz"},
		{`["Jean-Luc \"Frenchy\" Picard"]`, `Jean-Luc "Frenchy" Picard`},
		{`["  padded  "]`, "padded"},
		{`["unterminated`, ""},
		{`nonsense`, ""},
	} {
		if got := FirstCharacter(c.field); got != c.want {
			t.Errorf("FirstCharacter(%q) = %q, want %q", c.field, got, c.want)
		}
	}
}

func TestDirectorsAreExplodedFromCrew(t *testing.T) {
	const header = "tconst\tdirectors\twriters"
	movies := func(id string) bool { return id == "tt0133093" }

	r := row(t, header, "tt0133093\tnm0905154,nm0905152\tnm0905152")
	got := ReadDirectors(r, movies)
	if len(got) != 2 || got[0].NConst != "nm0905154" || got[1].NConst != "nm0905152" {
		t.Fatalf("directors = %+v", got)
	}
	// Writers are ignored even when the same person wrote and directed.
	for _, d := range got {
		if d.TConst != "tt0133093" {
			t.Errorf("director on the wrong title: %+v", d)
		}
	}

	if got := ReadDirectors(row(t, header, "tt0133093\t\\N\tnm1"), movies); got != nil {
		t.Errorf("a film with no director gave %v", got)
	}
	if got := ReadDirectors(row(t, header, "tt9999999\tnm1\tnm2"), movies); got != nil {
		t.Error("a director of an unkept title was stored")
	}
	// A series' director never reaches this file's kept list at all.
	if got := ReadDirectors(row(t, header, "tt0133093\tnm1,nm1\t\\N"), movies); len(got) != 1 {
		t.Errorf("a person listed twice gave %d rows", len(got))
	}
}

func TestRatings(t *testing.T) {
	const header = "tconst\taverageRating\tnumVotes"
	all := func(string) bool { return true }

	r := row(t, header, "tt0133093\t8.7\t2081234")
	got, ok := ReadRating(r, all)
	if !ok || got.Average != 8.7 || got.Votes != 2081234 {
		t.Errorf("rating = %+v, ok=%v", got, ok)
	}
	// An unrated title has no row rather than a zero, so it sits in the
	// unrated column instead of at the bottom of the axis.
	if _, ok := ReadRating(row(t, header, "tt1\t\\N\t\\N"), all); ok {
		t.Error("a null rating was stored")
	}
	if _, ok := ReadRating(row(t, header, "tt1\t0.0\t5"), all); ok {
		t.Error("a zero rating was stored as a score")
	}
	if _, ok := ReadRating(row(t, header, "tt9\t8.7\t10"), func(string) bool { return false }); ok {
		t.Error("a rating for an unkept title was stored")
	}
}

func TestNamesAreKeptOnlyWhenCredited(t *testing.T) {
	const header = "nconst\tprimaryName\tbirthYear\tdeathYear\tprimaryProfession\tknownForTitles"
	credited := func(id string) bool { return id == "nm0000206" }

	r := row(t, header, "nm0000206\tKeanu Reeves\t1964\t\\N\tactor\ttt0133093")
	got, ok := ReadName(r, credited)
	if !ok || got.Primary != "Keanu Reeves" {
		t.Errorf("name = %+v, ok=%v", got, ok)
	}
	// Someone known only from television never had a credit survive.
	if _, ok := ReadName(row(t, header, "nm9999999\tTV Person\t1970\t\\N\tactor\ttt9"), credited); ok {
		t.Error("an uncredited person was stored")
	}
}
