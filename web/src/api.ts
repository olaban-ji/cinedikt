// Thin client for the cinedikt Go API. In dev, Vite proxies /api to :8080.

import type { GridFilm, GridPayload } from './grid';
import { analyticsHeaders } from './analytics';

export type NodeKind = 'movie' | 'person';

export interface ApiNode {
  id: string; // "m:603" | "p:6384"
  type: NodeKind;
  label: string;
  tmdb_id: number;
  year?: number;
  poster?: string;
  backdrop?: string; // landscape still, for wide tiles
  rating?: number; // TMDb 0–10
  votes?: number;
  imdb_id?: string;
  imdb_rating?: number;
  imdb_votes?: number;
  /** TMDb person popularity; present on person nodes. */
  popularity?: number;
}

/** A film in a pathway, with the connecting actor's role in it. */
export interface PathwayFilm extends ApiNode {
  role: string;
  order: number;
}

/** One cast member of a movie with their most voted other films.
 *  Role is the character name, or "Director". */
export interface Pathway {
  person: ApiNode;
  role: string;
  order: number;
  films: PathwayFilm[];
}

/** The lean expansion of one stop: everything the map needs to grow from it. */
export interface Pathways {
  movie: ApiNode;
  cast: Pathway[];
}

export interface SearchHit {
  /** An IMDb title id, such as tt0133093. */
  id: string;
  title: string;
  /** Release year. Absent, or zero, when the catalog has none. */
  year?: number;
  /** Poster URL, so a result row shows the film rather than describing it. */
  poster?: string;
}

const BASE = '/api';

async function getJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  for (const [k, v] of Object.entries(analyticsHeaders())) {
    if (!headers.has(k)) headers.set(k, v);
  }
  const res = await fetch(BASE + path, { ...init, headers });
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // not JSON; keep the status line
    }
    throw new Error(message);
  }
  return (await res.json()) as T;
}

/** A stop's pathways. The API crawls the movie first if it never was, and
 *  warms the films it hands back so the next hop is ready. How many
 *  co-stars and films, the billing cutoff and the vote floor are the
 *  API's own defaults: an ordinary hop has only ever wanted one pool, so
 *  the map does not restate it. `films` is sent only when a career-wide
 *  follow needs a wider pool than that default. */
export function fetchPathways(
  movieId: string,
  opts: {
    /** Narrow the answer to one person's career, by TMDb id. */
    person?: number;
    /** Override the default film pool. Used when following one career. */
    films?: number;
    signal?: AbortSignal;
  } = {},
): Promise<Pathways> {
  const params = new URLSearchParams();
  if (opts.person) params.set('person', String(opts.person));
  if (opts.films) params.set('films', String(opts.films));
  const q = params.toString();
  return getJSON<Pathways>(`/movies/${movieId}/pathways${q ? `?${q}` : ''}`, { signal: opts.signal });
}

/** Eight films to start a map from, a different eight each time, one per
 *  era so the screen spans the century. The API answers with nothing when
 *  the graph is unreachable; the caller keeps a built-in set for that.
 *  This is the API root: the first thing a cold screen asks for. */
export async function fetchFirstRun(signal?: AbortSignal): Promise<FirstRunHit[]> {
  const res = await getJSON<{ results: FirstRunHit[] }>('/', { signal });
  return res.results ?? [];
}

/** One lookup at a time per film, shared by every card that missed.
 *  A failure is forgotten, so a later miss can ask again; a picture is
 *  kept for the visit. */
const standIns = new Map<string, Promise<string | undefined>>();

/** A replacement poster for a film whose picture just failed to load.
 *  Nothing when TMDb has no picture, or could not be asked. */
export function fetchPosterStandIn(id: string): Promise<string | undefined> {
  const existing = standIns.get(id);
  if (existing) return existing;
  const pending = getJSON<{ poster?: string }>(`/posters/${encodeURIComponent(id)}`)
    .then((body) => body.poster || undefined)
    .catch(() => undefined)
    .then((url) => {
      if (!url) standIns.delete(id);
      return url;
    });
  standIns.set(id, pending);
  return pending;
}

/** How long to wait before asking again, each time the answer comes back
 *  pending: the server has handed the film to its trailer job, which has
 *  not answered yet. About fifteen seconds in all, well past a lookup
 *  when the job is free; a film still pending after that is shown as
 *  having none this time, and asked about again the next time. */
export const TRAILER_ASK_AGAIN_MS = [1500, 3000, 5000, 6000];

interface TrailerBody {
  key?: string | null;
  pending?: boolean;
}

/** One film's lookup while it is going on, shared by every row that
 *  wants it: the preview and the panel can both be showing one film. */
interface Asking {
  answer: Promise<string | null>;
  /** The callers still waiting on it. */
  waiting: number;
  stop: AbortController;
}

