import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import type { DailyGame, DailyToday } from './api';
import {
  CLUE_COST,
  DAILY_START,
  FAN_LEFT_MS,
  FAN_MIDDLE_MS,
  FAN_RIGHT_MS,
  FLIP_MAX,
  FLIP_MIN,
  GAME_NAME,
  GLOW_MS,
  INTRO_IN_MS,
  INTRO_PART_MS,
  STEPS_WIDE_QUERY,
  WRONG_BASE,
  WRONG_STEP,
  costSpan,
  dailyDayText,
  fmtN,
  introButton,
  introDelay,
  playedText,
  pointsShare,
  stepColumns,
} from './daily';
import { EASE, animate } from './motion';
import { personColour } from './personColour';
import { useEscape, useFocusTrapped } from './sheet';
import type { Theme } from './theme';

interface IntroProps {
  today: DailyToday;
  game: DailyGame | null;
  /** The name shown, or the frame of it the reroll is spinning through. */
  name: string;
  /** "New name" has been pressed and its name has not landed yet, frames
   *  of the reel or not. Play waits for it: Play sends the name shown,
   *  and the one landing is the one the reader is about to see. */
  spinning: boolean;
  /** The streak to show in the pill; none at zero. */
  streak: number;
  /** The reader's place this week, "1,204th this week" (standingText),
   *  or empty for none. Shown on every visit, the rules included. */
  rank: string;
  /** Opened from "How it works", over a game already begun. */
  rules: boolean;
  /** Play has been pressed and the server has not answered yet. */
  busy: boolean;
  dialogRef: RefObject<HTMLDivElement | null>;
  nameRef: RefObject<HTMLSpanElement | null>;
  onGo: () => void;
  onReroll: () => void;
  /** Closes the rules; never called before Play. */
  onClose: () => void;
  /** The theme as drawn, for the Guess step's person, whose colour is
   *  worked out in script as every person's is. */
  theme: Theme;
}

// ---- how to play ----
//
// Four steps, Look, Spend, Guess and Score, each a name, a little picture
// and a line, so the whole game reads in one look. The pictures are for
// the eye and hidden from a screen reader, which hears the list as four
// steps in order, each its name and its line. Every price in them is
// read from the rules in daily.ts, so a change of price cannot leave the
// title screen saying the old one.

/** The Look step's little board, row by row, five across and three down:
 *  three cards face up, the hidden one dashed in the middle, the rest
 *  blank. */
const LOOK_CARDS = Array.from({ length: 15 }, (_, i) =>
  i === 7 ? 'hidden' : i === 1 || i === 9 || i === 11 ? 'up' : 'blank',
);

/** What the Spend step offers. A director and an actor are one chip,
 *  "Person", which says both prices should they ever part. */
const SPEND_CHIPS = [
  `Card ${costSpan(FLIP_MIN, FLIP_MAX)}`,
  `Person ${costSpan(CLUE_COST.director, CLUE_COST.actor)}`,
  `Genres ${CLUE_COST.genres}`,
  `Year ${CLUE_COST.year}`,
];

/** The Guess step's line: the first three wrong guesses' prices, then
 *  what a wrong guess tells. A wrong guess here always narrows the year
 *  and the rating, so this is the only line there is. */
const GUESS_CAPTION =
  `Wrong guesses cost ${WRONG_BASE}, then ${WRONG_BASE + WRONG_STEP}, ${WRONG_BASE + 2 * WRONG_STEP}… ` +
  'Each shows who it shares and narrows the year and rating.';

/** The person the Guess step's wrong guess shares, drawn in the colour
 *  the first person on a board is (hueSlots gives slot 0 the first of
 *  HUES): his own on the board, as the game would draw him. */
const SHARED_ID = 'nm0000206';
const SHARED_SLOTS = new Map([[SHARED_ID, 0]]);

/** The Score step's example: points kept, over a bar of that share. */
const SAMPLE_SCORE = 640;

/** One step: its number, which the list already says and so is hidden,
 *  its name, its picture and its line. Each is a part of the screen's
 *  entrance (data-in), so the four rise in turn. */
function Step({
  n,
  name,
  pic,
  caption,
  children,
}: {
  n: number;
  name: string;
  /** The picture's own class, beside the one every picture has. */
  pic: string;
  caption: string;
  children: ReactNode;
}) {
  return (
    <li className="cd-daily-step" data-in="1">
      <span className="cd-daily-step-head">
        <span className="cd-daily-step-n" aria-hidden="true">
          {n}
        </span>
        <span className="cd-daily-step-name">{name}</span>
      </span>
      <div className={`cd-daily-step-pic ${pic}`} aria-hidden="true">
        {children}
      </div>
      <span className="cd-daily-step-caption">{caption}</span>
    </li>
  );
}

