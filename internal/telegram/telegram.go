// Package telegram tells one chat what the catalog jobs are doing, in a
// way that can be read from the lock screen.
//
// Three levels, because a phone makes a sound only for a new message:
//
//   - loud: a new message with sound, for something that has lasted
//     long enough to matter, or that only a person can fix;
//   - quiet: a new message with disable_notification set, for good news
//     and milestones. It still lands in the notification list, silently;
//   - the board: one pinned message, edited in place, with a line per
//     job. Edits never make a sound, which is what keeps a five-second
//     progress tick from becoming a few hundred notifications.
//
// The catalog sends facts (notify.Event); every word is written here,
// in words.go. What has been said is saved through notify.Memory, so a
// deploy does not say it again.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"cinedikt/internal/notify"
)

// boardEvery is the most often progress rewrites the board. Often
// enough that a long pass does not look frozen, rare enough to stay far
// inside what Telegram accepts for one chat.
const boardEvery = time.Minute

// The delivery limits.
const (
	// gather is how long a push waits for others to go out with it, so
	// a failure and the stale alert it causes arrive as one message.
	gather = 2 * time.Second
	// callGap is the least time between any two API calls.
	callGap = time.Second
	// callTimeout bounds one request.
	callTimeout = 10 * time.Second
	// shutdownBudget is all the time a process on its way out gives
	// the last pushes, the last board edit and the last save.
	shutdownBudget = 3 * time.Second
	saveTimeout    = 3 * time.Second
	// maxQueue bounds the events waiting for the loop, and maxOutbox
	// the messages waiting to be sent.
	maxQueue  = 1000
	maxOutbox = 20
	// maxAttempts is how many times a message is tried against a
	// failing server before it is given up.
	maxAttempts = 5
	// maxRetryAfter caps how long a 429 can make a message wait.
	maxRetryAfter = 5 * time.Minute
	// saveWarnEvery keeps a database that is down from filling the log
	// with the same warning.
	saveWarnEvery = time.Hour
)

// Config is where to send, and how to write times.
type Config struct {
	Token, ChatID string
	// Location is the zone times are written in. Nil is UTC, and the
	// board says so.
	Location *time.Location
	// Env and Commit name the deploy in the board's footer.
	Env, Commit string
	// Manual is a one-off cmd/importer run: no board, no memory, and
	// one quiet summary when it closes.
	Manual bool
}

// Start posts events to the chat for as long as ctx lasts. An empty
// token and an empty chat is silence, which is what a machine without
// the variables should do. One without the other is a misconfiguration
// and is logged, because a token with nowhere to send looks exactly
// like a broken bot.
func Start(ctx context.Context, cfg Config, logger *slog.Logger) notify.Sink {
	token := strings.TrimSpace(cfg.Token)
	chatID := strings.TrimSpace(cfg.ChatID)
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if token == "" && chatID == "" {
		return nil
	}
	if token == "" || chatID == "" {
		logger.Warn("telegram notifications are off", "reason", "TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID must both be set")
		return nil
	}
	s := newSink(ctx, cfg, logger)
	s.token, s.chatID = token, chatID
	go s.loop(ctx)
	logger.Info("telegram notifications on", "manual", cfg.Manual)
	return s
}

// Zone is the location NOTIFY_TIMEZONE names. Unset, or a name that
// does not load, is nil: times in UTC, and the board says so rather
// than letting a person read 21:14 as their own evening. The binary
// has to import time/tzdata for a name to load on a machine without a
// zoneinfo database, which is what the container is.
func Zone(name string, logger *slog.Logger) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		if logger != nil {
			logger.Warn("NOTIFY_TIMEZONE does not name a zone; notifications will use UTC", "zone", name, "err", err)
		}
		return nil
	}
	return loc
}

func newSink(ctx context.Context, cfg Config, logger *slog.Logger) *Sink {
	loc := cfg.Location
	utc := loc == nil
	if loc == nil {
		loc = time.UTC
	}
	return &Sink{
		token:       cfg.Token,
		chatID:      cfg.ChatID,
		loc:         loc,
		place:       place{env: cfg.Env, commit: cfg.Commit, utc: utc},
		manual:      cfg.Manual,
		logger:      logger,
		client:      &http.Client{},
		base:        "https://api.telegram.org",
		now:         time.Now,
		sleep:       time.Sleep,
		attachRetry: 2 * time.Second,
		ctx:         ctx,
		reqBase:     context.WithoutCancel(ctx),
		wake:        make(chan struct{}, 1),
		closing:     make(chan struct{}),
		done:        make(chan struct{}),
		st:          newState(),
	}
}

