import { createElement, type ComponentProps, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import matrix from './fixtures/matrix-grid.json';
import {
  Card,
  RING_MS,
  centredScroll,
  filteredOut,
  isFlown,
  opacityOf,
  openingBox,
  previewHost,
  previewsDrawn,
  reflowPlan,
  ringDelay,
  spreadDelays,
  wordsFor,
  type Peek,
  type PreviewMap,
} from './GridMap';
import {
  DEFAULT_SETTINGS,
  isLit,
  layoutGrid,
  type GridFilm,
  type GridPayload,
  type Placed,
} from './grid';
import { MapPreview } from './MapPreview';
import { LEAVE_REACH_PX } from './motion';
import {
  PREVIEW_BACK_MS,
  PREVIEW_DRIFT_EXTRA_MS,
  PREVIEW_LEAVE_MS,
  PREVIEW_LEAVE_PLAYING_MS,
  PREVIEW_OUT_MS,
  PREVIEW_OUT_PLAYING_MS,
  PREVIEW_REST_MS,
  PREVIEW_SWAP_MS,
  PREVIEW_SWAP_OUT_MS,
  previewScheduler,
  type PreviewClock,
} from './preview';
import type { Player } from './TrailerRow';
import { startPlay, stopPlay, type Play } from './trailer';

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
      // Its closes are at once, so there is never a leave to take back
      // (see previewHost for the map's own).
      keep: () => {},
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

/** The Matrix's map at 1440, as a desktop lays it out. */
const MAP = matrix as unknown as GridPayload;
const LAID = layoutGrid(MAP, 1440, DEFAULT_SETTINGS);
/** Three of its cards, none of them the searched film's. */
const [A, B, C] = LAID.cards
  .filter((c) => !c.film.isAnchor)
  .slice(0, 3)
  .map((c) => c.film.id);
/** Room enough for any preview. */
const ROOM = { x0: 0, x1: LAID.plotW, vt: 0, vb: LAID.plotH };

/** What a card says once its words have come. */
function words(id: string): GridFilm {
  return { id, title: `Movie ${id}`, year: 2000, rating: 7, md: 0, people: [`nm-${id}`], isAnchor: false };
}

/** The hover preview as usePreview runs it: its host, and the scheduler
 *  over it on the page's (faked) timers. What it draws is kept in
 *  `slots`, the chips it lights in `lit`, and what it asks of the player
 *  in `did`. The pointer moves with `onto` and `off`, as the cards'
 *  events move it, and `set` sets the player in a card's preview. */
function previewing() {
  const slots = { peek: null as Peek | null, gone: null as Peek | null, held: null as string | null };
  const lit: string[][] = [];
  const did: string[] = [];
  let play: Play | null = null;
  const player: Player = {
    get play() {
      return play;
    },
    now: () => play,
    start: () => {},
    stop: () => {},
    drop: (where, id) => {
      did.push(`drop ${where} ${id}`);
      if (play?.where === where && (id == null || play.id === id)) play = null;
    },
    fade: (to, ms) => did.push(`fade to ${to} over ${ms}`),
    sound: () => {},
    resize: () => {},
    frame: { current: null },
  };
  const map: PreviewMap = {
    layout: LAID,
    blocked: false,
    selected: new Set(),
    minRating: null,
    want: 0,
    said: words,
    light: (people) => lit.push(people),
    player,
  };
  const showing = { current: null as string | null };
  const under = { current: null as string | null };
  const host = previewHost({
    map: () => map,
    bounds: () => ROOM,
    now: () => slots.peek,
    put: (p) => {
      slots.peek = p;
    },
    gone: (p) => {
      slots.gone = p;
    },
    held: (id) => {
      slots.held = id;
    },
    showing,
    under,
  });
  const sched = previewScheduler(host, {
    now: () => Date.now(),
    after: (ms, run) => window.setTimeout(run, ms),
    cancel: (timer) => window.clearTimeout(timer),
  });
  return {
    slots,
    lit,
    did,
    showing,
    sched,
    /** The player set in this card's preview and open, with its sound
     *  on unless `muted`. */
    set: (id: string, muted = false) => {
      play = { ...startPlay(null, 'preview', id, muted, 360, false)!, open: true };
    },
    stopPlaying: () => {
      play = stopPlay(play);
    },
    onto: (id: string) => {
      under.current = id;
      sched.rest(id);
    },
    off: () => {
      const id = under.current;
      under.current = null;
      if (id != null) host.leftCard(id);
      sched.leave();
    },
    /** Each preview GridMap draws, in its order and with its key, as the
     *  opening tag its render draws. */
    drawn: () =>
      previewsDrawn(MAP.anchor.id, slots.gone, slots.peek).map(({ key, p, leaving }) => {
        const html = renderToStaticMarkup(
          createElement(MapPreview, {
            film: words(p.id),
            leaving,
            back: p.back,
            people: MAP.people,
            anchorTitle: MAP.anchor.title,
            theme: 'dark',
            codes: new Map(),
            photoOf: () => undefined,
            place: p.place,
            player,
            bounds: () => ROOM,
            plotH: LAID.plotH,
            onEnter: () => {},
            onLeave: () => {},
            onFace: () => {},
            offFace: () => {},
          }),
        );
        return { key, tag: html.match(/^<div[^>]*>/)?.[0] ?? '' };
      }),
  };
}

/** The classes on an opening tag. */
const classes = (tag: string) => tag.match(/class="([^"]*)"/)?.[1].split(' ') ?? [];

