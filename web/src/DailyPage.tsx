import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { capture } from './analytics';
import {
  ApiError,
  fetchDaily,
  playDaily,
  renameDaily,
  sendDailyMove,
  type DailyGame,
  type DailyMove,
  type DailyPlayer,
  type DailyToday,
} from './api';
import { DailyBoard, NO_FX, useBoardView, type BoardFx } from './DailyBoard';
import { DailyIntro } from './DailyIntro';
import { DailyPanel } from './DailyPanel';
import {
  CLUE_COST,
  CONFETTI_AFTER_MS,
  CONFETTI_COLOURS,
  CONFETTI_MS,
  CONFETTI_PIECES,
  FRESH_HOLD_MS,
  INTRO_OUT_EASE,
  INTRO_OUT_KEYFRAMES,
  INTRO_OUT_MS,
  NAME_BOUNCE_MS,
  BOUNCE_EASE,
  PULSE_MS,
  REEL_GIVE_UP_MS,
  REEL_STEPS,
  RESULTS_AFTER_MS,
  RESULTS_FRESH_MS,
  SCORE_SHOWN_MS,
  SHAKE_KEYFRAMES,
  SHAKE_MS,
  SPEND_FLOAT_MS,
  TWINKLE_EASE,
  TWINKLE_EVERY_MS,
  TWINKLE_KEYFRAMES,
  TWINKLE_MS,
  UNREACHABLE,
  boardPayload,
  burstShift,
  clockOffset,
  dailyDateText,
  everyoneNamed,
  facesOf,
  flipCost,
  guessedIds,
  hueSlots,
  introStreak,
  midnightText,
  moveSig,
  newKey,
  newWrongGuess,
  peopleByCard,
  peopleOf,
  reelDelay,
  reelFrame,
  refusalText,
  resumeCards,
  revealedOf,
  scrollTarget,
  shareText,
  staggerDelays,
  staleGame,
  watchMidnight,
} from './daily';
import { initialsFor, layoutGrid, type GridLayout } from './grid';
import { animate, stillNow } from './motion';
import { useScreen } from './screen';
import { isTyping } from './search';
import { useResolvedTheme, type Theme } from './theme';
import { Toast, useToast } from './Toast';
import { useTapGuard } from './tap';

interface Props {
  /** Told the puzzle's number and day once it has loaded, for the
   *  header; null when it could not be loaded or is not ready yet. */
  onDay?: (day: { no: number; date: string } | null) => void;
  /** Bumped by the header's "How it works": each change opens the rules. */
  rulesSignal?: number;
  /** A map is loading over the page (Forward from here, say). The page
   *  steps back behind the progress line until it arrives, as the opening
   *  screen and the About page do. */
  dim?: boolean;
}

type Load =
  | { state: 'loading' }
  | { state: 'ready'; today: DailyToday; offset: number; n: number }
  | { state: 'failed'; reason: string | null };

/** Cinedikt Daily, under the app's header: the board, the panel, the
 *  intro that is also the rules, the toast and the confetti. It asks for
 *  today's puzzle as it opens, again at the reader's midnight, and again
 *  whenever the server says the game it is showing has moved on (a new
 *  day, a game that ended in another tab). */
