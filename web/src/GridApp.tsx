import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type RefObject,
} from 'react';
import { NO_APP_SCROLL, useHeaderAway, type AppScroll } from './overHeader';
import { fetchGrid, fetchGridFilms, fetchPeoplePhotos, searchMovies, type SearchHit } from './api';
import { gridCache } from './gridCache';
import { capture } from './analytics';
import { EAGER_TILES, coldScreenCount, tilesFrom, type FirstRunFilm } from './firstRun';
import { fetchFirstRun } from './api';
import {
  DEFAULT_SETTINGS,
  RATING_STOPS,
  activeFilters,
  rungLabel,
  withoutPill,
  changedCount,
  aloneAfterHiding,
  emptyYearCount,
  litOthers,
  nothingLit,
  onPlot,
  rangeHoldsNone,
  searchedTagAt,
  spineOf,
  yearBounds,
  yearCounts,
  type GridFilm,
  type GridPayload,
  type GridPerson,
  type GridSettings,
} from './grid';
import { GridMap, openingBox } from './GridMap';
import { GridSheet } from './GridSheet';
import { PeopleChips, filmCounts } from './PeopleChips';
import { PersonCard } from './PersonCard';
import { useFaceCard, warmBigPhotos } from './faceCard';
import { NOTHING_SHOWN, assignHues, nextShown } from './personColour';
import { Wordmark } from './Wordmark';
import { ViewPanel } from './ViewPanel';
import { useEscape } from './sheet';
import { HEADER_ROW_H, overOffset, screenOf, useScreen } from './screen';
import { PosterImage, usePosterSrc } from './PosterImage';
import { posterFallback } from './poster';
import { Progress, useProgress } from './Progress';
import { isSearchShortcut, isTyping, searchPlaceholder } from './search';
import { Toast, useToast } from './Toast';
import { usePlayer } from './TrailerRow';
import { dimsCards, hideEmptyToast } from './quickSwitch';
import {
  ABOUT_PATH,
  DAILY_PATH,
  filmPath,
  isAboutPath,
  isDailyPath,
  movieIdFromPath,
  routeAt,
  usePageTitle,
} from './movieParam';
import { AboutPage, MailLink } from './AboutPage';
import { DailyHeaderTail, DailyPage } from './DailyPage';
import { DailyBanner, useDailyBanner } from './DailyBanner';
import {
  applyFilters,
  filtersFromState,
  filtersOf,
  forwardEntry,
  freshFilters,
  preferencesFrom,
  stampFilters,
  viewPrefs,
  type MapFilters,
} from './trail';
import { ThemePicker } from './ThemePicker';
import {
  BREATHE_AT_MS,
  BREATHE_BLUR_PX,
  BREATHE_MS,
  CHIP_FLIP_MS,
  CHIP_IN_MS,
  CHIP_IN_RISE_PX,
  EASE,
  FACE_MS,
  FLY_SCALE,
  FOCUS_DELAY_MS,
  FOCUS_KEYFRAMES,
  FOCUS_MS,
  GLIDE_BLUR_PX,
  GLIDE_MS,
  Glider,
  LAND_TIMEOUT_MS,
  LOADER_H,
  LOADER_W,
  PLAIN_OUT_MS,
  RETURN_RISE_PX,
  RETURN_TILE_MS,
  SET_AT_MS,
  TAG_IN_MS,
  TILES_AFTER_LIFT_MS,
  VEIL_AT_MS,
  VEIL_OUT_MS,
  WORD_BEFORE_LANDING_MS,
  animate,
  chipInDelay,
  chipShift,
  flightTo,
  landingTransform,
  loaderSpot,
  markFlight,
  openingPlan,
  returnTileDelay,
  stillNow,
  type Box,
  type Pose,
  type Spot,
} from './motion';
import {
  resolved,
  useReducedMotion,
  useResolvedTheme,
  useTheme,
  type Theme,
  type ThemePref,
} from './theme';

/** The width a first-run poster is drawn at. Nothing waits on the set
 *  of them any more: each tile shows its own the moment it decodes. */
const TILE_W = 104;

/** How far the opening load has got. The header reads it: the mark's
 *  place is empty until the loader lands in it, and "inedikt" waits,
 *  out of focus, until the C is nearly there. Every timing it runs on is
 *  in motion.ts. */
type Opening = 'draw' | 'word' | 'done';

/** The longest the copy waits for Young Serif, when it is not already in
 *  hand as the frames appear, before it fades in with whatever face is
 *  available. */
const FONT_WAIT_MS = 400;

/** The move to another map (glideTo): a copy of the tapped card takes
 *  off while the old map fades, the new map is set underneath it, and
 *  the copy lands on its searched film. One flight throughout. */
type GlidePhase = 'fly' | 'set';

interface Glide {
  /** Which glide this is. A later one, or anything that cancels it,
   *  moves the count on, and a timer left over from this one sees that
   *  and does nothing. */
  token: number;
  film: GridFilm;
  phase: GlidePhase;
  /** The card's box as the copy took off from it. */
  rect: Box;
  /** The card itself, copied as it stood, so the copy that takes off
   *  from it is it to the pixel. */
  face: HTMLElement;
  /** Where the new map will put its searched card, worked out at take-off
   *  when that map is already in hand, so the copy heads straight there.
   *  Null when it is still on its way: the copy makes for the middle and
   *  waits. */
  aim: Box | null;
}

/** What the flying copy is asked to do once the new map is placed:
 *  land on this box, and say when it is there. How long that will take,
 *  or null if it cannot move and the landing is simply over. */
interface FlyerHandle {
  land: (to: Box, done: () => void) => number | null;
}

/** Where each chip in the header sits, by `data-chip`, so a move to
 *  another map can slide the ones both maps share into their new places. */
function chipBoxes(header: HTMLElement | null): Map<string, DOMRect> {
  const out = new Map<string, DOMRect>();
  header?.querySelectorAll('[data-chip]').forEach((n) => {
    out.set(n.getAttribute('data-chip') ?? '', n.getBoundingClientRect());
  });
  return out;
}

/** The chip row after a move to another map. A chip both maps share
 *  slides from where it was; a new one rises in after them, one short
 *  step behind the one before. */
function flipChips(header: HTMLElement | null, was: Map<string, DOMRect>) {
  let k = 0;
  header?.querySelectorAll('[data-chip]').forEach((n) => {
    const before = was.get(n.getAttribute('data-chip') ?? '');
    if (before) {
      const { dx, dy } = chipShift(before, n.getBoundingClientRect());
      if (dx === 0 && dy === 0) return;
      animate(n, [{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }], {
        duration: CHIP_FLIP_MS,
        easing: EASE.glide,
      });
      return;
    }
    animate(
      n,
      [
        { opacity: 0, transform: `translateY(${CHIP_IN_RISE_PX}px)` },
        { opacity: 1, transform: 'none' },
      ],
      { duration: CHIP_IN_MS, delay: chipInDelay(k++), fill: 'backwards' },
    );
  });
}

/** Share images already asked for this session. Once per movie: the
 *  point is that the picture exists, and asking twice does not make it
 *  exist harder. */
const warmedCards = new Set<string>();

/** Asks the server to have this movie's share card ready.
 *
 *  Slack, iMessage and X each cache the first thing they are given for
 *  an address, so a link pasted before the image has ever been rendered
 *  can show the generic card for as long as that cache lives. Rendering
 *  it while the reader is still looking at the map costs them nothing
 *  and settles it.
 *
 *  Not on a metered connection: a reader who has asked their phone to
 *  save data has not asked for a picture they will never see. */
function warmShareCard(id: string, version: string | undefined) {
  if (!version || warmedCards.has(id)) return;
  const link = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection;
  if (link?.saveData) return;
  warmedCards.add(id);
  fetch(`/og/movie/${id}.png?v=${version}`, {
    priority: 'low',
    credentials: 'omit',
  } as RequestInit).catch(() => {});
}

/** Asks, once per map, for the photos of the people it arrived without.
 *  The payload cannot say why one is missing (not looked up yet, none on
 *  TMDb, or an answer too old to send); the photos endpoint can. Nobody
 *  missing, nothing asked. Each answer is handed to `onSome` as it comes
 *  (see fetchPeoplePhotos); `signal` is the map going. */
export function askForMissingPhotos(
  payload: Pick<GridPayload, 'people'>,
  signal: AbortSignal,
  onSome: (some: Record<string, string | null>) => void,
  fetcher: typeof fetchPeoplePhotos = fetchPeoplePhotos,
): void {
  const missing = payload.people.filter((p) => !p.photo).map((p) => p.id);
  if (missing.length === 0) return;
  void fetcher(missing, signal, onSome);
}

/** The header's classes: ruled off over a map (`map`), lying over it on
 *  a small screen (`over`) and gone up out of the way (`away`). While
 *  the search field has the focus (`searching`) it stands over the
 *  blur behind a playing preview trailer (.cd-header-searching). */
export function headerClass({
  map,
  over,
  away,
  searching,
}: {
  map: boolean;
  over: boolean;
  away: boolean;
  searching: boolean;
}): string {
  return `cd-header${map ? ' cd-header-map' : ''}${over ? ' cd-header-over' : ''}${away ? ' cd-header-away' : ''}${searching ? ' cd-header-searching' : ''}`;
}

/** Where the reader's view preferences live between visits. Filters
 *  are not among them: those belong to the history entry. */
const SETTINGS_KEY = 'cinedikt.grid';

function readStoredPreferences(): GridSettings {
  try {
    return preferencesFrom(localStorage.getItem(SETTINGS_KEY));
  } catch {
    return DEFAULT_SETTINGS;
  }
}

function readInitialSettings(): GridSettings {
  return applyFilters(readStoredPreferences(), filtersFromState(history.state));
}

function readInitialPeople(): Set<string> {
  return new Set(filtersFromState(history.state).people);
}

function persistView(settings: GridSettings) {
  try {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(viewPrefs(settings)));
  } catch {
    // A reader with storage blocked still gets this visit's choices.
  }
}

/** Debounce before a keystroke becomes a request. */
const SEARCH_DEBOUNCE_MS = 250;

/** The rating grid, end to end: a film's people, every film they made,
 *  and nothing that has to be grown. */
