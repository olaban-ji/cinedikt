package config

import (
	"context"
	"log/slog"
	"testing"
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

func TestACatalogIsEnoughToStart(t *testing.T) {
	// A clean deployment sets a database and nothing else. Requiring
	// TMDb or Neo4j credentials — which nothing reads any more — would
	// stop it before it began.
	for _, v := range []string{"TMDB_API_KEY", "TMDB_ACCESS_TOKEN", "NEO4J_PASSWORD", "OMDB_API_KEY", "REDIS_URL"} {
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

func TestWithoutACatalogTheOldCredentialsAreStillRequired(t *testing.T) {
	for _, v := range []string{"DATABASE_URL", "TMDB_API_KEY", "TMDB_ACCESS_TOKEN", "NEO4J_PASSWORD"} {
		t.Setenv(v, "")
	}
	if _, err := Load(); err == nil {
		t.Error("an environment with nothing configured was accepted")
	}
}

func TestTheImporterRunsInTheAPIUnlessTurnedOff(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/cinedikt")

	// The common case is one service, and one command should bring up a
	// working map. So it is on without being asked for.
	t.Setenv("EMBEDDED_IMPORTER", "")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.EmbeddedImporter {
		t.Error("the embedded importer is off by default")
	}

	// And off for a deployment with a worker of its own.
	t.Setenv("EMBEDDED_IMPORTER", "false")
	got, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.EmbeddedImporter {
		t.Error("EMBEDDED_IMPORTER=false did not turn it off")
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
	t.Setenv("RAILWAY_GIT_COMMIT_SHA", "d0170089f00")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyTimezone != "Africa/Lagos" || cfg.RailwayEnvironment != "dev" || cfg.RailwayCommit != "d0170089f00" {
		t.Fatalf("got %q %q %q", cfg.NotifyTimezone, cfg.RailwayEnvironment, cfg.RailwayCommit)
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
