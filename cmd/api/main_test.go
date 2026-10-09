package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/api"
	"cinedikt/internal/catalog"
	"cinedikt/internal/config"
	"cinedikt/internal/tmdb"
)

func TestWebCacheHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "favicon.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(routes(api, dir, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		path, cache, body string
	}{
		{"/assets/app-abc123.js", assetCacheControl, "console.log(1)"},
		{"/", htmlCacheControl, "<html>app</html>"},
		{"/some/spa/route", htmlCacheControl, "<html>app</html>"},
		{"/favicon.svg", htmlCacheControl, "<svg/>"},
		{"/assets/missing.js", htmlCacheControl, "<html>app</html>"},
	} {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if got := resp.Header.Get("Cache-Control"); got != tc.cache {
			t.Errorf("GET %s Cache-Control = %q, want %q", tc.path, got, tc.cache)
		}
		if string(body) != tc.body {
			t.Errorf("GET %s body = %q, want %q", tc.path, body, tc.body)
		}
	}
}

func TestShareImageIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	html := `<meta property="og:image" content="/og.png?v=2" />` + "\n" +
		`<meta name="twitter:image" content="/og.png?v=2" />`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(routes(api, dir, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/movie/tt0133093-the-matrix", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "dev.cinedikt.com")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	want := `<meta property="og:image" content="https://dev.cinedikt.com/og.png?v=2" />` + "\n" +
		`<meta name="twitter:image" content="https://dev.cinedikt.com/og.png?v=2" />`
	if string(body) != want {
		t.Fatalf("share image tags = %q, want %q", body, want)
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", `dev.cinedikt.com"><script>`)
	resp, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != html {
		t.Fatalf("unsafe host was written into the page: %q", body)
	}
}

// TestEveryPageAddressIsServedTheApp: a map's own address, and anything
// that is not one, get the page itself, which decides what to show. Not
// a redirect, not a 404: the app's own URLs have to work on reload.
func TestEveryPageAddressIsServedTheApp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(routes(api, dir, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for _, path := range []string{"/movie/tt0133093-the-matrix", "/film/tt0133093", "/about", "/daily", "/"} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != "<html></html>" {
			t.Errorf("GET %s = %d %q, want 200 and the page", path, resp.StatusCode, body)
		}
	}
}

// indexFixture is the real index.html. The rewrites match tags in the
// page as it actually ships, so a head that is reshaped without them in
// mind fails here rather than in someone's chat window.
func indexFixture(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func previewServer(t *testing.T, meta movieMeta) *httptest.Server {
	t.Helper()
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(routes(api, indexFixture(t), meta, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	return srv
}

func fetchHead(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "cinedikt.com")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
	}
	return string(body)
}

// matrixPoster is what OMDb stores for The Matrix, which is what the
// share card is drawn from.
const matrixPoster = "https://m.media-amazon.com/images/M/matrix.jpg"

func theMatrix(_ context.Context, tconst string) (string, int, string, error) {
	if tconst != "tt0133093" {
		return "", 0, "", errors.New("not found")
	}
	return "The Matrix", 1999, matrixPoster, nil
}

func TestPreviewNamesTheMovie(t *testing.T) {
	srv := previewServer(t, theMatrix)
	head := fetchHead(t, srv, "/movie/tt0133093-the-matrix")

	for _, want := range []string{
		`<title>The Matrix — everything its cast and directors made · Cinedikt</title>`,
		`content="The Matrix (1999) — everything its cast and directors made"`,
		`content="See every movie The Matrix’s cast and directors made, arranged by year and rating."`,
		`<meta property="og:url" content="https://cinedikt.com/movie/tt0133093-the-matrix" />`,
		`<meta property="og:image:alt" content="The Matrix (1999) poster, on Cinedikt" />`,
		`<meta property="og:site_name" content="Cinedikt" />`,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("preview is missing %s", want)
		}
	}
	// The card is this movie's own, stamped so a new poster reaches an
	// unfurler that cached the old one.
	card := "https://cinedikt.com/og/movie/tt0133093.png?v=" + catalog.OGVersion(matrixPoster, "The Matrix")
	for _, want := range []string{
		`<meta property="og:image" content="` + card + `" />`,
		`<meta name="twitter:image" content="` + card + `" />`,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("preview is missing %s\n%s", want, head)
		}
	}
	if strings.Contains(head, "/og.png") {
		t.Error("the generic card is still on a page that knows the movie")
	}
	// Both the og: and the plain description say the movie's name.
	if n := strings.Count(head, "See every movie The Matrix’s cast and directors made"); n != 2 {
		t.Errorf("description written %d times, want 2 (og:description and description)", n)
	}
	if strings.Contains(head, "a movie’s cast and directors, and everything they made") {
		t.Error("the tagline is still on a page that knows the movie")
	}
}

func TestPreviewUsesTheCanonicalSlug(t *testing.T) {
	srv := previewServer(t, theMatrix)
	head := fetchHead(t, srv, "/movie/tt0133093-wrong-slug")
	if !strings.Contains(head, `content="https://cinedikt.com/movie/tt0133093-the-matrix"`) {
		t.Error("og:url followed the pasted slug instead of the stored title")
	}
	// A bare id is a movie route too.
	head = fetchHead(t, srv, "/movie/tt0133093")
	if !strings.Contains(head, `content="https://cinedikt.com/movie/tt0133093-the-matrix"`) {
		t.Error("og:url is wrong for a link with no slug")
	}
}

func TestPreviewEscapesTheTitle(t *testing.T) {
	srv := previewServer(t, func(_ context.Context, _ string) (string, int, string, error) {
		return `The "<Movie>" & Co`, 2001, "", nil
	})
	head := fetchHead(t, srv, "/movie/tt0000007")
	if strings.Contains(head, `<Movie>`) || strings.Contains(head, `content="The "`) {
		t.Errorf("an unescaped title reached the page:\n%s", head)
	}
	if !strings.Contains(head, `The &#34;&lt;Movie&gt;&#34; &amp; Co`) {
		t.Errorf("title was not escaped as expected:\n%s", head)
	}
	// And the slug drops the punctuation rather than carrying it into a URL.
	if !strings.Contains(head, `content="https://cinedikt.com/movie/tt0000007-the-movie-co"`) {
		t.Error("og:url slug kept characters a URL should not")
	}
}

func TestPreviewFallsBackToTheGenericPage(t *testing.T) {
	tagline := `<title>Cinedikt — a movie’s cast and directors, and everything they made</title>`
	slow := make(chan struct{})
	t.Cleanup(func() { close(slow) })

	for _, c := range []struct {
		name string
		meta movieMeta
	}{
		{"not in the catalog", func(context.Context, string) (string, int, string, error) {
			return "", 0, "", errors.New("catalog: not found")
		}},
		{"the catalog is down", func(context.Context, string) (string, int, string, error) {
			return "", 0, "", errors.New("dial tcp: connection refused")
		}},
		{"a movie with no title", func(context.Context, string) (string, int, string, error) {
			return "   ", 0, "", nil
		}},
		{"slower than the deadline", func(ctx context.Context, _ string) (string, int, string, error) {
			// Ignores the context on purpose: the page must not wait on a
			// read that will not stop when it is told to.
			select {
			case <-slow:
			case <-time.After(5 * time.Second):
			}
			return "Too Late", 1999, "", nil
		}},
		{"no lookup at all", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := previewServer(t, c.meta)
			head := fetchHead(t, srv, "/movie/tt0133093-the-matrix")
			if !strings.Contains(head, tagline) {
				t.Errorf("the generic title is gone:\n%s", head)
			}
			if strings.Contains(head, "everything its cast and directors made") {
				t.Error("a movie was named on a page that could not look one up")
			}
			// And the card stays the site's own. It is the same versioned
			// address the renderer redirects to, so index.html and og.go
			// cannot name two different pictures after a version bump.
			if !strings.Contains(head, `content="https://cinedikt.com`+ogGeneric+`"`) {
				t.Error("the generic share card is gone from a page with no movie")
			}
		})
	}
}

