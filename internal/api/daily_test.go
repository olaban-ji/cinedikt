package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
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

// matrixPuzzle is No. 142: The Matrix, two directors and the six in
// reveal order, sixth-billed first, with the Movies sheets: slot 0 Joe
// Pantoliano, 1 Gloria Foster, 2 Hugo Weaving, 3 Carrie-Anne Moss, 4
// Laurence Fishburne and 5 Keanu Reeves, the star.
func matrixPuzzle() *daily.Puzzle {
	p := &daily.Puzzle{
		No:  142,
		Day: daily.Today(todayAt),
		Answer: daily.Answer{ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, MD: 331, Length: 136,
			Colour: "#26382d", Genres: []string{"Action", "Sci-Fi"}},
		Directors: []daily.Named{{ID: "nm0905154", Name: "Lana Wachowski"}, {ID: "nm0905152", Name: "Lilly Wachowski"}},
		Cast: []daily.Billed{
			{ID: "nm0001592", Name: "Joe Pantoliano", Billing: 6, Also: &daily.Also{ID: "tt0106977", Title: "The Fugitive", Year: 1993}},
			{ID: "nm0287825", Name: "Gloria Foster", Billing: 5, Also: &daily.Also{ID: "tt0067433", Title: "Man and Boy", Year: 1971}},
			{ID: "nm0915989", Name: "Hugo Weaving", Billing: 4, Also: &daily.Also{ID: "tt0434409", Title: "V for Vendetta", Year: 2005}},
			{ID: "nm0005251", Name: "Carrie-Anne Moss", Billing: 3, Also: &daily.Also{ID: "tt0241303", Title: "Chocolat", Year: 2000}},
			{ID: "nm0000401", Name: "Laurence Fishburne", Billing: 2, Also: &daily.Also{ID: "tt0078788", Title: "Apocalypse Now", Year: 1979}},
			{ID: "nm0000206", Name: "Keanu Reeves", Billing: 1, Also: &daily.Also{ID: "tt2911666", Title: "John Wick", Year: 2014}},
		},
		Movies: []daily.Movie{
			{ID: "tt0067433", Title: "Man and Boy", Year: 1971, Rating: 5.5, Genres: []string{"Drama"}, Cast: []int{1}},
			{ID: "tt0078788", Title: "Apocalypse Now", Year: 1979, Rating: 8.4, Genres: []string{"Drama", "War"}, Cast: []int{4}},
			{ID: "tt0106977", Title: "The Fugitive", Year: 1993, Rating: 7.8, Genres: []string{"Action", "Crime"}, Cast: []int{0}},
			{ID: "tt0111257", Title: "Speed", Year: 1994, Rating: 7.3, Genres: []string{"Action", "Thriller"}, Cast: []int{5}},
			{ID: "tt0115736", Title: "Bound", Year: 1996, Rating: 7.3, Genres: []string{"Crime", "Thriller"}, Cast: []int{0}, Dir: true},
			{ID: "tt0133093", Title: "The Matrix", Year: 1999, Rating: 8.7, Genres: []string{"Action", "Sci-Fi"}, Cast: []int{0, 1, 2, 3, 4, 5}, Dir: true},
			{ID: "tt0209144", Title: "Memento", Year: 2000, Rating: 8.4, Genres: []string{"Mystery", "Thriller"}, Cast: []int{0, 3}},
			{ID: "tt0241303", Title: "Chocolat", Year: 2000, Rating: 7.2, Genres: []string{"Drama", "Romance"}, Cast: []int{3}},
			{ID: "tt0234215", Title: "The Matrix Reloaded", Year: 2003, Rating: 7.2, Genres: []string{"Action", "Sci-Fi"}, Cast: []int{1, 2, 3, 4, 5}, Dir: true},
			{ID: "tt0434409", Title: "V for Vendetta", Year: 2005, Rating: 8.1, Genres: []string{"Action", "Drama"}, Cast: []int{2}},
			{ID: "tt2911666", Title: "John Wick", Year: 2014, Rating: 7.4, Genres: []string{"Action", "Thriller"}, Cast: []int{5}},
		},
		Era:   1995,
		Genre: "Action",
	}
	// None of the six is near their cap, so every movie is on the sheet
	// of everyone it credits.
	for i := range p.Movies {
		p.Movies[i].Sheets = slices.Clone(p.Movies[i].Cast)
	}
	return p
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
	// repicks are the puzzles RepickDailyPuzzle dealt again, in order,
	// and noOther makes it find no other movie (dailydev_test.go).
	repicks []int
	noOther bool
}

