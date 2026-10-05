// Where a movie can be watched in the reader's country: the hover
// preview's Stream row and the panel's Where to watch section. The server
// asks the Streaming Availability API (by Movie of the Night) about the
// movie, in the country it reads from the reader's IP address, and keeps
// the answer; the page never names a country. Kept apart from the
// components, as small functions, so each rule is written once and can be
// checked without a DOM.

import { useEffect, useState } from 'react';
import { fetchWhereToWatch, type WatchOfferBody, type WhereToWatchBody } from './api';
import { isImdbId } from './movieParam';
import type { Theme } from './theme';

/** One service a movie can be watched on. */
export interface WatchOffer {
  /** The service's id: an add-on's own, for an add-on. */
  id: string;
  name: string;
  /** The movie's page on that service. */
  link: string;
  /** The service's logo, drawn for each theme; either can be missing. */
  logo: { dark?: string; light?: string };
  /** What renting or buying it costs, worded as the API words it. */
  price?: string;
  /** For an add-on, the service it is bought through ("Prime Video"). */
  via?: string;
}

/** Where a movie can be watched, in the country the server placed the
 *  reader in. A country without coverage has nothing in any group. */
export interface WhereToWatch {
  /** The country's name, as the API's list of countries gives it. */
  countryName?: string;
  covered: boolean;
  /** Included with a subscription, or with an add-on to one. */
  stream: WatchOffer[];
  free: WatchOffer[];
  rent: WatchOffer[];
  buy: WatchOffer[];
}

/** What useWhereToWatch hands its component: waiting for the answer,
 *  the answer, or a failure, which the page shows as nothing at all. */
export type WatchState =
  | { status: 'wait'; data: null }
  | { status: 'ok'; data: WhereToWatch }
  | { status: 'error'; data: null };

const WAIT: WatchState = { status: 'wait', data: null };
const ERROR: WatchState = { status: 'error', data: null };

/** The answers that have come back, kept for the visit. The reader's
 *  country does not change within it, and the server stores each answer
 *  and keeps it fresh itself, so a movie opened again answers from here
 *  at once. */
const answers = new Map<string, WhereToWatch>();

/** The movies being asked about right now: one request each, shared by
 *  the card the pointer rests on, its preview and its panel. */
const asking = new Map<string, Promise<WhereToWatch | null>>();

/** When each movie's last ask failed, by the page's clock. A preview
 *  opening moments after the ask made as the pointer came to rest has
 *  failed takes that failure, rather than asking again within the same
 *  open: a quick failure would otherwise cost two requests a preview. */
const failedAt = new Map<string, number>();

/** How long a failure stands: well past the rest before a preview opens
 *  (PREVIEW_REST_MS), and short enough that the next open asks again. */
export const FAILURE_HOLD_MS = 2000;

/** Where a movie can be watched, or null when the server could not say.
 *  An answer is kept for the visit. A failure stands only for a moment
 *  (FAILURE_HOLD_MS), so a later open asks again, as the server keeps no
 *  failure either. Asked when the pointer begins to rest on a card,
 *  before its preview opens, so the answer is mostly in by the time the
 *  preview shows; otherwise when the preview or the panel first needs
 *  it. */
export function askWhereToWatch(id: string): Promise<WhereToWatch | null> {
  const known = answers.get(id);
  if (known) return Promise.resolve(known);
  const pending = asking.get(id);
  if (pending) return pending;
  if (!isImdbId(id, 'tt')) return Promise.resolve(null);
  if (failureStands(id)) return Promise.resolve(null);
  const ask = fetchWhereToWatch(id)
    .then((body) => {
      const answer = readAnswer(body);
      answers.set(id, answer);
      failedAt.delete(id);
      return answer;
    })
    .catch(() => {
      failedAt.set(id, performance.now());
      return null;
    })
    .finally(() => {
      if (asking.get(id) === ask) asking.delete(id);
    });
  asking.set(id, ask);
  return ask;
}

