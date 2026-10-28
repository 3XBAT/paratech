import { type RefObject, useEffect, useRef, useState } from "react";
import { focusTargetAfter } from "../assignmentDoneView.js";
import type { DoneToast } from "../doneRunner.js";

/** Focuses the check of the row with this id inside root; false when there is none. */
export function focusDoneCheck(root: HTMLElement | null, id: string): boolean {
  if (root === null || id === "") {
    return false;
  }
  for (const el of root.querySelectorAll<HTMLElement>("[data-done-id]")) {
    if (el.dataset.doneId === id) {
      el.focus();
      return true;
    }
  }
  return false;
}

/**
 * The props of the page's Toast: «Отменить» (also by Ctrl+Z) and the close from inside bring the focus back to the
 * check of the affected row, or to the main region when the row is not there. The timer closing it moves nothing.
 */
export function doneToastProps(
  run: { toast: DoneToast | null; undo: () => Promise<boolean>; closeToast: () => void },
  focus: { restore: (id: string) => void },
) {
  const affected = () => run.toast?.undo.prior[0]?.id ?? "";
  return {
    toast: run.toast,
    onAction: () => {
      const id = affected();
      void run.undo().then((started) => {
        if (started) {
          focus.restore(id);
        }
      });
    },
    onClose: run.closeToast,
    onDismiss: () => focus.restore(affected()),
  };
}

/**
 * Keeps the keyboard focus on the screen when a row leaves a list or the toast closes from inside: the check of the
 * next row, else the previous one, else the page's main region (root, tabIndex -1). The id is chosen when the rows are
 * about to change and the focus is moved once they are drawn.
 */
export function useDoneFocus(root: RefObject<HTMLElement | null>) {
  const pending = useRef<string | null>(null);
  const [, redraw] = useState(0);

  useEffect(() => {
    const id = pending.current;
    if (id === null) {
      return;
    }
    pending.current = null;
    if (!focusDoneCheck(root.current, id)) {
      root.current?.focus({ preventScroll: true });
    }
  });

  return {
    /** Rows are about to leave (call before the state changes): only when the focus is inside the page now. */
    rowsLeaving(leaving: readonly string[]) {
      const el = root.current;
      if (el === null || leaving.length === 0 || !el.contains(document.activeElement)) {
        return;
      }
      const order = Array.from(el.querySelectorAll<HTMLElement>("[data-done-id]"), (x) => x.dataset.doneId ?? "");
      const target = focusTargetAfter(order, leaving);
      if (target !== null) {
        pending.current = target;
      }
    },
    /** After the toast closed from inside: the check of this row if it is there, else the main region. */
    restore(id: string) {
      pending.current = id;
      redraw((n) => n + 1);
    },
  };
}