export function DailyPage({ onDay, rulesSignal = 0, dim = false }: Props) {
  const toast = useToast();
  const [load, setLoad] = useState<Load>({ state: 'loading' });
  const [asked, setAsked] = useState(0);

  useEffect(() => {
    const ctrl = new AbortController();
    fetchDaily(ctrl.signal)
      .then((today) => {
        if (ctrl.signal.aborted) return;
        setLoad({ state: 'ready', today, offset: clockOffset(today.now, Date.now()), n: asked });
      })
      .catch((e: unknown) => {
        if (ctrl.signal.aborted) return;
        setLoad({ state: 'failed', reason: e instanceof ApiError ? e.reason : null });
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

  const { show } = toast;
  const say = useCallback((text: string) => show({ text }), [show]);
  const reload = useCallback(() => setAsked((n) => n + 1), []);

  return (
    <div className={`cd-daily${dim ? ' cd-daily-dim' : ''}`}>
      {load.state === 'ready' ? (
        <DailyGameView
          // A reload starts the game view afresh from what the server
          // says: whatever the old one was showing is what was wrong.
          key={`${load.today.no}:${load.n}`}
          today={load.today}
          offset={load.offset}
          say={say}
          reload={reload}
          rulesSignal={rulesSignal}
        />
      ) : load.state === 'failed' ? (
        <DailyMissing notReady={load.reason === 'not-ready'} onRetry={reload} />
      ) : (
        // The plot stays empty while today's puzzle is on its way, as a
        // map's does.
        <div className="cd-scroller" aria-hidden="true" />
      )}
      <Toast spec={toast.spec} visible={toast.visible} />
    </div>
  );
}

/** Today's puzzle could not be had: not picked yet, or not reachable. */
function DailyMissing({ notReady, onRetry }: { notReady: boolean; onRetry: () => void }) {
  const [title, body] = notReady
    ? ['Today’s map isn’t ready yet', 'Try again in a few minutes.']
    : ['We couldn’t open today’s map', UNREACHABLE];
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

/** Where the map is asked to go: to cards, or to the middle of the plot
 *  behind the intro. `n` makes asking twice for the same place two asks. */
type Aim = { ids: string[]; smooth: boolean; n: number } | { centre: true; n: number };

interface GameProps {
  today: DailyToday;
  /** The server's clock less this one's. */
  offset: number;
  say: (text: string) => void;
  reload: () => void;
  rulesSignal: number;
}

/** The game for one loaded puzzle. Exported for its tests, which draw it
 *  from a fixture; the page draws it once today's puzzle has arrived. */
export function DailyGameView({ today, offset, say, reload, rulesSignal }: GameProps) {
  const screen = useScreen();
  const theme = useResolvedTheme();
  const tap = useTapGuard();
  const scroller = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const answerRef = useRef<HTMLDivElement>(null);
  const nameRef = useRef<HTMLSpanElement>(null);

  const [game, setGame] = useState<DailyGame | null>(today.game);
  const gameRef = useRef(game);
  const [player, setPlayer] = useState<DailyPlayer>(today.player);
  const [reel, setReel] = useState<string | null>(null);
  // "New name" pressed and not landed yet: the ref for what is pressed
  // meanwhile, the state for the intro to draw. Not `reel != null`: with
  // stillness asked for there are no frames, but the name is still on
  // its way.
  const spinning = useRef(false);
  const [rolling, setRolling] = useState(false);
  const [rules, setRules] = useState(false);
  const rulesRef = useRef(rules);
  rulesRef.current = rules;
  const [min, setMin] = useState(false);
  const [resIn, setResIn] = useState(today.game?.phase === 'done');
  const [fx, setFx] = useState<BoardFx>(NO_FX);
  const [pulse, setPulse] = useState<string | null>(null);
  const [delta, setDelta] = useState<{ k: number; n: number } | null>(null);
  const [popPts, setPopPts] = useState<number | null>(null);
  const [burst, setBurst] = useState<{ k: number; x: number; y: number } | null>(null);
  // A solve asking for its confetti, counted, so the wait for it starts
  // from the commit that draws the end rather than from the moment the
  // game arrived: that render, every card turning face up at once, is
  // heavy, and must not eat into the beat.
  const [burstAsk, setBurstAsk] = useState(0);
  const [freshEnd, setFreshEnd] = useState(false);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const [focusAsk, setFocusAsk] = useState(0);
  const [aim, setAim] = useState<Aim>(() =>
    !today.game
      ? { centre: true, n: 0 }
      : today.game.phase === 'done' && today.game.end
        ? { ids: [today.game.end.answer.id], smooth: false, n: 0 }
        : { ids: resumeCards(today, today.game), smooth: false, n: 0 },
  );
  const aims = useRef(0);
  const goTo = useCallback((ids: string[], smooth: boolean) => {
    aims.current += 1;
    setAim({ ids, smooth, n: aims.current });
  }, []);

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
    return () => all.forEach((t) => window.clearTimeout(t));
  }, []);

  // ---- the board ----

  const view = useBoardView(scroller, tap.onScroll);
  const compact = screen.overlay;
  // Laid out again only when the answer arrives: a move changes what the
  // cards say, never where they are.
  const answer = game?.end?.answer;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const laid = useMemo(() => boardPayload(today, game), [today, answer]);
  const layout = useMemo(
    () => layoutGrid(laid.payload, view.width, laid.settings, undefined, compact),
    [laid, view.width, compact],
  );
  const layoutRef = useRef(layout);
  layoutRef.current = layout;
  // What a game would be laid out as at this width, for the end's
  // stagger, which is worked out before the end is drawn.
  const shape = useRef({ width: view.width, compact });
  shape.current = { width: view.width, compact };

  const faces = useMemo(() => facesOf(today, game), [today, game]);
  const people = useMemo(() => peopleByCard(peopleOf(game)), [game]);
  const named = useMemo(() => everyoneNamed(game), [game]);
  const hues = useMemo(() => hueSlots(named), [named]);
  const codes = useMemo(() => initialsFor(named), [named]);
  const cardRating = useMemo(() => new Map(today.cards.map((c) => [c.id, c.rating])), [today]);

  // ---- taking a game the server sent ----

  const shake = useCallback(() => {
    animate(panelRef.current, SHAKE_KEYFRAMES, { duration: SHAKE_MS, easing: 'ease-out' });
  }, []);

  /** The end of a game played here: the answer pops in and the map turns
   *  over from it, a solve rises over it in points and confetti, and a
   *  moment later the result takes the panel. */
  const finish = useCallback(
    (next: DailyGame, quiet: boolean) => {
      const still = stillNow();
      // A game that ended in another tab was counted there.
      if (!quiet) capture('daily_finish', { won: next.won, pts: next.pts, secs: next.secs });
      setFreshEnd(true);
      setMin(false);
      setPopPts(next.won ? next.pts : null);
      later(() => setPopPts(null), SCORE_SHOWN_MS);
      if (next.won && !still) setBurstAsk((n) => n + 1);
      if (next.end) goTo([next.end.answer.id], true);
      later(() => {
        setResIn(true);
        later(() => setFreshEnd(false), RESULTS_FRESH_MS);
      }, still ? 0 : RESULTS_AFTER_MS);
      if (!next.won) shake();
    },
    [goTo, later, shake],
  );

  /** Draws the game the server sent, moving whatever moved: the cards it
   *  turns over turn, one after another, and keep a ring for a moment;
   *  what was spent floats off the total; a wrong guess shakes the panel;
   *  the end ends. `quiet` takes a game handed back by a refusal, or by
   *  Play, which changes what is drawn and reports nothing about it. */
  const adopt = useCallback(
    (next: DailyGame, quiet = false) => {
      const was = gameRef.current;
      gameRef.current = next;
      const before = revealedOf(today, was);
      const added = [...revealedOf(today, next)].filter((id) => !before.has(id));
      const done = next.phase === 'done';
      let ending: GridLayout | null = null;
      if (done) {
        const p = boardPayload(today, next);
        ending = layoutGrid(p.payload, shape.current.width, p.settings, undefined, shape.current.compact);
      }
      const delays = stillNow() || added.length === 0 ? {} : staggerDelays(added, done, ending);
      setGame(next);
      if (added.length) {
        setFx({ fresh: new Set(done ? [] : added), delays });
        later(() => setFx(NO_FX), Math.max(0, ...Object.values(delays)) + FRESH_HOLD_MS);
      }
      if (was && next.pts < was.pts) {
        // Let go once it has floated off, so a panel drawn afresh — after
        // the rules close — does not float it again.
        const k = Date.now();
        setDelta({ k, n: was.pts - next.pts });
        later(() => setDelta((d) => (d?.k === k ? null : d)), SPEND_FLOAT_MS);
      }
      if (was?.phase === 'play' && done) finish(next, quiet);
      else if (done) {
        setResIn(true);
        if (next.end) goTo([next.end.answer.id], false);
      } else if (!quiet && newWrongGuess(was, next)) shake();
    },
    [today, later, finish, goTo, shake],
  );

  // A solve's confetti, CONFETTI_AFTER_MS after the commit that drew the
  // end. It bursts from the answer's card where the card is then, which
  // may be part way through the map's glide to it, and follows the card
  // from there (Confetti's `follow`).
  useLayoutEffect(() => {
    if (!burstAsk) return;
    later(() => {
      const r = answerRef.current?.getBoundingClientRect();
      setBurst({
        k: Date.now(),
        x: r ? r.left + r.width / 2 : window.innerWidth / 2,
        y: r ? r.top + r.height / 2 : window.innerHeight / 3,
      });
    }, CONFETTI_AFTER_MS);
  }, [burstAsk, later]);

  // ---- the intro and the rules ----

  const dialogUp = !game || rules;
  const leaving = useRef(false);
  /** The intro leaving, before `then`: it fades up and away. On a timer
   *  rather than the animation's finish, which a page nobody is painting
   *  never reaches. */
  const leaveIntro = useCallback(
    (then: () => void) => {
      if (leaving.current) return;
      leaving.current = true;
      const a = animate(dialogRef.current, INTRO_OUT_KEYFRAMES, {
        duration: INTRO_OUT_MS,
        easing: INTRO_OUT_EASE,
        fill: 'forwards',
      });
      later(() => {
        leaving.current = false;
        then();
      }, a ? INTRO_OUT_MS : 0);
    },
    [later],
  );

  const openRules = useCallback(() => {
    setRules(true);
    setMin(false);
  }, []);
  const closeRules = useCallback(() => {
    if (rulesRef.current) leaveIntro(() => setRules(false));
  }, [leaveIntro]);

  const signal = useRef(rulesSignal);
  useEffect(() => {
    if (rulesSignal === signal.current) return;
    signal.current = rulesSignal;
    if (gameRef.current) openRules();
  }, [rulesSignal, openRules]);

  const play = useCallback(async () => {
    if (gameRef.current) {
      closeRules();
      return;
    }
    // Play sends the name shown, so it waits for a new one to land (the
    // intro holds the button too).
    if (busyRef.current || spinning.current) return;
    busyRef.current = true;
    setBusy(true);
    try {
      const res = await playDaily(today.no, player.name);
      capture('daily_play');
      setPlayer(res.player);
      leaveIntro(() => {
        adopt(res.game, true);
        if (res.game.phase === 'play') {
          goTo(today.start.map((s) => s.card), true);
          // A keyboard is there to type with; a touch screen would throw
          // its keyboard up over the map the reader has just been shown.
          if (!screen.touch) setFocusAsk((n) => n + 1);
        }
      });
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
      setBusy(false);
    }
  }, [today, player.name, closeRules, leaveIntro, adopt, goTo, screen.touch, say, reload]);

  useEffect(() => {
    if (focusAsk) inputRef.current?.focus();
  }, [focusAsk]);

  // The reader's midnight, when "Next map in" runs out: this map is over
  // for them, and the next is theirs to play, so it is asked for at once,
  // whatever is up — the intro, the result, or a game, which the server
  // would refuse to go on with anyway. Only a game cut short is told why.
  useEffect(
    () =>
      watchMidnight(today.next, offset, () => {
        const text = midnightText(gameRef.current);
        if (text) say(text);
        reload();
      }),
    [today.next, offset, say, reload],
  );

  // "New name": a reel of scrambled letters while the server picks one,
  // landing on it with a bounce. Repeat presses are ignored while it
  // spins, and so is Play (see play). With stillness asked for, it simply
  // changes. The name on the screen goes with the ask, so what lands is
  // never the name it left.
  const reroll = useCallback(() => {
    if (spinning.current) return;
    spinning.current = true;
    setRolling(true);
    const from = player.name;
    let landed: DailyPlayer | null = null;
    let failed = false;
    renameDaily(from).then(
      (p) => {
        landed = p;
      },
      () => {
        failed = true;
      },
    );
    let k = 0;
    let held = 0;
    const settle = (p: DailyPlayer | null) => {
      spinning.current = false;
      setRolling(false);
      setReel(null);
      if (!p) {
        say(UNREACHABLE);
        return;
      }
      setPlayer(p);
      later(
        () =>
          animate(nameRef.current, [{ transform: 'scale(1.14)' }, { transform: 'scale(1)' }], {
            duration: NAME_BOUNCE_MS,
            easing: BOUNCE_EASE,
          }),
        0,
      );
    };
    const step = () => {
      if (failed) return settle(null);
      const still = stillNow();
      if (landed && (still || k >= REEL_STEPS)) return settle(landed);
      // The reel slows into its last steps only once there is a name to
      // land on; until then it keeps spinning at that pace, for as long as
      // an answer might still come.
      if (!landed && k >= REEL_STEPS - 4) {
        held += 1;
        if (held * reelDelay(k) > REEL_GIVE_UP_MS) return settle(null);
        if (!still) setReel(reelFrame(from, null, k, Math.random));
        later(step, reelDelay(k));
        return;
      }
      k += 1;
      const to: DailyPlayer | null = landed;
      if (!still) setReel(reelFrame(from, to ? to.name : null, k, Math.random));
      later(step, still ? 50 : reelDelay(k));
    };
    step();
  }, [player.name, later, say]);

  // While the intro is up, now and then a blank card behind it starts to
  // turn over and thinks better of it. A timer, skipped while the tab is
  // hidden, and never for a reader who has asked for stillness.
  useEffect(() => {
    if (!dialogUp) return;
    const t = window.setInterval(() => {
      const el = scroller.current;
      if (!el || document.hidden || stillNow()) return;
      const flips = el.querySelectorAll<HTMLElement>('[data-flip="0"]');
      const vr = el.getBoundingClientRect();
      for (let k = 0; k < 8 && flips.length; k++) {
        const f = flips[Math.floor(Math.random() * flips.length)];
        const r = f.getBoundingClientRect();
        if (r.bottom < vr.top || r.top > vr.bottom || r.right < vr.left || r.left > vr.right) continue;
        animate(f, TWINKLE_KEYFRAMES, { duration: TWINKLE_MS, easing: TWINKLE_EASE });
        return;
      }
    }, TWINKLE_EVERY_MS);
    return () => window.clearInterval(t);
  }, [dialogUp]);

  // ---- moves ----

  // The last move that never got an answer, so sending it again from the
  // same point uses the same key and is never charged twice.
  const unanswered = useRef<{ sig: string; key: string } | null>(null);

  const act = useCallback(
    async (move: DailyMove): Promise<boolean> => {
      const g = gameRef.current;
      if (!g || g.phase !== 'play' || busyRef.current) return false;
      // What the server would say anyway, said at once.
      if (move.kind === 'flip') {
        const r = cardRating.get(move.card);
        if (r != null && g.pts < flipCost(r)) {
          say(refusalText('points', 'flip'));
          return false;
        }
      } else if (move.kind === 'buy' && g.pts < CLUE_COST[move.clue]) {
        say(refusalText('points', 'buy'));
        return false;
      } else if (move.kind === 'guess' && guessedIds(g).has(move.film)) {
        say(refusalText('known', 'guess'));
        return false;
      }
      const sig = moveSig(move, g.seq);
      const key = unanswered.current?.sig === sig ? unanswered.current.key : newKey();
      busyRef.current = true;
      try {
        const res = await sendDailyMove(today.no, move, key, g.seq);
        unanswered.current = null;
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
        if (e.reason === 'day' || e.reason === 'done' || e.reason === 'cookie' || e.reason === 'no-game') {
          reload();
        }
        return false;
      } finally {
        busyRef.current = false;
      }
    },
    [today.no, cardRating, adopt, say, reload],
  );

  const flip = useCallback(
    (card: string, pointer: boolean) => {
      // A flick that ends on a card was a flick, not a choice to pay for it.
      if (pointer && !tap.allows()) return;
      void act({ kind: 'flip', card });
    },
    [act, tap],
  );

  const find = useCallback(
    (card: string) => {
      goTo([card], true);
      setPulse(card);
      later(() => setPulse((p) => (p === card ? null : p)), PULSE_MS);
    },
    [goTo, later],
  );

  const copy = useCallback(() => {
    const g = gameRef.current;
    if (!g) return;
    const fail = () => say('Couldn’t copy the result');
    try {
      navigator.clipboard
        .writeText(shareText(today.no, g, window.location.origin))
        .then(() => say('Result copied'), fail);
    } catch {
      fail();
    }
  }, [today.no, say]);

  // "/" reaches for the guess field from anywhere on the page, as it does
  // for the header's search on a map.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
      const g = gameRef.current;
      if (!g || g.phase !== 'play' || rulesRef.current || isTyping(document.activeElement)) return;
      e.preventDefault();
      inputRef.current?.focus();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // ---- scrolling ----

  // Read when it is asked for rather than watched, so a later change of
  // width does not send the map back to where it was last asked to go.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    if ('centre' in aim) {
      el.scrollTo({ left: 0, top: Math.max(0, (el.scrollHeight - el.clientHeight) / 2) });
      return;
    }
    const l = layoutRef.current;
    const at = new Map(l.cards.map((c) => [c.film.id, c]));
    const cards = aim.ids.flatMap((id) => at.get(id) ?? []);
    const panel = panelRef.current;
    const w = el.clientWidth - (!screen.phone && panel ? panel.offsetWidth + 32 : 0);
    const h = el.clientHeight - (screen.phone && panel ? panel.offsetHeight + 8 : 0);
    const to = scrollTarget(cards, { w: l.metrics.cardW, h: l.metrics.cardH }, { w, h });
    if (to) el.scrollTo({ ...to, behavior: aim.smooth && !stillNow() ? 'smooth' : 'auto' });
    // Only a new aim moves the map.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [aim]);

  // ---- drawing ----

  const results = !!game && game.phase === 'done' && resIn;
  return (
    <>
      <div
        ref={scroller}
        className="cd-scroller cd-daily-map"
        role="region"
        aria-label="Today’s map"
        aria-hidden={dialogUp || undefined}
        inert={dialogUp || undefined}
        onPointerDown={tap.onPointerDown}
        onPointerMove={tap.onPointerMove}
      >
        <DailyBoard
          today={today}
          game={game}
          layout={layout}
          faces={faces}
          people={people}
          hues={hues}
          codes={codes}
          phone={screen.phone}
          scrollTop={view.scrollTop}
          viewH={view.height}
          fx={fx}
          pulse={pulse}
          popAnswer={freshEnd}
          popPts={popPts}
          answerRef={answerRef}
          theme={theme}
          onFlip={flip}
        />
      </div>
      {game && !dialogUp && (
        <DailyPanel
          today={today}
          game={game}
          results={results}
          min={min}
          onMin={() => setMin((m) => !m)}
          delta={delta}
          offset={offset}
          touch={screen.touch}
          theme={theme}
          hues={hues}
          codes={codes}
          panelRef={panelRef}
          inputRef={inputRef}
          fresh={freshEnd}
          player={player.name}
          onClue={(clue) => void act({ kind: 'buy', clue })}
          onGuess={(film) => act({ kind: 'guess', film })}
          onReveal={() => void act({ kind: 'reveal' })}
          onFind={find}
          onFindAnswer={() => {
            if (game.end) goTo([game.end.answer.id], true);
          }}
          onCopy={copy}
          say={say}
        />
      )}
      {dialogUp && (
        <DailyIntro
          today={today}
          game={game}
          name={reel ?? player.name}
          spinning={rolling}
          streak={introStreak(today.streak, game)}
          rules={rules}
          busy={busy}
          dialogRef={dialogRef}
          nameRef={nameRef}
          onGo={() => void play()}
          onReroll={reroll}
          onClose={closeRules}
        />
      )}
      {burst && (
        <Confetti
          key={burst.k}
          x={burst.x}
          y={burst.y}
          follow={answerRef}
          theme={theme}
          onDone={() => setBurst((b) => (b?.k === burst.k ? null : b))}
        />
      )}
    </>
  );
}

/** A burst of confetti from the solved movie, on a canvas over everything
 *  for a couple of seconds. Drawn a frame at a time, as only a painted
 *  page can show it; taken away by a timer, which a hidden one still
 *  runs. Never mounted for a reader who has asked for stillness.
 *
 *  It bursts from (x, y), the card's centre on the screen as it began,
 *  and stays on the card, `follow`, after that: the canvas is fixed to
 *  the screen and the card is on the map, which may still be gliding to
 *  it, so each frame is drawn moved by however far the card has gone
 *  since (burstShift). */
function Confetti({
  x,
  y,
  follow,
  theme,
  onDone,
}: {
  x: number;
  y: number;
  follow: RefObject<HTMLElement | null>;
  theme: Theme;
  onDone: () => void;
}) {
  const ref = useRef<HTMLCanvasElement>(null);
  const done = useRef(onDone);
  done.current = onDone;
  useEffect(() => {
    const cv = ref.current;
    const ctx = cv?.getContext('2d');
    const gone = window.setTimeout(() => done.current(), CONFETTI_MS + 100);
    if (!cv || !ctx) return () => window.clearTimeout(gone);
    const W = window.innerWidth;
    const H = window.innerHeight;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    cv.width = W * dpr;
    cv.height = H * dpr;
    ctx.scale(dpr, dpr);
    const colours = CONFETTI_COLOURS[theme];
    const bits = Array.from({ length: CONFETTI_PIECES }, (_, i) => {
      const a = -Math.PI / 2 + (Math.random() - 0.5) * 2.2;
      const v = 7 + Math.random() * 10;
      return {
        x,
        y,
        vx: Math.cos(a) * v,
        vy: Math.sin(a) * v,
        w: 5 + Math.random() * 6,
        h: 3 + Math.random() * 5,
        r: Math.random() * 6.28,
        vr: (Math.random() - 0.5) * 0.45,
        c: colours[i % colours.length],
      };
    });
    let frame = 0;
    const t0 = performance.now();
    const draw = (t: number) => {
      const e = t - t0;
      ctx.clearRect(0, 0, W, H);
      ctx.globalAlpha = Math.max(0, Math.min(1, (CONFETTI_MS - e) / 700));
      // The pieces fly where they burst; the card carries them.
      const at = burstShift({ x, y }, follow.current?.getBoundingClientRect() ?? null);
      ctx.save();
      ctx.translate(at.dx, at.dy);
      for (const b of bits) {
        b.vy += 0.34;
        b.vx *= 0.986;
        b.vy *= 0.986;
        b.x += b.vx;
        b.y += b.vy;
        b.r += b.vr;
        ctx.save();
        ctx.translate(b.x, b.y);
        ctx.rotate(b.r);
        ctx.fillStyle = b.c;
        ctx.fillRect(-b.w / 2, -b.h / 2, b.w, b.h * Math.abs(Math.cos(b.r * 2)));
        ctx.restore();
      }
      ctx.restore();
      if (e < CONFETTI_MS) frame = requestAnimationFrame(draw);
    };
    frame = requestAnimationFrame(draw);
    return () => {
      cancelAnimationFrame(frame);
      window.clearTimeout(gone);
    };
    // One burst per mount, from where it was asked for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return <canvas ref={ref} className="cd-daily-confetti" aria-hidden="true" />;
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
