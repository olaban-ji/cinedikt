// Thin client for the cinedikt Go API. In dev, Vite proxies /api to :8080.

import type { GridFilm, GridPayload } from './grid';
import { isImdbId } from './movieParam';

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

/** A request the server answered with a refusal. The message is the
 *  server's sentence, written for us and never shown; `reason` is the
 *  short code a page maps to its own words, and `body` the whole answer,
 *  for the refusals that carry more than a reason (the Daily's "stale"
 *  hands back the game as it stands). A request that never got an
 *  answer at all — the network, an abort — is not one of these. */
export class ApiError extends Error {
  readonly status: number;
  readonly reason: string | null;
  readonly body: unknown;

  constructor(message: string, status: number, reason: string | null, body: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.reason = reason;
    this.body = body;
  }
}

/** The refusal in a response that is not ok, read once for every kind
 *  of request. */
async function refusal(res: Response): Promise<ApiError> {
  let message = `${res.status} ${res.statusText}`;
  let reason: string | null = null;
  let body: unknown = null;
  try {
    body = await res.json();
    const said = (body ?? {}) as { error?: unknown; reason?: unknown };
    if (typeof said.error === 'string' && said.error) message = said.error;
    if (typeof said.reason === 'string' && said.reason) reason = said.reason;
  } catch {
    // not JSON; keep the status line
  }
  return new ApiError(message, res.status, reason, body);
}

async function getJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, init);
  if (!res.ok) throw await refusal(res);
  // No Content is an answer with nothing in it to read, and reading one
  // throws: the Daily's development-only reset says it has done what it
  // was asked, and no more.
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

/** A write. Always JSON, and always said to be: the server refuses a
 *  write that does not say so, which is half of what keeps another site
 *  from making one in the reader's name (the cookie's SameSite is the
 *  other half). Answers and refusals are read exactly as getJSON reads
 *  them. */
export function postJSON<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  return getJSON<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
}

/** Eight films to start a map from, a different eight each time, one per
 *  era so the screen spans the century. This is the API root: the first
 *  thing a cold screen asks for. */
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
    // One malformed id would have the whole request refused.
    else if (isImdbId(id, 'nm')) asking.push(id);
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

// ---- the Daily ----
//
// One hidden movie a day, played against the server: the page sends what
// the reader does and draws the game the server sends back. Nothing here
// ever holds the answer before the game is over — the server does not
// send it, nor a hidden name, nor a fact nobody has paid for — so these
// shapes are what the page has to work with, and no more.

/** One of the people on today's movie, once the reader may see them: a
 *  cast member whose slot is showing, a director bought as a fact, or
 *  anyone at all once the game is over. */
export interface DailyPerson {
  /** An IMDb name id, such as nm0000206. */
  id: string;
  name: string;
  /** 0–359, from their place on the movie: the cast in billing order, the
   *  star first, then the directors. The page makes the colour from it
   *  (daily.ts's hueColour), the same for everyone playing, and it never
   *  changes during a game. */
  hue: number;
  /** TMDb's 185px photo, when there is one to show. */
  photo?: string;
}

/** Another movie a cast member is in, with none of the other five on it:
 *  the "Also in" line under their name. */
export interface DailyAlso {
  id: string;
  title: string;
  /** 0 when the catalog has none. */
  year: number;
}

/** One of the six names, in reveal order: slot 0 is the sixth-billed, who
 *  shows from the start, and slot 5 the star. A hidden slot is its number
 *  and nothing else, so no name can be read off the page early. */
export type DailySlot =
  | { slot: number; shown: false }
  | {
      slot: number;
      shown: true;
      person: DailyPerson;
      also?: DailyAlso;
      /** How it came to show: from the start, bought with Next name, or
       *  through a wrong guess, which fills in anyone it shares and then
       *  shows the next name. "end" is a name shown only because the game
       *  is over, never seen in play: none of the other three is true of
       *  it. Which names the reader saw is the log's to say (seenSlots),
       *  not this. */
      via: 'start' | 'next' | 'guess' | 'end';
      /** The guessed movie that filled it in, when a guess did: the
       *  "From your guess, Speed" line. */
      from?: { id: string; title: string };
    };

/** The facts that can be bought, in the order the facts row offers them.
 *  `years` narrows `decade`, and is only for sale once the decade has
 *  been bought. */
