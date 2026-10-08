package api

// Cinedikt Daily over HTTP: today's puzzle, the player's game, the moves
// and the boards. The rules are internal/daily's, and the games are kept
// by the catalog's store; this is who the reader is, the shapes, and the
// status codes.
//
// Each reader plays the puzzle for their own date, changing at their
// own midnight. The page names its time zone on every request, as the
// tz parameter, and the server works the date out from its own clock in
// that zone (zoneOf); a game keeps the zone Play was pressed in, and is
// played on its puzzle's day there and nowhere else. A player whose
// unfinished game has passed that zone's midnight is moved on to the day
// it has reached there, by read, Play and the board alike (readerDay).
//
// A player is a cookie, cd_daily, and nothing else: no account, no
// name typed in. Reading never makes one, so the opening screen's banner
// can ask on every visit; pressing Play does. Every change must say it
// is JSON, which a form another site posts cannot without asking first,
// and with SameSite=Lax that is the whole of the defence against a move
// being made on a reader's behalf.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"cinedikt/internal/catalog"
	"cinedikt/internal/daily"
	"cinedikt/internal/imdbid"
)

// DailyStore is what Daily needs kept: the puzzles the job picked, the
// players, and their games. *catalog.Store is the real one.
type DailyStore interface {
	// DailyPuzzle is a day's puzzle, and DailyPuzzleNo puzzle No. no;
	// either is catalog.ErrNotFound when there is none.
	DailyPuzzle(ctx context.Context, day time.Time) (*daily.Puzzle, error)
	DailyPuzzleNo(ctx context.Context, no int) (*daily.Puzzle, error)
	// DailyPlayer is the player behind a cookie's hash.
	DailyPlayer(ctx context.Context, token []byte) (daily.Player, bool, error)
	// CreateDailyPlayer adds one, or catalog.ErrNameTaken.
	CreateDailyPlayer(ctx context.Context, token []byte, name string, hue int) (daily.Player, error)
	// RenameDailyPlayer gives one a new name, or catalog.ErrNameTaken.
	RenameDailyPlayer(ctx context.Context, id int64, name string) error
	// DailyGame is a player's game of a puzzle, nil before they press
	// Play; StartDailyGame makes it, started in zone, or returns the one
	// they have, in the zone it was started in.
	DailyGame(ctx context.Context, player int64, no int) (*daily.Record, error)
	StartDailyGame(ctx context.Context, player int64, no int, at time.Time, zone *time.Location) (*daily.Record, error)
	// DailyAct makes a move and returns the game after it. A refusal is a
	// *daily.Refusal: daily.ErrDay once the puzzle's day has ended in the
	// game's own zone, and daily.ErrStale, which comes with the game as it
	// is.
	DailyAct(ctx context.Context, player int64, p *daily.Puzzle, r daily.Request, at time.Time) (*daily.Record, error)
	// DailyLive is the posters and photos to show with a game.
	DailyLive(ctx context.Context, films, people []string) (daily.Live, error)
	DailyPlayed(ctx context.Context, no int) (int, error)
	DailyStreak(ctx context.Context, player int64, no int) (daily.Streak, error)
	DailyBoard(ctx context.Context, p *daily.Puzzle, tab string, player int64) (daily.Board, error)
	// DailyNames is the characters players' names are made from, and the
	// real people no name may be.
	DailyNames(ctx context.Context) (daily.Credits, error)
}

// WithDaily serves Cinedikt Daily from store. Without it every Daily
// route answers 503 "unavailable", and the page leaves Daily out.
func (s *Server) WithDaily(store DailyStore) *Server {
	s.daily.store = store
	s.daily.names = &daily.NamePool{Load: store.DailyNames}
	return s
}

// The player's cookie. It is their only credential, so it is never
// readable by the page's scripts, never sent over plain HTTP, and never
// sent with a request another site starts, beyond following a link. A
// year from the last visit, renewed on every one.
const (
	dailyCookie = "cd_daily"
	dailyMaxAge = 365 * 24 * 60 * 60
)

