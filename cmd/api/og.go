package main

// The per-movie share card: a 1200×630 PNG of the movie's poster beside
// its title, drawn in Go inside this binary.
//
// Rendered here rather than in a headless browser because a browser is
// a second deployment, a second thing to keep alive, and half a gigabyte
// of memory to draw a rectangle and some words. What this needs is a
// poster, two fonts and a rounded corner.
//
// It is dark in both themes. The card is the brand surface: it appears
// in somebody else's chat window, where the app's own theme means
// nothing and a white rectangle is just a white rectangle.

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"cinedikt/internal/catalog"
	"cinedikt/internal/imdbid"
)

// The fonts the app itself is set in, so a card looks like the page it
// links to: Young Serif for the title, the letter and the wordmark, and
// Figtree for the rest. They are OFL, like the stand-in below, and each
// licence sits beside its font in assets/.
//
// Figtree is a static Medium, not the variable font the page loads:
// x/image's sfnt cannot pick an instance out of a variable font, and
// would draw its default weight instead of 500.
//
//go:embed assets/YoungSerif-Regular.ttf
var youngSerifTTF []byte

//go:embed assets/Figtree-Medium.ttf
var figtreeTTF []byte

// Young Serif has no Vietnamese past ă â đ ê ô: no ơ or ư, and none of
// the letters that stack a tone mark on another mark (ố, ợ, ữ). Fraunces,
// the face the card was set in before, has them all, so it stands in for
// each letter Young Serif lacks rather than letting a title such as
// "Bố Già" draw boxes. The page gets the same from the browser's own
// font fallback.
//
//go:embed assets/Fraunces-SemiBold.ttf
var frauncesTTF []byte

// The mark alone, pre-rendered at 2× rather than rasterised from SVG on
// every card. It is one 56×86 picture and it never changes.
//
//go:embed assets/mark-2x.png
var markPNG []byte

// The card, in numbers. Every one of these is from the spec's layout
// and none of them is derived from another, so a change to one is a
// change to one thing.
const (
	ogW, ogH = 1200, 630

	posterX, posterY = 64, 56
	posterW, posterH = 345, 518
	posterRadius     = 14

	ringX, ringY = 61, 53
	ringW, ringH = 351, 524
	ringRadius   = 17
	ringStroke   = 3

	shadowY    = 80
	shadowBlur = 24

	// The right-hand panel, which carries the map's own texture.
	panelX    = 460
	bandStep  = 72
	lineStep  = 96
	lineFirst = 520
	fadeTo    = 560

	markX, markY = 468, 64
	markW, markH = 28, 43

	textX     = 468
	textW     = 668
	titleTop  = 200
	titleSize = 68
	titleLead = 72
	// The size a title drops to when three lines will not hold it.
	titleSmall     = 56
	titleSmallLead = 60
	titleMaxLines  = 3
	titleHardMax   = 4

	yearSize   = 30
	yearGap    = 16
	lineSize   = 26
	lineBottom = 566

	letterSize = 160
)

// ogTagline is the one line of explanation the card carries.
const ogTagline = "Everything its cast and directors made"

// ogPosterWidth is what the card scales the poster down from. The slot
// is 345 wide; this is the next size up that both hosts serve.
const ogPosterWidth = 780

// The dark theme's tokens as hex, because nothing here draws oklch():
// ground is --g, ink --t, soft --t2, quiet --t3 and the accent --acc.
// A test converts the tokens and holds these to them.
var (
	ogGround = color.NRGBA{0x13, 0x10, 0x0d, 0xff}
	ogInk    = color.NRGBA{0xf6, 0xf3, 0xee, 0xff}
	ogAccent = color.NRGBA{0xb2, 0x96, 0xff, 0xff}
	ogSoft   = color.NRGBA{0xbc, 0xb6, 0xaf, 0xff}
	ogQuiet  = color.NRGBA{0x8e, 0x88, 0x81, 0xff}
	// The texture: the same two washes the map's bands are drawn in,
	// in ink rather than white, so they warm with the ground.
	ogRule = color.NRGBA{0xf6, 0xf3, 0xee, 13} // .05
	ogBand = color.NRGBA{0xf6, 0xf3, 0xee, 4}  // .015
	ogDrop = color.NRGBA{0, 0, 0, 128}         // .5
	ogFade = color.NRGBA{0xf6, 0xf3, 0xee, 89} // .35, the fallback letter
)