export type DailyFactKind = 'length' | 'rating' | 'genre' | 'decade' | 'years' | 'director';

/** The facts bought so far. Only a bought fact is there at all. Length
 *  and rating come as one of four bands each, and the years as a decade
 *  or a five-year span, so no single number can be checked against a
 *  candidate: the exact values only come with the end. */
export interface DailyFacts {
  /** 0 "Under 1h 30m", 1 "1h 30m to 2h", 2 "2h to 2h 30m", 3 "Over 2h 30m". */
  length?: number;
  /** 0 "Below 6.0", 1 "6.0 to 6.9", 2 "7.0 to 7.9", 3 "8.0 or higher". */
  rating?: number;
  /** IMDb's genres. */
  genre?: string[];
  /** The decade's first year: 1990. */
  decade?: number;
  /** The five years' first: 1995, for 1995–1999. */
  years?: number;
  /** Every director, in the crew's order. */
  director?: DailyPerson[];
}

/** How close a wrong guess was: 0 cold, 1 warm, 2 hot. */
export type DailyWarmth = 0 | 1 | 2;

/** What a wrong guess learned, worked out by the server when it was made
 *  and kept with it. Never which genre, and never which way in time or
 *  rating: those are facts the reader pays for. */
export interface DailyGuess {
  id: string;
  title: string;
  /** 0 when the catalog has none. */
  year: number;
  cost: number;
  /** The slots of the six it credits. Directors do not count. */
  shared: number[];
  sameDecade: boolean;
  sharesGenre: boolean;
  warmth: DailyWarmth;
}

/** One line of the game's record. A last wrong guess that runs the points
 *  out logs "guess" and then "out"; a right guess logs "win". "sheet" is
 *  the one Movies map the game allows, chosen by the person's IMDb name
 *  id: it costs nothing, so it is never among what was paid for. */
export type DailyEntry =
  | { type: 'next'; cost: number; slot: number }
  | { type: 'fact'; kind: DailyFactKind; cost: number }
  | { type: 'guess'; cost: number; guess: DailyGuess }
  | { type: 'sheet'; person: string }
  | { type: 'win' }
  | { type: 'gaveup' }
  | { type: 'out' };

/** Today's movie, sent once the game is over and never before. */
export interface DailyAnswer {
  id: string;
  title: string;
  year: number;
  rating: number;
  /** Its runtime, in minutes. */
  length: number;
  genres: string[];
  /** The poster's average, "#rrggbb", as DailyToday's `colour`. */
  colour: string;
  /** Absent when the catalog has no picture for it. */
  poster?: string;
}

/** A game, as the server has recorded it. */
export interface DailyGame {
  phase: 'play' | 'done';
  pts: number;
  /** How many moves are recorded. Every move is sent with it, so a move
   *  made from a point the game has already passed — an old tab, a
   *  second one — is refused rather than applied twice. */
  seq: number;
  /** RFC 3339. */
  startedAt: string;
  finishedAt: string | null;
  won: boolean;
  gaveUp: boolean;
  /** What the next wrong guess costs. */
  nextCost: number;
  /** Always six, in reveal order. Every one is shown once the game is
   *  over. */
  slots: DailySlot[];
  facts: DailyFacts;
  /** One entry per move, in order. */
  log: DailyEntry[];
  /** Whose Movies map the reader opened, by IMDb name id, or null before
   *  they have chosen. One a game: while it is on, the server shows that
   *  person's sheet and refuses every other (GET /movies, "sheet"), so
   *  two people's readable titles can never be laid side by side to
   *  leave only today's movie. Chosen once and never changed. */
  sheet: string | null;
  /** Everything the game kept back, sent once it is over. */
  end: null | { answer: DailyAnswer; directors: DailyPerson[] };
}

export interface DailyPlayer {
  /** A generated name, never typed. */
  name: string;
  /** False while it is only a proposal: nobody has pressed Play yet. */
  saved: boolean;
}

/** Today's puzzle, for the reader's own date, and their game in it if
 *  they have one. */
