import { describe, expect, it } from 'vitest';
import type { DailyGame, DailyGuess, DailyMovie, DailyPerson, DailySlot } from './api';
import {
  DAY_OVER,
  SHEET_CHIP_PHONE_H,
  SHEET_CLOSE_PX,
  SHEET_LIT_TOP,
  SHEET_METRICS,
  SHEET_SPRING_EASE,
  SHEET_SPRING_MS,
  UNREACHABLE,
  hueColour,
} from './daily';
import {
  ALREADY_TRIED,
  CARD_DIM,
  CARD_LIT,
  CARD_TRIED,
  DIRECTOR_CHIP,
  GUESS_IT,
  NO_PICKS,
  SHEET_BOTTOM_PAD,
  SHEET_EDGE_LEFT,
  SHEET_EDGE_RIGHT,
  cardOpacity,
  moviesFailed,
  moviesKey,
  openedOn,
  openingScroll,
  phoneSheetHeight,
  placeable,
  ratingColumn,
  sheetChips,
  sheetChoice,
  sheetFoot,
  sheetLayout,
  sheetRows,
  sheetSizes,
  sheetTicks,
  sheetView,
  tabWrap,
  titleNames,
  togglePick,
  widenReach,
  type SheetPicks,
  type SheetState,
} from './dailyMovies';
import { AXIS_H, DEFAULT_SETTINGS, R_HI, R_LO, layoutGrid, xOf, type GridPayload } from './grid';
import matrix from './fixtures/matrix-grid.json';
import { CLOSE_AT } from './sheet';
import css from './grid.css?raw';

// The Matrix, part way through: the sixth, fifth and fourth billed are
// showing, the decade is bought and Bound was guessed wrong. Hues are the
// handoff's, by place on the movie: the star first, then the directors.
const person = (id: string, name: string, hue: number, photo?: string): DailyPerson => ({ id, name, hue, photo });
const JOE = person('nm0001592', 'Joe Pantoliano', 205, 'https://image.tmdb.org/t/p/w185/joe.jpg');
const GLORIA = person('nm0287825', 'Gloria Foster', 78);
const HUGO = person('nm0915989', 'Hugo Weaving', 150);
const LANA = person('nm0905154', 'Lana Wachowski', 118);
const LILLY = person('nm0905152', 'Lilly Wachowski', 255);

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

// Joe Pantoliano's movies, as the server sends them: today's is among
// them, and nothing says which.
const MOVIES: DailyMovie[] = [
  movie('tt0086200', 'Risky Business', 1983, 6.8, { genres: ['Comedy', 'Romance'] }),
  movie('tt0089218', 'The Goonies', 1985, 7.7, { genres: ['Adventure', 'Comedy', 'Family'] }),
  movie('tt0092965', 'Empire of the Sun', 1987, 7.7, { genres: ['Action', 'Drama', 'History'] }),
  movie('tt0095631', 'Midnight Run', 1988, 7.5, { genres: ['Action', 'Comedy', 'Crime'] }),
  movie('tt0106977', 'The Fugitive', 1993, 7.8, { genres: ['Action', 'Crime', 'Drama'] }),
  movie('tt0115736', 'Bound', 1996, 7.3, { genres: ['Crime', 'Romance', 'Thriller'], dir: true }),
  movie('tt0133093', 'The Matrix', 1999, 8.7, { genres: ['Action', 'Sci-Fi'], on: [0, 1, 2], dir: true }),
  movie('tt0209144', 'Memento', 2000, 8.4, { genres: ['Mystery', 'Thriller'] }),
];

function stateOf(over: Partial<SheetState> = {}): SheetState {
  const movies = over.movies === undefined ? MOVIES : over.movies;
  return {
    game: gameOf(),
    person: JOE.id,
    movies,
    layout: movies ? sheetLayout(movies, 540, false) : null,
    picks: NO_PICKS,
    pick: null,
    buying: null,
    reach: null,
    theme: 'dark',
    ...over,
  };
}

const cardOf = (s: SheetState, id: string) => sheetView(s).cards.find((c) => c.id === id)!;

