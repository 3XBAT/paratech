// One row of «Записи и задачи» per recording (TASK-076): the title, the time and the one status of the whole chain the
// Go core derives (stage, queue reason, error, action). Texts and looks only: the stage and the offered action are the
// core's (ARCH-44).

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import type {
  ChainAction,
  ChainErrorCode,
  ChainStage,
  ChainStep,
  JobItem,
  QueueReason,
  RecordingItem,
} from "./bindings/activity.js";
import { formatDuration, formatTime, isFinishedJob, recordingDurationText, splitFinished } from "./activityView.js";
import type { PageId } from "./nav.js";
import { plural } from "./plural.js";
import type { StatusView } from "./statusPresentation.js";
import type { IconName } from "./ui/Icon.js";
import type { ChipKind } from "./ui/StatusChip.js";

interface Look {
  kind: ChipKind;
  icon: IconName | null;
}

export const stageLabels: Readonly<Record<ChainStage, string>> = {
  recording: "Идёт запись",
  saving: "Сохраняется",
  queued: "В очереди",
  transcribing: "Расшифровка",
  summarizing: "Краткое содержание",
  done: "Конспект готов",
  failed: "Ошибка",
};

const stageLooks: Readonly<Record<ChainStage, Look>> = {
  recording: { kind: "rec", icon: null },
  saving: { kind: "run", icon: "clock" },
  queued: { kind: "neutral", icon: "clock" },
  transcribing: { kind: "run", icon: "wave" },
  summarizing: { kind: "run", icon: "doc" },
  done: { kind: "ok", icon: "ok" },
  failed: { kind: "fail", icon: "warn" },
};

const stageHints: Readonly<Record<ChainStage, string>> = {
  recording: "Окно можно закрыть — запись продолжится.",
  saving: "Несколько секунд.",
  queued: "",
  transcribing: "",
  summarizing: "Обычно до минуты.",
  done: "",
  failed: "",
};

/** The saving stage of a recording cut off by a crash says what happens to it. */
export const recoveryHint = "Запись прервалась. Сохранённая часть восстановится при следующем запуске.";

/** Shown in the chip after «В очереди». */
export const queueReasonTexts: Readonly<Record<Exclude<QueueReason, "">, string>> = {
  inLine: "ждёт очереди",
  retry: "повтор позже",
  power: "ждёт питания от сети",
  network: "нет сети",
  quota: "лимит исчерпан",
  serviceNotConfigured: "сервис не настроен",
  serviceUnavailable: "сервис недоступен",
};

/** What happens next, under the chip. */
export const queueReasonHints: Readonly<Record<Exclude<QueueReason, "">, string>> = {
  inLine: "Начнётся после текущей задачи.",
  retry: "Не получилось, повторится само.",
  power: "Ноутбук на батарее. Начнётся само после подключения питания.",
  network: "Продолжится само, как только появится сеть.",
  quota: "Лимит обработки исчерпан. Продолжится, когда он пополнится. Запись сохранена.",
  serviceNotConfigured:
    "Сервис конспектов не настроен. Укажите адрес и ключ в «Подключения» → «Конспекты и задания». Запись сохранена.",
  serviceUnavailable: "Сервис конспектов недоступен, повторим автоматически.",
};

const queueReasonLooks: Readonly<Record<Exclude<QueueReason, "">, Look>> = {
  inLine: { kind: "neutral", icon: "clock" },
  retry: { kind: "wait", icon: "clock" },
  power: { kind: "wait", icon: "plug" },
  network: { kind: "net", icon: "off" },
  quota: { kind: "wait", icon: "clock" },
  // Not configured needs the user; unavailable only waits.
  serviceNotConfigured: { kind: "wait", icon: "warn" },
  serviceUnavailable: { kind: "wait", icon: "clock" },
};

/** The step a queued chain waits with, as the start of its hint. */
export const stepTexts: Readonly<Record<Exclude<ChainStep, "">, string>> = {
  transcription: "Дальше расшифровка.",
  summary: "Дальше краткое содержание.",
};

