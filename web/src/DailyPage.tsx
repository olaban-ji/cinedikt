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
import { DailySheetAsk } from './DailySheetAsk';
import { DailyTitle } from './DailyTitle';
import {
  AGAIN_FAILED,
  CARD_BOB,
  CAST_HEADING,
  FACTS_NOTE,
  GAME_NAME,
  NEXT_IN_VIEW_MS,
  NUDGE_NOTE,
  PLAY_HIDE_MS,
  RESULT_AFTER_MS,
  REVEAL_CONFIRM_MS,
  SHOW_ANSWER,
  SHOW_ANSWER_SURE,
  TOASTS,
  UNREACHABLE,
  aboutItems,
  cardColour,
  cardGlow,
  cardRipple,
  castRows,
  choiceOvertaken,
  clockOffset,
  codesOf,
  dailyDateText,
  earlyRefusal,
  factItems,
  factNudge,
  factsHeading,
  guessById,
  guessMessage,
  guessedIds,
  hueColour,
  midnightText,
  moveSig,
  moviesPress,
  newKey,
  newWrongGuess,
  newestGuess,
  nextInView,
  nudgePulse,
  openingRows,
  pageHeading,
  pageLine,
  pageSections,
  refusalText,
  resultScroll,
  revealDelays,
  rippleSlot,
  shareText,
  sheetAfter,
  shownAsk,
  shownSheet,
  staleGame,
  todaysPeople,
  toneStyle,
  triedChips,
  viewportFit,
  warmthColour,
  watchMidnight,
  type PageSection,
} from './daily';
import { useLiveScreen } from './dailyScreen';
import { animate, stillNow } from './motion';
import { listenWheelSideways } from './PeopleChips';
import { PosterImage } from './PosterImage';
import { posterFallback } from './poster';
import { useScreen } from './screen';
import { isTyping } from './search';
import { useReducedMotion, useResolvedTheme, type Theme } from './theme';
import { Toast, useToast } from './Toast';

