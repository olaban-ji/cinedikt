import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';
import matrix from './fixtures/matrix-grid.json';
import { opacityOf } from './GridMap';
import { keyYear } from './YearRange';
import {
  activeFilters,
  changedCount,
  RATING_STOPS,
  rungLabel,
  withoutPill,
  clampRating,
  isLit,
  yearBounds,
  yearCounts,
  histBars,
  DEFAULT_SETTINGS,
  fitLane,
  GAP,
  gridLines,
  initialsFor,
  inWarmSpan,
  layoutGrid,
  markersFor,
  dateOrd,
  metricsFor,
  aloneAfterHiding,
  emptyYearCount,
  litOthers,
  nothingLit,
  onPlot,
  othersOnPlot,
  rangeHoldsNone,
  passesFloor,
  revealDelay,
  settingsFrom,
  REVEAL_MAX_MS,
  warmSpan,
  NUDGE_RATIO,
  R_HI,
  R_LO,
  spineOf,
  xOf,
  footRoom,
  footWidth,
  MAX_MARKS,
  textWidth,
  AXIS_H,
  AXIS_LABEL_W,
  railLabelTop,
  rowKey,
  seamArriving,
  seamLeaving,
  searchedTagAt,
  type Row,
  type GridPayload,
  type GridPerson,
  type Placed,
  type SpineFilm,
  type SpineTuple,
  type GridSettings,
} from './grid';

const real = matrix as unknown as GridPayload;
/** A payload shaped like the server's: films as [id, year, rating]. */
type FilmSpec = {
  id: string;
  year: number;
  rating: number | null;
  md?: number;
  /** Places in the chip row, as the server sends them. */
  people?: number[];
};

function payloadOf(
  anchor: { id: string; year: number; rating: number | null; md?: number },
  films: FilmSpec[],
  people: GridPerson[] = [],
): GridPayload {
  return {
    anchor: { ...anchor, md: anchor.md ?? 0, title: 'Anchor', people: [], isAnchor: true },
    people,
    films: films.map(
      (f) => [f.id, f.year, f.rating, f.md ?? 0, f.people ?? []] as SpineTuple,
    ),
  };
}

/** A chip row of n people, for the tests that select one. */
function castOf(n: number): GridPerson[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `nm${String(i).padStart(7, '0')}`,
    name: `Person ${i}`,
    role: 'cast' as const,
    order: i,
  }));
}

const WIDTHS = [390, 924, 1280, 1680];

function settings(over: Partial<GridSettings> = {}): GridSettings {
  return { ...DEFAULT_SETTINGS, ...over };
}

describe('the rating scale', () => {
  it('puts a low rating left and a high one right', () => {
    const m = metricsFor(1280, settings());
    expect(xOf(R_LO, m)).toBeLessThan(xOf(R_HI, m));
    expect(xOf(4, m)).toBeLessThan(xOf(8, m));
  });

  it('is fixed, not taken from the data, so a 7 is always in the same place', () => {
    const m = metricsFor(1280, settings());
    expect(xOf(R_LO, m)).toBeCloseTo(m.left, 6);
    expect(xOf(R_HI, m)).toBeCloseTo(m.right, 6);
  });

  it('holds an off-scale rating at the end rather than off the plot', () => {
    expect(clampRating(1)).toBe(R_LO);
    expect(clampRating(10)).toBe(R_HI);
    const m = metricsFor(1280, settings());
    expect(xOf(1, m)).toBe(xOf(R_LO, m));
    expect(xOf(10, m)).toBe(xOf(R_HI, m));
  });

  it('labels 4.0 to 9.0 on their own gridlines, without overlapping', () => {
    for (const w of WIDTHS) {
      const m = metricsFor(w, settings());
      const lines = gridLines(m);
      expect(lines.map((l) => l.label)).toEqual(['4.0', '5.0', '6.0', '7.0', '8.0', '9.0']);
      for (const l of lines) {
        // §10.6: the label's box is centred on its line, within 1px. The
        // box is the handoff's 28px now (it was 40): its labels sit at
        // the rule's x less 14.
        expect(AXIS_LABEL_W).toBe(28);
        expect(Math.abs(l.labelLeft + AXIS_LABEL_W / 2 - l.x)).toBeLessThanOrEqual(1);
      }
      for (let i = 1; i < lines.length; i++) {
        expect(lines[i].labelLeft - (lines[i - 1].labelLeft + AXIS_LABEL_W)).toBeGreaterThanOrEqual(0);
      }
    }
  });

  it('keeps the compact card’s labels apart too', () => {
    for (const w of [390, 844]) {
      const lines = gridLines(metricsFor(w, settings(), true));
      for (let i = 1; i < lines.length; i++) {
        expect(lines[i].labelLeft - (lines[i - 1].labelLeft + AXIS_LABEL_W)).toBeGreaterThanOrEqual(0);
      }
    }
  });
});

describe('the axis bar', () => {
  it('starts the first row under the 26px bar', () => {
    // The handoff's bar is 26 high (the app's was 22), and .cd-axis is
    // pulled back over the rows by the same amount.
    expect(AXIS_H).toBe(26);
    for (const compact of [false, true]) {
      const l = layoutGrid(real, 1280, settings(), undefined, compact);
      expect(l.rows[0].top).toBe(AXIS_H);
    }
  });

  it('leaves 110px under the last row', () => {
    // The handoff's plot is its rows plus 110, clear of the floating
    // buttons.
    const l = layoutGrid(real, 1280);
    const last = l.rows[l.rows.length - 1];
    expect(l.plotH).toBe(last.top + last.height + 110);
  });

  it('puts "IMDb rating →" 10px past the unrated column', () => {
    // The handoff's desktop map: a 72px rail and a 184px column, so the
    // words start at 266.
    const on = layoutGrid(real, 1425);
    expect(on.unratedEdge).toBe(72 + 168 + 16);
    expect(on.axisTitleLeft).toBe(266);
    const phone = layoutGrid(real, 390, settings(), undefined, true);
    expect(phone.axisTitleLeft).toBe(52 + 132 + 16 + 10);
  });

  it('moves "IMDb rating →" to the rail’s edge with the unrated column off', () => {
    // Where "Unrated" would have been, as the handoff's prototype places it.
    const off = layoutGrid(real, 1425, settings({ showUnrated: false }));
    expect(off.axisTitleLeft).toBe(off.metrics.railW + 10);
  });

  it('keeps "IMDb rating →" clear of the 4.0 label at every size', () => {
    // About 75px of 11px Figtree 600, plus a little room.
    for (const showUnrated of [true, false]) {
      for (const [w, compact] of [[390, true], [844, true], [820, false], [1440, false]] as const) {
        const l = layoutGrid(real, w, settings({ showUnrated }), undefined, compact);
        expect(l.axisTitleLeft + 80, `${w} ${showUnrated}`).toBeLessThanOrEqual(l.lines[0].labelLeft);
      }
    }
  });
});

describe('the year rail', () => {
  const row = (over: Partial<Row>): Row => ({
    year: 1999,
    top: 100,
    height: 90,
    lanes: 1,
    decade: false,
    anchorYear: false,
    index: 0,
    ...over,
  });

  it('sets a decade’s larger numeral higher in its row than any other year', () => {
    // Young Serif 16 at 8px down, Figtree 12 (13 when searched) at 11.
    expect(railLabelTop(row({ decade: true, year: 2000 }))).toBe(108);
    expect(railLabelTop(row({}))).toBe(111);
    expect(railLabelTop(row({ anchorYear: true }))).toBe(111);
  });

  it('puts the break row’s marker where a decade’s would be', () => {
    expect(railLabelTop(row({ isBreak: true, year: 0, height: 28 }))).toBe(108);
  });

  it('pins each year’s label as far under the rating strip as it rests down its row', () => {
    // The stylesheet cannot read railLabelTop or AXIS_H, so its numbers
    // are held to them here.
    const rule = (sel: string) => css.match(new RegExp(`${sel} \\{([^}]*)\\}`))?.[1] ?? '';
    const inset = railLabelTop(row({ top: 0 }));
    const decadeInset = railLabelTop(row({ top: 0, decade: true, year: 2000 }));
    const year = rule('\\.cd-rail-slot \\.cd-rail-year');
    expect(year).toContain('position: sticky');
    expect(year).toContain(`top: calc(var(--pin-lift, 0px) + ${AXIS_H + inset}px)`);
    expect(year).toContain(`margin: ${inset}px 0 6px 14px`);
    const decade = rule('\\.cd-rail-slot \\.cd-rail-decade');
    expect(decade).toContain(`top: calc(var(--pin-lift, 0px) + ${AXIS_H + decadeInset}px)`);
    expect(decade).toContain(`margin-top: ${decadeInset}px`);
  });

  it('moves a pinned label with the header, on the header’s own timing', () => {
    const rule = (sel: string) => css.match(new RegExp(`${sel} \\{([^}]*)\\}`))?.[1] ?? '';
    expect(rule('\\.cd-rail-slot \\.cd-rail-year')).toContain('transition: top 0.32s var(--ease-glide)');
    expect(rule('\\.cd-header-over')).toContain('transition: transform 0.32s var(--ease-glide)');
  });
});

