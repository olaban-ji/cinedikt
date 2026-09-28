import { describe, expect, it, vi } from 'vitest';
import { askForMissingPhotos, headerClass } from './GridApp';
import type { fetchPeoplePhotos } from './api';
import type { GridPerson } from './grid';

const person = (id: string, name: string, photo?: string): GridPerson => ({
  id,
  name,
  role: 'cast',
  order: 0,
  ...(photo ? { photo } : {}),
});

/** A stand-in for fetchPeoplePhotos that records what it is asked. */
function fakeFetcher() {
  return vi.fn<typeof fetchPeoplePhotos>(async () => ({}));
}

// A map's payload carries the photo of everybody the people job has
// answered for. The rest are asked for, once per map.
describe('askForMissingPhotos', () => {
  it('asks for nothing when everyone came with a photo', () => {
    const fetcher = fakeFetcher();
    const people = [
      person('nm0000206', 'Keanu Reeves', 'https://image.tmdb.org/t/p/w185/keanu.jpg'),
      person('nm0000401', 'Laurence Fishburne', 'https://image.tmdb.org/t/p/w185/laurence.jpg'),
    ];
    askForMissingPhotos({ people }, new AbortController().signal, () => {}, fetcher);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('asks once, for exactly the people missing one, handing on the answers as they come', () => {
    const fetcher = fakeFetcher();
    const signal = new AbortController().signal;
    const onSome = () => {};
    const people = [
      person('nm0000206', 'Keanu Reeves', 'https://image.tmdb.org/t/p/w185/keanu.jpg'),
      person('nm0080930', 'Edward Biby'),
      person('nm0000401', 'Laurence Fishburne', 'https://image.tmdb.org/t/p/w185/laurence.jpg'),
      person('nm0088471', 'B.F. Blinn'),
    ];
    askForMissingPhotos({ people }, signal, onSome, fetcher);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledWith(['nm0080930', 'nm0088471'], signal, onSome);
  });
});

// A trailer playing in the hover preview blurs the header behind it
// (.cd-trailer-focus); the search field, and the results it opens, stay
// over the blur while the field has the focus.
describe('headerClass', () => {
  const flags = { map: true, over: false, away: false, searching: false };

  it('lifts the header while its search field has the focus, and only then', () => {
    expect(headerClass({ ...flags, searching: true }).split(' ')).toContain('cd-header-searching');
    expect(headerClass(flags).split(' ')).not.toContain('cd-header-searching');
  });

  it('keeps the classes it had', () => {
    expect(headerClass({ map: false, over: false, away: false, searching: false })).toBe('cd-header');
    expect(headerClass({ map: true, over: true, away: true, searching: false })).toBe(
      'cd-header cd-header-map cd-header-over cd-header-away',
    );
    expect(headerClass({ map: true, over: true, away: false, searching: true })).toBe(
      'cd-header cd-header-map cd-header-over cd-header-searching',
    );
  });
});
