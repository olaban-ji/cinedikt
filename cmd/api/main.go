// Command api serves the movie network over HTTP.
package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"
	"unicode"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/time/rate"

	"cinedikt/internal/analytics"
	"cinedikt/internal/api"
	"cinedikt/internal/app"
	"cinedikt/internal/catalog"
	"cinedikt/internal/config"
	"cinedikt/internal/notify"
	"cinedikt/internal/rediscache"
	"cinedikt/internal/telegram"
	"cinedikt/internal/tmdb"
	"cinedikt/internal/trailer"
)

// warmWorkers is how many background crawls run alongside requests.
const warmWorkers = 3

// Server timeouts. RequestTimeout is the budget a handler gets: long
// enough for a cold crawl to wait for a slot (SlotTimeout) and finish
// (SeedTimeout), short enough that a stuck dependency frees the
// connection. The rest are backstops for clients that stop reading.
const (
	requestTimeout    = 40 * time.Second
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
)

func main() {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	logger := config.NewLogger(level)
	if err := run(logger); err != nil {
		logger.Error("api failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := analytics.Init(analytics.Config{
		Production: cfg.Production(),
		Token:      cfg.PostHogToken,
		Host:       cfg.PostHogHost,
	}, logger); err != nil {
		return err
	}
	logger = analytics.Logger(logger, "cinedikt-api")
	logger.Info("starting", "environment", cfg.Environment, "addr", cfg.APIAddr)
	defer func() {
		if err := analytics.Close(); err != nil {
			logger.Error("close PostHog client", "err", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A catalog replaces the graph entirely: no Neo4j, no Redis, no
	// crawl. The old wiring stays for a process that has not been
	// given a database.
	var meta movieMeta
	var og http.Handler
	var server *api.Server
	if cfg.DatabaseURL != "" {
		store, err := catalog.Open(ctx, cfg.DatabaseURL, cfg.APIMaxConns)
		if err != nil {
			return fmt.Errorf("open catalog: %w", err)
		}
		defer store.Close()
		server = api.NewWithLimits(nil, nil, nil, limits(cfg), logger)
		catalogServer := api.NewCatalogServer(store, logger)
		// The process's one TMDb budget. TMDb counts requests per
		// address, so a reader's lookups and every background job wait
		// on this one limiter rather than each spending a rate of its
		// own.
		limiter := tmdb.NewLimiter(cfg.TMDBRatePerSecond)
		// And YouTube's, for the embed check a trailer has to pass,
		// shared by the endpoint and the trailer job the same way.
		oembed := trailer.NewOEmbed()
		client, closer, err := searchFallback(ctx, cfg, limiter, logger)
		if err != nil {
			return err
		}
		if closer != nil {
			defer closer.Close()
		}
		if client != nil {
			catalogServer.WithSearchFallback(client)
			catalogServer.WithPosterStandIn(client, store)
			catalogServer.WithTrailers(client, store, oembed)
			logger.Info("catalog search falls back to tmdb when nothing matches", "tmdb_rate", cfg.TMDBRatePerSecond)
		} else {
			// Stored trailers are still served; nothing new is looked up.
			catalogServer.WithTrailers(nil, store, oembed)
			logger.Info("tmdb search fallback is off", "reason", "no TMDB_API_KEY or TMDB_ACCESS_TOKEN")
		}
		server.WithCatalog(catalogServer)
		server.WithHealth(api.Dependency{Name: "postgres", Ping: store.Ping})
		// What a scraper reads. Without it every shared link previews as
		// the generic card, whatever movie it opens.
		meta = store.MovieMeta
		// And the card itself. Its assets are read once, here, so a
		// missing font is a process that will not start rather than a
		// link that will not unfurl.
		cards, err := newOGServer(store, logger.With("component", "og"))
		if err != nil {
			return err
		}
		og = cards
		logger.Info("serving from the catalog", "db_max_conns", cfg.APIMaxConns)

		// Keeping the catalog up to date runs here too, unless
		// something else is doing it. Starting the app on an empty
		// database should leave a working map behind it rather than a
		// 503 and a second command to go and find.
		//
		// It never blocks a request: the import runs behind the server,
		// which answers "still being built" until there is something to
		// serve. And it is safe to have both this and a separate
		// importer — the attempt is held under an advisory lock, and
		// whoever loses it exits.
		if cfg.EmbeddedImporter {
			// The chat the jobs report to. Closed on the way out, so
			// the last board and anything still queued are sent; the
			// lease has usually done that already, and then this
			// returns at once.
			sink := telegram.Start(ctx, telegram.Config{
				Token:    cfg.TelegramBotToken,
				ChatID:   cfg.TelegramChatID,
				Location: telegram.Zone(cfg.NotifyTimezone, logger),
				Env:      cfg.RailwayEnvironment,
				Commit:   cfg.RailwayCommit,
			}, logger)
			defer notify.Close(sink, 4*time.Second)
			// Its own pool, not the one serving requests. A bulk load
			// and a two-hour poster drain must not sit on the ten
			// connections a search is waiting for.
			if err := (&catalog.Runner{
				DatabaseURL:           cfg.DatabaseURL,
				MaxConns:              cfg.ImporterMaxConns,
				Logger:                logger.With("component", "importer"),
				OMDbKey:               cfg.OMDBAPIKey,
				BackfillRate:          cfg.OMDbBackfillRate,
				PosterWorkers:         cfg.PosterWorkers,
				SynopsisSweepMinVotes: cfg.SynopsisSweepMinVotes,
				// The second chance for titles OMDb has no picture
				// for, the id matcher and the trailers. Optional:
				// without it the catalog still works, with more grey
				// boxes in the long tail.
				TMDbAuth: tmdb.Auth{
					APIKey:      cfg.TMDBAPIKey,
					AccessToken: cfg.TMDBAccessToken,
				},
				// The same limiter the request-path client waits on.
				TMDbLimiter:          limiter,
				TMDbSweepMinVotes:    cfg.TMDbSweepMinVotes,
				TrailerSweepMinVotes: cfg.TrailerSweepMinVotes,
				OEmbed:               oembed,
				Notify:               sink,
			}).Start(ctx); err != nil {
				return err
			}
		} else {
			logger.Info("the catalog is kept up to date elsewhere", "embedded_importer", false)
		}
	} else {
		// Crawls happen inside requests and in the warmer; keep TMDb busy, but
		// only fetch the filmographies the map can use (lead + anchorCostars).
		a, err := app.New(ctx, cfg, 16, 8, logger)
		if err != nil {
			return err
		}
		defer a.Close(context.Background())
		server = api.NewWithLimits(a.Store, a.Crawler, a.TMDB, limits(cfg), logger)
		server.WithHealth(health(a)...)
		server.WithFirstRun(a.Store)
		// Off the startup path: the scan takes a moment and nothing should
		// wait on it, least of all the health check.
		go server.WarmFirstRun(ctx)
		server.StartWarming(ctx, warmWorkers)
	}
	if cfg.Production() {
		// The map reports only when this process does, so a development
		// build served from a LAN address cannot quietly send events.
		server.WithAnalytics(api.AnalyticsConfig{
			Token:         cfg.PostHogToken,
			Host:          cfg.PostHogHost,
			MixpanelToken: cfg.MixpanelToken,
		})
	}
	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           routes(server.Handler(), cfg.WebDir, meta, og, logger),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.APIAddr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// limits applies any environment overrides to the API's defaults.
func limits(cfg config.Config) api.Limits {
	l := api.DefaultLimits
	if cfg.MaxColdCrawls > 0 {
		l.ColdCrawls = cfg.MaxColdCrawls
	}
	return l
}

// searchFallback is the TMDb client a reader's request asks: a missed
// catalog search, a poster stand-in, a trailer nobody has looked up. It
// waits on limiter, the process's one TMDb budget.
//
// No credentials means no client: a title stored as a primary or
// original name is still found, and search does not depend on TMDb
// being up for those.
func searchFallback(ctx context.Context, cfg config.Config, limiter *rate.Limiter, logger *slog.Logger) (*tmdb.Client, io.Closer, error) {
	if cfg.TMDBAPIKey == "" && cfg.TMDBAccessToken == "" {
		return nil, nil, nil
	}
	opts := []tmdb.Option{tmdb.WithLimiter(limiter)}
	var closer io.Closer
	if cfg.RedisURL == "" {
		logger.Info("REDIS_URL not set; TMDb search responses will not be cached")
	} else {
		cache, err := rediscache.New(ctx, cfg.RedisURL, "cinedikt:tmdb", cfg.TMDBCacheTTL)
		if err != nil {
			return nil, nil, fmt.Errorf("tmdb cache: %w", err)
		}
		closer = cache
		opts = append(opts, tmdb.WithCache(cache))
		logger.Info("response cache in Redis", "prefix", "tmdb", "ttl", cfg.TMDBCacheTTL)
	}
	return tmdb.New(tmdb.Auth{APIKey: cfg.TMDBAPIKey, AccessToken: cfg.TMDBAccessToken}, opts...), closer, nil
}

// health adapts the app's dependencies to the API's health check.
func health(a *app.App) []api.Dependency {
	deps := a.Dependencies()
	out := make([]api.Dependency, 0, len(deps))
	for _, d := range deps {
		out = append(out, api.Dependency{Name: d.Name, Ping: d.Ping})
	}
	return out
}

// routes mounts the API at /api and, when webDir is set, the built
// frontend at / (with index.html for any path it does not have, so the
// app's own URLs work on reload). Without webDir the API also answers at /
// so curl examples keep working.
// movieMeta is a read of what a link preview needs: the movie's title,
// its year, and the poster the share card is drawn from. It is a
// function so a test can stand in for the catalog.
type movieMeta func(ctx context.Context, tconst string) (title string, year int, poster string, err error)

func routes(apiHandler http.Handler, webDir string, meta movieMeta, og http.Handler, logger *slog.Logger) http.Handler {
	// Every API handler runs under a deadline, so a stalled dependency
	// ends as a 503 rather than a connection held until the client or
	// the platform gives up.
	apiHandler = withTimeout(apiHandler, requestTimeout)
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", apiHandler))
	if og != nil {
		// Its own budget: a share card fetches a poster and draws, and
		// the scraper waiting for it gave up long before the API's.
		mux.Handle("/og/movie/", withTimeout(og, ogBudget+ogSlotWait))
	}
	if webDir == "" {
		mux.Handle("/", apiHandler)
	} else {
		if _, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil {
			logger.Warn("WEB_DIR has no index.html; build the frontend with `npm run build` in web/", "dir", webDir)
		}
		files := http.FileServer(http.Dir(webDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Maps used to live at /film/. Links to them are out in the
			// world for good, so they are moved rather than served: one
			// address per map, and the one people see is the new one.
			if to, ok := movieRoute(r.URL); ok {
				http.Redirect(w, r, to, http.StatusMovedPermanently)
				return
			}
			p := filepath.Join(webDir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				setWebCache(w, hashedAsset(r.URL.Path))
				files.ServeHTTP(w, r)
				return
			}
			setWebCache(w, false)
			serveIndex(w, r, filepath.Join(webDir, "index.html"), meta)
		})
		logger.Info("serving frontend", "dir", webDir)
	}
	return recoverPanics(mux, logger)
}

// movieRoute is where an old /film/ link should go, query and all.
func movieRoute(u *url.URL) (string, bool) {
	rest, ok := strings.CutPrefix(u.Path, "/film/")
	if !ok || rest == "" {
		return "", false
	}
	to := "/movie/" + rest
	if u.RawQuery != "" {
		to += "?" + u.RawQuery
	}
	return to, true
}

// withTimeout gives each request a deadline its handlers can observe.
func withTimeout(next http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Vite fingerprints JS/CSS under /assets/; those URLs never reuse a
// body, so they can be cached for a year. index.html (and anything else)
// must revalidate so a deploy's new asset hashes are picked up.
const (
	assetCacheControl = "public, max-age=31536000, immutable"
	htmlCacheControl  = "no-cache"
)

func hashedAsset(urlPath string) bool {
	return strings.HasPrefix(urlPath, "/assets/")
}

// ogImagePath is the share card. Link unfurlers refuse a relative URL
// and will not draw SVG, so index.html's og:image and twitter:image
// tags are rewritten to an absolute PNG URL for the host that was
// fetched. Whatever follows the path is kept: the ?v= is how a new card
// reaches an unfurler that is still holding the old one.
const ogImagePath = "/og.png"

var ogImageTag = regexp.MustCompile(`content="` + regexp.QuoteMeta(ogImagePath) + `([^"]*)"`)

// How long a share card may hold up the page. A scraper that waited is
// no better than one that got the generic tags, and a reader behind it
// would be waiting for nothing at all.
const previewTimeout = 300 * time.Millisecond

func serveIndex(w http.ResponseWriter, r *http.Request, path string, meta movieMeta) {
	body, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if origin := requestOrigin(r); origin != "" {
		abs := []byte(`content="` + origin + ogImagePath)
		body = ogImageTag.ReplaceAllFunc(body, func(tag []byte) []byte {
			// The host is already known safe, and the tail is copied
			// across as it stands, so no expansion runs over either.
			return append(append([]byte{}, abs...), tag[len(`content="`+ogImagePath):]...)
		})
		// A relative og:url helps no scraper. The site's own address is
		// the truthful answer until a movie is known, and namePreview
		// replaces it with that movie's canonical one when it is.
		body = setMeta(body, ogURL, origin+"/")
		body = namePreview(r.Context(), body, origin, r.URL.Path, meta)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

var (
	titleTag    = regexp.MustCompile(`<title>[^<]*</title>`)
	metaContent = regexp.MustCompile(`content="[^"]*"`)
)

// metaTag matches one <meta> element by the attribute that names it, so
// a rewrite can only ever land on the tag it was asked for.
func metaTag(attr, key string) *regexp.Regexp {
	return regexp.MustCompile(`<meta\b[^>]*\b` + attr + `="` + regexp.QuoteMeta(key) + `"[^>]*>`)
}

var (
	ogTitle = metaTag("property", "og:title")
	ogDesc  = metaTag("property", "og:description")
	ogURL   = metaTag("property", "og:url")
	ogAlt   = metaTag("property", "og:image:alt")
	ogImage = metaTag("property", "og:image")
	twImage = metaTag("name", "twitter:image")
	metaSum = metaTag("name", "description")
)

// setMeta rewrites that tag's content and nothing else's. The value is
// written literally: a title with a $ in it is a title, not a reference.
func setMeta(body []byte, tag *regexp.Regexp, value string) []byte {
	replacement := []byte(`content="` + html.EscapeString(value) + `"`)
	return tag.ReplaceAllFunc(body, func(m []byte) []byte {
		return metaContent.ReplaceAllLiteral(m, replacement)
	})
}

// namePreview puts the movie's name into the tags a scraper reads. A
// shared link is how this app travels, and "The Matrix — everything its
// cast and directors made" says what it opens where the tagline cannot.
//
// Every way of not knowing — not a movie route, not in the graph, an
// error, or simply too slow — leaves the generic tags alone.
func namePreview(ctx context.Context, body []byte, origin, path string, meta movieMeta) []byte {
	if meta == nil {
		return body
	}
	tconst, ok := movieIDFromPath(path)
	if !ok {
		return body
	}
	title, year, poster, ok := lookUp(ctx, meta, tconst)
	if !ok {
		return body
	}

	named := title + " — everything its cast and directors made"
	heading := title
	if year > 0 {
		heading = fmt.Sprintf("%s (%d)", title, year)
	}
	said := "See every movie " + title + "’s cast and directors made, arranged by year and rating."

	body = titleTag.ReplaceAllLiteral(body, []byte("<title>"+html.EscapeString(named+" · Cinedikt")+"</title>"))
	body = setMeta(body, ogTitle, heading+" — everything its cast and directors made")
	body = setMeta(body, ogDesc, said)
	body = setMeta(body, metaSum, said)
	// The card itself: this movie's poster rather than the site's mark.
	// The version changes whenever the poster, the title or the
	// template does, which is the only way an unfurler that caches by
	// address ever sees a new picture.
	card := fmt.Sprintf("%s/og/movie/%s.png?v=%s", origin, tconst, catalog.OGVersion(poster, title))
	body = setMeta(body, ogImage, card)
	body = setMeta(body, twImage, card)
	if year > 0 {
		body = setMeta(body, ogAlt, fmt.Sprintf("%s (%d) poster, on Cinedikt", title, year))
	} else {
		body = setMeta(body, ogAlt, named)
	}
	// The canonical address, built from the stored title: a link pasted
	// with a stale slug still previews as the one URL this map has.
	body = setMeta(body, ogURL, origin+moviePath(tconst, title))
	return body
}

// lookUp runs the read under the deadline and gives up on it rather than
// waiting, whatever the read itself does about the context.
func lookUp(ctx context.Context, meta movieMeta, tconst string) (string, int, string, bool) {
	ctx, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()
	type found struct {
		title  string
		year   int
		poster string
		err    error
	}
	// Buffered, so a read that outlives the deadline still has somewhere
	// to put its answer and its goroutine ends.
	done := make(chan found, 1)
	go func() {
		title, year, poster, err := meta(ctx, tconst)
		done <- found{title, year, poster, err}
	}()
	select {
	case got := <-done:
		title := strings.TrimSpace(got.title)
		return title, got.year, got.poster, got.err == nil && title != ""
	case <-ctx.Done():
		return "", 0, "", false
	}
}

// movieRoutePath is the address a map has, and the one it used to have.
// It is the same shape the client reads — TCONST in movieParam.ts — so
// the two cannot disagree about what counts as a movie route.
//
// An IMDb title id, not a number: the client has written tconst
// addresses since the catalog replaced the graph, and a numeric pattern
// here matched none of them. Every shared link previewed as the generic
// card for exactly that reason.
var movieRoutePath = regexp.MustCompile(`^/(?:movie|film)/(tt\d{1,17})(?:-[^/]*)?/?$`)

func movieIDFromPath(path string) (string, bool) {
	m := movieRoutePath.FindStringSubmatch(path)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func moviePath(tconst, title string) string {
	if slug := slugify(title); slug != "" {
		return "/movie/" + tconst + "-" + slug
	}
	return "/movie/" + tconst
}

// How long a slug may run before it is cut, matching web/src/movieParam.ts.
const slugMax = 60

// slugify is the Go half of the slug the client writes, kept in step
// with slugify() in web/src/movieParam.ts so og:url names the same
// address the address bar ends up showing.
func slugify(title string) string {
	var folded strings.Builder
	for _, r := range norm.NFKD.String(title) {
		switch {
		case r >= 0x300 && r <= 0x36f: // combining marks, dropped with the accent
		case r == '\'' || r == '’': // an apostrophe closes a word rather than breaking it
		default:
			folded.WriteRune(unicode.ToLower(r))
		}
	}
	var out strings.Builder
	dash := false
	for _, r := range folded.String() {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			out.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(out.String(), "-")
	if len(s) > slugMax {
		s = s[:slugMax]
	}
	return strings.TrimRight(s, "-")
}

// requestOrigin is the public scheme and host. Railway terminates TLS,
// so the scheme comes from X-Forwarded-Proto rather than r.TLS.
func requestOrigin(r *http.Request) string {
	proto := headerFirst(r, "X-Forwarded-Proto")
	if proto != "https" && proto != "http" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := headerFirst(r, "X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if !safeHost(host) {
		return ""
	}
	return proto + "://" + host
}

func headerFirst(r *http.Request, name string) string {
	v := r.Header.Get(name)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func safeHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, c := range host {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == ':':
		default:
			return false
		}
	}
	return true
}

func setWebCache(w http.ResponseWriter, hashed bool) {
	if hashed {
		w.Header().Set("Cache-Control", assetCacheControl)
		return
	}
	w.Header().Set("Cache-Control", htmlCacheControl)
}

// recoverPanics is the API's single global panic boundary. Its error log is
// handled by analytics.Logger, which sends the exception to PostHog.
func recoverPanics(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("unhandled request panic", "error", fmt.Errorf("%v", recovered))
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
