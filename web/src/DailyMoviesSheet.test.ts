import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { DailyGame, DailyGuess, DailyMovie, DailyPerson, DailySlot } from './api';
import { DailyMoviesSheet, MoviesSheetView, type MoviesSheetViewProps, type SheetHandlers } from './DailyMoviesSheet';
import { sheetLayout, sheetView, type SheetState } from './dailyMovies';

// The sheet as it is drawn, rendered without a DOM: no effect runs, so the
// sheet itself is seen as it opens, before its movies come, and every
// other state through MoviesSheetView, which takes the sheet as it stands.

const person = (id: string, name: string, hue: number, photo?: string): DailyPerson => ({ id, name, hue, photo });
const JOE = person('nm0001592', 'Joe Pantoliano', 205, 'https://image.tmdb.org/t/p/w185/joe.jpg');
const GLORIA = person('nm0287825', 'Gloria Foster', 78);
const HUGO = person('nm0915989', 'Hugo Weaving', 150);

const shown = (slot: number, p: DailyPerson): DailySlot => ({ slot, shown: true, person: p, via: slot ? 'next' : 'start' });
const hidden = (slot: number): DailySlot => ({ slot, shown: false });

const BOUND: DailyGuess = {
  id: 'tt0115736',
  title: 'Bound',
  year: 1996,
  cost: 100,
  shared: [0],
  sameDecade: true,
  sharesGenre: true,
  warmth: 2,
};

function gameOf(over: Partial<DailyGame> = {}): DailyGame {
  return {
    phase: 'play',
    pts: 600,
    seq: 4,
    startedAt: '2026-10-09T09:00:00Z',
    finishedAt: null,
    won: false,
    gaveUp: false,
    nextCost: 150,
    slots: [shown(0, JOE), shown(1, GLORIA), shown(2, HUGO), hidden(3), hidden(4), hidden(5)],
    facts: { decade: 1990 },
    log: [
      { type: 'next', cost: 100, slot: 1 },
      { type: 'fact', kind: 'decade', cost: 100 },
      { type: 'guess', cost: 100, guess: BOUND },
      // The game's one Movies map: Joe Pantoliano's, the sheet these draw.
      { type: 'sheet', person: JOE.id },
    ],
    sheet: JOE.id,
    end: null,
    ...over,
  };
}

const read = (id: string, title: string, year: number, rating: number): DailyMovie => ({ id, title, year, rating, genres: ['Drama'] });

// With the 1990s bought: the three inside readable, today's among them,
// and the rest blank, as the server sends them.
const MOVIES: DailyMovie[] = [
  { year: 1983, at: 7 },
  { year: 1985, at: 7.5 },
  read('tt0106977', 'The Fugitive', 1993, 7.8),
  read('tt0115736', 'Bound', 1996, 7.3),
  read('tt0133093', 'The Matrix', 1999, 8.7),
  { year: 2000, at: 8.5 },
];

// Before any range: every one of them blank.
const BLANKS: DailyMovie[] = [
  { year: 1983, at: 7 },
  { year: 1985, at: 7.5 },
  { year: 1993, at: 8 },
  { year: 1996, at: 7.5 },
  { year: 1999, at: 8.5 },
  { year: 2000, at: 8.5 },
];

const none: SheetHandlers = { close() {}, card() {}, guess() {}, retry() {} };

function stateOf(over: Partial<SheetState> = {}): SheetState {
  const movies = over.movies === undefined ? MOVIES : over.movies;
  return {
    game: gameOf(),
    person: JOE.id,
    movies,
    layout: movies ? sheetLayout(movies, 540, false) : null,
    pick: null,
    reach: null,
    theme: 'dark',
    ...over,
  };
}

const draw = (s: Partial<SheetState> = {}, props: Partial<MoviesSheetViewProps> = {}) =>
  renderToStaticMarkup(
    createElement(MoviesSheetView, {
      view: sheetView(stateOf(s)),
      phone: false,
      theme: 'dark',
      height: null,
      held: 0,
      loading: false,
      failed: null,
      on: none,
      ...props,
    }),
  );

const count = (html: string, needle: string) => html.split(needle).length - 1;

