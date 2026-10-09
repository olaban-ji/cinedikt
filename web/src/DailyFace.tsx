import { useState } from 'react';
import type { DailyPerson } from './api';
import { hueColour, toneStyle } from './daily';
import type { Theme } from './theme';

/** Where a Name Drop face is drawn, which sets its size: a cast row's 44px,
 *  the result's 34px, and the message box's 24px. */
export type DailyFaceSize = 'row' | 'result' | 'message';

/** One of today's people as Name Drop draws them: a disc of their colour,
 *  mixed into the surface, ringed in it a gap out, with their photo over
 *  it, cropped round the head, or their initials in the colour when TMDb
 *  has no photo of them or the photo will not load.
 *
 *  Not the map's PersonFace, whose photo sits inset inside a ring of its
 *  own: here the photo fills the disc and the ring stands off it, as the
 *  game's design draws its faces. Decorative: whatever it sits beside
 *  already names the person. */
export function DailyFace({
  person,
  code,
  size,
  theme,
  dim = false,
}: {
  person: DailyPerson;
  /** From codesOf: the initials, unique among today's people. */
  code: string;
  size: DailyFaceSize;
  theme: Theme;
  /** One of the result's faces the reader didn't see: faded, grey, and
   *  ringed in the line colour rather than theirs. */
  dim?: boolean;
}) {
  // The address that failed, so a face handed another one tries again.
  const [failed, setFailed] = useState<string | null>(null);
  const photo = person.photo && person.photo !== failed ? person.photo : null;
  return (
    <span
      className={`cd-nd-face cd-nd-face-${size}${dim ? ' cd-nd-face-dim' : ''}`}
      style={toneStyle(hueColour(person.hue, theme))}
      aria-hidden="true"
    >
      <span className="cd-nd-face-code">{code}</span>
      {photo && (
        <img
          key={photo}
          className="cd-nd-face-photo"
          src={photo}
          alt=""
          decoding="async"
          onError={() => setFailed(photo)}
        />
      )}
    </span>
  );
}
