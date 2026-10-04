// Package config loads runtime settings from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Environments this service knows about. Which one is running decides
// whether analytics reports anything; nothing else infers it from the
// shape of a URL or a log level.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Config holds every setting shared by the crawler and the API.
type Config struct {
	// Environment is EnvDevelopment or EnvProduction, from APP_ENV. It is
	// explicit on purpose: guessing it from a hostname means a production
	// deploy that happens to reach its database over localhost silently
	// stops reporting.
	Environment string

	TMDBAPIKey      string
	TMDBAccessToken string
	// TMDBCacheTTL is the Redis TTL for raw TMDb responses.
	TMDBCacheTTL time.Duration
	// TMDBRatePerSecond is the whole process's TMDb budget, in requests
	// a second: every client in the process waits on one limiter at this
	// rate. TMDb counts about 40 a second per address, not per key.
	TMDBRatePerSecond float64
	// OMDBAPIKey enables IMDb rating lookups; empty disables them.
	OMDBAPIKey string
	// PostHog reporting. Only read in production.
	PostHogToken string
	PostHogHost  string
	// MixpanelToken is the Mixpanel project the map reports to. Like
	// PostHog's, it is only served in production, and empty means the
	// page never loads Mixpanel at all.
	MixpanelToken string
	// AnalyticsEnabled is the switch for every tracker: PostHog and
	// Mixpanel in the page, and PostHog's error reports from the server.
	// Off unless ANALYTICS_ENABLED is "true", so the tokens can stay set
	// while nothing is tracked.
	AnalyticsEnabled bool
	// RedisURL, if set, holds the TMDb/OMDb response cache.
	RedisURL string

	Neo4jURI      string
	Neo4jUser     string
	Neo4jPassword string

	// --- catalog ---

	// DatabaseURL is the Postgres the catalog lives in. The importer
	// writes it; the API only reads.
	DatabaseURL string
	// APIMaxConns and ImporterMaxConns keep the two processes in their
	// own lane: a bulk COPY must not take the connections a reader needs.
	APIMaxConns      int32
	ImporterMaxConns int32
	// EmbeddedImporter runs the importer inside the API, so one
	// command brings up a working map on an empty database. Turn it off
	// where a separate worker does the importing; two of them is safe
	// but pointless, since the advisory lock means only one works.
	EmbeddedImporter bool

	// OMDbBackfillRate is how fast the poster job asks OMDb, in requests
	// a second, and PosterWorkers is how many run at once. The rate is
	// the dial; the workers are what make it reachable, since one
	// lookup at a time is bounded by the round trip instead.
	OMDbBackfillRate float64
	PosterWorkers    int

	// TMDbSweepMinVotes is the vote floor for the TMDb poster fallback's
	// sweep. Zero sweeps every title.
	TMDbSweepMinVotes int
	// SynopsisSweepMinVotes is the vote floor for asking OMDb for a
	// synopsis nobody has met yet. Zero, the default, sweeps every film;
	// a film a reader has been shown is asked about whatever its votes.
	SynopsisSweepMinVotes int
	// TrailerSweepMinVotes is the vote floor for the trailer job's
	// sweep. Zero, the default, sweeps every film; a film a reader opens
	// is looked up whatever its votes.
	TrailerSweepMinVotes int
	// PeopleSweepMinVotes is the vote floor for the people job's sweep,
	// on each person's best known film a map can show. Zero, the
	// default, sweeps everyone; the people on a map a reader opens are
	// looked up whatever their votes.
	PeopleSweepMinVotes int
	// PeopleSweepRate is how fast the people job's sweep and re-asks go,
	// in lookups a second, inside the process's TMDb budget. The people
	// on a map a reader opens skip it.
	PeopleSweepRate float64

	// --- where to watch ---

	// StreamingAPIKey is the Streaming Availability API's key, issued by
	// Movie of the Night. Empty turns where to watch off: the route
	// answers 503 and the page leaves the section out.
	StreamingAPIKey string
	// StreamingRatePerSecond is how many requests a second the process
	// makes to that API.
	StreamingRatePerSecond float64
	// StreamingChangesMaxPages is the most pages of that API's changes
	// feed the daily changes job reads for one country in one run. A run
	// it stops carries on from there the next day.
	StreamingChangesMaxPages int
	// MaxMindLicenseKey downloads GeoLite2 Country, which places a
	// reader's address in a country. Empty leaves every address
	// unplaced, so only the trusted CDN header (GeoCountryHeader) places
	// a reader.
	MaxMindLicenseKey string
	// MaxMindAccountID goes with the license key. Set, the database is
	// downloaded from MaxMind's current address with both; unset, from
	// the older one that takes the key alone. It is not a secret.
	MaxMindAccountID string
	// GeoCountryHeader names the one country header to trust, the one a
	// CDN in front of the app sets, such as CF-IPCountry. Empty trusts
	// none: Railway's edge sets no such header, so one arriving there was
	// written by the reader.
	GeoCountryHeader string

	// TelegramBotToken and TelegramChatID turn on job notifications.
	// Both empty leaves them off. The token is the bot's, from
	// BotFather; the chat id is the private chat or group it posts into.
	TelegramBotToken string
	TelegramChatID   string
	// NotifyTimezone is the IANA zone the notifications write times in,
	// such as Africa/Lagos. Empty writes them in UTC and says so.
	NotifyTimezone string
	// RailwayEnvironment and RailwayCommit are what Railway injects
	// about the running deploy. The notifications name it with them, so
	// a board can be matched to the deploy that wrote it.
	RailwayEnvironment string
	RailwayCommit      string

	// Crawl scoring; zero values mean the crawler's defaults.
	CrawlThresholdBase float64
	CrawlOrderPenalty  float64

	APIAddr string
	// WebDir, if set, is a built frontend (web/dist) served at / with the
	// API under /api.
	WebDir string

	// MaxColdCrawls caps simultaneous first-visit crawls on the old graph
	// path. Zero takes the API's default. A catalog request never crawls.
	MaxColdCrawls int
}

