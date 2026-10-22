// The one accent colour of the UI (docs/design/concept-agent.html): five swatches, chosen in «Оформление». The choice is
// kept in the webview's localStorage like the theme (ADR-0012). The copper default is the palette of tokens.css; any
// other swatch sets data-accent and --acc on <html>, and accent.css derives the text and fill colours from it.

export interface Accent {
  id: string;
  name: string;
  /** The swatch colour (#RRGGBB). */
  hex: string;
}

export const accents: readonly Accent[] = [
  { id: "copper", name: "Медный", hex: "#C26A2E" },
  { id: "blue", name: "Синий", hex: "#3B78C2" },
  { id: "green", name: "Зелёный", hex: "#3E8A5A" },
  { id: "violet", name: "Фиолетовый", hex: "#8A63CF" },
  { id: "graphite", name: "Графит", hex: "#6A6D73" },
];

export const defaultAccent: Accent = accents[0];

export const accentKey = "ui.accent";

interface Store {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

interface Root {
  setAttribute(name: string, value: string): void;
  removeAttribute(name: string): void;
  style: { setProperty(name: string, value: string): void; removeProperty(name: string): string };
}

/** A stored value as an accent; anything unknown is the default. */
export function parseAccent(value: unknown): Accent {
  return accents.find((a) => a.id === value) ?? defaultAccent;
}

export function readAccent(store: Store | null): Accent {
  try {
    return parseAccent(store?.getItem(accentKey));
  } catch {
    return defaultAccent;
  }
}

/** A storage that refuses the value only loses the choice. */
export function writeAccent(store: Store | null, accent: Accent): void {
  try {
    store?.setItem(accentKey, accent.id);
  } catch {
    // The accent still applies until the window closes.
  }
}

/** The default accent leaves the palette of tokens.css alone; another one sets the variable accent.css reads. */
export function applyAccent(root: Root, accent: Accent): void {
  if (accent.id === defaultAccent.id) {
    root.removeAttribute("data-accent");
    root.style.removeProperty("--acc");
  } else {
    root.setAttribute("data-accent", accent.id);
    root.style.setProperty("--acc", accent.hex);
  }
}

function browserStore(): Store | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

/** Applies the saved accent to <html> before the first render. */
export function initAccent(): Accent {
  const accent = readAccent(browserStore());
  applyAccent(document.documentElement, accent);
  return accent;
}

export function currentAccent(): Accent {
  return readAccent(browserStore());
}

/** Applies and saves a new accent. */
export function setAccent(accent: Accent): void {
  applyAccent(document.documentElement, accent);
  writeAccent(browserStore(), accent);
}
