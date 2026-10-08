package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cinedikt/internal/catalog"
	"cinedikt/internal/daily"
)

// The catalog's store is the real DailyStore.
var _ DailyStore = (*catalog.Store)(nil)

// todayAt is the moment the tests run at: puzzle No. 142's day.
var todayAt = time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC)

// matrixPuzzle is No. 142: The Matrix, two directors and four cast, a
// close relative, three starting cards each through a different actor.
func matrixPuzzle() *daily.Puzzle {
	return &daily.Puzzle{
		No:  142,
		Day: daily.Today(todayAt),
		Answer: daily.Answer{ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331,
			Genres: []string{"Action", "Sci-Fi"}},
		People: []daily.Slot{
			{ID: "nm0905154", Name: "Lana Wachowski", Role: daily.RoleDirector},
			{ID: "nm0905152", Name: "Lilly Wachowski", Role: daily.RoleDirector},
			{ID: "nm0000206", Name: "Keanu Reeves", Role: daily.RoleCast},
			{ID: "nm0000401", Name: "Laurence Fishburne", Role: daily.RoleCast},
			{ID: "nm0005251", Name: "Carrie-Anne Moss", Role: daily.RoleCast},
			{ID: "nm0001592", Name: "Joe Pantoliano", Role: daily.RoleCast},
		},
		Cards: []daily.Card{
			{ID: "c1", Film: "tt0234215", Title: "The Matrix Reloaded", Year: 2003, Rating: 7.2, MD: 515, Votes: 600000, People: []int{0, 1, 2, 3, 4}},
			{ID: "c2", Film: "tt0108065", Title: "Searching for Bobby Fischer", Year: 1993, Rating: 7.3, Votes: 40000, People: []int{3}},
			{ID: "c3", Film: "tt0109190", Title: "Baby's Day Out", Year: 1994, Rating: 6.3, Votes: 60000, People: []int{5}},
			{ID: "c4", Film: "tt0111257", Title: "Speed", Year: 1994, Rating: 7.3, Votes: 400000, People: []int{2}},
			{ID: "c5", Film: "tt0115736", Title: "Bound", Year: 1996, Rating: 7.3, Votes: 80000, People: []int{0, 1, 5}},
			{ID: "c6", Film: "tt0209144", Title: "Memento", Year: 2000, Rating: 8.4, Votes: 1400000, People: []int{4, 5}},
			{ID: "c7", Film: "tt1371111", Title: "Cloud Atlas", Year: 2012, Rating: 7.4, Votes: 380000, People: []int{0, 1}},
			{ID: "c8", Film: "tt0120601", Title: "Being John Malkovich", Year: 1999, Rating: 7.7, Votes: 400000, People: []int{4}},
		},
		Start: []string{"c2", "c3", "c8"},
		Era:   1995,
		Genre: "Action",
	}
}

// fakeDaily keeps Daily in memory, making moves the way the catalog's
// store does: one at a time, a retried key answered with the game, a
// stale seq refused with it.
type fakeDaily struct {
	mu      sync.Mutex
	puzzles map[int]*daily.Puzzle
	players map[string]daily.Player
	names   map[string]bool
	games   map[[2]int64]*daily.Record
	films   map[string]daily.Looked
	nextID  int64
	fail    error
	boards  []string
	// days are the days DailyPuzzle was asked for, nos the numbers
	// DailyPuzzleNo was, streaks the puzzles DailyStreak was, and
	// standings what DailyStanding was, as "no/player/finished", in
	// order.
	days      []string
	nos       []int
	streaks   []int
	standings []string
	// weeks are what DailyStanding answers, before and after the game.
	weeks map[bool]*daily.Week
}

func newFakeDaily(puzzles ...*daily.Puzzle) *fakeDaily {
	f := &fakeDaily{puzzles: map[int]*daily.Puzzle{}, players: map[string]daily.Player{}, names: map[string]bool{},
		games: map[[2]int64]*daily.Record{},
		weeks: map[bool]*daily.Week{false: {Rank: 2048, Players: 83500}, true: {Rank: 1204, Players: 83500}},
		films: map[string]daily.Looked{
			"tt0111257": {Title: "Speed", Year: 1994, Rating: ptrTo(7.3), Credited: []string{"nm0000206"}},
			"tt0034583": {Title: "Casablanca", Year: 1942, Rating: ptrTo(8.5)},
			"tt0234215": {Title: "The Matrix Reloaded", Year: 2003, Rating: ptrTo(7.2),
				Credited: []string{"nm0905154", "nm0905152", "nm0000206", "nm0000401", "nm0005251"}},
		}}
	for _, p := range puzzles {
		f.puzzles[p.No] = p
	}
	return f
}

func ptrTo[T any](v T) *T { return &v }

func (f *fakeDaily) err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fail
}

func (f *fakeDaily) DailyPuzzle(_ context.Context, day time.Time) (*daily.Puzzle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.days = append(f.days, daily.DayString(day))
	if f.fail != nil {
		return nil, f.fail
	}
	for _, p := range f.puzzles {
		if p.Day.Equal(daily.Today(day)) {
			return p, nil
		}
	}
	return nil, catalog.ErrNotFound
}

func (f *fakeDaily) DailyPuzzleNo(_ context.Context, no int) (*daily.Puzzle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nos = append(f.nos, no)
	if p, ok := f.puzzles[no]; ok {
		return p, nil
	}
	return nil, catalog.ErrNotFound
}

func (f *fakeDaily) DailyPlayer(_ context.Context, token []byte) (daily.Player, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.players[string(token)]
	return p, ok, f.fail
}

func (f *fakeDaily) CreateDailyPlayer(_ context.Context, token []byte, name string, hue int) (daily.Player, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.players[string(token)]; ok {
		return p, nil
	}
	if f.names[name] {
		return daily.Player{}, catalog.ErrNameTaken
	}
	f.nextID++
	p := daily.Player{ID: f.nextID, Name: name, Hue: hue}
	f.players[string(token)] = p
	f.names[name] = true
	return p, nil
}

func (f *fakeDaily) RenameDailyPlayer(_ context.Context, id int64, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.names[name] {
		return catalog.ErrNameTaken
	}
	for token, p := range f.players {
		if p.ID == id {
			delete(f.names, p.Name)
			p.Name = name
			f.players[token] = p
			f.names[name] = true
		}
	}
	return nil
}

func (f *fakeDaily) byID(id int64) (daily.Player, bool) {
	for _, p := range f.players {
		if p.ID == id {
			return p, true
		}
	}
	return daily.Player{}, false
}

func copyRecord(rec *daily.Record) *daily.Record {
	if rec == nil {
		return nil
	}
	c := *rec
	c.Moves = slices.Clone(rec.Moves)
	return &c
}

func (f *fakeDaily) DailyGame(_ context.Context, player int64, no int) (*daily.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyRecord(f.games[[2]int64{player, int64(no)}]), f.fail
}

func (f *fakeDaily) StartDailyGame(_ context.Context, player int64, no int, at time.Time, zone *time.Location) (*daily.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{player, int64(no)}
	if f.games[key] == nil {
		f.games[key] = &daily.Record{Started: at, Zone: zone.String()}
	}
	return copyRecord(f.games[key]), nil
}

func (f *fakeDaily) DailyAct(_ context.Context, player int64, p *daily.Puzzle, r daily.Request, at time.Time) (*daily.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := f.games[[2]int64{player, int64(p.No)}]
	if rec == nil {
		return nil, daily.ErrNoGame
	}
	for _, m := range rec.Moves {
		if m.Key == r.Key {
			return copyRecord(rec), nil
		}
	}
	if !p.On(at, daily.Zone(rec.Zone)) {
		return copyRecord(rec), daily.ErrDay
	}
	if r.Seq != len(rec.Moves) {
		return copyRecord(rec), daily.ErrStale
	}
	s := daily.Replay(p, rec.Moves)
	var looked *daily.Looked
	if l, ok := f.films[r.Arg]; ok && r.Kind == daily.KindGuess {
		looked = &l
	}
	m, err := daily.Apply(p, s, r, looked)
	if err != nil {
		return copyRecord(rec), err
	}
	m.Seq, m.At = len(rec.Moves)+1, at
	rec.Moves = append(rec.Moves, m)
	if s.Step(p, m); s.Done {
		rec.Finished = &at
	}
	return copyRecord(rec), nil
}