// How long the card may take, and how many may be drawn at once.
//
// The limit is not about CPU — one card is a few milliseconds of
// drawing — it is about the poster fetch. A link dropped into a busy
// channel arrives as a burst of identical requests, and four sockets to
// an image host is plenty for work that is thrown away the moment the
// first one stores its row.
const (
	ogRenders     = 4
	ogSlotWait    = 1 * time.Second
	ogBudget      = 5 * time.Second
	ogPosterFetch = 2 * time.Second
)

// How long a card may be cached. A stored card is immutable — its
// address carries the version — so it is kept for a year. One rendered
// without its poster is a card that should be tried again soon.
const (
	ogCache   = "public, max-age=31536000, immutable"
	ogRetry   = "public, max-age=300"
	ogGeneric = "/og.png?v=5"
)

// ogStore is what a card needs from the catalog: the movie, and
// somewhere to keep the picture.
type ogStore interface {
	MovieMeta(ctx context.Context, tconst string) (string, int, string, error)
	OGImage(ctx context.Context, tconst, v string) ([]byte, error)
	PutOGImage(ctx context.Context, tconst, v string, png []byte) error
}

// ogServer answers GET /og/movie/{tconst}.png.
type ogServer struct {
	store  ogStore
	client *http.Client
	logger *slog.Logger
	slots  chan struct{}
	// The fonts, parsed once. Parsing a font per request would be the
	// most expensive thing on this path by a wide margin. A parsed font
	// is only ever read, so every render shares these; the faces cut
	// from them are not, and each render cuts its own (see ogFaces).
	serif, sans, stand *opentype.Font
	mark               image.Image
}

// newOGServer builds the renderer, or reports why the assets it is
// made of cannot be read. It fails at startup rather than on the first
// shared link.
func newOGServer(store ogStore, logger *slog.Logger) (*ogServer, error) {
	serif, err := opentype.Parse(youngSerifTTF)
	if err != nil {
		return nil, fmt.Errorf("og: Young Serif: %w", err)
	}
	sans, err := opentype.Parse(figtreeTTF)
	if err != nil {
		return nil, fmt.Errorf("og: Figtree: %w", err)
	}
	stand, err := opentype.Parse(frauncesTTF)
	if err != nil {
		return nil, fmt.Errorf("og: Fraunces: %w", err)
	}
	mark, err := png.Decode(bytes.NewReader(markPNG))
	if err != nil {
		return nil, fmt.Errorf("og: mark: %w", err)
	}
	s := &ogServer{
		store:  store,
		logger: logger,
		slots:  make(chan struct{}, ogRenders),
		serif:  serif,
		sans:   sans,
		stand:  stand,
		mark:   mark,
		client: &http.Client{Timeout: ogPosterFetch},
	}
	// One set of faces now, and thrown away, so that a size a font will
	// not open at stops the process here too. After this, cutting a set
	// for a render is the same fonts at the same sizes.
	if _, err := s.faces(); err != nil {
		return nil, err
	}
	return s, nil
}

// ogFaces is the type one card is set in, and it belongs to that card
// alone. x/image's faces are not safe for concurrent use: each keeps
// the buffer it loads a glyph into and the mask it rasterises it onto,
// so two cards drawn at once with one set overwrite each other's
// letters mid-draw, or index past a mask the other has just resized.
//
// A fresh set per render rather than one set behind a lock, which would
// queue whole cards behind each other. Cutting a set takes microseconds,
// and the buffers it grows while drawing add a few percent to the memory
// a render already spends on its canvas, shadow and PNG, with no change
// in time that a benchmark can find.
type ogFaces struct {
	title, titleSmall, letter *ogFace
	wordmark                  *ogFace
	year, line                *ogFace
}