// Load reads a .env file if present, then the environment.
// Catalog defaults. These are the rate limits the design was reasoned
// about; every one is overridable, and the reasoning is here so a change
// is a decision rather than a guess.
const (
	// DefaultOMDbBackfillRate is how fast the poster job asks OMDb, on a
	// plan with no request limit. There are about 757,000 movies in the
	// dump, so a full first pass is roughly 757000/rate seconds: about
	// twenty-five minutes at this rate.
	//
	// It is not guarding a quota. What it bounds is how many sockets
	// this process holds open and how fast rows arrive at Postgres
	// behind it.
	DefaultOMDbBackfillRate = 500.0

	// DefaultPosterWorkers is how many lookups are in flight.
	//
	// This is what decides whether the rate above is reachable at all:
	// the ceiling is workers/latency however high the rate is set. A
	// round trip to OMDb measured at roughly 400ms, so 256 in flight is
	// about 600/s — enough headroom that the rate stays in charge.
	//
	// The writes are no longer part of that sum. They are batched, so a
	// few hundred titles share one statement and the connection pool
	// stopped being the real ceiling.
	DefaultPosterWorkers = 256

	// Connection pools, kept apart so a bulk COPY cannot take the
	// connections a reader needs.
	DefaultAPIMaxConns      = 10
	DefaultImporterMaxConns = 4

	// DefaultTMDbSweepMinVotes is how well known a movie has to be
	// before TMDb is asked for a picture nobody has wanted yet.
	//
	// Three hundred thousand titles in the catalog have no poster and
	// about thirteen hundred of them have this many votes. The rest are
	// still repaired the moment somebody opens one — the sweep is only
	// about what is fetched ahead of being asked for.
	//
	// Set TMDB_SWEEP_MIN_VOTES=0 to fetch a picture for every title
	// TMDb has one for. That is a few hours of a rate-limited API,
	// once.
	DefaultTMDbSweepMinVotes = 100

	// DefaultTMDbRatePerSecond is the process's TMDb budget: half of the
	// roughly 40 a second TMDb takes from one address, which leaves room
	// for the second container during a deploy. It is the total for
	// every client in the process, not each one's.
	DefaultTMDbRatePerSecond = 20.0

	// DefaultSynopsisSweepMinVotes is how well known a film has to be
	// before OMDb is asked for a synopsis nobody has met yet. Zero is
	// every film, behind the ones readers have met. Below a higher floor
	// a synopsis is asked for the first time a card for the film is
	// drawn.
	DefaultSynopsisSweepMinVotes = 0

	// DefaultTrailerSweepMinVotes is how well known a film has to be
	// for the trailer job's sweep to reach it. Zero is every film: the
	// endpoint only reads, so a film the sweep leaves out is looked up
	// only once a reader opens it, and that reader waits on the job.
	DefaultTrailerSweepMinVotes = 0

	// DefaultPeopleSweepMinVotes is how well known a person's best known
	// film has to be for the people job's sweep to reach them. Zero is
	// everyone a map can show: a photo is read, never looked up, when a
	// map opens, so a person the sweep leaves out has none until the job
	// has answered the mark that map left.
	DefaultPeopleSweepMinVotes = 0

	// DefaultPeopleSweepRate is the pace of the people job's sweep and
	// re-asks: a quarter of the default TMDb budget. The sweep is over a
	// million people long, and at the whole budget it would keep that
	// budget spent for most of a day, with every reader's trailer and
	// photos queued behind it.
	DefaultPeopleSweepRate = 5.0

	// DefaultStreamingRatePerSecond is how fast the process asks the
	// Streaming Availability API. It is metered per request, and what
	// keeps that bill down is that every answer is kept and only the
	// movies readers open are asked about; the rate only spreads out a
	// burst of first asks, which a reader is waiting on.
	DefaultStreamingRatePerSecond = 5.0

	// DefaultStreamingChangesMaxPages caps the changes job's read of one
	// country at a thousand changes a day, 25 to a page. Every page is a
	// metered request, whether or not anything on it is kept. A country
	// that reaches it is logged as a warning, and its next run carries on
	// from where the cap stopped it.
	DefaultStreamingChangesMaxPages = 40
)

