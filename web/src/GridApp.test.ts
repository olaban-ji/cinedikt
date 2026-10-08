import { createElement, type MouseEvent } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  GridApp,
  askForMissingPhotos,
  backLabel,
  drawsPage,
  headerClass,
  pageOf,
  skipsOpening,
  useFilmRoute,
  type FilmRoute,
} from './GridApp';
import type { fetchPeoplePhotos } from './api';
import { DailyPage } from './DailyPage';
import type { GridPerson } from './grid';
import { routeAt } from './movieParam';
import { freshFilters } from './trail';

const person = (id: string, name: string, photo?: string): GridPerson => ({
  id,
  name,
  role: 'cast',
  ...(photo ? { photo } : {}),
});

/** A stand-in for fetchPeoplePhotos that records what it is asked. */
function fakeFetcher() {
  return vi.fn<typeof fetchPeoplePhotos>(async () => ({}));
}

// A map's payload carries the photo of everybody the people job has
// answered for. The rest are asked for, once per map.
describe('askForMissingPhotos', () => {
  it('asks for nothing when everyone came with a photo', () => {
    const fetcher = fakeFetcher();
    const people = [
      person('nm0000206', 'Keanu Reeves', 'https://image.tmdb.org/t/p/w185/keanu.jpg'),
      person('nm0000401', 'Laurence Fishburne', 'https://image.tmdb.org/t/p/w185/laurence.jpg'),
    ];
    askForMissingPhotos({ people }, new AbortController().signal, () => {}, fetcher);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('asks once, for exactly the people missing one, handing on the answers as they come', () => {
    const fetcher = fakeFetcher();
    const signal = new AbortController().signal;
    const onSome = () => {};
    const people = [
      person('nm0000206', 'Keanu Reeves', 'https://image.tmdb.org/t/p/w185/keanu.jpg'),
      person('nm0080930', 'Edward Biby'),
      person('nm0000401', 'Laurence Fishburne', 'https://image.tmdb.org/t/p/w185/laurence.jpg'),
      person('nm0088471', 'B.F. Blinn'),
    ];
    askForMissingPhotos({ people }, signal, onSome, fetcher);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledWith(['nm0080930', 'nm0088471'], signal, onSome);
  });
});

// A trailer playing in the hover preview blurs the header behind it
// (.cd-trailer-focus); the search field, and the results it opens, stay
// over the blur while the field has the focus.
describe('headerClass', () => {
  const flags = { map: true, over: false, away: false, searching: false };

  it('lifts the header while its search field has the focus, and only then', () => {
    expect(headerClass({ ...flags, searching: true }).split(' ')).toContain('cd-header-searching');
    expect(headerClass(flags).split(' ')).not.toContain('cd-header-searching');
  });

  it('keeps the classes it had', () => {
    expect(headerClass({ map: false, over: false, away: false, searching: false })).toBe('cd-header');
    expect(headerClass({ map: true, over: true, away: true, searching: false })).toBe(
      'cd-header cd-header-map cd-header-over cd-header-away',
    );
    expect(headerClass({ map: true, over: true, away: false, searching: true })).toBe(
      'cd-header cd-header-map cd-header-over cd-header-searching',
    );
  });
});

/** Puts the page at `path` with `state` on its history entry, as a
 *  reload or a pasted link would find it. Its history records what is
 *  done to it, and a push moves the address, as a browser's does, so a
 *  test can read the route a move leads to from where it left the
 *  address. */
function at(path: string, state: unknown) {
  const here = { pathname: '', search: '', hash: '', href: 'https://cinedikt.com/' };
  const moveTo = (to: string) => {
    const url = new URL(to, here.href);
    Object.assign(here, { pathname: url.pathname, search: url.search, hash: url.hash, href: url.href });
  };
  moveTo(path);
  vi.stubGlobal('location', here);
  const h = {
    state,
    pushState: vi.fn((next: unknown, _unused: string, to: string) => {
      h.state = next;
      moveTo(to);
    }),
    replaceState: vi.fn(),
    back: vi.fn(),
  };
  vi.stubGlobal('history', h);
  vi.stubGlobal('window', { innerWidth: 1366, innerHeight: 768 });
  return h;
}

/** A plain click with the main button, unless told otherwise. */
function click(over: Partial<MouseEvent<HTMLAnchorElement>> = {}) {
  return {
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    button: 0,
    preventDefault: vi.fn(),
    ...over,
  } as unknown as MouseEvent<HTMLAnchorElement> & { preventDefault: ReturnType<typeof vi.fn> };
}

/** The router as the page's first render has it. The server renderer
 *  runs no effects, and a state update after the render is nothing to
 *  it, so what is left to see is what each move does to the history and
 *  the address. The router reads its route back from the address after
 *  every move, so `routeAt(location.pathname)` is the route it moves to. */
function route(adopt = { current: vi.fn() }): FilmRoute {
  let got: FilmRoute | undefined;
  renderToStaticMarkup(
    createElement(function Router() {
      got = useFilmRoute(adopt);
      return null;
    }),
  );
  if (!got) throw new Error('the router did not render');
  return got;
}

/** The whole page, as its first render draws it. */
const page = () => renderToStaticMarkup(createElement(GridApp));

describe('the About page’s route', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is pushed by the About link, one entry deeper, with the filters clear', () => {
    const h = at('/', { depth: 2, filters: freshFilters() });
    const adopt = { current: vi.fn() };
    const e = click();
    route(adopt).openAbout(e);
    expect(e.preventDefault).toHaveBeenCalled();
    expect(h.pushState).toHaveBeenCalledTimes(1);
    expect(h.pushState).toHaveBeenCalledWith({ depth: 3, filters: freshFilters() }, '', '/about');
    expect(adopt.current).toHaveBeenCalledWith(freshFilters());
    expect(routeAt(location.pathname)).toEqual({ movieId: null, about: true, daily: false });
  });

  it('is pushed from a map too, leaving the map behind', () => {
    const h = at('/movie/tt0133093-the-matrix', { depth: 1, filters: freshFilters() });
    route().openAbout(click());
    expect(h.pushState).toHaveBeenCalledWith({ depth: 2, filters: freshFilters() }, '', '/about');
    expect(routeAt(location.pathname)).toEqual({ movieId: null, about: true, daily: false });
  });

  it('leaves a click with a modifier, or not with the main button, to the browser', () => {
    for (const over of [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }]) {
      const h = at('/', { depth: 0, filters: freshFilters() });
      const e = click(over);
      route().openAbout(e);
      expect(e.preventDefault, JSON.stringify(over)).not.toHaveBeenCalled();
      expect(h.pushState, JSON.stringify(over)).not.toHaveBeenCalled();
    }
  });

  it('is not pushed again from the About page itself', () => {
    const h = at('/about', { depth: 1, filters: freshFilters() });
    route().openAbout(click());
    expect(h.pushState).not.toHaveBeenCalled();
  });

  it('has a Back that is the browser’s back, labelled Back', () => {
    const h = at('/about', { depth: 1, filters: freshFilters() });
    const r = route();
    expect(r.about).toBe(true);
    expect(r.movieId).toBeNull();
    expect(r.canGoBack).toBe(true);
    r.goBack();
    expect(h.back).toHaveBeenCalledTimes(1);
    expect(h.pushState).not.toHaveBeenCalled();
    // And the header draws it so.
    expect(page()).toMatch(/<button type="button" class="cd-back" aria-label="Back">/);
  });

  it('has no Back when it was opened straight from a link', () => {
    const h = at('/about', null);
    const r = route();
    expect(r.canGoBack).toBe(false);
    r.goBack();
    expect(h.back).not.toHaveBeenCalled();
    expect(page()).not.toContain('cd-back');
  });

  it('is left for home by the wordmark, which pushes /', () => {
    const h = at('/about', { depth: 1, filters: freshFilters() });
    const e = click();
    route().goHome(e);
    expect(e.preventDefault).toHaveBeenCalled();
    expect(h.pushState).toHaveBeenCalledWith({ depth: 2, filters: freshFilters() }, '', '/');
    expect(routeAt(location.pathname)).toEqual({ movieId: null, about: false, daily: false });
    // Keeping the query and hash, as the wordmark does from a map.
    const g = at('/about?device=phone#x', { depth: 1, filters: freshFilters() });
    route().goHome(click());
    expect(g.pushState).toHaveBeenCalledWith({ depth: 2, filters: freshFilters() }, '', '/?device=phone#x');
    expect(routeAt(location.pathname)).toEqual({ movieId: null, about: false, daily: false });
  });

  it('is left for a map picked from its search, which is pushed one deeper with the filters clear', () => {
    const h = at('/about', { depth: 1, filters: freshFilters() });
    const adopt = { current: vi.fn() };
    route(adopt).openMovie('tt0133093', 'The Matrix');
    expect(h.pushState).toHaveBeenCalledWith(
      { depth: 2, filters: freshFilters() },
      '',
      '/movie/tt0133093-the-matrix',
    );
    expect(adopt.current).toHaveBeenCalledWith(freshFilters());
    expect(routeAt(location.pathname)).toEqual({ movieId: 'tt0133093', about: false, daily: false });
  });

  it('is reached at /about/ too, and nowhere under it', () => {
    at('/about/', null);
    expect(route().about).toBe(true);
    at('/about/team', null);
    expect(route().about).toBe(false);
  });

  it('draws the About page under the whole header: the mark, the word and the search', () => {
    at('/about', null);
    const html = page();
    expect(html).toContain('<main class="cd-about"');
    expect(html).not.toContain('cd-cold');
    // Whole from the first paint, as on a map: the opening's mark
    // belongs to the opening screen.
    expect(html).toMatch(/<span class="cd-wordmark-slot" aria-hidden="true"><svg class="cd-mark"/);
    expect(html).toContain('cd-wordmark-word cd-wordmark-word-in');
    expect(html).toContain('aria-label="Search for a movie"');
  });
});