// faces cuts a set of faces from the fonts parsed at startup.
func (s *ogServer) faces() (*ogFaces, error) {
	var fs ogFaces
	// Only the faces that draw a title need the stand-in: the wordmark,
	// the year and the tagline never draw anything but their own ASCII.
	for _, f := range []struct {
		at         **ogFace
		from, back *opentype.Font
		size       float64
	}{
		{&fs.title, s.serif, s.stand, titleSize},
		{&fs.titleSmall, s.serif, s.stand, titleSmall},
		{&fs.letter, s.serif, s.stand, letterSize},
		{&fs.wordmark, s.serif, nil, 40},
		{&fs.year, s.sans, nil, yearSize},
		{&fs.line, s.sans, nil, lineSize},
	} {
		face, err := newOGFace(f.from, f.back, f.size)
		if err != nil {
			return nil, err
		}
		*f.at = face
	}
	return &fs, nil
}

// ogFace is a face and the one metric the layout asks it for: where
// its baseline sits in a line exactly as tall as its type, which is
// where the page's CSS puts it at line-height 1. That is half the
// leading, which is negative here, above the ascent, and snapped down
// to a whole pixel the way the browser drew the brand sample's year.
type ogFace struct {
	face  font.Face
	inBox int
}

// newOGFace is the font at this size, with back standing in for any
// letter the font has no glyph for. back may be nil.
func newOGFace(f, back *opentype.Font, size float64) (*ogFace, error) {
	face, err := sizedFace(f, size)
	if err != nil {
		return nil, err
	}
	m := face.Metrics()
	inBox := ((fixed.Int26_6(size*64) + m.Ascent - m.Descent) / 2).Floor()
	if back != nil {
		stand, err := sizedFace(back, size)
		if err != nil {
			return nil, err
		}
		face = &fallbackFace{Face: face, font: f, back: stand, backFont: back}
	}
	return &ogFace{face: face, inBox: inBox}, nil
}

func sizedFace(f *opentype.Font, size float64) (font.Face, error) {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("og: face at %vpx: %w", size, err)
	}
	return face, nil
}

// fallbackFace draws each letter in the first of two faces whose font
// has a glyph for it. A letter neither font has is left to the first,
// whose .notdef box it would have been anyway.
//
// The metrics are the first face's, so the card is measured by the face
// it is set in and a stand-in letter only lends its shape and advance.
// A pair of letters is kerned only when both come from the same face:
// a pair split across two fonts has no kerning table to ask.
type fallbackFace struct {
	font.Face
	font     *opentype.Font
	back     font.Face
	backFont *opentype.Font
}

func (f *fallbackFace) pick(r rune) font.Face {
	if hasGlyph(f.font, r) || !hasGlyph(f.backFont, r) {
		return f.Face
	}
	return f.back
}

// hasGlyph is whether the font draws this letter as itself rather than
// as glyph 0, the box a font draws for letters it does not have. The
// nil buffer has sfnt allocate its own, so nothing here is shared
// between two renders.
func hasGlyph(f *opentype.Font, r rune) bool {
	i, err := f.GlyphIndex(nil, r)
	return err == nil && i != 0
}

func (f *fallbackFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	return f.pick(r).Glyph(dot, r)
}

func (f *fallbackFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return f.pick(r).GlyphBounds(r)
}

func (f *fallbackFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	return f.pick(r).GlyphAdvance(r)
}

func (f *fallbackFace) Kern(r0, r1 rune) fixed.Int26_6 {
	a := f.pick(r0)
	if a != f.pick(r1) {
		return 0
	}
	return a.Kern(r0, r1)
}

func (f *fallbackFace) Close() error {
	return errors.Join(f.Face.Close(), f.back.Close())
}