// Sink is a notify.Sink. Start builds one; the zero value is not useful.
type Sink struct {
	token  string
	chatID string
	loc    *time.Location
	place  place
	manual bool
	logger *slog.Logger
	client *http.Client
	base   string
	now    func() time.Time
	// sleep keeps API calls apart. It blocks only the loop, never Note.
	sleep       func(time.Duration)
	attachRetry time.Duration
	ctx         context.Context

	wake      chan struct{}
	closing   chan struct{}
	closeOnce sync.Once
	done      chan struct{}

	mu        sync.Mutex
	queue     []notify.Event
	attaching bool
	attach    *attachment
	detach    bool

	// Everything below belongs to the loop goroutine.
	st     *state
	memory notify.Memory
	// lost is a sink that held the jobs and lost them without shutting
	// down. It keeps the board's id, and writes it only while the
	// database is out of reach, when nobody holds the jobs to say so.
	lost      bool
	reqBase   context.Context
	pending   []part
	gatherBy  time.Time
	outbox    []*outgoing
	dirty     bool
	force     bool
	pushed    bool
	lastEdit  time.Time
	boardWait time.Time
	lastCall  time.Time
	disabled  bool
	plainOnly bool
	heard     []notify.Event
	savedWarn time.Time
	pinWarned bool
}

type attachment struct {
	memory notify.Memory
	raw    []byte
}

// outgoing is one message waiting to be sent.
type outgoing struct {
	parts     []part
	loud      bool
	plain     bool
	attempts  int
	notBefore time.Time
}

// Note queues the event and returns. The HTTP happens on the sink's own
// goroutine, so a slow Telegram can never stall a job.
func (s *Sink) Note(e notify.Event) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if len(s.queue) >= maxQueue {
		s.queue = dropOne(s.queue)
	}
	s.queue = append(s.queue, e)
	s.mu.Unlock()
	s.kick()
}

// dropOne makes room in a full queue. Progress goes first: the next
// tick says the same thing again, and a failure or a publish is said
// only once.
func dropOne(q []notify.Event) []notify.Event {
	for i, e := range q {
		if e.Kind == notify.Progress {
			return append(q[:i], q[i+1:]...)
		}
	}
	return q[1:]
}

func (s *Sink) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Attach makes this sink the one that owns the board and remembers what
// it said. It is called by the process that took the catalog jobs, so a
// container waiting its turn during a deploy never edits the board the
// running one is editing. The saved state is read here, in the caller,
// so the loop is never blocked on the database.
func (s *Sink) Attach(m notify.Memory) {
	if s == nil || m == nil || s.manual {
		return
	}
	s.mu.Lock()
	if s.attaching {
		s.mu.Unlock()
		return
	}
	s.attaching = true
	s.mu.Unlock()

	var raw []byte
	var err error
	for try := 0; try < 3; try++ {
		if try > 0 {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(s.attachRetry):
			}
		}
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		raw, err = m.LoadNotifyState(ctx)
		cancel()
		if err == nil {
			break
		}
	}
	if err != nil {
		s.logger.Warn("telegram: could not read what was already said; starting afresh", "err", err)
		raw = nil
	}
	s.mu.Lock()
	s.attach = &attachment{memory: m, raw: raw}
	// A takeover straight after a loss: this process holds the jobs
	// again, so a detach not yet acted on no longer applies.
	s.detach = false
	s.mu.Unlock()
	s.kick()
}

// Detach is this process losing the jobs without shutting down: the
// lease connection dropped, and another process may take them within
// seconds. What was said is saved once more, and from then on the board
// and the saved state belong to whoever holds the jobs; a last board, a
// shutdown's "Shutting down" or a save from here would overwrite theirs.
// A later takeover here attaches afresh and reads the state back.
func (s *Sink) Detach() {
	if s == nil || s.manual {
		return
	}
	s.mu.Lock()
	s.attaching = false
	s.attach = nil
	s.detach = true
	s.mu.Unlock()
	s.kick()
}

