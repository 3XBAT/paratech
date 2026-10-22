// Width of the agent panel: pure rules for the drag handle on the left edge of the panel (TASK-137). The choice is kept
// in the webview's localStorage like the accent and the theme (ADR-0012); nothing here touches the DOM.

export const defaultWidth = 340;
export const minWidth = 260;
export const maxWidthLimit = 640;
/** A drag that leaves the panel narrower than this closes it (and a closed panel opens only past it). */
export const closeBelow = 180;
/** The rail (64 px) plus the narrowest main screen that still reads well. */
export const reservedWidth = 64 + 420;
export const keyStep = 16;

export const widthKey = "ui.agentWidth";
export const openKey = "ui.agentOpen";

export interface PanelPref {
  open: boolean;
  /** The width when open; kept while the panel is closed, so it opens at the same width again. */
  width: number;
}

interface Store {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/** The widest panel the window allows; never below the minimum. */
export function maxWidth(viewport: number): number {
  return Math.max(minWidth, Math.min(maxWidthLimit, viewport - reservedWidth));
}

export function clampWidth(width: number, viewport: number): number {
  if (!Number.isFinite(width)) {
    return Math.min(defaultWidth, maxWidth(viewport));
  }
  return Math.round(Math.min(maxWidth(viewport), Math.max(minWidth, width)));
}

/** The pointer at clientX: the panel runs from there to the right window edge. */
export function dragPref(pref: PanelPref, clientX: number, viewport: number): PanelPref {
  const raw = viewport - clientX;
  if (raw < closeBelow) {
    return { open: false, width: pref.width };
  }
  return { open: true, width: clampWidth(raw, viewport) };
}

/** Arrow keys move the left edge (Left widens, Right narrows); Home/End jump to the limits. Null for other keys. */
export function keyPref(pref: PanelPref, key: string, viewport: number): PanelPref | null {
  const current = pref.open ? pref.width : 0;
  switch (key) {
    case "ArrowLeft":
      return { open: true, width: clampWidth(pref.open ? current + keyStep : minWidth, viewport) };
    case "ArrowRight":
      if (!pref.open) {
        return pref;
      }
      return current - keyStep < minWidth ? { open: false, width: pref.width } : { open: true, width: clampWidth(current - keyStep, viewport) };
    case "Home":
      return { open: true, width: minWidth };
    case "End":
      return { open: true, width: maxWidth(viewport) };
    case "Escape":
      return pref.open ? { open: false, width: pref.width } : pref;
    default:
      return null;
  }
}

export function togglePref(pref: PanelPref): PanelPref {
  return { open: !pref.open, width: pref.width };
}

/** The same preference narrowed to what the window allows now. */
export function fitPref(pref: PanelPref, viewport: number): PanelPref {
  const width = clampWidth(pref.width, viewport);
  return width === pref.width ? pref : { open: pref.open, width };
}

/** Open at the default width unless the stored values say otherwise; anything unreadable is the default. */
export function readPref(store: Store | null, viewport: number): PanelPref {
  let width = defaultWidth;
  let open = true;
  try {
    const w = store?.getItem(widthKey);
    if (w != null && /^\d{1,4}$/.test(w)) {
      width = Number(w);
    }
    open = store?.getItem(openKey) !== "0";
  } catch {
    // A storage that refuses to answer gives the default.
  }
  return { open, width: clampWidth(width, viewport) };
}

/** A storage that refuses the value only loses the choice. */
export function writePref(store: Store | null, pref: PanelPref): void {
  try {
    store?.setItem(widthKey, String(pref.width));
    store?.setItem(openKey, pref.open ? "1" : "0");
  } catch {
    // The width still applies until the window closes.
  }
}