func (s *ogServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tconst, ok := ogTConst(r.URL.Path)
	if !ok {
		ogFallback(w, r, ogCache)
		return
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), ogBudget)
	defer cancel()

	title, year, poster, err := s.store.MovieMeta(ctx, tconst)
	if err != nil || strings.TrimSpace(title) == "" {
		// Nothing to draw. The site's own card is the honest answer,
		// and it is one an unfurler can cache.
		ogFallback(w, r, ogCache)
		return
	}
	// The ?v in the address is ignored: it is a cache key for whoever
	// is holding the URL, not an instruction about what to draw. The
	// version is recomputed from what the catalog says right now, so a
	// stale link never pins an old picture in this table.
	v := catalog.OGVersion(poster, title)

	if have, err := s.store.OGImage(ctx, tconst, v); err == nil && have != nil {
		s.send(w, r, tconst, v, have, ogCache)
		s.logger.Info("og_render", "tconst", tconst, "ms", ms(started), "poster_ok", true, "cached", true)
		return
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-time.After(ogSlotWait):
		// Busy. Better the generic card now than a scraper's timeout,
		// and not cached, so the next one gets the real thing.
		ogFallback(w, r, "no-store")
		return
	case <-ctx.Done():
		ogFallback(w, r, "no-store")
		return
	}

	art, posterOK := s.fetchPoster(ctx, poster)
	body, err := s.render(title, year, art)
	if err != nil {
		s.logger.Error("og render", "tconst", tconst, "err", err)
		ogFallback(w, r, "no-store")
		return
	}
	// A card drawn without its picture is not the card; it is kept out
	// of the table so the next request tries the image host again.
	cache := ogRetry
	if posterOK {
		cache = ogCache
		if err := s.store.PutOGImage(ctx, tconst, v, body); err != nil {
			s.logger.Error("og store", "tconst", tconst, "err", err)
		}
	}
	s.send(w, r, tconst, v, body, cache)
	s.logger.Info("og_render", "tconst", tconst, "ms", ms(started), "poster_ok", posterOK, "cached", false)
}

func ms(since time.Time) float64 {
	return float64(time.Since(since).Microseconds()) / 1000
}

func (s *ogServer) send(w http.ResponseWriter, r *http.Request, tconst, v string, body []byte, cache string) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("ETag", fmt.Sprintf("%q", tconst+"-"+v))
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(body)
}

// ogFallback sends the reader to the site's own card.
func ogFallback(w http.ResponseWriter, r *http.Request, cache string) {
	w.Header().Set("Cache-Control", cache)
	http.Redirect(w, r, ogGeneric, http.StatusFound)
}

// ogTConst reads the movie out of /og/movie/{tconst}.png.
func ogTConst(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/og/movie/")
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, ".png")
	if !ok || !imdbid.Title(id) {
		return "", false
	}
	return id, true
}

// fetchPoster gets the artwork at the width the card draws it, or
// reports that it could not. A card without a picture is still a card;
// a card that waited ten seconds for one is a link that never unfurled.
func (s *ogServer) fetchPoster(ctx context.Context, url string) (image.Image, bool) {
	if url == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, ogPosterFetch)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ogPosterURL(url), nil)
	if err != nil {
		return nil, false
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, false
	}
	return img, true
}

// ogPosterURL is the poster at the width this card draws it. The
// resizing itself is the catalog's, so the card, the map and the
// colour job all ask the image hosts the same way.
func ogPosterURL(url string) string {
	return catalog.PosterAt(url, ogPosterWidth)
}

// render draws the whole card.
func (s *ogServer) render(title string, year int, art image.Image) ([]byte, error) {
	faces, err := s.faces()
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, ogW, ogH))
	fillRect(dst, dst.Bounds(), ogGround)
	s.drawTexture(dst)
	s.drawPoster(dst, faces, title, art)
	s.drawWords(dst, faces, title, year)

	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&out, dst); err != nil {
		return nil, fmt.Errorf("og: encode: %w", err)
	}
	return out.Bytes(), nil
}

// drawTexture is the map's own background, behind the words: year bands
// across and rating gridlines down. It is the only decoration, because
// it is the only decoration that says what the link opens.
func (s *ogServer) drawTexture(dst *image.RGBA) {
	for i := 0; i*bandStep <= ogH; i++ {
		y := i * bandStep
		if i%2 == 1 {
			fillRect(dst, image.Rect(panelX, y, ogW, min(y+bandStep, ogH)), ogBand)
		}
		fillRect(dst, image.Rect(panelX, y, ogW, y+1), ogRule)
	}
	for x := lineFirst; x < ogW; x += lineStep {
		fillRect(dst, image.Rect(x, 0, x+1, ogH), ogRule)
	}
	// The texture is not meant to reach the poster. It fades out rather
	// than stopping at a hard edge, which would read as a second panel.
	for x := panelX; x < fadeTo; x++ {
		a := 1 - float64(x-panelX)/float64(fadeTo-panelX)
		c := ogGround
		c.A = uint8(math.Round(a * 255))
		drawOver(dst, image.Rect(x, 0, x+1, ogH), c)
	}
}

