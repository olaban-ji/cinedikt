import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import css from './grid.css?raw';
import {
  GridSheet,
  SheetSynopsis,
  collapsedSyn,
  foldingSyn,
  holdsFold,
  measureSynopsis,
  measuredSyn,
  restingSyn,
  roleLine,
  roomInPanel,
  scalePct,
  sheetEscape,
  synFor,
  synopsisView,
  toggledSyn,
  versus,
  versusText,
  type SynState,
} from './GridSheet';
import type { GridFilm, GridPayload, GridPerson } from './grid';
import { sheetPosterPx } from './poster';
import { screenOf, type ScreenClass } from './screen';
import { ABOUT_GAP, MORE_ROW_H, SYN_LH, restingLines } from './synopsis';
import type { Player } from './TrailerRow';
import { PLAYER, startPlay, stopPlay, videoHeight, videoWidth, type Play } from './trailer';

// What the trailer lookup has already answered for the film on show.
// The panel's first render shows it straight away, which is what a
// render with no effects, like these, can see.
const answer = vi.hoisted(() => ({ key: undefined as string | null | undefined }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  trailerKnown: () => answer.key,
  fetchTrailer: () => new Promise<string | null>(() => {}),
}));

const anchor: GridFilm = { id: 'nm0000001', title: 'The Matrix', year: 1999, rating: 8.7, md: 0, people: [], isAnchor: true };
const film = (rating: number | null, over: Partial<GridFilm> = {}): GridFilm => ({
  id: 'nm0000002', title: 'Bound', year: 1996, rating, md: 0, people: [], isAnchor: false, ...over,
});

describe('versus', () => {
  it('says how far below the searched film a film sits', () => {
    expect(versus(film(7.3), anchor)).toEqual({ dir: 'down', delta: -1.4, title: 'The Matrix' });
  });

  it('says how far above', () => {
    expect(versus(film(9.1), anchor)).toEqual({ dir: 'up', delta: 0.4, title: 'The Matrix' });
  });

  it('says the same when they match to a tenth', () => {
    expect(versus(film(8.7), anchor)).toEqual({ dir: 'same', delta: 0, title: 'The Matrix' });
    expect(versus(film(8.72), anchor)).toEqual({ dir: 'same', delta: 0, title: 'The Matrix' });
  });

  it('says nothing for the searched film or an unrated one', () => {
    expect(versus(film(8, { isAnchor: true }), anchor)).toBeNull();
    expect(versus(film(null), anchor)).toBeNull();
    expect(versus(film(7), { ...anchor, rating: null })).toBeNull();
  });
});

describe('roleLine', () => {
  const cast: GridPerson = { id: 'nm0000001', name: 'Keanu Reeves', role: 'cast', character: 'Neo', order: 0 };
  const helm: GridPerson = { id: 'nm0000002', name: 'Lana Wachowski', role: 'director', order: -1 };

  it('names the character for cast', () => {
    expect(roleLine(cast, 'The Matrix')).toBe('Neo in The Matrix');
  });

  it('says directed for a director', () => {
    expect(roleLine(helm, 'The Matrix')).toBe('Directed The Matrix');
  });

  it('copes with a missing character', () => {
    expect(roleLine({ ...cast, character: undefined }, 'The Matrix')).toBe('In The Matrix');
  });
});

describe('versusText', () => {
  it('says how far above or below, to a tenth, without a sign', () => {
    expect(versusText({ dir: 'up', delta: 0.4, title: 'The Matrix' })).toBe('0.4 above The Matrix');
    expect(versusText({ dir: 'down', delta: -1.4, title: 'The Matrix' })).toBe('1.4 below The Matrix');
    expect(versusText({ dir: 'down', delta: -0.3, title: 'The Matrix' })).toBe('0.3 below The Matrix');
  });

  it('says the same when they match', () => {
    expect(versusText({ dir: 'same', delta: 0, title: 'The Matrix' })).toBe('Same as The Matrix');
  });

  it('reads the same as the comparison it is given', () => {
    expect(versusText(versus(film(8.4), anchor)!)).toBe('0.3 below The Matrix');
  });
});

describe('scalePct', () => {
  it('runs from 4 at the left to 9 at the right', () => {
    expect(scalePct(4)).toBe(0);
    expect(scalePct(9)).toBe(100);
    expect(scalePct(6.5)).toBe(50);
    expect(scalePct(8.7)).toBeCloseTo(94);
  });

  it('holds a rating off the scale at its end', () => {
    expect(scalePct(3)).toBe(0);
    expect(scalePct(9.5)).toBe(100);
  });
});

