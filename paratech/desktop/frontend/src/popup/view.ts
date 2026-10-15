// Display mapping for the recording popup: texts and which buttons to show for a prompt state. Whether snoozing is
// possible is decided by the Go core ("canSnooze"), never here (ARCH-44).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { errorMessages, type PromptView } from "../bindings/prompt.js";

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
  snooze: "Отложить на 5 мин",
  skip: "Пропустить",
  close: "Закрыть",
};

export const startingLabel = "Запуск…";

/** Texts of the start problems (domain/recording/prompt Problem). */
export const problemTexts: Readonly<Record<string, string>> = {
  unavailable: "Запись пока недоступна в этой сборке.",
  in_progress: "Уже идёт другая запись. Остановите её на странице «Записи».",
  recovering: "Восстанавливаются прерванные записи. Повторите через минуту.",
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
