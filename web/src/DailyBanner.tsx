import { useEffect, useRef, useState, type MouseEvent, type RefObject } from 'react';
import { fetchDaily, fetchDailyWeek, type DailyToday, type DailyWeek } from './api';
import { clockOffset, dailyDayText, fmtN, standingText, watchMidnight } from './daily';
import { DAILY_PATH } from './movieParam';
import { animate } from './motion';
import { useReducedMotion } from './theme';

// Cinedikt Daily's way in: a banner first on the opening screen, so it is
// seen on arrival, that says what today's puzzle is and where the reader
// has got with it. It is one link, the whole of it; the button at its end
// is drawn, not a control of its own, so nothing interactive is nested
// inside another.
//
// Its box is on screen from the first paint, before the server has said
// anything, because the opening screen counts the rows of films that fit
// under it: a banner that arrived late would push the last row off the
// bottom. What it says is filled in when today's puzzle answers, and if
// it never does — the server is down, or today's puzzle has not been
// picked yet — the banner goes, and the screen measures again without it.
//
// Today is the reader's own date, so a screen left open over their
// midnight asks again then, and the banner moves on to the new puzzle
// without a word; the box stays as it is while it asks.

/** What the banner needs of today's puzzle: which it is, how many have
 *  played it, the reader's own game, if they have one, and the server's
 *  clock and the reader's next midnight, to know when to ask again. And
 *  the reader's place this week, from `GET /api/daily/me`, when they have
 *  one; absent or null, there is none to say. */
export type BannerToday = Pick<DailyToday, 'no' | 'date' | 'played' | 'game' | 'now' | 'next'> & {
  week?: DailyWeek | null;
};

/** Where the banner's answer has got to: on its way, with the box drawn
 *  and nothing in it yet; here; or not coming, and the banner gone. */
export type BannerState =
  | { state: 'waiting' }
  | { state: 'ready'; today: BannerToday }
  | { state: 'gone' };

/** The question the game asks, which the banner asks too. */
export const BANNER_TITLE = 'Whose map is it?';

/** Everything the banner says, worked out from today's puzzle. */
export interface BannerView {
  /** "No. 142", or nothing while the puzzle is on its way. */
  no: string;
  /** "Thursday 8 October", which follows the number where there is room
   *  for it. The stylesheet takes it off a phone, as it does the sub. */
  day: string;
  /** The line beside the button: the reader's place this week or how
   *  many are playing, the points left, or how today went. Empty when
   *  there is nothing worth saying. */
  sub: string;
  /** The button's word: Play, Keep going or Results. */
  go: string;
  /** The link's name for a screen reader. It holds every word the eye is
   *  given, the button's included, so a reader who says "click Results"
   *  to their speech software is understood. */
  label: string;
}

/** What the banner says. Null is the puzzle still on its way: the box,
 *  the pill and the question are drawn, the number and the sub are not,
 *  and the button keeps the room "Play" takes, with the word held back,
 *  so nothing guesses at a state it does not know yet. */
export function bannerView(today: BannerToday | null): BannerView {
  if (!today) return { no: '', day: '', sub: '', go: 'Play', label: `Cinedikt Daily: ${BANNER_TITLE}` };
  const g = today.game;
  const [sub, go] =
    g?.phase === 'done'
      ? [g.won ? `${fmtN(g.pts)} points today` : 'Missed today', 'Results']
      : g?.phase === 'play'
        ? [`${fmtN(g.pts)} points left`, 'Keep going']
        : // Not started: the reader's own place this week, which is
          // over the days before today, when they have one; otherwise the
          // crowd. Nobody yet is not a crowd worth mentioning; "0 playing
          // today" would say the game is empty, not that it is new.
          [
            standingText(today.week) || (today.played > 0 ? `${fmtN(today.played)} playing today` : ''),
            'Play',
          ];
  const no = `No. ${today.no}`;
  return {
    no,
    day: dailyDayText(today.date),
    sub,
    go,
    label: `Cinedikt Daily, ${no}: ${BANNER_TITLE} ${sub ? `${sub}. ` : ''}${go}`,
  };
}

/** The button's shine: a band of light that crosses it, then rests off
 *  to the side, once every 3.6s, starting 1.2s after the screen is up.
 *  The easing is the whole loop's, not each step's, so the crossing is
 *  over in about the first 1.3s and the rest is the pause: a stylesheet
 *  animation would ease each step on its own and cross in a second flat,
 *  which is why it is played from script. */
export const SHINE_MS = 3600;
export const SHINE_DELAY_MS = 1200;
export const SHINE_KEYFRAMES: Keyframe[] = [
  { translate: '-120% 0', offset: 0 },
  { translate: '320% 0', offset: 0.28 },
  { translate: '320% 0', offset: 1 },
];

/** The "?" card bobbing over the two face down behind it, up and back
 *  with a little rock, 1.7s each way. It is centred by a margin rather
 *  than a transform, because this is the transform it plays. */
export const BOB_MS = 1700;
export const BOB_KEYFRAMES: Keyframe[] = [
  { transform: 'translateY(0) rotate(-2deg)' },
  { transform: 'translateY(-5px) rotate(2deg)' },
];

/** Today's puzzle and the reader's place this week, asked for together
 *  and handed on as one, so the line beside the button is settled once,
 *  rather than changing under the reader a moment after it is drawn. A
 *  place that cannot be had is no place: only the puzzle failing takes
 *  the banner away. Both go through the Daily's own helpers, so both
 *  carry the reader's zone. */
