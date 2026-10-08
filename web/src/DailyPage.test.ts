import { createElement, isValidElement, type ComponentProps, type ReactElement, type ReactNode } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ApiError,
  fetchDailyWeek,
  type DailyEntry,
  type DailyFilm,
  type DailyGame,
  type DailyPerson,
  type DailyToday,
  type DailyWeek,
} from './api';
import { BoardCard } from './DailyBoard';
import { DailyIntro, nextStop } from './DailyIntro';
import { DailyGameView, DailyHeaderTail, DailyPage, askForStanding, fetchDailyPage, playAgain } from './DailyPage';
import { DailyPanel } from './DailyPanel';
import { YEAR_PLACEHOLDER, boardPayload } from './daily';
import { layoutGrid } from './grid';

// ---- a small puzzle: The Matrix, on a board of six ----

const film = (id: string, title: string, year: number, rating: number): DailyFilm => ({
  id,
  title,
  year,
  rating,
  md: 0,
});

const MAN_AND_BOY = film('tt0068907', 'Man and Boy', 1971, 5.5);
const BOBBY = film('tt0108065', 'Searching for Bobby Fischer', 1993, 7.3);
const BABY = film('tt0109190', 'Baby’s Day Out', 1994, 6.3);
const RELOADED = film('tt0234215', 'The Matrix Reloaded', 2003, 7.2);
const BOUND = film('tt0115736', 'Bound', 1996, 7.3);
const MATRIX = { ...film('tt0133093', 'The Matrix', 1999, 8.7), genres: ['Action', 'Sci-Fi'] };
const LANA: DailyPerson = { id: 'nm0905154', name: 'Lana Wachowski', role: 'director', slot: 0, cards: ['c4', 'c5'] };

function todayOf(game: DailyGame | null = null): DailyToday {
  return {
    no: 142,
    date: '2026-10-08',
    now: '2026-10-08T12:00:00Z',
    next: '2026-10-09T00:00:00Z',
    cards: [
      { id: 'c1', year: 1971, rating: 5.5, md: 0 },
      { id: 'c2', year: 1993, rating: 7.3, md: 0 },
      { id: 'c3', year: 1994, rating: 6.3, md: 0 },
      { id: 'c4', year: 2003, rating: 7.2, md: 0 },
      { id: 'c5', year: 1996, rating: 7.3, md: 0 },
      { id: 'c6', year: 2000, rating: 7.0, md: 0 },
    ],
    start: [
      { card: 'c1', film: MAN_AND_BOY },
      { card: 'c2', film: BOBBY },
      { card: 'c3', film: BABY },
    ],
    clues: { directors: 2, cast: 6 },
    player: { name: 'Trinity Kimble', saved: false },
    played: 61240,
    streak: { now: 0, before: 3 },
    game,
  };
}

function gameOf(log: DailyEntry[], over: Partial<DailyGame> = {}): DailyGame {
  return {
    phase: 'play',
    pts: 1000,
    seq: log.length,
    startedAt: '2026-10-08T11:58:00Z',
    finishedAt: null,
    secs: null,
    won: false,
    gaveUp: false,
    nextCost: 100,
    log: [{ type: 'start' }, ...log],
    known: [],
    end: null,
    ...over,
  };
}

function won(): DailyGame {
  return gameOf([{ type: 'flip', card: 'c5', cost: 55, film: BOUND }, { type: 'win' }], {
    phase: 'done',
    pts: 695,
    won: true,
    secs: 192,
    finishedAt: '2026-10-08T12:01:12Z',
    end: {
      answer: MATRIX,
      cards: [
        { id: 'c1', film: MAN_AND_BOY, people: [] },
        { id: 'c2', film: BOBBY, people: [] },
        { id: 'c3', film: BABY, people: [] },
        { id: 'c4', film: RELOADED, people: [0] },
        { id: 'c5', film: BOUND, people: [0] },
        { id: 'c6', film: film('tt0000006', 'Six', 2000, 7.0), people: [] },
      ],
      people: [LANA],
    },
  });
}

/** The game view's props for this puzzle, from a server in development
 *  when `dev` is set. */
function viewProps(
  game: DailyGame | null,
  week: DailyWeek | null = null,
  dev = false,
  again: () => void = () => {},
): ComponentProps<typeof DailyGameView> {
  return {
    today: dev ? { ...todayOf(game), dev: true } : todayOf(game),
    week,
    offset: 0,
    say: () => {},
    reload: () => {},
    again,
    rulesSignal: 0,
  };
}