/** Whether a movie's last ask failed too recently to ask again. */
function failureStands(id: string): boolean {
  const failed = failedAt.get(id);
  return failed !== undefined && performance.now() - failed < FAILURE_HOLD_MS;
}

/** What has already come back for a movie this visit, if anything. */
export function whereToWatchKnown(id: string): WhereToWatch | undefined {
  return answers.get(id);
}

/** A movie's state as a component first draws it: its answer if one has
 *  come back this visit, a failure that still stands, and otherwise
 *  waiting, an older failure included, since its component asks again as
 *  it mounts. */
function watchStateOf(id: string): WatchState {
  const known = answers.get(id);
  if (known) return { status: 'ok', data: known };
  return failureStands(id) ? ERROR : WAIT;
}

/** Asks about a movie on behalf of a component, and hands the outcome to
 *  `got` unless the component has gone first. Returns what it calls as
 *  it goes. The request itself carries on: its answer is kept for the
 *  next time the movie is shown. */
export function followWhereToWatch(id: string, got: (state: WatchState) => void): () => void {
  let on = true;
  void askWhereToWatch(id).then((answer) => {
    if (on) got(answer ? { status: 'ok', data: answer } : ERROR);
  });
  return () => {
    on = false;
  };
}

/** Where a movie can be watched, for its preview or its panel. A movie
 *  already answered this visit shows its answer from the first paint. */
export function useWhereToWatch(imdbId: string): WatchState {
  const [got, setGot] = useState(() => ({ id: imdbId, state: watchStateOf(imdbId) }));
  useEffect(
    () =>
      followWhereToWatch(imdbId, (state) =>
        setGot((was) => (was.id === imdbId && sameState(was.state, state) ? was : { id: imdbId, state })),
      ),
    [imdbId],
  );
  return got.id === imdbId ? got.state : watchStateOf(imdbId);
}

function sameState(a: WatchState, b: WatchState): boolean {
  return a.status === b.status && a.data === b.data;
}

/** The server's answer, read defensively: a group that is missing is
 *  empty, and an offer without a name or a web address to open is left
 *  out. An answer that does not say whether the country is covered
 *  cannot be read at all, and counts as a failure: saying a country has
 *  no coverage when it may have would be wrong. */
export function readAnswer(body: WhereToWatchBody): WhereToWatch {
  if (!body || typeof body !== 'object' || typeof body.covered !== 'boolean') {
    throw new Error('unreadable where-to-watch answer');
  }
  const countryName = text(body.countryName);
  const covered = body.covered;
  return {
    ...(countryName ? { countryName } : {}),
    covered,
    stream: covered ? offers(body.stream) : [],
    free: covered ? offers(body.free) : [],
    rent: covered ? offers(body.rent) : [],
    buy: covered ? offers(body.buy) : [],
  };
}

function text(v: unknown): string | undefined {
  return typeof v === 'string' && v.trim() ? v.trim() : undefined;
}

/** Only an address on the web: anything else in a link would run as
 *  script or go nowhere. */
function webLink(v: unknown): string | undefined {
  const s = text(v);
  return s && /^https?:\/\//i.test(s) ? s : undefined;
}

/** One group's offers, each service once, in the order the server sent
 *  them (it already keeps one entry per service, the cheapest). */
function offers(list: WatchOfferBody[] | undefined): WatchOffer[] {
  if (!Array.isArray(list)) return [];
  const out: WatchOffer[] = [];
  const seen = new Set<string>();
  for (const o of list) {
    if (!o || typeof o !== 'object') continue;
    const name = text(o.name);
    const link = webLink(o.link);
    if (!name || !link) continue;
    const id = text(o.id) ?? name;
    if (seen.has(id)) continue;
    seen.add(id);
    const logo = o.logo && typeof o.logo === 'object' ? o.logo : {};
    const dark = text(logo.dark);
    const light = text(logo.light);
    const price = text(o.price);
    const via = text(o.via);
    out.push({
      id,
      name,
      link,
      logo: { ...(dark ? { dark } : {}), ...(light ? { light } : {}) },
      ...(price ? { price } : {}),
      ...(via ? { via } : {}),
    });
  }
  return out;
}

