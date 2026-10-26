// In-window history (TASK-201): «Назад/Вперёд» like in a browser. An entry is a route: the page and what is open over
// it (transcript, summary, assignment). Pure functions; the hook that wires them to the window is in useNavHistory.ts.

import type { Route } from "./nav.js";

/** The most entries kept; the oldest one is dropped first. */
export const maxDepth = 50;

export interface NavHistory {
  entries: readonly Route[];
  index: number;
}

export function newHistory(first: Route): NavHistory {
  return { entries: [first], index: 0 };
}

export function currentRoute(h: NavHistory): Route {
  return h.entries[h.index];
}

export function sameRoute(a: Route, b: Route): boolean {
  return (
    a.page === b.page &&
    a.transcript === b.transcript &&
    a.summary === b.summary &&
    a.assignment === b.assignment &&
    a.subject === b.subject
  );
}

/** A new place: forward entries are cut off; the same place as the current one adds nothing. */
export function pushRoute(h: NavHistory, r: Route): NavHistory {
  if (sameRoute(currentRoute(h), r)) {
    return h;
  }
  const kept = [...h.entries.slice(0, h.index + 1), r];
  const entries = kept.length > maxDepth ? kept.slice(kept.length - maxDepth) : kept;
  return { entries, index: entries.length - 1 };
}

export const canGoBack = (h: NavHistory): boolean => h.index > 0;
export const canGoForward = (h: NavHistory): boolean => h.index < h.entries.length - 1;

export function goBack(h: NavHistory): NavHistory {
  return canGoBack(h) ? { entries: h.entries, index: h.index - 1 } : h;
}

export function goForward(h: NavHistory): NavHistory {
  return canGoForward(h) ? { entries: h.entries, index: h.index + 1 } : h;
}

export type Direction = "back" | "forward";

/** The part of a keyboard event the decision needs (a DOM KeyboardEvent fits). */
export interface KeyLike {
  key: string;
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  target: unknown;
}

/** True where Alt+← may belong to text editing: an input, a text area, a select or editable content. */
export function isEditableTarget(target: unknown): boolean {
  const t = target as { tagName?: string; isContentEditable?: boolean } | null;
  if (t === null || typeof t !== "object") {
    return false;
  }
  const tag = (t.tagName ?? "").toUpperCase();
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || t.isContentEditable === true;
}

/** Alt+← and Alt+→ without other modifiers, outside editable elements. */
export function keyDirection(e: KeyLike): Direction | null {
  if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || isEditableTarget(e.target)) {
    return null;
  }
  if (e.key === "ArrowLeft") {
    return "back";
  }
  return e.key === "ArrowRight" ? "forward" : null;
}

/** The side buttons of a mouse: 3 is «back», 4 is «forward». */
export function mouseDirection(button: number): Direction | null {
  if (button === 3) {
    return "back";
  }
  return button === 4 ? "forward" : null;
}

/** What a window event asks for: cancel it, and move to `next` (null: stay). */
export interface NavAction {
  prevent: boolean;
  next: NavHistory | null;
}

const ignored: NavAction = { prevent: false, next: null };

function step(h: NavHistory, d: Direction): NavHistory | null {
  const next = d === "back" ? goBack(h) : goForward(h);
  return next === h ? null : next;
}

/** A key press that was already handled (tabs, the resize handle) or is auto-repeated is left alone. */
export function handleKey(e: KeyLike & { defaultPrevented?: boolean; repeat?: boolean }, h: NavHistory): NavAction {
  if (e.defaultPrevented === true || e.repeat === true) {
    return ignored;
  }
  const d = keyDirection(e);
  return d === null ? ignored : { prevent: true, next: step(h, d) };
}

/** The side buttons are cancelled on press and click; the move happens on mouseup only. */
export function handleMouse(e: { button: number; type: string }, h: NavHistory): NavAction {
  const d = mouseDirection(e.button);
  if (d === null) {
    return ignored;
  }
  return { prevent: true, next: e.type === "mouseup" ? step(h, d) : null };
}

/** True when going from one route to another swaps the page or the assignment, so the page must open afresh. */
export function needsRemount(to: Route, from: Route): boolean {
  return to.assignment !== "" && (to.page !== from.page || to.assignment !== from.assignment);
}
