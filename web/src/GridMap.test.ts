import { createElement, type ComponentProps, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import matrix from './fixtures/matrix-grid.json';
import {
  Card,
  RING_MS,
  centredScroll,
  filteredOut,
  isFlown,
  opacityOf,
  openingBox,
  reflowPlan,
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
import { LEAVE_REACH_PX } from './motion';
import { previewScheduler, type PreviewClock } from './preview';

/** A card whose spine says it holds these places in the chip row. */
function card(people: number[] = [], id = 'tt0000001', isAnchor = false): Placed {
  return {
    film: { id, year: 2000, rating: 7, md: 0, people, genres: 0, isAnchor },
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

describe('a filtered-out card', () => {
  const layout = layoutGrid(matrix as unknown as GridPayload, 390, DEFAULT_SETTINGS, undefined, true);
  const said: GridFilm = {
    id: 'tt0000001',
    year: 2000,
    rating: 7,
    md: 0,
    people: ['nm0000001'],
    isAnchor: false,
    title: 'Bound',
  };

  interface Drawn {
    disabled?: boolean;
    className: string;
    'aria-label': string;
    onClick: () => void;
    onMouseEnter: () => void;
    onMouseLeave: () => void;
  }

  /** The card's button as it would be drawn, with its handlers live.
   *  The card is called inside a render of its own, so its hooks have a
   *  component to belong to, and that render draws nothing: no DOM is
   *  needed to press it. */
  function drawn(over: Partial<ComponentProps<typeof Card>>): Drawn {
    const props: ComponentProps<typeof Card> = {
      card: card([1]),
      said,
      layout,
      people: new Map(),
      codes: new Map(),
      opacity: 1,
      eager: false,
      enter: null,
      ringed: false,
      theme: 'dark',
      onOpen: () => {},
      onHover: () => {},
      ...over,
    };
    type Tree = ReactElement<{ children: ReactElement<Drawn>[] }>;
    // memo keeps the function it wraps as `type`, which its typings do
    // not say for a plain function component.
    const render = (Card as unknown as { type: (p: typeof props) => Tree }).type;
    let tree = null as Tree | null;
    function Probe() {
      tree = render(props);
      return null;
    }
    renderToStaticMarkup(createElement(Probe));
    // A fragment: the button, then the searched tag beside it, if any.
    return tree!.props.children[0].props;
  }

  it('is one the selection or the floor dims, never the searched film', () => {
    expect(filteredOut(card([1]).film, new Set([0]), null)).toBe(true);
    expect(filteredOut(card([1]).film, new Set([1]), 8)).toBe(true);
    expect(filteredOut(card([1]).film, new Set([1]), null)).toBe(false);
    expect(filteredOut(card([1], 'tt9', true).film, new Set([0]), 9)).toBe(false);
  });

  it('is disabled, says so, and never opens or lights chips', () => {
    const c = card([1]);
    const selected = new Set([0]);
    const opened: string[] = [];
    const lit: string[][] = [];
    const b = drawn({
      card: c,
      opacity: opacityOf(c, selected, null),
      off: filteredOut(c.film, selected, null),
      onOpen: (id) => opened.push(id),
      onHover: (people) => lit.push(people),
    });
    expect(b.disabled).toBe(true);
    expect(b.className).toContain('cd-card-off');
    expect(b['aria-label']).toBe('Bound, 2000, rated 7.0, filtered out');
    b.onClick();
    b.onMouseEnter();
    expect(opened).toEqual([]);
    expect(lit).toEqual([]);
  });

  it('never opens a hover preview', () => {
    const c = card([1]);
    const rested: string[] = [];
    const b = drawn({
      card: c,
      off: filteredOut(c.film, new Set([0]), null),
      onPreview: (id, over) => rested.push(`${id} ${over}`),
    });
    b.onMouseEnter();
    expect(rested).toEqual([]);
    // Nor is it drawn as hovered for one.
    expect(drawn({ card: c, off: true, hover: true }).className).not.toContain('cd-card-hover');
  });

  it('tells the preview when the pointer comes to rest on it and when it leaves', () => {
    const c = card([1]);
    const rested: string[] = [];
    const b = drawn({ card: c, onPreview: (id, over) => rested.push(`${id} ${over}`) });
    b.onMouseEnter();
    b.onMouseLeave();
    expect(rested).toEqual([`${c.film.id} true`, `${c.film.id} false`]);
  });

  it('keeps its hover state while the pointer is on its preview', () => {
    expect(drawn({ hover: true }).className).toContain('cd-card-hover');
    expect(drawn({}).className).not.toContain('cd-card-hover');
  });

  it('stays enabled when only a chip preview dims it', () => {
    // Their person is chosen, so the selection lights it; the pointer
    // resting on someone else's chip dims it only while it rests there.
    const c = card([1]);
    const selected = new Set([1]);
    expect(opacityOf(c, selected, 2)).toBeLessThan(1);
    const opened: string[] = [];
    const b = drawn({
      card: c,
      opacity: opacityOf(c, selected, 2),
      off: filteredOut(c.film, selected, null),
      onOpen: (id) => opened.push(id),
    });
    expect(b.disabled).toBeUndefined();
    expect(b.className).not.toContain('cd-card-off');
    expect(b['aria-label']).toBe('Bound, 2000, rated 7.0');
    b.onClick();
    expect(opened).toEqual([c.film.id]);
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

describe('reflowPlan', () => {
  // The Matrix at 924 wide, 418 of it on screen, lighting what is rated
  // 7 and up: hiding the empty years takes 18 of its 45 years away.
  const real = matrix as unknown as GridPayload;
  const lit = (f: Parameters<typeof isLit>[0]) => isLit(f, new Set(), 7);
  const shown = layoutGrid(real, 924, DEFAULT_SETTINGS, lit);
  const hidden = layoutGrid(real, 924, { ...DEFAULT_SETTINGS, hideEmptyYears: true }, lit);
  const viewH = 418;
  const at = centredScroll(shown, { clientWidth: 924, clientHeight: viewH }, 0)!.top;
  const plan = reflowPlan(shown, hidden, at, viewH, 0);
  const cardH = shown.metrics.cardH;
  const reach = { top: at - LEAVE_REACH_PX, bottom: at + viewH + LEAVE_REACH_PX };
  const meets = (top: number, h: number) => top + h > reach.top && top < reach.bottom;
  const inHidden = new Set(hidden.cards.map((c) => c.film.id));

  it('moves the scroll by the searched card’s shift, so the card stays where it is', () => {
    expect(shown.rows.length - hidden.rows.length).toBe(18);
    const want = hidden.anchor!.top - shown.anchor!.top;
    expect(want).toBe(-2200);
    expect(plan.to).toBe(at + want);
  });

  it('draws copies only of what leaves within 240px of the screen', () => {
    expect(plan.cards.length).toBeGreaterThan(0);
    for (const c of plan.cards) {
      expect(inHidden.has(c.film.id)).toBe(false);
      expect(meets(c.top, cardH)).toBe(true);
    }
    // Anything further out simply goes.
    const gone = shown.cards.filter((c) => !inHidden.has(c.film.id));
    expect(plan.cards.length).toBeLessThan(gone.length);
    expect(plan.cards.length).toBe(gone.filter((c) => meets(c.top, cardH)).length);
    const years = new Set(hidden.rows.map((r) => r.year));
    for (const r of plan.rows) {
      expect(years.has(r.year)).toBe(false);
      expect(meets(r.top, r.height)).toBe(true);
    }
  });

  it('keeps drawn the cards that stay from within that reach, wherever the motion takes them', () => {
    const near = shown.cards.filter((c) => inHidden.has(c.film.id) && meets(c.top, cardH));
    expect([...plan.keep].sort()).toEqual(near.map((c) => c.film.id).sort());
    expect(plan.keep.has(shown.anchor!.film.id)).toBe(true);
  });

  it('stops the scroll at the top of the map, and at the end of the new plot', () => {
    // Just below the top, the rows above the searched film close up by
    // more than the scroll has to give.
    expect(reflowPlan(shown, hidden, 40, viewH, 0).to).toBe(0);
    // At the very bottom of the whole map, the shorter plot ends first.
    const bottom = shown.plotH - viewH;
    expect(reflowPlan(shown, hidden, bottom, viewH, 0).to).toBe(hidden.plotH - viewH);
  });

  it('measures the screen from under a header lying over the map', () => {
    // The plot starts 116px down the scroller, so the same scroll shows
    // 116px less of it, and the scroll can go 116px further.
    const over = reflowPlan(shown, hidden, at + 116, viewH, 116);
    expect(over.to).toBe(plan.to + 116);
    expect(over.cards).toEqual(plan.cards);
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

/** Time the test turns by hand. */
function handClock(): PreviewClock & { advance: (ms: number) => void } {
  let now = 0;
  let next = 1;
  const due = new Map<number, { at: number; run: () => void }>();
  return {
    now: () => now,
    after: (ms, run) => {
      due.set(next, { at: now + ms, run });
      return next++;
    },
    cancel: (timer) => {
      due.delete(timer);
    },
    advance: (ms) => {
      const end = now + ms;
      for (;;) {
        let first: [number, { at: number; run: () => void }] | null = null;
        for (const entry of due) if (entry[1].at <= end && (!first || entry[1].at < first[1].at)) first = entry;
        if (!first) break;
        due.delete(first[0]);
        now = first[1].at;
        first[1].run();
      }
      now = end;
    },
  };
}

/** A scheduler over a map that records what it was asked to do. The
 *  map opens nothing for a card in `empty`, whose words have not come. */
function scheduled(empty: Set<string> = new Set()) {
  const clock = handClock();
  const state = { showing: null as string | null, playing: false, did: [] as string[] };
  const sched = previewScheduler(
    {
      showing: () => state.showing,
      playing: () => state.playing,
      open: (id) => {
        if (empty.has(id)) return;
        state.showing = id;
        state.did.push(`open ${id}`);
      },
      close: () => {
        state.showing = null;
        state.did.push('close');
      },
    },
    clock,
  );
  return { clock, state, sched };
}

describe('when the hover preview opens', () => {
  it('opens once the pointer has rested 480ms on a card', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(479);
    expect(state.did).toEqual([]);
    clock.advance(1);
    expect(state.did).toEqual(['open a']);
  });

  it('opens nothing for a pointer that crosses a card on the way somewhere', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(300);
    sched.leave();
    sched.rest('b');
    clock.advance(200);
    sched.leave();
    clock.advance(1000);
    expect(state.did).toEqual([]);
  });

  it('swaps to the next card after 90ms while one is showing', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(480);
    sched.leave();
    sched.rest('b');
    clock.advance(89);
    expect(state.showing).toBe('a');
    clock.advance(1);
    expect(state.did).toEqual(['open a', 'open b']);
  });

  it('swaps after 90ms from one closed less than 350ms ago, and waits 480ms after that', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(480);
    sched.leave();
    clock.advance(160);
    expect(state.did).toEqual(['open a', 'close']);
    clock.advance(200);
    sched.rest('b');
    clock.advance(90);
    expect(state.did).toEqual(['open a', 'close', 'open b']);
    sched.shut();
    clock.advance(350);
    sched.rest('c');
    clock.advance(90);
    expect(state.showing).toBeNull();
    clock.advance(390);
    expect(state.showing).toBe('c');
  });

  it('counts a card with nothing to show as no preview showing, so the next still waits 480ms', () => {
    const { clock, state, sched } = scheduled(new Set(['a']));
    sched.rest('a');
    clock.advance(480);
    expect(state.showing).toBeNull();
    sched.leave();
    sched.rest('b');
    clock.advance(479);
    expect(state.showing).toBeNull();
    clock.advance(1);
    expect(state.did).toEqual(['open b']);
  });

  it('waits the full 480ms while the preview’s trailer plays, so drifting over cards does not swap it', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(480);
    state.playing = true;
    sched.leave();
    sched.rest('b');
    clock.advance(90);
    expect(state.showing).toBe('a');
    clock.advance(389);
    expect(state.showing).toBe('a');
    clock.advance(1);
    expect(state.showing).toBe('b');
  });
});

describe('when the hover preview closes', () => {
  it('closes 160ms after the pointer has left the card and the preview', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(480);
    sched.leave();
    clock.advance(159);
    expect(state.showing).toBe('a');
    clock.advance(1);
    expect(state.showing).toBeNull();
  });

  it('waits 450ms while its trailer plays', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(480);
    state.playing = true;
    sched.leave();
    clock.advance(449);
    expect(state.showing).toBe('a');
    clock.advance(1);
    expect(state.showing).toBeNull();
  });

  it('stays open while the pointer is on the preview, and closes once it leaves that too', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(480);
    // Off the card, across the gap, onto the preview.
    sched.leave();
    clock.advance(60);
    sched.hold();
    clock.advance(5000);
    expect(state.showing).toBe('a');
    sched.leave();
    clock.advance(160);
    expect(state.did).toEqual(['open a', 'close']);
  });

  it('stays open for a pointer that comes back to its own card', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(480);
    sched.leave();
    clock.advance(100);
    sched.rest('a');
    clock.advance(5000);
    expect(state.did).toEqual(['open a']);
  });

  it('closes at once when told to, and forgets a preview on its way', () => {
    const { clock, state, sched } = scheduled();
    sched.rest('a');
    clock.advance(200);
    sched.shut();
    clock.advance(1000);
    expect(state.did).toEqual([]);
    sched.rest('a');
    clock.advance(480);
    sched.shut();
    expect(state.did).toEqual(['open a', 'close']);
    sched.dispose();
  });
});
