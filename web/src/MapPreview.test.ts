import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import type { GridFilm } from './grid';
import { MapPreview } from './MapPreview';
import type { PreviewPlace } from './preview';
import type { Player } from './TrailerRow';
import { startPlay, type Play } from './trailer';

// What the trailer lookup has already answered for the film on show,
// which a first render shows straight away.
const answer = vi.hoisted(() => ({ key: undefined as string | null | undefined }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  trailerKnown: () => answer.key,
  fetchTrailer: () => new Promise<string | null>(() => {}),
}));

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
    sound: () => {},
    resize: () => {},
    frame: { current: null },
  };
}

/** The preview as its first render draws it. */
function preview(
  key: string | null | undefined,
  over: Partial<GridFilm> = {},
  place: PreviewPlace = { x: 627, side: -1, top: 1306, bottom: null },
  play: Play | null = null,
): string {
  answer.key = key;
  return renderToStaticMarkup(
    createElement(MapPreview, {
      film: film(over),
      place,
      player: playerWith(play),
      bounds: () => ({ x0: 0, x1: 0, vt: 0, vb: 0 }),
      plotH: 20000,
      onEnter: () => {},
      onLeave: () => {},
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
