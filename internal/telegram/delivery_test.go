package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/notify"
)

// fakeClock is time that moves only when the test, or the sink's pacing,
// moves it.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.t) {
		c.t = t
	}
}

func (c *fakeClock) sleep(d time.Duration) { c.set(c.now().Add(d)) }

// apiCall is one request the fake Telegram received.
type apiCall struct {
	method string
	body   outbound
	at     time.Time
}

// fakeAPI is Telegram, answering from reply and remembering every call.
type fakeAPI struct {
	mu    sync.Mutex
	calls []apiCall
	clock *fakeClock
	// reply answers a call; nil, or a zero status, is success with the
	// next message id.
	reply func(c apiCall, n int) (int, string)
	next  int
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body outbound
	_ = json.NewDecoder(r.Body).Decode(&body)
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	c := apiCall{method: method, body: body, at: f.clock.now()}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	n := len(f.calls)
	reply := f.reply
	f.mu.Unlock()
	if reply != nil {
		if status, text := reply(c, n); status != 0 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, text)
			return
		}
	}
	f.mu.Lock()
	f.next++
	id := 100 + f.next
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if method == "sendMessage" {
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, id)
		return
	}
	_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
}

func (f *fakeAPI) all() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]apiCall(nil), f.calls...)
}

func (f *fakeAPI) of(method string) []apiCall {
	var out []apiCall
	for _, c := range f.all() {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// fakeMemory is the Store, as far as the sink can tell.
type fakeMemory struct {
	mu    sync.Mutex
	raw   []byte
	saves int
}

func (m *fakeMemory) LoadNotifyState(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.raw, nil
}

func (m *fakeMemory) SaveNotifyState(_ context.Context, raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.raw = append([]byte(nil), raw...)
	m.saves++
	return nil
}

func (m *fakeMemory) saved(t *testing.T) *state {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	return loadState(m.raw)
}

type harness struct {
	t     *testing.T
	sink  *Sink
	api   *fakeAPI
	clock *fakeClock
	logs  *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newHarness(t *testing.T, manual bool) *harness {
	t.Helper()
	clock := &fakeClock{t: testNow}
	api := &fakeAPI{clock: clock}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	logs := &syncBuffer{}
	s := newSink(context.Background(), Config{
		Token: "123:secret", ChatID: "42", Location: lagos, Env: "dev", Commit: "d017008c9a4e", Manual: manual,
	}, slog.New(slog.NewTextHandler(logs, nil)))
	s.base = srv.URL
	s.now = clock.now
	s.sleep = clock.sleep
	s.attachRetry = time.Millisecond
	return &harness{t: t, sink: s, api: api, clock: clock, logs: logs}
}

// run lets the loop do everything it would do over d, jumping the clock
// from one wake to the next.
func (h *harness) run(d time.Duration) {
	h.t.Helper()
	end := h.clock.now().Add(d)
	for i := 0; i < 10000; i++ {
		next := h.sink.step()
		if next.IsZero() || next.After(end) {
			h.clock.set(end)
			return
		}
		h.clock.set(next)
	}
	h.t.Fatal("the loop never went quiet")
}

func (h *harness) attach(m *fakeMemory) {
	h.sink.Attach(m)
}

func dbDown() notify.Event {
	return notify.Event{Job: notify.JobDatabase, Kind: notify.Failed, Cause: notify.DatabaseDown,
		Since: testNow.Add(-2 * time.Minute), Detail: "dial error"}
}

func TestA429IsRetriedNotDropped(t *testing.T) {
	h := newHarness(t, false)
	h.api.reply = func(c apiCall, n int) (int, string) {
		if n == 1 {
			return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`
		}
		return 0, ""
	}
	h.sink.Note(dbDown())
	h.run(30 * time.Second)
	sends := h.api.of("sendMessage")
	if len(sends) != 2 {
		t.Fatalf("sendMessage calls = %d, want the refused one and its retry", len(sends))
	}
	if gap := sends[1].at.Sub(sends[0].at); gap < 7*time.Second {
		t.Errorf("retried after %v, want at least the 7 s Telegram asked for", gap)
	}
	if sends[0].body.Text != sends[1].body.Text {
		t.Error("the retry was a different message")
	}
	// What goes over the wire is exactly what the goldens hold.
	w := writer{loc: lagos, now: testNow, token: "123:secret"}
	want, _ := message([]part{w.databaseDown(testNow.Add(-2*time.Minute), "dial error")})
	if sends[1].body.Text != want || sends[1].body.ParseMode != "HTML" || sends[1].body.DisableNotification {
		t.Errorf("sent %+v, want the loud HTML text\n%s", sends[1].body, want)
	}
}

func TestA5xxBacksOffThenIsDropped(t *testing.T) {
	h := newHarness(t, false)
	h.api.reply = func(apiCall, int) (int, string) { return 502, "Bad Gateway" }
	h.sink.Note(dbDown())
	h.run(5 * time.Minute)
	sends := h.api.of("sendMessage")
	if len(sends) != maxAttempts {
		t.Fatalf("attempts = %d, want %d", len(sends), maxAttempts)
	}
	for i, want := range []time.Duration{2, 4, 8, 16} {
		if gap := sends[i+1].at.Sub(sends[i].at); gap != want*time.Second {
			t.Errorf("gap %d = %v, want %v", i, gap, want*time.Second)
		}
	}
	if n := strings.Count(h.logs.String(), "level=ERROR"); n != 1 {
		t.Errorf("%d ERROR lines, want one for the dropped message:\n%s", n, h.logs)
	}
}

func TestA403TurnsSendingOffWithOneError(t *testing.T) {
	h := newHarness(t, false)
	h.api.reply = func(apiCall, int) (int, string) {
		return 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	}
	h.sink.Note(dbDown())
	h.run(time.Minute)
	h.sink.Note(notify.Event{Job: notify.JobDatabase, Kind: notify.Checked})
	h.sink.Note(dbDown())
	h.run(time.Hour)
	if n := len(h.api.all()); n != 1 {
		t.Errorf("%d calls, want the one that was refused and nothing after", n)
	}
	if n := strings.Count(h.logs.String(), "level=ERROR"); n != 1 {
		t.Errorf("%d ERROR lines, want 1:\n%s", n, h.logs)
	}
}

func TestAParseErrorIsResentAsPlainText(t *testing.T) {
	h := newHarness(t, false)
	h.api.reply = func(c apiCall, n int) (int, string) {
		if c.body.ParseMode != "" {
			return 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: unsupported start tag"}`
		}
		return 0, ""
	}
	h.sink.Note(dbDown())
	h.run(10 * time.Second)
	sends := h.api.of("sendMessage")
	if len(sends) != 2 {
		t.Fatalf("sendMessage calls = %d, want the refused one and the plain resend", len(sends))
	}
	plain := sends[1].body
	if plain.ParseMode != "" || strings.Contains(plain.Text, "<b>") || !strings.Contains(plain.Text, "🔴 Can't reach the database") {
		t.Errorf("resend = %+v, want plain text with no parse mode", plain)
	}
}

func TestAFailedEditIsTriedAgainWithoutWaitingForNews(t *testing.T) {
	h := newHarness(t, false)
	h.attach(&fakeMemory{raw: []byte(`{"v":1,"board_id":77,"jobs":{},"alerts":{}}`)})
	h.api.reply = func(c apiCall, n int) (int, string) {
		if c.method == "editMessageText" && n == 1 {
			return 500, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`
		}
		return 0, ""
	}
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.run(5 * time.Minute)
	edits := h.api.of("editMessageText")
	if len(edits) != 2 {
		t.Fatalf("edits = %d, want the failed one and a retry", len(edits))
	}
	if gap := edits[1].at.Sub(edits[0].at); gap < boardEvery {
		t.Errorf("retried after %v, want at least %v", gap, boardEvery)
	}
}

func TestEditsAreAMinuteApartAndCallsASecondApart(t *testing.T) {
	h := newHarness(t, false)
	h.attach(&fakeMemory{})
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.sink.Note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 100000})
	for i := 1; i <= 36; i++ {
		h.run(5 * time.Second)
		h.sink.Note(notify.Event{Job: notify.JobPosters, Kind: notify.Progress, Total: 100000, Done: int64(i * 1000), Share: float64(i) / 100})
	}
	h.run(2 * time.Minute)
	calls := h.api.all()
	for i := 1; i < len(calls); i++ {
		if gap := calls[i].at.Sub(calls[i-1].at); gap < callGap {
			t.Errorf("calls %d and %d were %v apart", i-1, i, gap)
		}
	}
	edits := h.api.of("editMessageText")
	if len(edits) < 2 || len(edits) > 4 {
		t.Errorf("%d edits over five minutes of ticks, want one a minute", len(edits))
	}
	for i := 1; i < len(edits); i++ {
		if gap := edits[i].at.Sub(edits[i-1].at); gap < boardEvery {
			t.Errorf("edits %d and %d were %v apart", i-1, i, gap)
		}
	}
	if pins := h.api.of("pinChatMessage"); len(pins) != 1 || !pins[0].body.DisableNotification {
		t.Errorf("pins = %+v, want the new board pinned once, silently", pins)
	}
	first := h.api.of("sendMessage")[0].body
	if !first.DisableNotification || first.LinkPreview == nil || !first.LinkPreview.IsDisabled || first.ParseMode != "HTML" {
		t.Errorf("board = %+v, want silent HTML with no link preview", first)
	}
}

func TestTwoPushesWithinTwoSecondsAreOneLoudMessage(t *testing.T) {
	h := newHarness(t, false)
	h.attach(&fakeMemory{})
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.run(10 * time.Second)
	before := len(h.api.of("sendMessage"))
	h.sink.Note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 10})
	h.sink.Note(notify.Event{Job: notify.JobPosters, Kind: notify.Finished, Done: 10, Took: time.Hour})
	h.run(time.Second)
	h.sink.Note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Failed, Cause: notify.KeyRejected, Provider: "TMDb"})
	h.run(10 * time.Second)
	var pushes []apiCall
	for _, c := range h.api.of("sendMessage")[before:] {
		pushes = append(pushes, c)
	}
	if len(pushes) != 1 {
		t.Fatalf("%d messages, want the two pushes as one", len(pushes))
	}
	text := pushes[0].body.Text
	if pushes[0].body.DisableNotification {
		t.Error("a message with a loud part went out silently")
	}
	if !strings.Contains(text, "TMDb turned down our key") || !strings.Contains(text, "Posters done") ||
		strings.Index(text, "TMDb") > strings.Index(text, "Posters done") {
		t.Errorf("text = %q, want both, the one that needs you first", text)
	}
}

func TestTheBoardIsLeftAloneUntilAttach(t *testing.T) {
	h := newHarness(t, false)
	h.sink.Note(notify.Event{Job: notify.JobImport, Kind: notify.Started, Step: 1, Steps: 4})
	h.sink.Note(notify.Event{Job: notify.JobImport, Kind: notify.Published, Films: 1, People: 1})
	h.run(10 * time.Minute)
	if n := len(h.api.all()); n != 0 {
		t.Fatalf("%d calls from a process that does not hold the jobs, want none", n)
	}
}

func TestAManualRunSendsOneQuietSummaryOnClose(t *testing.T) {
	h := newHarness(t, true)
	h.attach(&fakeMemory{})
	h.sink.Note(notify.Event{Job: notify.JobImport, Kind: notify.Started, Step: 1, Steps: 4})
	h.sink.Note(notify.Event{Job: notify.JobImport, Kind: notify.Published, Films: 757802, People: 3120442, PrevFilms: 756598, Took: time.Hour})
	h.run(10 * time.Minute)
	if n := len(h.api.all()); n != 0 {
		t.Fatalf("%d calls before Close, want none", n)
	}
	h.sink.shutdown()
	calls := h.api.all()
	if len(calls) != 1 || calls[0].method != "sendMessage" || !calls[0].body.DisableNotification {
		t.Fatalf("calls = %+v, want one silent sendMessage", calls)
	}
	if !strings.Contains(calls[0].body.Text, "New catalog is live (manual run)") {
		t.Errorf("text = %q", calls[0].body.Text)
	}
}

func TestCloseSendsWhatIsLeftAfterTheContextEnds(t *testing.T) {
	api := &fakeAPI{clock: &fakeClock{t: testNow}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	s := newSink(ctx, Config{Token: "t", ChatID: "42", Location: lagos}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.base = srv.URL
	s.sleep = func(time.Duration) {}
	mem := &fakeMemory{}
	s.Attach(mem)
	go s.loop(ctx)
	s.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	s.Note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 10})
	s.Note(dbDown())
	// The push is still gathering company when the process is told to
	// stop.
	cancel()
	closed, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	s.Close(closed)
	if closed.Err() != nil {
		t.Fatal("Close timed out")
	}
	var pushed, board bool
	for _, c := range api.all() {
		if c.method == "sendMessage" && strings.Contains(c.body.Text, "Can&#39;t reach the database</b>\n<b>To fix:</b>") {
			pushed = true
		}
		if strings.Contains(c.body.Text, "Shutting down") {
			board = true
		}
	}
	if !pushed || !board {
		t.Errorf("pushed = %v, shutting-down board = %v; want both before Close returned", pushed, board)
	}
	st := mem.saved(t)
	if st.Jobs[notify.JobPosters].State != stInterrupted {
		t.Errorf("saved posters state = %s, want interrupted", st.Jobs[notify.JobPosters].State)
	}
}

func TestAttachWithASavedBoardEditsThatBoard(t *testing.T) {
	h := newHarness(t, false)
	h.attach(&fakeMemory{raw: []byte(`{"v":1,"board_id":77,"board_body":"old","jobs":{},"alerts":{}}`)})
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.run(time.Minute)
	calls := h.api.all()
	if len(calls) != 1 || calls[0].method != "editMessageText" || calls[0].body.MessageID != 77 {
		t.Fatalf("calls = %+v, want one edit of message 77", calls)
	}
}

func TestALostBoardIsReplacedPinnedAndTheOldOneUnpinned(t *testing.T) {
	h := newHarness(t, false)
	mem := &fakeMemory{raw: []byte(`{"v":1,"board_id":77,"jobs":{},"alerts":{}}`)}
	h.attach(mem)
	h.api.reply = func(c apiCall, n int) (int, string) {
		if c.method == "editMessageText" {
			return 400, `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`
		}
		return 0, ""
	}
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.run(time.Minute)
	var got []string
	for _, c := range h.api.all() {
		got = append(got, fmt.Sprintf("%s:%d", c.method, c.body.MessageID))
	}
	want := "editMessageText:77 sendMessage:0 pinChatMessage:101 unpinChatMessage:77"
	if strings.Join(got, " ") != want {
		t.Errorf("calls = %v, want %s", got, want)
	}
	if id := mem.saved(t).BoardID; id != 101 {
		t.Errorf("saved board id = %d, want the new board's 101", id)
	}
}

func TestSavedStateIsNotSaidTwice(t *testing.T) {
	// A deploy: the first process calls the catalog old and saves; the
	// next one hears the same hourly stale event and stays quiet.
	mem := &fakeMemory{}
	stale := notify.Event{Job: notify.JobImport, Kind: notify.Stale, LiveSince: testNow.Add(-40 * time.Hour)}
	for i := 0; i < 2; i++ {
		h := newHarness(t, false)
		h.attach(mem)
		h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
		h.sink.Note(stale)
		h.run(time.Minute)
		var old int
		for _, c := range h.api.of("sendMessage") {
			if strings.Contains(c.body.Text, "Catalog is 1 day 16 hr old</b>\n") {
				old++
			}
		}
		if want := 1 - i; old != want {
			t.Errorf("process %d said the catalog was old %d times, want %d", i+1, old, want)
		}
	}
}

func TestAPIErrorsCarryTheirRetry(t *testing.T) {
	h := newHarness(t, false)
	h.api.reply = func(apiCall, int) (int, string) {
		return 400, `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-1001234}}`
	}
	err := h.sink.call("sendMessage", outbound{ChatID: "42", Text: "x"}, nil)
	var ae *apiError
	if !errors.As(err, &ae) || ae.Code != 400 || ae.MigrateTo != -1001234 {
		t.Fatalf("err = %v, want an apiError with the new chat id", err)
	}
}

func TestA401TurnsSendingOffWithOneError(t *testing.T) {
	h := newHarness(t, false)
	h.attach(&fakeMemory{})
	h.api.reply = func(apiCall, int) (int, string) {
		return 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`
	}
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.sink.Note(dbDown())
	h.run(time.Hour)
	if n := len(h.api.all()); n != 1 {
		t.Errorf("%d calls, want the one that was refused and nothing after", n)
	}
	logs := h.logs.String()
	if n := strings.Count(logs, "level=ERROR"); n != 1 || !strings.Contains(logs, "TELEGRAM_BOT_TOKEN was rejected") {
		t.Errorf("%d ERROR lines, want 1 naming the token:\n%s", n, logs)
	}
	if strings.Contains(logs, "level=WARN") {
		t.Errorf("a rejected token was retried:\n%s", logs)
	}
}

// losing is a harness whose process held the jobs, wrote its board, and
// has just lost the lease.
func losing(t *testing.T) (*harness, *fakeMemory) {
	h := newHarness(t, false)
	mem := &fakeMemory{}
	h.attach(mem)
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.sink.Note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 5000})
	h.run(time.Minute)
	if len(h.api.of("sendMessage")) != 1 {
		t.Fatalf("calls = %+v, want the board", h.api.all())
	}
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.Stopped})
	h.sink.Detach()
	h.run(time.Minute)
	return h, mem
}