describe('the searched tag', () => {
  it('sits 10px in from the searched card’s left and 10px above it', () => {
    const l = layoutGrid(real, 1440);
    const a = l.anchor!;
    expect(searchedTagAt(a)).toEqual({ left: a.left + 10, top: a.top - 10 });
  });

  it('stays below the axis bar, on the first row too', () => {
    // A row's cards start 12px down it, so the tag's top is still 2px
    // inside the row even when the searched film's year is first.
    const only = { id: 'tt0000001', year: 1999, rating: 8.7 };
    const l = layoutGrid(payloadOf(only, [only]), 1440);
    expect(searchedTagAt(l.anchor!).top).toBeGreaterThanOrEqual(AXIS_H);
  });
});

describe('dateOrd', () => {
  it('reads month and day, and puts a year-only date at the head of its year', () => {
    expect(dateOrd({ year: 2013, md: 310 })).toBe(20130310);
    expect(dateOrd({ year: 2013, md: 1201 })).toBe(20131201);
    expect(dateOrd({ year: 2013, md: 0 })).toBe(20130101);
  });
});

describe('lane packing', () => {
  it('keeps a card at its honest x when the lane is clear', () => {
    expect(fitLane([], 500, 116)).toEqual({ lane: 0, left: 500 });
    expect(fitLane([300], 500, 116)).toEqual({ lane: 0, left: 500 });
  });

  it('nudges a card right rather than opening a lane for a near miss', () => {
    const cardW = 116;
    const end = 500;
    const ideal = end + GAP - 10; // 10px short of clear
    const { lane, left } = fitLane([end], ideal, cardW);
    expect(lane).toBe(0);
    expect(left).toBe(end + GAP);
    expect(left - ideal).toBeLessThanOrEqual(Math.round(cardW * NUDGE_RATIO));
  });

  it('opens a new lane rather than telling a lie about the rating', () => {
    const cardW = 116;
    const { lane, left } = fitLane([900], 100, cardW);
    expect(lane).toBe(1);
    expect(left).toBe(100); // exactly where the rating says
  });
});

describe('layoutGrid on the real Matrix payload', () => {
  it('places every film', () => {
    const l = layoutGrid(real, 1280);
    expect(l.cards).toHaveLength(real.films.length);
    expect(l.rows.length).toBeGreaterThan(20);
  });

  // §10.1
  it('puts a rated card within a nudge of its rating, at every width', () => {
    for (const w of WIDTHS) {
      const l = layoutGrid(real, w);
      const slack = l.metrics.cardW * NUDGE_RATIO + 1;
      for (const c of l.cards) {
        if (c.film.rating == null) continue;
        const centre = c.left + l.metrics.cardW / 2;
        expect(Math.abs(centre - xOf(c.film.rating, l.metrics)), String(c.film.id)).toBeLessThanOrEqual(slack);
      }
    }
  });

  // §10.1
  it('keeps every unrated card in the No rating column', () => {
    const l = layoutGrid(real, 1280);
    const unrated = l.cards.filter((c) => c.film.rating == null);
    expect(unrated.length).toBeGreaterThan(0);
    for (const c of unrated) {
      expect(c.left).toBeGreaterThanOrEqual(l.metrics.railW);
      expect(c.left + l.metrics.cardW).toBeLessThanOrEqual(l.unratedEdge + l.metrics.cardW);
    }
  });

  // §10.2
  it('never overlaps two cards', () => {
    for (const w of WIDTHS) {
      const l = layoutGrid(real, w);
      const { cardW, cardH } = l.metrics;
      const byLane = new Map<string, typeof l.cards>();
      for (const c of l.cards) {
        const key = `${c.top}`;
        const list = byLane.get(key);
        if (list) list.push(c);
        else byLane.set(key, [c]);
      }
      for (const [, list] of byLane) {
        const sorted = [...list].sort((a, b) => a.left - b.left);
        for (let i = 1; i < sorted.length; i++) {
          expect(sorted[i].left, `film ${sorted[i].film.id} at ${w}px`).toBeGreaterThanOrEqual(
            sorted[i - 1].left + cardW,
          );
        }
      }
      // And no row's cards spill into the row below.
      for (const r of l.rows) {
        const last = r.top + 12 + (r.lanes - 1) * (cardH + GAP) + cardH;
        expect(last).toBeLessThanOrEqual(r.top + r.height + GAP);
      }
    }
  });

  it('orders rows oldest first, or newest first when asked', () => {
    const oldest = layoutGrid(real, 1280, settings({ yearOrder: 'oldest' }));
    expect(oldest.rows[0].year).toBeLessThan(oldest.rows[oldest.rows.length - 1].year);
    const newest = layoutGrid(real, 1280, settings({ yearOrder: 'newest' }));
    expect(newest.rows[0].year).toBeGreaterThan(newest.rows[newest.rows.length - 1].year);
  });

  it('stacks rows downward with no overlap and room at the bottom', () => {
    const l = layoutGrid(real, 1280);
    for (let i = 1; i < l.rows.length; i++) {
      expect(l.rows[i].top).toBeGreaterThanOrEqual(l.rows[i - 1].top + l.rows[i - 1].height);
    }
    const last = l.rows[l.rows.length - 1];
    expect(l.plotH).toBeGreaterThan(last.top + last.height);
  });

  it('marks the searched film and its year', () => {
    const l = layoutGrid(real, 1280);
    expect(l.anchor?.film.id).toBe(real.anchor.id);
    expect(l.anchor?.film.isAnchor).toBe(true);
    expect(l.rows.find((r) => r.year === real.anchor.year)?.anchorYear).toBe(true);
  });

  it('drops the unrated column when the reader turns it off', () => {
    const l = layoutGrid(real, 1280, settings({ showUnrated: false }));
    expect(l.cards.every((c) => c.film.rating != null)).toBe(true);
    expect(l.metrics.unratedW).toBe(0);
    expect(l.unratedEdge).toBe(l.metrics.railW);
  });

  // §10.10
  it('does not move a card when people are selected — that is opacity only', () => {
    const a = layoutGrid(real, 1280);
    const b = layoutGrid(real, 1280);
    expect(b.cards.map((c) => [c.film.id, c.left, c.top])).toEqual(
      a.cards.map((c) => [c.film.id, c.left, c.top]),
    );
  });



  it('stacks an earlier month above a later one when the cards would collide', () => {
    const year = 2013;
    const jan = { id: 'tt0000001', year, rating: 6.0, md: 120 };
    const dec = { id: 'tt0000002', year, rating: 6.05, md: 1205 };
    const laid = layoutGrid(payloadOf(jan, [jan, dec]), 1280);
    const a = laid.cards.find((c) => c.film.id === 'tt0000001')!;
    const b = laid.cards.find((c) => c.film.id === 'tt0000002')!;
    expect(a.lane).toBeLessThan(b.lane);
    expect(a.left).toBeLessThan(b.left + laid.metrics.cardW);
  });

  it('keeps a later month on top when the years themselves run newest first', () => {
    const year = 2013;
    const jan = { id: 'tt0000001', year, rating: 6.0, md: 120 };
    const dec = { id: 'tt0000002', year, rating: 6.05, md: 1205 };
    const laid = layoutGrid(
      payloadOf(jan, [jan, dec]),
      1280,
      settings({ yearOrder: 'newest' }),
    );
    const a = laid.cards.find((c) => c.film.id === 'tt0000001')!;
    const b = laid.cards.find((c) => c.film.id === 'tt0000002')!;
    expect(b.lane).toBeLessThan(a.lane);
  });


});

describe('initials', () => {
  it('takes the first letter of each end of the name', () => {
    const codes = initialsFor(real.people);
    const keanu = real.people.find((p) => p.name === 'Keanu Reeves')!;
    expect(codes.get(keanu.id)).toBe('KR');
  });

  it('tells two people with the same initials apart', () => {
    // Both Wachowskis are L + Wachowski, which is exactly the case the
    // spec's own rule cannot separate.
    const codes = initialsFor([
      { id: 'nm0000001', name: 'Lana Wachowski', role: 'director', order: -1 },
      { id: 'nm0000002', name: 'Lilly Wachowski', role: 'director', order: -1 },
    ]);
    expect(codes.get('nm0000001')).toBe('LaW');
    expect(codes.get('nm0000002')).toBe('LiW');
    expect(new Set([...codes.values()]).size).toBe(2);
  });

  it('gives everyone on a real map a code of their own', () => {
    const codes = initialsFor(real.people);
    expect(new Set([...codes.values()]).size).toBe(real.people.length);
  });

  it('copes with a single name', () => {
    const codes = initialsFor([{ id: 'nm0000001', name: 'Cher', role: 'cast', order: 0 }]);
    expect(codes.get('nm0000001')).toBe('C');
  });
});

