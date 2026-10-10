import { afterEach, describe, expect, it, vi } from 'vitest';
import type {
  DailyAlso,
  DailyEntry,
  DailyFacts,
  DailyGame,
  DailyGuess,
  DailyPerson,
  DailySlot,
} from './api';
import {
  AGAIN_FAILED,
  BAR_WIDTHS,
  BOARD_TABS,
  CARD_BOB,
  CARD_TURN_EASE,
  CARD_TURN_MS,
  CAST_HEADING,
  CAST_LIST_LABEL,
  CAST_SIZE,
  CHART_LABELS,
  DAILY_START,
  DAY_OVER,
  DROP_AFTER_MS,
  DROP_FROM_PX,
  DROP_GONE_AT,
  DROP_HOLD_TO,
  DROP_LANDS,
  DROP_LOOP_MS,
  DROP_OVER_PX,
  END_REVEAL_FIRST_MS,
  END_REVEAL_STEP_MS,
  FACE_GROW_EASE,
  FACE_GROW_FROM,
  FACE_GROW_MS,
  FACTS,
  FACTS_NOTE,
  FACT_COST,
  FACT_ORDER,
  GAME_NAME,
  GLOW_LIGHTEN,
  GUESS_FIELD,
  HIT_INSETS,
  HOW_IT_WORKS,
  LANDSCAPE_MAX_H,
  LENGTH_BANDS,
  LIST_CLOSE_MS,
  MESSAGE_FACES,
  MIDNIGHT_STEP_MS,
  NAME_IN_MS,
  NAME_OUT_MS,
  NAME_RISE_PX,
  NEXT_COST,
  NEXT_IN_VIEW_GAP,
  NEXT_IN_VIEW_MS,
  NEXT_MOVIE_IN,
  NEXT_ROW_LABEL,
  NOT_READY,
  NUDGE_NAMES,
  NUDGE_NOTE,
  NUDGE_PULSES,
  NUDGE_PULSE_MS,
  NUDGE_PULSE_PX,
  PEEK_EASE,
  PEEK_FADE_MS,
  PEEK_GROW_FROM,
  PEEK_GROW_MS,
  PEEK_LEFT,
  PEEK_REST_MS,
  PLAY_AGAIN,
  PLAY_HEADING,
  PLAY_HIDE_MS,
  PLAY_LINE,
  PLAY_SHINE_KEYFRAMES,
  RATING_BANDS,
  RESULT_AFTER_MS,
  RESULT_GAP_PX,
  REVEAL_CONFIRM_MS,
  RIPPLE_EASE,
  RIPPLE_MS,
  RIPPLE_PX,
  RISE_EASE,
  RISE_FIRST_MS,
  RISE_MS,
  RISE_PX,
  RISE_STEP_MS,
  SCORE_COUNT_AFTER_MS,
  SCORE_COUNT_MS,
  SEARCH_MIN_CHARS,
  SEARCH_WAIT_MS,
  SHEET_CLOSE_PX,
  SHEET_METRICS,
  SHEET_PHONE_SHARE,
  SHEET_SPRING_EASE,
  SHEET_SPRING_MS,
  SHOW_ANSWER,
  SHEET_READ_TOP,
  SHOW_ANSWER_SURE,
  TITLE_GLOW_ALPHA,
  TITLE_HUES,
  TITLE_LOOPS,
  TITLE_SCREEN,
  TOASTS,
  UNREACHABLE,
  WARMTH_COLOURS,
  WARMTH_LABELS,
  WRONG_BASE,
  WRONG_STEP,
  aboutItems,
  accentGlow,
  affords,
  andList,
  bannerLabel,
  bannerLine,
  betterLine,
  blankCardLabel,
  boardNote,
  boardRows,
  boardsWanted,
  canNext,
  cardColour,
  cardGlow,
  cardRipple,
  castLine,
  castRows,
  chartBars,
  clockOffset,
  codesOf,
  countUp,
  countdown,
  dailyDateText,
  dailyDayText,
  decadeText,
  dropFailed,
  dropKeyframes,
  earlyRefusal,
  endRevealDelay,
  factItems,
  factNudge,
  factValue,
  factsHeading,
  fmtN,
  guessById,
  guessLabel,
  guessMessage,
  guessOptions,
  guessPlaceholder,
  guessedIds,
  guessesOf,
  hasFact,
  hoursMinutes,
  hueColour,
  isLandscapePhone,
  lastGuess,
  lengthBandText,
  liveScreen,
  midnightText,
  moveSig,
  moviesLabel,
  namesLine,
  newKey,
  newWrongGuess,
  newestGuess,
  nextInView,
  nextSlot,
  nudgePulse,
  openingRows,
  ord,
  pageHeading,
  pageLine,
  pageSections,
  paidFor,
  peekOpensUp,
  pickLine,
  placeText,
  playedText,
  posterGlow,
  rangeBought,
  rangesKey,
  ratingBandText,
  ratingSpan,
  refusalText,
  resultFaces,
  resultKicker,
  resultScroll,
  resultsMax,
  revealDelays,
  rippleSlot,
  scoreAt,
  seenSlots,
  shareMarks,
  shareText,
  sharedPeople,
  sheetCardLabel,
  sheetHint,
  sheetLegend,
  sheetSub,
  sheetTitle,
  shownSlots,
  staleGame,
  standingText,
  streakAfter,
  streakPill,
  titleRise,
  todaysPeople,
  toneStyle,
  triedChips,
  usedText,
  viewportFit,
  warmthColour,
  watchMidnight,
  weekLetters,
  weekdayOf,
  wrongCost,
  yearSpan,
  yearsShown,
  yearsText,
  type GuessFound,
} from './daily';

// ---- today's movie: The Matrix, No. 143 ----

const person = (id: string, name: string, hue: number): DailyPerson => ({ id, name, hue });

// The cast in reveal order, sixth-billed first; each person's hue from
// their place on the movie, the star first, then the directors.
const JOE = person('nm0001592', 'Joe Pantoliano', 205);
const GLORIA = person('nm0287825', 'Gloria Foster', 78);
const HUGO = person('nm0915989', 'Hugo Weaving', 150);
const CARRIE = person('nm0005251', 'Carrie-Anne Moss', 345);
const LAURENCE = person('nm0000401', 'Laurence Fishburne', 28);
const KEANU = person('nm0000206', 'Keanu Reeves', 232);
const LANA = person('nm0905154', 'Lana Wachowski', 118);
const LILLY = person('nm0905152', 'Lilly Wachowski', 255);
const CAST = [JOE, GLORIA, HUGO, CARRIE, LAURENCE, KEANU];

const ALSO: DailyAlso[] = [
  { id: 'tt0106977', title: 'The Fugitive', year: 1993 },
  { id: 'tt0068907', title: 'Man and Boy', year: 1971 },
  { id: 'tt0434409', title: 'V for Vendetta', year: 2005 },
  { id: 'tt0241303', title: 'Chocolat', year: 2000 },
  { id: 'tt0078788', title: 'Apocalypse Now', year: 1979 },
  { id: 'tt2911666', title: 'John Wick', year: 2014 },
];

const MATRIX = {
  id: 'tt0133093',
  title: 'The Matrix',
  year: 1999,
  rating: 8.7,
  length: 136,
  genres: ['Action', 'Sci-Fi'],
  colour: '#26382d',
};

/** The six slots, each shown as `shown` says (how it came to show, and
 *  the guess that filled it in), or hidden. */
function slotsOf(shown: Record<number, { via: Extract<DailySlot, { shown: true }>['via']; from?: { id: string; title: string } }>): DailySlot[] {
  return CAST.map((p, slot): DailySlot => {
    const s = shown[slot];
    if (!s) return { slot, shown: false };
    return { slot, shown: true, person: p, also: ALSO[slot], via: s.via, ...(s.from ? { from: s.from } : {}) };
  });
}

/** Every slot shown, as at the end. */
const ALL_SHOWN = slotsOf(Object.fromEntries(CAST.map((_, k) => [k, { via: k ? 'next' : 'start' }] as const)));

/** A wrong guess, its warmth worked out as the server works it. */
function wrong(
  id: string,
  title: string,
  year: number,
  over: { shared?: number[]; sameDecade?: boolean; sharesGenre?: boolean; cost?: number } = {},
): Extract<DailyEntry, { type: 'guess' }> {
  const shared = over.shared ?? [];
  const sameDecade = over.sameDecade ?? false;
  const sharesGenre = over.sharesGenre ?? false;
  const score = 2 * shared.length + (sameDecade ? 1 : 0) + (sharesGenre ? 1 : 0);
  const guess: DailyGuess = {
    id,
    title,
    year,
    cost: over.cost ?? 100,
    shared,
    sameDecade,
    sharesGenre,
    warmth: score >= 3 ? 2 : score >= 1 ? 1 : 0,
  };
  return { type: 'guess', cost: guess.cost, guess };
}

const THIRTEENTH = wrong('tt0139809', 'The Thirteenth Floor', 1999, { sameDecade: true, sharesGenre: true });
// Keanu Reeves is in it, five years earlier, and it is an action movie.
const SPEED = wrong('tt0111257', 'Speed', 1994, { shared: [5], sharesGenre: true, cost: 150 });

/** A game just started: the sixth-billed showing, nothing bought. */
function gameOf(over: Partial<DailyGame> = {}): DailyGame {
  return {
    phase: 'play',
    pts: DAILY_START,
    seq: 0,
    startedAt: '2026-10-09T11:58:00Z',
    finishedAt: null,
    won: false,
    gaveUp: false,
    nextCost: 100,
    slots: slotsOf({ 0: { via: 'start' } }),
    facts: {},
    log: [],
    end: null,
    ...over,
  };
}

/** The screenshots' game in progress: two extra names, the decade, and
 *  one wrong guess, which showed a fourth. */