// Close waits, up to ctx, for the sink to say what it still has to say.
// For a manual run that is its one summary; for the process that owns
// the board it is the last pushes and the last board.
func (s *Sink) Close(ctx context.Context) {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() { close(s.closing) })
	select {
	case <-s.done:
	case <-ctx.Done():
	}
}

// loop is the only goroutine that touches the state or Telegram. Every
// delay it needs, from a retry to the next board edit, is a time it
// wakes at, never a wait that holds anyone else up.
func (s *Sink) loop(ctx context.Context) {
	defer close(s.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		next := s.step()
		timer.Stop()
		if !next.IsZero() {
			timer.Reset(max(next.Sub(s.now()), 0))
		}
		select {
		case <-ctx.Done():
			s.shutdown()
			return
		case <-s.closing:
			s.shutdown()
			return
		case <-s.wake:
		case <-timer.C:
		}
	}
}

// step takes in whatever has arrived, sends what is due, and says when
// it next has something to do. Zero means nothing until woken.
func (s *Sink) step() time.Time {
	s.absorb()
	if len(s.pending) > 0 && !s.now().Before(s.gatherBy) {
		s.enqueue()
	}
	for !s.disabled {
		m := s.due()
		if m == nil {
			break
		}
		s.send(m)
	}
	if s.boardDue() {
		s.writeBoard()
	}
	return s.nextWake()
}

// absorb applies a pending Attach, then every queued event, in order,
// then a pending Detach. The events before a detach are the last of
// this process's time with the jobs, and belong in what it saves.
func (s *Sink) absorb() {
	s.mu.Lock()
	events := s.queue
	s.queue = nil
	att := s.attach
	s.attach = nil
	det := s.detach
	s.detach = false
	s.mu.Unlock()
	if att != nil {
		s.applyAttach(att)
	}
	for _, e := range events {
		s.handle(e)
	}
	if det {
		s.applyDetach()
	}
}

// applyAttach takes up the saved state. The one thing kept from memory
// is a database alert this process raised before it had anything to
// load: it is newer than anything saved.
func (s *Sink) applyAttach(att *attachment) {
	saved := loadState(att.raw)
	if a := s.st.Alerts[alertDatabase]; a != nil {
		saved.Alerts[alertDatabase] = a
	}
	s.st = saved
	s.memory = att.memory
	s.lost = false
	s.dirty = true
}

// applyDetach saves what was said one last time and lets go of the
// memory, which is what stops the board edits and the saves.
func (s *Sink) applyDetach() {
	if s.memory == nil {
		return
	}
	s.save()
	s.memory = nil
	s.lost = true
	s.dirty, s.force, s.pushed = false, false, false
}

// ownsBoard says whether this process may write the board: when it holds
// the jobs, or when it lost them to the database going away and the
// database is still away, so that nobody holds them.
func (s *Sink) ownsBoard() bool {
	if s.manual || s.disabled {
		return false
	}
	return s.memory != nil || (s.lost && s.st.Alerts[alertDatabase] != nil)
}

func (s *Sink) writer() writer {
	return writer{loc: s.loc, now: s.now(), token: s.token}
}

func (s *Sink) handle(e notify.Event) {
	if e.At.IsZero() {
		e.At = s.now()
	}
	w := s.writer()
	fx := s.st.apply(e, w)
	if s.manual {
		// A manual run says one thing, at the end, from all of it.
		s.heard = append(s.heard, e)
		return
	}
	attached := s.memory != nil
	// A process that does not hold the jobs speaks only about the
	// database, which is the one thing it can see better than anyone.
	if len(fx.parts) > 0 && (attached || e.Job == notify.JobDatabase) {
		if len(s.pending) == 0 {
			s.gatherBy = w.now.Add(gather)
		}
		s.pending = append(s.pending, fx.parts...)
	}
	if s.ownsBoard() {
		s.dirty = s.dirty || fx.dirty
		s.force = s.force || fx.force
	}
}

