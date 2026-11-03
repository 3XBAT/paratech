// In-window history (TASK-201): «Назад/Вперёд» like in a browser. An entry is a route: the page and what is open over
// it (lesson, assignment). Pure functions; the hook that wires them to the window is in useNavHistory.ts.

import { closeLesson, type Route } from "./nav.js";

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

/** The same place; the tab of a lesson and the tab of a subject are not part of it (switching tabs is not a step of the history). */
export function sameRoute(a: Route, b: Route): boolean {
  return (
    a.page === b.page &&
    a.assignment === b.assignment &&
    a.subject === b.subject &&
    a.lesson === b.lesson
  );
}

/**
 * A new place: forward entries are cut off; the same place as the current one adds nothing. Another tab of the open
 * lesson (or of the subject page) replaces the tab of the current entry, so «Назад» leaves the lesson whatever tabs were
 * visited and «Вперёд» returns to the tab last used; «Назад» from a lesson opened on a subject's tab lands on that tab.
 */
export function pushRoute(h: NavHistory, r: Route): NavHistory {
  const now = currentRoute(h);
  if (sameRoute(now, r)) {
    if (now.tab === r.tab && now.subjectTab === r.subjectTab) {
      return h;
    }
    const entries = h.entries.slice();
    entries[h.index] = r;
    return { entries, index: h.index };
  }
  const kept = [...h.entries.slice(0, h.index + 1), r];
  const entries = kept.length > maxDepth ? kept.slice(kept.length - maxDepth) : kept;
  return { entries, index: entries.length - 1 };
}

/**
 * The entries of a lesson that no longer exists are taken out of the history, so Alt+← and Alt+→ never land on it. What
 * is left stays in order; neighbours that became the same place are one entry; the position is the last kept entry at or
 * before the current one. Nothing left: the current place without the lesson.
 */
export function dropLesson(h: NavHistory, lessonKey: string): NavHistory {
  if (!h.entries.some((r) => r.lesson === lessonKey)) {
    return h;
  }
  const entries: Route[] = [];
  let index = 0;
  h.entries.forEach((r, i) => {
    if (r.lesson !== lessonKey && !(entries.length > 0 && sameRoute(entries[entries.length - 1], r))) {
      entries.push(r);
    }
    if (i === h.index) {
      index = Math.max(entries.length - 1, 0);
    }
  });
  return entries.length === 0 ? newHistory(closeLesson(currentRoute(h))) : { entries, index };
}

/**
 * The recording of the open lesson was deleted: the window goes back to the section the lesson was opened from, and the
 * lesson leaves the history instead of becoming a step in it (otherwise Alt+← would return to a lesson that is gone).
 * When the window has already left the lesson, only its entries are dropped.
 */
export function afterLessonDeleted(h: NavHistory, lessonKey: string): { history: NavHistory; route: Route } {
  const cur = currentRoute(h);
  const route = cur.lesson === lessonKey ? closeLesson(cur) : cur;
  return { history: pushRoute(dropLesson(h, lessonKey), route), route };
}

export const canGoBack =(h: NavHistory): boolean => h.index > 0;
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

/**
 * True when going from one route to another swaps the page or the assignment, so the page must open afresh. A lesson
 * (and its tab) is not such a case: its overlay has its own key (the lesson), and the page under it stays as left.
 */
export function needsRemount(to: Route, from: Route): boolean {
  return to.assignment !== "" && (to.page !== from.page || to.assignment !== from.assignment);
}