const PLAYING = gameOf({
  pts: 600,
  seq: 4,
  nextCost: 150,
  slots: slotsOf({ 0: { via: 'start' }, 1: { via: 'next' }, 2: { via: 'next' }, 3: { via: 'guess' } }),
  facts: { decade: 1990 },
  log: [
    { type: 'next', cost: 100, slot: 1 },
    { type: 'next', cost: 100, slot: 2 },
    { type: 'fact', kind: 'decade', cost: 100 },
    THIRTEENTH,
  ],
});

/** A game over, every slot shown and the answer sent. */
function ended(log: DailyEntry[], over: Partial<DailyGame> = {}): DailyGame {
  return gameOf({
    phase: 'done',
    finishedAt: '2026-10-09T12:04:00Z',
    slots: ALL_SHOWN,
    log,
    end: { answer: MATRIX, directors: [LANA, LILLY] },
    ...over,
  });
}

/** The screenshots' solve, at 600 points. */
const SOLVED = ended([...PLAYING.log, { type: 'win' }], { pts: 600, won: true, facts: { decade: 1990 } });

describe('the rules’ prices', () => {
  it('start a game with a thousand points and price everything as the handoff’s table does', () => {
    expect(DAILY_START).toBe(1000);
    expect(CAST_SIZE).toBe(6);
    expect(NEXT_COST).toBe(100);
    expect(FACT_COST).toEqual({ length: 50, rating: 50, genre: 100, decade: 100, years: 100, director: 250 });
  });

  it('make each wrong guess dearer than the last: 100, 150, 200, and on by 50', () => {
    expect([WRONG_BASE, WRONG_STEP]).toEqual([100, 50]);
    expect([0, 1, 2, 3].map(wrongCost)).toEqual([100, 150, 200, 250]);
  });

  it('refuse a purchase that would leave no points, as the server does', () => {
    expect(affords(101, 100)).toBe(true);
    expect(affords(100, 100)).toBe(false);
    expect(affords(50, 100)).toBe(false);
  });

  it('call a wrong guess the points cannot cover the last one', () => {
    expect(lastGuess({ pts: 600, nextCost: 150 })).toBe(false);
    expect(lastGuess({ pts: 151, nextCost: 150 })).toBe(false);
    expect(lastGuess({ pts: 150, nextCost: 150 })).toBe(true);
    expect(lastGuess({ pts: 100, nextCost: 150 })).toBe(true);
  });
});

describe('the game’s name', () => {
  // Every source file and stylesheet in the app, and the page they load
  // into. The old names are spelt out in pieces so this file is not one
  // of the places they are found.
  const sources = {
    ...import.meta.glob('./**/*.{ts,tsx,css}', { query: '?raw', import: 'default', eager: true }),
    ...import.meta.glob('../index.html', { query: '?raw', import: 'default', eager: true }),
  } as Record<string, string>;
  const OLD_NAMES = [['point', 'blank'], ['whose', 'map', 'is', 'it']].map(
    (words) => new RegExp(words.join('\\s+'), 'i'),
  );

  it('reads the whole app', () => {
    expect(Object.keys(sources).length).toBeGreaterThan(70);
    expect(Object.keys(sources)).toContain('./DailyPage.tsx');
    expect(Object.keys(sources)).toContain('../index.html');
  });

  it('is Name Drop, and the old ones are nowhere: not in the copy, the labels, the tests or the styles', () => {
    expect(GAME_NAME).toBe('Name Drop');
    for (const name of OLD_NAMES) {
      expect(Object.entries(sources).filter(([, text]) => name.test(text)).map(([file]) => file)).toEqual([]);
    }
  });
});

describe('colours', () => {
  it('make a person’s colour from their hue, softer on the dark ground and stronger on paper', () => {
    expect(hueColour(232, 'dark')).toBe('oklch(0.76 0.13 232)');
    expect(hueColour(232, 'light')).toBe('oklch(0.56 0.16 232)');
    expect(TITLE_HUES).toEqual([118, 205, 345]);
    expect(hueColour(TITLE_HUES[0], 'dark')).toBe('oklch(0.76 0.13 118)');
  });

  it('call a wrong guess cold, warm or hot, in the handoff’s colours for each theme', () => {
    expect([0, 1, 2].map((w) => WARMTH_LABELS[w as 0 | 1 | 2])).toEqual(['Cold', 'Warm', 'Hot']);
    expect(WARMTH_COLOURS.dark).toEqual(['oklch(0.76 0.1 235)', 'oklch(0.82 0.13 78)', 'oklch(0.74 0.17 32)']);
    expect(WARMTH_COLOURS.light).toEqual(['oklch(0.5 0.13 240)', 'oklch(0.55 0.13 65)', 'oklch(0.54 0.19 30)']);
    expect(warmthColour(2, 'light')).toBe('oklch(0.54 0.19 30)');
  });

  it('go on the element as --tone, a custom property, never into the stylesheet', () => {
    expect(toneStyle(hueColour(205, 'dark'))).toEqual({ '--tone': 'oklch(0.76 0.13 205)' });
  });

  it('fill the hidden card only with a colour the server could have sent', () => {
    expect(cardColour('#26382d')).toBe('#26382d');
    expect(cardColour('#26382D')).toBe('#26382D');
    for (const bad of ['red', '#2638', 'url(x)', '#26382d; background: url(x)', '', null, undefined]) {
      expect(cardColour(bad)).toBe('var(--c)');
    }
  });

  it('glow round the hidden card in the poster colour lightened 45% towards white, and nothing else', () => {
    // #26382d is (38, 56, 45): each channel 45% of the way to 255.
    expect(GLOW_LIGHTEN).toBe(0.45);
    expect(posterGlow('#26382d', 0.3)).toBe('rgba(136, 146, 140, 0.3)');
    expect(posterGlow('#000000', 1)).toBe('rgba(115, 115, 115, 1)');
    expect(posterGlow('#ffffff', 0.12)).toBe('rgba(255, 255, 255, 0.12)');
    for (const bad of ['red', '#2638', 'var(--c)', '', null, undefined]) expect(posterGlow(bad, 0.3)).toBeNull();
    expect(TITLE_GLOW_ALPHA).toBe(0.3);
  });

  it('draw the accent’s glows in the design’s own rgba, only ever from script', () => {
    expect(accentGlow(0.2)).toBe('rgba(170, 140, 255, 0.2)');
    expect(accentGlow(0)).toBe('rgba(170, 140, 255, 0)');
  });

  it('give each face without a photo the map’s initials, unique among today’s people', () => {
    const codes = codesOf([...CAST, LANA, LILLY]);
    expect(codes.get(KEANU.id)).toBe('KR');
    expect(codes.get(JOE.id)).toBe('JP');
    expect(codes.get(LANA.id)).not.toBe(codes.get(LILLY.id));
  });
});

describe('the facts', () => {
  it('say each band as the handoff writes it', () => {
    expect(LENGTH_BANDS).toEqual(['Under 1h 30m', '1h 30m to 2h', '2h to 2h 30m', 'Over 2h 30m']);
    expect(RATING_BANDS).toEqual(['Below 6.0', '6.0 to 6.9', '7.0 to 7.9', '8.0 or higher']);
    expect(lengthBandText(2)).toBe('2h to 2h 30m');
    expect(ratingBandText(3)).toBe('8.0 or higher');
    expect(lengthBandText(9)).toBe('');
  });

  it('write the decade and the five years from their first years', () => {
    expect(decadeText(1990)).toBe('1990s');
    expect(yearsText(1995)).toBe('1995–1999');
  });

  it('write a runtime exactly at the end, in hours and minutes', () => {
    expect(hoursMinutes(136)).toBe('2h 16m');
    expect(hoursMinutes(120)).toBe('2h');
    expect(hoursMinutes(45)).toBe('45m');
  });

  it('offer all five in order, each with its price and what it shows to a screen reader', () => {
    const items = factItems(gameOf());
    expect(items.map((f) => [f.label, f.price])).toEqual([
      ['Length range', '−50'],
      ['Rating range', '−50'],
      ['Genre', '−100'],
      ['Decade', '−100'],
      ['Director', '−250'],
    ]);
    expect(items.every((f) => !f.bought && f.can && f.value === '')).toBe(true);
    expect(items.map((f) => f.aria)).toEqual([
      'Show roughly how long it is. It costs 50 points.',
      'Show its IMDb rating range. It costs 50 points.',
      'Show its genre. It costs 100 points.',
      'Show the decade it came out. It costs 100 points.',
      'Show the director. It costs 250 points.',
    ]);
  });

  it('follow the decade with Narrow the years, and put the five years in its place once bought', () => {
    expect(factItems(PLAYING).map((f) => [f.label, f.value || f.price])).toEqual([
      ['Length range', '−50'],
      ['Rating range', '−50'],
      ['Genre', '−100'],
      ['Decade', '1990s'],
      ['Narrow the years', '−100'],
      ['Director', '−250'],
    ]);
    expect(factItems(PLAYING)[4].aria).toBe('Narrow the years to five. It costs 100 points.');
    const narrowed = factItems({ ...PLAYING, facts: { decade: 1990, years: 1995 } });
    expect(narrowed.map((f) => [f.label, f.value || f.price])).toEqual([
      ['Length range', '−50'],
      ['Rating range', '−50'],
      ['Genre', '−100'],
      ['Years', '1995–1999'],
      ['Director', '−250'],
    ]);
  });

  it('say what each bought fact is, and nothing for one not bought', () => {
    const facts: DailyFacts = {
      length: 2,
      rating: 3,
      genre: ['Action', 'Sci-Fi'],
      decade: 1990,
      years: 1995,
      director: [LANA, LILLY],
    };
    expect(
      factItems({ phase: 'play', pts: 1000, facts })
        .filter((f) => f.bought)
        .map((f) => `${f.label} ${f.value}`),
    ).toEqual([
      'Length 2h to 2h 30m',
      'Rating 8.0 or higher',
      'Genre Action, Sci-Fi',
      'Years 1995–1999',
      'Director Lana Wachowski and Lilly Wachowski',
    ]);
    expect(factValue('genre', {})).toBe('');
    expect(hasFact('length', { length: 0 })).toBe(true);
    expect(hasFact('length', {})).toBe(false);
  });

  it('cannot be bought when the points would not leave one over, or once the game is over', () => {
    const at100 = factItems(gameOf({ pts: 100 }));
    expect(at100.map((f) => f.can)).toEqual([true, true, false, false, false]);
    expect(factItems(gameOf({ pts: 1000, phase: 'done' })).some((f) => f.can)).toBe(false);
  });

  it('are all exact once it is over, as About the movie', () => {
    expect(aboutItems({ answer: MATRIX, directors: [LANA, LILLY] }).map((f) => `${f.label} ${f.value}`)).toEqual([
      'Length 2h 16m',
      'Genre Action, Sci-Fi',
      'IMDb rating 8.7',
      'Year 1999',
      'Director Lana Wachowski and Lilly Wachowski',
    ]);
    expect(factsHeading(false)).toBe('Buy a fact');
    expect(factsHeading(true)).toBe('About the movie');
    expect(FACTS_NOTE).toBe('Each one also marks the map');
  });

  it('nudge the reader once three names are showing and not one fact has been bought', () => {
    expect(NUDGE_NAMES).toBe(3);
    expect(NUDGE_NOTE).toBe('Stuck? A fact narrows it down.');
    const three = slotsOf({ 0: { via: 'start' }, 1: { via: 'next' }, 2: { via: 'next' } });
    expect(factNudge(gameOf({ slots: three }))).toBe(true);
    // Two names are not yet stuck.
    expect(factNudge(gameOf({ slots: slotsOf({ 0: { via: 'start' }, 1: { via: 'next' } }) }))).toBe(false);
    // Any fact bought, a length or a director as much as a range, and
    // the reader has found the facts.
    expect(factNudge(gameOf({ slots: three, facts: { length: 0 } }))).toBe(false);
    expect(factNudge(gameOf({ slots: three, facts: { director: [LANA] } }))).toBe(false);
    expect(factNudge(PLAYING)).toBe(false);
    // Never once it is over.
    expect(factNudge(gameOf({ slots: ALL_SHOWN, phase: 'done' }))).toBe(false);
  });

  it('leave out what the catalog has nothing for, rather than draw it empty', () => {
    const labels = aboutItems({ answer: { ...MATRIX, genres: [], year: 0 }, directors: [] }).map((f) => f.label);
    expect(labels).toEqual(['Length', 'IMDb rating']);
  });
});

