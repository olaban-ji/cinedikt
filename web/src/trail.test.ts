import { describe, expect, it } from 'vitest';
import { DEFAULT_SETTINGS } from './grid';
import {
  applyFilters,
  filtersFromState,
  filtersOf,
  forwardEntry,
  freshFilters,
  preferencesFrom,
  stampFilters,
  viewPrefs,
} from './trail';

const narrowed = {
  people: ['nm0000206'],
  minRating: 7.5,
  yearFrom: 1999,
  yearTo: 2010,
  hideEmptyYears: true,
  genres: ['Sci-Fi', 'Action'],
};

describe('freshFilters', () => {
  it('starts a newly opened movie clear', () => {
    expect(freshFilters()).toEqual({
      people: [],
      minRating: null,
      yearFrom: null,
      yearTo: null,
      hideEmptyYears: false,
      genres: [],
    });
  });

  it('does not share its lists between movies', () => {
    const a = freshFilters();
    a.people.push('nm0000001');
    a.genres.push('Drama');
    expect(freshFilters().people).toEqual([]);
    expect(freshFilters().genres).toEqual([]);
  });
});

describe('opening another movie', () => {
  it('stores a clear map and leaves the one behind untouched', () => {
    const left = { depth: 1, filters: narrowed };
    const opened = forwardEntry(2);
    expect(opened).toEqual({ depth: 2, filters: freshFilters() });
    expect(filtersFromState(left)).toEqual(narrowed);
  });
});

describe('filtersFromState', () => {
  it('reads the filters the entry was left with', () => {
    expect(filtersFromState({ depth: 2, filters: narrowed })).toEqual(narrowed);
  });

  it('is clear when the entry never stored any', () => {
    expect(filtersFromState(null)).toEqual(freshFilters());
    expect(filtersFromState({ depth: 1 })).toEqual(freshFilters());
    expect(filtersFromState({ filters: null })).toEqual(freshFilters());
  });

  it('drops a selection that is not one the chips could have made', () => {
    expect(
      filtersFromState({
        filters: {
          people: ['nm0000206', 'nope', 3, 'nm0000206'],
          minRating: 7.2,
          yearFrom: '1999',
          yearTo: 1999.5,
          hideEmptyYears: 'yes',
          genres: ['Sci-Fi', 'sci-fi', 7, 'Sci-Fi', 'Film-Noir', '<b>', 'Drama; DROP'],
        },
      }),
    ).toEqual({ ...freshFilters(), people: ['nm0000206'], genres: ['Sci-Fi', 'Film-Noir'] });
  });

  it('keeps no more genres than IMDb has', () => {
    const many = Array.from({ length: 40 }, (_, i) => `Genre${String.fromCharCode(97 + (i % 26))}${i >= 26 ? 'x' : ''}`);
    expect(filtersFromState({ filters: { genres: many } }).genres).toHaveLength(28);
  });
});

describe('the genres', () => {
  it('start clear on a movie opened forward, and come back in pick order on Back', () => {
    // Genres picked on The Matrix, stamped on its entry as they changed.
    const prefs = preferencesFrom(null);
    const picked = applyFilters(prefs, { ...freshFilters(), genres: ['Sci-Fi', 'Action'] });
    const left = stampFilters({ depth: 1 }, filtersOf(picked, []));
    // Another movie opens clear.
    const opened = forwardEntry(2);
    expect(applyFilters(picked, filtersFromState(opened)).genres).toEqual([]);
    // Back to The Matrix puts them back, in the order they were picked.
    expect(applyFilters(prefs, filtersFromState(left)).genres).toEqual(['Sci-Fi', 'Action']);
  });

  it('are copied onto the entry, so a later pick does not rewrite history', () => {
    const genres = ['Drama'];
    const stamped = stampFilters({ depth: 1 }, { ...freshFilters(), genres });
    genres.push('War');
    expect(filtersFromState(stamped).genres).toEqual(['Drama']);
  });

  it('are never kept with the preferences', () => {
    const prefs = preferencesFrom(JSON.stringify({ yearOrder: 'newest', genres: ['Drama'] }));
    expect(prefs.genres).toEqual([]);
    expect(viewPrefs(applyFilters(prefs, narrowed))).not.toHaveProperty('genres');
  });
});

describe('stampFilters', () => {
  it('keeps the depth already on the entry', () => {
    expect(stampFilters({ depth: 4 }, narrowed)).toEqual({
      depth: 4,
      filters: narrowed,
    });
  });

  it('starts depth at zero when the entry has none', () => {
    expect(stampFilters(null, freshFilters())).toMatchObject({ depth: 0, filters: freshFilters() });
  });

  it('copies the people, so a later edit does not rewrite history', () => {
    const people = ['nm0000206'];
    const stamped = stampFilters({ depth: 1 }, { ...freshFilters(), people });
    people.push('nm0000001');
    expect(filtersFromState(stamped).people).toEqual(['nm0000206']);
  });
});

describe('preferences', () => {
  it('keeps how the map is drawn and drops filters that used to be stored with it', () => {
    const prefs = preferencesFrom(
      JSON.stringify({
        yearOrder: 'newest',
        showUnrated: false,
        highlightYear: false,
        yearFrom: 2000,
        yearTo: 2010,
        minRating: 8,
        hideEmptyYears: true,
      }),
    );
    expect(prefs.yearOrder).toBe('newest');
    expect(prefs.showUnrated).toBe(false);
    expect(prefs.highlightYear).toBe(false);
    expect(prefs.yearFrom).toBeNull();
    expect(prefs.yearTo).toBeNull();
    expect(prefs.minRating).toBeNull();
    expect(prefs.hideEmptyYears).toBe(false);
  });

  it('is the defaults when nothing is stored', () => {
    expect(preferencesFrom(null)).toEqual(DEFAULT_SETTINGS);
  });

  it('puts a visit’s filters back on without touching the preferences', () => {
    const prefs = preferencesFrom(JSON.stringify({ yearOrder: 'newest', showUnrated: false }));
    const next = applyFilters(prefs, narrowed);
    expect(next.yearOrder).toBe('newest');
    expect(next.showUnrated).toBe(false);
    expect(filtersOf(next, narrowed.people)).toEqual(narrowed);
    expect(viewPrefs(next)).toEqual({
      yearOrder: 'newest',
      showUnrated: false,
      highlightYear: true,
    });
  });
});