func TestALostLeaseLeavesTheBoardAndTheStateToTheNextHolder(t *testing.T) {
	h, mem := losing(t)
	if got := mem.saved(t).Jobs[notify.JobPosters].State; got != stInterrupted {
		t.Errorf("saved posters = %s, want the stop saved on the way out", got)
	}
	mem.mu.Lock()
	saves := mem.saves
	mem.mu.Unlock()
	calls := len(h.api.all())
	h.sink.Note(notify.Event{Job: notify.JobDatabase, Kind: notify.Checked})
	h.sink.Note(notify.Event{Job: notify.JobImport, Kind: notify.Checked, NextTry: testNow.Add(time.Hour)})
	h.run(10 * time.Minute)
	h.sink.shutdown()
	if got := h.api.all()[calls:]; len(got) != 0 {
		t.Errorf("a process without the jobs made %d calls: %+v", len(got), got)
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if mem.saves != saves {
		t.Errorf("a process without the jobs saved %d more times", mem.saves-saves)
	}
}

// TestALostLeaseStillSaysTheDatabaseIsDown: while the database is away
// nobody holds the jobs, so the process that last did keeps the board
// saying so, and lets go of it the moment the database is back.
func TestALostLeaseStillSaysTheDatabaseIsDown(t *testing.T) {
	h, _ := losing(t)
	calls := len(h.api.all())
	h.sink.Note(dbDown())
	h.run(time.Minute)
	var push, board bool
	for _, c := range h.api.all()[calls:] {
		push = push || (c.method == "sendMessage" && strings.Contains(c.body.Text, "Can&#39;t reach the database</b>\n"))
		board = board || (c.method == "editMessageText" && strings.Contains(c.body.Text, "Can&#39;t reach the database</b> · updated"))
	}
	if !push || !board {
		t.Fatalf("push = %v, board = %v; want both", push, board)
	}
	calls = len(h.api.all())
	h.sink.Note(notify.Event{Job: notify.JobDatabase, Kind: notify.Checked})
	h.run(10 * time.Minute)
	got := h.api.all()[calls:]
	if len(got) != 1 || got[0].method != "sendMessage" || !strings.Contains(got[0].body.Text, "Database is back") {
		t.Errorf("calls = %+v, want only the push that the database is back", got)
	}
}

func TestATakeoverAfterALostLeaseAttachesAgain(t *testing.T) {
	h, mem := losing(t)
	calls := len(h.api.all())
	h.attach(mem)
	h.sink.Note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	h.run(time.Minute)
	got := h.api.all()[calls:]
	if len(got) != 1 || got[0].method != "editMessageText" || got[0].body.MessageID != mem.saved(t).BoardID {
		t.Errorf("calls = %+v, want the saved board edited", got)
	}
}
