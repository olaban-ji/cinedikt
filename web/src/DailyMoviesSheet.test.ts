import { createElement, isValidElement, type ReactElement, type ReactNode } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { DailyGame, DailyGuess, DailyMovie, DailyPerson, DailySlot } from './api';
import { DailyMoviesSheet, MoviesSheetView, type MoviesSheetViewProps, type SheetHandlers } from './DailyMoviesSheet';
import { NO_PICKS, sheetLayout, sheetView, type SheetState } from './dailyMovies';

// The sheet as it is drawn, rendered without a DOM: no effect runs, so the
// sheet itself is seen as it opens, before its movies come, and every
// other state through MoviesSheetView, which takes the sheet as it stands.

const person = (id: string, name: string, hue: number, photo?: string): DailyPerson => ({ id, name, hue, photo });
const JOE = person('nm0001592', 'Joe Pantoliano', 205, 'https://image.tmdb.org/t/p/w185/joe.jpg');
const GLORIA = person('nm0287825', 'Gloria Foster', 78);
const HUGO = person('nm0915989', 'Hugo Weaving', 150);
const LANA = person('nm0905154', 'Lana Wachowski', 118);

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
    seq: 3,
    startedAt: '2026-10-09T09:00:00Z',
    finishedAt: null,
    won: false,
    gaveUp: false,
    nextCost: 150,
    slots: [shown(0, JOE), shown(1, GLORIA), shown(2, HUGO), hidden(3), hidden(4), hidden(5)],
    facts: { decade: 1990 },
    overlaps: [],
    log: [
      { type: 'next', cost: 100, slot: 1 },
      { type: 'fact', kind: 'decade', cost: 100 },
      { type: 'guess', cost: 100, guess: BOUND },
    ],
    end: null,
    ...over,
  };
}

const movie = (id: string, title: string, year: number, rating: number, over: Partial<DailyMovie> = {}): DailyMovie => ({
  id,
  title,
  year,
  rating,
  genres: ['Drama'],
  on: [0],
  ...over,
});

const MOVIES: DailyMovie[] = [
  movie('tt0086200', 'Risky Business', 1983, 6.8),
  movie('tt0089218', 'The Goonies', 1985, 7.7),
  movie('tt0106977', 'The Fugitive', 1993, 7.8),
  movie('tt0115736', 'Bound', 1996, 7.3),
  movie('tt0133093', 'The Matrix', 1999, 8.7, { on: [0, 1, 2] }),
  movie('tt0209144', 'Memento', 2000, 8.4),
];

const none: SheetHandlers = { close() {}, chip() {}, card() {}, guess() {}, retry() {} };

