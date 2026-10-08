import { useLayoutEffect, useRef, type KeyboardEvent, type RefObject } from 'react';
import type { DailyGame, DailyToday } from './api';
import {
  CLUE_COST,
  DAILY_START,
  FAN_LEFT_MS,
  FAN_MIDDLE_MS,
  FAN_RIGHT_MS,
  GLOW_MS,
  INTRO_IN_MS,
  INTRO_PART_MS,
  WRONG_BASE,
  WRONG_STEP,
  dailyDayText,
  fmtN,
  introButton,
  introDelay,
  playedText,
} from './daily';
import { EASE, animate } from './motion';
import { useEscape, useFocusTrapped } from './sheet';

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
}

/** The six prices, as the intro lists them. */
const COSTS: { label: string; tag: string; down?: boolean }[] = [
  { label: 'Turn over a card', tag: '20–80' },
  { label: 'See a director', tag: String(CLUE_COST.director) },
  { label: 'See an actor', tag: String(CLUE_COST.actor) },
  { label: 'See its genres', tag: String(CLUE_COST.genres) },
  { label: 'See its year', tag: String(CLUE_COST.year) },
  { label: 'A wrong guess', tag: `${WRONG_BASE}+`, down: true },
];

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

/** The screen before the game, which is also its rules: a fan of blank
 *  cards with the hidden one in the middle, what everything costs, who
 *  the reader is playing as, and Play. Opened again from "How it works",
 *  it says the same and leads back to the game.
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
}: IntroProps) {
  const fanL = useRef<HTMLSpanElement>(null);
  const fanR = useRef<HTMLSpanElement>(null);
  const fanC = useRef<HTMLSpanElement>(null);
  const glow = useRef<HTMLSpanElement>(null);
  useFocusTrapped(dialogRef);
  useLoop(fanC, [{ transform: 'translateY(0) rotate(-1.5deg)' }, { transform: 'translateY(-9px) rotate(1.5deg)' }], FAN_MIDDLE_MS);
  useLoop(fanL, [{ rotate: '-13deg' }, { rotate: '-8deg' }], FAN_LEFT_MS);
  useLoop(fanR, [{ rotate: '13deg' }, { rotate: '8deg' }], FAN_RIGHT_MS);
  useLoop(glow, [{ opacity: 0.16, transform: 'scale(0.97)' }, { opacity: 0.5, transform: 'scale(1.03)' }], GLOW_MS);

  // Arriving: the screen fades in and its parts rise into place one
  // after another, each held at the start through its wait.
  useLayoutEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
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
            Whose map is it?
          </h1>
          <p className="cd-daily-lead">
            One hidden movie. Every movie on its map shares an actor or director with it. Find it with as
            many of your {fmtN(DAILY_START)} points left as you can.
          </p>
        </div>
        <ol className="cd-daily-costs" data-in="1" aria-label="What things cost">
          {COSTS.map((c) => (
            <li key={c.label} className="cd-daily-cost-item">
              <span className="cd-daily-cost-label">{c.label}</span>
              <span className={`cd-daily-tag${c.down ? ' cd-daily-tag-down' : ''}`}>{c.tag}</span>
            </li>
          ))}
        </ol>
        <p className="cd-daily-rules" data-in="1">
          You start with three movies showing. Cards further right are higher rated, so they cost more. A
          wrong guess tells you who it shares with the hidden movie, and whether it’s older or newer and
          rated higher or lower. Each wrong guess costs {WRONG_STEP} more than the last. Your score is the
          points you have left.
        </p>
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