// dailyBody bounds a request body. The largest is a move: a key, a
// number and an id.
const dailyBody = 4 << 10

// Making a player is the one thing Daily limits, per address: a private
// window is a new player, and a script making thousands would fill the
// board's pool of names and the table with nobody. Ten at once, then one
// every six minutes, which no household sharing an address will meet.
// Nothing else is limited: a game is a handful of moves, each checked.
const (
	joinBurst = 10
	joinEvery = 6 * time.Minute
	// joinIdle is how long an address's bucket is kept unused: as long
	// as a spent bucket takes to fill again, after which dropping it
	// costs nothing.
	joinIdle = joinBurst * joinEvery
	// joinClients caps how many addresses are held at once.
	joinClients = 10_000
)

// joinTries is how many names a new player, or a rename, is offered
// before giving up: each one taken means another draw from fifty
// thousand characters, so a second is rare and a ninth unheard of.
const joinTries = 8

// dailyRoutes is the Daily routes and what they hold between requests.
type dailyRoutes struct {
	store  DailyStore
	logger *slog.Logger
	names  *daily.NamePool
	offers *nameOffers
	joins  *joinLimiter
	// now, intn and hue are the clock and the draws; a test fixes them.
	now  func() time.Time
	intn func(int) int
	hue  func() int

	// live is the puzzles kept once read, by number: a puzzle never
	// changes once it is picked, and the banner asks for the reader's on
	// every visit. Up to three dates are current somewhere on Earth at
	// once, so up to three are kept, and one whose day has ended
	// everywhere is let go.
	mu   sync.Mutex
	live map[int]*daily.Puzzle
}

func newDailyRoutes(logger *slog.Logger) *dailyRoutes {
	return &dailyRoutes{
		logger: logger,
		offers: newNameOffers(),
		joins:  newJoinLimiter(),
		now:    time.Now,
		intn:   rand.IntN,
		hue:    func() int { return rand.IntN(360) },
		live:   map[int]*daily.Puzzle{},
	}
}

// zoneOf is the reader's time zone, from the tz parameter the page adds
// to every Daily request (Intl's name for it, "Asia/Tokyo"); UTC when
// there is none or it names no zone. Only the date it gives is used, and
// only with the server's clock, so a made-up zone moves a reader a day
// at most, never into a puzzle that is not yet anybody's.
func zoneOf(r *http.Request) *time.Location {
	return daily.Zone(r.URL.Query().Get("tz"))
}

func (d *dailyRoutes) register(mux *http.ServeMux) {
	route := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, d.guard(h)) }
	route("GET /daily", d.read)
	route("POST /daily/name", d.rename)
	route("POST /daily/{no}/play", d.play)
	route("POST /daily/{no}/flip", d.flip)
	route("POST /daily/{no}/buy", d.buy)
	route("POST /daily/{no}/guess", d.guess)
	route("POST /daily/{no}/reveal", d.reveal)
	route("GET /daily/{no}/board", d.board)
}

// guard is what every Daily route does first: keep the answer out of
// caches, answer 503 while Daily is not set up, and give a crawler's
// change nothing at all, so it never becomes a player.
func (d *dailyRoutes) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		if d.store == nil {
			dailyRefuse(w, http.StatusServiceUnavailable, "unavailable", "daily is not set up")
			return
		}
		if r.Method == http.MethodPost && isBot(r.UserAgent()) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// botMarks are words in the user agents of search engines' and link
// previewers' crawlers. A bare "bot" is not one: it is in too many
// ordinary agents. Nor is HeadlessChrome, which is how tests run.
var botMarks = []string{
	"googlebot", "bingbot", "duckduckbot", "yandex", "baiduspider", "slurp", "applebot",
	"facebookexternalhit", "twitterbot", "slackbot", "discordbot", "linkedinbot", "embedly",
	"crawler", "spider",
}

