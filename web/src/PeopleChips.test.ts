import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import css from './grid.css?raw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { faceCardController, type FaceCard, type FaceHold, type ImageLoad } from './faceCard';
import { PeopleChips, chipEvents, chipName, filmCounts, listenWheelSideways, wheelSideways } from './PeopleChips';
import matrix from './fixtures/matrix-grid.json';
import type { GridPayload, GridPerson, SpineTuple } from './grid';
import type { PreviewClock } from './preview';

const real = matrix as unknown as GridPayload;

const person = (id: string): GridPerson => ({ id, name: id, role: 'cast' });

describe('filmCounts', () => {
  // The refresh puts a count back on every chip: how many of that
  // person's films this map holds. It came off in an earlier design pass
  // ("the map is open-ended"); the handoff brings it back on purpose, as
  // a way to see whose work the map is mostly made of. It is read off
  // the spine the map already has, so it needs nothing new from the API.
  const people = [person('nm0000001'), person('nm0000002'), person('nm0000003')];

  it('counts the films each person is on, the searched film included', () => {
    const films: SpineTuple[] = [
      ['tt0000001', 1999, 8.7, 331, [0, 1, 2]],
      ['tt0000002', 2003, 7.2, 515, [1]],
      ['tt0000003', 2005, null, 0, [1, 2]],
      ['tt0000004', 2010, 6.1, 0, []],
    ];
    const counts = filmCounts({ people, films });
    expect(counts.get('nm0000001')).toBe(1);
    expect(counts.get('nm0000002')).toBe(3);
    // Unrated films are theirs too: the count is the map's, not what
    // the filters have left lit.
    expect(counts.get('nm0000003')).toBe(2);
    expect(counts.get('nm0000009')).toBeUndefined();
  });

  it('counts a person once per film, however many credits they have', () => {
    const films: SpineTuple[] = [
      ['tt0000001', 1999, 8.7, 331, [0, 0, 1]],
      ['tt0000002', 2003, 7.2, 515, [0]],
    ];
    const counts = filmCounts({ people, films });
    expect(counts.get('nm0000001')).toBe(2);
    expect(counts.get('nm0000002')).toBe(1);
  });

  it('ignores a place in the row that names nobody', () => {
    const counts = filmCounts({ people, films: [['tt0000001', 1999, 8.7, 331, [0, 7]]] });
    expect([...counts]).toEqual([['nm0000001', 1]]);
  });

  it('gives no counts at all for a spine that does not say who is on each film', () => {
    // The fixture is such a spine. A chip then shows its name alone,
    // rather than a zero that would be a claim the map cannot make.
    expect(real.films.every((f) => f[4] === undefined)).toBe(true);
    expect(filmCounts(real).size).toBe(0);
  });
});

describe('chipName', () => {
  it('says the count as words after the name', () => {
    expect(chipName('Keanu Reeves', 41)).toBe('Keanu Reeves, 41 movies');
    expect(chipName('Gloria Foster', 1)).toBe('Gloria Foster, 1 movie');
  });

  it('leaves a chip with no count to its own text', () => {
    expect(chipName('Keanu Reeves', undefined)).toBeUndefined();
  });
});

/** A chip row that nothing holds. */
const NO_HOLD: FaceHold = { down: () => {}, up: () => {}, cancel: () => {}, click: () => false, menu: () => false };

describe('a chip', () => {
  const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
  const LANA = 'https://image.tmdb.org/t/p/w185/lana.jpg';

  /** The row as its first render draws it. Keanu's photo came with the
   *  payload; Lana's was asked for since; nobody else has one. */
  function row(): string {
    const people = real.people.map((p) => (p.id === 'nm0000206' ? { ...p, photo: KEANU } : p));
    return renderToStaticMarkup(
      createElement(PeopleChips, {
        people,
        carried: new Set<string>(),
        selected: new Set<string>(),
        lit: new Set<string>(),
        hovered: null,
        counts: new Map([
          ['nm0000206', 66],
          ['nm0905154', 8],
          ['nm0915989', 43],
        ]),
        photoOf: (p) => p.photo ?? (p.id === 'nm0905154' ? LANA : undefined),
        onToggle: () => {},
        onHover: () => {},
        onClear: () => {},
        onFace: () => {},
        offFace: () => {},
        hold: NO_HOLD,
      }),
    );
  }

  /** One person's chip, from its tag to its end. */
  function chip(html: string, id: string): string {
    const at = html.indexOf(`data-chip="${id}"`);
    expect(at).toBeGreaterThan(-1);
    return html.slice(html.lastIndexOf('<button', at), html.indexOf('</button>', at));
  }

  it('shows the person’s face where the dot was, and says the same as before', () => {
    const html = row();
    expect(html).not.toContain('cd-chip-dot');
    const keanu = chip(html, 'nm0000206');
    expect(keanu).toContain('aria-label="Keanu Reeves, 66 movies"');
    expect(keanu).toContain(
      '<span class="cd-face cd-face-chip" aria-hidden="true"><span class="cd-face-initials">KR</span>' +
        `<span class="cd-face-ring"></span><img class="cd-face-photo" src="${KEANU}" alt="" decoding="async"/></span>` +
        '<span class="cd-chip-name">Keanu Reeves</span><span class="cd-chip-count">66</span>',
    );
  });

  it('takes a photo asked for since the payload, and the initials without one', () => {
    const html = row();
    expect(chip(html, 'nm0905154')).toContain(`src="${LANA}"`);
    const hugo = chip(html, 'nm0915989');
    expect(hugo).toContain('aria-label="Hugo Weaving, 43 movies"');
    expect(hugo).toContain('<span class="cd-face cd-face-chip" aria-hidden="true"><span class="cd-face-initials">HW</span></span>');
    expect(hugo).not.toContain('<img');
  });

  it('gives a director a rounded square and the cast a circle, with the codes the cards use', () => {
    const html = row();
    // The Wachowskis share initials, so the codes lengthen their first names.
    expect(chip(html, 'nm0905154')).toContain('class="cd-face cd-face-chip cd-face-square"');
    expect(chip(html, 'nm0905154')).toContain('<span class="cd-face-initials">LaW</span>');
    expect(chip(html, 'nm0905152')).toContain('<span class="cd-face-initials">LiW</span>');
    expect(chip(html, 'nm0000206')).not.toContain('cd-face-square');
  });
});