// enqueue turns the parts gathered so far into one message.
func (s *Sink) enqueue() {
	if len(s.pending) == 0 {
		return
	}
	m := &outgoing{parts: s.pending}
	for _, p := range m.parts {
		m.loud = m.loud || p.loud
	}
	s.pending = nil
	s.outbox = append(s.outbox, m)
	if len(s.outbox) > maxOutbox {
		drop := 0
		for i, o := range s.outbox {
			if !o.loud {
				drop = i
				break
			}
		}
		s.logger.Warn("telegram: too many messages waiting; dropping one", "loud", s.outbox[drop].loud)
		s.outbox = append(s.outbox[:drop], s.outbox[drop+1:]...)
	}
}

func (s *Sink) due() *outgoing {
	now := s.now()
	for _, m := range s.outbox {
		if !now.Before(m.notBefore) {
			return m
		}
	}
	return nil
}

func (s *Sink) remove(m *outgoing) {
	for i, o := range s.outbox {
		if o == m {
			s.outbox = append(s.outbox[:i], s.outbox[i+1:]...)
			return
		}
	}
}

// send tries one message once, and decides what happens to it if
// Telegram says no.
func (s *Sink) send(m *outgoing) {
	text, loud := message(m.parts)
	mode := "HTML"
	if m.plain || s.plainOnly {
		text, mode = stripTags(text), ""
	}
	_, err := s.sendMessage(text, mode, !loud)
	if err == nil {
		s.remove(m)
		s.pushed = true
		s.save()
		return
	}
	now := s.now()
	var ae *apiError
	isAPI := errors.As(err, &ae)
	switch {
	case isAPI && ae.Code == http.StatusTooManyRequests:
		// Telegram's own number, which is never a reason to give up.
		m.notBefore = now.Add(min(max(ae.RetryAfter, time.Second), maxRetryAfter))
	case s.fatal(err):
	case isAPI && ae.Code == http.StatusBadRequest && ae.has("can't parse entities") && mode != "":
		m.plain = true
	case isAPI && ae.Code >= 400 && ae.Code < 500:
		s.logger.Error("telegram: message refused; dropping it", "err", s.scrub(err))
		s.remove(m)
	default:
		m.attempts++
		if m.attempts >= maxAttempts {
			s.logger.Error("telegram: giving up on a message", "attempts", m.attempts, "err", s.scrub(err))
			s.remove(m)
			return
		}
		m.notBefore = now.Add(backoff(m.attempts))
	}
}

// backoff is 2, 4, 8, 16, then 30 seconds.
func backoff(attempt int) time.Duration {
	d := time.Second << attempt
	return min(d, 30*time.Second)
}

func (s *Sink) disable() {
	s.disabled = true
	s.outbox = nil
	s.pending = nil
}

// fatal handles Telegram saying no in a way no retry will change: the
// token is not a bot's, the bot cannot post to the chat, or the chat has
// a new id. It logs one error, turns sending off until the next start,
// and reports whether it did.
func (s *Sink) fatal(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	switch {
	case ae.MigrateTo != 0:
		s.logger.Error("telegram: the chat became a supergroup; set TELEGRAM_CHAT_ID to the new id and redeploy",
			"new_chat_id", ae.MigrateTo)
	case ae.Code == http.StatusUnauthorized || ae.Code == http.StatusNotFound:
		s.logger.Error("telegram: TELEGRAM_BOT_TOKEN was rejected; sending is off until the next start",
			"err", s.scrub(err))
	case ae.Code == http.StatusForbidden || (ae.Code == http.StatusBadRequest && ae.has("chat not found")):
		s.logger.Error("telegram: the bot cannot post to this chat; sending is off until the next start",
			"err", s.scrub(err))
	default:
		return false
	}
	s.disable()
	return true
}

// boardDue says whether the board should be written now. Only the
// process that holds the jobs writes it, save for the one case in
// ownsBoard where nobody does.
func (s *Sink) boardDue() bool {
	if !s.ownsBoard() {
		return false
	}
	now := s.now()
	if now.Before(s.boardWait) {
		return false
	}
	if s.force || s.pushed {
		return true
	}
	return s.dirty && (s.lastEdit.IsZero() || now.Sub(s.lastEdit) >= boardEvery)
}

