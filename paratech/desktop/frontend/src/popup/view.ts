// Display mapping for the recording popup: texts and which buttons to show for a prompt state. The prompt states are
// the Go core's; the popup offers only «Начать запись» and «Не записывать» (no snooze since TASK-076).
// The source picker and the start problem texts are shared with the main window (sourcePicker/view.ts).

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import type { RecordingItem } from "../bindings/activity.js";
import type { PromptView } from "../bindings/prompt.js";
import { problemText } from "../sourcePicker/view.js";

/** An action the popup calls on the core. */
export type Action = "start" | "skip";

/** A popup button; "close" only hides the window. */
export type ButtonId = Action | "close";

export const headings = {
  lesson: "Начинается пара",
  adhoc: "Запись без пары",
  none: "Запись",
} as const;

/** Heading of the silent-source advice (TASK-082); its text and buttons are in sourcePicker/advice.ts. */
export const adviceHeading = "Идёт запись — нет звука";

export const buttonLabels: Readonly<Record<ButtonId, string>> = {
  start: "Начать запись",
  skip: "Не записывать",
  close: "Закрыть",
};

/** Hover texts and accessible descriptions: what each button does. */
export const buttonHints: Readonly<Record<ButtonId, string>> = {
  start: "Записать звук этой пары",
  skip: "Закрыть, эта пара не записывается",
  close: "Закрыть окно",
};

export const startingLabel = "Запуск…";

/** Notes for a prompt that is no longer on offer. */
export const stateNotes: Readonly<Record<string, string>> = {
  STARTED: "Запись началась.",
  SKIPPED: "Эта пара не записывается.",
  EXPIRED: "Пара закончилась.",
};

/**
 * What the top line of the popup is about. The dot is neutral while only a recording is offered; the red pulsing dot
 * (and the red heading) mean a recording runs: the running view and the silent-source advice, which is about it.
 */
export type PopupStage = "offer" | "recording" | "advice";

/** The class of the dot: only a running recording pulses red (owner 2026-11-08). */
export function dotClass(stage: PopupStage): string {
  return stage === "offer" ? "dot" : "dot rec";
}

/** The class of the top line: the heading takes the recording colour only with the red dot. */
export function topClass(stage: PopupStage): string {
  return stage === "offer" ? "p-top" : "p-top live";
}

export const noPromptNote = "Сейчас нет предложения записи.";
export const staleNote = "Предложение больше не актуально.";

/** What the popup shows. */
export interface PopupModel {
  heading: string;
  /** Lesson title (plain text); "" for an ad-hoc prompt. */
  title: string;
  /** "start–end"; "" when unknown. */
  time: string;
  /** Status line for a prompt that is not on offer; "" while it is. */
  note: string;
  /** Text of the last failed start; "" when none. */
  problem: string;
  buttons: readonly ButtonId[];
  startLabel: string;
  /** All buttons are disabled while a call runs or the core is starting the recording. */
  disabled: boolean;
}

/** Joins the lesson times; a missing end shows only the start. */
export function timeRange(start: string, end: string): string {
  if (start === "" || end === "") {
    return start || end;
  }
  return `${start}–${end}`;
}

/** Maps the current prompt (null: none) and the action this page is waiting for to what the popup shows. */
export function popupModel(view: PromptView | null, pending: Action | null): PopupModel {
  if (view === null) {
    return {
      heading: headings.none, title: "", time: "", note: noPromptNote, problem: "",
      buttons: ["close"], startLabel: buttonLabels.start, disabled: pending !== null,
    };
  }

  const onOffer = view.state === "OFFERED" || view.state === "STARTING";
  const starting = view.state === "STARTING" || pending === "start";
  const buttons: ButtonId[] = onOffer ? ["start", "skip"] : ["close"];
  let note = "";
  if (!onOffer) {
    note = Object.hasOwn(stateNotes, view.state) ? stateNotes[view.state] : staleNote;
  }

  return {
    heading: view.kind === "adhoc" ? headings.adhoc : headings.lesson,
    title: view.kind === "adhoc" ? "" : view.title,
    time: view.kind === "adhoc" ? "" : timeRange(view.start, view.end),
    note,
    // A problem belongs to the last attempt: hidden while a new one runs.
    problem: view.state === "OFFERED" && !starting ? problemText(view.problem) : "",
    buttons,
    startLabel: starting ? startingLabel : buttonLabels.start,
    disabled: starting || pending !== null,
  };
}

export const loadErrorText = "Не удалось загрузить предложение записи.";

/**
 * Whether the general advice shrinks to one line (full text on hover), so the buttons stay in the 300 px popup.
 * Only when two or more other messages are shown: next to one warning (the browser one included) or an error alone
 * it stays in full, since it carries the muted-tab advice.
 */
export function collapseAdvice(warnings: readonly string[], error: string, joinOwnRow = false): boolean {
  // The join button on its own row (TASK-406) takes one more button row (about 33 px) from the texts: one other message
  // is enough to shrink the advice then.
  const limit = joinOwnRow ? 1 : 2;
  return warnings.length + (error !== "" ? 1 : 0) >= limit;
}

// Window growth for a start error (owner's decision 2026-10-16): the error popup grows in height to show its texts in
// full; the normal popup keeps 360x300. The Go shell clamps the height (popupMaxHeight, the work area).

/** The normal popup height in DIP (popupHeight in internal/input/wails/spec.go). */
export const popupBaseHeight = 300;