func isBot(ua string) bool {
	ua = strings.ToLower(ua)
	for _, m := range botMarks {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// dailyRefuse answers a request Daily will not serve: msg for whoever
// reads the logs, reason for the page, which maps it to its own words.
func dailyRefuse(w http.ResponseWriter, status int, reason, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "reason": reason})
}

// fail answers a route that failed: with the refusal it is, or, for
// anything else, a 503 whose cause goes only to the log. The database
// being out of reach takes Daily away, never the map.
func (d *dailyRoutes) fail(w http.ResponseWriter, r *http.Request, err error) {
	var no *daily.Refusal
	if errors.As(err, &no) {
		dailyRefuse(w, no.Status, no.Reason, no.Msg)
		return
	}
	if gone(r) {
		return
	}
	if d.logger != nil {
		d.logger.Error("daily route", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	dailyRefuse(w, http.StatusServiceUnavailable, "unavailable", "daily is unavailable")
}

// readDailyJSON decodes a change's body into v. It must say it is JSON,
// and fit in dailyBody. An empty body reads as {}.
func readDailyJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		dailyRefuse(w, http.StatusUnsupportedMediaType, "content-type", "send JSON")
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, dailyBody)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		dailyRefuse(w, http.StatusBadRequest, "bad", "the body is not what this takes")
		return false
	}
	return true
}

// dailyToken is the reader's cookie, when it is one this server could
// have given.
func dailyToken(r *http.Request) string {
	c, err := r.Cookie(dailyCookie)
	if err != nil || !daily.TokenOK(c.Value) {
		return ""
	}
	return c.Value
}

func setDailyCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     dailyCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   dailyMaxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// player is the reader's player, and their cookie, when they have one
// this server knows.
func (d *dailyRoutes) player(r *http.Request) (daily.Player, string, bool, error) {
	token := dailyToken(r)
	if token == "" {
		return daily.Player{}, "", false, nil
	}
	p, ok, err := d.store.DailyPlayer(r.Context(), daily.TokenHash(token))
	return p, token, ok, err
}

// puzzleOn is the puzzle for day, or catalog.ErrNotFound while the job
// has not picked one.
func (d *dailyRoutes) puzzleOn(ctx context.Context, day, now time.Time) (*daily.Puzzle, error) {
	d.mu.Lock()
	for _, p := range d.live {
		if p.Day.Equal(day) {
			d.mu.Unlock()
			return p, nil
		}
	}
	d.mu.Unlock()
	p, err := d.store.DailyPuzzle(ctx, day)
	if err != nil {
		return nil, err
	}
	d.keep(p, now)
	return p, nil
}

// puzzleNo is puzzle No. no, or catalog.ErrNotFound.
func (d *dailyRoutes) puzzleNo(ctx context.Context, no int, now time.Time) (*daily.Puzzle, error) {
	d.mu.Lock()
	p := d.live[no]
	d.mu.Unlock()
	if p != nil {
		return p, nil
	}
	p, err := d.store.DailyPuzzleNo(ctx, no)
	if err != nil {
		return nil, err
	}
	d.keep(p, now)
	return p, nil
}

// keep holds p while its day is current somewhere, and lets go of any
// held puzzle whose day has ended everywhere. A board of last month's
// puzzle is read from the store each time, and never crowds out today's.
func (d *dailyRoutes) keep(p *daily.Puzzle, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for no, q := range d.live {
		if !daily.Current(q.Day, now) {
			delete(d.live, no)
		}
	}
	if daily.Current(p.Day, now) {
		d.live[p.No] = p
	}
}