describe('the small map', () => {
  it('is laid out at the handoff’s numbers: the 112px card on a panel, 100px on a phone', () => {
    expect(sheetSizes(false)).toEqual({
      cardW: 112,
      cardH: 42,
      posterW: 24,
      posterH: 36,
      railW: 44,
      gap: 4,
      padTop: 3,
      padBottom: 3,
      gapMark: 4,
      edgeLeft: 4,
      edgeRight: 8,
      bottomPad: 12,
    });
    expect(sheetSizes(true)).toMatchObject({ cardW: 100, cardH: 42, railW: 40, posterH: 36 });
    // The same numbers daily.ts holds the sheet to.
    expect(sheetSizes(true)).toMatchObject(SHEET_METRICS.phone);
    expect(sheetSizes(false)).toMatchObject(SHEET_METRICS.desktop);
  });

  it('runs its rating from 3.5 to 9.2, the lowest card 4px past the rail and the highest 8px short of the edge', () => {
    const l = sheetLayout(
      [movie('tt1', 'Low', 1990, 3.5), movie('tt2', 'High', 1990, 9.2), movie('tt3', 'Off the top', 1991, 9.8)],
      540,
      false,
    );
    const at = (id: string) => l.cards.find((c) => c.film.id === id)!;
    expect(at('tt1').left).toBe(44 + SHEET_EDGE_LEFT);
    expect(at('tt2').left + 112).toBe(540 - SHEET_EDGE_RIGHT);
    // Clamped, as on the page's map.
    expect(at('tt3').left).toBe(at('tt2').left);
    expect(l.plotW).toBe(540);
    expect(Math.round(xOf(R_LO, l.metrics))).toBe(44 + 4 + 56);
    expect(Math.round(xOf(R_HI, l.metrics))).toBe(540 - 8 - 56);
  });

  it('packs a year’s cards into rows 3px from their edges, 4px apart, under the 26px axis', () => {
    // Three movies of one year at the same rating need three lanes.
    const same = [1, 2, 3].map((i) => movie(`tt${i}`, `Same ${i}`, 1990, 7));
    const l = sheetLayout([...same, movie('tt9', 'Later', 1991, 7)], 540, false);
    expect(l.rows[0].top).toBe(AXIS_H);
    expect(l.rows[0].lanes).toBe(3);
    expect(l.rows[0].height).toBe(3 + 3 * (42 + 4) + 3);
    expect(l.cards.filter((c) => c.film.year === 1990).map((c) => c.top)).toEqual([29, 75, 121]);
    // The next year follows straight on: no jump to mark.
    expect(l.rows[1].top).toBe(l.rows[0].top + l.rows[0].height);
    expect(l.plotH).toBe(l.rows[1].top + l.rows[1].height + SHEET_BOTTOM_PAD);
  });

  it('marks a jump in the years with 4px, not the map’s 10', () => {
    const l = sheetLayout([movie('tt1', 'A', 1990, 7), movie('tt2', 'B', 1995, 7)], 540, false);
    expect(l.rows[1].top).toBe(l.rows[0].top + l.rows[0].height + 4);
  });

  it('marks no card as the searched one, and lights no year for it', () => {
    const l = sheetLayout(MOVIES, 540, false);
    expect(l.anchor).toBeNull();
    expect(l.cards.every((c) => !c.film.isAnchor)).toBe(true);
    expect(l.rows.every((r) => !r.anchorYear && !r.isBreak)).toBe(true);
    expect(l.cards).toHaveLength(MOVIES.length);
  });

  it('leaves out only a movie it cannot place, and counts the same cards it draws', () => {
    const odd = [...MOVIES, movie('tt0', 'No year', 0, 7), movie('ttn', 'No rating', 1990, Number.NaN)];
    expect(placeable(odd).map((m) => m.id)).toEqual(MOVIES.map((m) => m.id));
    expect(sheetLayout(odd, 540, false).cards).toHaveLength(MOVIES.length);
    expect(sheetView(stateOf({ movies: odd, layout: sheetLayout(odd, 540, false) })).sub).toBe(
      '3 of 8 movies lit · on Cinedikt',
    );
  });

  it('holds together at a width it has not measured yet', () => {
    expect(sheetLayout(MOVIES, 0, true).plotW).toBe(300);
  });

  it('leaves the page’s map exactly as it was: its own numbers given as sizes lay it out the same', () => {
    // The sizes option only names what layoutGrid already used. Given the
    // map's own card, rail and gaps, it must reproduce the map to the
    // pixel; GridMap, which passes none, is untouched.
    const real = matrix as unknown as GridPayload;
    const s = { ...DEFAULT_SETTINGS, showUnrated: false };
    const own = layoutGrid(real, 1280, s);
    const sized = layoutGrid(real, 1280, s, undefined, undefined, {
      cardW: 168,
      cardH: 90,
      posterW: 50,
      posterH: 78,
      railW: 72,
      gap: 6,
      padTop: 12,
      padBottom: 6,
      gapMark: 10,
      edgeLeft: 10,
      edgeRight: 16,
      bottomPad: 110,
    });
    expect(sized.rows).toEqual(own.rows);
    expect(sized.cards).toEqual(own.cards);
    expect(sized.plotH).toBe(own.plotH);
    expect(sized.lines).toEqual(own.lines);
  });
});