/** A player holding this, with every action recorded rather than done. */
function playerWith(play: Play | null, did: string[] = []): Player {
  return {
    play,
    now: () => play,
    start: (where, id, muted) => did.push(`start ${where} ${id} ${muted ? 'muted' : 'sound'}`),
    stop: (where) => did.push(`stop ${where ?? 'any'}`),
    drop: (where) => did.push(`drop ${where}`),
    sound: () => did.push('sound'),
    resize: () => {},
    frame: { current: null },
  };
}

/** The panel, drawn once as a first render draws it, with the trailer
 *  lookup's answer so far. */
function panel(key: string | null | undefined, over: Partial<GridFilm> = {}, play: Play | null = null): string {
  answer.key = key;
  const shown = film(7.2, { title: 'The Matrix Reloaded', year: 2003, ...over });
  const payload: GridPayload = { anchor, people: [], films: [] };
  return renderToStaticMarkup(
    createElement(GridSheet, {
      film: shown,
      payload,
      onOnly: () => {},
      onRemap: () => {},
      onClose: () => {},
      player: playerWith(play),
    }),
  );
}

/** The film the panel shows, and its trailer. */
const ID = film(null).id;
const KEY = 'vKQi3bBA1y8';

const SYNOPSIS = 'Neo and the rebel leaders estimate that they have 72 hours until Zion falls.';

describe('the panel’s synopsis', () => {
  it('rests clamped to its lines, between the title and the comparison', () => {
    const html = panel(KEY, { synopsis: SYNOPSIS });
    // Four lines until the panel has been measured, which happens before
    // it is first painted.
    expect(html).toContain(
      `<div class="cd-sheet-syn" style="--syn-tr:0s"><p class="cd-sheet-syn-text" style="max-height:93px;-webkit-line-clamp:4">${SYNOPSIS}</p></div>`,
    );
    const head = html.indexOf('cd-sheet-head');
    const about = html.indexOf('cd-sheet-about');
    const versusAt = html.indexOf('cd-sheet-versus');
    expect(head).toBeGreaterThan(-1);
    expect(about).toBeGreaterThan(head);
    expect(versusAt).toBeGreaterThan(about);
  });

  it('is left out when there is none, with the trailer row still there', () => {
    for (const synopsis of [undefined, '', '   ']) {
      const html = panel(KEY, { synopsis });
      expect(html).not.toContain('cd-sheet-syn');
      expect(html).toContain('cd-sheet-about');
      expect(html).toContain('Watch trailer');
    }
  });
});

describe('the panel’s year and rating line', () => {
  it('follows the rating with the genres, as plain text, when IMDb lists some', () => {
    expect(panel(KEY, { genres: ['Action', 'Sci-Fi'] })).toContain(
      '<div class="cd-sheet-meta"><span>2003</span><span class="cd-sheet-pill">7.2</span><span class="cd-sheet-genres">Action · Sci-Fi</span></div>',
    );
    expect(panel(KEY, { rating: null, genres: ['Crime', 'Drama', 'Film-Noir'] })).toContain(
      '<span class="cd-sheet-pill">No rating</span><span class="cd-sheet-genres">Crime · Drama · Film-Noir</span></div>',
    );
  });

  it('is the year and the rating alone when IMDb lists none', () => {
    for (const genres of [undefined, [], ['', ' ']]) {
      const html = panel(KEY, { genres });
      expect(html).toContain('<div class="cd-sheet-meta"><span>2003</span><span class="cd-sheet-pill">7.2</span></div>');
      expect(html).not.toContain('cd-sheet-genres');
    }
  });
});