describe('markersFor', () => {
  const wide = metricsFor(1280, DEFAULT_SETTINGS);
  const tablet = metricsFor(820, DEFAULT_SETTINGS);
  const phone = metricsFor(390, DEFAULT_SETTINGS);
  const ids = (n: number) => Array.from({ length: n }, (_, i) => `nm${String(i).padStart(7, '0')}`);
  const codes = initialsFor(real.people);
  const who = (name: string) => real.people.find((p) => p.name === name)!.id;
  const [lana, lilly, keanu, carrie, joe] = [
    'Lana Wachowski',
    'Lilly Wachowski',
    'Keanu Reeves',
    'Carrie-Anne Moss',
    'Joe Pantoliano',
  ].map(who);

  it('names one or two people on a desktop or tablet card', () => {
    // Memento on The Matrix's map, as the handoff draws it: "● CM ● JP".
    for (const m of [wide, tablet]) {
      expect(markersFor([carrie, joe], m, 8.4, codes)).toEqual({
        show: [carrie, joe],
        extra: 0,
        initials: true,
      });
    }
  });

  it('draws swatches alone on a phone card, however few people', () => {
    expect(markersFor([keanu], phone, 7.5, codes)).toEqual({ show: [keanu], extra: 0, initials: false });
  });

  it('draws swatches alone on a landscape phone’s card too', () => {
    // As wide as a tablet, but it has the phone's card, and the handoff
    // keeps initials to desktop and tablet sizes.
    const landscape = metricsFor(844, DEFAULT_SETTINGS, true);
    expect(markersFor([keanu], landscape, 7.5, codes).initials).toBe(false);
  });

  it('switches to swatches once there are three', () => {
    const m = markersFor(ids(3), wide, 7.5);
    expect(m.initials).toBe(false);
    expect(m.show).toEqual(ids(3));
    expect(m.extra).toBe(0);
  });

  it('draws up to five, then four and a count, when the card has room', () => {
    expect(markersFor(ids(5), wide, 7.5)).toEqual({ show: ids(5), extra: 0, initials: false });
    expect(markersFor(ids(6), wide, 7.5)).toEqual({ show: ids(4), extra: 2, initials: false });
    expect(markersFor(ids(7), wide, 7.5)).toEqual({ show: ids(4), extra: 3, initials: false });
  });

  // A mark cut in half by the card's edge says nothing, so the handoff's
  // cap is an upper bound and the row's room decides the rest.
  it('never draws a row wider than the card has room for', () => {
    const named = [lana, lilly, ...real.people.slice(2).map((p) => p.id)];
    for (const m of [wide, tablet, phone]) {
      for (const rating of [null, 7.2, 10]) {
        for (let n = 0; n <= 40; n++) {
          const people = n <= named.length ? named.slice(0, n) : ids(n);
          const got = markersFor(people, m, rating, codes);
          const at = `${m.cardW}px card, rating ${rating}, ${n} people`;
          expect(footWidth(got, rating, codes), at).toBeLessThanOrEqual(footRoom(m));
          expect(got.show.length, at).toBeLessThanOrEqual(MAX_MARKS);
          // Whatever it shows, it never invents or loses anyone it draws.
          if (got.show.length > 0) expect(got.show.length + got.extra, at).toBe(n);
          // And a count always follows at least one swatch.
          if (got.extra > 0) expect(got.show.length, at).toBeGreaterThan(0);
        }
      }
    }
  });

  it('names both Wachowskis on a desktop or tablet card, where they fit', () => {
    // The handoff's own box: a 168px card with a 50px poster. Their codes
    // are "LaW" and "LiW", and the row leaves under a pixel spare (Speed
    // Racer 6.1, Jupiter Ascending 5.3).
    const box = metricsFor(1440, DEFAULT_SETTINGS);
    expect([box.cardW, box.posterW]).toEqual([168, 50]);
    for (const rating of [6.1, 5.3]) {
      expect(markersFor([lana, lilly], box, rating, codes)).toEqual({
        show: [lana, lilly],
        extra: 0,
        initials: true,
      });
    }
  });

  it('gives up the initials before it gives up a person', () => {
    // "No rating" and both Wachowskis' initials are wider than a desktop
    // card's foot; two swatches are not.
    expect(markersFor([lana, lilly], wide, null, codes)).toEqual({
      show: [lana, lilly],
      extra: 0,
      initials: false,
    });
  });

  it('draws fewer swatches and a bigger count on a small card', () => {
    expect(markersFor(ids(5), phone, 7.2)).toEqual({ show: ids(2), extra: 3, initials: false });
    // And on a wide one, once the count grows a digit.
    expect(markersFor(ids(20), wide, 8.1)).toEqual({ show: ids(3), extra: 17, initials: false });
  });

  it('gives the rating the room on a card too small for both', () => {
    expect(markersFor(ids(3), phone, null)).toEqual({ show: [], extra: 0, initials: false });
  });

  it('leaves more room when the rating is short', () => {
    expect(markersFor(ids(20), wide, 8.1).show.length).toBeGreaterThanOrEqual(
      markersFor(ids(20), wide, null).show.length,
    );
  });

  it('never returns more people than it was given', () => {
    for (const n of [0, 1, 2, 3, 8, 39]) {
      const got = markersFor(ids(n), wide, 7);
      expect(got.show.length + got.extra).toBe(n);
    }
  });
});

describe('textWidth', () => {
  it('matches Figtree 700 as the browser sets it', () => {
    // Measured in Chrome with the loaded font: "No rating" at 12.5px is
    // 54.23px, "KR" at 10.5px is 13.77px, and "+16" in tabular figures
    // at 10.5px is 19.91px.
    expect(textWidth('No rating', 12.5)).toBeCloseTo(54.23, 0);
    expect(textWidth('KR', 10.5)).toBeCloseTo(13.77, 0);
    expect(textWidth('+16', 10.5)).toBeCloseTo(19.91, 0);
  });

  it('gives every digit the same width, as tabular figures do', () => {
    expect(textWidth('1.1', 12.5)).toBe(textWidth('8.8', 12.5));
  });

  it('measures an accented letter as the letter under the accent', () => {
    expect(textWidth('É', 10)).toBe(textWidth('E', 10));
  });

  it('takes a character it does not know to be as wide as a W', () => {
    expect(textWidth('Ж', 10)).toBe(textWidth('W', 10));
  });
});

describe('the card, now that Compact is gone', () => {
  it('gives a phone card two title lines beside its poster', () => {
    const m = metricsFor(390, settings());
    expect(m.titleLines).toBe(2);
    expect(m.cardW).toBe(132);
    expect(m.cardH).toBe(72);
  });

  it('leaves the desktop card the size it always was', () => {
    const m = metricsFor(1280, settings());
    expect(m.cardW).toBe(168);
    expect(m.cardH).toBe(90);
    expect(m.titleLines).toBe(2);
  });

  it('fills the card’s height with the poster, inside its padding', () => {
    // The handoff's poster is the card's height less 6px top and bottom,
    // not 2:3: 50×78 on the large card and 40×60 on the compact one.
    // (It was 52×78, a 2:3 frame that set the card's height.)
    const wide = metricsFor(1280, settings());
    expect([wide.posterW, wide.posterH]).toEqual([50, 78]);
    const phone = metricsFor(390, settings());
    expect([phone.posterW, phone.posterH]).toEqual([40, 60]);
  });

  it('gives a landscape phone the phone’s card, rail and plot', () => {
    // 844 wide is a tablet's width, but a landscape phone has a phone's
    // height to fit rows into, so the screen asks for the compact card.
    const m = metricsFor(844, settings(), true);
    expect([m.cardW, m.cardH, m.posterW, m.railW]).toEqual([132, 72, 40, 52]);
    expect(m.compact).toBe(true);
    expect(m.plotW).toBe(844);
    expect(metricsFor(700, settings(), true).plotW).toBe(820);
    // Left to the width alone, the same scroller gets the large card and
    // the wider plot.
    const wide = metricsFor(844, settings());
    expect([wide.cardW, wide.cardH, wide.railW, wide.plotW]).toEqual([168, 90, 72, 980]);
  });

  it('lays a map out with the card the screen asks for', () => {
    const compact = layoutGrid(real, 844, settings(), undefined, true);
    const large = layoutGrid(real, 844, settings());
    expect(compact.metrics.cardW).toBe(132);
    expect(large.metrics.cardW).toBe(168);
  });

  it('has no density setting left to read', () => {
    expect('density' in DEFAULT_SETTINGS).toBe(false);
  });

  it('drops the stored density of an install from before it went', () => {
    const s = settingsFrom(JSON.stringify({ density: 'compact', yearOrder: 'newest' }));
    expect('density' in s).toBe(false);
    expect(s.yearOrder).toBe('newest');
  });

  it('falls back to the defaults for nothing stored, or nonsense', () => {
    expect(settingsFrom(null)).toEqual(DEFAULT_SETTINGS);
    expect(settingsFrom('{oops')).toEqual(DEFAULT_SETTINGS);
    expect(settingsFrom('null')).toEqual(DEFAULT_SETTINGS);
  });
});

describe('a card with a poster', () => {
  it('keeps a portrait poster beside a text column, at every width', () => {
    for (const w of WIDTHS) {
      const m = metricsFor(w, settings());
      expect(m.posterW).toBeGreaterThanOrEqual(40);
      expect(m.posterH).toBeGreaterThan(m.posterW);
      expect(m.cardH).toBeGreaterThan(m.posterH);
      expect(m.cardW).toBeGreaterThan(m.posterW + 60);
    }
  });
});