func Load() (Config, error) {
	// A missing .env is fine; the environment alone may be complete.
	_ = godotenv.Load()

	ttl, err := time.ParseDuration(envOr("TMDB_CACHE_TTL", "168h"))
	if err != nil {
		return Config{}, fmt.Errorf("config: TMDB_CACHE_TTL: %w", err)
	}

	tmdbRate, err := envFloatOr("TMDB_RATE_PER_SEC", DefaultTMDbRatePerSecond)
	if err != nil {
		return Config{}, err
	}
	// Below one a second is not a budget but a stall: every lookup a
	// reader waits on would queue behind the jobs for seconds. NaN and
	// infinity parse as numbers but pass no comparison, and an infinite
	// rate would take the process's TMDb limit away altogether.
	if math.IsNaN(tmdbRate) || math.IsInf(tmdbRate, 0) || tmdbRate < 1 {
		return Config{}, fmt.Errorf("config: TMDB_RATE_PER_SEC must be a number of at least 1, got %v", tmdbRate)
	}
	base, err := envFloat("CRAWL_THRESHOLD_BASE")
	if err != nil {
		return Config{}, err
	}
	penalty, err := envFloat("CRAWL_ORDER_PENALTY")
	if err != nil {
		return Config{}, err
	}

	coldCrawls, err := envInt("MAX_COLD_CRAWLS")
	if err != nil {
		return Config{}, err
	}

	// --- catalog ---
	//
	// The defaults are the values the design was reasoned about with;
	// each one is a deliberate choice rather than a round number, and
	// each can be overridden per environment.
	backfillRate, err := envFloatOr("OMDB_BACKFILL_RATE", DefaultOMDbBackfillRate)
	if err != nil {
		return Config{}, err
	}
	posterWorkers, err := envIntOr("POSTER_WORKERS", DefaultPosterWorkers)
	if err != nil {
		return Config{}, err
	}
	// On unless it is explicitly turned off: the common case is one
	// service, and it should just work.
	embedded := envOr("EMBEDDED_IMPORTER", "true") != "false"
	apiConns, err := envIntOr("CATALOG_API_MAX_CONNS", DefaultAPIMaxConns)
	if err != nil {
		return Config{}, err
	}
	importerConns, err := envIntOr("CATALOG_IMPORTER_MAX_CONNS", DefaultImporterMaxConns)
	if err != nil {
		return Config{}, err
	}
	// An explicit 0 is kept: it is how the whole catalog is asked for.
	sweepVotes, err := envIntOr("TMDB_SWEEP_MIN_VOTES", DefaultTMDbSweepMinVotes)
	if err != nil {
		return Config{}, err
	}
	synopsisVotes, err := envIntOr("SYNOPSIS_SWEEP_MIN_VOTES", DefaultSynopsisSweepMinVotes)
	if err != nil {
		return Config{}, err
	}
	trailerVotes, err := envIntOr("TRAILER_SWEEP_MIN_VOTES", DefaultTrailerSweepMinVotes)
	if err != nil {
		return Config{}, err
	}
	peopleVotes, err := envIntOr("PEOPLE_SWEEP_MIN_VOTES", DefaultPeopleSweepMinVotes)
	if err != nil {
		return Config{}, err
	}
	peopleRate, err := envFloatOr("PEOPLE_SWEEP_RATE", DefaultPeopleSweepRate)
	if err != nil {
		return Config{}, err
	}
	// Zero would stop the sweep for good rather than turn a limit off,
	// and an infinite rate would hand the sweep the whole TMDb budget
	// this exists to keep it inside.
	if math.IsNaN(peopleRate) || math.IsInf(peopleRate, 0) || peopleRate <= 0 {
		return Config{}, fmt.Errorf("config: PEOPLE_SWEEP_RATE must be a number greater than 0, got %v", peopleRate)
	}

	streamingRate, err := envFloatOr("STREAMING_RATE_PER_SEC", DefaultStreamingRatePerSecond)
	if err != nil {
		return Config{}, err
	}
	// Zero would never ask, which is what an unset key is for, and an
	// infinite rate would take away the one thing spreading a burst out.
	if math.IsNaN(streamingRate) || math.IsInf(streamingRate, 0) || streamingRate <= 0 {
		return Config{}, fmt.Errorf("config: STREAMING_RATE_PER_SEC must be a number greater than 0, got %v", streamingRate)
	}
	changesPages, err := envIntOr("STREAMING_CHANGES_MAX_PAGES", DefaultStreamingChangesMaxPages)
	if err != nil {
		return Config{}, err
	}
	// Zero would read no changes at all, and answers would go on as they
	// were kept until they were 30 days old: not a limit but the job
	// switched off, and an unset key already switches off the feature.
	if changesPages <= 0 {
		return Config{}, fmt.Errorf("config: STREAMING_CHANGES_MAX_PAGES must be greater than 0, got %d", changesPages)
	}

	env, err := environment()
	if err != nil {
		return Config{}, err
	}

	c := Config{
		Environment:        env,
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		APIMaxConns:        int32(apiConns),
		ImporterMaxConns:   int32(importerConns),
		EmbeddedImporter:   embedded,
		OMDbBackfillRate:   backfillRate,
		PosterWorkers:      posterWorkers,
		TMDbSweepMinVotes:  sweepVotes,
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:     os.Getenv("TELEGRAM_CHAT_ID"),
		NotifyTimezone:     strings.TrimSpace(os.Getenv("NOTIFY_TIMEZONE")),
		RailwayEnvironment: os.Getenv("RAILWAY_ENVIRONMENT_NAME"),
		RailwayCommit:      os.Getenv("RAILWAY_GIT_COMMIT_SHA"),
		TMDBAPIKey:         os.Getenv("TMDB_API_KEY"),
		TMDBAccessToken:    os.Getenv("TMDB_ACCESS_TOKEN"),
		TMDBCacheTTL:       ttl,
		TMDBRatePerSecond:  tmdbRate,
		OMDBAPIKey:         os.Getenv("OMDB_API_KEY"),
		PostHogToken:       os.Getenv("POSTHOG_PROJECT_TOKEN"),
		PostHogHost:        envOr("POSTHOG_HOST", "https://us.i.posthog.com"),
		MixpanelToken:      strings.TrimSpace(os.Getenv("MIXPANEL_PROJECT_TOKEN")),
		AnalyticsEnabled:   strings.EqualFold(strings.TrimSpace(os.Getenv("ANALYTICS_ENABLED")), "true"),
		RedisURL:           os.Getenv("REDIS_URL"),
		Neo4jURI:           envOr("NEO4J_URI", "bolt://localhost:7687"),
		Neo4jUser:          envOr("NEO4J_USER", "neo4j"),
		Neo4jPassword:      os.Getenv("NEO4J_PASSWORD"),
		APIAddr:            listenAddr(),
		WebDir:             os.Getenv("WEB_DIR"),

		CrawlThresholdBase: base,
		CrawlOrderPenalty:  penalty,

		MaxColdCrawls: coldCrawls,

		SynopsisSweepMinVotes: synopsisVotes,
		TrailerSweepMinVotes:  trailerVotes,
		PeopleSweepMinVotes:   peopleVotes,
		PeopleSweepRate:       peopleRate,

		StreamingAPIKey:          strings.TrimSpace(os.Getenv("STREAMING_API_KEY")),
		StreamingRatePerSecond:   streamingRate,
		StreamingChangesMaxPages: changesPages,
		MaxMindLicenseKey:        strings.TrimSpace(os.Getenv("MAXMIND_LICENSE_KEY")),
		MaxMindAccountID:         strings.TrimSpace(os.Getenv("MAXMIND_ACCOUNT_ID")),
		GeoCountryHeader:         strings.TrimSpace(os.Getenv("GEO_COUNTRY_HEADER")),
	}
	// A catalog is all either process needs. TMDb and Neo4j belong to
	// the crawling map that the catalog replaced, and requiring their
	// credentials would stop a clean deployment — one with nothing set
	// but a database — from starting at all.
	if c.DatabaseURL == "" {
		if c.TMDBAPIKey == "" && c.TMDBAccessToken == "" {
			return Config{}, errors.New("config: set DATABASE_URL, or TMDB_API_KEY/TMDB_ACCESS_TOKEN for the old crawling map")
		}
		if c.Neo4jPassword == "" {
			return Config{}, errors.New("config: set DATABASE_URL, or NEO4J_PASSWORD for the old crawling map")
		}
	}
	return c, nil
}

