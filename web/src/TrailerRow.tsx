import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type RefObject,
} from 'react';
import { capture } from './analytics';
import { fetchTrailer, trailerKnown } from './api';
import { stillNow } from './motion';
import { ENTER_MS } from './sheet';
import { canRest } from './tap';
import {
  TRAILER_GONE_MS,
  TRAILER_REST_MS,
  YT_LISTENING,
  embedSrc,
  isFor,
  playerLink,
  playerVars,
  startPlay,
  stopPlay,
  videoHeight,
  videoWidth,
  watchUrl,
  type Play,
  type PlayWhere,
} from './trailer';

/** The one trailer player on the page. GridApp holds it, so the preview
 *  and the panel share it and opening one closes the other. */
export interface Player {
  /** What is open, as of this render. */
  play: Play | null;
  /** What is open right now, for a handler that runs between renders. */
  now: () => Play | null;
  /** Open a player at this width, closing any other. */
  start: (where: PlayWhere, id: string, muted: boolean, W: number, synLines?: number | null) => void;
  /** Close whatever is open, or only a player open in `where`. */
  stop: (where?: PlayWhere) => void;
  /** Take away a player open in `where` at once, with no close to play:
   *  for a place that has itself gone. */
  drop: (where: PlayWhere) => void;
  /** Turn the sound on, or off again. */
  sound: () => void;
  /** The box the video bleeds across is a new width; the video follows. */
  resize: (where: PlayWhere, id: string, W: number) => void;
  /** The frame, which the commands are sent to. There is only ever one. */
  frame: RefObject<HTMLIFrameElement | null>;
}

export function usePlayer(): Player {
  const [play, setPlay] = useState<Play | null>(null);
  // Kept beside the state, so a handler called twice before React has
  // rendered (a click during a close, say) sees what the first did.
  const current = useRef<Play | null>(null);
  const frame = useRef<HTMLIFrameElement | null>(null);
  const timer = useRef(0);

  const put = useCallback((next: Play | null) => {
    current.current = next;
    setPlay(next);
  }, []);

  // Holds the commands until the player says it is ready, then sends it
  // the state as it stands (see playerLink).
  const [link] = useState(() =>
    playerLink((message) => frame.current?.contentWindow?.postMessage(message, '*')),
  );
  const { send } = link;

  useEffect(() => () => window.clearTimeout(timer.current), []);

  useEffect(() => {
    const heard = (e: MessageEvent) => {
      if (e.source === frame.current?.contentWindow) link.heard(e.data, current.current);
    };
    window.addEventListener('message', heard);
    return () => window.removeEventListener('message', heard);
  }, [link]);

  const start = useCallback(
    (where: PlayWhere, id: string, muted: boolean, W: number, synLines: number | null = null) => {
      const was = current.current;
      const next = startPlay(was, where, id, muted, W, stillNow(), synLines);
      if (!next) return;
      window.clearTimeout(timer.current);
      if (isFor(was, where, id)) {
        // Taken back while it was closing. Its frame is still here and
        // paused, so it is told to carry on, with the sound as asked for
        // this time, rather than loaded again.
        send(muted ? 'mute' : 'unMute');
        send('playVideo');
      } else {
        // A new frame mounts, and nothing it is sent is heard until it
        // says it is ready.
        link.reset();
      }
      put(next);
      capture('trailer_play', { from: where, muted });
      if (next.open) return;
      // A moment after it mounts, so the transitions have its closed
      // state to run from. A timer, as the sheets use, rather than a
      // frame.
      timer.current = window.setTimeout(() => {
        const p = current.current;
        if (isFor(p, where, id) && !p.open && !p.closing) put({ ...p, open: true });
      }, ENTER_MS);
    },
    [link, put, send],
  );

  const stop = useCallback(
    (where?: PlayWhere) => {
      const was = current.current;
      if (where && was?.where !== where) return;
      const next = stopPlay(was);
      if (!next) return;
      // At once, so the sound stops as the close starts rather than when
      // the frame is finally taken away.
      send('pauseVideo');
      window.clearTimeout(timer.current);
      if (stillNow()) {
        put(null);
        return;
      }
      put(next);
      timer.current = window.setTimeout(() => {
        if (current.current?.closing) put(null);
      }, TRAILER_GONE_MS);
    },
    [put, send],
  );

  const drop = useCallback(
    (where: PlayWhere) => {
      const was = current.current;
      if (was?.where !== where) return;
      if (!was.closing) send('pauseVideo');
      window.clearTimeout(timer.current);
      put(null);
    },
    [put, send],
  );

  const sound = useCallback(() => {
    const p = current.current;
    if (!p || p.closing) return;
    if (p.muted) {
      send('unMute');
      // A muted start that the browser held back, or a video that has
      // ended, plays on from here.
      send('playVideo');
    } else {
      send('mute');
    }
    put({ ...p, muted: !p.muted });
  }, [put, send]);

  const resize = useCallback(
    (where: PlayWhere, id: string, W: number) => {
      const p = current.current;
      if (!isFor(p, where, id) || p.W === W) return;
      put({ ...p, W, VH: videoHeight(W) });
    },
    [put],
  );

  const now = useCallback(() => current.current, []);

  return useMemo(
    () => ({ play, now, start, stop, drop, sound, resize, frame }),
    [play, now, start, stop, drop, sound, resize],
  );
}

