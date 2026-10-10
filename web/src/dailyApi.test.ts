import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

type Answer = { status: number; body?: unknown };

/** A fetch that answers from a list, and remembers what it was asked. */
function stubFetch(answers: Answer[]) {
  const fetch = vi.fn(async (_url: string, _init?: RequestInit) => {
    const next = answers.shift() ?? { status: 500 };
    return {
      ok: next.status >= 200 && next.status < 300,
      status: next.status,
      statusText: '',
      json: async () => {
        if (next.body === undefined) throw new SyntaxError('no body');
        return next.body;
      },
    };
  });
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

async function load() {
  vi.resetModules();
  return import('./api');
}

/** The browser's own idea of where the reader is: a zone it names, an
 *  answer with no zone in it, or an Intl that throws when asked. */
function stubZone(zone: string | undefined | Error) {
  return vi.spyOn(Intl, 'DateTimeFormat').mockImplementation((() => ({
    resolvedOptions: () => {
      if (zone instanceof Error) throw zone;
      return { timeZone: zone };
    },
  })) as unknown as typeof Intl.DateTimeFormat);
}

/** New York, written so an address can carry it. */
const NY = 'tz=America%2FNew_York';

describe('the Daily’s requests', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    stubZone('America/New_York');
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('ask for today’s puzzle without writing anything', async () => {
    const fetch = stubFetch([{ status: 200, body: { no: 142 } }]);
    const { fetchDaily } = await load();
    await expect(fetchDaily()).resolves.toEqual({ no: 142 });
    expect(fetch.mock.calls[0][0]).toBe(`/api/daily?${NY}`);
    expect(fetch.mock.calls[0][1]?.method).toBeUndefined();
  });

  it('send every write as JSON, said to be JSON', async () => {
    const fetch = stubFetch([{ status: 200, body: { name: 'Neo Kimble', saved: false } }]);
    const { renameDaily } = await load();
    await expect(renameDaily('Trinity Kimble')).resolves.toEqual({ name: 'Neo Kimble', saved: false });
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe(`/api/daily/name?${NY}`);
    expect(init?.method).toBe('POST');
    expect(init?.headers).toEqual({ 'Content-Type': 'application/json' });
  });

  it('ask for a new name with the one on the screen, so the server never offers it again', async () => {
    const fetch = stubFetch([{ status: 200, body: { name: 'Neo Kimble', saved: false } }]);
    const { renameDaily } = await load();
    await renameDaily('Trinity Kimble');
    expect(fetch.mock.calls[0][1]?.body).toBe('{"name":"Trinity Kimble"}');
  });

  it('start a game under the name the reader was shown', async () => {
    const fetch = stubFetch([{ status: 200, body: { game: {}, player: { name: 'Neo Kimble', saved: true } } }]);
    const { playDaily } = await load();
    await playDaily(142, 'Neo Kimble');
    expect(fetch.mock.calls[0][0]).toBe(`/api/daily/142/play?${NY}`);
    expect(JSON.parse(String(fetch.mock.calls[0][1]?.body))).toEqual({ name: 'Neo Kimble' });
  });

  it('send each move to its own address with its key and the moves seen', async () => {
    const fetch = stubFetch(Array.from({ length: 4 }, (_, i) => ({ status: 200, body: { game: i } })));
    const { buyFact, guessMovie, nextName, revealAnswer } = await load();
    await nextName(143, 'k-1', 1);
    await buyFact(143, 'decade', 'k-2', 2);
    await guessMovie(143, 'tt0139809', 'k-3', 3);
    await revealAnswer(143, 'k-4', 4);
    const sent = fetch.mock.calls.map(([url, init]) => [url, init?.method, JSON.parse(String(init?.body))]);
    expect(sent).toEqual([
      [`/api/daily/143/next?${NY}`, 'POST', { key: 'k-1', seq: 1 }],
      [`/api/daily/143/buy?${NY}`, 'POST', { key: 'k-2', seq: 2, kind: 'decade' }],
      [`/api/daily/143/guess?${NY}`, 'POST', { key: 'k-3', seq: 3, film: 'tt0139809' }],
      [`/api/daily/143/reveal?${NY}`, 'POST', { key: 'k-4', seq: 4 }],
    ]);
  });

  it('have no way to combine two names on the Movies sheet: overlaps are gone', async () => {
    const api = (await load()) as Record<string, unknown>;
    expect(api.buyOverlap).toBeUndefined();
  });

  it('send a move held as a value through its own call, the same request either way', async () => {
    const fetch = stubFetch(Array.from({ length: 4 }, () => ({ status: 200, body: { game: {} } })));
    const { sendDailyMove } = await load();
    await sendDailyMove(143, { kind: 'next' }, 'k-1', 1);
    await sendDailyMove(143, { kind: 'buy', fact: 'years' }, 'k-2', 2);
    await sendDailyMove(143, { kind: 'guess', film: 'tt0111257' }, 'k-3', 3);
    await sendDailyMove(143, { kind: 'reveal' }, 'k-4', 4);
    const sent = fetch.mock.calls.map(([url, init]) => [url, JSON.parse(String(init?.body))]);
    expect(sent).toEqual([
      [`/api/daily/143/next?${NY}`, { key: 'k-1', seq: 1 }],
      [`/api/daily/143/buy?${NY}`, { key: 'k-2', seq: 2, kind: 'years' }],
      [`/api/daily/143/guess?${NY}`, { key: 'k-3', seq: 3, film: 'tt0111257' }],
      [`/api/daily/143/reveal?${NY}`, { key: 'k-4', seq: 4 }],
    ]);
  });

  it('ask for a showing person’s movies without writing anything, the person written so the address carries it', async () => {
    // One readable movie and one blank, as the server sends them: a blank
    // card is its year and its place on the rating axis, and no more.
    const body = {
      person: 'nm0001592',
      total: 2,
      movies: [{ id: 'tt0106977', title: 'The Fugitive', year: 1993, rating: 7.8, genres: ['Action'] }, { year: 1985, at: 7.5 }],
    };
    const fetch = stubFetch([{ status: 200, body }]);
    const { fetchDailyMovies } = await load();
    await expect(fetchDailyMovies(143, 'nm0001592')).resolves.toEqual(body);
    expect(fetch.mock.calls[0][0]).toBe(`/api/daily/143/movies?person=nm0001592&${NY}`);
    expect(fetch.mock.calls[0][1]?.method).toBeUndefined();
  });

  it('ask for the reader’s standing without writing anything', async () => {
    const fetch = stubFetch([{ status: 200, body: { streak: 3, week: { rank: 1204, players: 83500 } } }]);
    const { fetchDailyMe } = await load();
    await expect(fetchDailyMe()).resolves.toEqual({ streak: 3, week: { rank: 1204, players: 83500 } });
    expect(fetch.mock.calls[0][0]).toBe(`/api/daily/me?${NY}`);
    expect(fetch.mock.calls[0][1]?.method).toBeUndefined();
  });

  it('ask for the reader’s place alone, in their zone, and take one that cannot be had as none', async () => {
    const fetch = stubFetch([
      { status: 200, body: { streak: 3, week: { rank: 1204, players: 83500 } } },
      { status: 200, body: { streak: 0, week: null } },
      { status: 503, body: { error: 'down', reason: 'unavailable' } },
    ]);
    const { fetchDailyWeek } = await load();
    await expect(fetchDailyWeek()).resolves.toEqual({ rank: 1204, players: 83500 });
    await expect(fetchDailyWeek()).resolves.toBeNull();
    await expect(fetchDailyWeek()).resolves.toBeNull();
    expect(fetch.mock.calls.map(([url]) => url)).toEqual(Array(3).fill(`/api/daily/me?${NY}`));
    // Nor does a request that never got an answer take anything away.
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))));
    await expect(fetchDailyWeek()).resolves.toBeNull();
  });

  it('start the reader again, in development, with an empty JSON write in their zone, and take the 204 as done', async () => {
    // A 204 has nothing in it to read: reading it as JSON would throw.
    const fetch = stubFetch([{ status: 204 }]);
    const { resetDaily } = await load();
    await expect(resetDaily()).resolves.toBeUndefined();
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe(`/api/daily/dev/reset?${NY}`);
    expect(init?.method).toBe('POST');
    expect(init?.headers).toEqual({ 'Content-Type': 'application/json' });
    expect(init?.body).toBe('{}');
  });

  it('carry a reset the server cannot make as a refusal, with its reason', async () => {
    stubFetch([{ status: 503, body: { error: 'no other movie fits', reason: 'unavailable' } }]);
    const { ApiError, resetDaily } = await load();
    const err = await resetDaily().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 503, reason: 'unavailable' });
  });

  it('ask for a leaderboard by puzzle and tab', async () => {
    const fetch = stubFetch([{ status: 200, body: { tab: 'week' } }]);
    const { fetchDailyBoard } = await load();
    await fetchDailyBoard(142, 'week');
    expect(fetch.mock.calls[0][0]).toBe(`/api/daily/142/board?tab=week&${NY}`);
  });
});

