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
// send it — so these shapes are what the page has to work with, and no
// more.

/** A movie as the Daily shows it: a card turned over, a starting movie,
 *  a guess, or the answer at the end. `rating` is a number for every card
 *  and for the answer; a guessed movie can be unrated (null), and its
 *  year is 0 when the catalog has none. */
export interface DailyFilm {
  id: string;
  title: string;
  year: number;
  rating: number | null;
  /** Month and day as MMDD, 0 when the date says only a year. */
  md: number;
  /** Absent when the catalog has no picture for it. */
  poster?: string;
}

/** One of the answer's people, once the reader knows them. */
export interface DailyPerson {
  /** An IMDb name id, such as nm0000206. */
  id: string;
  name: string;
  role: 'director' | 'cast';
  /** Their place in the puzzle's people: directors first, then the cast
   *  in billing order. It is their colour, HUES[slot % 16], and it never
   *  changes during a game. */
  slot: number;
  /** TMDb's 185px photo, when there is one to show. */
  photo?: string;
  /** The board cards they are on, so their dots can be drawn. */
  cards: string[];
}

/** Which side of a wrong guess the answer is on. */
export type DailyYearHint = 'older' | 'newer' | 'same';
export type DailyRatingHint = 'higher' | 'lower' | 'same';

/** One line of the game's record, as the panel's feed tells it. */
export type DailyEntry =
  | { type: 'start' }
  | { type: 'flip'; card: string; cost: number; film: DailyFilm; relative?: undefined }
  | { type: 'flip'; card: string; cost: number; relative: { shared: number }; film?: undefined }
  | { type: 'person'; role: 'director' | 'actor'; cost: number; people: DailyPerson[] }
  | { type: 'genres'; cost: number; genres: string[] }
  /** The answer's year, which the server sends with this entry and never
   *  before it, or before the end. */
  | { type: 'year'; cost: number; year: number }
  | {
      type: 'guess';
      cost: number;
      film: DailyFilm;
      /** The board card the guessed movie is, which turns over; null
       *  when it is not on the board. */
      card: string | null;
      shared: DailyPerson[];
      /** Where the answer sits relative to the guess. Null when the
       *  guess has no year, or no rating, to compare. */
      year: DailyYearHint | null;
      rating: DailyRatingHint | null;
    }
  | { type: 'win' }
  | { type: 'gaveup' }
  | { type: 'out' };

/** Everything the game kept back, sent once it is over. */
export interface DailyEnd {
  answer: DailyFilm & { genres: string[] };
  /** Every card, its people as slots. */
  cards: { id: string; film: DailyFilm; people: number[] }[];
  /** Every one of the answer's people, with their cards. */
  people: DailyPerson[];
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
  /** Whole seconds from Play to the end, once it is over. */
  secs: number | null;
  won: boolean;
  gaveUp: boolean;
  /** What the next wrong guess costs. */
  nextCost: number;
  /** "start" first, then one entry per move. A last wrong guess that
   *  runs the points out logs "guess" and then "out". */
  log: DailyEntry[];
  /** The people bought and the people found, in slot order. */
  known: DailyPerson[];
  end: DailyEnd | null;
}

/** A card on the board before anything is known about it: where it goes,
 *  and nothing else. The id is opaque, so its number says nothing. */
export interface DailyCard {
  id: string;
  year: number;
  rating: number;
  md: number;
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
   *  instant: when their next map starts. RFC 3339. */
  next: string;
  cards: DailyCard[];
  /** The three movies showing from the start, the only ones sent whole. */
  start: { card: string; film: DailyFilm }[];
  /** How many directors and cast the answer has, for the clue buttons. */
  clues: { directors: number; cast: number };
  player: DailyPlayer;
  /** Games started on this puzzle, by anyone, from every zone that has
   *  had its date: the count rolls on for as long as the puzzle is
   *  someone's today. */
  played: number;
  streak: { now: number; before: number };
  game: DailyGame | null;
}

/** The clues the panel sells. */
export type DailyClue = 'director' | 'actor' | 'genres' | 'year';

/** What the reader can do to a game in progress. */
export type DailyMove =
  | { kind: 'flip'; card: string }
  | { kind: 'buy'; clue: DailyClue }
  | { kind: 'guess'; film: string }
  | { kind: 'reveal' };

export type DailyTab = 'today' | 'week';

/** A player on a leaderboard. On the week's board `pts` is the week's sum
 *  and `days` its Monday-to-Sunday points, null for a day not played. */
export interface DailyBoardRow {
  rank: number;
  name: string;
  /** 0–359, for their avatar. */
  hue: number;
  pts: number;
  /** Today's time, on today's board only: a week row never carries it.
   *  The reader's own time for the week is `you.secs`. */
  secs?: number;
  you: boolean;
  days?: (number | null)[];
}