// drawPoster is the artwork, its shadow and the accent ring around it —
// the anchor card's own treatment, because that is what this movie is
// on the map the link opens.
func (s *ogServer) drawPoster(dst *image.RGBA, faces *ogFaces, title string, art image.Image) {
	shadow := blurAlpha(
		roundRectMask(posterW, posterH, posterRadius),
		shadowBlur,
	)
	drawMaskAt(dst, image.Pt(posterX-shadowBlur, shadowY-shadowBlur), shadow, ogDrop)

	box := image.Rect(posterX, posterY, posterX+posterW, posterY+posterH)
	mask := roundRectMask(posterW, posterH, posterRadius)
	if art != nil {
		cover := image.NewRGBA(image.Rect(0, 0, posterW, posterH))
		xdraw.CatmullRom.Scale(cover, cover.Bounds(), art, coverCrop(art.Bounds(), posterW, posterH), xdraw.Src, nil)
		xdraw.DrawMask(dst, box, cover, image.Point{}, mask, image.Point{}, xdraw.Over)
	} else {
		// No poster: the film's own gradient and its first letter, the
		// same fallback the cards on the map use.
		fill := ogPosterFallback(title, posterW, posterH)
		xdraw.DrawMask(dst, box, fill, image.Point{}, mask, image.Point{}, xdraw.Over)
		faces.drawFirstLetter(dst, box, title)
	}

	strokeRoundRect(dst,
		image.Rect(ringX, ringY, ringX+ringW, ringY+ringH),
		ringRadius, ringStroke, ogAccent)
}

// drawFirstLetter centres the title's first character in the poster's
// place. The first grapheme, not the first byte: a title that starts
// with an emoji or an accented letter should show that.
func (f *ogFaces) drawFirstLetter(dst *image.RGBA, box image.Rectangle, title string) {
	r, size := utf8.DecodeRuneInString(strings.TrimSpace(title))
	if size == 0 || r == utf8.RuneError {
		return
	}
	letter := strings.ToUpper(string(r))
	w := textWidth(f.letter.face, letter)
	m := f.letter.face.Metrics()
	x := box.Min.X + (box.Dx()-w)/2
	// Centred on the letter's own body rather than on the line box,
	// which would sit it low by the whole descender.
	y := box.Min.Y + (box.Dy()+m.CapHeight.Ceil())/2
	drawText(dst, f.letter.face, x, y, ogFade, letter)
}

// drawWords is the wordmark, the title, the year and the line that says
// what the map is.
func (s *ogServer) drawWords(dst *image.RGBA, faces *ogFaces, title string, year int) {
	xdraw.CatmullRom.Scale(dst,
		image.Rect(markX, markY, markX+markW, markY+markH),
		s.mark, s.mark.Bounds(), xdraw.Over, nil)
	// The wordmark's baseline is the bottom of the mark's bowl, so the
	// mark reads as the C it stands in for.
	drawText(dst, faces.wordmark.face, markX+markW+1, markY+36, ogInk, "inedikt")

	face, lead, lines := faces.wrapTitle(title)
	baseline := titleTop + 56
	for _, line := range lines {
		drawText(dst, face.face, textX, baseline, ogInk, line)
		baseline += lead
	}

	if year > 0 {
		// The year goes under the title's line boxes, the way the page's
		// CSS stacks the two: each title line is lead tall from titleTop,
		// then the gap, then a line exactly as tall as the year's type.
		// Measuring from the title font's descent instead moved the year
		// whenever the title's font changed.
		top := titleTop + len(lines)*lead + yearGap
		drawText(dst, faces.year.face, textX, top+faces.year.inBox, ogSoft, fmt.Sprint(year))
	}
	drawText(dst, faces.line.face, textX, lineBottom, ogQuiet, ogTagline)
}

