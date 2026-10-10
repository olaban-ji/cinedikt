package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"cinedikt/internal/catalog"
	"cinedikt/internal/daily"
)

// heatPuzzle is what the fake deals in place of a puzzle: Heat, under
// the same number on the same day, with a colour of its own, so a read
// can tell which of the two it was served without being told either
// answer.
func heatPuzzle(no int, day time.Time) *daily.Puzzle {
	return &daily.Puzzle{
		No:  no,
		Day: day,
		Answer: daily.Answer{ID: "tt0113277", Title: "Heat", Year: 1995, Rating: 8.3, MD: 1215, Length: 170,
			Colour: "#2b3f57", Genres: []string{"Action", "Crime", "Drama"}},
		Directors: []daily.Named{{ID: "nm0000520", Name: "Michael Mann"}},
		Cast: []daily.Billed{
			{ID: "nm0001827", Name: "Diane Venora", Billing: 6},
			{ID: "nm0000634", Name: "Tom Sizemore", Billing: 5},
			{ID: "nm0000685", Name: "Jon Voight", Billing: 4},
			{ID: "nm0000174", Name: "Val Kilmer", Billing: 3, Also: &daily.Also{ID: "tt0092099", Title: "Top Gun", Year: 1986}},
			{ID: "nm0000134", Name: "Robert De Niro", Billing: 2, Also: &daily.Also{ID: "tt0075314", Title: "Taxi Driver", Year: 1976}},
			{ID: "nm0000199", Name: "Al Pacino", Billing: 1, Also: &daily.Also{ID: "tt0086250", Title: "Scarface", Year: 1983}},
		},
		Movies: []daily.Movie{
			{ID: "tt0113277", Title: "Heat", Year: 1995, Rating: 8.3, Genres: []string{"Action", "Crime", "Drama"},
				Cast: []int{0, 1, 2, 3, 4, 5}, Sheets: []int{0, 1, 2, 3, 4, 5}, Dir: true},
		},
		Era:   1995,
		Genre: "Action",
	}
}

// heatSaid is whether a response or a log names the answer the fake
// deals.
func heatSaid(raw string) bool {
	return strings.Contains(raw, "tt0113277") || strings.Contains(raw, "Heat")
}

// DealDailyPuzzle deals player Heat for puzzle No. no and deletes their
// game of it, as the catalog's store does, or finds no other movie.
func (f *fakeDaily) DealDailyPuzzle(_ context.Context, player int64, no int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dealt = append(f.dealt, fmt.Sprintf("%d/%d", player, no))
	if f.fail != nil {
		return f.fail
	}
	p, ok := f.puzzles[no]
	if !ok {
		return catalog.ErrNotFound
	}
	if f.noOther {
		return catalog.ErrNoOtherAnswer
	}
	key := [2]int64{player, int64(no)}
	f.deals[key] = heatPuzzle(no, p.Day)
	delete(f.games, key)
	return nil
}

// WarmDailyDeals counts the times it was asked.
func (f *fakeDaily) WarmDailyDeals() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warms++
}

// DailyDeal is the movie player was dealt for No. no, or
// catalog.ErrNotFound.
func (f *fakeDaily) DailyDeal(_ context.Context, player int64, no int) (*daily.Puzzle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dealReads++
	if f.fail != nil {
		return nil, f.fail
	}
	if p, ok := f.deals[[2]int64{player, int64(no)}]; ok {
		return p, nil
	}
	return nil, catalog.ErrNotFound
}

// devAsked is what the fake has been asked so far: the days, the deals
// made, and the standings.
func (f *fakeDaily) devAsked() (days, dealt, standings []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.days), slices.Clone(f.dealt), slices.Clone(f.standings)
}

// devServer serves f with the development tools on, the clock at the
// moment given, and logger, or none, hearing the API's log.
func devServer(t *testing.T, f DailyStore, at time.Time, logger *slog.Logger) (*httptest.Server, *clock) {
	t.Helper()
	if logger == nil {
		logger = discardLogger()
	}
	api := New(NewCatalogServer(nil, logger), logger).WithDaily(f).WithDailyDev(true)
	c := &clock{at: at}
	api.daily.now = c.now
	srv := httptest.NewTLSServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, c
}

// reset presses Play again.
func (b *browser) reset(query string) reply {
	b.t.Helper()
	return b.post("/daily/dev/reset"+query, map[string]any{})
}

