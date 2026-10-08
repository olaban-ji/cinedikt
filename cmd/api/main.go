// Command api serves the movie network over HTTP.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
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
	"cinedikt/internal/catalog"
	"cinedikt/internal/config"
	"cinedikt/internal/geoip"
	"cinedikt/internal/notify"
	"cinedikt/internal/streaming"
	"cinedikt/internal/telegram"
	"cinedikt/internal/tmdb"
)

// Server timeouts. requestTimeout is the most any API handler may take.
// Every call a request makes outside the process has a shorter budget of
// its own, so this is the backstop for a stalled database: a stuck query
// frees the connection rather than holding it until the client or the
// platform gives up. The rest are backstops for clients that stop
// reading.
const (
	requestTimeout    = 40 * time.Second
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
	// shutdownBudget is how long a stopping process gives in-flight
	// requests, and the queue its running jobs. Railway keeps a draining
	// container for ten seconds.
	shutdownBudget = 10 * time.Second
)

// geoipFollow is how often a process looks for a GeoLite2 build another
// process has downloaded. MaxMind publishes twice a week, so a process
// that is a few minutes behind places readers with the old build for
// those few minutes.
const geoipFollow = 10 * time.Minute

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
		Enabled:    cfg.AnalyticsEnabled,
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

	store, err := catalog.Open(ctx, cfg.DatabaseURL, cfg.APIMaxConns)
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer store.Close()
	catalogServer := api.NewCatalogServer(store, logger)
	// The process's one TMDb budget. TMDb counts requests per address, so
	// a reader's lookups and every background job wait on this one limiter
	// rather than each spending a rate of its own.
	limiter := tmdb.NewLimiter(cfg.TMDBRatePerSecond)
	client := searchFallback(cfg, limiter)
	// Trailers and people's photos are only ever read here; their jobs
	// look them up. Without TMDb credentials they cannot, and what they
	// have not reached is "none" rather than an answer on its way.
	catalogServer.WithTrailers(store, client != nil)
	catalogServer.WithPeoplePhotos(store, client != nil)
	if client != nil {
		catalogServer.WithSearchFallback(client)
		catalogServer.WithPosterStandIn(client, store)
		logger.Info("catalog search falls back to tmdb when nothing matches", "tmdb_rate", cfg.TMDBRatePerSecond)
	} else {
		logger.Info("tmdb search fallback is off", "reason", "no TMDB_API_KEY or TMDB_ACCESS_TOKEN")
	}
	// The chat the jobs report to. Started before the queue, whose GeoIP
	// check reports to it too, and which may run a check before the runner
	// below has the lease; the sink keeps what it hears until then. Closed
	// on the way out, so the last board and anything still queued are
	// sent; the lease has usually done that already, and then this returns
	// at once.
	sink := telegram.Start(ctx, telegram.Config{
		Token:    cfg.TelegramBotToken,
		ChatID:   cfg.TelegramChatID,
		Location: telegram.Zone(cfg.NotifyTimezone, logger),
		Env:      cfg.RailwayEnvironment,
	}, logger)
	defer notify.Close(sink, 4*time.Second)
	// Where to watch: answered from what is kept, asked of the Streaming
	// Availability API once per movie and country, and kept right by the
	// queue. Without its key the route answers 503 and the page leaves the
	// section out. The queue is stopped beside the server's shutdown,
	// inside the same draining window.
	var queue *catalog.Queue
	if cfg.StreamingAPIKey != "" {
		q, err := whereToWatch(ctx, cfg, store, catalogServer, sink, logger)
		if err != nil {
			// The rest of the app does not depend on it. A map with no
			// where-to-watch section is better than no map.
			logger.Error("where to watch is off", "err", err)
		}
		if q != nil {
			queue = q
			// On any other way out. Registered after the store's Close, so
			// it runs first: the jobs write through the store. A second
			// Stop does nothing.
			defer func() {
				stopCtx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
				defer cancel()
				q.Stop(stopCtx)
			}()
		}
	} else {
		logger.Info("where to watch is off", "reason", "no STREAMING_API_KEY")
	}
	// Cinedikt Daily is always on: its puzzles, players and games are kept
	// in the same Postgres the catalog is, and the runner below picks the
	// puzzles.
	server := api.New(catalogServer, logger).
		WithHealth(api.Dependency{Name: "postgres", Ping: store.Ping}).
		WithDaily(store)
	// The share card. Its assets are read once, here, so a missing font is
	// a process that will not start rather than a link that will not
	// unfurl.
	cards, err := newOGServer(store, logger.With("component", "og"))
	if err != nil {
		return err
	}
	logger.Info("serving from the catalog", "db_max_conns", cfg.APIMaxConns)

	// Keeping the catalog up to date runs here too. Starting the app on an
	// empty database should leave a working map behind it rather than a
	// 503.
	//
	// It never blocks a request: the import runs behind the server, which
	// answers "still being built" until there is something to serve. And
	// it is safe while a deploy's old and new containers overlap: the jobs
	// run only in the process holding the lease, an advisory lock.
	//
	// Its own pool, not the one serving requests. A bulk load and a
	// two-hour poster drain must not sit on the ten connections a search
	// is waiting for.
	if err := (&catalog.Runner{
		DatabaseURL:           cfg.DatabaseURL,
		MaxConns:              cfg.ImporterMaxConns,
		Logger:                logger.With("component", "importer"),
		OMDbKey:               cfg.OMDBAPIKey,
		BackfillRate:          cfg.OMDbBackfillRate,
		PosterWorkers:         cfg.PosterWorkers,
		SynopsisSweepMinVotes: cfg.SynopsisSweepMinVotes,
		// The second chance for titles OMDb has no picture for, the id
		// matcher, the trailers and people's photos. Optional: without it
		// the catalog still works, with more grey boxes in the long tail.
		TMDbAuth: tmdb.Auth{
			APIKey:      cfg.TMDBAPIKey,
			AccessToken: cfg.TMDBAccessToken,
		},
		// The same limiter the request-path client waits on.
		TMDbLimiter:          limiter,
		TMDbSweepMinVotes:    cfg.TMDbSweepMinVotes,
		TrailerSweepMinVotes: cfg.TrailerSweepMinVotes,
		PeopleSweepMinVotes:  cfg.PeopleSweepMinVotes,
		PeopleSweepRate:      cfg.PeopleSweepRate,
		Notify:               sink,
		// The queue's jobs that report to the same sink, so the board
		// counts them as running here.
		Queued: queue.Jobs(),
	}).Start(ctx); err != nil {
		return err
	}
	// What the page is handed to report with, written into it rather
	// than asked for, so a page with tracking off makes no request for it.
	tracking := pageAnalyticsFor(cfg)
	// store.MovieMeta is what a scraper reads: without it every shared
	// link previews as the generic card, whatever movie it opens.
	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           routes(server.Handler(), cfg.WebDir, store.MovieMeta, cards, tracking, logger),
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
		defer cancel()
		// The queue stops beside the server rather than after it, so
		// both fit inside the draining window.
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			if queue != nil {
				queue.Stop(shutdownCtx)
			}
		}()
		err := srv.Shutdown(shutdownCtx)
		<-stopped
		return err
	}
}