describe('the names the reader saw', () => {
  it('start with the sixth-billed, before anything is bought', () => {
    expect([...seenSlots(gameOf())]).toEqual([0]);
    expect(seenSlots(null).size).toBe(0);
  });

  it('add each name bought, and for a wrong guess everyone it shares and then the next', () => {
    // Speed shares the star, so it fills him in, and then shows slot 1,
    // the first still hidden.
    expect([...seenSlots({ log: [SPEED] })].sort()).toEqual([0, 1, 5]);
    expect([...seenSlots({ log: [{ type: 'next', cost: 100, slot: 1 }, SPEED] })].sort()).toEqual([0, 1, 2, 5]);
  });

  it('are the names showing while the game is on', () => {
    expect([...seenSlots(PLAYING)].sort()).toEqual(shownSlots(PLAYING).map((s) => s.slot));
  });

  it('come from the log at the end, when every slot is shown', () => {
    expect(seenSlots(SOLVED).size).toBe(4);
    expect(shownSlots(SOLVED)).toHaveLength(6);
  });

  it('count the names a last wrong guess showed, as the server’s replay does', () => {
    const out = ended([THIRTEENTH, { type: 'out' }], { pts: 0 });
    expect([...seenSlots(out)].sort()).toEqual([0, 1]);
  });

  it('leave out the names only the end showed, which the server sends with via end', () => {
    // Given up on the first name: the other five come with the end, as
    // the server sends them, and were never seen.
    const gaveUp = ended([{ type: 'gaveup' }], {
      pts: 0,
      gaveUp: true,
      slots: slotsOf(Object.fromEntries(CAST.map((_, k) => [k, { via: k ? 'end' : 'start' }] as const))),
    });
    expect(shownSlots(gaveUp).map((s) => s.via)).toEqual(['start', 'end', 'end', 'end', 'end', 'end']);
    expect([...seenSlots(gaveUp)]).toEqual([0]);
    expect(namesLine(gaveUp)).toBe('You saw 1 of the 6 names');
  });
});

describe('the cast list', () => {
  it('shows each name it may, says the next row is next, and leaves the rest as bars', () => {
    const rows = castRows(PLAYING);
    expect(rows.map((r) => r.state)).toEqual(['shown', 'shown', 'shown', 'shown', 'next', 'hidden']);
    expect(rows.map((r) => (r.state === 'shown' ? r.person.name : r.bar))).toEqual([
      'Joe Pantoliano',
      'Gloria Foster',
      'Hugo Weaving',
      'Carrie-Anne Moss',
      BAR_WIDTHS[4],
      BAR_WIDTHS[5],
    ]);
    expect(BAR_WIDTHS).toEqual([46, 58, 40, 52, 62, 44]);
  });

  it('never carries a hidden name', () => {
    const json = JSON.stringify(castRows(PLAYING));
    expect(json).not.toContain('Laurence Fishburne');
    expect(json).not.toContain('Keanu Reeves');
  });

  it('offers no next row when the next name cannot be asked for', () => {
    const poor = { ...PLAYING, pts: 100 };
    expect(castRows(poor).map((r) => r.state)).toEqual(['shown', 'shown', 'shown', 'shown', 'hidden', 'hidden']);
    expect(nextSlot(poor)).toBeNull();
    expect(nextSlot(PLAYING)).toBe(4);
    expect(canNext({ ...PLAYING, phase: 'done' })).toBe(false);
  });

  it('marks who the reader saw at the end, so the rest can appear one after another', () => {
    expect(castRows(SOLVED).map((r) => r.state === 'shown' && r.seen)).toEqual([true, true, true, true, false, false]);
  });

  it('puts another movie under a name, or the guess that filled it in', () => {
    expect(castLine({ also: ALSO[0] })).toBe('Also in The Fugitive (1993)');
    expect(castLine({ also: ALSO[5], from: { id: SPEED.guess.id, title: 'Speed' } })).toBe('From your guess, Speed');
    expect(castLine({ also: { id: 'tt1', title: 'Untitled', year: 0 } })).toBe('Also in Untitled');
    expect(castLine({})).toBe('');
  });

  it('names the row that shows the next name, and each Movies button, for a screen reader', () => {
    expect(NEXT_ROW_LABEL).toBe('Show the next name. It costs 100 points.');
    expect(moviesLabel('Joe Pantoliano')).toBe('See Joe Pantoliano’s movies on a Cinedikt map');
    expect(CAST_LIST_LABEL).toBe('Today’s names');
    expect(CAST_HEADING).toBe('The cast');
  });

  it('opens the lower three rows’ photo upwards', () => {
    expect([0, 1, 2, 3, 4, 5].map(peekOpensUp)).toEqual([false, false, false, true, true, true]);
  });
});

describe('a name appearing', () => {
  it('ripples out of the card in the colour of whoever has just joined', () => {
    const one = gameOf();
    const two = gameOf({ slots: slotsOf({ 0: { via: 'start' }, 1: { via: 'next' } }), log: [{ type: 'next', cost: 100, slot: 1 }] });
    expect(rippleSlot(one, two)).toBe(1);
    // Nothing new, nothing ripples.
    expect(rippleSlot(two, two)).toBeNull();
  });

  it('ripples for the name a wrong guess showed next, not for those it filled in', () => {
    // Speed fills in the star, slot 5, and then shows slot 1.
    const after = gameOf({
      slots: slotsOf({ 0: { via: 'start' }, 1: { via: 'guess' }, 5: { via: 'guess', from: { id: SPEED.guess.id, title: 'Speed' } } }),
      log: [SPEED],
    });
    expect(rippleSlot(gameOf(), after)).toBe(1);
    // With no one hidden left to show, the last it filled in.
    const allButStar = slotsOf({ 0: { via: 'start' }, 1: { via: 'next' }, 2: { via: 'next' }, 3: { via: 'next' }, 4: { via: 'next' } });
    expect(rippleSlot(gameOf({ slots: allButStar }), gameOf({ slots: ALL_SHOWN, log: [SPEED] }))).toBe(5);
  });

  it('never ripples as the page opens on a game, nor at the end', () => {
    expect(rippleSlot(null, PLAYING)).toBeNull();
    expect(rippleSlot(PLAYING, SOLVED)).toBeNull();
  });

  it('keeps the next row in sight above the guess bar, once the name has settled, with 34px to spare', () => {
    expect([NEXT_IN_VIEW_MS, NEXT_IN_VIEW_GAP]).toEqual([650, 34]);
    expect(nextInView(700, 600)).toBe(134);
    expect(nextInView(566, 600)).toBe(0);
    expect(nextInView(200, 600)).toBe(0);
  });
});