// writeBoard renders the board and puts it in the chat: an edit when
// there is one to edit, otherwise a new message, pinned.
func (s *Sink) writeBoard() {
	w := s.writer()
	full, body := s.st.render(w, s.place)
	if !s.force && s.st.BoardID != 0 && body == s.st.BoardBody {
		s.dirty, s.pushed = false, false
		return
	}
	var err error
	if s.st.BoardID == 0 {
		err = s.newBoard(full, 0)
	} else {
		err = s.editBoard(full)
		if isNotModified(err) {
			err = nil
		}
		if isGone(err) {
			err = s.newBoard(full, s.st.BoardID)
		}
	}
	if err != nil {
		if s.fatal(err) {
			return
		}
		s.logger.Warn("telegram: board not written; trying again", "err", s.scrub(err))
		var ae *apiError
		wait := boardEvery
		if errors.As(err, &ae) && ae.RetryAfter > wait {
			wait = min(ae.RetryAfter, maxRetryAfter)
		}
		s.boardWait = s.now().Add(wait)
		return
	}
	s.st.BoardBody = body
	s.lastEdit = s.now()
	s.dirty, s.force, s.pushed = false, false, false
	s.save()
}

// editBoard edits the board, falling back to plain text for good if
// Telegram will not parse it.
func (s *Sink) editBoard(text string) error {
	if !s.plainOnly {
		err := s.editMessage(s.st.BoardID, text, "HTML")
		if !isParseError(err) {
			return err
		}
		s.logger.Warn("telegram: formatting refused; sending plain text from now on", "err", s.scrub(err))
		s.plainOnly = true
	}
	return s.editMessage(s.st.BoardID, stripTags(text), "")
}

// newBoard sends a fresh board, pins it without a sound, and unpins the
// one it replaces, if any.
func (s *Sink) newBoard(text string, old int) error {
	mode := "HTML"
	if s.plainOnly {
		text, mode = stripTags(text), ""
	}
	id, err := s.sendMessage(text, mode, true)
	if isParseError(err) {
		s.logger.Warn("telegram: formatting refused; sending plain text from now on", "err", s.scrub(err))
		s.plainOnly = true
		id, err = s.sendMessage(stripTags(text), "", true)
	}
	if err != nil {
		return err
	}
	s.st.BoardID = id
	if err := s.call("pinChatMessage", outbound{ChatID: s.chatID, MessageID: id, DisableNotification: true}, nil); err != nil && !s.pinWarned {
		s.logger.Warn("telegram: could not pin the board; it still works, just not at the top", "err", s.scrub(err))
		s.pinWarned = true
	}
	if old != 0 {
		_ = s.call("unpinChatMessage", outbound{ChatID: s.chatID, MessageID: old}, nil)
	}
	return nil
}

// nextWake is the earliest thing still to do.
func (s *Sink) nextWake() time.Time {
	var next time.Time
	soonest := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	gap := s.lastCall.Add(callGap)
	if len(s.pending) > 0 {
		soonest(s.gatherBy)
	}
	if !s.disabled {
		for _, m := range s.outbox {
			soonest(later(m.notBefore, gap))
		}
	}
	if s.ownsBoard() {
		switch {
		case s.force || s.pushed:
			soonest(later(s.boardWait, gap))
		case s.dirty:
			soonest(later(later(s.lastEdit.Add(boardEvery), s.boardWait), gap))
		}
	}
	return next
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// shutdown is the last thing the loop does: whatever is still waiting
// goes out, the board says the process is shutting down, and the state
// is saved, all within shutdownBudget. A process that has lost the jobs
// only sends what is waiting: the board and the state are not its own.
func (s *Sink) shutdown() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), shutdownBudget)
	defer cancel()
	s.reqBase = ctx
	s.absorb()
	if s.manual {
		if p, ok := s.writer().manual(s.heard); ok {
			s.pending = append(s.pending, p)
		}
	}
	attached := s.memory != nil && !s.manual
	if attached {
		for _, j := range s.st.Jobs {
			if j.State == stRunning {
				j.State, j.Since = stInterrupted, s.now()
			}
		}
		s.st.shutting = true
		s.force = true
		// Saved first, while the budget is whole: what has been said
		// matters more to the next process than the last board does.
		s.save()
	}
	s.enqueue()
	for len(s.outbox) > 0 && !s.disabled && ctx.Err() == nil {
		m := s.outbox[0]
		wasPlain := m.plain
		s.send(m)
		if len(s.outbox) == 0 || s.outbox[0] != m {
			continue
		}
		// Still waiting. A plain-text resend gets its one more try;
		// anything else has had its chance, because there is no later.
		if m.plain && !wasPlain {
			continue
		}
		s.remove(m)
	}
	if attached && !s.disabled && ctx.Err() == nil {
		s.boardWait = time.Time{}
		s.writeBoard()
	}
	if attached {
		s.save()
	}
}