describe('the sheet as it opens', () => {
  const html = renderToStaticMarkup(
    createElement(DailyMoviesSheet, {
      no: 143,
      person: JOE.id,
      game: gameOf(),
      theme: 'dark',
      onGuess: () => {},
      onClose: () => {},
    }),
  );

  it('is a modal dialog named for its person, over a scrim', () => {
    expect(html).toContain('class="cd-msheet-scrim"');
    expect(html).toContain('role="dialog" aria-modal="true" aria-label="Joe Pantoliano’s movies" tabindex="-1"');
    expect(html).toContain('<h2 class="cd-msheet-title">Joe Pantoliano’s movies</h2>');
    expect(html).toContain('aria-label="Close"');
  });

  it('shows the legend and how to guess while the movies are on their way, and no chips to combine names', () => {
    expect(html).toContain('Titles show inside your ranges: 1990s. Today’s movie is one of these cards, but it isn’t marked.');
    expect(html).toContain('Tap a movie to guess it. A wrong guess costs 150.');
    expect(html).toContain('aria-busy="true"');
    expect(html).not.toContain('cd-msheet-card');
    expect(html).not.toContain('cd-msheet-chip');
    expect(html).not.toContain('Gloria Foster');
  });

  it('says the movies are coming in the map’s place, rather than leave it blank', () => {
    expect(html).toMatch(/aria-busy="true"[^>]*><div class="cd-msheet-note"><p>Loading their movies…<\/p><\/div><\/div>/);
    expect(html).not.toContain('cd-msheet-plot');
    const drawn = draw({ movies: null, layout: null }, { loading: true });
    expect(drawn).toContain('<p>Loading their movies…</p>');
    expect(draw()).not.toContain('Loading their movies');
  });

  it('is the side panel on a wide screen: no grabber, and no height of its own', () => {
    expect(html).toContain('class="cd-msheet"');
    expect(html).not.toContain('cd-msheet-grab');
  });
});

describe('the sheet on a phone', () => {
  it('comes up from the bottom at 90% of the visual viewport, with a grabber', () => {
    const html = draw({}, { phone: true, height: 486 });
    expect(html).toContain('class="cd-msheet cd-msheet-phone" style="height:486px"');
    expect(html).toContain('<div class="cd-msheet-grab" aria-hidden="true"><span></span></div>');
  });

  it('follows a finger down with no transition while it is held', () => {
    const html = draw({}, { phone: true, height: 486, held: 40 });
    expect(html).toContain('style="height:486px;transform:translateY(40px);transition:none"');
    expect(draw({}, { phone: true, height: 486, held: 0 })).not.toContain('translateY');
  });
});

describe('the header', () => {
  const html = draw();

  it('rings the face in the person’s colour, set inline as --tone', () => {
    expect(html).toContain('class="cd-msheet-head" style="--tone:oklch(0.76 0.13 205)"');
  });

  it('says how much can be read as a polite live region', () => {
    expect(html).toContain('<p class="cd-msheet-sub" aria-live="polite">3 of 6 movies readable · on Cinedikt</p>');
  });

  it('goes straight from the header to the legend, with no row of names between', () => {
    expect(html).toMatch(/<\/div><\/div><p class="cd-msheet-legend">/);
  });
});

describe('before any range is bought', () => {
  const html = draw({ game: gameOf({ facts: {} }), movies: BLANKS });

  it('counts the movies, and says titles only show inside the ranges bought', () => {
    expect(html).toContain('<p class="cd-msheet-sub" aria-live="polite">6 movies · on Cinedikt</p>');
    expect(html).toContain(
      '<p class="cd-msheet-legend">Titles only show inside the ranges you buy: the decade, the years, a rating range or the genre. Today’s movie is one of these cards.</p>',
    );
  });

  it('draws every card as a blank tile: an empty poster, no title, no rating, faded, and not to be pressed', () => {
    expect(count(html, 'class="cd-msheet-card cd-msheet-card-blank"')).toBe(6);
    expect(html).toMatch(
      /<button type="button" class="cd-msheet-card cd-msheet-card-blank" style="left:\d+px;top:\d+px;width:112px;height:42px;opacity:0.3" aria-label="A movie from 1983. Buy a range to read it" disabled=""><span class="cd-msheet-poster" aria-hidden="true"><\/span><\/button>/,
    );
    expect(html).not.toContain('cd-msheet-card-title');
    expect(html).not.toContain('cd-msheet-card-foot');
    expect(html).not.toContain('aria-pressed');
  });

  it('points the reader at the facts under the card', () => {
    expect(html).toContain('<span class="cd-msheet-hint">Buy a range to read this map. The facts are under the card.</span>');
  });
});