describe('a wrong guess', () => {
  it('says how warm it was, and compares the decade, the genre and the cast without giving either away', () => {
    expect(guessMessage(THIRTEENTH.guess, PLAYING)).toEqual({
      title: 'Not The Thirteenth Floor',
      warmth: 1,
      label: 'Warm',
      faces: [],
      chips: ['Same decade', 'Shares a genre', 'No one from today’s cast'],
    });
  });

  it('names who it shares, with their faces', () => {
    const game = gameOf({ slots: slotsOf({ 0: { via: 'start' }, 1: { via: 'guess' }, 5: { via: 'guess', from: { id: SPEED.guess.id, title: 'Speed' } } }), log: [SPEED] });
    const m = guessMessage(SPEED.guess, game);
    expect(m.label).toBe('Hot');
    expect(m.faces).toEqual([KEANU]);
    expect(m.chips).toEqual(['Different decade', 'Shares a genre', 'Shares Keanu Reeves']);
    expect(sharedPeople({ shared: [0, 5] }, game)).toEqual([JOE, KEANU]);
  });

  it('is a chip in the top block that says what it does', () => {
    expect(triedChips(PLAYING)).toEqual([
      { id: 'tt0139809', title: 'The Thirteenth Floor', warmth: 1, aria: 'The Thirteenth Floor, warm. Show what it told you' },
    ]);
  });

  it('is marked tried, by IMDb id, and noticed when one more comes back', () => {
    expect(guessesOf(PLAYING)).toEqual([THIRTEENTH.guess]);
    expect(guessedIds(PLAYING)).toEqual(new Set(['tt0139809']));
    expect(guessedIds(null).size).toBe(0);
    expect(newWrongGuess(gameOf(), PLAYING)).toBe(true);
    expect(newWrongGuess(PLAYING, PLAYING)).toBe(false);
    expect(newWrongGuess(null, gameOf())).toBe(false);
  });
});

describe('the guess bar', () => {
  it('says in the field what a miss costs, rising as the misses do', () => {
    expect(guessPlaceholder({ pts: 1000, nextCost: 100 })).toBe('Name the movie · a miss costs 100');
    expect(guessPlaceholder(PLAYING)).toBe('Name the movie · a miss costs 150');
    expect(guessPlaceholder({ pts: 5000, nextCost: 1200 })).toBe('Name the movie · a miss costs 1,200');
  });

  it('says it is the last guess when the next miss would end the game', () => {
    expect(guessPlaceholder({ pts: 100, nextCost: 150 })).toBe('Name the movie · last guess');
    expect(guessPlaceholder({ pts: 150, nextCost: 150 })).toBe('Name the movie · last guess');
    expect(guessPlaceholder({ pts: 151, nextCost: 150 })).toBe('Name the movie · a miss costs 150');
  });

  it('puts the points getting it now would score on Guess', () => {
    expect(guessLabel({ pts: 1000 })).toBe('Guess · 1,000');
    expect(guessLabel(PLAYING)).toBe('Guess · 600');
  });
});

describe('the results list', () => {
  const godfather = { id: 'tt0068646', title: 'The Godfather' };
  const partII = { id: 'tt0071562', title: 'The Godfather Part II' };
  const seen = (q: string, hits: { id: string; title: string }[], failed = false): GuessFound<{ id: string; title: string }> => ({
    q,
    hits,
    failed,
  });

  it('offers the rows found for exactly what is typed', () => {
    expect(guessOptions('godfather part ii', seen('godfather part ii', [partII, godfather]))).toEqual({
      rows: [partII, godfather],
      held: [],
      note: '',
    });
  });

  it('offers nothing to choose from words the reader has typed past, only holds them faded', () => {
    // Enter on the top row would make a guess the reader never meant.
    const { rows, held, note } = guessOptions('godfather part ii', seen('godfather part', [godfather, partII]));
    expect(rows).toEqual([]);
    expect(held).toEqual([godfather, partII]);
    expect(note).toBe('');
  });

  it('says it is searching while there is nothing yet to hold', () => {
    expect(guessOptions('heat', null)).toEqual({ rows: [], held: [], note: 'Searching…' });
    expect(guessOptions('heat', seen('hea', [], true))).toEqual({ rows: [], held: [], note: 'Searching…' });
  });

  it('says the search never answered, never that nothing matched', () => {
    expect(guessOptions('heat', seen('heat', [], true)).note).toBe(UNREACHABLE);
  });

  it('says nothing matched when that is the catalog’s answer', () => {
    expect(guessOptions('qqzx', seen('qqzx', [])).note).toBe('No movies match “qqzx”');
  });

  it('shows a year only where two results share a title, so a bought range cannot sift it', () => {
    expect(
      yearsShown([{ title: 'Heat' }, { title: 'The Matrix' }, { title: 'heat' }, { title: 'Amélie' }, { title: 'Amelie' }]),
    ).toEqual([true, false, true, true, true]);
    expect(yearsShown([{ title: 'The Matrix' }, { title: 'The Matrix Reloaded' }])).toEqual([false, false]);
  });

  it('holds four results on a phone and six elsewhere', () => {
    expect(resultsMax(true)).toBe(4);
    expect(resultsMax(false)).toBe(6);
  });
});

describe('the result', () => {
  it('heads a solve, a solve on the first name, and each kind of miss', () => {
    expect(resultKicker(SOLVED)).toBe('Got it');
    expect(resultKicker(ended([{ type: 'win' }], { won: true, pts: 1000 }))).toBe('Got it on the first name');
    // A fact bought makes it no longer the first name alone.
    expect(resultKicker(ended([{ type: 'fact', kind: 'decade', cost: 100 }, { type: 'win' }], { won: true }))).toBe('Got it');
    expect(resultKicker(ended([{ type: 'out' }], { pts: 0 }))).toBe('Your points ran out');
    expect(resultKicker(ended([{ type: 'gaveup' }], { pts: 0, gaveUp: true }))).toBe('You asked for the answer');
  });

  it('counts the names needed, or seen for a loss', () => {
    expect(namesLine(SOLVED)).toBe('You needed 4 of the 6 names');
    expect(namesLine(ended([THIRTEENTH, { type: 'out' }], { pts: 0 }))).toBe('You saw 2 of the 6 names');
  });

  it('lists what was paid for: each fact in the row’s order, then the wrong guesses', () => {
    const log: DailyEntry[] = [
      { type: 'fact', kind: 'years', cost: 100 },
      { type: 'fact', kind: 'decade', cost: 100 },
      THIRTEENTH,
      { ...SPEED, cost: 150 },
    ];
    expect(paidFor(log)).toEqual([
      { label: 'Decade', price: '−100' },
      { label: 'Five-year range', price: '−100' },
      { label: '2 wrong guesses', price: '−250' },
    ]);
    expect(paidFor([{ type: 'next', cost: 100, slot: 1 }, { type: 'win' }])).toEqual([]);
    expect(FACT_ORDER.map((k) => FACTS[k].paid)).toEqual(['Length', 'Rating', 'Genre', 'Decade', 'Five-year range', 'Director']);
  });

  it('says the streak once today is in it', () => {
    expect(streakPill({ now: 0, before: 1 }, SOLVED)).toBe('2-day streak');
    expect(streakPill({ now: 0, before: 0 }, SOLVED)).toBe('Streak started');
    // A solve opened later: the server counted today already.
    expect(streakPill({ now: 5, before: 0 }, SOLVED)).toBe('5-day streak');
    const lost = ended([{ type: 'gaveup' }], { pts: 0, gaveUp: true });
    expect(streakPill({ now: 0, before: 3 }, lost)).toBe('Streak ended');
    expect(streakPill({ now: 0, before: 0 }, lost)).toBe('');
    expect(streakAfter({ now: 0, before: 3 }, SOLVED)).toEqual({ now: 4, before: 0 });
  });

  it('says how the reader did against today’s players, once today’s board is here', () => {
    expect(betterLine(SOLVED, { beat: 78, solved: 92 })).toEqual({
      figure: '78%',
      rest: ' of today’s players scored less than you.',
    });
    expect(betterLine({ won: false }, { beat: 0, solved: 92 })).toEqual({ figure: '92%', rest: ' of today’s players got it.' });
    expect(betterLine(SOLVED, null)).toBeNull();
  });

  it('charts eleven bars, never shorter than 6%, with the reader’s own lit', () => {
    const chart = [100, 0, 50, 200, 400, 300, 120, 80, 40, 10, 5];
    const bars = chartBars(chart, SOLVED);
    expect(bars).toHaveLength(11);
    expect(bars.map((b) => b.h)).toEqual([25, 6, 13, 50, 100, 75, 30, 20, 10, 6, 6]);
    expect(bars.findIndex((b) => b.you)).toBe(6);
    expect(chartBars(chart, { won: true, pts: 1000 }).findIndex((b) => b.you)).toBe(10);
    expect(chartBars(chart, { won: false, pts: 0 }).findIndex((b) => b.you)).toBe(0);
    expect(chartBars(null, SOLVED).map((b) => b.h)).toEqual(Array(11).fill(6));
    expect(CHART_LABELS).toEqual(['0', '500', '1,000']);
  });
});

describe('sharing a result', () => {
  const origin = 'https://cinedikt.com';

  it('copies the game, the score and what it took, the six squares, and where to play', () => {
    // The handoff's example: three names, the decade and one wrong guess.
    const game = ended(
      [{ type: 'fact', kind: 'decade', cost: 100 }, { type: 'next', cost: 100, slot: 1 }, THIRTEENTH, { type: 'win' }],
      { won: true, pts: 800 },
    );
    expect(shareText(143, game, origin)).toBe(
      'Cinedikt Daily No. 143 · Name Drop\n800 points · 3 names, the decade and one wrong guess\n■■■□□□\nhttps://cinedikt.com/daily',
    );
  });

  it('says one name in words, and counts the rest', () => {
    expect(usedText({ log: [] })).toBe('one name');
    expect(
      usedText({
        log: [
          { type: 'fact', kind: 'director', cost: 250 },
          { type: 'fact', kind: 'length', cost: 50 },
          THIRTEENTH,
          SPEED,
        ],
      }),
    ).toBe('4 names, the length range, the director and 2 wrong guesses');
  });

  it('says a loss as missed or given up, with the squares of the names seen', () => {
    expect(shareText(143, ended([THIRTEENTH, { type: 'out' }], { pts: 0 }), origin)).toBe(
      'Cinedikt Daily No. 143 · Name Drop\nMissed it\n■■□□□□\nhttps://cinedikt.com/daily',
    );
    expect(shareText(143, ended([{ type: 'gaveup' }], { pts: 0, gaveUp: true }), origin).split('\n')[1]).toBe('Gave up');
    expect(shareMarks(SOLVED)).toBe('■■■■□□');
  });
});