/** The game for this puzzle, as its first render draws it, with the
 *  reader's place this week as the page asked for it, or none. Effects
 *  never run on the server renderer, so nothing is fetched, nothing is
 *  scrolled and nothing moves: what is left is what each state draws. */
function drawn(game: DailyGame | null, week: DailyWeek | null = null, dev = false): string {
  return renderToStaticMarkup(createElement(DailyGameView, viewProps(game, week, dev)));
}

/** What a reader can read: the text, and the words the page says out
 *  loud or as a placeholder. Class names are code, and may say film. */
function words(markup: string): string {
  const said = [...markup.matchAll(/(?:aria-label|placeholder|title)="([^"]*)"/g)].map((m) => m[1]);
  return `${markup.replace(/<[^>]*>/g, ' ')} ${said.join(' ')}`;
}

beforeEach(() => {
  vi.stubGlobal('window', { innerWidth: 1366, innerHeight: 768 });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the page before Play', () => {
  it('opens on the intro, over a board of blank cards', () => {
    const html = drawn(null);
    expect(html).toContain('role="dialog"');
    expect(html).toContain('aria-modal="true"');
    expect(html).toContain('Whose map is it?');
    expect(html).toContain(
      'One hidden movie. Every movie on its map shares an actor or director with it. Find it with as many of your 1,000 points left as you can.',
    );
    expect(html).toContain('Hidden movie from 1971. Turn it over for 35 points');
    // The starting three turn over when the clock starts, not before.
    expect(html).not.toContain('Man and Boy');
    expect(html).not.toContain('Today’s game');
  });

  it('keeps the board out of reach behind it', () => {
    expect(drawn(null)).toMatch(/aria-label="Today’s map" aria-hidden="true" inert=""/);
  });

  it('lists what everything costs', () => {
    const html = drawn(null);
    for (const [label, tag] of [
      ['Turn over a card', '20–80'],
      ['See a director', '150'],
      ['See an actor', '150'],
      ['See its genres', '80'],
      ['See its year', '200'],
      ['A wrong guess', '100+'],
    ]) {
      expect(html).toMatch(new RegExp(`${label}</span><span class="cd-daily-tag[^"]*">${tag.replace('+', '\\+')}<`));
    }
    expect(html).toContain('You start with three movies showing.');
    expect(html).toContain('Each wrong guess costs 50 more than the last.');
  });

  it('says who the reader is playing as, the day, the streak, and how many have played', () => {
    const html = drawn(null);
    expect(html).toContain('Playing as');
    expect(html).toContain('Trinity Kimble');
    expect(html).toContain('New name');
    expect(html).toContain('Thursday 8 October');
    expect(html).toContain('3-day streak');
    expect(html).toContain('Play No. 142');
    expect(words(html)).toMatch(/61,240\s*people have played today\. The clock starts when you press Play\./);
  });

  it('counts nobody before anyone has played, and still says when the clock starts', () => {
    const today = { ...todayOf(null), played: 0 };
    const html = renderToStaticMarkup(
      createElement(DailyGameView, { ...viewProps(null), today }),
    );
    expect(html).not.toContain('people have played today');
    expect(html).not.toContain('cd-daily-live-n');
    expect(html).toContain('<p class="cd-daily-live" data-in="1">The clock starts when you press Play.</p>');
  });
});

describe('a new name on its way', () => {
  const props = (game: DailyGame | null, spinning: boolean): ComponentProps<typeof DailyIntro> => ({
    today: todayOf(game),
    game,
    name: 'Tr?ntky K?mble',
    spinning,
    streak: 0,
    rank: '',
    rules: !!game,
    busy: false,
    dialogRef: { current: null },
    nameRef: { current: null },
    onGo: () => {},
    onReroll: () => {},
    onClose: () => {},
  });
  const playButton = (html: string) => html.match(/<button[^>]*class="cd-daily-play-button"[^>]*>/)?.[0] ?? '';

  it('holds Play until it lands, so the game starts under the name the reader ends up seeing', () => {
    // Play sends the name shown. Pressed mid-spin it would start the game
    // under the old name, and the reel would then land on one the server
    // never kept.
    const button = playButton(renderToStaticMarkup(createElement(DailyIntro, props(null, true))));
    expect(button).toContain('disabled=""');
    expect(button).toContain('aria-busy="true"');
    expect(playButton(renderToStaticMarkup(createElement(DailyIntro, props(null, false))))).not.toContain('disabled');
  });

  it('never holds the way back to a game, which sends no name', () => {
    const button = playButton(renderToStaticMarkup(createElement(DailyIntro, props(gameOf([]), true))));
    expect(button).not.toContain('disabled');
  });
});

