import {
  RATING_STOPS,
  settingsFrom,
  type GridSettings,
} from './grid';
import { isImdbId } from './movieParam';

/** What narrows one map: who is selected, the rating floor, the year
 *  window, the genres picked, and whether empty years are hidden.
 *
 *  How the map is drawn — newest first, unrated films, the searched
 *  year's highlight — stays with the reader. A filter does not. It
 *  belongs to the visit it was set on. */
export interface MapFilters {
  people: string[];
  minRating: number | null;
  yearFrom: number | null;
  yearTo: number | null;
  hideEmptyYears: boolean;
  /** In the order they were picked, as the map's legend names them. */
  genres: string[];
}

/** A movie just opened. Nothing set on the one being left comes with it. */
export function freshFilters(): MapFilters {
  return {
    people: [],
    minRating: null,
    yearFrom: null,
    yearTo: null,
    hideEmptyYears: false,
    genres: [],
  };
}

/** The history entry for a movie opened forward: search, a card, home.
 *  Back through that entry is what restores the filters, so a forward
 *  step stores a clear map rather than copying the one being left. The
 *  movie is not stored: the address names it. */
export function forwardEntry(depth: number): { depth: number; filters: MapFilters } {
  return { depth, filters: freshFilters() };
}

/** Filters saved on the entry being returned to. Missing or nonsense
 *  is a clear map: an old entry, from before filters lived here, must
 *  not invent a selection. */
export function filtersFromState(state: unknown): MapFilters {
  if (!state || typeof state !== 'object' || !('filters' in state)) return freshFilters();
  const raw = (state as { filters: unknown }).filters;
  if (!raw || typeof raw !== 'object') return freshFilters();
  const f = raw as Record<string, unknown>;
  return {
    people: peopleOf(f.people),
    minRating: ratingOf(f.minRating),
    yearFrom: yearOf(f.yearFrom),
    yearTo: yearOf(f.yearTo),
    hideEmptyYears: f.hideEmptyYears === true,
    genres: genresOf(f.genres),
  };
}

/** Write filters onto the entry the reader is on, keeping the depth
 *  already stored there. */
export function stampFilters(state: unknown, filters: MapFilters): Record<string, unknown> {
  const base: Record<string, unknown> =
    state !== null && typeof state === 'object' ? { ...(state as Record<string, unknown>) } : {};
  if (typeof base.depth !== 'number') base.depth = 0;
  return { ...base, filters: { ...filters, people: [...filters.people], genres: [...filters.genres] } };
}

export function filtersOf(settings: GridSettings, people: Iterable<string>): MapFilters {
  return {
    people: [...people],
    minRating: settings.minRating,
    yearFrom: settings.yearFrom,
    yearTo: settings.yearTo,
    hideEmptyYears: settings.hideEmptyYears,
    genres: [...settings.genres],
  };
}

/** View preferences kept, filter fields replaced. */
export function applyFilters(settings: GridSettings, filters: MapFilters): GridSettings {
  return {
    ...settings,
    minRating: filters.minRating,
    yearFrom: filters.yearFrom,
    yearTo: filters.yearTo,
    hideEmptyYears: filters.hideEmptyYears,
    genres: filters.genres,
  };
}

/** The part that is allowed to follow the reader from map to map. */
export function viewPrefs(
  settings: GridSettings,
): Pick<GridSettings, 'yearOrder' | 'showUnrated' | 'highlightYear'> {
  return {
    yearOrder: settings.yearOrder,
    showUnrated: settings.showUnrated,
    highlightYear: settings.highlightYear,
  };
}

/** Preferences from storage. A year range or a rating floor left in
 *  there from when filters were global is dropped, so it cannot walk
 *  onto the next movie. */
export function preferencesFrom(raw: string | null): GridSettings {
  return applyFilters(settingsFrom(raw), freshFilters());
}

const STOPS = new Set<number>(RATING_STOPS);

function ratingOf(v: unknown): number | null {
  if (typeof v !== 'number' || !STOPS.has(v)) return null;
  return v;
}

function yearOf(v: unknown): number | null {
  if (typeof v !== 'number' || !Number.isInteger(v) || v < 1800 || v > 2100) return null;
  return v;
}

/** Genre names as IMDb writes them ("Sci-Fi", "Film-Noir"), each once,
 *  and no more than IMDb has. Which of them the map's legend holds is the
 *  map's business (see genreMask); this only keeps a stale entry from
 *  carrying anything that could not have been picked. */
function genresOf(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  const out: string[] = [];
  for (const g of v) {
    if (typeof g !== 'string' || !/^[A-Z][A-Za-z]*(-[A-Z][A-Za-z]*)?$/.test(g) || out.includes(g)) continue;
    out.push(g);
    if (out.length === IMDB_GENRES) break;
  }
  return out;
}

/** How many genres IMDb has, the most a reader could ever pick. */
const IMDB_GENRES = 28;

/** An IMDb name id, and not a hundred of them: a chip row is a
 *  selection, not a dump of whatever a stale entry was holding. */
function peopleOf(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  const out: string[] = [];
  const seen = new Set<string>();
  for (const id of v) {
    if (typeof id !== 'string' || !isImdbId(id, 'nm') || seen.has(id)) continue;
    seen.add(id);
    out.push(id);
    if (out.length === 80) break;
  }
  return out;
}
