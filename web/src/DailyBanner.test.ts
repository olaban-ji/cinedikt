import { createElement, type MouseEvent, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { DailyGame } from './api';
import {
  BANNER_TITLE,
  BOB_KEYFRAMES,
  BOB_MS,
  DailyBanner,
  SHINE_DELAY_MS,
  SHINE_KEYFRAMES,
  SHINE_MS,
  askForBanner,
  bannerView,
  followBanner,
  type BannerState,
  type BannerToday,
} from './DailyBanner';

/** A game as far as the banner reads one: its phase, points and outcome.
 *  The rest is the shape the server sends, filled with nothing. */
function gameOf(over: Partial<DailyGame>): DailyGame {
  return {
    phase: 'play',
    pts: 1000,
    seq: 0,
    startedAt: '2026-10-08T09:00:00Z',
    finishedAt: null,
    secs: null,
    won: false,
    gaveUp: false,
    nextCost: 100,
    log: [{ type: 'start' }],
    known: [],
    end: null,
    ...over,
  };
}

const today = (game: DailyGame | null, played = 61240): BannerToday => ({
  no: 142,
  date: '2026-10-08',
  played,
  game,
  now: '2026-10-08T12:00:00Z',
  next: '2026-10-09T00:00:00Z',
});

const playing = gameOf({ phase: 'play', pts: 640 });
const won = gameOf({ phase: 'done', pts: 640, won: true, secs: 187 });
const missed = gameOf({ phase: 'done', pts: 0, won: false, gaveUp: true });

const render = (day: BannerState, onDaily = () => {}) =>
  renderToStaticMarkup(createElement(DailyBanner, { day, onDaily }));

describe('what the banner says', () => {
  it('asks to play, with how many are, before the reader has started', () => {
    expect(bannerView(today(null))).toEqual({
      no: 'No. 142',
      day: 'Thursday 8 October',
      sub: '61,240 playing today',
      go: 'Play',
      label: 'Cinedikt Daily, No. 142: Whose map is it? 61,240 playing today. Play',
    });
  });

  it('says nothing about the crowd while there is none', () => {
    // "0 playing today" would say the game is empty, not that it is new.
    const v = bannerView(today(null, 0));
    expect(v.sub).toBe('');
    expect(v.go).toBe('Play');
    expect(v.label).toBe('Cinedikt Daily, No. 142: Whose map is it? Play');
    expect(bannerView(today(null, 1)).sub).toBe('1 playing today');
  });

  it('offers the way back into a game under way, with the points left', () => {
    const v = bannerView(today(playing));
    expect(v.sub).toBe('640 points left');
    expect(v.go).toBe('Keep going');
    expect(v.label).toBe('Cinedikt Daily, No. 142: Whose map is it? 640 points left. Keep going');
  });

  it('offers the result once today is over, with the points kept or the miss', () => {
    expect(bannerView(today(won))).toMatchObject({ sub: '640 points today', go: 'Results' });
    expect(bannerView(today(gameOf({ phase: 'done', pts: 1000, won: true })))).toMatchObject({
      sub: '1,000 points today',
    });
    expect(bannerView(today(missed))).toMatchObject({ sub: 'Missed today', go: 'Results' });
    // Run out of points is missed as surely as asking for the answer.
    expect(bannerView(today(gameOf({ phase: 'done', pts: 0 }))).sub).toBe('Missed today');
  });

  it('puts every word the eye is given into the link’s name, the button’s included', () => {
    for (const g of [null, playing, won, missed]) {
      const v = bannerView(today(g));
      for (const words of [v.no, BANNER_TITLE, v.sub, v.go]) expect(v.label).toContain(words);
    }
  });

  it('draws the box with nothing guessed while the puzzle is on its way', () => {
    expect(bannerView(null)).toEqual({
      no: '',
      day: '',
      sub: '',
      // The room "Play" takes, kept; the stylesheet holds the word back.
      go: 'Play',
      label: 'Cinedikt Daily: Whose map is it?',
    });
  });

  it('never says film', () => {
    for (const g of [null, playing, won, missed]) {
      expect(JSON.stringify(bannerView(today(g)))).not.toMatch(/film/i);
    }
    expect(JSON.stringify(bannerView(null))).not.toMatch(/film/i);
  });
});

describe('the banner', () => {
  it('is one link to /daily, the whole of it, named for what it says', () => {
    const html = render({ state: 'ready', today: today(playing) });
    expect(html).toMatch(
      /^<a class="cd-daily-banner" href="\/daily" aria-label="Cinedikt Daily, No. 142: Whose map is it\? 640 points left. Keep going">/,
    );
    // The button is drawn, not a control nested inside the link.
    expect(html).not.toContain('<button');
    expect(html.match(/<a /g)).toHaveLength(1);
  });

  it('draws the fan, the pill, the number and day, the question, the sub and the button', () => {
    const html = render({ state: 'ready', today: today(null) });
    expect(html).toContain(
      '<span class="cd-daily-banner-fan" aria-hidden="true"><span class="cd-daily-banner-card cd-daily-banner-card-l"></span><span class="cd-daily-banner-card cd-daily-banner-card-r"></span><span class="cd-daily-banner-q">?</span></span>',
    );
    expect(html).toContain(
      '<span class="cd-daily-pill">Daily</span><span class="cd-daily-banner-meta">No. 142<span class="cd-daily-banner-day"> · Thursday 8 October</span></span>',
    );
    expect(html).toContain('<span class="cd-daily-banner-title">Whose map is it?</span>');
    expect(html).toContain('<span class="cd-daily-banner-sub">61,240 playing today</span>');
    expect(html).toMatch(
      /<span class="cd-daily-banner-go"><span class="cd-daily-banner-shine" aria-hidden="true"><\/span><span class="cd-daily-banner-label">Play<\/span><svg [^>]*aria-hidden="true"><path d="M5 12h14M13 6l6 6-6 6"><\/path><\/svg><\/span>/,
    );
  });

  it('leaves the sub out when there is nothing to say', () => {
    expect(render({ state: 'ready', today: today(null, 0) })).not.toContain('cd-daily-banner-sub');
  });

  it('is drawn while the puzzle is on its way, marked waiting and holding the number back', () => {
    const html = render({ state: 'waiting' });
    expect(html).toMatch(/^<a class="cd-daily-banner cd-daily-banner-waiting" href="\/daily"/);
    expect(html).toContain('<span class="cd-daily-banner-title">Whose map is it?</span>');
    expect(html).not.toContain('cd-daily-banner-meta');
    expect(html).not.toContain('cd-daily-banner-sub');
  });

  it('is gone when today’s puzzle cannot be had', () => {
    expect(render({ state: 'gone' })).toBe('');
  });

  it('hands its click to the router, event and all, which leaves a modified one to the browser', () => {
    const onDaily = vi.fn();
    type Link = ReactElement<{ onClick: (e: MouseEvent<HTMLAnchorElement>) => void; href: string }>;
    const drawn: { link?: Link } = {};
    // The component is called inside a probe, as a child of it, so its
    // hooks have a render to belong to and its link can be pressed
    // without a DOM.
    renderToStaticMarkup(
      createElement(function Probe() {
        drawn.link = DailyBanner({ day: { state: 'ready', today: today(null) }, onDaily }) as Link;
        return null;
      }),
    );
    const link = drawn.link;
    if (!link) throw new Error('the banner drew nothing');
    expect(link.props.href).toBe('/daily');
    const e = { metaKey: true } as unknown as MouseEvent<HTMLAnchorElement>;
    link.props.onClick(e);
    expect(onDaily).toHaveBeenCalledWith(e);
  });
});

describe('the banner’s answer', () => {
  it('is the puzzle, once it has come', async () => {
    const got: BannerState[] = [];
    const t = today(won);
    await askForBanner(async () => t, new AbortController().signal, (d) => got.push(d));
    expect(got).toEqual([{ state: 'ready', today: t }]);
  });

  it('takes the banner away on any failure, not-ready included', async () => {
    const got: BannerState[] = [];
    const refusal = Object.assign(new Error('no puzzle today'), { status: 503, reason: 'not-ready' });
    await askForBanner(
      async () => {
        throw refusal;
      },
      new AbortController().signal,
      (d) => got.push(d),
    );
    await askForBanner(
      async () => {
        throw new TypeError('Failed to fetch');
      },
      new AbortController().signal,
      (d) => got.push(d),
    );
    expect(got).toEqual([{ state: 'gone' }, { state: 'gone' }]);
  });

  it('is dropped when the screen has gone before it lands', async () => {
    const got: BannerState[] = [];
    const ctrl = new AbortController();
    const asked = askForBanner(
      async (signal) => {
        ctrl.abort();
        expect(signal.aborted).toBe(true);
        return today(null);
      },
      ctrl.signal,
      (d) => got.push(d),
    );
    await asked;
    const failing = new AbortController();
    failing.abort();
    await askForBanner(
      async () => {
        throw new DOMException('aborted', 'AbortError');
      },
      failing.signal,
      (d) => got.push(d),
    );
    expect(got).toEqual([]);
  });

  it('is asked for with the screen’s signal, so leaving it calls the request off', async () => {
    const fetcher = vi.fn(async (_signal: AbortSignal) => today(null));
    const ctrl = new AbortController();
    await askForBanner(fetcher, ctrl.signal, () => {});
    expect(fetcher).toHaveBeenCalledWith(ctrl.signal);
  });
});

describe('the banner left open over the reader’s midnight', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  // A reader in Tokyo: their 8 October ends at 15:00 UTC, and this clock
  // is 10 s behind the server's.
  const tokyo = (no: number, date: string, now: string, next: string, game: DailyGame | null = null) => ({
    ...today(game),
    no,
    date,
    now,
    next,
  });
  const EIGHTH = tokyo(142, '2026-10-08', '2026-10-08T14:58:00Z', '2026-10-09T00:00:00+09:00', won);
  const NINTH = tokyo(143, '2026-10-09', '2026-10-08T15:00:01Z', '2026-10-10T00:00:00+09:00');
  const settle = () => vi.advanceTimersByTimeAsync(0);

  it('asks again at midnight on the server’s clock, and moves on to the new puzzle', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-10-08T14:57:50Z'));
    const answers = [EIGHTH, NINTH];
    const fetcher = vi.fn(async () => answers.shift() ?? NINTH);
    const got: BannerState[] = [];
    const stop = followBanner(fetcher, (d) => got.push(d));
    await settle();
    expect(got).toEqual([{ state: 'ready', today: EIGHTH }]);
    // A second short of midnight on the server's clock: nothing yet.
    await vi.advanceTimersByTimeAsync(119_000);
    expect(fetcher).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(fetcher).toHaveBeenCalledTimes(2);
    // The box is never emptied while it asks: the 8th stays up until the
    // 9th is here, and then the 9th is offered to play.
    expect(got).toEqual([
      { state: 'ready', today: EIGHTH },
      { state: 'ready', today: NINTH },
    ]);
    expect(bannerView(NINTH)).toMatchObject({ no: 'No. 143', day: 'Friday 9 October', go: 'Play' });
    stop();
  });

  it('goes, as on any failure, when the ask at midnight fails', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-10-08T14:57:50Z'));
    let first = true;
    const fetcher = vi.fn(async () => {
      if (first) {
        first = false;
        return EIGHTH;
      }
      throw Object.assign(new Error('no puzzle'), { status: 503, reason: 'not-ready' });
    });
    const got: BannerState[] = [];
    followBanner(fetcher, (d) => got.push(d));
    await vi.advanceTimersByTimeAsync(120_000);
    expect(got).toEqual([{ state: 'ready', today: EIGHTH }, { state: 'gone' }]);
    // Gone is gone: nothing is left waiting to ask again.
    expect(vi.getTimerCount()).toBe(0);
  });

  it('stops asking once the screen has gone, the request in flight and the watch alike', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-10-08T14:57:50Z'));
    const fetcher = vi.fn(async (_signal: AbortSignal) => EIGHTH);
    const got: BannerState[] = [];
    const stop = followBanner(fetcher, (d) => got.push(d));
    await settle();
    stop();
    expect(fetcher.mock.calls[0][0].aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(86_400_000);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(got).toHaveLength(1);
  });
});

describe('the banner’s motion', () => {
  // The prototype's numbers (Cinedikt Prototype.dc.html, setDailyShine
  // and setDailyQ).
  it('crosses the button once every 3.6s, after 1.2s, and rests off to the side', () => {
    expect(SHINE_MS).toBe(3600);
    expect(SHINE_DELAY_MS).toBe(1200);
    expect(SHINE_KEYFRAMES).toEqual([
      { translate: '-120% 0', offset: 0 },
      { translate: '320% 0', offset: 0.28 },
      { translate: '320% 0', offset: 1 },
    ]);
  });

  it('bobs the hidden card up and back with a little rock, 1.7s each way', () => {
    expect(BOB_MS).toBe(1700);
    expect(BOB_KEYFRAMES).toEqual([
      { transform: 'translateY(0) rotate(-2deg)' },
      { transform: 'translateY(-5px) rotate(2deg)' },
    ]);
  });
});
