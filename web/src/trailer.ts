// The trailer player's numbers and its state changes, and the strings it
// hands to YouTube. Kept apart from the components so each is written
// once and can be checked without a DOM. The player opens where Watch
// trailer sat, in the film panel and in the map's hover preview, and
// only one is open at a time.

/** Where a player can open. */
export type PlayWhere = 'preview' | 'panel';

/** The one player on the page, shared by the preview and the panel.
 *
 *  `open` turns on a moment after the player mounts, so its transitions
 *  have a starting point to run from. `closing` keeps it in the tree
 *  while its close plays. `W` × `VH` is the size the video is drawn at
 *  from the first frame, so the frame never lays itself out again as it
 *  grows. `synLines` is how many lines the preview's synopsis folds to
 *  make room for it (0 for none at all); null when nothing folds, which
 *  is always so in the panel. */
export interface Play {
  where: PlayWhere;
  id: string;
  muted: boolean;
  open: boolean;
  closing: boolean;
  W: number;
  VH: number;
  synLines: number | null;
}

/** Opening takes this long, on the glide curve. */
export const TRAILER_OPEN_MS = 520;
/** Closing is a little quicker and plays the same path backwards. */
export const TRAILER_CLOSE_MS = 400;
/** How long a closing player stays in the tree: its close, and a little
 *  over, so nothing is taken away mid-move. */
export const TRAILER_GONE_MS = 420;
/** Resting this long on Watch trailer starts it muted. */
export const TRAILER_REST_MS = 250;

/** What differs between the two places a player opens: how far in from
 *  the box's edges the row sits, which is where the video grows from and
 *  how far it bleeds, and the gap between the controls and the video.
 *  The panel's is its body's padding, the same at every screen class. */
export const PLAYER: Record<PlayWhere, { pad: number; gap: number }> = {
  preview: { pad: 16, gap: 6 },
  panel: { pad: 22, gap: 4 },
};

/** YouTube asks for an embedded player to be at least this tall. */
export const MIN_VIDEO_H = 200;

/** How wide the video is drawn: the row's own width and the padding on
 *  either side of it, so it bleeds to the box's edges. */
export function videoWidth(where: PlayWhere, rowWidth: number): number {
  return rowWidth + 2 * PLAYER[where].pad;
}

/** The video's height for its width: 16:9, and never under YouTube's
 *  floor. */
export function videoHeight(width: number): number {
  return Math.max(MIN_VIDEO_H, (width * 9) / 16);
}

/** How tall the clipping well under the row grows: the gap, then the
 *  video. Everything below it moves down by this much. */
export function wellHeight(where: PlayWhere, vh: number): number {
  return PLAYER[where].gap + vh;
}

/** Where the video grows from: the top-left corner of the button it
 *  replaces, which sits the padding in from the edge the video bleeds
 *  to, at the top of the video. */
export function videoOrigin(where: PlayWhere): string {
  return `${PLAYER[where].pad}px 0`;
}

/** The player's measurements, as the custom properties the stylesheet
 *  draws the well and the video from. */
export function playerVars(play: Play): Record<string, string> {
  return {
    '--trailer-w': `${play.W}px`,
    '--trailer-vh': `${play.VH}px`,
    '--trailer-gap': `${PLAYER[play.where].gap}px`,
    '--trailer-well': `${wellHeight(play.where, play.VH)}px`,
    '--trailer-origin': videoOrigin(play.where),
  };
}

/** Whether this is the player for this film in this place. */
export function isFor(play: Play | null, where: PlayWhere, id: string): play is Play {
  return play != null && play.where === where && play.id === id;
}

/** A player starting, from whatever was open before. Null when the one
 *  asked for is already open or opening, which leaves it as it is. One
 *  that is still closing is taken back: it keeps its frame, and opens
 *  again at once, since it never left. Anything else is replaced, so
 *  only one plays at a time. `still` opens it at once too, for a reader
 *  who has asked for nothing to move. */