/** A film's trailer, for its row: undefined while it is being asked for,
 *  then its YouTube key, or null when it has none that plays embedded or
 *  the lookup failed. Asked the first time the row is shown; a film
 *  already answered this visit shows its answer at once. */
export function useTrailer(id: string): string | null | undefined {
  const [got, setGot] = useState<{ id: string; key: string | null | undefined }>(() => ({
    id,
    key: trailerKnown(id),
  }));
  useEffect(() => {
    if (trailerKnown(id) !== undefined) return;
    let live = true;
    fetchTrailer(id).then((key) => {
      if (live) setGot({ id, key });
    });
    return () => {
      live = false;
    };
  }, [id]);
  return got.id === id ? got.key : trailerKnown(id);
}

interface RowProps {
  where: PlayWhere;
  id: string;
  title: string;
  /** From useTrailer. */
  trailer: string | null | undefined;
  player: Player;
  /** Start this row's player, the video drawn at width `W`. The panel
   *  starts it as it stands; the preview first works out how to make
   *  room for it. */
  onPlay: (muted: boolean, W: number) => void;
}

/** Watch trailer, and the player that opens where it sits. While the
 *  answer is on its way a placeholder holds the button's place; a film
 *  with no trailer, or one that could not be asked about, says so. */
export function TrailerRow(props: RowProps) {
  const { where, trailer } = props;
  if (trailer === undefined) {
    return <span className={`cd-trailer-skel cd-trailer-${where}`} aria-hidden="true" />;
  }
  if (trailer === null) {
    return (
      <span className={`cd-trailer-none cd-trailer-${where}`}>
        <span className="cd-trailer-sq" aria-hidden="true">
          <PlayIcon />
        </span>
        No trailer available
      </span>
    );
  }
  return <Watch {...props} youtubeKey={trailer} />;
}

/** Whether a point is inside a box, edges included. */
function inside(at: { x: number; y: number } | null, r: DOMRect | undefined): boolean {
  return !!at && !!r && at.x >= r.left && at.x <= r.right && at.y >= r.top && at.y <= r.bottom;
}

