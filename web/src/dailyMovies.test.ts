import { describe, expect, it } from 'vitest';
import type { DailyBlankMovie, DailyGame, DailyGuess, DailyMovie, DailyPerson, DailyReadableMovie, DailySlot } from './api';
import { DAY_OVER, SHEET_CLOSE_PX, SHEET_METRICS, SHEET_READ_TOP, SHEET_SPRING_EASE, SHEET_SPRING_MS, UNREACHABLE } from './daily';
import {
  ALREADY_TRIED,
  CARD_BLANK,
  CARD_READ,
  CARD_TRIED,
  GUESS_IT,
  SHEET_BOTTOM_PAD,
  SHEET_EDGE_LEFT,
  SHEET_EDGE_RIGHT,
  cardOpacity,
  isReadable,
  moviesFailed,
  openedOn,
  openingScroll,
  phoneSheetHeight,
  ratingAt,
  ratingColumn,
  sheetEntries,
  sheetFoot,
  sheetLayout,
  sheetRows,
  sheetSizes,
  sheetTicks,
  sheetView,
  tabWrap,
  widenReach,
  type SheetCard,
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
    seq: 5,
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

const read = (id: string, title: string, year: number, rating: number): DailyReadableMovie => ({ id, title, year, rating });
const blank = (year: number, at: number): DailyBlankMovie => ({ year, at });

// Joe Pantoliano's movies, as the server sends them with the 1990s
// bought: the four inside the decade readable, today's among them and
// nothing to say which, and the four outside it blank, a year and a
// place on the rating axis apiece.
const MOVIES: DailyMovie[] = [
  blank(1983, 7),
  blank(1985, 7.5),
  blank(1987, 7.5),
  blank(1988, 7.5),
  read('tt0106977', 'The Fugitive', 1993, 7.8),
  read('tt0115736', 'Bound', 1996, 7.3),
  read('tt0133093', 'The Matrix', 1999, 8.7),
  blank(2000, 8.5),
];

// The same movies before any range is bought: every one blank.
const BLANKS: DailyMovie[] = MOVIES.map((m) => (isReadable(m) ? blank(m.year, Math.round(m.rating * 2) / 2) : m));

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

const readable = (s: SheetState) =>
  sheetView(s).cards.filter((c): c is Extract<SheetCard, { kind: 'readable' }> => c.kind === 'readable');
const cardOf = (s: SheetState, id: string) => readable(s).find((c) => c.id === id)!;

describe('the movies the server sends', () => {
  it('are readable when they have an id, and blank when they are only a year and a place', () => {
    expect(isReadable(MOVIES[4])).toBe(true);
    expect(isReadable(MOVIES[0])).toBe(false);
    expect(ratingAt(MOVIES[4])).toBe(7.8);
    expect(ratingAt(blank(1985, 7.5))).toBe(7.5);
  });

  it('are keyed by id, or a blank one by its year, its place and a count, every key its own', () => {
    const entries = sheetEntries([...MOVIES, blank(1985, 7.5)]);
    expect(entries.map((e) => e.key)).toEqual([
      '~1983:7.0:0',
      '~1985:7.5:0',
      '~1987:7.5:0',
      '~1988:7.5:0',
      'tt0106977',
      'tt0115736',
      'tt0133093',
      '~2000:8.5:0',
      '~1985:7.5:1',
    ]);
    // The same answer keys the same cards, so nothing is drawn afresh.
    expect(sheetEntries(MOVIES).map((e) => e.key)).toEqual(sheetEntries([...MOVIES]).map((e) => e.key));
  });

  it('leave out only a movie the map cannot place, and count the same cards it draws', () => {
    const odd = [...MOVIES, read('tt0', 'No year', 0, 7), read('ttn', 'No rating', 1990, Number.NaN), blank(0, 7), blank(1990, Number.NaN)];
    expect(sheetEntries(odd)).toHaveLength(MOVIES.length);
    expect(sheetLayout(odd, 540, false).cards).toHaveLength(MOVIES.length);
    expect(sheetView(stateOf({ movies: odd, layout: sheetLayout(odd, 540, false) })).sub).toBe(
      '3 of 8 movies readable · on Cinedikt',
    );
  });
});

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
    const l = sheetLayout([read('tt1', 'Low', 1990, 3.5), read('tt2', 'High', 1990, 9.2), read('tt3', 'Off the top', 1991, 9.8)], 540, false);
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
    const same = [1, 2, 3].map((i) => read(`tt${i}`, `Same ${i}`, 1990, 7));
    const l = sheetLayout([...same, read('tt9', 'Later', 1991, 7)], 540, false);
    expect(l.rows[0].top).toBe(AXIS_H);
    expect(l.rows[0].lanes).toBe(3);
    expect(l.rows[0].height).toBe(3 + 3 * (42 + 4) + 3);
    expect(l.cards.filter((c) => c.film.year === 1990).map((c) => c.top)).toEqual([29, 75, 121]);
    // The next year follows straight on: no jump to mark.
    expect(l.rows[1].top).toBe(l.rows[0].top + l.rows[0].height);
    expect(l.plotH).toBe(l.rows[1].top + l.rows[1].height + SHEET_BOTTOM_PAD);
  });

  it('marks a jump in the years with 4px, not the map’s 10', () => {
    const l = sheetLayout([read('tt1', 'A', 1990, 7), read('tt2', 'B', 1995, 7)], 540, false);
    expect(l.rows[1].top).toBe(l.rows[0].top + l.rows[0].height + 4);
  });

  it('marks no card as the searched one, and lights no year for it', () => {
    const l = sheetLayout(MOVIES, 540, false);
    expect(l.anchor).toBeNull();
    expect(l.cards.every((c) => !c.film.isAnchor)).toBe(true);
    expect(l.rows.every((r) => !r.anchorYear && !r.isBreak)).toBe(true);
    expect(l.cards).toHaveLength(MOVIES.length);
  });

  it('places a blank card at its place on the axis, as a readable one at its rating', () => {
    const l = sheetLayout([read('tt1', 'Readable', 1990, 7.5), blank(1991, 7.5)], 540, false);
    const [a, b] = l.cards;
    expect(a.left).toBe(b.left);
    expect(a.left).toBe(Math.round(xOf(7.5, l.metrics) - 112 / 2));
  });

  it('stacks a year’s readable cards first and its blank ones after, whatever they are', () => {
    const l = sheetLayout([blank(1990, 7), read('tt1', 'Readable', 1990, 7)], 540, false);
    expect(l.cards.map((c) => c.film.id)).toEqual(['tt1', '~1990:7.0:0']);
    expect(l.cards.map((c) => c.lane)).toEqual([0, 1]);
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

describe('the person it opened on', () => {
  it('is a showing slot, or nobody, and then the server refuses the sheet', () => {
    const game = gameOf();
    expect(openedOn(game, JOE.id)?.slot).toBe(0);
    expect(openedOn(game, 'nm9999999')).toBeNull();
  });

  it('heads the sheet, by name, in their colour, with their initials for a face with no photo', () => {
    const v = sheetView(stateOf({ person: GLORIA.id }));
    expect(v.title).toBe('Gloria Foster’s movies');
    expect(v.tone).toBe('oklch(0.76 0.13 78)');
    expect(v.code).toBe('GF');
    expect(sheetView(stateOf({ theme: 'light' })).tone).toBe('oklch(0.56 0.16 205)');
  });
});

describe('what can be read', () => {
  it('is every card the server sent readable, drawn whole, a guessed one at 0.55 with a cross', () => {
    const s = stateOf();
    expect(readable(s).map((c) => c.title)).toEqual(['The Fugitive', 'Bound', 'The Matrix']);
    expect(cardOf(s, 'tt0106977')).toMatchObject({ kind: 'readable', rating: '7.8', tried: false, opacity: 1 });
    expect(cardOf(s, 'tt0115736')).toMatchObject({ tried: true, opacity: 0.55, aria: 'Bound, 1996, rated 7.3, already tried' });
    expect(cardOf(s, 'tt0106977').aria).toBe('The Fugitive, 1993, rated 7.8');
  });

  it('draws every other card blank, at 0.3, carrying its place and nothing to read', () => {
    const blanks = sheetView(stateOf()).cards.filter((c) => c.kind === 'blank');
    expect(blanks).toHaveLength(5);
    for (const c of blanks) {
      expect(Object.keys(c).sort()).toEqual(['aria', 'key', 'kind', 'left', 'opacity', 'top']);
      expect(c.opacity).toBe(0.3);
    }
    expect(blanks[0].aria).toBe('A movie from 1983, outside your ranges');
    expect([CARD_READ, CARD_TRIED, CARD_BLANK]).toEqual([1, 0.55, 0.3]);
    expect([cardOpacity(true, false), cardOpacity(true, true), cardOpacity(false, true)]).toEqual([1, 0.55, 0.3]);
  });

  it('is nothing before a range is bought: every card blank, the count plain, and the footer pointing at the facts', () => {
    const v = sheetView(stateOf({ game: gameOf({ facts: {} }), movies: BLANKS }));
    expect(v.cards.every((c) => c.kind === 'blank')).toBe(true);
    expect(v.cards[0].aria).toBe('A movie from 1983. Buy the decade or a rating range to read it');
    expect(v.sub).toBe('8 movies · on Cinedikt');
    expect(v.legend).toBe(
      'Titles only show inside the ranges you buy: the decade or a rating range. Today’s movie is one of these cards.',
    );
    expect(v.foot).toEqual({
      kind: 'hint',
      text: 'Buy the decade or a rating range to read this map. The facts are under the card.',
    });
  });

  it('counts how many can be read once a range is bought, and names the ranges', () => {
    const v = sheetView(stateOf());
    expect(v.sub).toBe('3 of 8 movies readable · on Cinedikt');
    expect(v.legend).toBe('Titles show inside your ranges: 1990s. Today’s movie is one of these cards, but it isn’t marked.');
    expect(sheetView(stateOf({ movies: null })).sub).toBe('');
    expect(sheetView(stateOf({ movies: null })).cards).toEqual([]);
  });

  it('never counts a length or a genre bought as a range: the map stays unread, and the genre unnamed', () => {
    for (const facts of [{ length: 2 }, { genre: ['Action', 'Sci-Fi'] }, { length: 2, genre: ['Drama'] }]) {
      const v = sheetView(stateOf({ game: gameOf({ facts }), movies: BLANKS }));
      expect(v.sub, JSON.stringify(facts)).toBe('8 movies · on Cinedikt');
      expect(v.legend).toBe(
        'Titles only show inside the ranges you buy: the decade or a rating range. Today’s movie is one of these cards.',
      );
      expect(v.foot.kind === 'hint' && v.foot.text).toBe(
        'Buy the decade or a rating range to read this map. The facts are under the card.',
      );
      expect(v.cards[0].aria).toBe('A movie from 1983. Buy the decade or a rating range to read it');
      // Nothing is drawn for them: no decade washed, no rating column.
      expect(v.rows.some((r) => r.inDecade)).toBe(false);
      expect(v.column).toBeNull();
    }
  });

  it('reads only what the server sends readable: a genre beside the decade adds nothing to the legend or the map', () => {
    // The 1990s and Drama bought: the server reads every card in the
    // decade, whatever its genres (The Matrix is no drama), and says
    // none of them, and the page draws exactly those, with only the
    // decade named and washed.
    const s = stateOf({ game: gameOf({ facts: { decade: 1990, genre: ['Drama'] } }) });
    const v = sheetView(s);
    expect(readable(s).map((c) => c.title)).toEqual(['The Fugitive', 'Bound', 'The Matrix']);
    expect(v.legend).toBe('Titles show inside your ranges: 1990s. Today’s movie is one of these cards, but it isn’t marked.');
    expect(v.legend).not.toContain('Drama');
    expect(v.rows.filter((r) => r.inDecade).map((r) => r.year)).toEqual([1993, 1996, 1999]);
    expect(v.column).toBeNull();
    expect(v.sub).toBe('3 of 8 movies readable · on Cinedikt');
  });

  it('counts a rating band alone as a range, its column drawn and no decade washed', () => {
    // The 7.0 to 7.9 band alone: the server reads The Fugitive and Bound,
    // and sends the rest blank.
    const movies = [
      blank(1983, 7),
      read('tt0106977', 'The Fugitive', 1993, 7.8),
      read('tt0115736', 'Bound', 1996, 7.3),
      blank(1999, 8.5),
    ];
    const v = sheetView(stateOf({ game: gameOf({ facts: { rating: 2 } }), movies }));
    expect(v.sub).toBe('2 of 4 movies readable · on Cinedikt');
    expect(v.legend).toBe(
      'Titles show inside your ranges: rated 7.0 to 7.9. Today’s movie is one of these cards, but it isn’t marked.',
    );
    expect(v.foot.kind === 'hint' && v.foot.text).toBe('Tap a movie to guess it. A wrong guess costs 150.');
    expect(v.column).not.toBeNull();
    expect(v.rows.some((r) => r.inDecade)).toBe(false);
    expect(v.cards.find((c) => c.kind === 'blank')?.aria).toBe('A movie from 1983, outside your ranges');
  });

  it('draws today’s movie by the same rule as every other card, readable or blank', () => {
    // Two movies alike but for which is today's — and nothing the server
    // sends says which. Their cards differ only in what each movie is.
    const a = read('tt0000001', 'Alpha', 1990, 7.1);
    const b = read('tt0000002', 'Beta', 1991, 7.1);
    const pair = stateOf({ movies: [a, b], layout: sheetLayout([a, b], 540, false) });
    const [ca, cb] = sheetView(pair).cards;
    const strip = (c: SheetCard) => {
      if (c.kind === 'blank') return c;
      const { id: _i, key: _k, title: _t, aria: _a, top: _top, ...rest } = c;
      return rest;
    };
    expect(strip(ca)).toEqual(strip(cb));
    const blanks = [blank(1990, 7), blank(1991, 7)];
    const [ba, bb] = sheetView(stateOf({ movies: blanks, layout: sheetLayout(blanks, 540, false) })).cards;
    const place = ({ key: _k, top: _top, aria: _a, ...rest }: SheetCard) => rest;
    expect(place(ba)).toEqual(place(bb));
  });
});

describe('the bands drawn for the facts', () => {
  it('washes the years inside the decade bought, on the map’s own striped and ruled bands', () => {
    const rows = sheetRows({ decade: 1990 }, sheetLayout(MOVIES, 540, false));
    expect(rows.map((r) => [r.year, r.inDecade])).toEqual([
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
    expect(sheetRows({}, sheetLayout(MOVIES, 540, false)).some((r) => r.inDecade)).toBe(false);
    // Nor for anything else bought: only the decade washes a year.
    const others = { rating: 2, genre: ['Action'], length: 1 };
    expect(sheetRows(others, sheetLayout(MOVIES, 540, false)).some((r) => r.inDecade)).toBe(false);
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
  it('scrolls the first readable card to 36px from the top', () => {
    const v = sheetView(stateOf());
    const fugitive = v.cards.find((c) => c.kind === 'readable' && c.title === 'The Fugitive')!;
    expect(SHEET_READ_TOP).toBe(36);
    expect(v.scrollTo).toBe(fugitive.top - SHEET_READ_TOP);
  });

  it('takes the highest readable card, whatever order the cards come in, passes over blank ones, and never scrolls above the top', () => {
    expect(
      openingScroll([
        { top: 500, kind: 'readable' },
        { top: 80, kind: 'blank' },
        { top: 300, kind: 'readable' },
      ]),
    ).toBe(264);
    expect(openingScroll([{ top: 29, kind: 'readable' }])).toBe(0);
    expect(openingScroll([{ top: 400, kind: 'blank' }])).toBe(0);
    expect(sheetView(stateOf({ game: gameOf({ facts: {} }), movies: BLANKS })).scrollTo).toBe(0);
  });

  it('loads posters only for the readable cards in the stretch the reader has been near', () => {
    expect(readable(stateOf()).some((c) => c.warm)).toBe(false);
    const fugitive = cardOf(stateOf(), 'tt0106977');
    const near = readable(stateOf({ reach: { top: fugitive.top - 10, bottom: fugitive.top + 20 } }));
    expect(near.filter((c) => c.warm).map((c) => c.title)).toEqual(['The Fugitive']);
  });

  it('widens what it has loaded as the reader scrolls, never narrowing it', () => {
    const a = widenReach(null, { top: 0, bottom: 600 });
    expect(a).toEqual({ top: 0, bottom: 600 });
    expect(widenReach(a, { top: 100, bottom: 500 })).toBe(a);
    expect(widenReach(a, { top: 300, bottom: 900 })).toEqual({ top: 0, bottom: 900 });
  });
});

describe('the footer', () => {
  const FUGITIVE = MOVIES[4] as DailyReadableMovie;
  const BOUND_MOVIE = MOVIES[5] as DailyReadableMovie;

  it('says how to guess, at the next wrong guess’s price, with nothing tapped', () => {
    expect(sheetFoot(null, gameOf())).toEqual({ kind: 'hint', text: 'Tap a movie to guess it. A wrong guess costs 150.' });
    expect(sheetFoot(null, gameOf({ nextCost: 1200 }))).toEqual({
      kind: 'hint',
      text: 'Tap a movie to guess it. A wrong guess costs 1,200.',
    });
  });

  it('offers the card tapped, with Guess it', () => {
    expect(sheetFoot(FUGITIVE, gameOf())).toEqual({
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
    expect(sheetFoot(BOUND_MOVIE, gameOf())).toMatchObject({ label: ALREADY_TRIED, can: false });
    expect(ALREADY_TRIED).toBe('Already tried');
  });

  it('cannot guess once the game is over', () => {
    expect(sheetFoot(FUGITIVE, gameOf({ phase: 'done' }))).toMatchObject({ label: GUESS_IT, can: false });
  });

  it('rings the card tapped', () => {
    const s = stateOf({ pick: 'tt0106977' });
    expect(cardOf(s, 'tt0106977').picked).toBe(true);
    expect(readable(s).filter((c) => c.picked)).toHaveLength(1);
    expect(sheetView(s).foot.kind).toBe('pick');
  });

  it('never offers a blank card, nor one a fresh answer has left blank', () => {
    // A blank card cannot be tapped; and a pick the server no longer sends
    // readable is let go rather than guessed blind.
    expect(sheetView(stateOf({ pick: '~1983:7.0:0' })).foot.kind).toBe('hint');
    expect(sheetView(stateOf({ pick: 'tt0106977', movies: BLANKS })).foot.kind).toBe('hint');
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

  it('has no chips row left, nor anything that drew one', () => {
    expect(text).not.toMatch(/\.cd-msheet-chip|\.cd-msheet-price/);
  });

  it('draws a blank card as an empty poster on the tile, with nothing to press', () => {
    expect(rule('.cd-msheet-card-blank .cd-msheet-poster')).toContain('background: var(--skel);');
    expect(rule('.cd-msheet-card-blank:disabled')).toContain('cursor: default;');
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
