import { createElement, type ComponentProps } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import css from './grid.css?raw';
import { faceCardController, type FaceCard } from './faceCard';
import { headerClass } from './GridApp';
import { previewHost, type Peek } from './GridMap';
import matrix from './fixtures/matrix-grid.json';
import { initialsFor, layoutGrid, type GridFilm, type GridPayload, type GridPerson } from './grid';
import {
  MapPreview,
  PreviewStream,
  TrailerFocus,
  afterGrow,
  faceEvents,
  faceTitle,
  facesRow,
  growSoon,
  growsUp,
  holdAgain,
  leavingClass,
  previewPlay,
  rowHolds,
  streamPhaseOf,
  streamStartsGrown,
  type Lift,
} from './MapPreview';
import { faceAfter, faceStart, photoArrived } from './PersonFace';
import {
  PREVIEW_BACK_MS,
  PREVIEW_LEAVE_PLAYING_MS,
  PREVIEW_OUT_PLAYING_MS,
  PREVIEW_REST_MS,
  STREAM_FOLD_MS,
  STREAM_GROW_MS,
  holdInside,
  previewScheduler,
  type PreviewClock,
  type PreviewPlace,
} from './preview';
import { ENTER_MS } from './sheet';
import { usePlayer, type Player } from './TrailerRow';
import { TRAILER_GONE_MS, TRAILER_OPEN_MS, startPlay, stopPlay, type Play } from './trailer';
import { logoBroke, type WatchOffer, type WatchState, type WhereToWatch } from './whereToWatch';

// What the trailer lookup has already answered for the film on show,
// which a first render shows straight away.
const answer = vi.hoisted(() => ({ key: undefined as string | null | undefined }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  trailerKnown: () => answer.key,
  fetchTrailer: () => new Promise<string | null>(() => {}),
}));

// Where the movie on show can be watched, as its preview first draws it.
// Nothing yet, unless a test says otherwise.
const watch = vi.hoisted(() => ({ state: { status: 'wait', data: null } as WatchState }));
vi.mock('./whereToWatch', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./whereToWatch')>()),
  useWhereToWatch: () => watch.state,
}));

// The real module reaches for PostHog, which has nothing to do with
// what is under test here.
vi.mock('./analytics', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./analytics')>()),
  capture: () => {},
}));

// The effects each render asks for. The server renderer runs none of
// them; a test that needs what it drew mounted, and then unmounted, runs
// them by hand (see mount).
const effects = vi.hoisted(() => [] as (() => void | (() => void))[]);
vi.mock('react', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react')>()),
  useEffect: (effect: () => void | (() => void)) => {
    effects.push(effect);
  },
}));

/** Runs the effects the renders since the last call asked for, as
 *  mounting what they drew would, and returns its unmount, which runs
 *  their clean-ups. */
function mount(): () => void {
  const cleanups = effects.splice(0).map((effect) => effect());
  return () => {
    for (const cleanup of cleanups) if (typeof cleanup === 'function') cleanup();
  };
}

const ID = 'tt0089218';
const KEY = 'hJ2j4oWdQtU';
const SYNOPSIS = 'A gang of kids about to lose their homes find an old treasure map.';

function film(over: Partial<GridFilm> = {}): GridFilm {
  return { id: ID, title: 'The Goonies', year: 1985, rating: 7.7, md: 0, people: [], isAnchor: false, ...over };
}

function playerWith(play: Play | null): Player {
  return {
    play,
    now: () => play,
    start: () => {},
    stop: () => {},
    drop: () => {},
    fade: () => {},
    sound: () => {},
    resize: () => {},
    frame: { current: null },
  };
}

/** The Matrix's twelve, directors first, as its map lists them. */
const PEOPLE = (matrix as unknown as GridPayload).people;

/** The preview as its first render draws it, on The Matrix's map. */
function preview(
  key: string | null | undefined,
  over: Partial<GridFilm> = {},
  place: PreviewPlace = { x: 627, side: -1, top: 1306, bottom: null },
  play: Play | null = null,
  photoOf: (p: GridPerson) => string | undefined = (p) => p.photo,
  more: Partial<ComponentProps<typeof MapPreview>> = {},
): string {
  answer.key = key;
  return renderToStaticMarkup(
    createElement(MapPreview, {
      film: film(over),
      people: PEOPLE,
      anchorTitle: 'The Matrix',
      theme: 'dark',
      codes: initialsFor(PEOPLE),
      photoOf,
      place,
      player: playerWith(play),
      bounds: () => ({ x0: 0, x1: 0, vt: 0, vb: 0 }),
      plotH: 20000,
      onEnter: () => {},
      onLeave: () => {},
      onFace: () => {},
      offFace: () => {},
      ...more,
    }),
  );
}

