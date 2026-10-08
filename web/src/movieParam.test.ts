import { describe, expect, it } from 'vitest';
import {
  ABOUT_PATH,
  DAILY_PATH,
  DAILY_TITLE,
  filmPath,
  isAboutPath,
  isDailyPath,
  isImdbId,
  movieIdFromPath,
  HOME_TITLE,
  pageTitle,
  routeAt,
  slugify,
} from './movieParam';

describe('isImdbId', () => {
  it('accepts an IMDb title id as a title and nothing else', () => {
    expect(isImdbId('tt0133093', 'tt')).toBe(true);
    expect(isImdbId('tt1', 'tt')).toBe(true);
    // The old TMDb ids are not addresses any more.
    expect(isImdbId('603', 'tt')).toBe(false);
    expect(isImdbId('nm0000206', 'tt')).toBe(false);
    expect(isImdbId('tt', 'tt')).toBe(false);
    expect(isImdbId('ttabc', 'tt')).toBe(false);
    expect(isImdbId('tt0133093x', 'tt')).toBe(false);
    expect(isImdbId('TT0133093', 'tt')).toBe(false);
    expect(isImdbId('', 'tt')).toBe(false);
  });

  it('accepts an IMDb name id as a person and nothing else', () => {
    expect(isImdbId('nm0000206', 'nm')).toBe(true);
    expect(isImdbId('tt0133093', 'nm')).toBe(false);
    expect(isImdbId('nm', 'nm')).toBe(false);
    expect(isImdbId('nm-1', 'nm')).toBe(false);
    expect(isImdbId('NM0000206', 'nm')).toBe(false);
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
    expect(movieIdFromPath('/movie/nm0000206')).toBeNull();
    expect(movieIdFromPath('/movie/tt0133093x-the-matrix')).toBeNull();
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
  });
});

describe('isDailyPath', () => {
  it('is the Daily at /daily and /daily/, and only there', () => {
    expect(DAILY_PATH).toBe('/daily');
    expect(isDailyPath('/daily')).toBe(true);
    expect(isDailyPath('/daily/')).toBe(true);
  });

  it('is not anything else', () => {
    // The puzzle is always today's: a number under it names nothing.
    for (const path of ['/', '', '/daily/142', '/daily//', '/dailys', '/Daily', '/about', '/x/daily']) {
      expect(isDailyPath(path), path).toBe(false);
    }
  });

  it('is not a movie route or the About page either', () => {
    expect(movieIdFromPath('/daily')).toBeNull();
    expect(isAboutPath('/daily')).toBe(false);
  });
});

describe('routeAt', () => {
  it('reads one of the four routes from a path', () => {
    expect(routeAt('/')).toEqual({ movieId: null, about: false, daily: false });
    expect(routeAt('/about')).toEqual({ movieId: null, about: true, daily: false });
    expect(routeAt('/about/')).toEqual({ movieId: null, about: true, daily: false });
    expect(routeAt('/daily')).toEqual({ movieId: null, about: false, daily: true });
    expect(routeAt('/daily/')).toEqual({ movieId: null, about: false, daily: true });
    expect(routeAt('/movie/tt0133093-the-matrix')).toEqual({
      movieId: 'tt0133093',
      about: false,
      daily: false,
    });
  });

  it('takes anything else for the opening screen', () => {
    for (const path of ['/about/team', '/daily/142', '/movie/nope', '/film/tt0133093', '']) {
      expect(routeAt(path), path).toEqual({ movieId: null, about: false, daily: false });
    }
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
    expect(pageTitle(undefined, 'about')).toBe('About · Cinedikt');
    // Whatever map was last open: the About page names itself.
    expect(pageTitle('The Matrix', 'about')).toBe('About · Cinedikt');
    expect(pageTitle('The Matrix', undefined)).toBe(
      'The Matrix — everything its cast and directors made · Cinedikt',
    );
  });

  it('is Daily · Cinedikt on the Daily, whatever map was last open', () => {
    expect(DAILY_TITLE).toBe('Daily · Cinedikt');
    expect(pageTitle(undefined, 'daily')).toBe('Daily · Cinedikt');
    expect(pageTitle('The Matrix', 'daily')).toBe('Daily · Cinedikt');
    // The tab says "movie" wherever it says anything of the kind, and
    // this one says neither: it never names today's answer.
    expect(pageTitle('The Matrix', 'daily')).not.toMatch(/film|Matrix/i);
  });

  it('matches the tagline the page is served with', () => {
    expect(HOME_TITLE).toBe('Cinedikt — a movie’s cast and directors, and everything they made');
  });
});
