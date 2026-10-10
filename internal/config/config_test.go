package config

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestListenAddr(t *testing.T) {
	t.Setenv("API_ADDR", "")
	t.Setenv("PORT", "")
	if got := listenAddr(); got != ":8080" {
		t.Fatalf("listenAddr() = %q, want :8080", got)
	}

	t.Setenv("PORT", "9090")
	if got := listenAddr(); got != ":9090" {
		t.Fatalf("PORT=9090 listenAddr() = %q, want :9090", got)
	}

	t.Setenv("PORT", ":7070")
	if got := listenAddr(); got != ":7070" {
		t.Fatalf("PORT=:7070 listenAddr() = %q, want :7070", got)
	}

	t.Setenv("API_ADDR", ":6060")
	t.Setenv("PORT", "9090")
	if got := listenAddr(); got != ":6060" {
		t.Fatalf("API_ADDR wins: listenAddr() = %q, want :6060", got)
	}
}

func TestEnvironment(t *testing.T) {
	cases := map[string]struct {
		want    string
		wantErr bool
	}{
		"":            {EnvDevelopment, false},
		"development": {EnvDevelopment, false},
		"dev":         {EnvDevelopment, false},
		"production":  {EnvProduction, false},
		"PRODUCTION":  {EnvProduction, false},
		"prod":        {EnvProduction, false},
		" prod ":      {EnvProduction, false},
		"staging":     {"", true},
		"prodction":   {"", true},
	}
	for value, tc := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("APP_ENV", value)
			got, err := environment()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("APP_ENV=%q: error = nil; a misspelt environment must not silently downgrade", value)
				}
				return
			}
			if err != nil {
				t.Fatalf("APP_ENV=%q: %v", value, err)
			}
			if got != tc.want {
				t.Errorf("APP_ENV=%q gave %q, want %q", value, got, tc.want)
			}
		})
	}
}

func TestProduction(t *testing.T) {
	if (Config{Environment: EnvProduction}).Production() != true {
		t.Error("production config does not report Production()")
	}
	if (Config{Environment: EnvDevelopment}).Production() != false {
		t.Error("development config reports Production()")
	}
}

func TestNewLoggerFormatsForTheEnvironment(t *testing.T) {
	t.Run("production writes single-line JSON a collector can read", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		h := NewLogger(slog.LevelInfo).Handler()
		if _, ok := h.(*slog.JSONHandler); !ok {
			t.Errorf("handler = %T, want *slog.JSONHandler", h)
		}
	})
	t.Run("development stays readable text", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		h := NewLogger(slog.LevelInfo).Handler()
		if _, ok := h.(*slog.TextHandler); !ok {
			t.Errorf("handler = %T, want *slog.TextHandler", h)
		}
	})
	t.Run("an unset or unreadable APP_ENV is not production", func(t *testing.T) {
		t.Setenv("APP_ENV", "")
		if _, ok := NewLogger(slog.LevelInfo).Handler().(*slog.TextHandler); !ok {
			t.Error("unset APP_ENV should log as development")
		}
		t.Setenv("APP_ENV", "nonsense")
		if _, ok := NewLogger(slog.LevelInfo).Handler().(*slog.TextHandler); !ok {
			t.Error("an unreadable APP_ENV should log as development")
		}
	})
	t.Run("respects the level it is given", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		if NewLogger(slog.LevelDebug).Enabled(context.Background(), slog.LevelDebug) != true {
			t.Error("debug logger does not log at debug")
		}
		if NewLogger(slog.LevelInfo).Enabled(context.Background(), slog.LevelDebug) != false {
			t.Error("info logger logs at debug")
		}
	})
}

// TestTheDailysToolsAreOnOutsideProductionOrWhenAsked: a checkout has
// them; a deployed environment has them only with DAILY_DEV_TOOLS set to
// "true", so dev can keep production's logs and analytics and still have
// Play again.
func TestTheDailysToolsAreOnOutsideProductionOrWhenAsked(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/cinedikt")
	for _, c := range []struct {
		env, tools string
		want       bool
	}{
		{"", "", true},
		{"development", "false", true},
		{"production", "", false},
		{"production", "false", false},
		{"production", "yes", false},
		{"production", "true", true},
		{"production", " TRUE ", true},
	} {
		t.Setenv("APP_ENV", c.env)
		t.Setenv("DAILY_DEV_TOOLS", c.tools)
		got, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if got.DailyDev() != c.want {
			t.Errorf("APP_ENV=%q DAILY_DEV_TOOLS=%q: tools on %v, want %v", c.env, c.tools, got.DailyDev(), c.want)
		}
	}
}

