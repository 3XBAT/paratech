// Recording prompt bindings: calls into the Go core (internal/input/wails/prompt.go). The core owns the prompt and
// decides what is offered ("canSnooze"); the popup only shows it.

import type { CallByName } from "./settings";

export type PromptKind = "lesson" | "adhoc";

/** The current recording prompt, as the Go binding returns it. */
export interface PromptView {
  id: string;
  kind: PromptKind;
  /** Imported, untrusted text: render as plain text only. Empty for an ad-hoc prompt. */
  title: string;
  /** "15:04" in the lesson's time zone; empty for an ad-hoc prompt. */
  start: string;
  end: string;
  /** OFFERED, STARTING, STARTED, SNOOZED, SKIPPED, EXPIRED or WITHDRAWN. */
  state: string;
  /** Why the last start failed: "", "unavailable", "in_progress", "recovering", "source_gone" or "failed". */
  problem: string;
  canSnooze: boolean;
}

/** An app the popup may record from. */
export interface SourceApp {
  /** Executable name, for example "chrome.exe": the value Start takes. */
  app: string;
  /** Window title: untrusted text, render as plain text only. May be empty. */
  title: string;
  playing: boolean;
  /** Muted in the Windows volume mixer: the recording would be silent. */
  muted: boolean;
  /** A browser records all its tabs. */
  browser: boolean;
  /** "" or a "data:image/png;base64," URL; use only as an <img> source. */
  icon: string;
}

/** What the source picker offers. App names are exe names; "" is the whole computer. */
export interface SourcesView {
  apps: SourceApp[];
  selected: string;
  /** The app recorded last time, even when it is not open now. */
  remembered: string;
  /** The apps could not be listed: only the whole computer is offered. */
  listError: boolean;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  current: "paratech/desktop/internal/input/wails.PromptService.Current",
  sources: "paratech/desktop/internal/input/wails.PromptService.Sources",
  start: "paratech/desktop/internal/input/wails.PromptService.Start",
  snooze: "paratech/desktop/internal/input/wails.PromptService.Snooze",
  skip: "paratech/desktop/internal/input/wails.PromptService.Skip",
} as const;

/** Wails event the core emits each time it shows the popup; the page then reads the current prompt. */
export const shownEvent = "recordingprompt:shown";

/** Messages of the errors the Go binding returns; the page may match them. */
export const errorMessages = {
  invalidId: "invalid id",
  gone: "the prompt is no longer current",
  refused: "the action is not possible now",
  actionFailed: "the action failed",
  invalidSource: "invalid source",
} as const;

/** The only icon form the page shows. */
export const iconPrefix = "data:image/png;base64,";

function str(r: Record<string, unknown>, key: string): string {
  const v = r[key];
  if (typeof v !== "string") {
    throw new Error(`prompt: ${key} is not a string`);
  }
  return v;
}

function bool(r: Record<string, unknown>, key: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`prompt: ${key} is not a boolean`);
  }
  return v;
}

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new Error(`prompt: ${what} is not an object`);
  }
  return raw as Record<string, unknown>;
}

// Base64 alphabet only, so the URL cannot carry anything but the image data.
const base64Data = /^[A-Za-z0-9+/]+={0,2}$/;

function parseSourceApp(raw: unknown): SourceApp {
  const r = record(raw, "source");
  const app = str(r, "app");
  if (app === "") {
    throw new Error("prompt: source app is empty");
  }
  const icon = str(r, "icon");
  if (icon !== "" && !(icon.startsWith(iconPrefix) && base64Data.test(icon.slice(iconPrefix.length)))) {
    throw new Error("prompt: source icon is not a PNG data URL");
  }
  return {
    app,
    title: str(r, "title"),
    playing: bool(r, "playing"),
    muted: bool(r, "muted"),
    browser: bool(r, "browser"),
    icon,
  };
}

/** Checks the shape of the picker reply; a malformed reply (or icon) is an error, not a default. */
export function parseSourcesView(raw: unknown): SourcesView {
  const r = record(raw, "reply");
  if (!Array.isArray(r.apps)) {
    throw new Error("prompt: apps is not a list");
  }
  return {
    apps: r.apps.map(parseSourceApp),
    selected: str(r, "selected"),
    remembered: str(r, "remembered"),
    listError: bool(r, "listError"),
  };
}

/** Checks the shape of a prompt; a malformed reply is an error, not a default. */
export function parsePromptView(raw: unknown): PromptView {
  if (typeof raw !== "object" || raw === null) {
    throw new Error("prompt: reply is not an object");
  }
  const r = raw as Record<string, unknown>;
  const kind = str(r, "kind");
  if (kind !== "lesson" && kind !== "adhoc") {
    throw new Error("prompt: unknown kind");
  }
  if (typeof r.canSnooze !== "boolean") {
    throw new Error("prompt: canSnooze is not a boolean");
  }
  return {
    id: str(r, "id"),
    kind,
    title: str(r, "title"),
    start: str(r, "start"),
    end: str(r, "end"),
    state: str(r, "state"),
    problem: str(r, "problem"),
    canSnooze: r.canSnooze,
  };
}

export interface PromptApi {
  /** The prompt that runs now, or null. */
  current(): Promise<PromptView | null>;
  sources(): Promise<SourcesView>;
  /** Starts recording from app (an exe name; "" = the whole computer). */
  start(id: string, app: string): Promise<PromptView>;
  snooze(id: string): Promise<PromptView>;
  skip(id: string): Promise<PromptView>;
}

/** Builds the prompt API over a call function (the Wails runtime in the app, a fake in tests). */
export function promptApi(call: CallByName): PromptApi {
  return {
    current: async () => {
      const raw = await call(methods.current);
      return raw === null || raw === undefined ? null : parsePromptView(raw);
    },
    sources: async () => parseSourcesView(await call(methods.sources)),
    start: async (id, app) => parsePromptView(await call(methods.start, id, app)),
    snooze: async (id) => parsePromptView(await call(methods.snooze, id)),
    skip: async (id) => parsePromptView(await call(methods.skip, id)),
  };
}

/** Bound method of the popup window (internal/input/wails/popupwindow.go); checked by a Go test like `methods`. */
export const windowMethods = {
  fit: "paratech/desktop/internal/input/wails.PopupWindowService.Fit",
} as const;

export interface PopupWindowApi {
  /** Asks for a window with this content height (DIP); resolves to the height the window got. */
  fit(height: number): Promise<number>;
}

/** Builds the popup window API over a call function. */
export function popupWindowApi(call: CallByName): PopupWindowApi {
  return {
    fit: async (height) => {
      const got = await call(windowMethods.fit, Math.round(height));
      if (typeof got !== "number" || !Number.isFinite(got)) {
        throw new Error("popup: fit reply is not a number");
      }
      return got;
    },
  };
}
