import type { GridPayload } from './grid';

/** Maps already asked for, so opening one again — Back, a card's own
 *  map after a hover prefetched it — does not wait on the network.
 *
 *  Keyed by movie alone. The payload is the same whatever the reader
 *  has chosen to draw: the unrated column is taken off the plot by the
 *  layout, so turning it off is a relayout rather than another fetch.
 *
 *  A fetch belongs to the cache, not to whoever asked first, so `load`
 *  takes no caller's abort signal. Everyone asking for the same movie
 *  joins one request; if the first of them could cancel it, the rest
 *  would be handed an abort they never asked for — which is exactly
 *  what StrictMode's second mount got, and why a reloaded map sat on
 *  the opening screen in development. A caller that has moved on
 *  ignores the answer instead, and the cache keeps it for Back.
 *
 *  A request that fails is forgotten before anyone hears about it, so
 *  asking again — Try again — is a new request rather than a place in
 *  the queue behind the one that already failed. */
interface GridCache {
  /** The map, if it has already arrived. */
  peek: (id: string) => GridPayload | undefined;
  /** The map, from the cache, from the request already in flight, or
   *  from a new one. */
  load: (id: string) => Promise<GridPayload>;
}

/** How many maps are kept. Past this the oldest is dropped. */
const GRID_CACHE_MAX = 8;

export function gridCache(
  fetchGrid: (id: string) => Promise<GridPayload>,
  max = GRID_CACHE_MAX,
): GridCache {
  const grids = new Map<string, { payload?: GridPayload; pending?: Promise<GridPayload> }>();
  return {
    peek: (id) => grids.get(id)?.payload,
    load: (id) => {
      const have = grids.get(id);
      if (have?.payload) return Promise.resolve(have.payload);
      if (have?.pending) return have.pending;
      const pending = fetchGrid(id).then(
        (p) => {
          if (grids.size > max) {
            const oldest = grids.keys().next().value;
            if (oldest) grids.delete(oldest);
          }
          grids.set(id, { payload: p });
          return p;
        },
        (e: unknown) => {
          if (grids.get(id)?.pending === pending) grids.delete(id);
          throw e;
        },
      );
      grids.set(id, { pending });
      return pending;
    },
  };
}