describe('who is on the sheet', () => {
  it('always has the person it opened on, who nobody can switch off', () => {
    const game = gameOf();
    expect(openedOn(game, JOE.id)?.slot).toBe(0);
    expect(openedOn(game, 'nm9999999')).toBeNull();
    expect(sheetChoice(game, JOE.id, NO_PICKS)).toEqual({ slots: [0], director: false });
    expect(sheetChoice(game, GLORIA.id, NO_PICKS)).toEqual({ slots: [1], director: false });
  });

  it('adds another name only once their overlap is the reader’s', () => {
    const picked: SheetPicks = { others: [GLORIA.id], director: false };
    expect(sheetChoice(gameOf(), JOE.id, picked).slots).toEqual([0]);
    expect(sheetChoice(gameOf({ overlaps: [GLORIA.id] }), JOE.id, picked).slots).toEqual([0, 1]);
  });

  it('adds the directors only once Director has been bought', () => {
    const picked: SheetPicks = { others: [], director: true };
    expect(sheetChoice(gameOf(), JOE.id, picked).director).toBe(false);
    expect(sheetChoice(gameOf({ facts: { director: [LANA, LILLY] } }), JOE.id, picked).director).toBe(true);
  });

  it('switches a name or the directors either way', () => {
    const once = togglePick(NO_PICKS, GLORIA.id);
    expect(once).toEqual({ others: [GLORIA.id], director: false });
    expect(togglePick(once, GLORIA.id)).toEqual(NO_PICKS);
    expect(togglePick(NO_PICKS, DIRECTOR_CHIP)).toEqual({ others: [], director: true });
  });

  it('names the person it opened on first, then the others in the cast’s order, then the directors', () => {
    const game = gameOf({ overlaps: [JOE.id, GLORIA.id, HUGO.id], facts: { director: [LANA, LILLY] } });
    expect(titleNames(game, JOE.id, NO_PICKS)).toEqual(['Joe Pantoliano']);
    expect(titleNames(game, JOE.id, { others: [HUGO.id, GLORIA.id], director: false })).toEqual([
      'Joe Pantoliano',
      'Gloria Foster',
      'Hugo Weaving',
    ]);
    expect(titleNames(game, HUGO.id, { others: [JOE.id], director: true })).toEqual([
      'Hugo Weaving',
      'Joe Pantoliano',
      'Lana Wachowski and Lilly Wachowski',
    ]);
    const s = stateOf({ game: gameOf({ overlaps: [GLORIA.id] }), picks: { others: [GLORIA.id], director: false } });
    expect(sheetView(s).title).toBe('Movies with Joe Pantoliano and Gloria Foster');
    expect(sheetView(stateOf()).title).toBe('Joe Pantoliano’s movies');
  });
});

