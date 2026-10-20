// The summary viewer without React (TASK-087): when to ask again, which rows offer the summary, paragraphs and state
// lines. The core owns the summary and every state text; this module only arranges what it returned. Summary text, key
// points and assignment titles are model output: they stay plain strings for React text nodes, never HTML or links.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import { formatDuration, formatTime } from "./activityView.js";
import type { RecordingItem } from "./bindings/activity.js";
import {
  errorMessages,
  lectureErrorMessages,
  type SummaryAssignment,
  type SummaryState,
  type SummaryStep,
  type SummaryView,
} from "./bindings/summary.js";
import { dueText } from "./homeworkView.js";
import type { ResultKind } from "./ui/controls.js";

/** How often a waiting or running step is asked for again. */
export const pollMs = 3000;

/** How often a step that has not started is asked for again: the job may be enqueued later. */
export const idlePollMs = 5000;

/** The longest wait between retries after failed calls. */
export const maxErrorPollMs = 30000;

/** How a recording row offers «Открыть конспект»: the main action, an extra one, or not at all. */
export type SummaryOffer = "main" | "extra" | null;

/**
 * Whether a row of «Записи и задачи» offers «Открыть конспект», from the chain the list already has (no call per row).
 * Main: the chain is done or at the lecture text, or only the homework search or the lecture text failed. Extra: the summary is queued,
 * running or failed, so the page shows why and keeps the place for «Повторить».
 */
export function rowSummaryOffer(r: Pick<RecordingItem, "stage" | "step" | "errorCode">): SummaryOffer {
  switch (r.stage) {
    case "done":
      return "main";
    case "summarizing":
      return "extra";
    // The lecture text comes after the summary and the homework, so the summary is there.
    case "lectureText":
      return "main";
    case "queued":
      if (r.step === "lectureText") {
        return "main";
      }
      return r.step === "summary" ? "extra" : null;
    case "failed":
      if (r.errorCode === "homeworkFailed" || r.errorCode === "lectureTextFailed") {
        return "main";
      }
      return r.errorCode === "summaryFailed" ? "extra" : null;
    default:
      return null;
  }
}

/** Whether a step still goes on in the core. */
export function isActive(state: SummaryState): boolean {
  return state === "waiting" || state === "running";
}

function isFinal(state: SummaryState): boolean {
  return state === "done" || state === "failed";
}

/**
 * The wait before the next view, or null to stop: errors is the number of failed calls in a row. Only the summary and
 * the homework done or failed with no lecture text being made stop (a lecture text never made waits for a click); a
 * step that has not started is asked for slower, since the recognition may still run.
 */
export function nextPollMs(last: SummaryView | null, errors: number): number | null {
  if (errors > 0) {
    return Math.min(maxErrorPollMs, pollMs * 2 ** Math.min(errors - 1, 10));
  }
  if (last === null) {
    return pollMs;
  }
  const lecture = isActive(last.lectureText.state);
  if (isFinal(last.summary.state) && isFinal(last.homework.state) && !lecture) {
    return null;
  }
  return isActive(last.summary.state) || isActive(last.homework.state) || lecture ? pollMs : idlePollMs;
}

/** «<дата, время> · Длительность 0:45:00» under the title; "" parts are left out. */
export function headerMeta(v: Pick<SummaryView, "startedAt" | "hasDuration" | "durationSeconds">): string {
  const at = v.startedAt === "" ? "" : formatTime(v.startedAt);
  const parts = [at === "—" ? "" : at, v.hasDuration ? `Длительность ${formatDuration(v.durationSeconds)}` : ""];
  return parts.filter((s) => s !== "").join(" · ");
}

/** The title of the page: the lesson, or the core's «Запись <дата, время>». */
export function pageTitle(v: Pick<SummaryView, "title">): string {
  return v.title.trim() !== "" ? v.title : "Запись";
}

/** The summary text as paragraphs: blank lines split them, single line breaks stay inside (shown with pre-line). */
export function summaryParagraphs(text: string): string[] {
  return text
    .replace(/\r\n?/g, "\n")
    .split(/\n[ \t]*\n/)
    .map((p) => p.trim())
    .filter((p) => p !== "");
}

/** The key points to list, without empty ones. */
export function keyPointList(points: readonly string[]): string[] {
  return points.map((p) => p.trim()).filter((p) => p !== "");
}

/** The state line of a step: the core's text and its look; detail is the core's last error for the tooltip only, "" for none. */
export interface StepLine {
  /** null: plain text (running, none). */
  kind: ResultKind | null;
  message: string;
  detail: string;
}

