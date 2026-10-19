// Display mapping for the recording popup: texts and which buttons to show for a prompt state. The prompt states are
// the Go core's; the popup offers only «Начать запись» and «Не записывать» (no snooze since TASK-076).
// The source picker and the start problem texts are shared with the main window (sourcePicker/view.ts).

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import type { PromptView } from "../bindings/prompt.js";
import { problemText } from "../sourcePicker/view.js";

/** An action the popup calls on the core. */
export type Action = "start" | "skip";

/** A popup button; "close" only hides the window. */
export type ButtonId = Action | "close";

export const headings = {
  lesson: "Начинается занятие",
  adhoc: "Запись без занятия",
  none: "Запись",
} as const;

export const buttonLabels: Readonly<Record<ButtonId, string>> = {
  start: "Начать запись",
  skip: "Не записывать",
  close: "Закрыть",
};

/** Hover texts and accessible descriptions: what each button does. */
export const buttonHints: Readonly<Record<ButtonId, string>> = {
  start: "Записать звук этого занятия",
  skip: "Закрыть, это занятие не записывается",
  close: "Закрыть окно",
};

export const startingLabel = "Запуск…";

/** Notes for a prompt that is no longer on offer. */
export const stateNotes: Readonly<Record<string, string>> = {
  STARTED: "Запись началась.",
  SKIPPED: "Это занятие не записывается.",
  EXPIRED: "Занятие закончилось.",
};

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
export function collapseAdvice(warnings: readonly string[], error: string): boolean {
  return warnings.length + (error !== "" ? 1 : 0) >= 2;
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

/** Layout of the text area: an error grows the window and shows everything in full; otherwise the 300 px rules hold. */
export function textsLayout(warnings: readonly string[], error: string): TextsLayout {
  const grows = error !== "";
  return { grows, clamp: !grows, collapsed: !grows && collapseAdvice(warnings, error) };
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