// named is the puzzle the path names, for a change to a game of it. A
// number that is no puzzle, or one whose day is no longer anybody's
// date, is refused as "day", which is how a page left open past
// midnight learns its map has ended; whether it has ended for this
// game, in the zone the game was started in, is for Play and the store
// to say.
func (d *dailyRoutes) named(w http.ResponseWriter, r *http.Request, now time.Time) (*daily.Puzzle, bool) {
	no, ok := puzzleNumber(w, r)
	if !ok {
		return nil, false
	}
	p, err := d.puzzleNo(r.Context(), no, now)
	if errors.Is(err, catalog.ErrNotFound) {
		dailyRefuse(w, http.StatusConflict, daily.ErrDay.Reason, "there is no such puzzle")
		return nil, false
	}
	if err != nil {
		d.fail(w, r, err)
		return nil, false
	}
	if !daily.Current(p.Day, now) {
		d.fail(w, r, daily.ErrDay)
		return nil, false
	}
	return p, true
}

// puzzleNumber is the puzzle number the path names, refused as "bad"
// when it is not one. A number is kept in an int4, so one past what that
// holds is no puzzle's, and is refused here: handed to the database it
// would fail to encode, which reads as an outage, a 503 and an error in
// the log, for anyone to ask for as often as they like.
func puzzleNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	no, err := strconv.ParseInt(r.PathValue("no"), 10, 32)
	if err != nil || no < 1 {
		dailyRefuse(w, http.StatusBadRequest, "bad", "no must be a puzzle's number")
		return 0, false
	}
	return int(no), true
}

