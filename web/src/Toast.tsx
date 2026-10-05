import { useCallback, useEffect, useRef, useState } from 'react';

/** What a toast says, and what it offers to undo. */
export interface ToastSpec {
  text: string;
  /** Something is still happening: no timeout, and an accent dot instead
   *  of an action. It stays until it is replaced or hidden. */
  busy?: boolean;
  action?: { label: string; run: () => void };
}

/** How long a toast that is not busy stays up. */
export const TOAST_MS = 3600;

/** How long the exit takes, after which it leaves the tree. */
export const LEAVE_MS = 260;

interface Toaster {
  spec: ToastSpec | null;
  visible: boolean;
  show: (spec: ToastSpec) => void;
  hide: () => void;
}

/** One toast at a time. A second one arriving while the first is up
 *  swaps the words in place — it does not leave and come back, which
 *  reads as two interruptions instead of one thing changing its mind.
 *
 *  Timers are `setTimeout` throughout: a toast raised while the tab is
 *  hidden still has to leave on time, and a frame may never come. */
export function useToast(): Toaster {
  const [spec, setSpec] = useState<ToastSpec | null>(null);
  const [visible, setVisible] = useState(false);
  const away = useRef(0);
  const gone = useRef(0);

  useEffect(
    () => () => {
      window.clearTimeout(away.current);
      window.clearTimeout(gone.current);
    },
    [],
  );

  const hide = useCallback(() => {
    window.clearTimeout(away.current);
    window.clearTimeout(gone.current);
    setVisible(false);
    gone.current = window.setTimeout(() => setSpec(null), LEAVE_MS);
  }, []);

  const show = useCallback((next: ToastSpec) => {
    window.clearTimeout(away.current);
    window.clearTimeout(gone.current);
    setSpec(next);
    setVisible(true);
    if (!next.busy) {
      away.current = window.setTimeout(() => {
        setVisible(false);
        gone.current = window.setTimeout(() => setSpec(null), LEAVE_MS);
      }, TOAST_MS);
    }
  }, []);

  return { spec, visible, show, hide };
}

/** The toast's classes. `onMap` lets the stylesheet raise it clear of
 *  the floating buttons, which it does on phones, landscape phones
 *  (including any window under 500 tall) and tablets (.cd-toast-map);
 *  on desktop it stays low, unless the quick "Hide empty years" switch
 *  has widened View's pill under it ([data-quick]). On the opening
 *  screen there are no buttons, so it sits low at every width. */
export function toastClass(visible: boolean, onMap: boolean): string {
  return `cd-toast${visible ? ' cd-toast-in' : ''}${onMap ? ' cd-toast-map' : ''}`;
}

/** The toast itself. It is `role="status"`, so a reader hears the change
 *  without being interrupted. */
export function Toast({
  spec,
  visible,
  onMap = false,
}: {
  spec: ToastSpec | null;
  visible: boolean;
  onMap?: boolean;
}) {
  if (!spec) return null;
  return (
    <div className={toastClass(visible, onMap)} role="status" aria-live="polite">
      {spec.busy && <span className="cd-toast-dot" aria-hidden="true" />}
      <span className="cd-toast-text">{spec.text}</span>
      {spec.action && !spec.busy && (
        <button type="button" className="cd-toast-action" onClick={spec.action.run}>
          {spec.action.label}
        </button>
      )}
    </div>
  );
}