export interface DailyToday {
  no: number;
  /** The puzzle's calendar date, "2026-10-08": the reader's own date
   *  when they asked, in the zone the page sent. A date with no zone of
   *  its own, so it is written as it stands and never moved a day. */
  date: string;
  /** The server's clock, so the page can count down to `next` on the
   *  server's time rather than the reader's. RFC 3339. */
  now: string;
  /** The reader's next midnight, in the zone the page sent, as an
   *  instant: when their next movie starts. RFC 3339. */
  next: string;
  /** Today's poster's average colour, "#rrggbb", sent from the start: it
   *  fills the hidden card, which says nothing a search could use. */
  colour: string;
  player: DailyPlayer;
  /** Games started on this puzzle, by anyone, from every zone that has
   *  had its date: the count rolls on for as long as the puzzle is
   *  someone's today. */
  played: number;
  streak: { now: number; before: number };
  game: DailyGame | null;
  /** True from a server that is not in production, where the page offers
   *  Play again (development only): resetDaily. Never there in
   *  production, where nothing offers it and the address is not served. */
  dev?: boolean;
}

/** What the reader can do to a game in progress, one request each.
 *  "sheet" chooses the game's one Movies map, a showing person's, by
 *  IMDb name id. */
export type DailyMove =
  | { kind: 'next' }
  | { kind: 'buy'; fact: DailyFactKind }
  | { kind: 'guess'; film: string }
  | { kind: 'sheet'; person: string }
  | { kind: 'reveal' };

/** One of a showing person's movies on the Movies sheet, readable: it is
 *  inside every range the reader has bought (the decade or five years,
 *  the rating band, the genre; never the length), or the game is over.
 *  Today's movie is one of these by the same rule as any other, and looks
 *  like every other: there is a poster for every readable card, or for
 *  none. Readable cards are the ones with an `id`. */
export interface DailyReadableMovie {
  id: string;
  title: string;
  year: number;
  rating: number;
  genres: string[];
  poster?: string;
}

/** One of the person's movies outside the ranges bought, or before any
 *  range is: its year and its place on the rating axis, `at`, its rating
 *  rounded to the nearest half, and nothing else. No id, title, exact
 *  rating, genres or poster, so two people's sheets cannot be laid side
 *  by side and matched card for card, which is how combining names gave
 *  the answer away. Sent all the same, so the map keeps its shape. */
export interface DailyBlankMovie {
  year: number;
  at: number;
}

export type DailyMovie = DailyReadableMovie | DailyBlankMovie;

export interface DailyMovies {
  /** The person asked about, by id. */
  person: string;
  /** How many movies of theirs there are, readable and blank. */
  total: number;
  movies: DailyMovie[];
}

export type DailyTab = 'today' | 'week';

/** A player on a leaderboard. On the week's board `pts` is the total
 *  since Monday and `days` each day's points, Monday to the puzzle's day,
 *  0 for a day not played. */
export interface DailyBoardRow {
  /** A competition rank: one more than the players with more points, so
   *  equal scores share it. */
  place: number;
  /** Another listed player shares this place ("=7,804"). */
  tied: boolean;
  name: string;
  /** 0–359. */
  hue: number;
  pts: number;
  days?: number[];
  you: boolean;
}

/** A leaderboard: the players around the reader, never a top list, which
 *  would be the only reason to look an answer up. Today's is two above,
 *  the reader, one on their score and one below; the week's two ahead
 *  and two behind. Empty without the reader on it. */
export interface DailyBoard {
  tab: DailyTab;
  /** The players on this board. */
  total: number;
  /** The reader's own place; null without a game to place. */
  you: null | { place: number; tied: boolean; pts: number; days?: number[] };
  rows: DailyBoardRow[];
  /** Today's board only: the share of finished players who kept fewer
   *  points, and the share of finished games that were won, in percent. */
  beat: number | null;
  solved: number | null;
  /** Today's board only: how many finished scores fell in each hundred,
   *  0–99 up to 900–999, and then 1,000 on its own. Eleven counts. */
  chart?: number[] | null;
}

// Every Daily request carries the reader's time zone, as `tz`: each
// puzzle belongs to a calendar date, and the reader is given the one for
// their own date, which changes at their own midnight. The server works
// that date out from its own clock and this zone, never from the
// device's clock, so a clock wound forward opens nothing early; picking
// another zone moves a reader a day at most. It goes on in the two
// helpers below, which every Daily call goes through, so no call can be
// written without it.

