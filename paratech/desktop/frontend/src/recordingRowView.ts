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
  RetryWork,
} from "./bindings/activity.js";
import { formatDuration, formatTime, isFinishedJob, recordingDurationText, splitFinished } from "./activityView.js";
import { remainingText } from "./durationEstimate.js";
import type { PageId } from "./nav.js";
import { plural } from "./plural.js";
import type { StatusView } from "./statusPresentation.js";
import { rowSummaryOffer, type SummaryOffer } from "./summaryView.js";
import { rowOffersTranscript } from "./transcriptView.js";
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
  lectureText: "Текст лекции…",
  done: "Конспект готов",
  failed: "Ошибка",
};

const stageLooks: Readonly<Record<ChainStage, Look>> = {
  recording: { kind: "rec", icon: null },
  saving: { kind: "run", icon: "clock" },
  queued: { kind: "neutral", icon: "clock" },
  transcribing: { kind: "run", icon: "wave" },
  summarizing: { kind: "run", icon: "doc" },
  lectureText: { kind: "run", icon: "doc" },
  done: { kind: "ok", icon: "ok" },
  failed: { kind: "fail", icon: "warn" },
};

const stageHints: Readonly<Record<ChainStage, string>> = {
  recording: "Окно можно закрыть — запись продолжится.",
  saving: "Несколько секунд.",
  queued: "",
  transcribing: "",
  summarizing: "Обычно до минуты.",
  lectureText: "",
  done: "",
  failed: "",
};

/** The saving stage of a recording cut off by a crash says what happens to it. */
export const recoveryHint = "Запись прервалась. Сохранённая часть восстановится при следующем запуске.";

/** Reasons whose chip is its own text, not «В очереди · …»: the acceptance criterion of TASK-083 quotes it. */
type OwnLabelReason = "serviceNotConfigured" | "serviceUnavailable";

/** Shown in the chip after «В очереди». */
export const queueReasonTexts: Readonly<Record<Exclude<QueueReason, "" | OwnLabelReason>, string>> = {
  inLine: "ждёт очереди",
  retry: "повтор позже",
  power: "ждёт питания от сети",
  network: "нет сети",
  quota: "лимит исчерпан",
};

/** The whole chip for the reasons that replace «В очереди · …». */
export const queueReasonLabels: Readonly<Record<OwnLabelReason, string>> = {
  serviceNotConfigured: "Сервис конспектов не настроен",
  serviceUnavailable: "Сервис конспектов недоступен",
};

/** What happens next, under the chip. */
export const queueReasonHints: Readonly<Record<Exclude<QueueReason, "">, string>> = {
  inLine: "Начнётся после текущей задачи.",
  retry: "Не получилось, повторится само.",
  power: "Ноутбук на батарее. Начнётся само после подключения питания.",
  network: "Продолжится само, как только появится сеть.",
  quota: "Лимит обработки исчерпан. Продолжится, когда он пополнится. Запись сохранена.",
  // The chip already names the service; the hint only says what to do.
  serviceNotConfigured: "Укажите адрес и ключ в «Подключения» → «Конспекты и задания». Запись сохранена.",
  serviceUnavailable: "Повторим автоматически.",
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
  lectureText: "Дальше текст лекции.",
};

export const errorTexts: Readonly<Record<Exclude<ChainErrorCode, "" | "silentRecording">, string>> = {
  recordingFailed: "Запись не удалась.",
  transcriptionFailed: "Не удалось расшифровать запись.",
  summaryFailed: "Не удалось сделать краткое содержание.",
  homeworkFailed: "Не удалось найти домашнее задание.",
  lectureTextFailed: "Не удалось составить текст лекции.",
};

/** A silent recording (TASK-082) has its own chip instead of «Ошибка»: nothing failed, the source played nothing. */
export const silentTexts = {
  label: "Запись без звука",
  hint: "В записи нет звука — скорее всего, был выбран не тот источник. Проверьте звук перед следующим занятием.",
} as const;