// Cinedikt Daily's page, under the app's header, and its game, Name Drop:
// the title screen, then the game page — the hidden card and the wrong
// guesses, the facts for sale right under it, the cast showing up one name
// at a time, and the guess bar pinned under them — and, once it is over,
// the answer, the result and the leaderboard, with the cast below. The
// server owns the game: every move is sent, and the page draws the game it
// sends back. The page's size is read live (dailyScreen.ts): the phone's
// four results and the landscape column follow the window as it is now.

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
  const live = useLiveScreen();
  const theme = useResolvedTheme();
  const still = useReducedMotion();
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
  // The Movies sheet, open on this person, by IMDb id: only ever the one
  // whose map the reader chose, the game's `sheet`.
  const [sheet, setSheet] = useState<string | null>(null);
  const sheetRef = useRef(sheet);
  sheetRef.current = sheet;
  // The name whose Movies button asked to choose the game's one map,
  // while the question is up (DailySheetAsk).
  const [asking, setAsking] = useState<DailyPerson | null>(null);
  const askingRef = useRef(asking);
  askingRef.current = asking;
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

  // ---- moving the page ----

  /** The result, brought up to RESULT_GAP_PX under the page's top: smoothly,
   *  or in one jump with stillness asked for. */
  const toResult = useCallback(() => {
    const m = main.current;
    const res = m?.querySelector('.cd-nd-res');
    if (!m || !res) return;
    const top = resultScroll(m.scrollTop, res.getBoundingClientRect().top, m.getBoundingClientRect().top);
    m.scrollTo({ top, behavior: stillNow() ? 'auto' : 'smooth' });
  }, []);

  /** The next row, the only way to the next name, brought into sight over
   *  the guess bar should a name that has just appeared have pushed it
   *  under; failing a next row, the foot of the cast. Nothing moves when
   *  it is in sight already. */
  const keepNextInView = useCallback(() => {
    const m = main.current;
    const row = m?.querySelector('.cd-nd-row-next') ?? m?.querySelector('.cd-nd-cast');
    if (!m || !row) return;
    const by = nextInView(row.getBoundingClientRect().bottom, m.getBoundingClientRect().bottom);
    if (by > 0) m.scrollTo({ top: m.scrollTop + by, behavior: stillNow() ? 'auto' : 'smooth' });
  }, []);

  // ---- taking a game the server sent ----

  /** Draws the game the server sent. A new wrong guess puts its message
   *  in the bar, and a new name, once it has settled in, has the page
   *  keep the next row in sight. A game that ends here ends: the card
   *  turns, the page goes back to its top, where the answer is, and the
   *  field lets the keyboard go; RESULT_AFTER_MS later the page moves on
   *  down to the result. `quiet` takes a game handed back by a refusal,
   *  or by Play, which changes what is drawn and reports nothing about
   *  it. */
  const adopt = useCallback(
    (next: DailyGame, quiet = false) => {
      const was = gameRef.current;
      gameRef.current = next;
      setGame(next);
      if (next.phase === 'play' && newWrongGuess(was, next)) setMsgId(newestGuess(next)?.id ?? null);
      if (rippleSlot(was, next) != null) later(keepNextInView, NEXT_IN_VIEW_MS);
      if (was?.phase === 'play' && next.phase === 'done') {
        // A game that ended in another tab was counted there.
        if (!quiet) capture('daily_finish', { won: next.won, pts: next.pts });
        setEnded(true);
        setSheet(null);
        setAsking(null);
        setMsgId(null);
        setSure(false);
        inputRef.current?.blur();
        setTopAsk((n) => n + 1);
        later(toResult, RESULT_AFTER_MS);
      }
      // A map chosen, here or in another tab, answers any question still
      // up about choosing one (shownAsk).
      if (next.sheet) setAsking(null);
    },
    [later, keepNextInView, toResult],
  );

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
      if (!g || g.phase !== 'play' || rulesRef.current || sheetRef.current || shownAsk(g, askingRef.current)) return;
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

  /** Catches up with the game as the server has it, for a refusal that
   *  says the page is behind without saying where: the game the refusal
   *  handed back, if it did, and otherwise today's, asked for afresh and
   *  drawn quietly. Null when it cannot be had, or when today's puzzle is
   *  no longer this one, which the reader's midnight is already on its
   *  way to sort out. */
  const catchUp = useCallback(
    async (body: unknown): Promise<DailyGame | null> => {
      let now = staleGame(body);
      if (!now) {
        try {
          const fresh = await fetchDaily();
          if (fresh.no === today.no) now = fresh.game;
        } catch {
          // Nothing to catch up with: the caller says so.
        }
      }
      if (now) adopt(now, true);
      return now;
    },
    [today.no, adopt],
  );

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
        // A Movies map chosen already: in another tab, or by a press that
        // crossed this one. The page catches up, still holding the game
        // so nothing else is sent from where it was, and the map chosen
        // then opens (chooseSheet); the words are for when there is none
        // to open (sheetAfter).
        if (move.kind === 'sheet' && e.reason === 'known') {
          if (sheetAfter(await catchUp(e.body)).say) say(refusalText(e.reason, move.kind));
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
    [today.no, adopt, catchUp, say, reload],
  );

  const next = useCallback(() => void act({ kind: 'next' }), [act]);
  const buy = useCallback((fact: DailyFactKind) => void act({ kind: 'buy', fact }), [act]);
  const guess = useCallback((film: string) => act({ kind: 'guess', film }), [act]);

  // ---- the one Movies map ----

  /** A Movies button pressed, read against the game as it stands now
   *  rather than as the row was drawn (moviesPress): the map chosen
   *  opens straight away; before one is chosen the page asks first
   *  (DailySheetAsk), though never while a move is on its way, the choice
   *  among them, whose map would land under the question; a shut one does
   *  nothing, and cannot be pressed. */
  const movies = useCallback((p: DailyPerson) => {
    const g = gameRef.current;
    if (!g) return;
    const press = moviesPress(g, p, busyRef.current);
    if (press === 'open') setSheet(p.id);
    else if (press === 'ask') setAsking(p);
  }, []);

  /** Open the map, answered: the question goes, the choice is sent as a
   *  move like any other, under its key from where the game stands, so a
   *  second press, or a retry after an answer that never came, is the
   *  same request and never a second map. Then the map chosen opens,
   *  whoever's it is: usually `p`'s, but a choice made first in another
   *  tab, which a refusal or a game handed back brings, is the one that
   *  holds. Nothing opens when nothing was chosen, and the refusal has
   *  said why.
   *
   *  With a move still on its way, which act would drop unsent, the
   *  question stays up, to be answered again once it has landed, rather
   *  than go as though the map were chosen. A choice refused as stale,
   *  the game having moved on in another tab without a map being chosen
   *  there, still stands: the reader has answered, so it is sent once
   *  more from where the game now is; overtaken again, the question goes
   *  back up, since a stale refusal says nothing (choiceOvertaken). */
  const chooseSheet = useCallback(
    async (p: DailyPerson) => {
      if (busyRef.current) return;
      setAsking(null);
      const from = gameRef.current;
      if (from && !from.sheet && !(await act({ kind: 'sheet', person: p.id }))) {
        const now = gameRef.current;
        if (now && choiceOvertaken(from, now) && !(await act({ kind: 'sheet', person: p.id }))) {
          if (choiceOvertaken(now, gameRef.current)) {
            setAsking(p);
            return;
          }
        }
      }
      const { open } = sheetAfter(gameRef.current);
      if (open) setSheet(open);
    },
    [act],
  );

  /** The server would not show the map that is open ("sheet"): the page
   *  had it wrong, as it can after a game was played on elsewhere. It
   *  catches up and opens the map that was chosen, unless it is this one
   *  again or there is none, when the sheet goes and the reader is told
   *  the rule (sheetAfter). Nothing happens if the sheet was closed
   *  meanwhile. */
  const sheetRefused = useCallback(async () => {
    const refused = sheetRef.current;
    const now = await catchUp(null);
    if (sheetRef.current !== refused) return;
    const after = sheetAfter(now, refused);
    setSheet(after.open);
    if (after.say) say(refusalText('sheet', 'movies'));
  }, [catchUp, say]);

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
  const sheetOn = shownSheet(game, sheet);
  const askOn = shownAsk(game, asking);
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
        onMovies={movies}
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
      <div className={`cd-nd${live.land ? ' cd-nd-land' : ''}`}>
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
            phone={live.phone}
            opening={opening}
            guessed={guessed}
            inputRef={inputRef}
            onGuess={guess}
            say={say}
          />
        )}
      </div>
      {sheetOn && (
        <DailyMoviesSheet
          no={today.no}
          person={sheetOn}
          game={game}
          theme={theme}
          onGuess={(id) => {
            // A move still on its way would have act drop the guess
            // unsaid: the sheet stays up rather than close on a guess
            // never made.
            if (busyRef.current) return;
            // The sheet goes first, then the guess is made as any other.
            setSheet(null);
            void guess(id);
          }}
          onClose={() => setSheet(null)}
          onNotChosen={() => void sheetRefused()}
        />
      )}
      {askOn && <DailySheetAsk name={askOn.name} onYes={() => void chooseSheet(askOn)} onNo={() => setAsking(null)} />}
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
  const chips = triedChips(game);
  const line = pageLine(game);
  return (
    <div className="cd-nd-top">
      <HiddenCard colour={today.colour} game={game} theme={theme} />
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

/** The hidden card: today's poster colour with a "?", which turns over to
 *  the poster at the end. Its back is not drawn until then, and the page
 *  holds nothing of the poster before: it is not told it.
 *
 *  While the game is on it is alive: its glow breathes in the lightened
 *  poster colour (cardGlow), its "?" bobs (CARD_BOB), and each name that
 *  appears sends a ripple of that person's colour out of it (rippleSlot,
 *  cardRipple), so the eye goes from the card to whoever has just joined.
 *  The loops start once, as the game starts or the page opens on one, and
 *  stop at the end; nothing moves for a reader who has asked for
 *  stillness, and the card simply sits there. */
function HiddenCard({ colour, game, theme }: { colour: string; game: DailyGame; theme: Theme }) {
  const card = useRef<HTMLDivElement>(null);
  const q = useRef<HTMLSpanElement>(null);
  const still = useReducedMotion();
  const playing = game.phase === 'play';
  const answer = game.end?.answer;

  useEffect(() => {
    if (!playing || still) return;
    const glow = cardGlow(colour);
    const loops = [
      glow ? animate(card.current, glow.keyframes, glow.options) : null,
      animate(q.current, CARD_BOB.keyframes, CARD_BOB.options),
    ];
    return () => loops.forEach((a) => a?.cancel());
  }, [playing, still, colour]);

  // The game as last drawn, so a name that has just appeared is told from
  // one that was there: none ripples as the page opens.
  const was = useRef(game);
  useEffect(() => {
    const k = rippleSlot(was.current, game);
    was.current = game;
    const slot = k == null ? null : game.slots[k];
    if (!slot?.shown) return;
    const ripple = cardRipple(hueColour(slot.person.hue, theme));
    animate(card.current, ripple.keyframes, ripple.options);
  }, [game, theme]);

  return (
    <div ref={card} className="cd-nd-card" aria-hidden="true">
      <div className={`cd-nd-card-in${answer ? ' cd-nd-card-over' : ''}`}>
        <div className="cd-nd-card-front" style={toneStyle(cardColour(colour))}>
          <span ref={q} className="cd-nd-card-q">
            ?
          </span>
        </div>
        {answer && (
          <div className="cd-nd-card-back" style={{ ['--poster-fill' as string]: posterFallback(answer.title, theme) }}>
            <PosterImage id={answer.id} url={answer.poster} cssPx={104} className="cd-nd-card-poster" eager />
            {!answer.poster && <span className="cd-nd-card-standin">{answer.title}</span>}
          </div>
        )}
      </div>
    </div>
  );
}

/** The facts panel, right under the card while the game is on, where it
 *  cannot be missed: "Buy a fact" with its note beside it, over one row
 *  of the facts, each for sale with its price, or what it says once
 *  bought (factItems). The row scrolls sideways, its scrollbar hidden, so
 *  a mouse wheel moves it too (FactsPanel). One the points do not cover
 *  with one to spare is drawn at half strength and cannot be pressed.
 *
 *  Once three names are showing and nothing has been bought (factNudge),
 *  the panel's ring turns the accent and its note asks, and the first
 *  time it does, it pulses three times (nudgePulse), its edge in the
 *  accent the page resolves --acc to now, in whichever theme. */
function DailyFacts({
  game,
  opening,
  onBuy,
}: {
  game: DailyGame;
  opening: boolean;
  onBuy: (fact: DailyFactKind) => void;
}) {
  const panel = useRef<HTMLElement>(null);
  const nudge = factNudge(game);
  const pulsed = useRef(false);
  useEffect(() => {
    if (!nudge || pulsed.current) return;
    pulsed.current = true;
    const el = panel.current;
    const pulse = nudgePulse(el ? getComputedStyle(el).getPropertyValue('--acc').trim() : '');
    animate(el, pulse.keyframes, pulse.options);
  }, [nudge]);
  return (
    <FactsPanel
      panelRef={panel}
      id="cd-nd-facts-head"
      heading={factsHeading(false)}
      note={nudge ? NUDGE_NOTE : FACTS_NOTE}
      nudge={nudge}
    >
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
    </FactsPanel>
  );
}

/** About the movie, once the game is over, in the same panel: every
 *  fact, exact. */
function DailyAbout({ end }: { end: NonNullable<DailyGame['end']> }) {
  return (
    <FactsPanel id="cd-nd-about-head" heading={factsHeading(true)}>
      {aboutItems(end).map((f) => (
        <span key={f.label} className="cd-nd-fact-got">
          <span className="cd-nd-fact-label">{f.label}</span>
          <span className="cd-nd-fact-value">{f.value}</span>
        </span>
      ))}
    </FactsPanel>
  );
}

/** The panel the facts sit in: a head row, the heading and a note beside
 *  it, over one row of chips that scrolls sideways. The note is a polite
 *  live region, so the nudge is heard as well as seen.
 *
 *  A mouse wheel over the row moves the row (listenWheelSideways), heard
 *  on the row itself rather than through onWheel: React hears every wheel
 *  passively, at its root, so the page here would scroll first and carry
 *  the row out from under the pointer, and the chips past the edge could
 *  only be reached with Shift or Tab. */
function FactsPanel({
  id,
  heading,
  note,
  nudge = false,
  panelRef,
  children,
}: {
  id: string;
  heading: string;
  note?: string;
  nudge?: boolean;
  panelRef?: RefObject<HTMLElement | null>;
  children: ReactNode;
}) {
  const list = useRef<HTMLDivElement>(null);
  useEffect(() => listenWheelSideways(list.current), []);
  return (
    <section ref={panelRef} className={`cd-nd-facts${nudge ? ' cd-nd-facts-nudge' : ''}`} aria-labelledby={id}>
      <div className="cd-nd-facts-top">
        <h2 id={id} className="cd-nd-facts-head">
          {heading}
        </h2>
        {note != null && (
          <p className="cd-nd-facts-note" aria-live="polite">
            {note}
          </p>
        )}
      </div>
      <div ref={list} className="cd-nd-fact-list">
        {children}
      </div>
    </section>
  );
}

/** The header's end on /daily, for the app to put after its wordmark:
 *  the Daily pill, the puzzle's number and day ("No. 143" alone on a
 *  phone), and "How it works". The header keeps its own row; this is only
 *  what goes in it. Whether it is on a phone is read from the window as it
 *  is now (useLiveScreen), whatever the app last measured. */
export function DailyHeaderTail({
  day,
  onRules,
}: {
  day: { no: number; date: string } | null;
  /** The app's own reading of the screen. Not read: the tail reads the
   *  window itself, live, so the date follows a phone turned or a window
   *  dragged narrow. */
  phone?: boolean;
  onRules: () => void;
}) {
  const { phone } = useLiveScreen();
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