describe('the year range', () => {
  /** The Matrix, 1999, with a career either side of it. */
  const career = () =>
    payloadOf({ id: 'tt0000001', year: 1999, rating: 8 }, [
      { id: 'tt0000001', year: 1999, rating: 8, people: [0, 1] },
      { id: 'tt0000002', year: 2000, rating: 7, people: [0] },
      { id: 'tt0000003', year: 2003, rating: 6, people: [1] },
      { id: 'tt0000004', year: 1994, rating: 9, people: [0] },
    ], castOf(2));

  it('crops the rows outside it and keeps the searched film', () => {
    const l = layoutGrid(career(), 1280, settings({ yearFrom: 2000 }));
    expect(l.rows.filter((r) => !r.isBreak).map((r) => r.year)).toEqual([1999, 2000, 2003]);
    expect(l.cards.map((c) => c.film.id).sort()).toEqual([
      'tt0000001',
      'tt0000002',
      'tt0000003',
    ]);
  });

  it('puts a break between the searched film and the range', () => {
    const oldest = layoutGrid(career(), 1280, settings({ yearFrom: 2000 }));
    expect(oldest.rows.map((r) => (r.isBreak ? 'break' : r.year))).toEqual([
      1999,
      'break',
      2000,
      2003,
    ]);
    const newest = layoutGrid(
      career(),
      1280,
      settings({ yearFrom: 2000, yearOrder: 'newest' }),
    );
    expect(newest.rows.map((r) => (r.isBreak ? 'break' : r.year))).toEqual([
      2003,
      2000,
      'break',
      1999,
    ]);
  });

  it('has no break when the searched film is inside the range', () => {
    const l = layoutGrid(career(), 1280, settings({ yearFrom: 1995, yearTo: 2001 }));
    expect(l.rows.some((r) => r.isBreak)).toBe(false);
  });

  it('knows the years this map actually holds', () => {
    expect(yearBounds(career())).toEqual({ lo: 1994, hi: 2003 });
  });

  it('does not stretch to a year only unrated films hold, with their column off', () => {
    const p = payloadOf({ id: 'tt0000001', year: 1999, rating: 8 }, [
      { id: 'tt0000001', year: 1999, rating: 8, people: [0] },
      { id: 'tt0000002', year: 1988, rating: null, people: [0] },
      { id: 'tt0000003', year: 2003, rating: 6, people: [0] },
      { id: 'tt0000004', year: 2011, rating: null, people: [0] },
    ], castOf(1));
    expect(yearBounds(p)).toEqual({ lo: 1988, hi: 2011 });
    expect(yearBounds(p, true)).toEqual({ lo: 1988, hi: 2011 });
    expect(yearBounds(p, false)).toEqual({ lo: 1999, hi: 2003 });
  });

  it('keeps the searched film’s year in the bounds even when it is unrated', () => {
    const p = payloadOf({ id: 'tt0000001', year: 1980, rating: null }, [
      { id: 'tt0000001', year: 1980, rating: null, people: [0] },
      { id: 'tt0000002', year: 2000, rating: 7, people: [0] },
    ], castOf(1));
    expect(yearBounds(p, false)).toEqual({ lo: 1980, hi: 2000 });
  });
});

describe('yearCounts', () => {
  it('counts every film in each year, the searched one included', () => {
    const counts = yearCounts(real);
    expect(counts.get(1999)).toBe(2);
    expect(counts.get(2003)).toBe(7);
    expect(counts.get(1971)).toBe(1);
    const total = [...counts.values()].reduce((a, n) => a + n, 0);
    expect(total).toBe(real.films.length);
  });

  it('leaves out films with no year, which have no row to stand over', () => {
    const p = payloadOf({ id: 'tt0000001', year: 1999, rating: 8 }, [
      { id: 'tt0000001', year: 1999, rating: 8 },
      { id: 'tt0000002', year: 0, rating: 7 },
    ]);
    expect([...yearCounts(p)]).toEqual([[1999, 1]]);
  });

  it('counts by the slider’s own rule, so no bar stands off its track', () => {
    // With the unrated column off, the slider's ends drop the years only
    // unrated films hold, and so do the bars. The searched film counts
    // even unrated: it is always on the plot.
    const p = payloadOf({ id: 'tt0000001', year: 1980, rating: null }, [
      { id: 'tt0000001', year: 1980, rating: null },
      { id: 'tt0000002', year: 1975, rating: null },
      { id: 'tt0000003', year: 2000, rating: 7 },
      { id: 'tt0000004', year: 2000, rating: null },
    ]);
    expect(yearCounts(p, true)).toEqual(new Map([[1980, 1], [1975, 1], [2000, 2]]));
    expect(yearCounts(p, false)).toEqual(new Map([[1980, 1], [2000, 1]]));
    const { lo, hi } = yearBounds(p, false);
    for (const year of yearCounts(p, false).keys()) {
      expect(year).toBeGreaterThanOrEqual(lo);
      expect(year).toBeLessThanOrEqual(hi);
    }
  });

  it('keeps every bar of a real map on its track', () => {
    const { lo, hi } = yearBounds(real);
    const bars = histBars(yearCounts(real), lo, hi, lo, hi);
    expect(bars.every((b) => b.at >= 0 && b.at <= 1)).toBe(true);
  });
});

describe('histBars', () => {
  const counts = new Map([
    [2003, 7],
    [1999, 2],
    [1971, 1],
  ]);

  it('stands one bar on each year that holds a film, oldest first', () => {
    expect(histBars(counts, 1971, 2026, 1971, 2026).map((b) => b.year)).toEqual([1971, 1999, 2003]);
  });

  it('is 4px tall plus up to 26 more for the busiest year', () => {
    const bars = histBars(counts, 1971, 2026, 1971, 2026);
    const h = Object.fromEntries(bars.map((b) => [b.year, b.h]));
    expect(h[2003]).toBe(30);
    expect(h[1999]).toBe(Math.round(4 + (2 / 7) * 26));
    expect(h[1971]).toBe(Math.round(4 + (1 / 7) * 26));
    expect(Math.min(...bars.map((b) => b.h))).toBeGreaterThanOrEqual(4);
  });

  it('places each bar along the track from the first year to the last', () => {
    const bars = histBars(counts, 1971, 2026, 1971, 2026);
    expect(bars[0].at).toBe(0);
    expect(bars[1].at).toBeCloseTo((1999 - 1971) / 55);
  });

  it('marks the years between the thumbs, both ends included', () => {
    const bars = histBars(counts, 1971, 2026, 1999, 2003);
    expect(bars.map((b) => b.inRange)).toEqual([false, true, true]);
  });

  it('draws nothing for a map with no years to count', () => {
    expect(histBars(new Map(), 1999, 1999, 1999, 1999)).toEqual([]);
  });
});

describe('hiding the empty years', () => {
  const career = () =>
    payloadOf({ id: 'tt0000001', year: 1999, rating: 8 }, [
      { id: 'tt0000001', year: 1999, rating: 8, people: [0, 1] },
      { id: 'tt0000002', year: 2000, rating: 7, people: [0] },
      { id: 'tt0000003', year: 2003, rating: 6, people: [1] },
    ], castOf(2));

  it('is every card that person is in, plus the searched film', () => {
    const lit = (f: SpineFilm) => isLit(f, new Set([0]), null);
    const l = layoutGrid(career(), 1280, settings({ hideEmptyYears: true }), lit);
    expect(l.cards.map((c) => c.film.id)).toEqual(['tt0000001', 'tt0000002']);
    expect(l.rows.map((r) => r.year)).toEqual([1999, 2000]);
  });

  it('keeps the searched film’s row even with nothing else lit', () => {
    const lit = (f: SpineFilm) => isLit(f, new Set([9]), null);
    const l = layoutGrid(career(), 1280, settings({ hideEmptyYears: true }), lit);
    expect(l.cards.map((c) => c.film.id)).toEqual(['tt0000001']);
    expect(l.rows.map((r) => r.year)).toEqual([1999]);
  });

  it('hides nothing when the caller cannot say what is lit', () => {
    const l = layoutGrid(career(), 1280, settings({ hideEmptyYears: true }));
    expect(l.cards.length).toBe(3);
  });
});

