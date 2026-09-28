// Command importer builds the movie catalog from the IMDb datasets.
//
// The API can do this itself, and on a single-service deployment it
// does. This command is for running the work somewhere of its own: a
// worker beside the web service, or a one-off from a terminal.
//
// Either way only one import happens. The attempt is held under a
// Postgres advisory lock, and whoever loses it exits.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"cinedikt/internal/catalog"
	"cinedikt/internal/config"
	"cinedikt/internal/notify"
	"cinedikt/internal/telegram"
	"cinedikt/internal/tmdb"
)

func main() {
	os.Exit(run())
}

// run is the whole command, returning the exit code rather than calling
// os.Exit itself, so the deferred close of the notifier gets to send
// its last message before the process ends.
func run() int {
	once := flag.Bool("once", false, "run a single attempt and exit")
	postersOnly := flag.Bool("posters-only", false, "fill in posters, release dates and synopses against the live catalog, and do not import")
	dir := flag.String("dir", "", "where to keep the downloaded files (default: a temp directory)")
	keep := flag.Bool("keep", false, "leave the downloaded files on disk (for development)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		return 1
	}
	logger := config.NewLogger(slog.LevelInfo).With("component", "importer")
	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL is not set")
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := catalog.OpenForJobs(ctx, cfg.DatabaseURL, cfg.ImporterMaxConns)
	if err != nil {
		logger.Error("open catalog", "err", err)
		return 1
	}
	defer store.Close()

	// Opened after the store, so it is closed before it: the notifier's
	// last save goes through that store. A one-off run sends one quiet
	// summary when it closes, and never touches the board the long
	// running jobs keep.
	sink := telegram.Start(ctx, telegram.Config{
		Token:    cfg.TelegramBotToken,
		ChatID:   cfg.TelegramChatID,
		Location: telegram.Zone(cfg.NotifyTimezone, logger),
		Env:      cfg.RailwayEnvironment,
		Commit:   cfg.RailwayCommit,
		Manual:   *once || *postersOnly,
	}, logger)
	defer notify.Close(sink, 5*time.Second)

	runner := &catalog.Runner{
		// Its own pool already, opened above at ImporterMaxConns. The
		// URL is still needed: the lease lives on a connection of its
		// own, outside any pool.
		Store:                 store,
		DatabaseURL:           cfg.DatabaseURL,
		Logger:                logger,
		Dir:                   *dir,
		OMDbKey:               cfg.OMDBAPIKey,
		BackfillRate:          cfg.OMDbBackfillRate,
		PosterWorkers:         cfg.PosterWorkers,
		SynopsisSweepMinVotes: cfg.SynopsisSweepMinVotes,
		TMDbAuth: tmdb.Auth{
			APIKey:      cfg.TMDBAPIKey,
			AccessToken: cfg.TMDBAccessToken,
		},
		// This process's one TMDb budget. The runner's jobs are its
		// only TMDb callers, and they all wait on it.
		TMDbLimiter:          tmdb.NewLimiter(cfg.TMDBRatePerSecond),
		TMDbSweepMinVotes:    cfg.TMDbSweepMinVotes,
		TrailerSweepMinVotes: cfg.TrailerSweepMinVotes,
		PeopleSweepMinVotes:  cfg.PeopleSweepMinVotes,
		PeopleSweepRate:      cfg.PeopleSweepRate,
		Keep:                 *keep,
		Notify:               sink,
	}

	switch {
	case *postersOnly:
		if err := runner.Posters(ctx); err != nil {
			logger.Error("poster backfill", "err", err)
			return 1
		}
		// And the second chance for what OMDb had nothing for, so one
		// command still leaves the pictures as complete as they get.
		if err := runner.TMDbPosters(ctx); err != nil {
			logger.Error("tmdb poster fallback", "err", err)
			return 1
		}
	case *once:
		if !runner.Once(ctx) {
			return 1
		}
	default:
		if err := runner.Start(ctx); err != nil {
			logger.Error("start the catalog jobs", "err", err)
			return 1
		}
		<-ctx.Done()
		logger.Info("importer stopping")
	}
	return 0
}