/** The reader's time zone as the browser names it, "Europe/London", or
 *  null when it cannot say: an Intl that throws, or one that answers
 *  with nothing. Without one the server keeps to UTC. Read afresh for
 *  each request, so a reader who has travelled is asked about where they
 *  are now; a game already started keeps the zone it was started in. */
export function readerZone(): string | null {
  try {
    const zone: unknown = Intl.DateTimeFormat().resolvedOptions().timeZone;
    return typeof zone === 'string' && zone ? zone : null;
  } catch {
    return null;
  }
}

/** A Daily address with the reader's zone on the end of it. */
function dailyPath(path: string): string {
  const zone = readerZone();
  if (!zone) return path;
  return `${path}${path.includes('?') ? '&' : '?'}tz=${encodeURIComponent(zone)}`;
}

function dailyGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  return getJSON<T>(dailyPath(path), { signal });
}

function dailyPost<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  return postJSON<T>(dailyPath(path), body, signal);
}

/** Today's puzzle, for the reader's date. 503 with reason "not-ready"
 *  until that date's puzzle has been picked, and while the catalog is
 *  older than the runtimes a puzzle needs. It never writes, so the
 *  opening screen can ask on every visit. */
export function fetchDaily(signal?: AbortSignal): Promise<DailyToday> {
  return dailyGet<DailyToday>('/daily', signal);
}

/** Another generated name. With a player already made it renames them;
 *  without one it only proposes, and nothing is written until Play.
 *  `shown` is the name on the screen, sent so the server never offers
 *  it again. */
export function renameDaily(shown: string, signal?: AbortSignal): Promise<DailyPlayer> {
  return dailyPost<DailyPlayer>('/daily/name', { name: shown }, signal);
}

/** Starts the reader's game on puzzle `no`, under the name they were
 *  offered, making them a player first if they are not one yet. A game
 *  already started comes back as it stands. The game keeps the zone it
 *  is started in: its day ends at that zone's midnight, whatever zone a
 *  later move says it is from. */
export function playDaily(
  no: number,
  name: string,
  signal?: AbortSignal,
): Promise<{ game: DailyGame; player: DailyPlayer }> {
  return dailyPost(`/daily/${no}/play`, { name }, signal);
}

/** The reader's place on this week's board, as the This week tab ranks
 *  it: a competition rank by points, among the same players. `players`
 *  is the board's size, the reader counted. */
export interface DailyWeek {
  rank: number;
  players: number;
}

/** The reader's standing, for the start screen's banner: their streak,
 *  which no screen shows now, and their place this week, which is null with no points this week to
 *  place (a Monday before playing, say) and for a reader who has never
 *  played. */
export interface DailyMe {
  streak: number;
  week: DailyWeek | null;
}

/** The reader's standing on their own puzzle's week. Before they have
 *  finished today's game it is over the days before today, so not having
 *  played yet never counts against them; once they have, today is in it.
 *  It never writes, and the server keeps it for a minute, so the banner
 *  can ask on every visit; a reader with no cookie is simply
 *  nobody, `{ streak: 0, week: null }`. */
export function fetchDailyMe(signal?: AbortSignal): Promise<DailyMe> {
  return dailyGet<DailyMe>('/daily/me', signal);
}

/** The reader's place this week alone, for a screen that shows it beside
 *  something else: null with no place to show, and null too when it
 *  cannot be had. A place that fails is no place, so it never takes the
 *  screen it sits on away, and never leaves an older one standing in its
 *  stead. */
export function fetchDailyWeek(signal?: AbortSignal): Promise<DailyWeek | null> {
  return fetchDailyMe(signal).then(
    (me) => me.week,
    () => null,
  );
}

// The moves. Each is one request to its own address, carrying `key` and
// `seq`, and each answers with the game as it now stands. `key` names the
// move for good, so sending it again after a lost answer is never
// charged twice; `seq` is the number of moves the page has seen, so a
// move from a point the game has passed is refused ("stale") with the
// game as it stands.

/** Shows the next hidden name, for NEXT_COST. */
export function nextName(no: number, key: string, seq: number, signal?: AbortSignal): Promise<{ game: DailyGame }> {
  return dailyPost(`/daily/${no}/next`, { key, seq }, signal);
}