/** Where the leaderboard skips ranks. */
export interface DailyBoardGap {
  gap: true;
}

export interface DailyBoard {
  tab: DailyTab;
  /** The players on this board, and the reader if they are not on it. */
  total: number;
  /** The reader's own place, which they see whether or not they are
   *  listed; null without a game to place. */
  you: null | {
    rank: number;
    pts: number;
    secs: number;
    /** On the board for everyone to see: false until they have played on
     *  enough earlier days. */
    listed: boolean;
    days?: (number | null)[];
  };
  rows: (DailyBoardRow | DailyBoardGap)[];
  /** Today's board only: the share of finished players who kept fewer
   *  points, and the share of finished games that were won, in percent. */
  beat: number | null;
  solved: number | null;
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
 *  until that date's puzzle has been picked. It never writes, so the
 *  opening screen can ask on every visit. */
export function fetchDaily(signal?: AbortSignal): Promise<DailyToday> {
  return dailyGet<DailyToday>('/daily', signal);
}

/** Another generated name. With a player already made it renames them;
 *  without one it only proposes, and nothing is written until Play.
 *  `shown` is the name on the screen, sent so the server never offers
 *  it again: a reader who presses New name always sees it change, even
 *  when the pool of names is small. */
export function renameDaily(shown: string, signal?: AbortSignal): Promise<DailyPlayer> {
  return dailyPost<DailyPlayer>('/daily/name', { name: shown }, signal);
}

/** Starts the reader's game on puzzle `no`, under the name they were
 *  shown, making them a player first if they are not one yet. A game
 *  already started comes back as it stands. The game keeps the zone it
 *  is started in: its map ends at that zone's midnight, whatever zone a
 *  later move says it is from. */
export function playDaily(
  no: number,
  name: string,
  signal?: AbortSignal,
): Promise<{ game: DailyGame; player: DailyPlayer }> {
  return dailyPost(`/daily/${no}/play`, { name }, signal);
}

/** The reader's place on this week's board, as the This week tab ranks
 *  it: by points, then by time, among the same players. `players` is
 *  the board's size, the reader counted. */
export interface DailyWeek {
  rank: number;
  players: number;
}

/** The reader's standing, for the opening screens: the streak the title
 *  screen shows, and their place this week, which is null with no
 *  points this week to place (a Monday before playing, say) and for a
 *  reader who has never played. */
export interface DailyMe {
  streak: number;
  week: DailyWeek | null;
}

/** The reader's standing on their own puzzle's week. Before they have
 *  finished today's game it is over the days before today, so not having
 *  played yet never counts against them; once they have, today is in it.
 *  It never writes, and the server keeps it for a minute, so both opening
 *  screens can ask on every visit; a reader with no cookie is simply
 *  nobody, `{ streak: 0, week: null }`. */
export function fetchDailyMe(signal?: AbortSignal): Promise<DailyMe> {
  return dailyGet<DailyMe>('/daily/me', signal);
}

/** The reader's place this week alone, for a screen that shows it beside
 *  something else: null with no place to show, and null too when it
 *  cannot be had. A place that fails is no place, so it never takes the
 *  screen it sits on away, and never leaves an older one standing in its
 *  stead: the banner and both of the title screen's asks go through it. */
export function fetchDailyWeek(signal?: AbortSignal): Promise<DailyWeek | null> {
  return fetchDailyMe(signal).then(
    (me) => me.week,
    () => null,
  );
}

/** One move in a game in progress. `key` names this move for good, so
 *  sending it again after a lost answer is never charged twice; `seq` is
 *  the number of moves the page has seen, so a move from a point the game
 *  has passed is refused ("stale") with the game as it stands. */
export function sendDailyMove(
  no: number,
  move: DailyMove,
  key: string,
  seq: number,
  signal?: AbortSignal,
): Promise<{ game: DailyGame }> {
  const base = { key, seq };
  switch (move.kind) {
    case 'flip':
      return dailyPost(`/daily/${no}/flip`, { ...base, card: move.card }, signal);
    case 'buy':
      return dailyPost(`/daily/${no}/buy`, { ...base, kind: move.clue }, signal);
    case 'guess':
      return dailyPost(`/daily/${no}/guess`, { ...base, film: move.film }, signal);
    case 'reveal':
      return dailyPost(`/daily/${no}/reveal`, base, signal);
  }
}

/** A leaderboard for puzzle `no`: today's, which is everyone who has
 *  finished that puzzle from any zone, or the week's so far. */
export function fetchDailyBoard(
  no: number,
  tab: DailyTab,
  signal?: AbortSignal,
): Promise<DailyBoard> {
  return dailyGet<DailyBoard>(`/daily/${no}/board?tab=${tab}`, signal);
}