// TestTheDailysLaunchIsADate: DAILY_LAUNCH is a day, read as UTC, and
// anything but a date stops the start rather than launching on the
// wrong day; unset is no date at all.
func TestTheDailysLaunchIsADate(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/cinedikt")
	for _, c := range []struct {
		value string
		want  time.Time
		bad   bool
	}{
		{"", time.Time{}, false},
		{"2026-10-12", time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), false},
		{" 2026-10-12 ", time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), false},
		{"12/10/2026", time.Time{}, true},
		{"2026-13-01", time.Time{}, true},
		{"Monday", time.Time{}, true},
	} {
		t.Setenv("DAILY_LAUNCH", c.value)
		got, err := Load()
		if c.bad {
			if err == nil || !strings.Contains(err.Error(), "DAILY_LAUNCH") {
				t.Errorf("DAILY_LAUNCH=%q: %v, want a refusal naming it", c.value, err)
			}
			continue
		}
		if err != nil || !got.DailyLaunch.Equal(c.want) {
			t.Errorf("DAILY_LAUNCH=%q: %v, %v; want %v", c.value, got.DailyLaunch, err, c.want)
		}
	}
}

func TestACatalogIsEnoughToStart(t *testing.T) {
	// A clean deployment sets a database and nothing else. Every other
	// service is optional, and requiring its credentials would stop the
	// deployment before it began.
	for _, v := range []string{"TMDB_API_KEY", "TMDB_ACCESS_TOKEN", "OMDB_API_KEY", "STREAMING_API_KEY", "MAXMIND_LICENSE_KEY"} {
		t.Setenv(v, "")
	}
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/cinedikt")
	got, err := Load()
	if err != nil {
		t.Fatalf("a catalog-only environment was refused: %v", err)
	}
	if got.DatabaseURL == "" {
		t.Error("DATABASE_URL was not read")
	}
}

// TestADatabaseIsRequired: every map is read from the catalog, so a
// process without one refuses to start rather than serving nothing,
// whatever else is set.
func TestADatabaseIsRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("TMDB_API_KEY", "k")
	_, err := Load()
	if err == nil {
		t.Fatal("an environment without DATABASE_URL was accepted")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error %q does not name DATABASE_URL", err)
	}
}

func TestTelegramIsReadAndOptional(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/cinedikt")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramBotToken != "" || cfg.TelegramChatID != "" {
		t.Fatal("unset telegram settings were not empty")
	}

	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("TELEGRAM_CHAT_ID", "42")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramBotToken != "token" || cfg.TelegramChatID != "42" {
		t.Fatalf("telegram settings = %q %q", cfg.TelegramBotToken, cfg.TelegramChatID)
	}
}

// TestNotificationsKnowTheZoneAndTheDeploy is what the board's times
// and footer are written from.
func TestNotificationsKnowTheZoneAndTheDeploy(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("NOTIFY_TIMEZONE", " Africa/Lagos ")
	t.Setenv("RAILWAY_ENVIRONMENT_NAME", "dev")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyTimezone != "Africa/Lagos" || cfg.RailwayEnvironment != "dev" {
		t.Fatalf("got %q %q", cfg.NotifyTimezone, cfg.RailwayEnvironment)
	}
}

// TestSweepFloorKeepsAnExplicitZero is the difference between "fetch a
// picture for the well-known ones" and "fetch one for everything TMDb
// has". Zero is a real answer here, not a missing variable.
func TestSweepFloorKeepsAnExplicitZero(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")

	t.Setenv("TMDB_SWEEP_MIN_VOTES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TMDbSweepMinVotes != DefaultTMDbSweepMinVotes {
		t.Errorf("unset = %d, want the default %d", cfg.TMDbSweepMinVotes, DefaultTMDbSweepMinVotes)
	}

	t.Setenv("TMDB_SWEEP_MIN_VOTES", "0")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TMDbSweepMinVotes != 0 {
		t.Errorf("an explicit 0 came back as %d; the whole catalog cannot be asked for", cfg.TMDbSweepMinVotes)
	}
}