// readerDay is a player's puzzle, the zone it is worked out in, and their
// game of it, nil before Play: the puzzle for their date in zone, unless
// their game of that one is unfinished and was started in a zone whose
// date has since moved on. Read from a zone still on its day after the
// game zone's midnight (a reader who has flown west, or a page sending
// another zone) that game could never be moved again, and the page would
// sit on a dead map until the reader's own midnight. So they are given
// the day the game's zone has moved on to, in that zone, as they would
// have had it there: its countdown, and, since Play and the board ask
// this too (gameZone), its Play and its board. That date is always the
// later one, since dates only go forward from the one the game was
// started on, and it is one the reader could have had by naming that
// zone, so it opens nothing early. Until the job has picked it, the dead
// game is theirs as it was. catalog.ErrNotFound when their date in zone
// has no puzzle.
func (d *dailyRoutes) readerDay(ctx context.Context, player int64, zone *time.Location, now time.Time) (*time.Location, *daily.Puzzle, *daily.Record, error) {
	p, err := d.puzzleOn(ctx, daily.DayIn(now, zone), now)
	if err != nil {
		return nil, nil, nil, err
	}
	rec, err := d.store.DailyGame(ctx, player, p.No)
	if err != nil || rec == nil || rec.Finished != nil || p.On(now, daily.Zone(rec.Zone)) {
		return zone, p, rec, err
	}
	there := daily.Zone(rec.Zone)
	later, err := d.puzzleOn(ctx, daily.DayIn(now, there), now)
	if errors.Is(err, catalog.ErrNotFound) {
		return zone, p, rec, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	rec, err = d.store.DailyGame(ctx, player, later.No)
	return there, later, rec, err
}

// gameZone is the zone readerDay works a player's puzzle out in, for
// Play and the board, which are handed their puzzle by number: zone
// itself while the player's date there has no puzzle.
func (d *dailyRoutes) gameZone(ctx context.Context, player int64, zone *time.Location, now time.Time) (*time.Location, error) {
	there, _, _, err := d.readerDay(ctx, player, zone, now)
	if errors.Is(err, catalog.ErrNotFound) {
		return zone, nil
	}
	return there, err
}

// rendered is a game as the page draws it, with its posters and photos.
func (d *dailyRoutes) rendered(ctx context.Context, p *daily.Puzzle, rec *daily.Record) (daily.Game, error) {
	films, people := p.Wants(rec)
	live, err := d.store.DailyLive(ctx, films, people)
	if err != nil {
		return daily.Game{}, err
	}
	return daily.Render(p, rec, live), nil
}

// dailyPlayer is who the page plays as: a name, and whether it is kept
// (a player) or only offered (nobody has pressed Play yet).
type dailyPlayer struct {
	Name  string `json:"name"`
	Saved bool   `json:"saved"`
}

// dailyToday is GET /daily.
type dailyToday struct {
	No   int    `json:"no"`
	Date string `json:"date"`
	// Now is the server's clock, and Next the next midnight in the zone
	// the puzzle was worked out in, when it ends: the reader's own, or
	// their game's when readerDay moved them on to its day. Both are in
	// UTC. The page counts down to Next on its own clock, corrected by
	// the difference between the two.
	Now    time.Time      `json:"now"`
	Next   time.Time      `json:"next"`
	Cards  []daily.Face   `json:"cards"`
	Start  []daily.Opened `json:"start"`
	Clues  daily.Clues    `json:"clues"`
	Player dailyPlayer    `json:"player"`
	// Played is how many games of the reader's puzzle have been started,
	// in every zone.
	Played int          `json:"played"`
	Streak daily.Streak `json:"streak"`
	Game   *daily.Game  `json:"game"`
}

// read is GET /daily: the reader's puzzle, the one for their date, or
// for a player whose game was left behind at its own zone's midnight the
// day that zone has moved on to (readerDay), laid out as the page draws
// it, the three starting cards, their game when they have one, and
// otherwise a name to play as. It writes nothing to the database; a
// reader with a player has their cookie renewed, and a name offered is
// remembered (nameOffers).
func (d *dailyRoutes) read(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := d.now()
	zone := zoneOf(r)
	player, token, ok, err := d.player(r)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	var p *daily.Puzzle
	var rec *daily.Record
	if ok {
		zone, p, rec, err = d.readerDay(ctx, player.ID, zone, now)
	} else {
		p, err = d.puzzleOn(ctx, daily.DayIn(now, zone), now)
	}
	if errors.Is(err, catalog.ErrNotFound) {
		dailyRefuse(w, http.StatusServiceUnavailable, "not-ready", "today's puzzle has not been picked yet")
		return
	}
	if err != nil {
		d.fail(w, r, err)
		return
	}
	var streak daily.Streak
	who := dailyPlayer{Name: player.Name, Saved: ok}
	if ok {
		setDailyCookie(w, token)
		if streak, err = d.store.DailyStreak(ctx, player.ID, p.No); err != nil {
			d.fail(w, r, err)
			return
		}
	} else {
		who.Name = d.names.Names(ctx).Make(d.intn, nil)
		d.offers.add(who.Name, now)
	}
	played, err := d.store.DailyPlayed(ctx, p.No)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	films, people := p.Wants(rec)
	live, err := d.store.DailyLive(ctx, films, people)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	body := dailyToday{
		No:     p.No,
		Date:   daily.DayString(p.Day),
		Now:    daily.Instant(now),
		Next:   daily.Instant(daily.Next(now, zone)),
		Cards:  p.Faces(),
		Start:  p.Opened(live),
		Clues:  p.Clues(),
		Player: who,
		Played: played,
		Streak: streak,
	}
	if rec != nil {
		g := daily.Render(p, rec, live)
		body.Game = &g
	}
	writeJSON(w, http.StatusOK, body)
}

// rename is POST /daily/name {}: a player is given a fresh name, kept;
// a reader with none is only offered another, remembered so Play can
// keep it (nameOffers), and nothing is written. Either way it is never
// the name they had.
func (d *dailyRoutes) rename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !readDailyJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	player, token, ok, err := d.player(r)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	names := d.names.Names(ctx)
	if !ok {
		name := names.Make(d.intn, func(n string) bool { return n == body.Name })
		d.offers.add(name, d.now())
		writeJSON(w, http.StatusOK, dailyPlayer{Name: name})
		return
	}
	tried := map[string]bool{player.Name: true}
	for range joinTries {
		name := names.Make(d.intn, func(n string) bool { return tried[n] })
		tried[name] = true
		err := d.store.RenameDailyPlayer(ctx, player.ID, name)
		if errors.Is(err, catalog.ErrNameTaken) {
			continue
		}
		if err != nil {
			d.fail(w, r, err)
			return
		}
		setDailyCookie(w, token)
		writeJSON(w, http.StatusOK, dailyPlayer{Name: name, Saved: true})
		return
	}
	d.fail(w, r, errors.New("api: every name offered to a rename was taken"))
}

