// The silent-source advice of a running recording (TASK-082), shown in its row of «Записи и задачи» and in the popup:
// the chosen app is silent while another one plays. The core decides when to advise and what to suggest; the pages
// only show it and pass «Переключиться» / «Оставить» back. App names are untrusted: render them as text only.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { errorMessages, type AdviceView, type SourceSwitch } from "../bindings/activity.js";

export const adviceLabels = {
  switch: "Переключиться",
  keep: "Оставить",
  switching: "Переключение…",
} as const;

export const adviceHints = {
  switch: "Записывать дальше из этого источника; запись продолжится",
  keep: "Записывать как сейчас; предупреждение скроется",
} as const;

/** Texts of the switch problems (SourceSwitch.problem). */
export const switchProblemTexts = {
  source_gone: "Это приложение уже закрыто — переключиться не получилось. Запись идёт как раньше.",
  refused: "Сейчас переключиться нельзя. Повторите через несколько секунд.",
  failed: "Не удалось переключиться: источник не запустился. Запись идёт как раньше.",
} as const;

const switchErrors: Readonly<Record<string, string>> = {
  [errorMessages.recordingNotRunning]: "Эта запись уже не идёт.",
  [errorMessages.notFound]: "Эта запись уже не идёт.",
  [errorMessages.invalidId]: "Неверный идентификатор записи.",
  [errorMessages.invalidSource]: "Этот источник звука нельзя выбрать.",
  [errorMessages.actionFailed]: "Не удалось выполнить действие.",
};

const genericSwitchError = "Не удалось выполнить действие.";

/** What the advice shows. */
export interface AdviceModel {
  /** Plain text: «В <app> нет звука, звук идёт из <app>. Переключиться?». */
  text: string;
  /** Hover text with the exe names. */
  hint: string;
}

/** The text and the hover text of an advice; a missing readable name falls back to the exe name. */
export function adviceModel(a: AdviceView): AdviceModel {
  const silent = a.silentName.trim() || a.silentApp;
  if (a.suggestApp === "") {
    return {
      text: `В ${silent} нет звука, звук идёт из других приложений. Записывать весь звук компьютера?`,
      hint: `${a.silentApp} → весь звук компьютера`,
    };
  }
  const suggest = a.suggestName.trim() || a.suggestApp;
  return { text: `В ${silent} нет звука, звук идёт из ${suggest}. Переключиться?`, hint: `${a.silentApp} → ${a.suggestApp}` };
}

/** Text of a switch that did not happen; "" once switched. An unknown problem gets the generic text. */
export function switchProblemText(s: SourceSwitch): string {
  if (s.switched) {
    return "";
  }
  return Object.hasOwn(switchProblemTexts, s.problem)
    ? switchProblemTexts[s.problem as keyof typeof switchProblemTexts]
    : genericSwitchError;
}

/** Russian text for an error of switchSource or dismissAdvice; raw text is never shown. */
export function adviceErrorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return Object.hasOwn(switchErrors, msg) ? switchErrors[msg] : genericSwitchError;
}

/** Identifies one advice: a new suggestion for the same recording is another advice. */
export function adviceKey(a: AdviceView): string {
  return [a.recordingId, a.silentApp, a.suggestApp].join("|");
}

/** The advice action that runs, if any. */
export type AdvicePending = "switch" | "dismiss" | null;

/** The calls behind the two buttons; activityApi in the app, fakes in tests. */
export interface AdviceCalls {
  switchSource(recordingId: string, app: string): Promise<SourceSwitch>;
  dismissAdvice(recordingId: string): Promise<void>;
}

/** The outcome of a click: done (the core withdraws the advice) or a problem to show next to it. */
export type AdviceOutcome = { kind: "done" } | { kind: "problem"; text: string };

/** «Переключиться»: switches to the suggestion; a refused switch keeps the advice with its problem. */
export async function switchToSuggestion(calls: AdviceCalls, a: AdviceView): Promise<AdviceOutcome> {
  try {
    const text = switchProblemText(await calls.switchSource(a.recordingId, a.suggestApp));
    return text === "" ? { kind: "done" } : { kind: "problem", text };
  } catch (err) {
    return { kind: "problem", text: adviceErrorText(err) };
  }
}

/** «Оставить»: hides the advice until it changes. */
export async function keepSource(calls: AdviceCalls, a: AdviceView): Promise<AdviceOutcome> {
  try {
    await calls.dismissAdvice(a.recordingId);
    return { kind: "done" };
  } catch (err) {
    return { kind: "problem", text: adviceErrorText(err) };
  }
}
