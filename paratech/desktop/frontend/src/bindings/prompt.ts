// Recording prompt bindings: calls into the Go core (internal/input/wails/prompt.go). The core owns the prompt; the
// popup only shows it and offers "Start recording" and "Do not record" (no snooze since TASK-076). The main window uses
// the same source picker: selectSource remembers the source (shared with the popup and the sound check) and
// startRecording starts a recording without the popup (TASK-081).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { parseAdvice, type AdviceView } from "./activity.js";
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
  /** OFFERED, STARTING, STARTED, SKIPPED or EXPIRED. */
  state: string;
  /** Why the last start failed: "", "unavailable", "in_progress", "recovering", "source_gone" or "failed". */
  problem: string;
}

/** An app the popup may record from. */
export interface SourceApp {
  /** Executable name, for example "chrome.exe": the value Start takes. Shown only in a tooltip. */
  app: string;
  /** Readable name («Яндекс Браузер», TASK-082), never empty: the label. Untrusted text, render as plain text only. */
  name: string;
  /** Window title: untrusted text, render as plain text only. May be empty. */
  title: string;
  /** It has an active audio session: it may play sound. */
  playing: boolean;
  /** Its sound plays right now (TASK-082): the «звук идёт» mark. */
  sounding: boolean;
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
  /** selected plays while the remembered app does not (TASK-082): the picker says why it is chosen. */
  autoSelected: boolean;
  /** The app recorded last time, even when it is not open now. */
  remembered: string;
  /** The apps could not be listed: only the whole computer is offered. */
  listError: boolean;
}

/** The answer of a start from the main window. */
export interface RecordingStart {
  started: boolean;
  /** Why it did not start, with the codes of PromptView.problem ("in_progress" while a recording runs); "" once started. */
  problem: string;
  /** "" unless started. */
  recordingId: string;
  /** The recording belongs to the lesson in progress; otherwise it has no lesson. */
  linked: boolean;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  current: "paratech/desktop/internal/input/wails.PromptService.Current",
  sources: "paratech/desktop/internal/input/wails.PromptService.Sources",
  start: "paratech/desktop/internal/input/wails.PromptService.Start",
  skip: "paratech/desktop/internal/input/wails.PromptService.Skip",
  selectSource: "paratech/desktop/internal/input/wails.PromptService.SelectSource",
  startRecording: "paratech/desktop/internal/input/wails.PromptService.StartRecording",
  advice: "paratech/desktop/internal/input/wails.PromptService.Advice",
} as const;

/** Wails event the core emits each time it shows the popup; the page then reads the current prompt and advice. */
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
  // The core never sends an empty name; if it did, the exe name still labels the entry.
  const name = str(r, "name").trim() || app;
  return {
    app,
    name,
    title: str(r, "title"),
    playing: bool(r, "playing"),
    sounding: bool(r, "sounding"),
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
    autoSelected: bool(r, "autoSelected"),
    remembered: str(r, "remembered"),
    listError: bool(r, "listError"),
  };
}

/** Checks the shape of a start from the main window; a malformed reply is an error, not a default. */
export function parseRecordingStart(raw: unknown): RecordingStart {
  const r = record(raw, "reply");
  const started = bool(r, "started");
  const problem = str(r, "problem");
  if (started !== (problem === "")) {
    throw new Error("prompt: started and problem disagree");
  }
  return { started, problem, recordingId: str(r, "recordingId"), linked: bool(r, "linked") };
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
  return {
    id: str(r, "id"),
    kind,
    title: str(r, "title"),
    start: str(r, "start"),
    end: str(r, "end"),
    state: str(r, "state"),
    problem: str(r, "problem"),
  };
}

export interface PromptApi {
  /** The prompt that runs now, or null. */
  current(): Promise<PromptView | null>;
  sources(): Promise<SourcesView>;
  /** Starts recording from app (an exe name; "" = the whole computer). */
  start(id: string, app: string): Promise<PromptView>;
  skip(id: string): Promise<PromptView>;
  /** Remembers app ("" = the whole computer) as the source of the next recordings and of the sound check. */
  selectSource(app: string): Promise<void>;
  /** Starts recording app ("" = the whole computer) without the popup; a refused start resolves with its problem. */
  startRecording(app: string): Promise<RecordingStart>;
  /** The silent-source advice the popup shows (TASK-082), or null. */
  advice(): Promise<AdviceView | null>;
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
    skip: async (id) => parsePromptView(await call(methods.skip, id)),
    selectSource: async (app) => {
      await call(methods.selectSource, app);
    },
    startRecording: async (app) => parseRecordingStart(await call(methods.startRecording, app)),
    advice: async () => {
      const raw = await call(methods.advice);
      return raw === null || raw === undefined ? null : parseAdvice(raw);
    },
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