describe('the Daily’s route', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is pushed by the banner, one entry deeper, with the filters clear', () => {
    const h = at('/', { depth: 2, filters: freshFilters() });
    const adopt = { current: vi.fn() };
    const e = click();
    route(adopt).openDaily(e);
    expect(e.preventDefault).toHaveBeenCalled();
    expect(h.pushState).toHaveBeenCalledTimes(1);
    expect(h.pushState).toHaveBeenCalledWith({ depth: 3, filters: freshFilters() }, '', '/daily');
    expect(adopt.current).toHaveBeenCalledWith(freshFilters());
    expect(routeAt(location.pathname)).toEqual({ movieId: null, about: false, daily: true });
  });

  it('leaves a click with a modifier, or not with the main button, to the browser', () => {
    // So the banner can still be opened in a new tab.
    for (const over of [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }]) {
      const h = at('/', { depth: 0, filters: freshFilters() });
      const e = click(over);
      route().openDaily(e);
      expect(e.preventDefault, JSON.stringify(over)).not.toHaveBeenCalled();
      expect(h.pushState, JSON.stringify(over)).not.toHaveBeenCalled();
    }
  });

  it('is not pushed again from the Daily itself', () => {
    const h = at('/daily', { depth: 1, filters: freshFilters() });
    route().openDaily(click());
    expect(h.pushState).not.toHaveBeenCalled();
  });

  it('has a Back that is the browser’s back, labelled Back', () => {
    const h = at('/daily', { depth: 1, filters: freshFilters() });
    const r = route();
    expect(r.daily).toBe(true);
    expect(r.about).toBe(false);
    expect(r.movieId).toBeNull();
    expect(r.canGoBack).toBe(true);
    r.goBack();
    expect(h.back).toHaveBeenCalledTimes(1);
    expect(h.pushState).not.toHaveBeenCalled();
    expect(page()).toMatch(/<button type="button" class="cd-back" aria-label="Back">/);
  });

  it('has no Back when it was opened straight from a link', () => {
    at('/daily', null);
    expect(route().canGoBack).toBe(false);
    expect(page()).not.toContain('cd-back');
  });

  it('is left for home by the wordmark, which pushes /', () => {
    // The Daily has no movie in its path, and is not home either.
    const h = at('/daily', { depth: 1, filters: freshFilters() });
    const e = click();
    route().goHome(e);
    expect(e.preventDefault).toHaveBeenCalled();
    expect(h.pushState).toHaveBeenCalledWith({ depth: 2, filters: freshFilters() }, '', '/');
    expect(routeAt(location.pathname)).toEqual({ movieId: null, about: false, daily: false });
  });

  it('is left for the About page like any other page', () => {
    const h = at('/daily', { depth: 1, filters: freshFilters() });
    route().openAbout(click());
    expect(h.pushState).toHaveBeenCalledWith({ depth: 2, filters: freshFilters() }, '', '/about');
    expect(routeAt(location.pathname)).toEqual({ movieId: null, about: true, daily: false });
  });

  it('is reached at /daily/ too, and nowhere under it', () => {
    at('/daily/', null);
    expect(route().daily).toBe(true);
    at('/daily/142', null);
    expect(route().daily).toBe(false);
  });

  it('draws the Daily under the whole wordmark, its pill and How it works, with no search', () => {
    at('/daily', null);
    const html = page();
    expect(html).toContain('<div class="cd-daily"');
    expect(html).not.toContain('cd-cold');
    expect(html).not.toContain('cd-about');
    // Whole from the first paint, as on a map.
    expect(html).toMatch(/<span class="cd-wordmark-slot" aria-hidden="true"><svg class="cd-mark"/);
    expect(html).toContain('cd-wordmark-word cd-wordmark-word-in');
    // Then the pill, the room between, and the rules.
    const row = /<div class="cd-header-row">([\s\S]*?)<\/div>/.exec(html)?.[1] ?? '';
    expect(row).toMatch(
      /class="cd-wordmark"[\s\S]*<span class="cd-daily-pill">Daily<\/span><span class="cd-daily-spacer"><\/span><button type="button" class="cd-daily-help" aria-label="How it works">/,
    );
    // The date waits for the puzzle: it is not known before it loads.
    expect(row).not.toContain('cd-daily-date');
    // No search, and so none of its shortcuts; no rating filter.
    expect(html).not.toContain('aria-label="Search for a movie"');
    expect(html).not.toContain('cd-search');
    expect(html).not.toContain('cd-rating-filter');
  });

  it('keeps the word in its wordmark on a phone, where a map’s header drops it', () => {
    at('/daily', null);
    vi.stubGlobal('window', { innerWidth: 390, innerHeight: 844 });
    expect(page()).toContain('cd-wordmark-word cd-wordmark-word-in');
    at('/about', null);
    vi.stubGlobal('window', { innerWidth: 390, innerHeight: 844 });
    expect(page()).not.toContain('cd-wordmark-word');
  });

  it('rules the header off from the board, and keeps it on the ground', () => {
    at('/daily', null);
    vi.stubGlobal('window', { innerWidth: 390, innerHeight: 844 });
    // Never lying over the page as glass: the Daily places its panel and
    // its intro against its own box, which starts under the header.
    const header = /<header class="([^"]*)"/.exec(page())?.[1];
    expect(header).toBe('cd-header cd-header-map');
  });
});