func (f *fakeDaily) DailyLive(context.Context, []string, []string) (daily.Live, error) {
	return daily.Live{
		Posters: map[string]string{"tt0108065": "https://img.example/bobby.jpg", "tt0133093": "https://img.example/matrix.jpg"},
		Photos:  map[string]string{"nm0000206": "https://image.tmdb.org/t/p/w185/keanu.jpg"},
	}, f.err()
}

func (f *fakeDaily) DailyPlayed(_ context.Context, no int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for key := range f.games {
		if key[1] == int64(no) {
			n++
		}
	}
	return n, f.fail
}

// DailyStreak is a run of five ending at No. no once the player has
// finished it with points, and otherwise one of four they can extend.
func (f *fakeDaily) DailyStreak(_ context.Context, player int64, no int) (daily.Streak, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streaks = append(f.streaks, no)
	rec, p := f.games[[2]int64{player, int64(no)}], f.puzzles[no]
	if rec != nil && rec.Finished != nil && p != nil && daily.Replay(p, rec.Moves).Pts > 0 {
		return daily.Streak{Now: 5}, f.fail
	}
	return daily.Streak{Before: 4}, f.fail
}

func (f *fakeDaily) DailyStanding(_ context.Context, p *daily.Puzzle, player int64, finished bool) (*daily.Week, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.standings = append(f.standings, fmt.Sprintf("%d/%d/%v", p.No, player, finished))
	if f.fail != nil {
		return nil, f.fail
	}
	return f.weeks[finished], nil
}

func (f *fakeDaily) DailyBoard(_ context.Context, p *daily.Puzzle, tab string, player int64) (daily.Board, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boards = append(f.boards, fmt.Sprintf("%d/%s/%d", p.No, tab, player))
	b := daily.Board{Tab: tab, Total: 1, Rows: []daily.Row{}}
	if pl, ok := f.byID(player); ok {
		b.You = &daily.You{Rank: 1, Pts: 900, Secs: 95, Listed: false}
		b.Rows = daily.Lay([]daily.Ranked{{Rank: 1, Player: pl.ID, Name: pl.Name, Pts: 900, MS: 95000}}, pl.ID, tab)
	}
	return b, f.fail
}

// DailyNames lends the words its people have: Trinity and Morpheus,
// whom nobody shares a name with, lend nothing, and Sarah Lecter is a
// real person no draw may make.
func (f *fakeDaily) DailyNames(context.Context) (daily.Credits, error) {
	return daily.Credits{
		Characters: []string{"Trinity", "Morpheus", "Richard Kimble", "Ellen Ripley", "Marty McFly", "Clarice Starling",
			"Hannibal Lecter", "Indiana Jones", "Forrest Gump", "Sarah Connor", "John McClane", "Rick Blaine"},
		People: []string{"Sarah Lecter", "Richard Gere", "Ellen Burstyn", "Marty Feldman", "Clarice Taylor", "John Hurt",
			"Rick Moranis", "Grace Jones", "Kevin Connor", "David Blaine", "Bob Kimble", "Ann Ripley", "Jo Starling"},
	}, nil
}

// dailyServer serves f through the API's own router, over TLS so a
// browser's jar sends the Secure cookie back, with the clock at
// todayAt.
func dailyServer(t *testing.T, f DailyStore) (*httptest.Server, *Server) {
	t.Helper()
	api := New(NewCatalogServer(nil, discardLogger()), discardLogger())
	if f != nil {
		api.WithDaily(f)
	}
	api.daily.now = func() time.Time { return todayAt }
	srv := httptest.NewTLSServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, api
}

// browser is a reader: their own cookie jar, address and user agent.
type browser struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
	ip     string
	ua     string
}

func newBrowser(t *testing.T, srv *httptest.Server) *browser {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := *srv.Client()
	c.Jar = jar
	return &browser{t: t, srv: srv, client: &c, ip: "81.2.69.142"}
}

type reply struct {
	status int
	raw    string
	body   map[string]any
	header http.Header
}

func (b *browser) do(method, path, contentType string, body []byte) reply {
	b.t.Helper()
	req, err := http.NewRequest(method, b.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-Real-IP", b.ip)
	if b.ua != "" {
		req.Header.Set("User-Agent", b.ua)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	r := reply{status: resp.StatusCode, raw: string(raw), header: resp.Header}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(raw, &r.body); err != nil {
			b.t.Fatalf("%s %s: %v in %s", method, path, err, raw)
		}
	}
	return r
}

func (b *browser) get(path string) reply { return b.do(http.MethodGet, path, "", nil) }

func (b *browser) post(path string, body any) reply {
	b.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		b.t.Fatal(err)
	}
	return b.do(http.MethodPost, path, "application/json", raw)
}

// move posts a move with a fresh key, following the game at seq.
func (b *browser) move(verb string, seq int, extra map[string]any) reply {
	b.t.Helper()
	body := map[string]any{"key": fmt.Sprintf("key-%s-%d-%d", verb, seq, time.Now().UnixNano()), "seq": seq}
	for k, v := range extra {
		body[k] = v
	}
	return b.post("/daily/142/"+verb, body)
}

func gameOf(t *testing.T, r reply) map[string]any {
	t.Helper()
	g, ok := r.body["game"].(map[string]any)
	if !ok {
		t.Fatalf("no game in %d %s", r.status, r.raw)
	}
	return g
}

func seqOf(t *testing.T, r reply) int { return int(gameOf(t, r)["seq"].(float64)) }

func ptsOf(t *testing.T, r reply) int { return int(gameOf(t, r)["pts"].(float64)) }

func TestDailyIsUnavailableWithoutAStore(t *testing.T) {
	srv, _ := dailyServer(t, nil)
	b := newBrowser(t, srv)
	for _, r := range []reply{b.get("/daily"), b.get("/daily/me"), b.post("/daily/142/play", map[string]any{}), b.get("/daily/142/board")} {
		if r.status != http.StatusServiceUnavailable || r.body["reason"] != "unavailable" {
			t.Errorf("without a store: %d %s", r.status, r.raw)
		}
	}
}

func TestTodayIsNotReadyUntilItIsPicked(t *testing.T) {
	srv, _ := dailyServer(t, newFakeDaily())
	r := newBrowser(t, srv).get("/daily")
	if r.status != http.StatusServiceUnavailable || r.body["reason"] != "not-ready" {
		t.Errorf("no puzzle today: %d %s", r.status, r.raw)
	}
}

// TestReadingTodayWritesNothing: the banner asks on every visit, so a
// reader with no cookie is offered a name and given no cookie, and no
// player is made.
func TestReadingTodayWritesNothing(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	r := b.get("/daily")
	if r.status != http.StatusOK {
		t.Fatalf("GET /daily = %d %s", r.status, r.raw)
	}
	if r.header.Get("Set-Cookie") != "" {
		t.Errorf("reading today set a cookie: %s", r.header.Get("Set-Cookie"))
	}
	if len(f.players) != 0 {
		t.Errorf("reading today made %d players", len(f.players))
	}
	player := r.body["player"].(map[string]any)
	if name, _ := player["name"].(string); name == "" || player["saved"] != false {
		t.Errorf("player = %v", player)
	}
	if r.body["no"] != 142.0 || r.body["date"] != "2026-10-08" || r.body["game"] != nil || r.body["played"] != 0.0 {
		t.Errorf("body = %s", r.raw)
	}
	if r.body["now"] != "2026-10-08T09:30:00Z" || r.body["next"] != "2026-10-09T00:00:00Z" {
		t.Errorf("now %v, next %v", r.body["now"], r.body["next"])
	}
	if clues := r.body["clues"].(map[string]any); clues["directors"] != 2.0 || clues["cast"] != 4.0 {
		t.Errorf("clues = %v", clues)
	}
	cards := r.body["cards"].([]any)
	if len(cards) != 8 {
		t.Fatalf("%d cards", len(cards))
	}
	if first := cards[0].(map[string]any); len(first) != 4 || first["id"] != "c1" || first["year"] != 2003.0 || first["rating"] != 7.2 {
		t.Errorf("a card at load is %v, want its id, year, rating and month-day only", first)
	}
	start := r.body["start"].([]any)
	if len(start) != 3 {
		t.Fatalf("start = %v", start)
	}
	if bobby := start[0].(map[string]any); bobby["card"] != "c2" ||
		bobby["film"].(map[string]any)["title"] != "Searching for Bobby Fischer" ||
		bobby["film"].(map[string]any)["poster"] != "https://img.example/bobby.jpg" {
		t.Errorf("start[0] = %v", bobby)
	}
	if streak := r.body["streak"].(map[string]any); streak["now"] != 0.0 || streak["before"] != 0.0 {
		t.Errorf("a reader with no player has a streak: %v", streak)
	}
	if r.header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", r.header.Get("Cache-Control"))
	}
}