export const actionLabels: Readonly<Record<Exclude<ChainAction, "">, string>> = {
  runNow: "Запустить сейчас",
  openModels: "Открыть модели речи",
  openConnections: "Открыть подключения",
  checkSound: "Проверить звук",
  retry: "Повторить",
};

/** What the hint of a failed step adds when the row offers «Повторить» (TASK-086). */
export const retryHints = {
  retry: "Нажмите «Повторить».",
  // The LLM call failed: the connection, the key or the quota may need a fix first.
  afterConnections: "Проверьте подключения и нажмите «Повторить».",
} as const;

/** The page a navigation action opens; the sound check is on «Диагностика». */
export const actionPages: Readonly<Record<Exclude<ChainAction, "" | "runNow" | "retry">, PageId>> = {
  openModels: "models",
  openConnections: "connections",
  checkSound: "diagnostics",
};

/** The core's action of the row besides Stop. */
export type RowAction =
  | { kind: "runNow"; jobId: string; label: string }
  | { kind: "open"; page: PageId; label: string }
  | { kind: "retry"; work: Exclude<RetryWork, "">; label: string };

/** What one recording row shows. */
export interface RecordingRowView {
  /** Plain text: the lesson title, or «Запись <дата, время>» for a recording without a lesson. */
  title: string;
  /** The title is the start time (tabular digits). */
  titleIsTime: boolean;
  /** «Матанализ · лекция · Иванов И. И.»: the known parts of the lesson; "" for none. Plain text. */
  lesson: string;
  /** Start time (when the title is the lesson) and the final duration; "" for none. */
  meta: string;
  status: StatusView;
  /** Transcription or lecture text share for the progress bar; null for none. */
  progress: number | null;
  /** «≈ 25 мин осталось» of a queued or running transcription (TASK-084); "" for none. */
  remaining: string;
  /** The core's notes meant for the user (Russian); plain text. Technical texts go to details only (TASK-096). */
  notes: string[];
  /** The tooltip of the chip: the problem of the recording and the last error of the job as they are; "" for none. */
  details: string;
  action: RowAction | null;
  /** The round «Повторить» next to the chip (TASK-086, TASK-096); null for none. */
  retry: RowRetry | null;
}

/** The round «Повторить» of a row: the work it starts and its name for the tooltip and screen readers. */
export interface RowRetry {
  work: Exclude<RetryWork, "">;
  label: string;
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

/** Russian names of the lesson kinds; other codes ("other", "unknown", new ones) are not shown. */
export const lessonKindTexts: Readonly<Record<string, string>> = {
  lecture: "лекция",
  seminar: "семинар",
  practice: "практика",
  lab: "лабораторная",
};

/** «Предмет · тип · преподаватель» from the known parts; the subject is skipped when it repeats the title. */
export function lessonLine(r: Pick<RecordingItem, "title" | "subject" | "kind" | "teacher">): string {
  const subject = r.subject.trim();
  const repeatsTitle = subject.toLowerCase() === r.title.trim().toLowerCase();
  const parts = [repeatsTitle ? "" : subject, own(lessonKindTexts, r.kind) ?? "", r.teacher.trim()];
  return parts.filter((s) => s !== "").join(" · ");
}

/** «3 из 12» of the lecture text's parts; "" while their number is unknown. */
export function partsText(r: Pick<RecordingItem, "partsDone" | "parts">): string {
  return r.parts > 0 ? `${r.partsDone} из ${r.parts}` : "";
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
      const ownLabel = own(queueReasonLabels, r.queueReason);
      const reasonLook = own(queueReasonLooks, r.queueReason);
      if ((reason !== undefined || ownLabel !== undefined) && reasonLook !== undefined) {
        view.label = ownLabel ?? `${view.label} · ${reason}`;
        view.kind = reasonLook.kind;
        view.icon = reasonLook.icon;
      }
      // A lecture text that waits halfway says how far it got.
      const parts = r.step === "lectureText" ? partsText(r) : "";
      const step = parts === "" ? own(stepTexts, r.step) : `Текст лекции: готово ${parts}.`;
      view.hint = [step, own(queueReasonHints, r.queueReason)].filter((s) => s).join(" ");
      break;
    }
    case "transcribing":
      view.value = r.progressPercent > 0 ? `${r.progressPercent} %` : "";
      break;
    case "lectureText":
      view.value = partsText(r);
      break;
    case "failed":
      if (r.errorCode === "silentRecording") {
        view.label = silentTexts.label;
        view.kind = "wait";
        view.hint = silentTexts.hint;
        break;
      }
      view.hint = [own(errorTexts, r.errorCode), failedRetryHint(r)].filter((s) => s).join(" ");
      break;
  }
  return view;
}