/** The films being asked about right now. */
const asking = new Map<string, Asking>();

/** The answers that have come back, kept for the visit. A film opened
 *  again answers from here at once, with no request and no placeholder,
 *  and a film with no trailer keeps its null. Pending is not an answer
 *  and a failure is not one either, so neither is kept: the next open
 *  asks again. */
const trailerAnswers = new Map<string, string | null>();

/** The YouTube id of a film's trailer, or null when it has none that can
 *  play embedded, the lookup failed, or the answer was still pending
 *  when the asking stopped. Asked the first time a film's preview or
 *  panel opens.
 *
 *  A pending answer is asked again after each of TRAILER_ASK_AGAIN_MS
 *  for as long as somebody wants it. `signal` is the caller going away,
 *  a closed panel or preview: its promise settles on null at once, and
 *  once every caller with a signal has gone the asking stops. */
export function fetchTrailer(id: string, signal?: AbortSignal): Promise<string | null> {
  const known = trailerAnswers.get(id);
  if (known !== undefined) return Promise.resolve(known);
  if (signal?.aborted) return Promise.resolve(null);
  let ask = asking.get(id);
  if (!ask) {
    ask = askFor(id);
    asking.set(id, ask);
  }
  const shared = ask;
  shared.waiting++;
  if (!signal) return shared.answer;
  return new Promise((resolve) => {
    const leave = () => {
      shared.waiting--;
      if (shared.waiting === 0) {
        // Nobody is left to show it to. A caller who comes next starts
        // a lookup of its own rather than joining one that is ending.
        if (asking.get(id) === shared) asking.delete(id);
        shared.stop.abort();
      }
      resolve(null);
    };
    signal.addEventListener('abort', leave, { once: true });
    void shared.answer.then((key) => {
      signal.removeEventListener('abort', leave);
      resolve(key);
    });
  });
}

/** Asks the server about one film until it has an answer, the waits run
 *  out, or nobody wants it any more. */
function askFor(id: string): Asking {
  const stop = new AbortController();
  const ask: Asking = { answer: Promise.resolve(null), waiting: 0, stop };
  ask.answer = (async () => {
    try {
      for (let i = 0; ; i++) {
        const body = await getJSON<TrailerBody>(`/trailers/${encodeURIComponent(id)}`, {
          signal: stop.signal,
        });
        if (!body.pending) {
          const key = body.key || null;
          trailerAnswers.set(id, key);
          return key;
        }
        if (i >= TRAILER_ASK_AGAIN_MS.length || stop.signal.aborted) return null;
        await pause(TRAILER_ASK_AGAIN_MS[i], stop.signal);
      }
    } catch {
      return null;
    } finally {
      if (asking.get(id) === ask) asking.delete(id);
    }
  })();
  return ask;
}

/** Waits `ms`, or rejects as soon as `signal` aborts. */
function pause(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason);
      return;
    }
    const cancel = () => {
      clearTimeout(timer);
      reject(signal.reason);
    };
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', cancel);
      resolve();
    }, ms);
    signal.addEventListener('abort', cancel, { once: true });
  });
}

/** What fetchTrailer has already answered for a film this visit: its
 *  key, or null for none. Undefined while nothing is known, which is
 *  also so while the answer is pending and after a failure, since
 *  neither is kept. */
export function trailerKnown(id: string): string | null | undefined {
  return trailerAnswers.get(id);
}

/** How many people one photos request may name: the server's cap. */
const PHOTO_IDS_PER_ASK = 50;

/** How long to wait before asking again for the people the server says
 *  are pending. The people job answers a reader's map the way the
 *  trailer job answers a film, so the schedule is the trailer's. */
export const PHOTO_ASK_AGAIN_MS = TRAILER_ASK_AGAIN_MS;

interface PhotosBody {
  photos?: Record<string, string | null>;
  pending?: string[];
}

/** The photo answers that have come back, kept for the visit: an
 *  address, or null for none. Pending is not an answer and a failure is
 *  not one either, so neither is kept: the next call asks again. */
const photoAnswers = new Map<string, string | null>();

/** IMDb's name id, the only kind the server takes. One malformed id
 *  would have the whole request refused. */
const NCONST = /^nm\d+$/;

/** The photos of people a map's payload came without: each person's
 *  photo address, or null when they have none. A person whose answer was
 *  still pending when the asking stopped, whose request failed, or whose
 *  id is not a name id, is left out, since that is not an answer; the
 *  next call asks about them again.
 *
 *  Answers already known this visit come back without a request. The
 *  rest are asked for PHOTO_IDS_PER_ASK at a time, and the people the
 *  server says are pending are asked about again after each of
 *  PHOTO_ASK_AGAIN_MS, until none are. `signal` is the caller going
 *  away: the asking stops, and the promise settles on what is known by
 *  then.
 *
 *  The promise settles only after the last attempt, which can be some
 *  fifteen seconds away while anybody is pending, and a photo that has
 *  been found should not wait for that. So `onSome` is handed the answers
 *  as they come: the ones already known, at once, and then each
 *  response's own, found photos and nulls alike. A response that answers
 *  for nobody, everyone in it still pending, hands on nothing. */
