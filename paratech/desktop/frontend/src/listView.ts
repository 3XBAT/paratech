// Pure logic of CollapsibleList: how many rows of a long list are shown. The state lives in the page, so a refreshed
// list (a new preview, a poll) keeps it.

/** Rows a list shows before «Показать ещё». */
export const defaultLimit = 8;

export interface ListState {
  /** The group is folded to its header. */
  collapsed: boolean;
  /** All rows are shown, not only the first `limit`. */
  showAll: boolean;
}

export const initialListState: ListState = { collapsed: false, showAll: false };

export interface ListWindow {
  /** Rows to render. */
  shown: number;
  /** Rows not rendered. */
  hidden: number;
  /** Offer «Показать ещё N». */
  more: boolean;
  /** Offer «Свернуть» back to the first `limit` rows. */
  less: boolean;
}

export function listWindow(total: number, state: ListState, limit: number = defaultLimit): ListWindow {
  const n = Math.max(0, Math.floor(total));
  const max = Math.max(1, Math.floor(limit));
  if (state.collapsed) {
    return { shown: 0, hidden: n, more: false, less: false };
  }
  const shown = state.showAll ? n : Math.min(n, max);
  return { shown, hidden: n - shown, more: shown < n, less: state.showAll && n > max };
}

/** The rows to render: a slice, so hidden rows never reach the DOM. */
export function visibleRows<T>(items: readonly T[], state: ListState, limit: number = defaultLimit): readonly T[] {
  return items.slice(0, listWindow(items.length, state, limit).shown);
}

export const listTexts = {
  more: (hidden: number) => `Показать ещё ${hidden}`,
  allErrors: "Показать все ошибки",
  less: "Свернуть",
  collapseAll: "Свернуть всё",
  expandAll: "Развернуть всё",
} as const;

/** States of several lists of a page, by list key. */
export type ListStates = Readonly<Record<string, ListState>>;

export function stateOf(states: ListStates, key: string): ListState {
  return Object.hasOwn(states, key) ? states[key] : initialListState;
}

export function withState(states: ListStates, key: string, state: ListState): ListStates {
  return { ...states, [key]: state };
}

/** Whether every list of `keys` is folded; false for no lists. */
export function allCollapsed(states: ListStates, keys: readonly string[]): boolean {
  return keys.length > 0 && keys.every((k) => stateOf(states, k).collapsed);
}

/** Folds or unfolds every list of `keys`, keeping their «show all» choice. */
export function setCollapsed(states: ListStates, keys: readonly string[], collapsed: boolean): ListStates {
  const next: Record<string, ListState> = { ...states };
  for (const k of keys) {
    next[k] = { ...stateOf(states, k), collapsed };
  }
  return next;
}