describe('the leaderboard', () => {
  it('writes a shared place with an equals sign', () => {
    expect(placeText(7804, true)).toBe('=7,804');
    expect(placeText(12, false)).toBe('12');
  });

  it('draws the reader as You, and the week’s rows with a cell for each day so far', () => {
    const today = boardRows({
      tab: 'today',
      rows: [
        { place: 7790, tied: true, name: 'Marty Starling', hue: 40, pts: 700, you: false },
        { place: 7804, tied: true, name: 'Clarice McFly', hue: 200, pts: 600, you: true },
      ],
    });
    expect(today.map((r) => [r.place, r.name, r.pts, r.days])).toEqual([
      ['=7,790', 'Marty Starling', '700', null],
      ['=7,804', 'You', '600', null],
    ]);
    const week = boardRows({
      tab: 'week',
      rows: [{ place: 1204, tied: false, name: 'Vito Gunderson', hue: 5, pts: 2950, days: [650, 800, 0, 900, 600], you: true }],
    });
    expect(week[0].days).toEqual([
      { v: '650', zero: false },
      { v: '800', zero: false },
      { v: '0', zero: true },
      { v: '900', zero: false },
      { v: '600', zero: false },
    ]);
    expect(weekLetters('2026-10-09')).toEqual(['M', 'T', 'W', 'T', 'F']);
    expect(weekLetters('2026-10-05')).toEqual(['M']);
  });

  it('says whose places these are under each tab', () => {
    expect(boardNote('today', 61240)).toBe('The players around your score. Equal scores share a place. 61,240 played today.');
    expect(boardNote('week', 152400)).toBe('The players around your total since Monday. A day you didn’t play counts as 0.');
    expect(BOARD_TABS.map((t) => t.label)).toEqual(['Today', 'This week']);
  });

  it('asks for today’s board whichever tab is showing, since the result’s figure and chart are from it', () => {
    expect(boardsWanted('today')).toEqual(['today']);
    expect(boardsWanted('week')).toEqual(['today', 'week']);
  });

  it('forgets a board that failed when it is wanted again, and keeps the same boards when nothing failed', () => {
    const seen = { today: 'failed' as const, week: 1 };
    expect(dropFailed(seen, ['today'])).toEqual({ week: 1 });
    const fine = { today: 1 };
    expect(dropFailed(fine, ['today', 'week'])).toBe(fine);
  });

  it('writes a place with its suffix, "th" for the teens, and en-GB thousands', () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 101, 111, 1204].map(ord)).toEqual([
      '1st',
      '2nd',
      '3rd',
      '4th',
      '11th',
      '12th',
      '13th',
      '21st',
      '101st',
      '111th',
      '1,204th',
    ]);
    expect(standingText({ rank: 1204, players: 83500 })).toBe('1,204th this week');
    expect(standingText(null)).toBe('');
  });
});

describe('the words', () => {
  it('on the title screen are the handoff’s', () => {
    expect(TITLE_SCREEN).toEqual({
      pill: 'Cinedikt Daily',
      heading: 'Name Drop',
      lead: 'Today’s movie is hidden. Its cast shows up one name at a time, working up to the star. Name the movie in as few names as you can.',
      steps: ['See who’s in it', 'Guess, or show the next name', 'Fewer names, more points'],
      fine: 'You start with 1,000 points. Extra names, facts and wrong guesses cost points. There’s no clock.',
      play: 'Play',
    });
  });

  it('count today’s players under Play, one or many, and nobody before anyone has played', () => {
    expect(playedText(61240)).toEqual({ count: '61,240', rest: ' people have played today.' });
    expect(playedText(1)).toEqual({ count: '1', rest: ' person has played today.' });
    expect(playedText(0)).toBeNull();
  });

  it('in How it works are the handoff’s, with the prices the rules charge', () => {
    expect(HOW_IT_WORKS.heading).toBe('How it works');
    expect(HOW_IT_WORKS.items).toEqual([
      'Today’s movie starts as a blank card in its poster’s colour, and you see one person from its cast, with another movie they were in.',
      'Guess whenever you like. Each wrong guess, or each tap on the next name, shows another person, working up to the star.',
      'Every wrong guess says how warm it was: cold, warm or hot, with the decade and genre compared.',
      'Stuck? Buy a fact about the movie: a length range, a rating range, its genre, the decade and then a five-year range, or the director.',
      'Tap Movies on any name to see their movies on a Cinedikt map. Titles only show inside the ranges you’ve bought.',
      'A wrong guess also fills in anyone from the cast it shares with today’s movie.',
    ]);
    expect(HOW_IT_WORKS.then).toBe(
      'You start with 1,000 points. Each extra name costs 100. Wrong guesses cost 100, then 150, 200 and so on. Facts cost 50 to 250. There’s no clock.',
    );
    expect(HOW_IT_WORKS.close).toBe('Got it');
  });

  it('on the game page head it with the task, and with the answer once it is over', () => {
    expect(pageHeading(PLAYING)).toBe(PLAY_HEADING);
    expect(PLAY_HEADING).toBe('Name today’s movie');
    expect(pageLine(PLAYING)).toBe('Its cast shows up one name at a time, working up to the star.');
    expect(pageHeading(SOLVED)).toBe('The Matrix');
    expect(pageLine(SOLVED)).toBe('');
    expect(pageHeading(null)).toBe(PLAY_HEADING);
    expect(PLAY_LINE).toBe(pageLine(null));
  });

  it('for the field, Show the answer, the toasts and the countdown are the handoff’s', () => {
    expect(GUESS_FIELD).toBe('Name the movie');
    expect([SHOW_ANSWER, SHOW_ANSWER_SURE]).toEqual(['Show the answer', 'Sure? Show it']);
    expect(TOASTS).toEqual({
      tried: 'You’ve already tried that one',
      copied: 'Result copied',
      copyFailed: 'Couldn’t copy the result',
    });
    expect(NEXT_MOVIE_IN).toBe('Next movie in');
  });
});

describe('the banner’s line', () => {
  const week = { rank: 1204, players: 83500 };

  it('says the crowd, or the reader’s place this week, before the game', () => {
    expect(bannerLine(null, 61240, null)).toBe('61,240 playing today');
    expect(bannerLine(null, 61240, week)).toBe('1,204th this week');
    expect(bannerLine(null, 0, null)).toBe('');
  });

  it('says the points left during the game, and the score or the miss once it is over', () => {
    expect(bannerLine({ phase: 'play', won: false, pts: 800 }, 61240, week)).toBe('800 points left');
    expect(bannerLine({ phase: 'done', won: true, pts: 800 }, 61240, week)).toBe('800 points today');
    expect(bannerLine({ phase: 'done', won: false, pts: 0 }, 61240, week)).toBe('Missed today');
  });

  it('names the banner as the handoff does, and by its button’s words during a game', () => {
    expect(bannerLabel(143, null)).toBe('Cinedikt Daily, No. 143: Name Drop. Play');
    expect(bannerLabel(143, { phase: 'done' })).toBe('Cinedikt Daily, No. 143. See your result');
    expect(bannerLabel(143, { phase: 'play' })).toBe('Cinedikt Daily, No. 143: Name Drop. Keep going');
  });
});