describe('the seam the rows close into and open out of', () => {
  // Person 0 is in 1999 and 2002, and the searched film is 2000's; the
  // other years hold only person 1. Selecting person 0 and hiding the
  // empty years takes 1998, 2001 and 2003 away.
  const career = () =>
    payloadOf({ id: 'tt0000001', year: 2000, rating: 8 }, [
      { id: 'tt0000010', year: 1998, rating: 7, people: [1] },
      { id: 'tt0000011', year: 1999, rating: 7, people: [0] },
      { id: 'tt0000001', year: 2000, rating: 8, people: [0, 1] },
      { id: 'tt0000012', year: 2001, rating: 6, people: [1] },
      { id: 'tt0000013', year: 2002, rating: 7.5, people: [0] },
      { id: 'tt0000014', year: 2003, rating: 6.5, people: [1] },
    ], castOf(2));
  const lit = (f: SpineFilm) => isLit(f, new Set([0]), null);
  const shown = layoutGrid(career(), 1280, settings(), lit).rows;
  const hidden = layoutGrid(career(), 1280, settings({ hideEmptyYears: true }), lit).rows;
  const at = (rows: Row[], year: number) => rows.find((r) => r.year === year)!;

  it('keys a row by its year, and a film by the row it is in', () => {
    expect(shown.map(rowKey)).toEqual(['1998', '1999', '2000', '2001', '2002', '2003']);
    expect(hidden.map(rowKey)).toEqual(['1999', '2000', '2002']);
    expect(rowKey(real.anchor)).toBe(String(real.anchor.year));
    expect(rowKey({ year: 0, isBreak: true })).toBe('break');
  });

  it('closes a leaving year into the new top of the next row below it that stays', () => {
    expect(seamLeaving('2001', shown, hidden)).toBe(at(hidden, 2002).top);
    // The first row is no exception: 1998 closes into 1999, which moves
    // up to the top of the plot.
    expect(seamLeaving('1998', shown, hidden)).toBe(at(hidden, 1999).top);
    expect(at(hidden, 1999).top).toBe(AXIS_H);
  });

  it('closes a year with nothing staying below it into the bottom of the last row above it that stays', () => {
    const last = at(hidden, 2002);
    expect(seamLeaving('2003', shown, hidden)).toBe(last.top + last.height);
  });

  it('closes a card leaving a year that stays into the foot of its own row', () => {
    // A film nobody selected shares 1999 with one of person 0's, on the
    // same rating, so it takes a second lane. Hiding the empty years
    // takes it and keeps the row, one lane shorter.
    const p = career();
    p.films.push(['tt0000015', 1999, 7, 6, [1]]);
    const whole = layoutGrid(p, 1280, settings(), lit).rows;
    const kept = layoutGrid(p, 1280, settings({ hideEmptyYears: true }), lit).rows;
    expect(at(kept, 1999).height).toBeLessThan(at(whole, 1999).height);
    expect(seamLeaving('1999', whole, kept)).toBe(at(kept, 1999).top + at(kept, 1999).height);
  });

  it('opens an arriving year out of the old top of the next row below it that was showing, moved by the scroll', () => {
    // Showing the years again pushes the searched film down, and the
    // scroll follows it by that much: the seam is where the two rows met
    // on screen, in the new layout's terms.
    const shift = at(shown, 2000).top - at(hidden, 2000).top;
    expect(shift).toBeGreaterThan(0);
    expect(seamArriving('2001', hidden, shown, shift)).toBe(at(hidden, 2002).top + shift);
    expect(seamArriving('1998', hidden, shown, shift)).toBe(at(hidden, 1999).top + shift);
    // With the scroll clamped, only what it did move by.
    expect(seamArriving('2001', hidden, shown, 12)).toBe(at(hidden, 2002).top + 12);
  });

  it('opens a year with nothing showing below it out of the old bottom of the row above it', () => {
    const shift = at(shown, 2000).top - at(hidden, 2000).top;
    const last = at(hidden, 2002);
    expect(seamArriving('2003', hidden, shown, shift)).toBe(last.top + last.height + shift);
  });

  it('plays the one motion both ways: a year opens out of the seam it closes into', () => {
    // On screen, with the scroll following the searched film, the seam a
    // year closes into when hiding is the one it opens out of when shown.
    const shift = at(hidden, 2000).top - at(shown, 2000).top;
    for (const year of ['1998', '2001', '2003']) {
      expect(seamArriving(year, hidden, shown, -shift) + shift).toBe(
        seamLeaving(year, shown, hidden),
      );
    }
  });
});

describe('litOthers', () => {
  /** A career either side of 2005, with an unrated film and a film far
   *  outside any range the tests below set. */
  const career = () =>
    payloadOf({ id: 'tt0000001', year: 2005, rating: 7 }, [
      { id: 'tt0000001', year: 2005, rating: 7, people: [0, 1] },
      { id: 'tt0000002', year: 1990, rating: null, people: [0] },
      { id: 'tt0000003', year: 2010, rating: 7.5, people: [1] },
      { id: 'tt0000004', year: 2005, rating: 8.2, people: [1] },
      { id: 'tt0000005', year: 1971, rating: 8.8, people: [0] },
      { id: 'tt0000006', year: 2008, rating: 6.1, people: [0, 1] },
    ], castOf(2));

  it('leaves out a match the unrated column has taken off the plot', () => {
    // Person 0's 1990 film is unrated with its column off, and their
    // 1971 film is before the range. Only 2008 is left on the page.
    const s = settings({ showUnrated: false, yearFrom: 1985 });
    expect(litOthers(career(), s, new Set([0])).map((f) => f.id)).toEqual(['tt0000006']);
  });

  it('leaves out a match the year range has cropped', () => {
    const s = settings({ yearFrom: 2000, minRating: 8.5 });
    // P's 1971 film clears the floor, but the range has taken it away.
    expect(litOthers(career(), s, new Set([0]))).toEqual([]);
  });

  it('never counts the searched film', () => {
    expect(litOthers(career(), settings(), new Set([0, 1])).some((f) => f.isAnchor)).toBe(false);
  });

  // The count the reader is told, and whether the searched film is
  // alone, must match what the layout actually draws — for every
  // combination of the things that take films off the page.
  const combos: Partial<GridSettings>[] = [];
  for (const showUnrated of [true, false])
    for (const [yearFrom, yearTo] of [[null, null], [2000, null], [null, 2006], [2006, 2009], [2012, null]] as const)
      for (const minRating of [null, 7, 8.5])
        combos.push({ showUnrated, yearFrom, yearTo, minRating });
  const selections = [new Set<number>(), new Set([0]), new Set([1]), new Set([0, 1])];

  it('agrees with the layout about which other cards a collapsed map holds', () => {
    for (const over of combos) {
      for (const sel of selections) {
        const s = settings({ ...over, hideEmptyYears: true });
        const laid = layoutGrid(career(), 1280, s, (f) => isLit(f, sel, s.minRating));
        const drawn = laid.cards.filter((c) => !c.film.isAnchor).map((c) => c.film.id).sort();
        const counted = litOthers(career(), s, sel).map((f) => f.id).sort();
        expect({ over, sel: [...sel], ids: counted }).toEqual({ over, sel: [...sel], ids: drawn });
      }
    }
  });

  it('holds the films on the plot, lit or not, for hiding empty years to work with', () => {
    const s = settings({ showUnrated: false, yearFrom: 2000 });
    expect(othersOnPlot(career(), s).map((f) => f.id).sort()).toEqual([
      'tt0000003',
      'tt0000004',
      'tt0000006',
    ]);
    expect(othersOnPlot(career(), settings({ yearFrom: 2006, yearTo: 2007 }))).toEqual([]);
  });

  it('says the searched film is alone only when hiding the empty years did it', () => {
    const p = career();
    // Nobody lit at 8.5 for person 1, with their films still on the plot.
    expect(aloneAfterHiding(p, settings({ hideEmptyYears: true, minRating: 8.5 }), new Set([1]))).toBe(true);
    // The same, without hiding: nothing to say.
    expect(aloneAfterHiding(p, settings({ minRating: 8.5 }), new Set([1]))).toBe(false);
    // Something of theirs lit: not alone.
    expect(aloneAfterHiding(p, settings({ hideEmptyYears: true }), new Set([1]))).toBe(false);
    // The range has left nothing else on the plot. Hiding emptied
    // nothing, and showing every year again would bring nothing back.
    expect(aloneAfterHiding(p, settings({ hideEmptyYears: true, yearFrom: 2006, yearTo: 2007 }), new Set([1]))).toBe(false);
  });

  it('knows when the year range holds none of the cast’s other films', () => {
    const p = career();
    // No range, no claim.
    expect(rangeHoldsNone(p, settings())).toBe(false);
    // Wholly outside the years they worked.
    expect(rangeHoldsNone(p, settings({ yearFrom: 2030 }))).toBe(true);
    // Inside those years, but in a gap between two films: 2006–2007.
    expect(rangeHoldsNone(p, settings({ yearFrom: 2006, yearTo: 2007 }))).toBe(true);
    // An unrated film in range is still one of theirs, column or not.
    expect(rangeHoldsNone(p, settings({ yearFrom: 1989, yearTo: 1991, showUnrated: false }))).toBe(false);
    // Another film of theirs shares the searched film's year.
    expect(rangeHoldsNone(p, settings({ yearFrom: 2005, yearTo: 2005 }))).toBe(false);
    // The searched film itself does not count: it is never cropped, so
    // it is on the page whatever the range, and says nothing about it.
    const alone = payloadOf({ id: 'tt0000001', year: 2005, rating: 7 }, [
      { id: 'tt0000001', year: 2005, rating: 7, people: [0] },
      { id: 'tt0000002', year: 1990, rating: 6, people: [0] },
    ], castOf(1));
    expect(rangeHoldsNone(alone, settings({ yearFrom: 2000, yearTo: 2010 }))).toBe(true);
  });

  it('only says so when showing every year would bring something back', () => {
    let said = 0;
    for (const over of combos) {
      for (const sel of selections) {
        const s = settings({ ...over, hideEmptyYears: true });
        if (!aloneAfterHiding(career(), s, sel)) continue;
        said++;
        const lit = (f: SpineFilm) => isLit(f, sel, s.minRating);
        const hidden = layoutGrid(career(), 1280, s, lit);
        const shown = layoutGrid(career(), 1280, { ...s, hideEmptyYears: false }, lit);
        expect({ over, sel: [...sel], cards: hidden.cards.map((c) => c.film.id) }).toEqual({
          over,
          sel: [...sel],
          cards: ['tt0000001'],
        });
        expect(shown.cards.length).toBeGreaterThan(1);
      }
    }
    // Not a loop over nothing: the combinations do reach the case.
    expect(said).toBeGreaterThan(0);
  });

  it('agrees with the layout about how many years a collapsed map shows', () => {
    for (const over of combos) {
      for (const sel of selections) {
        const s = settings({ ...over, hideEmptyYears: true });
        const laid = layoutGrid(career(), 1280, s, (f) => isLit(f, sel, s.minRating));
        const rows = laid.rows.filter((r) => !r.isBreak).length;
        const p = career();
        const years = new Set([p.anchor.year, ...litOthers(p, s, sel).map((f) => f.year)]).size;
        expect({ over, sel: [...sel], years }).toEqual({ over, sel: [...sel], years: rows });
      }
    }
  });
});

