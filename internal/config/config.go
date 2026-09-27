// Package config loads runtime settings from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
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
	// TMDBRatePerSecond caps outgoing TMDb requests; 0 means the client default.
	TMDBRatePerSecond float64
	// OMDBAPIKey enables IMDb rating lookups; empty disables them.
	OMDBAPIKey string
	// PostHog reporting. Only read in production.
	PostHogToken string
	PostHogHost  string
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
)

func Load() (Config, error) {
	// A missing .env is fine; the environment alone may be complete.
	_ = godotenv.Load()

	ttl, err := time.ParseDuration(envOr("TMDB_CACHE_TTL", "168h"))
	if err != nil {
		return Config{}, fmt.Errorf("config: TMDB_CACHE_TTL: %w", err)
	}

	tmdbRate, err := envFloat("TMDB_RATE_PER_SEC")
	if err != nil {
		return Config{}, err
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
		RedisURL:           os.Getenv("REDIS_URL"),
		Neo4jURI:           envOr("NEO4J_URI", "bolt://localhost:7687"),
		Neo4jUser:          envOr("NEO4J_USER", "neo4j"),
		Neo4jPassword:      os.Getenv("NEO4J_PASSWORD"),
		APIAddr:            listenAddr(),
		WebDir:             os.Getenv("WEB_DIR"),

		CrawlThresholdBase: base,
		CrawlOrderPenalty:  penalty,

		MaxColdCrawls: coldCrawls,
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
