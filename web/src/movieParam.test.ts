import { describe, expect, it } from 'vitest';
import {
  ABOUT_PATH,
  filmHref,
  filmPath,
  isAboutPath,
  movieIdFrom,
  movieIdFromPath,
  movieIdFromState,
  HOME_TITLE,
  pageTitle,
  routeAt,
  routeFrom,
  slugify,
} from './movieParam';

describe('movieIdFrom', () => {
  it('accepts an IMDb title id and nothing else', () => {
    expect(movieIdFrom('tt0133093')).toBe('tt0133093');
    expect(movieIdFrom('tt1')).toBe('tt1');
    // The old TMDb ids are not addresses any more.
    expect(movieIdFrom('603')).toBeNull();
    expect(movieIdFrom('nm0000206')).toBeNull();
    expect(movieIdFrom('tt')).toBeNull();
    expect(movieIdFrom('ttabc')).toBeNull();
    expect(movieIdFrom('TT0133093')).toBeNull();
    expect(movieIdFrom(null)).toBeNull();
  });
});

describe('movieIdFromState', () => {
  it('reads the id a pushState left behind', () => {
    expect(movieIdFromState({ movie: 'tt0133093' })).toBe('tt0133093');
    expect(movieIdFromState({ movie: 603 })).toBeNull();
    expect(movieIdFromState({ movie: 'x' })).toBeNull();
    expect(movieIdFromState(null)).toBeNull();
  });
});

describe('slugify', () => {
  it('makes a title safe to paste into a URL', () => {
    expect(slugify('The Matrix')).toBe('the-matrix');
    expect(slugify("Ocean's Eleven")).toBe('oceans-eleven');
    expect(slugify('Amélie')).toBe('amelie');
    // NFKD keeps the digits of a fraction rather than dropping them: ugly,
    // but the id is what the route reads and nothing is lost.
    expect(slugify('9½ Weeks!')).toBe('91-2-weeks');
    expect(slugify('WALL·E')).toBe('wall-e');
  });
  it('never ends in a hyphen, even when truncated', () => {
    expect(slugify('a'.repeat(58) + ' bb')).not.toMatch(/-$/);
  });
});

describe('filmPath', () => {
  it('is id plus slug, and id alone before the title is known', () => {
    expect(filmPath('tt0133093', 'The Matrix')).toBe('/movie/tt0133093-the-matrix');
    expect(filmPath('tt0133093')).toBe('/movie/tt0133093');
  });
});

describe('movieIdFromPath', () => {
  it('reads the id whatever the slug says', () => {
    expect(movieIdFromPath('/movie/tt0133093-the-matrix')).toBe('tt0133093');
    expect(movieIdFromPath('/movie/tt0133093')).toBe('tt0133093');
    expect(movieIdFromPath('/movie/tt0133093-anything-at-all/')).toBe('tt0133093');
  });
  it('rejects anything that is not a movie route', () => {
    expect(movieIdFromPath('/')).toBeNull();
    expect(movieIdFromPath('/movie/')).toBeNull();
    expect(movieIdFromPath('/movie/abc')).toBeNull();
    expect(movieIdFromPath('/movies/tt0133093')).toBeNull();
    expect(movieIdFromPath('/film/tt0133093')).toBeNull();
  });
});

describe('isAboutPath', () => {
  it('is the About page at /about and /about/, and only there', () => {
    expect(ABOUT_PATH).toBe('/about');
    expect(isAboutPath('/about')).toBe(true);
    expect(isAboutPath('/about/')).toBe(true);
  });

  it('is not anything else', () => {
    for (const path of ['/', '', '/about/team', '/about//', '/aboutus', '/About', '/movie/tt0133093', '/x/about']) {
      expect(isAboutPath(path), path).toBe(false);
    }
  });

  it('is not a movie route either', () => {
    expect(movieIdFromPath('/about')).toBeNull();
    expect(routeFrom('https://x.test/about')).toEqual({ movieId: null, path: '/about' });
  });
});

describe('routeAt', () => {
  it('reads one of the three routes from a path', () => {
    expect(routeAt('/')).toEqual({ movieId: null, about: false });
    expect(routeAt('/about')).toEqual({ movieId: null, about: true });
    expect(routeAt('/about/')).toEqual({ movieId: null, about: true });
    expect(routeAt('/movie/tt0133093-the-matrix')).toEqual({ movieId: 'tt0133093', about: false });
  });

  it('takes anything else for the opening screen', () => {
    for (const path of ['/about/team', '/movie/nope', '/film/tt0133093', '']) {
      expect(routeAt(path), path).toEqual({ movieId: null, about: false });
    }
  });
});

describe('routeFrom', () => {
  it('keeps a movie route as it is', () => {
    expect(routeFrom('https://x.test/movie/tt0133093-the-matrix')).toEqual({
      movieId: 'tt0133093',
      path: '/movie/tt0133093-the-matrix',
    });
  });
  it('leaves a cold start alone', () => {
    expect(routeFrom('https://x.test/?device=phone')).toEqual({
      movieId: null,
      path: '/?device=phone',
    });
  });
});

describe('filmHref', () => {
  it('moves to the movie path and keeps the query', () => {
    expect(filmHref('tt0137523', 'Fight Club', 'https://x.test/movie/tt0133093-the-matrix?device=phone')).toBe(
      '/movie/tt0137523-fight-club?device=phone',
    );
  });
});

describe('pageTitle', () => {
  it('names the movie the map is of', () => {
    expect(pageTitle('The Matrix')).toBe(
      'The Matrix — everything its cast and directors made · Cinedikt',
    );
  });

  it('falls back to the tagline before a title is known, and on first run', () => {
    expect(pageTitle()).toBe(HOME_TITLE);
    expect(pageTitle(undefined)).toBe(HOME_TITLE);
    expect(pageTitle('')).toBe(HOME_TITLE);
    // A title that is only spaces would leave the tab reading " — everything…".
    expect(pageTitle('   ')).toBe(HOME_TITLE);
  });

  it('is About · Cinedikt on the About page', () => {
    expect(pageTitle(undefined, true)).toBe('About · Cinedikt');
    // Whatever map was last open: the About page names itself.
    expect(pageTitle('The Matrix', true)).toBe('About · Cinedikt');
    expect(pageTitle('The Matrix', false)).toBe(
      'The Matrix — everything its cast and directors made · Cinedikt',
    );
  });

  it('matches the tagline the page is served with', () => {
    expect(HOME_TITLE).toBe('Cinedikt — a movie’s cast and directors, and everything they made');
  });
});