describe('the hover preview', () => {
  it('is a group named for its film, placed where it was worked out to go', () => {
    const html = preview(KEY, { synopsis: SYNOPSIS });
    expect(html).toMatch(/^<div class="cd-preview cd-preview-left" style="left:627px;top:1306px;--h:\d+" role="group" aria-label="The Goonies, preview">/);
    // Never focusable itself: it never takes the focus.
    expect(html).not.toMatch(/^<div[^>]*tabindex/);
  });

  it('grows up from its bottom when that is the edge it is held by', () => {
    const html = preview(KEY, {}, { x: 824, side: 1, top: null, bottom: 1200 });
    expect(html).toMatch(/^<div class="cd-preview" style="left:824px;bottom:1200px;/);
  });

  it('shows the title, the year and the rating, or No rating', () => {
    expect(preview(KEY)).toContain(
      '<span class="cd-preview-title">The Goonies</span><span class="cd-preview-meta"><span>1985</span><span class="cd-preview-pill">7.7</span></span>',
    );
    expect(preview(KEY, { rating: null })).toContain('<span class="cd-preview-pill">No rating</span>');
  });

  it('follows the rating with the genres, as plain text, when IMDb lists some', () => {
    expect(preview(KEY, { genres: ['Action', 'Sci-Fi'] })).toContain(
      '<span class="cd-preview-meta"><span>1985</span><span class="cd-preview-pill">7.7</span><span class="cd-preview-genres">Action · Sci-Fi</span></span>',
    );
    expect(preview(KEY, { rating: null, genres: ['Adventure', 'Comedy', 'Family'] })).toContain(
      '<span class="cd-preview-pill">No rating</span><span class="cd-preview-genres">Adventure · Comedy · Family</span></span>',
    );
    // With none, the line is the year and the rating alone.
    for (const genres of [undefined, [], ['', ' ']]) {
      const html = preview(KEY, { genres });
      expect(html).toContain('<span class="cd-preview-meta"><span>1985</span><span class="cd-preview-pill">7.7</span></span>');
      expect(html).not.toContain('cd-preview-genres');
    }
  });

  it('shows the synopsis, or says there is none yet', () => {
    expect(preview(KEY, { synopsis: SYNOPSIS })).toContain(`<p class="cd-preview-syn">${SYNOPSIS}</p>`);
    for (const synopsis of [undefined, '', '  ']) {
      expect(preview(KEY, { synopsis })).toContain('<p class="cd-preview-syn cd-preview-syn-none">No synopsis yet.</p>');
    }
  });

  it('holds the trailer row’s place while the answer is on its way, then offers it, or says there is none', () => {
    expect(preview(undefined)).toContain('<span class="cd-trailer-skel cd-trailer-preview" aria-hidden="true"></span>');
    const has = preview(KEY);
    expect(has).toContain('cd-trailer cd-trailer-preview');
    expect(has).toContain('aria-label="Play the trailer for The Goonies"');
    expect(preview(null)).toContain('No trailer available');
  });

  it('folds the synopsis to the lines the trailer left it, its clamp following once the fold is done', () => {
    const open: Play = { ...startPlay(null, 'preview', ID, true, 360, true, 3)!, open: true };
    const html = preview(KEY, { synopsis: SYNOPSIS }, undefined, open);
    expect(html).toContain('cd-preview-trailer cd-preview-trailer-open');
    // A first render has not waited for the fold, so the clamp is still six.
    expect(html).toContain('<p class="cd-preview-syn" style="max-height:60.75px;-webkit-line-clamp:6">');
    const gone: Play = { ...open, synLines: 0 };
    expect(preview(KEY, { synopsis: SYNOPSIS }, undefined, gone)).toContain(
      '<p class="cd-preview-syn cd-preview-syn-gone" style="max-height:0;-webkit-line-clamp:6">',
    );
  });

  it('leaves the synopsis whole while the trailer is closing', () => {
    const closing: Play = { ...startPlay(null, 'preview', ID, true, 360, true, 3)!, open: false, closing: true };
    const html = preview(KEY, { synopsis: SYNOPSIS }, undefined, closing);
    expect(html).toContain(`<p class="cd-preview-syn">${SYNOPSIS}</p>`);
    expect(html).toContain('cd-preview-trailer');
    expect(html).not.toContain('cd-preview-trailer-open');
  });

  it('leaves a player open in the panel alone', () => {
    const panel: Play = { ...startPlay(null, 'panel', ID, false, 430, true)! };
    const html = preview(KEY, {}, undefined, panel);
    expect(html).not.toContain('cd-preview-trailer');
    expect(html).not.toContain('<iframe');
  });
});

describe('the hover preview as it leaves', () => {
  /** The opening tag of the preview, leaving as asked. */
  const tag = (more: Partial<ComponentProps<typeof MapPreview>>) =>
    preview(KEY, {}, undefined, null, undefined, more).match(/^<div[^>]*>/)?.[0] ?? '';
  const classOf = (more: Partial<ComponentProps<typeof MapPreview>>) => tag(more).match(/class="([^"]*)"/)?.[1];

  it('takes cd-preview-out, with -out-playing for its trailer, or -gone giving way to the next card’s', () => {
    // Not leaving, or taken back while it was: the entrance alone.
    expect(classOf({})).toBe('cd-preview cd-preview-left');
    expect(leavingClass(undefined)).toBe('');
    expect(classOf({ leaving: 'plain' })).toBe('cd-preview cd-preview-left cd-preview-out');
    expect(classOf({ leaving: 'playing' })).toBe('cd-preview cd-preview-left cd-preview-out cd-preview-out-playing');
    expect(classOf({ leaving: 'gone' })).toBe('cd-preview cd-preview-left cd-preview-out cd-preview-gone');
  });

  it('is inert and hidden only while giving way: one leaving can still be taken back by the pointer', () => {
    for (const leaving of [undefined, 'plain', 'playing'] as const) {
      expect(tag({ leaving }), leaving).not.toMatch(/ (inert|aria-hidden)=/);
    }
    expect(tag({ leaving: 'gone' })).toContain(' inert=""');
    expect(tag({ leaving: 'gone' })).toContain(' aria-hidden="true"');
  });

  describe('as it unmounts', () => {
    beforeEach(() => {
      vi.useFakeTimers();
      vi.stubGlobal('window', globalThis);
    });
    afterEach(() => {
      vi.useRealTimers();
      vi.unstubAllGlobals();
    });

    it('drops the player in the preview for its own film', () => {
      const dropped: unknown[][] = [];
      const player = { ...playerWith(null), drop: (...args: unknown[]) => dropped.push(args) };
      effects.length = 0;
      preview(null, {}, undefined, null, undefined, { player });
      const unmount = mount();
      expect(dropped).toEqual([]);
      unmount();
      expect(dropped).toEqual([['preview', ID]]);
    });

    it('leaves a player that belongs to another preview alone', () => {
      const player = realPlayer();
      effects.length = 0;
      preview(null, {}, undefined, null, undefined, { player });
      const unmount = mount();
      // The next card's preview has started its trailer meanwhile.
      player.start('preview', 'tt0088763', true, 360);
      unmount();
      expect(player.now()?.id).toBe('tt0088763');
    });

    it('puts away a bigger photo from its faces, or one on its way, as its leave begins', () => {
      for (const leaving of ['plain', 'playing', 'gone'] as const) {
        const offFace = vi.fn();
        effects.length = 0;
        preview(null, {}, undefined, null, undefined, { leaving, offFace });
        mount();
        expect(offFace, leaving).toHaveBeenCalledExactlyOnceWith('peek');
      }
      // Not while it is on show.
      const offFace = vi.fn();
      effects.length = 0;
      preview(null, {}, undefined, null, undefined, { offFace });
      mount();
      expect(offFace).not.toHaveBeenCalled();
    });

    it('puts one away again as it unmounts, unless it gave way on a swap: by then one can only be the next preview’s', () => {
      for (const leaving of [undefined, 'plain', 'playing'] as const) {
        const offFace = vi.fn();
        effects.length = 0;
        preview(null, {}, undefined, null, undefined, { leaving, offFace });
        const unmount = mount();
        offFace.mockClear();
        unmount();
        expect(offFace, leaving).toHaveBeenCalledExactlyOnceWith('peek');
      }
      const offFace = vi.fn();
      effects.length = 0;
      preview(null, {}, undefined, null, undefined, { leaving: 'gone', offFace });
      const unmount = mount();
      offFace.mockClear();
      unmount();
      expect(offFace).not.toHaveBeenCalled();
    });
  });
});