describe('the chips', () => {
  it('lead with the person it opened on, always on and not switchable, then the others by their place in the cast', () => {
    const chips = sheetChips(gameOf(), GLORIA.id, NO_PICKS, null, 'dark');
    expect(chips.map((c) => c.key)).toEqual([GLORIA.id, JOE.id, HUGO.id]);
    expect(chips[0]).toMatchObject({ label: 'Gloria Foster', price: '', on: true, act: 'none', can: false, faint: false });
  });

  it('sell another showing name for 250, at half strength when the points cannot cover it', () => {
    const [, gloria] = sheetChips(gameOf({ pts: 600 }), JOE.id, NO_PICKS, null, 'dark');
    expect(gloria).toMatchObject({ label: '+ Gloria Foster', price: '−250', act: 'buy', can: true, faint: false, on: false });
    expect(gloria.aria).toBe('Add Gloria Foster to light only the movies they share. It costs 250 points.');
    // A purchase must leave a point: 250 cannot buy 250.
    expect(sheetChips(gameOf({ pts: 251 }), JOE.id, NO_PICKS, null, 'dark')[1].faint).toBe(false);
    expect(sheetChips(gameOf({ pts: 250 }), JOE.id, NO_PICKS, null, 'dark')[1]).toMatchObject({ can: false, faint: true });
    expect(sheetChips(gameOf({ phase: 'done' }), JOE.id, NO_PICKS, null, 'dark')[1]).toMatchObject({ can: false, faint: true });
  });

  it('hold every name for sale still while one is being bought, without fading them', () => {
    const chips = sheetChips(gameOf(), JOE.id, NO_PICKS, GLORIA.id, 'dark');
    expect(chips.slice(1).map((c) => [c.can, c.faint])).toEqual([
      [false, false],
      [false, false],
    ]);
  });

  it('make a bought name a free switch for the rest of the game', () => {
    const game = gameOf({ overlaps: [GLORIA.id], pts: 120 });
    const off = sheetChips(game, JOE.id, NO_PICKS, null, 'dark')[1];
    expect(off).toMatchObject({ label: 'Gloria Foster', price: '', act: 'toggle', can: true, faint: false, on: false });
    const on = sheetChips(game, JOE.id, { others: [GLORIA.id], director: false }, null, 'dark')[1];
    expect(on.on).toBe(true);
  });

  it('add the directors’ chip once Director is bought: free, square-faced, naming them all', () => {
    expect(sheetChips(gameOf(), JOE.id, NO_PICKS, null, 'dark').some((c) => c.key === DIRECTOR_CHIP)).toBe(false);
    const chips = sheetChips(gameOf({ facts: { director: [LANA, LILLY] } }), JOE.id, NO_PICKS, null, 'light');
    const dir = chips[chips.length - 1];
    expect(dir).toMatchObject({
      key: DIRECTOR_CHIP,
      label: 'Lana Wachowski and Lilly Wachowski',
      director: true,
      act: 'toggle',
      can: true,
      on: false,
      person: LANA,
      tone: hueColour(118, 'light'),
    });
  });

  it('carry each person’s own colour for the theme, and initials for a face with no photo', () => {
    const [joe, gloria] = sheetChips(gameOf(), JOE.id, NO_PICKS, null, 'dark');
    expect(joe.tone).toBe('oklch(0.76 0.13 205)');
    expect(gloria.tone).toBe('oklch(0.76 0.13 78)');
    expect(sheetChips(gameOf(), JOE.id, NO_PICKS, null, 'light')[0].tone).toBe('oklch(0.56 0.16 205)');
    expect([joe.code, gloria.code]).toEqual(['JP', 'GF']);
  });
});

