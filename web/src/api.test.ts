import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// The real module reaches for PostHog, which has nothing to do with
// what is under test here.
vi.mock('./analytics', () => ({ analyticsHeaders: () => ({}) }));

type Answer = { status: number; body?: unknown };

function stubFetch(answers: Answer[]) {
  const fetch = vi.fn(async (_url: string, _init?: RequestInit) => {
    const next = answers.shift() ?? { status: 500 };
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

// A fresh module each time, so one test's kept answers are not the next
// test's.
async function load() {
  vi.resetModules();
  return import('./api');
}

describe('fetchTrailer', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('asks for the film by id and answers with the key', async () => {
    const fetch = stubFetch([{ status: 200, body: { key: 'vKQi3bBA1y8' } }]);
    const { fetchTrailer } = await load();
    await expect(fetchTrailer('tt0133093')).resolves.toBe('vKQi3bBA1y8');
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe('/api/trailers/tt0133093');
  });

  it('keeps the answer for the visit, one request per film', async () => {
    const fetch = stubFetch([
      { status: 200, body: { key: 'vKQi3bBA1y8' } },
      { status: 200, body: { key: 'other' } },
    ]);
    const { fetchTrailer } = await load();
    const [a, b] = await Promise.all([fetchTrailer('tt0133093'), fetchTrailer('tt0133093')]);
    expect(a).toBe('vKQi3bBA1y8');
    expect(b).toBe('vKQi3bBA1y8');
    await expect(fetchTrailer('tt0133093')).resolves.toBe('vKQi3bBA1y8');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('keeps "no trailer" too, so opening the film again asks nothing', async () => {
    const fetch = stubFetch([{ status: 200, body: { key: null } }]);
    const { fetchTrailer } = await load();
    await expect(fetchTrailer('tt0000001')).resolves.toBeNull();
    await expect(fetchTrailer('tt0000001')).resolves.toBeNull();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('resolves null on a failure and forgets it, so the next open asks again', async () => {
    const fetch = stubFetch([
      { status: 502, body: { error: 'could not look up a trailer' } },
      { status: 200, body: { key: 'vKQi3bBA1y8' } },
    ]);
    const { fetchTrailer } = await load();
    await expect(fetchTrailer('tt0133093')).resolves.toBeNull();
    await expect(fetchTrailer('tt0133093')).resolves.toBe('vKQi3bBA1y8');
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('resolves null when the network itself fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('offline'))));
    const { fetchTrailer } = await load();
    await expect(fetchTrailer('tt0133093')).resolves.toBeNull();
  });
});

describe('trailerKnown', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('knows nothing until an answer has come back', async () => {
    stubFetch([{ status: 200, body: { key: 'vKQi3bBA1y8' } }]);
    const { fetchTrailer, trailerKnown } = await load();
    expect(trailerKnown('tt0133093')).toBeUndefined();
    const asking = fetchTrailer('tt0133093');
    expect(trailerKnown('tt0133093')).toBeUndefined();
    await asking;
    expect(trailerKnown('tt0133093')).toBe('vKQi3bBA1y8');
  });

  it('knows "no trailer" as null', async () => {
    stubFetch([{ status: 200, body: { key: null } }]);
    const { fetchTrailer, trailerKnown } = await load();
    await fetchTrailer('tt0000001');
    expect(trailerKnown('tt0000001')).toBeNull();
  });

  it('keeps nothing from a failure, so the row asks again rather than saying none', async () => {
    stubFetch([{ status: 502, body: { error: 'could not look up a trailer' } }]);
    const { fetchTrailer, trailerKnown } = await load();
    await expect(fetchTrailer('tt0133093')).resolves.toBeNull();
    expect(trailerKnown('tt0133093')).toBeUndefined();
  });
});