// TestPlayAgainIsARouteOnlyInDevelopment: without the development tools,
// as for real players, the router has never heard of the reset, as of
// any other path, today does not say dev at all, and no deal is ever
// read; with them, today says dev and the reset is there.
func TestPlayAgainIsARouteOnlyInDevelopment(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	b.move("next", 0, nil)
	if r := b.reset(""); r.status != http.StatusNotFound || r.body != nil || strings.Contains(r.header.Get("Set-Cookie"), "cd_daily") {
		t.Errorf("reset in production: %d %q, Set-Cookie %q", r.status, r.raw, r.header.Get("Set-Cookie"))
	}
	today := b.get("/daily")
	if _, said := today.body["dev"]; said || strings.Contains(today.raw, `"dev"`) || today.body["game"] == nil {
		t.Errorf("today in production: %s", today.raw)
	}
	f.mu.Lock()
	if len(f.dealt) != 0 || f.dealReads != 0 || f.warms != 0 {
		t.Errorf("production dealt %v, read %d deals and warmed %d times", f.dealt, f.dealReads, f.warms)
	}
	f.mu.Unlock()

	devStore := newFakeDaily(matrixPuzzle())
	dev, _ := devServer(t, devStore, todayAt, nil)
	d := newBrowser(t, dev)
	if r := d.get("/daily"); r.status != http.StatusOK || r.body["dev"] != true {
		t.Errorf("today in development: %d %s", r.status, r.raw)
	}
	// Opening the Daily gets Play again's candidates ready behind it.
	devStore.mu.Lock()
	if devStore.warms != 1 {
		t.Errorf("today in development warmed %d times, want once", devStore.warms)
	}
	devStore.mu.Unlock()
	if r := d.reset(""); r.status != http.StatusNoContent || r.raw != "" {
		t.Errorf("reset in development: %d %q", r.status, r.raw)
	}
}

// TestPlayAgainDealsTheReaderAMovieOfTheirOwn: the reader is a new
// player at once, with the cookie set the way Play sets one, and the
// next read, at once, is a movie dealt to them alone, their name and no
// game. The game they had stays, and so does everyone else's, on the
// day's own movie, which the other player still reads; the day's count
// is of those games alone, before and after the new player plays.
// Every move and Movies sheet of the new game is on the new movie's
// cast, and the new game opens a sheet of its own.
func TestPlayAgainDealsTheReaderAMovieOfTheirOwn(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := devServer(t, f, todayAt, nil)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	b.move("next", 0, nil)
	if r := b.move("sheet", 1, map[string]any{"person": "nm0001592"}); r.status != http.StatusOK {
		t.Fatalf("opening a sheet before Play again: %d %s", r.status, r.raw)
	}
	other := newBrowser(t, srv)
	other.ip = "216.160.83.56"
	other.post("/daily/142/play", map[string]any{})
	before := b.get("/daily")
	if before.body["game"] == nil || before.body["played"] != 2.0 {
		t.Fatalf("before Play again: %s", before.raw)
	}
	was := before.body["player"].(map[string]any)["name"]

	r := b.reset("")
	if r.status != http.StatusNoContent {
		t.Fatalf("reset = %d %s", r.status, r.raw)
	}
	cookie := r.header.Get("Set-Cookie")
	for _, want := range []string{"cd_daily=", "Path=/", "Max-Age=31536000", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("Set-Cookie %q lacks %s", cookie, want)
		}
	}
	if r.header.Get("Cache-Control") != "no-store" || heatSaid(r.raw) {
		t.Errorf("reset answered %q with Cache-Control %q", r.raw, r.header.Get("Cache-Control"))
	}

	after := b.get("/daily")
	if after.status != http.StatusOK || after.body["no"] != 142.0 || after.body["date"] != "2026-10-08" {
		t.Fatalf("today after Play again: %d %s", after.status, after.raw)
	}
	if player := after.body["player"].(map[string]any); player["saved"] != true || player["name"] == "" || player["name"] == was {
		t.Errorf("after Play again the reader is %v; they were %v", player, was)
	}
	if after.body["game"] != nil || after.body["played"] != 2.0 {
		t.Errorf("after Play again: game %v, played %v", after.body["game"], after.body["played"])
	}
	if after.body["colour"] != "#2b3f57" || heatSaid(after.raw) {
		t.Errorf("after Play again the reader's movie: %s", after.raw)
	}
	_, dealt, _ := f.devAsked()
	f.mu.Lock()
	if len(f.players) != 3 || len(f.games) != 2 || !slices.Equal(dealt, []string{"3/142"}) {
		t.Errorf("after Play again: %d players, %d games, dealt %v; want the new player alone dealt a movie, every game kept",
			len(f.players), len(f.games), dealt)
	}
	f.mu.Unlock()

	// The other player is still on the day's own movie, game and all.
	if mine := other.get("/daily"); mine.body["colour"] != "#26382d" || mine.body["game"] == nil || seqOf(t, mine) != 0 {
		t.Errorf("the other player after Play again: %s", mine.raw)
	}

	// The new game is on Heat: its sixth-billed is Diane Venora, and it
	// has no sheet, so none is read, the old one's person's or Venora's,
	// until it opens its own.
	played := b.post("/daily/142/play", map[string]any{})
	if g := gameOf(t, played); g["sheet"] != nil || g["seq"] != 0.0 {
		t.Errorf("the new game: %s", played.raw)
	}
	for _, who := range []string{"nm0001592", "nm0001827"} {
		if r := b.sheet(who); r.status != http.StatusConflict || r.body["reason"] != "sheet" {
			t.Errorf("%s's sheet in the new game, none opened: %d %s", who, r.status, r.raw)
		}
	}
	if r := b.move("sheet", 0, map[string]any{"person": "nm0001827"}); r.status != http.StatusOK || gameOf(t, r)["sheet"] != "nm0001827" {
		t.Errorf("opening Venora's sheet in the new game: %d %s", r.status, r.raw)
	}
	if r := b.sheet("nm0001827"); r.status != http.StatusOK || heatSaid(r.raw) {
		t.Errorf("Venora's sheet, opened: %d %s", r.status, r.raw)
	}
	if again := b.get("/daily"); again.body["played"] != 2.0 {
		t.Errorf("played after the new game began: %v, want the day's two", again.body["played"])
	}
}