// whereToWatch builds the where-to-watch service, places readers with
// GeoLite2 when there are MaxMind credentials, and starts the queue that
// keeps answers right from the changes feed, refreshes them, and keeps
// the GeoLite2 database current, telling sink how each GeoLite2 check
// ended. The service is handed to the catalog server only once all of
// that works.
func whereToWatch(ctx context.Context, cfg config.Config, store *catalog.Store, cs *api.CatalogServer, sink notify.Sink, logger *slog.Logger) (*catalog.Queue, error) {
	watch := &catalog.WhereToWatch{
		Store:           store,
		API:             streaming.New(cfg.StreamingAPIKey, streaming.WithRate(cfg.StreamingRatePerSecond)),
		Logger:          logger.With("component", "where-to-watch"),
		ChangesMaxPages: cfg.StreamingChangesMaxPages,
	}
	// Nil without MaxMind credentials, so nobody is placed. With them,
	// the route answers 503 to a reader the lookup cannot place until it
	// has a database, rather than telling them their country has no
	// coverage.
	var geo api.CountryLookup
	updater := geoIPFor(cfg, store, logger)
	if updater != nil {
		geo = updater.Lookup
		// What another process, or this one before a restart, already
		// downloaded. The first check, which the queue's leader queues
		// when it is elected, fetches one when there is none.
		if err := updater.LoadStored(ctx); err != nil {
			logger.Warn("load the kept GeoLite2 database", "err", err)
		}
	}
	queue, err := catalog.OpenQueue(ctx, catalog.QueueConfig{
		DatabaseURL:  cfg.DatabaseURL,
		Logger:       logger.With("component", "queue"),
		WhereToWatch: watch,
		GeoIP:        updater,
		Notify:       sink,
	})
	if err != nil {
		return nil, err
	}
	if err := queue.Start(ctx); err != nil {
		queue.Stop(context.Background())
		return nil, fmt.Errorf("start the queue: %w", err)
	}
	if updater != nil {
		go updater.Follow(ctx, geoipFollow)
	}
	cs.WithWhereToWatch(watch, geo)
	logger.Info("where to watch is on", "streaming_rate", cfg.StreamingRatePerSecond,
		"changes_max_pages", cfg.StreamingChangesMaxPages, "geoip", updater != nil)
	return queue, nil
}