describe('the hover preview’s faces', () => {
  const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
  const ids = (...names: string[]) => names.map((n) => PEOPLE.find((p) => p.name === n)!.id);
  /** The faces row, from its tag to its end. */
  const facesIn = (html: string) => {
    const at = html.indexOf('<div class="cd-preview-faces');
    expect(at).toBeGreaterThan(-1);
    return html.slice(at, html.indexOf('</div>', at) + 6);
  };
  const count = (html: string, s: string) => html.split(s).length - 1;

  it('names one or two people beside their faces', () => {
    const two = facesIn(preview(KEY, { people: ids('Keanu Reeves', 'Laurence Fishburne') }));
    expect(two).toMatch(/^<div class="cd-preview-faces cd-preview-faces-named" role="img" aria-label="[^"]*">/);
    expect(count(two, 'class="cd-preview-face"')).toBe(2);
    expect(two).toContain('<span class="cd-preview-face-name">Keanu Reeves</span>');
    expect(two).toContain('<span class="cd-preview-face-name">Laurence Fishburne</span>');
    const one = facesIn(preview(KEY, { people: ids('Hugo Weaving') }));
    expect(one).toContain('cd-preview-faces-named');
    expect(one).toContain('<span class="cd-preview-face-name">Hugo Weaving</span>');
  });

  it('shows three to nine as faces alone, each with its name in its title', () => {
    for (const n of [3, 9]) {
      const names = PEOPLE.slice(0, n).map((p) => p.name);
      const row = facesIn(preview(KEY, { people: ids(...names) }));
      expect(row).toMatch(/^<div class="cd-preview-faces" role="img"/);
      expect(row).not.toContain('cd-preview-face-name');
      expect(count(row, 'class="cd-preview-face"')).toBe(n);
      for (const name of names) expect(row).toContain(`title="${name}"`);
      expect(row).not.toContain('cd-preview-faces-more');
    }
  });

  it('shows eight and a count past nine: all twelve of The Matrix, for the searched film', () => {
    // The searched film is everyone's, whatever its own list says.
    const row = facesIn(preview(KEY, { id: 'tt0133093', title: 'The Matrix', isAnchor: true, people: [] }));
    expect(count(row, 'class="cd-preview-face"')).toBe(8);
    expect(row).toContain('<span class="cd-preview-faces-more">+4</span>');
    // The first eight in the chip row's order.
    expect([...row.matchAll(/title="([^"]+)"/g)].map((m) => m[1])).toEqual(PEOPLE.slice(0, 8).map((p) => p.name));
    const ten = facesIn(preview(KEY, { people: PEOPLE.slice(0, 10).map((p) => p.id) }));
    expect(count(ten, 'class="cd-preview-face"')).toBe(8);
    expect(ten).toContain('<span class="cd-preview-faces-more">+2</span>');
  });

  it('is one image named for the panel’s heading and every name', () => {
    const connected = facesIn(preview(KEY, { people: ids('Laurence Fishburne', 'Keanu Reeves') }));
    expect(connected).toContain(
      'role="img" aria-label="Connected to The Matrix through: Keanu Reeves, Laurence Fishburne"',
    );
    const searched = facesIn(preview(KEY, { id: 'tt0133093', title: 'The Matrix', isAnchor: true, people: [] }));
    expect(searched).toContain(`aria-label="Its cast and directors: ${PEOPLE.map((p) => p.name).join(', ')}"`);
    // The four past the eighth are named too.
    expect(searched).toContain('Matt Doran, Belinda McClory"');
  });

  it('keeps the chip row’s order, directors first, whatever order the film lists them in', () => {
    const row = facesIn(preview(KEY, { people: ids('Hugo Weaving', 'Keanu Reeves', 'Lilly Wachowski') }));
    expect([...row.matchAll(/title="([^"]+)"/g)].map((m) => m[1])).toEqual([
      'Lilly Wachowski',
      'Keanu Reeves',
      'Hugo Weaving',
    ]);
  });

  it('draws each face in its person’s colour, a director’s as a rounded square', () => {
    const row = facesIn(
      preview(KEY, { people: ids('Lana Wachowski', 'Keanu Reeves', 'Hugo Weaving') }, undefined, null, (p) =>
        p.name === 'Keanu Reeves' ? KEANU : undefined,
      ),
    );
    expect(row).toMatch(
      new RegExp(
        '<span class="cd-preview-face" title="Lana Wachowski" style="--tone:oklch\\([^)]*\\);--swatch-r:2px">' +
          '<span class="cd-face cd-face-preview cd-face-square" aria-hidden="true"><span class="cd-face-initials">LaW</span></span></span>',
      ),
    );
    // His photo is drawn over his initials, and until it has loaded the
    // face is named by its title as one without a photo is.
    expect(row).toMatch(
      new RegExp(
        '<span class="cd-preview-face" title="Keanu Reeves" style="--tone:oklch\\([^)]*\\);--swatch-r:50%">' +
          '<span class="cd-face cd-face-preview" aria-hidden="true"><span class="cd-face-initials">KR</span>' +
          `<span class="cd-face-ring"></span><img class="cd-face-photo" src="${KEANU}" alt="" decoding="async"/></span></span>`,
      ),
    );
  });

  it('sits between the title, year and rating and the synopsis', () => {
    const html = preview(KEY, { synopsis: SYNOPSIS, people: ids('Keanu Reeves') });
    const head = html.indexOf('class="cd-preview-head"');
    const faces = html.indexOf('class="cd-preview-faces');
    const syn = html.indexOf('class="cd-preview-syn"');
    expect(head).toBeGreaterThan(-1);
    expect(faces).toBeGreaterThan(head);
    expect(syn).toBeGreaterThan(faces);
  });

  it('is left out when nobody on the map is on the film', () => {
    expect(preview(KEY, { people: [] })).not.toContain('cd-preview-faces');
    expect(preview(KEY, { people: ['nm9999999'] })).not.toContain('cd-preview-faces');
  });
});

describe('a bigger photo from the hover preview’s faces', () => {
  const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
  const HUGO = 'https://image.tmdb.org/t/p/w185/hugo.jpg';
  const ids = (...names: string[]) => names.map((n) => PEOPLE.find((p) => p.name === n)!.id);

  afterEach(() => {
    vi.useRealTimers();
  });

  it('is asked for from: peek by a pointer resting on a face', () => {
    vi.useFakeTimers();
    const clock: PreviewClock = {
      now: () => Date.now(),
      after: (ms, run) => setTimeout(run, ms),
      cancel: (t) => clearTimeout(t),
    };
    const opened: FaceCard[] = [];
    const face = {} as HTMLElement;
    const cards = faceCardController(
      {
        hovers: () => true,
        photo: (id) => (id === 'nm0000206' ? KEANU : undefined),
        // A face low in the window: the card stands above it.
        place: (el, from) => (el === face && from === 'peek' ? { x: 400, y: 212, down: false } : null),
        open: (card) => opened.push(card),
        close: () => {},
      },
      clock,
      (_url, done) => {
        done(true);
        return () => {};
      },
    );
    const keanu = faceEvents('nm0000206', cards.onFace, cards.offFace);
    keanu.onMouseEnter({ currentTarget: face });
    vi.advanceTimersByTime(399);
    expect(opened).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(opened).toEqual([{ id: 'nm0000206', from: 'peek', x: 400, y: 212, down: false }]);
  });

  it('drops the title of a face showing its photo, and keeps it on one without', () => {
    // Keanu's photo has loaded already this visit, as the chips' have by
    // the time a preview opens; Hugo's is still on its way; Gloria has none.
    const SEEN = 'https://image.tmdb.org/t/p/w185/keanu-seen.jpg';
    photoArrived(SEEN);
    const photos: Record<string, string> = { 'Keanu Reeves': SEEN, 'Hugo Weaving': HUGO };
    const html = preview(KEY, { people: ids('Keanu Reeves', 'Hugo Weaving', 'Gloria Foster') }, undefined, null, (p) =>
      photos[p.name],
    );
    expect(html).toMatch(
      /<span class="cd-preview-face" style="--tone:[^"]*"><span class="cd-face cd-face-preview cd-face-in" aria-hidden="true">/,
    );
    expect(html).not.toContain('title="Keanu Reeves"');
    expect(html).toMatch(
      /<span class="cd-preview-face" title="Hugo Weaving" style="--tone:[^"]*"><span class="cd-face cd-face-preview" aria-hidden="true">/,
    );
    expect(html).toContain('<span class="cd-preview-face" title="Gloria Foster" style="--tone:');
  });

  it('names a face in its title for as long as it shows initials: no photo, one on its way, or one that failed', () => {
    const start = faceStart(KEANU, new Set());
    const loaded = faceAfter(start, { type: 'load', photo: KEANU });
    const failed = faceAfter(start, { type: 'error', photo: KEANU });
    expect(faceTitle('Keanu Reeves', loaded.loaded)).toBeUndefined();
    expect(faceTitle('Keanu Reeves', start.loaded)).toBe('Keanu Reeves');
    expect(faceTitle('Keanu Reeves', failed.loaded)).toBe('Keanu Reeves');
    expect(faceTitle('Gloria Foster', faceStart(undefined).loaded)).toBe('Gloria Foster');
  });

  it('opens none while a trailer is set in the preview, since nothing may lie on YouTube’s player, and names every face instead', () => {
    const onFace = vi.fn();
    const offFace = vi.fn();
    const keanu = faceEvents('nm0000206', onFace, offFace, false);
    keanu.onMouseEnter({ currentTarget: {} as HTMLElement });
    expect(onFace).not.toHaveBeenCalled();
    keanu.onMouseLeave();
    expect(offFace).toHaveBeenCalledTimes(1);

    const SEEN = 'https://image.tmdb.org/t/p/w185/keanu-seen-playing.jpg';
    photoArrived(SEEN);
    const opening = startPlay(null, 'preview', ID, true, 360, false)!;
    const photoOf = (p: GridPerson) => (p.name === 'Keanu Reeves' ? SEEN : undefined);
    const people = { people: ids('Keanu Reeves', 'Gloria Foster') };
    for (const play of [opening, { ...opening, open: true }]) {
      const html = preview(KEY, people, undefined, play, photoOf);
      expect(html).toContain('<span class="cd-preview-face" title="Keanu Reeves" style="--tone:');
      expect(html).toContain('<span class="cd-preview-face" title="Gloria Foster" style="--tone:');
    }
    // Without a trailer, the face showing its photo drops the title again.
    expect(preview(KEY, people, undefined, null, photoOf)).not.toContain('title="Keanu Reeves"');
  });

  it('opens none from a preview that is leaving, and names every face instead', () => {
    const SEEN = 'https://image.tmdb.org/t/p/w185/keanu-seen-leaving.jpg';
    photoArrived(SEEN);
    const photoOf = (p: GridPerson) => (p.name === 'Keanu Reeves' ? SEEN : undefined);
    const people = { people: ids('Keanu Reeves') };
    for (const leaving of ['plain', 'playing', 'gone'] as const) {
      expect(preview(KEY, people, undefined, null, photoOf, { leaving }), leaving).toContain(
        '<span class="cd-preview-face" title="Keanu Reeves" style="--tone:',
      );
    }
    // Taken back, it is no longer leaving, and its faces open one again.
    expect(preview(KEY, people, undefined, null, photoOf)).not.toContain('title="Keanu Reeves"');
  });
});

