import {
  useEffect,
  useMemo,
  useRef,
  type ReactNode,
  type RefObject,
  type WheelEvent,
} from 'react';
import type { FaceCardEvents, FaceHold } from './faceCard';
import { initialsFor, type GridPayload, type GridPerson } from './grid';
import { carriedFirst, personVars } from './personColour';
import { PersonFace } from './PersonFace';
import { useHoverDelay, useTapGuard, type TapGuard } from './tap';
import { useResolvedTheme } from './theme';

interface Props {
  /** The map's own list, in its own order. The row reorders a copy. */
  people: GridPerson[];
  /** People who were also on the map shown before this one. They lead
   *  the row, so the ones the reader was following are where they left
   *  them. */
  carried: ReadonlySet<string>;
  selected: Set<string>;
  /** People on the card the pointer is resting on, which lights their chips. */
  lit: Set<string>;
  /** The person being previewed by a pointer resting on their chip. That
   *  chip is lit too, so the reader can see whose films stayed bright. */
  hovered: string | null;
  /** How many of each person's films this map holds (see filmCounts).
   *  A person missing from it shows no number. */
  counts: ReadonlyMap<string, number>;
  /** A person's photo, from the payload or asked for since; undefined
   *  while there is none to show. */
  photoOf: (p: GridPerson) => string | undefined;
  onToggle: (id: string) => void;
  onHover: (id: string | null) => void;
  onClear: () => void;
  /** A pointer resting on a chip, and leaving it, for its person's
   *  bigger photo (see useFaceCard). */
  onFace: FaceCardEvents['onFace'];
  offFace: FaceCardEvents['offFace'];
  /** A press on a chip, which a finger holds to see the bigger photo. */
  hold: FaceHold;
  /** Rendered before "Everyone": the active-filter pill, when there is
   *  one. Nothing that hides content may be invisible. */
  lead?: ReactNode;
  /** The "Everyone" chip, so focus has somewhere to land when the pill
   *  before it is cleared away. */
  allRef?: RefObject<HTMLButtonElement | null>;
}

/** One line of chips: everyone, then the people carried over from the
 *  last map, then everyone else in billing order. A chip selects that
 *  person; the grid dims everything they are not in.
 *
 *  Each chip carries its person's photo in a ring of their colour, the
 *  same one their marks have on the cards, or their initials on a disc
 *  of it, and the face's shape says cast or director. After the name
 *  comes how many of their films are on this map, so the reader can
 *  tell at a glance whose work the map is mostly made of. A pointer
 *  resting on a chip, or a finger held on one, shows the photo bigger
 *  (see chipEvents). */
export function PeopleChips({
  people,
  carried,
  selected,
  lit,
  hovered,
  counts,
  photoOf,
  onToggle,
  onHover,
  onClear,
  onFace,
  offFace,
  hold,
  lead,
  allRef,
}: Props) {
  useEffect(() => () => onHover(null), [onHover]);
  const theme = useResolvedTheme();
  // Worked out over the whole map, so a person has the same initials
  // here as on every card.
  const codes = useMemo(() => initialsFor(people), [people]);
  const row = useRef<HTMLDivElement>(null);
  // The strip scrolls sideways, so the same rule the map uses applies:
  // a chip that ends a flick was not chosen.
  const tap = useTapGuard();
  // And a pointer crossing the row on its way elsewhere should not make
  // the whole map flash. It has to stop on a chip to mean it.
  const rest = useHoverDelay(onHover);
  return (
    <div
      className="cd-chips"
      ref={row}
      onMouseLeave={rest.leave}
      onPointerDown={tap.onPointerDown}
      onPointerMove={tap.onPointerMove}
      onScroll={tap.onScroll}
      onWheel={(e) => wheelSideways(e, row.current)}
    >
      {lead}
      {/* data-chip names each chip for the slide into its new place
          after a move to another map (flipChips in GridApp.tsx). */}
      <button
        type="button"
        ref={allRef}
        data-chip="all"
        className={`cd-chip cd-chip-all${selected.size === 0 ? ' cd-chip-on' : ''}`}
        onClick={() => tap.allows() && onClear()}
      >
        Everyone
      </button>
      {carriedFirst(people, carried).map((p) => {
        const on = selected.has(p.id);
        const shining = lit.has(p.id) || hovered === p.id;
        const n = counts.get(p.id);
        return (
          <button
            key={p.id}
            type="button"
            data-chip={p.id}
            className={`cd-chip${on ? ' cd-chip-on' : ''}${shining ? ' cd-chip-lit' : ''}`}
            style={personVars(p, theme)}
            aria-pressed={on}
            aria-label={chipName(p.name, n)}
            {...chipEvents(p.id, { tap, rest, onToggle, onFace, offFace, hold })}
          >
            <PersonFace
              photo={photoOf(p)}
              code={codes.get(p.id) ?? '?'}
              size="chip"
              square={p.role === 'director'}
            />
            <span className="cd-chip-name">{p.name}</span>
            {n != null && <span className="cd-chip-count">{n}</span>}
          </button>
        );
      })}
    </div>
  );
}

