package catalog

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/notify"
	"cinedikt/internal/omdb"
)

// cutOff is an answer that stops part way through, so it is not JSON
// however it is repaired. The poster address in it must not be used.
const cutOff = `{"Response":"True","Poster":"https://x/cut.jpg","Plot":"This answer stops mid-`

// omdbServer is OMDb as a test server, behind the real client: each id
// gets the body given for it, and any other gets OMDb's answer for an id
// it does not hold. It records what it was asked.
type omdbServer struct {
	mu    sync.Mutex
	asked []string
}

func (o *omdbServer) askedFor() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.asked)
}

func serveOMDb(t *testing.T, bodies map[string]string) (*omdb.Client, *omdbServer) {
	t.Helper()
	rec := &omdbServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("i")
		rec.mu.Lock()
		rec.asked = append(rec.asked, id)
		rec.mu.Unlock()
		body, ok := bodies[id]
		if !ok {
			body = `{"Response":"False","Error":"Error getting data."}`
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return omdb.New("key", omdb.WithBaseURL(srv.URL), omdb.WithRateLimit(1000, 1000)), rec
}

// logLines is a logger whose output a test can read.
type logLines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logLines) logger() *slog.Logger { return slog.New(slog.NewTextHandler(l, nil)) }

// about is every line that names the title.
func (l *logLines) about(tconst string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if strings.Contains(line, "tconst="+tconst) {
			out = append(out, line)
		}
	}
	return out
}

// TestAnUnreadableAnswerIsNoSynopsis: an answer that cannot be read even
// once repaired is OMDb having no synopsis to give. It is recorded as
// that, is not a failed lookup, and is not asked again; TMDb's overview
// fills in for it the way it does for any movie OMDb has none for.
func TestAnUnreadableAnswerIsNoSynopsis(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	clearSynopses(t, s)
	posterPassHas(t, s, map[string]string{"tt0111161": "ok", "tt0133093": "ok"})
	client, server := serveOMDb(t, map[string]string{
		"tt0111161": cutOff,
		// Broken, but repairable: this one has a plot.
		"tt0133093": `{"Response":"True","Poster":"N/A","Plot":"Red\pill, blue\tpill."}`,
	})
	var sink recordingSink
	var logs logLines
	job := &SynopsisJob{Store: s, Client: client, Logger: logs.logger(), MinVotes: 0, Batch: 10, Workers: 1, Notify: &sink}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tt0111161", "tt0133093"}; !slices.Equal(server.askedFor(), want) {
		t.Fatalf("asked %v, want %v", server.askedFor(), want)
	}
	if text, source, ok := synopsisRow(t, s, "tt0111161"); !ok || text != "" || source != "omdb" {
		t.Errorf("unreadable = %q from %q (row %v), want OMDb's null", text, source, ok)
	}
	if !omdbAnswered(t, s, "tt0111161") {
		t.Error("an unreadable answer was not recorded as OMDb having answered")
	}
	if text, _, _ := synopsisRow(t, s, "tt0133093"); text != `Red\pill, blue pill.` {
		t.Errorf("repaired plot = %q", text)
	}
	// One synopsis found, one answer with none, and nothing failed.
	if fin := sink.of(notify.Finished); len(fin) != 1 || fin[0].Done != 1 || fin[0].None != 1 || fin[0].Errors != 0 {
		t.Errorf("finished = %+v, want 1 found, 1 none and no failures", fin)
	}
	lines := logs.about("tt0111161")
	if len(lines) != 1 || !strings.Contains(lines[0], "level=INFO") || !strings.Contains(lines[0], "unexpected end of JSON input") {
		t.Errorf("logged %q, want one Info line with the decode error", lines)
	}

	// The next pass, with the queue refilled, does not ask it again.
	job.refilled = time.Time{}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := server.askedFor(); len(got) != 2 {
		t.Errorf("asked again: %v", got[2:])
	}

	// TMDb's overview is shown where OMDb has none, and OMDb's answer
	// still stands.
	if err := s.keepTMDbOverview(ctx, "tt0111161", "TMDb's overview."); err != nil {
		t.Fatal(err)
	}
	if text, source, _ := synopsisRow(t, s, "tt0111161"); text != "TMDb's overview." || source != "tmdb" {
		t.Errorf("after TMDb = %q from %q", text, source)
	}
	if !omdbAnswered(t, s, "tt0111161") {
		t.Error("TMDb's overview erased OMDb's answer")
	}
}

// TestAnUnreadableAnswerIsNoPoster: the poster pass records it the way
// it records OMDb having no picture, status ok with no address, so it is
// not retried every day and the TMDb stand-in's queue takes it up.
func TestAnUnreadableAnswerIsNoPoster(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	publishFixture(t, s)
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.posters`); err != nil {
		t.Fatal(err)
	}
	clearSynopses(t, s)
	client, server := serveOMDb(t, map[string]string{
		"tt0133093": cutOff,
		"tt0111161": "{\"Response\":\"True\",\"Poster\":\"https://x/shawshank.jpg\",\"Released\":\"23 Sep 1994\",\"Plot\":\"Hope\tfloats.\"}",
	})
	var sink recordingSink
	var logs logLines
	job := &PosterJob{Store: s, Client: client, Logger: logs.logger(), Batch: 100, Workers: 1, Notify: &sink}
	if err := job.Run(ctx, Live); err != nil {
		t.Fatal(err)
	}

	var url *string
	var status string
	if err := s.pool.QueryRow(ctx,
		`SELECT poster_url, status FROM meta.posters WHERE tconst = 'tt0133093'`).Scan(&url, &status); err != nil {
		t.Fatal(err)
	}
	if url != nil || status != "ok" {
		t.Errorf("unreadable = %v %q, want no address and ok", url, status)
	}
	if text, source, ok := synopsisRow(t, s, "tt0133093"); !ok || text != "" || source != "omdb" {
		t.Errorf("unreadable synopsis = %q from %q (row %v), want OMDb's null", text, source, ok)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT poster_url, status FROM meta.posters WHERE tconst = 'tt0111161'`).Scan(&url, &status); err != nil {
		t.Fatal(err)
	}
	if url == nil || *url != "https://x/shawshank.jpg" || status != "ok" {
		t.Errorf("shawshank = %v %q", url, status)
	}
	if text, _, _ := synopsisRow(t, s, "tt0111161"); text != "Hope floats." {
		t.Errorf("repaired plot = %q", text)
	}
	if fin := sink.of(notify.Finished); len(fin) != 1 || fin[0].Errors != 0 {
		t.Errorf("finished = %+v, want no failures", fin)
	}
	lines := logs.about("tt0133093")
	if len(lines) != 1 || !strings.Contains(lines[0], "level=INFO") {
		t.Errorf("logged %q, want one Info line", lines)
	}

	// The TMDb stand-in's queue takes it up.
	wanted, err := s.tmdbWanted(ctx, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(wanted, "tt0133093") {
		t.Errorf("the TMDb queue is %v, want the unreadable title in it", wanted)
	}

	// And it is an answer: the next run asks nothing.
	asked := len(server.askedFor())
	if err := job.Run(ctx, Live); err != nil {
		t.Fatal(err)
	}
	if got := server.askedFor(); len(got) != asked {
		t.Errorf("a second run asked %v", got[asked:])
	}
}