// save writes the state through the Memory, if this sink has one.
func (s *Sink) save() {
	if s.memory == nil || s.manual {
		return
	}
	raw, err := json.Marshal(s.st)
	if err == nil {
		// Under reqBase, so a save on the way out stays inside the
		// shutdown budget.
		ctx, cancel := context.WithTimeout(s.reqBase, saveTimeout)
		err = s.memory.SaveNotifyState(ctx, raw)
		cancel()
	}
	if err != nil && (s.savedWarn.IsZero() || s.now().Sub(s.savedWarn) >= saveWarnEvery) {
		s.logger.Warn("telegram: could not save what was said; a restart may repeat it", "err", err)
		s.savedWarn = s.now()
	}
}

type outbound struct {
	ChatID              string       `json:"chat_id"`
	Text                string       `json:"text,omitempty"`
	ParseMode           string       `json:"parse_mode,omitempty"`
	DisableNotification bool         `json:"disable_notification,omitempty"`
	MessageID           int          `json:"message_id,omitempty"`
	LinkPreview         *linkPreview `json:"link_preview_options,omitempty"`
}

type linkPreview struct {
	IsDisabled bool `json:"is_disabled"`
}

var noPreview = &linkPreview{IsDisabled: true}

type apiResponse struct {
	OK          bool            `json:"ok"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
	Parameters  struct {
		RetryAfter      int   `json:"retry_after"`
		MigrateToChatID int64 `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

// apiError is Telegram saying no, with what it said about when to ask
// again.
type apiError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  time.Duration
	MigrateTo   int64
}

func (e *apiError) Error() string {
	return fmt.Sprintf("telegram: %s: %d %s", e.Method, e.Code, e.Description)
}

func (e *apiError) has(s string) bool {
	return strings.Contains(strings.ToLower(e.Description), s)
}

func (s *Sink) sendMessage(text, mode string, silent bool) (int, error) {
	var out struct {
		MessageID int `json:"message_id"`
	}
	err := s.call("sendMessage", outbound{
		ChatID:              s.chatID,
		Text:                text,
		ParseMode:           mode,
		DisableNotification: silent,
		LinkPreview:         noPreview,
	}, &out)
	return out.MessageID, err
}

func (s *Sink) editMessage(id int, text, mode string) error {
	return s.call("editMessageText", outbound{
		ChatID:      s.chatID,
		Text:        text,
		ParseMode:   mode,
		MessageID:   id,
		LinkPreview: noPreview,
	}, nil)
}

// call makes one request, at least callGap after the one before it.
func (s *Sink) call(method string, body outbound, result any) error {
	if !s.lastCall.IsZero() {
		if wait := s.lastCall.Add(callGap).Sub(s.now()); wait > 0 {
			s.sleep(wait)
		}
	}
	s.lastCall = s.now()
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.reqBase, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/bot"+s.token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var out apiResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return &apiError{Method: method, Code: resp.StatusCode, Description: http.StatusText(resp.StatusCode)}
	}
	if !out.OK {
		code := out.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &apiError{
			Method:      method,
			Code:        code,
			Description: out.Description,
			RetryAfter:  time.Duration(out.Parameters.RetryAfter) * time.Second,
			MigrateTo:   out.Parameters.MigrateToChatID,
		}
	}
	if result != nil && len(out.Result) > 0 && out.Result[0] == '{' {
		_ = json.Unmarshal(out.Result, result)
	}
	return nil
}

// scrub takes the token out of an error before it is logged. A
// transport error carries the request URL, and the URL carries the
// token.
func (s *Sink) scrub(err error) error {
	if err == nil {
		return nil
	}
	if s.token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), s.token, "…"))
}

func isNotModified(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.has("message is not modified")
}

func isGone(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.has("message to edit not found") || ae.has("message can't be edited"))
}

func isParseError(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Code == http.StatusBadRequest && ae.has("can't parse entities")
}