describe('the reader’s time zone', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  /** Every Daily request there is, once each, and the addresses asked. */
  async function everyRequest(): Promise<string[]> {
    const fetch = stubFetch([...Array.from({ length: 10 }, () => ({ status: 200, body: {} })), { status: 204 }]);
    const api = await load();
    await api.fetchDaily();
    await api.fetchDailyMe();
    await api.renameDaily('Trinity Kimble');
    await api.playDaily(142, 'Trinity Kimble');
    await api.nextName(142, 'k-1', 1);
    await api.buyFact(142, 'genre', 'k-2', 2);
    await api.guessMovie(142, 'tt0133093', 'k-3', 3);
    await api.revealAnswer(142, 'k-4', 4);
    await api.fetchDailyMovies(142, 'nm0000401');
    await api.fetchDailyBoard(142, 'today');
    await api.resetDaily();
    return fetch.mock.calls.map(([url]) => url);
  }

  it('goes with every Daily request, reads and writes alike, so the server knows the reader’s date', async () => {
    stubZone('Asia/Tokyo');
    const urls = await everyRequest();
    expect(urls).toHaveLength(11);
    for (const url of urls) {
      expect(new URL(url, 'https://cinedikt.test').searchParams.get('tz')).toBe('Asia/Tokyo');
    }
    // The movies keep their person, and the board its tab: the zone is
    // added to what is there.
    expect(urls[8]).toBe('/api/daily/142/movies?person=nm0000401&tz=Asia%2FTokyo');
    expect(urls[9]).toBe('/api/daily/142/board?tab=today&tz=Asia%2FTokyo');
  });

  it('is written so the address carries it whole', async () => {
    // A "+" left bare in a query is a space by the time the server reads
    // it, and "Etc/GMT 5" is no zone at all.
    stubZone('Etc/GMT+5');
    const fetch = stubFetch([{ status: 200, body: {} }]);
    const { fetchDaily } = await load();
    await fetchDaily();
    expect(fetch.mock.calls[0][0]).toBe('/api/daily?tz=Etc%2FGMT%2B5');
    expect(new URL(fetch.mock.calls[0][0], 'https://cinedikt.test').searchParams.get('tz')).toBe('Etc/GMT+5');
  });

  it('is left off when the browser cannot say, and the server keeps to UTC', async () => {
    for (const zone of [new RangeError('no time zone data'), undefined, '']) {
      vi.restoreAllMocks();
      stubZone(zone);
      const urls = await everyRequest();
      expect(urls).toEqual([
        '/api/daily',
        '/api/daily/me',
        '/api/daily/name',
        '/api/daily/142/play',
        '/api/daily/142/next',
        '/api/daily/142/buy',
        '/api/daily/142/guess',
        '/api/daily/142/reveal',
        '/api/daily/142/movies?person=nm0000401',
        '/api/daily/142/board?tab=today',
        '/api/daily/dev/reset',
      ]);
    }
  });

  it('is left off when Intl itself throws, or is not there at all', async () => {
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(() => {
      throw new TypeError('Intl is broken');
    });
    const { readerZone } = await load();
    expect(readerZone()).toBeNull();
    vi.restoreAllMocks();
    vi.stubGlobal('Intl', undefined);
    expect(readerZone()).toBeNull();
  });

  it('is the zone the browser names, read afresh each time', async () => {
    const { readerZone } = await load();
    stubZone('Pacific/Kiritimati');
    expect(readerZone()).toBe('Pacific/Kiritimati');
    vi.restoreAllMocks();
    stubZone('America/Los_Angeles');
    expect(readerZone()).toBe('America/Los_Angeles');
  });
});

describe('a refusal', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('carries the server’s reason and its whole answer', async () => {
    const game = { seq: 4, log: [] };
    stubFetch([{ status: 409, body: { error: 'seq 3 is behind 4', reason: 'stale', game } }]);
    const { ApiError, nextName } = await load();
    const err = await nextName(142, 'k', 3).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toBeInstanceOf(Error);
    expect(err).toMatchObject({ status: 409, reason: 'stale', message: 'seq 3 is behind 4', body: { game } });
  });

  it('keeps the status line when the answer is not JSON', async () => {
    stubFetch([{ status: 503 }]);
    const { ApiError, fetchDaily } = await load();
    const err = await fetchDaily().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 503, reason: null, message: '503 ' });
  });

  it('is not what a request that never got an answer throws', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch');
      }),
    );
    const { ApiError, fetchDaily } = await load();
    const err = await fetchDaily().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TypeError);
    expect(err).not.toBeInstanceOf(ApiError);
  });
});
