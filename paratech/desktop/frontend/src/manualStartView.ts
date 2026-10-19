// «Начать запись» of «Записи и задачи» (TASK-081): a recording without the popup, from the shared source picker. The
// core starts it and links it to the lesson in progress, if any; the page only maps the answer to what it shows.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import type { RecordingItem } from "./bindings/activity.js";
import type { RecordingStart } from "./bindings/prompt.js";
import { problemText } from "./sourcePicker/view.js";

export const manualStartTexts = {
  open: "Начать запись",
  title: "Новая запись",
  explain: "Если сейчас идёт занятие из расписания, запись попадёт к нему.",
  sourceLabel: "Источник звука",
  start: "Начать запись",
  starting: "Запуск…",
  cancel: "Отмена",
  busy: "Уже идёт запись. Остановите её, чтобы начать новую.",
} as const;

/** A recording of the list is capturing or about to: a second one cannot start. */
export function recordingRuns(rows: readonly Pick<RecordingItem, "stage">[] | null): boolean {
  return (rows ?? []).some((r) => r.stage === "recording");
}

export interface StartButtonState {
  disabled: boolean;
  /** Why it is disabled; "" when it is not. */
  reason: string;
}

/** «Начать запись» in the page header (and in the panel): unavailable while a recording runs or a start is on its way. */
export function startButton(rows: readonly Pick<RecordingItem, "stage">[] | null, starting: boolean): StartButtonState {
  if (recordingRuns(rows)) {
    return { disabled: true, reason: manualStartTexts.busy };
  }
  return { disabled: starting, reason: "" };
}

/** What a start answer does to the panel: a started one closes it, a refused one keeps it open with its problem. */
export type StartOutcome = { kind: "started" } | { kind: "refused"; problem: string };

export function startOutcome(r: RecordingStart): StartOutcome {
  if (r.started) {
    return { kind: "started" };
  }
  return { kind: "refused", problem: problemText(r.problem) || problemText("failed") };
}