func newFakeDaily(puzzles ...*daily.Puzzle) *fakeDaily {
	f := &fakeDaily{puzzles: map[int]*daily.Puzzle{}, players: map[string]daily.Player{}, names: map[string]bool{},
		games: map[[2]int64]*daily.Record{},
		weeks: map[bool]*daily.Week{false: {Rank: 2048, Players: 83500}, true: {Rank: 1204, Players: 83500}},
		films: map[string]daily.Looked{
			"tt0111257": {Title: "Speed", Year: 1994, Genres: []string{"Action", "Thriller"}, Credited: []string{"nm0000206"}},
			"tt0034583": {Title: "Casablanca", Year: 1942, Genres: []string{"Drama", "Romance", "War"}},
			"tt0209144": {Title: "Memento", Year: 2000, Genres: []string{"Mystery", "Thriller"}, Credited: []string{"nm0005251", "nm0001592"}},
			"tt0120601": {Title: "Being John Malkovich", Year: 1999, Genres: []string{"Comedy", "Drama"}},
			"tt0234215": {Title: "The Matrix Reloaded", Year: 2003, Genres: []string{"Action", "Sci-Fi"},
				Credited: []string{"nm0287825", "nm0915989", "nm0005251", "nm0000401", "nm0000206"}},
		}}
	for _, p := range puzzles {
		f.puzzles[p.No] = p
	}
	return f
}

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