// wrapTitle fits a title into the box, dropping a size rather than
// shrinking the card's one piece of typography to nothing. Past four
// lines at the smaller size it is cut: a title nobody can read in four
// lines is not going to be read in six.
func (f *ogFaces) wrapTitle(title string) (*ogFace, int, []string) {
	title = strings.TrimSpace(title)
	if lines := wrapText(f.title.face, title, textW); len(lines) <= titleMaxLines {
		return f.title, titleLead, lines
	}
	lines := wrapText(f.titleSmall.face, title, textW)
	if len(lines) > titleHardMax {
		lines = lines[:titleHardMax]
		lines[titleHardMax-1] = ellipsise(f.titleSmall.face, lines[titleHardMax-1], textW)
	}
	return f.titleSmall, titleSmallLead, lines
}

// wrapText breaks a string into lines that fit, greedily. A single word
// longer than the box is broken by character, because the alternative
// is one word running off the side of the picture.
func wrapText(face font.Face, s string, width int) []string {
	var lines []string
	var line string
	for _, word := range strings.Fields(s) {
		try := word
		if line != "" {
			try = line + " " + word
		}
		if textWidth(face, try) <= width {
			line = try
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for textWidth(face, word) > width {
			cut := breakPoint(face, word, width)
			if cut == 0 {
				break
			}
			lines = append(lines, word[:cut])
			word = word[cut:]
		}
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// breakPoint is the most of a word that fits, in bytes.
func breakPoint(face font.Face, word string, width int) int {
	last := 0
	for i := range word {
		if i == 0 {
			continue
		}
		if textWidth(face, word[:i]) > width {
			return last
		}
		last = i
	}
	return last
}

// ellipsise cuts a line down until it and an ellipsis fit.
func ellipsise(face font.Face, line string, width int) string {
	const dots = "…"
	if textWidth(face, line+dots) <= width {
		return line + dots
	}
	runes := []rune(line)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		try := strings.TrimRight(string(runes), " ") + dots
		if textWidth(face, try) <= width {
			return try
		}
	}
	return dots
}

func textWidth(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

func drawText(dst *image.RGBA, face font.Face, x, baseline int, c color.Color, s string) {
	d := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, baseline),
	}
	d.DrawString(s)
}

// ogHue is a film's hue, 0–359, from its title, by the same hash as
// hueOf in web/src/poster.ts, so for a film with no poster the card and
// the map land on the same hue.
//
// The hash runs over UTF-16 code units, because that is what
// String.charCodeAt gives the client, and wraps at 32 bits the way the
// client's >>> 0 does.
func ogHue(title string) int {
	var h uint32
	for _, u := range utf16.Encode([]rune(title)) {
		h = h*31 + uint32(u)
	}
	return int(h % 360)
}

// ogPosterFallback is what the poster's place shows when there is no
// poster: the app's own dark fallback (posterFallback in
// web/src/poster.ts), CSS's
//
//	linear-gradient(165deg, oklch(0.45 0.07 h), oklch(0.28 0.05 h))
//
// at the size of the box it fills, with h = ogHue(title). These are the
// dark theme's stops, because the card is dark whatever theme the
// reader's own app is in. A change to those stops is a change here too,
// and to catalog.OGTemplateVersion.
//
// The browser mixes oklch() stops in OKLab — the CSS default for any
// colour that is not a legacy sRGB one — so this does too, and only
// then converts to sRGB. Mixing the two ends as hex instead would grey
// out the middle. Both ends share a hue, so OKLab and OKLCH give the
// same colours here; OKLab is simply what the browser does.
//
// One colour per 1/1024 of the gradient line is worked out and every
// pixel looks its colour up. That is finer than a pixel along the
// poster's 590 px line, and it keeps the cubes and powers of the
// conversion to a thousand rather than one per pixel.
func ogPosterFallback(title string, w, h int) *image.RGBA {
	hue := float64(ogHue(title))
	l0, a0, b0 := okLCh(0.45, 0.07, hue)
	l1, a1, b1 := okLCh(0.28, 0.05, hue)
	const steps = 1024
	var ramp [steps + 1]color.RGBA
	for i := range ramp {
		t := float64(i) / steps
		c := okLabToNRGBA(l0+(l1-l0)*t, a0+(a1-a0)*t, b0+(b1-b0)*t)
		ramp[i] = color.RGBA{c.R, c.G, c.B, 0xff}
	}
	at := cssGradientLine(w, h, 165)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, ramp[int(math.Round(at(x, y)*steps))])
		}
	}
	return img
}

