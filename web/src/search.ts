// The search field's rules that do not need a DOM: what it says when it
// is empty, and which keys reach for it from anywhere on the page.

/** What the field says when there is nothing else to say. */
export const SEARCH_INVITE = 'Search a movie';

/** What the empty field says.
 *
 *  While a map is on its way it names the film being fetched, so the
 *  reader can see their pick was taken. The app does not always know
 *  that title — Back and Forward, a reloaded link — and a field that
 *  names the wrong film is worse than one that names none, so then it
 *  goes back to the invitation. On a map it names the searched film;
 *  anywhere else it is the invitation. */
export function searchPlaceholder(
  loading: boolean,
  loadingTitle: string | null | undefined,
  mapTitle: string | null | undefined,
): string {
  if (loading) return loadingTitle || SEARCH_INVITE;
  return mapTitle || SEARCH_INVITE;
}

/** The parts of a key press the shortcut looks at. */
interface ShortcutKey {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
}

/** Whether a key press anywhere on the page should put the reader in the
 *  search field: ⌘K or Ctrl+K from anywhere, or a bare "/" when they are
 *  not typing into something else. A "/" typed into a field is the
 *  character "/", not a request to go somewhere. */
export function isSearchShortcut(e: ShortcutKey, typing: boolean): boolean {
  if (e.altKey) return false;
  if (e.key === 'k' && (e.metaKey || e.ctrlKey)) return true;
  return e.key === '/' && !e.metaKey && !e.ctrlKey && !typing;
}

/** Input types that take typed text. A checkbox or a button that happens
 *  to be an <input> does not. */
const TEXT_INPUTS = new Set(['', 'text', 'search', 'email', 'number', 'password', 'tel', 'url']);

/** Whether the element the keyboard is on takes typed text. */
export function isTyping(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName;
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag === 'INPUT') return TEXT_INPUTS.has((el.getAttribute('type') ?? '').toLowerCase());
  return (el as HTMLElement).isContentEditable === true;
}
