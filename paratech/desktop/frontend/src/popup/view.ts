// Display mapping for the recording popup: texts and which buttons to show for a prompt state. Whether snoozing is
// possible is decided by the Go core ("canSnooze"), never here (ARCH-44).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { errorMessages, type PromptView, type SourcesView } from "../bindings/prompt.js";

/** An action the popup calls on the core. */
export type Action = "start" | "snooze" | "skip";

/** A popup button; "close" only hides the window. */
export type ButtonId = Action | "close";

export const headings = {
  lesson: "Начинается занятие",
  adhoc: "Запись без занятия",
  none: "Запись",
} as const;

export const buttonLabels: Readonly<Record<ButtonId, string>> = {
  start: "Начать запись",
  snooze: "Через 5 минут",
  skip: "Пропустить",
  close: "Закрыть",
};

export const startingLabel = "Запуск…";

/** Texts of the start problems (domain/recording/prompt Problem). */
export const problemTexts: Readonly<Record<string, string>> = {
  unavailable: "Запись пока недоступна в этой сборке.",
  in_progress: "Уже идёт другая запись — её можно остановить в «Записях и задачах».",
  recovering: "Восстанавливаются прерванные записи. Повторите через минуту.",
  source_gone: "Выбранное приложение не запущено. Откройте его или выберите другое.",
  failed: "Не удалось начать запись.",
};

/** Notes for a prompt that is no longer on offer. */
export const stateNotes: Readonly<Record<string, string>> = {
  STARTED: "Запись началась.",
  SNOOZED: "Предложение отложено на 5 минут.",
  SKIPPED: "Предложение пропущено.",
  EXPIRED: "Занятие закончилось.",
  WITHDRAWN: "Предложения записи выключены в настройках.",
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

/** Text of a start problem; an unknown non-empty code gets the generic failure text. */
export function problemText(problem: string): string {
  if (problem === "") {
    return "";
  }
  return Object.hasOwn(problemTexts, problem) ? problemTexts[problem] : problemTexts.failed;
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
  const buttons: ButtonId[] = onOffer ? ["start", ...(view.canSnooze ? (["snooze"] as const) : []), "skip"] : ["close"];
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

const actionErrors: Readonly<Record<string, string>> = {
  [errorMessages.gone]: "Это предложение уже не актуально.",
  [errorMessages.refused]: "Это действие сейчас недоступно.",
  [errorMessages.invalidId]: "Неверный идентификатор предложения.",
  [errorMessages.actionFailed]: "Не удалось выполнить действие.",
  [errorMessages.invalidSource]: "Этот источник звука нельзя выбрать.",
};

const genericActionError = "Не удалось выполнить действие.";

export const loadErrorText = "Не удалось загрузить предложение записи.";

/** Russian text for an error of an action; an unknown error gets a generic text (raw text is not shown). */
export function actionErrorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return Object.hasOwn(actionErrors, msg) ? actionErrors[msg] : genericActionError;
}

// Source picker (TASK-059). Names and titles of apps are untrusted: the page renders them as text only.

export const wholeComputerName = "Весь звук компьютера";

/** Placeholder of the whole-computer entry in place of an icon. */
export const wholeComputerBadge = "ПК";

export const sourceMarks = {
  playing: "звучит",
  muted: "звук выключен в микшере",
  gone: "приложение закрыто",
} as const;

export const adviceTexts = {
  always:
    "Чтобы не слышать лекцию, убавьте громкость Windows или наушников. Не выключайте звук на вкладке, в приложении встречи или в микшере: тогда запись будет пустой.",
  browser: "Записывается звук всего браузера, со всех вкладок. Другие видео включайте в другом браузере или приложении.",
  muted: "Звук этого приложения выключен в микшере Windows: запись будет пустой. Включите его и убавьте общую громкость.",
  listError: "Список приложений недоступен, можно записать весь звук компьютера.",
} as const;

/** How often the open apps are listed again while the prompt is on offer. */
export const refreshMs = 3000;

/** The source the popup will record: an exe name, "" for the whole computer. */
export interface Selection {
  app: string;
  /** The user picked it: refreshes never change it. */
  explicit: boolean;
}

export const initialSelection: Selection = { app: "", explicit: false };

/** The user's own choice. */
export function chooseSource(app: string): Selection {
  return { app, explicit: true };
}

/**
 * The selection after a new picker reply. The user's choice wins; otherwise the remembered app is taken as soon as
 * it is listed, an app taken before is kept (it may only be restarting), and else the core's preselection.
 */
export function nextSelection(prev: Selection, sources: SourcesView): Selection {
  if (prev.explicit) {
    return prev;
  }
  if (sources.remembered !== "" && sources.apps.some((a) => a.app === sources.remembered)) {
    return { app: sources.remembered, explicit: false };
  }
  if (prev.app !== "") {
    return prev;
  }
  return { app: sources.selected, explicit: false };
}

/** One entry of the picker. */
export interface SourceOption {
  /** Exe name; "" for the whole computer. */
  app: string;
  /** What the entry shows: the window title, else the exe name. */
  name: string;
  /** Hover text: the title with the exe name. */
  hint: string;
  /** Validated PNG data URL or "". */
  icon: string;
  /** Shown in place of a missing icon. */
  letter: string;
  playing: boolean;
  muted: boolean;
  browser: boolean;
  /** The chosen app is not in the list any more. */
  gone: boolean;
}

const wholeComputer: SourceOption = {
  app: "", name: wholeComputerName, hint: wholeComputerName, icon: "", letter: wholeComputerBadge,
  playing: false, muted: false, browser: false, gone: false,
};

function firstLetter(name: string): string {
  const first = Array.from(name.trim())[0] ?? "?";
  return first.toUpperCase();
}

/** The entries of the picker: the whole computer first, then the apps in the core's order. */
export function sourceOptions(sources: SourcesView | null): SourceOption[] {
  const apps = sources?.apps ?? [];
  return [
    wholeComputer,
    ...apps.map((a): SourceOption => {
      const title = a.title.trim();
      const name = title || a.app;
      return {
        app: a.app, name, hint: title ? `${title} (${a.app})` : a.app, icon: a.icon, letter: firstLetter(name),
        playing: a.playing, muted: a.muted, browser: a.browser, gone: false,
      };
    }),
  ];
}

/** What the picker shows. */
export interface PickerModel {
  options: SourceOption[];
  chosen: SourceOption;
  /** Warnings about the chosen source, most important first. */
  warnings: string[];
  /** The small general advice, always shown. */
  advice: string;
}

/** Maps the last picker reply (null: not loaded yet) and the selection to what the picker shows. */
export function pickerModel(sources: SourcesView | null, selection: Selection): PickerModel {
  const options = sourceOptions(sources);
  let chosen = options.find((o) => o.app === selection.app);
  if (chosen === undefined) {
    // A chosen app that has closed stays chosen; the start then reports source_gone. Without a list nothing is known.
    const known = sources !== null && !sources.listError;
    chosen = { ...wholeComputer, app: selection.app, name: selection.app, hint: selection.app,
      letter: firstLetter(selection.app), gone: known };
  }

  const warnings: string[] = [];
  if (chosen.muted) {
    warnings.push(adviceTexts.muted);
  }
  if (chosen.browser) {
    warnings.push(adviceTexts.browser);
  }
  if (sources?.listError) {
    warnings.push(adviceTexts.listError);
  }
  return { options, chosen, warnings, advice: adviceTexts.always };
}

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
  /** The height last sent; undefined when the window size is unknown (shown again, or a call failed). */
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

  /** The shell has reset the window (it was shown again): the next request is sent even if it repeats. */
  forget(): void {
    this.sent = undefined;
  }
}