describe('what is lit', () => {
  const ids = (s: SheetState) =>
    sheetView(s)
      .cards.filter((c) => c.lit)
      .map((c) => c.title);

  it('lights the person’s movies inside the decade bought', () => {
    expect(ids(stateOf())).toEqual(['The Fugitive', 'Bound', 'The Matrix']);
  });

  it('narrows to the five years once they are bought', () => {
    expect(ids(stateOf({ game: gameOf({ facts: { decade: 1990, years: 1995 } }) }))).toEqual(['Bound', 'The Matrix']);
  });

  it('lights only inside the rating band bought, its ceiling not included', () => {
    expect(ids(stateOf({ game: gameOf({ facts: { rating: 2 } }) }))).toEqual([
      'The Goonies',
      'Empire of the Sun',
      'Midnight Run',
      'The Fugitive',
      'Bound',
    ]);
    expect(ids(stateOf({ game: gameOf({ facts: { rating: 3 } }) }))).toEqual(['The Matrix', 'Memento']);
  });

  it('wants every one of the answer’s genres once Genre is bought', () => {
    expect(ids(stateOf({ game: gameOf({ facts: { genre: ['Action', 'Crime'] } }) }))).toEqual([
      'Midnight Run',
      'The Fugitive',
    ]);
  });

  it('never draws the length: a bought length lights the same cards as none', () => {
    expect(ids(stateOf({ game: gameOf({ facts: { length: 2 } }) }))).toEqual(MOVIES.map((m) => m.title));
  });

  it('wants every name switched on, and the directors’ chip any of the directors', () => {
    const both = stateOf({ game: gameOf({ facts: {}, overlaps: [HUGO.id] }), picks: { others: [HUGO.id], director: false } });
    expect(ids(both)).toEqual(['The Matrix']);
    const dir = stateOf({ game: gameOf({ facts: { director: [LANA, LILLY] } }), picks: { others: [], director: true } });
    expect(ids(dir)).toEqual(['Bound', 'The Matrix']);
  });

  it('draws lit cards whole, a guessed one at 0.55, and the rest at 0.14', () => {
    expect([CARD_LIT, CARD_TRIED, CARD_DIM]).toEqual([1, 0.55, 0.14]);
    expect(cardOpacity(true, false)).toBe(1);
    expect(cardOpacity(true, true)).toBe(0.55);
    expect(cardOpacity(false, true)).toBe(0.14);
    const s = stateOf();
    expect(cardOf(s, 'tt0115736')).toMatchObject({ lit: true, tried: true, opacity: 0.55 });
    expect(cardOf(s, 'tt0106977')).toMatchObject({ lit: true, tried: false, opacity: 1 });
    expect(cardOf(s, 'tt0086200')).toMatchObject({ lit: false, tried: false, opacity: 0.14 });
    expect(cardOf(s, 'tt0115736').aria).toBe('Bound, 1996, rated 7.3, already tried');
    expect(cardOf(s, 'tt0086200').aria).toBe('Risky Business, 1983, rated 6.8, dimmed');
  });

  it('draws today’s movie by the same rule as every other card', () => {
    // Two movies alike but for which is today's — and nothing the server
    // sends says which. Their cards differ only in what each movie is.
    const a = movie('tt0000001', 'Alpha', 1990, 7.1, { genres: ['Drama'] });
    const b = movie('tt0000002', 'Beta', 1991, 7.1, { genres: ['Drama'] });
    const s = stateOf({ movies: [a, b], layout: sheetLayout([a, b], 540, false) });
    const [ca, cb] = sheetView(s).cards;
    const strip = ({ id: _i, title: _t, aria: _a, top: _top, ...rest }: typeof ca) => rest;
    expect(strip(ca)).toEqual(strip(cb));
  });

  it('says how much of the map is lit, of every card on it', () => {
    expect(sheetView(stateOf()).sub).toBe('3 of 8 movies lit · on Cinedikt');
    expect(sheetView(stateOf({ movies: null })).sub).toBe('');
    expect(sheetView(stateOf({ movies: null })).cards).toEqual([]);
  });
});