/** A service, with a logo for each theme. */
const service = (id: string, name: string, more: Partial<WatchOffer> = {}): WatchOffer => ({
  id,
  name,
  link: `https://example.com/${id}/watch`,
  logo: { dark: `https://img.example.com/${id}-dark.svg`, light: `https://img.example.com/${id}-light.svg` },
  ...more,
});

/** An answer in the United States with these groups. */
function streams(groups: Partial<Pick<WhereToWatch, 'stream' | 'free' | 'rent' | 'buy'>>): WatchState {
  return {
    status: 'ok',
    data: { countryName: 'United States', covered: true, stream: [], free: [], rent: [], buy: [], ...groups },
  };
}

const SIX = streams({
  stream: [
    service('netflix', 'Netflix'),
    service('prime', 'Prime Video'),
    service('max', 'Max'),
    service('starz', 'Starz', { via: 'Prime Video' }),
  ],
  free: [service('tubi', 'Tubi'), service('pluto', 'Pluto TV')],
  rent: [service('apple', 'Apple TV', { price: '3.99 USD' })],
});

/** The preview as its first render draws it, knowing where its movie can
 *  be watched. */
function withWatch(state: WatchState, ...rest: Parameters<typeof preview>): string {
  watch.state = state;
  try {
    return preview(...rest);
  } finally {
    watch.state = { status: 'wait', data: null };
  }
}

/** The Stream row, from its tag to the end of the preview. */
const streamOf = (html: string) => {
  const at = html.indexOf('<div class="cd-preview-stream');
  return at < 0 ? '' : html.slice(at, html.lastIndexOf('</div>'));
};

describe('the hover preview’s Stream row', () => {
  it('names three services and counts the rest, as the preview’s last line, under Watch trailer', () => {
    const html = withWatch(SIX, KEY);
    const row = streamOf(html);
    expect(row).toMatch(/^<div class="cd-preview-stream"><div class="cd-preview-stream-clip"><div class="cd-preview-stream-row"><span class="cd-preview-stream-label">Stream<\/span><a /);
    expect(row.match(/<a /g)).toHaveLength(3);
    // Starz, Tubi and Pluto TV; rent is not streaming. And the last
    // child: the row runs to the end of the box.
    expect(row.endsWith('<span class="cd-preview-stream-more">+3</span></div></div></div>')).toBe(true);
    expect(html.indexOf('cd-trailer cd-trailer-preview')).toBeLessThan(html.indexOf('cd-preview-stream'));
  });

  it('counts nothing when three or fewer stream, free ones after the rest, each service once', () => {
    const row = streamOf(
      withWatch(streams({ stream: [service('netflix', 'Netflix')], free: [service('tubi', 'Tubi'), service('netflix', 'Netflix')] }), KEY),
    );
    expect(row.match(/aria-label="([^"]*)"/g)).toEqual([
      'aria-label="Stream on Netflix, opens in a new tab"',
      'aria-label="Stream on Tubi, opens in a new tab"',
    ]);
    expect(row).not.toContain('cd-preview-stream-more');
  });

  it('opens each service’s page for the movie in a new tab, saying so to assistive tech', () => {
    const row = streamOf(withWatch(SIX, KEY));
    expect(row).toContain(
      '<a class="cd-preview-stream-chip" href="https://example.com/netflix/watch" target="_blank" rel="noopener noreferrer" aria-label="Stream on Netflix, opens in a new tab">',
    );
    expect(row).toContain('aria-label="Stream on Prime Video, opens in a new tab"');
  });

  it('draws each logo for the theme, as decoration, since the chip already names the service', () => {
    const dark = streamOf(withWatch(SIX, KEY));
    expect(dark).toContain('<img class="cd-preview-stream-logo" src="https://img.example.com/netflix-dark.svg" alt="" decoding="async"/>');
    const light = streamOf(withWatch(SIX, KEY, {}, undefined, null, undefined, { theme: 'light' }));
    expect(light).toContain('src="https://img.example.com/netflix-light.svg"');
    expect(light).not.toContain('-dark.svg');
  });

  it('shows the service’s name for a logo with no address, or one that has failed to load', () => {
    logoBroke('https://img.example.com/max-dark.svg');
    const row = streamOf(
      withWatch(streams({ stream: [service('netflix', 'Netflix', { logo: {} }), service('max', 'Max')] }), KEY),
    );
    expect(row).toContain('aria-label="Stream on Netflix, opens in a new tab"><span class="cd-preview-stream-name">Netflix</span></a>');
    expect(row).toContain('aria-label="Stream on Max, opens in a new tab"><span class="cd-preview-stream-name">Max</span></a>');
    expect(row).not.toContain('<img');
  });

  it('is not there without anything to stream: rent and buy only, nothing at all, no coverage, no answer yet, or a failure', () => {
    const none: WatchState[] = [
      streams({ rent: [service('apple', 'Apple TV', { price: '3.99 USD' })], buy: [service('apple', 'Apple TV')] }),
      streams({}),
      { status: 'ok', data: { covered: false, stream: [], free: [], rent: [], buy: [] } },
      { status: 'wait', data: null },
      { status: 'error', data: null },
    ];
    for (const state of none) {
      const html = withWatch(state, KEY);
      expect(html, JSON.stringify(state)).not.toContain('cd-preview-stream');
      expect(html).toContain('cd-trailer cd-trailer-preview');
    }
  });

  it('folds away while a trailer is set in the preview, opening, open or closing, and takes no pointer then', () => {
    const opening = startPlay(null, 'preview', ID, true, 360, false)!;
    const open: Play = { ...opening, open: true };
    for (const play of [opening, open, stopPlay(open)!]) {
      expect(streamOf(withWatch(SIX, KEY, {}, undefined, play))).toMatch(
        /^<div class="cd-preview-stream cd-preview-stream-folded" inert="" aria-hidden="true">/,
      );
    }
    expect(decls('.cd-preview-stream-folded').get('pointer-events')).toBe('none');
    // Not for a player in the panel, nor another movie's.
    const panel = startPlay(null, 'panel', ID, false, 430, true)!;
    const other = { ...startPlay(null, 'preview', 'tt0088763', true, 360, true)! };
    for (const play of [panel, other]) {
      expect(streamOf(withWatch(SIX, KEY, {}, undefined, play))).toMatch(/^<div class="cd-preview-stream">/);
    }
  });

  it('goes with the preview as it leaves, folding nothing on its own', () => {
    for (const leaving of ['plain', 'playing', 'gone'] as const) {
      const html = withWatch(SIX, KEY, {}, undefined, null, undefined, { leaving });
      expect(streamOf(html), leaving).toMatch(/^<div class="cd-preview-stream">/);
    }
  });

  it('is there from the first paint for an answer in as the preview opens', () => {
    // An answer that comes later is drawn closed, and grows in (see below).
    expect(streamOf(withWatch(SIX, KEY))).toMatch(/^<div class="cd-preview-stream">/);
  });
});