// play is POST /daily/{no}/play {name}: the reader's player, made now
// if they have none, and their game of the puzzle for their date,
// started now in their zone if it has not been. A new player keeps the
// name they were offered when the server offered it, it is still one the
// pool could draw, and nobody has it.
func (d *dailyRoutes) play(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !readDailyJSON(w, r, &body) {
		return
	}
	now := d.now()
	zone := zoneOf(r)
	p, ok := d.named(w, r, now)
	if !ok {
		return
	}
	ctx := r.Context()
	player, token, ok, err := d.player(r)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	// The puzzle read gives a player whose game was left behind at its
	// zone's midnight is that zone's next one (readerDay), and Play takes
	// it, in that zone, as it would from there: otherwise the page would
	// offer a Play that could never be taken. Asked only once the
	// request's own zone has refused, so an ordinary Play costs nothing
	// more; a reader with no player has no game to have left.
	if !p.On(now, zone) && ok {
		if zone, err = d.gameZone(ctx, player.ID, zone, now); err != nil {
			d.fail(w, r, err)
			return
		}
	}
	if !p.On(now, zone) {
		d.fail(w, r, daily.ErrDay)
		return
	}
	if !ok {
		if !d.joins.allow(d.addr(r), now) {
			w.Header().Set("Retry-After", strconv.Itoa(int(joinEvery.Seconds())))
			dailyRefuse(w, http.StatusTooManyRequests, "busy", "too many new players from this address")
			return
		}
		token = daily.NewToken()
		if player, err = d.join(ctx, token, body.Name, now); err != nil {
			d.fail(w, r, err)
			return
		}
	}
	setDailyCookie(w, token)
	rec, err := d.store.StartDailyGame(ctx, player.ID, p.No, now, zone)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	// A game they already had keeps the zone it was started in, and
	// pressing Play again from another zone does not bring it back once
	// its own midnight has passed.
	if !p.On(now, daily.Zone(rec.Zone)) {
		d.fail(w, r, daily.ErrDay)
		return
	}
	g, err := d.rendered(ctx, p, rec)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"game": g, "player": dailyPlayer{Name: player.Name, Saved: true}})
}

// join makes a player for token, named offered when the server offered
// that name and it is still one the pool could draw, and otherwise, or
// when it is taken, a fresh one. Valid alone would let a page choose any
// first word and any last word in the pool, and put the pair on every
// reader's board.
func (d *dailyRoutes) join(ctx context.Context, token, offered string, now time.Time) (daily.Player, error) {
	names := d.names.Names(ctx)
	name := offered
	tried := map[string]bool{}
	if !d.offers.has(name, now) || !names.Valid(name) {
		name = names.Make(d.intn, nil)
	}
	for range joinTries {
		tried[name] = true
		p, err := d.store.CreateDailyPlayer(ctx, daily.TokenHash(token), name, d.hue())
		if !errors.Is(err, catalog.ErrNameTaken) {
			return p, err
		}
		name = names.Make(d.intn, func(n string) bool { return tried[n] })
	}
	return daily.Player{}, errors.New("api: every name offered to a new player was taken")
}

// addr is the address a new player is counted against: the client's
// own, as clientAddr reads it behind Railway's edge. Requests whose
// address cannot be read share one bucket.
func (d *dailyRoutes) addr(r *http.Request) string {
	if a, ok := clientAddr(r); ok {
		return a.String()
	}
	return ""
}