describe('the panel’s trailer row', () => {
  it('holds a placeholder the size of the button while the answer is on its way', () => {
    const html = panel(undefined);
    expect(html).toContain('<span class="cd-trailer-skel cd-trailer-panel" aria-hidden="true"></span>');
    expect(html).not.toContain('Watch trailer');
    expect(html).not.toContain('No trailer available');
  });

  it('offers the trailer, named for the film, once there is one', () => {
    const html = panel(KEY);
    expect(html).toContain('aria-label="Play the trailer for The Matrix Reloaded"');
    expect(html).toMatch(/<span>Watch trailer<\/span><span class="cd-trailer-yt">YouTube<\/span>/);
    expect(html).not.toContain('cd-trailer-skel');
  });

  it('says there is none when there is none, or the lookup failed', () => {
    // A failure resolves to null, the same as none, and is not kept.
    const html = panel(null);
    expect(html).toContain('No trailer available');
    expect(html).not.toContain('Watch trailer');
    expect(html).not.toContain('cd-trailer-skel');
  });

  it('asks YouTube for nothing until the player opens', () => {
    expect(panel(KEY)).not.toContain('<iframe');
  });

  it('opens the player where the button was, with its controls and the frame', () => {
    const open: Play = { ...startPlay(null, 'panel', ID, false, 430, true)! };
    const html = panel(KEY, {}, open);
    expect(html).toContain('cd-trailer cd-trailer-panel cd-trailer-open');
    expect(html).toContain('--trailer-w:430px');
    expect(html).toContain('--trailer-vh:241.875px');
    // Watch trailer steps aside while its player is open.
    expect(html).toMatch(/<button[^>]*class="cd-trailer-btn"[^>]*disabled=""/);
    expect(html).toContain('aria-pressed="true"');
    expect(html).toContain('aria-label="Turn off sound"');
    expect(html).toContain('<span>Sound on</span>');
    expect(html).toContain('href="https://www.youtube.com/watch?v=vKQi3bBA1y8"');
    expect(html).toContain('aria-label="Close the trailer"');
    expect(html).toMatch(
      /<iframe class="cd-trailer-frame" src="https:\/\/www\.youtube-nocookie\.com\/embed\/vKQi3bBA1y8\?autoplay=1&amp;mute=0&amp;playsinline=1&amp;rel=0&amp;iv_load_policy=3&amp;enablejsapi=1" title="The Matrix Reloaded trailer" allow="autoplay; encrypted-media; picture-in-picture; fullscreen" allowFullScreen="">/,
    );
    // The corner under the close button is cut out of the body while it
    // plays (see grid.css).
    expect(html).toContain('class="cd-sheet-body cd-sheet-playing"');
  });

  it('says the sound is off, and offers it, when it started muted', () => {
    const muted: Play = { ...startPlay(null, 'panel', ID, true, 430, true)! };
    const html = panel(KEY, {}, muted);
    expect(html).toContain('aria-pressed="false"');
    expect(html).toContain('aria-label="Turn on sound"');
    expect(html).toContain('<span>Turn on sound</span>');
    expect(html).toContain('&amp;mute=1&amp;');
  });

  it('keeps its controls out of reach while they fade in and out', () => {
    const opening = startPlay(null, 'panel', ID, false, 430, false)!;
    expect(panel(KEY, {}, opening)).toContain('class="cd-trailer-controls" inert=""');
    expect(panel(KEY, {}, stopPlay({ ...opening, open: true }))).toContain('class="cd-trailer-controls" inert=""');
  });

  it('leaves a player open for another film alone', () => {
    const other: Play = { ...startPlay(null, 'panel', 'tt9999999', false, 430, true)! };
    const html = panel(KEY, {}, other);
    expect(html).not.toContain('<iframe');
    expect(html).not.toContain('cd-sheet-playing');
  });
});

describe('Escape in the panel', () => {
  it('closes the trailer first, and the panel with the next press', () => {
    let play: Play | null = { ...startPlay(null, 'panel', ID, false, 430, false)!, open: true };
    const did: string[] = [];
    // A player whose stop does what the real one does to the state.
    const player: Player = {
      ...playerWith(null, did),
      now: () => play,
      stop: (where) => {
        did.push(`stop ${where}`);
        play = stopPlay(play) ?? play;
      },
    };
    const leave = () => did.push('leave');
    sheetEscape(player, ID, leave);
    expect(did).toEqual(['stop panel']);
    sheetEscape(player, ID, leave);
    expect(did).toEqual(['stop panel', 'leave']);
  });

  it('closes the panel at once when no trailer is open', () => {
    const did: string[] = [];
    sheetEscape(playerWith(null, did), ID, () => did.push('leave'));
    expect(did).toEqual(['leave']);
  });
});

/** The declarations of every rule with exactly this selector, @media
 *  ones included, later ones winning. Enough of a parser for a sheet
 *  with no nesting beyond @media. */
function decls(selector: string): Map<string, string> {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const out = new Map<string, string>();
  for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!m[1].split(',').map((x) => x.trim()).includes(selector)) continue;
    for (const d of m[2].split(';')) {
      const at = d.indexOf(':');
      if (at > 0) out.set(d.slice(0, at).trim(), d.slice(at + 1).trim());
    }
  }
  return out;
}

const px = (v: string | undefined) => Number(v?.match(/^(-?[\d.]+)px$/)?.[1] ?? NaN);

/** What the panel's synopsis is measured against at a window size. */
interface Panel {
  cls: ScreenClass;
  /** The body's visible height. */
  bodyH: number;
  /** Where the synopsis starts in it. */
  synTop: number;
  /** The video's height. */
  VH: number;
}

/** The panel at a window size, as the stylesheet lays it out with enough
 *  in it to fill its room (a long synopsis and a cast list). The head is
 *  as tall as its poster, as it is for any title of a line or two. */