// TestPlayMakesThePlayerAndTheGame: the cookie is set the way only the
// server can read it; the offered name is kept; playing again returns
// the game as it was; and today, read with the cookie, has the game and
// renews the cookie.
func TestPlayMakesThePlayerAndTheGame(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	offered := b.get("/daily").body["player"].(map[string]any)["name"].(string)

	r := b.post("/daily/142/play", map[string]any{"name": offered})
	if r.status != http.StatusOK {
		t.Fatalf("play = %d %s", r.status, r.raw)
	}
	cookie := r.header.Get("Set-Cookie")
	for _, want := range []string{"cd_daily=", "Path=/", "Max-Age=31536000", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("Set-Cookie %q lacks %s", cookie, want)
		}
	}
	if player := r.body["player"].(map[string]any); player["name"] != offered || player["saved"] != true {
		t.Errorf("player = %v, want %q kept", player, offered)
	}
	g := gameOf(t, r)
	if g["phase"] != "play" || g["pts"] != 1000.0 || g["seq"] != 0.0 || g["startedAt"] != "2026-10-08T09:30:00Z" || g["end"] != nil {
		t.Errorf("a new game = %v", g)
	}
	if log := g["log"].([]any); len(log) != 1 || log[0].(map[string]any)["type"] != "start" {
		t.Errorf("log = %v", log)
	}
	b.move("flip", 0, map[string]any{"card": "c4"})
	again := b.post("/daily/142/play", map[string]any{"name": "Somebody Else"})
	if seqOf(t, again) != 1 || len(f.players) != 1 {
		t.Errorf("playing again: seq %d, %d players", seqOf(t, again), len(f.players))
	}
	today := b.get("/daily")
	if today.body["player"].(map[string]any)["saved"] != true || today.body["game"] == nil || today.body["played"] != 1.0 {
		t.Errorf("today with the cookie: %s", today.raw)
	}
	if today.body["streak"].(map[string]any)["before"] != 4.0 {
		t.Errorf("streak = %v", today.body["streak"])
	}
	if !strings.Contains(today.header.Get("Set-Cookie"), "cd_daily=") {
		t.Error("reading today with a player did not renew the cookie")
	}
}

// TestANewPlayerKeepsOnlyAGeneratedName that nobody else has.
func TestANewPlayerKeepsOnlyAGeneratedName(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	typed := newBrowser(t, srv).post("/daily/142/play", map[string]any{"name": "Robert'); DROP TABLE"})
	name := typed.body["player"].(map[string]any)["name"].(string)
	if name == "Robert'); DROP TABLE" || !strings.Contains(name, " ") {
		t.Errorf("a typed name was kept: %q", name)
	}
	taken := newBrowser(t, srv).post("/daily/142/play", map[string]any{"name": name})
	if other := taken.body["player"].(map[string]any)["name"]; other == name || taken.status != http.StatusOK {
		t.Errorf("a taken name was given twice: %v, %d", other, taken.status)
	}
	if len(f.players) != 2 {
		t.Errorf("%d players", len(f.players))
	}
}