// DailyLive has posters for every one of Pantoliano's movies and only
// some of the others', and photos for Pantoliano, Keanu and Lana: only
// what it is asked for, as the store's reads are.
func (f *fakeDaily) DailyLive(_ context.Context, films, people []string) (daily.Live, error) {
	posters := map[string]string{
		"tt0106977": "https://img.example/fugitive.jpg", "tt0115736": "https://img.example/bound.jpg",
		"tt0133093": "https://img.example/matrix.jpg", "tt0209144": "https://img.example/memento.jpg",
		"tt0111257": "https://img.example/speed.jpg",
	}
	photos := map[string]string{
		"nm0001592": "https://image.tmdb.org/t/p/w185/joe.jpg", "nm0000206": "https://image.tmdb.org/t/p/w185/keanu.jpg",
		"nm0905154": "https://image.tmdb.org/t/p/w185/lana.jpg",
	}
	live := daily.Live{Posters: map[string]string{}, Photos: map[string]string{}}
	for _, id := range films {
		if url, ok := posters[id]; ok {
			live.Posters[id] = url
		}
	}
	for _, id := range people {
		if url, ok := photos[id]; ok {
			live.Photos[id] = url
		}
	}
	return live, f.err()
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

// DailyBoard ranks the player with 900 among four strangers, as the
// store would place them, and says today's figures on the today tab.
func (f *fakeDaily) DailyBoard(_ context.Context, p *daily.Puzzle, tab string, player int64) (daily.Board, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boards = append(f.boards, fmt.Sprintf("%d/%s/%d", p.No, tab, player))
	board := []daily.Ranked{
		{Player: 9001, Name: "Marty Starling", Hue: 30, Pts: 1000},
		{Player: 9002, Name: "Clarice McFly", Hue: 60, Pts: 950},
		{Player: 9003, Name: "Leia Bickle", Hue: 90, Pts: 900},
		{Player: 9004, Name: "Ellis Kimble", Hue: 120, Pts: 850},
	}
	if pl, ok := f.byID(player); ok {
		board = append(board, daily.Ranked{Player: pl.ID, Name: pl.Name, Hue: pl.Hue, Pts: 900})
	}
	if tab == daily.TabWeek {
		for i := range board {
			board[i].Days = []int{board[i].Pts}
		}
	}
	daily.Place(board)
	b := daily.Board{Tab: tab, Total: len(board), Rows: daily.Around(board, player, tab)}
	for _, r := range board {
		if player != 0 && r.Player == player {
			b.You = &daily.You{Place: r.Place, Tied: r.Tied, Pts: r.Pts, Days: r.Days}
		}
	}
	if tab == daily.TabToday {
		b.Chart = make([]int, daily.ChartBars)
		for _, r := range board {
			b.Chart[daily.Bar(r.Pts)]++
		}
		b.Solved = daily.Percent(len(board), len(board))
		if b.You != nil {
			b.Beat = daily.Percent(1, len(board))
		}
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

// sheet asks for a person's Movies sheet in No. 142.
func (b *browser) sheet(person string) reply {
	b.t.Helper()
	return b.get("/daily/142/movies?person=" + person)
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
	for _, r := range []reply{b.get("/daily"), b.get("/daily/me"), b.post("/daily/142/play", map[string]any{}),
		b.get("/daily/142/board"), b.sheet("nm0001592"), b.move("next", 0, nil)} {
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
// player is made. Today is its number, date, countdown and colour, and
// nothing of the old board.
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
	if r.body["no"] != 142.0 || r.body["date"] != "2026-10-08" || r.body["game"] != nil || r.body["played"] != 0.0 || r.body["colour"] != "#26382d" {
		t.Errorf("body = %s", r.raw)
	}
	if r.body["now"] != "2026-10-08T09:30:00Z" || r.body["next"] != "2026-10-09T00:00:00Z" {
		t.Errorf("now %v, next %v", r.body["now"], r.body["next"])
	}
	keys := slices.Sorted(maps.Keys(r.body))
	if want := []string{"colour", "date", "game", "next", "no", "now", "played", "player", "streak"}; !slices.Equal(keys, want) {
		t.Errorf("today says %v, want %v", keys, want)
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
	if g["phase"] != "play" || g["pts"] != 1000.0 || g["seq"] != 0.0 || g["startedAt"] != "2026-10-08T09:30:00Z" || g["end"] != nil || g["nextCost"] != 100.0 {
		t.Errorf("a new game = %v", g)
	}
	if log := g["log"].([]any); len(log) != 0 {
		t.Errorf("log = %v", log)
	}
	slots := g["slots"].([]any)
	if len(slots) != 6 || slots[0].(map[string]any)["shown"] != true || slots[1].(map[string]any)["shown"] != false {
		t.Errorf("slots = %v", slots)
	}
	if joe := slots[0].(map[string]any)["person"].(map[string]any); joe["name"] != "Joe Pantoliano" || joe["photo"] != "https://image.tmdb.org/t/p/w185/joe.jpg" || joe["hue"] != 205.0 {
		t.Errorf("the sixth-billed = %v", joe)
	}
	b.move("next", 0, nil)
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
		stranger.do(http.MethodPost, "/daily/142/next", "text/plain", []byte(`{"key":"abcdefgh","seq":0}`)),
		stranger.do(http.MethodPost, "/daily/name", "", []byte(`{}`)),
	} {
		if r.status != http.StatusUnsupportedMediaType || r.body["reason"] != "content-type" {
			t.Errorf("not JSON: %d %s", r.status, r.raw)
		}
	}
	if r := stranger.move("next", 0, nil); r.status != http.StatusForbidden || r.body["reason"] != "cookie" {
		t.Errorf("no cookie: %d %s", r.status, r.raw)
	}
	stranger.client.Jar.SetCookies(mustURL(t, srv.URL), []*http.Cookie{{Name: "cd_daily", Value: daily.NewToken(), Secure: true}})
	if r := stranger.move("next", 0, nil); r.status != http.StatusForbidden || r.body["reason"] != "cookie" {
		t.Errorf("a cookie nobody has: %d %s", r.status, r.raw)
	}

	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	for name, c := range map[string]struct {
		verb string
		body map[string]any
	}{
		"a short key":             {"next", map[string]any{"key": "short", "seq": 0}},
		"a key with space":        {"next", map[string]any{"key": "has a space", "seq": 0}},
		"no seq":                  {"next", map[string]any{"key": "abcdefgh-1"}},
		"a negative seq":          {"next", map[string]any{"key": "abcdefgh-2", "seq": -1}},
		"no kind":                 {"buy", map[string]any{"key": "abcdefgh-3", "seq": 0}},
		"no person":               {"overlap", map[string]any{"key": "abcdefgh-4", "seq": 0}},
		"a person by name":        {"overlap", map[string]any{"key": "abcdefgh-5", "seq": 0, "person": "Joe Pantoliano"}},
		"a person by a title id":  {"overlap", map[string]any{"key": "abcdefgh-6", "seq": 0, "person": "tt0133093"}},
		"a movie by a TMDb id":    {"guess", map[string]any{"key": "abcdefgh-7", "seq": 0, "film": "603"}},
		"a movie by a person id":  {"guess", map[string]any{"key": "abcdefgh-8", "seq": 0, "film": "nm0001592"}},
		"a buy of a trailer":      {"buy", map[string]any{"key": "abcdefgh-9", "seq": 0, "kind": "trailer"}},
		"a buy of an old actor":   {"buy", map[string]any{"key": "abcdefgh-a", "seq": 0, "kind": "actor"}},
		"a buy of the old year":   {"buy", map[string]any{"key": "abcdefgh-b", "seq": 0, "kind": "year"}},
		"a buy of the old genres": {"buy", map[string]any{"key": "abcdefgh-c", "seq": 0, "kind": "genres"}},
		"a buy of a next name":    {"buy", map[string]any{"key": "abcdefgh-d", "seq": 0, "kind": "next"}},
		"a buy of an overlap":     {"buy", map[string]any{"key": "abcdefgh-e", "seq": 0, "kind": "overlap"}},
	} {
		if r := b.post("/daily/142/"+c.verb, c.body); r.status != http.StatusBadRequest || r.body["reason"] != "bad" {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	if r := b.post("/daily/142/flip", map[string]any{"key": "abcdefgh-f", "seq": 0, "card": "c4"}); r.status != http.StatusNotFound {
		t.Errorf("the old game's flip: %d %s", r.status, r.raw)
	}
	big := b.do(http.MethodPost, "/daily/142/guess", "application/json",
		[]byte(`{"key":"abcdefgh","seq":0,"film":"`+strings.Repeat("t", dailyBody)+`"}`))
	if big.status != http.StatusBadRequest {
		t.Errorf("a body past 4 KB: %d", big.status)
	}
	if r := b.do(http.MethodPost, "/daily/142/next", "application/json", []byte(`{"key":`)); r.status != http.StatusBadRequest {
		t.Errorf("broken JSON: %d %s", r.status, r.raw)
	}
	if r := b.post("/daily/x/next", map[string]any{"key": "abcdefgh", "seq": 0}); r.status != http.StatusBadRequest {
		t.Errorf("a puzzle number that is not one: %d %s", r.status, r.raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec := f.games[[2]int64{1, 142}]; rec == nil || len(rec.Moves) != 0 {
		t.Errorf("a malformed move was recorded: %+v", rec)
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
	body := map[string]any{"key": "6f1c1b7e-retry", "seq": 0}
	first := b.post("/daily/142/next", body)
	retry := b.post("/daily/142/next", body)
	if first.status != http.StatusOK || retry.status != http.StatusOK || ptsOf(t, first) != 900 || ptsOf(t, retry) != 900 || seqOf(t, retry) != 1 {
		t.Errorf("first %d (%d points), retry %d (%d points)", first.status, ptsOf(t, first), retry.status, ptsOf(t, retry))
	}
	stale := b.post("/daily/142/buy", map[string]any{"key": "other-tab-1", "seq": 0, "kind": "genre"})
	if stale.status != http.StatusConflict || stale.body["reason"] != "stale" || seqOf(t, stale) != 1 || ptsOf(t, stale) != 900 {
		t.Errorf("a stale move: %d %s", stale.status, stale.raw)
	}
}

// TestEveryMoveAnswersWithTheGame, each at its own address: Next name, a
// fact, an overlap, a wrong guess that fills in the cast it shares, and
// showing the answer, which ends it with everything.
func TestEveryMoveAnswersWithTheGame(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	for i, c := range []struct {
		verb string
		body map[string]any
		pts  int
		want string
	}{
		{"next", nil, 900, `"slots":[{"slot":0,"shown":true,`},
		{"buy", map[string]any{"kind": "decade"}, 800, `"facts":{"decade":1990}`},
		{"overlap", map[string]any{"person": "nm0287825"}, 550, `"overlaps":["nm0287825"]`},
		{"guess", map[string]any{"film": "tt0209144"}, 450, `"from":{"id":"tt0209144","title":"Memento"}`},
		{"reveal", nil, 0, `"end":{"answer":{"id":"tt0133093"`},
	} {
		r := b.move(c.verb, i, c.body)
		if r.status != http.StatusOK || ptsOf(t, r) != c.pts || seqOf(t, r) != i+1 || !strings.Contains(r.raw, c.want) {
			t.Errorf("%s: %d, %d points, want %d and %s in %s", c.verb, r.status, ptsOf(t, r), c.pts, c.want, r.raw)
		}
		if keys := slices.Sorted(maps.Keys(r.body)); !slices.Equal(keys, []string{"game"}) {
			t.Errorf("%s answers with %v", c.verb, keys)
		}
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
	expect(b.move("guess", 0, map[string]any{"film": "tt9999999"}), http.StatusNotFound, "unknown")
	expect(b.move("buy", 0, map[string]any{"kind": "years"}), http.StatusBadRequest, "bad")
	expect(b.move("overlap", 0, map[string]any{"person": "nm0000206"}), http.StatusBadRequest, "bad")
	expect(b.move("overlap", 0, map[string]any{"person": "nm0905154"}), http.StatusBadRequest, "bad")
	seq := 0
	for _, body := range []map[string]any{
		{"kind": "director"}, {"kind": "decade"}, {"kind": "years"}, {"kind": "genre"}, {"kind": "length"}, {"kind": "rating"},
	} {
		if r := b.move("buy", seq, body); r.status != http.StatusOK {
			t.Fatalf("buy %v: %d %s", body, r.status, r.raw)
		}
		seq++
	}
	if r := b.move("overlap", seq, map[string]any{"person": "nm0001592"}); r.status != http.StatusOK {
		t.Fatalf("an overlap: %d %s", r.status, r.raw)
	}
	seq++
	// A hundred points left, which no purchase may spend.
	expect(b.move("buy", seq, map[string]any{"kind": "decade"}), http.StatusConflict, "known")
	expect(b.move("overlap", seq, map[string]any{"person": "nm0001592"}), http.StatusConflict, "known")
	expect(b.move("next", seq, nil), http.StatusPaymentRequired, "points")
	expect(b.move("guess", seq, map[string]any{"film": "tt0111257"}), http.StatusOK, "")
	seq++
	expect(b.move("reveal", seq, nil), http.StatusConflict, "done")
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

	// All six showing: no next name to buy.
	all := newBrowser(t, srv)
	all.ip = "216.160.83.56"
	all.post("/daily/143/play", map[string]any{})
	for i := range 5 {
		if r := all.post("/daily/143/next", map[string]any{"key": fmt.Sprintf("next-name-%d", i), "seq": i}); r.status != http.StatusOK {
			t.Fatalf("next name %d: %d %s", i+1, r.status, r.raw)
		}
	}
	expect(all.post("/daily/143/next", map[string]any{"key": "next-name-5", "seq": 5}), http.StatusConflict, "known")
}

// said is every place a response says value, by its path: a string
// equal to it, or a number equal to it. A movie's own "year", the
// "year" of an object with an "id", is skipped when ownYear is set: a
// guess or an "Also in" saying its own year, as a movie may.
func said(raw string, value any, ownYear bool) []string {
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
				if !(ownYear && movie && k == "year") {
					walk(path+"."+k, e)
				}
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		case float64:
			if n, ok := value.(float64); ok && x == n {
				out = append(out, path)
			}
		case string:
			if str, ok := value.(string); ok && x == str {
				out = append(out, path)
			}
		}
	}
	walk("", v)
	slices.Sort(out)
	return out
}

// TestNothingBeforeTheEndSaysWhatIsHidden: every response a game gets,
// from the first read, Play and every kind of move short of the end to
// the Movies sheets and the boards, never says the answer's id, title,
// year, rating, length, poster or genres, nor names a director before
// Director is bought, nor anyone in the cast the player has not been
// shown, nor a fact not bought. The sheets carry the answer as one card
// among the rest, which is the point of them: there it must look like
// every other card, with the same fields, a poster for every card or
// none, the slots showing and never a hidden one, and no director until
// Director. The end names everything.
func TestNothingBeforeTheEndSaysWhatIsHidden(t *testing.T) {
	p := matrixPuzzle()
	f := newFakeDaily(p)
	srv, _ := dailyServer(t, f)
	b := newBrowser(t, srv)
	shown := map[string]bool{"nm0001592": true}
	bought := map[string]bool{}
	check := func(r reply, when string) {
		t.Helper()
		if r.status != http.StatusOK {
			t.Fatalf("%s: %d %s", when, r.status, r.raw)
		}
		a := p.Answer
		for _, secret := range []any{a.ID, a.Title, "https://img.example/matrix.jpg", a.Rating, float64(a.Length)} {
			if got := said(r.raw, secret, false); got != nil {
				t.Errorf("%s: the answer's %v is said at %v", when, secret, got)
			}
		}
		if got := said(r.raw, float64(a.Year), true); got != nil {
			t.Errorf("%s: the answer's year is said at %v", when, got)
		}
		_, isGame := r.body["game"]
		for i, g := range a.Genres {
			var want []string
			if bought["genre"] && isGame {
				want = []string{fmt.Sprintf(".game.facts.genre[%d]", i)}
			}
			if got := said(r.raw, g, false); !slices.Equal(got, want) {
				t.Errorf("%s: the genre %s is said at %v, want %v", when, g, got, want)
			}
		}
		for _, d := range p.Directors {
			if !bought["director"] && (strings.Contains(r.raw, d.ID) || strings.Contains(r.raw, d.Name)) {
				t.Errorf("%s: %s is named before Director is bought", when, d.Name)
			}
		}
		for _, c := range p.Cast {
			if !shown[c.ID] && (strings.Contains(r.raw, `"`+c.ID+`"`) || strings.Contains(r.raw, c.Name) || strings.Contains(r.raw, c.Also.Title)) {
				t.Errorf("%s: hidden %s is said in %s", when, c.Name, r.raw)
			}
		}
		if g, ok := r.body["game"].(map[string]any); ok {
			facts := slices.Sorted(maps.Keys(g["facts"].(map[string]any)))
			want := slices.Sorted(maps.Keys(bought))
			if !slices.Equal(facts, want) || g["end"] != nil {
				t.Errorf("%s: the facts say %v, want %v; end %v", when, facts, want, g["end"])
			}
		}
	}
	// sheets checks every showing person's Movies sheet: what any
	// response must keep, and that the answer's card is like the rest.
	sheets := func(when string) {
		t.Helper()
		for _, c := range p.Cast {
			r := b.sheet(c.ID)
			if !shown[c.ID] {
				if r.status != http.StatusBadRequest || r.body["reason"] != "bad" {
					t.Errorf("%s: hidden %s's sheet: %d %s", when, c.Name, r.status, r.raw)
				}
				continue
			}
			if r.status != http.StatusOK || r.body["person"] != c.ID {
				t.Fatalf("%s: %s's sheet: %d %s", when, c.Name, r.status, r.raw)
			}
			movies := r.body["movies"].([]any)
			fields := ""
			posters := 0
			for _, m := range movies {
				card := m.(map[string]any)
				// Director's own mark aside, which lights every card a
				// director is on, the answer's among them.
				keys := strings.Join(slices.DeleteFunc(slices.Sorted(maps.Keys(card)), func(k string) bool { return k == "dir" }), " ")
				if fields == "" {
					fields = keys
				}
				if keys != fields {
					t.Errorf("%s: %s's sheet has a card with %s and one with %s", when, c.Name, fields, keys)
				}
				if _, ok := card["poster"]; ok {
					posters++
				}
				if _, ok := card["dir"]; ok && !bought["director"] {
					t.Errorf("%s: %s's sheet marks a director before Director: %v", when, c.Name, card)
				}
				for _, on := range card["on"].([]any) {
					if !shown[p.Cast[int(on.(float64))].ID] {
						t.Errorf("%s: %s's sheet lists hidden slot %v on %v", when, c.Name, on, card["id"])
					}
				}
			}
			if posters != 0 && posters != len(movies) {
				t.Errorf("%s: %s's sheet has %d posters for %d cards", when, c.Name, posters, len(movies))
			}
			if !slices.ContainsFunc(movies, func(m any) bool { return m.(map[string]any)["id"] == p.Answer.ID }) {
				t.Errorf("%s: %s's sheet lacks today's movie", when, c.Name)
			}
			// Other movies have genres and years of their own, so the
			// rest is held only to what no sheet may say: the answer
			// anywhere but its own card, a director, or anyone hidden.
			rest := slices.DeleteFunc(slices.Clone(movies), func(m any) bool { return m.(map[string]any)["id"] == p.Answer.ID })
			raw, _ := json.Marshal(rest)
			for _, secret := range []string{`"` + p.Answer.ID + `"`, `"` + p.Answer.Title + `"`, "matrix.jpg"} {
				if strings.Contains(string(raw), secret) {
					t.Errorf("%s: %s's sheet says %s outside today's card", when, c.Name, secret)
				}
			}
			for _, d := range p.Directors {
				if strings.Contains(r.raw, d.ID) || strings.Contains(r.raw, d.Name) {
					t.Errorf("%s: %s's sheet names %s", when, c.Name, d.Name)
				}
			}
			for j, o := range p.Cast {
				if !shown[o.ID] && (strings.Contains(r.raw, o.ID) || strings.Contains(r.raw, o.Name)) {
					t.Errorf("%s: %s's sheet names hidden slot %d", when, c.Name, j)
				}
			}
		}
	}
	check(b.get("/daily"), "at load")
	check(b.post("/daily/142/play", map[string]any{}), "Play")
	check(b.get("/daily"), "reading the game")
	sheets("after Play")
	seq := 0
	for _, m := range []struct {
		verb  string
		body  map[string]any
		shows []string
		buys  string
	}{
		{"next", nil, []string{"nm0287825"}, ""},
		{"overlap", map[string]any{"person": "nm0287825"}, nil, ""},
		// Memento fills in Moss through it, and then shows Weaving.
		{"guess", map[string]any{"film": "tt0209144"}, []string{"nm0005251", "nm0915989"}, ""},
		// A movie from the answer's year says its own year.
		{"guess", map[string]any{"film": "tt0120601"}, []string{"nm0000401"}, ""},
		{"buy", map[string]any{"kind": "genre"}, nil, "genre"},
	} {
		for _, id := range m.shows {
			shown[id] = true
		}
		if m.buys != "" {
			bought[m.buys] = true
		}
		r := b.move(m.verb, seq, m.body)
		check(r, fmt.Sprintf("%s %v", m.verb, m.body))
		seq++
	}
	check(b.get("/daily"), "reading the game again")
	sheets("before Director")
	stale := b.move("buy", 0, map[string]any{"kind": "length"})
	if stale.status != http.StatusConflict {
		t.Fatalf("stale = %d", stale.status)
	}
	stale.status = http.StatusOK
	check(stale, "a stale move")
	bought["director"] = true
	check(b.move("buy", seq, map[string]any{"kind": "director"}), "Director")
	seq++
	sheets("after Director")
	for _, tab := range []string{"today", "week"} {
		check(b.get("/daily/142/board?tab="+tab), "the "+tab+" board")
	}
	check(b.get("/daily/me"), "the standing")

	end := b.move("guess", seq, map[string]any{"film": p.Answer.ID})
	for _, want := range []string{`"tt0133093"`, `"The Matrix"`, `"Keanu Reeves"`, `"length":136`, `"rating":8.7`, `"poster":"https://img.example/matrix.jpg"`} {
		if !strings.Contains(end.raw, want) {
			t.Errorf("the end does not say %s: %s", want, end.raw)
		}
	}
	g := gameOf(t, end)
	if g["phase"] != "done" || g["won"] != true || g["end"] == nil {
		t.Errorf("after the right guess: %v", g)
	}
	// Every slot shows at the end, each saying how: the star, whom the
	// player never saw, by "end", the only way a slot comes to show
	// without being seen.
	var via []any
	for _, sl := range g["slots"].([]any) {
		via = append(via, sl.(map[string]any)["via"])
	}
	if want := []any{"start", "next", "guess", "guess", "guess", "end"}; !slices.Equal(via, want) {
		t.Errorf("at the end the slots show by %v, want %v", via, want)
	}
	if r := b.sheet("nm0000206"); r.status != http.StatusOK {
		t.Errorf("the star's sheet once it is over: %d %s", r.status, r.raw)
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
	if r := kept.move("next", 0, nil); r.status != http.StatusOK {
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
// store's to rank by, and the board is the players around the reader,
// placed; a reader with no player is on no board and sees no rows, only
// its size and today's figures. A tab that is neither, a puzzle that
// does not exist, and one whose day has not come are refused.
func TestTheBoardIsReadForTheReader(t *testing.T) {
	tomorrow := matrixPuzzle()
	tomorrow.No, tomorrow.Day = 143, tomorrow.Day.AddDate(0, 0, 1)
	f := newFakeDaily(matrixPuzzle(), tomorrow)
	srv, _ := dailyServer(t, f)
	anyone := newBrowser(t, srv)
	r := anyone.get("/daily/142/board")
	if r.status != http.StatusOK || r.body["tab"] != "today" || r.body["you"] != nil || r.body["beat"] != nil || r.body["total"] != 4.0 {
		t.Errorf("without a player: %d %s", r.status, r.raw)
	}
	if rows := r.body["rows"].([]any); len(rows) != 0 {
		t.Errorf("a reader on no board sees %v", rows)
	}
	if chart := r.body["chart"].([]any); len(chart) != 11 || chart[10] != 1.0 || chart[9] != 2.0 {
		t.Errorf("chart = %v", chart)
	}
	b := newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	today := b.get("/daily/142/board?tab=today")
	var places []string
	for _, row := range today.body["rows"].([]any) {
		r := row.(map[string]any)
		places = append(places, fmt.Sprintf("%v %v %v %v", r["place"], r["tied"], r["pts"], r["you"]))
		if _, ok := r["days"]; ok {
			t.Errorf("a row on today's board has days: %v", r)
		}
	}
	if want := []string{"1 false 1000 false", "2 false 950 false", "3 true 900 true", "3 true 900 false", "5 false 850 false"}; !slices.Equal(places, want) {
		t.Errorf("today's rows = %v, want %v", places, want)
	}
	if you := today.body["you"].(map[string]any); you["place"] != 3.0 || you["tied"] != true || you["pts"] != 900.0 {
		t.Errorf("you = %v", you)
	}
	week := b.get("/daily/142/board?tab=week")
	if week.status != http.StatusOK || week.body["tab"] != "week" || week.body["chart"] != nil || week.body["beat"] != nil || week.body["solved"] != nil {
		t.Errorf("the week: %d %s", week.status, week.raw)
	}
	if rows := week.body["rows"].([]any); len(rows) != 5 || rows[2].(map[string]any)["you"] != true || rows[2].(map[string]any)["days"] == nil {
		t.Errorf("week rows = %v", rows)
	}
	if want := []string{"142/today/0", "142/today/1", "142/week/1"}; !slices.Equal(f.boards, want) {
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

// TestTheMoviesSheetIsOnlyForSomeoneShowing in the reader's own game:
// refused without a player, before Play, for anyone hidden, a director
// or someone not in the cast at all, and for a puzzle that is no longer
// anybody's or a game past its own midnight; once the game is over, it
// is there for anyone in the cast. It never writes.
func TestTheMoviesSheetIsOnlyForSomeoneShowing(t *testing.T) {
	f := newFakeDaily(matrixPuzzle())
	srv, clk := dailyAt(t, f, todayAt)
	b := newBrowser(t, srv)
	expect := func(r reply, status int, reason string) {
		t.Helper()
		if r.status != status || (reason != "" && r.body["reason"] != reason) {
			t.Errorf("%d %s, want %d %s", r.status, r.raw, status, reason)
		}
	}
	expect(b.sheet("nm0001592"), http.StatusForbidden, "cookie")
	b.client.Jar.SetCookies(mustURL(t, srv.URL), []*http.Cookie{{Name: "cd_daily", Value: daily.NewToken(), Secure: true}})
	expect(b.sheet("nm0001592"), http.StatusForbidden, "cookie")

	// A player with no game of No. 142.
	b = newBrowser(t, srv)
	b.post("/daily/142/play", map[string]any{})
	f.mu.Lock()
	delete(f.games, [2]int64{1, 142})
	f.mu.Unlock()
	expect(b.sheet("nm0001592"), http.StatusNotFound, "no-game")
	b.post("/daily/142/play", map[string]any{})
	for _, who := range []string{"nm0287825", "nm0000206", "nm0905154", "nm9999999", "Joe", ""} {
		expect(b.sheet(who), http.StatusBadRequest, "bad")
	}
	expect(b.get("/daily/x/movies?person=nm0001592"), http.StatusBadRequest, "bad")
	expect(b.get("/daily/9/movies?person=nm0001592"), http.StatusConflict, "day")

	r := b.sheet("nm0001592")
	if r.status != http.StatusOK || r.body["person"] != "nm0001592" || r.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Pantoliano's sheet: %d %s", r.status, r.raw)
	}
	want := `{"person":"nm0001592","movies":[` +
		`{"id":"tt0106977","title":"The Fugitive","year":1993,"rating":7.8,"genres":["Action","Crime"],"poster":"https://img.example/fugitive.jpg","on":[0]},` +
		`{"id":"tt0115736","title":"Bound","year":1996,"rating":7.3,"genres":["Crime","Thriller"],"poster":"https://img.example/bound.jpg","on":[0]},` +
		`{"id":"tt0133093","title":"The Matrix","year":1999,"rating":8.7,"genres":["Action","Sci-Fi"],"poster":"https://img.example/matrix.jpg","on":[0]},` +
		`{"id":"tt0209144","title":"Memento","year":2000,"rating":8.4,"genres":["Mystery","Thriller"],"poster":"https://img.example/memento.jpg","on":[0]}]}` + "\n"
	if r.raw != want {
		t.Errorf("Pantoliano's sheet =\n%s\nwant\n%s", r.raw, want)
	}
	// Keanu's movies do not all have posters, so none of them is sent one.
	b.move("guess", 0, map[string]any{"film": "tt0111257"})
	if keanu := b.sheet("nm0000206"); keanu.status != http.StatusOK || strings.Contains(keanu.raw, "poster") {
		t.Errorf("Keanu's sheet: %d %s", keanu.status, keanu.raw)
	}
	b.move("buy", 1, map[string]any{"kind": "director"})
	if joe := b.sheet("nm0001592"); !strings.Contains(joe.raw, `"on":[0],"dir":true}`) || strings.Count(joe.raw, `"dir":true`) != 2 {
		t.Errorf("Pantoliano's sheet after Director: %s", joe.raw)
	}
	// Past its own midnight an unfinished game's sheet is gone, as its
	// moves are; finished, the game's sheets are anyone's in the cast
	// for as long as the puzzle is somebody's.
	clk.set(time.Date(2026, 10, 9, 0, 30, 0, 0, time.UTC))
	expect(b.sheet("nm0001592"), http.StatusConflict, "day")
	clk.set(todayAt)
	b.move("reveal", 2, nil)
	clk.set(time.Date(2026, 10, 9, 0, 30, 0, 0, time.UTC))
	if r := b.sheet("nm0000401"); r.status != http.StatusOK || !strings.Contains(r.raw, `"on":[1,2,3,4,5]`) {
		t.Errorf("Fishburne's sheet once it is over: %d %s", r.status, r.raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec := f.games[[2]int64{1, 142}]; rec == nil || len(rec.Moves) != 3 {
		t.Errorf("the sheets moved the game: %+v", rec)
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
	next := func(key string, seq int, query string) reply {
		return b.post("/daily/143/next"+query, map[string]any{"key": key, "seq": seq})
	}
	if r := next("from-la-1", 0, "?tz=America/Los_Angeles"); r.status != http.StatusOK {
		t.Errorf("a move naming Los Angeles while Tokyo is on the 9th: %d %s", r.status, r.raw)
	}
	// 00:30 on the 10th in Tokyo; 08:30 on the 9th in Los Angeles.
	clk.set(time.Date(2026, 10, 9, 15, 30, 0, 0, time.UTC))
	for i, query := range []string{"?tz=America/Los_Angeles", "?tz=Asia/Tokyo", ""} {
		if r := b.post("/daily/143/buy"+query, map[string]any{"key": fmt.Sprintf("too-late-%d", i), "seq": 1, "kind": "genre"}); r.status != http.StatusConflict || r.body["reason"] != "day" {
			t.Errorf("a move past Tokyo's midnight naming %q: %d %s", query, r.status, r.raw)
		}
	}
	// The retry of a move made in time is still answered.
	if r := next("from-la-1", 0, "?tz=America/Los_Angeles"); r.status != http.StatusOK || seqOf(t, r) != 1 {
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
	if r := b.post("/daily/144/next?tz=America/Los_Angeles", map[string]any{"key": "on-the-10th", "seq": 0}); r.status != http.StatusOK {
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
	move := map[string]any{"key": "abcdefgh-big", "seq": 0}
	for path, r := range map[string]reply{
		"Play":   b.post("/daily/99999999999/play", map[string]any{}),
		"next":   b.post("/daily/2147483648/next", move),
		"board":  b.get("/daily/99999999999/board"),
		"movies": b.get("/daily/2147483648/movies?person=nm0001592"),
		"minus":  b.get("/daily/-1/board"),
	} {
		if r.status != http.StatusBadRequest || r.body["reason"] != "bad" {
			t.Errorf("%s: %d %s", path, r.status, r.raw)
		}
	}
	if r := b.post("/daily/2147483647/next", move); r.status != http.StatusConflict || r.body["reason"] != "day" {
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

	// The fake's pool is a few dozen names, so an offer is now and then
	// one a player here already has, and Play rightly draws another for
	// it: a name taken, which is not what this asks about, and which
	// failed it about one run in fifteen. So an offer is asked for until
	// it is one nobody has.
	untaken := func(offer func() string) string {
		t.Helper()
		for range 100 {
			name := offer()
			f.mu.Lock()
			taken := f.names[name]
			f.mu.Unlock()
			if !taken {
				return name
			}
		}
		t.Fatal("every name offered was taken")
		return ""
	}

	if name, drawn := play(newBrowser(t, srv), chosen); drawn == 0 {
		t.Errorf("a name nobody was offered: kept %q without a draw", name)
	}
	read := newBrowser(t, srv)
	offered := untaken(func() string { return read.get("/daily").body["player"].(map[string]any)["name"].(string) })
	if name, drawn := play(read, offered); name != offered || drawn != 0 {
		t.Errorf("the name GET /daily offered: kept %q, %d draws, want %q", name, drawn, offered)
	}
	spun := newBrowser(t, srv)
	another := untaken(func() string { return spun.post("/daily/name", map[string]any{}).body["name"].(string) })
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

// TestMeIsTheStreakAndTheWeeksStanding: before the game,
// the run today can extend and the standing over the days before
// today; once the game is finished, today's run and the standing
// through today, worked out at once, since finishing is a new key. In
// between each is kept a minute, and the cookie is renewed as GET
// /daily renews it.
func TestMeIsTheStreakAndTheWeeksStanding(t *testing.T) {
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

	b.move("next", 0, nil)
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