function Watch({ where, id, title, youtubeKey, player, onPlay }: RowProps & { youtubeKey: string }) {
  const cell = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const rest = useRef(0);
  // Where the pointer last was over the row, if it is still over it.
  const pointer = useRef<{ x: number; y: number } | null>(null);
  // Set while the pointer rests where Watch trailer came back under it.
  const quiet = useRef(false);
  const soundBtn = useRef<HTMLButtonElement>(null);
  const focusIn = useRef(false);
  const refocus = useRef(false);
  // Set when the player starts from a Watch trailer that has the focus.
  const handoff = useRef(false);
  const wasOpen = useRef(false);
  // The latest onPlay, for a rest that comes due renders after it began:
  // the one from when the pointer arrived would not know that the panel
  // or the preview has started to go since.
  const playNow = useRef(onPlay);
  playNow.current = onPlay;

  const play = isFor(player.play, where, id) ? player.play : null;
  const on = play != null;
  const open = !!play?.open;

  const begin = (muted: boolean) => {
    window.clearTimeout(rest.current);
    handoff.current = document.activeElement === button.current;
    playNow.current(muted, videoWidth(where, cell.current?.getBoundingClientRect().width ?? 0));
  };

  useEffect(() => () => window.clearTimeout(rest.current), []);

  // Its closed state is worked out while it is on screen, so the flip to
  // open has somewhere to run from however busy the page is.
  useLayoutEffect(() => {
    if (on) void cell.current?.offsetHeight;
  }, [on]);

  // The video follows the width of the box it bleeds across, should that
  // change while it plays: the window turned, or a scrollbar came in as
  // the well pushed the panel's contents past its height. The row is the
  // box's width less its padding, and only its width ever changes.
  const { resize } = player;
  useEffect(() => {
    const el = cell.current;
    if (!on || !el || typeof ResizeObserver === 'undefined') return;
    const watch = new ResizeObserver(() => resize(where, id, videoWidth(where, el.getBoundingClientRect().width)));
    watch.observe(el);
    return () => watch.disconnect();
  }, [on, resize, where, id]);

  useLayoutEffect(() => {
    if (open && !wasOpen.current) {
      // Watch trailer is disabled while its player is open, and a browser
      // drops the focus from a disabled control onto the page itself,
      // where a screen reader loses its place. So a reader on it is moved
      // to the controls that replace it. They stop being inert in this
      // same commit, and the focus lands before the browser can take it
      // away.
      if (handoff.current) {
        soundBtn.current?.focus({ preventScroll: true });
        focusIn.current = true;
      }
      handoff.current = false;
    }
    if (wasOpen.current && !open) {
      // Closed from its own controls, by the ✕ or by Escape while on one
      // of them: the focus goes back to Watch trailer rather than staying
      // on a control that is fading away.
      if (refocus.current || focusIn.current) button.current?.focus({ preventScroll: true });
      refocus.current = false;
      focusIn.current = false;
      // A pointer resting where Watch trailer comes back does not start
      // it again until it has moved off it.
      quiet.current = inside(pointer.current, button.current?.getBoundingClientRect());
    }
    wasOpen.current = open;
  }, [open]);

  const small = where === 'preview';
  return (
    <div
      className={`cd-trailer cd-trailer-${where}${open ? ' cd-trailer-open' : ''}`}
      style={play ? (playerVars(play) as CSSProperties) : undefined}
    >
      <div
        className="cd-trailer-cell"
        ref={cell}
        onPointerMove={(e) => {
          pointer.current = { x: e.clientX, y: e.clientY };
        }}
        onPointerLeave={() => {
          pointer.current = null;
        }}
      >
        <button
          ref={button}
          type="button"
          className="cd-trailer-btn"
          disabled={open}
          aria-label={`Play the trailer for ${title}`}
          onClick={() => begin(false)}
          onMouseEnter={() => {
            // Muted: a browser plays sound only after a tap or a click.
            if (quiet.current || !canRest() || stillNow()) return;
            window.clearTimeout(rest.current);
            rest.current = window.setTimeout(() => begin(true), TRAILER_REST_MS);
          }}
          onMouseLeave={() => {
            window.clearTimeout(rest.current);
            quiet.current = false;
          }}
        >
          <span className="cd-trailer-sq" aria-hidden="true">
            <PlayIcon />
          </span>
          <span>Watch trailer</span>
          <span className="cd-trailer-yt">YouTube</span>
        </button>
        {play && (
          // In the same cell as the button, over it. Inert until it is
          // open and again as it closes, so a keyboard never lands on a
          // control that cannot be seen.
          <div
            className="cd-trailer-controls"
            inert={!open || undefined}
            onFocus={() => {
              focusIn.current = true;
            }}
            onBlur={(e) => {
              if (!e.currentTarget.contains(e.relatedTarget as Node | null)) focusIn.current = false;
            }}
          >
            <button
              ref={soundBtn}
              type="button"
              className="cd-trailer-sound"
              aria-pressed={!play.muted}
              aria-label={play.muted ? 'Turn on sound' : 'Turn off sound'}
              onClick={player.sound}
            >
              <span className="cd-trailer-sq" aria-hidden="true">
                {play.muted ? <MutedIcon size={small ? 15 : 16} /> : <SoundIcon size={small ? 15 : 16} />}
              </span>
              <span>{play.muted ? 'Turn on sound' : 'Sound on'}</span>
            </button>
            <span className="cd-trailer-spacer" />
            <a
              className="cd-trailer-out"
              href={watchUrl(youtubeKey)}
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Watch on YouTube"
            >
              YouTube
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M7 17 17 7M9 7h8v8" />
              </svg>
            </a>
            <button
              type="button"
              className="cd-trailer-close"
              aria-label="Close the trailer"
              onClick={() => {
                refocus.current = true;
                player.stop(where);
              }}
            >
              <svg width={small ? 14 : 15} height={small ? 14 : 15} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                <path d="M6 6l12 12M18 6 6 18" />
              </svg>
            </button>
          </div>
        )}
      </div>
      {play && (
        // Grows open under the row and pushes what is below it down. The
        // video inside is drawn at its full size from the start and
        // grows out of the button's corner, so the frame never lays
        // itself out again. Nothing is drawn over it: YouTube allows no
        // overlays on its player.
        <div className="cd-trailer-well">
          <div className="cd-trailer-video">
            <TrailerFrame youtubeKey={youtubeKey} muted={play.muted} title={title} frame={player.frame} />
          </div>
        </div>
      )}
    </div>
  );
}

