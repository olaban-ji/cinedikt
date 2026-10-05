import type { RefObject } from 'react';

/** The mark's size in the header, on every screen. The slot reserves
 *  exactly this whether or not there is a mark in it yet, so the loader
 *  landing and the real mark taking its place never move the header. */
const MARK_W = 14.7;
const MARK_H = 22.5;

/** The mark stands in for the C of "Cinedikt": a C left open on the right,
 *  its top end rising into the hooked neck of a lowercase delta — δίκτυο,
 *  network — with an accent dot at the centre for the searched film.
 *
 *  The accent is the dot's alone. The stroke takes `currentColor` so the
 *  mark darkens and lightens with the text beside it. */
function Mark() {
  return (
    <svg
      className="cd-mark"
      viewBox="15.5 9.5 29.5 45"
      width={MARK_W}
      height={MARK_H}
      aria-hidden="true"
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
      <circle className="cd-mark-dot" cx="32" cy="38" r="5" />
    </svg>
  );
}

interface Props {
  /** True on phones, where only the mark shows. */
  markOnly: boolean;
  href: string;
  onClick: (e: React.MouseEvent<HTMLAnchorElement>) => void;
  /** While the opening screen is loading, the mark is somewhere else:
   *  drawn over the tiles, on its way here. The header keeps its exact
   *  space so nothing moves when it lands, but draws nothing in it. */
  hollow?: boolean;
  /** "inedikt" is held back until the mark is nearly home, so the word
   *  arrives around its own C rather than ahead of it. */
  wordIn?: boolean;
  /** The empty slot, measured so the loader knows where to fly to. */
  slotRef?: RefObject<HTMLSpanElement | null>;
}

/** A link home. On a phone the mark carries it alone, in a box 34 wide
 *  and 44 high that the stylesheet widens to a 44 px target; everywhere
 *  else "inedikt" follows it on the same baseline. */
export function Wordmark({
  markOnly,
  href,
  onClick,
  hollow = false,
  wordIn = true,
  slotRef,
}: Props) {
  return (
    <a className="cd-wordmark" href={href} onClick={onClick} aria-label="Cinedikt, home">
      {/* The mark and the word share a baseline, so the mark reads as
          the word's first letter. The link around them only centres the
          pair in its 44 px of height. */}
      <span className="cd-wordmark-inner">
        {/* The slot is the same box either way, so the header never
            changes shape between the loader landing and the real mark
            taking its place. */}
        <span className="cd-wordmark-slot" ref={slotRef} aria-hidden="true">
          {hollow ? null : <Mark />}
        </span>
        {!markOnly && (
          <span className={`cd-wordmark-word${wordIn ? ' cd-wordmark-word-in' : ''}`} aria-hidden="true">
            inedikt
          </span>
        )}
      </span>
    </a>
  );
}
