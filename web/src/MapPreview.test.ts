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
  TrailerFocus,
  faceEvents,
  faceTitle,
  facesRow,
  leavingClass,
  previewPlay,
} from './MapPreview';
import { faceAfter, faceStart, photoArrived } from './PersonFace';
import {
  PREVIEW_BACK_MS,
  PREVIEW_LEAVE_PLAYING_MS,
  PREVIEW_OUT_PLAYING_MS,
  PREVIEW_REST_MS,
  previewScheduler,
  type PreviewClock,
  type PreviewPlace,
} from './preview';
import { ENTER_MS } from './sheet';
import { usePlayer, type Player } from './TrailerRow';
import { TRAILER_GONE_MS, TRAILER_OPEN_MS, startPlay, stopPlay, type Play } from './trailer';

// What the trailer lookup has already answered for the film on show,
// which a first render shows straight away.
const answer = vi.hoisted(() => ({ key: undefined as string | null | undefined }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  trailerKnown: () => answer.key,
  fetchTrailer: () => new Promise<string | null>(() => {}),
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
const PEOPLE = (matrix as GridPayload).people;

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
    expect(classOf({})).toBe('cd-preview cd-preview-left');
    expect(classOf({ leaving: 'plain' })).toBe('cd-preview cd-preview-left cd-preview-out');
    expect(classOf({ leaving: 'playing' })).toBe('cd-preview cd-preview-left cd-preview-out cd-preview-out-playing');
    expect(classOf({ leaving: 'gone' })).toBe('cd-preview cd-preview-left cd-preview-out cd-preview-gone');
    // Taken back, it simply loses them, and the entrance takes over.
    expect(classOf({ back: true })).toBe('cd-preview cd-preview-left');
    expect(leavingClass(undefined)).toBe('');
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
    // Taken back, its faces open one again.
    expect(preview(KEY, people, undefined, null, photoOf, { back: true })).not.toContain('title="Keanu Reeves"');
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
