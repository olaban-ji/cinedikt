package telegram

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartIsSilentWithoutBothSettings(t *testing.T) {
	if Start(context.Background(), Config{}, nil) != nil {
		t.Fatal("an unconfigured sink was started")
	}
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if Start(context.Background(), Config{Token: "token"}, logger) != nil {
		t.Fatal("a token with no chat was started")
	}
	if !strings.Contains(buf.String(), "must both be set") {
		t.Fatalf("log = %q, want the reason both settings are required", buf.String())
	}
}

func TestTheTokenDoesNotAppearInALoggedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("no hijacker")
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	var buf syncBuffer
	s := newSink(context.Background(), Config{Token: "secret-token", ChatID: "1"}, slog.New(slog.NewTextHandler(&buf, nil)))
	s.base = srv.URL
	s.sleep = func(time.Duration) {}
	s.Note(dbDown())
	s.gatherBy = time.Time{}
	s.absorb()
	s.enqueue()
	for i := 0; i < maxAttempts; i++ {
		for _, m := range s.outbox {
			m.notBefore = time.Time{}
		}
		s.step()
	}
	if strings.Contains(buf.String(), "secret-token") {
		t.Fatalf("the token was logged: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "telegram") {
		t.Fatalf("log = %q, want the failure recorded", buf.String())
	}
}

func TestZoneFallsBackToUTCAndSaysWhy(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if Zone("", logger) != nil {
		t.Error("an unset zone was not nil")
	}
	if Zone("Mars/Olympus_Mons", logger) != nil || !strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("a bad zone: log = %q, want a warning and UTC", buf.String())
	}
	if loc := Zone("Africa/Lagos", logger); loc == nil || loc.String() != "Africa/Lagos" {
		t.Errorf("Zone = %v", loc)
	}
}
