import { useEffect } from 'react';

/** A map lives at /movie/tt0133093-the-matrix. The id is IMDb's own, and
 *  the slug is there so a pasted link says what it opens. A map nobody
 *  can link is a map nobody shares. */

/** An IMDb id: "tt" and at least one digit for a title, "nm" and at
 *  least one digit for a person. Ids have grown over the years, so the
 *  length is not assumed. The server takes no other kind of id, and
 *  refuses a request that names one. */
export function isImdbId(id: string, kind: 'tt' | 'nm'): boolean {
  return id.startsWith(kind) && /^\d{1,17}$/.test(id.slice(kind.length));
}

/** The id in /movie/<tconst>[-slug]. Anything else is not a route. */
export function movieIdFromPath(pathname: string): string | null {
  const m = /^\/movie\/([^/-]+)(?:-[^/]*)?\/?$/.exec(pathname);
  return m && isImdbId(m[1], 'tt') ? m[1] : null;
}

/** Lower-case, hyphenated, ASCII-ish: a slug that survives being pasted
 *  into a chat window. */
export function slugify(title: string): string {
  return title
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/['’]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
    .replace(/-+$/g, '');
}

/** The canonical path for a movie. The title is optional: an id alone is
 *  a valid route, and the slug is added once the title is known. */
export function filmPath(id: string, title?: string): string {
  const slug = title ? slugify(title) : '';
  return slug ? `/movie/${id}-${slug}` : `/movie/${id}`;
}

/** The About page's address, which is what its link pushes. */
export const ABOUT_PATH = '/about';

/** Whether a path is the About page: /about, with or without the
 *  slash, and nothing under it. The route is one of four things — the
 *  opening screen, the About page, the Daily, or a map — and a path
 *  that is none of the others is the opening screen. cmd/api's
 *  serveIndex reads the address the same way when it names the page
 *  for a scraper. */
export function isAboutPath(pathname: string): boolean {
  return pathname === ABOUT_PATH || pathname === `${ABOUT_PATH}/`;
}

/** Cinedikt Daily's address, which the opening screen's banner pushes. */
export const DAILY_PATH = '/daily';

/** Whether a path is the Daily: /daily, with or without the slash, and
 *  nothing under it, held as strictly as the About page is. The puzzle
 *  is always today's, so there is nothing for a deeper path to name; a
 *  link to /daily/142 is the opening screen, as /about/team is. The
 *  server names the page it serves at these two paths the same way. */
export function isDailyPath(pathname: string): boolean {
  return pathname === DAILY_PATH || pathname === `${DAILY_PATH}/`;
}

/** Where a path puts the reader: on a map (`movieId`), on the About page
 *  (`about`), on the Daily (`daily`), or, with none of them, on the
 *  opening screen. The router reads its route from the address with
 *  this after every move, so the parts of it cannot disagree with each
 *  other or with the address. */
export function routeAt(pathname: string): {
  movieId: string | null;
  about: boolean;
  daily: boolean;
} {
  return {
    movieId: movieIdFromPath(pathname),
    about: isAboutPath(pathname),
    daily: isDailyPath(pathname),
  };
}

/** What the tab says. Naming the movie is the point: a reader with half
 *  a dozen maps open should be able to tell them apart, and a link
 *  previewed in a chat should say what it opens.
 *
 *  Every view takes its title from here so none of them drift. */
export const HOME_TITLE = 'Cinedikt — a movie’s cast and directors, and everything they made';

/** The About page's tab. The server writes the same title, and the same
 *  og:title, into the page it serves at /about. */
const ABOUT_TITLE = 'About · Cinedikt';

/** The Daily's tab. The server writes the same title into the page it
 *  serves at /daily (cmd/api's nameDaily), with an og:title of its own
 *  for a link pasted into a chat. */
export const DAILY_TITLE = 'Daily · Cinedikt';

/** The pages that name themselves whatever map was last open. */
export type NamedPage = 'about' | 'daily';

export function pageTitle(title?: string, page?: NamedPage): string {
  if (page === 'about') return ABOUT_TITLE;
  if (page === 'daily') return DAILY_TITLE;
  const named = title?.trim();
  return named ? `${named} — everything its cast and directors made · Cinedikt` : HOME_TITLE;
}

/** Puts the movie's name in the tab, or the About page's or the Daily's,
 *  and takes it out again on the way back to first run. */
export function usePageTitle(title: string | undefined, page?: NamedPage): void {
  useEffect(() => {
    document.title = pageTitle(title, page);
  }, [title, page]);
}