export function GridApp() {
  const adopt = useRef<(filters: MapFilters) => void>(() => {});
  const { movieId, about, daily, canGoBack, openMovie, openAbout, openDaily, goBack, goHome } =
    useFilmRoute(adopt);
  const screen = useScreen();
  // Only a phone drops "inedikt": a landscape phone has the width for it.
  // The Daily's header keeps it even there, as the game's design draws
  // it: with no search field beside it, the row has the room.
  const compactHeader = screen.phone && !daily;
  // Anything narrower than 1024 px sends the rating rungs to the View
  // panel: below that the header has no room for them beside the
  // wordmark and the search field. A wide window squashed short keeps
  // them — it has the width, and its header row has the height for a
  // 36px group.
  const rungsInView = screen.narrow;
  const [settings, setSettingsState] = useState(readInitialSettings);
  const [selected, setSelectedState] = useState(readInitialPeople);
  const settingsRef = useRef(settings);
  const selectedRef = useRef(selected);
  settingsRef.current = settings;
  selectedRef.current = selected;
  const commit = useCallback((next: GridSettings, people: Set<string>, store: boolean) => {
    settingsRef.current = next;
    selectedRef.current = people;
    setSettingsState(next);
    setSelectedState(people);
    if (store) persistView(next);
    try {
      history.replaceState(stampFilters(history.state, filtersOf(next, people)), '');
    } catch {
      // Back remembers the last stamp that succeeded. The map still moves.
    }
  }, []);
  const setSettings = useCallback<SetSettings>((s) => {
    const next = typeof s === 'function' ? s(settingsRef.current) : s;
    commit(next, selectedRef.current, true);
  }, [commit]);
  const setPeople = useCallback((people: Set<string>) => {
    commit(settingsRef.current, people, false);
  }, [commit]);
  const [theme, setTheme] = useTheme();
  // Where the opening load has got to. It drives the header, which is
  // why it lives here rather than in ColdStart: the mark ends up in
  // the wordmark, and only this component renders both. See
  // skipsOpening for the visits that never play it.
  const [opening, setOpening] = useState<Opening>(() => {
    if (skipsOpening(location.pathname)) {
      openingPlayed = true;
      return 'done';
    }
    return stillNow() ? 'done' : 'draw';
  });
  const markSlot = useRef<HTMLSpanElement>(null);
  const onTheme = useCallback(
    (pref: ThemePref) => {
      setTheme(pref);
      capture('theme_set', { pref, resolved: resolved(pref) });
    },
    [setTheme],
  );
  // The map on screen, with the movie it was fetched for. It stays for a
  // moment after the route has moved on: the map being left fades out,
  // and while the next is fetched it stays in the tree out of sight (see
  // `leaving` below), so the next one fades up in the same place.
  const [drawn, setDrawn] = useState<{ id: string; payload: GridPayload } | null>(null);
  // Loading from the first render when the page opens on a map's
  // address. Waiting for the effect below to say so would draw one frame
  // of the opening screen first, and that screen's first-run request
  // would go out only to be cancelled as it unmounted.
  const [loading, setLoading] = useState(() => movieId !== null);
  // A map is being fetched from the opening screen, the About page or
  // the Daily, which stays behind the progress line, dimmed, until it
  // arrives.
  const [fromCold, setFromCold] = useState(false);
  // Which of the three pages without a map is drawn when there is no map
  // to draw: the opening screen, the About page or the Daily (see
  // drawsPage).
  const [pageDrawn, setPageDrawn] = useState<Page>(() => pageOf(about, daily));
  const pageShown = drawsPage(movieId, pageOf(about, daily), pageDrawn);
  if (pageShown !== pageDrawn) setPageDrawn(pageShown);
  // The Daily's puzzle, once its page has loaded it, for the date in the
  // header; it is forgotten when the page goes, so coming back to it
  // another day never shows yesterday's number while today's loads. And
  // a count the header's "How it works" moves on, which the page reads
  // as a request to open the rules.
  const [day, setDay] = useState<{ no: number; date: string } | null>(null);
  if (pageDrawn !== 'daily' && day !== null) setDay(null);
  const [rulesAsked, setRulesAsked] = useState(0);
  const drawnRef = useRef(drawn);
  drawnRef.current = drawn;
  const [error, setError] = useState<string | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const [lit, setLit] = useState<Set<string>>(new Set());
  const [openId, setOpenId] = useState<string | null>(null);
  const [viewOpen, setViewOpen] = useState(false);
  // The one trailer player, shared by the panel and the map's preview so
  // that only one is ever open.
  const player = usePlayer();
  const stopTrailer = player.stop;
  const [searching, setSearching] = useState(false);
  // The Daily's header has no search field. One that had the focus as
  // the route moved there — Back, from the keyboard — is taken away
  // without a blur, so what it said about itself goes with it.
  if (daily && searching) setSearching(false);
  // The search field has text in it, which an Escape clears before it
  // closes the map's hover preview.
  const [searchTyped, setSearchTyped] = useState(false);
  const scrollerRef = useRef<HTMLDivElement>(null);
  // Marked by the map before each scroll it makes by itself, so the
  // header lying over it can tell those from the reader's.
  const appScroll = useRef<AppScroll>(NO_APP_SCROLL);
  // Bumped when a setting rearranges the plot, so the map can put the
  // searched film back in the middle of it.
  const [relaid, setRelaid] = useState(0);
  // Bumped by Try again. The movie has not changed, so nothing else
  // would ask for it again: opening the same id is no change to React.
  const [attempt, setAttempt] = useState(0);
  const session = useRef<AbortController | null>(null);
  const movieSeen = useRef(movieId);
  const toast = useToast();
  // Forward navigation clears. Moving through history — the chevron,
  // the browser's back and forward buttons — restores the entry just
  // landed on. The write happens after the position has moved, so it
  // stamps that entry and leaves the one behind as it was. The toast
  // goes too: its Undo would otherwise put the previous visit's
  // selection onto this one.
  adopt.current = (filters) => {
    toast.hide();
    commit(applyFilters(settingsRef.current, filters), new Set(filters.people), false);
  };
  // The title of the film being fetched, for the busy toast: the payload
  // is not here yet, so the name comes from whatever started the load —
  // the search hit, or the card that was remapped. Kept with the id it
  // belongs to: Back and Forward change the movie without a title, and
  // a bare string would name whichever film was opened last instead.
  const titleRef = useRef<{ id: string; title: string } | null>(null);

  // The move to another map from a card's sheet (glideTo, below). Held
  // in state for what it draws, and in a ref for the timers and effects
  // that must see where it has got to without waiting for a render.
  const [glide, setGlide] = useState<Glide | null>(null);
  const glideRef = useRef<Glide | null>(null);
  glideRef.current = glide;
  const glideToken = useRef(0);
  const glideTimers = useRef<number[]>([]);
  // The copy in flight, for the landing to steer. Its own, and gone with it.
  const flyer = useRef<FlyerHandle | null>(null);
  // The chips as they stood when the card took off, for the chip row to
  // slide from once the new map is in.
  const chipsWere = useRef<Map<string, DOMRect> | null>(null);
  const headerRef = useRef<HTMLElement>(null);
  const endGlide = useCallback(() => {
    glideToken.current += 1;
    glideTimers.current.forEach(window.clearTimeout);
    glideTimers.current = [];
    chipsWere.current = null;
    glideRef.current = null;
    setGlide(null);
  }, []);
  useEffect(() => () => glideTimers.current.forEach(window.clearTimeout), []);

  // The sheet and the View panel, each able to close itself with its
  // exit (see closeLayers).
  const sheetCloser = useRef<Closer | null>(null);
  const viewCloser = useRef<Closer | null>(null);
  // Opening a map starts by closing whatever sheet or panel is up, each
  // on its own exit, and only then moves.
  const closeLayers = useCallback((then: () => void) => {
    const open = [sheetCloser.current, viewCloser.current].filter((c): c is Closer => c != null);
    if (open.length === 0) {
      then();
      return;
    }
    let left = open.length;
    for (const leave of open) {
      leave(() => {
        left -= 1;
        if (left === 0) then();
      });
    }
  }, []);

  const movieRef = useRef(movieId);
  movieRef.current = movieId;
  const errorRef = useRef(error);
  errorRef.current = error;
  const setMovieId = useCallback(
    (id: string, title?: string) => {
      // A pick from search, a tile or the error screen outranks a move
      // already under way.
      endGlide();
      closeLayers(() => {
        if (title) titleRef.current = { id, title };
        // The map on screen failed and the reader has picked the same
        // movie again — from search, most likely. That is Try again by
        // another way in: routing it would push a second entry for the
        // same address and, the id being unchanged, fetch nothing.
        if (id === movieRef.current && errorRef.current != null) {
          setAttempt((n) => n + 1);
          return;
        }
        openMovie(id, title);
      });
    },
    [openMovie, endGlide, closeLayers],
  );
  const inflight = useRef(new Set<'before' | 'after'>());
  // Maps already asked for, so "Map this film instead" can open on a
  // payload that arrived while the panel was still up. See gridCache.ts
  // for why its fetches take no caller's abort signal.
  const [grids] = useState(() => gridCache((id) => fetchGrid(id)));
  const loadGrid = grids.load;

  // The map on screen, and whether it is still the one the route is on.
  const mapPayload = drawn?.payload ?? null;
  const stale = drawn != null && drawn.id !== movieId;
  // A move to another map that has taken off and not yet been handed its
  // map: the progress line runs and the chips wait as for any load.
  const gliding = glide != null && drawn?.id !== glide.film.id;
  // The map being left. It fades out and is kept, out of sight and out
  // of reach, until the next one takes its place — except when the next
  // one is already in hand, when there is no wait to cover and the new
  // map simply replaces it.
  const leaving = gliding || (stale && !(movieId !== null && grids.peek(movieId)));
  // The map the rest of the page answers to: none while one is being
  // left, so its chips, its sheet and its buttons go with it.
  const payload = leaving ? null : mapPayload;

  useEffect(() => {
    const movieChanged = movieSeen.current !== movieId;
    const cameFrom = movieSeen.current;
    movieSeen.current = movieId;
    session.current?.abort();
    inflight.current.clear();
    // Any route change the move did not make itself calls it off: Back
    // or Forward mid-flight (even to the map it was headed for), a pick
    // from search, going home. Its own push, at SET_AT_MS, sets phase
    // 'set' in the same timer callback, batched into this render, so it
    // is the only change that finds the move already in that phase.
    const g = glideRef.current;
    if (g && (g.phase !== 'set' || g.film.id !== movieId)) endGlide();
    // A chip preview belongs to the grid it was taken on. Leaving it set
    // while the chips unmount (no mouseleave) paints the next film's
    // cards dim until the pointer happens to cross a chip again.
    // The selection is not cleared here: forward already started clean,
    // and coming back has put the previous visit's filters in place.
    setHovered(null);
    setLit(new Set());
    // A sheet or panel still up when the route moves underneath it —
    // Back or Forward — goes at once: it belongs to the map being left.
    // A move the app makes itself has already closed them on their exits
    // (closeLayers).
    setOpenId(null);
    setViewOpen(false);
    // A trailer belongs to the map it was opened on.
    stopTrailer();
    if (movieChanged) toast.hide();
    if (movieId === null) {
      setDrawn(null);
      setLoading(false);
      setFromCold(false);
      setError(null);
      return;
    }
    const ctrl = new AbortController();
    session.current = ctrl;
    const cached = grids.peek(movieId);
    if (cached) {
      setDrawn({ id: movieId, payload: cached });
      setLoading(false);
      setFromCold(false);
      setError(null);
      capture('grid_loaded', { movie_id: movieId, films: cached.films.length });
      warmShareCard(movieId, cached.og_v);
      return () => ctrl.abort();
    }
    // The map being left is not a stand-in for the one being fetched:
    // its chips and its cards would both be answering for the wrong film.
    // It is not dropped, though. It fades out and stays in the tree, out
    // of sight and out of reach, with skeletons where its chips were,
    // until the new one takes its place (see `leaving`).
    setLoading(true);
    // From the opening screen or the About page with nothing drawn yet,
    // that page stays, dimmed, behind the progress line; a map opened
    // from a link has nothing behind it, and loads into an empty plot.
    if (movieChanged) setFromCold((was) => drawnRef.current == null && (cameFrom === null || was));
    setError(null);
    const named = titleRef.current?.id === movieId ? titleRef.current.title : 'this movie';
    toast.show({ text: `Finding everyone who made ${named}…`, busy: true });
    loadGrid(movieId)
      .then((p) => {
        if (ctrl.signal.aborted) return;
        toast.show({ text: 'Laying out their movies…', busy: true });
        setDrawn({ id: movieId, payload: p });
        setFromCold(false);
        capture('grid_loaded', { movie_id: movieId });
        warmShareCard(movieId, p.og_v);
      })
      .catch(() => {
        // A failure that lands after the reader has moved on belongs to
        // a map nobody is waiting for. Reported, it would put an error
        // over whichever one they went to instead.
        if (ctrl.signal.aborted) return;
        // Never the raw message: it is written for us, not the reader.
        setError('failed');
        setDrawn(null);
        setFromCold(false);
        toast.hide();
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false);
      });
    return () => ctrl.abort();
    // `attempt` is not read here: it is what makes Try again run this
    // again for a movie that has not changed.
  }, [movieId, attempt, loadGrid, stopTrailer]);

  const prefetch = useCallback(
    (id: string) => {
      if (id === movieId) return;
      loadGrid(id).catch(() => {});
    },
    [movieId, loadGrid],
  );

  const openFilm = useCallback(
    (id: string) => {
      setOpenId(id);
      prefetch(id);
    },
    [prefetch],
  );

  // The spine says where every card goes; this is what they say. The
  // view asks for the ids it can see, and each one is asked for once.
  const [detail, setDetail] = useState<Map<string, GridFilm>>(new Map());
  const asked = useRef(new Set<string>());
  useEffect(() => {
    asked.current = new Set();
    setDetail(new Map());
  }, [movieId]);

  const onNeedDetail = useCallback(
    (ids: string[]) => {
      if (movieId === null) return;
      const fresh = ids.filter((id) => !asked.current.has(id));
      if (fresh.length === 0) return;
      for (const id of fresh) asked.current.add(id);
      const ctrl = session.current;
      fetchGridFilms(movieId, fresh, ctrl?.signal)
        .then((films) => {
          setDetail((have) => {
            const next = new Map(have);
            for (const f of films) next.set(f.id, f);
            return next;
          });
        })
        .catch(() => {
          // Asking again is better than a card that never fills in.
          for (const id of fresh) asked.current.delete(id);
        });
    },
    [movieId],
  );


  // Setting a floor dims most of the map at once, which on a narrowed
  // map can leave nothing lit at all. The toast says which it was, and
  // offers the way back.
  const onFloor = useCallback(
    (minRating: number | null) => {
      setSettings((was) => ({ ...was, minRating }));
      if (minRating === null) {
        toast.hide();
        return;
      }
      const clear = {
        label: 'Clear',
        run: () => {
          setSettings((was) => ({ ...was, minRating: null }));
          toast.hide();
        },
      };
      const only = selected.size === 1 ? [...selected][0] : null;
      // Where they sit in the chip row, which is how the spine names them.
      const at = only === null || !payload ? -1 : payload.people.findIndex((p) => p.id === only);
      const alone = at < 0 ? undefined : payload?.people[at];
      // Judged over what the page holds, not the whole spine: a film the
      // year range has cropped away lights nothing, so counting it would
      // hide the one case this toast is here to explain. The ref already
      // has the floor just set, and the range it is being set against.
      const now = settingsRef.current;
      // With a range set, the claim is about these years only. Whatever
      // of theirs lies outside it is not on the page to be judged.
      const years = now.yearFrom != null || now.yearTo != null ? ' in these years' : '';
      const text =
        alone && payload && nothingLit(spineOf(payload).filter((f) => onPlot(f, now)), at, minRating)
          ? `Nothing of ${alone.name}’s${years} is rated ${minRating.toFixed(1)} or higher`
          : `Lighting movies rated ${minRating.toFixed(1)} and up`;
      toast.show({ text, action: clear });
    },
    // The toaster's own functions are stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [setSettings, selected, payload],
  );

  // Everyone on the map being shown has a colour before anything draws
  // them. It changes nothing for anyone already coloured, so a second
  // render of the same map is harmless, and only a map on screen gets
  // here: one fetched ahead for an open sheet does not use up colours
  // out of the order the reader meets people in.
  if (payload) assignHues(payload.people);
  // Who of this map was also on the last one shown, for the chip row's
  // order. Settled while rendering, so the first paint of a new map
  // already has it (see nextShown for what counts as shown).
  const [shown, setShown] = useState(NOTHING_SHOWN);
  const nowShown = nextShown(shown, movieId, payload);
  if (nowShown !== shown) setShown(nowShown);

  // The chips hold people by id; the spine names them by their place in
  // that row. The map from one to the other is per payload, so it is
  // made once rather than inside every card's judgement.
  const selectedIdx = useMemo(() => {
    if (!payload) return new Set<number>();
    const out = new Set<number>();
    payload.people.forEach((p, i) => {
      if (selected.has(p.id)) out.add(i);
    });
    return out;
  }, [payload, selected]);

  // Each chip's count, off the spine. Per map: the filters do not change
  // how many of someone's films the map holds.
  const counts = useMemo(() => (payload ? filmCounts(payload) : new Map<string, number>()), [payload]);

  // The photos of the people a map came without, as they are found. Kept
  // beside the payload rather than folded into its people: a new payload
  // lays the whole map out again. A person's photo is the same on every
  // map, so what is found is kept for the visit.
  const [photos, setPhotos] = useState<ReadonlyMap<string, string>>(new Map());
  useEffect(() => {
    if (!payload) return;
    const ctrl = new AbortController();
    askForMissingPhotos(payload, ctrl.signal, (some) =>
      setPhotos((was) => {
        const next = new Map(was);
        // A null is TMDb having no photo, which is the same as no answer:
        // the initials stay.
        for (const [id, url] of Object.entries(some)) if (url) next.set(id, url);
        // Answers that add nothing, "none" or photos already in hand,
        // redraw nothing.
        return [...next].every(([id, url]) => was.get(id) === url) ? was : next;
      }),
    );
    return () => ctrl.abort();
  }, [payload]);
  const photoOf = useCallback((p: GridPerson) => p.photo ?? photos.get(p.id), [photos]);

  // The one bigger photo of a person for the page, asked for by a chip
  // or by a face in the map's hover preview.
  const faceCard = useFaceCard((id) => {
    const p = payload?.people.find((q) => q.id === id);
    return p && photoOf(p);
  }, scrollerRef);
  // It belongs to the map it was opened on, as does one on its way.
  const shutFaceCard = faceCard.shut;
  useEffect(() => shutFaceCard(), [movieId, shutFaceCard]);
  const cardPerson = faceCard.card && payload?.people.find((p) => p.id === faceCard.card?.id);
  const cardPhoto = cardPerson ? photoOf(cardPerson) : undefined;
  // Once a card has opened on a map, everyone else's bigger photo is
  // fetched too, so moving on to the next person swaps at once rather
  // than leaving a gap while TMDb sends theirs. The first card on a map
  // still waits for its own after the dwell.
  const warmedBig = useRef(new Set<string>());
  const cardOpen = faceCard.card != null;
  useEffect(() => {
    if (cardOpen && payload) warmBigPhotos(payload.people.map(photoOf), warmedBig.current);
  }, [cardOpen, payload, photoOf]);

  const bounds = useMemo(
    () => (payload ? yearBounds(payload, settings.showUnrated) : { lo: 1900, hi: 2100 }),
    [payload, settings.showUnrated],
  );
  // The histogram over the year slider, counted by the same rule as its
  // ends so that every bar has a year under it.
  const perYear = useMemo(
    () => (payload ? yearCounts(payload, settings.showUnrated) : new Map<number, number>()),
    [payload, settings.showUnrated],
  );

  const pillText = activeFilters(settings, rungsInView);
  const changed = changedCount(settings, rungsInView);
  const pillRef = useRef<HTMLButtonElement>(null);
  const everyoneRef = useRef<HTMLButtonElement>(null);
  const openedFromPill = useRef(false);

  // The pill's ✕ clears what the pill says: the floor, when the pill is
  // where the floor is shown, the year window and the genres. Hiding the
  // empty years is a preference the pill does not speak for, so it is
  // left.
  const clearPill = useCallback(() => {
    const was = settingsRef.current;
    const years = was.yearFrom != null || was.yearTo != null;
    const floor = rungsInView && was.minRating != null;
    const genres = was.genres.length > 0;
    setSettings(withoutPill(was, rungsInView));
    // The toast about the floor goes with the floor, the same as picking
    // Any would take it.
    if (floor) toast.hide();
    // Only the years move the map. A floor or the genres light and dim
    // in place, and with the empty years hidden their rows close up
    // around the searched film, which stays where it is.
    if (years) setRelaid((n) => n + 1);
    capture('filter_pill_cleared', { years, floor, genres });
    // The pill is about to unmount, so the focus has to go somewhere it
    // can be seen: the first chip, which is where the row starts.
    everyoneRef.current?.focus();
    // The toaster's own functions are stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [setSettings, rungsInView]);

  // What else the page holds and lights. Counted off the spine, which
  // holds every year whether or not anybody has scrolled to it, and on
  // the layout's own terms: a film the unrated column or the year range
  // has taken off the plot is not on the page, so it lights nothing.
  const lighting = useMemo(
    () => (payload ? litOthers(payload, settings, selectedIdx) : []),
    [payload, settings, selectedIdx],
  );
  // Hiding the empty years can leave the searched film alone on the
  // page. When it is the hiding that did it, the toast below says so.
  // A range or the unrated column that left nothing else on the plot is
  // not the hiding's doing: the pill names the range, the View panel
  // shows the column, and says so outright when the range holds none of
  // the cast's films (see rangeHoldsNone).
  const aloneOnTheMap = useMemo(
    () => payload != null && aloneAfterHiding(payload, settings, selectedIdx),
    [payload, settings, selectedIdx],
  );
  // How many rows the map is down to, for the reader who cannot see it
  // collapse: the searched film's year, and every year something lit
  // is in.
  const yearsShowing = useMemo(
    () => (payload ? new Set([payload.anchor.year, ...lighting.map((f) => f.year)]).size : 0),
    [payload, lighting],
  );

  // The quick "Hide empty years" switch beside View. It is offered while
  // the chosen people, the rating floor or the genres are dimming cards,
  // which is when a year can be empty, and only if turning it on would
  // hide a year, or it is already on and so can be turned off from here.
  // Clearing the filters folds it away and leaves the setting as it was.
  const filtering = dimsCards(settings, selectedIdx.size);
  const hidesSome = useMemo(
    () => payload != null && filtering && emptyYearCount(payload, settings, selectedIdx) > 0,
    [payload, filtering, settings, selectedIdx],
  );
  const quick = payload != null && !loading && filtering && (settings.hideEmptyYears || hidesSome);
  // The same setting as the View panel's switch, through the same path,
  // so the map closes up the same way. It never recentres: see
  // movesTheMap.
  const viewOpenRef = useRef<HTMLButtonElement>(null);
  const flipHideEmpty = () => {
    const on = !settings.hideEmptyYears;
    // How many years turning it on hides, from the settings as they
    // stand before the flip.
    const hidden = payload ? emptyYearCount(payload, settings, selectedIdx) : 0;
    setSettings((was) => ({ ...was, hideEmptyYears: on }));
    capture('hide_empty_years', { on, from: 'quick' });
    // On phones and short screens the switch is an icon, so the toast
    // says what it did. Wider, its label already says it.
    if (screen.overlay) toast.show(hideEmptyToast(on, hidden, setSettings, toast.hide));
    // Turned off with no year left for it to hide, the switch folds away
    // and goes inert under the press, which would drop the focus on the
    // document. hidesSome does not depend on the setting, so this is
    // exactly when it folds, and the focus goes to View beside it.
    if (!on && !hidesSome) viewOpenRef.current?.focus();
  };

  const wasAlone = useRef(false);
  useEffect(() => {
    if (!aloneOnTheMap || !payload) {
      wasAlone.current = false;
      return;
    }
    if (wasAlone.current) return;
    wasAlone.current = true;
    toast.show({
      text: `Nothing else matches. Showing only ${payload.anchor.title}.`,
      action: {
        label: 'Show all years',
        run: () => {
          setSettings((was) => ({ ...was, hideEmptyYears: false }));
          toast.hide();
        },
      },
    });
    // The toaster's own functions are stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [aloneOnTheMap, payload, setSettings]);

  const onToggle = useCallback((id: string) => {
    const next = new Set(selectedRef.current);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setPeople(next);
  }, [setPeople]);

  const onCardHover = useCallback((people: string[]) => {
    setLit(new Set(people));
  }, []);

  // "Map <title>" in a card's sheet. The sheet has already gone on its
  // own exit (GridSheet's leave). Then a copy of the tapped card takes
  // off from it, in the same frame the card itself goes, and flies while
  // the old map fades: straight for the spot the new map will put its
  // searched film in, when that map is already here to work it out
  // from, or for the middle to wait for it. The new map is set
  // underneath it, and the copy lands on its searched film from
  // wherever it has got to (the landing is GridMap's, once the map is
  // centred, steering the Flyer below).
  //
  // The route moves at the moment the new map is set, not at the tap:
  // until then the reader is still on the old map, watching the card
  // leave it. Nothing about fetching changes — the sheet asked for this
  // map when it opened (openFilm), and setting the route finds it in the
  // cache, or joins the request still out while the copy rests in the
  // middle.
  //
  // A reader who has asked for no movement, or a card that is not on
  // the glass to take off from, is simply taken there.
  const glideTo = useCallback(
    (film: GridFilm) => {
      const node = scrollerRef.current?.querySelector<HTMLElement>(
        `[data-card="${CSS.escape(film.id)}"]`,
      );
      const r = node?.getBoundingClientRect();
      if (stillNow() || !node || !r || !(r.width > 0) || !drawnRef.current) {
        // Not through setMovieId: this runs as the sheet's own exit
        // completes, while the sheet is still mounted, and closeLayers
        // would re-arm the exit of a layer already on its way out, whose
        // unmount then clears that timer and loses the navigation with
        // it. A sheet only exists on a drawn map, so there is no failed
        // map here for a same-id pick to retry.
        endGlide();
        titleRef.current = { id: film.id, title: film.title };
        openMovie(film.id, film.title);
        return;
      }
      endGlide();
      const token = glideToken.current;
      const live = () => glideToken.current === token;
      toast.hide();
      titleRef.current = { id: film.id, title: film.title };
      prefetch(film.id);
      // Taken before the chips give way to skeletons, so the row can
      // slide from exactly here once the new map is in.
      chipsWere.current = chipBoxes(headerRef.current);
      // A map opened forward starts with nothing narrowed (see
      // useFilmRoute), so that is the map the landing is worked out on,
      // under the header as it lies over any map on this screen.
      const next = grids.peek(film.id);
      const sc = scrollerRef.current;
      const nextSettings = applyFilters(settingsRef.current, freshFilters());
      const over = screen.overlay ? overOffset(screen) : 0;
      setGlide({
        token,
        film,
        phase: 'fly',
        rect: { left: r.left, top: r.top, width: r.width, height: r.height },
        face: node.cloneNode(true) as HTMLElement,
        aim: next && sc ? openingBox(next, nextSettings, sc, over, screen.overlay) : null,
      });
      glideTimers.current.push(
        window.setTimeout(() => {
          if (!live()) return;
          setGlide((g) => (g && g.token === token ? { ...g, phase: 'set' } : g));
          if (movieRef.current !== film.id) openMovie(film.id, film.title);
        }, SET_AT_MS),
      );
    },
    // The toaster's own functions are stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [endGlide, openMovie, prefetch, grids, screen],
  );
  const onRemap = glideTo;

  // A map that failed to load has nothing for the copy to land on.
  useEffect(() => {
    if (error != null && glideRef.current) endGlide();
  }, [error, endGlide]);

  // Asked for stillness mid-move: the move is dropped where it is, and
  // the reader is taken straight to where it was going.
  const still = useReducedMotion();
  useEffect(() => {
    const g = glideRef.current;
    if (!still || !g) return;
    endGlide();
    if (movieRef.current !== g.film.id) openMovie(g.film.id, g.film.title);
  }, [still, endGlide, openMovie]);

  // Narrowing to one person is easy to do by accident on a phone, where
  // the row is the size of a thumb, so it comes with its way back.
  const onOnly = useCallback(
    (id: string) => {
      const person = payload?.people.find((p) => p.id === id);
      const before = selectedRef.current;
      setPeople(new Set([id]));
      toast.show({
        text: `Showing only ${person?.name ?? 'them'}`,
        action: {
          label: 'Undo',
          run: () => {
            setPeople(new Set(before));
            toast.hide();
          },
        },
      });
    },
    // The toaster's own functions are stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [payload, setPeople],
  );

  // The panel wants the whole film, which is detail. Opening a card the
  // reader can see means its detail is already here.
  usePageTitle(payload?.anchor.title, about ? 'about' : daily ? 'daily' : undefined);

  const open = openId == null ? null : (detail.get(openId) ?? null);
  // A sheet or popover is up, and the floating buttons belong to the map
  // underneath it.
  const covered = open != null || viewOpen;
  // A header over a map — or over the empty plot one is loading into,
  // or over one being left — holds the chip row. It is ruled off from
  // the map from the first paint: the rule is what says the chips belong
  // to the header and not to the plot. The opening screen, the About
  // page and the error have nothing under the header to divide it from,
  // and neither does either page while a map loads from it: it stays as
  // it was.
  const holdsChips = mapPayload != null || (loading && !fromCold);
  // On a phone, and on a landscape phone, the header lies over the map
  // and goes up out of the way as the reader travels down the years —
  // but never while they are waiting, reading a panel, typing, or moving
  // to another map. Only over a map, or the empty plot one is loading
  // into: the opening screen, the About page and the error all keep the
  // header in flow, on the ground, with nothing underneath it to pass
  // under the glass.
  const overlay = screen.overlay && holdsChips;
  // Where the map starts under a header lying over it: the header row
  // and the chip row, which the stylesheet sets to fixed heights.
  const overlayH = overlay ? overOffset(screen) : 0;
  const headerAway = useHeaderAway(
    scrollerRef,
    overlay && !loading && !covered && !searching && !leaving && glide == null,
    // What the map scrolls itself for that the quiet window cannot see
    // coming: a recentre, any change to which rows are on the plot or
    // which of them close up, and a change of screen class, which lays
    // the map out again with a different card. Clearing the pill does
    // the first two, from a header the reader is looking at.
    [
      screen.cls,
      relaid,
      settings.yearOrder,
      settings.showUnrated,
      settings.yearFrom,
      settings.yearTo,
      settings.hideEmptyYears,
      settings.minRating,
      settings.genres.join(','),
      [...selectedIdx].join(','),
    ].join('|'),
    appScroll,
  );
  // On a map: one has been asked for, whether it has landed, is still
  // loading or failed. This is what the toast is placed by — raised
  // clear of the floating buttons on a map, low on the opening screen.
  // Read from the address rather than the payload, so a toast raised
  // while a map loads is already where it will stay once it lands.
  const onMap = movieId !== null;
  // What the empty search field says. While a map loads, the film being
  // fetched — when the app was told which one (see titleRef). A move to
  // another map is loading from take-off, before the route has moved.
  const pendingId = gliding && glide ? glide.film.id : movieId;
  const loadingTitle = titleRef.current?.id === pendingId ? titleRef.current.title : null;
  const progress = useProgress(loading || gliding);

  // The card the copy took off from. It goes from the map being left in
  // the frame the copy appears over it, so there are never two of it.
  const flown = glide?.film.id ?? null;
  // The copy lands once its map is the one drawn.
  const landingGlide = glide?.phase === 'set' && drawn?.id === glide.film.id ? glide : null;
  const landingToken = landingGlide?.token;
  const onLanded = useCallback(() => {
    if (landingToken != null && glideRef.current?.token === landingToken) endGlide();
  }, [landingToken, endGlide]);
  const landing = useMemo(
    () =>
      landingGlide
        ? { land: (to: Box, done: () => void) => flyer.current?.land(to, done) ?? null, onLanded }
        : null,
    // One landing per glide.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [landingToken, onLanded],
  );
  // If the map never gets to land the copy — a page nobody is painting
  // never lays it out — it is not left hanging over the map for good.
  useEffect(() => {
    if (landingToken == null) return;
    const t = window.setTimeout(onLanded, LAND_TIMEOUT_MS);
    return () => window.clearTimeout(t);
  }, [landingToken, onLanded]);

  // Once the new map is in, the chips it shares with the old one slide
  // to their new places, and the new ones rise in after them. Before the
  // first paint of the new row, so no chip is ever seen where it is
  // going before it has set off.
  useLayoutEffect(() => {
    const was = chipsWere.current;
    if (!was || !payload || !glide || drawn?.id !== glide.film.id) return;
    chipsWere.current = null;
    flipChips(headerRef.current, was);
    // Once per map.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [payload?.anchor.id]);

  // What the map was last drawn with while it was the live one. A map
  // being left keeps these, so it fades out as it was rather than
  // redrawing itself under the next map's selection, filters and words.
  const liveDraw = useRef<{
    settings: GridSettings;
    selectedIdx: Set<number>;
    detail: Map<string, GridFilm>;
  } | null>(null);
  useLayoutEffect(() => {
    if (mapPayload && !stale && !leaving) liveDraw.current = { settings, selectedIdx, detail };
  });
  const frozen = mapPayload && (stale || leaving) ? liveDraw.current : null;
  const back = backLabel({ movieId, about, daily, canGoBack });

  return (
    // data-quick marks the quick switch as out, so that on a phone
    // Recenter drops its word to make room for it (grid.css).
    <div className="cd-app" data-quick={quick || undefined}>
      <header
        ref={headerRef}
        // The Daily is ruled off from the header as a map is. The header
        // never lies over it: the page under it places its guess bar and
        // its toast against its own box, which starts below.
        className={headerClass({
          map: holdsChips || daily,
          over: overlay,
          away: headerAway,
          searching,
        })}
      >
        <div className="cd-header-row">
          {/* See backLabel for where it shows. A permanently disabled
              button would be a dead control in the corner of every
              first visit. */}
          {back !== null && (
            <button type="button" className="cd-back" aria-label={back} onClick={goBack}>
              <span className="cd-back-circle">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M15 18l-6-6 6-6" />
                </svg>
              </span>
            </button>
          )}
          <Wordmark
            markOnly={compactHeader}
            href={homeHref()}
            onClick={goHome}
            slotRef={markSlot}
            // Only the opening screen borrows the header's mark. A map,
            // the About page and the Daily have the wordmark whole from
            // the first paint — and an opening left unfinished, by a
            // reader who pressed the Daily's banner mid-flight, must not
            // leave the Daily's mark missing.
            hollow={movieId === null && !about && !daily && opening !== 'done'}
            wordIn={movieId !== null || about || daily || opening !== 'draw'}
          />
          {daily ? (
            // The Daily's own: its pill, the puzzle's number and day, and
            // "How it works". No search, and so none of its shortcuts:
            // the game has "/" for its own guess field, and a search that
            // opened a map would walk out of a game part way through.
            <DailyHeaderTail
              day={day}
              phone={screen.phone}
              onRules={() => setRulesAsked((n) => n + 1)}
            />
          ) : (
            <>
              <SearchField
                placeholder={searchPlaceholder(loading || gliding, loadingTitle, payload?.anchor.title)}
                onPick={setMovieId}
                onFocusChange={setSearching}
                onTyped={setSearchTyped}
                // A film sheet or the View panel is a dialog with the
                // focus inside it. A key that pulled the focus out to the
                // field behind the scrim would leave the reader typing
                // into a page they cannot see.
                shortcuts={!covered}
              />
              {holdsChips && !rungsInView && (
                <RatingFilter idle={!payload} value={settings.minRating} onChange={onFloor} />
              )}
            </>
          )}
        </div>
        {payload ? (
          <PeopleChips
            // In the map's own order: the spine names people by their
            // place in it, so only the row's drawing reorders.
            people={payload.people}
            carried={nowShown.carried}
            selected={selected}
            lit={lit}
            hovered={hovered}
            counts={counts}
            photoOf={photoOf}
            onToggle={onToggle}
            onHover={setHovered}
            onClear={() => setPeople(new Set())}
            onFace={faceCard.onFace}
            offFace={faceCard.offFace}
            hold={faceCard.hold}
            allRef={everyoneRef}
            lead={
              pillText ? (
                <>
                  <span className="cd-filter-pill">
                    <button
                      type="button"
                      ref={pillRef}
                      className="cd-filter-pill-body"
                      aria-label={`Filters: ${pillText}. Open View`}
                      onClick={() => {
                        openedFromPill.current = true;
                        setViewOpen(true);
                      }}
                    >
                      {pillText}
                    </button>
                    <button
                      type="button"
                      className="cd-filter-pill-x"
                      aria-label="Clear these filters"
                      onClick={clearPill}
                    >
                      ✕
                    </button>
                  </span>
                  <span className="cd-chips-sep" aria-hidden="true" />
                </>
              ) : undefined
            }
          />
        ) : holdsChips ? (
          <ChipSkeletons />
        ) : null}
        <Progress width={progress.width} showing={progress.showing} />
      </header>

      {mapPayload ? (
        <GridMap
          payload={mapPayload}
          // A map being left is drawn as it was last seen (see liveDraw),
          // and asks for nothing more: the words it would ask for belong
          // to a map the reader has left.
          settings={frozen?.settings ?? settings}
          selectedIdx={frozen?.selectedIdx ?? selectedIdx}
          hovered={frozen ? null : hovered}
          onCardHover={onCardHover}
          onOpen={openFilm}
          detail={frozen?.detail ?? detail}
          onNeedDetail={frozen ? ignoreDetail : onNeedDetail}
          onRevealed={toast.hide}
          covered={covered}
          recentreKey={relaid}
          scroller={scrollerRef}
          overlayH={overlayH}
          headerAway={headerAway}
          compact={screen.overlay}
          appScroll={appScroll}
          flown={flown}
          leaving={leaving}
          landing={landing}
          player={player}
          searchTyped={searchTyped}
          photoOf={photoOf}
          onFace={faceCard.onFace}
          offFace={faceCard.offFace}
        />
      ) : error ? (
        <MapError
          // Asks again for the same map, in place. It used to reopen the
          // movie through the router, which pushed a duplicate history
          // entry and, the id being the same, fetched nothing.
          onRetry={() => setAttempt((n) => n + 1)}
          onPickAnother={goHome}
        />
      ) : loading && !fromCold ? (
        // The waiting is said by the progress line and the toast. The
        // plot stays empty rather than holding a message the reader
        // would have to read and then watch disappear.
        <div className="cd-scroller" ref={scrollerRef} aria-hidden="true" />
      ) : pageDrawn === 'about' ? (
        <AboutPage dim={loading} />
      ) : pageDrawn === 'daily' ? (
        // Map this movie leaves for the answer's map as a pick from search
        // does, the Daily held dimmed behind the progress line until it
        // arrives.
        <DailyPage onDay={setDay} rulesSignal={rulesAsked} dim={loading} onOpenMovie={setMovieId} />
      ) : (
        <ColdStart
          onPick={setMovieId}
          onAbout={openAbout}
          onDaily={openDaily}
          theme={theme}
          onTheme={onTheme}
          markSlot={markSlot}
          onOpening={setOpening}
          dim={loading}
        />
      )}

      {glide && (
        <Flyer
          // One copy per glide: a new move puts down a new one.
          key={glide.token}
          face={glide.face}
          rect={glide.rect}
          aim={glide.aim}
          handle={flyer}
          scroller={scrollerRef}
          over={overOffset(screen)}
        />
      )}

      {open && payload && (
        <GridSheet
          film={open}
          payload={payload}
          photoOf={photoOf}
          onOnly={onOnly}
          onRemap={onRemap}
          onClose={() => setOpenId(null)}
          closer={sheetCloser}
          player={player}
        />
      )}

      {payload && (
        // One pill holding two buttons: View, and the quick "Hide empty
        // years" switch that grows out of its right side while it is
        // useful (see `quick`). Folded away, the switch is inert as well
        // as hidden, so a keyboard never lands on it.
        <div
          className={`cd-float cd-view-button${covered ? '' : ' cd-float-up'}`}
          aria-hidden={covered || undefined}
          inert={covered || undefined}
        >
          <span className="cd-float-pill">
            <button
              ref={viewOpenRef}
              type="button"
              className="cd-view-open"
              aria-label={`How the map is drawn, ${changed} changed`}
              onClick={() => setViewOpen(true)}
            >
              {/* Sliders: two rails, each with its knob set somewhere
                  along it. The knobs are filled with the card colour so
                  each one cuts its rail rather than sitting on it. */}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                <path d="M4 7h16M4 17h16" />
                <circle className="cd-view-knob" cx="9" cy="7" r="2.2" />
                <circle className="cd-view-knob" cx="15" cy="17" r="2.2" />
              </svg>
              {/* The word goes on phones and short screens, where View
                  is its icon alone. */}
              <span className="cd-view-label">View</span>
              {/* How many settings differ from the defaults. The label
                  says it for a screen reader, the badge for the eye. */}
              {changed > 0 && (
                <span className="cd-view-badge" aria-hidden="true">
                  {changed}
                </span>
              )}
            </button>
            <span
              className={`cd-view-quick${quick ? ' cd-view-quick-on' : ''}`}
              inert={!quick || undefined}
            >
              <span className="cd-view-rule" aria-hidden="true" />
              {/* Named outright: on phones and short screens the words
                  and the track give way to the icon, which says nothing
                  to a screen reader. */}
              <button
                type="button"
                role="switch"
                aria-checked={settings.hideEmptyYears}
                aria-label="Hide empty years"
                className="cd-view-quick-switch"
                onClick={flipHideEmpty}
              >
                <span className="cd-quick-icon" aria-hidden="true">
                  {/* Two arrows closing on a dashed line: rows folding together. */}
                  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M12 2v6M9 5l3 3 3-3M12 22v-6M9 19l3-3 3 3M3 12h2M9 12h2M13 12h2M19 12h2" />
                  </svg>
                </span>
                <span className="cd-quick-track" aria-hidden="true">
                  <span className="cd-quick-knob" />
                </span>
                <span className="cd-quick-label">Hide empty years</span>
              </button>
            </span>
          </span>
        </div>
      )}

      {viewOpen && (
        <ViewPanel
          settings={settings}
          onChange={setSettings}
          onRelaid={() => setRelaid((n) => n + 1)}
          rungs={rungsInView}
          bounds={bounds}
          perYear={perYear}
          anchorYear={payload?.anchor.year ?? 0}
          rangeEmpty={payload != null && rangeHoldsNone(payload, settings)}
          payload={payload}
          onFloor={onFloor}
          theme={theme}
          onTheme={onTheme}
          closer={viewCloser}
          onClose={() => {
            setViewOpen(false);
            // A panel opened from the pill gives the focus back to it,
            // rather than dropping it on the document. If what was set in
            // the panel took the pill away, the focus goes where the ✕
            // sends it: the first chip, where the row starts.
            if (openedFromPill.current) {
              openedFromPill.current = false;
              (pillRef.current ?? everyoneRef.current)?.focus();
            }
          }}
        />
      )}

      {faceCard.card && cardPerson && cardPhoto && payload && (
        <PersonCard
          // Mounted afresh for each card, so each comes in from its edge.
          key={`${faceCard.card.id}:${faceCard.card.from}`}
          card={faceCard.card}
          person={cardPerson}
          photo={cardPhoto}
          anchorTitle={payload.anchor.title}
        />
      )}

      {/* The Daily has a toast of its own, raised clear of its guess
          bar. Two would talk over each other. */}
      {!daily && <Toast spec={toast.spec} visible={toast.visible} onMap={onMap} />}
      <p className="cd-sr-live" aria-live="polite">
        {!payload
          ? ''
          : settings.hideEmptyYears
            ? `Showing ${yearsShowing} ${yearsShowing === 1 ? 'year' : 'years'}`
            : 'Map ready'}
      </p>
    </div>
  );
}

/** What a map being left asks for: nothing. */
function ignoreDetail() {}

/** The copy of a card that flies from the map being left to the new
 *  map's searched film.
 *
 *  It is two copies of the card itself, taken as it stood, one over the
 *  other. The one below is the card exactly, so the copy takes off
 *  without a seam. The one above is dressed as the searched card — its
 *  fill, its ring and a shadow for being off the page, and none of the
 *  person marks the searched card does not carry — and fades in as the
 *  copy leaves, with the one below fading out from under it after. It
 *  lands dressed exactly as the card it lands on, its shadow settling to
 *  that card's and the Searched tag coming in over the last of the
 *  landing, so the card can take over from it in a single frame without
 *  anything changing.
 *
 *  The flight starts as it is put down, before it is painted, and the
 *  landing turns it towards the new card from wherever it has got to
 *  (see Glider). */
function Flyer({
  face,
  rect,
  aim,
  handle,
  scroller,
  over,
}: {
  face: HTMLElement;
  rect: Box;
  /** Where it will land, when that is known as it takes off. */
  aim: Box | null;
  handle: RefObject<FlyerHandle | null>;
  scroller: RefObject<HTMLDivElement | null>;
  /** How much of the top of the map the header is lying over. */
  over: number;
}) {
  const el = useRef<HTMLDivElement>(null);
  const tag = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const box = el.current;
    if (!box) return;
    const plain = flyerFace(face, false);
    const dressed = flyerFace(face, true);
    box.prepend(plain, dressed);
    const glider = new Glider(box);
    glider.aim(takeOff(rect, aim, scroller.current?.getBoundingClientRect(), over));
    animate(dressed, [{ opacity: 0 }, { opacity: 1 }], { duration: FACE_MS, easing: 'ease' });
    animate(plain, [{ opacity: plain.style.opacity || 1 }, { opacity: 0 }], {
      duration: PLAIN_OUT_MS,
      delay: FACE_MS,
      fill: 'forwards',
    });
    let tagIn: Animation | null = null;
    const mine: FlyerHandle = {
      land(to, done) {
        const t = landingTransform(rect, to);
        const took = glider.aim({ x: t.tx, y: t.ty, sx: t.sx, sy: t.sy }, done);
        if (took == null) return null;
        dressed.style.transition = `box-shadow ${Math.round(took)}ms ${EASE.glide}`;
        dressed.classList.add('cd-flyer-land');
        // All of it inside the landing, however short, so the tag is
        // whole by the time the card takes over.
        const fade = Math.min(TAG_IN_MS, took);
        tagIn?.cancel();
        tagIn = animate(tag.current, [{ opacity: 0 }, { opacity: 1 }], {
          duration: fade,
          delay: took - fade,
          fill: 'both',
        });
        glider.follow(tagIn);
        return took;
      },
    };
    handle.current = mine;
    // Called off, or StrictMode's second mount, which puts it down again
    // and starts the flight over from the card.
    return () => {
      glider.cancel();
      plain.remove();
      dressed.remove();
      if (handle.current === mine) handle.current = null;
    };
    // Once, as the copy is put down.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const tagAt = searchedTagAt({ left: 0, top: 0 });
  return (
    <div
      ref={el}
      className="cd-flyer"
      style={{ left: rect.left, top: rect.top, width: rect.width, height: rect.height }}
      aria-hidden="true"
      inert
    >
      <span ref={tag} className="cd-searched-tag cd-flyer-tag" style={tagAt}>
        Searched
      </span>
    </div>
  );
}

/** Where the copy first heads, grown towards the reader: straight for
 *  the card it will land on when the next map is in hand, and otherwise
 *  for the middle of the map, to wait there. */
function takeOff(rect: Box, aim: Box | null, scroller: Box | undefined, over: number): Pose {
  if (aim) {
    const t = landingTransform(rect, aim);
    return { x: t.tx, y: t.ty, sx: t.sx * FLY_SCALE, sy: t.sy * FLY_SCALE };
  }
  const { tx, ty } = scroller ? flightTo(rect, scroller, over) : { tx: 0, ty: 0 };
  return { x: tx, y: ty, sx: FLY_SCALE, sy: FLY_SCALE };
}

/** One face of the flying copy: a copy of the card as it stood, put at
 *  the flyer's corner and cut loose from the map. Dressed, it wears the
 *  searched card's classes and drops the marks that card does not show. */
function flyerFace(card: HTMLElement, dressed: boolean): HTMLElement {
  const face = card.cloneNode(true) as HTMLElement;
  // A card filtered out after its sheet opened is disabled, and the copy
  // lands as the next map's searched card, which never is.
  for (const a of ['data-card', 'aria-label', 'tabindex', 'type', 'disabled']) face.removeAttribute(a);
  face.className = `cd-card cd-flyer-face${dressed ? ' cd-card-anchor' : ''}`;
  const s = face.style;
  s.left = '0px';
  s.top = '0px';
  // The plain face is the card as it stood, dimmed under a selection, a
  // floor or the genres included, so the copy takes off at the card's
  // own strength and brightens as the dressed face comes in over it. The
  // dressed one starts from clear.
  s.opacity = dressed ? '' : card.style.opacity;
  s.transform = '';
  s.transitionDelay = '';
  s.pointerEvents = '';
  // A poster the card has already drawn is drawn again at once, rather
  // than decoded off to the side for a frame with nothing in its place.
  for (const img of face.querySelectorAll('img')) img.decoding = 'sync';
  if (dressed) for (const n of face.querySelectorAll('.cd-card-mark, .cd-more')) n.remove();
  return face;
}

/** The cold start, keeping whatever query the page was opened with. */
function homeHref(): string {
  return `/${location.search}${location.hash}`;
}

/** Every map has an address, so it can be shared and reloaded. */
function historyDepth(state: unknown): number {
  if (!state || typeof state !== 'object' || !('depth' in state)) return 0;
  const d = (state as { depth: unknown }).depth;
  return typeof d === 'number' && d > 0 ? d : 0;
}

/** A click the browser keeps: one with a modifier key held, or not with
 *  the main button, opens the link in a new tab or window, or not at
 *  all, as the reader asked. */
function browserKeeps(e: MouseEvent<HTMLAnchorElement>): boolean {
  return e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0;
}

/** Where the reader is, and the ways to move. The route is one of four
 *  things: a map (`movieId`), the About page (`about`), the Daily
 *  (`daily`), or, with none of them, the opening screen. */
export interface FilmRoute {
  movieId: string | null;
  about: boolean;
  daily: boolean;
  /** This visit has an entry before this one to go back to. */
  canGoBack: boolean;
  openMovie: (id: string, title?: string) => void;
  openAbout: (e?: MouseEvent<HTMLAnchorElement>) => void;
  openDaily: (e?: MouseEvent<HTMLAnchorElement>) => void;
  goBack: () => void;
  goHome: (e?: MouseEvent<HTMLAnchorElement>) => void;
}

/** A visit that begins on a map, the About page or the Daily never plays
 *  the opening, not even when the reader later goes home: the header is
 *  complete from the first paint. */
export function skipsOpening(pathname: string): boolean {
  return movieIdFromPath(pathname) !== null || isAboutPath(pathname) || isDailyPath(pathname);
}

/** The three pages without a map. */
export type Page = 'cold' | 'about' | 'daily';

/** The page without a map that a route names. */
export function pageOf(about: boolean, daily: boolean): Page {
  return about ? 'about' : daily ? 'daily' : 'cold';
}

/** Which page without a map is drawn, given the one the route names and
 *  the one drawn before. It follows the route while there is no movie
 *  and holds once one is picked, so a map picked from the About page's
 *  search loads over the About page, as one picked on the opening screen
 *  loads over that and one reached from the Daily by Forward loads over
 *  the Daily, and no page is put down afresh in the render before the
 *  load begins. */
export function drawsPage(movieId: string | null, page: Page, was: Page): Page {
  return movieId === null ? page : was;
}

/** What the header's Back says, or null where there is none. It is on a
 *  map, on the About page and on the Daily, and only when there is
 *  somewhere to go back to. The opening screen has its own ways on — the
 *  tiles and the search — and a map, an About page or a Daily opened
 *  from a link has nothing behind it. */
export function backLabel({
  movieId,
  about,
  daily,
  canGoBack,
}: Pick<FilmRoute, 'movieId' | 'about' | 'daily' | 'canGoBack'>): string | null {
  if (!canGoBack) return null;
  if (movieId !== null) return 'Back to the previous movie';
  return about || daily ? 'Back' : null;
}

export function useFilmRoute(adopt: { current: (filters: MapFilters) => void }): FilmRoute {
  const [route, setRoute] = useState(() => routeAt(location.pathname));
  const [depth, setDepth] = useState(() => historyDepth(history.state));
  // Every move ends here, once the address has changed: the route is
  // read back from the address rather than set beside it, so the page
  // drawn is always the one the address names. The same route again
  // keeps the same object, and asks for no render.
  const follow = useCallback(() => {
    const now = routeAt(location.pathname);
    setRoute((was) =>
      was.movieId === now.movieId && was.about === now.about && was.daily === now.daily ? was : now,
    );
  }, []);
  useEffect(() => {
    // A page opened from a link or typed in has no entry of its own yet.
    // It gets one at depth zero, so Back is not offered from it.
    if (history.state == null) {
      history.replaceState(forwardEntry(0), '');
      setDepth(0);
    }
    const onPop = () => {
      follow();
      setDepth(historyDepth(history.state));
      adopt.current(filtersFromState(history.state));
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [adopt, follow]);
  const go = useCallback((id: string, title?: string) => {
    const next = historyDepth(history.state) + 1;
    const path = filmPath(id, title);
    // Opening the movie already on screen — picking it again from
    // search while its map is up — keeps this visit's filters. A map
    // that failed never gets here: Try again, or picking the same movie
    // after the failure, asks for the map again without moving. A different movie starts clear, and the clear
    // is stamped on the new entry so the one left behind stays as it was.
    if (movieIdFromPath(location.pathname) === id) {
      history.pushState({ depth: next, filters: filtersFromState(history.state) }, '', path);
      follow();
      setDepth(next);
      return;
    }
    history.pushState(forwardEntry(next), '', path);
    follow();
    setDepth(next);
    adopt.current(freshFilters());
  }, [adopt, follow]);
  // The About page, from the opening screen's footer: a new entry one
  // deeper, as a map is, so Back returns to where the link was.
  const openAbout = useCallback((e?: MouseEvent<HTMLAnchorElement>) => {
    if (e) {
      if (browserKeeps(e)) return;
      e.preventDefault();
    }
    if (isAboutPath(location.pathname)) return;
    const next = historyDepth(history.state) + 1;
    history.pushState(forwardEntry(next), '', ABOUT_PATH);
    follow();
    setDepth(next);
    adopt.current(freshFilters());
  }, [adopt, follow]);
  // The Daily, from the opening screen's banner: a new entry one deeper,
  // as the About page is, so Back returns to the opening screen.
  const openDaily = useCallback((e?: MouseEvent<HTMLAnchorElement>) => {
    if (e) {
      if (browserKeeps(e)) return;
      e.preventDefault();
    }
    if (isDailyPath(location.pathname)) return;
    const next = historyDepth(history.state) + 1;
    history.pushState(forwardEntry(next), '', DAILY_PATH);
    follow();
    setDepth(next);
    adopt.current(freshFilters());
  }, [adopt, follow]);
  const back = useCallback(() => {
    if (historyDepth(history.state) === 0) return;
    history.back();
  }, []);
  // Also called without an event, by "Pick another movie" on the error.
  const home = useCallback((e?: MouseEvent<HTMLAnchorElement>) => {
    if (e) {
      if (browserKeeps(e)) return;
      e.preventDefault();
    }
    // Already home. The About page and the Daily have no movie in their
    // paths either, but neither is home, so each moves like a map does.
    const p = location.pathname;
    if (movieIdFromPath(p) === null && !isAboutPath(p) && !isDailyPath(p)) {
      follow();
      return;
    }
    const next = historyDepth(history.state) + 1;
    history.pushState(forwardEntry(next), '', homeHref());
    follow();
    setDepth(next);
    adopt.current(freshFilters());
  }, [adopt, follow]);
  return {
    movieId: route.movieId,
    about: route.about,
    daily: route.daily,
    canGoBack: depth > 0,
    openMovie: go,
    openAbout,
    openDaily,
    goBack: back,
    goHome: home,
  };
}

type SetSettings = (s: GridSettings | ((was: GridSettings) => GridSettings)) => void;

/** A sheet or panel's own way out: its exit, then `then`. */
type Closer = (then?: () => void) => void;

function SearchField({
  placeholder,
  onPick,
  onFocusChange,
  onTyped,
  shortcuts,
}: {
  /** What the empty field says (see searchPlaceholder). */
  placeholder: string;
  onPick: (id: string, title?: string) => void;
  /** The header must not slide away from under a reader who is typing,
   *  nor lie under the blur behind a preview trailer (headerClass). */
  onFocusChange: (on: boolean) => void;
  /** Whether the field has text in it, for the map's hover preview. */
  onTyped: (typed: boolean) => void;
  /** Whether ⌘K, Ctrl+K and "/" may bring the reader here. */
  shortcuts: boolean;
}) {
  const [query, setQuery] = useState('');
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [busy, setBusy] = useState(false);
  const [at, setAt] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listId = useId();
  const theme = useResolvedTheme();
  const typed = query.trim();
  // The list is up while there is a query worth asking about, whether
  // or not the field still has the focus: a reader who looks away from
  // the field has not withdrawn the question. It is a listbox only when
  // it holds films; otherwise it is a line saying why not.
  const open = typed.length >= 2;
  const listed = open && hits.length > 0;

  useEffect(() => {
    if (typed.length < 2) {
      setHits([]);
      return;
    }
    const ctrl = new AbortController();
    setBusy(true);
    const timer = window.setTimeout(() => {
      searchMovies(typed, ctrl.signal)
        .then((r) => {
          setHits(r);
          setAt(0);
        })
        .catch(() => {})
        .finally(() => setBusy(false));
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      window.clearTimeout(timer);
      ctrl.abort();
    };
  }, [typed]);

  // ⌘K or Ctrl+K from anywhere, and "/" from anywhere that is not a
  // text field, put the reader in the search. The browser's own use of
  // the key is stopped: "/" is find-in-page in Firefox, and Ctrl+K is
  // the browser's search bar.
  useEffect(() => {
    if (!shortcuts) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || !isSearchShortcut(e, isTyping(document.activeElement))) return;
      e.preventDefault();
      inputRef.current?.focus();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [shortcuts]);

  // The highlighted film stays in sight as the arrows move through a
  // list taller than its box.
  useEffect(() => {
    if (!listed) return;
    document.getElementById(optionId(listId, at))?.scrollIntoView({ block: 'nearest' });
  }, [listed, at, listId]);

  const choose = (hit: SearchHit) => {
    setQuery('');
    setHits([]);
    // The field is not in a form, so the phone's Search key leaves it
    // focused and the keyboard stays up unless we dismiss it.
    inputRef.current?.blur();
    onPick(hit.id, hit.title);
  };

  // Escape gives the field back before it gives up the map behind it.
  useEscape(() => {
    if (typed.length > 0) setQuery('');
  });
  // The same rule for the map's hover preview, which hears Escape on its
  // own and leaves this one to the field while it has text.
  const hasText = typed.length > 0;
  useEffect(() => onTyped(hasText), [hasText, onTyped]);

  return (
    <div
      className="cd-search"
      onFocus={() => onFocusChange(true)}
      onBlur={() => onFocusChange(false)}
    >
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
        <circle cx="11" cy="11" r="7" />
        <path d="M21 21l-4.3-4.3" />
      </svg>
      <input
        ref={inputRef}
        type="search"
        enterKeyHint="search"
        autoComplete="off"
        value={query}
        placeholder={placeholder}
        aria-label="Search for a movie"
        aria-keyshortcuts="Meta+K Control+K /"
        // A combobox: the arrows move a highlight through the list while
        // the focus stays here, and the highlight is announced as if it
        // had the focus.
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={listed}
        aria-controls={listed ? listId : undefined}
        aria-activedescendant={listed ? optionId(listId, at) : undefined}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          // The arrows would otherwise move the caret to the ends of
          // the text as well as the highlight.
          if (e.key === 'ArrowDown') {
            e.preventDefault();
            setAt((i) => Math.max(0, Math.min(i + 1, hits.length - 1)));
          } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            setAt((i) => Math.max(i - 1, 0));
          } else if (e.key === 'Enter') {
            e.preventDefault();
            if (listed && hits[at]) choose(hits[at]);
            else inputRef.current?.blur();
          } else if (e.key === 'Escape') {
            // Clears and lets go, even when there was nothing to clear:
            // in the field, Escape is the way back out to the map.
            setQuery('');
            inputRef.current?.blur();
          }
        }}
      />
      {/* Hidden once the field has the focus, and on phones, which have
          no keyboard to press it on (see .cd-kbd). */}
      {query === '' && (
        <span className="cd-kbd" aria-hidden="true">
          ⌘K
        </span>
      )}
      {open && (
        <div className="cd-results">
          {listed ? (
            <ul className="cd-results-list" role="listbox" id={listId} aria-label="Movies">
              {hits.map((h, i) => (
                <li key={h.id} role="presentation">
                  <button
                    type="button"
                    role="option"
                    id={optionId(listId, i)}
                    aria-selected={i === at}
                    // Reached with the arrows from the field, not with Tab.
                    tabIndex={-1}
                    className={`cd-result${i === at ? ' cd-result-at' : ''}`}
                    // On pointerdown, not click: the input blurs first and
                    // would take the list down before the tap ever landed.
                    onMouseDown={(e) => {
                      e.preventDefault();
                      choose(h);
                    }}
                    // A screen reader's "activate" is a click with no
                    // press before it. After a press the list is gone, so
                    // this never picks twice.
                    onClick={() => choose(h)}
                  >
                    <PosterImage
                      id={h.id}
                      url={h.poster}
                      blankClassName="cd-result-blank"
                      width={28}
                      height={42}
                      loading="lazy"
                      // The film's own hue until the picture arrives, and
                      // instead of one when there is none.
                      style={{ background: posterFallback(h.title, theme) }}
                    />
                    <span className="cd-result-title">
                      {h.title}
                      {h.year ? <span className="cd-result-year"> ({h.year})</span> : null}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="cd-result-note">{busy ? 'Searching…' : `No movies match “${typed}”`}</p>
          )}
        </div>
      )}
    </div>
  );
}

/** The id of the result at `i`, for the field to point at. */
function optionId(listId: string, i: number): string {
  return `${listId}-${i}`;
}

/** A floor, not a window: a reader asks for "at least a seven", and the
 *  grid's own x axis already shows them how far above it everything sits. */
function RatingFilter({
  idle,
  value,
  onChange,
}: {
  /** The map is still being fetched: the rungs show where they will be,
   *  but there is nothing yet for them to filter. */
  idle: boolean;
  value: number | null;
  onChange: (v: number | null) => void;
}) {
  return (
    <div
      className={`cd-rating-filter${idle ? ' cd-rating-filter-idle' : ''}`}
      role="group"
      aria-label="Light movies by rating"
      inert={idle || undefined}
    >
      {/* The group's own name already says it. */}
      <span className="cd-rating-label" aria-hidden="true">
        Rating
      </span>
      <div className="cd-rungs">
        {[null, ...RATING_STOPS].map((r) => {
          const on = value === r;
          return (
            <button
              key={r ?? 'any'}
              type="button"
              className={`cd-rung${on ? ' cd-rung-on' : ''}`}
              aria-pressed={on}
              aria-label={r == null ? undefined : `Light movies rated at least ${r.toFixed(1)}`}
              // Pressing the lit rung again takes the floor away, the
              // same as Any.
              onClick={() => onChange(r == null || on ? null : r)}
            >
              {rungLabel(r)}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** Eight inert pills where the chips will be. The widths are uneven on
 *  purpose: a row of identical bars reads as a loading bar, not as names
 *  that are about to arrive. */
function ChipSkeletons() {
  // Each after "Everyone" has room for a face beside its name, so the
  // row does not jump when the names arrive.
  const widths = [96, 144, 176, 162, 136, 144, 146, 140];
  return (
    <div className="cd-chips cd-chips-loading" aria-hidden="true">
      {widths.map((w, i) => (
        <span key={i} className="cd-chip-skeleton" style={{ width: w }} />
      ))}
    </div>
  );
}

/** What went wrong is never the reader's problem to parse, so the raw
 *  message stays out of it. Both ways forward are offered. */
function MapError({ onRetry, onPickAnother }: { onRetry: () => void; onPickAnother: () => void }) {
  return (
    <div className="cd-error" role="alert">
      <h2 className="cd-error-title">We couldn’t open this map</h2>
      <p className="cd-error-body">
        The movie database didn’t answer. Check your connection, then try again.
      </p>
      <button type="button" className="cd-error-primary" onClick={onRetry}>
        Try again
      </button>
      <button type="button" className="cd-error-secondary" onClick={onPickAnother}>
        Pick another movie
      </button>
    </div>
  );
}

/** Whether this page has played the opening. It plays once: coming back
 *  to the opening screen from a map does not replay it, and a visit that
 *  began on a map never plays it at all. Module-wide, because the
 *  opening screen is mounted afresh each time the reader comes back. */
let openingPlayed = false;

/** The trip from the tiles to the header's slot. */
type Flight = ReturnType<typeof markFlight>;

/** The mark, brought into focus over the tiles while the opening screen
 *  waits for its films.
 *
 *  It is the one thing on the page that says "working" — and unlike a
 *  spinner it says what is working. It racks into focus over the empty
 *  frames and then flies into the header to become the C of the
 *  wordmark; it never fades out where it was drawn, because a mark that
 *  dissolves over the films is a spinner pretending to be a logo.
 *
 *  The box carries the flight, as a transition on its own transform; the
 *  drawing inside carries the focus, the breathing and the blur, played
 *  from script, so the two never fight over one property. */
function LoadingMark({
  at,
  flight,
  svgRef,
}: {
  at: Spot;
  flight: Flight | null;
  svgRef: RefObject<SVGSVGElement | null>;
}) {
  // Started as the mark is put down, before it is painted. The focus
  // holds it invisible for its first 140 ms, so the mark is never seen
  // sharp before it has come into focus.
  useLayoutEffect(() => {
    const focus = animate(svgRef.current, FOCUS_KEYFRAMES, {
      duration: FOCUS_MS,
      delay: FOCUS_DELAY_MS,
      easing: EASE.focus,
      fill: 'both',
    });
    return () => focus?.cancel();
  }, [svgRef]);
  return (
    <span
      className="cd-cold-mark"
      style={{
        left: at.x,
        top: at.y,
        // Centred on its spot, then carried to the header. The scale is
        // about the centre, which is what keeps the landing on the slot
        // rather than beside it.
        transform: flight
          ? `translate(-50%, -50%) translate(${flight.dx}px, ${flight.dy}px) scale(${flight.scale})`
          : 'translate(-50%, -50%)',
        transition: flight ? `transform ${GLIDE_MS}ms var(--ease-glide)` : undefined,
      }}
      aria-hidden="true"
    >
      <svg
        ref={svgRef}
        className="cd-cold-mark-svg"
        viewBox="15.5 9.5 29.5 45"
        width={LOADER_W}
        height={LOADER_H}
        focusable="false"
      >
        <path
          d="M42.7 47 A14 14 0 1 1 42.7 29 C38 23.5 31 19 29 13 C33 11.8 37.5 11.6 41.5 12.4"
          fill="none"
          stroke="currentColor"
          strokeWidth="5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle className="cd-cold-mark-dot" cx="32" cy="38" r="5" />
      </svg>
    </span>
  );
}

/** One tile on the opening screen.
 *
 *  The frame is on screen from the first paint, before there is a film
 *  to put in it. Then the film comes into focus in it — its colour, with
 *  the poster inside that colour once the picture has decoded, so the
 *  picture landing is a sharpening rather than an appearance.
 *
 *  Each tile waits only for its own poster. The screen used to hold all
 *  eight back until the slowest had decoded, which meant one bad image
 *  host decided when anybody saw anything. */
function ColdTile({
  film,
  index,
  shown: letIn,
  theme,
  onPick,
}: {
  film: FirstRunFilm | undefined;
  index: number;
  /** Set once the mark has lifted off. The fills wait for that rather
   *  than for the list, so nothing starts underneath the loader, and a
   *  tile cannot be picked before its film is in it. */
  shown: boolean;
  theme: Theme;
  onPick: (id: string, title?: string) => void;
}) {
  const { src, onError } = usePosterSrc(film?.poster, TILE_W, film?.id);
  const [shown, setShown] = useState(false);
  useEffect(() => {
    setShown(false);
  }, [src]);

  return (
    <button
      type="button"
      className={`cd-cold-tile${film && letIn ? ' cd-cold-tile-in' : ''}`}
      style={{ ['--i' as string]: index }}
      aria-hidden={film ? undefined : true}
      tabIndex={film ? undefined : -1}
      onClick={() => film && letIn && onPick(film.id, film.title)}
    >
      <span className="cd-cold-frame">
        {film && (
          <span
            className="cd-cold-fill"
            style={{ background: film.c ?? posterFallback(film.title, theme) }}
          >
            {src && (
              <img
                src={src}
                alt=""
                decoding="async"
                fetchPriority={index < EAGER_TILES ? 'high' : 'low'}
                data-in={shown || undefined}
                onLoad={(e) => {
                  // decode() rather than load alone, so the fade is over a
                  // frame the browser can already paint. Either outcome
                  // shows it: a picture that will not decode is one the
                  // browser will draw badly, not one to hide.
                  e.currentTarget.decode().then(
                    () => setShown(true),
                    () => setShown(true),
                  );
                }}
                onError={onError}
              />
            )}
          </span>
        )}
      </span>
      <span className="cd-cold-meta">
        <span className="cd-cold-title">{film?.title ?? ''}</span>
        <span className="cd-cold-year">{film?.year ?? ''}</span>
      </span>
    </button>
  );
}

/** The veil's life: mounted and still down, raised (at VEIL_AT_MS) while
 *  the mark comes into focus, fading as it glides home, and gone. A
 *  visit that does not play the opening starts at gone and never mounts
 *  it. */
type Veil = 'down' | 'up' | 'leaving' | 'gone';

/** The face the headline is set in, as the font loader is asked for it. */
const HEADLINE_FACE = '400 46px "Young Serif"';

function ColdStart({
  onPick,
  onAbout,
  onDaily,
  theme,
  onTheme,
  markSlot,
  onOpening,
  dim,
}: {
  onPick: (id: string, title?: string) => void;
  /** The router's way to the About page, for its link in the footer. */
  onAbout: (e: MouseEvent<HTMLAnchorElement>) => void;
  /** The router's way to the Daily, for the banner over the headline. */
  onDaily: (e: MouseEvent<HTMLAnchorElement>) => void;
  theme: ThemePref;
  onTheme: (p: ThemePref) => void;
  /** The header's empty mark slot, which is where the loader is going. */
  markSlot: RefObject<HTMLSpanElement | null>;
  onOpening: (phase: Opening) => void;
  /** A map is being fetched from here. The screen steps back, and the
   *  opening, if it is still playing, is finished at once. */
  dim: boolean;
}) {
  // The films, once they are known. A late answer does not swap a new
  // eight in under one the reader is already looking at.
  const [tiles, setTiles] = useState<FirstRunFilm[] | null>(null);
  // Set when the list could not be fetched at all. Eight empty frames
  // that never fill is the screen saying nothing, forever.
  const [failed, setFailed] = useState(false);
  // The copy mounts in its "from" state and is let go a frame later: a
  // transition needs a committed state to travel out of, so setting the
  // opacity in the same render that mounts the element leaves the
  // browser nothing to animate between.
  const [textIn, setTextIn] = useState(false);
  const [box, setBox] = useState(() => ({ w: window.innerWidth, h: window.innerHeight }));
  const drawn = useResolvedTheme();
  // Today's Daily, for the banner over the headline. Its box is drawn
  // from the first paint, so the grid is measured under it; it goes only
  // when the server cannot offer today's puzzle.
  const daily = useDailyBanner();
  const bannerUp = daily.state !== 'gone';
  const banner = useRef<HTMLAnchorElement>(null);
  // Where the grid actually starts. The row count needs it, and the
  // alternative is keeping a copy of the stylesheet's paddings in
  // JavaScript and remembering to change both.
  const grid = useRef<HTMLDivElement>(null);
  const [gridTop, setGridTop] = useState<number | undefined>(undefined);
  useLayoutEffect(() => {
    const el = grid.current;
    if (!el) return;
    const read = () => {
      const top = el.getBoundingClientRect().top;
      // A page that has never been laid out — one opened in a
      // background tab — measures zero everywhere. The stand-in
      // numbers are better than a grid of one row.
      setGridTop(top > 0 ? top : undefined);
    };
    read();
    const ro = new ResizeObserver(read);
    ro.observe(document.documentElement);
    // The window does not change size when the banner does: its
    // name taking a second line on a phone, once the button says
    // "Keep going", moves the grid down without moving anything this
    // would otherwise hear. And the banner going is measured again here,
    // with this run again for it.
    if (banner.current) ro.observe(banner.current);
    return () => ro.disconnect();
  }, [bannerUp]);

  // The copy fades in from the moment the frames are on screen. Young
  // Serif is preloaded, so it is nearly always in hand by then; when it
  // is not, the fade waits for it — never longer than FONT_WAIT_MS — so
  // the balanced headline is not rewrapped under the reader halfway
  // through fading in, the one movement on this screen nobody asked for.
  useEffect(() => {
    let live = true;
    let frame = 0;
    const show = () => {
      if (live) frame = window.requestAnimationFrame(() => setTextIn(true));
    };
    const fonts = document.fonts;
    if (!fonts || fonts.check(HEADLINE_FACE)) {
      show();
    } else {
      const cap = new Promise((r) => window.setTimeout(r, FONT_WAIT_MS));
      void Promise.race([fonts.load(HEADLINE_FACE), cap]).then(show, show);
    }
    return () => {
      live = false;
      window.cancelAnimationFrame(frame);
    };
  }, []);

  useEffect(() => {
    const onResize = () => setBox({ w: window.innerWidth, h: window.innerHeight });
    window.addEventListener('resize', onResize);
    window.addEventListener('orientationchange', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
      window.removeEventListener('orientationchange', onResize);
    };
  }, []);

  useEffect(() => {
    const ctrl = new AbortController();
    fetchFirstRun(ctrl.signal)
      .then((hits) => setTiles(tilesFrom(hits)))
      .catch((e: Error) => {
        if (e.name === 'AbortError') return;
        // The catalog is the only source, so there is nothing to fall
        // back to — but silence is not an answer. The search field is
        // the way on, which is why there is no retry button here.
        setTiles([]);
        setFailed(true);
      });
    return () => ctrl.abort();
  }, []);

  const room = coldScreenCount(box.w, box.h, gridTop);
  const shown = tiles ? tiles.slice(0, room) : [];
  const waiting = tiles === null;

  // Which visit this is, settled once: the first plays the opening, any
  // later one the return. A reader who has asked for no movement gets
  // neither, whichever it is.
  const still = useReducedMotion();
  const [first] = useState(() => !openingPlayed);
  // The frames are on screen from the first paint, and every time in
  // the opening is measured from it.
  const started = useRef(performance.now());
  // Under way and not yet over. Cleared by the landing, and by anything
  // that finishes the opening early.
  const playing = useRef(first && !still);
  // Once the mark has lifted off, where it was drawn is fixed.
  const lifted = useRef(false);

  // Where the loader sits while it comes into focus, and where it is
  // headed. Null once it has landed and the header owns the mark again.
  const [spot, setSpot] = useState<Spot | null>(null);
  const [flight, setFlight] = useState<Flight | null>(null);
  // The tiles wait for the mark to lift off rather than for the list,
  // so the fills never start underneath it.
  const [tilesIn, setTilesIn] = useState(false);
  const [veil, setVeil] = useState<Veil>(() => (playing.current ? 'down' : 'gone'));
  const markSvg = useRef<SVGSVGElement>(null);
  const breathing = useRef<Animation | null>(null);
  const timers = useRef<number[]>([]);
  useEffect(() => () => timers.current.forEach(window.clearTimeout), []);

  const opening = useRef(onOpening);
  opening.current = onOpening;
  const slot = useRef(markSlot);
  slot.current = markSlot;
  const spotRef = useRef<Spot | null>(null);
  spotRef.current = spot;

  // Claim the header's mark on the way in, or hand it over complete. The
  // phase lives in GridApp so it outlives this component: the mark ends
  // up in the wordmark, and only GridApp renders both.
  useEffect(() => {
    if (first) openingPlayed = true;
    opening.current(playing.current ? 'draw' : 'done');
    // Once, on the way in: a later change of mind about motion finishes
    // the opening (below) rather than starting it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Straight to the end: header complete, veil down, loader gone, films
  // let in the moment they are here.
  const finish = useCallback(() => {
    playing.current = false;
    timers.current.forEach(window.clearTimeout);
    timers.current = [];
    breathing.current?.cancel();
    breathing.current = null;
    setSpot(null);
    setFlight(null);
    setVeil((v) => (v === 'up' ? 'leaving' : v === 'down' ? 'gone' : v));
    setTilesIn(true);
    opening.current('done');
  }, []);

  // The glide home: the mark flies to the header's slot, softening a
  // little on the way; the page comes back up as it leaves; the posters
  // follow it into focus, then the word, and the header's own mark takes
  // over from the loader in the same frame the loader goes.
  const liftOff = () => {
    if (!playing.current) return;
    const target = slot.current.current?.getBoundingClientRect();
    const here = spotRef.current;
    if (!target || !(target.width > 0) || !here) {
      // Nowhere to fly to. Better a complete header than a mark
      // stranded over the films.
      finish();
      return;
    }
    lifted.current = true;
    const svg = markSvg.current;
    // Whatever the breathing had it at, so cutting the loop short is
    // never seen as a jump.
    const now = svg ? getComputedStyle(svg).filter : 'none';
    breathing.current?.cancel();
    breathing.current = null;
    animate(
      svg,
      [
        { filter: now && now !== 'none' ? now : 'blur(0px)' },
        { filter: `blur(${GLIDE_BLUR_PX}px)`, offset: 0.4 },
        { filter: 'blur(0px)' },
      ],
      { duration: GLIDE_MS, easing: EASE.glide, fill: 'forwards' },
    );
    setFlight(markFlight(target, here));
    setVeil((v) => (v === 'up' ? 'leaving' : v));
    timers.current.push(window.setTimeout(() => setTilesIn(true), TILES_AFTER_LIFT_MS));
    timers.current.push(
      window.setTimeout(() => opening.current('word'), GLIDE_MS - WORD_BEFORE_LANDING_MS),
    );
    timers.current.push(
      window.setTimeout(() => {
        playing.current = false;
        opening.current('done');
        setSpot(null);
      }, GLIDE_MS),
    );
  };

  // The whole opening, in one place: focus, lift, land.
  //
  // A list already in hand skips all of it — there is nothing to wait
  // for, so there is nothing to say — and so does a reader who has asked
  // for no movement, or who has picked a film before it was over.
  useEffect(() => {
    if (!playing.current) {
      if (!waiting) setTilesIn(true);
      return;
    }
    if (still || dim) {
      finish();
      return;
    }
    const elapsed = performance.now() - started.current;
    if (waiting) {
      // A slow list: once the focus has settled, it breathes until the
      // list lands. Nobody can know in advance that it will be slow.
      const t = window.setTimeout(() => {
        if (!playing.current || lifted.current) return;
        breathing.current = animate(
          markSvg.current,
          [
            { filter: 'blur(0px)' },
            { filter: `blur(${BREATHE_BLUR_PX}px)` },
            { filter: 'blur(0px)' },
          ],
          { duration: BREATHE_MS, iterations: Infinity, easing: 'ease-in-out' },
        );
      }, Math.max(0, BREATHE_AT_MS - elapsed));
      return () => window.clearTimeout(t);
    }
    const plan = openingPlan(elapsed);
    if (plan.fast) {
      finish();
      return;
    }
    const t = window.setTimeout(liftOff, Math.max(0, plan.lift - elapsed));
    return () => window.clearTimeout(t);
    // `waiting` is what starts this; `still` and `dim` can end it early.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [waiting, still, dim]);

  // The veil is taken off the page once it has faded.
  useEffect(() => {
    if (veil !== 'leaving') return;
    const t = window.setTimeout(() => setVeil('gone'), VEIL_OUT_MS);
    return () => window.clearTimeout(t);
  }, [veil]);

  // The loader is put down over the tiles, in viewport coordinates: it
  // has to leave `.cd-cold`, which scrolls, and arrive in the header,
  // which is not inside it. Measured once the frames exist, which is the
  // first paint, and again if the number of rows changes before the
  // mark has lifted off.
  useLayoutEffect(() => {
    if (!playing.current || lifted.current) return;
    const g = grid.current?.getBoundingClientRect();
    const f = grid.current?.querySelector('.cd-cold-frame')?.getBoundingClientRect();
    const at = g && f ? loaderSpot(g, f, window.innerHeight) : null;
    if (!at) {
      finish();
      return;
    }
    setSpot(at);
    // The banner going takes the grid up by its height, and the mark
    // with it.
  }, [room, finish, bannerUp]);

  // The veil is mounted down from the start but only raised at
  // VEIL_AT_MS, once a fast list has been ruled out: finish() clears
  // this timer and takes a veil still down straight to gone, so a fast
  // load never sees it. Armed once, in its own effect rather than the
  // measurement's (which runs again on `room`), and with its own
  // cleanup: StrictMode's rehearsal unmount clears every timer, and a
  // once-only guard would then never raise it at all.
  useLayoutEffect(() => {
    if (!playing.current) return;
    const t = window.setTimeout(
      () => {
        if (playing.current) setVeil((v) => (v === 'down' ? 'up' : v));
      },
      Math.max(0, VEIL_AT_MS - (performance.now() - started.current)),
    );
    timers.current.push(t);
    return () => window.clearTimeout(t);
  }, []);

  // Coming back from a map: no opening, the tiles rise into place in
  // reading order instead, and the copy fades in again (above).
  useLayoutEffect(() => {
    if (first) return;
    const rises: (Animation | null)[] = [];
    grid.current?.querySelectorAll('.cd-cold-tile').forEach((n, i) => {
      rises.push(
        animate(
          n,
          [
            { opacity: 0, transform: `translateY(${RETURN_RISE_PX}px)` },
            { opacity: 1, transform: 'none' },
          ],
          {
            duration: RETURN_TILE_MS,
            delay: returnTileDelay(i),
            easing: EASE.settle,
            fill: 'backwards',
          },
        ),
      );
    });
    return () => rises.forEach((a) => a?.cancel());
    // Once, as the screen comes back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const veilTop = HEADER_ROW_H[screenOf(box.w, box.h).cls];

  return (
    <div className={`cd-cold${textIn ? ' cd-cold-in' : ''}${dim ? ' cd-cold-dim' : ''}`}>
      {/* The Daily, first, so it is seen on arrival. It fades in with
          the copy, on the same class: it is set in the same face. */}
      <DailyBanner day={daily} onDaily={onDaily} boxRef={banner} />
      <strong className="cd-cold-head">Start with a movie you love</strong>
      <p className="cd-cold-sub">
        See every movie its cast and directors made, arranged by year and rating.
      </p>
      {/* The frames are drawn before there is anything to put in them,
          so the screen has its full shape from the first paint and
          nothing moves when the films land. */}
      {failed ? (
        <p className="cd-cold-error" role="status">
          Couldn&rsquo;t load suggestions. Search for a movie above.
        </p>
      ) : null}
      {veil !== 'gone' && (
        <div
          className={`cd-veil${veil === 'up' && !dim ? ' cd-veil-on' : ''}`}
          style={{ top: veilTop }}
          aria-hidden="true"
        />
      )}
      {spot && !dim && <LoadingMark at={spot} flight={flight} svgRef={markSvg} />}
      <div
        ref={grid}
        className={`cd-tiles${failed ? ' cd-tiles-gone' : ''}`}
        aria-busy={!tilesIn || undefined}
        aria-hidden={failed || undefined}
      >
        {Array.from({ length: room }, (_, i) => (
          <ColdTile
            // By index, never by film id. Keying on the id swapped
            // every key the moment the list landed, so React threw the
            // eight frames away and mounted eight more — which is why
            // they blinked, and why the stagger and the fades never
            // played: the replacements mounted already visible.
            key={i}
            film={shown[i]}
            index={i}
            shown={tilesIn}
            theme={drawn}
            onPick={onPick}
          />
        ))}
      </div>
      {/* There is no View button on this screen, so the theme choice
          lives here. It fades in after the sub-line rather than with the
          tiles: it is not one of the eight movies. */}
      <ThemePicker value={theme} onChange={onTheme} />
      {/* The way to the About page, which holds the credits the app's
          sources ask for, then who to write to, on one row. It fades in
          with the theme choice. */}
      <footer className="cd-cold-foot">
        <a className="cd-foot-link" href={ABOUT_PATH} onClick={onAbout}>
          {/* An i in a circle: a ring, a stem and a dot. */}
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <circle cx="12" cy="12" r="9" />
            <path d="M12 11v5.5" />
            <path d="M12 7.6h.01" strokeWidth="2.6" />
          </svg>
          About
        </a>
        <MailLink />
      </footer>
    </div>
  );
}
