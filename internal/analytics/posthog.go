// Package analytics owns the process-wide PostHog client.
package analytics

import (
	"fmt"
	"log/slog"

	"github.com/posthog/posthog-go"
)

var client posthog.Client

// Config is what this process needs to report analytics. Production is
// the only environment that sends anything: a developer's clicks and
// stack traces are not product data.
type Config struct {
	// Production turns reporting on. Everything else runs with a nil
	// client, so captures and error reports are cheap no-ops.
	Production bool
	// Enabled is the switch for every tracker (ANALYTICS_ENABLED). Off,
	// production runs with a nil client too, with its token left in
	// place for the day it is turned back on.
	Enabled bool
	Token   string
	Host    string
}

// Init configures the single PostHog client for this process.
//
// Missing configuration is never fatal. Whether the process starts is a
// question about Neo4j, TMDb and a port; analytics is telemetry, and a
// service that refuses to boot because it cannot report on itself is
// worse than one that boots quietly unreported. A production process
// without configuration says so loudly instead.
func Init(cfg Config, logger *slog.Logger) error {
	if !cfg.Production {
		if cfg.Token != "" {
			logger.Info("analytics disabled outside production", "posthog_configured", true)
		}
		return nil
	}
	if !cfg.Enabled {
		logger.Info("analytics switched off; set ANALYTICS_ENABLED=true to report", "posthog_configured", cfg.Token != "")
		return nil
	}
	switch {
	case cfg.Token == "":
		logger.Warn("POSTHOG_PROJECT_TOKEN is unset in production; " +
			"product events and error reports will be dropped")
		return nil
	case cfg.Host == "":
		logger.Warn("POSTHOG_HOST is unset in production; " +
			"product events and error reports will be dropped")
		return nil
	}

	configuredClient, err := posthog.NewWithConfig(cfg.Token, posthog.Config{Endpoint: cfg.Host})
	if err != nil {
		return fmt.Errorf("configure PostHog client: %w", err)
	}
	client = configuredClient
	logger.Info("analytics enabled", "host", cfg.Host)
	return nil
}

// Client returns the process-wide PostHog client, or nil when analytics is
// not configured or not enabled in this environment.
func Client() posthog.Client {
	return client
}

// Logger wraps the application logger so warnings and errors are reported to
// PostHog error tracking. The service identifier is stable for this process
// and avoids attaching request or user PII to operational exceptions.
func Logger(logger *slog.Logger, distinctID string) *slog.Logger {
	if client == nil {
		return logger
	}
	return slog.New(newSlogHandler(logger.Handler(), distinctID))
}

// Close flushes queued events during graceful process shutdown.
func Close() error {
	if client == nil {
		return nil
	}
	return client.Close()
}