/** The YouTube player. Mounted only once the player opens, so the page
 *  asks nothing of YouTube before that. */
function TrailerFrame({
  youtubeKey,
  muted,
  title,
  frame,
}: {
  youtubeKey: string;
  muted: boolean;
  title: string;
  frame: RefObject<HTMLIFrameElement | null>;
}) {
  // Built once, from how the player started: turning the sound on or off
  // is a command to the player, not a new address that would load the
  // whole thing again.
  const [src] = useState(() =>
    embedSrc(youtubeKey, muted, typeof location === 'undefined' ? '' : location.origin),
  );
  return (
    <iframe
      ref={frame}
      className="cd-trailer-frame"
      src={src}
      title={`${title} trailer`}
      allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
      allowFullScreen
      // Asks the player to say when it is ready: until it does, the
      // commands it is sent are held back (see playerLink).
      onLoad={(e) => e.currentTarget.contentWindow?.postMessage(YT_LISTENING, '*')}
    />
  );
}

function PlayIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M8 5.14v13.72a1 1 0 0 0 1.52.85l10.8-6.86a1 1 0 0 0 0-1.7L9.52 4.29A1 1 0 0 0 8 5.14z" />
    </svg>
  );
}

/** A speaker, crossed out. */
function MutedIcon({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M11 5 6 9H3v6h3l5 4V5z" fill="currentColor" />
      <path d="m22 9-6 6M16 9l6 6" />
    </svg>
  );
}

/** A speaker, with its sound coming out. */
function SoundIcon({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M11 5 6 9H3v6h3l5 4V5z" fill="currentColor" />
      <path d="M15.5 8.5a5 5 0 0 1 0 7M18.5 5.5a9 9 0 0 1 0 13" />
    </svg>
  );
}