describe('emptyYearCount', () => {
  /** Two people across five years: a film of the second person's in the
   *  searched film's year, two in 2008, an unrated film in 1990 and a
   *  film in 1971 that the ranges below crop. */
  const career = () =>
    payloadOf({ id: 'tt0000001', year: 2005, rating: 7 }, [
      { id: 'tt0000001', year: 2005, rating: 7, people: [0, 1] },
      { id: 'tt0000002', year: 2005, rating: 6, people: [1] },
      { id: 'tt0000003', year: 1990, rating: null, people: [0] },
      { id: 'tt0000004', year: 2008, rating: 8.2, people: [1] },
      { id: 'tt0000005', year: 2008, rating: 6.1, people: [0] },
      { id: 'tt0000006', year: 2010, rating: 7.5, people: [1] },
      { id: 'tt0000007', year: 1971, rating: 8.8, people: [0] },
    ], castOf(2));

  it('is 0 with no filter', () => {
    expect(emptyYearCount(career(), settings(), new Set())).toBe(0);
  });

  it('counts only the years where nothing is lit', () => {
    // Person 1 has nothing in 1971 or 1990.
    expect(emptyYearCount(career(), settings(), new Set([1]))).toBe(2);
    // Person 0 has nothing in 2010; 2008 holds one of theirs beside one
    // of person 1's, and one lit film is enough to keep a year.
    expect(emptyYearCount(career(), settings(), new Set([0]))).toBe(1);
    // At 8.0 and up: the unrated 1990 film and 2010's 7.5.
    expect(emptyYearCount(career(), settings({ minRating: 8 }), new Set())).toBe(2);
    expect(emptyYearCount(career(), settings({ minRating: 8 }), new Set([1]))).toBe(3);
  });

  it('never counts the searched film’s year', () => {
    // Nobody on the map chosen, and a floor nothing clears: every year
    // is empty but 2005, whose other film is dark too.
    expect(emptyYearCount(career(), settings({ minRating: 9 }), new Set([9]))).toBe(4);
    // An unrated searched film clears no floor, and still counts as lit.
    const unrated = payloadOf({ id: 'tt0000001', year: 2005, rating: null }, [
      { id: 'tt0000001', year: 2005, rating: null, people: [0] },
      { id: 'tt0000002', year: 2006, rating: 6, people: [0] },
    ], castOf(1));
    expect(emptyYearCount(unrated, settings({ minRating: 7 }), new Set())).toBe(1);
  });

  it('ignores films the year range or the unrated column has taken off the plot', () => {
    // 1971 and 1990 are before the range, so there is nothing of theirs
    // for person 1 to be missing from.
    expect(emptyYearCount(career(), settings({ yearFrom: 2000 }), new Set([1]))).toBe(0);
    // The unrated 1990 film goes with its column; 1971 is still there.
    expect(emptyYearCount(career(), settings({ showUnrated: false }), new Set([1]))).toBe(1);
    // A range that leaves out the searched film's year: it stays on the
    // plot, lit, and 2008 and 2010 are empty for person 0 at 7.0 and up.
    expect(emptyYearCount(career(), settings({ yearFrom: 2006, minRating: 7 }), new Set([0]))).toBe(2);
  });

  it('is how many rows hiding the empty years takes off the layout', () => {
    const selections = [new Set<number>(), new Set([0]), new Set([1]), new Set([0, 1]), new Set([9])];
    for (const showUnrated of [true, false])
      for (const [yearFrom, yearTo] of [[null, null], [2000, null], [null, 2006], [2006, 2009]] as const)
        for (const minRating of [null, 7, 8.5])
          for (const sel of selections) {
            const s = settings({ showUnrated, yearFrom, yearTo, minRating });
            const lit = (f: SpineFilm) => isLit(f, sel, minRating);
            const rows = (hideEmptyYears: boolean) =>
              layoutGrid(career(), 1280, { ...s, hideEmptyYears }, lit).rows.filter((r) => !r.isBreak).length;
            const over = { showUnrated, yearFrom, yearTo, minRating, sel: [...sel] };
            expect({ over, n: emptyYearCount(career(), s, sel) }).toEqual({ over, n: rows(false) - rows(true) });
          }
  });
});

describe('isLit', () => {
  const film = (over: Partial<SpineFilm> = {}): SpineFilm => ({
    id: 'tt0000002',
    year: 2000,
    rating: 7,
    md: 0,
    people: [1],
    isAnchor: false,
    ...over,
  });

  it('always lights the searched film', () => {
    expect(isLit(film({ isAnchor: true, rating: null }), new Set([5]), 9)).toBe(true);
  });

  it('fails an unrated film against any floor', () => {
    expect(isLit(film({ rating: null }), new Set(), 6)).toBe(false);
    expect(isLit(film({ rating: null }), new Set(), null)).toBe(true);
  });

  it('lights everything when nobody is selected', () => {
    expect(isLit(film(), new Set(), null)).toBe(true);
  });

  it('asks whether the selected person is on it', () => {
    expect(isLit(film(), new Set([1]), null)).toBe(true);
    expect(isLit(film(), new Set([0]), null)).toBe(false);
  });
});

describe('rungLabel', () => {
  it('says Any for no floor, and the floor with a plus otherwise', () => {
    expect(rungLabel(null)).toBe('Any');
    expect(RATING_STOPS.map(rungLabel)).toEqual(['6.0+', '6.5+', '7.0+', '7.5+', '8.0+', '8.5+']);
  });
});

describe('what the header says is narrowing the map', () => {
  it('reads the floor, then the range', () => {
    expect(activeFilters(settings(), false)).toBe('');
    expect(activeFilters(settings({ minRating: 7 }), false)).toBe('');
    expect(activeFilters(settings({ minRating: 7 }), true)).toBe('7.0+');
    expect(activeFilters(settings({ yearFrom: 2000, yearTo: 2026 }), false)).toBe('2000–2026');
    expect(activeFilters(settings({ yearFrom: 2000 }), false)).toBe('From 2000');
    expect(activeFilters(settings({ yearTo: 2012 }), false)).toBe('To 2012');
    expect(activeFilters(settings({ minRating: 7, yearFrom: 2000 }), true)).toBe(
      '7.0+ · From 2000',
    );
  });

  // The pill is for what is hidden from the reader. Hiding the empty
  // years hides no movie — it closes the gaps between the rows that are
  // already there, and the closing is the evidence.
  it('says nothing about hiding the empty years', () => {
    expect(activeFilters(settings({ hideEmptyYears: true }), false)).toBe('');
    expect(activeFilters(settings({ hideEmptyYears: true, yearFrom: 2000 }), false)).toBe(
      'From 2000',
    );
    expect(changedCount(settings({ hideEmptyYears: true }), true)).toBe(0);
  });

  it('counts a range once, however many ends it has', () => {
    expect(changedCount(settings(), true)).toBe(0);
    expect(changedCount(settings({ yearFrom: 2000 }), true)).toBe(1);
    expect(changedCount(settings({ yearFrom: 2000, yearTo: 2010 }), true)).toBe(1);
    expect(
      changedCount(settings({ yearFrom: 2000, yearTo: 2010, hideEmptyYears: true }), true),
    ).toBe(1);
    expect(changedCount(settings({ yearOrder: 'newest', showUnrated: false }), true)).toBe(2);
    // The floor counts only where it is changed from.
    expect(changedCount(settings({ minRating: 7 }), true)).toBe(1);
    expect(changedCount(settings({ minRating: 7 }), false)).toBe(0);
  });
});

describe('the pill’s ✕', () => {
  it('clears a floor the pill names, on a phone, when it is all the pill says', () => {
    // The reported case: a pill reading "6.5+" whose ✕ did nothing.
    const s = settings({ minRating: 6.5 });
    expect(activeFilters(s, true)).toBe('6.5+');
    const cleared = withoutPill(s, true);
    expect(cleared.minRating).toBeNull();
    expect(activeFilters(cleared, true)).toBe('');
  });

  it('clears the floor and the range together, leaving no pill behind', () => {
    const s = settings({ minRating: 7, yearFrom: 2000, yearTo: 2010 });
    expect(activeFilters(withoutPill(s, true), true)).toBe('');
  });

  it('leaves a floor alone where the header’s own rungs show it', () => {
    const s = settings({ minRating: 7, yearFrom: 2000 });
    const cleared = withoutPill(s, false);
    expect(cleared.minRating).toBe(7);
    expect(cleared.yearFrom).toBeNull();
    expect(activeFilters(cleared, false)).toBe('');
  });

  it('touches nothing the pill does not name', () => {
    const s = settings({
      minRating: 8,
      yearFrom: 1990,
      hideEmptyYears: true,
      yearOrder: 'newest',
      showUnrated: false,
      highlightYear: false,
    });
    for (const rungsInView of [true, false]) {
      const { minRating: _m, yearFrom: _f, yearTo: _t, ...rest } = withoutPill(s, rungsInView);
      const { minRating: _m2, yearFrom: _f2, yearTo: _t2, ...before } = s;
      expect(rest).toEqual(before);
    }
  });

  // Whatever the pill says, its ✕ takes all of it away: the two are
  // one rule, and a pill that survives its own ✕ is a dead control.
  it('never survives its own ✕', () => {
    for (const minRating of [null, ...RATING_STOPS])
      for (const [yearFrom, yearTo] of [[null, null], [2000, null], [null, 2010], [2000, 2010]] as const)
        for (const rungsInView of [true, false]) {
          const s = settings({ minRating, yearFrom, yearTo });
          expect({ s, rungsInView, pill: activeFilters(withoutPill(s, rungsInView), rungsInView) }).toEqual({
            s,
            rungsInView,
            pill: '',
          });
        }
  });
});