// TestPlayAgainDealsThePuzzleTheReaderIsShownNext: the movie is dealt for
// the puzzle of the reader's date in their zone, which is what a new
// player with no game is shown. For a player GET /daily had moved on to
// the day their left-behind game's zone has reached, that is the day
// before, the one their game was left on, and the new movie is there at
// once rather than on a day they are not shown. The game they left stays
// theirs.
func TestPlayAgainDealsThePuzzleTheReaderIsShownNext(t *testing.T) {
	f := newFakeDaily(puzzleOn(143, "2026-10-09"), puzzleOn(144, "2026-10-10"))
	// 17:00 in Tokyo, 01:00 in Los Angeles, both on the 9th.
	srv, clk := devServer(t, f, time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC), nil)
	b := newBrowser(t, srv)
	if r := b.post("/daily/143/play?tz=Asia/Tokyo", map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("play in Tokyo: %d %s", r.status, r.raw)
	}
	// 00:30 on the 10th in Tokyo; 08:30 on the 9th in Los Angeles.
	clk.set(time.Date(2026, 10, 9, 15, 30, 0, 0, time.UTC))
	if r := b.get("/daily?tz=America/Los_Angeles"); r.body["no"] != 144.0 {
		t.Fatalf("the page shows No. %v", r.body["no"])
	}
	if r := b.reset("?tz=America/Los_Angeles"); r.status != http.StatusNoContent {
		t.Fatalf("reset = %d %s", r.status, r.raw)
	}
	after := b.get("/daily?tz=America/Los_Angeles")
	if after.body["no"] != 143.0 || after.body["colour"] != "#2b3f57" || after.body["game"] != nil {
		t.Errorf("after Play again the page shows No. %v in %v: %s", after.body["no"], after.body["colour"], after.raw)
	}
	for _, query := range []string{"?tz=America/Los_Angeles", "?tz=Asia/Tokyo", ""} {
		stranger := newBrowser(t, srv)
		if r := stranger.reset(query); r.status != http.StatusNoContent {
			t.Errorf("a stranger's reset%s: %d %s", query, r.status, r.raw)
		}
	}
	if _, dealt, _ := f.devAsked(); !slices.Equal(dealt, []string{"2/143", "3/143", "4/144", "5/143"}) {
		t.Errorf("dealt %v, want the player's own date's 143, then each stranger's own date", dealt)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.games) != 1 {
		t.Errorf("%d games after Play again, want the one left behind on the 9th, kept", len(f.games))
	}
}

// TestPlayAgainWithNoOtherMovieChangesNothing: the reader keeps their
// cookie, their game and their movie.
func TestPlayAgainWithNoOtherMovieChangesNothing(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	f.noOther = true
	srv, _ := devServer(t, f, todayAt, nil)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	b.move("next", 0, nil)

	r := b.reset("")
	if r.status != http.StatusServiceUnavailable || r.body["reason"] != "unavailable" || r.header.Get("Set-Cookie") != "" {
		t.Errorf("reset with no other movie: %d %s, Set-Cookie %q", r.status, r.raw, r.header.Get("Set-Cookie"))
	}
	after := b.get("/daily")
	if after.body["player"].(map[string]any)["saved"] != true || after.body["game"] == nil || seqOf(t, after) != 1 {
		t.Errorf("after a reset that failed: %s", after.raw)
	}
	if after.body["colour"] != "#26382d" {
		t.Errorf("after a reset that failed the movie is %s", after.raw)
	}
}

