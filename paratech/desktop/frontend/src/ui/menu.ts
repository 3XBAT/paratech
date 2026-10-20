// The «⋯» menu of a list row (models, recordings): when it is shown and how the keys move in it.

/**
 * Whether the «⋯» menu is shown: only while it is open and has items. A menu whose items went away closes, so it does
 * not come back open (and keeps no outside-click listener) when they return.
 */
export function menuShown(open: boolean, count: number): boolean {
  return open && count > 0;
}

/**
 * A key pressed in an open menu of count items, the focused one at index: the index to focus, "close" (Escape: close
 * and focus the «⋯» button), "leave" (Tab: close and let the focus move on) or null when the menu ignores the key.
 */
export function menuKey(key: string, index: number, count: number): number | "close" | "leave" | null {
  if (key === "Escape") {
    return "close";
  }
  if (key === "Tab") {
    return "leave";
  }
  if (count <= 0) {
    return null;
  }
  switch (key) {
    // index is -1 while the «⋯» button itself has the focus.
    case "ArrowDown":
      return index < 0 || index + 1 >= count ? 0 : index + 1;
    case "ArrowUp":
      return index <= 0 ? count - 1 : index - 1;
    case "Home":
      return 0;
    case "End":
      return count - 1;
    default:
      return null;
  }
}