/** A chip's events. A click that was meant toggles its person, unless
 *  it ends a finger's hold, which was for the bigger photo. A pointer
 *  resting on it lights their films after the row's short delay and
 *  opens their bigger photo after a longer one; leaving puts both away.
 *  A finger held on it opens the photo, and lifting closes it. */
export function chipEvents(
  id: string,
  o: {
    tap: Pick<TapGuard, 'allows'>;
    rest: { enter: (id: string) => void; leave: () => void };
    onToggle: (id: string) => void;
    onFace: FaceCardEvents['onFace'];
    offFace: FaceCardEvents['offFace'];
    hold: FaceHold;
  },
) {
  return {
    onClick: () => {
      // Asked first, and every time: the hold is over either way.
      if (o.hold.click()) return;
      if (o.tap.allows()) o.onToggle(id);
    },
    onMouseEnter: (e: { currentTarget: HTMLElement }) => {
      o.rest.enter(id);
      o.onFace(id, e.currentTarget, 'chip');
    },
    onMouseLeave: () => {
      o.rest.leave();
      o.offFace();
    },
    onPointerDown: (e: { currentTarget: HTMLElement; pointerType: string }) =>
      o.hold.down(id, e.currentTarget, e.pointerType),
    onPointerUp: () => o.hold.up(),
    onPointerCancel: () => o.hold.cancel(),
    onContextMenu: (e: { preventDefault: () => void }) => {
      if (o.hold.menu()) e.preventDefault();
    },
  };
}

/** A wheel over the chips moves them sideways.
 *
 *  A mouse wheel and a trackpad's vertical flick both send deltaY, and a
 *  row that only scrolls horizontally does nothing with it — so on a
 *  desktop the chips past the edge could only be reached by dragging.
 *  A horizontal gesture already works and is left alone. */
function wheelSideways(e: WheelEvent<HTMLDivElement>, row: HTMLDivElement | null) {
  if (!row || e.deltaX !== 0 || e.deltaY === 0) return;
  const room = row.scrollWidth - row.clientWidth;
  if (room <= 0) return;
  // Not past either end: the page behind should still scroll when the
  // row has nowhere left to go.
  const to = Math.min(Math.max(row.scrollLeft + e.deltaY, 0), room);
  if (to === row.scrollLeft) return;
  e.preventDefault();
  row.scrollLeft = to;
}

/** How many of each person's films this map holds, by person id.
 *
 *  Counted off the spine, which names the people on each film by their
 *  place in the chip row. Every film of theirs counts once, the searched
 *  film included, whatever the filters are doing: it says how much of
 *  the map is theirs, not how much of it is lit. The spine is the map's
 *  most-voted four hundred, so this is their films on this map, not
 *  their whole career.
 *
 *  A spine sent without people on its films (an older server, or a
 *  fixture) says nothing about who is where, and gives no counts rather
 *  than a row of zeros. */
export function filmCounts(payload: Pick<GridPayload, 'people' | 'films'>): Map<string, number> {
  const counts = new Map<string, number>();
  for (const tuple of payload.films) {
    // Once per film: a person credited twice is still one card.
    for (const i of new Set(tuple[4] ?? [])) {
      const id = payload.people[i]?.id;
      if (id !== undefined) counts.set(id, (counts.get(id) ?? 0) + 1);
    }
  }
  return counts;
}

/** A chip's accessible name: the person, and how many of their movies
 *  the map holds, said as words rather than a bare number after a name.
 *  Without a count the chip's own text is its name. */
export function chipName(name: string, count: number | undefined): string | undefined {
  if (count == null) return undefined;
  return `${name}, ${count} ${count === 1 ? 'movie' : 'movies'}`;
}