describe('a chip’s bigger photo', () => {
  const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
  const chipEl = {} as HTMLElement;

  afterEach(() => {
    vi.useRealTimers();
  });

  /** Keanu's chip, wired as the row wires it, to a card controller on
   *  the test's timers whose photos are in hand at once. */
  function keanu(hovers = true) {
    vi.useFakeTimers();
    const clock: PreviewClock = {
      now: () => Date.now(),
      after: (ms, run) => setTimeout(run, ms),
      cancel: (t) => clearTimeout(t),
    };
    const loaded: ImageLoad = (_url, done) => {
      done(true);
      return () => {};
    };
    const log: string[] = [];
    const opened: FaceCard[] = [];
    const cards = faceCardController(
      {
        hovers: () => hovers,
        photo: (id) => (id === 'nm0000206' ? KEANU : undefined),
        place: () => ({ x: 120, y: 142, down: true }),
        open: (card) => {
          opened.push(card);
          log.push('open');
        },
        close: () => log.push('close'),
      },
      clock,
      loaded,
    );
    const toggled: string[] = [];
    const lit: (string | null)[] = [];
    const on = chipEvents('nm0000206', {
      tap: { allows: () => true },
      rest: { enter: (id) => lit.push(id), leave: () => lit.push(null) },
      onToggle: (id) => toggled.push(id),
      onFace: cards.onFace,
      offFace: cards.offFace,
      hold: { down: cards.down, up: cards.up, cancel: cards.cancel, click: cards.click, menu: cards.menu },
    });
    return { on, log, opened, toggled, lit };
  }

  it('asks to open 400ms after the mouse comes onto the chip, and closes as it leaves', () => {
    const { on, log, opened, lit } = keanu();
    on.onMouseEnter({ currentTarget: chipEl });
    // The chip lights its films on its own, shorter delay, as before.
    expect(lit).toEqual(['nm0000206']);
    vi.advanceTimersByTime(399);
    expect(log).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(opened).toEqual([{ id: 'nm0000206', from: 'chip', x: 120, y: 142, down: true }]);
    on.onMouseLeave();
    expect(log).toEqual(['open', 'close']);
    expect(lit).toEqual(['nm0000206', null]);
  });

  it('opens after a finger holds the chip for 450ms, closes as it lifts, and the click after does not toggle the chip', () => {
    const { on, log, opened, toggled } = keanu(false);
    on.onPointerDown({ currentTarget: chipEl, pointerType: 'touch' });
    vi.advanceTimersByTime(449);
    expect(log).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(opened.map((c) => c.from)).toEqual(['chip']);
    // Holding, the phone's own menu is kept away.
    const menu = { preventDefault: vi.fn() };
    on.onContextMenu(menu);
    expect(menu.preventDefault).toHaveBeenCalled();
    on.onPointerUp();
    expect(log).toEqual(['open', 'close']);
    on.onClick();
    expect(toggled).toEqual([]);
    // The next tap is the chip's again.
    on.onPointerDown({ currentTarget: chipEl, pointerType: 'touch' });
    on.onPointerUp();
    on.onClick();
    expect(toggled).toEqual(['nm0000206']);
  });

  it('opens nothing for a press called off before 450ms', () => {
    const { on, log, toggled } = keanu(false);
    on.onPointerDown({ currentTarget: chipEl, pointerType: 'touch' });
    vi.advanceTimersByTime(300);
    on.onPointerCancel();
    vi.advanceTimersByTime(1000);
    expect(log).toEqual([]);
    // A tap let go of in time is a tap: the chip toggles.
    on.onPointerDown({ currentTarget: chipEl, pointerType: 'touch' });
    vi.advanceTimersByTime(200);
    on.onPointerUp();
    on.onClick();
    vi.advanceTimersByTime(1000);
    expect(log).toEqual([]);
    expect(toggled).toEqual(['nm0000206']);
  });
});

