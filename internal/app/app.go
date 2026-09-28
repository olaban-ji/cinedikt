// Package app wires the TMDb client, OMDb client, graph store and crawler
// from a Config, for both commands.
package app

import (
	"context"
	"io"
	"log/slog"
	"time"

	"cinedikt/internal/config"
	"cinedikt/internal/crawl"
	"cinedikt/internal/graph"
	"cinedikt/internal/omdb"
	"cinedikt/internal/rediscache"
	"cinedikt/internal/tmdb"
)

// App holds the built components.
type App struct {
	TMDB    *tmdb.Client
	Store   *graph.Store
	Crawler *crawl.Crawler

	// cache is the TMDb response cache, kept for health checks; nil when
	// REDIS_URL is unset.
	cache   *rediscache.Cache
	closers []io.Closer
}

// responseCache is what both API clients need from a cache.
type responseCache interface {
	Get(key string) ([]byte, bool)
	Set(key string, body []byte) error
}

// Dependency is a backing service and a cheap way to ask whether it is
// answering, for the API's health check.
type Dependency struct {
	Name string
	Ping func(ctx context.Context) error
}

// omdbCacheTTL: IMDb ratings move slowly and the OMDb quota is small.
const omdbCacheTTL = 30 * 24 * time.Hour

// New builds everything, connects to Neo4j and ensures its schema.
func New(ctx context.Context, cfg config.Config, concurrency, maxPeoplePerMovie int, logger *slog.Logger) (*App, error) {
	a := &App{}
	// The one TMDb client this path has, on the same budget every other
	// process gets: TMDB_RATE_PER_SEC with a small fixed burst.
	tmdbOpts := []tmdb.Option{tmdb.WithLimiter(tmdb.NewLimiter(cfg.TMDBRatePerSecond))}
	if cfg.RedisURL == "" {
		logger.Info("REDIS_URL not set; TMDb/OMDb responses will not be cached")
	} else {
		tmdbCache, err := a.openCache(ctx, cfg, "tmdb", cfg.TMDBCacheTTL, logger)
		if err != nil {
			return nil, err
		}
		tmdbOpts = append(tmdbOpts, tmdb.WithCache(tmdbCache))
	}
	client := tmdb.New(tmdb.Auth{APIKey: cfg.TMDBAPIKey, AccessToken: cfg.TMDBAccessToken}, tmdbOpts...)

	var ratings crawl.RatingSource
	if cfg.OMDBAPIKey != "" {
		var omdbOpts []omdb.Option
		if cfg.RedisURL != "" {
			omdbCache, err := a.openCache(ctx, cfg, "omdb", omdbCacheTTL, logger)
			if err != nil {
				a.Close(ctx)
				return nil, err
			}
			omdbOpts = append(omdbOpts, omdb.WithCache(omdbCache))
		}
		ratings = omdb.New(cfg.OMDBAPIKey, omdbOpts...)
	} else {
		logger.Info("OMDB_API_KEY not set; movies will carry TMDb ratings only")
	}

	store, err := graph.Open(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPassword)
	if err != nil {
		a.Close(ctx)
		return nil, err
	}
	if err := store.EnsureSchema(ctx); err != nil {
		store.Close(ctx)
		a.Close(ctx)
		return nil, err
	}

	crawler := crawl.New(client, store, crawl.Options{
		Concurrency:       concurrency,
		MaxPeoplePerMovie: maxPeoplePerMovie,
		// The grid is built from a film's whole cast and all its
		// directors, and shows every film they made, so those
		// filmographies have to be there whatever TMDb's popularity
		// says today.
		ExpandAllCast: true,
		Scoring:       scoring(cfg),
		Ratings:       ratings,
		Logger:        logger,
	})
	a.TMDB, a.Store, a.Crawler = client, store, crawler
	return a, nil
}

// openCache connects to Redis. The prefix keeps the two clients' keys apart
// in a shared instance.
func (a *App) openCache(ctx context.Context, cfg config.Config, prefix string, ttl time.Duration, logger *slog.Logger) (responseCache, error) {
	c, err := rediscache.New(ctx, cfg.RedisURL, "cinedikt:"+prefix, ttl)
	if err != nil {
		return nil, err
	}
	if a.cache == nil {
		a.cache = c
	}
	a.closers = append(a.closers, c)
	logger.Info("response cache in Redis", "prefix", prefix, "ttl", ttl)
	return c, nil
}

// Dependencies are the backing services this process needs to serve a
// request. Redis appears only when it is configured: without it the
// service is slower, not broken.
func (a *App) Dependencies() []Dependency {
	deps := []Dependency{{Name: "neo4j", Ping: a.Store.Ping}}
	if a.cache != nil {
		deps = append(deps, Dependency{Name: "redis", Ping: a.cache.Ping})
	}
	return deps
}

// Close releases the Neo4j driver and any Redis connections.
func (a *App) Close(ctx context.Context) error {
	var err error
	if a.Store != nil {
		err = a.Store.Close(ctx)
	}
	for _, c := range a.closers {
		if cerr := c.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

// scoring applies any env overrides on top of the crawler's defaults.
func scoring(cfg config.Config) crawl.Scoring {
	sc := crawl.DefaultScoring
	if cfg.CrawlThresholdBase > 0 {
		sc.ThresholdBase = cfg.CrawlThresholdBase
	}
	if cfg.CrawlOrderPenalty > 0 {
		sc.OrderPenalty = cfg.CrawlOrderPenalty
	}
	return sc
}
