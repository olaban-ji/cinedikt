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

// The server hands a film nobody has looked up to its trailer job and
// says the answer is pending. The client asks again, on a schedule,
// while somebody still wants the answer.
describe('fetchTrailer while the answer is pending', () => {
  const PENDING: Answer = { status: 200, body: { key: null, pending: true } };

  beforeEach(() => {
    vi.unstubAllGlobals();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('asks again after each wait until there is an answer, and keeps that', async () => {
    const fetch = stubFetch([PENDING, PENDING, { status: 200, body: { key: 'vKQi3bBA1y8' } }]);
    const { fetchTrailer, trailerKnown, TRAILER_ASK_AGAIN_MS } = await load();
    const asking = fetchTrailer('tt0133093');
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(TRAILER_ASK_AGAIN_MS[0] - 1);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(trailerKnown('tt0133093')).toBeUndefined();
    await vi.advanceTimersByTimeAsync(1);
    expect(fetch).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(TRAILER_ASK_AGAIN_MS[1]);
    await expect(asking).resolves.toBe('vKQi3bBA1y8');
    expect(fetch).toHaveBeenCalledTimes(3);
    expect(trailerKnown('tt0133093')).toBe('vKQi3bBA1y8');
    await expect(fetchTrailer('tt0133093')).resolves.toBe('vKQi3bBA1y8');
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it('waits about 1.5, 3, 5 and 6 seconds, about fifteen in all', async () => {
    const { TRAILER_ASK_AGAIN_MS } = await load();
    expect(TRAILER_ASK_AGAIN_MS).toEqual([1500, 3000, 5000, 6000]);
  });

  it('keeps "no trailer" once the job has said so', async () => {
    const fetch = stubFetch([PENDING, { status: 200, body: { key: null } }]);
    const { fetchTrailer, trailerKnown } = await load();
    const asking = fetchTrailer('tt0000001');
    await vi.advanceTimersByTimeAsync(1500);
    await expect(asking).resolves.toBeNull();
    expect(trailerKnown('tt0000001')).toBeNull();
    await expect(fetchTrailer('tt0000001')).resolves.toBeNull();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('gives up with null once the waits run out, and keeps nothing, so the next open asks again', async () => {
    const fetch = stubFetch([PENDING, PENDING, PENDING, PENDING, PENDING, PENDING]);
    const { fetchTrailer, trailerKnown } = await load();
    const asking = fetchTrailer('tt0133093');
    await vi.advanceTimersByTimeAsync(15_500);
    await expect(asking).resolves.toBeNull();
    // The first ask and one after each of the four waits.
    expect(fetch).toHaveBeenCalledTimes(5);
    expect(trailerKnown('tt0133093')).toBeUndefined();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(5);
    void fetchTrailer('tt0133093');
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(6);
  });

  it('stops asking when the panel or preview goes, and keeps nothing', async () => {
    const fetch = stubFetch([PENDING, PENDING, PENDING, PENDING, PENDING, PENDING]);
    const { fetchTrailer, trailerKnown } = await load();
    const gone = new AbortController();
    const asking = fetchTrailer('tt0133093', gone.signal);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
    // The request carries the lookup's own signal, so leaving can cancel it.
    expect(fetch.mock.calls[0][1]?.signal).toBeInstanceOf(AbortSignal);
    gone.abort();
    await expect(asking).resolves.toBeNull();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(trailerKnown('tt0133093')).toBeUndefined();
    // Opening the film again asks again.
    void fetchTrailer('tt0133093');
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('keeps asking for as long as one of the rows showing the film is still there', async () => {
    const fetch = stubFetch([PENDING, { status: 200, body: { key: 'vKQi3bBA1y8' } }]);
    const { fetchTrailer } = await load();
    const preview = new AbortController();
    const panel = new AbortController();
    const fromPreview = fetchTrailer('tt0133093', preview.signal);
    const fromPanel = fetchTrailer('tt0133093', panel.signal);
    await vi.advanceTimersByTimeAsync(0);
    preview.abort();
    await expect(fromPreview).resolves.toBeNull();
    await vi.advanceTimersByTimeAsync(1500);
    await expect(fromPanel).resolves.toBe('vKQi3bBA1y8');
    // One lookup between them.
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('asks nothing for a caller that has already gone', async () => {
    const fetch = stubFetch([PENDING]);
    const { fetchTrailer } = await load();
    const gone = new AbortController();
    gone.abort();
    await expect(fetchTrailer('tt0133093', gone.signal)).resolves.toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('keeps nothing from a failure on a second ask', async () => {
    stubFetch([PENDING, { status: 500, body: { error: 'could not read the trailer' } }]);
    const { fetchTrailer, trailerKnown } = await load();
    const asking = fetchTrailer('tt0133093');
    await vi.advanceTimersByTimeAsync(1500);
    await expect(asking).resolves.toBeNull();
    expect(trailerKnown('tt0133093')).toBeUndefined();
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

// The server's people job answers the people on a map the way the
// trailer job answers a film: a person it has not reached is pending,
// and the client asks again, on the same schedule, while the caller
// still wants the answer.
describe('fetchPeoplePhotos', () => {
  const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
  const MOSS = 'https://image.tmdb.org/t/p/w185/moss.jpg';

  beforeEach(() => {
    vi.unstubAllGlobals();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('asks for the people by id and answers with each photo, or null for none', async () => {
    const fetch = stubFetch([
      { status: 200, body: { photos: { nm0000206: KEANU, nm0000401: null }, pending: [] } },
    ]);
    const { fetchPeoplePhotos } = await load();
    await expect(fetchPeoplePhotos(['nm0000206', 'nm0000401'])).resolves.toEqual({
      nm0000206: KEANU,
      nm0000401: null,
    });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe('/api/people/photos?ids=nm0000206,nm0000401');
  });

  it('keeps the answers for the visit, "none" included, and asks only about the rest', async () => {
    const fetch = stubFetch([
      { status: 200, body: { photos: { nm0000206: KEANU, nm0000401: null }, pending: [] } },
      { status: 200, body: { photos: { nm0915989: null }, pending: [] } },
    ]);
    const { fetchPeoplePhotos } = await load();
    await fetchPeoplePhotos(['nm0000206', 'nm0000401']);
    await expect(fetchPeoplePhotos(['nm0000206', 'nm0000401', 'nm0915989'])).resolves.toEqual({
      nm0000206: KEANU,
      nm0000401: null,
      nm0915989: null,
    });
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[1][0]).toBe('/api/people/photos?ids=nm0915989');
    await fetchPeoplePhotos(['nm0000206', 'nm0000401', 'nm0915989']);
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('asks again about the pending people only, after each wait, until none are pending', async () => {
    const fetch = stubFetch([
      { status: 200, body: { photos: { nm0000206: KEANU, nm0000401: null, nm0905154: null }, pending: ['nm0000401', 'nm0905154'] } },
      { status: 200, body: { photos: { nm0000401: MOSS, nm0905154: null }, pending: ['nm0905154'] } },
      { status: 200, body: { photos: { nm0905154: null }, pending: [] } },
    ]);
    const { fetchPeoplePhotos, PHOTO_ASK_AGAIN_MS } = await load();
    const asking = fetchPeoplePhotos(['nm0000206', 'nm0000401', 'nm0905154']);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(PHOTO_ASK_AGAIN_MS[0] - 1);
    expect(fetch).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[1][0]).toBe('/api/people/photos?ids=nm0000401,nm0905154');
    await vi.advanceTimersByTimeAsync(PHOTO_ASK_AGAIN_MS[1]);
    expect(fetch.mock.calls[2][0]).toBe('/api/people/photos?ids=nm0905154');
    await expect(asking).resolves.toEqual({ nm0000206: KEANU, nm0000401: MOSS, nm0905154: null });
    // Every one of them is known now.
    await fetchPeoplePhotos(['nm0000206', 'nm0000401', 'nm0905154']);
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it('waits about 1.5, 3, 5 and 6 seconds, the way a trailer does', async () => {
    const { PHOTO_ASK_AGAIN_MS, TRAILER_ASK_AGAIN_MS } = await load();
    expect(PHOTO_ASK_AGAIN_MS).toEqual([1500, 3000, 5000, 6000]);
    expect(PHOTO_ASK_AGAIN_MS).toEqual(TRAILER_ASK_AGAIN_MS);
  });

  it('gives up once the waits run out, leaves the pending out, and keeps nothing of them', async () => {
    const PENDING: Answer = { status: 200, body: { photos: { nm0905154: null }, pending: ['nm0905154'] } };
    const fetch = stubFetch([
      { status: 200, body: { photos: { nm0000206: KEANU, nm0905154: null }, pending: ['nm0905154'] } },
      PENDING, PENDING, PENDING, PENDING, PENDING,
    ]);
    const { fetchPeoplePhotos } = await load();
    const asking = fetchPeoplePhotos(['nm0000206', 'nm0905154']);
    await vi.advanceTimersByTimeAsync(15_500);
    // Pending is not an answer: the person is left out, not null.
    await expect(asking).resolves.toEqual({ nm0000206: KEANU });
    // The first ask and one after each of the four waits.
    expect(fetch).toHaveBeenCalledTimes(5);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(5);
    void fetchPeoplePhotos(['nm0000206', 'nm0905154']);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(6);
    expect(fetch.mock.calls[5][0]).toBe('/api/people/photos?ids=nm0905154');
  });

  it('stops asking when the caller goes, settling on what it knows, and keeps nothing pending', async () => {
    const fetch = stubFetch([
      { status: 200, body: { photos: { nm0000206: KEANU, nm0905154: null }, pending: ['nm0905154'] } },
      { status: 200, body: { photos: { nm0905154: null }, pending: ['nm0905154'] } },
    ]);
    const { fetchPeoplePhotos } = await load();
    const gone = new AbortController();
    const asking = fetchPeoplePhotos(['nm0000206', 'nm0905154'], gone.signal);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][1]?.signal).toBe(gone.signal);
    gone.abort();
    await expect(asking).resolves.toEqual({ nm0000206: KEANU });
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(1);
    // Opening the map again asks again about the one still pending.
    void fetchPeoplePhotos(['nm0000206', 'nm0905154']);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('asks nothing for a caller that has already gone', async () => {
    const fetch = stubFetch([]);
    const { fetchPeoplePhotos } = await load();
    const gone = new AbortController();
    gone.abort();
    await expect(fetchPeoplePhotos(['nm0000206'], gone.signal)).resolves.toEqual({});
    expect(fetch).not.toHaveBeenCalled();
  });

  it('keeps nothing from a failure, so the next call asks again', async () => {
    const fetch = stubFetch([
      { status: 500, body: { error: 'could not read the photos' } },
      { status: 200, body: { photos: { nm0000206: KEANU }, pending: [] } },
    ]);
    const { fetchPeoplePhotos } = await load();
    await expect(fetchPeoplePhotos(['nm0000206'])).resolves.toEqual({});
    await expect(fetchPeoplePhotos(['nm0000206'])).resolves.toEqual({ nm0000206: KEANU });
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  // A found photo should not wait some fifteen seconds for a person who
  // is still pending, so the answers are handed on as they come.
  describe('handing on answers as they come', () => {
    const LANA = 'https://image.tmdb.org/t/p/w185/lana.jpg';

    it('hands on the known answers at once, then each response’s own, and settles on the same whole', async () => {
      const fetch = stubFetch([
        { status: 200, body: { photos: { nm0000206: KEANU, nm0000401: null }, pending: [] } },
        { status: 200, body: { photos: { nm0915989: null, nm0005251: MOSS, nm0905154: null }, pending: ['nm0905154'] } },
        { status: 200, body: { photos: { nm0905154: LANA }, pending: [] } },
      ]);
      const { fetchPeoplePhotos, PHOTO_ASK_AGAIN_MS } = await load();
      await fetchPeoplePhotos(['nm0000206', 'nm0000401']);
      const got: Record<string, string | null>[] = [];
      const asked: number[] = [];
      const ids = ['nm0000206', 'nm0000401', 'nm0915989', 'nm0005251', 'nm0905154'];
      const asking = fetchPeoplePhotos(ids, undefined, (some) => {
        got.push(some);
        asked.push(fetch.mock.calls.length);
      });
      // Before this call's request goes out: "none" is an answer too.
      expect(got).toEqual([{ nm0000206: KEANU, nm0000401: null }]);
      expect(asked).toEqual([1]);
      await vi.advanceTimersByTimeAsync(0);
      // The first response's answers, without the person still pending.
      expect(got).toEqual([
        { nm0000206: KEANU, nm0000401: null },
        { nm0915989: null, nm0005251: MOSS },
      ]);
      await vi.advanceTimersByTimeAsync(PHOTO_ASK_AGAIN_MS[0]);
      expect(got).toHaveLength(3);
      expect(got[2]).toEqual({ nm0905154: LANA });
      const whole = { nm0000206: KEANU, nm0000401: null, nm0915989: null, nm0005251: MOSS, nm0905154: LANA };
      await expect(asking).resolves.toEqual(whole);
      // The same whole as a call that is handed nothing along the way.
      await expect(fetchPeoplePhotos(ids)).resolves.toEqual(whole);
    });

    it('says nothing at once when nothing is known, nor for a response that answers for nobody', async () => {
      const fetch = stubFetch([
        { status: 200, body: { photos: { nm0905154: null }, pending: ['nm0905154'] } },
        { status: 200, body: { photos: { nm0905154: LANA }, pending: [] } },
      ]);
      const { fetchPeoplePhotos, PHOTO_ASK_AGAIN_MS } = await load();
      const got: Record<string, string | null>[] = [];
      const asking = fetchPeoplePhotos(['nm0905154'], undefined, (some) => got.push(some));
      expect(got).toEqual([]);
      await vi.advanceTimersByTimeAsync(0);
      expect(fetch).toHaveBeenCalledTimes(1);
      // Everyone in it was still pending.
      expect(got).toEqual([]);
      await vi.advanceTimersByTimeAsync(PHOTO_ASK_AGAIN_MS[0]);
      expect(got).toEqual([{ nm0905154: LANA }]);
      await expect(asking).resolves.toEqual({ nm0905154: LANA });
    });

    it('tells a caller that has gone nothing more', async () => {
      const fetch = stubFetch([
        { status: 200, body: { photos: { nm0000206: KEANU, nm0905154: null }, pending: ['nm0905154'] } },
        { status: 200, body: { photos: { nm0905154: LANA }, pending: [] } },
      ]);
      const { fetchPeoplePhotos, PHOTO_ASK_AGAIN_MS } = await load();
      const gone = new AbortController();
      const got: Record<string, string | null>[] = [];
      const asking = fetchPeoplePhotos(['nm0000206', 'nm0905154'], gone.signal, (some) => got.push(some));
      await vi.advanceTimersByTimeAsync(0);
      expect(got).toEqual([{ nm0000206: KEANU }]);
      gone.abort();
      await vi.advanceTimersByTimeAsync(PHOTO_ASK_AGAIN_MS[0] * 10);
      expect(fetch).toHaveBeenCalledTimes(1);
      expect(got).toEqual([{ nm0000206: KEANU }]);
      await expect(asking).resolves.toEqual({ nm0000206: KEANU });
      // Nor one that had gone before it asked, known answers or not.
      const gotLate: Record<string, string | null>[] = [];
      await fetchPeoplePhotos(['nm0000206'], gone.signal, (some) => gotLate.push(some));
      expect(gotLate).toEqual([]);
    });
  });

  it('asks at most fifty at a time, each person once, and never sends an id the server would refuse', async () => {
    const ids = Array.from({ length: 60 }, (_, i) => `nm${String(i + 1).padStart(7, '0')}`);
    const answer = (some: string[]): Answer => ({
      status: 200,
      body: { photos: Object.fromEntries(some.map((id) => [id, null])), pending: [] },
    });
    const fetch = stubFetch([answer(ids.slice(0, 50)), answer(ids.slice(50))]);
    const { fetchPeoplePhotos } = await load();
    const got = await fetchPeoplePhotos([...ids, ids[0], 'tt0133093', 'nope']);
    expect(Object.keys(got)).toHaveLength(60);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch.mock.calls[0][0]).toBe(`/api/people/photos?ids=${ids.slice(0, 50).join(',')}`);
    expect(fetch.mock.calls[1][0]).toBe(`/api/people/photos?ids=${ids.slice(50).join(',')}`);
  });
});

// A map's own payload carries the photo of everybody the people job has
// one for, and nothing for anybody else.
describe('fetchGrid and people photos', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("hands each person's photo through as the server sent it", async () => {
    const keanu = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
    stubFetch([
      {
        status: 200,
        body: {
          anchor: { id: 'tt0133093', title: 'The Matrix', year: 1999, rating: 8.7, md: 331, people: [], isAnchor: true },
          people: [
            { id: 'nm0000206', name: 'Keanu Reeves', role: 'cast', character: 'Neo', order: 0, photo: keanu },
            { id: 'nm0000401', name: 'Carrie-Anne Moss', role: 'cast', character: 'Trinity', order: 1 },
          ],
          films: [],
        },
      },
    ]);
    const { fetchGrid } = await load();
    const grid = await fetchGrid('tt0133093');
    expect(grid.people[0].photo).toBe(keanu);
    expect(grid.people[1].photo).toBeUndefined();
    expect('photo' in grid.people[1]).toBe(false);
  });
});