export async function fetchPeoplePhotos(
  ids: string[],
  signal?: AbortSignal,
  onSome?: (some: Record<string, string | null>) => void,
): Promise<Record<string, string | null>> {
  const out: Record<string, string | null> = {};
  let asking: string[] = [];
  for (const id of new Set(ids)) {
    const known = photoAnswers.get(id);
    if (known !== undefined) out[id] = known;
    else if (NCONST.test(id)) asking.push(id);
  }
  const stop = signal ?? new AbortController().signal;
  if (onSome && !stop.aborted && Object.keys(out).length > 0) onSome({ ...out });
  for (let i = 0; asking.length > 0 && !stop.aborted; i++) {
    const pending: string[] = [];
    for (let at = 0; at < asking.length && !stop.aborted; at += PHOTO_IDS_PER_ASK) {
      const some = asking.slice(at, at + PHOTO_IDS_PER_ASK);
      let body: PhotosBody;
      try {
        body = await getJSON<PhotosBody>(`/people/photos?ids=${some.join(',')}`, { signal: stop });
      } catch {
        continue;
      }
      const waiting = new Set(body.pending ?? []);
      const photos = body.photos ?? {};
      const answered: Record<string, string | null> = {};
      for (const id of some) {
        if (waiting.has(id)) {
          pending.push(id);
        } else if (id in photos) {
          const photo = photos[id] || null;
          photoAnswers.set(id, photo);
          out[id] = photo;
          answered[id] = photo;
        }
      }
      // A caller that has gone is told nothing more.
      if (onSome && !stop.aborted && Object.keys(answered).length > 0) onSome(answered);
    }
    asking = pending;
    if (asking.length === 0 || i >= PHOTO_ASK_AGAIN_MS.length) break;
    try {
      await pause(PHOTO_ASK_AGAIN_MS[i], stop);
    } catch {
      break;
    }
  }
  return out;
}

/** One service a movie can be watched on, as the server sends it. */
export interface WatchOfferBody {
  id?: string;
  name?: string;
  /** The movie's page on that service. */
  link?: string;
  logo?: { dark?: string; light?: string };
  /** Only on rent and buy. */
  price?: string;
  /** Only on an add-on: the service it is bought through. */
  via?: string;
}

/** Where a movie can be watched, as the server sends it. A country
 *  without coverage comes with `covered: false` and nothing else. */
export interface WhereToWatchBody {
  country?: string;
  countryName?: string;
  covered?: boolean;
  stream?: WatchOfferBody[];
  free?: WatchOfferBody[];
  rent?: WatchOfferBody[];
  buy?: WatchOfferBody[];
}

/** Where a movie can be watched in the reader's country. The server
 *  works the country out from the request itself: nothing here names
 *  one. See whereToWatch.ts for how the answers are kept. */
export function fetchWhereToWatch(imdbId: string): Promise<WhereToWatchBody> {
  return getJSON<WhereToWatchBody>(`/where-to-watch/${encodeURIComponent(imdbId)}`);
}

export interface FirstRunHit {
  id: string;
  title: string;
  year: number;
  poster: string;
  /** What the poster averages to, as "#rrggbb". The opening screen
   *  fills a frame with it while the picture is still on its way, so a
   *  film shows its own colour before it shows itself. Absent until the
   *  server has worked it out. */
  c?: string;
}

/** A whole map: the searched film, its people, and the spine of every
 *  card. It does not depend on how the map is drawn — the unrated
 *  column is taken off the plot by the layout, not by the server — so
 *  one payload serves every setting. */
export function fetchGrid(
  movieId: string,
  q: { signal?: AbortSignal } = {},
): Promise<GridPayload> {
  return getJSON<GridPayload>(`/grid/${movieId}`, { signal: q.signal });
}

/** What the cards the reader can see actually say. The spine already
 *  told us which ids those are, so this never has to guess a window. */
export function fetchGridFilms(
  movieId: string,
  ids: string[],
  signal?: AbortSignal,
): Promise<GridFilm[]> {
  if (ids.length === 0) return Promise.resolve([]);
  return getJSON<{ films: GridFilm[] }>(
    `/grid/${movieId}/films?ids=${ids.join(',')}`,
    { signal },
  ).then((r) => r.films ?? []);
}

export async function searchMovies(
  q: string,
  signal?: AbortSignal,
): Promise<SearchHit[]> {
  const res = await getJSON<{ results: SearchHit[] }>(
    `/search/movies?q=${encodeURIComponent(q)}`,
    { signal },
  );
  return res.results;
}