// Production reports whether this process is serving real traffic.
func (c Config) Production() bool { return c.Environment == EnvProduction }

// NewLogger builds the logger for this environment. In production it
// writes single-line JSON to stdout, which is what a log collector reads:
// Railway, for one, turns anything on stderr into an error, so plain text
// there makes every served request look like a failure and buries the
// real ones. JSON also hands `method`, `path`, `status` and the rest over
// as queryable fields rather than a string to grep. Locally it stays
// human-readable text on stderr, where a person is reading it.
//
// It takes the environment from APP_ENV directly, because a process needs
// a logger before it has finished loading its configuration — and before
// it can report that the configuration is wrong.
func NewLogger(level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	if env, err := environment(); err == nil && env == EnvProduction {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// environment reads APP_ENV. Unset means development, so a forgotten
// variable is quiet rather than chatty; a misspelt one is an error rather
// than a silent downgrade.
func environment() (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))); v {
	case "":
		return EnvDevelopment, nil
	case "dev", EnvDevelopment:
		return EnvDevelopment, nil
	case "prod", EnvProduction:
		return EnvProduction, nil
	default:
		return "", fmt.Errorf("config: APP_ENV=%q: want %q or %q", v, EnvDevelopment, EnvProduction)
	}
}

// envFloat parses an optional float variable; unset means 0.
func envFloat(key string) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return f, nil
}

// envInt parses an optional integer variable; unset means 0.
func envInt(key string) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return n, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// listenAddr prefers API_ADDR, then Railway/Fly-style PORT, then :8080.
func listenAddr() string {
	if v := os.Getenv("API_ADDR"); v != "" {
		return v
	}
	if p := os.Getenv("PORT"); p != "" {
		if strings.HasPrefix(p, ":") {
			return p
		}
		return ":" + p
	}
	return ":8080"
}

// envFloatOr reads a float, falling back when the variable is unset. An
// explicit 0 is kept: it is how a limit is deliberately turned off.
func envFloatOr(key string, fallback float64) (float64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("config: %s must not be negative", key)
	}
	return v, nil
}

// envIntOr reads an int, falling back when the variable is unset.
func envIntOr(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("config: %s must not be negative", key)
	}
	return v, nil
}
