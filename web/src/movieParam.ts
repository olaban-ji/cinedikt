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
 *  slash, and nothing under it. The route is one of three things — the
 *  opening screen, the About page, or a map — and a path that is
 *  neither of the others is the opening screen. cmd/api's serveIndex
 *  reads the address the same way when it names the page for a
 *  scraper. */
export function isAboutPath(pathname: string): boolean {
  return pathname === ABOUT_PATH || pathname === `${ABOUT_PATH}/`;
}

/** Where a path puts the reader: on a map (`movieId`), on the About page
 *  (`about`), or, with neither, on the opening screen. The router reads
 *  its route from the address with this after every move, so the two
 *  halves of it cannot disagree with each other or with the address. */
export function routeAt(pathname: string): { movieId: string | null; about: boolean } {
  return { movieId: movieIdFromPath(pathname), about: isAboutPath(pathname) };
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

export function pageTitle(title?: string, about = false): string {
  if (about) return ABOUT_TITLE;
  const named = title?.trim();
  return named ? `${named} — everything its cast and directors made · Cinedikt` : HOME_TITLE;
}

/** Puts the movie's name in the tab, or the About page's, and takes it
 *  out again on the way back to first run. */
export function usePageTitle(title: string | undefined, about = false): void {
  useEffect(() => {
    document.title = pageTitle(title, about);
  }, [title, about]);
}
