import { describe, expect, it } from 'vitest';
import matrix from './fixtures/matrix-grid.json';
import {
  RING_MS,
  centredScroll,
  isFlown,
  opacityOf,
  openingBox,
  ringDelay,
  spreadDelays,
  wordsFor,
} from './GridMap';
import {
  DEFAULT_SETTINGS,
  isLit,
  layoutGrid,
  type GridFilm,
  type GridPayload,
  type Placed,
} from './grid';

/** A card whose spine says it holds these places in the chip row. */
function card(people: number[] = [], id = 'tt0000001', isAnchor = false): Placed {
  return {
    film: { id, year: 2000, rating: 7, md: 0, people, isAnchor },
    left: 0,
    top: 0,
    lane: 0,
  };
}

describe('opacityOf', () => {
  it('is full strength when nobody is being previewed or selected', () => {
    expect(opacityOf(card([0]), new Set(), null)).toBe(1);
  });

  it('dims every card when the preview is a person who is not on this grid', () => {
    // A leftover hover from the film just left looks like this: the
    // pointer is in the search field, so nothing will clear it. The id
    // is not in this chip row, so its place is -1.
    expect(opacityOf(card([0, 1]), new Set(), -1)).toBeLessThan(1);
  });

  it('keeps a card that holds the previewed person', () => {
    expect(opacityOf(card([0, 1]), new Set(), 1)).toBe(1);
  });

  it('judges a card from the spine, before its detail has arrived', () => {
    // Who is on a card is known from the first paint. A card scrolled
    // into view under a selection is dim at once rather than lit until
    // its words land and then dimmed.
    expect(opacityOf(card([1]), new Set([0]), null)).toBeLessThan(1);
    expect(opacityOf(card([1]), new Set([1]), null)).toBe(1);
    expect(opacityOf(card([1]), new Set(), 0)).toBeLessThan(1);
  });
});

describe('the card a copy takes off from', () => {
  it('is the tapped card on the map being left', () => {
    expect(isFlown(card([0], 'tt1'), 'tt1')).toBe(true);
    expect(isFlown(card([0], 'tt2'), 'tt1')).toBe(false);
    expect(isFlown(card([0], 'tt1'), null)).toBe(false);
  });

  it('is never the searched film the copy is landing on', () => {
    // On the next map the flown film is the searched one, hidden for the
    // landing by other means and shown when it lands.
    expect(isFlown(card([], 'tt1', true), 'tt1')).toBe(false);
  });
});

describe('wordsFor', () => {
  const anchor: GridFilm = {
    id: 'tt9',
    year: 1999,
    rating: 8.7,
    md: 0,
    people: ['p1'],
    isAnchor: true,
    title: 'The Matrix',
    poster: 'https://example.test/matrix.jpg',
  };

  it('gives the searched card its words from the map, before any detail', () => {
    // A copy lands on this card and hands over to it in one frame, so it
    // must never be an empty box waiting for its title.
    expect(wordsFor({ id: 'tt9', isAnchor: true }, new Map(), anchor)).toBe(anchor);
  });

  it('prefers the detail once it is here', () => {
    const said = { ...anchor, title: 'The Matrix (1999)' };
    expect(wordsFor({ id: 'tt9', isAnchor: true }, new Map([['tt9', said]]), anchor)).toBe(said);
  });

  it('leaves every other card to its detail', () => {
    expect(wordsFor({ id: 'tt2', isAnchor: false }, new Map(), anchor)).toBeUndefined();
  });
});

describe('spreadDelays', () => {
  it('delays the arrival only, never hover', () => {
    // One delay per transition .cd-card-entering lists: opacity and
    // transform wait their turn; translate, box-shadow and background do
    // not, so a card answers the pointer even mid-spread.
    expect(spreadDelays(340)).toBe('340ms, 340ms, 0s, 0s, 0s');
  });
});

describe('the ring after a Recenter', () => {
  it('waits for the glide to land, then plays for 900 ms', () => {
    expect(ringDelay(false)).toBe(420);
    expect(RING_MS).toBe(900);
  });

  it('starts at once when the map jumps instead of gliding', () => {
    // With reduced motion asked for, the scroll is instant, so the
    // searched card is already where it was taken.
    expect(ringDelay(true)).toBe(0);
  });
});