describe('the bands drawn for the facts', () => {
  it('washes the years inside the decade bought, on the map’s own striped and ruled bands', () => {
    const rows = sheetRows({ decade: 1990 }, sheetLayout(MOVIES, 540, false));
    expect(rows.map((r) => [r.year, r.inYears])).toEqual([
      [1983, false],
      [1985, false],
      [1987, false],
      [1988, false],
      [1993, true],
      [1996, true],
      [1999, true],
      [2000, false],
    ]);
    expect(rows[1].band).toBe('cd-band cd-band-odd');
    expect(rows[4].band).toBe('cd-band cd-msheet-band-in');
    expect(rows[5].band).toBe('cd-band cd-band-odd cd-msheet-band-in');
    expect(rows[7].band).toBe('cd-band cd-band-odd cd-band-decade');
    expect(sheetRows({}, sheetLayout(MOVIES, 540, false)).some((r) => r.inYears)).toBe(false);
  });

  it('centres each year’s label on its first lane', () => {
    const [row] = sheetRows({}, sheetLayout(MOVIES, 540, false));
    expect(row.labelTop).toBe(3 + 42 / 2 - 7);
  });

  it('draws the rating band bought as a column between its floor and ceiling, clamped to the axis', () => {
    const l = sheetLayout(MOVIES, 540, false);
    expect(ratingColumn({}, l)).toBeNull();
    const x = (r: number) => Math.round(xOf(r, l.metrics));
    expect(ratingColumn({ rating: 2 }, l)).toEqual({ left: x(7), width: x(8) - x(7) });
    expect(ratingColumn({ rating: 3 }, l)).toEqual({ left: x(8), width: x(R_HI) - x(8) });
    expect(ratingColumn({ rating: 0 }, l)).toEqual({ left: x(R_LO), width: x(6) - x(R_LO) });
  });

  it('labels the axis 4 to 9, each label centred on its gridline', () => {
    const l = sheetLayout(MOVIES, 540, false);
    const ticks = sheetTicks(l);
    expect(ticks.map((t) => t.label)).toEqual(['4', '5', '6', '7', '8', '9']);
    for (const t of ticks) {
      expect(t.x).toBe(Math.round(xOf(t.rating, l.metrics)));
      expect(t.labelLeft).toBe(t.x - 14);
    }
  });
});

describe('where it opens', () => {
  it('scrolls the first lit card to 36px from the top', () => {
    const v = sheetView(stateOf());
    const fugitive = v.cards.find((c) => c.title === 'The Fugitive')!;
    expect(v.scrollTo).toBe(fugitive.top - SHEET_LIT_TOP);
  });

  it('takes the highest lit card, whatever order the cards come in, and never scrolls above the top', () => {
    expect(
      openingScroll([
        { top: 500, lit: true },
        { top: 80, lit: false },
        { top: 300, lit: true },
      ]),
    ).toBe(264);
    expect(openingScroll([{ top: 29, lit: true }])).toBe(0);
    expect(openingScroll([{ top: 400, lit: false }])).toBe(0);
  });

  it('loads posters only for the stretch the reader has been near', () => {
    expect(sheetView(stateOf()).cards.some((c) => c.warm)).toBe(false);
    // 1983's card is at 29; 1985's, after a 4px jump, at 85.
    const near = sheetView(stateOf({ reach: { top: 0, bottom: 60 } })).cards;
    expect(near.filter((c) => c.warm).map((c) => c.title)).toEqual(['Risky Business']);
  });

  it('widens what it has loaded as the reader scrolls, never narrowing it', () => {
    const a = widenReach(null, { top: 0, bottom: 600 });
    expect(a).toEqual({ top: 0, bottom: 600 });
    expect(widenReach(a, { top: 100, bottom: 500 })).toBe(a);
    expect(widenReach(a, { top: 300, bottom: 900 })).toEqual({ top: 0, bottom: 900 });
  });
});

