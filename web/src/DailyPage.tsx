import {
  Fragment,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from 'react';
import { capture } from './analytics';
import {
  ApiError,
  fetchDaily,
  playDaily,
  resetDaily,
  sendDailyMove,
  type DailyFactKind,
  type DailyGame,
  type DailyMove,
  type DailyPerson,
  type DailyToday,
} from './api';
import { DailyBar } from './DailyBar';
import { DailyCast } from './DailyCast';
import { DailyMoviesSheet } from './DailyMoviesSheet';
import { AgainButton, DailyLeaderboard, DailyResult, useBoards } from './DailyResult';
import { DailyRules } from './DailyRules';
import { DailyTitle } from './DailyTitle';
import {
  AGAIN_FAILED,
  CAST_HEADING,
  GAME_NAME,
  LANDSCAPE_QUERY,
  PLAY_HIDE_MS,
  REVEAL_CONFIRM_MS,
  SHOW_ANSWER,
  SHOW_ANSWER_SURE,
  TOASTS,
  UNREACHABLE,
  aboutItems,
  cardColour,
  castRows,
  clockOffset,
  codesOf,
  dailyDateText,
  earlyRefusal,
  factItems,
  factsHeading,
  guessById,
  guessMessage,
  guessedIds,
  isLandscapePhone,
  midnightText,
  moveSig,
  newKey,
  newWrongGuess,
  newestGuess,
  openingRows,
  pageHeading,
  pageLine,
  pageSections,
  refusalText,
  revealDelays,
  shareText,
  staleGame,
  todaysPeople,
  toneStyle,
  triedChips,
  viewportFit,
  warmthColour,
  watchMidnight,
  type PageSection,
} from './daily';
import { stillNow } from './motion';
import { PosterImage } from './PosterImage';
import { posterFallback } from './poster';
import { useScreen } from './screen';
import { isTyping } from './search';
import { useReducedMotion, useResolvedTheme, type Theme } from './theme';
import { Toast, useToast } from './Toast';

// Cinedikt Daily's page, under the app's header, and its game, Name Drop:
// the title screen, then the game page — the hidden card and the wrong
// guesses, the cast showing up one name at a time, the facts for sale and
// the guess bar pinned under them — and, once it is over, the answer, the
// result and the leaderboard, with the cast below. The server owns the
// game: every move is sent, and the page draws the game it sends back.

interface Props {
  /** Told the puzzle's number and day once it has loaded, for the
   *  header; null when it could not be loaded or is not ready yet. */
  onDay?: (day: { no: number; date: string } | null) => void;
  /** Bumped by the header's "How it works": each change opens it. */
  rulesSignal?: number;
  /** A map is loading over the page (Map this movie, say). The page steps
   *  back behind the progress line until it arrives, as the opening
   *  screen and the About page do. */
  dim?: boolean;
  /** Map this movie: the app's own way to a movie's map, by its IMDb id,
   *  with its title for the progress line, so the Daily leaves for the
   *  map as any other screen does. */
  onOpenMovie?: (id: string, title: string) => void;
}

type Load =
  | { state: 'loading' }
  | { state: 'ready'; today: DailyToday; offset: number; n: number }
  | { state: 'failed'; reason: string | null };

/** Play again, in development: asks the server to start the reader
 *  again (resetDaily, as `reset`), and once it has, starts the page
 *  again from nothing, `restart`, as though it had just been opened. A
 *  reset that is refused changed nothing, and one that never got an
 *  answer changed nothing the page can know of, so either leaves the
 *  page as it was and tells the reader, who can simply press again. */
export function playAgain(
  reset: () => Promise<void>,
  restart: () => void,
  say: (text: string) => void,
): Promise<void> {
  return reset().then(restart, () => say(AGAIN_FAILED));
}

/** Sizes the app to the visual viewport while the Daily is up: its top
 *  and height, as custom properties on the app's box, which the
 *  stylesheet reads (.cd-app:has(> .cd-daily)). On iOS the keyboard
 *  covers the bottom of a layout viewport that stays full height, and the
 *  guess bar would sit under it; fitted to the visual viewport, the page
 *  ends where the keyboard begins and the bar rides just above it. Let go
 *  while the reader is zoomed in (viewportFit), and when the page goes. */
function useViewportFit(box: RefObject<HTMLElement | null>): void {
  useEffect(() => {
    const vv = typeof window === 'undefined' ? null : window.visualViewport;
    const app = box.current?.parentElement;
    if (!vv || !app) return;
    const fit = () => {
      const f = viewportFit(vv);
      if (f) {
        app.style.setProperty('--vv-top', `${f.top}px`);
        app.style.setProperty('--vv-h', `${f.height}px`);
      } else {
        app.style.removeProperty('--vv-top');
        app.style.removeProperty('--vv-h');
      }
    };
    fit();
    vv.addEventListener('resize', fit);
    vv.addEventListener('scroll', fit);
    return () => {
      vv.removeEventListener('resize', fit);
      vv.removeEventListener('scroll', fit);
      app.style.removeProperty('--vv-top');
      app.style.removeProperty('--vv-h');
    };
  }, [box]);
}

/** Whether the window is a landscape phone (LANDSCAPE_QUERY), read now.
 *  Its width and height stand in where matchMedia is missing. */
function landscapeNow(): boolean {
  if (typeof window === 'undefined') return false;
  if (typeof window.matchMedia === 'function') return window.matchMedia(LANDSCAPE_QUERY).matches;
  return isLandscapePhone(window.innerWidth, window.innerHeight);
}

/** A landscape phone, kept as the window turns: the guess bar becomes a
 *  column beside the cast. Watched from script rather than written as a
 *  media query because 520px tall is not one of the stylesheet's screen
 *  classes, which screen.test.ts holds every query in grid.css to. */
function useLandscape(): boolean {
  const [land, setLand] = useState(landscapeNow);
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const mq = window.matchMedia(LANDSCAPE_QUERY);
    const read = () => setLand(mq.matches);
    read();
    mq.addEventListener('change', read);
    return () => mq.removeEventListener('change', read);
  }, []);
  return land;
}