describe('the Movies sheet', () => {
  it('takes in the years bought, the five years over the decade', () => {
    expect(yearSpan({})).toBeNull();
    expect(yearSpan({ decade: 1990 })).toEqual([1990, 1999]);
    expect(yearSpan({ decade: 1990, years: 1995 })).toEqual([1995, 1999]);
  });

  it('takes in the rating band bought, up to but not including its ceiling', () => {
    expect(ratingSpan({})).toBeNull();
    expect(ratingSpan({ rating: 1 })).toEqual([6, 7]);
    expect(ratingSpan({ rating: 3 })).toEqual([8, Infinity]);
  });

  it('counts the decade, the years, a rating band and the genre as ranges, and never the length or the director', () => {
    expect(rangeBought({})).toBe(false);
    expect(rangeBought({ length: 2, director: [LANA] })).toBe(false);
    for (const facts of [{ decade: 1990 }, { decade: 1990, years: 1995 }, { rating: 0 }, { genre: [] }]) {
      expect(rangeBought(facts), JSON.stringify(facts)).toBe(true);
    }
  });

  it('asks for the movies again whenever a range is bought, and not for anything else', () => {
    const none = rangesKey({});
    expect(rangesKey({ length: 2, director: [LANA] })).toBe(none);
    const keys = [none, rangesKey({ decade: 1990 }), rangesKey({ decade: 1990, years: 1995 }), rangesKey({ rating: 0 }), rangesKey({ genre: [] })];
    expect(new Set(keys).size).toBe(keys.length);
  });

  it('is titled for its person', () => {
    expect(sheetTitle('Joe Pantoliano')).toBe('Joe Pantoliano’s movies');
  });

  it('counts its movies, and once a range is bought how many of them can be read', () => {
    expect(sheetSub(0, 13, false)).toBe('13 movies · on Cinedikt');
    expect(sheetSub(5, 13, true)).toBe('5 of 13 movies readable · on Cinedikt');
    expect(sheetSub(1, 1, true)).toBe('1 of 1 movie readable · on Cinedikt');
    expect(sheetSub(0, 1, false)).toBe('1 movie · on Cinedikt');
  });

  it('says where titles show, and once ranges are bought which, and that today’s movie is unmarked', () => {
    expect(sheetLegend({})).toBe(
      'Titles only show inside the ranges you buy: the decade, the years, a rating range or the genre. Today’s movie is one of these cards.',
    );
    // A length or a director is not a range: nothing more can be read.
    expect(sheetLegend({ length: 2, director: [LANA] })).toBe(sheetLegend({}));
    expect(sheetLegend({ decade: 1990, rating: 3 })).toBe(
      'Titles show inside your ranges: 1990s · rated 8.0 or higher. Today’s movie is one of these cards, but it isn’t marked.',
    );
    expect(sheetLegend({ decade: 1990, years: 1995, genre: ['Action', 'Sci-Fi'] })).toBe(
      'Titles show inside your ranges: 1995–1999 · Action, Sci-Fi. Today’s movie is one of these cards, but it isn’t marked.',
    );
  });

  it('writes the lowest rating band in lower case inside the legend’s sentence, and as a heading in the facts row', () => {
    expect(sheetLegend({ rating: 0 })).toBe(
      'Titles show inside your ranges: rated below 6.0. Today’s movie is one of these cards, but it isn’t marked.',
    );
    expect(factValue('rating', { rating: 0 })).toBe('Below 6.0');
  });

  it('says how to guess from it once there is something to read, and before that where the ranges are', () => {
    expect(sheetHint(150, true)).toBe('Tap a movie to guess it. A wrong guess costs 150.');
    expect(sheetHint(1200, true)).toBe('Tap a movie to guess it. A wrong guess costs 1,200.');
    expect(sheetHint(150, false)).toBe('Buy a range to read this map. The facts are under the card.');
  });

  it('names a readable card for a screen reader, and a blank one by its year alone', () => {
    const fugitive = { title: 'The Fugitive', year: 1993, rating: 7.8 };
    expect(pickLine(fugitive)).toBe('1993 · IMDb 7.8');
    expect(pickLine({ year: 0, rating: 7 })).toBe('IMDb 7.0');
    expect(sheetCardLabel(fugitive, false)).toBe('The Fugitive, 1993, rated 7.8');
    expect(sheetCardLabel(fugitive, true)).toBe('The Fugitive, 1993, rated 7.8, already tried');
    expect(blankCardLabel(1985, false)).toBe('A movie from 1985. Buy a range to read it');
    expect(blankCardLabel(1985, true)).toBe('A movie from 1985, outside your ranges');
  });

  it('lays its map out at the handoff’s small metrics, and opens on the first readable card', () => {
    expect(SHEET_METRICS.desktop).toEqual({ cardW: 112, cardH: 42, railW: 44 });
    expect(SHEET_METRICS.phone).toEqual({ cardW: 100, cardH: 42, railW: 40 });
    expect([SHEET_METRICS.gap, SHEET_METRICS.padTop, SHEET_METRICS.padBottom, SHEET_METRICS.gapMark]).toEqual([4, 3, 3, 4]);
    expect(SHEET_METRICS.axisH).toBe(26);
    expect(SHEET_METRICS.labels).toEqual([4, 5, 6, 7, 8, 9]);
    expect(SHEET_READ_TOP).toBe(36);
  });
});

describe('time', () => {
  it('counts down to the next movie in hours, minutes and seconds', () => {
    expect(countdown(42_423_000)).toBe('11:47:03');
    expect(countdown(59_999)).toBe('0:00:59');
    expect(countdown(-1)).toBe('0:00:00');
  });

  it('runs on the server’s clock', () => {
    const local = Date.parse('2026-10-09T11:59:50Z');
    expect(clockOffset('2026-10-09T12:00:00Z', local)).toBe(10_000);
    expect(clockOffset('not a time', local)).toBe(0);
  });
});

describe('the reader’s midnight', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  // Tokyo's midnight going into 10 October is 15:00 UTC on the 9th. This
  // clock is 10 s behind the server's, so it reads 14:59:50 at midnight.
  const TOKYO_NEXT = '2026-10-10T00:00:00+09:00';
  const AT_MIDNIGHT_HERE = Date.parse('2026-10-09T14:59:50Z');

  it('comes once, when the server’s clock reaches it, however this one is set', () => {
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE - 60_000);
    const struck = vi.fn();
    watchMidnight(TOKYO_NEXT, 10_000, struck);
    vi.advanceTimersByTime(59_999);
    expect(struck).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(struck).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(86_400_000);
    expect(struck).toHaveBeenCalledTimes(1);
  });

  it('looks at the clock in steps, so a machine that slept through midnight is caught up within one', () => {
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE - 4 * 3_600_000);
    const struck = vi.fn();
    watchMidnight(TOKYO_NEXT, 10_000, struck);
    vi.setSystemTime(AT_MIDNIGHT_HERE + 600_000);
    expect(struck).not.toHaveBeenCalled();
    vi.advanceTimersByTime(MIDNIGHT_STEP_MS);
    expect(struck).toHaveBeenCalledTimes(1);
  });

  it('is not waited for when it has already gone, or cannot be read', () => {
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE + 1);
    const struck = vi.fn();
    watchMidnight(TOKYO_NEXT, 10_000, struck);
    watchMidnight('not a time', 0, struck);
    expect(vi.getTimerCount()).toBe(0);
    vi.advanceTimersByTime(86_400_000);
    expect(struck).not.toHaveBeenCalled();
  });

  it('is called off by what it hands back', () => {
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE - 3_600_000);
    const struck = vi.fn();
    const stop = watchMidnight(TOKYO_NEXT, 10_000, struck);
    vi.advanceTimersByTime(MIDNIGHT_STEP_MS * 3);
    stop();
    vi.advanceTimersByTime(3_600_000);
    expect(struck).not.toHaveBeenCalled();
  });

  it('tells a reader part way through a game that the day is over, and nobody else anything', () => {
    expect(midnightText(PLAYING)).toBe('That day’s game is over.');
    expect(midnightText(SOLVED)).toBe('');
    expect(midnightText(null)).toBe('');
  });
});

describe('the date', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it('is read in UTC, with Monday as the first day of the week', () => {
    expect(weekdayOf('2026-10-05')).toBe(0);
    expect(weekdayOf('2026-10-09')).toBe(4);
    expect(weekdayOf('2026-10-11')).toBe(6);
  });

  it('puts the number and the day in the header, and the number alone on a phone', () => {
    expect(dailyDayText('2026-10-09')).toBe('Friday 9 October');
    expect(dailyDayText('nonsense')).toBe('');
    expect(dailyDateText({ no: 143, date: '2026-10-09' }, false)).toBe('No. 143 · Friday 9 October');
    expect(dailyDateText({ no: 143, date: '2026-10-09' }, true)).toBe('No. 143');
  });

  it('is the puzzle’s calendar date wherever the reader is, never a day either side', () => {
    for (const zone of ['Pacific/Pago_Pago', 'Pacific/Kiritimati']) {
      vi.stubEnv('TZ', zone);
      expect(dailyDayText('2026-10-09')).toBe('Friday 9 October');
      expect(weekdayOf('2026-10-09')).toBe(4);
    }
  });
});

describe('a move', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is named by a key the server accepts, and still gets one where randomUUID is missing', () => {
    const a = newKey();
    expect(a).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(newKey()).not.toBe(a);
    vi.stubGlobal('crypto', { getRandomValues: (b: Uint8Array) => b.fill(171) });
    expect(newKey()).toBe('ab'.repeat(16));
  });

  it('is the same request from the same point, and a new one from the next or with another argument', () => {
    expect(moveSig({ kind: 'next' }, 3)).toBe('3:next:');
    expect(moveSig({ kind: 'next' }, 3)).not.toBe(moveSig({ kind: 'next' }, 4));
    expect(moveSig({ kind: 'buy', fact: 'decade' }, 3)).toBe('3:buy:decade');
    expect(moveSig({ kind: 'buy', fact: 'decade' }, 3)).not.toBe(moveSig({ kind: 'buy', fact: 'years' }, 3));
    expect(moveSig({ kind: 'guess', film: 'tt1' }, 3)).toBe('3:guess:tt1');
    expect(moveSig({ kind: 'reveal' }, 5)).toBe('5:reveal:');
  });
});

describe('a refused move', () => {
  it('says what went wrong, by what was being done', () => {
    expect(refusalText('points', 'buy')).toBe('Not enough points for that.');
    expect(refusalText('known', 'guess')).toBe('You’ve already tried that one');
    expect(refusalText('known', 'buy')).toBe('You already have that fact.');
    expect(refusalText('known', 'next')).toBe('Everyone’s showing.');
    expect(refusalText('bad', 'buy')).toBe('Buy the decade first.');
    expect(refusalText('bad', 'movies')).toBe('That name isn’t showing yet.');
    expect(refusalText('day', 'guess')).toBe(DAY_OVER);
  });

  it('says nothing when it hands back the game as it stands', () => {
    expect(refusalText('stale', 'next')).toBe('');
  });

  it('falls back to the network’s words for anything it does not know', () => {
    expect(refusalText(null)).toBe('Couldn’t reach Cinedikt. Try again.');
    expect(refusalText('unavailable')).toBe(UNREACHABLE);
    expect(refusalText('not-ready')).toBe('Today’s movie isn’t ready yet. Try again in a few minutes.');
  });

  it('carries the game back when it is stale, and nothing that is not a game', () => {
    expect(staleGame({ error: 'x', reason: 'stale', game: PLAYING })).toBe(PLAYING);
    expect(staleGame({ error: 'x', reason: 'stale', game: { log: [] } })).toBeNull();
    expect(staleGame({ error: 'x', reason: 'stale' })).toBeNull();
    expect(staleGame(null)).toBeNull();
  });
});

