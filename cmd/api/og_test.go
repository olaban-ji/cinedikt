package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"cinedikt/internal/catalog"
)

// fakeOGStore is a catalog with one movie in it and a table in memory.
type fakeOGStore struct {
	title  string
	year   int
	poster string
	err    error

	mu   sync.Mutex
	rows map[string][]byte
	puts int
}

func (f *fakeOGStore) MovieMeta(context.Context, string) (string, int, string, error) {
	if f.err != nil {
		return "", 0, "", f.err
	}
	return f.title, f.year, f.poster, nil
}

func (f *fakeOGStore) OGImage(_ context.Context, tconst, v string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[tconst+"-"+v], nil
}

func (f *fakeOGStore) PutOGImage(_ context.Context, tconst, v string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = map[string][]byte{}
	}
	f.rows[tconst+"-"+v] = body
	f.puts++
	return nil
}

// posterServer stands in for the image host.
func posterServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		// A portrait picture, so the cover crop has something to do.
		art := image.NewRGBA(image.Rect(0, 0, 400, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 400; x++ {
				art.Set(x, y, color.RGBA{R: uint8(x % 256), G: 0x40, B: 0x80, A: 0xff})
			}
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, art, nil)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ogServerFor(t *testing.T, store ogStore) *httptest.Server {
	t.Helper()
	cards, err := newOGServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(cards)
	t.Cleanup(srv.Close)
	return srv
}

// get fetches without following the redirect, so a fallback can be seen.
func get(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestShareCardIsTheRightSizeAndCarriesTheRing(t *testing.T) {
	art := posterServer(t, http.StatusOK)
	store := &fakeOGStore{title: "The Matrix", year: 1999, poster: art.URL + "/poster.jpg"}
	srv := ogServerFor(t, store)

	resp := get(t, srv, "/og/movie/tt0133093.png")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != ogCache {
		t.Errorf("Cache-Control = %q, want %q", got, ogCache)
	}
	want := `"tt0133093-` + catalog.OGVersion(store.poster, store.title) + `"`
	if got := resp.Header.Get("ETag"); got != want {
		t.Errorf("ETag = %q, want %q", got, want)
	}

	img := decodePNG(t, resp.Body)
	if b := img.Bounds(); b.Dx() != ogW || b.Dy() != ogH {
		t.Fatalf("card is %dx%d, want %dx%d", b.Dx(), b.Dy(), ogW, ogH)
	}
	// The accent ring runs down the left of the poster, and the panel
	// behind the words is the app's own ground.
	near(t, img, 62, 300, ogAccent, 8, "the ring")
	near(t, img, 1100, 20, ogGround, 4, "the ground")
}

func TestShareCardIsDrawnOnceAndThenRead(t *testing.T) {
	art := posterServer(t, http.StatusOK)
	store := &fakeOGStore{title: "The Matrix", year: 1999, poster: art.URL + "/poster.jpg"}
	srv := ogServerFor(t, store)

	first, _ := io.ReadAll(get(t, srv, "/og/movie/tt0133093.png").Body)
	second, _ := io.ReadAll(get(t, srv, "/og/movie/tt0133093.png").Body)
	if !bytes.Equal(first, second) {
		t.Error("the second request drew a different card")
	}
	if store.puts != 1 {
		t.Errorf("stored %d times, want 1", store.puts)
	}
}

func TestShareCardWithoutItsPosterIsNotKept(t *testing.T) {
	art := posterServer(t, http.StatusInternalServerError)
	store := &fakeOGStore{title: "The Matrix", year: 1999, poster: art.URL + "/poster.jpg"}
	srv := ogServerFor(t, store)

	resp := get(t, srv, "/og/movie/tt0133093.png")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != ogRetry {
		t.Errorf("Cache-Control = %q, want %q — the next request has to try again", got, ogRetry)
	}
	if store.puts != 0 {
		t.Error("a card drawn without its poster was stored")
	}
	img := decodePNG(t, resp.Body)
	// Still a whole card, ring and all.
	if b := img.Bounds(); b.Dx() != ogW || b.Dy() != ogH {
		t.Fatalf("card is %dx%d", b.Dx(), b.Dy())
	}
	near(t, img, 62, 300, ogAccent, 8, "the ring")
	// And the poster's place is the film's own gradient, not the
	// ground: the pixel Chrome draws there for The Matrix's fallback.
	if at(img, 200, 300) == ogGround {
		t.Error("the poster's place was left empty")
	}
	near(t, img, 200, 300, color.NRGBA{0x5c, 0x32, 0x30, 0xff}, 1, "the fallback gradient")
}

func TestShareCardFallsBackToTheSiteCard(t *testing.T) {
	for _, c := range []struct {
		name, path string
		store      ogStore
	}{
		{"an unknown movie", "/og/movie/tt9999999.png", &fakeOGStore{err: errors.New("not found")}},
		{"a movie with no title", "/og/movie/tt9999999.png", &fakeOGStore{title: "  "}},
		{"not a title id", "/og/movie/603.png", &fakeOGStore{title: "The Matrix"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp := get(t, ogServerFor(t, c.store), c.path)
			if resp.StatusCode != http.StatusFound {
				t.Fatalf("status = %d, want 302", resp.StatusCode)
			}
			if got := resp.Header.Get("Location"); got != ogGeneric {
				t.Errorf("Location = %q, want %q", got, ogGeneric)
			}
		})
	}
}

func TestShareCardAnswersHeadAndRefusesPost(t *testing.T) {
	art := posterServer(t, http.StatusOK)
	srv := ogServerFor(t, &fakeOGStore{title: "The Matrix", year: 1999, poster: art.URL + "/p.jpg"})

	resp, err := http.Head(srv.URL + "/og/movie/tt0133093.png")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("HEAD = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	resp, err = http.Post(srv.URL+"/og/movie/tt0133093.png", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", resp.StatusCode)
	}
}

// facesFor is a set of the card's faces, cut the way a render cuts its
// own.
func facesFor(t *testing.T) *ogFaces {
	t.Helper()
	cards, err := newOGServer(&fakeOGStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	faces, err := cards.faces()
	if err != nil {
		t.Fatal(err)
	}
	return faces
}

func TestShareCardWrapsALongTitle(t *testing.T) {
	cards := facesFor(t)
	const long = "Dr. Strangelove or: How I Learned to Stop Worrying and Love the Bomb"
	face, lead, lines := cards.wrapTitle(long)
	if len(lines) <= titleMaxLines {
		t.Fatalf("a title this long fitted in %d lines; the fixture is no longer long", len(lines))
	}
	if lead != titleSmallLead {
		t.Errorf("leading = %d, want the smaller size's %d", lead, titleSmallLead)
	}
	if len(lines) > titleHardMax {
		t.Errorf("%d lines, want at most %d", len(lines), titleHardMax)
	}
	// Four lines is the most a title gets, and Strangelove fits in
	// them, so it is shown whole.
	if strings.HasSuffix(lines[len(lines)-1], "…") {
		t.Errorf("a title that fitted was cut anyway: %q", lines[len(lines)-1])
	}
	if got := strings.Join(lines, " "); got != long {
		t.Errorf("the title came back as %q", got)
	}
	for _, line := range lines {
		if w := textWidth(face.face, line); w > textW {
			t.Errorf("%q is %dpx wide, past the %dpx box", line, w, textW)
		}
	}

	// Past four lines it is cut, and says so.
	_, _, cut := cards.wrapTitle(long + " " + long)
	if len(cut) != titleHardMax {
		t.Fatalf("a title twice that long came out as %d lines", len(cut))
	}
	if !strings.HasSuffix(cut[len(cut)-1], "…") {
		t.Errorf("a cut title did not say so: %q", cut[len(cut)-1])
	}

	// A short title stays at the large size, on one line.
	_, lead, lines = cards.wrapTitle("Heat")
	if lead != titleLead || len(lines) != 1 {
		t.Errorf("Heat came out as %d lines at leading %d", len(lines), lead)
	}
}

func TestShareCardBreaksAWordNothingCouldFit(t *testing.T) {
	cards := facesFor(t)
	lines := wrapText(cards.title.face, strings.Repeat("M", 60), textW)
	if len(lines) < 2 {
		t.Fatal("a word wider than the card was left to run off the side")
	}
	for _, line := range lines {
		if w := textWidth(cards.title.face, line); w > textW {
			t.Errorf("%q is %dpx wide, past the %dpx box", line, w, textW)
		}
	}
}

func TestShareCardDrawsLettersYoungSerifLacks(t *testing.T) {
	cards := facesFor(t)
	serif, err := opentype.Parse(youngSerifTTF)
	if err != nil {
		t.Fatal(err)
	}
	// A private-use letter no font here has: what a box looks like.
	const none = '\ue000'
	for _, f := range []struct {
		name string
		face *ogFace
		size float64
	}{
		{"title", cards.title, titleSize},
		{"small title", cards.titleSmall, titleSmall},
		{"first letter", cards.letter, letterSize},
	} {
		plain, err := sizedFace(serif, f.size)
		if err != nil {
			t.Fatal(err)
		}
		box := drawRune(f.face.face, none)
		// Young Serif has none of these, so each is a box without the
		// stand-in. Ơ and Ư also matter as the first letter.
		for _, r := range "ơưƠƯợốếắữẢ" {
			if hasGlyph(serif, r) {
				t.Fatalf("Young Serif has %q now; pick a letter it lacks", r)
			}
			if bytes.Equal(drawRune(f.face.face, r), box) {
				t.Errorf("%s: %q is drawn as a box", f.name, r)
			}
		}
		// A letter Young Serif has is still its own, and the layout is
		// still measured by it.
		for _, r := range "Maêô" {
			if !bytes.Equal(drawRune(f.face.face, r), drawRune(plain, r)) {
				t.Errorf("%s: %q is not drawn in Young Serif", f.name, r)
			}
		}
		if f.face.face.Metrics() != plain.Metrics() {
			t.Errorf("%s: the metrics are not Young Serif's", f.name)
		}
	}
}

// drawRune is one letter drawn alone, as the bytes of its picture.
func drawRune(face font.Face, r rune) []byte {
	dst := image.NewRGBA(image.Rect(0, 0, 240, 240))
	d := font.Drawer{Dst: dst, Src: image.White, Face: face, Dot: fixed.P(20, 190)}
	d.DrawString(string(r))
	return dst.Pix
}

func TestShareCardYearSitsWhereTheSampleHasIt(t *testing.T) {
	cards, err := newOGServer(&fakeOGStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := cards.render("The Matrix", 1999, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := decodePNG(t, bytes.NewReader(body))
	// brand/og-movie-sample.png has 1999's ink on rows 291–312: the
	// sample's CSS puts the baseline 16 px under the 72 px title line
	// box, plus Figtree's own place in a 30 px line.
	top, bottom := -1, -1
	for y := 276; y < 360; y++ {
		for x := textX; x < textX+120; x++ {
			if at(img, x, y).R > 0x60 {
				if top < 0 {
					top = y
				}
				bottom = y
				break
			}
		}
	}
	if top < 0 {
		t.Fatal("no year drawn under the title")
	}
	if math.Abs(float64(top-291)) > 1 || math.Abs(float64(bottom-312)) > 1 {
		t.Errorf("year ink on rows %d–%d, want the sample's 291–312", top, bottom)
	}
}

func TestShareCardHueMatchesTheClient(t *testing.T) {
	// The numbers the client's own hueOf (web/src/poster.ts) gives for
	// these titles: all nine were computed with hueOf in node, and
	// web/src/poster.test.ts pins most of the same ones. The accented,
	// CJK and emoji titles are here because the two hash UTF-16 code
	// units, and an emoji is two of them.
	for _, c := range []struct {
		title string
		want  int
	}{
		{"The Matrix", 24},
		{"Heat", 176},
		{"Memento", 289},
		{"Amélie", 331},
		{"千と千尋の神隠し", 156},
		{"🎬 Cut", 322},
		{"😀 Emoji", 353},
		{"", 0},
		{"Dr. Strangelove or: How I Learned to Stop Worrying and Love the Bomb", 175},
	} {
		if got := ogHue(c.title); got != c.want {
			t.Errorf("ogHue(%q) = %d, want %d, as the client has it", c.title, got, c.want)
		}
	}
}

// oklch is one CSS oklch(L C h) colour as 8-bit sRGB, the oracle the
// card's colours are checked against.
func oklch(l, c, hue float64) color.NRGBA {
	return okLabToNRGBA(okLCh(l, c, hue))
}

func TestOKLCHMatchesTheBrowser(t *testing.T) {
	// Each colour as Chrome paints it: a canvas filled with the oklch()
	// value, read back. The browser rounds a little differently, so
	// one step in 255 is allowed.
	for _, c := range []struct {
		l, c, h float64
		want    color.NRGBA
	}{
		{0.45, 0.07, 24, color.NRGBA{0x77, 0x45, 0x42, 0xff}},
		{0.28, 0.05, 24, color.NRGBA{0x3e, 0x1e, 0x1d, 0xff}},
		{0.45, 0.07, 176, color.NRGBA{0x20, 0x62, 0x54, 0xff}},
		{0.28, 0.05, 176, color.NRGBA{0x03, 0x31, 0x28, 0xff}},
		{0.45, 0.07, 289, color.NRGBA{0x54, 0x4f, 0x7a, 0xff}},
		{0.28, 0.05, 289, color.NRGBA{0x28, 0x24, 0x40, 0xff}},
	} {
		got := oklch(c.l, c.c, c.h)
		if !within(got, c.want, 1) {
			t.Errorf("oklch(%v %v %v) = %v, want about %v", c.l, c.c, c.h, got, c.want)
		}
	}

	// And the card's hex colours are the dark theme's tokens, so the
	// two cannot drift apart. These are exact: this conversion lands on
	// the README's hex table to the digit.
	for _, c := range []struct {
		name    string
		l, c, h float64
		want    color.NRGBA
	}{
		{"ground (--g)", 0.175, 0.008, 60, ogGround},
		{"ink (--t)", 0.965, 0.008, 80, ogInk},
		{"soft (--t2)", 0.78, 0.012, 70, ogSoft},
		{"quiet (--t3)", 0.63, 0.012, 70, ogQuiet},
		{"accent (--acc)", 0.74, 0.15, 295, ogAccent},
	} {
		if got := oklch(c.l, c.c, c.h); got != c.want {
			t.Errorf("%s: oklch(%v %v %v) = %v, but the card uses %v", c.name, c.l, c.c, c.h, got, c.want)
		}
	}
	for name, c := range map[string]color.NRGBA{"ogRule": ogRule, "ogBand": ogBand, "ogFade": ogFade} {
		if c.R != ogInk.R || c.G != ogInk.G || c.B != ogInk.B {
			t.Errorf("%s is %v; the texture and the letter are ink", name, c)
		}
	}
}

func TestGradientLineFollowsCSSAngles(t *testing.T) {
	const w, h = 200, 100
	near := func(what string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: t = %v, want %v", what, got, want)
		}
	}
	// 180deg is "to bottom" and 90deg "to right": the line is the
	// box's own height or width, sampled at pixel centres.
	down := cssGradientLine(w, h, 180)
	near("to bottom, first row", down(7, 0), 0.5/h)
	near("to bottom, last row", down(7, h-1), 1-0.5/h)
	right := cssGradientLine(w, h, 90)
	near("to right, first column", right(0, 42), 0.5/w)
	near("to right, last column", right(w-1, 42), 1-0.5/w)
	// 0deg runs upwards.
	near("to top, first row", cssGradientLine(w, h, 0)(7, 0), 1-0.5/h)

	// 165deg runs from the top-left corner to the bottom-right, and the
	// line is just long enough that those two corners are its ends.
	at := cssGradientLine(posterW, posterH, 165)
	if tl, br := at(0, 0), at(posterW-1, posterH-1); tl > 0.002 || br < 0.998 {
		t.Errorf("165deg: the corners are at t = %v and %v, want the two ends", tl, br)
	}
	// A step along the line's own perpendicular leaves t where it was.
	// That runs at 15° below the horizontal, so 56 across is 15.005
	// down, near enough to 15 that t moves by less than 1e-4.
	if a, b := at(100, 100), at(44, 115); math.Abs(a-b) > 1e-4 {
		t.Errorf("165deg: points on one isoline are at t = %v and %v", a, b)
	}
}

func TestPosterFallbackMatchesTheBrowser(t *testing.T) {
	// Pixels of linear-gradient(165deg, oklch(0.45 0.07 24),
	// oklch(0.28 0.05 24)) — The Matrix's fallback — as Chrome renders
	// it in a 345×518 box. Over the whole box the browser and this
	// arithmetic agree within one step in 255; these are the corners,
	// the middle, and a few between.
	fill := ogPosterFallback("The Matrix", posterW, posterH)
	for _, c := range []struct {
		x, y int
		want color.NRGBA
	}{
		{0, 0, color.NRGBA{0x77, 0x45, 0x42, 0xff}},
		{posterW - 1, 0, color.NRGBA{0x6e, 0x3e, 0x3c, 0xff}},
		{0, posterH - 1, color.NRGBA{0x47, 0x24, 0x22, 0xff}},
		{posterW - 1, posterH - 1, color.NRGBA{0x3e, 0x1e, 0x1d, 0xff}},
		{172, 259, color.NRGBA{0x5a, 0x31, 0x2f, 0xff}},
		{100, 50, color.NRGBA{0x70, 0x40, 0x3d, 0xff}},
		{300, 400, color.NRGBA{0x49, 0x26, 0x24, 0xff}},
		{50, 450, color.NRGBA{0x4b, 0x27, 0x25, 0xff}},
	} {
		if got := at(fill, c.x, c.y); !within(got, c.want, 1) {
			t.Errorf("fallback at (%d,%d) = %v, want about %v", c.x, c.y, got, c.want)
		}
	}
	if got := at(fill, 10, 10); got.A != 0xff {
		t.Errorf("the fallback is translucent: %v", got)
	}
	if again := ogPosterFallback("The Matrix", posterW, posterH); !bytes.Equal(again.Pix, fill.Pix) {
		t.Error("the fallback is not stable for one title")
	}
	if other := ogPosterFallback("Heat", posterW, posterH); at(other, 0, 0) == at(fill, 0, 0) {
		t.Error("two titles came out the same colour")
	}
}

func TestPosterIsAskedForAtTheWidthItIsDrawn(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{
			"https://m.media-amazon.com/images/M/abc.jpg",
			"https://m.media-amazon.com/images/M/abc._SX780_.jpg",
		},
		{
			"https://m.media-amazon.com/images/M/abc._V1_SX300.jpg",
			"https://m.media-amazon.com/images/M/abc._SX780_.jpg",
		},
		// TMDb names the width in the path instead. The fallback
		// stores these for films OMDb had no picture for.
		{
			"https://image.tmdb.org/t/p/w342/abc.jpg",
			"https://image.tmdb.org/t/p/w780/abc.jpg",
		},
		// An image host this does not know is fetched as it stands.
		{"https://example.com/p.jpg", "https://example.com/p.jpg"},
	} {
		if got := ogPosterURL(c.in); got != c.want {
			t.Errorf("ogPosterURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMovieRouteCarriesThePerMovieCard(t *testing.T) {
	// The page a scraper actually reads, with the image URL on it.
	dir := indexFixture(t)
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(routes(api, dir, theMatrix, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)

	head := fetchHead(t, srv, "/movie/tt0133093-the-matrix")
	want := "https://cinedikt.com/og/movie/tt0133093.png?v=" +
		catalog.OGVersion(matrixPoster, "The Matrix")
	if !strings.Contains(head, want) {
		t.Errorf("the head does not carry %s", want)
	}
}

// goldenPath is where a reference card is kept.
func goldenPath(name string) string { return filepath.Join("testdata", name+".png") }

// TestShareCardMatchesItsGolden compares against a stored card, within
// a tolerance: font rasterising differs a little between machines, so
// an exact match would fail on somebody else's laptop and tell them
// nothing. Run with UPDATE_GOLDEN=1 to redraw the references.
func TestShareCardMatchesItsGolden(t *testing.T) {
	art := posterServer(t, http.StatusOK)
	cards, err := newOGServer(&fakeOGStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	poster, ok := cards.fetchPoster(context.Background(), art.URL+"/p.jpg")
	if !ok {
		t.Fatal("the fixture poster did not load")
	}

	for _, c := range []struct {
		name  string
		title string
		year  int
		art   image.Image
	}{
		{"matrix", "The Matrix", 1999, poster},
		{"no-poster", "The Matrix", 1999, nil},
		{"long-title", "Dr. Strangelove or: How I Learned to Stop Worrying and Love the Bomb", 1964, poster},
	} {
		t.Run(c.name, func(t *testing.T) {
			body, err := cards.render(c.title, c.year, c.art)
			if err != nil {
				t.Fatal(err)
			}
			path := goldenPath(c.name)
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Skip("golden rewritten")
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("no golden yet (%v); run with UPDATE_GOLDEN=1", err)
			}
			compare(t, decodePNG(t, bytes.NewReader(body)), decodePNG(t, bytes.NewReader(want)))
		})
	}
}

// TestShareCardsDrawnAtOnceMatchOneDrawnAlone draws cards at the same
// moment, the way ServeHTTP lets ogRenders of them run, and holds each
// to the same card drawn on its own. None has a poster, so every one
// sets the first letter as well as the title, and the Vietnamese ones
// go through the stand-in, which puts every face the card has to work.
// If two renders share a face's buffers, -race says so, and without it
// the pictures stop matching, since nothing else here varies.
func TestShareCardsDrawnAtOnceMatchOneDrawnAlone(t *testing.T) {
	cards, err := newOGServer(&fakeOGStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	films := []struct {
		title string
		year  int
	}{
		{"The Matrix", 1999},
		{"Bố Già", 2021},
		{"Hai Phượng", 2019},
		{"Dr. Strangelove or: How I Learned to Stop Worrying and Love the Bomb", 1964},
	}
	alone := make([][]byte, len(films))
	for i, f := range films {
		if alone[i], err = cards.render(f.title, f.year, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Each film twice over, so the same face size is always in use by
	// more than one render.
	const rounds = 2
	got := make([][]byte, rounds*len(films))
	errs := make([]error, len(got))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range got {
		f := films[i%len(films)]
		wg.Go(func() {
			<-start
			got[i], errs[i] = cards.render(f.title, f.year, nil)
		})
	}
	close(start)
	wg.Wait()

	for i, body := range got {
		f := films[i%len(films)]
		if errs[i] != nil {
			t.Errorf("%s: %v", f.title, errs[i])
			continue
		}
		if !bytes.Equal(body, alone[i%len(films)]) {
			t.Errorf("%s: drawn alongside other cards, it is not the card drawn alone", f.title)
		}
	}
}

// compare fails if two cards differ by more than a rasteriser would.
func compare(t *testing.T, got, want image.Image) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds %v, want %v", got.Bounds(), want.Bounds())
	}
	const tolerance = 2
	off := 0
	for y := want.Bounds().Min.Y; y < want.Bounds().Max.Y; y++ {
		for x := want.Bounds().Min.X; x < want.Bounds().Max.X; x++ {
			a, b := at(got, x, y), at(want, x, y)
			if absDiff(a.R, b.R) > tolerance || absDiff(a.G, b.G) > tolerance ||
				absDiff(a.B, b.B) > tolerance {
				off++
			}
		}
	}
	// A handful of pixels is hinting; a field of them is the layout.
	if limit := want.Bounds().Dx() * want.Bounds().Dy() / 200; off > limit {
		t.Errorf("%d pixels differ by more than %d, past the %d allowed", off, tolerance, limit)
	}
}

func decodePNG(t *testing.T, r io.Reader) image.Image {
	t.Helper()
	img, err := png.Decode(r)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func at(img image.Image, x, y int) color.NRGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func near(t *testing.T, img image.Image, x, y int, want color.NRGBA, tolerance uint8, what string) {
	t.Helper()
	got := at(img, x, y)
	if absDiff(got.R, want.R) > tolerance || absDiff(got.G, want.G) > tolerance ||
		absDiff(got.B, want.B) > tolerance {
		t.Errorf("%s at (%d,%d) is %+v, want about %+v", what, x, y, got, want)
	}
}

// within reports whether two colours are no more than this far apart
// on every channel.
func within(got, want color.NRGBA, tolerance uint8) bool {
	return absDiff(got.R, want.R) <= tolerance && absDiff(got.G, want.G) <= tolerance &&
		absDiff(got.B, want.B) <= tolerance && absDiff(got.A, want.A) <= tolerance
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
