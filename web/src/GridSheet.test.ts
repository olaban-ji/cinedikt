import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import { GridSheet, roleLine, scalePct, sheetEscape, versus, versusText } from './GridSheet';
import type { GridFilm, GridPayload, GridPerson } from './grid';
import type { Player } from './TrailerRow';
import { startPlay, stopPlay, type Play } from './trailer';

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
  it('is shown whole, between the title and the comparison', () => {
    const html = panel(KEY, { synopsis: SYNOPSIS });
    expect(html).toContain(`<p class="cd-sheet-synopsis">${SYNOPSIS}</p>`);
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
      expect(html).not.toContain('cd-sheet-synopsis');
      expect(html).toContain('cd-sheet-about');
      expect(html).toContain('Watch trailer');
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