export const errorTexts: Readonly<Record<Exclude<ChainErrorCode, "">, string>> = {
  recordingFailed: "Запись не удалась.",
  transcriptionFailed: "Не удалось расшифровать запись.",
  summaryFailed: "Не удалось сделать краткое содержание.",
  homeworkFailed: "Не удалось найти домашнее задание.",
};

export const actionLabels: Readonly<Record<Exclude<ChainAction, "">, string>> = {
  runNow: "Запустить сейчас",
  openModels: "Открыть модели речи",
  openConnections: "Открыть подключения",
  checkSound: "Проверить звук",
};

/** The page a navigation action opens; the sound check is on «Диагностика». */
export const actionPages: Readonly<Record<Exclude<ChainAction, "" | "runNow">, PageId>> = {
  openModels: "models",
  openConnections: "connections",
  checkSound: "diagnostics",
};

/** What the row's button does besides Stop. */
export type RowAction =
  | { kind: "runNow"; jobId: string; label: string }
  | { kind: "open"; page: PageId; label: string };

/** What one recording row shows. */
export interface RecordingRowView {
  /** Plain text: the lesson title, or «Запись <дата, время>» for a recording without a lesson. */
  title: string;
  /** The title is the start time (tabular digits). */
  titleIsTime: boolean;
  /** Start time (when the title is the lesson) and the final duration; "" for none. */
  meta: string;
  status: StatusView;
  /** Transcription share for the progress bar; null for none. */
  progress: number | null;
  /** The core's notes: the problem of the recording, the last error of the job; plain text. */
  notes: string[];
  action: RowAction | null;
}

function own<K extends string, V>(map: Readonly<Record<K, V>>, key: string): V | undefined {
  return Object.hasOwn(map, key) ? map[key as K] : undefined;
}

/** Title of the row: the lesson title, else «Запись <дата, время>» from the start. */
export function rowTitle(r: Pick<RecordingItem, "title" | "startedAt">): { text: string; isTime: boolean } {
  const title = r.title.trim();
  if (title !== "") {
    return { text: title, isTime: false };
  }
  const at = formatTime(r.startedAt);
  return { text: at === "—" ? "Запись" : `Запись ${at}`, isTime: true };
}

/** The chip, its hint and the progress of the chain; a running recording shows its elapsed time by the clock `now`. */
export function chainStatusView(r: RecordingItem, now: number): StatusView {
  const look = own(stageLooks, r.stage) ?? { kind: "neutral" as const, icon: null };
  const view: StatusView = {
    ...look,
    label: own(stageLabels, r.stage) ?? r.stage,
    value: "",
    hint: own(stageHints, r.stage) ?? "",
  };
  switch (r.stage) {
    case "recording":
      view.value = r.status === "RECORDING" ? recordingDurationText(r, now) : "";
      break;
    case "saving":
      if (r.status === "RECOVERY_REQUIRED") {
        view.kind = "wait";
        view.icon = "warn";
        view.hint = recoveryHint;
      }
      break;
    case "queued": {
      const reason = own(queueReasonTexts, r.queueReason);
      const reasonLook = own(queueReasonLooks, r.queueReason);
      if (reason !== undefined && reasonLook !== undefined) {
        view.label = `${view.label} · ${reason}`;
        view.kind = reasonLook.kind;
        view.icon = reasonLook.icon;
      }
      view.hint = [own(stepTexts, r.step), own(queueReasonHints, r.queueReason)].filter((s) => s).join(" ");
      break;
    }
    case "transcribing":
      view.value = r.progressPercent > 0 ? `${r.progressPercent} %` : "";
      break;
    case "failed":
      view.hint = own(errorTexts, r.errorCode) ?? "";
      break;
  }
  return view;
}

