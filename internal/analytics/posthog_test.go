package analytics

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestInitOutsideProductionDoesNotReport(t *testing.T) {
	t.Cleanup(func() { client = nil })
	client = nil

	logger := discardLogger()
	cfg := Config{Production: false, Token: "phc_test", Host: "https://us.i.posthog.com"}
	if err := Init(cfg, logger); err != nil {
		t.Fatalf("Init() = %v, want nil", err)
	}
	if Client() != nil {
		t.Fatal("Client() != nil: development must not send events")
	}
	if got := Logger(logger, "cinedikt-api"); got != logger {
		t.Fatal("Logger() wrapped a client that should not exist")
	}
	if err := Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

// Missing analytics configuration must never stop the service from
// starting, whatever the log level: a port, a database and an API key are
// what a boot depends on.
func TestInitWithoutConfigIsNeverFatal(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"development, nothing set", Config{}},
		{"production, nothing set", Config{Production: true, Enabled: true}},
		{"production, token without host", Config{Production: true, Enabled: true, Token: "phc_test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { client = nil })
			client = nil
			if err := Init(tc.cfg, discardLogger()); err != nil {
				t.Fatalf("Init() = %v, want nil", err)
			}
			if Client() != nil {
				t.Fatal("Client() != nil without full configuration")
			}
		})
	}
}

func TestInitWarnsWhenProductionIsUnconfigured(t *testing.T) {
	t.Cleanup(func() { client = nil })
	client = nil
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	if err := Init(Config{Production: true, Enabled: true}, logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "POSTHOG_PROJECT_TOKEN") {
		t.Errorf("no warning about missing configuration; log = %q", buf.String())
	}
}

func TestInitInProductionCreatesAClient(t *testing.T) {
	t.Cleanup(func() {
		if client != nil {
			client.Close()
		}
		client = nil
	})
	client = nil

	cfg := Config{Production: true, Enabled: true, Token: "phc_test", Host: "https://us.i.posthog.com"}
	if err := Init(cfg, discardLogger()); err != nil {
		t.Fatalf("Init() = %v, want nil", err)
	}
	if Client() == nil {
		t.Fatal("Client() = nil, want a configured client")
	}
	logger := discardLogger()
	if got := Logger(logger, "cinedikt-api"); got == logger {
		t.Fatal("Logger() did not wrap a configured client")
	}
}

// TestInitSwitchedOffDoesNotReport: with the switch off, production
// with a token and a host still runs with no client, so nothing is sent
// and error reports go nowhere.
func TestInitSwitchedOffDoesNotReport(t *testing.T) {
	t.Cleanup(func() { client = nil })
	client = nil

	cfg := Config{Production: true, Enabled: false, Token: "phc_test", Host: "https://us.i.posthog.com"}
	if err := Init(cfg, discardLogger()); err != nil {
		t.Fatalf("Init() = %v, want nil", err)
	}
	if Client() != nil {
		t.Fatal("Client() is configured with analytics switched off")
	}
	logger := discardLogger()
	if got := Logger(logger, "cinedikt-api"); got != logger {
		t.Fatal("Logger() wrapped the logger with analytics switched off")
	}
}