describe('the Daily’s motion', () => {
  it('matches the design’s numbers', () => {
    expect(PLAY_HIDE_MS).toBe(450);
    expect([NAME_OUT_MS, NAME_IN_MS, NAME_RISE_PX]).toEqual([300, 450, 6]);
    expect([FACE_GROW_MS, FACE_GROW_FROM, FACE_GROW_EASE]).toEqual([500, 0.7, 'cubic-bezier(0.2, 0.9, 0.3, 1.25)']);
    expect([END_REVEAL_FIRST_MS, END_REVEAL_STEP_MS]).toEqual([350, 140]);
    expect([0, 1, 2].map(endRevealDelay)).toEqual([350, 490, 630]);
    expect([PEEK_REST_MS, PEEK_FADE_MS, PEEK_GROW_MS, PEEK_GROW_FROM, PEEK_LEFT]).toEqual([160, 160, 220, 0.92, 54]);
    expect(PEEK_EASE).toBe('cubic-bezier(0.2, 0.9, 0.3, 1.2)');
    expect([CARD_TURN_MS, CARD_TURN_EASE]).toEqual([800, 'cubic-bezier(0.4, 0, 0.2, 1)']);
    expect([RESULT_AFTER_MS, RESULT_GAP_PX]).toEqual([1400, 12]);
    expect([SCORE_COUNT_AFTER_MS, SCORE_COUNT_MS]).toEqual([1500, 1100]);
    expect(REVEAL_CONFIRM_MS).toBe(3500);
    expect([SHEET_PHONE_SHARE, SHEET_CLOSE_PX, SHEET_SPRING_MS]).toEqual([0.9, 90, 250]);
    expect(SHEET_SPRING_EASE).toBe('cubic-bezier(0.2, 0.9, 0.3, 1)');
    expect([LIST_CLOSE_MS, SEARCH_WAIT_MS]).toEqual([120, 250]);
  });

  it('counts the score up from nought, easing out, and lands on it', () => {
    expect(countUp(600, 0)).toBe(0);
    expect(countUp(600, 550)).toBe(525);
    expect(countUp(600, SCORE_COUNT_MS)).toBe(600);
    expect(countUp(600, 5000)).toBe(600);
  });

  it('holds the score at nought while the card turns and the page reaches the result, then counts', () => {
    expect(scoreAt(600, 0)).toBe(0);
    expect(scoreAt(600, 1499)).toBe(0);
    expect(scoreAt(600, 1500)).toBe(0);
    expect(scoreAt(600, 1500 + 550)).toBe(525);
    expect(scoreAt(600, 1500 + 1100)).toBe(600);
    expect(scoreAt(600, 60_000)).toBe(600);
  });

  it('brings the result up 12px under the page’s top, and never above the top', () => {
    // The result 900px down a page scrolled 0, the page starting 64px
    // down the window.
    expect(resultScroll(0, 964, 64)).toBe(888);
    expect(resultScroll(300, 664, 64)).toBe(888);
    expect(resultScroll(0, 70, 64)).toBe(0);
  });
});

describe('the title screen’s motion', () => {
  it('rises each part in turn: 14px over 560ms on the settle curve, 80ms in and 90ms apart, held out of sight until then', () => {
    expect([RISE_PX, RISE_MS, RISE_EASE, RISE_FIRST_MS, RISE_STEP_MS]).toEqual([14, 560, 'cubic-bezier(0.16, 1, 0.3, 1)', 80, 90]);
    expect([0, 1, 6].map((i) => titleRise(i).options.delay)).toEqual([80, 170, 620]);
    expect(titleRise(0).keyframes).toEqual([
      { opacity: 0, translate: '0 14px' },
      { opacity: 1, translate: '0 0' },
    ]);
    expect(titleRise(3).options).toMatchObject({ duration: 560, easing: RISE_EASE, fill: 'backwards' });
  });

  it('floats the card 7px, turning it from −4° to −2°, 3.2s each way, and bobs its “?” 4px, 1.6s each way', () => {
    expect(TITLE_LOOPS.float.keyframes).toEqual([
      { translate: '0 0', rotate: '-4deg' },
      { translate: '0 -7px', rotate: '-2deg' },
    ]);
    expect(TITLE_LOOPS.float.options).toEqual({ duration: 3200, direction: 'alternate', iterations: Infinity, easing: 'ease-in-out' });
    expect(TITLE_LOOPS.bob.keyframes).toEqual([{ translate: '0 0' }, { translate: '0 -4px' }]);
    expect(TITLE_LOOPS.bob.options).toMatchObject({ duration: 1600, direction: 'alternate', iterations: Infinity });
  });

  it('crosses the card with a shine every 4.2s from 900ms, and Play with the banner’s every 3.6s from 1.4s', () => {
    expect(TITLE_LOOPS.sheen.options).toEqual({ duration: 4200, delay: 900, iterations: Infinity, easing: 'ease-in-out' });
    expect(TITLE_LOOPS.sheen.keyframes.map((k) => k.offset)).toEqual([0, 0.3, 1]);
    expect(TITLE_LOOPS.shine.options).toEqual({ duration: 3600, delay: 1400, iterations: Infinity, easing: 'ease-in-out' });
    expect(TITLE_LOOPS.shine.keyframes).toBe(PLAY_SHINE_KEYFRAMES);
    expect(PLAY_SHINE_KEYFRAMES.map((k) => [k.translate, k.offset])).toEqual([
      ['-120% 0', 0],
      ['320% 0', 0.28],
      ['320% 0', 1],
    ]);
  });

  it('pulses the next row in a 5px ring of the accent, 1.1s each way', () => {
    expect(TITLE_LOOPS.pulse.keyframes).toEqual([
      { boxShadow: '0 0 0 0 rgba(170, 140, 255, 0)' },
      { boxShadow: '0 0 0 5px rgba(170, 140, 255, 0.2)' },
    ]);
    expect(TITLE_LOOPS.pulse.options).toMatchObject({ duration: 1100, direction: 'alternate', iterations: Infinity });
  });

  it('drops a filled row into the next and hidden rows on a 6s loop from 700ms, landing at 24% and 52%', () => {
    expect([DROP_LOOP_MS, DROP_AFTER_MS, DROP_FROM_PX, DROP_OVER_PX]).toEqual([6000, 700, 28, 3]);
    expect([...DROP_LANDS]).toEqual([0.24, 0.52]);
    expect([DROP_HOLD_TO, DROP_GONE_AT]).toEqual([0.84, 0.94]);
    for (const [name, land] of [['drop1', 0.24], ['drop2', 0.52]] as const) {
      expect(TITLE_LOOPS[name].keyframes).toEqual(dropKeyframes(land));
      expect(TITLE_LOOPS[name].options).toEqual({ duration: 6000, delay: 700, iterations: Infinity, easing: 'ease-out' });
    }
    const frames = dropKeyframes(0.24);
    // Out of sight 28px up until just before it lands; 3px past; settled;
    // held to 84%; gone by 94%.
    expect(frames.map((k) => [k.opacity, k.translate])).toEqual([
      [0, '0 -28px'],
      [0, '0 -28px'],
      [1, '0 3px'],
      [1, '0 0'],
      [1, '0 0'],
      [0, '0 0'],
      [0, '0 0'],
    ]);
    expect(frames.map((k) => k.offset)).toEqual([0, 0.14, 0.24, 0.28, 0.84, 0.94, 1]);
    expect(dropKeyframes(0.52).map((k) => k.offset)).toEqual([0, 0.42, 0.52, 0.56, 0.84, 0.94, 1]);
  });
});

describe('the hidden card’s motion', () => {
  it('breathes a glow in the lightened poster colour, 16px at 12% to 34px at 42%, 2.6s each way', () => {
    const glow = cardGlow('#26382d')!;
    expect(glow.keyframes).toEqual([
      { boxShadow: '0 0 16px 1px rgba(136, 146, 140, 0.12)' },
      { boxShadow: '0 0 34px 8px rgba(136, 146, 140, 0.42)' },
    ]);
    expect(glow.options).toEqual({ duration: 2600, direction: 'alternate', iterations: Infinity, easing: 'ease-in-out' });
    expect(cardGlow('not a colour')).toBeNull();
  });

  it('bobs its “?” 4px, 1.8s each way', () => {
    expect(CARD_BOB.keyframes).toEqual([{ translate: '0 0' }, { translate: '0 -4px' }]);
    expect(CARD_BOB.options).toMatchObject({ duration: 1800, direction: 'alternate', iterations: Infinity });
  });

  it('sends a ripple of a new name’s colour 22px out over 850ms', () => {
    expect([RIPPLE_MS, RIPPLE_PX, RIPPLE_EASE]).toEqual([850, 22, 'cubic-bezier(0.2, 0.8, 0.2, 1)']);
    expect(cardRipple('oklch(0.76 0.13 205)')).toEqual({
      keyframes: [{ boxShadow: '0 0 0 0 oklch(0.76 0.13 205)' }, { boxShadow: '0 0 0 22px transparent' }],
      options: { duration: 850, easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)' },
    });
  });

  it('pulses the facts panel three times, 900ms each, a 12px ring of the accent fading as it spreads', () => {
    expect([NUDGE_PULSE_MS, NUDGE_PULSES, NUDGE_PULSE_PX]).toEqual([900, 3, 12]);
    expect(nudgePulse('oklch(0.74 0.15 295)')).toEqual({
      keyframes: [
        { boxShadow: 'inset 0 0 0 1px oklch(0.74 0.15 295), 0 0 0 0 rgba(170, 140, 255, 0.45)' },
        { boxShadow: 'inset 0 0 0 1px oklch(0.74 0.15 295), 0 0 0 12px rgba(170, 140, 255, 0)' },
      ],
      options: { duration: 900, iterations: 3, easing: 'ease-out' },
    });
  });

  it('keeps the panel’s edge in the theme’s own accent through the pulses, the light theme’s darker one too', () => {
    const light = nudgePulse('oklch(0.5 0.2 295)');
    // Only the ring spreading out is the glow's lavender, whatever the theme.
    const edge = 'inset 0 0 0 1px oklch(0.5 0.2 295), ';
    expect(light.keyframes.map((f) => f.boxShadow)).toEqual([
      `${edge}0 0 0 0 ${accentGlow(0.45)}`,
      `${edge}0 0 0 12px ${accentGlow(0)}`,
    ]);
    expect(light.options).toEqual({ duration: 900, iterations: 3, easing: 'ease-out' });
  });

  it('draws the edge in the glow’s colour when the accent cannot be read', () => {
    expect(nudgePulse('').keyframes.map((f) => f.boxShadow)).toEqual([
      'inset 0 0 0 1px rgba(170, 140, 255, 1), 0 0 0 0 rgba(170, 140, 255, 0.45)',
      'inset 0 0 0 1px rgba(170, 140, 255, 1), 0 0 0 12px rgba(170, 140, 255, 0)',
    ]);
  });
});

