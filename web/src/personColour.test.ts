import { describe, expect, it } from 'vitest';
import matrix from './fixtures/matrix-grid.json';
import type { GridPayload, GridPerson } from './grid';
import {
  HUES,
  NOTHING_SHOWN,
  assignHues,
  carriedFirst,
  carriedFrom,
  hueFor,
  nextShown,
  personColour,
  personVars,
  swatchRadius,
} from './personColour';

const real = matrix as unknown as GridPayload;

/** Memento's people as the server lists them: its director, then its cast. */
const memento: GridPerson[] = [
  { id: 'nm0634240', name: 'Christopher Nolan', role: 'director' },
  { id: 'nm0001602', name: 'Guy Pearce', role: 'cast' },
  { id: 'nm0005251', name: 'Carrie-Anne Moss', role: 'cast' },
  { id: 'nm0001592', name: 'Joe Pantoliano', role: 'cast' },
];

const people = (n: number) => Array.from({ length: n }, (_, i) => ({ id: `nm${String(i).padStart(7, '0')}` }));

describe('assignHues', () => {
  it('colours people in the order the map lists them', () => {
    const table = new Map<string, number>();
    assignHues(real.people, table);
    // The handoff's own map: the Wachowskis, Keanu Reeves, Laurence Fishburne …
    expect(real.people.slice(0, 4).map((p) => hueFor(p.id, table))).toEqual([205, 232, 78, 28]);
  });

  it('lets people keep their colour on the next map, and colours the new ones on from there', () => {
    // Screenshot 06: after The Matrix, Moss and Pantoliano keep theirs,
    // and Nolan and Pearce take the thirteenth and fourteenth hues.
    const table = new Map<string, number>();
    assignHues(real.people, table);
    assignHues(memento, table);
    const [nolan, pearce, moss, joe] = memento.map((p) => hueFor(p.id, table));
    expect([moss, joe]).toEqual([345, 255]);
    expect([nolan, pearce]).toEqual([95, 165]);
  });

  it('starts the list again after sixteen people', () => {
    const table = new Map<string, number>();
    const crowd = people(18);
    assignHues(crowd, table);
    expect(hueFor(crowd[15].id, table)).toBe(HUES[15]);
    expect(hueFor(crowd[16].id, table)).toBe(HUES[0]);
    expect(hueFor(crowd[17].id, table)).toBe(HUES[1]);
  });

  it('changes nothing when the same map is coloured twice', () => {
    // A second render of the same map must give the same answer.
    const table = new Map<string, number>();
    assignHues(real.people, table);
    const once = [...table];
    assignHues(real.people, table);
    assignHues([...real.people].reverse(), table);
    expect([...table]).toEqual(once);
  });

  it('gives anyone not met yet the first hue', () => {
    expect(hueFor('nm9999999', new Map())).toBe(HUES[0]);
  });
});

describe('personColour', () => {
  const table = new Map([['nm0000001', 2]]);

  it('is the handoff’s light-on-dark tone in the dark theme', () => {
    expect(personColour('nm0000001', 'dark', table)).toBe('oklch(0.78 0.12 78)');
  });

  it('is the darker, stronger tone on paper', () => {
    expect(personColour('nm0000001', 'light', table)).toBe('oklch(0.55 0.15 78)');
  });
});

describe('personVars', () => {
  const table = new Map([
    ['nm0000001', 0],
    ['nm0000002', 1],
  ]);

  it('gives cast a round swatch and a director a square one', () => {
    expect(swatchRadius('cast')).toBe('50%');
    expect(swatchRadius('director')).toBe('2px');
  });

  it('sets the colour and the corner as custom properties', () => {
    expect(personVars({ id: 'nm0000001', role: 'director' }, 'dark', table)).toEqual({
      '--tone': 'oklch(0.78 0.12 205)',
      '--swatch-r': '2px',
    });
    expect(personVars({ id: 'nm0000002', role: 'cast' }, 'light', table)).toEqual({
      '--tone': 'oklch(0.55 0.15 232)',
      '--swatch-r': '50%',
    });
  });
});

describe('carriedFrom', () => {
  it('carries nobody from the opening screen', () => {
    expect(carriedFrom(null, memento).size).toBe(0);
  });

  it('carries the people on both maps', () => {
    const shown = real.people.map((p) => p.id);
    expect([...carriedFrom(shown, memento)]).toEqual(['nm0005251', 'nm0001592']);
  });
});

describe('carriedFirst', () => {
  it('puts the carried people first, each group in the map’s own order', () => {
    const order = carriedFirst(memento, new Set(['nm0001592', 'nm0005251'])).map((p) => p.name);
    // Screenshot 06's chip row.
    expect(order).toEqual(['Carrie-Anne Moss', 'Joe Pantoliano', 'Christopher Nolan', 'Guy Pearce']);
  });

  it('leaves the map’s own list as it was', () => {
    // The spine names people by their place in it.
    const before = memento.map((p) => p.id);
    const out = carriedFirst(memento, new Set(['nm0001592']));
    expect(memento.map((p) => p.id)).toEqual(before);
    expect(out).not.toBe(memento);
  });

  it('is the map’s order when nobody is carried', () => {
    expect(carriedFirst(memento, new Set())).toEqual(memento);
  });
});

describe('nextShown', () => {
  const matrixMap = { anchor: { id: 'tt0133093' }, people: real.people };
  const mementoMap = { anchor: { id: 'tt0209144' }, people: memento };

  it('carries nobody onto the first map', () => {
    const now = nextShown(NOTHING_SHOWN, 'tt0133093', matrixMap);
    expect(now.id).toBe('tt0133093');
    expect(now.carried.size).toBe(0);
  });

  it('carries the people shared with the map shown before', () => {
    const first = nextShown(NOTHING_SHOWN, 'tt0133093', matrixMap);
    const second = nextShown(first, 'tt0209144', mementoMap);
    expect([...second.carried]).toEqual(['nm0005251', 'nm0001592']);
  });

  it('is the same object while nothing changes, so it can be set while rendering', () => {
    const first = nextShown(NOTHING_SHOWN, 'tt0133093', matrixMap);
    expect(nextShown(first, 'tt0133093', matrixMap)).toBe(first);
    // A load in progress, or one that failed, shows no map.
    expect(nextShown(first, 'tt0209144', null)).toBe(first);
    expect(nextShown(NOTHING_SHOWN, null, null)).toBe(NOTHING_SHOWN);
  });

  it('compares with the last map actually shown when a load in between failed', () => {
    const first = nextShown(NOTHING_SHOWN, 'tt0133093', matrixMap);
    const failed = nextShown(first, 'tt9999999', null);
    const next = nextShown(failed, 'tt0209144', mementoMap);
    expect([...next.carried]).toEqual(['nm0005251', 'nm0001592']);
  });

  it('forgets the map on the way home, even while the old one is still in hand', () => {
    const first = nextShown(NOTHING_SHOWN, 'tt0133093', matrixMap);
    // The route has gone home; the old payload is cleared a moment later.
    const home = nextShown(first, null, matrixMap);
    expect(home).toBe(NOTHING_SHOWN);
    // And it stays settled: no render loop between the two.
    expect(nextShown(home, null, matrixMap)).toBe(home);
    // A map opened from home carries nobody.
    expect(nextShown(home, 'tt0209144', mementoMap).carried.size).toBe(0);
  });
});
