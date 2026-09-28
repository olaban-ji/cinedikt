import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { bigPhoto, type FaceCard } from './faceCard';
import type { GridPerson } from './grid';
import { roleLine } from './GridSheet';
import { stillNow } from './motion';
import { personColour } from './personColour';
import { ENTER_MS } from './sheet';
import { useResolvedTheme, type Theme } from './theme';

interface Props {
  /** Where it goes, worked out as it opened (see useFaceCard). */
  card: FaceCard;
  person: GridPerson;
  /** The person's photo, the small one the chips and faces draw. The card
   *  draws the bigger one from the same path, which useFaceCard waited
   *  for before opening it. */
  photo: string;
  /** The searched film's title, which the role line names. */
  anchorTitle: string;
}

/** A bigger photo of a person, with their name and what they did on the
 *  searched film, beside the chip or preview face it was asked for from.
 *
 *  For looking only: it never takes the pointer or the focus, and it is
 *  hidden from assistive technology, because the chip's label already
 *  says who it is. One for the page, over everything but the toast, so it
 *  is drawn on the body rather than inside whatever asked for it. Mounted
 *  afresh for each card, so each one comes in from its own edge. */
export function PersonCard(props: Props) {
  const theme = useResolvedTheme();
  // In a moment after it mounts, so its entrance has somewhere to run
  // from; at once for a reader who has asked for nothing to move.
  const [shown, setShown] = useState(stillNow);
  useEffect(() => {
    const t = window.setTimeout(() => setShown(true), ENTER_MS);
    return () => window.clearTimeout(t);
  }, []);
  return createPortal(<PersonCardView {...props} theme={theme} shown={shown} />, document.body);
}

/** The card as it stands: PersonCard's drawing, with its theme and its
 *  entrance handed in. Every card is a portrait rectangle, cast and
 *  directors alike: the role line says which. */
export function PersonCardView({
  card,
  person: p,
  photo,
  anchorTitle,
  theme,
  shown,
}: Props & { theme: Theme; shown: boolean }) {
  const { x, y, down } = card;
  const big = bigPhoto(photo);
  return (
    <div
      className={`cd-person-card${shown ? ' cd-person-card-in' : ''}${down ? '' : ' cd-person-card-up'}`}
      style={{
        left: x,
        top: down ? y : undefined,
        bottom: down ? undefined : y,
        ['--tone' as string]: personColour(p.id, theme),
      }}
      aria-hidden="true"
    >
      <span className="cd-person-card-frame">
        <span className="cd-person-card-photo" style={{ backgroundImage: `url("${big}")` }} />
      </span>
      <span className="cd-person-card-text">
        <span className="cd-person-card-name">{p.name}</span>
        <span className="cd-person-card-role">{roleLine(p, anchorTitle)}</span>
      </span>
    </div>
  );
}