describe('the hover preview’s lifetime (usePreview)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // The host's timers are the window's.
    vi.stubGlobal('window', globalThis);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('are the handoff’s timings: gone 340ms after it starts to leave, or 420ms with its trailer set', () => {
    expect(PREVIEW_OUT_MS + PREVIEW_DRIFT_EXTRA_MS + 20).toBe(340);
    expect(PREVIEW_OUT_PLAYING_MS + PREVIEW_DRIFT_EXTRA_MS + 20).toBe(420);
    expect(PREVIEW_SWAP_OUT_MS + 40).toBe(240);
  });

  it('keeps a preview that leaves drawn, fading with cd-preview-out, and unmounts it 340ms later', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    expect(t.slots.peek?.id).toBe(A);
    const [open] = t.drawn();
    expect(classes(open.tag)).not.toContain('cd-preview-out');
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS);
    expect(t.slots.peek).toMatchObject({ id: A, out: 'plain' });
    const [leaving] = t.drawn();
    // The same preview, with the same key, fading where it stands.
    expect(leaving.key).toBe(open.key);
    expect(classes(leaving.tag)).toContain('cd-preview-out');
    expect(classes(leaving.tag)).not.toContain('cd-preview-out-playing');
    // Still interactive, so the pointer can take it back.
    expect(leaving.tag).not.toContain('inert');
    // Still showing, so its own card takes it back rather than opening
    // it afresh; but its card's hover state and chips go at once.
    expect(t.showing.current).toBe(A);
    expect(t.slots.held).toBeNull();
    expect(t.lit.at(-1)).toEqual([]);
    vi.advanceTimersByTime(339);
    expect(t.slots.peek?.id).toBe(A);
    vi.advanceTimersByTime(1);
    expect(t.slots.peek).toBeNull();
    expect(t.showing.current).toBeNull();
    expect(t.drawn()).toEqual([]);
    // A preview with no player has no sound to fade.
    expect(t.did).toEqual([`drop preview ${A}`]);
  });

  it('leaves with its trailer set over 420ms, the video playing on and its sound fading out', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.set(A);
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_PLAYING_MS);
    expect(t.slots.peek).toMatchObject({ id: A, out: 'playing' });
    const [leaving] = t.drawn();
    // Nothing inside moves: the player stays open, and the box keeps its
    // raised stacking over the lifting blur.
    expect(classes(leaving.tag)).toEqual(
      expect.arrayContaining(['cd-preview-trailer', 'cd-preview-trailer-open', 'cd-preview-out', 'cd-preview-out-playing']),
    );
    expect(t.did).toEqual([`fade to 0 over ${PREVIEW_OUT_PLAYING_MS}`]);
    vi.advanceTimersByTime(419);
    expect(t.slots.peek?.id).toBe(A);
    vi.advanceTimersByTime(1);
    expect(t.slots.peek).toBeNull();
    // Its player goes with it, and nothing else's.
    expect(t.did).toEqual([`fade to 0 over ${PREVIEW_OUT_PLAYING_MS}`, `drop preview ${A}`]);
  });

  it('fades only a trailer with its sound on: a muted one, or one closing, has nothing to fade', () => {
    for (const quiet of ['muted', 'closing'] as const) {
      const t = previewing();
      t.onto(A);
      vi.advanceTimersByTime(PREVIEW_REST_MS);
      t.set(A, quiet === 'muted');
      if (quiet === 'closing') t.stopPlaying();
      t.off();
      vi.advanceTimersByTime(PREVIEW_LEAVE_PLAYING_MS);
      expect(t.slots.peek?.out, quiet).toBe('playing');
      t.sched.hold();
      t.off();
      vi.advanceTimersByTime(PREVIEW_LEAVE_PLAYING_MS + 420);
      expect(t.slots.peek, quiet).toBeNull();
      expect(t.did, quiet).toEqual([`drop preview ${A}`]);
    }
  });

  it('comes back, from wherever it had got to, for a pointer back on it or its card before it has gone', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS + 200);
    expect(t.slots.peek?.out).toBe('plain');
    // Back onto the preview itself.
    t.sched.hold();
    expect(t.slots.peek).toMatchObject({ id: A, out: undefined, back: true });
    expect(classes(t.drawn()[0].tag)).not.toContain('cd-preview-out');
    expect(t.slots.held).toBe(A);
    expect(t.lit.at(-1)).toEqual([`nm-${A}`]);
    vi.advanceTimersByTime(5000);
    expect(t.slots.peek?.id).toBe(A);
    // And again, back onto its card this time.
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS + 300);
    expect(t.slots.peek?.out).toBe('plain');
    t.onto(A);
    expect(t.slots.peek).toMatchObject({ id: A, out: undefined, back: true });
    vi.advanceTimersByTime(5000);
    expect(t.slots.peek?.id).toBe(A);
    expect(t.did).toEqual([]);
  });

  it('brings its trailer’s sound back up over 240ms as it comes back', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.set(A);
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_PLAYING_MS + 150);
    t.sched.hold();
    expect(t.did).toEqual([`fade to 0 over ${PREVIEW_OUT_PLAYING_MS}`, `fade to 100 over ${PREVIEW_BACK_MS}`]);
    expect(t.slots.peek).toMatchObject({ id: A, out: undefined, back: true });
  });

  it('carries on leaving through a scroll or Escape, which cannot hurry it', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS + 200);
    t.sched.shut();
    expect(t.slots.peek?.out).toBe('plain');
    vi.advanceTimersByTime(139);
    expect(t.slots.peek?.id).toBe(A);
    vi.advanceTimersByTime(1);
    expect(t.slots.peek).toBeNull();
  });

  it('lets its card’s hover go for good once it is leaving, though the pointer leaves the card after a scroll or Escape', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    // Escape, with the pointer still resting on the card.
    t.sched.shut();
    expect(t.slots.peek?.out).toBe('plain');
    // Then off the card, onto empty map, before the preview has gone.
    vi.advanceTimersByTime(60);
    t.off();
    expect(t.slots.held).toBeNull();
    expect(t.lit).not.toContainEqual([`nm-${A}`]);
    vi.advanceTimersByTime(340);
    expect(t.slots.peek).toBeNull();
    expect(t.slots.held).toBeNull();
    expect(t.lit).not.toContainEqual([`nm-${A}`]);
  });

  it('fades the old preview out under the next card’s: the same one, keyed as before, inert and drawn first, for 240ms', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    const [was] = t.drawn();
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    expect(t.slots.peek?.id).toBe(B);
    const [gone, now] = t.drawn();
    expect(gone.key).toBe(was.key);
    expect(classes(gone.tag)).toEqual(expect.arrayContaining(['cd-preview-out', 'cd-preview-gone']));
    expect(gone.tag).toContain(' inert=""');
    expect(gone.tag).toContain(' aria-hidden="true"');
    // The new one comes in on its usual entrance.
    expect(now.key).toBe(`${MAP.anchor.id}:${B}:${t.slots.peek?.n}`);
    expect(now.key).not.toBe(was.key);
    expect(classes(now.tag)).not.toContain('cd-preview-out');
    expect(now.tag).not.toContain('inert');
    vi.advanceTimersByTime(239);
    expect(t.drawn().map((d) => d.key)).toEqual([was.key, now.key]);
    vi.advanceTimersByTime(1);
    expect(t.drawn().map((d) => d.key)).toEqual([now.key]);
    expect(t.did).toEqual([]);
  });

  it('replaces a preview still fading out at once, and fades out one that was already leaving', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    const [, b] = t.drawn();
    t.onto(C);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    const [, c] = t.drawn();
    expect(t.drawn().map((d) => d.key)).toEqual([b.key, c.key]);
    // C leaves, and B's pointer comes to rest before it has gone.
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS + 100);
    expect(t.slots.peek?.out).toBe('plain');
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    expect(t.slots.gone).toMatchObject({ id: C, out: 'plain' });
    expect(t.slots.peek?.id).toBe(B);
    // Its own unmount no longer comes: it goes as the gone one does.
    vi.advanceTimersByTime(240);
    expect(t.slots.gone).toBeNull();
    expect(t.slots.peek?.id).toBe(B);
  });

  it('mounts a card opened again while its last preview is still fading out afresh, under a key of its own, so its entrance runs', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    const [a] = t.drawn();
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    const [, b] = t.drawn();
    // Back to A inside the 240ms A takes to fade out under B.
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    expect(PREVIEW_SWAP_MS * 2).toBeLessThan(PREVIEW_SWAP_OUT_MS + 40);
    const drawn = t.drawn();
    expect(drawn).toHaveLength(2);
    // B gives way as A did, keeping its key, so it fades where it is; the
    // A still fading is replaced at once by a new A, keyed afresh, which
    // React mounts rather than moving the fading one after B.
    const [gone, again] = drawn;
    expect(gone.key).toBe(b.key);
    expect(classes(gone.tag)).toContain('cd-preview-gone');
    expect(t.slots.peek?.id).toBe(A);
    expect(again.key).not.toBe(a.key);
    expect(classes(again.tag)).not.toContain('cd-preview-out');
  });

  it('lets a preview with its trailer set leave first, and opens the next card’s as it goes, the pointer still on that card', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.set(A);
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    expect(t.slots.peek).toMatchObject({ id: A, out: 'playing' });
    expect(t.slots.gone).toBeNull();
    expect(t.drawn()).toHaveLength(1);
    vi.advanceTimersByTime(419);
    expect(t.slots.peek?.id).toBe(A);
    vi.advanceTimersByTime(1);
    expect(t.slots.peek?.id).toBe(B);
    expect(t.slots.peek?.out).toBeUndefined();
    expect(t.slots.gone).toBeNull();
    expect(t.did).toEqual([`fade to 0 over ${PREVIEW_OUT_PLAYING_MS}`, `drop preview ${A}`]);
  });

  it('opens nothing as it goes once the pointer has left that card', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.set(A);
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    expect(t.slots.peek?.out).toBe('playing');
    t.off();
    vi.advanceTimersByTime(420);
    expect(t.slots.peek).toBeNull();
    vi.advanceTimersByTime(5000);
    expect(t.slots.peek).toBeNull();
  });

  it('forgets the card waiting on a trailer’s leave for a scroll or Escape, as it forgets any open on its way', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.set(A);
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    expect(t.slots.peek).toMatchObject({ id: A, out: 'playing' });
    // Escape, the pointer still on B.
    t.sched.shut();
    vi.advanceTimersByTime(420);
    expect(t.slots.peek).toBeNull();
    vi.advanceTimersByTime(5000);
    expect(t.slots.peek).toBeNull();
    expect(t.showing.current).toBeNull();
  });

  it('takes it away at once for another map, a panel or View, or a new layout, mid-leave or mid-swap', () => {
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.sched.shut(true);
    expect(t.slots.peek).toBeNull();
    expect(t.showing.current).toBeNull();

    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS);
    expect(t.slots.peek?.out).toBe('plain');
    t.sched.shut(true);
    expect(t.slots.peek).toBeNull();

    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    expect(t.drawn()).toHaveLength(2);
    t.sched.shut(true);
    expect(t.drawn()).toEqual([]);
    expect(t.showing.current).toBeNull();
    // Nothing left waiting to come back.
    vi.advanceTimersByTime(5000);
    expect(t.drawn()).toEqual([]);
    // MapPreview's unmount drops a player; the host has nothing to drop.
    expect(t.did).toEqual([]);
  });

  it('takes it away at once on every close for a reader who has asked for nothing to move', () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }));
    const t = previewing();
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    t.off();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS);
    expect(t.slots.peek).toBeNull();

    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    t.sched.shut();
    expect(t.slots.peek).toBeNull();

    // A swap simply replaces it.
    t.onto(A);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    t.onto(B);
    vi.advanceTimersByTime(PREVIEW_SWAP_MS);
    expect(t.slots.gone).toBeNull();
    expect(t.drawn().map((d) => d.key)).toEqual([`${MAP.anchor.id}:${B}:${t.slots.peek?.n}`]);

    // Even from a trailer, which goes with it, and the next opens at once.
    t.set(B);
    t.onto(C);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    expect(t.slots.gone).toBeNull();
    expect(t.drawn().map((d) => d.key)).toEqual([`${MAP.anchor.id}:${C}:${t.slots.peek?.n}`]);
    expect(t.did).toEqual([]);
  });
});