// geoIPFor is the GeoLite2 updater for cfg's MaxMind credentials, with
// an empty lookup for it to fill, or nil when there are none and readers
// are not placed in a country. MaxMind's download takes the account id
// with the license key, so a key on its own is a mistake, and is said as
// one rather than left to fail at the first check.
func geoIPFor(cfg config.Config, store geoip.Store, logger *slog.Logger) *geoip.Updater {
	switch {
	case cfg.MaxMindLicenseKey == "":
		logger.Info("geoip is off; readers are not placed in a country", "reason", "no MAXMIND_LICENSE_KEY")
		return nil
	case cfg.MaxMindAccountID == "":
		logger.Warn("geoip is off; readers are not placed in a country",
			"reason", "MAXMIND_LICENSE_KEY is set without MAXMIND_ACCOUNT_ID, and MaxMind takes the two together")
		return nil
	}
	return &geoip.Updater{
		AccountID:  cfg.MaxMindAccountID,
		LicenseKey: cfg.MaxMindLicenseKey,
		Store:      store,
		Lookup:     &geoip.Lookup{},
		Logger:     logger.With("component", "geoip"),
	}
}

// searchFallback is the TMDb client a reader's request asks: a missed
// catalog search, and a poster stand-in. It waits on limiter, the
// process's one TMDb budget.
//
// No credentials means no client: a title stored as a primary or
// original name is still found, and search does not depend on TMDb
// being up for those.
func searchFallback(cfg config.Config, limiter *rate.Limiter) *tmdb.Client {
	if cfg.TMDBAPIKey == "" && cfg.TMDBAccessToken == "" {
		return nil
	}
	return tmdb.New(tmdb.Auth{APIKey: cfg.TMDBAPIKey, AccessToken: cfg.TMDBAccessToken}, tmdb.WithLimiter(limiter))
}

// pageAnalytics is what the page needs to report to PostHog and to
// Mixpanel. An empty token turns that service off in the page. Both
// tokens are write-only keys, the same class of credential the SDKs would
// otherwise be built with; handing them over here is what lets each
// environment point at its own projects through its variables, with one
// build for all of them.
type pageAnalytics struct {
	Token         string `json:"token"`
	Host          string `json:"host"`
	MixpanelToken string `json:"mixpanel_token,omitempty"`
}

// pageAnalyticsFor is what the page is handed to report with, or nil. The
// map reports only when this process does, so a development build served
// from a LAN address cannot quietly send events, and with the switch off
// the page is handed nothing and loads neither tracker.
func pageAnalyticsFor(cfg config.Config) *pageAnalytics {
	if !cfg.Production() || !cfg.AnalyticsEnabled {
		return nil
	}
	return &pageAnalytics{
		Token:         cfg.PostHogToken,
		Host:          cfg.PostHogHost,
		MixpanelToken: cfg.MixpanelToken,
	}
}

// analyticsMetaName names the tag the page reads its trackers from, in
// web/src/analytics.ts.
const analyticsMetaName = "cinedikt-analytics"

// analyticsMeta is the tag that hands the page its trackers' settings, or
// nil when there is nothing to hand it: tracking off, or on with neither
// token set. Without the tag the page loads neither tracker and asks for
// nothing. The JSON is escaped for an attribute, so no token or host can
// end the attribute, or the tag, early.
func analyticsMeta(a *pageAnalytics) []byte {
	if a == nil || (a.Token == "" && a.MixpanelToken == "") {
		return nil
	}
	// A struct of strings always marshals.
	content, _ := json.Marshal(a)
	return []byte(`<meta name="` + analyticsMetaName + `" content="` + html.EscapeString(string(content)) + `" />`)
}

// headEnd is where the trackers' tag goes, once every share-tag rewrite
// is done, so none of them can ever match it.
var headEnd = []byte("</head>")

// withAnalyticsMeta puts tag at the end of the page's head. A page with
// no head is left as it is, and reports nothing.
func withAnalyticsMeta(body, tag []byte) []byte {
	if len(tag) == 0 {
		return body
	}
	i := bytes.Index(body, headEnd)
	if i < 0 {
		return body
	}
	out := make([]byte, 0, len(body)+len(tag))
	out = append(out, body[:i]...)
	out = append(out, tag...)
	return append(out, body[i:]...)
}