describe('the Daily held under a map', () => {
  it('steps back behind the progress line while the map loads, as the About page does', () => {
    // Forward from the Daily to a map that is not in hand yet: drawsPage
    // keeps the Daily drawn, and GridApp passes it the load as `dim`.
    expect(renderToStaticMarkup(createElement(DailyPage, { dim: true }))).toMatch(
      /^<div class="cd-daily cd-daily-dim">/,
    );
    expect(renderToStaticMarkup(createElement(DailyPage, {}))).toMatch(/^<div class="cd-daily">/);
  });
});

describe('pageOf', () => {
  it('names the page without a map that a route is on', () => {
    expect(pageOf(false, false)).toBe('cold');
    expect(pageOf(true, false)).toBe('about');
    expect(pageOf(false, true)).toBe('daily');
  });
});

describe('drawsPage', () => {
  it('follows the route while there is no movie', () => {
    expect(drawsPage(null, 'about', 'cold')).toBe('about');
    expect(drawsPage(null, 'cold', 'about')).toBe('cold');
    expect(drawsPage(null, 'daily', 'cold')).toBe('daily');
    expect(drawsPage(null, 'cold', 'daily')).toBe('cold');
    expect(drawsPage(null, 'about', 'daily')).toBe('about');
  });

  it('holds what was drawn while a picked map loads over it', () => {
    expect(drawsPage('tt0133093', 'cold', 'about')).toBe('about');
    expect(drawsPage('tt0133093', 'cold', 'cold')).toBe('cold');
    // Forward from the Daily to a map: the Daily stays, dimmed, until the
    // map arrives, as the About page does.
    expect(drawsPage('tt0133093', 'cold', 'daily')).toBe('daily');
  });
});