// TestChangesNeedJSONAndTheCookie: a form another site posts cannot say
// it is JSON; a move needs a player; and a move that is malformed is
// refused before anything is asked of the store.
func TestChangesNeedJSONAndTheCookie(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	stranger := newBrowser(t, srv)
	for _, r := range []reply{
		stranger.do(http.MethodPost, "/daily/142/play", "application/x-www-form-urlencoded", []byte("name=x")),
		stranger.do(http.MethodPost, "/daily/142/flip", "text/plain", []byte(`{"key":"abcdefgh","seq":0,"card":"c4"}`)),
		stranger.do(http.MethodPost, "/daily/name", "", []byte(`{}`)),
	} {
		if r.status != http.StatusUnsupportedMediaType || r.body["reason"] != "content-type" {
			t.Errorf("not JSON: %d %s", r.status, r.raw)
		}
	}
	if r := stranger.move("flip", 0, map[string]any{"card": "c4"}); r.status != http.StatusForbidden || r.body["reason"] != "cookie" {
		t.Errorf("no cookie: %d %s", r.status, r.raw)
	}
	stranger.client.Jar.SetCookies(mustURL(t, srv.URL), []*http.Cookie{{Name: "cd_daily", Value: daily.NewToken(), Secure: true}})
	if r := stranger.move("flip", 0, map[string]any{"card": "c4"}); r.status != http.StatusForbidden || r.body["reason"] != "cookie" {
		t.Errorf("a cookie nobody has: %d %s", r.status, r.raw)
	}

	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	for name, body := range map[string]map[string]any{
		"a short key":      {"key": "short", "seq": 0, "card": "c4"},
		"a key with space": {"key": "has a space", "seq": 0, "card": "c4"},
		"no seq":           {"key": "abcdefgh-1", "card": "c4"},
		"a negative seq":   {"key": "abcdefgh-2", "seq": -1, "card": "c4"},
		"no card":          {"key": "abcdefgh-3", "seq": 0},
	} {
		if r := b.post("/daily/142/flip", body); r.status != http.StatusBadRequest || r.body["reason"] != "bad" {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	if r := b.move("buy", 0, map[string]any{"kind": "trailer"}); r.status != http.StatusBadRequest || r.body["reason"] != "bad" {
		t.Errorf("buying a trailer: %d %s", r.status, r.raw)
	}
	if r := b.move("guess", 0, map[string]any{"film": "603"}); r.status != http.StatusBadRequest {
		t.Errorf("guessing a TMDb id: %d %s", r.status, r.raw)
	}
	if r := b.move("flip", 0, map[string]any{"card": "c99"}); r.status != http.StatusBadRequest || r.body["reason"] != "bad" {
		t.Errorf("a card not on the board: %d %s", r.status, r.raw)
	}
	big := b.do(http.MethodPost, "/daily/142/flip", "application/json",
		[]byte(`{"key":"abcdefgh","seq":0,"card":"`+strings.Repeat("c", dailyBody)+`"}`))
	if big.status != http.StatusBadRequest {
		t.Errorf("a body past 4 KB: %d", big.status)
	}
	if r := b.do(http.MethodPost, "/daily/142/flip", "application/json", []byte(`{"key":`)); r.status != http.StatusBadRequest {
		t.Errorf("broken JSON: %d %s", r.status, r.raw)
	}
	if r := b.post("/daily/x/flip", map[string]any{"key": "abcdefgh", "seq": 0, "card": "c4"}); r.status != http.StatusBadRequest {
		t.Errorf("a puzzle number that is not one: %d %s", r.status, r.raw)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// TestAMoveIsMadeOnceAndOnlyFromTheGameAsItStands: a retry with the same
// key is answered with the game and not charged again; a move from an
// old point is refused as stale, with the game as it is.
func TestAMoveIsMadeOnceAndOnlyFromTheGameAsItStands(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	body := map[string]any{"key": "6f1c1b7e-retry", "seq": 0, "card": "c4"}
	first := b.post("/daily/142/flip", body)
	retry := b.post("/daily/142/flip", body)
	if first.status != http.StatusOK || retry.status != http.StatusOK || ptsOf(t, first) != 945 || ptsOf(t, retry) != 945 || seqOf(t, retry) != 1 {
		t.Errorf("first %d (%d points), retry %d (%d points)", first.status, ptsOf(t, first), retry.status, ptsOf(t, retry))
	}
	stale := b.post("/daily/142/buy", map[string]any{"key": "other-tab-1", "seq": 0, "kind": "genres"})
	if stale.status != http.StatusConflict || stale.body["reason"] != "stale" || seqOf(t, stale) != 1 || ptsOf(t, stale) != 945 {
		t.Errorf("a stale move: %d %s", stale.status, stale.raw)
	}
}

// TestEachRefusalSaysWhy with the reason the page maps to its words.
func TestEachRefusalSaysWhy(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, api := dailyServer(t, f)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	expect := func(r reply, status int, reason string) {
		t.Helper()
		if r.status != status || (reason != "" && r.body["reason"] != reason) {
			t.Errorf("%d %s, want %d %s", r.status, r.raw, status, reason)
		}
	}
	expect(b.move("flip", 0, map[string]any{"card": "c2"}), http.StatusConflict, "known")
	expect(b.move("guess", 0, map[string]any{"film": "tt9999999"}), http.StatusNotFound, "unknown")
	seq := 0
	for _, kind := range []string{"year", "director", "actor", "actor", "actor", "actor"} {
		r := b.move("buy", seq, map[string]any{"kind": kind})
		if r.status != http.StatusOK {
			t.Fatalf("buy %s: %d %s", kind, r.status, r.raw)
		}
		seq++
	}
	// Fifty points left.
	expect(b.move("buy", seq, map[string]any{"kind": "year"}), http.StatusConflict, "known")
	expect(b.move("buy", seq, map[string]any{"kind": "story"}), http.StatusBadRequest, "bad")
	expect(b.move("buy", seq, map[string]any{"kind": "director"}), http.StatusConflict, "known")
	expect(b.move("buy", seq, map[string]any{"kind": "genres"}), http.StatusPaymentRequired, "points")
	expect(b.move("flip", seq, map[string]any{"card": "c6"}), http.StatusPaymentRequired, "points")
	expect(b.move("reveal", seq, nil), http.StatusOK, "")
	seq++
	expect(b.move("flip", seq, map[string]any{"card": "c6"}), http.StatusConflict, "done")
	expect(b.post("/daily/141/reveal", map[string]any{"key": "yesterday", "seq": 0}), http.StatusConflict, "day")

	// Past midnight the page still has yesterday's number, and today's
	// puzzle has no game yet.
	tomorrow := matrixPuzzle()
	tomorrow.No, tomorrow.Day = 143, tomorrow.Day.AddDate(0, 0, 1)
	f.mu.Lock()
	f.puzzles[143] = tomorrow
	f.mu.Unlock()
	api.daily.now = func() time.Time { return todayAt.Add(24 * time.Hour) }
	expect(b.move("reveal", seq, nil), http.StatusConflict, "day")
	expect(b.post("/daily/143/reveal", map[string]any{"key": "tomorrow", "seq": 0}), http.StatusNotFound, "no-game")
}

// yearSaid is everywhere a response says year other than as a movie's
// own: every number or string that is the year, by its path, unless it
// is the "year" of an object with an "id", a card or a movie saying its
// own year, as every card does. The year clue's entry has no id.
func yearSaid(raw string, year int) []string {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return []string{"(not JSON)"}
	}
	var out []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			_, movie := x["id"]
			for k, e := range x {
				if !movie || k != "year" {
					walk(path+"."+k, e)
				}
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		case float64:
			if x == float64(year) {
				out = append(out, path)
			}
		case string:
			if x == fmt.Sprint(year) {
				out = append(out, path)
			}
		}
	}
	walk("", v)
	slices.Sort(out)
	return out
}

// yearEntries are where a response's game logs the year clue.
func yearEntries(body map[string]any) []string {
	g, _ := body["game"].(map[string]any)
	log, _ := g["log"].([]any)
	var out []string
	for i, e := range log {
		if e.(map[string]any)["type"] == "year" {
			out = append(out, fmt.Sprintf(".game.log[%d].year", i))
		}
	}
	return out
}

// TestNothingBeforeTheEndNamesTheAnswer: every response a game gets,
// from the first read through every kind of move short of the end,
// keeps the answer and every face-down card to itself, and says the
// answer's year nowhere but in the year clue's entry once it is bought.
// Being John Malkovich, a starting card, is from the same year, and
// says so as its own. The end names it.
func TestNothingBeforeTheEndNamesTheAnswer(t *testing.T) {
	p := matrixPuzzle()
	f := newFakeDaily(p)
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	up := map[string]bool{"c2": true, "c3": true, "c8": true}
	check := func(r reply, when string) {
		t.Helper()
		if r.status != http.StatusOK {
			t.Fatalf("%s: %d %s", when, r.status, r.raw)
		}
		for _, secret := range []string{p.Answer.ID, p.Answer.Title} {
			if strings.Contains(r.raw, `"`+secret+`"`) {
				t.Errorf("%s: the answer's %q is in %s", when, secret, r.raw)
			}
		}
		for _, c := range p.Cards {
			if up[c.ID] {
				continue
			}
			for _, secret := range []string{c.Film, c.Title} {
				if strings.Contains(r.raw, `"`+secret+`"`) {
					t.Errorf("%s: face-down %s's %q is in the body", when, c.ID, secret)
				}
			}
		}
		if got, want := yearSaid(r.raw, p.Answer.Year), yearEntries(r.body); !slices.Equal(got, want) {
			t.Errorf("%s: the answer's year is said at %v, want %v", when, got, want)
		}
	}
	check(b.get("/daily"), "at load")
	check(b.post("/daily/142/play", map[string]any{}), "Play")
	check(b.get("/daily"), "reading the game")
	seq := 0
	for _, m := range []struct {
		verb string
		body map[string]any
		ups  string
	}{
		{"flip", map[string]any{"card": "c7"}, "c7"},
		{"flip", map[string]any{"card": "c1"}, ""},
		{"buy", map[string]any{"kind": "director"}, ""},
		{"buy", map[string]any{"kind": "actor"}, ""},
		{"buy", map[string]any{"kind": "genres"}, ""},
		{"guess", map[string]any{"film": "tt0111257"}, "c4"},
		{"guess", map[string]any{"film": "tt0034583"}, ""},
		{"buy", map[string]any{"kind": "year"}, ""},
	} {
		if m.ups != "" {
			up[m.ups] = true
		}
		r := b.move(m.verb, seq, m.body)
		check(r, fmt.Sprintf("%s %v", m.verb, m.body))
		seq++
	}
	read := b.get("/daily")
	check(read, "reading the game again")
	if len(yearEntries(read.body)) != 1 {
		t.Errorf("the year bought is not in the log: %s", read.raw)
	}
	stale := b.move("buy", 0, map[string]any{"kind": "year"})
	if stale.status != http.StatusConflict {
		t.Fatalf("stale = %d", stale.status)
	}
	stale.status = http.StatusOK
	check(stale, "a stale move")

	end := b.move("guess", seq, map[string]any{"film": p.Answer.ID})
	if !strings.Contains(end.raw, `"tt0133093"`) || !strings.Contains(end.raw, `"The Matrix"`) {
		t.Errorf("the end does not name the answer: %s", end.raw)
	}
	g := gameOf(t, end)
	if g["phase"] != "done" || g["won"] != true || g["end"] == nil {
		t.Errorf("after the right guess: %v", g)
	}
}

// TestCrawlersAreNeverPlayers: a search engine's POST gets nothing, and
// no cookie.
func TestCrawlersAreNeverPlayers(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	b.ua = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	r := b.post("/daily/142/play", map[string]any{})
	if r.status != http.StatusNoContent || r.header.Get("Set-Cookie") != "" || len(f.players) != 0 {
		t.Errorf("a crawler's Play: %d, cookie %q, %d players", r.status, r.header.Get("Set-Cookie"), len(f.players))
	}
	if r := b.get("/daily"); r.status != http.StatusOK {
		t.Errorf("a crawler reading today: %d", r.status)
	}
}

// TestNewPlayersAreLimitedPerAddress: ten at once from one address, then
// a refusal saying when to try again; another address, or a player who
// already has a cookie, is not held up.
func TestNewPlayersAreLimitedPerAddress(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	var kept *browser
	for i := range joinBurst {
		b := newBrowser(t, srv)
		if r := b.post("/daily/142/play", map[string]any{}); r.status != http.StatusOK {
			t.Fatalf("player %d: %d %s", i+1, r.status, r.raw)
		}
		kept = b
	}
	r := newBrowser(t, srv).post("/daily/142/play", map[string]any{})
	if r.status != http.StatusTooManyRequests || r.body["reason"] != "busy" || r.header.Get("Retry-After") == "" {
		t.Errorf("the eleventh: %d %s, Retry-After %q", r.status, r.raw, r.header.Get("Retry-After"))
	}
	if r := kept.post("/daily/142/play", map[string]any{}); r.status != http.StatusOK {
		t.Errorf("a player with a cookie, from the same address: %d", r.status)
	}
	other := newBrowser(t, srv)
	other.ip = "216.160.83.56"
	if r := other.post("/daily/142/play", map[string]any{}); r.status != http.StatusOK {
		t.Errorf("another address: %d", r.status)
	}
	// Only Play is limited: the address can still read and move.
	if r := kept.move("flip", 0, map[string]any{"card": "c4"}); r.status != http.StatusOK {
		t.Errorf("a move from the busy address: %d", r.status)
	}
}

func TestTheJoinLimiterRefillsOneEverySixMinutes(t *testing.T) {
	l := newJoinLimiter()
	at := todayAt
	for range joinBurst {
		if !l.allow("81.2.69.142", at) {
			t.Fatal("refused within the burst")
		}
	}
	if l.allow("81.2.69.142", at) {
		t.Error("allowed past the burst")
	}
	if l.allow("81.2.69.142", at.Add(5*time.Minute)) {
		t.Error("allowed before a token came back")
	}
	if !l.allow("81.2.69.142", at.Add(joinEvery)) {
		t.Error("refused six minutes on")
	}
}

// TestANewNameIsKeptOnlyForAPlayer: without a cookie it is an offer,
// and nothing is written; with one the player is renamed. Neither is
// ever the name they had.
func TestANewNameIsKeptOnlyForAPlayer(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	for range 20 {
		r := b.post("/daily/name", map[string]any{"name": "Trinity Kimble"})
		if r.status != http.StatusOK || r.body["saved"] != false || r.body["name"] == "Trinity Kimble" {
			t.Fatalf("an offer: %d %s", r.status, r.raw)
		}
	}
	if r := b.do(http.MethodPost, "/daily/name", "application/json", nil); r.status != http.StatusOK {
		t.Errorf("an empty body: %d %s", r.status, r.raw)
	}
	if len(f.players) != 0 || b.client.Jar.Cookies(mustURL(t, srv.URL)) != nil {
		t.Errorf("an offer made %d players", len(f.players))
	}
	was := b.post("/daily/142/play", map[string]any{}).body["player"].(map[string]any)["name"]
	r := b.post("/daily/name", map[string]any{})
	if r.status != http.StatusOK || r.body["saved"] != true || r.body["name"] == was {
		t.Errorf("a rename: %d %s (was %v)", r.status, r.raw, was)
	}
	if today := b.get("/daily"); today.body["player"].(map[string]any)["name"] != r.body["name"] {
		t.Errorf("after the rename today says %v", today.body["player"])
	}
}

// TestTheBoardIsReadForTheReader: the tab and the player are the
// store's to rank by; a tab that is neither, a puzzle that does not
// exist, and one whose day has not come are refused.
func TestTheBoardIsReadForTheReader(t *testing.T) {
	tomorrow := matrixPuzzle()
	tomorrow.No, tomorrow.Day = 143, tomorrow.Day.AddDate(0, 0, 1)
	f := newFakeDaily(matrixPuzzle(), tomorrow)
	srv, _ := dailyServer(t, f)
	anyone := newBrowser(t, srv)
	if r := anyone.get("/daily/142/board"); r.status != http.StatusOK || r.body["tab"] != "today" || r.body["you"] != nil {
		t.Errorf("without a player: %d %s", r.status, r.raw)
	}
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	r := b.get("/daily/142/board?tab=week")
	if r.status != http.StatusOK || r.body["tab"] != "week" || r.body["you"] == nil {
		t.Errorf("the week: %d %s", r.status, r.raw)
	}
	if rows := r.body["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["you"] != true {
		t.Errorf("rows = %v", rows)
	}
	if want := []string{"142/today/0", "142/week/1"}; !slices.Equal(f.boards, want) {
		t.Errorf("asked %v, want %v", f.boards, want)
	}
	for path, status := range map[string]int{
		"/daily/142/board?tab=streak": http.StatusBadRequest,
		"/daily/9/board":              http.StatusNotFound,
		"/daily/143/board":            http.StatusNotFound,
		"/daily/zero/board":           http.StatusBadRequest,
	} {
		if r := b.get(path); r.status != status || r.header.Get("Cache-Control") != "no-store" {
			t.Errorf("GET %s: %d %s, want %d", path, r.status, r.raw, status)
		}
	}
}

// TestADatabaseFailureIsUnavailableAndSaysNoMore: the store's own error
// stays in the log.
func TestADatabaseFailureIsUnavailableAndSaysNoMore(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	f.fail = errors.New("failed to connect to `user=postgres database=railway`: connection refused")
	srv, _ := dailyServer(t, f)
	r := newBrowser(t, srv).get("/daily")
	if r.status != http.StatusServiceUnavailable || r.body["reason"] != "unavailable" || strings.Contains(r.raw, "postgres") {
		t.Errorf("a failing store: %d %s", r.status, r.raw)
	}
}

// clock is a test's clock, moved between requests while the server
// reads it.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

// dailyAt serves f with the clock at the moment given, and the clock.
func dailyAt(t *testing.T, f DailyStore, at time.Time) (*httptest.Server, *clock) {
	t.Helper()
	srv, api := dailyServer(t, f)
	c := &clock{at: at}
	api.daily.now = c.now
	return srv, c
}

// puzzleOn is the Matrix puzzle as No. no on day.
func puzzleOn(no int, day string) *daily.Puzzle {
	p := matrixPuzzle()
	p.No = no
	p.Day, _ = time.Parse("2006-01-02", day)
	return p
}

// TestTheZoneIsTheReadersOrElseUTC: a zone the page names is used, and
// anything else, however it was made, is UTC.
func TestTheZoneIsTheReadersOrElseUTC(t *testing.T) {
	for query, want := range map[string]string{
		"tz=Asia/Tokyo":         "Asia/Tokyo",
		"tz=America%2FNew_York": "America/New_York",
		"tz=Pacific/Kiritimati": "Pacific/Kiritimati",
		"tz=UTC":                "UTC",
		"":                      "UTC",
		"tz=":                   "UTC",
		"tz=Not/AZone":          "UTC",
		"tz=asia%20tokyo":       "UTC",
		"tz=" + strings.Repeat("A", daily.ZoneMax+1): "UTC",
		"tz=Europe/London" + strings.Repeat("x", 60): "UTC",
		"tz=../../etc":              "UTC",
		"tz=..%2F..%2Fetc%2Fpasswd": "UTC",
		"tz=%2Fetc%2Flocaltime":     "UTC",
		"tz=Asia%2F..%2F..%2Fetc":   "UTC",
		"tz=Europe%5CLondon":        "UTC",
		"tz=Asia/Tokyo%00":          "UTC",
		"tz=%3Cscript%3E":           "UTC",
		"tz=Local":                  "UTC",
		"tz=zoneinfo.zip":           "UTC",
	} {
		r := httptest.NewRequest(http.MethodGet, "/daily?"+query, nil)
		if got := zoneOf(r).String(); got != want {
			t.Errorf("zoneOf(?%s) = %s, want %s", query, got, want)
		}
	}
}

// TestEachReaderGetsThePuzzleForTheirOwnDate: at 23:30 UTC on 8
// October, Tokyo and Kiritimati are on the 9th's puzzle and Los Angeles
// and a page that names no zone on the 8th's, each counting down to its
// own midnight. Both puzzles are live at once, and each is read from
// the store once however the readers alternate.
func TestEachReaderGetsThePuzzleForTheirOwnDate(t *testing.T) {
	f := newFakeDaily(puzzleOn(142, "2026-10-08"), puzzleOn(143, "2026-10-09"))
	srv, _ := dailyAt(t, f, time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC))
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	for range 2 {
		for _, c := range []struct {
			query      string
			no         float64
			date, next string
		}{
			{"?tz=Asia/Tokyo", 143, "2026-10-09", "2026-10-09T15:00:00Z"},
			{"?tz=America/Los_Angeles", 142, "2026-10-08", "2026-10-09T07:00:00Z"},
			{"?tz=Pacific/Kiritimati", 143, "2026-10-09", "2026-10-09T10:00:00Z"},
			{"", 142, "2026-10-08", "2026-10-09T00:00:00Z"},
			{"?tz=Nowhere/Special", 142, "2026-10-08", "2026-10-09T00:00:00Z"},
		} {
			r := b.get("/daily" + c.query)
			if r.status != http.StatusOK || r.body["no"] != c.no || r.body["date"] != c.date || r.body["next"] != c.next ||
				r.body["now"] != "2026-10-08T23:30:00Z" {
				t.Errorf("GET /daily%s: %d no %v, date %v, next %v, now %v; want No. %v on %s, next %s",
					c.query, r.status, r.body["no"], r.body["date"], r.body["next"], r.body["now"], c.no, c.date, c.next)
			}
			// The game started on the 8th is the 8th's, and only there.
			if hasGame := r.body["game"] != nil; hasGame != (c.no == 142) {
				t.Errorf("GET /daily%s: game %v", c.query, r.body["game"])
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	days := slices.Clone(f.days)
	slices.Sort(days)
	if len(slices.Compact(days)) != len(f.days) {
		t.Errorf("the store was asked for %v, want each day once at most", f.days)
	}
	// The streak ends at the reader's own puzzle: Tokyo's and
	// Kiritimati's at the 9th's, the others at the 8th's.
	if want := []int{143, 142, 143, 142, 142, 143, 142, 143, 142, 142}; !slices.Equal(f.streaks, want) {
		t.Errorf("streaks asked for %v, want %v", f.streaks, want)
	}
}

// TestNextIsLocalMidnightAcrossTheClockChange: London's clocks go back
// at two on Sunday 25 October, so that day is 25 hours long, and the
// Saturday before still ends at midnight on summer time.
func TestNextIsLocalMidnightAcrossTheClockChange(t *testing.T) {
	f := newFakeDaily(puzzleOn(158, "2026-10-24"), puzzleOn(159, "2026-10-25"))
	srv, clk := dailyAt(t, f, time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC))
	b := newBrowser(t, srv)
	if r := b.get("/daily?tz=Europe/London"); r.body["no"] != 158.0 || r.body["next"] != "2026-10-24T23:00:00Z" {
		t.Errorf("23:30 BST on the 24th: No. %v, next %v", r.body["no"], r.body["next"])
	}
	clk.set(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC))
	if r := b.get("/daily?tz=Europe/London"); r.body["no"] != 159.0 || r.body["next"] != "2026-10-26T00:00:00Z" {
		t.Errorf("01:30 BST on the 25th: No. %v, next %v, want midnight GMT, 23½ hours on", r.body["no"], r.body["next"])
	}
}

// TestAGameIsPlayedOnItsDayInTheZoneItWasStartedIn: a game started in
// Tokyo plays on while it is still that day in Tokyo, whatever zone a
// move names, and once Tokyo's date has turned it is over, even for a
// request that claims Los Angeles, where the day still has hours to
// run. Pressing Play again from there does not bring it back.
func TestAGameIsPlayedOnItsDayInTheZoneItWasStartedIn(t *testing.T) {
	f := newFakeDaily(puzzleOn(143, "2026-10-09"))
	// 17:00 in Tokyo, 01:00 in Los Angeles, both on the 9th.
	srv, clk := dailyAt(t, f, time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC))
	b := newBrowser(t, srv)
	if r := b.post("/daily/143/play?tz=Asia/Tokyo", map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("play in Tokyo: %d %s", r.status, r.raw)
	}
	for _, rec := range f.games {
		if rec.Zone != "Asia/Tokyo" {
			t.Errorf("the game was kept in %q", rec.Zone)
		}
	}
	flip := func(key string, seq int, query string) reply {
		return b.post("/daily/143/flip"+query, map[string]any{"key": key, "seq": seq, "card": "c4"})
	}
	if r := flip("from-la-1", 0, "?tz=America/Los_Angeles"); r.status != http.StatusOK {
		t.Errorf("a move naming Los Angeles while Tokyo is on the 9th: %d %s", r.status, r.raw)
	}
	// 00:30 on the 10th in Tokyo; 08:30 on the 9th in Los Angeles.
	clk.set(time.Date(2026, 10, 9, 15, 30, 0, 0, time.UTC))
	for i, query := range []string{"?tz=America/Los_Angeles", "?tz=Asia/Tokyo", ""} {
		if r := b.post("/daily/143/buy"+query, map[string]any{"key": fmt.Sprintf("too-late-%d", i), "seq": 1, "kind": "genres"}); r.status != http.StatusConflict || r.body["reason"] != "day" {
			t.Errorf("a move past Tokyo's midnight naming %q: %d %s", query, r.status, r.raw)
		}
	}
	// The retry of a move made in time is still answered.
	if r := flip("from-la-1", 0, "?tz=America/Los_Angeles"); r.status != http.StatusOK || seqOf(t, r) != 1 {
		t.Errorf("a retry past midnight: %d %s", r.status, r.raw)
	}
	if r := b.post("/daily/143/play?tz=America/Los_Angeles", map[string]any{}); r.status != http.StatusConflict || r.body["reason"] != "day" {
		t.Errorf("Play again from Los Angeles: %d %s", r.status, r.raw)
	}
	// A reader in Los Angeles starts their own game of the 9th's puzzle.
	la := newBrowser(t, srv)
	la.ip = "216.160.83.56"
	if r := la.post("/daily/143/play?tz=America/Los_Angeles", map[string]any{}); r.status != http.StatusOK {
		t.Errorf("play in Los Angeles on its 9th: %d %s", r.status, r.raw)
	}
	// And nobody can start the 9th's puzzle from Tokyo now.
	tokyo := newBrowser(t, srv)
	tokyo.ip = "1.0.16.1"
	if r := tokyo.post("/daily/143/play?tz=Asia/Tokyo", map[string]any{}); r.status != http.StatusConflict || r.body["reason"] != "day" {
		t.Errorf("play from Tokyo on its 10th: %d %s", r.status, r.raw)
	}
}

// TestAGameLeftBehindAtItsMidnightGivesWayToItsZonesNextDay: a reader
// who started the 9th's puzzle in Tokyo, and reads from Los Angeles
// after Tokyo's midnight, is not shown a game that can no longer be
// moved. They get the day Tokyo has moved on to, as they would have
// there, with its own countdown; while Tokyo is still on the 9th, and
// once the game is finished, they get Los Angeles's day as anyone does.
func TestAGameLeftBehindAtItsMidnightGivesWayToItsZonesNextDay(t *testing.T) {
	f := newFakeDaily(puzzleOn(143, "2026-10-09"), puzzleOn(144, "2026-10-10"))
	// 17:00 in Tokyo, 01:00 in Los Angeles, both on the 9th.
	srv, clk := dailyAt(t, f, time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC))
	b := newBrowser(t, srv)
	if r := b.post("/daily/143/play?tz=Asia/Tokyo", map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("play in Tokyo: %d %s", r.status, r.raw)
	}
	if r := b.get("/daily?tz=America/Los_Angeles"); r.status != http.StatusOK || r.body["no"] != 143.0 || r.body["game"] == nil {
		t.Errorf("Los Angeles while Tokyo is on the 9th: %d %s", r.status, r.raw)
	}
	// 00:30 on the 10th in Tokyo; 08:30 on the 9th in Los Angeles.
	clk.set(time.Date(2026, 10, 9, 15, 30, 0, 0, time.UTC))
	r := b.get("/daily?tz=America/Los_Angeles")
	if r.status != http.StatusOK || r.body["no"] != 144.0 || r.body["date"] != "2026-10-10" {
		t.Fatalf("Los Angeles after Tokyo's midnight: %d %s", r.status, r.raw)
	}
	if r.body["game"] != nil {
		t.Errorf("the 10th has a game already: %s", r.raw)
	}
	if r.body["next"] != "2026-10-10T15:00:00Z" {
		t.Errorf("next = %v, want Tokyo's next midnight", r.body["next"])
	}
	// What read offers can be played, though the page still names Los
	// Angeles, and in Tokyo, where it is that day: Play, a move, the
	// board, and the game read back.
	if r := b.post("/daily/144/play?tz=America/Los_Angeles", map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("Play on the day read moved them on to: %d %s", r.status, r.raw)
	}
	f.mu.Lock()
	for key, rec := range f.games {
		if key[1] == 144 && rec.Zone != "Asia/Tokyo" {
			t.Errorf("No. 144 was started in %q, want Tokyo, whose day it is", rec.Zone)
		}
	}
	f.mu.Unlock()
	if r := b.post("/daily/144/flip?tz=America/Los_Angeles", map[string]any{"key": "on-the-10th", "seq": 0, "card": "c4"}); r.status != http.StatusOK {
		t.Errorf("a move on No. 144: %d %s", r.status, r.raw)
	}
	if r := b.get("/daily/144/board?tz=America/Los_Angeles"); r.status != http.StatusOK {
		t.Errorf("No. 144's board: %d %s", r.status, r.raw)
	}
	if r := b.get("/daily?tz=America/Los_Angeles"); r.body["no"] != 144.0 || r.body["game"] == nil || seqOf(t, r) != 1 {
		t.Errorf("Los Angeles after playing No. 144: %d %s", r.status, r.raw)
	}
	// Without the cookie, Los Angeles is still on the 9th, and the 10th
	// can be neither played nor its board opened.
	la := newBrowser(t, srv)
	if r := la.get("/daily?tz=America/Los_Angeles"); r.body["no"] != 143.0 {
		t.Errorf("a stranger in Los Angeles: %d %s", r.status, r.raw)
	}
	if r := la.post("/daily/144/play?tz=America/Los_Angeles", map[string]any{}); r.status != http.StatusConflict || r.body["reason"] != "day" {
		t.Errorf("a stranger's Play on No. 144 from Los Angeles: %d %s", r.status, r.raw)
	}
	if r := la.get("/daily/144/board?tz=America/Los_Angeles"); r.status != http.StatusNotFound || r.body["reason"] != "no-puzzle" {
		t.Errorf("a stranger's board of No. 144 from Los Angeles: %d %s", r.status, r.raw)
	}
	// Nor is a player who left no game behind moved on: one playing the
	// 9th in Los Angeles is held to the 9th there.
	if r := la.post("/daily/143/play?tz=America/Los_Angeles", map[string]any{}); r.status != http.StatusOK {
		t.Fatalf("Play on the 9th in Los Angeles: %d %s", r.status, r.raw)
	}
	if r := la.post("/daily/144/play?tz=America/Los_Angeles", map[string]any{}); r.status != http.StatusConflict || r.body["reason"] != "day" {
		t.Errorf("Play on No. 144 by a player on the 9th in Los Angeles: %d %s", r.status, r.raw)
	}
	if r := la.get("/daily/144/board?tz=America/Los_Angeles"); r.status != http.StatusNotFound {
		t.Errorf("No. 144's board for a player on the 9th in Los Angeles: %d %s", r.status, r.raw)
	}
}