// dailyKey is the shape of an idempotency key: the page sends
// crypto.randomUUID().
var dailyKey = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// dailyMove is a move's body.
type dailyMove struct {
	Key  string `json:"key"`
	Seq  *int   `json:"seq"`
	Card string `json:"card"`
	Kind string `json:"kind"`
	Film string `json:"film"`
}

// The four clues "buy" sells.
var dailyBuys = map[string]bool{daily.KindDirector: true, daily.KindActor: true, daily.KindGenres: true, daily.KindStory: true}

func (d *dailyRoutes) flip(w http.ResponseWriter, r *http.Request) {
	d.act(w, r, func(m dailyMove) (string, string, bool) { return daily.KindFlip, m.Card, m.Card != "" })
}

func (d *dailyRoutes) buy(w http.ResponseWriter, r *http.Request) {
	d.act(w, r, func(m dailyMove) (string, string, bool) { return m.Kind, "", dailyBuys[m.Kind] })
}

func (d *dailyRoutes) guess(w http.ResponseWriter, r *http.Request) {
	d.act(w, r, func(m dailyMove) (string, string, bool) { return daily.KindGuess, m.Film, imdbid.Title(m.Film) })
}

func (d *dailyRoutes) reveal(w http.ResponseWriter, r *http.Request) {
	d.act(w, r, func(dailyMove) (string, string, bool) { return daily.KindReveal, "", true })
}

// act is every move: read and check the body, find the player and the
// puzzle, and hand the move to the store, which applies it once, and
// only while the puzzle's day lasts in the zone the game was started
// in. The zone this request names plays no part. what reads the move's
// kind and argument from the body, and whether they are well formed.
func (d *dailyRoutes) act(w http.ResponseWriter, r *http.Request, what func(dailyMove) (kind, arg string, ok bool)) {
	var body dailyMove
	if !readDailyJSON(w, r, &body) {
		return
	}
	kind, arg, ok := what(body)
	if !ok || !dailyKey.MatchString(body.Key) || body.Seq == nil || *body.Seq < 0 {
		dailyRefuse(w, http.StatusBadRequest, "bad", "a move takes a key, the seq it follows, and what it is")
		return
	}
	if dailyToken(r) == "" {
		dailyRefuse(w, http.StatusForbidden, "cookie", "a move needs the player cookie")
		return
	}
	now := d.now()
	p, ok := d.named(w, r, now)
	if !ok {
		return
	}
	ctx := r.Context()
	player, _, ok, err := d.player(r)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	if !ok {
		dailyRefuse(w, http.StatusForbidden, "cookie", "that cookie is nobody's")
		return
	}
	rec, err := d.store.DailyAct(ctx, player.ID, p, daily.Request{Key: body.Key, Seq: *body.Seq, Kind: kind, Arg: arg}, now)
	if errors.Is(err, daily.ErrStale) && rec != nil {
		g, gerr := d.rendered(ctx, p, rec)
		if gerr != nil {
			d.fail(w, r, gerr)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]any{"error": daily.ErrStale.Msg, "reason": daily.ErrStale.Reason, "game": g})
		return
	}
	if err != nil {
		d.fail(w, r, err)
		return
	}
	g, err := d.rendered(ctx, p, rec)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"game": g})
}