func TestPreviewOnlyLooksUpMovieRoutes(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	srv := previewServer(t, func(_ context.Context, tconst string) (string, int, string, error) {
		mu.Lock()
		asked = append(asked, tconst)
		mu.Unlock()
		return "The Matrix", 1999, matrixPoster, nil
	})
	// The last two were movie routes once. A TMDb id is not an address
	// this app writes any more, and a page that guessed a movie from one
	// would be naming whatever film happens to hold that number.
	for _, path := range []string{
		"/", "/some/spa/route", "/movie/", "/movie/abc", "/movies/tt0133093",
		"/movie/603", "/movie/603-the-matrix",
	} {
		fetchHead(t, srv, path)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 0 {
		t.Errorf("the catalog was read for %v, which are not movie routes", asked)
	}
}

func TestSlugifyMatchesTheClient(t *testing.T) {
	// The cases are the ones in web/src/movieParam.test.ts: og:url has to
	// name the address the address bar settles on.
	for _, c := range []struct{ title, want string }{
		{"The Matrix", "the-matrix"},
		{"Ocean's Eleven", "oceans-eleven"},
		{"Amélie", "amelie"},
		{"9½ Weeks!", "91-2-weeks"},
		{"WALL·E", "wall-e"},
		{"", ""},
		{"!!!", ""},
	} {
		if got := slugify(c.title); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.title, got, c.want)
		}
	}
	long := strings.Repeat("a", 58) + " bb"
	if got := slugify(long); strings.HasSuffix(got, "-") || len(got) > 60 {
		t.Errorf("slugify(long) = %q, want no trailing hyphen and at most 60", got)
	}
}