// TestABoardOpensOnTheReadersDate: the 9th's board is there for a reader
// already on the 9th, and not for one still on the 8th.
func TestABoardOpensOnTheReadersDate(t *testing.T) {
	f := newFakeDaily(puzzleOn(142, "2026-10-08"), puzzleOn(143, "2026-10-09"))
	srv, _ := dailyAt(t, f, time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC))
	b := newBrowser(t, srv)
	if r := b.get("/daily/143/board?tz=Asia/Tokyo"); r.status != http.StatusOK {
		t.Errorf("the 9th's board in Tokyo: %d %s", r.status, r.raw)
	}
	for _, query := range []string{"", "&tz=America/Los_Angeles"} {
		if r := b.get("/daily/143/board?tab=today" + query); r.status != http.StatusNotFound || r.body["reason"] != "no-puzzle" {
			t.Errorf("the 9th's board on the 8th (%q): %d %s", query, r.status, r.raw)
		}
	}
	if r := b.get("/daily/142/board?tz=Asia/Tokyo"); r.status != http.StatusOK {
		t.Errorf("the 8th's board from Tokyo: %d %s", r.status, r.raw)
	}
}

// TestAPuzzleNumberPastWhatTheDatabaseHoldsIsMalformed: a puzzle's
// number is an int4, so a bigger one is refused as bad before the store
// is asked, rather than failing to encode there and reading as an
// outage. The biggest an int4 holds is asked about, and is no puzzle.
func TestAPuzzleNumberPastWhatTheDatabaseHoldsIsMalformed(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	move := map[string]any{"key": "abcdefgh-big", "seq": 0, "card": "c4"}
	for path, r := range map[string]reply{
		"Play":  b.post("/daily/99999999999/play", map[string]any{}),
		"flip":  b.post("/daily/2147483648/flip", move),
		"board": b.get("/daily/99999999999/board"),
		"minus": b.get("/daily/-1/board"),
	} {
		if r.status != http.StatusBadRequest || r.body["reason"] != "bad" {
			t.Errorf("%s: %d %s", path, r.status, r.raw)
		}
	}
	if r := b.post("/daily/2147483647/flip", move); r.status != http.StatusConflict || r.body["reason"] != "day" {
		t.Errorf("the biggest int4: %d %s", r.status, r.raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, no := range f.nos {
		if no > 2147483647 {
			t.Errorf("the store was asked for No. %d", no)
		}
	}
	if !slices.Contains(f.nos, 2147483647) {
		t.Errorf("asked for %v, want the biggest int4 among them", f.nos)
	}
}

// TestANewPlayerKeepsOnlyANameTheyWereOffered: a name the pool could
// draw, but that nobody was offered, is drawn afresh at Play, so no page
// can put a first word and a last word of its choosing on the board; a
// name offered by GET /daily or New name is kept, until it is twelve
// hours old.
func TestANewPlayerKeepsOnlyANameTheyWereOffered(t *testing.T) {
	f := newFakeDaily(puzzleOn(143, "2026-10-09"))
	srv, api := dailyServer(t, f)
	clk := &clock{at: time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)}
	api.daily.now = clk.now
	var draws atomic.Int32
	api.daily.intn = func(n int) int {
		draws.Add(1)
		return rand.IntN(n)
	}
	chosen := "Marty Starling"
	if !api.daily.names.Names(context.Background()).Valid(chosen) {
		t.Fatalf("%q is not a name the pool could draw", chosen)
	}
	play := func(b *browser, name string) (string, int32) {
		t.Helper()
		draws.Store(0)
		r := b.post("/daily/143/play", map[string]any{"name": name})
		if r.status != http.StatusOK {
			t.Fatalf("Play with %q: %d %s", name, r.status, r.raw)
		}
		return r.body["player"].(map[string]any)["name"].(string), draws.Load()
	}

	if name, drawn := play(newBrowser(t, srv), chosen); drawn == 0 {
		t.Errorf("a name nobody was offered: kept %q without a draw", name)
	}
	read := newBrowser(t, srv)
	offered := read.get("/daily").body["player"].(map[string]any)["name"].(string)
	if name, drawn := play(read, offered); name != offered || drawn != 0 {
		t.Errorf("the name GET /daily offered: kept %q, %d draws, want %q", name, drawn, offered)
	}
	spun := newBrowser(t, srv)
	another := spun.post("/daily/name", map[string]any{}).body["name"].(string)
	if name, drawn := play(spun, another); name != another || drawn != 0 {
		t.Errorf("the name New name offered: kept %q, %d draws, want %q", name, drawn, another)
	}
	late := newBrowser(t, srv)
	stale := late.get("/daily").body["player"].(map[string]any)["name"].(string)
	clk.set(clk.now().Add(offerLife))
	if _, drawn := play(late, stale); drawn == 0 {
		t.Errorf("an offer %s old was kept without a draw", offerLife)
	}
}

// TestOffersAreKeptTwelveHoursAndBounded: an offer is good until it is
// twelve hours old; past offerMax the stale ones are dropped, and if
// none is stale, all of them.
func TestOffersAreKeptTwelveHoursAndBounded(t *testing.T) {
	o := newNameOffers()
	at := todayAt
	o.add("Ellen Kimble", at)
	if !o.has("Ellen Kimble", at.Add(offerLife-time.Second)) || o.has("Ellen Kimble", at.Add(offerLife)) || o.has("Rick Ripley", at) {
		t.Error("an offer is not good for exactly offerLife")
	}
	for i := len(o.at); i < offerMax; i++ {
		o.add(fmt.Sprintf("Offer %d", i), at.Add(time.Hour))
	}
	o.add("Marty Jones", at.Add(offerLife+time.Minute))
	if len(o.at) != offerMax || o.has("Ellen Kimble", at) || !o.has("Offer 7", at.Add(offerLife)) {
		t.Errorf("past the cap with one stale offer: %d held", len(o.at))
	}
	o.add("Clarice Connor", at.Add(offerLife+2*time.Minute))
	if len(o.at) != 1 || !o.has("Clarice Connor", at.Add(offerLife+2*time.Minute)) {
		t.Errorf("past the cap with none stale: %d held", len(o.at))
	}
}

// TestMeWithoutAPlayerIsNothingAndReadsNothing: the start screen's
// banner asks on every visit, so a reader with no player, or a cookie
// nobody has, is answered with no streak and no week without the store
// being asked anything, and given no cookie.
func TestMeWithoutAPlayerIsNothingAndReadsNothing(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	for _, when := range []string{"no cookie", "a cookie nobody has"} {
		r := b.get("/daily/me?tz=Europe/London")
		if r.status != http.StatusOK || r.raw != `{"streak":0,"week":null}`+"\n" {
			t.Errorf("%s: %d %q", when, r.status, r.raw)
		}
		if r.header.Get("Set-Cookie") != "" || r.header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Set-Cookie %q, Cache-Control %q", when, r.header.Get("Set-Cookie"), r.header.Get("Cache-Control"))
		}
		b.client.Jar.SetCookies(mustURL(t, srv.URL), []*http.Cookie{{Name: "cd_daily", Value: daily.NewToken(), Secure: true}})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.days)+len(f.nos)+len(f.streaks)+len(f.standings) != 0 || len(f.players) != 0 {
		t.Errorf("the store was asked for days %v, puzzles %v, streaks %v, standings %v", f.days, f.nos, f.streaks, f.standings)
	}
}