describe('the year slider keys', () => {
  const map = { a: 2000, b: 2010, lo: 1990, hi: 2020 };

  it('moves a year at a time and ten at a time', () => {
    expect(keyYear('from', 'PageUp', map)).toBe(2010);
    expect(keyYear('from', 'ArrowRight', map)).toBe(2001);
    expect(keyYear('from', 'ArrowLeft', map)).toBe(1999);
    expect(keyYear('to', 'PageDown', map)).toBe(2000);
  });

  it('opens the side when a thumb reaches the end of the map', () => {
    expect(keyYear('from', 'Home', map)).toBe(null);
    expect(keyYear('to', 'End', map)).toBe(null);
  });

  it('stops each thumb at the other one', () => {
    expect(keyYear('from', 'End', map)).toBe(2010);
    expect(keyYear('to', 'Home', map)).toBe(2000);
    expect(keyYear('from', 'PageUp', { ...map, a: 2008 })).toBe(2010);
  });

  it('leaves a key it does not own alone', () => {
    expect(keyYear('from', 'Enter', map)).toBe(undefined);
  });
});


describe('the spine', () => {
  it('reads the server tuples into films that know where they go', () => {
    const p = payloadOf({ id: 'tt0000001', year: 1999, rating: 8, md: 331 }, [
      { id: 'tt0000001', year: 1999, rating: 8, md: 331 },
      { id: 'tt0000002', year: 2001, rating: null },
    ]);
    const spine = spineOf(p);
    expect(spine).toEqual([
      { id: 'tt0000001', year: 1999, rating: 8, md: 331, people: [], isAnchor: true },
      { id: 'tt0000002', year: 2001, rating: null, md: 0, people: [], isAnchor: false },
    ]);
  });

  it('is the whole grid, so a layout is final the first time', () => {
    const once = layoutGrid(real, 1280);
    const again = layoutGrid(real, 1280);
    expect(again.cards.map((c) => [c.film.id, c.left, c.top])).toEqual(
      once.cards.map((c) => [c.film.id, c.left, c.top]),
    );
    expect(once.cards.length).toBe(real.films.length);
  });
});

describe('the warm band', () => {
  it('is the screen in front of the reader, plus one screen above and below', () => {
    for (const viewH of [480, 720, 900, 1400]) {
      const at = 2000;
      const span = warmSpan(at, viewH);
      expect(span.top).toBe(at - viewH);
      expect(span.bottom).toBe(at + viewH * 2);
      expect(span.bottom - span.top).toBe(viewH * 3);
    }
  });

  it('follows the reader, including back to the top', () => {
    expect(warmSpan(0, 800)).toEqual({ top: -800, bottom: 1600 });
    expect(warmSpan(400, 800).top).toBe(-400);
    expect(warmSpan(5000, 800).top).toBe(4200);
  });

  it('keeps a card that only just enters the band, and drops one past it', () => {
    const span = warmSpan(1000, 800);
    const cardH = 90;
    expect(inWarmSpan(span.top - cardH + 1, cardH, span)).toBe(true);
    expect(inWarmSpan(span.top - cardH, cardH, span)).toBe(false);
    expect(inWarmSpan(span.bottom - 1, cardH, span)).toBe(true);
    expect(inWarmSpan(span.bottom, cardH, span)).toBe(false);
    // The screen itself, and the full screen on either side of it.
    expect(inWarmSpan(1000, cardH, span)).toBe(true);
    expect(inWarmSpan(1000 - 800, cardH, span)).toBe(true);
    expect(inWarmSpan(1000 + 800, cardH, span)).toBe(true);
  });
});

describe('the rating floor', () => {
  it('keeps everything when there is no floor', () => {
    expect(passesFloor(2, null)).toBe(true);
    expect(passesFloor(9, null)).toBe(true);
  });

  it('keeps a rating at the floor, and drops what is under it', () => {
    expect(passesFloor(7, 7)).toBe(true);
    expect(passesFloor(6.9, 7)).toBe(false);
  });

  it('dims rather than removes: every card stays exactly where it was', () => {
    const all = layoutGrid(real, 1280, settings());
    const high = layoutGrid(real, 1280, settings({ minRating: 8 }));
    expect(high.cards.length).toBe(all.cards.length);
    expect(high.cards.map((c) => [c.film.id, c.left, c.top])).toEqual(
      all.cards.map((c) => [c.film.id, c.left, c.top]),
    );
    expect(high.rows.map((r) => [r.year, r.top, r.height])).toEqual(
      all.rows.map((r) => [r.year, r.top, r.height]),
    );
  });

  it('counts an unrated film as below any floor, and lit when there is none', () => {
    expect(passesFloor(null, null)).toBe(true);
    expect(passesFloor(null, 6)).toBe(false);
  });

  it('always keeps the searched film, whatever the floor', () => {
    const strict = layoutGrid(real, 1280, settings({ minRating: 8.5 }));
    expect(strict.anchor?.film.id).toBe(real.anchor.id);
  });

  it('leaves fewer rows, and none empty', () => {
    const high = layoutGrid(real, 1280, settings({ minRating: 8 }));
    expect(high.rows.every((r) => r.lanes > 0)).toBe(true);
    const years = new Set(high.cards.map((c) => c.film.year));
    expect(high.rows.length).toBe(years.size);
  });
});

describe('opacityOf', () => {
  /** People are places in the chip row, as the spine names them. */
  const card = (rating: number | null, people: number[], isAnchor = false): Placed => ({
    film: { id: 'tt0000001', year: 2000, rating, md: 0, people, isAnchor },
    left: 0,
    top: 0,
    lane: 0,
  });
  const none = new Set<number>();

  it('lights everything when nothing is narrowing the grid', () => {
    expect(opacityOf(card(5, [0]), none, null, null)).toBe(1);
    expect(opacityOf(card(null, [0]), none, null, null)).toBe(1);
  });

  it('dims a film under the floor and lights one at it', () => {
    expect(opacityOf(card(7, [0]), none, null, 7)).toBe(1);
    expect(opacityOf(card(6.9, [0]), none, null, 7)).toBeLessThan(1);
    expect(opacityOf(card(null, [0]), none, null, 7)).toBeLessThan(1);
  });

  it('asks for both the person and the rating', () => {
    const selected = new Set([0]);
    expect(opacityOf(card(8, [0]), selected, null, 7)).toBe(1);
    expect(opacityOf(card(8, [1]), selected, null, 7)).toBeLessThan(1);
    expect(opacityOf(card(5, [0]), selected, null, 7)).toBeLessThan(1);
  });

  it('keeps the searched film lit, whatever is asked for', () => {
    expect(opacityOf(card(2, [8], true), new Set([0]), null, 9)).toBe(1);
    expect(opacityOf(card(2, [8], true), none, 0, 9)).toBe(1);
  });

  it('previews one person on hover, still honouring the floor', () => {
    expect(opacityOf(card(8, [2]), none, 2, 7)).toBe(1);
    expect(opacityOf(card(4, [2]), none, 2, 7)).toBeLessThan(1);
    expect(opacityOf(card(8, [3]), none, 2, 7)).toBeLessThan(1);
  });

  it('lets a hover override the selection while the pointer is on it', () => {
    // Selected someone else; previewing this card's person lights it.
    expect(opacityOf(card(8, [2]), new Set([0]), 2, null)).toBe(1);
    // Selected this card's person; previewing someone else dims it.
    expect(opacityOf(card(8, [0]), new Set([0]), 2, null)).toBeLessThan(1);
  });
});

describe('a year reads as a calendar', () => {
  /** Every pair where a later date sits above an earlier one. */
  /** Cards inside one year that sit against the direction the axis
   *  runs. Time flows one way down the whole page, so which way that is
   *  depends on the year order the reader asked for. */
  function inversions(l: ReturnType<typeof layoutGrid>, newestFirst = false): number {
    const byYear = new Map<number, typeof l.cards>();
    for (const c of l.cards) {
      const list = byYear.get(c.film.year);
      if (list) list.push(c);
      else byYear.set(c.film.year, [c]);
    }
    let bad = 0;
    for (const [, list] of byYear) {
      for (const a of list) {
        for (const b of list) {
          if (a.top >= b.top) continue;
          const later = dateOrd(a.film) > dateOrd(b.film);
          if (newestFirst ? !later && dateOrd(a.film) !== dateOrd(b.film) : later) bad++;
        }
      }
    }
    return bad;
  }

  it('never sits a later film above an earlier one, at any width', () => {
    for (const w of WIDTHS) {
      expect(inversions(layoutGrid(real, w)), `${w}px`).toBe(0);
    }
  });

  it('holds when the reader asks for the newest year first', () => {
    // The years run the other way; inside one, the calendar does not.
    expect(inversions(layoutGrid(real, 1280, settings({ yearOrder: 'newest' })), true)).toBe(0);
  });

  it('still packs a row rather than giving every film its own lane', () => {
    const l = layoutGrid(real, 1280);
    const busiest = l.rows.reduce((m, r) => Math.max(m, r.lanes), 0);
    const inBusiest = l.cards.filter(
      (c) => c.film.year === l.rows.find((r) => r.lanes === busiest)!.year,
    ).length;
    expect(busiest).toBeLessThan(inBusiest);
  });

  it('keeps a card at its own rating, which the ordering must not disturb', () => {
    const l = layoutGrid(real, 1280);
    const slack = l.metrics.cardW * NUDGE_RATIO + 1;
    for (const c of l.cards) {
      if (c.film.rating == null) continue;
      const centre = c.left + l.metrics.cardW / 2;
      expect(Math.abs(centre - xOf(c.film.rating, l.metrics)), String(c.film.id)).toBeLessThanOrEqual(slack);
    }
  });
});