describe('the Stream row arriving after the preview has opened', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal('window', globalThis);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  // The row's opening tag. A server render puts a preload for its logo
  // ahead of it.
  const row = (phase: 'out' | 'in' | 'folded') =>
    renderToStaticMarkup(
      createElement(PreviewStream, {
        shown: [service('netflix', 'Netflix')],
        more: 0,
        theme: 'dark',
        phase,
      }),
    ).match(/<div class="cd-preview-stream[^>]*>/)?.[0];

  it('is drawn closed, then grows in a moment later, once its closed state has been drawn', () => {
    expect(row('out')).toBe('<div class="cd-preview-stream cd-preview-stream-out" inert="" aria-hidden="true">');
    const open = vi.fn();
    growSoon(open);
    vi.advanceTimersByTime(ENTER_MS - 1);
    expect(open).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(open).toHaveBeenCalledOnce();
    expect(row('in')).toBe('<div class="cd-preview-stream">');
    // A row taken away first never grows.
    const gone = vi.fn();
    growSoon(gone)();
    vi.advanceTimersByTime(1000);
    expect(gone).not.toHaveBeenCalled();
  });

  it('is not drawn while the preview has no answer: there is nothing to grow from', () => {
    expect(preview(KEY)).not.toContain('cd-preview-stream');
  });

  it('starts closed for an answer that comes after the preview opened, and open for one in as it opened', () => {
    // No answer as the preview opened: drawn closed once one comes, and
    // open once growSoon has fired.
    let grown = streamStartsGrown(false, false);
    expect(grown).toBe(false);
    expect(streamPhaseOf(false, grown, undefined, false)).toBeNull();
    expect(streamPhaseOf(true, grown, undefined, false)).toBe('out');
    const open = vi.fn(() => {
      grown = true;
    });
    growSoon(open);
    vi.advanceTimersByTime(ENTER_MS);
    expect(open).toHaveBeenCalledOnce();
    expect(streamPhaseOf(true, grown, undefined, false)).toBe('in');
    // An answer in as it opened is open from the first paint, and so is
    // any for a reader who has asked for nothing to move.
    expect(streamStartsGrown(true, false)).toBe(true);
    expect(streamPhaseOf(true, streamStartsGrown(true, false), undefined, false)).toBe('in');
    expect(streamStartsGrown(false, true)).toBe(true);
  });

  it('waits on an answer that comes while the preview is leaving, and grows in if it is taken back', () => {
    for (const leaving of ['plain', 'playing', 'gone'] as const) {
      expect(streamPhaseOf(true, false, leaving, false), leaving).toBeNull();
      // One that had grown goes with the leave, folding nothing.
      expect(streamPhaseOf(true, true, leaving, false), leaving).toBe('in');
    }
    // Taken back: drawn closed, to grow in.
    expect(streamPhaseOf(true, false, undefined, false)).toBe('out');
  });

  it('folds while a trailer is set in the preview, once it has grown', () => {
    expect(streamPhaseOf(true, true, undefined, true)).toBe('folded');
    expect(streamPhaseOf(true, false, undefined, true)).toBe('out');
    expect(streamPhaseOf(false, true, undefined, true)).toBeNull();
    expect(row('folded')).toBe('<div class="cd-preview-stream cd-preview-stream-folded" inert="" aria-hidden="true">');
  });

  it('is inert and hidden whenever it is not open, so no keyboard or screen reader reaches a link that cannot be seen', () => {
    expect(row('in')).toBe('<div class="cd-preview-stream">');
    for (const phase of ['out', 'folded'] as const) {
      expect(row(phase), phase).toContain(' inert=""');
      expect(row(phase), phase).toContain(' aria-hidden="true"');
    }
  });

  it('holds the preview inside the map again once it has grown', () => {
    const hold = vi.fn();
    afterGrow(hold);
    vi.advanceTimersByTime(STREAM_GROW_MS + 19);
    expect(hold).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(hold).toHaveBeenCalledOnce();
    // For a reader who has asked for nothing to move, the row is open as
    // it is drawn: held there and then, with no timer to wait a paint on.
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }));
    const now = vi.fn();
    afterGrow(now);
    expect(now).toHaveBeenCalledOnce();
  });

  it('holds the preview again only while the row is open and the preview is not leaving, which fades where it stands', () => {
    expect(rowHolds('in', undefined)).toBe(true);
    for (const leaving of ['plain', 'playing', 'gone'] as const) {
      expect(rowHolds('in', leaving), leaving).toBe(false);
    }
    for (const phase of ['out', 'folded', null] as const) {
      expect(rowHolds(phase, undefined), String(phase)).toBe(false);
    }
  });

  it('grows the preview held by its bottom upwards, held by its top from the axis if it would pass it', () => {
    const b = { x0: 0, x1: 1440, vt: 1034, vb: 1718 };
    const up: PreviewPlace = { x: 600, side: 1, top: null, bottom: 20000 - 1700 };
    // Still room above: it stays held by its bottom, and so grew upwards.
    expect(holdAgain(up, null, 344, b, 20000)).toEqual({});
    // Past the axis: held by its top at the axis instead.
    expect(holdAgain(up, null, 700, b, 20000)).toEqual({ held: { ...up, top: 1034, bottom: null } });
    expect(holdAgain(up, null, 700, b, 20000).held).toEqual(holdInside(up, 700, b, 20000));
  });

  it('moves a preview held by its top up as far as it grew past the floating buttons’ strip', () => {
    const b = { x0: 0, x1: 1440, vt: 1034, vb: 1718 };
    const down: PreviewPlace = { x: 600, side: 1, top: 1400, bottom: null };
    expect(holdAgain(down, null, 306, b, 20000)).toEqual({});
    expect(holdAgain(down, null, 344, b, 20000)).toEqual({ held: { ...down, top: 1718 - 344 } });
    // Once a trailer has opened in it, it is held by its top at y0, which
    // moves instead.
    const lift: Lift = { y0: 1400, y1: 1200, synLines: null };
    expect(holdAgain(down, lift, 306, b, 20000)).toEqual({});
    expect(holdAgain(down, lift, 344, b, 20000)).toEqual({ lift: { ...lift, y0: 1718 - 344 } });
  });

  it('grows a preview held by its top upwards, from where its bottom is, when the row would take it past the edge', () => {
    const b = { x0: 0, x1: 1440, vt: 1034, vb: 1718 };
    // Its bottom is at 1712, and 38px more would pass 1718: held by that
    // bottom instead, so the row grows upwards with nothing moving first.
    const down: PreviewPlace = { x: 600, side: 1, top: 1406, bottom: null };
    expect(growsUp(down, null, 306, 38, b, 20000)).toEqual({ ...down, top: null, bottom: 20000 - 1712 });
    // Once grown, it fits there, so holding it again moves nothing.
    const up = growsUp(down, null, 306, 38, b, 20000)!;
    expect(holdAgain(up, null, 344, b, 20000)).toEqual({});
  });

  it('leaves a preview where it is when the row fits below, it already grows upwards, a trailer has moved it, or it would pass the top', () => {
    const b = { x0: 0, x1: 1440, vt: 1034, vb: 1718 };
    // Room below for the row: it grows down.
    expect(growsUp({ x: 600, side: 1, top: 1374, bottom: null }, null, 306, 38, b, 20000)).toBeNull();
    // Held by its bottom already.
    expect(growsUp({ x: 600, side: 1, top: null, bottom: 20000 - 1700 }, null, 306, 38, b, 20000)).toBeNull();
    // Held at the lift's y0 since a trailer opened in it.
    const lift: Lift = { y0: 1406, y1: 1200, synLines: null };
    expect(growsUp({ x: 600, side: 1, top: 1406, bottom: null }, lift, 306, 38, b, 20000)).toBeNull();
    // Taller than the room above as well: holdAgain settles it once grown.
    expect(growsUp({ x: 600, side: 1, top: 1050, bottom: null }, null, 700, 38, b, 20000)).toBeNull();
  });
});