// TestPlayAgainIsAChangeLikeAnyOther: it must say it is JSON and fit in
// the same four kilobytes, a crawler's is nothing, and a day with no
// puzzle yet is not ready; none of them deals anything.
func TestPlayAgainIsAChangeLikeAnyOther(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, clk := devServer(t, f, todayAt, nil)
	b := newBrowser(t, srv)
	for _, c := range []struct {
		contentType, body string
		status            int
		reason            string
	}{
		{"", "{}", http.StatusUnsupportedMediaType, "content-type"},
		{"text/plain", "{}", http.StatusUnsupportedMediaType, "content-type"},
		{"application/x-www-form-urlencoded", "a=b", http.StatusUnsupportedMediaType, "content-type"},
		{"application/json", `{"pad":"` + strings.Repeat("x", dailyBody) + `"}`, http.StatusBadRequest, "bad"},
		{"application/json", "[", http.StatusBadRequest, "bad"},
	} {
		if r := b.do(http.MethodPost, "/daily/dev/reset", c.contentType, []byte(c.body)); r.status != c.status || r.body["reason"] != c.reason {
			t.Errorf("%q %.20q: %d %s, want %d %s", c.contentType, c.body, r.status, r.raw, c.status, c.reason)
		}
	}
	bot := newBrowser(t, srv)
	bot.ua = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	if r := bot.reset(""); r.status != http.StatusNoContent || r.header.Get("Set-Cookie") != "" {
		t.Errorf("a crawler's reset: %d, Set-Cookie %q", r.status, r.header.Get("Set-Cookie"))
	}
	// 08:30 on the 9th in Tokyo, which has no puzzle yet.
	clk.set(time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC))
	if r := b.reset("?tz=Asia/Tokyo"); r.status != http.StatusServiceUnavailable || r.body["reason"] != "not-ready" {
		t.Errorf("a reset on a date with no puzzle: %d %s", r.status, r.raw)
	}
	if r := b.do(http.MethodGet, "/daily/dev/reset", "", nil); r.status != http.StatusMethodNotAllowed {
		t.Errorf("GET of the reset: %d %s", r.status, r.raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.dealt) != 0 || len(f.players) != 0 {
		t.Errorf("dealt %v and made %d players", f.dealt, len(f.players))
	}
}

// TestPlayAgainNeverCountsAgainstTheAddresssAllowance: every press makes
// a new player, and a developer pressing it all afternoon from one
// address is never taken for a script making players, nor is anyone
// else who shares the address.
func TestPlayAgainNeverCountsAgainstTheAddresssAllowance(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := devServer(t, f, todayAt, nil)
	b := newBrowser(t, srv)
	for i := range joinBurst + 5 {
		if r := b.reset(""); r.status != http.StatusNoContent {
			t.Fatalf("reset %d: %d %s", i+1, r.status, r.raw)
		}
		if r := b.post("/daily/142/play", map[string]any{}); r.status != http.StatusOK {
			t.Fatalf("Play %d: %d %s", i+1, r.status, r.raw)
		}
	}
	// Had the presses counted, the address's ten would be long spent.
	if r := newBrowser(t, srv).post("/daily/142/play", map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("a stranger's Play from the same address: %d %s", r.status, r.raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.players) != joinBurst+6 {
		t.Errorf("%d players, want one for every press and the stranger", len(f.players))
	}
}

// TestPlayAgainNeverLogsTheNewAnswer: whoever reads the log is about to
// play it, so it says which puzzle a movie was dealt for and nothing of
// what, at every level, and nor does any response.
func TestPlayAgainNeverLogsTheNewAnswer(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f := newFakeDaily(matrixPuzzle())
	srv, _ := devServer(t, f, todayAt, logger)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	replies := []reply{b.reset("?tz=Europe/London"), b.get("/daily?tz=Europe/London")}
	replies = append(replies, b.post("/daily/142/play?tz=Europe/London", map[string]any{}))
	replies = append(replies, b.move("next", 0, nil))
	for _, r := range replies {
		if r.status >= 400 || heatSaid(r.raw) {
			t.Errorf("a response after Play again: %d %s", r.status, r.raw)
		}
	}
	if !strings.Contains(logs.String(), `msg="daily puzzle dealt for development" no=142 day=2026-10-08`) {
		t.Errorf("the log does not say which puzzle a movie was dealt for:\n%s", logs.String())
	}
	if heatSaid(logs.String()) {
		t.Errorf("the log names the new answer:\n%s", logs.String())
	}
}