/**
 * Whether the open apps are listed again: only while the page is visible with a prompt on offer and no action runs.
 * Also not while the list is open, so entries do not move under the pointer.
 */
export function shouldRefresh(view: PromptView | null, visible: boolean, pending: Action | null, listOpen: boolean): boolean {
  return visible && !listOpen && view !== null && view.state === "OFFERED" && pending === null;
}

/** Timers the poller uses; the browser's in the page, fakes in tests. */
export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

/**
 * SourcesPoller lists the apps every refreshMs while active. A load never overlaps another: the next one is planned
 * only after the previous one has settled.
 */
export class SourcesPoller {
  private active = false;
  private busy = false;
  /** refreshNow came during a load: load again right after it. */
  private again = false;
  private handle: unknown = undefined;

  constructor(
    private readonly load: () => Promise<void>,
    private readonly timers: Timers,
    private readonly ms = refreshMs,
  ) {}

  /** Turns polling on (loading at once) or off. */
  setActive(on: boolean): void {
    if (on === this.active) {
      return;
    }
    this.active = on;
    if (on) {
      this.tick();
    } else {
      this.again = false;
      this.cancel();
    }
  }

  /** Loads now when active (the popup was shown again or the prompt changed); a load in flight is not doubled. */
  refreshNow(): void {
    if (this.active) {
      this.tick();
    }
  }

  private cancel(): void {
    if (this.handle !== undefined) {
      this.timers.clear(this.handle);
      this.handle = undefined;
    }
  }

  private tick(): void {
    this.cancel();
    if (!this.active) {
      return;
    }
    if (this.busy) {
      this.again = true;
      return;
    }
    this.busy = true;
    this.load()
      .catch(() => undefined)
      .finally(() => {
        this.busy = false;
        const again = this.again;
        this.again = false;
        if (!this.active) {
          return;
        }
        if (again) {
          this.tick();
        } else if (this.handle === undefined) {
          this.handle = this.timers.set(() => this.tick(), this.ms);
        }
      });
  }
}