export function fetchBanner(signal: AbortSignal): Promise<BannerToday> {
  return Promise.all([fetchDaily(signal), fetchDailyWeek(signal)]).then(([today, week]) => ({ ...today, week }));
}

/** Asks for today's puzzle for the banner and hands on what came of it:
 *  the puzzle, or the banner gone. Any failure, not-ready included, takes
 *  the banner away rather than offer a game that will not open. An
 *  answer that lands after `signal` has gone belongs to a screen nobody
 *  is looking at, and is dropped. */
export function askForBanner(
  fetcher: (signal: AbortSignal) => Promise<BannerToday>,
  signal: AbortSignal,
  onDay: (day: BannerState) => void,
): Promise<void> {
  return fetcher(signal).then(
    (today) => {
      if (!signal.aborted) onDay({ state: 'ready', today });
    },
    () => {
      if (!signal.aborted) onDay({ state: 'gone' });
    },
  );
}

/** Asks for today's puzzle for the banner, as askForBanner does, and
 *  again at the reader's midnight for as long as it is followed, since
 *  today is their own date and a screen left open over it would offer a
 *  map that has ended. Each answer brings its own midnight and its own
 *  reading of the server's clock to wait on, and an ask at midnight that
 *  fails takes the banner away, as any failure does. Hands back what
 *  stops it: the request in flight is called off and the watch with it. */
export function followBanner(
  fetcher: (signal: AbortSignal) => Promise<BannerToday>,
  onDay: (day: BannerState) => void,
): () => void {
  const ctrl = new AbortController();
  let watch = () => {};
  const ask = () => {
    void askForBanner(fetcher, ctrl.signal, (day) => {
      onDay(day);
      if (day.state === 'ready') {
        watch = watchMidnight(day.today.next, clockOffset(day.today.now, Date.now()), ask);
      }
    });
  };
  ask();
  return () => {
    ctrl.abort();
    watch();
  };
}

/** Today's puzzle and the reader's place, asked for as the opening screen
 *  comes up, on every visit to it: the answer changes as the reader
 *  plays, and the server's is the only game there is. Neither request
 *  writes, so asking costs nothing but the requests. A screen still up
 *  at the reader's midnight asks again, keeping what it has until the
 *  answer comes. */
export function useDailyBanner(
  fetcher: (signal: AbortSignal) => Promise<BannerToday> = fetchBanner,
): BannerState {
  const [day, setDay] = useState<BannerState>({ state: 'waiting' });
  // Followed from when the screen comes up until it goes.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => followBanner(fetcher, setDay), []);
  return day;
}

/** The banner itself. `onDaily` is the router's way to /daily, which
 *  leaves a click with a modifier, or not with the main button, to the
 *  browser, so the link can still be opened in a new tab. */
export function DailyBanner({
  day,
  onDaily,
  boxRef,
}: {
  day: BannerState;
  onDaily: (e: MouseEvent<HTMLAnchorElement>) => void;
  /** The banner's box, for the opening screen to watch: a change in its
   *  height moves the grid of films under it. */
  boxRef?: RefObject<HTMLAnchorElement | null>;
}) {
  const still = useReducedMotion();
  const shine = useRef<HTMLSpanElement>(null);
  const card = useRef<HTMLSpanElement>(null);
  const shown = day.state !== 'gone';
  // Both loops start as the banner is put down and stop with it, and a
  // reader who asks for stillness while they run has them called off;
  // the shine then rests off to the side and the card sits flat.
  useEffect(() => {
    if (still || !shown) return;
    const played = [
      animate(shine.current, SHINE_KEYFRAMES, {
        duration: SHINE_MS,
        delay: SHINE_DELAY_MS,
        iterations: Infinity,
        easing: 'ease-in-out',
      }),
      animate(card.current, BOB_KEYFRAMES, {
        duration: BOB_MS,
        direction: 'alternate',
        iterations: Infinity,
        easing: 'ease-in-out',
      }),
    ];
    return () => played.forEach((a) => a?.cancel());
  }, [still, shown]);
  if (!shown) return null;
  const ready = day.state === 'ready';
  const view = bannerView(ready ? day.today : null);
  return (
    <a
      ref={boxRef}
      className={`cd-daily-banner${ready ? '' : ' cd-daily-banner-waiting'}`}
      href={DAILY_PATH}
      onClick={onDaily}
      aria-label={view.label}
    >
      {/* Two cards face down and the hidden one standing over them, as
          the game's intro fans them. */}
      <span className="cd-daily-banner-fan" aria-hidden="true">
        <span className="cd-daily-banner-card cd-daily-banner-card-l" />
        <span className="cd-daily-banner-card cd-daily-banner-card-r" />
        <span ref={card} className="cd-daily-banner-q">
          ?
        </span>
      </span>
      <span className="cd-daily-banner-words">
        <span className="cd-daily-banner-top">
          <span className="cd-daily-pill">Daily</span>
          {view.no && (
            <span className="cd-daily-banner-meta">
              {view.no}
              {view.day && <span className="cd-daily-banner-day">{` · ${view.day}`}</span>}
            </span>
          )}
        </span>
        <span className="cd-daily-banner-title">{BANNER_TITLE}</span>
      </span>
      {view.sub && <span className="cd-daily-banner-sub">{view.sub}</span>}
      <span className="cd-daily-banner-go">
        <span ref={shine} className="cd-daily-banner-shine" aria-hidden="true" />
        <span className="cd-daily-banner-label">{view.go}</span>
        <svg
          width="15"
          height="15"
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
      </span>
    </a>
  );
}
