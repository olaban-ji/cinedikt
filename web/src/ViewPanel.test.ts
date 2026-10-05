import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import matrix from './fixtures/matrix-grid.json';
import {
  DEFAULT_SETTINGS,
  toggleGenre,
  type GridPayload,
  type GridSettings,
  type SpineTuple,
} from './grid';
import { GenreSection, ViewPanel } from './ViewPanel';

/** The legend the server sends: IMDb's genres a mapped film can carry,
 *  in IMDb's alphabetical order. Bit i is LEGEND[i]. */
const LEGEND = [
  'Action', 'Adventure', 'Animation', 'Biography', 'Comedy', 'Crime', 'Drama',
  'Family', 'Fantasy', 'Film-Noir', 'History', 'Horror', 'Music', 'Musical',
  'Mystery', 'Romance', 'Sci-Fi', 'Sport', 'Thriller', 'War', 'Western',
];
const bitsOf = (...names: string[]) =>
  names.reduce((bits, n) => bits | (1 << LEGEND.indexOf(n)), 0);

/** A map with genres on its spine. Nothing on it is a Western, and its
 *  one Drama shares no genre with the Action films. */
function mapped(): GridPayload {
  const films: SpineTuple[] = [
    ['tt0133093', 1999, 8.7, 331, [0], bitsOf('Action', 'Sci-Fi')],
    ['tt0234215', 2003, 7.2, 515, [0], bitsOf('Action', 'Sci-Fi')],
    ['tt0242653', 2003, 6.7, 1105, [0], bitsOf('Action', 'Sci-Fi')],
    ['tt0111257', 1994, 7.3, 610, [0], bitsOf('Action', 'Crime', 'Thriller')],
    ['tt0000010', 2001, 7.0, 0, [0], bitsOf('Drama', 'Romance')],
    ['tt0000011', 2005, null, 0, [0], bitsOf('Comedy')],
    ['tt0000012', 1985, 6.0, 0, [0], bitsOf('Horror')],
  ];
  return {
    anchor: { id: 'tt0133093', title: 'The Matrix', year: 1999, rating: 8.7, md: 331, people: [], isAnchor: true },
    people: [{ id: 'nm0000206', name: 'Keanu Reeves', role: 'cast' }],
    films,
    genres: LEGEND,
  };
}

const settings = (over: Partial<GridSettings> = {}): GridSettings => ({ ...DEFAULT_SETTINGS, ...over });

function section(s: GridSettings, payload = mapped()): string {
  return renderToStaticMarkup(createElement(GenreSection, { payload, settings: s, onChange: () => {} }));
}

interface Toggle {
  name: string;
  count: string | null;
  label: string;
  pressed: boolean;
  disabled: boolean;
  on: boolean;
}

/** The section's toggles, as the markup draws them. */
function toggles(html: string): Toggle[] {
  return [...html.matchAll(/<button([^>]*)>(.*?)<\/button>/g)]
    .filter(([, attrs]) => /class="cd-genre[ "]/.test(attrs))
    .map(([, attrs, inner]) => ({
      name: /<span class="cd-genre-name">([^<]*)<\/span>/.exec(inner)?.[1] ?? '',
      count: /<span class="cd-genre-n">([^<]*)<\/span>/.exec(inner)?.[1] ?? null,
      label: /aria-label="([^"]*)"/.exec(attrs)?.[1] ?? '',
      pressed: /aria-pressed="true"/.test(attrs),
      disabled: /\sdisabled=""/.test(attrs),
      on: /class="cd-genre cd-genre-on"/.test(attrs),
    }));
}

const toggle = (html: string, name: string) => toggles(html).find((t) => t.name === name)!;

/** The Clear button's opening tag. */
function clear(html: string): string {
  return /<button[^>]*class="cd-link cd-genres-clear"[^>]*>/.exec(html)?.[0] ?? '';
}