/** How many services the preview's row names before "+N". */
const STREAM_PEEK = 3;

/** The preview's Stream row: what streams with a subscription and what
 *  is free, in that order and each service once, the first three named
 *  and a count of the rest. Null when there is no row: nothing to stream,
 *  a country without coverage, a failure, or no answer yet. */
export function streamPeek(w: WatchState): { shown: WatchOffer[]; more: number } | null {
  if (w.status !== 'ok' || !w.data.covered) return null;
  const seen = new Set<string>();
  const all = [...w.data.stream, ...w.data.free].filter((o) => !seen.has(o.id) && !!seen.add(o.id));
  if (all.length === 0) return null;
  return { shown: all.slice(0, STREAM_PEEK), more: Math.max(0, all.length - STREAM_PEEK) };
}

/** The four ways to watch, in the order the panel lists them. */
type WatchKind = 'stream' | 'free' | 'rent' | 'buy';

/** Each way's row label in the panel. */
export const WATCH_LABEL: Record<WatchKind, string> = {
  stream: 'Stream',
  free: 'Free',
  rent: 'Rent',
  buy: 'Buy',
};

const VERB: Record<WatchKind, string> = {
  stream: 'Stream on',
  free: 'Free on',
  rent: 'Rent on',
  buy: 'Buy on',
};

/** The panel's rows: Stream, Free, Rent and Buy, only the ones that
 *  have something. */
export function watchGroups(w: WhereToWatch): { kind: WatchKind; offers: WatchOffer[] }[] {
  if (!w.covered) return [];
  return (['stream', 'free', 'rent', 'buy'] as const)
    .map((kind) => ({ kind, offers: w[kind] }))
    .filter((g) => g.offers.length > 0);
}

/** What a service's chip says to assistive tech: the way to watch, the
 *  service, the add-on's own service, the price, and that it opens a new
 *  tab. "Rent on Prime Video, 3.99 USD, opens in a new tab". */
export function offerLabel(kind: WatchKind, o: WatchOffer): string {
  return `${VERB[kind]} ${o.name}${o.via ? ` via ${o.via}` : ''}${o.price ? `, ${o.price}` : ''}, opens in a new tab`;
}

/** The logo drawn for this theme: the one made for it, else the other,
 *  else none, when the chip shows the service's name instead. */
export function logoFor(o: Pick<WatchOffer, 'logo'>, theme: Theme): string | undefined {
  return theme === 'dark' ? (o.logo.dark ?? o.logo.light) : (o.logo.light ?? o.logo.dark);
}

/** Logo addresses that failed to load this visit. A chip given one shows
 *  the service's name from its first paint, rather than an empty box
 *  until the browser has failed again. */
const brokenLogos = new Set<string>();

export function logoBroke(src: string): void {
  brokenLogos.add(src);
}

export function logoBroken(src: string): boolean {
  return brokenLogos.has(src);
}

/** The countries whose names read with "the" in front of them, beyond
 *  the ones the rules in countryIn catch. */
const WITH_THE = new Set(['Netherlands', 'Philippines', 'Bahamas', 'Gambia', 'Maldives', 'Comoros']);

/** A country's name as it reads after "in": "in the United States", "in
 *  the Netherlands", "in France". A name the API gives with its own
 *  "The" is lowercased mid-sentence; with no name, "your country". */
export function countryIn(name: string | undefined): string {
  const n = name?.trim().replace(/^the\s+/i, '');
  if (!n) return 'your country';
  const the = WITH_THE.has(n) || /^United\s/.test(n) || /\bRepublic\b/.test(n) || /\sIslands$/.test(n);
  return the ? `the ${n}` : n;
}

/** What the panel says for a covered country with nowhere to watch the
 *  movie. */
export function nothingText(w: Pick<WhereToWatch, 'countryName'>): string {
  return `Not available to stream, rent or buy in ${countryIn(w.countryName)} right now.`;
}

/** What the panel says for a country without coverage. */
export const UNCOVERED_TEXT = 'Streaming info isn’t available in your country yet.';
