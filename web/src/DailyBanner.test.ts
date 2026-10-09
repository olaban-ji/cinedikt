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
  fetchBanner,
  followBanner,
  type BannerState,
  type BannerToday,
} from './DailyBanner';
import { GAME_NAME } from './daily';
import css from './grid.css?raw';

/** A game as far as the banner reads one: its phase, points and outcome.
 *  The rest is the shape the server sends, filled with nothing. */
function gameOf(over: Partial<DailyGame>): DailyGame {
  return {
    phase: 'play',
    pts: 1000,
    seq: 0,
    startedAt: '2026-10-08T09:00:00Z',
    finishedAt: null,
    won: false,
    gaveUp: false,
    nextCost: 100,
    slots: Array.from({ length: 6 }, (_, slot) => ({ slot, shown: false as const })),
    facts: {},
    overlaps: [],
    log: [],
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
const won = gameOf({ phase: 'done', pts: 640, won: true });
const missed = gameOf({ phase: 'done', pts: 0, won: false, gaveUp: true });

const render = (day: BannerState, onDaily = () => {}) =>
  renderToStaticMarkup(createElement(DailyBanner, { day, onDaily }));

describe('what the banner says', () => {
  it('is titled with the game’s name, Name Drop, as the title screen is', () => {
    expect(BANNER_TITLE).toBe('Name Drop');
    expect(BANNER_TITLE).toBe(GAME_NAME);
    // The handoff's label: the name ends its own sentence before the
    // button's word.
    expect(bannerView(today(null, 0)).label).toBe('Cinedikt Daily, No. 142: Name Drop. Play');
  });

  it('sets the name in Young Serif at 19px, and 17px on a phone', () => {
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    const rule = (body: string) =>
      [...body.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
        .filter((r) => r[1].split(',').map((x) => x.trim()).includes('.cd-daily-banner-title'))
        .map((r) => r[2].replace(/\s+/g, ' ').trim());
    const outside = text.replace(/@media[^{]*\{[\s\S]*?\n\}/g, '');
    expect(rule(outside)).toEqual([
      "font-family: 'Young Serif', serif; font-weight: 400; font-size: 19px; line-height: 1.15;",
    ]);
    const phone = [...text.matchAll(/@media \(max-width: 639\.98px\) \{([\s\S]*?)\n\}/g)].flatMap((m) => rule(m[1]));
    expect(phone).toEqual(['font-size: 17px;']);
  });

  it('asks to play, with how many are, before the reader has started', () => {
    expect(bannerView(today(null))).toEqual({
      no: 'No. 142',
      day: 'Thursday 8 October',
      sub: '61,240 playing today',
      go: 'Play',
      label: 'Cinedikt Daily, No. 142: Name Drop. Play',
    });
  });

  it('says nothing about the crowd while there is none', () => {
    // "0 playing today" would say the game is empty, not that it is new.
    const v = bannerView(today(null, 0));
    expect(v.sub).toBe('');
    expect(v.go).toBe('Play');
    expect(v.label).toBe('Cinedikt Daily, No. 142: Name Drop. Play');
    expect(bannerView(today(null, 1)).sub).toBe('1 playing today');
  });

  it('offers the way back into a game under way, with the points left', () => {
    const v = bannerView(today(playing));
    expect(v.sub).toBe('640 points left');
    expect(v.go).toBe('Keep going');
    // The handoff names it as before Play, ending on the button's own
    // words, so a reader who says what they see is understood.
    expect(v.label).toBe('Cinedikt Daily, No. 142: Name Drop. Keep going');
  });

  it('offers the result once today is over, with the points kept or the miss', () => {
    expect(bannerView(today(won))).toMatchObject({
      sub: '640 points today',
      go: 'Results',
      label: 'Cinedikt Daily, No. 142. See your result',
    });
    expect(bannerView(today(gameOf({ phase: 'done', pts: 1000, won: true })))).toMatchObject({
      sub: '1,000 points today',
    });
    expect(bannerView(today(missed))).toMatchObject({
      sub: 'Missed today',
      go: 'Results',
      label: 'Cinedikt Daily, No. 142. See your result',
    });
    // Run out of points is missed as surely as asking for the answer.
    expect(bannerView(today(gameOf({ phase: 'done', pts: 0 }))).sub).toBe('Missed today');
  });

  it('is named as the handoff names it, the line beside the button left to the eye', () => {
    // The page behind the link says the line again, and more.
    for (const g of [null, playing, won, missed]) {
      const v = bannerView(today(g));
      expect(v.label).toContain('Cinedikt Daily, No. 142');
      expect(v.label).not.toContain(v.sub);
    }
    expect(bannerView(today(null)).label).toContain(BANNER_TITLE);
  });

  it('draws the box with nothing guessed while the puzzle is on its way', () => {
    expect(bannerView(null)).toEqual({
      no: '',
      day: '',
      sub: '',
      // The room "Play" takes, kept; the stylesheet holds the word back.
      go: 'Play',
      label: 'Cinedikt Daily: Name Drop',
    });
  });

  it('never says film', () => {
    for (const g of [null, playing, won, missed]) {
      expect(JSON.stringify(bannerView(today(g)))).not.toMatch(/film/i);
    }
    expect(JSON.stringify(bannerView(null))).not.toMatch(/film/i);
  });
});

describe('the banner’s line with a place this week', () => {
  const week = { rank: 1204, players: 83500 };
  const ranked = (game: DailyGame | null) => ({ ...today(game), week });

  it('says the reader’s place in place of the crowd, before they have started today', () => {
    expect(bannerView(ranked(null))).toMatchObject({
      sub: '1,204th this week',
      go: 'Play',
      label: 'Cinedikt Daily, No. 142: Name Drop. Play',
    });
    // Even on a day nobody has played yet.
    expect(bannerView({ ...ranked(null), played: 0 }).sub).toBe('1,204th this week');
  });

  it('says the crowd as before with no place to say', () => {
    expect(bannerView({ ...today(null), week: null }).sub).toBe('61,240 playing today');
    expect(bannerView(today(null)).sub).toBe('61,240 playing today');
  });

  it('keeps to the points while the game is on, and to how it went once it is over', () => {
    expect(bannerView(ranked(playing)).sub).toBe('640 points left');
    expect(bannerView(ranked(won)).sub).toBe('640 points today');
    expect(bannerView(ranked(missed)).sub).toBe('Missed today');
  });

  it('draws the line and no leaderboard', () => {
    const html = render({ state: 'ready', today: ranked(null) });
    expect(html).toContain('<span class="cd-daily-banner-sub">1,204th this week</span>');
    expect(html).not.toContain('cd-daily-row');
  });

  it('is hidden on a phone, whatever it says', () => {
    // The phone's block: "(max-width: 639.98px)" alone, as the screen
    // classes write it.
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    const blocks = [...text.matchAll(/@media \(max-width: 639\.98px\) \{([\s\S]*?)\n\}/g)].map((m) => m[1]);
    const hides = blocks.some((b) =>
      [...b.matchAll(/([^{}]+)\{([^{}]*)\}/g)].some(
        (r) => r[1].split(',').map((x) => x.trim()).includes('.cd-daily-banner-sub') && /display:\s*none/.test(r[2]),
      ),
    );
    expect(hides).toBe(true);
  });
});

describe('asking for the banner', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  /** A fetch that answers today's puzzle and the reader's standing by
   *  address, and remembers what it was asked. */
  function stub(me: { status: number; body?: unknown }) {
    const fetch = vi.fn(async (url: string) => {
      const a = url.startsWith('/api/daily/me') ? me : { status: 200, body: today(null) };
      return {
        ok: a.status >= 200 && a.status < 300,
        status: a.status,
        statusText: '',
        json: async () => a.body,
      };
    });
    vi.stubGlobal('fetch', fetch);
    return fetch;
  }

  it('asks for today’s puzzle and the reader’s standing together, and hands on both as one', async () => {
    const fetch = stub({ status: 200, body: { streak: 3, week: { rank: 1204, players: 83500 } } });
    const got = await fetchBanner(new AbortController().signal);
    expect(got).toEqual({ ...today(null), week: { rank: 1204, players: 83500 } });
    const asked = fetch.mock.calls.map(([url]) => new URL(url, 'https://cinedikt.test').pathname).sort();
    expect(asked).toEqual(['/api/daily', '/api/daily/me']);
  });

  it('takes a standing that fails, or that has no week, as no place', async () => {
    stub({ status: 500 });
    expect((await fetchBanner(new AbortController().signal)).week).toBeNull();
    stub({ status: 200, body: { streak: 0, week: null } });
    const got = await fetchBanner(new AbortController().signal);
    expect(got.week).toBeNull();
    expect(bannerView(got).sub).toBe('61,240 playing today');
  });
});

describe('the banner', () => {
  it('is one link to /daily, the whole of it, named for what it says', () => {
    const html = render({ state: 'ready', today: today(playing) });
    expect(html).toMatch(
      /^<a class="cd-daily-banner" href="\/daily" aria-label="Cinedikt Daily, No. 142: Name Drop. Keep going">/,
    );
    // The button is drawn, not a control nested inside the link.
    expect(html).not.toContain('<button');
    expect(html.match(/<a /g)).toHaveLength(1);
  });

  it('draws the fan, the pill, the number and day, the name, the sub and the button', () => {
    const html = render({ state: 'ready', today: today(null) });
    expect(html).toContain(
      '<span class="cd-daily-banner-fan" aria-hidden="true"><span class="cd-daily-banner-card cd-daily-banner-card-l"></span><span class="cd-daily-banner-card cd-daily-banner-card-r"></span><span class="cd-daily-banner-q">?</span></span>',
    );
    expect(html).toContain(
      '<span class="cd-daily-pill">Daily</span><span class="cd-daily-banner-meta">No. 142<span class="cd-daily-banner-day"> · Thursday 8 October</span></span>',
    );
    expect(html).toContain('<span class="cd-daily-banner-title">Name Drop</span>');
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
    expect(html).toContain('<span class="cd-daily-banner-title">Name Drop</span>');
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