describe('a chip’s touch rules', () => {
  it('keep iOS’s callout and a text selection from coming up under a held finger', () => {
    const rule = css.replace(/\/\*[\s\S]*?\*\//g, '').match(/(?:^|\})\s*\.cd-chip\s*\{([^}]*)\}/)?.[1] ?? '';
    expect(rule).toMatch(/-webkit-touch-callout:\s*none;/);
    expect(rule).toMatch(/(?:^|[;\s])user-select:\s*none;/);
  });
});

describe('a wheel over a row of chips', () => {
  /** A row 300px wide holding 700px of chips, scrolled to `left`. */
  const rowAt = (left: number) => ({ scrollWidth: 700, clientWidth: 300, scrollLeft: left }) as HTMLDivElement;
  const wheel = (deltaX: number, deltaY: number) => {
    const preventDefault = vi.fn();
    return { e: { deltaX, deltaY, preventDefault } as unknown as Parameters<typeof wheelSideways>[0], preventDefault };
  };

  it('moves the chips sideways for a mouse wheel’s up and down', () => {
    const row = rowAt(0);
    const { e, preventDefault } = wheel(0, 120);
    wheelSideways(e, row);
    expect(row.scrollLeft).toBe(120);
    expect(preventDefault).toHaveBeenCalled();
    wheelSideways(wheel(0, -50).e, row);
    expect(row.scrollLeft).toBe(70);
  });

  it('stops at either end, and leaves the page to scroll once the row has nowhere to go', () => {
    const end = rowAt(400);
    const past = wheel(0, 120);
    wheelSideways(past.e, end);
    expect(end.scrollLeft).toBe(400);
    expect(past.preventDefault).not.toHaveBeenCalled();
    const near = rowAt(350);
    wheelSideways(wheel(0, 120).e, near);
    expect(near.scrollLeft).toBe(400);
    const start = rowAt(0);
    wheelSideways(wheel(0, -120).e, start);
    expect(start.scrollLeft).toBe(0);
  });

  it('leaves a sideways gesture, and a row that fits, as they are', () => {
    const row = rowAt(100);
    const side = wheel(40, 120);
    wheelSideways(side.e, row);
    expect(row.scrollLeft).toBe(100);
    expect(side.preventDefault).not.toHaveBeenCalled();
    const fits = { scrollWidth: 300, clientWidth: 300, scrollLeft: 0 } as HTMLDivElement;
    wheelSideways(wheel(0, 120).e, fits);
    expect(fits.scrollLeft).toBe(0);
    expect(() => wheelSideways(wheel(0, 120).e, null)).not.toThrow();
  });
});

describe('a wheel over a row of chips, heard by the row itself', () => {
  /** A row with 50px of chips past its edge, scrolled to `left`, that
   *  keeps what listens to it and how. */
  function listenedRow(left: number) {
    const added: { type: string; fn: (e: unknown) => void; options: unknown }[] = [];
    const removed: { type: string; fn: unknown }[] = [];
    const row = {
      scrollWidth: 350,
      clientWidth: 300,
      scrollLeft: left,
      addEventListener: (type: string, fn: (e: unknown) => void, options: unknown) => added.push({ type, fn, options }),
      removeEventListener: (type: string, fn: unknown) => removed.push({ type, fn }),
    };
    return { row, added, removed, stop: listenWheelSideways(row as unknown as HTMLDivElement) };
  }
  const wheel = (deltaY: number) => ({ deltaX: 0, deltaY, preventDefault: vi.fn() });

  it('listens for the wheel on the row, not passively, so the page holds still while the row moves', () => {
    const { row, added } = listenedRow(0);
    expect(added).toHaveLength(1);
    expect(added[0].type).toBe('wheel');
    expect(added[0].options).toEqual({ passive: false });
    const tick = wheel(100);
    added[0].fn(tick);
    expect(row.scrollLeft).toBe(50);
    expect(tick.preventDefault).toHaveBeenCalled();
  });

  it('lets the page scroll on once the row is at its end', () => {
    const { row, added } = listenedRow(50);
    const tick = wheel(100);
    added[0].fn(tick);
    expect(row.scrollLeft).toBe(50);
    expect(tick.preventDefault).not.toHaveBeenCalled();
  });

  it('stops listening with the very handler it listened with', () => {
    const { added, removed, stop } = listenedRow(0);
    expect(removed).toEqual([]);
    stop?.();
    expect(removed).toEqual([{ type: 'wheel', fn: added[0].fn }]);
  });

  it('listens to nothing without a row', () => {
    expect(listenWheelSideways(null)).toBeUndefined();
  });
});
