import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { WhereToWatchBody } from './api';
import { PREVIEW_REST_MS } from './preview';
import type { WatchOffer, WatchState, WhereToWatch } from './whereToWatch';

// The real module reaches for PostHog, which has nothing to do with
// what is under test here.
vi.mock('./analytics', () => ({ analyticsHeaders: () => ({}) }));

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

/** The page's clock, which a failure is timed by, moved by hand. */
const clock = { now: 0 };
beforeEach(() => {
  clock.now = 0;
  vi.spyOn(performance, 'now').mockImplementation(() => clock.now);
});
afterEach(() => {
  vi.restoreAllMocks();
});

type Answer = { status: number; body?: unknown } | 'offline';

/** A server that answers from `answers` in turn, and with a 500 once
 *  they run out. */
function stubServer(answers: Answer[]) {
  const fetch = vi.fn(async (_url: string, _init?: RequestInit) => {
    const next = answers.shift() ?? { status: 500 };
    if (next === 'offline') throw new TypeError('offline');
    return {
      ok: next.status >= 200 && next.status < 300,
      status: next.status,
      statusText: '',
      json: async () => next.body,
    };
  });
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

const ID = 'tt0133093';

const offer = (id: string, name: string, more: Partial<WatchOffer> = {}): WatchOffer => ({
  id,
  name,
  link: `https://example.com/${id}/watch`,
  logo: { dark: `https://img.example.com/${id}-dark.svg`, light: `https://img.example.com/${id}-light.svg` },
  ...more,
});

const US: WhereToWatchBody = {
  country: 'us',
  countryName: 'United States',
  covered: true,
  stream: [offer('netflix', 'Netflix'), offer('starz', 'Starz', { via: 'Prime Video' })],
  free: [offer('tubi', 'Tubi')],
  rent: [offer('prime', 'Prime Video', { price: '3.99 USD' })],
  buy: [],
};

// A fresh module each time, so one test's kept answers are not the
// next test's.
async function load() {
  vi.resetModules();
  const mod = await import('./whereToWatch');
  /** What useWhereToWatch hands a component as it is first drawn. */
  const drawn = (id = ID) => {
    let got: WatchState | undefined;
    renderToStaticMarkup(
      createElement(function Probe() {
        got = mod.useWhereToWatch(id);
        return null;
      }),
    );
    return got!;
  };
  return { ...mod, drawn };
}

describe('asking where a movie can be watched', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    effects.length = 0;
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('asks the server by the movie’s IMDb id and names no country: the server reads it from the request', async () => {
    const fetch = stubServer([{ status: 200, body: US }]);
    const { askWhereToWatch } = await load();
    const got = await askWhereToWatch(ID);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe('/api/where-to-watch/tt0133093');
    expect(got?.country).toBe('us');
    expect(got?.countryName).toBe('United States');
    expect(got?.stream.map((o) => o.name)).toEqual(['Netflix', 'Starz']);
  });

  it('makes one request per movie, however many ask while it is out, and keeps the answer for the visit', async () => {
    const fetch = stubServer([{ status: 200, body: US }, { status: 200, body: { ...US, stream: [] } }]);
    const { askWhereToWatch, whereToWatchKnown } = await load();
    const [a, b] = await Promise.all([askWhereToWatch(ID), askWhereToWatch(ID)]);
    expect(a).toBe(b);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(whereToWatchKnown(ID)).toBe(a);
    await expect(askWhereToWatch(ID)).resolves.toBe(a);
    expect(fetch).toHaveBeenCalledTimes(1);
    // Another movie is its own request.
    await askWhereToWatch('tt0234215');
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('keeps an empty answer and an uncovered one too: each is an answer', async () => {
    const fetch = stubServer([
      { status: 200, body: { country: 'us', countryName: 'United States', covered: true, stream: [], free: [], rent: [], buy: [] } },
      { status: 200, body: { country: 'xx', covered: false } },
    ]);
    const { askWhereToWatch, whereToWatchKnown } = await load();
    await askWhereToWatch(ID);
    await askWhereToWatch('tt0234215');
    expect(whereToWatchKnown(ID)?.covered).toBe(true);
    expect(whereToWatchKnown('tt0234215')).toEqual({ country: 'xx', covered: false, stream: [], free: [], rent: [], buy: [] });
    await askWhereToWatch(ID);
    await askWhereToWatch('tt0234215');
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('forgets a failure after a moment, so a later open asks again, as the server keeps no failure either', async () => {
    const fetch = stubServer([{ status: 502, body: { error: 'streaming availability is unreachable' } }, { status: 200, body: US }]);
    const { FAILURE_HOLD_MS, askWhereToWatch, whereToWatchKnown } = await load();
    await expect(askWhereToWatch(ID)).resolves.toBeNull();
    expect(whereToWatchKnown(ID)).toBeUndefined();
    clock.now += FAILURE_HOLD_MS;
    await expect(askWhereToWatch(ID)).resolves.toMatchObject({ covered: true });
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('holds a failure for the preview opening on the rest whose ask failed, rather than asking twice in one open', async () => {
    const fetch = stubServer([{ status: 502 }, { status: 502 }]);
    const { FAILURE_HOLD_MS, askWhereToWatch } = await load();
    // The pointer comes to rest, and the ask fails at once.
    await expect(askWhereToWatch(ID)).resolves.toBeNull();
    // The preview opens on that rest, and takes the failure.
    clock.now += PREVIEW_REST_MS;
    await expect(askWhereToWatch(ID)).resolves.toBeNull();
    expect(fetch).toHaveBeenCalledTimes(1);
    // Only for the movie that failed.
    await askWhereToWatch('tt0234215');
    expect(fetch).toHaveBeenCalledTimes(2);
    clock.now += FAILURE_HOLD_MS - PREVIEW_REST_MS - 1;
    await askWhereToWatch(ID);
    expect(fetch).toHaveBeenCalledTimes(2);
    clock.now += 1;
    await askWhereToWatch(ID);
    expect(fetch).toHaveBeenCalledTimes(3);
    expect(FAILURE_HOLD_MS).toBeGreaterThan(PREVIEW_REST_MS);
  });

  it('takes the network failing, and an answer it cannot read, as failures', async () => {
    stubServer(['offline', { status: 200, body: { country: 'us', stream: [] } }, { status: 200, body: null }]);
    const { FAILURE_HOLD_MS, askWhereToWatch, whereToWatchKnown } = await load();
    await expect(askWhereToWatch(ID)).resolves.toBeNull();
    clock.now += FAILURE_HOLD_MS;
    // No word on coverage: saying there is none might be wrong.
    await expect(askWhereToWatch(ID)).resolves.toBeNull();
    clock.now += FAILURE_HOLD_MS;
    await expect(askWhereToWatch(ID)).resolves.toBeNull();
    expect(whereToWatchKnown(ID)).toBeUndefined();
  });

  it('never asks about an id the server would refuse', async () => {
    const fetch = stubServer([]);
    const { askWhereToWatch } = await load();
    for (const id of ['nm0000206', '', 'tt', 'tt12a', '../tt0133093']) {
      await expect(askWhereToWatch(id)).resolves.toBeNull();
    }
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('useWhereToWatch', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    effects.length = 0;
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('waits on a movie not yet asked about, asking only once it is mounted', async () => {
    const fetch = stubServer([{ status: 200, body: US }]);
    const { drawn } = await load();
    expect(drawn()).toEqual({ status: 'wait', data: null });
    expect(fetch).not.toHaveBeenCalled();
    mount();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('hands over the answer once it is in, and shows it from the first paint the next time', async () => {
    const fetch = stubServer([{ status: 200, body: US }]);
    const { drawn, followWhereToWatch } = await load();
    const got = vi.fn();
    followWhereToWatch(ID, got);
    await vi.waitFor(() => expect(got).toHaveBeenCalledTimes(1));
    expect(got.mock.calls[0][0]).toMatchObject({ status: 'ok', data: { covered: true } });
    // A preview or panel opened now has it at once, and asks nothing.
    const now = drawn();
    expect(now.status).toBe('ok');
    expect(now.data).toBe(got.mock.calls[0][0].data);
    mount();
    await Promise.resolve();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('shares one request between the card the pointer rests on, its preview and its panel', async () => {
    const fetch = stubServer([{ status: 200, body: US }]);
    const { askWhereToWatch, drawn, whereToWatchKnown } = await load();
    // The pointer begins to rest on the card.
    const resting = askWhereToWatch(ID);
    // Its preview opens, then its panel, both before the answer is in.
    expect(drawn().status).toBe('wait');
    expect(drawn().status).toBe('wait');
    mount();
    await resting;
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(whereToWatchKnown(ID)).toBeDefined();
  });

  it('says error on a failure, and asks again the next time the movie is shown', async () => {
    const fetch = stubServer([{ status: 502 }, { status: 200, body: US }]);
    const { FAILURE_HOLD_MS, drawn, followWhereToWatch } = await load();
    const got = vi.fn();
    followWhereToWatch(ID, got);
    await vi.waitFor(() => expect(got).toHaveBeenCalledExactlyOnceWith({ status: 'error', data: null }));
    // Opened in the moment the failure stands, it shows nothing from the
    // first paint, and asks nothing.
    expect(drawn()).toEqual({ status: 'error', data: null });
    mount();
    expect(fetch).toHaveBeenCalledTimes(1);
    // Nothing kept past that: the next one waits and asks again.
    clock.now += FAILURE_HOLD_MS;
    expect(drawn().status).toBe('wait');
    mount();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('hands nothing to a component that has gone, and keeps the answer for the next', async () => {
    stubServer([{ status: 200, body: US }]);
    const { askWhereToWatch, followWhereToWatch, whereToWatchKnown } = await load();
    const got = vi.fn();
    const leave = followWhereToWatch(ID, got);
    leave();
    await askWhereToWatch(ID);
    await Promise.resolve();
    expect(got).not.toHaveBeenCalled();
    expect(whereToWatchKnown(ID)).toBeDefined();
  });
});

describe('readAnswer', () => {
  it('takes what the server sent, groups missing as empty', async () => {
    const { readAnswer } = await load();
    expect(readAnswer({ country: 'GB', countryName: 'United Kingdom', covered: true, stream: [offer('netflix', 'Netflix')] })).toEqual({
      country: 'gb',
      countryName: 'United Kingdom',
      covered: true,
      stream: [offer('netflix', 'Netflix')],
      free: [],
      rent: [],
      buy: [],
    });
  });

  it('leaves out an offer with no name or no web address to open, and keeps each service once', async () => {
    const { readAnswer } = await load();
    const got = readAnswer({
      covered: true,
      stream: [
        offer('netflix', 'Netflix'),
        { ...offer('bad', 'Bad'), link: 'javascript:alert(1)' },
        { ...offer('none', ''), name: '' },
        { id: 'nolink', name: 'No link' },
        offer('netflix', 'Netflix again'),
        offer('max', 'Max', { logo: {} }),
      ],
    });
    expect(got.stream.map((o) => o.name)).toEqual(['Netflix', 'Max']);
    expect(got.stream[1].logo).toEqual({});
  });

  it('puts nothing in any group for a country without coverage', async () => {
    const { readAnswer } = await load();
    expect(readAnswer({ country: 'xx', covered: false, stream: [offer('netflix', 'Netflix')] })).toEqual({
      country: 'xx',
      covered: false,
      stream: [],
      free: [],
      rent: [],
      buy: [],
    });
  });
});

/** An answer for these groups, in the United States. */
const answer = (groups: Partial<Pick<WhereToWatch, 'stream' | 'free' | 'rent' | 'buy'>>): WhereToWatch => ({
  country: 'us',
  countryName: 'United States',
  covered: true,
  stream: [],
  free: [],
  rent: [],
  buy: [],
  ...groups,
});
const ok = (w: WhereToWatch): WatchState => ({ status: 'ok', data: w });

describe('the preview’s Stream row', () => {
  it('names streaming then free, each service once, the first three and a count of the rest', async () => {
    const { streamPeek } = await load();
    const w = answer({
      stream: [offer('netflix', 'Netflix'), offer('prime', 'Prime Video'), offer('max', 'Max'), offer('hulu', 'Hulu')],
      free: [offer('tubi', 'Tubi'), offer('netflix', 'Netflix')],
      rent: [offer('apple', 'Apple TV', { price: '3.99 USD' })],
    });
    const row = streamPeek(ok(w))!;
    expect(row.shown.map((o) => o.name)).toEqual(['Netflix', 'Prime Video', 'Max']);
    // Hulu and Tubi; Netflix free is Netflix already.
    expect(row.more).toBe(2);
    const free = streamPeek(ok(answer({ free: [offer('tubi', 'Tubi')] })))!;
    expect(free.shown.map((o) => o.name)).toEqual(['Tubi']);
    expect(free.more).toBe(0);
  });

  it('is not there with nothing to stream, without coverage, with no answer yet, or after a failure', async () => {
    const { streamPeek } = await load();
    expect(streamPeek(ok(answer({ rent: [offer('apple', 'Apple TV')], buy: [offer('apple', 'Apple TV')] })))).toBeNull();
    expect(streamPeek(ok(answer({})))).toBeNull();
    expect(streamPeek(ok({ ...answer({}), country: 'xx', covered: false }))).toBeNull();
    expect(streamPeek({ status: 'wait', data: null })).toBeNull();
    expect(streamPeek({ status: 'error', data: null })).toBeNull();
  });
});

describe('the panel’s rows', () => {
  it('are Stream, Free, Rent and Buy, in that order, only the ones with something', async () => {
    const { watchGroups } = await load();
    const w = answer({
      buy: [offer('apple', 'Apple TV')],
      stream: [offer('netflix', 'Netflix')],
      rent: [offer('prime', 'Prime Video')],
    });
    expect(watchGroups(w).map((g) => g.kind)).toEqual(['stream', 'rent', 'buy']);
    expect(watchGroups(answer({ free: [offer('tubi', 'Tubi')] })).map((g) => g.kind)).toEqual(['free']);
    expect(watchGroups(answer({}))).toEqual([]);
  });
});

describe('what a service’s chip says to assistive tech', () => {
  it('is the way to watch, the service, its price and that it opens a new tab', async () => {
    const { offerLabel } = await load();
    expect(offerLabel('stream', offer('netflix', 'Netflix'))).toBe('Stream on Netflix, opens in a new tab');
    expect(offerLabel('rent', offer('prime', 'Prime Video', { price: '$3.99' }))).toBe(
      'Rent on Prime Video, $3.99, opens in a new tab',
    );
    expect(offerLabel('buy', offer('apple', 'Apple TV', { price: '14.99 USD' }))).toBe(
      'Buy on Apple TV, 14.99 USD, opens in a new tab',
    );
    expect(offerLabel('free', offer('tubi', 'Tubi'))).toBe('Free on Tubi, opens in a new tab');
  });

  it('names the service an add-on comes through', async () => {
    const { offerLabel } = await load();
    expect(offerLabel('stream', offer('starz', 'Starz', { via: 'Prime Video' }))).toBe(
      'Stream on Starz via Prime Video, opens in a new tab',
    );
  });
});

describe('a service’s logo', () => {
  it('is the one drawn for the theme, else the other, else none', async () => {
    const { logoFor } = await load();
    const both = { logo: { dark: 'd.svg', light: 'l.svg' } };
    expect(logoFor(both, 'dark')).toBe('d.svg');
    expect(logoFor(both, 'light')).toBe('l.svg');
    expect(logoFor({ logo: { light: 'l.svg' } }, 'dark')).toBe('l.svg');
    expect(logoFor({ logo: { dark: 'd.svg' } }, 'light')).toBe('d.svg');
    expect(logoFor({ logo: {} }, 'dark')).toBeUndefined();
  });

  it('is remembered for the visit once it has failed to load', async () => {
    const { logoBroke, logoBroken } = await load();
    expect(logoBroken('https://img.example.com/hulu.svg')).toBe(false);
    logoBroke('https://img.example.com/hulu.svg');
    expect(logoBroken('https://img.example.com/hulu.svg')).toBe(true);
    expect(logoBroken('https://img.example.com/max.svg')).toBe(false);
  });
});

describe('the country, as the panel names it', () => {
  it('puts "the" before the names that take it', async () => {
    const { countryIn } = await load();
    for (const [name, said] of [
      ['United States', 'the United States'],
      ['United Kingdom', 'the United Kingdom'],
      ['United Arab Emirates', 'the United Arab Emirates'],
      ['Netherlands', 'the Netherlands'],
      ['Philippines', 'the Philippines'],
      ['Czech Republic', 'the Czech Republic'],
      ['Dominican Republic', 'the Dominican Republic'],
      ['Central African Republic', 'the Central African Republic'],
      ['Republic of the Congo', 'the Republic of the Congo'],
      ['Bahamas', 'the Bahamas'],
      ['Gambia', 'the Gambia'],
      ['Cayman Islands', 'the Cayman Islands'],
    ]) {
      expect(countryIn(name), name).toBe(said);
    }
  });

  it('leaves the rest as they are, and lowercases a "The" the API gives', async () => {
    const { countryIn } = await load();
    for (const name of ['France', 'Germany', 'Ireland', 'India', 'Hong Kong', 'Congo']) {
      expect(countryIn(name), name).toBe(name);
    }
    expect(countryIn('The Netherlands')).toBe('the Netherlands');
    expect(countryIn('The Gambia')).toBe('the Gambia');
  });

  it('is "your country" when the server named none', async () => {
    const { countryIn } = await load();
    expect(countryIn(undefined)).toBe('your country');
    expect(countryIn('  ')).toBe('your country');
  });

  it('is named in the sentence for a covered country with nowhere to watch the movie', async () => {
    const { nothingText, UNCOVERED_TEXT } = await load();
    expect(nothingText({ countryName: 'United States' })).toBe(
      'Not available to stream, rent or buy in the United States right now.',
    );
    expect(nothingText({ countryName: 'France' })).toBe('Not available to stream, rent or buy in France right now.');
    expect(UNCOVERED_TEXT).toBe('Streaming info isn’t available in your country yet.');
  });
});