describe('the Stream row as the stylesheet draws it', () => {
  it('is a wrapping row with 6px gaps, its label 12px/600 in --t3 with 2px after it', () => {
    expect(Object.fromEntries(decls('.cd-preview-stream-row'))).toEqual({
      display: 'flex',
      'flex-wrap': 'wrap',
      'align-items': 'center',
      gap: '6px',
    });
    expect(Object.fromEntries(decls('.cd-preview-stream-label'))).toEqual({
      'margin-right': '2px',
      'font-size': '12px',
      'font-weight': '600',
      color: 'var(--t3)',
    });
    // The box's own 12px gap is what separates it from Watch trailer.
    expect(decls('.cd-preview').get('gap')).toBe('12px');
  });

  it('draws each chip 26px tall, 9px in, rounded 8px, in a 1px --ln3 ring, lit as Watch trailer is on hover', () => {
    const chip = decls('.cd-preview-stream-chip');
    expect(chip.get('height')).toBe('26px');
    expect(chip.get('box-sizing')).toBe('border-box');
    expect(chip.get('padding')).toBe('0 9px');
    expect(chip.get('border-radius')).toBe('8px');
    expect(chip.get('box-shadow')).toBe('inset 0 0 0 1px var(--ln3)');
    expect(chip.get('font-size')).toBe('12.5px');
    expect(chip.get('font-weight')).toBe('600');
    expect(chip.get('transition')).toBe('background-color 0.15s ease, box-shadow 0.15s ease');
    expect(Object.fromEntries(decls('.cd-preview-stream-chip:hover', '(hover: hover)'))).toEqual({
      background: 'var(--accWash)',
      'box-shadow': 'inset 0 0 0 1px var(--acc)',
    });
  });

  it('draws each logo 14px tall and at most 72px wide, whole', () => {
    expect(Object.fromEntries(decls('.cd-preview-stream-logo'))).toEqual({
      display: 'block',
      height: '14px',
      'max-width': '72px',
      'object-fit': 'contain',
    });
  });

  it('counts the rest 12px/700 in --t3, in tabular figures', () => {
    const more = decls('.cd-preview-stream-more');
    expect(more.get('font-size')).toBe('12px');
    expect(more.get('font-weight')).toBe('700');
    expect(more.get('color')).toBe('var(--t3)');
    expect(more.get('font-variant-numeric')).toBe('tabular-nums');
  });

  it('grows in from 0fr, cancelling the gap above it, over 260ms on the glide, the fade over 0.2s', () => {
    expect(decls('.cd-preview-stream').get('grid-template-rows')).toBe('1fr');
    expect(decls('.cd-preview-stream').get('transition')).toBe(
      `grid-template-rows ${STREAM_GROW_MS}ms var(--ease-glide), margin-top ${STREAM_GROW_MS}ms var(--ease-glide), opacity 0.2s ease`,
    );
    expect(Object.fromEntries(decls('.cd-preview-stream-out'))).toEqual({
      'grid-template-rows': '0fr',
      'margin-top': '-12px',
      opacity: '0',
    });
    expect(Object.fromEntries(decls('.cd-preview-stream-clip'))).toEqual({ 'min-height': '0', overflow: 'hidden' });
    expect(STREAM_GROW_MS).toBe(260);
  });

  it('folds the same way over 320ms', () => {
    const folded = decls('.cd-preview-stream-folded');
    expect(folded.get('grid-template-rows')).toBe('0fr');
    expect(folded.get('margin-top')).toBe('-12px');
    expect(folded.get('opacity')).toBe('0');
    expect(folded.get('transition')).toBe(
      `grid-template-rows ${STREAM_FOLD_MS}ms var(--ease-glide), margin-top ${STREAM_FOLD_MS}ms var(--ease-glide), opacity 0.2s ease`,
    );
    expect(STREAM_FOLD_MS).toBe(320);
  });

  it('simply appears and goes for a reader who has asked for nothing to move', () => {
    for (const selector of ['.cd-preview-stream', '.cd-preview-stream-folded', '.cd-preview-stream-chip']) {
      expect(decls(selector, '(prefers-reduced-motion: reduce)').get('transition'), selector).toBe('none');
    }
  });

  it('keeps each chip’s edge in forced colours, and its focus ring inside the clip', () => {
    expect(decls('.cd-preview-stream-chip', '(forced-colors: active)').get('outline')).toBe('1px solid ButtonText');
    expect(decls('.cd-preview-stream-chip:focus-visible').get('outline-offset')).toBe('-2px');
  });
});

describe('facesRow', () => {
  it('says who, how many more, and whether they are named', () => {
    const film = { isAnchor: false, people: PEOPLE.slice(2, 4).map((p) => p.id) };
    const row = facesRow(film, PEOPLE, 'The Matrix');
    expect(row.shown.map((p) => p.name)).toEqual(['Keanu Reeves', 'Laurence Fishburne']);
    expect(row.more).toBe(0);
    expect(row.named).toBe(true);
    expect(row.label).toBe('Connected to The Matrix through: Keanu Reeves, Laurence Fishburne');
    const all = facesRow({ isAnchor: true, people: [] }, PEOPLE, 'The Matrix');
    expect(all.shown).toHaveLength(8);
    expect(all.more).toBe(4);
    expect(all.named).toBe(false);
    const nine = facesRow({ isAnchor: false, people: PEOPLE.slice(0, 9).map((p) => p.id) }, PEOPLE, 'The Matrix');
    expect(nine.shown).toHaveLength(9);
    expect(nine.more).toBe(0);
  });
});

/** Every rule that holds declarations, in the sheet's order, with the
 *  @media query it sits in (null at the top level). Enough of a parser
 *  for a sheet with no nesting beyond @media and no braces inside
 *  strings. */
function rules(): { media: string | null; selector: string; decls: Map<string, string> }[] {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const out: { media: string | null; selector: string; decls: Map<string, string> }[] = [];
  const open: { prelude: string; start: number }[] = [];
  let mark = 0;
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '{') {
      open.push({ prelude: text.slice(mark, i).trim(), start: i + 1 });
      mark = i + 1;
    } else if (text[i] === '}') {
      const block = open.pop();
      mark = i + 1;
      if (!block || block.prelude.startsWith('@')) continue;
      const media = open.find((b) => b.prelude.startsWith('@media'));
      const decls = new Map<string, string>();
      for (const d of text.slice(block.start, i).split(';')) {
        const at = d.indexOf(':');
        if (at > 0) decls.set(d.slice(0, at).trim(), d.slice(at + 1).trim().replace(/\s+/g, ' '));
      }
      out.push({
        media: media ? media.prelude.replace(/^@media\s+/, '') : null,
        selector: block.prelude.replace(/\s+/g, ' '),
        decls,
      });
    }
  }
  return out;
}

const SHEET = rules();

/** The declarations every rule with exactly this selector gives, in
 *  this @media query (null: outside any), a later one winning. */
