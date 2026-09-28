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

/** One lookup per film for the visit. A film with no trailer keeps its
 *  null, so opening it again asks nothing; a failure is forgotten, so
 *  the next open asks again. */
const trailers = new Map<string, Promise<string | null>>();

/** The answers that have come back, kept beside the lookups so a film
 *  opened again can show its trailer row straight away rather than a
 *  placeholder for the moment a settled promise takes to say so. */
const trailerAnswers = new Map<string, string | null>();

/** The YouTube id of a film's trailer, or null when it has none that can
 *  play embedded, or the lookup failed. Asked the first time a film's
 *  preview or panel opens. */
export function fetchTrailer(id: string): Promise<string | null> {
  const existing = trailers.get(id);
  if (existing) return existing;
  const pending = getJSON<{ key?: string | null }>(`/trailers/${encodeURIComponent(id)}`)
    .then((body) => {
      const key = body.key || null;
      trailerAnswers.set(id, key);
      return key;
    })
    .catch(() => {
      trailers.delete(id);
      return null;
    });
  trailers.set(id, pending);
  return pending;
}

/** What fetchTrailer has already answered for a film this visit: its
 *  key, or null for none. Undefined while nothing is known, which is
 *  also so after a failure, since that is not kept. */
export function trailerKnown(id: string): string | null | undefined {
  return trailerAnswers.get(id);
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