describe('centredScroll', () => {
  const real = matrix as unknown as GridPayload;
  const layout = layoutGrid(real, 390, DEFAULT_SETTINGS, undefined, true);
  const { cardW, cardH } = layout.metrics;
  const card = layout.anchor!;

  it('puts the searched card in the middle of what is left under the header', () => {
    const at = centredScroll(layout, { clientWidth: 390, clientHeight: 844 }, 116)!;
    // Its centre, on screen, is halfway down the 728px below the header.
    expect(116 + card.top + cardH / 2 - at.top).toBeCloseTo(116 + (844 - 116) / 2);
    // The Matrix is rated 8.7, far enough right that the plot's edge
    // stops the scroll first: it sits right of the middle, at 267 of a
    // 390px phone, as it does on the real map.
    expect(at.left).toBe(layout.plotW - 390);
    expect(card.left + cardW / 2 - at.left).toBeCloseTo(267);
  });

  it('goes no further than the plot, so a card near its end sits where the end leaves it', () => {
    // A screen taller than the whole plot cannot scroll at all.
    const tall = centredScroll(layout, { clientWidth: 390, clientHeight: layout.plotH + 500 }, 0)!;
    expect(tall.top).toBe(0);
    // Nor one as wide as it.
    const wide = centredScroll(layout, { clientWidth: layout.plotW, clientHeight: 844 }, 0)!;
    expect(wide.left).toBe(0);
    // And never past the far edge.
    const at = centredScroll(layout, { clientWidth: 390, clientHeight: 300 }, 0)!;
    expect(at.left).toBeLessThanOrEqual(layout.plotW - 390);
    expect(at.top).toBeLessThanOrEqual(layout.plotH - 300);
  });

  it('scrolls on past the plot for a card nudged over its right edge', () => {
    // A top-rated film nudged into a lane can hang past plotW; the page
    // scrolls that far, and so must the centring, or the card is cut off.
    const hanging = {
      ...layout,
      anchor: { ...card, left: layout.plotW - cardW + 17 },
      cards: [...layout.cards, { ...card, left: layout.plotW - cardW + 17 }],
    };
    const at = centredScroll(hanging, { clientWidth: 390, clientHeight: 844 }, 0)!;
    expect(at.left).toBe(layout.plotW + 17 - 390);
    // Its right edge is on the glass.
    expect(hanging.anchor.left + cardW - at.left).toBeLessThanOrEqual(390);
  });
});

describe('openingBox', () => {
  const real = matrix as unknown as GridPayload;
  // A phone's scroller: the whole window, the header lying over its top.
  const scroller = (h = 844) =>
    ({
      clientWidth: 390,
      clientHeight: h,
      clientLeft: 0,
      clientTop: 0,
      getBoundingClientRect: () => ({ left: 0, top: 0, width: 390, height: h }),
    }) as unknown as HTMLElement;

  it('is where the new map will centre its searched card, before that map is drawn', () => {
    const box = openingBox(real, DEFAULT_SETTINGS, scroller(), 116, true)!;
    const layout = layoutGrid(real, 390, DEFAULT_SETTINGS, (f) => isLit(f, new Set(), null), true);
    expect(box.width).toBe(layout.metrics.cardW);
    expect(box.height).toBe(layout.metrics.cardH);
    expect(box.left + box.width / 2).toBeCloseTo(267);
    expect(box.top + box.height / 2).toBeCloseTo(116 + (844 - 116) / 2);
  });

  it('follows the scroll wherever the plot stops it', () => {
    // A screen taller than the plot: the card is where the plot has it.
    const layout = layoutGrid(real, 390, DEFAULT_SETTINGS, undefined, true);
    const box = openingBox(real, DEFAULT_SETTINGS, scroller(layout.plotH + 500), 0, true)!;
    expect(box.top).toBeCloseTo(layout.anchor!.top);
  });
});