function decls(selector: string, media: string | null = null): Map<string, string> {
  const out = new Map<string, string>();
  for (const r of SHEET) {
    if (r.media !== media || !r.selector.split(',').map((x) => x.trim()).includes(selector)) continue;
    for (const [k, v] of r.decls) out.set(k, v);
  }
  return out;
}

/** The z-index the stylesheet gives the element at the start of this
 *  markup, from its classes: the last top-level rule for one of them
 *  that sets it, as the cascade settles rules of the same weight. */
function zIndexOf(html: string): number {
  const classes = (html.match(/^<[a-z]+ class="([^"]*)"/)?.[1] ?? '').split(' ').map((c) => `.${c}`);
  let z: string | undefined;
  for (const r of SHEET) {
    if (r.media === null && classes.includes(r.selector) && r.decls.has('z-index')) z = r.decls.get('z-index');
  }
  return Number(z);
}

/** The page's own player, made by a render so its hooks are real, then
 *  driven by its own commands and timers. That render is over, so what
 *  it would draw next is read from `now()`, which is what GridMap's next
 *  render would hand the layer as `play`. */
function realPlayer(): Player {
  const made: { player?: Player } = {};
  renderToStaticMarkup(
    createElement(function Page() {
      made.player = usePlayer();
      return null;
    }),
  );
  return made.player!;
}

/** The layer as GridMap draws it, for the player as it stands, the
 *  film whose preview is drawn (null: none is), and whether that preview
 *  is leaving or was taken back. */
function layer(player: Player, showing: string | null = ID, peek: Pick<Peek, 'out' | 'back'> = {}): string {
  return renderToStaticMarkup(createElement(TrailerFocus, { play: player.now(), showing, ...peek }));
}

const FOCUS = '<div class="cd-trailer-focus" aria-hidden="true"></div>';
const FOCUS_IN = '<div class="cd-trailer-focus cd-trailer-focus-in" aria-hidden="true"></div>';
const FOCUS_OUT = '<div class="cd-trailer-focus cd-trailer-focus-out" aria-hidden="true"></div>';
const FOCUS_BACK = '<div class="cd-trailer-focus cd-trailer-focus-in cd-trailer-focus-back" aria-hidden="true"></div>';

describe('the focus on a trailer playing in the preview', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // The player's timers are the window's.
    vi.stubGlobal('window', globalThis);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('mounts as the trailer is opened, and comes in once the player is open', () => {
    const player = realPlayer();
    expect(layer(player)).toBe('');
    player.start('preview', ID, true, 360);
    // Mounted with the player, still out, so its fade has somewhere to
    // run from.
    expect(layer(player)).toBe(FOCUS);
    vi.advanceTimersByTime(ENTER_MS - 1);
    expect(layer(player)).toBe(FOCUS);
    vi.advanceTimersByTime(1);
    expect(player.now()?.open).toBe(true);
    expect(layer(player)).toBe(FOCUS_IN);
  });

  it('goes out as the trailer is stopped, and is gone once the player is cleared', () => {
    const player = realPlayer();
    player.start('preview', ID, true, 360);
    vi.advanceTimersByTime(ENTER_MS);
    expect(layer(player)).toBe(FOCUS_IN);
    player.stop('preview');
    expect(layer(player)).toBe(FOCUS);
    // Still there while the player's close plays, fading with it.
    vi.advanceTimersByTime(TRAILER_GONE_MS - 1);
    expect(layer(player)).toBe(FOCUS);
    vi.advanceTimersByTime(1);
    expect(player.now()).toBeNull();
    expect(layer(player)).toBe('');
  });

  it('comes back in with a trailer taken back as it closes', () => {
    const player = realPlayer();
    player.start('preview', ID, true, 360);
    vi.advanceTimersByTime(ENTER_MS);
    player.stop('preview');
    vi.advanceTimersByTime(200);
    player.start('preview', ID, false, 360);
    expect(layer(player)).toBe(FOCUS_IN);
  });

  it('goes at once when the preview is taken away at once, before its player has been dropped', () => {
    const player = realPlayer();
    player.start('preview', ID, true, 360);
    vi.advanceTimersByTime(ENTER_MS);
    // The render that draws no preview, for another map, a panel or a
    // new layout, draws no layer either, with the player still set: the
    // preview drops it only as it unmounts.
    expect(layer(player, null)).toBe('');
    // Nor for another card's preview drawn in its place.
    expect(layer(player, 'tt0088763')).toBe('');
    // Then the unmount drops it, with no close to play.
    player.drop('preview');
    expect(player.now()).toBeNull();
    expect(layer(player)).toBe('');
  });

  it('lifts as the preview leaves, in step with it, and comes back quicker with a preview taken back', () => {
    const player = realPlayer();
    player.start('preview', ID, false, 360);
    vi.advanceTimersByTime(ENTER_MS);
    expect(layer(player, ID, { out: 'playing' })).toBe(FOCUS_OUT);
    expect(layer(player, ID, { back: true })).toBe(FOCUS_BACK);
    // Still out while the player is opening, and nothing to come back to
    // while it closes.
    const opening = realPlayer();
    opening.start('preview', ID, false, 360);
    expect(layer(opening, ID, { back: true })).toBe(
      '<div class="cd-trailer-focus cd-trailer-focus-back" aria-hidden="true"></div>',
    );
    player.stop('preview');
    expect(layer(player, ID, { out: 'playing' })).toBe(FOCUS_OUT);
  });

  it('takes no pointer, so leaving the preview still starts its leave after 450ms, and the layer lifts with it until it has gone', () => {
    expect(decls('.cd-trailer-focus').get('pointer-events')).toBe('none');
    const player = realPlayer();
    const laid = layoutGrid(matrix as unknown as GridPayload, 1440);
    const card = laid.cards.find((c) => !c.film.isAnchor)!.film.id;
    const slot = { peek: null as Peek | null };
    // As GridMap draws it.
    const drawn = () => layer(player, slot.peek?.id ?? null, { out: slot.peek?.out, back: slot.peek?.back });
    const host = previewHost({
      map: () => ({
        layout: laid,
        blocked: false,
        selected: new Set(),
        minRating: null,
        want: 0,
        said: (id) => film({ id }),
        light: () => {},
        player,
      }),
      bounds: () => ({ x0: 0, x1: laid.plotW, vt: 0, vb: laid.plotH }),
      now: () => slot.peek,
      put: (p) => {
        slot.peek = p;
      },
      gone: () => {},
      held: () => {},
      showing: { current: null },
      under: { current: null },
    });
    const sched = previewScheduler(host, {
      now: () => Date.now(),
      after: (ms, run) => setTimeout(run, ms),
      cancel: (t) => clearTimeout(t),
    });
    sched.rest(card);
    vi.advanceTimersByTime(PREVIEW_REST_MS);
    expect(slot.peek?.id).toBe(card);
    player.start('preview', card, true, 360);
    vi.advanceTimersByTime(ENTER_MS);
    expect(drawn()).toBe(FOCUS_IN);
    sched.hold();
    sched.leave();
    vi.advanceTimersByTime(PREVIEW_LEAVE_PLAYING_MS - 1);
    expect(drawn()).toBe(FOCUS_IN);
    vi.advanceTimersByTime(1);
    expect(slot.peek?.out).toBe('playing');
    expect(drawn()).toBe(FOCUS_OUT);
    // Mounted as long as the leaving preview is, whose player is still set.
    vi.advanceTimersByTime(419);
    expect(player.now()?.id).toBe(card);
    expect(drawn()).toBe(FOCUS_OUT);
    vi.advanceTimersByTime(1);
    expect(slot.peek).toBeNull();
    expect(player.now()).toBeNull();
    expect(drawn()).toBe('');
    sched.dispose();
    host.dispose();
  });

  it('leaves a player in the panel alone: the panel has its own scrim', () => {
    const panel = { ...startPlay(null, 'panel', ID, false, 430, true)! };
    expect(renderToStaticMarkup(createElement(TrailerFocus, { play: panel, showing: ID }))).toBe('');
    expect(previewPlay(panel, ID)).toBeNull();
  });
});

