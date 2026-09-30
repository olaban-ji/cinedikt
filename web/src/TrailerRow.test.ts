import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ENTER_MS } from './sheet';
import { FADE_STEP_MS, usePlayer, type Player } from './TrailerRow';

// The real module reaches for PostHog, which has nothing to do with
// what is under test here.
vi.mock('./analytics', () => ({ analyticsHeaders: () => ({}), capture: () => {} }));

// The effects each render asks for. The server renderer runs none of
// them; a test that needs what it drew mounted runs them by hand (see
// mount).
const effects = vi.hoisted(() => [] as (() => void | (() => void))[]);
vi.mock('react', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react')>()),
  useEffect: (effect: () => void | (() => void)) => {
    effects.push(effect);
  },
}));

/** Runs the effects the renders since the last call asked for, as
 *  mounting what they drew would. */
function mount() {
  for (const effect of effects.splice(0)) effect();
}

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
  fade: () => {},
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

describe('the player’s volume', () => {
  const READY = '{"event":"onReady","id":1,"channel":"widget"}';
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  /** The page's player, mounted, its trailer set in the preview with its
   *  sound on, open, and in a frame that has said it is ready. `sent` is
   *  what the frame has been sent since, and empties it; `attach` puts a
   *  frame in the page again, and `ready` has it say it is ready. */
  function playing() {
    const listeners = new Map<string, (e: { data: unknown; source: unknown }) => void>();
    // The page's window: its timers, and the frame's messages.
    vi.stubGlobal(
      'window',
      Object.assign(Object.create(globalThis), {
        addEventListener: (type: string, listener: (e: { data: unknown; source: unknown }) => void) =>
          listeners.set(type, listener),
        removeEventListener: () => {},
      }),
    );
    const made: { player?: Player } = {};
    effects.length = 0;
    renderToStaticMarkup(
      createElement(function Page() {
        made.player = usePlayer();
        return null;
      }),
    );
    mount();
    const player = made.player!;
    const posted: string[] = [];
    const frame = { postMessage: (message: string) => posted.push(message) };
    const attach = () => {
      player.frame.current = { contentWindow: frame } as unknown as HTMLIFrameElement;
    };
    const ready = () => listeners.get('message')?.({ data: READY, source: frame });
    attach();
    player.start('preview', ID, false, 360);
    ready();
    vi.advanceTimersByTime(ENTER_MS);
    expect(player.now()?.open).toBe(true);
    posted.length = 0;
    const sent = () =>
      posted.splice(0).map((message) => {
        const { func, args } = JSON.parse(message) as { func: string; args: number[] };
        return [func, ...args].join(' ');
      });
    return { player, sent, attach, ready };
  }

  it('fades on a smoothstep, a step every 30ms, from full down to nothing', () => {
    const { player, sent } = playing();
    expect(FADE_STEP_MS).toBe(30);
    player.fade(0, 360);
    vi.advanceTimersByTime(29);
    expect(sent()).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(sent()).toEqual(['setVolume 98']);
    vi.advanceTimersByTime(330);
    expect(sent()).toEqual([
      'setVolume 93',
      'setVolume 84',
      'setVolume 74',
      'setVolume 62',
      'setVolume 50',
      'setVolume 38',
      'setVolume 26',
      'setVolume 16',
      'setVolume 7',
      'setVolume 2',
      'setVolume 0',
    ]);
    vi.advanceTimersByTime(1000);
    expect(sent()).toEqual([]);
  });

  it('puts a lowered volume back to full as it is dropped, muted first so that is not heard', () => {
    const { player, sent } = playing();
    player.fade(0, 360);
    vi.advanceTimersByTime(360);
    sent();
    player.drop('preview', ID);
    expect(sent()).toEqual(['mute', 'setVolume 100', 'pauseVideo']);
    expect(player.now()).toBeNull();
  });

  it('ends a fade where it is dropped, part of the way down', () => {
    const { player, sent } = playing();
    player.fade(0, 360);
    vi.advanceTimersByTime(90);
    expect(sent()).toEqual(['setVolume 98', 'setVolume 93', 'setVolume 84']);
    player.drop('preview', ID);
    expect(sent()).toEqual(['mute', 'setVolume 100', 'pauseVideo']);
    vi.advanceTimersByTime(1000);
    expect(sent()).toEqual([]);
  });

  it('drops a player still at full volume as it always has', () => {
    const { player, sent } = playing();
    player.drop('preview', ID);
    expect(sent()).toEqual(['pauseVideo']);
  });

  it('drops only the film asked for', () => {
    const { player, sent } = playing();
    player.drop('preview', 'tt0088763');
    expect(sent()).toEqual([]);
    expect(player.now()?.id).toBe(ID);
  });

  it('fades back up from wherever it had got to, and a second fade ends the first', () => {
    const { player, sent } = playing();
    player.fade(0, 360);
    vi.advanceTimersByTime(150);
    expect(sent().at(-1)).toBe('setVolume 62');
    player.fade(100, 240);
    vi.advanceTimersByTime(240);
    const up = sent().map((s) => Number(s.split(' ')[1]));
    expect(up).toHaveLength(8);
    expect(up[0]).toBeGreaterThan(62);
    for (let i = 1; i < up.length; i++) expect(up[i]).toBeGreaterThan(up[i - 1]);
    expect(up.at(-1)).toBe(100);
    vi.advanceTimersByTime(1000);
    expect(sent()).toEqual([]);
    // Back at full, there is nothing to put back.
    player.drop('preview', ID);
    expect(sent()).toEqual(['pauseVideo']);
  });

  it('ends a fade as the player stops, and plays on at full volume if it is taken back as it closes', () => {
    const { player, sent } = playing();
    player.fade(0, 360);
    vi.advanceTimersByTime(60);
    player.stop('preview');
    expect(sent()).toEqual(['setVolume 98', 'setVolume 93', 'pauseVideo']);
    vi.advanceTimersByTime(100);
    expect(sent()).toEqual([]);
    player.start('preview', ID, false, 360);
    expect(sent()).toEqual(['setVolume 100', 'unMute', 'playVideo']);
    vi.advanceTimersByTime(1000);
    expect(sent()).toEqual([]);
    // Taken back muted, it is put back to full all the same, so its sound
    // is at full once it is turned on.
    player.fade(0, 360);
    vi.advanceTimersByTime(60);
    player.stop('preview');
    sent();
    player.start('preview', ID, true, 360);
    expect(sent()).toEqual(['setVolume 100', 'mute', 'playVideo']);
  });

  it('turns the sound on at the volume the page believes it is at, which a frame taken away mid-fade never heard put back', () => {
    const { player, sent, attach, ready } = playing();
    player.fade(0, 360);
    vi.advanceTimersByTime(150);
    expect(sent().at(-1)).toBe('setVolume 62');
    // Taken away at once, by a panel opening: its frame has gone before
    // its player is dropped, so the volume put back is never heard.
    player.frame.current = null;
    player.drop('preview', ID);
    expect(sent()).toEqual([]);
    // The next trailer starts muted, in a frame of its own.
    attach();
    player.start('preview', 'tt0088763', true, 360);
    ready();
    vi.advanceTimersByTime(ENTER_MS);
    expect(sent()).toEqual(['mute']);
    player.sound();
    expect(sent()).toEqual(['setVolume 100', 'unMute', 'playVideo']);
    // Turned off again, it is simply muted.
    player.sound();
    expect(sent()).toEqual(['mute']);
  });
});