// board is GET /daily/{no}/board?tab=today|week: the top of the board
// and the rows around the reader, with their own place. A reader with no
// player sees the top alone. A puzzle that does not exist, or whose day
// has not come for the reader, in their zone, or for a player in the
// zone their game moved them on in (gameZone), has no board.
func (d *dailyRoutes) board(w http.ResponseWriter, r *http.Request) {
	no, ok := puzzleNumber(w, r)
	if !ok {
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = daily.TabToday
	}
	if tab != daily.TabToday && tab != daily.TabWeek {
		dailyRefuse(w, http.StatusBadRequest, "bad", "tab must be today or week")
		return
	}
	ctx := r.Context()
	now := d.now()
	p, err := d.puzzleNo(ctx, no, now)
	if errors.Is(err, catalog.ErrNotFound) {
		dailyRefuse(w, http.StatusNotFound, "no-puzzle", "there is no such puzzle")
		return
	}
	if err != nil {
		d.fail(w, r, err)
		return
	}
	player, _, ok, err := d.player(r)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	zone := zoneOf(r)
	if p.Day.After(daily.DayIn(now, zone)) && ok {
		if zone, err = d.gameZone(ctx, player.ID, zone, now); err != nil {
			d.fail(w, r, err)
			return
		}
	}
	if p.Day.After(daily.DayIn(now, zone)) {
		dailyRefuse(w, http.StatusNotFound, "no-puzzle", "there is no such puzzle")
		return
	}
	b, err := d.store.DailyBoard(ctx, p, tab, player.ID)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// nameOffers are the names offered to readers with no player, by
// GET /daily and New name, each with when it was offered: a new player
// keeps the name they bring to Play only when it is one of them. Every
// name on a board is then one a draw made, never a pair a page chose,
// and a pair can say what neither of its words does. They are held in
// each process's memory, which is all a name needs: an offer forgotten
// (past offerLife, swept, or lost to a restart) costs the reader only
// that name, as Play draws another and the page shows the one Play
// answers with.
type nameOffers struct {
	mu sync.Mutex
	at map[string]time.Time
}

const (
	// offerLife is how long an offer is kept: longer than anyone reads
	// the rules before pressing Play.
	offerLife = 12 * time.Hour
	// offerMax caps how many are held at once, a few megabytes. The
	// banner reads today on every visit, and every visit without a
	// player is an offer.
	offerMax = 50_000
)

func newNameOffers() *nameOffers {
	return &nameOffers{at: map[string]time.Time{}}
}

func (o *nameOffers) add(name string, now time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.at[name]; !ok && len(o.at) >= offerMax {
		o.sweep(now)
	}
	o.at[name] = now
}

// has is whether name was offered within offerLife of now.
func (o *nameOffers) has(name string, now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	at, ok := o.at[name]
	return ok && now.Sub(at) < offerLife
}

// sweep drops the offers past offerLife, and if none were, all of them,
// as joinLimiter's sweep does: a reader whose offer goes is given
// another name at Play, which is cheaper than memory without a bound.
// Called with the lock held.
func (o *nameOffers) sweep(now time.Time) {
	before := len(o.at)
	for name, at := range o.at {
		if now.Sub(at) >= offerLife {
			delete(o.at, name)
		}
	}
	if len(o.at) == before {
		clear(o.at)
	}
}

// joinLimiter is a token bucket per address, for making players.
type joinLimiter struct {
	mu      sync.Mutex
	clients map[string]*joinBucket
}

type joinBucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newJoinLimiter() *joinLimiter {
	return &joinLimiter{clients: map[string]*joinBucket{}}
}

// allow reports whether addr may make a player at now.
func (l *joinLimiter) allow(addr string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.clients[addr]
	if !ok {
		if len(l.clients) >= joinClients {
			l.sweep(now)
		}
		b = &joinBucket{limiter: rate.NewLimiter(rate.Every(joinEvery), joinBurst)}
		l.clients[addr] = b
	}
	b.seen = now
	return b.limiter.AllowN(now, 1)
}

// sweep drops addresses idle long enough to have refilled, and if none
// were, the whole table: a bucket lost that way gives one address a few
// extra players, which is cheaper than memory without a bound. Called
// with the lock held.
func (l *joinLimiter) sweep(now time.Time) {
	before := len(l.clients)
	for addr, b := range l.clients {
		if now.Sub(b.seen) > joinIdle {
			delete(l.clients, addr)
		}
	}
	if len(l.clients) == before {
		clear(l.clients)
	}
}