// TestMeIsTheTitleScreensStreakAndTheWeeksStanding: before the game,
// the run today can extend and the standing over the days before
// today; once the game is finished, today's run and the standing
// through today, worked out at once, since finishing is a new key. In
// between each is kept a minute, and the cookie is renewed as GET
// /daily renews it.
func TestMeIsTheTitleScreensStreakAndTheWeeksStanding(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, clk := dailyAt(t, f, todayAt)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	asked := func() (streaks []int, standings []string) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return slices.Clone(f.streaks), slices.Clone(f.standings)
	}
	me := func(when, want string) {
		t.Helper()
		r := b.get("/daily/me")
		if r.status != http.StatusOK || r.raw != want+"\n" {
			t.Errorf("%s: %d %s, want %s", when, r.status, r.raw, want)
		}
		if !strings.Contains(r.header.Get("Set-Cookie"), "cd_daily=") {
			t.Errorf("%s: the cookie was not renewed", when)
		}
	}

	me("before the game", `{"streak":4,"week":{"rank":2048,"players":83500}}`)
	me("again within the minute", `{"streak":4,"week":{"rank":2048,"players":83500}}`)
	if streaks, standings := asked(); !slices.Equal(streaks, []int{142}) || !slices.Equal(standings, []string{"142/1/false"}) {
		t.Errorf("within the minute the store was asked for streaks %v and standings %v, want each once", streaks, standings)
	}

	b.move("flip", 0, map[string]any{"card": "c4"})
	me("a game in play", `{"streak":4,"week":{"rank":2048,"players":83500}}`)
	if r := b.move("guess", 1, map[string]any{"film": "tt0133093"}); r.status != http.StatusOK {
		t.Fatalf("the right guess: %d %s", r.status, r.raw)
	}
	me("after the game", `{"streak":5,"week":{"rank":1204,"players":83500}}`)
	me("after the game, again", `{"streak":5,"week":{"rank":1204,"players":83500}}`)
	if _, standings := asked(); !slices.Equal(standings, []string{"142/1/false", "142/1/true"}) {
		t.Errorf("standings asked for %v, want before and after the game once each", standings)
	}
	clk.set(todayAt.Add(standingLife))
	me("a minute on", `{"streak":5,"week":{"rank":1204,"players":83500}}`)
	if _, standings := asked(); len(standings) != 3 {
		t.Errorf("a minute on the standing was not asked again: %v", standings)
	}

	// No points this week: no week.
	f.mu.Lock()
	f.weeks[true] = nil
	f.mu.Unlock()
	clk.set(todayAt.Add(2 * standingLife))
	me("with no points this week", `{"streak":5,"week":null}`)
}