/** The four steps, four across in a window 760px wide or more and two by
 *  two below it (stepColumns). */
function HowToPlay({ cols, theme }: { cols: 2 | 4; theme: Theme }) {
  return (
    <ol className={`cd-daily-steps${cols === 2 ? ' cd-daily-steps-pairs' : ''}`} aria-label="How to play">
      <Step
        n={1}
        name="Look"
        pic="cd-daily-step-look"
        caption="Every movie here shares an actor or director with the hidden one. Three start face up."
      >
        <span className="cd-daily-step-axis cd-daily-step-axis-year">Year ↓</span>
        <span className="cd-daily-step-axis cd-daily-step-axis-rating">Rating →</span>
        <span className="cd-daily-step-board">
          {LOOK_CARDS.map((kind, i) => (
            <span key={i} className={`cd-daily-step-card cd-daily-step-card-${kind}`}>
              {kind === 'hidden' ? '?' : null}
            </span>
          ))}
        </span>
      </Step>
      <Step n={2} name="Spend" pic="cd-daily-step-spend" caption="Turn over cards and buy clues. Higher-rated cards cost more.">
        <span className="cd-daily-step-points">
          <span className="cd-daily-step-total">{fmtN(DAILY_START)}</span>
          <span className="cd-daily-step-full" />
        </span>
        <span className="cd-daily-step-chips">
          {SPEND_CHIPS.map((c) => (
            <span key={c} className="cd-daily-step-chip">
              {c}
            </span>
          ))}
        </span>
      </Step>
      <Step n={3} name="Guess" pic="cd-daily-step-guess" caption={GUESS_CAPTION}>
        <span className="cd-daily-step-field">
          <span className="cd-daily-step-typed">John Wick</span>
          <span className="cd-daily-step-chip cd-daily-step-chip-down">−{WRONG_BASE}</span>
        </span>
        <span className="cd-daily-step-chips">
          <span className="cd-daily-step-chip cd-daily-step-chip-plain">
            <span
              className="cd-daily-step-dot"
              style={{ ['--tone' as string]: personColour(SHARED_ID, theme, SHARED_SLOTS) }}
            />
            Keanu Reeves
          </span>
          <span className="cd-daily-step-chip cd-daily-step-chip-plain">Older</span>
          <span className="cd-daily-step-chip cd-daily-step-chip-plain">Rated higher</span>
        </span>
      </Step>
      <Step n={4} name="Score" pic="cd-daily-step-score" caption="The points you keep are your score. Time breaks ties.">
        <span className="cd-daily-step-kept">
          <span className="cd-daily-step-kept-n">{fmtN(SAMPLE_SCORE)}</span>
          <span className="cd-daily-step-kept-words">points left</span>
        </span>
        <span className="cd-daily-step-bar">
          <span className="cd-daily-step-bar-fill" style={{ width: `${pointsShare(SAMPLE_SCORE)}%` }} />
        </span>
      </Step>
    </ol>
  );
}

/** Whether the window is wide enough for the steps' four columns, read
 *  now. matchMedia measures the window exactly as the design's 760 does,
 *  fractional widths and all; innerWidth stands in where it is missing. */
function wideNow(): boolean {
  if (typeof window === 'undefined') return true;
  if (typeof window.matchMedia === 'function') return window.matchMedia(STEPS_WIDE_QUERY).matches;
  return stepColumns(window.innerWidth) === 4;
}

/** The steps' columns, kept as the window changes. Watched from script
 *  rather than written as a media query because 760 is not one of the
 *  stylesheet's screen classes, which screen.test.ts holds every query
 *  in grid.css to; nor as a container query, which would measure the
 *  dialog, a scroller that loses its scrollbar's width, and so turn at a
 *  different width from the window's. Without matchMedia it is read
 *  once. */
function useStepColumns(): 2 | 4 {
  const [wide, setWide] = useState(wideNow);
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const mq = window.matchMedia(STEPS_WIDE_QUERY);
    const read = () => setWide(mq.matches);
    read();
    mq.addEventListener('change', read);
    return () => mq.removeEventListener('change', read);
  }, []);
  return wide ? 4 : 2;
}