/** The action the core offers for the row (Stop aside). */
export function rowAction(r: Pick<RecordingItem, "action" | "actionJobId">): RowAction | null {
  if (r.action === "runNow") {
    return r.actionJobId === "" ? null : { kind: "runNow", jobId: r.actionJobId, label: actionLabels.runNow };
  }
  const page = own(actionPages, r.action);
  return page === undefined ? null : { kind: "open", page, label: actionLabels[r.action as keyof typeof actionPages] };
}

/** Everything one row shows. */
export function recordingRowView(r: RecordingItem, now: number): RecordingRowView {
  const t = rowTitle(r);
  const meta = [t.isTime ? "" : formatTime(r.startedAt), r.hasDuration ? `Длительность ${formatDuration(r.durationSeconds)}` : ""];
  return {
    title: t.text,
    titleIsTime: t.isTime,
    meta: meta.filter((s) => s !== "").join(" · "),
    status: chainStatusView(r, now),
    progress: r.stage === "transcribing" && r.progressPercent > 0 ? Math.min(100, r.progressPercent) : null,
    notes: [r.problem, r.jobError].filter((s, i, all) => s !== "" && all.indexOf(s) === i),
    action: rowAction(r),
  };
}

/**
 * A finished row goes to the one folded group «Завершённые»: the summary is ready or the chain failed. A row with
 * Stop or Run now stays in view; a failed row keeps its link (models, connections, sound check) in the group.
 */
export function isFinishedRow(r: Pick<RecordingItem, "stage" | "stoppable" | "action">): boolean {
  return !r.stoppable && r.action !== "runNow" && (r.stage === "done" || r.stage === "failed");
}

/** One row of the «Завершённые» group: a recording or one of the other jobs (homework draft, email). */
export type FinishedEntry = { kind: "recording"; item: RecordingItem } | { kind: "job"; item: JobItem };

/** What «Записи и задачи» shows: rows in progress, unfinished other jobs, and the one «Завершённые» group. */
export interface PageGroups {
  recordings: RecordingItem[];
  otherJobs: JobItem[];
  finished: FinishedEntry[];
}

// A recording sorts by its start, a job by its creation: a job made from a lesson's summary lands next to it.
function entryTime(e: FinishedEntry): number {
  const t = new Date(e.kind === "recording" ? e.item.startedAt : e.item.createdAt).getTime();
  return Number.isNaN(t) ? -Infinity : t;
}

/**
 * Splits the page into its groups. Finished recordings and finished other jobs share the one «Завершённые» group,
 * newest first, so a failed email or homework draft keeps its error in view.
 */
export function pageGroups(recordings: readonly RecordingItem[], jobs: readonly JobItem[]): PageGroups {
  const rec = splitFinished(recordings, isFinishedRow);
  const job = splitFinished(jobs, isFinishedJob);
  const finished: FinishedEntry[] = [
    ...rec.finished.map((item) => ({ kind: "recording" as const, item })),
    ...job.finished.map((item) => ({ kind: "job" as const, item })),
  ];
  // Array.prototype.sort is stable: equal times keep the core's order, recordings before jobs.
  finished.sort((a, b) => entryTime(b) - entryTime(a));
  return { recordings: rec.active, otherJobs: job.active, finished };
}

/** The key of a «Завершённые» row; recording and job ids may not collide. */
export function finishedKey(e: FinishedEntry): string {
  return `${e.kind}:${e.item.id}`;
}

/** A recording is capturing: the page ticks its elapsed time every second. */
export function anyRecording(rows: readonly Pick<RecordingItem, "stage" | "status">[]): boolean {
  return rows.some((r) => r.stage === "recording" && r.status === "RECORDING");
}

/** "В очереди 2 задачи" for the status line: rows and other jobs waiting for their turn; "" when none. */
export function queueLine(rows: readonly Pick<RecordingItem, "stage" | "queueReason">[], jobs: readonly { status: string }[]): string {
  const n =
    rows.filter((r) => r.stage === "queued" && r.queueReason === "inLine").length +
    jobs.filter((j) => j.status === "QUEUED").length;
  return n === 0 ? "" : `В очереди ${n} ${plural(n, "задача", "задачи", "задач")}`;
}
