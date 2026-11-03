// Where the keyboard focus goes when a panel opened by a button opens or closes (TASK-302). Pure, so it is tested without a DOM.

/** The element that has the focus: null for none; isBody when it is the page body (the focus is lost). */
export interface ActiveElement {
  id: string;
  isBody: boolean;
}

export type FocusMove = "panel" | "opener" | "none";

/**
 * Opening moves the focus into the panel. Closing gives it back to the opener only when it is lost (the focused control went
 * away with the panel) or still on the opener: a student who has moved on to another control keeps the place they chose.
 * Anything else (a re-render without a change) moves nothing.
 */
export function focusMove(wasOpen: boolean, open: boolean, active: ActiveElement | null, openerId: string): FocusMove {
  if (open === wasOpen) {
    return "none";
  }
  if (open) {
    return "panel";
  }
  return active === null || active.isBody || active.id === openerId ? "opener" : "none";
}

/** The controls a panel can be entered by; the first enabled one takes the focus. */
export const firstControlSelector = "button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled)";
