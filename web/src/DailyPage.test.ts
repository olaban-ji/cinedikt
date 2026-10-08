import { createElement, type ComponentProps, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { DailyEntry, DailyFilm, DailyGame, DailyPerson, DailyToday } from './api';
import { BoardCard } from './DailyBoard';
import { DailyIntro, nextStop } from './DailyIntro';
import { DailyGameView, DailyHeaderTail, DailyPage } from './DailyPage';
import { DailyPanel } from './DailyPanel';
import { boardPayload } from './daily';
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

/** The game for this puzzle, as its first render draws it. Effects never
 *  run on the server renderer, so nothing is fetched, nothing is scrolled
 *  and nothing moves: what is left is what each state draws. */
function drawn(game: DailyGame | null): string {
  return renderToStaticMarkup(
    createElement(DailyGameView, {
      today: todayOf(game),
      offset: 0,
      say: () => {},
      reload: () => {},
      rulesSignal: 0,
    }),
  );
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
      ['Read how it starts', '300'],
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
      createElement(DailyGameView, { today, offset: 0, say: () => {}, reload: () => {}, rulesSignal: 0 }),
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
    expect(html).toContain('aria-label="How it starts for 300 points"');
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

describe('the panel folded away', () => {
  const base = (game: DailyGame, results: boolean): ComponentProps<typeof DailyPanel> => ({
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
  });

  it('keeps what the reader can do while the game is on, and hides the feed', () => {
    const html = renderToStaticMarkup(createElement(DailyPanel, base(gameOf([]), false)));
    expect(html).toContain('aria-label="Show the panel"');
    expect(html).toContain('aria-expanded="false"');
    expect(html).not.toContain('Three movies from its map are showing.');
    expect(html).toContain('Tap a blank card to turn it over.');
  });

  it('keeps the countdown and the way back to the result', () => {
    const html = renderToStaticMarkup(createElement(DailyPanel, base(won(), true)));
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