describe('the footer', () => {
  it('says how to guess, at the next wrong guess’s price, with nothing tapped', () => {
    expect(sheetFoot(null, gameOf())).toEqual({ kind: 'hint', text: 'Tap a movie to guess it. A wrong guess costs 150.' });
    expect(sheetFoot(null, gameOf({ nextCost: 1200 }))).toEqual({
      kind: 'hint',
      text: 'Tap a movie to guess it. A wrong guess costs 1,200.',
    });
  });

  it('offers the card tapped, with Guess it', () => {
    expect(sheetFoot(MOVIES[4], gameOf())).toEqual({
      kind: 'pick',
      id: 'tt0106977',
      title: 'The Fugitive',
      line: '1993 · IMDb 7.8',
      label: GUESS_IT,
      can: true,
    });
    expect(GUESS_IT).toBe('Guess it');
  });

  it('says Already tried, and cannot be pressed, for a movie guessed already', () => {
    expect(sheetFoot(MOVIES[5], gameOf())).toMatchObject({ label: ALREADY_TRIED, can: false });
    expect(ALREADY_TRIED).toBe('Already tried');
  });

  it('cannot guess once the game is over', () => {
    expect(sheetFoot(MOVIES[4], gameOf({ phase: 'done' }))).toMatchObject({ label: GUESS_IT, can: false });
  });

  it('holds Guess it while a name is being bought, as it holds the names for sale', () => {
    // The page makes one move at a time: a guess pressed while the
    // overlap is on its way would close the sheet on a guess never sent.
    const buying = stateOf({ pick: 'tt0106977', buying: 'nm0287825' });
    expect(sheetView(buying).foot).toMatchObject({ kind: 'pick', label: GUESS_IT, can: false });
    expect(sheetView({ ...buying, buying: null }).foot).toMatchObject({ kind: 'pick', label: GUESS_IT, can: true });
    expect(sheetFoot(MOVIES[4], gameOf(), 'nm0287825')).toMatchObject({ can: false });
  });

  it('rings the card tapped', () => {
    const s = stateOf({ pick: 'tt0106977' });
    expect(cardOf(s, 'tt0106977').picked).toBe(true);
    expect(sheetView(s).cards.filter((c) => c.picked)).toHaveLength(1);
    expect(sheetView(s).foot.kind).toBe('pick');
  });
});

describe('the phone sheet', () => {
  it('is 90% of the visual viewport tall', () => {
    expect(phoneSheetHeight(540)).toBe(486);
    expect(phoneSheetHeight(-1)).toBe(0);
  });

  it('closes past the same 90px as the app’s other sheets', () => {
    expect(SHEET_CLOSE_PX).toBe(90);
    expect(CLOSE_AT).toBe(SHEET_CLOSE_PX);
  });

  it('springs back over .25s on the handoff’s curve, and at once under reduced motion', () => {
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    const phone = /\.cd-msheet-phone \{([^}]*)\}/.exec(text)?.[1] ?? '';
    expect(phone).toContain(`transition: transform ${SHEET_SPRING_MS / 1000}s ${SHEET_SPRING_EASE};`);
    expect(phone).toContain('border-radius: 20px 20px 0 0;');
    const still = [...text.matchAll(/@media \(prefers-reduced-motion: reduce\) \{([\s\S]*?)\n\}/g)].map((m) => m[1]);
    expect(still.some((b) => /\.cd-msheet-phone,[\s\S]*?\{\s*transition: none;/.test(b))).toBe(true);
  });
});