export function startPlay(
  was: Play | null,
  where: PlayWhere,
  id: string,
  muted: boolean,
  W: number,
  still: boolean,
  synLines: number | null = null,
): Play | null {
  if (isFor(was, where, id) && !was.closing) return null;
  const VH = videoHeight(W);
  if (isFor(was, where, id)) return { ...was, muted, open: true, closing: false, W, VH, synLines };
  return { where, id, muted, open: still, closing: false, W, VH, synLines };
}

/** A player starting to close. Null when there is none, or it is already
 *  on its way. */
export function stopPlay(was: Play | null): Play | null {
  if (!was || was.closing) return null;
  return { ...was, open: false, closing: true };
}

/** What Escape closes while a layer holding a player is on top: the
 *  player first, if it is this layer's and still open, and the layer
 *  itself after that. */
export function escapeCloses(play: Play | null, where: PlayWhere, id: string): 'trailer' | 'layer' {
  return isFor(play, where, id) && !play.closing ? 'trailer' : 'layer';
}

/** The embedded player's address. The privacy-enhanced host sets no
 *  cookies until the video plays. `mute` is how it starts: a trailer
 *  started by resting on the button has no tap to allow sound. The API
 *  is on so the sound toggle and the close can talk to it, and `origin`
 *  says who is talking. */
export function embedSrc(key: string, muted: boolean, origin: string): string {
  const from = origin && origin !== 'null' ? `&origin=${encodeURIComponent(origin)}` : '';
  return (
    `https://www.youtube-nocookie.com/embed/${encodeURIComponent(key)}` +
    `?autoplay=1&mute=${muted ? 1 : 0}&playsinline=1&rel=0&iv_load_policy=3&enablejsapi=1${from}`
  );
}

/** The trailer on YouTube itself. */
export function watchUrl(key: string): string {
  return `https://www.youtube.com/watch?v=${encodeURIComponent(key)}`;
}

/** The commands the player is sent. */
export type YouTubeCommand = 'mute' | 'unMute' | 'playVideo' | 'pauseVideo';

/** One IFrame API command, as the message the player listens for. */
export function ytCommand(func: YouTubeCommand): string {
  return JSON.stringify({ event: 'command', func, args: [] });
}

/** Asks the player to start telling the page about itself, which is how
 *  the page hears that it is ready. Sent once the frame has loaded. */
export const YT_LISTENING = JSON.stringify({ event: 'listening', id: 1, channel: 'widget' });

/** Whether a message from the player says it is ready for commands. */
export function saysReady(data: unknown): boolean {
  if (typeof data !== 'string') return false;
  try {
    return (JSON.parse(data) as { event?: unknown } | null)?.event === 'onReady';
  } catch {
    return false;
  }
}

/** What a player that has just said it is ready is told, so that it
 *  matches what its controls already show: paused if it is closing,
 *  otherwise muted, or playing with its sound on. */
export function catchUp(play: Play | null): YouTubeCommand[] {
  if (!play) return [];
  if (play.closing) return ['pauseVideo'];
  return play.muted ? ['mute'] : ['unMute', 'playVideo'];
}

/** The page's line to the embedded player. */
export interface PlayerLink {
  /** A new frame is about to mount, which has to say it is ready again. */
  reset: () => void;
  /** Sends a command, if the player can hear it yet. */
  send: (func: YouTubeCommand) => void;
  /** A message from the player. The first that says it is ready brings
   *  the player into line with `play`, what is open as of now. */
  heard: (data: unknown, play: Play | null) => void;
}

/** A line to the player that holds its commands until it is ready.
 *  YouTube drops any command that arrives before its player has loaded,
 *  so a sound toggle pressed while it is still loading would otherwise
 *  change what the controls say and not what the video does. Nothing is
 *  queued: the controls' state is the one to keep, and it is sent whole
 *  the moment the player is ready. */
export function playerLink(post: (message: string) => void): PlayerLink {
  let ready = false;
  return {
    reset: () => {
      ready = false;
    },
    send: (func) => {
      if (ready) post(ytCommand(func));
    },
    heard: (data, play) => {
      if (ready || !saysReady(data)) return;
      ready = true;
      for (const func of catchUp(play)) post(ytCommand(func));
    },
  };
}