describe('fitLane with a floor', () => {
  it('will not drop a card into a lane above the one before it', () => {
    // Lane 0 is free at this x, but the previous card sat in lane 2.
    expect(fitLane([0, 900, 900], 100, 116, 2).lane).toBe(3);
    expect(fitLane([0, 900, 900], 100, 116, 0).lane).toBe(0);
  });

  it('takes the first lane at or after the floor that has room', () => {
    expect(fitLane([900, 0, 0], 100, 116, 1)).toEqual({ lane: 1, left: 100 });
  });

  it('opens a new lane rather than lying about the rating', () => {
    const { lane, left } = fitLane([900, 900], 100, 116, 1);
    expect(lane).toBe(2);
    expect(left).toBe(100);
  });
});

describe('no film tallies', () => {
  // §1.6 and check 12: the map is open-ended. A number that tallies the
  // films on it makes it look like a list with an end.
  //
  // Three counts are let through on purpose, because the refresh brings
  // them back. The card's "+3" counts the people one card had no room
  // to draw. The chip's count (`cd-chip-count`, PeopleChips.tsx only)
  // says how many of one person's films the map holds, which tells the
  // reader whose work the map is mostly made of; it is a person's share
  // of the map, not a total for it. The View panel's histogram draws
  // films per year as bars over the year slider, with no number on any
  // of them: it shows where the work is before a reader picks years.
  // None of them says how many films there are, and nothing else may.
  const sources = import.meta.glob('./{GridApp,GridMap,GridSheet,PeopleChips,ViewPanel}.tsx', {
    query: '?raw',
    import: 'default',
    eager: true,
  }) as Record<string, string>;

  it('reads the components it is guarding', () => {
    expect(Object.keys(sources).length).toBe(5);
  });

  it('has no film tally, and a chip count only on the chips', () => {
    for (const [path, src] of Object.entries(sources)) {
      if (!path.endsWith('/PeopleChips.tsx')) expect(src, path).not.toContain('cd-chip-count');
      expect(src, path).not.toMatch(/films\.length\}/);
      expect(src, path).not.toMatch(/\{[^}]*\}\s*films/);
    }
  });
});

describe('revealDelay', () => {
  const at = (left: number, top: number, isAnchor = false): Placed => ({
    film: {
      id: `tt${String(left + top).padStart(7, '0')}`,
      year: 2000,
      rating: 7,
      md: 0,
      people: [],
      isAnchor,
    },
    left,
    top,
    lane: 0,
  });

  it('lets the searched film appear at once', () => {
    const anchor = at(100, 100, true);
    expect(revealDelay(anchor, anchor)).toBe(0);
  });

  it('makes a card wait for its distance from the searched film', () => {
    const anchor = at(0, 0, true);
    // 3-4-5: 300px away, at 0.35ms a pixel.
    expect(revealDelay(at(180, 240), anchor)).toBe(105);
  });

  it('measures the same distance in every direction', () => {
    const anchor = at(400, 400, true);
    expect(revealDelay(at(400, 100), anchor)).toBe(revealDelay(at(400, 700), anchor));
    expect(revealDelay(at(100, 400), anchor)).toBe(revealDelay(at(700, 400), anchor));
  });

  it('caps the wait, so the far corner of a long map still arrives', () => {
    expect(revealDelay(at(9000, 9000), at(0, 0, true))).toBe(REVEAL_MAX_MS);
  });

  it('has nothing to measure from before the anchor is placed', () => {
    expect(revealDelay(at(500, 500), null)).toBe(0);
  });

  it('holds the whole spread back on a map a flown card lands on', () => {
    // The landing starts first; the rest of the map spreads from the
    // new searched card 220 ms into it. The searched card itself is
    // never held back: it is the one the copy lands on.
    const anchor = at(0, 0, true);
    expect(revealDelay(at(180, 240), anchor, 220)).toBe(220 + 105);
    expect(revealDelay(at(9000, 9000), anchor, 220)).toBe(220 + REVEAL_MAX_MS);
    expect(revealDelay(anchor, anchor, 220)).toBe(0);
  });
});

describe('nothingLit', () => {
  /** A spine film; people are places in the chip row. */
  const film = (id: number, rating: number | null, people: number[], isAnchor = false): SpineFilm => ({
    id: `tt${String(id).padStart(7, '0')}`,
    year: 2000,
    rating,
    md: 0,
    isAnchor,
    people,
  });

  it('says so when every film of theirs sits below the floor', () => {
    expect(nothingLit([film(1, 5.2, [7]), film(2, 6.1, [7])], 7, 7)).toBe(true);
  });

  it('says nothing when one of theirs clears it', () => {
    expect(nothingLit([film(1, 5.2, [7]), film(2, 8.4, [7])], 7, 7)).toBe(false);
  });

  it('ignores films that are not theirs', () => {
    expect(nothingLit([film(1, 9.0, [8]), film(2, 5.0, [7])], 7, 7)).toBe(true);
  });

  it('leaves the searched film out of it: it is lit whatever the floor', () => {
    // Its own rating clearing the floor would not light anything else.
    expect(nothingLit([film(1, 9.0, [7], true), film(2, 5.0, [7])], 7, 7)).toBe(true);
  });

  it('makes no claim about someone with no other film on the map', () => {
    expect(nothingLit([film(1, 5.0, [8])], 7, 7)).toBe(false);
    expect(nothingLit([film(1, 9.0, [7], true)], 7, 7)).toBe(false);
    expect(nothingLit([], 7, 7)).toBe(false);
  });

  it('counts an unrated film as below the floor', () => {
    expect(nothingLit([film(1, null, [7])], 7, 6)).toBe(true);
  });

  it('counts a film of theirs far down the spine', () => {
    // The spine is every film on the map from the first paint, not the
    // few cards somebody has scrolled to. One that clears the floor at
    // the far end of a long career still means something of theirs is lit.
    const spine = [
      film(1, 8.7, [7], true),
      ...Array.from({ length: 60 }, (_, i) => film(i + 2, 5.5, [7])),
      film(99, 8.1, [7]),
    ];
    expect(nothingLit(spine, 7, 8)).toBe(false);
    expect(nothingLit(spine.slice(0, -1), 7, 8)).toBe(true);
  });
});

describe('onPlot', () => {
  const film = (year: number, rating: number | null, isAnchor = false): SpineFilm => ({
    id: `tt${String(year).padStart(7, '0')}`,
    year,
    rating,
    md: 0,
    isAnchor,
    people: [0],
  });

  it('holds every film when nothing is cropped', () => {
    expect(onPlot(film(1950, 7), DEFAULT_SETTINGS)).toBe(true);
    expect(onPlot(film(1950, null), DEFAULT_SETTINGS)).toBe(true);
  });

  it('takes unrated films off with their column', () => {
    const s = { ...DEFAULT_SETTINGS, showUnrated: false };
    expect(onPlot(film(2000, null), s)).toBe(false);
    expect(onPlot(film(2000, 6.1), s)).toBe(true);
  });

  it('crops outside the year range, inclusive at both ends', () => {
    const s = { ...DEFAULT_SETTINGS, yearFrom: 2000, yearTo: 2010 };
    expect(onPlot(film(1999, 8), s)).toBe(false);
    expect(onPlot(film(2000, 8), s)).toBe(true);
    expect(onPlot(film(2010, 8), s)).toBe(true);
    expect(onPlot(film(2011, 8), s)).toBe(false);
  });

  it('never drops the searched film', () => {
    const s = { ...DEFAULT_SETTINGS, showUnrated: false, yearFrom: 2000, yearTo: 2010 };
    expect(onPlot(film(1980, null, true), s)).toBe(true);
  });

  it('is the ground the layout stands on', () => {
    const anchor = { id: 'tt0000001', year: 2005, rating: 7 };
    const payload = payloadOf(anchor, [
      anchor,
      { id: 'tt0000002', year: 1995, rating: 8.5 },
      { id: 'tt0000003', year: 2003, rating: null },
      { id: 'tt0000004', year: 2008, rating: 6.5 },
    ]);
    const s = { ...DEFAULT_SETTINGS, showUnrated: false, yearFrom: 2000, yearTo: 2010 };
    const laid = layoutGrid(payload, 1200, s).cards.map((c) => c.film.id).sort();
    const held = spineOf(payload).filter((f) => onPlot(f, s)).map((f) => f.id).sort();
    expect(laid).toEqual(held);
  });

  it('keeps a cropped film out of the floor toast', () => {
    // Her only film over the floor is outside the range. Every card of
    // hers on the page is dark, and the toast has to be able to say so.
    const spine = [
      { ...film(2005, 7, true), people: [0, 1] },
      { ...film(1995, 8.5), people: [1] },
      { ...film(2003, 7.0), people: [1] },
      { ...film(2008, 6.5), people: [1] },
    ];
    const s = { ...DEFAULT_SETTINGS, yearFrom: 2000, yearTo: 2010 };
    expect(nothingLit(spine, 1, 8)).toBe(false);
    expect(nothingLit(spine.filter((f) => onPlot(f, s)), 1, 8)).toBe(true);
  });
});