// TestMeIsForTheReadersOwnPuzzle: at 23:30 UTC on 8 October, a player
// whose game of the 8th's puzzle was played in UTC gets the 9th's
// standing in Tokyo, where it is the 9th and they have not played, and
// the 8th's in Los Angeles and with no zone. Each is kept under its own
// puzzle. A day with no puzzle yet is not ready.
func TestMeIsForTheReadersOwnPuzzle(t *testing.T) {
	f := newFakeDaily(puzzleOn(142, "2026-10-08"), puzzleOn(143, "2026-10-09"))
	srv, clk := dailyAt(t, f, time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC))
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	b.move("reveal", 0, nil)
	for _, query := range []string{"?tz=Asia/Tokyo", "?tz=America/Los_Angeles", "", "?tz=Pacific/Kiritimati"} {
		if r := b.get("/daily/me" + query); r.status != http.StatusOK {
			t.Errorf("GET /daily/me%s: %d %s", query, r.status, r.raw)
		}
	}
	f.mu.Lock()
	if want := []string{"143/1/false", "142/1/true"}; !slices.Equal(f.standings, want) {
		t.Errorf("standings asked for %v, want %v", f.standings, want)
	}
	if want := []int{143, 142}; !slices.Equal(f.streaks, want) {
		t.Errorf("streaks asked for %v, want %v", f.streaks, want)
	}
	f.mu.Unlock()
	clk.set(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if r := b.get("/daily/me?tz=Asia/Tokyo"); r.status != http.StatusServiceUnavailable || r.body["reason"] != "not-ready" {
		t.Errorf("a day with no puzzle: %d %s", r.status, r.raw)
	}
}