function failedRetryHint(r: Pick<RecordingItem, "action" | "retryWork">): string {
  if (r.retryWork === "") {
    return "";
  }
  if (r.action === "retry") {
    return retryHints.retry;
  }
  return r.action === "openConnections" ? retryHints.afterConnections : "";
}

function retryAction(work: RetryWork): RowAction | null {
  return work === "" ? null : { kind: "retry", work, label: actionLabels.retry };
}

/** The action the core offers for the row (Stop aside). */
export function rowAction(r: Pick<RecordingItem, "action" | "actionJobId" | "retryWork">): RowAction | null {
  if (r.action === "runNow") {
    return r.actionJobId === "" ? null : { kind: "runNow", jobId: r.actionJobId, label: actionLabels.runNow };
  }
  if (r.action === "retry") {
    return retryAction(r.retryWork);
  }
  const page = own(actionPages, r.action);
  return page === undefined ? null : { kind: "open", page, label: actionLabels[r.action as keyof typeof actionPages] };
}

/** «Повторить» after a fix of the connection: the core sets retryWork also with openConnections (TASK-086). */
export function rowSecondaryAction(r: Pick<RecordingItem, "action" | "retryWork">): RowAction | null {
  return r.action === "openConnections" ? retryAction(r.retryWork) : null;
}

/** The names of the round «Повторить» per work (TASK-096). */
export const retryLabels: Readonly<Record<Exclude<RetryWork, "">, string>> = {
  summary: "Повторить краткое содержание",
  homework: "Повторить поиск заданий",
  lectureText: "Повторить текст лекции",
};

/** The retry the core offers, as the main action or after a fix of the connection. */
export function rowRetry(r: Pick<RecordingItem, "action" | "actionJobId" | "retryWork">): RowRetry | null {
  const a = r.action === "retry" ? rowAction(r) : rowSecondaryAction(r);
  return a?.kind === "retry" ? { work: a.work, label: own(retryLabels, a.work) ?? actionLabels.retry } : null;
}

// The core's own notes (the capture switch) are Russian; its failure reasons and job errors are English for support.
const userText = /^[«А-Яа-яЁё]/;

/** The parts of the recording's problem meant for the user; a technical reason before "; " stays in the details. */
export function userNotes(problem: string): string[] {
  return problem
    .split("; ")
    .map((s) => s.trim())
    .filter((s, i, all) => userText.test(s) && all.indexOf(s) === i);
}

/** The chip's tooltip: the problem and the job error once each, one per line. */
export function detailsText(r: Pick<RecordingItem, "problem" | "jobError">): string {
  return [r.problem.trim(), r.jobError.trim()].filter((s, i, all) => s !== "" && all.indexOf(s) === i).join("\n");
}

/** Everything one row shows. */
export function recordingRowView(r: RecordingItem, now: number): RecordingRowView {
  const t = rowTitle(r);
  const meta = [t.isTime ? "" : formatTime(r.startedAt), r.hasDuration ? `Длительность ${formatDuration(r.durationSeconds)}` : ""];
  return {
    title: t.text,
    titleIsTime: t.isTime,
    lesson: lessonLine(r),
    meta: meta.filter((s) => s !== "").join(" · "),
    status: chainStatusView(r, now),
    progress:
      (r.stage === "transcribing" || r.stage === "lectureText") && r.progressPercent > 0
        ? Math.min(100, r.progressPercent)
        : null,
    remaining: remainingText(r),
    notes: userNotes(r.problem),
    details: detailsText(r),
    action: rowAction(r),
    retry: rowRetry(r),
  };
}