// cssGradientLine is where each pixel of a w×h box falls along a CSS
// linear-gradient at this angle, from 0 at the first stop to 1 at the
// last.
//
// CSS's angles: 0deg points up and they turn clockwise, so 90deg is
// "to right" and 180deg "to bottom". The line runs through the box's
// centre, and it is |w·sin a| + |h·cos a| long, which is what puts the
// two corners it runs between exactly on its ends. A pixel is taken at
// its centre.
func cssGradientLine(w, h int, deg float64) func(x, y int) float64 {
	rad := deg * math.Pi / 180
	// Screen y runs down, so "up" is −y.
	dx, dy := math.Sin(rad), -math.Cos(rad)
	length := math.Abs(float64(w)*dx) + math.Abs(float64(h)*dy)
	cx, cy := float64(w)/2, float64(h)/2
	return func(x, y int) float64 {
		t := 0.5 + ((float64(x)+0.5-cx)*dx+(float64(y)+0.5-cy)*dy)/length
		return math.Min(math.Max(t, 0), 1)
	}
}

// okLCh turns oklch's polar form into OKLab's a and b.
func okLCh(l, c, hue float64) (float64, float64, float64) {
	rad := hue * math.Pi / 180
	return l, c * math.Cos(rad), c * math.Sin(rad)
}

// okLabToNRGBA converts OKLab to 8-bit sRGB: to the cone responses,
// cubed back to linear light, through the matrix to linear sRGB, then
// the sRGB transfer curve. The matrices are Björn Ottosson's, the same
// ones internal/catalog/postercolour.go uses for the posters' own
// colours; they are repeated here rather than exported from the catalog
// because this is the only other place that needs them.
func okLabToNRGBA(L, a, b float64) color.NRGBA {
	cube := func(x float64) float64 { return x * x * x }
	l := cube(L + 0.3963377774*a + 0.2158037573*b)
	m := cube(L - 0.1055613458*a - 0.0638541728*b)
	s := cube(L - 0.0894841775*a - 1.2914855480*b)
	return color.NRGBA{
		R: srgb8(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		G: srgb8(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		B: srgb8(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
		A: 0xff,
	}
}

// srgb8 is one linear-light channel encoded for sRGB, in 8 bits. A
// colour just outside sRGB is clipped channel by channel, which is how
// Chrome draws the same gradient.
func srgb8(c float64) uint8 {
	if c <= 0.0031308 {
		c *= 12.92
	} else {
		c = 1.055*math.Pow(c, 1/2.4) - 0.055
	}
	return uint8(math.Round(math.Min(math.Max(c, 0), 1) * 255))
}

// coverCrop is the part of a source image to take so it fills a box of
// this shape without distorting it: the middle of whichever dimension
// is too long.
func coverCrop(src image.Rectangle, w, h int) image.Rectangle {
	want := float64(w) / float64(h)
	have := float64(src.Dx()) / float64(src.Dy())
	if math.Abs(want-have) < 1e-9 {
		return src
	}
	if have > want {
		// Too wide: take a centred column.
		keep := int(math.Round(float64(src.Dy()) * want))
		off := (src.Dx() - keep) / 2
		return image.Rect(src.Min.X+off, src.Min.Y, src.Min.X+off+keep, src.Max.Y)
	}
	keep := int(math.Round(float64(src.Dx()) / want))
	off := (src.Dy() - keep) / 2
	return image.Rect(src.Min.X, src.Min.Y+off, src.Max.X, src.Min.Y+off+keep)
}

func fillRect(dst *image.RGBA, r image.Rectangle, c color.NRGBA) {
	xdraw.Draw(dst, r, image.NewUniform(c), image.Point{}, xdraw.Over)
}

func drawOver(dst *image.RGBA, r image.Rectangle, c color.NRGBA) {
	xdraw.Draw(dst, r, image.NewUniform(c), image.Point{}, xdraw.Over)
}

func drawMaskAt(dst *image.RGBA, at image.Point, mask *image.Alpha, c color.NRGBA) {
	r := image.Rectangle{Min: at, Max: at.Add(mask.Bounds().Size())}
	xdraw.DrawMask(dst, r, image.NewUniform(c), image.Point{}, mask, mask.Bounds().Min, xdraw.Over)
}

// roundRectMask is a filled rounded rectangle as coverage, antialiased
// at the corners by sampling each edge pixel's distance from the arc.
func roundRectMask(w, h, radius int) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	r := float64(radius)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetAlpha(x, y, color.Alpha{A: cornerCoverage(float64(x)+0.5, float64(y)+0.5, float64(w), float64(h), r)})
		}
	}
	return m
}