// TestStandingsAreKeptAMinuteAndBounded: an answer is good until it is
// a minute old; past standingMax the stale ones are dropped, and if
// none is stale, all of them.
func TestStandingsAreKeptAMinuteAndBounded(t *testing.T) {
	c := newStandings()
	at := todayAt
	first := standingKey{player: 1, no: 142}
	c.put(first, dailyMe{Streak: 3}, at)
	if me, ok := c.get(first, at.Add(standingLife-time.Second)); !ok || me.Streak != 3 {
		t.Error("an answer is not kept for its minute")
	}
	if _, ok := c.get(first, at.Add(standingLife)); ok {
		t.Error("an answer is kept past its minute")
	}
	if _, ok := c.get(standingKey{player: 1, no: 142, finished: true}, at); ok {
		t.Error("finishing the game found the answer kept from before it")
	}
	for i := len(c.at); i < standingMax; i++ {
		c.put(standingKey{player: int64(100 + i), no: 142}, dailyMe{}, at.Add(30*time.Second))
	}
	c.put(standingKey{player: 2, no: 142}, dailyMe{}, at.Add(standingLife+time.Second))
	if _, ok := c.at[first]; ok || len(c.at) != standingMax {
		t.Errorf("past the cap with one stale answer: %d held, the stale one kept %v", len(c.at), ok)
	}
	c.put(standingKey{player: 3, no: 142}, dailyMe{}, at.Add(standingLife+2*time.Second))
	if len(c.at) != 1 {
		t.Errorf("past the cap with none stale: %d held", len(c.at))
	}
}