/** What a row button or «⋯» item does; runNow and open take the job or the page from the core's action. */
export type RowItemKey = "stop" | "runNow" | "open" | "summary" | "lectureText" | "transcript" | "redo";

export interface RowItem {
  key: RowItemKey;
  label: string;
}

export const rowItemLabels = {
  stop: "Остановить",
  summary: "Открыть конспект",
  lectureText: "Открыть текст лекции",
  transcript: "Открыть расшифровку",
  redo: "Сделать конспект заново",
} as const;

/** What the page lets the row do besides the core's actions. */
export interface RowOffers {
  /** Opening the summary, the lecture text and the transcript. */
  texts: boolean;
  /** «Сделать конспект заново» (asks first: it spends quota). */
  redo: boolean;
}

/** The one main button of a row and the items of its «⋯» menu, in menu order. */
export interface RowActions {
  main: RowItem | null;
  menu: RowItem[];
}

/**
 * The actions of a row (TASK-096). The main button is the first of: Stop, «Запустить сейчас», a ready summary, a ready
 * lecture text, the transcript; the core's fix link (models, connections, sound check) only when no text is offered.
 * The rest goes to «⋯».
 */
export function rowActions(
  r: Pick<RecordingItem, "stage" | "step" | "errorCode" | "lectureTextReady" | "stoppable" | "action" | "actionJobId" | "retryWork" | "canRedoSummary">,
  offers: RowOffers,
): RowActions {
  const core = rowAction(r);
  const links: RowLinks = offers.texts ? rowLinks(r) : { lectureText: false, summary: null, transcript: null };
  const summary = links.transcript === null ? null : rowSummaryOffer(r);
  const item = (key: RowItemKey, label: string): RowItem => ({ key, label });
  const stop = r.stoppable ? item("stop", rowItemLabels.stop) : null;
  const runNow = core?.kind === "runNow" ? item("runNow", core.label) : null;
  const lecture = links.lectureText ? item("lectureText", rowItemLabels.lectureText) : null;
  const sum = summary !== null ? item("summary", rowItemLabels.summary) : null;
  const transcript = links.transcript !== null ? item("transcript", rowItemLabels.transcript) : null;
  const open = core?.kind === "open" ? item("open", core.label) : null;
  const redo = offers.redo && r.canRedoSummary ? item("redo", rowItemLabels.redo) : null;
  const main = stop ?? runNow ?? (summary === "main" ? sum : null) ?? lecture ?? transcript ?? open;
  const menu = [stop, runNow, lecture, sum, transcript, open, redo].filter((i): i is RowItem => i !== null && i !== main);
  return { main, menu };
}

/** The links of a row to the texts of the recording. */
export interface RowLinks {
  /** «Открыть текст лекции», the main link once the lecture text is ready (TASK-090). */
  lectureText: boolean;
  summary: SummaryOffer;
  /** «Открыть расшифровку» without a lecture text; with one, the small «Исходная расшифровка». */
  transcript: "full" | "source" | null;
}

/** Which links a row offers, from the chain the list already has (no call per row). */
export function rowLinks(
  r: Pick<RecordingItem, "stage" | "step" | "errorCode" | "lectureTextReady">,
): RowLinks {
  if (!rowOffersTranscript(r)) {
    return { lectureText: false, summary: null, transcript: null };
  }
  const summary = rowSummaryOffer(r);
  if (!r.lectureTextReady) {
    return { lectureText: false, summary, transcript: "full" };
  }
  return { lectureText: true, summary: summary === null ? null : "extra", transcript: "source" };
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