describe('skipsOpening', () => {
  it('plays the opening only for a visit that begins on the opening screen', () => {
    expect(skipsOpening('/')).toBe(false);
    expect(skipsOpening('/about')).toBe(true);
    expect(skipsOpening('/about/')).toBe(true);
    expect(skipsOpening('/daily')).toBe(true);
    expect(skipsOpening('/daily/')).toBe(true);
    expect(skipsOpening('/movie/tt0133093')).toBe(true);
    expect(skipsOpening('/movie/tt0133093-the-matrix')).toBe(true);
    // Not a page: the opening screen, which plays it.
    expect(skipsOpening('/daily/142')).toBe(false);
  });
});

describe('backLabel', () => {
  const none = { movieId: null, about: false, daily: false };

  it('is Back on the About page and the Daily, and names the movie trail on a map, once there is somewhere to go', () => {
    expect(backLabel({ ...none, about: true, canGoBack: true })).toBe('Back');
    expect(backLabel({ ...none, daily: true, canGoBack: true })).toBe('Back');
    expect(backLabel({ ...none, movieId: 'tt0133093', canGoBack: true })).toBe('Back to the previous movie');
  });

  it('is nothing on the opening screen, or with nothing behind', () => {
    expect(backLabel({ ...none, canGoBack: true })).toBeNull();
    expect(backLabel({ ...none, about: true, canGoBack: false })).toBeNull();
    expect(backLabel({ ...none, daily: true, canGoBack: false })).toBeNull();
    expect(backLabel({ ...none, movieId: 'tt0133093', canGoBack: false })).toBeNull();
  });
});