const stepKinds: Readonly<Record<SummaryState, ResultKind | null>> = {
  none: null,
  waiting: "wait",
  running: null,
  done: "ok",
  failed: "fail",
};

export function stepLine(s: SummaryStep): StepLine {
  return {
    kind: stepKinds[s.state],
    message: s.message,
    detail: s.state === "failed" || s.state === "waiting" ? s.jobError : "",
  };
}

/** The summary's state line, or null when the ready summary says it all. */
export function summaryLine(v: SummaryView): StepLine | null {
  return v.summary.state === "done" && v.summary.ready ? null : stepLine(v.summary);
}

/** The homework's state line, or null when the found assignments say it all. */
export function homeworkLine(v: SummaryView): StepLine | null {
  return v.homework.state === "done" && v.homework.assignments.length > 0 ? null : stepLine(v.homework);
}

/** Under a ready summary while a new run waits, runs or failed. */
export const earlierNote = "Показан конспект, составленный раньше.";

export function showsEarlier(v: SummaryView): boolean {
  return v.summary.ready && v.summary.state !== "done";
}

/** «Составлен <дата, время>» of a ready summary; "" otherwise. */
export function updatedText(v: SummaryView): string {
  if (!v.summary.ready || v.summary.updatedAt === "") {
    return "";
  }
  const at = formatTime(v.summary.updatedAt);
  return at === "—" ? "" : `Составлен ${at}`;
}

/** The deadline of an assignment and whether the student changed it. */
export function assignmentMeta(a: Pick<SummaryAssignment, "due" | "userEdited">): string {
  return a.userEdited ? `${dueText(a.due)} · изменено вами` : dueText(a.due);
}

/** Which step a retry is for (TASK-086). */
export type RetryStep = "summary" | "homework";

/** What the retry button of a step does: start the failed work again, or make a done summary again (asks first). */
export type RetryAction = { kind: "retry"; work: RetryStep } | { kind: "redo" };

/**
 * The action of a step's retry button, or null when it offers none: a failed step is retried, a ready summary may be
 * made again. A waiting or running step offers nothing.
 */
export function retryAction(v: SummaryView, step: RetryStep): RetryAction | null {
  if (step === "homework") {
    return v.homework.state === "failed" ? { kind: "retry", work: "homework" } : null;
  }
  if (v.summary.state === "failed") {
    return { kind: "retry", work: "summary" };
  }
  return v.summary.state === "done" && v.summary.ready ? { kind: "redo" } : null;
}

/** The label of the retry button of a step, or null when it offers none. */
export function retryLabel(v: SummaryView, step: RetryStep): string | null {
  const a = retryAction(v, step);
  if (a === null) {
    return null;
  }
  return a.kind === "redo" ? "Сделать конспект заново" : "Повторить";
}

/** A click on a step's retry button: a retry starts at once, a redo only opens the confirmation (it spends quota). */
export function retryClick(
  v: SummaryView,
  step: RetryStep,
  on: { retry: (work: RetryStep) => void; askRedo: () => void },
): void {
  const a = retryAction(v, step);
  if (a === null) {
    return;
  }
  if (a.kind === "redo") {
    on.askRedo();
  } else {
    on.retry(a.work);
  }
}

/** The confirmation of «Сделать конспект заново». */
export const redoTexts = {
  title: "Сделать конспект заново?",
  body: "Модель прочитает расшифровку ещё раз, это расходует квоту. Текущий конспект останется, пока не будет готов новый.",
  confirm: "Сделать заново",
  cancel: "Отмена",
} as const;

export const noSummaryHint = "Конспекта пока нет: копировать и сохранять нечего.";

const knownErrors: ReadonlySet<string> = new Set([
  ...Object.values(errorMessages),
  ...Object.values(lectureErrorMessages),
]);

/** The core's fixed text of a failed call as it is; anything else (a malformed reply) gets the generic text. */
export function errorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return knownErrors.has(msg) ? msg : errorMessages.unavailable;
}

/** The parts of the summary screen (TASK-100): one is shown at a time. */
export type SummarySegment = "summary" | "lecture" | "homework";

export const summarySegments: readonly { value: SummarySegment; label: string }[] = [
  { value: "summary", label: "Конспект" },
  { value: "lecture", label: "Текст лекции" },
  { value: "homework", label: "Задания" },
];

/** The part shown first: the lecture text when the row's «Открыть текст лекции» opened the screen, else the summary. */
export function firstSegment(section?: "lecture"): SummarySegment {
  return section === "lecture" ? "lecture" : "summary";
}
