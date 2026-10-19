// The source picker shared by the popup, «Диагностика» and «Записи и задачи» (TASK-059, TASK-081): the entries, the
// selection rules, the warnings, the polling of the open apps and the texts of a refused start. Names and titles of
// apps are untrusted: the pages render them as text only.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { errorMessages, type SourcesView } from "../bindings/prompt.js";

/** Texts of the start problems (domain/recording/prompt Problem). */
export const problemTexts: Readonly<Record<string, string>> = {
  unavailable: "Запись пока недоступна в этой сборке.",
  in_progress: "Уже идёт другая запись — её можно остановить в «Записях и задачах».",
  recovering: "Восстанавливаются прерванные записи. Повторите через минуту.",
  source_gone: "Выбранное приложение не запущено. Откройте его или выберите другое.",
  failed: "Не удалось начать запись.",
};

/** Text of a start problem; an unknown non-empty code gets the generic failure text. */
export function problemText(problem: string): string {
  if (problem === "") {
    return "";
  }
  return Object.hasOwn(problemTexts, problem) ? problemTexts[problem] : problemTexts.failed;
}

const actionErrors: Readonly<Record<string, string>> = {
  [errorMessages.gone]: "Это предложение уже не актуально.",
  [errorMessages.refused]: "Это действие сейчас недоступно.",
  [errorMessages.invalidId]: "Неверный идентификатор предложения.",
  [errorMessages.actionFailed]: "Не удалось выполнить действие.",
  [errorMessages.invalidSource]: "Этот источник звука нельзя выбрать.",
};

const genericActionError = "Не удалось выполнить действие.";

/** Russian text for an error of a prompt call; an unknown error gets a generic text (raw text is not shown). */
export function actionErrorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return Object.hasOwn(actionErrors, msg) ? actionErrors[msg] : genericActionError;
}

export const wholeComputerName = "Весь звук компьютера";

/** Placeholder of the whole-computer entry in place of an icon. */
export const wholeComputerBadge = "ПК";

/** Marks of an entry. Only sounding apps are marked: an open audio session alone may well be silent (TASK-082). */
export const sourceMarks = {
  sounding: "звук идёт",
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

/** Said under the picker when the core chose the app because it plays while the remembered one does not. */
export const autoSelectedNote = "Выбрано: звук идёт отсюда.";

/** How often the open apps are listed again while a picker is shown. */
export const refreshMs = 3000;

/** The source to record: an exe name, "" for the whole computer. */
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
 * The selection of a start after a new picker reply (popup, «Начать запись»). The user's choice wins; then the core's
 * preselection of the app that plays while the remembered one does not (TASK-082), kept while that app is open so a
 * pause in its sound does not switch back; otherwise the remembered app is taken as soon as it is listed, an app taken
 * before is kept (it may only be restarting), and else the core's preselection.
 */
export function nextSelection(prev: Selection, sources: SourcesView): Selection {
  if (prev.explicit) {
    return prev;
  }
  if (sources.autoSelected) {
    return { app: sources.selected, explicit: false };
  }
  // A non-explicit app other than the remembered one can only come from the preselection above.
  const listed = (app: string) => sources.apps.some((a) => a.app === app);
  if (prev.app !== "" && prev.app !== sources.remembered && listed(prev.app)) {
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

/**
 * The selection of «Диагностика»: the remembered source itself, since the sound check reads it even when the app is
 * not running (then the entry is marked closed). A choice being saved shows at once.
 */
export function savedSelection(sources: SourcesView | null, saving: string | null): Selection {
  return { app: saving ?? sources?.remembered ?? "", explicit: saving !== null };
}

/** What a failed list call is taken as: only the whole computer, the remembered source kept from the last reply. */
export function failedSources(prev: SourcesView | null): SourcesView {
  return { apps: [], selected: "", autoSelected: false, remembered: prev?.remembered ?? "", listError: true };
}

/** One entry of the picker. */
export interface SourceOption {
  /** Exe name; "" for the whole computer. */
  app: string;
  /** What the entry shows: the readable app name (TASK-082). */
  name: string;
  /** Hover text: the name, the window title and the exe name. */
  hint: string;
  /** Validated PNG data URL or "". */
  icon: string;
  /** Shown in place of a missing icon. */
  letter: string;
  /** Its sound plays right now: the «звук идёт» mark. */
  sounding: boolean;
  muted: boolean;
  browser: boolean;
  /** The chosen app is not in the list any more. */
  gone: boolean;
}

const wholeComputer: SourceOption = {
  app: "", name: wholeComputerName, hint: wholeComputerName, icon: "", letter: wholeComputerBadge,
  sounding: false, muted: false, browser: false, gone: false,
};

function firstLetter(name: string): string {
  const first = Array.from(name.trim())[0] ?? "?";
  return first.toUpperCase();
}

/** Hover text of an app: its name, the window title when it says more, and the exe name in brackets. */
export function sourceHint(name: string, title: string, app: string): string {
  const t = title.trim();
  const label = t !== "" && t !== name ? `${name} — ${t}` : name;
  return label === app ? app : `${label} (${app})`;
}

/**
 * The entries of the picker: the whole computer first, then the apps in the core's order (sounding ones first; the
 * page does not re-sort them).
 */
export function sourceOptions(sources: SourcesView | null): SourceOption[] {
  const apps = sources?.apps ?? [];
  return [
    wholeComputer,
    ...apps.map((a): SourceOption => {
      const name = a.name.trim() || a.app;
      return {
        app: a.app, name, hint: sourceHint(name, a.title, a.app), icon: a.icon, letter: firstLetter(name),
        sounding: a.sounding, muted: a.muted, browser: a.browser, gone: false,
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
  /** autoSelectedNote when the chosen app is the core's choice by sound, else "". */
  autoNote: string;
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
  // Not the user's choice and not the remembered app: only the preselection by sound picks that (nextSelection).
  const auto = sources !== null && !sources.listError && !selection.explicit && selection.app !== "" &&
    selection.app !== sources.remembered && !chosen.gone;
  return { options, chosen, warnings, autoNote: auto ? autoSelectedNote : "", advice: adviceTexts.always };
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

  /** Loads now when active (the picker was shown again or its owner changed); a load in flight is not doubled. */
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