describe('the opening screen’s Daily banner', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('comes first, before the headline, as a link to /daily', () => {
    at('/', null);
    const cold = /<div class="cd-cold[^"]*">([\s\S]*)$/.exec(page())?.[1] ?? '';
    expect(cold.startsWith('<a class="cd-daily-banner')).toBe(true);
    expect(cold.indexOf('cd-daily-banner')).toBeLessThan(cold.indexOf('cd-cold-head'));
    expect(cold).toMatch(/<a class="cd-daily-banner[^"]*" href="\/daily"/);
  });

  it('is drawn whole from the first paint, before today’s puzzle has answered', () => {
    // The grid of films is measured under it, so its box cannot wait.
    at('/', null);
    const banner = /<a class="cd-daily-banner[\s\S]*?<\/a>/.exec(page())?.[0] ?? '';
    expect(banner).toContain('cd-daily-banner-waiting');
    expect(banner).toContain('<span class="cd-daily-pill">Daily</span>');
    expect(banner).toContain('<span class="cd-daily-banner-title">Whose map is it?</span>');
    expect(banner).toContain('aria-label="Cinedikt Daily: Whose map is it?"');
    // No number and no line about today until the server says.
    expect(banner).not.toContain('cd-daily-banner-meta');
    expect(banner).not.toContain('cd-daily-banner-sub');
  });

  it('is not on the About page or a map', () => {
    at('/about', null);
    expect(page()).not.toContain('cd-daily-banner');
  });
});

describe('the opening screen’s footer', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('holds no TMDB notice: the credits are on the About page', () => {
    at('/', null);
    const html = page();
    expect(html).toContain('class="cd-cold');
    expect(html).not.toMatch(/TMDB|themoviedb|tmdb/i);
  });

  it('has the About link, then the email, and nothing else', () => {
    at('/', null);
    const foot = /<footer class="cd-cold-foot">([\s\S]*?)<\/footer>/.exec(page())?.[1] ?? '';
    const links = [...foot.matchAll(/<a ([^>]*)>([\s\S]*?)<\/a>/g)].map((m) => ({
      attrs: m[1],
      text: m[2].replace(/<[^>]+>/g, '').trim(),
    }));
    expect(links.map((l) => l.text)).toEqual(['About', 'hello@cinedikt.com']);
    expect(links[0].attrs).toContain('href="/about"');
    expect(links[1].attrs).toContain('href="mailto:hello@cinedikt.com"');
    // One box for both, the email's as it was.
    for (const l of links) expect(l.attrs).toContain('class="cd-foot-link"');
    // Nothing but the two links: no notice, no logo.
    expect(foot.replace(/<a [\s\S]*?<\/a>/g, '').trim()).toBe('');
  });

  it('draws the About link’s info circle: a ring, a stem and a dot', () => {
    at('/', null);
    const about = /<a class="cd-foot-link" href="\/about">([\s\S]*?)<\/a>/.exec(page())?.[1] ?? '';
    expect(about).toContain('width="16" height="16"');
    expect(about).toContain('stroke-width="2" stroke-linecap="round" stroke-linejoin="round"');
    expect(about).toContain('<circle cx="12" cy="12" r="9"></circle>');
    expect(about).toContain('<path d="M12 11v5.5"></path>');
    expect(about).toContain('<path d="M12 7.6h.01" stroke-width="2.6"></path>');
  });
});