// movieMeta is a read of what a link preview needs: the movie's title,
// its year, and the poster the share card is drawn from. It is a
// function so a test can stand in for the catalog.
type movieMeta func(ctx context.Context, tconst string) (title string, year int, poster string, err error)

// routes mounts the API at /api and, when webDir is set, the built
// frontend at / (with index.html for any path it does not have, so the
// app's own URLs work on reload). Without webDir the API also answers at /
// so curl examples keep working. tracking, when it is not nil and holds a
// token, is written into every index.html served.
func routes(apiHandler http.Handler, webDir string, meta movieMeta, og http.Handler, tracking *pageAnalytics, logger *slog.Logger) http.Handler {
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
		// Built once: the settings do not change while the process runs.
		analyticsTag := analyticsMeta(tracking)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			p := filepath.Join(webDir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				setWebCache(w, hashedAsset(r.URL.Path))
				files.ServeHTTP(w, r)
				return
			}
			setWebCache(w, false)
			serveIndex(w, r, filepath.Join(webDir, "index.html"), meta, analyticsTag)
		})
		logger.Info("serving frontend", "dir", webDir)
	}
	return recoverPanics(mux, logger)
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

// serveIndex writes index.html with its share tags made absolute and, on
// a movie's route, the About page or Daily, named for that page, and with
// analyticsTag, when there is one, at the end of its head.
func serveIndex(w http.ResponseWriter, r *http.Request, path string, meta movieMeta, analyticsTag []byte) {
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
		// replaces it with that movie's canonical one when it is, as
		// nameAbout does with the About page's.
		body = setMeta(body, ogURL, origin+"/")
		body = namePreview(r.Context(), body, origin, r.URL.Path, meta)
		body = nameAbout(body, origin, r.URL.Path)
		body = nameDaily(body, origin, r.URL.Path)
	}
	body = withAnalyticsMeta(body, analyticsTag)
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
// Every way of not knowing — not a movie route, not in the catalog, an
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

// aboutTitle is the About page's title, in a preview of its link and in
// its tab. ABOUT_TITLE in web/src/movieParam.ts is the tab's, and the
// two must agree.
const aboutTitle = "About · Cinedikt"

// aboutPath is the About page's one address. It is also reached at
// /about/, as isAboutPath in web/src/movieParam.ts reads it.
const aboutPath = "/about"

// nameAbout names the About page in the tags a scraper reads: its title,
// and its address without the slash however it was reached. The
// description and the share card stay the site's own: the page is about
// the site.
func nameAbout(body []byte, origin, path string) []byte {
	if path != aboutPath && path != aboutPath+"/" {
		return body
	}
	body = titleTag.ReplaceAllLiteral(body, []byte("<title>"+html.EscapeString(aboutTitle)+"</title>"))
	body = setMeta(body, ogTitle, aboutTitle)
	body = setMeta(body, ogURL, origin+aboutPath)
	return body
}

// dailyTitle is Cinedikt Daily's title in its tab. DAILY_TITLE in
// web/src/movieParam.ts is the page's own, and the two must agree.
const dailyTitle = "Daily · Cinedikt"

// dailyPath is Daily's one address. It is also reached at /daily/, as
// isDailyPath in web/src/movieParam.ts reads it.
const dailyPath = "/daily"

// What a preview of Daily's link says. The card stays the site's own:
// a picture of the day's map would give the answer away.
const (
	dailyShareTitle = "Cinedikt Daily: whose map is it?"
	dailyShareText  = "One hidden movie a day. Every movie on its map shares an actor or director with it."
)

// nameDaily names Daily in the tags a scraper reads: its tab title, a
// title and a line for the preview of a shared link, and its address
// without the slash however it was reached.
func nameDaily(body []byte, origin, path string) []byte {
	if path != dailyPath && path != dailyPath+"/" {
		return body
	}
	body = titleTag.ReplaceAllLiteral(body, []byte("<title>"+html.EscapeString(dailyTitle)+"</title>"))
	body = setMeta(body, ogTitle, dailyShareTitle)
	body = setMeta(body, ogDesc, dailyShareText)
	body = setMeta(body, ogURL, origin+dailyPath)
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

// movieRoutePath is the address a map has, the same shape
// movieIdFromPath in web/src/movieParam.ts reads, so the two cannot
// disagree about what counts as a movie route.
//
// An IMDb title id, not a number: the client writes tconst addresses,
// and a numeric pattern here would match none of them, so every shared
// link would preview as the generic card.
var movieRoutePath = regexp.MustCompile(`^/movie/(tt\d{1,17})(?:-[^/]*)?/?$`)

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
