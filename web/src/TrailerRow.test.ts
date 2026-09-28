import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Player } from './TrailerRow';

// The real module reaches for PostHog, which has nothing to do with
// what is under test here.
vi.mock('./analytics', () => ({ analyticsHeaders: () => ({}), capture: () => {} }));

const ID = 'tt0133093';
const PENDING = { key: null, pending: true };

/** A server that answers from `bodies` in turn, pending once they run out. */
function stubServer(bodies: unknown[]) {
  const fetch = vi.fn(async () => {
    const body = bodies.length ? bodies.shift() : PENDING;
    return { ok: true, status: 200, statusText: '', json: async () => body };
  });
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

const player: Player = {
  play: null,
  now: () => null,
  start: () => {},
  stop: () => {},
  drop: () => {},
  sound: () => {},
  resize: () => {},
  frame: { current: null },
};

// A fresh pair of modules each time, so one test's kept answers are not
// the next test's.
async function load() {
  vi.resetModules();
  const { TRAILER_ASK_AGAIN_MS } = await import('./api');
  const { TrailerRow, askForTrailer, useTrailer } = await import('./TrailerRow');
  /** The row as a panel's first render draws it, from what useTrailer
   *  knows at that moment. */
  const drawn = () =>
    renderToStaticMarkup(
      createElement(function Row() {
        const trailer = useTrailer(ID);
        const props = { where: 'panel' as const, id: ID, title: 'The Matrix', trailer, player, onPlay: () => {} };
        return createElement(TrailerRow, props);
      }),
    );
  return { TRAILER_ASK_AGAIN_MS, askForTrailer, drawn };
}

describe('the trailer row while the answer is pending', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('keeps its placeholder, then shows the trailer once the job has found it', async () => {
    stubServer([PENDING, PENDING, { key: 'vKQi3bBA1y8' }]);
    const { TRAILER_ASK_AGAIN_MS, askForTrailer, drawn } = await load();
    const got = vi.fn();
    askForTrailer(ID, got);
    await vi.advanceTimersByTimeAsync(0);
    expect(got).not.toHaveBeenCalled();
    // A row drawn now, a moment into the wait, still holds the button's place.
    expect(drawn()).toContain('cd-trailer-skel');
    await vi.advanceTimersByTimeAsync(TRAILER_ASK_AGAIN_MS[0]);
    expect(got).not.toHaveBeenCalled();
    expect(drawn()).toContain('cd-trailer-skel');
    await vi.advanceTimersByTimeAsync(TRAILER_ASK_AGAIN_MS[1]);
    expect(got).toHaveBeenCalledExactlyOnceWith('vKQi3bBA1y8');
    const html = drawn();
    expect(html).not.toContain('cd-trailer-skel');
    expect(html).toContain('aria-label="Play the trailer for The Matrix"');
  });

  it('says there is none only once the job has said so', async () => {
    stubServer([PENDING, { key: null }]);
    const { TRAILER_ASK_AGAIN_MS, askForTrailer, drawn } = await load();
    const got = vi.fn();
    askForTrailer(ID, got);
    await vi.advanceTimersByTimeAsync(0);
    expect(drawn()).toContain('cd-trailer-skel');
    await vi.advanceTimersByTimeAsync(TRAILER_ASK_AGAIN_MS[0]);
    expect(got).toHaveBeenCalledExactlyOnceWith(null);
    expect(drawn()).toContain('No trailer available');
  });

  it('stops asking when the row goes, and hands nothing to a row that is gone', async () => {
    const fetch = stubServer([]);
    const { askForTrailer } = await load();
    const got = vi.fn();
    const leave = askForTrailer(ID, got);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
    leave();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(got).not.toHaveBeenCalled();
  });

  it('asks nothing for a film already answered this visit', async () => {
    const fetch = stubServer([{ key: 'vKQi3bBA1y8' }]);
    const { askForTrailer } = await load();
    askForTrailer(ID, () => {});
    await vi.advanceTimersByTimeAsync(0);
    const got = vi.fn();
    askForTrailer(ID, got);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(got).not.toHaveBeenCalled();
  });
});