function panelAt(w: number, h: number, searched = false): Panel {
  const screen = screenOf(w, h);
  const cls = screen.cls;
  const sheet = decls(cls === 'desktop' ? '.cd-sheet' : `.cd-sheet-${cls}`);
  const base = decls('.cd-sheet');
  // Its own rule for this class, or the one every class shares.
  const own = (sel: string, prop: string) =>
    px(decls(`.cd-sheet-${cls} ${sel}`).get(prop) ?? decls(sel).get(prop));
  let sheetH: number;
  let sheetW: number;
  if (cls === 'phone') {
    // Up from the bottom, as tall as its contents up to its cap, and
    // held in from both sides.
    sheetH = h - Number(sheet.get('max-height')!.match(/^calc\(100% - (\d+)px\)$/)![1]);
    sheetW = w - px(sheet.get('left')) - px(sheet.get('right'));
  } else {
    sheetH = h - px(sheet.get('top') ?? base.get('top')) - px(sheet.get('bottom') ?? base.get('bottom'));
    const width = sheet.get('width') ?? base.get('width')!;
    const min = width.match(/^min\((\d+)px, (\d+)vw\)$/);
    sheetW = min ? Math.min(Number(min[1]), (w * Number(min[2])) / 100) : px(width);
  }
  // The phone's pinned Map foot, for any film but the searched one:
  // its padding, its top border and the button.
  const foot = decls('.cd-sheet-foot');
  const [padTop, , padBottom] = foot.get('padding')!.split(' ').map(px);
  const footH =
    cls === 'phone' && !searched
      ? padTop + padBottom + px(foot.get('border-top')!.split(' ')[0]) + px(decls('.cd-sheet-primary').get('height'))
      : 0;
  // The body is pulled up over the wash by its negative margin.
  const bodyH = sheetH - own('.cd-sheet-wash', 'height') - own('.cd-sheet-body', 'margin-top') - footH;
  const poster = own('.cd-sheet-poster', 'width');
  expect(poster).toBe(sheetPosterPx(screen));
  expect(decls('.cd-sheet-poster').get('aspect-ratio')).toBe('2 / 3');
  const synTop = poster * 1.5 + px(decls('.cd-sheet-body').get('gap'));
  return { cls, bodyH, synTop, VH: videoHeight(videoWidth('panel', sheetW - 2 * PLAYER.panel.pad)) };
}

/** A body and a synopsis `textLines` long, laid out as `at` says, the
 *  synopsis drawn at `style` (its clamp and max-height) as React left it.
 *  Only what the panel reads is here. */
function laidOut(at: Panel, textLines: number, style = { webkitLineClamp: '4', maxHeight: '93px' }) {
  const full = textLines * SYN_LH;
  const body = { clientHeight: at.bodyH, scrollTop: 0, querySelector: () => row };
  const p = {
    offsetTop: at.synTop,
    offsetParent: body,
    style: { ...style },
    getBoundingClientRect: () => ({
      height: p.style.maxHeight === 'none' ? full : Math.min(full, parseFloat(p.style.maxHeight)),
    }),
  };
  const row = { offsetTop: 0, offsetParent: body };
  return {
    body: body as unknown as HTMLElement,
    p: p as unknown as HTMLElement,
    style: p.style,
    /** Puts the trailer row under the synopsis as it now shows. */
    rowUnder(over: boolean) {
      row.offsetTop = at.synTop + p.getBoundingClientRect().height + (over ? MORE_ROW_H : 0) + ABOUT_GAP;
    },
  };
}

/** A long overview, of the kind that used to push Watch trailer below
 *  the fold, in lines at the panel's width. */
const LONG = 9;