// cornerCoverage is how much of the pixel at (x, y) the rounded
// rectangle covers: full in the straight parts, and a one-pixel ramp
// across each arc so the corners are not staircases.
func cornerCoverage(x, y, w, h, r float64) uint8 {
	cx, cy := x, y
	switch {
	case x < r:
		cx = r
	case x > w-r:
		cx = w - r
	}
	switch {
	case y < r:
		cy = r
	case y > h-r:
		cy = h - r
	}
	if cx == x && cy == y {
		return 0xff
	}
	d := math.Hypot(x-cx, y-cy)
	if d <= r-0.5 {
		return 0xff
	}
	if d >= r+0.5 {
		return 0
	}
	return uint8(math.Round((r + 0.5 - d) * 255))
}

// strokeRoundRect draws the outline of a rounded rectangle: the shape
// itself, minus the same shape inset by the stroke width.
func strokeRoundRect(dst *image.RGBA, r image.Rectangle, radius, width int, c color.NRGBA) {
	outer := roundRectMask(r.Dx(), r.Dy(), radius)
	inner := roundRectMask(r.Dx()-2*width, r.Dy()-2*width, max(radius-width, 0))
	for y := inner.Bounds().Min.Y; y < inner.Bounds().Max.Y; y++ {
		for x := inner.Bounds().Min.X; x < inner.Bounds().Max.X; x++ {
			a := outer.AlphaAt(x+width, y+width).A
			if in := inner.AlphaAt(x, y).A; in > 0 {
				if int(a)-int(in) < 0 {
					a = 0
				} else {
					a -= in
				}
			}
			outer.SetAlpha(x+width, y+width, color.Alpha{A: a})
		}
	}
	drawMaskAt(dst, r.Min, outer, c)
}

// blurAlpha softens a mask with three box passes, which is close enough
// to a Gaussian that nobody has ever told the difference in a shadow.
// The result is larger than what went in by the blur radius on every
// side, so the spread has somewhere to go.
func blurAlpha(src *image.Alpha, radius int) *image.Alpha {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := image.NewAlpha(image.Rect(0, 0, w+2*radius, h+2*radius))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.SetAlpha(x+radius, y+radius, src.AlphaAt(x, y))
		}
	}
	r := max(radius/3, 1)
	for pass := 0; pass < 3; pass++ {
		out = boxBlur(out, r, true)
		out = boxBlur(out, r, false)
	}
	return out
}

// boxBlur averages each pixel with its neighbours along one axis,
// running a sum along the row rather than re-adding the window at every
// step.
func boxBlur(src *image.Alpha, r int, horizontal bool) *image.Alpha {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewAlpha(b)
	outer, inner := h, w
	if !horizontal {
		outer, inner = w, h
	}
	at := func(a, b int) uint8 {
		if horizontal {
			return src.AlphaAt(b, a).A
		}
		return src.AlphaAt(a, b).A
	}
	set := func(a, b int, v uint8) {
		if horizontal {
			out.SetAlpha(b, a, color.Alpha{A: v})
			return
		}
		out.SetAlpha(a, b, color.Alpha{A: v})
	}
	width := 2*r + 1
	for a := 0; a < outer; a++ {
		sum := 0
		for i := -r; i <= r; i++ {
			sum += int(at(a, clamp(i, 0, inner-1)))
		}
		for i := 0; i < inner; i++ {
			set(a, i, uint8(sum/width))
			sum -= int(at(a, clamp(i-r, 0, inner-1)))
			sum += int(at(a, clamp(i+r+1, 0, inner-1)))
		}
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