describe('the stylesheet', () => {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const rule = (sel: string) => {
    const esc = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`(^|\\n)${esc} \\{([^}]*)\\}`).exec(text)?.[2] ?? '';
  };

  it('dims behind the sheet in black at 0.45, as a token, on either ground', () => {
    expect(text).toMatch(/:root \{\s*--scrimDaily: rgba\(0, 0, 0, 0\.45\);\s*\}/);
    expect(rule('.cd-msheet-scrim')).toContain('background: var(--scrimDaily);');
  });

  it('holds the panel 12px in, at most 560px wide, with 20px corners', () => {
    const panel = rule('.cd-msheet');
    for (const d of ['top: 12px;', 'right: 12px;', 'bottom: 12px;', 'width: min(560px, calc(100vw - 24px));', 'border-radius: 20px;'])
      expect(panel).toContain(d);
    expect(panel).toContain('box-shadow: inset 0 0 0 1px var(--ln2), var(--pop);');
  });

  it('draws the chips 34px tall, and 40px wherever a finger may be: a phone either way up, a tablet, a touch screen', () => {
    expect(rule('.cd-msheet-chip')).toContain('height: 34px;');
    const touch = /@media \(max-width: 1023\.98px\), \(max-height: 499\.98px\), \(pointer: coarse\) \{([\s\S]*?)\n\}/g;
    const blocks = [...text.matchAll(touch)].map((m) => m[1]);
    expect(blocks.some((b) => /\.cd-msheet-chip \{\s*height: 40px;\s*\}/.test(b))).toBe(true);
    expect(`${SHEET_CHIP_PHONE_H}px`).toBe('40px');
  });

  it('reaches 44px round the close button and Try again, at every size', () => {
    // The close button is 40 to the eye and Try again 36: a transparent
    // ::before on a position: relative control makes up the rest.
    for (const [sel, inset, eye] of [
      ['.cd-msheet-close', '-2px', 40],
      ['.cd-msheet-retry', '-4px 0', 36],
    ] as const) {
      expect(rule(sel), sel).toContain('position: relative;');
      expect(rule(`${sel}::before`), sel).toMatch(new RegExp(`content: '';\\s*position: absolute;\\s*inset: ${inset};`));
      const grow = 2 * Math.abs(parseFloat(inset));
      expect(eye + grow, sel).toBeGreaterThanOrEqual(44);
    }
  });

  it('draws the grabber 38 by 5, 8px from the top', () => {
    expect(rule('.cd-msheet-grab span')).toMatch(/width: 38px;\s*height: 5px;\s*border-radius: 3px;\s*background: var\(--ln3\);/);
    expect(rule('.cd-msheet-grab')).toContain('padding-top: 8px;');
  });

  it('fades cards in and out over .25s, and keeps the axis 26px tall and pinned', () => {
    expect(rule('.cd-msheet-card')).toMatch(/opacity 0\.25s ease/);
    expect(rule('.cd-msheet-axis')).toMatch(/position: sticky;\s*top: 0;[\s\S]*height: 26px;/);
    expect(rule('.cd-msheet-card-picked')).toContain('box-shadow: inset 0 0 0 2px var(--acc), var(--sh);');
    expect(rule('.cd-msheet-range')).toContain('border-left: 1px dashed var(--acc);');
  });
});

describe('asking for the movies', () => {
  it('asks again when the showing names or Director change, and not otherwise', () => {
    const base = moviesKey(gameOf());
    expect(moviesKey(gameOf({ pts: 100, overlaps: [GLORIA.id] }))).toBe(base);
    expect(moviesKey(gameOf({ slots: [shown(0, JOE), shown(1, GLORIA), shown(2, HUGO), shown(3, LANA), hidden(4), hidden(5)] }))).not.toBe(base);
    expect(moviesKey(gameOf({ facts: { decade: 1990, director: [LANA] } }))).not.toBe(base);
  });

  it('says what went wrong in the refusal’s own words, or that Cinedikt could not be reached', () => {
    expect(moviesFailed('bad')).toBe('That name isn’t showing yet.');
    expect(moviesFailed('day')).toBe(DAY_OVER);
    expect(moviesFailed('stale')).toBe(UNREACHABLE);
    expect(moviesFailed(null)).toBe(UNREACHABLE);
  });
});

describe('the focus', () => {
  it('goes round inside the sheet, from the last control to the first and back', () => {
    expect(tabWrap(4, 5, false)).toBe(0);
    expect(tabWrap(0, 5, true)).toBe(4);
    expect(tabWrap(2, 5, false)).toBeNull();
    expect(tabWrap(2, 5, true)).toBeNull();
  });

  it('starts from the sheet itself, as it opens', () => {
    expect(tabWrap(-1, 5, false)).toBe(0);
    expect(tabWrap(-1, 5, true)).toBe(4);
    expect(tabWrap(-1, 0, false)).toBeNull();
  });
});