/** Cinedikt Daily, under the app's header. It asks for today's puzzle as
 *  it opens, again at the reader's midnight, and again whenever the
 *  server says the game it is showing has moved on (a new day, a game
 *  that ended in another tab). */
export function DailyPage({ onDay, rulesSignal = 0, dim = false, onOpenMovie }: Props) {
  const toast = useToast();
  const box = useRef<HTMLDivElement>(null);
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [asked, setAsked] = useState(0);
  useViewportFit(box);

  useEffect(() => {
    const ctrl = new AbortController();
    fetchDaily(ctrl.signal)
      .then((today) => {
        if (!ctrl.signal.aborted) setLoad({ state: 'ready', today, offset: clockOffset(today.now, Date.now()), n: asked });
      })
      .catch((e: unknown) => {
        if (!ctrl.signal.aborted) setLoad({ state: 'failed', reason: e instanceof ApiError ? e.reason : null });
      });
    return () => ctrl.abort();
  }, [asked]);

  // The latest callback, read when there is news, so one passed afresh on
  // each render is not told the same day again.
  const tell = useRef(onDay);
  tell.current = onDay;
  const day = load.state === 'ready' ? `${load.today.no}|${load.today.date}` : load.state;
  useEffect(() => {
    if (load.state === 'ready') tell.current?.({ no: load.today.no, date: load.today.date });
    else if (load.state === 'failed') tell.current?.(null);
    // `day` stands for the puzzle shown, or for its absence.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [day]);

  const { show, hide } = toast;
  const say = useCallback((text: string) => show({ text }), [show]);
  const reload = useCallback(() => setAsked((n) => n + 1), []);

  // Play again (development only). Not a reload, which keeps the old game
  // up until the new puzzle lands: that game is gone on the server, so the
  // view drawing it goes at once, and with it all it had running — its
  // timers, a guess half typed — and any toast it raised. A press while
  // one is on its way is the same press.
  const restart = useCallback(() => {
    hide();
    setLoad({ state: 'loading' });
    setAsked((n) => n + 1);
  }, [hide]);
  const resetting = useRef(false);
  const again = useCallback(() => {
    if (resetting.current) return;
    resetting.current = true;
    void playAgain(resetDaily, restart, say).finally(() => {
      resetting.current = false;
    });
  }, [restart, say]);

  return (
    <div ref={box} className={`cd-daily${dim ? ' cd-daily-dim' : ''}`}>
      {load.state === 'ready' ? (
        <DailyGameView
          // A reload starts the game view afresh from what the server
          // says: whatever the old one was showing is what was wrong.
          key={`${load.today.no}:${load.n}`}
          today={load.today}
          offset={load.offset}
          say={say}
          reload={reload}
          again={again}
          rulesSignal={rulesSignal}
          onOpenMovie={onOpenMovie}
        />
      ) : load.state === 'failed' ? (
        <DailyMissing notReady={load.reason === 'not-ready'} onRetry={reload} />
      ) : null}
      {/* The Daily's own toast, 170px up, clear of the guess bar. */}
      <Toast spec={toast.spec} visible={toast.visible} />
    </div>
  );
}

/** Today's puzzle could not be had: not picked yet, or not reachable. */
function DailyMissing({ notReady, onRetry }: { notReady: boolean; onRetry: () => void }) {
  const [title, body] = notReady
    ? ['Today’s movie isn’t ready yet', 'Try again in a few minutes.']
    : ['We couldn’t open today’s movie', UNREACHABLE];
  return (
    <div className="cd-error" role="alert">
      <h2 className="cd-error-title">{title}</h2>
      <p className="cd-error-body">{body}</p>
      <button type="button" className="cd-error-primary" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}

interface GameProps {
  today: DailyToday;
  /** The server's clock less this one's. */
  offset: number;
  say: (text: string) => void;
  reload: () => void;
  /** Play again, offered only when today's puzzle says `dev`: a server in
   *  production never does. */
  again: () => void;
  rulesSignal: number;
  onOpenMovie?: (id: string, title: string) => void;
}

/** No end cascade: the names are simply there. */
const NO_DELAYS: ReadonlyMap<number, number> = new Map();

/** The game for one loaded puzzle. Exported for its tests, which draw it
 *  from a fixture; the page draws it once today's puzzle has arrived. */
export function DailyGameView({ today, offset, say, reload, again, rulesSignal, onOpenMovie }: GameProps) {
  const screen = useScreen();
  const theme = useResolvedTheme();
  const still = useReducedMotion();
  const land = useLandscape();
  const main = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const [game, setGame] = useState<DailyGame | null>(today.game);
  const gameRef = useRef(game);
  // Play hiding every name for a moment, so the first comes in from
  // nothing. Nothing can be done meanwhile.
  const [opening, setOpening] = useState(false);
  const openingRef = useRef(false);
  // The game ended here, rather than being opened over: the card turns,
  // the names not seen arrive one after another, the score counts up.
  const [ended, setEnded] = useState(false);
  // The wrong guess whose message the bar shows, by IMDb id.
  const [msgId, setMsgId] = useState<string | null>(null);
  // "Show the answer" pressed once: a second press within
  // REVEAL_CONFIRM_MS gives the game up.
  const [sure, setSure] = useState(false);
  const sureTimer = useRef(0);
  const [rules, setRules] = useState(false);
  const rulesRef = useRef(rules);
  rulesRef.current = rules;
  // The Movies sheet, open on this person, by IMDb id.
  const [sheet, setSheet] = useState<string | null>(null);
  const sheetRef = useRef(sheet);
  sheetRef.current = sheet;
  const [starting, setStarting] = useState(false);
  const busyRef = useRef(false);
  const [topAsk, setTopAsk] = useState(0);
  const [focusAsk, setFocusAsk] = useState(0);

  // Timers that must not outlive the view.
  const timers = useRef(new Set<number>());
  const later = useCallback((fn: () => void, ms: number) => {
    const t = window.setTimeout(() => {
      timers.current.delete(t);
      fn();
    }, ms);
    timers.current.add(t);
  }, []);
  useEffect(() => {
    const all = timers.current;
    return () => {
      all.forEach((t) => window.clearTimeout(t));
      window.clearTimeout(sureTimer.current);
    };
  }, []);

  const codes = useMemo(() => codesOf(game ? todaysPeople(game) : []), [game]);
  const done = game?.phase === 'done';
  const boards = useBoards(today.no, done);

  // ---- taking a game the server sent ----

  /** Draws the game the server sent. A new wrong guess puts its message
   *  in the bar. A game that ends here ends: the card turns, the page
   *  goes back to its top, where the answer is, and the field lets the
   *  keyboard go. `quiet` takes a game handed back by a refusal, or by
   *  Play, which changes what is drawn and reports nothing about it. */
  const adopt = useCallback((next: DailyGame, quiet = false) => {
    const was = gameRef.current;
    gameRef.current = next;
    setGame(next);
    if (next.phase === 'play' && newWrongGuess(was, next)) setMsgId(newestGuess(next)?.id ?? null);
    if (was?.phase === 'play' && next.phase === 'done') {
      // A game that ended in another tab was counted there.
      if (!quiet) capture('daily_finish', { won: next.won, pts: next.pts });
      setEnded(true);
      setSheet(null);
      setMsgId(null);
      setSure(false);
      inputRef.current?.blur();
      setTopAsk((n) => n + 1);
    }
  }, []);

  // Back to the top once the end is drawn, smoothly, or at once with
  // stillness asked for.
  useLayoutEffect(() => {
    if (topAsk) main.current?.scrollTo({ top: 0, behavior: stillNow() ? 'auto' : 'smooth' });
  }, [topAsk]);

  useEffect(() => {
    if (focusAsk) inputRef.current?.focus();
  }, [focusAsk]);

  // ---- the rules, the reader's midnight, "/" ----

  const signal = useRef(rulesSignal);
  useEffect(() => {
    if (rulesSignal === signal.current) return;
    signal.current = rulesSignal;
    setRules(true);
  }, [rulesSignal]);

  // The reader's midnight, when "Next movie in" runs out: this movie is
  // over for them, and the next is theirs to play, so it is asked for at
  // once, whatever is up — the title screen, the result, or a game, which
  // the server would refuse to go on with anyway. Only a game cut short is
  // told why.
  useEffect(
    () =>
      watchMidnight(today.next, offset, () => {
        const text = midnightText(gameRef.current);
        if (text) say(text);
        reload();
      }),
    [today.next, offset, say, reload],
  );

  // "/" reaches for the guess field from anywhere on the page, as it does
  // for the header's search on a map.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
      const g = gameRef.current;
      if (!g || g.phase !== 'play' || rulesRef.current || sheetRef.current) return;
      if (isTyping(document.activeElement)) return;
      e.preventDefault();
      inputRef.current?.focus();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // ---- Play ----

  const touch = screen.touch;
  const play = useCallback(async () => {
    if (gameRef.current || busyRef.current) return;
    busyRef.current = true;
    setStarting(true);
    try {
      const res = await playDaily(today.no, today.player.name);
      capture('daily_play');
      const hide = res.game.phase === 'play' && !stillNow();
      if (hide) {
        openingRef.current = true;
        setOpening(true);
        later(() => {
          openingRef.current = false;
          setOpening(false);
          // A keyboard is there to type with; a touch screen would throw
          // its keyboard up over the names the reader has come to see.
          if (!touch) setFocusAsk((n) => n + 1);
        }, PLAY_HIDE_MS);
      } else if (!touch) {
        setFocusAsk((n) => n + 1);
      }
      adopt(res.game, true);
    } catch (e) {
      if (e instanceof ApiError) {
        const text = refusalText(e.reason, 'play');
        if (text) say(text);
        if (e.reason === 'day') reload();
      } else {
        say(UNREACHABLE);
      }
    } finally {
      busyRef.current = false;
      setStarting(false);
    }
  }, [today.no, today.player.name, touch, later, adopt, say, reload]);

  // ---- moves ----

  // The last move that never got an answer, so sending it again from the
  // same point uses the same key and is never charged twice.
  const unanswered = useRef<{ sig: string; key: string } | null>(null);

  const act = useCallback(
    async (move: DailyMove): Promise<boolean> => {
      const g = gameRef.current;
      if (!g || g.phase !== 'play' || busyRef.current || openingRef.current) return false;
      // What the server would say anyway, said at once.
      const early = earlyRefusal(g, move);
      if (early) {
        say(early);
        return false;
      }
      const sig = moveSig(move, g.seq);
      const key = unanswered.current?.sig === sig ? unanswered.current.key : newKey();
      busyRef.current = true;
      try {
        const res = await sendDailyMove(today.no, move, key, g.seq);
        unanswered.current = null;
        // A name or a fact bought moves the reader on from the last
        // wrong guess's message; anything done takes back a first press
        // of Show the answer.
        if (move.kind === 'next' || move.kind === 'buy') setMsgId(null);
        setSure(false);
        adopt(res.game);
        return true;
      } catch (e) {
        if (!(e instanceof ApiError)) {
          unanswered.current = { sig, key };
          say(UNREACHABLE);
          return false;
        }
        unanswered.current = null;
        if (e.reason === 'stale') {
          const now = staleGame(e.body);
          if (now) adopt(now, true);
          return false;
        }
        const text = refusalText(e.reason, move.kind);
        if (text) say(text);
        if (e.reason === 'day' || e.reason === 'done' || e.reason === 'cookie' || e.reason === 'no-game') reload();
        return false;
      } finally {
        busyRef.current = false;
      }
    },
    [today.no, adopt, say, reload],
  );

  const next = useCallback(() => void act({ kind: 'next' }), [act]);
  const buy = useCallback((fact: DailyFactKind) => void act({ kind: 'buy', fact }), [act]);
  const guess = useCallback((film: string) => act({ kind: 'guess', film }), [act]);
  const overlap = useCallback((person: string) => act({ kind: 'overlap', person }), [act]);

  /** Show the answer takes two presses: the first asks "Sure? Show it"
   *  for REVEAL_CONFIRM_MS, and a second within that gives the game up. */
  const reveal = useCallback(() => {
    window.clearTimeout(sureTimer.current);
    if (!sure) {
      setSure(true);
      sureTimer.current = window.setTimeout(() => setSure(false), REVEAL_CONFIRM_MS);
      return;
    }
    setSure(false);
    void act({ kind: 'reveal' });
  }, [sure, act]);

  const share = useCallback(() => {
    const g = gameRef.current;
    if (!g) return;
    const fail = () => say(TOASTS.copyFailed);
    try {
      navigator.clipboard.writeText(shareText(today.no, g, window.location.origin)).then(() => say(TOASTS.copied), fail);
    } catch {
      fail();
    }
  }, [today.no, say]);

  // ---- drawing ----

  if (!game) {
    return (
      <>
        <DailyTitle today={today} busy={starting} theme={theme} onPlay={() => void play()} />
        {rules && <DailyRules onClose={() => setRules(false)} />}
      </>
    );
  }

  const playing = game.phase === 'play';
  const rows = opening ? openingRows() : castRows(game);
  const delays = ended && !still ? revealDelays(rows) : NO_DELAYS;
  const shownGuess = playing ? guessById(game, msgId) : null;
  const message = shownGuess ? guessMessage(shownGuess, game) : null;
  const guessed = guessedIds(game);

  const parts: Record<PageSection, ReactNode> = {
    top: (
      <TopBlock
        today={today}
        game={game}
        theme={theme}
        msgId={shownGuess?.id ?? null}
        sure={sure}
        opening={opening}
        onChip={setMsgId}
        onReveal={reveal}
        onAgain={today.dev ? again : undefined}
      />
    ),
    cast: (
      <DailyCast
        rows={rows}
        playing={playing && !opening}
        delays={delays}
        codes={codes}
        theme={theme}
        onNext={next}
        onMovies={(p: DailyPerson) => setSheet(p.id)}
      />
    ),
    facts: <DailyFacts game={game} opening={opening} onBuy={buy} />,
    about: game.end ? <DailyAbout end={game.end} /> : null,
    result: (
      <DailyResult
        today={today}
        game={game}
        board={boards.boards.today && boards.boards.today !== 'failed' ? boards.boards.today : null}
        fresh={ended}
        offset={offset}
        codes={codes}
        theme={theme}
        onShare={share}
        onOpenMovie={onOpenMovie}
        onAgain={today.dev ? again : undefined}
      />
    ),
    board: (
      <DailyLeaderboard
        date={today.date}
        tab={boards.tab}
        boards={boards.boards}
        onTab={boards.pick}
        onRetry={boards.retry}
      />
    ),
    castHead: <h2 className="cd-nd-cast-head">{CAST_HEADING}</h2>,
  };

  return (
    <>
      <div className={`cd-nd${land ? ' cd-nd-land' : ''}`}>
        <div ref={main} className="cd-nd-main">
          <div className="cd-nd-col">
            {pageSections(!playing).map((s) => (
              <Fragment key={s}>{parts[s]}</Fragment>
            ))}
          </div>
        </div>
        {playing && (
          <DailyBar
            game={game}
            message={message}
            codes={codes}
            theme={theme}
            phone={screen.phone}
            opening={opening}
            guessed={guessed}
            inputRef={inputRef}
            onNext={next}
            onGuess={guess}
            say={say}
          />
        )}
      </div>
      {sheet && playing && (
        <DailyMoviesSheet
          no={today.no}
          person={sheet}
          game={game}
          theme={theme}
          onOverlap={overlap}
          onGuess={(id) => {
            // A move still on its way (a name the sheet is buying) would
            // have act drop the guess unsaid. The sheet holds Guess it
            // until then; should a press get through, the sheet stays up
            // rather than close on a guess never made.
            if (busyRef.current) return;
            // The sheet goes first, then the guess is made as any other.
            setSheet(null);
            void guess(id);
          }}
          onClose={() => setSheet(null)}
        />
      )}
      {rules && <DailyRules onClose={() => setRules(false)} />}
    </>
  );
}

/** The top of the game page: the hidden card in today's poster colour,
 *  which turns over to the poster at the end, and beside it the game's
 *  name, the heading (the answer's title once it is over), the line, the
 *  wrong guesses as chips, and Show the answer. */
function TopBlock({
  today,
  game,
  theme,
  msgId,
  sure,
  opening,
  onChip,
  onReveal,
  onAgain,
}: {
  today: DailyToday;
  game: DailyGame;
  theme: Theme;
  /** The wrong guess whose message is in the bar: its chip takes its
   *  warmth colour for a ring. */
  msgId: string | null;
  sure: boolean;
  opening: boolean;
  onChip: (id: string) => void;
  onReveal: () => void;
  onAgain?: () => void;
}) {
  const playing = game.phase === 'play';
  const answer = game.end?.answer;
  const chips = triedChips(game);
  const line = pageLine(game);
  return (
    <div className="cd-nd-top">
      <div className="cd-nd-card" aria-hidden="true">
        <div className={`cd-nd-card-in${answer ? ' cd-nd-card-over' : ''}`}>
          <div className="cd-nd-card-front" style={toneStyle(cardColour(today.colour))}>
            <span className="cd-nd-card-q">?</span>
          </div>
          {/* The back is not drawn until the game is over: the page is not
              told the poster before then. */}
          {answer && (
            <div className="cd-nd-card-back" style={{ ['--poster-fill' as string]: posterFallback(answer.title, theme) }}>
              <PosterImage id={answer.id} url={answer.poster} cssPx={104} className="cd-nd-card-poster" eager />
              {!answer.poster && <span className="cd-nd-card-standin">{answer.title}</span>}
            </div>
          )}
        </div>
      </div>
      <div className="cd-nd-beside">
        <span className="cd-nd-kicker">{GAME_NAME}</span>
        <h1 className="cd-nd-heading">{pageHeading(game)}</h1>
        {line && <p className="cd-nd-line">{line}</p>}
        {chips.length > 0 && (
          <div className="cd-nd-tried" role="group" aria-label="Wrong guesses">
            {chips.map((c) =>
              playing ? (
                <button
                  key={c.id}
                  type="button"
                  className={`cd-nd-tried-chip${c.id === msgId ? ' cd-nd-tried-on' : ''}`}
                  style={toneStyle(warmthColour(c.warmth, theme))}
                  aria-label={c.aria}
                  onClick={() => onChip(c.id)}
                >
                  <span className="cd-nd-tried-dot" aria-hidden="true" />
                  <span className="cd-nd-tried-title">{c.title}</span>
                </button>
              ) : (
                <span key={c.id} className="cd-nd-tried-chip" style={toneStyle(warmthColour(c.warmth, theme))}>
                  <span className="cd-nd-tried-dot" aria-hidden="true" />
                  <span className="cd-nd-tried-title">{c.title}</span>
                </span>
              ),
            )}
          </div>
        )}
        {playing && (
          <div className="cd-nd-giveup-row">
            <button type="button" className="cd-nd-giveup" disabled={opening} onClick={onReveal}>
              {sure ? SHOW_ANSWER_SURE : SHOW_ANSWER}
            </button>
            {/* Beside the way out, so starting again never takes giving
                the answer away first. */}
            {onAgain && <AgainButton onAgain={onAgain} />}
          </div>
        )}
      </div>
    </div>
  );
}

/** The facts row while the game is on: each fact for sale, with its price,
 *  or what it says once bought (factItems). One the points do not cover
 *  with one to spare is drawn at half strength and cannot be pressed. */
function DailyFacts({
  game,
  opening,
  onBuy,
}: {
  game: DailyGame;
  opening: boolean;
  onBuy: (fact: DailyFactKind) => void;
}) {
  return (
    <section className="cd-nd-facts" aria-labelledby="cd-nd-facts-head">
      <h2 id="cd-nd-facts-head" className="cd-nd-facts-head">
        {factsHeading(false)}
      </h2>
      <div className="cd-nd-fact-list">
        {factItems(game).map((f) =>
          f.bought ? (
            <span key={f.kind} className="cd-nd-fact-got">
              <span className="cd-nd-fact-label">{f.label}</span>
              <span className="cd-nd-fact-value">{f.value}</span>
            </span>
          ) : (
            <button
              key={f.kind}
              type="button"
              className="cd-nd-fact"
              disabled={!f.can || opening}
              aria-label={f.aria}
              onClick={() => onBuy(f.kind)}
            >
              {f.label}
              <span className="cd-nd-cost">{f.price}</span>
            </button>
          ),
        )}
      </div>
    </section>
  );
}

/** About the movie, once the game is over: every fact, exact. */
function DailyAbout({ end }: { end: NonNullable<DailyGame['end']> }) {
  return (
    <section className="cd-nd-facts" aria-labelledby="cd-nd-about-head">
      <h2 id="cd-nd-about-head" className="cd-nd-facts-head">
        {factsHeading(true)}
      </h2>
      <div className="cd-nd-fact-list">
        {aboutItems(end).map((f) => (
          <span key={f.label} className="cd-nd-fact-got">
            <span className="cd-nd-fact-label">{f.label}</span>
            <span className="cd-nd-fact-value">{f.value}</span>
          </span>
        ))}
      </div>
    </section>
  );
}

/** The header's end on /daily, for the app to put after its wordmark:
 *  the Daily pill, the puzzle's number and day, and "How it works". The
 *  header keeps its own row; this is only what goes in it. */
export function DailyHeaderTail({
  day,
  phone,
  onRules,
}: {
  day: { no: number; date: string } | null;
  phone: boolean;
  onRules: () => void;
}) {
  return (
    <>
      <span className="cd-daily-pill">Daily</span>
      <span className="cd-daily-spacer" />
      {day && <span className="cd-daily-date">{dailyDateText(day, phone)}</span>}
      <button type="button" className="cd-daily-help" aria-label="How it works" onClick={onRules}>
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="9" />
          <path d="M9.6 9.3a2.5 2.5 0 0 1 4.8.9c0 1.7-2.4 2.2-2.4 3.6" />
          <path d="M12 17.2h.01" strokeWidth="2.6" />
        </svg>
      </button>
    </>
  );
}