/** Plays a breathing loop on an element for as long as it is there. */
function useLoop(el: RefObject<Element | null>, frames: Keyframe[], ms: number) {
  useLayoutEffect(() => {
    const a = animate(el.current, frames, {
      duration: ms,
      direction: 'alternate',
      iterations: Infinity,
      easing: 'ease-in-out',
    });
    return () => a?.cancel();
    // The loop belongs to the element, started once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}

/** Escape closes the rules. Only drawn while they are open: before Play
 *  there is nothing behind the intro to go back to. */
function EscapeCloses({ onClose }: { onClose: () => void }) {
  useEscape(onClose);
  return null;
}

/** The elements a Tab can land on inside `box`, in order. */
function tabbable(box: HTMLElement): HTMLElement[] {
  return [
    ...box.querySelectorAll<HTMLElement>(
      'button:not([disabled]), a[href], input:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  ];
}

/** Where Tab goes next inside a dialog with `count` stops, from `at` (-1
 *  for the dialog itself): round to the first after the last, and to the
 *  last before the first, so the focus never leaves it. */
export function nextStop(at: number, count: number, back: boolean): number {
  if (count === 0) return -1;
  if (back) return at <= 0 ? count - 1 : at - 1;
  return at < 0 || at >= count - 1 ? 0 : at + 1;
}

/** The title screen arriving: it fades in, and its parts, each marked
 *  data-in, rise into place one after another in the order they are
 *  drawn (introDelay), each held at its start through its wait. The four
 *  steps are a part each, so they rise in turn, after the goal line and
 *  before the name. Played through motion.ts's animate, which plays
 *  nothing for a reader who has asked for stillness: the screen is then
 *  simply there, all of it at once. */
export function enterIntro(el: Element): void {
  animate(el, [{ opacity: 0 }, { opacity: 1 }], { duration: INTRO_IN_MS, fill: 'backwards' });
  el.querySelectorAll('[data-in]').forEach((part, i) =>
    animate(
      part,
      [
        { opacity: 0, transform: 'translateY(14px)' },
        { opacity: 1, transform: 'none' },
      ],
      { duration: INTRO_PART_MS, delay: introDelay(i), easing: EASE.settle, fill: 'backwards' },
    ),
  );
}

/** The screen before the game, which is also its rules: a fan of blank
 *  cards with the hidden one in the middle, the game's name and what it
 *  asks, how to play it in four steps, who the reader is playing as, and
 *  Play. Opened again from "How it works", it says the same and leads
 *  back to the game.
 *
 *  It is a modal dialog: the focus starts on it and is kept in it, and
 *  goes back where it was when it closes. The map under it shows through,
 *  out of reach. */
export function DailyIntro({
  today,
  game,
  name,
  spinning,
  streak,
  rank,
  rules,
  busy,
  dialogRef,
  nameRef,
  onGo,
  onReroll,
  onClose,
  theme,
}: IntroProps) {
  const fanL = useRef<HTMLSpanElement>(null);
  const fanR = useRef<HTMLSpanElement>(null);
  const fanC = useRef<HTMLSpanElement>(null);
  const glow = useRef<HTMLSpanElement>(null);
  const cols = useStepColumns();
  useFocusTrapped(dialogRef);
  useLoop(fanC, [{ transform: 'translateY(0) rotate(-1.5deg)' }, { transform: 'translateY(-9px) rotate(1.5deg)' }], FAN_MIDDLE_MS);
  useLoop(fanL, [{ rotate: '-13deg' }, { rotate: '-8deg' }], FAN_LEFT_MS);
  useLoop(fanR, [{ rotate: '13deg' }, { rotate: '8deg' }], FAN_RIGHT_MS);
  useLoop(glow, [{ opacity: 0.16, transform: 'scale(0.97)' }, { opacity: 0.5, transform: 'scale(1.03)' }], GLOW_MS);

  useLayoutEffect(() => {
    if (dialogRef.current) enterIntro(dialogRef.current);
  }, [dialogRef]);

  const keepFocus = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Tab' || !dialogRef.current) return;
    const stops = tabbable(dialogRef.current);
    const at = stops.indexOf(document.activeElement as HTMLElement);
    const next = nextStop(at, stops.length, e.shiftKey);
    if (next < 0) return;
    e.preventDefault();
    stops[next].focus();
  };

  const day = dailyDayText(today.date);
  const played = playedText(today.played);
  // Before the game, Play waits for a new name to land: pressed while the
  // reel spins, it would start the game under the old name, and the reel
  // would then land on a new one the server never kept. Over a game it
  // only goes back to it, and sends nothing.
  const held = busy || (!game && spinning);
  return (
    <div
      ref={dialogRef}
      className="cd-daily-intro"
      role="dialog"
      aria-modal="true"
      aria-labelledby="cd-daily-title"
      tabIndex={-1}
      onKeyDown={keepFocus}
    >
      {rules && <EscapeCloses onClose={onClose} />}
      <div className="cd-daily-intro-col">
        <div className="cd-daily-fan" data-in="1" aria-hidden="true">
          <span ref={fanL} className="cd-daily-fan-card cd-daily-fan-left">
            <span className="cd-daily-fan-slot" />
          </span>
          <span ref={fanR} className="cd-daily-fan-card cd-daily-fan-right">
            <span className="cd-daily-fan-slot" />
          </span>
          <span ref={fanC} className="cd-daily-fan-middle">
            <span className="cd-daily-fan-q">?</span>
            <span className="cd-daily-fan-no">No. {today.no}</span>
          </span>
        </div>
        <div className="cd-daily-meta" data-in="1">
          <span className="cd-daily-pill">Cinedikt Daily</span>
          {day && <span className="cd-daily-day">{day}</span>}
          {streak > 0 && (
            <span className="cd-daily-streak">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M12.5 2.5c.6 3.3-1.4 5-2.8 7.1C8.6 11.3 7.5 13 7.5 15.3c0 3.1 2.2 5.7 5 5.7s4.9-2.4 4.9-5.4c0-2-.9-3.6-1.9-4.9-.1 1.3-.6 2.3-1.5 2.9.4-3.4-.9-7.4-1.5-11.1z" />
              </svg>
              {streak}-day streak
            </span>
          )}
          {/* The streak pill's box, with a podium for the flame. */}
          {rank && (
            <span className="cd-daily-streak cd-daily-standing">
              <svg
                width="13"
                height="13"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M3 21h18M4.5 21v-6h5v6M9.5 21V8h5v13M14.5 21v-4h5v4" />
              </svg>
              {rank}
            </span>
          )}
        </div>
        <div className="cd-daily-intro-words" data-in="1">
          <h1 id="cd-daily-title" className="cd-daily-intro-title">
            {GAME_NAME}
          </h1>
          <p className="cd-daily-goal">
            Name today’s hidden movie. Keep as many of your {fmtN(DAILY_START)} points as you can.
          </p>
        </div>
        <HowToPlay cols={cols} theme={theme} />
        <div className="cd-daily-player" data-in="1">
          <span className="cd-daily-you" aria-hidden="true">
            You
          </span>
          <span className="cd-daily-player-text">
            <span className="cd-daily-player-kicker">Playing as</span>
            <span ref={nameRef} className="cd-daily-player-name">
              {name}
            </span>
          </span>
          {/* The name as it lands, said once: the frames of the spin are
              for the eye. Empty while it spins, so the landing is a
              change a screen reader hears. */}
          <span className="cd-sr-live" aria-live="polite">
            {spinning ? '' : `Playing as ${name}`}
          </span>
          <button type="button" className="cd-daily-soft cd-daily-soft-name" onClick={onReroll}>
            <svg
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <rect x="4" y="4" width="16" height="16" rx="3.5" />
              <circle cx="9" cy="9" r="1.1" fill="currentColor" />
              <circle cx="15" cy="9" r="1.1" fill="currentColor" />
              <circle cx="9" cy="15" r="1.1" fill="currentColor" />
              <circle cx="15" cy="15" r="1.1" fill="currentColor" />
            </svg>
            New name
          </button>
        </div>
        <div className="cd-daily-play" data-in="1">
          <span ref={glow} className="cd-daily-play-glow" aria-hidden="true" />
          <button
            type="button"
            className="cd-daily-play-button"
            onClick={onGo}
            disabled={held}
            aria-busy={held || undefined}
          >
            {introButton(today.no, game)}
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.4"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M5 12h14M13 6l6 6-6 6" />
            </svg>
          </button>
        </div>
        {!game && (
          <p className="cd-daily-live" data-in="1">
            {played && (
              <>
                <span className="cd-daily-live-n">{played.count}</span>
                {played.rest}{' '}
              </>
            )}
            The clock starts when you press Play.
          </p>
        )}
      </div>
    </div>
  );
}
