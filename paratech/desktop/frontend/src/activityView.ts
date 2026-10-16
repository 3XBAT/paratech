// Display mapping for the recordings and jobs page: labels and formats only. Whether an action is offered is decided
// by the Go core ("stoppable", "runNowOffered"), never here (ARCH-44).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { errorMessages } from "./bindings/activity.js";

/** Labels of the recording statuses (domain/recording/recording). */
export const recordingStatusLabels: Readonly<Record<string, string>> = {
  STARTING: "Запускается",
  RECORDING: "Идёт запись",
  STOPPING: "Останавливается",
  FINALIZING: "Сохраняется",
  COMPLETED: "Завершена",
  FAILED: "Ошибка",
  RECOVERY_REQUIRED: "Нужно восстановить",
};

/** Labels of the job statuses (domain/jobs/job). A waiting status is shown as its reason. */
export const jobStatusLabels: Readonly<Record<string, string>> = {
  QUEUED: "В очереди",
  RUNNING: "Выполняется",
  COMPLETED: "Готово",
  RETRY_WAIT: "Ждёт повторной попытки",
  CANCELLED: "Отменена",
  FAILED: "Ошибка",
  WAITING_FOR_NETWORK: "Ждёт интернет",
  WAITING_FOR_QUOTA: "Минуты обработки закончились",
  WAITING_FOR_POWER: "Ждёт зарядку",
};

/** The waiting statuses that must each have their own reason text (AC of TASK-023). */
export const waitingStatuses = ["WAITING_FOR_POWER", "WAITING_FOR_NETWORK", "WAITING_FOR_QUOTA"] as const;

export const jobTypeLabels: Readonly<Record<string, string>> = {
  stt: "Распознавание речи",
  summarize: "Конспект",
  extract_homework: "Поиск домашнего задания",
};

function lookup(labels: Readonly<Record<string, string>>, key: string): string {
  return Object.hasOwn(labels, key) ? labels[key] : key;
}

/** Label of a recording status; an unknown status is shown as is. */
export function recordingStatusLabel(status: string): string {
  return lookup(recordingStatusLabels, status);
}

/** Label of a job status (the reason for a waiting job); an unknown status is shown as is. */
export function jobStatusLabel(status: string): string {
  return lookup(jobStatusLabels, status);
}

/** Label of a job type; an unknown type is shown as is. */
export function jobTypeLabel(type: string): string {
  return lookup(jobTypeLabels, type);
}

// The terminal statuses of the domain (recording.IsTerminal, job.IsTerminal). Only the display grouping uses them: a
// finished row goes to the folded list, a row the core offers an action for always stays in view.
const finishedRecordingStatuses: ReadonlySet<string> = new Set(["COMPLETED", "FAILED"]);
const finishedJobStatuses: ReadonlySet<string> = new Set(["COMPLETED", "FAILED", "CANCELLED"]);

export function isFinishedRecording(r: { status: string; stoppable: boolean }): boolean {
  return !r.stoppable && finishedRecordingStatuses.has(r.status);
}

export function isFinishedJob(j: { status: string; runNowOffered: boolean }): boolean {
  return !j.runNowOffered && finishedJobStatuses.has(j.status);
}

/** Splits a list into the rows still in progress and the finished ones, keeping the order. */
export function splitFinished<T>(items: readonly T[], finished: (item: T) => boolean): { active: T[]; finished: T[] } {
  const out = { active: [] as T[], finished: [] as T[] };
  for (const item of items) {
    (finished(item) ? out.finished : out.active).push(item);
  }
  return out;
}

/** Formats seconds as h:mm:ss; negative or non-finite input is "—". */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "—";
  }
  const total = Math.floor(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

/**
 * Duration cell of a recording: the final duration when known, otherwise for a running recording the time elapsed
 * since startedAt by the client clock `now` (ms since epoch; clock skew is clamped to 0), otherwise "—".
 */
export function recordingDurationText(
  r: { status: string; startedAt: string; durationSeconds: number; hasDuration: boolean },
  now: number,
): string {
  if (r.hasDuration) {
    return formatDuration(r.durationSeconds);
  }
  if (r.status !== "RECORDING" || r.startedAt === "") {
    return "—";
  }
  const started = new Date(r.startedAt).getTime();
  if (Number.isNaN(started) || !Number.isFinite(now)) {
    return "—";
  }
  return formatDuration(Math.max(0, (now - started) / 1000));
}

/** Formats an RFC 3339 time in local time; "" or an unparsable value is "—". */
export function formatTime(rfc3339: string): string {
  if (rfc3339 === "") {
    return "—";
  }
  const d = new Date(rfc3339);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString("ru-RU");
}

const actionErrors: Readonly<Record<string, string>> = {
  [errorMessages.unavailable]: "Записи и задачи сейчас недоступны.",
  [errorMessages.invalidId]: "Неверный идентификатор.",
  [errorMessages.notFound]: "Не найдено. Обновите список.",
  [errorMessages.recordingNotRunning]: "Запись не идёт в этом сеансе.",
  [errorMessages.cannotRunNow]: "Эту задачу сейчас нельзя запустить.",
  [errorMessages.actionFailed]: "Не удалось выполнить действие.",
};

const genericActionError = "Не удалось выполнить действие.";

const loadErrors: Readonly<Record<"recordings" | "jobs", string>> = {
  recordings: "Не удалось загрузить записи.",
  jobs: "Не удалось загрузить задачи.",
};

/** Russian text for a failed load of one list; the raw error text is not shown. */
export function loadErrorText(err: unknown, list: "recordings" | "jobs"): string {
  return errorMessage(err) === errorMessages.unavailable ? actionErrors[errorMessages.unavailable] : loadErrors[list];
}

/** Russian text for an error of an action; an unknown error gets a generic text (raw text is not shown). */
export function actionErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return Object.hasOwn(actionErrors, msg) ? actionErrors[msg] : genericActionError;
}

// Wails rejects a call with an Error-like object carrying the Go error text.
function errorMessage(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return msg;
}
