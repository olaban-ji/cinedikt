import { useLayoutEffect, useRef, type CSSProperties, type RefObject } from 'react';
import type { DailyToday } from './api';
import {
  TITLE_GLOW_ALPHA,
  TITLE_HUES,
  TITLE_LOOPS,
  TITLE_SCREEN,
  cardColour,
  dailyDayText,
  hueColour,
  playedText,
  posterGlow,
  titleRise,
  toneStyle,
  type TitleLoop,
} from './daily';
import { animate } from './motion';
import { useReducedMotion, type Theme } from './theme';

/** Name Drop's title screen, before Play: the game in one picture — the
 *  hidden card in today's poster colour, glowing, beside three rows of a
 *  cast list, one shown, one next, one hidden — then the pill and the day,
 *  the name, what it asks, three steps, the fine print, Play, and how many
 *  have played. Every number in its words is the rules' (TITLE_SCREEN). The
 *  picture is for the eye, and hidden from a screen reader, which hears
 *  the steps as a list.
 *
 *  It moves to welcome the reader in: each part rises in turn, the card
 *  floats and turns with a shine crossing it and its "?" bobbing, filled
 *  rows drop into the empty ones as the game's names do, the next row
 *  pulses, and a shine crosses Play. All of it is daily.ts's (titleRise,
 *  TITLE_LOOPS), played through the Web Animations API on the parts this
 *  marks (data-rise, data-anim), started once as the screen is put down:
 *  pressing Play redraws the screen and restarts nothing. None of it runs
 *  for a reader who has asked for stillness, who sees the screen at rest
 *  as the stylesheet draws it. */
export function DailyTitle({
  today,
  busy,
  theme,
  onPlay,
}: {
  today: DailyToday;
  /** Play has been pressed and the server has not answered yet. */
  busy: boolean;
  theme: Theme;
  onPlay: () => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const still = useReducedMotion();
  useTitleMotion(box, still);
  const day = dailyDayText(today.date);
  const played = playedText(today.played);
  const glow = posterGlow(today.colour, TITLE_GLOW_ALPHA);
  const card: CSSProperties = { ...toneStyle(cardColour(today.colour)), ...(glow ? { ['--glow' as string]: glow } : {}) };
  const [shownHue, nextHue, hiddenHue] = TITLE_HUES;
  return (
    <div className="cd-nd-title">
      <div ref={box} className="cd-nd-title-col">
        <div className="cd-nd-title-pic" aria-hidden="true" data-rise="">
          <span className="cd-nd-title-card" style={card} data-anim="float">
            <span className="cd-nd-title-q" data-anim="bob">
              ?
            </span>
            <span className="cd-nd-title-sheen" data-anim="sheen" />
          </span>
          <span className="cd-nd-title-rows">
            <TitleRow hue={shownHue} theme={theme} />
            <span className="cd-nd-title-row cd-nd-title-row-next" data-anim="pulse">
              <span className="cd-nd-title-drop" data-anim="drop1">
                <TitleRow hue={nextHue} theme={theme} />
              </span>
            </span>
            <span className="cd-nd-title-row cd-nd-title-row-hidden">
              <span className="cd-nd-title-drop" data-anim="drop2">
                <TitleRow hue={hiddenHue} theme={theme} />
              </span>
            </span>
          </span>
        </div>
        <div className="cd-nd-title-meta" data-rise="">
          <span className="cd-daily-pill">{TITLE_SCREEN.pill}</span>
          {day && <span className="cd-nd-title-day">{day}</span>}
        </div>
        <div className="cd-nd-title-words" data-rise="">
          <h1 className="cd-nd-title-heading">{TITLE_SCREEN.heading}</h1>
          <p className="cd-nd-title-lead">{TITLE_SCREEN.lead}</p>
        </div>
        <ol className="cd-nd-steps" aria-label="How to play" data-rise="">
          {TITLE_SCREEN.steps.map((step, i) => (
            <li key={step} className="cd-nd-step">
              <span className="cd-nd-badge" aria-hidden="true">
                {i + 1}
              </span>
              {step}
            </li>
          ))}
        </ol>
        <p className="cd-nd-title-fine" data-rise="">
          {TITLE_SCREEN.fine}
        </p>
        {/* Play and the players line share a footer that keeps to the
            foot of the scroller, so on a phone where the screen runs
            taller than the window Play is never under the browser's
            bottom bar; where it all fits, it sits where it would. The two
            rise as one. */}
        <div className="cd-nd-title-go" data-rise="">
          <button
            type="button"
            className="cd-nd-play"
            onClick={onPlay}
            disabled={busy}
            aria-busy={busy || undefined}
          >
            <span className="cd-nd-play-shine" aria-hidden="true" data-anim="shine" />
            {TITLE_SCREEN.play}
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
          {played && (
            <p className="cd-nd-title-played">
              <span className="cd-nd-title-count">{played.count}</span>
              {played.rest}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

/** A filled row of the picture: a face tinted in this hue, and a bar. */
function TitleRow({ hue, theme }: { hue: number; theme: Theme }) {
  return (
    <span className="cd-nd-title-row cd-nd-title-row-shown">
      <span className="cd-nd-title-face" style={toneStyle(hueColour(hue, theme))} />
      <span className="cd-nd-title-bar" />
    </span>
  );
}

/** Plays the title screen's motion on the parts inside `box`: the rise,
 *  in the order the parts are drawn, and every loop, until the screen
 *  goes. Before the screen is painted, so no part shows for a frame
 *  before it rises. Started only as the screen is put down, and again
 *  should the reader stop asking for stillness, never by a redraw. A
 *  reader who asks for stillness while it plays has it all called off,
 *  and the parts settle where the stylesheet puts them. (Development's
 *  StrictMode puts the screen down twice before it is painted; the
 *  second start is the one seen.) */
function useTitleMotion(box: RefObject<HTMLDivElement | null>, still: boolean) {
  useLayoutEffect(() => {
    const el = box.current;
    if (!el || still) return;
    const played: (Animation | null)[] = [];
    el.querySelectorAll('[data-rise]').forEach((part, i) => {
      const rise = titleRise(i);
      played.push(animate(part, rise.keyframes, rise.options));
    });
    for (const name of Object.keys(TITLE_LOOPS) as TitleLoop[]) {
      const loop = TITLE_LOOPS[name];
      played.push(animate(el.querySelector(`[data-anim="${name}"]`), loop.keyframes, loop.options));
    }
    return () => played.forEach((a) => a?.cancel());
  }, [box, still]);
}