func TestGenericPageStillHasAnAbsoluteURL(t *testing.T) {
	srv := previewServer(t, theMatrix)
	for _, path := range []string{"/", "/some/spa/route"} {
		if head := fetchHead(t, srv, path); !strings.Contains(head, `<meta property="og:url" content="https://cinedikt.com/" />`) {
			t.Errorf("GET %s left og:url relative", path)
		}
	}
}

// The About page is named for a scraper at its one address, however it
// was reached, and keeps the site's own description and card: it is a
// page about the site, not about a movie.
func TestTheAboutPageIsNamed(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	srv := previewServer(t, func(_ context.Context, tconst string) (string, int, string, error) {
		mu.Lock()
		asked = append(asked, tconst)
		mu.Unlock()
		return "The Matrix", 1999, matrixPoster, nil
	})
	generic := fetchHead(t, srv, "/")
	for _, path := range []string{"/about", "/about/"} {
		head := fetchHead(t, srv, path)
		for _, want := range []string{
			`<title>About · Cinedikt</title>`,
			`<meta property="og:title" content="About · Cinedikt" />`,
			`<meta property="og:url" content="https://cinedikt.com/about" />`,
			// What stays the generic page's: the description, the card
			// and what the card is said to be.
			`content="Start from a movie and follow its cast and directors across a timeline of everything they went on to make."`,
			`<meta property="og:image" content="https://cinedikt.com` + ogGeneric + `" />`,
			`<meta name="twitter:image" content="https://cinedikt.com` + ogGeneric + `" />`,
			`<meta property="og:image:alt" content="Cinedikt — a movie’s cast and directors, and everything they made" />`,
		} {
			if !strings.Contains(head, want) {
				t.Errorf("GET %s is missing %s", path, want)
			}
		}
		if strings.Contains(head, "<title>Cinedikt — ") || strings.Contains(head, `og:title" content="Cinedikt — `) {
			t.Errorf("GET %s still has the tagline as its title", path)
		}
		// Nothing else about the page changes.
		for _, tag := range []*regexp.Regexp{titleTag, ogTitle, ogURL} {
			head = tag.ReplaceAllString(head, "")
		}
		want := generic
		for _, tag := range []*regexp.Regexp{titleTag, ogTitle, ogURL} {
			want = tag.ReplaceAllString(want, "")
		}
		if head != want {
			t.Errorf("GET %s changed more than its title and address", path)
		}
	}
	// Neither is a movie route, so nothing is looked up, and a path
	// under it is not the About page.
	if head := fetchHead(t, srv, "/about/team"); strings.Contains(head, "About · Cinedikt") || strings.Contains(head, "cinedikt.com/about") {
		t.Error("a path under /about was named as the About page")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 0 {
		t.Errorf("the catalog was read for %v on the About page", asked)
	}
}

// Daily is named for a scraper at its one address, however it was
// reached: its own title, and a preview that names the game, Name Drop,
// and says what it is. The card stays the site's: a picture of the day's
// movie would give it away.
func TestTheDailyPageIsNamed(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	srv := previewServer(t, func(_ context.Context, tconst string) (string, int, string, error) {
		mu.Lock()
		asked = append(asked, tconst)
		mu.Unlock()
		return "The Matrix", 1999, matrixPoster, nil
	})
	generic := fetchHead(t, srv, "/")
	for _, path := range []string{"/daily", "/daily/"} {
		head := fetchHead(t, srv, path)
		for _, want := range []string{
			`<title>Daily · Cinedikt</title>`,
			`<meta property="og:title" content="Cinedikt Daily: Name Drop" />`,
			`content="One hidden movie a day. Its cast shows up one name at a time."`,
			`<meta property="og:url" content="https://cinedikt.com/daily" />`,
			// What stays the generic page's: the card, what the card is
			// said to be, and the description search engines read.
			`<meta property="og:image" content="https://cinedikt.com` + ogGeneric + `" />`,
			`<meta name="twitter:image" content="https://cinedikt.com` + ogGeneric + `" />`,
			`<meta property="og:image:alt" content="Cinedikt — a movie’s cast and directors, and everything they made" />`,
		} {
			if !strings.Contains(head, want) {
				t.Errorf("GET %s is missing %s", path, want)
			}
		}
		// Nor does any preview still use a name the game had before,
		// spelt out in pieces so that this file does not hold them either.
		for _, old := range [][]string{{"whose", "map", "is", "it"}, {"point", "blank"}} {
			if strings.Contains(strings.ToLower(head), strings.Join(old, " ")) {
				t.Errorf("GET %s still names the game by an old name", path)
			}
		}
		// Nothing else about the page changes.
		for _, tag := range []*regexp.Regexp{titleTag, ogTitle, ogDesc, ogURL} {
			head = tag.ReplaceAllString(head, "")
		}
		want := generic
		for _, tag := range []*regexp.Regexp{titleTag, ogTitle, ogDesc, ogURL} {
			want = tag.ReplaceAllString(want, "")
		}
		if head != want {
			t.Errorf("GET %s changed more than its title, description and address", path)
		}
	}
	// Neither is a movie route, so nothing is looked up, and a path
	// under it is not Daily.
	if head := fetchHead(t, srv, "/daily/142"); strings.Contains(head, "Daily · Cinedikt") || strings.Contains(head, "cinedikt.com/daily") {
		t.Error("a path under /daily was named as Daily")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 0 {
		t.Errorf("the catalog was read for %v on Daily", asked)
	}
}

// TestTheRequestPathWaitsOnTheProcessLimiter: the search fallback and the
// poster stand-in share the client this builds, and it has to draw on
// the same budget as the catalog jobs.
func TestTheRequestPathWaitsOnTheProcessLimiter(t *testing.T) {
	limiter := tmdb.NewLimiter(20)
	cfg := config.Config{TMDBAPIKey: "k"}
	if client := searchFallback(cfg, limiter); client == nil || client.Limiter() != limiter {
		t.Error("the request-path TMDb client has a limiter of its own")
	}
	if c := searchFallback(config.Config{}, limiter); c != nil {
		t.Error("a client was built without credentials")
	}
}

// TestGeoIPTakesBothMaxMindCredentials: MaxMind's download takes the
// account id with the license key, so readers are placed only with both.
// A key on its own leaves the lookup off, and the log says which variable
// is missing rather than leaving it to the first check to fail.
func TestGeoIPTakesBothMaxMindCredentials(t *testing.T) {
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))

	if u := geoIPFor(config.Config{}, nil, logger); u != nil {
		t.Errorf("without a key: an updater %+v", u)
	}
	if strings.Contains(log.String(), "level=WARN") {
		t.Errorf("without a key: a warning, when nothing was set wrong: %s", log.String())
	}

	log.Reset()
	if u := geoIPFor(config.Config{MaxMindLicenseKey: "key"}, nil, logger); u != nil {
		t.Errorf("with a key alone: an updater %+v", u)
	}
	if got := log.String(); !strings.Contains(got, "level=WARN") || !strings.Contains(got, "MAXMIND_ACCOUNT_ID") {
		t.Errorf("with a key alone: log = %q, want a warning naming MAXMIND_ACCOUNT_ID", got)
	}

	log.Reset()
	u := geoIPFor(config.Config{MaxMindLicenseKey: "key", MaxMindAccountID: "42"}, nil, logger)
	if u == nil {
		t.Fatal("with both credentials: no updater")
	}
	if u.LicenseKey != "key" || u.AccountID != "42" || u.Lookup == nil {
		t.Errorf("with both credentials: updater %+v, want both credentials and a lookup to fill", u)
	}
	if strings.Contains(log.String(), "geoip is off") {
		t.Errorf("with both credentials: log = %q, want nothing saying the lookup is off", log.String())
	}
}