describe('the page while the game is on', () => {
  it('shows the starting three, the panel and nothing of the intro', () => {
    const html = drawn(gameOf([]));
    expect(html).not.toContain('role="dialog"');
    expect(html).toContain('aria-label="Today’s game"');
    expect(html).toContain('Searching for Bobby Fischer');
    expect(html).toContain('Three movies from its map are showing.');
    expect(html).toContain('aria-label="Show Man and Boy on the map"');
    expect(html).toContain('aria-label="Points left"');
    expect(html).toContain('aria-valuenow="1000"');
  });

  it('makes every blank card a button that says its price', () => {
    const html = drawn(gameOf([]));
    expect(html).toMatch(/role="button" tabindex="0" aria-label="Hidden movie from 2003\. Turn it over for 55 points"/);
    expect(html).toMatch(/role="img" tabindex="-1" aria-label="Man and Boy, 1971, rated 5\.5"/);
    expect(html).toContain('<span class="cd-daily-cost">55</span>');
  });

  it('offers the clues, the guess and the way out', () => {
    const html = drawn(gameOf([]));
    expect(html).toContain('aria-label="Clues to buy"');
    expect(html).toContain('aria-label="Directors for 150 points"');
    expect(html).toContain('aria-label="Year for 200 points"');
    expect(html).toContain('placeholder="Name the movie"');
    expect(html).toContain('role="combobox"');
    expect(html).toContain('Click a blank card to turn it over. Your next wrong guess costs 100.');
    expect(html).toContain('Show the answer');
  });

  it('turns a close relative over without its title', () => {
    const html = drawn(gameOf([{ type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } }]));
    expect(html).toContain('A close relative');
    expect(html).toContain('4 in common');
    expect(html).toContain('A close relative of today’s movie, from 2003, rated 7.2. It shares 4 people with it');
    expect(html).not.toContain('Reloaded');
  });

  it('dots a blank card for each known person on it', () => {
    const html = drawn(gameOf([{ type: 'person', role: 'director', cost: 150, people: [LANA] }], { known: [LANA] }));
    expect(html.match(/class="cd-daily-dot"/g)).toHaveLength(2);
    expect(html).toContain('It was directed by:');
    expect(html).toContain('Lana Wachowski');
  });

  it('fades what the guesses rule out and draws where the answer can be', () => {
    const html = drawn(
      gameOf([
        {
          type: 'guess',
          cost: 100,
          film: BABY,
          card: 'c3',
          shared: [],
          year: 'newer',
          rating: 'higher',
        },
      ]),
    );
    expect(html).toContain('1995 or later');
    expect(html).toContain('6.4+');
    expect(html).toContain('Baby’s Day Out (1994) shares no one with today’s movie.');
    expect(html).toContain('Today’s movie is newer and rated higher.');
    // Man and Boy, from 1971 and rated 5.5, cannot be the answer's neighbour.
    expect(html).toMatch(/data-card="c1"[^>]*opacity:0\.28/);
    expect(html).not.toMatch(/data-card="c4"[^>]*opacity:0\.28/);
  });

  it('never holds the answer', () => {
    const html = drawn(
      gameOf([
        { type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } },
        { type: 'person', role: 'director', cost: 150, people: [LANA] },
      ]),
    );
    expect(html).not.toContain(MATRIX.id);
    expect(html).not.toContain('The Matrix');
    expect(html).not.toContain('Today’s movie:');
  });
});