describe('touch', () => {
  it('reaches 44px round every small control, as the handoff sets each one out', () => {
    expect(HIT_INSETS).toEqual({
      triedChip: '-9px -3px',
      showAnswer: '-6px 0',
      fact: '-5px -3px',
      movies: '-5px -2px',
      tab: '-7px 0',
    });
  });

  it('takes a landscape phone as under 520px tall and wider than tall', () => {
    expect(LANDSCAPE_MAX_H).toBe(520);
    expect(isLandscapePhone(844, 390)).toBe(true);
    expect(isLandscapePhone(390, 844)).toBe(false);
    expect(isLandscapePhone(1280, 600)).toBe(false);
  });
});

describe('the screen, read live', () => {
  it('is a phone under 640px wide, whatever its height', () => {
    expect(liveScreen(375, 540).phone).toBe(true);
    expect(liveScreen(639, 1000).phone).toBe(true);
    expect(liveScreen(640, 400).phone).toBe(false);
  });

  it('is a landscape phone under 520px tall and wider than tall, and says so as the phone turns', () => {
    expect(liveScreen(844, 390)).toMatchObject({ phone: false, land: true });
    expect(liveScreen(390, 844)).toMatchObject({ phone: true, land: false });
  });

  it('carries the visual viewport’s height for the phone sheet, the window’s where there is none', () => {
    expect(liveScreen(375, 667, 420).viewH).toBe(420);
    expect(liveScreen(375, 667).viewH).toBe(667);
    expect(liveScreen(375, 667, 0).viewH).toBe(667);
  });
});

describe('the Daily’s words', () => {
  it('say "movie", never "film"', () => {
    const lost = ended([THIRTEENTH, { type: 'out' }], { pts: 0 });
    const words = [
      ...Object.values(TITLE_SCREEN).flat(),
      ...HOW_IT_WORKS.items,
      HOW_IT_WORKS.then,
      PLAY_HEADING,
      PLAY_LINE,
      factsHeading(false),
      factsHeading(true),
      ...factItems(gameOf()).flatMap((f) => [f.label, f.aria]),
      ...FACT_ORDER.flatMap((k) => Object.values(FACTS[k])),
      ...castRows(PLAYING).map((r) => (r.state === 'shown' ? r.line : '')),
      NEXT_ROW_LABEL,
      moviesLabel('Joe Pantoliano'),
      ...guessMessage(THIRTEENTH.guess, PLAYING).chips,
      guessPlaceholder(PLAYING),
      guessPlaceholder({ pts: 100, nextCost: 150 }),
      guessLabel(PLAYING),
      FACTS_NOTE,
      NUDGE_NOTE,
      resultKicker(SOLVED),
      resultKicker(lost),
      namesLine(SOLVED),
      namesLine(lost),
      shareText(143, SOLVED, 'https://cinedikt.com'),
      ...Object.values(betterLine(SOLVED, { beat: 1, solved: 1 }) ?? {}),
      ...Object.values(betterLine(lost, { beat: 1, solved: 1 }) ?? {}),
      boardNote('today', 2),
      boardNote('week', 2),
      sheetLegend({}),
      sheetLegend({ decade: 1990 }),
      sheetSub(1, 2, true),
      sheetSub(1, 2, false),
      sheetHint(150, true),
      sheetHint(150, false),
      blankCardLabel(1993, true),
      blankCardLabel(1993, false),
      bannerLabel(143, null),
      bannerLabel(143, SOLVED),
      ...['points', 'known', 'bad', 'unknown', 'day', 'done', 'cookie', 'busy', 'not-ready', null].map((r) =>
        refusalText(r, 'guess'),
      ),
      ...Object.values(TOASTS),
      NEXT_MOVIE_IN,
      NOT_READY,
      PLAY_AGAIN,
      AGAIN_FAILED,
      fmtN(1),
      andList(['a', 'b']),
    ];
    for (const w of words) expect(w).not.toMatch(/\bfilms?\b/i);
  });
});

describe('the game page', () => {
  it('puts the facts right under the card while the game is on, and the result, the leaderboard and a heading before the cast once over', () => {
    expect(pageSections(false)).toEqual(['top', 'facts', 'cast']);
    expect(pageSections(true)).toEqual(['top', 'about', 'result', 'board', 'castHead', 'cast']);
  });

  it('hides every name while Play holds them back, with none next', () => {
    const rows = openingRows();
    expect(rows).toHaveLength(CAST_SIZE);
    expect(rows.every((r) => r.state === 'hidden')).toBe(true);
    expect(rows.map((r) => (r.state === 'hidden' ? r.bar : 0))).toEqual([...BAR_WIDTHS]);
  });

  it('brings the names not seen in at the end one after another, 350ms in and 140ms apart, and leaves the rest', () => {
    expect([...revealDelays(castRows(SOLVED))]).toEqual([
      [4, 350],
      [5, 490],
    ]);
    // A solve on the first name: the other five, in reveal order.
    const first = ended([{ type: 'win' }], { pts: 1000, won: true });
    expect([...revealDelays(castRows(first)).values()]).toEqual([350, 490, 630, 770, 910]);
    // Nothing waits while the game is on.
    expect(revealDelays(castRows(PLAYING)).size).toBe(0);
  });

  it('knows everyone it can name, for the initials: the names showing, then the directors once bought or over', () => {
    expect(todaysPeople(PLAYING).map((p) => p.name)).toEqual([
      'Joe Pantoliano',
      'Gloria Foster',
      'Hugo Weaving',
      'Carrie-Anne Moss',
    ]);
    expect(todaysPeople({ ...gameOf(), facts: { director: [LANA, LILLY] } }).map((p) => p.id)).toEqual([
      JOE.id,
      LANA.id,
      LILLY.id,
    ]);
    expect(todaysPeople(SOLVED)).toHaveLength(8);
  });

  it('finds the wrong guess a chip asks for, and the newest', () => {
    const game = gameOf({ log: [THIRTEENTH, SPEED] });
    expect(guessById(game, SPEED.guess.id)).toBe(SPEED.guess);
    expect(guessById(game, 'tt0000001')).toBeNull();
    expect(guessById(game, null)).toBeNull();
    expect(newestGuess(game)).toBe(SPEED.guess);
    expect(newestGuess(gameOf())).toBeNull();
    // The message box draws at most four of the faces it shares.
    expect(MESSAGE_FACES).toBe(4);
  });

  it('marks which of the result’s six faces the reader saw', () => {
    expect(resultFaces(SOLVED).map((f) => [f.person.name, f.seen])).toEqual([
      ['Joe Pantoliano', true],
      ['Gloria Foster', true],
      ['Hugo Weaving', true],
      ['Carrie-Anne Moss', true],
      ['Laurence Fishburne', false],
      ['Keanu Reeves', false],
    ]);
  });

  it('searches from two characters, as the header does', () => {
    expect(SEARCH_MIN_CHARS).toBe(2);
  });
});

describe('a move the server would refuse', () => {
  it('is said at once, in the words the refusal would bring', () => {
    expect(earlyRefusal(gameOf({ slots: ALL_SHOWN }), { kind: 'next' })).toBe('Everyone’s showing.');
    expect(earlyRefusal(gameOf({ pts: 100 }), { kind: 'next' })).toBe('Not enough points for that.');
    expect(earlyRefusal(PLAYING, { kind: 'buy', fact: 'decade' })).toBe('You already have that fact.');
    expect(earlyRefusal(gameOf(), { kind: 'buy', fact: 'years' })).toBe('Buy the decade first.');
    expect(earlyRefusal(gameOf({ pts: 250 }), { kind: 'buy', fact: 'director' })).toBe('Not enough points for that.');
    expect(earlyRefusal(PLAYING, { kind: 'guess', film: THIRTEENTH.guess.id })).toBe('You’ve already tried that one');
  });

  it('is nothing for a move worth sending', () => {
    expect(earlyRefusal(gameOf(), { kind: 'next' })).toBe('');
    expect(earlyRefusal(PLAYING, { kind: 'buy', fact: 'years' })).toBe('');
    expect(earlyRefusal(gameOf({ pts: 51 }), { kind: 'buy', fact: 'length' })).toBe('');
    expect(earlyRefusal(PLAYING, { kind: 'guess', film: MATRIX.id })).toBe('');
    // A wrong guess is never refused for its price: one the points cannot
    // cover ends the game instead. Nor is giving up.
    expect(earlyRefusal(gameOf({ pts: 50, nextCost: 150 }), { kind: 'guess', film: MATRIX.id })).toBe('');
    expect(earlyRefusal(gameOf({ pts: 1 }), { kind: 'reveal' })).toBe('');
  });
});

describe('the visual viewport', () => {
  it('places the page in it, whole pixels, so the guess bar rides over an on-screen keyboard', () => {
    expect(viewportFit({ height: 351.4, offsetTop: 188.6, scale: 1 })).toEqual({ top: 189, height: 351 });
    expect(viewportFit({ height: 812, offsetTop: 0, scale: 1 })).toEqual({ top: 0, height: 812 });
  });

  it('lets the page keep its own size while the reader is zoomed in, or with nothing to go on', () => {
    expect(viewportFit({ height: 406, offsetTop: 120, scale: 2 })).toBeNull();
    expect(viewportFit({ height: 0, offsetTop: 0, scale: 1 })).toBeNull();
    expect(viewportFit(null)).toBeNull();
    expect(viewportFit(undefined)).toBeNull();
  });
});
