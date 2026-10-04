import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AboutPage, enterAbout } from './AboutPage';

const html = () => renderToStaticMarkup(createElement(AboutPage, { dim: false }));

/** What an element's markup says, tags gone. */
const text = (markup: string) => markup.replace(/<[^>]+>/g, '');

/** Every link on the page, with the attributes that decide where it
 *  goes and how. */
function links(markup: string) {
  return [...markup.matchAll(/<a ([^>]*)>([\s\S]*?)<\/a>/g)].map((m) => {
    const attr = (name: string) => new RegExp(`\\b${name}="([^"]*)"`).exec(m[1])?.[1];
    return {
      href: attr('href'),
      target: attr('target'),
      rel: attr('rel'),
      label: attr('aria-label'),
      text: text(m[2]),
    };
  });
}

// The sources' own words. IMDb's statement and MaxMind's example are
// required word for word, and the rest are what the sources ask for, so
// a change to any of them is a change to what the app has agreed to.
const NOTICES = [
  'Information courtesy of IMDb (https://www.imdb.com). Used with permission.',
  'OMDb’s content is licensed under CC BY-NC 4.0.',
  'This product uses the TMDB API but is not endorsed or certified by TMDB.',
  'Streaming availability information is provided by Streaming Availability API by Movie of the Night.',
  'This product includes GeoLite Data created by MaxMind, available from https://www.maxmind.com.',
];

describe('the About page', () => {
  it('gives every source’s notice as written, one credit each, in order', () => {
    const credits = [...html().matchAll(/<li class="cd-credit">([\s\S]*?)<\/li>/g)].map((m) => m[1]);
    expect(credits).toHaveLength(5);
    const notices = credits.map((c) => text(/<p class="cd-credit-notice">([\s\S]*?)<\/p>/.exec(c)?.[1] ?? ''));
    expect(notices).toEqual(NOTICES);
  });

  it('links each source, and OMDb’s licence, in a new tab', () => {
    const out = links(html()).filter((l) => !l.href?.startsWith('mailto:'));
    expect(out.map((l) => l.href)).toEqual([
      'https://www.imdb.com',
      'https://www.omdbapi.com',
      'https://creativecommons.org/licenses/by-nc/4.0/',
      'https://www.themoviedb.org',
      'https://www.movieofthenight.com/about/api',
      'https://www.maxmind.com',
    ]);
    for (const l of out) {
      expect(l.target, l.href).toBe('_blank');
      expect(l.rel, l.href).toBe('noopener noreferrer');
    }
  });

  it('names each source in its link, and says aloud that it leaves', () => {
    const named = links(html()).filter((l) => l.label);
    expect(named.map((l) => l.text)).toEqual([
      'IMDb',
      'OMDb APIThe Open Movie Database',
      // TMDB is its logo, which only the label names.
      '',
      'Streaming Availability APIby Movie of the Night',
      'GeoLiteby MaxMind',
    ]);
    expect(named.map((l) => l.label)).toEqual([
      'IMDb, opens in a new tab',
      'OMDb API The Open Movie Database, opens in a new tab',
      'TMDB, opens in a new tab',
      'Streaming Availability API by Movie of the Night, opens in a new tab',
      'GeoLite by MaxMind, opens in a new tab',
    ]);
    expect(html()).toContain('<span class="cd-about-tmdb-logo" aria-hidden="true"></span>');
  });

  it('is headed and laid out as the design has it: intro, credits, footer', () => {
    const page = html();
    const main = /^<main class="cd-about" aria-labelledby="([^"]+)">/.exec(page);
    expect(main).not.toBeNull();
    expect(page).toContain(`<h1 id="${main?.[1]}" class="cd-about-head">About Cinedikt</h1>`);
    const section = /<section class="cd-about-credits" aria-labelledby="([^"]+)">/.exec(page);
    expect(page).toContain(`<h2 id="${section?.[1]}" class="cd-about-credits-head">Credits</h2>`);
    // The three parts that arrive in turn are the page's own children,
    // the email last.
    expect(page).toMatch(
      /^<main [^>]*><div class="cd-about-intro">[\s\S]*<\/div><section [^>]*>[\s\S]*<\/section><footer class="cd-about-foot"><a class="cd-foot-link" href="mailto:hello@cinedikt\.com">[\s\S]*hello@cinedikt\.com<\/a><\/footer><\/main>$/,
    );
  });

  it('says what Cinedikt is, in the app’s words', () => {
    const intro = text(/<div class="cd-about-intro">([\s\S]*?)<\/div>/.exec(html())?.[1] ?? '');
    expect(intro).toContain(
      'Cinedikt starts with a movie you love and shows every movie its cast and directors made, arranged by year and IMDb rating.',
    );
    expect(intro).toContain(
      'Years run down the side and ratings run across, on the same scale on every map, so a 7.4 always sits in the same place. Pick someone from the row of people to light only their movies, or open any movie to map it next.',
    );
    // Cinedikt's copy says movie.
    expect(intro).not.toMatch(/\bfilms?\b/i);
  });

  it('steps back while a map loads from it', () => {
    expect(renderToStaticMarkup(createElement(AboutPage, { dim: true }))).toMatch(/^<main class="cd-about cd-about-dim"/);
    expect(html()).toMatch(/^<main class="cd-about"/);
  });
});

/** A part of the page, recording how it is asked to move. */
function part() {
  return { animate: vi.fn(() => ({ cancel: vi.fn() })) } as unknown as Element & {
    animate: ReturnType<typeof vi.fn>;
  };
}

/** A browser answering the reduced-motion question with `still`. */
function prefersStill(still: boolean) {
  vi.stubGlobal('window', {
    matchMedia: (q: string) => ({ matches: still && q === '(prefers-reduced-motion: reduce)' }),
  });
}

describe('the About page arriving', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fades each part up 8px in turn, 80ms apart, over 520ms on the settle curve', () => {
    prefersStill(false);
    const parts = [part(), part(), part()];
    expect(enterAbout(parts)).toHaveLength(3);
    parts.forEach((p, i) => {
      expect(p.animate).toHaveBeenCalledTimes(1);
      expect(p.animate).toHaveBeenCalledWith(
        [
          { opacity: 0, transform: 'translateY(8px)' },
          { opacity: 1, transform: 'none' },
        ],
        { duration: 520, delay: i * 80, easing: 'cubic-bezier(0.16, 1, 0.3, 1)', fill: 'backwards' },
      );
    });
  });

  it('does not move at all for a reader who has asked for no motion', () => {
    prefersStill(true);
    const parts = [part(), part(), part()];
    expect(enterAbout(parts)).toEqual([]);
    for (const p of parts) expect(p.animate).not.toHaveBeenCalled();
  });
});