describe('the page once the year is bought', () => {
  // No card on this board is from 1999, so a row opens for it.
  const YEAR: DailyEntry = { type: 'year', cost: 200, year: 1999 };
  const band = (html: string, year: number) =>
    html.match(new RegExp(`<div class="cd-band[^"]*" style="[^"]*" data-band="${year}"`))?.[0] ?? '';
  const label = (html: string, year: number) =>
    html.match(new RegExp(`data-rail="${year}"><span class="[^"]*"`))?.[0] ?? '';

  it('says the year in the feed, and the button says it has been seen', () => {
    const html = drawn(gameOf([YEAR], { pts: 800 }));
    expect(html).toContain('It came out in 1999. The map marks where that year sits.');
    expect(html).toMatch(/<span>Year<\/span><span class="cd-daily-entry-cost">−200<\/span>/);
    expect(html).toMatch(/<button type="button" class="cd-daily-clue" disabled="" aria-label="Year: seen">/);
    expect(html).toContain('<span class="cd-daily-tag cd-daily-tag-muted">Seen</span>');
    expect(html).toContain('aria-valuenow="800"');
  });

  it('opens a row for it, washed and labelled in the accent, with no card drawn in it', () => {
    const html = drawn(gameOf([YEAR], { pts: 800 }));
    expect(band(html, 1999)).toContain('cd-band-anchor');
    expect(band(html, 1999)).not.toContain('opacity');
    expect(label(html, 1999)).toContain('cd-rail-anchor');
    expect(html).not.toContain(YEAR_PLACEHOLDER);
    expect(html.match(/data-card="/g)).toHaveLength(6);
  });

  it('fades every other year to .35, and no card', () => {
    const html = drawn(gameOf([YEAR], { pts: 800 }));
    for (const y of [1971, 1993, 1994, 1996, 2000, 2003]) {
      expect(band(html, y), `${y}`).toContain('opacity:0.35');
      expect(html, `${y}`).toMatch(new RegExp(`style="[^"]*opacity:0\\.35" data-rail="${y}"`));
    }
    // The starting three included: the year rules no card out.
    expect(html).not.toContain('opacity:0.28');
  });

  it('rules both edges of the row and names it once', () => {
    const html = drawn(gameOf([YEAR], { pts: 800 }));
    expect(html.match(/class="cd-daily-bound-across"/g)).toHaveLength(2);
    expect(html.match(/cd-daily-bound-pill-year"[^>]*>1999</g)).toHaveLength(1);
  });

  it('keeps the year on the rows after a wrong guess, adds its rating line, and fades the cards it rules out', () => {
    const html = drawn(
      gameOf([YEAR, { type: 'guess', cost: 100, film: BABY, card: 'c3', shared: [], year: 'newer', rating: 'higher' }], {
        pts: 700,
      }),
    );
    expect(html).toContain('6.4+');
    expect(html).not.toContain('1995 or later');
    expect(html.match(/cd-daily-bound-pill-year"[^>]*>1999</g)).toHaveLength(1);
    // Man and Boy, 1971 and 5.5, is ruled out by the guess; The Matrix
    // Reloaded's blank card, 2003 and 7.2, is not, though its row is.
    expect(html).toMatch(/data-card="c1"[^>]*opacity:0\.28/);
    expect(html).not.toMatch(/data-card="c4"[^>]*opacity:0\.28/);
  });

  it('keeps the row’s wash once the game is over, with the answer in it', () => {
    const g = won();
    const html = drawn({ ...g, log: [g.log[0], YEAR, ...g.log.slice(1)] });
    expect(band(html, 1999)).toContain('cd-band-anchor');
    expect(html).toContain('data-answer="1"');
    expect(html).not.toContain('cd-daily-bound-across');
    expect(html).toContain('You used 1 card and the year.');
  });
});

describe('the reader’s place on the title screen', () => {
  const props = (game: DailyGame | null, rank: string): ComponentProps<typeof DailyIntro> => ({
    today: todayOf(game),
    game,
    name: 'Trinity Kimble',
    spinning: false,
    streak: 3,
    rank,
    rules: !!game,
    busy: false,
    dialogRef: { current: null },
    nameRef: { current: null },
    onGo: () => {},
    onReroll: () => {},
    onClose: () => {},
  });
  const intro = (game: DailyGame | null, rank: string) =>
    renderToStaticMarkup(createElement(DailyIntro, props(game, rank)));

  it('follows the streak, in the streak pill’s box, with a podium for the flame', () => {
    const html = intro(null, '1,204th this week');
    expect(html).toMatch(
      /<span class="cd-daily-streak">[\s\S]*?3-day streak<\/span><span class="cd-daily-streak cd-daily-standing"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 21h18M4.5 21v-6h5v6M9.5 21V8h5v13M14.5 21v-4h5v4"><\/path><\/svg>1,204th this week<\/span><\/div>/,
    );
  });

  it('shows on every visit, the rules after playing included', () => {
    expect(intro(gameOf([]), '1,204th this week')).toContain('1,204th this week');
    expect(intro(won(), '1,203rd this week')).toContain('1,203rd this week');
  });

  it('is not there without a place to show', () => {
    expect(intro(null, '')).not.toContain('cd-daily-standing');
  });

  it('is drawn in the title screen’s first frame, from the place the page asked for with the puzzle', () => {
    // Not a round trip later, when it would re-centre the row it sits in
    // part way through the entrance.
    const html = drawn(null, { rank: 1204, players: 83500 });
    expect(html).toMatch(
      /<span class="cd-daily-streak cd-daily-standing"><svg[\s\S]*?<\/svg>1,204th this week<\/span>/,
    );
    expect(drawn(null)).not.toContain('cd-daily-standing');
  });

  it('comes with no leaderboard', () => {
    const page = drawn(null, { rank: 1204, players: 83500 });
    for (const html of [intro(null, '1,204th this week'), intro(won(), '1,204th this week'), page]) {
      expect(html).not.toContain('cd-daily-row');
      expect(html).not.toContain('Leaderboard');
    }
  });
});

describe('asking for the reader’s place', () => {
  const WEEK: DailyWeek = { rank: 1204, players: 83500 };
  // The place over the days before today, as the page opened with it.
  const BEFORE: DailyWeek = { rank: 2048, players: 83500 };

  afterEach(() => {
    vi.restoreAllMocks();
  });

  /** A fetch that answers today's puzzle and the reader's standing by
   *  address, and remembers what it was asked. A status of 0 is a request
   *  that never got an answer. */
  function stub(daily: { status: number; body?: unknown }, me: { status: number; body?: unknown }) {
    const fetch = vi.fn(async (url: string) => {
      const a = new URL(url, 'https://cinedikt.test').pathname === '/api/daily/me' ? me : daily;
      if (a.status === 0) throw new TypeError('Failed to fetch');
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

  it('asks for today’s puzzle and the reader’s place together as the page opens, both in the reader’s zone', async () => {
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation((() => ({
      resolvedOptions: () => ({ timeZone: 'Asia/Tokyo' }),
    })) as unknown as typeof Intl.DateTimeFormat);
    const fetch = stub({ status: 200, body: todayOf(null) }, { status: 200, body: { streak: 3, week: WEEK } });
    await expect(fetchDailyPage(new AbortController().signal)).resolves.toEqual({ today: todayOf(null), week: WEEK });
    const asked = fetch.mock.calls.map(([url]) => new URL(url, 'https://cinedikt.test'));
    expect(asked.map((u) => u.pathname).sort()).toEqual(['/api/daily', '/api/daily/me']);
    for (const u of asked) expect(u.searchParams.get('tz')).toBe('Asia/Tokyo');
  });

  it('opens the page with no place when the place cannot be had, or there is none', async () => {
    for (const me of [{ status: 503 }, { status: 0 }, { status: 200, body: { streak: 0, week: null } }]) {
      stub({ status: 200, body: todayOf(null) }, me);
      const got = await fetchDailyPage(new AbortController().signal);
      expect(got.today.no, `${me.status}`).toBe(142);
      expect(got.week, `${me.status}`).toBeNull();
    }
  });

  it('fails the page only when today’s puzzle fails', async () => {
    stub(
      { status: 503, body: { error: 'no puzzle yet', reason: 'not-ready' } },
      { status: 200, body: { streak: 3, week: WEEK } },
    );
    await expect(fetchDailyPage(new AbortController().signal)).rejects.toMatchObject({ reason: 'not-ready' });
  });

  it('takes the place worked out with today in once the game ends here', async () => {
    stub({ status: 200 }, { status: 200, body: { streak: 4, week: WEEK } });
    let held: DailyWeek | null = BEFORE;
    await askForStanding(fetchDailyWeek, new AbortController().signal, (w) => (held = w));
    expect(held).toEqual(WEEK);
  });

  it('shows no place, never the one from before today’s game, when the place after it cannot be had', async () => {
    for (const me of [{ status: 503 }, { status: 0 }]) {
      stub({ status: 200 }, me);
      let held: DailyWeek | null = BEFORE;
      await askForStanding(fetchDailyWeek, new AbortController().signal, (w) => (held = w));
      expect(held, `${me.status}`).toBeNull();
    }
    // Nor with a fetcher that fails outright.
    let held: DailyWeek | null = BEFORE;
    await askForStanding(() => Promise.reject(new Error('down')), new AbortController().signal, (w) => (held = w));
    expect(held).toBeNull();
  });

  it('lets the place from before go as it asks, so none is shown while the new one is on its way', () => {
    const told: (DailyWeek | null)[] = [];
    void askForStanding(() => new Promise(() => {}), new AbortController().signal, (w) => told.push(w));
    expect(told).toEqual([null]);
  });

  it('drops an answer that lands after the view has gone', async () => {
    const ctrl = new AbortController();
    // The view goes while the place is on its way.
    const fetcher = async () => {
      ctrl.abort();
      return WEEK;
    };
    const told: (DailyWeek | null)[] = [];
    await askForStanding(fetcher, ctrl.signal, (w) => told.push(w));
    expect(told).toEqual([null]);
  });
});

describe('the page once the game is over', () => {
  it('puts the answer in its place, ringed and tagged, with its year lit', () => {
    const html = drawn(won());
    expect(html).toContain('data-answer="1"');
    expect(html).toContain('aria-label="Today’s movie: The Matrix, 1999, rated 8.7"');
    expect(html).toContain('<span class="cd-searched-tag"');
    expect(html).toContain('cd-band-anchor');
    // Every card is face up.
    expect(html).not.toContain('Hidden movie from');
  });

  it('shows the result: the score, what it took, the answer and the card to share', () => {
    const html = drawn(won());
    expect(html).toContain('<h2 class="cd-daily-res-title">695 points</h2>');
    expect(html).toContain('Solved in 3:12. You used 1 card.');
    expect(html).toContain('aria-label="Show The Matrix on the map"');
    expect(html).toContain('1999 · rated 8.7');
    expect(html).toContain('Cinedikt Daily No. 142');
    expect(html).toContain('695 points · 3:12');
    expect(html).toContain('Copy');
    expect(html).toContain('Leaderboard');
    expect(html).toContain('Loading the leaderboard…');
    expect(words(html)).toContain('Next map in');
    expect(html).toContain('Explore the map');
  });

  it('waits for the board before saying how the reader did', () => {
    const html = drawn(won());
    expect(html).toContain('<span class="cd-daily-stat-n">–</span><span class="cd-daily-stat-caption">of players scored less</span>');
    // Today's solve adds to the three days coming in.
    expect(html).toContain('<span class="cd-daily-stat-n">4</span><span class="cd-daily-stat-caption">days in a row</span>');
  });
});

describe('the Daily’s words on the page', () => {
  it('say "movie", never "film", in every state', () => {
    for (const game of [
      null,
      gameOf([]),
      gameOf([{ type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } }]),
      won(),
    ]) {
      expect(words(drawn(game))).not.toMatch(/\bfilms?\b/i);
      // And in development, Play again's own.
      expect(words(drawn(game, null, true))).not.toMatch(/\bfilms?\b/i);
    }
  });
});

/** The handlers on every element of `el` with class `cls`, every
 *  component in it drawn out, as one server render would draw it. Called
 *  inside a render of its own, so the hooks they use have one to belong
 *  to, and no DOM is needed to press what they draw. */
function pressesIn(el: ReactElement, cls: string): (() => void)[] {
  const found: (() => void)[] = [];
  const walk = (node: ReactNode): void => {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!isValidElement(node)) return;
    const { type, props } = node as ReactElement<{ className?: string; onClick?: () => void; children?: ReactNode }>;
    if (typeof type === 'function') return walk((type as (p: unknown) => ReactNode)(props));
    if (typeof type === 'string' && props.className?.split(' ').includes(cls) && props.onClick) found.push(props.onClick);
    walk(props.children);
  };
  function Probe() {
    walk(el);
    return null;
  }
  renderToStaticMarkup(createElement(Probe));
  return found;
}

describe('Play again, in development', () => {
  const AGAIN = '<button type="button" class="cd-daily-again">Play again (development only)</button>';

  it('sits beside “Show the answer” while the game is on, so starting again never takes giving the answer away', () => {
    const html = drawn(gameOf([]), null, true);
    expect(html).toContain(`${AGAIN}<button type="button" class="cd-daily-reveal">Show the answer</button>`);
    expect(html).toContain('<div class="cd-daily-hintrow cd-daily-hintrow-dev"><span class="cd-daily-hint">');
  });

  it('comes last in the result, where the prototype had its own', () => {
    const html = drawn(won(), null, true);
    expect(html).toContain(`${AGAIN}</div><div class="cd-daily-res-foot">`);
    expect(html.match(/cd-daily-again/g)).toHaveLength(1);
  });

  it('stays while the panel is folded away during the game, with the rest of what the reader can do', () => {
    const html = renderToStaticMarkup(
      createElement(DailyPanel, { ...folded(gameOf([]), false), onAgain: () => {} }),
    );
    expect(html).toContain(AGAIN);
  });

  it('is never offered unless the server says it is not in production', () => {
    for (const game of [null, gameOf([]), won()]) {
      const html = drawn(game);
      expect(html).not.toContain('cd-daily-again');
      expect(words(html)).not.toContain('development only');
      // And the row beside the hint is as it always was.
      expect(html).not.toContain('cd-daily-hintrow-dev');
    }
    const no = renderToStaticMarkup(
      createElement(DailyGameView, { ...viewProps(gameOf([])), today: { ...todayOf(gameOf([])), dev: false } }),
    );
    expect(no).not.toContain('cd-daily-again');
  });

  it('is not on the intro, which starts a game rather than ending one', () => {
    expect(drawn(null, null, true)).not.toContain('cd-daily-again');
  });

  it('asks the page to start again when pressed, from either place', () => {
    for (const game of [gameOf([]), won()]) {
      const again = vi.fn();
      const presses = pressesIn(createElement(DailyGameView, viewProps(game, null, true, again)), 'cd-daily-again');
      expect(presses).toHaveLength(1);
      presses[0]();
      expect(again).toHaveBeenCalledTimes(1);
    }
  });
});

describe('starting again', () => {
  it('starts the page again from nothing once the server has started the reader again, and not before', async () => {
    // Before, the page would ask for today's puzzle and be handed the old
    // movie's game back.
    const order: string[] = [];
    let answer = () => {};
    const done = playAgain(
      () =>
        new Promise<void>((resolve) => {
          order.push('reset');
          answer = resolve;
        }),
      () => order.push('restart'),
      (text) => order.push(`say ${text}`),
    );
    await Promise.resolve();
    expect(order).toEqual(['reset']);
    answer();
    await done;
    expect(order).toEqual(['reset', 'restart']);
  });

  it('leaves the page as it was, and says so, when the reset is refused or never answered', async () => {
    for (const failure of [
      new ApiError('no other movie fits today’s puzzle', 503, 'unavailable', {}),
      new TypeError('Failed to fetch'),
    ]) {
      const restart = vi.fn();
      const said: string[] = [];
      await playAgain(() => Promise.reject(failure), restart, (text) => said.push(text));
      expect(restart, failure.name).not.toHaveBeenCalled();
      expect(said, failure.name).toEqual(['Couldn’t start again. Try again.']);
    }
  });
});

describe('the rules', () => {
  const props = (game: DailyGame): ComponentProps<typeof DailyIntro> => ({
    today: todayOf(game),
    game,
    name: 'Trinity Kimble',
    spinning: false,
    streak: 0,
    rank: '',
    rules: true,
    busy: false,
    dialogRef: { current: null },
    nameRef: { current: null },
    onGo: () => {},
    onReroll: () => {},
    onClose: () => {},
  });

  it('are the intro again, leading back to the game', () => {
    const html = renderToStaticMarkup(createElement(DailyIntro, props(gameOf([]))));
    expect(html).toContain('Whose map is it?');
    expect(html).toContain('Back to the game');
    expect(html).not.toContain('people have played today');
    expect(html).not.toContain('-day streak');
  });

  it('lead back to the result once the game is over', () => {
    expect(renderToStaticMarkup(createElement(DailyIntro, props(won())))).toContain('Back to your result');
  });

  it('keep the focus inside, going round from either end', () => {
    expect(nextStop(-1, 3, false)).toBe(0);
    expect(nextStop(2, 3, false)).toBe(0);
    expect(nextStop(0, 3, true)).toBe(2);
    expect(nextStop(1, 3, true)).toBe(0);
    expect(nextStop(0, 0, false)).toBe(-1);
  });
});

describe('the header’s end on /daily', () => {
  it('names the puzzle by number and day, and offers the rules', () => {
    const html = renderToStaticMarkup(
      createElement(DailyHeaderTail, { day: { no: 142, date: '2026-10-08' }, phone: false, onRules: () => {} }),
    );
    expect(html).toContain('<span class="cd-daily-pill">Daily</span>');
    expect(html).toContain('No. 142 · Thursday 8 October');
    expect(html).toContain('aria-label="How it works"');
  });

  it('names it by number alone on a phone, and not at all before it has loaded', () => {
    const phone = renderToStaticMarkup(
      createElement(DailyHeaderTail, { day: { no: 142, date: '2026-10-08' }, phone: true, onRules: () => {} }),
    );
    expect(phone).toContain('>No. 142<');
    const none = renderToStaticMarkup(createElement(DailyHeaderTail, { day: null, phone: false, onRules: () => {} }));
    expect(none).not.toContain('cd-daily-date');
  });
});

describe('the page as it opens', () => {
  it('holds an empty board while today’s puzzle is on its way', () => {
    expect(renderToStaticMarkup(createElement(DailyPage))).toBe(
      '<div class="cd-daily"><div class="cd-scroller" aria-hidden="true"></div></div>',
    );
  });
});

/** The panel's props, folded away. */
function folded(game: DailyGame, results: boolean): ComponentProps<typeof DailyPanel> {
  return {
    today: todayOf(game),
    game,
    results,
    min: true,
    onMin: () => {},
    delta: null,
    offset: 0,
    touch: true,
    theme: 'dark',
    hues: new Map(),
    codes: new Map(),
    panelRef: { current: null },
    inputRef: { current: null },
    fresh: false,
    player: 'Trinity Kimble',
    onClue: () => {},
    onGuess: async () => true,
    onReveal: () => {},
    onFind: () => {},
    onFindAnswer: () => {},
    onCopy: () => {},
    say: () => {},
  };
}

describe('the panel folded away', () => {
  it('keeps what the reader can do while the game is on, and hides the feed', () => {
    const html = renderToStaticMarkup(createElement(DailyPanel, folded(gameOf([]), false)));
    expect(html).toContain('aria-label="Show the panel"');
    expect(html).toContain('aria-expanded="false"');
    expect(html).not.toContain('Three movies from its map are showing.');
    expect(html).toContain('Tap a blank card to turn it over.');
  });

  it('keeps the countdown and the way back to the result', () => {
    const html = renderToStaticMarkup(createElement(DailyPanel, folded(won(), true)));
    expect(html).toContain('Show results');
    expect(html).not.toContain('Leaderboard');
  });
});

describe('a blank card', () => {
  const today = todayOf(gameOf([]));
  const { payload, settings } = boardPayload(today, gameOf([]));
  const layout = layoutGrid(payload, 1366, settings);
  const placed = layout.cards.find((c) => c.film.id === 'c4')!;

  interface Drawn {
    role: string;
    onClick?: () => void;
    onKeyDown?: (e: { key: string; preventDefault: () => void }) => void;
  }

  /** The card's box as it would be drawn, with its handlers live: called
   *  inside a render of its own, so no DOM is needed to press it. */
  function box(over: Partial<ComponentProps<typeof BoardCard>>): Drawn {
    const props: ComponentProps<typeof BoardCard> = {
      placed,
      card: today.cards[3],
      face: undefined,
      who: undefined,
      layout,
      can: true,
      afford: true,
      hot: false,
      delay: 0,
      faded: false,
      eager: false,
      hues: new Map(),
      codes: new Map(),
      theme: 'dark',
      onFlip: () => {},
      ...over,
    };
    const render = (BoardCard as unknown as { type: (p: typeof props) => ReactElement<Drawn> }).type;
    let tree = null as ReactElement<Drawn> | null;
    function Probe() {
      tree = render(props);
      return null;
    }
    renderToStaticMarkup(createElement(Probe));
    return tree!.props;
  }

  it('turns over on a press, which the page holds to its tap guard', () => {
    const asked: [string, boolean][] = [];
    box({ onFlip: (id, pointer) => asked.push([id, pointer]) }).onClick?.();
    expect(asked).toEqual([['c4', true]]);
  });

  it('turns over on Enter or Space, which is always meant', () => {
    const asked: [string, boolean][] = [];
    const prevent = vi.fn();
    const onKeyDown = box({ onFlip: (id, pointer) => asked.push([id, pointer]) }).onKeyDown!;
    onKeyDown({ key: 'Enter', preventDefault: prevent });
    onKeyDown({ key: ' ', preventDefault: prevent });
    onKeyDown({ key: 'a', preventDefault: prevent });
    expect(asked).toEqual([
      ['c4', false],
      ['c4', false],
    ]);
    expect(prevent).toHaveBeenCalledTimes(2);
  });

  it('is only a picture once it is face up, or before the game is on', () => {
    const up = box({ can: false, face: { kind: 'film', film: RELOADED } });
    expect(up.role).toBe('img');
    expect(up.onClick).toBeUndefined();
    expect(box({ can: false }).onKeyDown).toBeUndefined();
  });
});
