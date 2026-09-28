package omdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

// The bodies here are written for these tests, each as short as the
// fault it shows. They are shaped like OMDb's answers, and broken the
// ways its answers are.

func plotOf(t *testing.T, body string) string {
	t.Helper()
	got, err := parseTitle([]byte(body), "tt0000001")
	if err != nil {
		t.Fatalf("%q: %v", body, err)
	}
	return got.Plot
}

func TestRepairReadsWhatOMDbGetsWrong(t *testing.T) {
	for _, c := range []struct {
		name, body, plot string
	}{
		{"a backslash before a letter",
			`{"Response":"True","Plot":"A clerk\Sam and a baker\sing."}`,
			`A clerk\Sam and a baker\sing.`},
		{"an escaped apostrophe",
			`{"Response":"True","Plot":"The band \'Loud Crows\' play; the drummer\'s late."}`,
			`The band 'Loud Crows' play; the drummer's late.`},
		{"a raw tab",
			"{\"Response\":\"True\",\"Plot\":\"One\ttwo.\"}",
			"One two."},
		{"raw control bytes",
			"{\"Response\":\"True\",\"Plot\":\"Bu\x1fns and the \x1bend.\"}",
			"Buns and the end."},
		{"a stray backslash before the end of the object",
			`{"Response":"True","Plot":"It ends oddly \"}`,
			`It ends oddly \`},
		{"an escaped quote stays a quote",
			`{"Response":"True","Plot":"He said \"hi\" to her\qand left."}`,
			`He said "hi" to her\qand left.`},
		{"an escaped backslash stays one backslash",
			`{"Response":"True","Plot":"A \\ B, and C\D."}`,
			`A \ B, and C\D.`},
		// The backslash and the u are kept apart in the source, so no tool
		// that rewrites this file can turn the escape into the character
		// it stands for. Go joins them back into the escape.
		{"a \\u escape stays its character",
			`{"Response":"True","Plot":"Caf\` + `u00e9\zone."}`,
			"Café\\zone."},
		// A brace or bracket just after a quote that really is escaped is
		// part of the plot, not the end of the field, in a body that is
		// repaired for something else.
		{"an escaped quote before a bracket",
			"{\"Response\":\"True\",\"Title\":\"Six\tknives\",\"Plot\":\"He is [the \\\"Butcher\\\"] to locals.\"}",
			`He is [the "Butcher"] to locals.`},
		{"an escaped quote before a brace",
			"{\"Response\":\"True\",\"Title\":\"Six\tknives\",\"Plot\":\"Say \\\"}\\\" twice.\"}",
			`Say "}" twice.`},
		{"UTF-8 beside a repair",
			"{\"Response\":\"True\",\"Plot\":\"Niño\\é\tcafé\x1f🎬\\ñ\"}",
			`Niño\é café🎬\ñ`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if json.Valid([]byte(c.body)) {
				t.Fatal("the body is valid JSON; the test proves nothing about the repair")
			}
			if got := plotOf(t, c.body); got != c.plot {
				t.Errorf("plot = %q, want %q", got, c.plot)
			}
		})
	}
}

// TestAStrayBackslashDoesNotSwallowTheNextField is a field whose last
// character is a backslash. Read strictly, it escapes the quote that
// closes the field, and every field after it misreads.
func TestAStrayBackslashDoesNotSwallowTheNextField(t *testing.T) {
	for _, body := range []string{
		`{"Response":"True","Director":"Ann Lee \","Writer":"Ann Lee \, Bo Park","Plot":"Two cooks argue.","Poster":"N/A"}`,
		// White space before the comma is still the field's end.
		`{"Response":"True","Director":"Ann Lee \" ,"Writer":"Ann Lee \, Bo Park","Plot":"Two cooks argue.","Poster":"N/A"}`,
	} {
		var got struct{ Director, Writer, Plot, Poster string }
		if err := decode([]byte(body), &got); err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if got.Director != `Ann Lee \` || got.Writer != `Ann Lee \, Bo Park` || got.Plot != "Two cooks argue." || got.Poster != "N/A" {
			t.Errorf("%q read as %+v", body, got)
		}
		if plot := plotOf(t, body); plot != "Two cooks argue." {
			t.Errorf("plot = %q", plot)
		}
	}

	// And at the end of an array.
	var got struct {
		Tags []string
		Plot string
	}
	if err := decode([]byte(`{"Tags":["one\"],"Plot":"P."}`), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 1 || got.Tags[0] != `one\` || got.Plot != "P." {
		t.Errorf("read as %+v", got)
	}
}

// TestValidJSONTakesTheFastPath: a body that decodes strictly is never
// repaired. This one holds exactly what the repair would misread, an
// escaped quote just before two braces, which is how a stray backslash
// at the end of a nested object looks, so reading it any other way
// shows.
func TestValidJSONTakesTheFastPath(t *testing.T) {
	body := []byte(`{"Response":"True","Plot":"Say \"}}\" twice."}`)
	if bytes.Equal(repair(body), body) {
		t.Fatal("the repair leaves this body alone; the test proves nothing")
	}
	var strict, got struct{ Response, Plot string }
	if err := json.Unmarshal(body, &strict); err != nil {
		t.Fatal(err)
	}
	if err := decode(body, &got); err != nil {
		t.Fatal(err)
	}
	if got != strict || got.Plot != `Say "}}" twice.` {
		t.Errorf("decoded %+v, strictly %+v", got, strict)
	}
}

func TestAHopelessAnswerIsErrUnreadable(t *testing.T) {
	for _, body := range []string{
		`{"Response":"True","Plot":"cut off`,
		`{"Response":"True" "Plot":"no comma"}`,
		`{"Response":"True","Plot":"one brace too many"}}`,
		// JSON, but not of the shape asked for. It will be the same
		// next time too.
		`{"Response":"True","Plot":42}`,
	} {
		_, err := parseTitle([]byte(body), "tt0000001")
		if !errors.Is(err, ErrUnreadable) {
			t.Errorf("%q gave %v, want ErrUnreadable", body, err)
			continue
		}
		// The log says why, not only that.
		if err.Error() == ErrUnreadable.Error() {
			t.Errorf("%q: the error %q does not carry the decode error", body, err)
		}
	}
}

// TestABodyThatIsNotAnObjectIsStillAFailure: nothing shaped like OMDb's
// answer came back, so there is nothing to record, and the lookup is
// worth trying again.
func TestABodyThatIsNotAnObjectIsStillAFailure(t *testing.T) {
	for _, body := range []string{"", "   ", "<html><body>Bad gateway</body></html>"} {
		_, err := parseTitle([]byte(body), "tt0000001")
		if err == nil || errors.Is(err, ErrUnreadable) || errors.Is(err, ErrNotFound) {
			t.Errorf("%q gave %v, want a plain error the caller will retry", body, err)
		}
	}
}

// TestAnErrorAnswerStillSaysWhatItSays: OMDb's refusals map to the
// same errors as before, repaired or not.
func TestAnErrorAnswerStillSaysWhatItSays(t *testing.T) {
	for _, c := range []struct {
		body string
		want error
	}{
		{`{"Response":"False","Error":"Request limit reached!"}`, ErrQuota},
		{"{\"Response\":\"False\",\"Error\":\"Request limit reached!\t\"}", ErrQuota},
		{`{"Response":"False","Error":"Invalid API key!"}`, ErrKey},
		{`{"Response":"False","Error":"Invalid API key!","Note":"see\here"}`, ErrKey},
		{`{"Response":"False","Error":"Movie not found!"}`, ErrNotFound},
		{`{"Response":"False","Error":"Movie not found!\'"}`, ErrNotFound},
		{`{"Response":"False","Error":"Error getting data.\"}`, ErrNotFound},
	} {
		if _, err := parseTitle([]byte(c.body), "tt0000001"); !errors.Is(err, c.want) {
			t.Errorf("title %q gave %v, want %v", c.body, err, c.want)
		}
		if _, err := parse([]byte(c.body), "tt0000001"); !errors.Is(err, c.want) {
			t.Errorf("rating %q gave %v, want %v", c.body, err, c.want)
		}
		if c.want == ErrKey {
			continue // search does not tell a refused key apart
		}
		if _, err := parseSearch([]byte(c.body)); !errors.Is(err, c.want) {
			t.Errorf("search %q gave %v, want %v", c.body, err, c.want)
		}
	}
}

// TestAPlotKeepsNoControlCharacters: valid escapes carry control
// characters past the decoder, and past the repair, which never sees a
// valid body. The plot is cleaned after decoding either way.
func TestAPlotKeepsNoControlCharacters(t *testing.T) {
	body := `{"Response":"True","Plot":"\tFirst line.\nSecond\u0007 line,\u001f\r\nthird\u0085.  "}`
	if !json.Valid([]byte(body)) {
		t.Fatal("the body should be valid JSON")
	}
	if got, want := plotOf(t, body), "First line. Second line,  third."; got != want {
		t.Errorf("plot = %q, want %q", got, want)
	}
	for raw, want := range map[string]string{
		"\x01N/A\x02": "",
		"\u0085":      "",
		"A\x7fB":      "AB",
		"é\tñ":        "é ñ",
	} {
		if got := ParsePlot(raw); got != want {
			t.Errorf("ParsePlot(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestTheClientReadsARepairedAnswer(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("s") != "" {
			w.Write([]byte("{\"Response\":\"True\",\"Search\":[{\"Title\":\"The\tLong\\Road\",\"Year\":\"1999\",\"imdbID\":\"tt0000001\",\"Type\":\"movie\",\"Poster\":\"N/A\"}]}"))
			return
		}
		w.Write([]byte(`{"Response":"True","imdbRating":"7.5","imdbVotes":"1,234","Poster":"https://x/p.jpg","Released":"02 Jan 2001","Plot":"Two\sisters walk."}`))
	})
	title, err := c.Lookup(context.Background(), "tt0000001")
	if err != nil {
		t.Fatal(err)
	}
	if title.Plot != `Two\sisters walk.` || title.Poster != "https://x/p.jpg" || title.Released.IsZero() {
		t.Errorf("title = %+v", title)
	}
	rating, err := c.IMDbRating(context.Background(), "tt0000001")
	if err != nil || rating != (Rating{Value: 7.5, Votes: 1234}) {
		t.Errorf("rating = %+v, %v", rating, err)
	}
	hits, err := c.Search(context.Background(), "long road")
	if err != nil || len(hits) != 1 || hits[0].Title != `The Long\Road` {
		t.Errorf("hits = %+v, %v", hits, err)
	}
}

// TestTheClientTreatsAnUnreadableAnswerAsAnAnswer: the rating is cached
// like OMDb's "not found", so it is not asked again, and a search that
// cannot be read is no hits, as one with nothing in it is.
func TestTheClientTreatsAnUnreadableAnswerAsAnAnswer(t *testing.T) {
	var hits atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"Response":"True","imdbRating":"7.5","Plot":"cut off`))
	}, WithCache(newMemCache()))
	if _, err := c.Lookup(context.Background(), "tt0000001"); !errors.Is(err, ErrUnreadable) {
		t.Errorf("Lookup gave %v, want ErrUnreadable", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.IMDbRating(context.Background(), "tt0000001"); !errors.Is(err, ErrUnreadable) {
			t.Errorf("IMDbRating gave %v, want ErrUnreadable", err)
		}
	}
	if hits.Load() != 2 {
		t.Errorf("OMDb was asked %d times, want 2: the rating's answer should be cached", hits.Load())
	}
	found, err := c.Search(context.Background(), "anything")
	if err != nil || len(found) != 0 {
		t.Errorf("search = %v, %v, want no hits and no error", found, err)
	}
}

// FuzzRepair: whatever OMDb sends, the repair does not panic and always
// gives something back; valid JSON is read exactly as a strict decoder
// reads it; and a field holding any bytes but a quote is readable once
// repaired.
func FuzzRepair(f *testing.F) {
	for _, seed := range []string{
		``,
		`{}`,
		`{"Plot":"a\Sb"}`,
		`{"Plot":"it\'s"}`,
		`{"Director":"x \","Writer":"y \, z"}`,
		`{"Plot":"x \"}`,
		"{\"Plot\":\"a\tb\x1fc\x1bd\"}",
		`{"Plot":"é \u12"}`,
		`{"Plot":"ñ\é🎬"}`,
		`{"Plot":"He said \"hi\" to her"}`,
		`"\`,
		`[1,"\x"]`,
		`{"n":1e400}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		out := repair(body)
		if len(body) > 0 && len(out) == 0 {
			t.Fatalf("repair(%q) gave nothing back", body)
		}
		if utf8.Valid(body) && !utf8.Valid(out) {
			t.Fatalf("repair(%q) = %q split a character", body, out)
		}

		// Valid JSON can still fail a strict decode, with a number too
		// big for a float; then the helper fails too, and otherwise it
		// reads exactly what the strict decoder reads.
		var got, strict any
		err := decode(body, &got)
		if json.Valid(body) {
			serr := json.Unmarshal(body, &strict)
			if (err == nil) != (serr == nil) || !reflect.DeepEqual(got, strict) {
				t.Fatalf("valid JSON %q read as %#v (%v), strictly %#v (%v)", body, got, err, strict, serr)
			}
		}
		_, _ = parseTitle(body, "tt0000001")

		field := strings.ReplaceAll(string(body), `"`, "")
		wrapped := []byte(`{"Response":"True","Plot":"` + field + `"}`)
		var title struct{ Response, Plot string }
		if err := decode(wrapped, &title); err != nil || title.Response != "True" {
			t.Fatalf("%q was not read once repaired: %v (%+v)", wrapped, err, title)
		}
	})
}