function stateOf(over: Partial<SheetState> = {}): SheetState {
  return {
    game: gameOf(),
    person: JOE.id,
    movies: MOVIES,
    layout: sheetLayout(MOVIES, 540, false),
    picks: NO_PICKS,
    pick: null,
    buying: null,
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
      onOverlap: () => Promise.resolve(true),
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

  it('shows the chips, the legend and how to guess while the movies are on their way', () => {
    expect(html).toContain('+ Gloria Foster');
    expect(html).toContain('Your facts are on the map: 1990s. Today’s movie is one of these cards, but it isn’t marked.');
    expect(html).toContain('Tap a movie to guess it. A wrong guess costs 150.');
    expect(html).toContain('aria-busy="true"');
    expect(html).not.toContain('cd-msheet-card');
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

describe('the header and the chips', () => {
  const html = draw();

  it('rings the face in the person’s colour, set inline as --tone', () => {
    expect(html).toContain('class="cd-msheet-head" style="--tone:oklch(0.76 0.13 205)"');
  });

  it('says how much is lit as a polite live region', () => {
    expect(html).toContain('<p class="cd-msheet-sub" aria-live="polite">3 of 6 movies lit · on Cinedikt</p>');
  });

  it('keeps the person it opened on switched on, and not to be switched off', () => {
    expect(html).toMatch(/class="cd-msheet-chip cd-msheet-chip-on" style="--tone:oklch\(0\.76 0\.13 205\)" aria-pressed="true" disabled=""/);
  });

  it('sells the other names by their price, saying what buying one does', () => {
    expect(html).toContain(
      'class="cd-msheet-chip" style="--tone:oklch(0.76 0.13 78)" aria-label="Add Gloria Foster to light only the movies they share. It costs 250 points."',
    );
    expect(html).toContain('+ Gloria Foster<span class="cd-msheet-price">−250</span>');
    expect(count(html, '<button type="button" class="cd-msheet-chip')).toBe(3);
  });

  it('fades a name the points cannot buy', () => {
    const poor = draw({ game: gameOf({ pts: 250 }) });
    expect(count(poor, 'cd-msheet-chip-faint')).toBe(2);
  });

  it('draws a bought name as a switch, and the directors’ chip with a director’s square face', () => {
    const html2 = draw({
      game: gameOf({ overlaps: [GLORIA.id], facts: { director: [LANA] } }),
      picks: { others: [GLORIA.id], director: false },
    });
    expect(html2).toMatch(/style="--tone:oklch\(0\.76 0\.13 78\)" aria-pressed="true">/);
    expect(html2).toContain('Movies with Joe Pantoliano and Gloria Foster');
    expect(html2).toMatch(/aria-pressed="false"><span class="cd-face cd-face-chip cd-face-square"[^]*?Lana Wachowski</);
  });

  it('moves the chips sideways under a mouse wheel, since the row hides its scrollbar', () => {
    const tree = MoviesSheetView({
      view: sheetView(stateOf()),
      phone: false,
      theme: 'dark',
      height: null,
      held: 0,
      loading: false,
      failed: null,
      on: none,
    });
    const row = findByClass(tree, 'cd-msheet-chips');
    expect(row).not.toBeNull();
    const onWheel = (row!.props as { onWheel?: (e: unknown) => void }).onWheel;
    expect(onWheel).toBeTypeOf('function');
    // A row with room to its right, as the scroller hands it to the
    // handler: a wheel down moves it along.
    const el = { scrollWidth: 900, clientWidth: 532, scrollLeft: 0 };
    onWheel!({ deltaX: 0, deltaY: 100, currentTarget: el, preventDefault() {} });
    expect(el.scrollLeft).toBe(100);
  });
});

/** The first element in a rendered tree with this class, walking into
 *  children but not into components, which are not called. */
function findByClass(node: ReactNode, name: string): ReactElement | null {
  if (Array.isArray(node)) {
    for (const n of node) {
      const hit = findByClass(n, name);
      if (hit) return hit;
    }
    return null;
  }
  if (!isValidElement(node)) return null;
  const props = node.props as { className?: string; children?: ReactNode };
  if (props.className?.split(' ').includes(name)) return node;
  return findByClass(props.children, name);
}

describe('the map', () => {
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

  it('draws every card, the lit whole, the guessed at 0.55 and the rest faded', () => {
    expect(count(html, 'class="cd-msheet-card"')).toBe(6);
    // 1990s and rated 7.0 to 7.9: The Fugitive whole, Bound guessed.
    expect(html).toMatch(/opacity:1" aria-label="The Fugitive, 1993, rated 7\.8"/);
    expect(html).toMatch(/opacity:0\.55" aria-label="Bound, 1996, rated 7\.3, already tried"/);
    expect(html).toMatch(/opacity:0\.14" aria-label="The Matrix, 1999, rated 8\.7, dimmed"/);
  });

  it('crosses only the movie already guessed', () => {
    expect(count(html, '<span class="cd-msheet-tried">✕</span>')).toBe(1);
    expect(html).toContain('7.3<span class="cd-msheet-tried">✕</span>');
  });

  it('gives every card a stand-in poster in its own title’s hue, today’s as much as any', () => {
    expect(count(html, '<span class="cd-msheet-poster" style="--poster-fill:linear-gradient(165deg, oklch(')).toBe(6);
    expect(html).not.toContain('<img class="cd-msheet-poster"');
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
    expect(count(html, 'aria-pressed="true"')).toBe(2);
    expect(html).toContain('<span class="cd-msheet-pick-title">The Fugitive</span><span class="cd-msheet-pick-line">1993 · IMDb 7.8</span>');
    expect(html).toContain('<button type="button" class="cd-msheet-guess">Guess it</button>');
  });

  it('says Already tried, and cannot be pressed, for a movie guessed already', () => {
    expect(draw({ pick: 'tt0115736' })).toContain('<button type="button" class="cd-msheet-guess" disabled="">Already tried</button>');
  });

  it('holds Guess it while a name is being bought', () => {
    expect(draw({ pick: 'tt0106977', buying: GLORIA.id })).toContain(
      '<button type="button" class="cd-msheet-guess" disabled="">Guess it</button>',
    );
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