describe('the panel’s synopsis at every screen class', () => {
  const lines = (w: number, h: number, searched = false) => {
    const { body, p } = laidOut(panelAt(w, h, searched), LONG);
    return measureSynopsis(body, p).lines;
  };

  it('rests at four lines on a desktop, a tablet and a portrait phone', () => {
    expect(panelAt(1440, 900)).toMatchObject({ cls: 'desktop', bodyH: 800, synTop: 188 });
    expect(lines(1440, 900)).toBe(4);
    expect(lines(1366, 768)).toBe(4);
    expect(lines(820, 1180)).toBe(4);
    // A film other than the searched one, so the Map foot is pinned.
    expect(panelAt(390, 844)).toMatchObject({ cls: 'phone', bodyH: 659, synTop: 158 });
    expect(lines(390, 844)).toBe(4);
    expect(lines(390, 844, true)).toBe(4);
  });

  it('rests at fewer in a short window, so Watch trailer shows without a scroll', () => {
    expect(panelAt(667, 375)).toMatchObject({ cls: 'short', bodyH: 313, synTop: 134 });
    expect(lines(667, 375)).toBe(3);
    expect(lines(640, 360)).toBe(2);
    for (const [w, h] of [
      [1440, 900],
      [1366, 768],
      [390, 844],
      [667, 375],
      [640, 360],
    ]) {
      const at = panelAt(w, h);
      const n = lines(w, h);
      // The trailer row's foot, More's row counted, and 16 to spare.
      expect(at.synTop + n * SYN_LH + MORE_ROW_H + ABOUT_GAP + 44 + 16, `${w}x${h}`).toBeLessThanOrEqual(at.bodyH);
    }
  });

  it('never rests at fewer than two, however little room there is', () => {
    expect(lines(640, 320)).toBe(2);
    expect(panelAt(568, 320).cls).toBe('phone');
    expect(lines(568, 320)).toBe(2);
  });

  it('is measured whole with its clamp and max-height off for the one read, then put back', () => {
    const { body, p, style } = laidOut(panelAt(1440, 900), LONG);
    expect(measureSynopsis(body, p)).toEqual({ lines: 4, over: true, full: LONG * SYN_LH });
    expect(style).toEqual({ webkitLineClamp: '4', maxHeight: '93px' });
  });

  it('is measured where it rests while a trailer holds it folded away, or partway there', () => {
    // The screen class changing under an open trailer: the block is
    // pulled up by its margin, and the paragraph with it.
    const pulledUp = (at: Panel, margin: number) => {
      const { body, p } = laidOut({ ...at, synTop: at.synTop + margin }, LONG, {
        webkitLineClamp: 'unset',
        maxHeight: '0px',
      });
      Object.assign(p, { parentElement: { margin } });
      return measureSynopsis(body, p);
    };
    vi.stubGlobal('getComputedStyle', (el: { margin: number }) => ({ marginTop: `${el.margin}px` }));
    try {
      // 380 tall, three lines, where the 14px the block is pulled up by
      // would look like room for a fourth.
      const at = panelAt(667, 380);
      expect(at.cls).toBe('short');
      expect(restingLines(at.bodyH, at.synTop - ABOUT_GAP)).toBe(4);
      for (const [w, h] of [
        [1440, 900],
        [390, 844],
        [667, 375],
        [667, 380],
        [640, 360],
      ]) {
        const rest = laidOut(panelAt(w, h), LONG);
        const want = measureSynopsis(rest.body, rest.p);
        for (const margin of [-ABOUT_GAP, -5, 0]) {
          expect(pulledUp(panelAt(w, h), margin), `${w}x${h} pulled up ${-margin}`).toEqual(want);
        }
      }
      expect(pulledUp(at, -ABOUT_GAP).lines).toBe(3);
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

/** What the stylesheet cannot say about the phone's head at 390×844,
 *  as Chrome lays it out: the text column beside the poster is 220px
 *  wide, which a long title such as "The Lord of the Rings: The
 *  Fellowship of the Ring" takes four lines of, and a line of the meta's
 *  13.5px Figtree is 16px tall. */
const LONG_TITLE_LINES = 4;
const META_LINE = 16;

/** Where the synopsis starts in a phone's body under a long title and
 *  three genres, which do not fit beside the year and the pill and drop
 *  onto a second line: the head's text, taller than its poster, and the
 *  body's gap. */
function phoneTopUnderGenres(): number {
  const titleLine =
    px(decls('.cd-sheet-phone .cd-sheet-title').get('font-size')) * Number(decls('.cd-sheet-title').get('line-height'));
  const text = decls('.cd-sheet-head-text');
  const pill = META_LINE + 2 * px(decls('.cd-sheet-pill').get('padding')!.split(' ')[0]);
  const meta = pill + px(decls('.cd-sheet-meta').get('row-gap')) + META_LINE;
  const head = LONG_TITLE_LINES * titleLine + px(text.get('gap')) + meta + px(text.get('padding-bottom'));
  return head + px(decls('.cd-sheet-body').get('gap'));
}

describe('the panel’s genres', () => {
  it('drop onto a second line as one piece, in the line’s own type', () => {
    const meta = decls('.cd-sheet-meta');
    expect(meta.get('flex-wrap')).toBe('wrap');
    expect(px(meta.get('gap'))).toBe(8);
    expect(px(meta.get('row-gap'))).toBe(5);
    // No rule of their own: a flex item moves to the next line whole,
    // and breaks inside itself only when it is wider than the column.
    // Kept to one line instead, a list that long would run out of it.
    expect(decls('.cd-sheet-genres').size).toBe(0);
  });

  it('leave the synopsis measured from its real top, on a phone under a long title', () => {
    const at = panelAt(390, 844);
    const top = phoneTopUnderGenres();
    // The head runs past its poster, so the synopsis starts lower than
    // a head as tall as the poster would put it; Chrome has it at 194.
    expect(top).toBeCloseTo(194, 0);
    expect(top).toBeGreaterThan(at.synTop);
    const { body, p } = laidOut({ ...at, synTop: top }, LONG);
    const m = measureSynopsis(body, p);
    expect(m.lines).toBe(restingLines(at.bodyH, top));
    expect(m.lines).toBe(4);
    // Watch trailer's foot, More's row counted, and 16 to spare.
    expect(top + m.lines * SYN_LH + MORE_ROW_H + ABOUT_GAP + 44 + 16).toBeLessThanOrEqual(at.bodyH);
    // Where the room is short, the taller head costs lines: counted from
    // a poster-high head, a phone 540 tall would rest at four.
    const low = panelAt(390, 540);
    expect(restingLines(low.bodyH, low.synTop)).toBe(4);
    const under = laidOut({ ...low, synTop: top }, LONG);
    expect(measureSynopsis(under.body, under.p).lines).toBe(2);
  });
});

describe('More / Less', () => {
  const ID2 = 'tt0145487';
  const measured = (textLines: number, id = ID) => {
    const { body, p } = laidOut(panelAt(1440, 900), textLines);
    return measuredSyn(restingSyn(id), id, measureSynopsis(body, p));
  };
  const drawn = (s: SynState, play: Play | null = null) =>
    renderToStaticMarkup(
      createElement(SheetSynopsis, { text: SYNOPSIS, view: synopsisView(s, play, false), onToggle: () => {} }),
    );

  it('shows only when the text runs past its resting lines', () => {
    expect(measured(LONG).over).toBe(true);
    expect(drawn(measured(LONG))).toContain(
      '<div class="cd-sheet-syn-more"><button type="button" aria-expanded="false">More</button></div>',
    );
    for (const n of [1, 3, 4]) {
      expect(measured(n).over).toBe(false);
      expect(drawn(measured(n))).not.toContain('cd-sheet-syn-more');
    }
    // Where it rests at three, a fourth line is one too many.
    const { body, p } = laidOut(panelAt(667, 375), 4);
    expect(measureSynopsis(body, p)).toMatchObject({ lines: 3, over: true });
  });

  it('opens to the whole text with the clamp off, and says so', () => {
    const open = toggledSyn(measured(LONG), true, LONG * SYN_LH, false);
    const v = synopsisView(open, null, false);
    expect(v).toMatchObject({ expanded: true, maxHeight: LONG * SYN_LH, clamp: 'unset', tr: '360ms var(--ease-glide)' });
    expect(drawn(open)).toContain('<button type="button" aria-expanded="true">Less</button>');
  });

  it('folds back with the clamp off until the fold is done, then clamps again', () => {
    const open = toggledSyn(measured(LONG), true, LONG * SYN_LH, false);
    const folding = toggledSyn(open, false, LONG * SYN_LH, false);
    expect(synopsisView(folding, null, false)).toMatchObject({
      expanded: false,
      maxHeight: 93,
      clamp: 'unset',
      tr: '300ms var(--ease-close)',
    });
    expect(drawn(folding)).toContain('aria-expanded="false">More</button>');
    expect(synopsisView({ ...folding, easing: false }, null, false).clamp).toBe(4);
    // Nothing waits for a reader who has asked for nothing to move.
    const still = toggledSyn(open, false, LONG * SYN_LH, true);
    expect(synopsisView(still, null, true)).toMatchObject({ clamp: 4, tr: '0s' });
  });

  it('starts collapsed again when the panel opens another film', () => {
    const open = toggledSyn(measured(LONG), true, LONG * SYN_LH, false);
    expect(synFor(open, ID)).toBe(open);
    expect(synFor(open, ID2)).toEqual(restingSyn(ID2));
    const { body, p } = laidOut(panelAt(1440, 900), LONG);
    const next = measuredSyn(open, ID2, measureSynopsis(body, p));
    expect(next).toMatchObject({ id: ID2, expanded: false, over: true, lines: 4 });
    // The same film measured again, when the screen class changes, keeps
    // it open.
    expect(measuredSyn(open, ID, { lines: 3, over: true, full: LONG * SYN_LH }).expanded).toBe(true);
  });
});

describe('the panel’s synopsis as a trailer opens', () => {
  /** How the panel makes room at a size, the synopsis resting as
   *  measured there and `expanded` if More has opened it. */
  function room(w: number, h: number, expanded = false) {
    const at = panelAt(w, h);
    const probe = laidOut(at, LONG);
    const m = measureSynopsis(probe.body, probe.p);
    const shownStyle = { webkitLineClamp: 'unset', maxHeight: expanded ? 'none' : `${m.lines * SYN_LH}px` };
    const { body, p, rowUnder } = laidOut(at, LONG, shownStyle);
    rowUnder(m.over);
    return { ...roomInPanel(body, p, at.VH, m), lines: m.lines, at };
  }

  it('changes nothing where the whole video already fits, even with More open', () => {
    for (const [w, h] of [
      [1440, 900],
      [1366, 768],
      [390, 844],
    ]) {
      expect(room(w, h), `${w}x${h}`).toMatchObject({ synLines: null, to: null });
    }
    expect(room(1440, 900, true)).toMatchObject({ synLines: null, to: null });
  });

  it('folds to as many lines as leave room, with no scroll', () => {
    expect(room(820, 700)).toMatchObject({ lines: 4, synLines: 3, to: null });
    expect(room(820, 660)).toMatchObject({ lines: 4, synLines: 1, to: null });
    // With More open the row sits lower, so it needs more room; the
    // synopsis collapses as part of the fold, back to its resting lines
    // when that alone is enough, and further when not.
    expect(room(820, 760)).toMatchObject({ synLines: null, to: null });
    expect(room(820, 760, true)).toMatchObject({ synLines: 4, to: null });
    expect(room(820, 700, true)).toMatchObject({ synLines: 3, to: null });
  });

  it('folds away when one line is too many, still with no scroll when that is enough', () => {
    expect(room(820, 620)).toMatchObject({ synLines: 0, to: null });
  });

  it('scrolls only for what folding away could not find', () => {
    const r = room(667, 375);
    expect(r).toMatchObject({ lines: 3, synLines: 0 });
    expect(r.to).toBeCloseTo(81);
    // Folded away, the row is where the synopsis started; scrolled, the
    // video ends 12 above the body's visible bottom.
    const rowTop = r.at.synTop;
    expect(rowTop + 44 + 4 + r.at.VH - (r.to! + r.at.bodyH - 12)).toBeCloseTo(0);
  });

  it('never scrolls the controls row nearer the top than 8', () => {
    const r = room(640, 320);
    expect(r.synLines).toBe(0);
    // The row is where the synopsis started once it has folded away.
    expect(r.to).toBe(r.at.synTop - 8);
    // So some of the video is left below the fold, rather than the
    // controls above it.
    expect(r.at.synTop + 48 + r.at.VH).toBeGreaterThan(r.to! + r.at.bodyH - 12);
  });

  it('folds with the player, its clamp following once the fold is done', () => {
    const s = foldingSyn(measured4());
    const opening = startPlay(null, 'panel', ID, false, 400, false, 3)!;
    // Before the player opens nothing has moved; the clamp is already off.
    expect(synopsisView(s, opening, false)).toMatchObject({ maxHeight: 93, clamp: 'unset' });
    const open = { ...opening, open: true };
    expect(synopsisView(s, open, false)).toMatchObject({
      maxHeight: 3 * SYN_LH,
      clamp: 'unset',
      tr: '520ms var(--ease-glide)',
    });
    expect(synopsisView(s, { ...open, synSettled: true }, false).clamp).toBe(3);
  });

  it('folds away with More’s row, which takes no press while it is gone', () => {
    const s = foldingSyn(measured4());
    const gone = { ...startPlay(null, 'panel', ID, false, 400, false, 0)!, open: true };
    expect(synopsisView(s, gone, false)).toMatchObject({ gone: true, maxHeight: 0 });
    const html = renderToStaticMarkup(
      createElement(SheetSynopsis, { text: SYNOPSIS, view: synopsisView(s, gone, false), onToggle: () => {} }),
    );
    expect(html).toContain('<div class="cd-sheet-syn cd-sheet-syn-gone"');
    expect(html).toContain('<button type="button" aria-expanded="false" disabled="">More</button>');
  });

  it('keeps its fold while the trailer holds it, More opening nothing', () => {
    const s = foldingSyn(measured4());
    const opening = startPlay(null, 'panel', ID, false, 400, false, 1)!;
    const open = { ...opening, open: true, synSettled: true };
    // From the moment the trailer is asked for until its close starts,
    // More takes no press.
    expect(holdsFold(opening)).toBe(true);
    expect(holdsFold(open)).toBe(true);
    const folded = { maxHeight: SYN_LH, clamp: 1, expanded: false, more: true };
    expect(synopsisView(s, open, false)).toMatchObject(folded);
    // And the fold is the view's to keep: the whole text, were it open,
    // would push the video past the body's bottom edge.
    expect(synopsisView({ ...s, expanded: true }, open, false)).toMatchObject(folded);
    const html = renderToStaticMarkup(
      createElement(SheetSynopsis, { text: SYNOPSIS, view: synopsisView(s, open, false), onToggle: () => {} }),
    );
    expect(html).toContain('<button type="button" aria-expanded="false">More</button>');
    // A trailer that needed none of its room, or one on its way out,
    // holds nothing.
    expect(holdsFold({ ...open, synLines: null })).toBe(false);
    expect(holdsFold(stopPlay(open))).toBe(false);
    expect(holdsFold(null)).toBe(false);
  });

  it('unfolds to its resting lines as the player closes, collapsed, its clamp back once the player has gone', () => {
    const s = foldingSyn(measured4());
    const open = { ...startPlay(null, 'panel', ID, false, 400, false, 1)!, open: true, synSettled: true };
    const closing = stopPlay(open)!;
    expect(synopsisView(collapsedSyn(s), closing, false)).toMatchObject({
      expanded: false,
      maxHeight: 93,
      clamp: 'unset',
      tr: '400ms var(--ease-close)',
    });
    expect(synopsisView(collapsedSyn(s), null, false)).toMatchObject({ expanded: false, maxHeight: 93, clamp: 4 });
  });

  it('comes back collapsed however the play ends, with no close to play too', () => {
    // For a reader who has asked for nothing to move the player opens
    // settled and goes at once, never closing. Open, even: whatever left
    // it so, what the panel gives back as the fold ends is collapsed.
    const open = startPlay(null, 'panel', ID, false, 400, true, 1)!;
    expect(open).toMatchObject({ open: true, synSettled: true });
    const s = { ...foldingSyn(measured4()), expanded: true, full: LONG * SYN_LH };
    expect(holdsFold(open)).toBe(true);
    expect(synopsisView(s, open, true)).toMatchObject({ expanded: false, maxHeight: SYN_LH, clamp: 1 });
    expect(holdsFold(null)).toBe(false);
    expect(synopsisView(collapsedSyn(s), null, true)).toMatchObject({
      expanded: false,
      maxHeight: 93,
      clamp: 4,
      tr: '0s',
    });
  });

  it('opens at once to More pressed as the player closes', () => {
    const open = { ...startPlay(null, 'panel', ID, false, 400, false, 1)!, open: true, synSettled: true };
    const closing = stopPlay(open)!;
    const s = toggledSyn(foldingSyn(measured4()), true, LONG * SYN_LH, false);
    expect(synopsisView(s, closing, false)).toMatchObject({
      expanded: true,
      maxHeight: LONG * SYN_LH,
      clamp: 'unset',
      tr: '400ms var(--ease-close)',
    });
    const html = renderToStaticMarkup(
      createElement(SheetSynopsis, { text: SYNOPSIS, view: synopsisView(s, closing, false), onToggle: () => {} }),
    );
    expect(html).toContain('<button type="button" aria-expanded="true">Less</button>');
  });

  it('leaves More open through a trailer that did not need its room', () => {
    const s = toggledSyn(measured4(), true, LONG * SYN_LH, false);
    const open = { ...startPlay(null, 'panel', ID, false, 430, false, null)!, open: true };
    expect(synopsisView(s, open, false)).toMatchObject({ expanded: true, maxHeight: LONG * SYN_LH });
    expect(synopsisView(s, stopPlay(open), false)).toMatchObject({ expanded: true, maxHeight: LONG * SYN_LH });
  });

  it('is drawn from the player’s state in the panel itself', () => {
    const folded = { ...startPlay(null, 'panel', ID, false, 430, false, 1)!, open: true, synSettled: true };
    expect(panel(KEY, { synopsis: SYNOPSIS }, folded)).toContain(
      '<p class="cd-sheet-syn-text" style="max-height:23.25px;-webkit-line-clamp:1">',
    );
    const gone = panel(KEY, { synopsis: SYNOPSIS }, { ...folded, synLines: 0 });
    expect(gone).toContain('<div class="cd-sheet-syn cd-sheet-syn-gone"');
    expect(gone).toContain('<p class="cd-sheet-syn-text" style="max-height:0;');
    const closing = panel(KEY, { synopsis: SYNOPSIS }, stopPlay(folded));
    expect(closing).toContain('<div class="cd-sheet-syn" style="--syn-tr:400ms var(--ease-close)">');
    expect(closing).toContain('style="max-height:93px;-webkit-line-clamp:unset"');
  });
});

/** A long synopsis measured where it rests at four lines. */
function measured4(): SynState {
  const { body, p } = laidOut(panelAt(1440, 900), LONG);
  return measuredSyn(restingSyn(ID), ID, measureSynopsis(body, p));
}