describe('the preview while a trailer is set in it', () => {
  it('stands at z-index 32, over the layer, and at 12 otherwise', () => {
    const opening = startPlay(null, 'preview', ID, true, 360, false)!;
    const open: Play = { ...opening, open: true };
    const closing = stopPlay(open)!;
    const panel = startPlay(null, 'panel', ID, false, 430, true)!;
    for (const play of [opening, open, closing]) {
      const html = preview(KEY, {}, undefined, play);
      expect(html).toMatch(/^<div class="cd-preview[^"]* cd-preview-trailer[ "]/);
      expect(zIndexOf(html)).toBe(32);
    }
    expect(zIndexOf(preview(KEY))).toBe(12);
    expect(zIndexOf(preview(KEY, {}, undefined, panel))).toBe(12);
    expect(zIndexOf(preview(KEY, {}, { x: 824, side: 1, top: null, bottom: 1200 }))).toBe(12);
  });

  it('puts the layer over the header and the floating buttons, and under the preview, a header being searched from, a bigger photo and the toast', () => {
    const z = (selector: string) => Number(decls(selector).get('z-index'));
    expect(z('.cd-trailer-focus')).toBe(31);
    // Under the layer: the header with its chips, and View and Recenter.
    expect(z('.cd-header')).toBe(20);
    expect(z('.cd-float')).toBe(30);
    // Over it: the preview, the header while its search field has the
    // focus (the field and its results, which cannot rise out of it), a
    // bigger photo, and the toast. The preview's own faces open no bigger
    // photo while its trailer is set, since nothing may lie on YouTube's
    // player; one from a chip can still be showing as the preview goes.
    expect(z('.cd-preview-trailer')).toBe(32);
    expect(z('.cd-header-searching')).toBeGreaterThan(z('.cd-preview-trailer'));
    expect(zIndexOf(`<header class="${headerClass({ map: true, over: false, away: false, searching: true })}">`)).toBe(
      z('.cd-header-searching'),
    );
    expect(zIndexOf(`<header class="${headerClass({ map: true, over: true, away: false, searching: true })}">`)).toBe(
      z('.cd-header-searching'),
    );
    expect(z('.cd-person-card')).toBeGreaterThan(32);
    expect(z('.cd-toast')).toBeGreaterThan(32);
  });
});

describe('the layer as the stylesheet draws it', () => {
  it('is the handoff’s: fixed over the window, the ground at 48%, blurred 8px and a little desaturated', () => {
    const d = decls('.cd-trailer-focus');
    expect(Object.fromEntries(d)).toEqual({
      position: 'fixed',
      inset: '0',
      'z-index': '31',
      'pointer-events': 'none',
      background: 'color-mix(in oklch, var(--g) 48%, transparent)',
      '-webkit-backdrop-filter': 'blur(8px) saturate(0.85)',
      'backdrop-filter': 'blur(8px) saturate(0.85)',
      opacity: '0',
      transition: 'opacity 300ms ease',
    });
  });

  it('fades in with the player’s opening, and out before a closing player is cleared', () => {
    expect(Object.fromEntries(decls('.cd-trailer-focus-in'))).toEqual({
      opacity: '1',
      'transition-duration': `${TRAILER_OPEN_MS}ms`,
    });
    // Out in 300ms, over well before the closing player, and the layer
    // with it, is taken away.
    const out = Number(decls('.cd-trailer-focus').get('transition')?.match(/(\d+)ms/)?.[1]);
    expect(out).toBe(300);
    expect(out).toBeLessThan(TRAILER_GONE_MS);
  });

  it('only ever fades: nothing sets its blur but its own rule, and nothing moves but its opacity', () => {
    for (const r of SHEET) {
      if (!/\.cd-trailer-focus(?![\w-])|\.cd-trailer-focus-in/.test(r.selector)) continue;
      if (r.selector !== '.cd-trailer-focus' || r.media !== null) {
        expect(r.decls.has('backdrop-filter')).toBe(false);
        expect(r.decls.has('-webkit-backdrop-filter')).toBe(false);
      }
      for (const [k, v] of r.decls) {
        if (k === 'transition') expect(v === 'none' || v.startsWith('opacity ')).toBe(true);
        if (k === 'transition-property') expect(v).toBe('opacity');
      }
    }
  });

  it('lifts in step with a preview leaving with its trailer, and comes back in 240ms with one taken back', () => {
    expect(Object.fromEntries(decls('.cd-trailer-focus.cd-trailer-focus-out'))).toEqual({
      opacity: '0',
      transition: `opacity ${PREVIEW_OUT_PLAYING_MS}ms var(--ease-close)`,
    });
    expect(Object.fromEntries(decls('.cd-trailer-focus-in.cd-trailer-focus-back'))).toEqual({
      'transition-duration': `${PREVIEW_BACK_MS}ms`,
    });
    // After the rule it comes in by, so their transitions win.
    const at = (selector: string) => SHEET.findIndex((r) => r.media === null && r.selector === selector);
    expect(at('.cd-trailer-focus-in')).toBeGreaterThan(at('.cd-trailer-focus'));
    expect(at('.cd-trailer-focus.cd-trailer-focus-out')).toBeGreaterThan(at('.cd-trailer-focus-in'));
    expect(at('.cd-trailer-focus-in.cd-trailer-focus-back')).toBeGreaterThan(at('.cd-trailer-focus-in'));
  });

  it('is there at once for a reader who has asked for nothing to move, and not at all in forced colours', () => {
    expect(decls('.cd-trailer-focus', '(prefers-reduced-motion: reduce)').get('transition')).toBe('none');
    for (const selector of ['.cd-trailer-focus.cd-trailer-focus-out', '.cd-trailer-focus-in.cd-trailer-focus-back']) {
      expect(decls(selector, '(prefers-reduced-motion: reduce)').get('transition'), selector).toBe('none');
    }
    expect(decls('.cd-trailer-focus', '(forced-colors: active)').get('display')).toBe('none');
  });
});

describe('the stacking the preview rises through', () => {
  // A stacking context on any of these would hold the preview inside
  // it, under the layer and the header whatever its own z-index.
  const OWN = [
    'transform',
    'translate',
    'rotate',
    'scale',
    'filter',
    'backdrop-filter',
    '-webkit-backdrop-filter',
    'perspective',
    'will-change',
    'z-index',
    'isolation',
    'contain',
    'container-type',
    'mix-blend-mode',
    'clip-path',
    'mask',
    'opacity',
  ];

  it('has none from the app, the scroller or the plot', () => {
    const subject = /(^|[\s>+~])\.cd-(app|scroller|plot-wrap|plot)(?![\w-])(:[\w-]+)*$/;
    const seen = new Set<string>();
    for (const r of SHEET) {
      for (const one of r.selector.split(',').map((x) => x.trim())) {
        const m = one.match(subject);
        if (!m) continue;
        seen.add(m[2]);
        for (const prop of OWN) expect(r.decls.has(prop), `${one} sets ${prop}`).toBe(false);
      }
    }
    expect([...seen].sort()).toEqual(['app', 'plot', 'plot-wrap', 'scroller']);
  });

  it('has one from the scroller only while it fades, as a map is left or lands', () => {
    // GridMap opens no preview then (previewBlocked), so there is no
    // trailer for the layer to be behind.
    expect(decls('.cd-scroller-leaving').get('opacity')).toBe('0');
    expect(decls('.cd-scroller').get('transition')).toBe('opacity 0.22s ease');
  });
});