// plainIndex is a page with a head and nothing a share-tag rewrite
// touches, so whatever is served that differs from it was put there by
// the trackers' tag.
const plainIndex = "<!doctype html>\n<html>\n  <head>\n    <title>Cinedikt</title>\n  </head>\n  <body><div id=\"root\"></div></body>\n</html>\n"

func plainIndexDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(plainIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// trackingServer serves dir's index.html with tracking, behind a stand-in
// API.
func trackingServer(t *testing.T, dir string, meta movieMeta, tracking *pageAnalytics) *httptest.Server {
	t.Helper()
	stub := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(routes(stub, dir, meta, nil, tracking, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	return srv
}

// trackersOn is a production process with the switch on and both
// trackers' test tokens set.
var trackersOn = config.Config{
	Environment:      config.EnvProduction,
	AnalyticsEnabled: true,
	PostHogToken:     "phc_test",
	PostHogHost:      "https://us.i.posthog.com",
	MixpanelToken:    "mp_test",
}

var analyticsTagPattern = regexp.MustCompile(`<meta name="cinedikt-analytics" content="([^"]*)" />`)

// servedAnalytics is the page's one trackers' tag, decoded the way the
// page decodes it: the attribute unescaped, then the JSON parsed. It
// fails unless there is exactly one, at the very end of the head.
func servedAnalytics(t *testing.T, body string) (tag string, settings map[string]any) {
	t.Helper()
	if n := strings.Count(body, "cinedikt-analytics"); n != 1 {
		t.Fatalf("the page names cinedikt-analytics %d times, want 1:\n%s", n, body)
	}
	m := analyticsTagPattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the trackers' tag is not a whole, quoted meta tag:\n%s", body)
	}
	if !strings.Contains(body, m[0]+"</head>") {
		t.Fatalf("the trackers' tag is not at the end of the head:\n%s", body)
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &settings); err != nil {
		t.Fatalf("the trackers' tag does not hold JSON: %v\n%s", err, m[1])
	}
	return m[0], settings
}

func TestThePageIsHandedTheTrackersOnlyWhenTrackingIsOn(t *testing.T) {
	srv := trackingServer(t, plainIndexDir(t), nil, pageAnalyticsFor(trackersOn))
	body := fetchHead(t, srv, "/")
	tag, settings := servedAnalytics(t, body)
	if want := strings.Replace(plainIndex, "</head>", tag+"</head>", 1); body != want {
		t.Errorf("the page was changed beyond the trackers' tag:\n%s\nwant\n%s", body, want)
	}
	want := map[string]any{"token": "phc_test", "host": "https://us.i.posthog.com", "mixpanel_token": "mp_test"}
	if len(settings) != len(want) {
		t.Errorf("settings = %v, want %v", settings, want)
	}
	for k, v := range want {
		if settings[k] != v {
			t.Errorf("settings[%q] = %v, want %v", k, settings[k], v)
		}
	}

	development := trackersOn
	development.Environment = config.EnvDevelopment
	switchedOff := trackersOn
	switchedOff.AnalyticsEnabled = false
	noTokens := trackersOn
	noTokens.PostHogToken, noTokens.MixpanelToken = "", ""
	for name, cfg := range map[string]config.Config{
		"development":                 development,
		"production, switched off":    switchedOff,
		"production, on and no token": noTokens,
	} {
		// The page exactly as it is on disk.
		srv := trackingServer(t, plainIndexDir(t), nil, pageAnalyticsFor(cfg))
		for _, path := range []string{"/", "/movie/tt0133093-the-matrix"} {
			if body := fetchHead(t, srv, path); body != plainIndex {
				t.Errorf("%s: GET %s changed the page:\n%s", name, path, body)
			}
		}
		// And the page as it ships, with its share tags rewritten exactly
		// as they would be with no trackers' settings at all.
		withTracking := trackingServer(t, indexFixture(t), theMatrix, pageAnalyticsFor(cfg))
		without := trackingServer(t, indexFixture(t), theMatrix, nil)
		for _, path := range []string{"/", "/movie/tt0133093-the-matrix"} {
			body := fetchHead(t, withTracking, path)
			if strings.Contains(body, "cinedikt-analytics") {
				t.Errorf("%s: GET %s carries the trackers' tag", name, path)
			}
			if want := fetchHead(t, without, path); body != want {
				t.Errorf("%s: GET %s differs from the page with tracking off", name, path)
			}
		}
	}
}

// Mixpanel's token is left out when it is not set, as the page has always
// been handed it, and either tracker alone is reason enough for the tag.
func TestTheTrackersTagCarriesWhicheverTokensAreSet(t *testing.T) {
	postHogOnly := trackersOn
	postHogOnly.MixpanelToken = ""
	_, settings := servedAnalytics(t, fetchHead(t, trackingServer(t, plainIndexDir(t), nil, pageAnalyticsFor(postHogOnly)), "/"))
	if _, ok := settings["mixpanel_token"]; ok {
		t.Errorf("mixpanel_token is in the tag with no token set: %v", settings)
	}
	if settings["token"] != "phc_test" || settings["host"] != "https://us.i.posthog.com" {
		t.Errorf("settings = %v", settings)
	}

	mixpanelOnly := trackersOn
	mixpanelOnly.PostHogToken = ""
	_, settings = servedAnalytics(t, fetchHead(t, trackingServer(t, plainIndexDir(t), nil, pageAnalyticsFor(mixpanelOnly)), "/"))
	if settings["token"] != "" || settings["mixpanel_token"] != "mp_test" {
		t.Errorf("settings = %v", settings)
	}
}

// A token or host is written into an attribute. Whatever it holds, it
// comes back out as it went in, and nothing in it reaches the page as
// markup.
func TestTheTrackersTagIsEscaped(t *testing.T) {
	tracking := &pageAnalytics{
		Token:         `phc_"><script>alert(1)</script>&amp;`,
		Host:          `https://ph.example/?a=1&b="2"'`,
		MixpanelToken: `mp_<&>"' />`,
	}
	body := fetchHead(t, trackingServer(t, plainIndexDir(t), nil, tracking), "/")
	tag, settings := servedAnalytics(t, body)
	if settings["token"] != tracking.Token || settings["host"] != tracking.Host || settings["mixpanel_token"] != tracking.MixpanelToken {
		t.Errorf("settings = %#v, want %#v", settings, tracking)
	}
	if rest := strings.Replace(body, tag, "", 1); rest != plainIndex {
		t.Errorf("something outside the trackers' tag was written into the page:\n%s", rest)
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("a token was written into the page as markup:\n%s", body)
	}
}

func TestTheTrackersTagSitsBesideTheShareTags(t *testing.T) {
	srv := trackingServer(t, indexFixture(t), theMatrix, pageAnalyticsFor(trackersOn))
	without := trackingServer(t, indexFixture(t), theMatrix, nil)

	head := fetchHead(t, srv, "/movie/tt0133093-the-matrix")
	tag, settings := servedAnalytics(t, head)
	if settings["token"] != "phc_test" || settings["mixpanel_token"] != "mp_test" {
		t.Errorf("settings = %v", settings)
	}
	for _, want := range []string{
		"<title>The Matrix — everything its cast and directors made · Cinedikt</title>",
		`<meta property="og:url" content="https://cinedikt.com/movie/tt0133093-the-matrix" />`,
		"https://cinedikt.com/og/movie/tt0133093.png?v=" + catalog.OGVersion(matrixPoster, "The Matrix"),
	} {
		if !strings.Contains(head, want) {
			t.Errorf("the movie's page lost %s beside the trackers' tag", want)
		}
	}
	if rest := strings.Replace(head, tag, "", 1); rest != fetchHead(t, without, "/movie/tt0133093-the-matrix") {
		t.Error("the trackers' tag changed the movie's page beyond itself")
	}

	head = fetchHead(t, srv, "/")
	servedAnalytics(t, head)
	if !strings.Contains(head, `<meta property="og:url" content="https://cinedikt.com/" />`) {
		t.Error("the generic page left og:url relative beside the trackers' tag")
	}
}

// A host the share tags cannot be made absolute from still gets the
// trackers: the tag needs no origin, and a page without it would go
// unreported.
func TestTheTrackersTagDoesNotWaitOnAKnownOrigin(t *testing.T) {
	srv := trackingServer(t, plainIndexDir(t), nil, pageAnalyticsFor(trackersOn))
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	// An underscore fails safeHost, so requestOrigin has no origin for it.
	req.Host = "not_a_safe_host"
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", resp.StatusCode)
	}
	servedAnalytics(t, string(body))
}

// The page is handed its settings, so nothing answers for them any more.
func TestTheAnalyticsConfigRouteIsGone(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := api.New(api.NewCatalogServer(nil, logger), logger)
	srv := httptest.NewServer(routes(server.Handler(), plainIndexDir(t), nil, nil, pageAnalyticsFor(trackersOn), logger))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/api/analytics-config")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /api/analytics-config = %d, want 404", resp.StatusCode)
	}
}

// TestPlayAgainIsOnlyOutsideProduction: Daily's development reset is a
// route only a server that is not production's has. Production's router
// has never heard of it, so it is 404 as any other unknown API path is,
// not the page and not a refusal; a development server's is there, and
// refuses a body that is not JSON before it asks the store anything,
// which is why no store is needed to tell the two apart.
func TestPlayAgainIsOnlyOutsideProduction(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for env, want := range map[string]int{
		config.EnvProduction:  http.StatusNotFound,
		config.EnvDevelopment: http.StatusUnsupportedMediaType,
	} {
		server := apiServer(config.Config{Environment: env}, api.NewCatalogServer(nil, logger), nil, logger)
		srv := httptest.NewServer(routes(server.Handler(), plainIndexDir(t), nil, nil, nil, logger))
		resp, err := http.Post(srv.URL+"/api/daily/dev/reset", "text/plain", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		srv.Close()
		if resp.StatusCode != want || strings.Contains(string(body), "<html") {
			t.Errorf("%s: POST /api/daily/dev/reset = %d %q, want %d", env, resp.StatusCode, body, want)
		}
	}
}