/** Added to the measured height, so DIP rounding at 125 % or 150 % does not leave a 1 px scroll. */
export const fitSlack = 2;

/** How the text area is laid out. */
export interface TextsLayout {
  /** The window may grow to show the texts in full: only while an error is shown. */
  grows: boolean;
  /** Error and warnings are cut at 3 lines (full text on hover). */
  clamp: boolean;
  /** The general advice shrinks to one line (see collapseAdvice). */
  collapsed: boolean;
}

/**
 * Layout of the text area: an error grows the window and shows everything in full; otherwise the 300 px rules hold.
 * `joinOwnRow`: «Открыть ещё раз» and «Не записывать» each take a full row (see joinOnOwnRow in joinView.ts).
 */
export function textsLayout(warnings: readonly string[], error: string, joinOwnRow = false): TextsLayout {
  const grows = error !== "";
  return { grows, clamp: !grows, collapsed: !grows && collapseAdvice(warnings, error, joinOwnRow) };
}

/**
 * The window height the page asks for, from the natural height of its content (CSS px = DIP). The normal size unless
 * the window grows and the content needs more.
 */
export function wantedHeight(natural: number, grows: boolean): number {
  if (!grows || !Number.isFinite(natural) || natural <= popupBaseHeight) {
    return popupBaseHeight;
  }
  return Math.ceil(natural) + fitSlack;
}

/**
 * HeightFitter sends the wanted window height to the core only when it changes. Calls never overlap: a height wanted
 * during a call is sent after it, and only the newest one.
 */
export class HeightFitter {
  /** The height last sent; undefined when the window size is unknown (a call failed). */
  private sent: number | undefined = popupBaseHeight;
  private busy = false;
  private waiting: number | undefined = undefined;

  constructor(private readonly fit: (height: number) => Promise<unknown>) {}

  /** Asks for height unless the window was already asked for it. */
  request(height: number): void {
    if (this.busy) {
      this.waiting = height;
      return;
    }
    if (height === this.sent) {
      return;
    }
    this.busy = true;
    this.sent = height;
    this.fit(height)
      .catch(() => {
        this.sent = undefined;
      })
      .finally(() => {
        this.busy = false;
        const next = this.waiting;
        this.waiting = undefined;
        if (next !== undefined) {
          this.request(next);
        }
      });
  }

  /** The shell has reset the window to the base height (it was shown again). */
  forget(): void {
    this.sent = popupBaseHeight;
  }
}

/**
 * Whether the open apps are listed again: only while the page is visible with a prompt on offer and no action runs.
 * Also not while the list is open, so entries do not move under the pointer.
 */
export function shouldRefresh(view: PromptView | null, visible: boolean, pending: Action | null, listOpen: boolean): boolean {
  return visible && !listOpen && view !== null && view.state === "OFFERED" && pending === null;
}

// The popup while a recording runs (TASK-099, docs/design/concept-strict.html section 4): the lesson title, the
// elapsed time, the level capsule, «Остановить» and «Скрыть». The core says whether a recording captures
// (RecordingSound.recording). The mockup's source row is not shown: no binding says which source the running
// recording captures (the remembered source may differ after a switch or a choice in «Диагностика»).

export const recordingTexts = {
  heading: "Идёт запись",
  tip: "Окно можно закрыть — запись продолжится. Остановить можно и в окне paratech, раздел «Записи».",
  stop: "Остановить",
  stopping: "Остановка…",
  hide: "Скрыть",
  stopHint: "Остановить запись",
  hideHint: "Скрыть окно, запись продолжится",
  stopFailed: "Не удалось остановить запись. Остановите её в окне paratech, раздел «Записи».",
  noLesson: "Запись без пары",
} as const;

/** What the popup shows of the running recording besides its sound. */
export interface RecordingInfo {
  /** Lesson title (untrusted, plain text) or recordingTexts.noLesson. */
  title: string;
  /** RFC 3339; "" when unknown (no timer). */
  startedAt: string;
}

/** The running recording id from the list the core returns; null when it is not listed. */
export function recordingInfo(items: readonly RecordingItem[], id: string): RecordingInfo | null {
  const r = items.find((i) => i.id === id);
  if (r === undefined) {
    return null;
  }
  return { title: r.title.trim() || recordingTexts.noLesson, startedAt: r.startedAt };
}

/** The elapsed time «hh:mm:ss» since startedAt by the clock now (skew clamped to 0); "" when startedAt is unknown. */
export function elapsedText(startedAt: string, now: number): string {
  const started = startedAt === "" ? NaN : Date.parse(startedAt);
  if (Number.isNaN(started) || !Number.isFinite(now)) {
    return "";
  }
  const total = Math.floor(Math.max(0, now - started) / 1000);
  const two = (n: number) => String(n).padStart(2, "0");
  return `${two(Math.floor(total / 3600))}:${two(Math.floor((total % 3600) / 60))}:${two(total % 60)}`;
}

/**
 * Whether the popup shows the running recording instead of the prompt: while a recording captures and no prompt is on
 * offer. The tray's ad-hoc offer is replaced too (a second recording would be refused); a lesson offer stays.
 */
export function showsRecording(view: PromptView | null, recording: boolean, pending: Action | null): boolean {
  if (!recording || pending !== null) {
    return false;
  }
  if (view === null) {
    return true;
  }
  if (view.state === "STARTING") {
    return false;
  }
  return view.state !== "OFFERED" || view.kind === "adhoc";
}
