import { useLayoutEffect, useReducer, useRef, type Ref } from 'react';

/** Where a face is drawn, which sets its size (see .cd-face-chip and the
 *  rest in grid.css). */
type FaceSize = 'chip' | 'preview' | 'sheet';

interface Props {
  photo?: string;
  /** From initialsFor: the code the cards use. */
  code: string;
  size: FaceSize;
  /** A rounded square instead of a circle: directors, where the shape says the role. */
  square: boolean;
}

/** How far one address has got. Everything here is about `photo`: a
 *  face given another address starts again, neither loaded nor failed. */
export interface FaceState {
  photo: string | undefined;
  /** The image has arrived, so the photo shows and the initials go. */
  loaded: boolean;
  /** The image could not be loaded, so the initials stay for good. */
  failed: boolean;
}

/** What can happen to a face: another address, or its image arriving or
 *  failing. The image's own events carry the address they were for, so
 *  one that lands for an address the face has moved on from changes
 *  nothing. */
type FaceEvent =
  | { type: 'photo'; photo: string | undefined }
  | { type: 'load'; photo: string }
  | { type: 'error'; photo: string };

/** The addresses that have loaded this visit. The browser has each of
 *  them in hand, so a face given one starts with its photo showing, and
 *  one seen before shows at once wherever it appears next. Taking it as
 *  loaded in a layout effect would be too late for that: the change
 *  lands only after the preview or the panel around the face has
 *  measured itself in a layout effect of its own, which settles the
 *  face's styles as they were, so the photo would still fade in. */
const arrived = new Set<string>();

/** Records that an address has loaded, so every face given it from now
 *  on starts with its photo showing. */
export function photoArrived(photo: string): void {
  arrived.add(photo);
}

/** A face for this address that has seen nothing happen yet: loaded
 *  already if the address has loaded this visit, and otherwise not. */
export function faceStart(photo: string | undefined, seen: ReadonlySet<string> = arrived): FaceState {
  return { photo, loaded: photo != null && seen.has(photo), failed: false };
}

/** A face after `e`. The same object when nothing changes, so it can be
 *  settled while rendering without a loop. */
export function faceAfter(s: FaceState, e: FaceEvent): FaceState {
  switch (e.type) {
    case 'photo':
      return e.photo === s.photo ? s : faceStart(e.photo);
    case 'load':
      return e.photo !== s.photo || s.loaded || s.failed ? s : { ...s, loaded: true };
    case 'error':
      return e.photo !== s.photo || s.failed ? s : { ...s, loaded: false, failed: true };
  }
}

/** Whether an image has already arrived, as one the browser had in hand
 *  has by the time it is on the page. A failed image is complete too,
 *  with nothing in it. */
export function alreadyLoaded(img: Pick<HTMLImageElement, 'complete' | 'naturalWidth'> | null): boolean {
  return img != null && img.complete && img.naturalWidth > 0;
}

/** A person's photo in a ring of their colour, and their initials on a
 *  disc of it in the same box: until the photo has loaded, for good when
 *  there is none, and if it fails to load. As the photo arrives the
 *  initials fade out while the ring and the photo fade in.
 *
 *  Decorative: whatever it sits in already names the person. Its colour
 *  is the --tone of the element around it. */
export function PersonFace({
  photo,
  code,
  size,
  square,
  onShown,
}: Props & {
  /** Told whether the photo is showing: as the face starts, and each time
   *  that changes, before the change is painted. */
  onShown?: (shown: boolean) => void;
}) {
  const [kept, dispatch] = useReducer(faceAfter, photo, faceStart);
  // Settled while rendering, so the last address's state is never drawn
  // with this one.
  if (kept.photo !== photo) dispatch({ type: 'photo', photo });
  const img = useRef<HTMLImageElement>(null);
  const loaded = (at: string) => {
    photoArrived(at);
    dispatch({ type: 'load', photo: at });
  };
  // A photo the browser already has in hand is taken as loaded before
  // anything is painted, rather than after its load event, which only
  // comes after a paint.
  useLayoutEffect(() => {
    if (photo && alreadyLoaded(img.current)) loaded(photo);
  }, [photo]);
  // The latest function, read when there is news, so one passed afresh
  // on each render is not told the same thing again.
  const tell = useRef(onShown);
  tell.current = onShown;
  useLayoutEffect(() => {
    tell.current?.(kept.loaded);
  }, [kept.loaded]);
  return (
    <FaceView
      photo={photo}
      code={code}
      size={size}
      square={square}
      face={kept}
      imgRef={img}
      onLoad={() => photo && loaded(photo)}
      onError={() => photo && dispatch({ type: 'error', photo })}
    />
  );
}

/** The face as it stands: PersonFace's drawing, with its state handed in. */
export function FaceView({
  photo,
  code,
  size,
  square,
  face,
  imgRef,
  onLoad,
  onError,
}: Props & {
  face: Pick<FaceState, 'loaded' | 'failed'>;
  imgRef?: Ref<HTMLImageElement>;
  onLoad?: () => void;
  onError?: () => void;
}) {
  const { loaded, failed } = face;
  return (
    <span
      className={`cd-face cd-face-${size}${square ? ' cd-face-square' : ''}${loaded ? ' cd-face-in' : ''}`}
      aria-hidden="true"
    >
      <span className="cd-face-initials">{code}</span>
      {photo && !failed && (
        <>
          <span className="cd-face-ring" />
          <img
            key={photo}
            ref={imgRef}
            className="cd-face-photo"
            src={photo}
            alt=""
            decoding="async"
            onLoad={onLoad}
            onError={onError}
          />
        </>
      )}
    </span>
  );
}