// TestTheTMDbRateIsTheProcessBudget: unset takes half of TMDb's per-address
// ceiling, and anything below one a second is refused rather than left
// to stall every lookup a reader waits on.
func TestTheTMDbRateIsTheProcessBudget(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")

	t.Setenv("TMDB_RATE_PER_SEC", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TMDBRatePerSecond != DefaultTMDbRatePerSecond || DefaultTMDbRatePerSecond != 20 {
		t.Errorf("unset = %v, want 20", cfg.TMDBRatePerSecond)
	}

	t.Setenv("TMDB_RATE_PER_SEC", "1")
	if cfg, err = Load(); err != nil || cfg.TMDBRatePerSecond != 1 {
		t.Errorf("1 gave %v, %v", cfg.TMDBRatePerSecond, err)
	}

	for _, bad := range []string{"0", "0.5", "-3", "fast", "NaN", "Inf"} {
		t.Setenv("TMDB_RATE_PER_SEC", bad)
		if _, err := Load(); err == nil {
			t.Errorf("TMDB_RATE_PER_SEC=%s was accepted", bad)
		}
	}
}

func TestTheSynopsisAndTrailerFloorsHaveDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("SYNOPSIS_SWEEP_MIN_VOTES", "")
	t.Setenv("TRAILER_SWEEP_MIN_VOTES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// Both sweeps cover every film by default. The trailer endpoint only
	// reads, so its job has to reach every film a reader can open, and
	// a synopsis is worth having for every film a map can draw.
	if cfg.SynopsisSweepMinVotes != 0 || cfg.TrailerSweepMinVotes != 0 {
		t.Errorf("defaults = %d and %d, want 0 and 0", cfg.SynopsisSweepMinVotes, cfg.TrailerSweepMinVotes)
	}

	// Each is still a dial.
	t.Setenv("SYNOPSIS_SWEEP_MIN_VOTES", "1000")
	t.Setenv("TRAILER_SWEEP_MIN_VOTES", "250")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SynopsisSweepMinVotes != 1000 || cfg.TrailerSweepMinVotes != 250 {
		t.Errorf("set = %d and %d, want 1000 and 250", cfg.SynopsisSweepMinVotes, cfg.TrailerSweepMinVotes)
	}

	t.Setenv("TRAILER_SWEEP_MIN_VOTES", "-1")
	if _, err := Load(); err == nil {
		t.Error("a negative floor was accepted")
	}
}

// TestThePeopleSweepHasAFloorAndAPace: the sweep covers everyone a map
// can show by default, at a quarter of the default TMDb budget, and a
// pace that would stop it, or take the budget's cap away, is refused.
func TestThePeopleSweepHasAFloorAndAPace(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("PEOPLE_SWEEP_MIN_VOTES", "")
	t.Setenv("PEOPLE_SWEEP_RATE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PeopleSweepMinVotes != 0 || cfg.PeopleSweepRate != 5 {
		t.Errorf("defaults = %d and %v, want 0 and 5", cfg.PeopleSweepMinVotes, cfg.PeopleSweepRate)
	}

	t.Setenv("PEOPLE_SWEEP_MIN_VOTES", "500")
	t.Setenv("PEOPLE_SWEEP_RATE", "2.5")
	if cfg, err = Load(); err != nil || cfg.PeopleSweepMinVotes != 500 || cfg.PeopleSweepRate != 2.5 {
		t.Errorf("set = %d and %v (%v), want 500 and 2.5", cfg.PeopleSweepMinVotes, cfg.PeopleSweepRate, err)
	}

	t.Setenv("PEOPLE_SWEEP_MIN_VOTES", "-1")
	if _, err := Load(); err == nil {
		t.Error("a negative floor was accepted")
	}
	t.Setenv("PEOPLE_SWEEP_MIN_VOTES", "")
	for _, bad := range []string{"0", "-1", "slow", "NaN", "Inf"} {
		t.Setenv("PEOPLE_SWEEP_RATE", bad)
		if _, err := Load(); err == nil {
			t.Errorf("PEOPLE_SWEEP_RATE=%s was accepted", bad)
		}
	}
}

// TestWhereToWatchIsReadAndOptional: without a Streaming Availability key
// the feature is off and the process still starts; the rate has a small
// default, and one that would never ask, or would not limit, is refused.
func TestWhereToWatchIsReadAndOptional(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	for _, v := range []string{"STREAMING_API_KEY", "STREAMING_RATE_PER_SEC", "STREAMING_CHANGES_MAX_PAGES", "MAXMIND_LICENSE_KEY", "MAXMIND_ACCOUNT_ID"} {
		t.Setenv(v, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StreamingAPIKey != "" || cfg.MaxMindLicenseKey != "" || cfg.MaxMindAccountID != "" {
		t.Errorf("unset settings came back as %q %q %q", cfg.StreamingAPIKey, cfg.MaxMindLicenseKey, cfg.MaxMindAccountID)
	}
	if cfg.StreamingRatePerSecond != DefaultStreamingRatePerSecond || DefaultStreamingRatePerSecond != 5 {
		t.Errorf("rate = %v, want 5", cfg.StreamingRatePerSecond)
	}

	t.Setenv("STREAMING_API_KEY", " streaming-key ")
	t.Setenv("STREAMING_RATE_PER_SEC", "0.5")
	t.Setenv("MAXMIND_LICENSE_KEY", "licence")
	t.Setenv("MAXMIND_ACCOUNT_ID", "42")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if cfg.StreamingAPIKey != "streaming-key" || cfg.StreamingRatePerSecond != 0.5 ||
		cfg.MaxMindLicenseKey != "licence" || cfg.MaxMindAccountID != "42" {
		t.Errorf("set = %+v", cfg)
	}

	for _, bad := range []string{"0", "-1", "fast", "NaN", "Inf"} {
		t.Setenv("STREAMING_RATE_PER_SEC", bad)
		if _, err := Load(); err == nil {
			t.Errorf("STREAMING_RATE_PER_SEC=%s was accepted", bad)
		}
	}
}

// TestTheChangesJobHasAPageCap: forty pages a country by default, any
// positive number when set, and never none, which would not be a limit
// but the job switched off.
func TestTheChangesJobHasAPageCap(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("STREAMING_CHANGES_MAX_PAGES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StreamingChangesMaxPages != DefaultStreamingChangesMaxPages || DefaultStreamingChangesMaxPages != 40 {
		t.Errorf("unset = %d, want 40", cfg.StreamingChangesMaxPages)
	}
	t.Setenv("STREAMING_CHANGES_MAX_PAGES", "12")
	if cfg, err = Load(); err != nil || cfg.StreamingChangesMaxPages != 12 {
		t.Errorf("12 = %d, %v", cfg.StreamingChangesMaxPages, err)
	}
	for _, bad := range []string{"0", "-1", "many", "2.5"} {
		t.Setenv("STREAMING_CHANGES_MAX_PAGES", bad)
		if _, err := Load(); err == nil {
			t.Errorf("STREAMING_CHANGES_MAX_PAGES=%s was accepted", bad)
		}
	}
}

// TestAnalyticsIsOffUnlessSwitchedOn: every tracker stays off, whatever
// tokens are set, until ANALYTICS_ENABLED says "true".
func TestAnalyticsIsOffUnlessSwitchedOn(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("POSTHOG_PROJECT_TOKEN", "phc_test")
	t.Setenv("MIXPANEL_PROJECT_TOKEN", "mp_test")
	for _, c := range []struct {
		value string
		want  bool
	}{
		{"", false},
		{"false", false},
		{"0", false},
		{"yes", false},
		{"true", true},
		{" TRUE ", true},
	} {
		t.Setenv("ANALYTICS_ENABLED", c.value)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AnalyticsEnabled != c.want {
			t.Errorf("ANALYTICS_ENABLED=%q: enabled = %v, want %v", c.value, cfg.AnalyticsEnabled, c.want)
		}
	}
}

// TestARateOutOfBoundsSaysWhatItMustBe: the error names the variable and
// its bound, inclusive or not.
func TestARateOutOfBoundsSaysWhatItMustBe(t *testing.T) {
	t.Setenv("SOME_RATE", "0.5")
	if _, err := envRateOr("SOME_RATE", 20, 1, true); err == nil || err.Error() != "config: SOME_RATE must be a number of at least 1, got 0.5" {
		t.Errorf("at least: %v", err)
	}
	t.Setenv("SOME_RATE", "1")
	if v, err := envRateOr("SOME_RATE", 20, 1, true); err != nil || v != 1 {
		t.Errorf("the floor itself: %v, %v", v, err)
	}
	t.Setenv("SOME_RATE", "0")
	if _, err := envRateOr("SOME_RATE", 5, 0, false); err == nil || err.Error() != "config: SOME_RATE must be a number greater than 0, got 0" {
		t.Errorf("greater than: %v", err)
	}
	t.Setenv("SOME_RATE", "Inf")
	if _, err := envRateOr("SOME_RATE", 5, 0, false); err == nil {
		t.Error("an infinite rate was accepted")
	}
	t.Setenv("SOME_RATE", "")
	if v, err := envRateOr("SOME_RATE", 5, 0, false); err != nil || v != 5 {
		t.Errorf("unset: %v, %v", v, err)
	}
}