/** Buys a fact about the movie. */
export function buyFact(
  no: number,
  kind: DailyFactKind,
  key: string,
  seq: number,
  signal?: AbortSignal,
): Promise<{ game: DailyGame }> {
  return dailyPost(`/daily/${no}/buy`, { key, seq, kind }, signal);
}

/** Guesses a movie, by its IMDb id. */
export function guessMovie(
  no: number,
  film: string,
  key: string,
  seq: number,
  signal?: AbortSignal,
): Promise<{ game: DailyGame }> {
  return dailyPost(`/daily/${no}/guess`, { key, seq, film }, signal);
}

/** Chooses whose Movies map the reader opens, by IMDb name id: one of
 *  the names showing, once a game and for nothing. Every other sheet
 *  stays closed until the end. A second choice, of anyone, the same
 *  person included, is refused ("known"), as is anyone not showing
 *  ("bad"). */
export function openSheet(
  no: number,
  person: string,
  key: string,
  seq: number,
  signal?: AbortSignal,
): Promise<{ game: DailyGame }> {
  return dailyPost(`/daily/${no}/sheet`, { key, seq, person }, signal);
}

/** Shows the answer, which ends the game at nought. */
export function revealAnswer(no: number, key: string, seq: number, signal?: AbortSignal): Promise<{ game: DailyGame }> {
  return dailyPost(`/daily/${no}/reveal`, { key, seq }, signal);
}

/** Any one move, sent through its own call above: for a page that keeps
 *  the move it is waiting on as a value, to send again under the same key
 *  (daily.ts's moveSig). */
export function sendDailyMove(
  no: number,
  move: DailyMove,
  key: string,
  seq: number,
  signal?: AbortSignal,
): Promise<{ game: DailyGame }> {
  switch (move.kind) {
    case 'next':
      return nextName(no, key, seq, signal);
    case 'buy':
      return buyFact(no, move.fact, key, seq, signal);
    case 'guess':
      return guessMovie(no, move.film, key, seq, signal);
    case 'sheet':
      return openSheet(no, move.person, key, seq, signal);
    case 'reveal':
      return revealAnswer(no, key, seq, signal);
  }
}

/** One showing person's movies, for the Movies sheet, from today's
 *  snapshot: readable inside every range the reader has bought, and
 *  blank, a year and a place on the rating axis, everywhere else (see
 *  DailyMovie). Asked again once a range is bought. While the game is on
 *  it answers only for the person whose map the reader chose (openSheet,
 *  DailyGame's `sheet`): anyone else, shown, hidden, outside the cast or
 *  anyone at all before a map is chosen, is refused alike (409 "sheet"),
 *  so the refusal never says who is in the cast. 400 "bad" is for an id
 *  that is no IMDb name id, or, once the game is over, someone not in the
 *  cast, when it answers for anyone in it, every movie readable. */
export function fetchDailyMovies(no: number, person: string, signal?: AbortSignal): Promise<DailyMovies> {
  return dailyGet<DailyMovies>(`/daily/${no}/movies?person=${encodeURIComponent(person)}`, signal);
}

/** Development only, for trying the Daily again and again on one day:
 *  starts the reader again as a brand-new player, on a movie newly
 *  picked for the puzzle of their date, the one GET /daily shows them
 *  once they are nobody again. The server picks it as the daily job
 *  would, never today's answer again, and tells nobody which; it deletes
 *  every game on that puzzle, lets go of the reader's cookie and answers
 *  204, with nothing in it. In production the address is not there at
 *  all (404), and today's puzzle never says `dev`. It goes through the
 *  Daily's helpers with the rest, so it carries the reader's zone, which
 *  is how the server finds their date, and it is a JSON write like every
 *  other, so another site cannot make it in the reader's name. */
export function resetDaily(signal?: AbortSignal): Promise<void> {
  return dailyPost<void>('/daily/dev/reset', {}, signal);
}

/** A leaderboard for puzzle `no`: today's, which is everyone who has
 *  finished that puzzle from any zone, or the week's so far. */
export function fetchDailyBoard(no: number, tab: DailyTab, signal?: AbortSignal): Promise<DailyBoard> {
  return dailyGet<DailyBoard>(`/daily/${no}/board?tab=${tab}`, signal);
}