describe('the map once a range is bought', () => {
  const html = draw({ game: gameOf({ facts: { decade: 1990, rating: 2 } }) });

  it('pins a rating axis, 4 to 9, over the years', () => {
    expect(html).toContain('<span class="cd-msheet-axis-title">Rating →</span>');
    for (const r of ['4', '5', '6', '7', '8', '9']) expect(html).toMatch(new RegExp(`class="cd-msheet-axis-label" style="left:\\d+px">${r}<`));
    expect(count(html, 'class="cd-msheet-line"')).toBe(6);
  });

  it('washes the years bought, and draws the rating bought as a column', () => {
    expect(html).toContain('class="cd-band cd-msheet-band-in"');
    expect(html).toContain('class="cd-msheet-year cd-msheet-year-in"');
    expect(count(html, 'cd-msheet-year-in')).toBe(3);
    expect(html).toMatch(/class="cd-msheet-range" style="left:\d+px;width:\d+px"/);
    expect(draw()).not.toContain('cd-msheet-range');
  });

  it('draws the readable cards whole, the guessed one at 0.55, and the rest blank at 0.3', () => {
    const plain = draw();
    expect(count(plain, 'class="cd-msheet-card"')).toBe(3);
    expect(count(plain, 'cd-msheet-card-blank')).toBe(3);
    expect(plain).toMatch(/opacity:1" aria-label="The Fugitive, 1993, rated 7\.8" aria-pressed="false"/);
    expect(plain).toMatch(/opacity:0\.55" aria-label="Bound, 1996, rated 7\.3, already tried"/);
    expect(plain).toMatch(/opacity:0\.3" aria-label="A movie from 2000, outside your ranges" disabled=""/);
    expect(plain).toContain('<span class="cd-msheet-card-title">The Matrix</span><span class="cd-msheet-card-foot">8.7</span>');
  });

  it('crosses only the movie already guessed', () => {
    const plain = draw();
    expect(count(plain, '<span class="cd-msheet-tried">✕</span>')).toBe(1);
    expect(plain).toContain('7.3<span class="cd-msheet-tried">✕</span>');
  });

  it('gives every readable card a stand-in poster in its own title’s hue, today’s as much as any', () => {
    const plain = draw();
    expect(count(plain, '<span class="cd-msheet-poster" style="--poster-fill:linear-gradient(165deg, oklch(')).toBe(3);
    expect(plain).not.toContain('<img class="cd-msheet-poster"');
  });
});

describe('the footer', () => {
  it('says how to guess with nothing tapped', () => {
    const html = draw();
    expect(html).toContain(
      '<div class="cd-msheet-say" aria-live="polite"><span class="cd-msheet-hint">Tap a movie to guess it. A wrong guess costs 150.</span></div>',
    );
    expect(html).not.toContain('cd-msheet-guess');
  });

  it('rings the card tapped and offers it, with Guess it', () => {
    const html = draw({ pick: 'tt0106977' });
    expect(html).toMatch(/class="cd-msheet-card cd-msheet-card-picked"[^>]*aria-pressed="true"/);
    expect(count(html, 'aria-pressed="true"')).toBe(1);
    expect(html).toContain('<span class="cd-msheet-pick-title">The Fugitive</span><span class="cd-msheet-pick-line">1993 · IMDb 7.8</span>');
    expect(html).toContain('<button type="button" class="cd-msheet-guess">Guess it</button>');
  });

  it('says Already tried, and cannot be pressed, for a movie guessed already', () => {
    expect(draw({ pick: 'tt0115736' })).toContain('<button type="button" class="cd-msheet-guess" disabled="">Already tried</button>');
  });
});

describe('when the movies cannot be had', () => {
  it('says so in the map’s place, with Try again', () => {
    const html = draw({ movies: null, layout: null }, { failed: 'That name isn’t showing yet.' });
    expect(html).toContain('<div class="cd-msheet-note"><p>That name isn’t showing yet.</p>');
    expect(html).toContain('class="cd-msheet-retry">Try again</button>');
    expect(html).not.toContain('cd-msheet-plot');
  });
});