describe('the Genres section', () => {
  it('draws all 21 genres, in the legend’s order', () => {
    const drawn = toggles(section(settings()));
    expect(drawn.map((t) => t.name)).toEqual(LEGEND);
    expect(drawn).toHaveLength(21);
  });

  it('heads them with its name, the hint and Clear', () => {
    const html = section(settings());
    expect(html).toContain('<span class="cd-view-heading">Genres</span>');
    expect(html).toContain('<span class="cd-genres-hint">Lights movies with all you pick</span>');
    expect(html).toContain('role="group" aria-label="Genres"');
  });

  it('greys out a genre no film on the map has, with no count', () => {
    const western = toggle(section(settings()), 'Western');
    expect(western).toMatchObject({ disabled: true, count: null, pressed: false });
    expect(western.label).toBe('Western, none on this map');
  });

  it('counts the films on the plot with each genre, the searched film included', () => {
    const html = section(settings());
    expect(toggle(html, 'Sci-Fi')).toMatchObject({ count: '3', label: 'Sci-Fi, 3 movies', disabled: false });
    expect(toggle(html, 'Action')).toMatchObject({ count: '4', label: 'Action, 4 movies' });
    expect(toggle(html, 'Horror')).toMatchObject({ count: '1', label: 'Horror, 1 movie' });
  });

  it('greys out, after Action is picked, a genre no Action film shares', () => {
    const html = section(settings({ genres: ['Action'] }));
    expect(toggle(html, 'Action')).toMatchObject({ pressed: true, on: true, disabled: false, count: '4' });
    // Sci-Fi now counts the films that are Action and Sci-Fi.
    expect(toggle(html, 'Sci-Fi')).toMatchObject({ count: '3', disabled: false });
    expect(toggle(html, 'Thriller')).toMatchObject({ count: '1', label: 'Thriller, 1 movie' });
    const drama = toggle(html, 'Drama');
    expect(drama).toMatchObject({ disabled: true, count: null, pressed: false });
    expect(drama.label).toBe('Drama, none with the genres picked');
    // A genre nothing has is still said to be nowhere on the map.
    expect(toggle(html, 'Western').label).toBe('Western, none on this map');
  });

  it('counts what the year range and the unrated column leave on the plot', () => {
    // Horror is 1985's, before the range.
    expect(toggle(section(settings({ yearFrom: 1990 })), 'Horror')).toMatchObject({
      disabled: true,
      label: 'Horror, none on this map',
    });
    // The only Comedy is unrated.
    expect(toggle(section(settings({ showUnrated: false })), 'Comedy')).toMatchObject({ disabled: true });
    // The searched film stays on the plot whatever the range.
    expect(toggle(section(settings({ yearFrom: 2003 })), 'Sci-Fi')).toMatchObject({ count: '3' });
  });

  it('counts the same whatever the floor and the people are lighting', () => {
    const plain = toggles(section(settings()));
    expect(toggles(section(settings({ minRating: 8.5 })))).toEqual(plain);
  });

  it('never greys out a genre that is picked, so it can always be unpicked', () => {
    const html = section(settings({ genres: ['Horror'], yearFrom: 1990 }));
    expect(toggle(html, 'Horror')).toMatchObject({ pressed: true, disabled: false, count: null });
  });

  it('shows Clear only while something is picked', () => {
    expect(clear(section(settings()))).toContain('visibility:hidden');
    expect(clear(section(settings()))).toContain('tabindex="-1"');
    expect(clear(section(settings({ genres: ['Action'] })))).not.toContain('visibility');
    expect(clear(section(settings({ genres: ['Action'] })))).toContain('aria-label="Clear genres"');
  });

  it('is not drawn for a map without a legend', () => {
    const noLegend = matrix as unknown as GridPayload;
    expect(section(settings(), noLegend)).toBe('');
    expect(section(settings(), { ...mapped(), genres: undefined })).toBe('');
  });
});

describe('the View panel', () => {
  const panel = (payload: GridPayload | null) =>
    renderToStaticMarkup(
      createElement(ViewPanel, {
        settings: settings(),
        onChange: () => {},
        onRelaid: () => {},
        rungs: false,
        bounds: { lo: 1985, hi: 2005 },
        perYear: new Map(),
        anchorYear: 1999,
        rangeEmpty: false,
        payload,
        onFloor: () => {},
        theme: 'system',
        onTheme: () => {},
        onClose: () => {},
      }),
    );

  it('puts the genres after Years and before the switches', () => {
    const html = panel(mapped());
    const years = html.indexOf('cd-years');
    const genres = html.indexOf('cd-genres');
    const switches = html.indexOf('cd-view-switches');
    expect(years).toBeGreaterThan(-1);
    expect(genres).toBeGreaterThan(years);
    expect(switches).toBeGreaterThan(genres);
  });

  it('has no Genres section on a map without a legend', () => {
    expect(panel(matrix as unknown as GridPayload)).not.toContain('cd-genres');
    expect(panel(null)).not.toContain('cd-genres');
  });
});

describe('toggleGenre', () => {
  it('keeps picks in the order they were made, and unpicks', () => {
    const one = toggleGenre(settings(), 'Sci-Fi');
    const two = toggleGenre(one, 'Action');
    expect(two.genres).toEqual(['Sci-Fi', 'Action']);
    expect(toggleGenre(two, 'Sci-Fi').genres).toEqual(['Action']);
    // The settings it was given are left as they were.
    expect(one.genres).toEqual(['Sci-Fi']);
  });
});
