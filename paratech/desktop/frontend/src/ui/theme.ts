// Theme choice of the UI: "system" follows Windows, "light"/"dark" force one. Kept in the webview's localStorage
// (ADR-0012); tokens.css reacts to data-theme on <html>.

export type ThemeChoice = "system" | "light" | "dark";

export const themeKey = "ui.theme";
export const themeChoices: readonly ThemeChoice[] = ["system", "light", "dark"];

interface Store {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

interface Root {
  setAttribute(name: string, value: string): void;
  removeAttribute(name: string): void;
}

/** A stored value as a choice; anything unknown is "system". */
export function parseTheme(value: unknown): ThemeChoice {
  return value === "light" || value === "dark" ? value : "system";
}

/** The saved choice; "system" when nothing is saved or the storage is not available. */
export function readTheme(store: Store | null): ThemeChoice {
  try {
    return parseTheme(store?.getItem(themeKey));
  } catch {
    return "system";
  }
}

/** Saves the choice; a storage that refuses it (quota, policy) only loses the choice. */
export function writeTheme(store: Store | null, choice: ThemeChoice): void {
  try {
    store?.setItem(themeKey, choice);
  } catch {
    // Nothing to report: the theme still applies until the window closes.
  }
}

/** Sets data-theme for a forced theme and removes it for "system". */
export function applyTheme(root: Root, choice: ThemeChoice): void {
  if (choice === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.setAttribute("data-theme", choice);
  }
}

function browserStore(): Store | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

/** Applies the saved choice to <html>; called before the first render, so the page never flashes the other theme. */
export function initTheme(): ThemeChoice {
  const choice = readTheme(browserStore());
  applyTheme(document.documentElement, choice);
  return choice;
}

/**
 * The choice a storage event of another window carries, or null when the event is about something else. A cleared
 * storage (key null) means the system theme.
 */
export function themeFromStorageEvent(e: { key: string | null; newValue: string | null }): ThemeChoice | null {
  if (e.key === null) {
    return "system";
  }
  return e.key === themeKey ? parseTheme(e.newValue) : null;
}

/** Applies a choice made in another window (Settings) to this one; returns the function that stops listening. */
export function followTheme(): () => void {
  const onStorage = (e: StorageEvent) => {
    const choice = themeFromStorageEvent(e);
    if (choice !== null) {
      applyTheme(document.documentElement, choice);
    }
  };
  window.addEventListener("storage", onStorage);
  return () => window.removeEventListener("storage", onStorage);
}

/** The saved choice, for the theme switch in Settings. */
export function currentTheme(): ThemeChoice {
  return readTheme(browserStore());
}

/** Applies and saves a new choice (the theme switch in Settings). */
export function setTheme(choice: ThemeChoice): void {
  applyTheme(document.documentElement, choice);
  writeTheme(browserStore(), choice);
}
