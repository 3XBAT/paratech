// One row of «Записи» per recording (TASK-076): the title, the time and the one status of the whole chain the
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
import type { LessonTab } from "./knowledgeView.js";
import type { PageId } from "./nav.js";
import type { ListState, ListStates } from "./listView.js";
import { jobStatusView, type StatusView } from "./statusPresentation.js";
import { waitingInline } from "./statusWords.js";
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
  summarizing: "Конспект",
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
  retry: waitingInline("retry"),
  power: waitingInline("power"),
  network: waitingInline("network"),
  quota: waitingInline("quota"),
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
  quota: "Продолжится, когда минуты обработки пополнятся. Запись сохранена.",
  // The chip already names the service; the hint only says what to do.
  serviceNotConfigured: "Укажите адрес и ключ в «Подключения» → «Конспекты и задания». Запись сохранена.",
  serviceUnavailable: "Повторится автоматически. Запись сохранена.",
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
  summary: "Дальше конспект.",
  lectureText: "Дальше текст лекции.",
};

// transcriptionCancelled is no error: it has its own quiet status (cancelledTexts).
export const errorTexts: Readonly<
  Record<Exclude<ChainErrorCode, "" | "silentRecording" | "noSpeech" | "transcriptionCancelled">, string>
> = {
  recordingFailed: "Запись не удалась.",
  transcriptionFailed: "Не удалось расшифровать запись.",
  summaryFailed: "Не удалось сделать конспект.",
  homeworkFailed: "Не удалось найти задание.",
  lectureTextFailed: "Не удалось составить текст лекции.",
};

/** A silent recording (TASK-082) has its own chip instead of «Ошибка»: nothing failed, the source played nothing. */
export const silentTexts = {
  label: "Запись без звука",
  hint: "В записи нет звука — скорее всего, был выбран не тот источник. Проверьте звук перед следующей парой.",
} as const;

/** A transcript without speech (TASK-110): the same calm chip, the sound check and no retry. */
export const noSpeechTexts = {
  label: "Запись без речи",
  hint: "В записи нет речи — скорее всего, выбран не тот источник звука.",
} as const;

/** A transcription the student cancelled (TASK-393): nothing failed, so a quiet status instead of «Ошибка». */
export const cancelledTexts = {
  label: "Расшифровка отменена",
  hint: "Запись сохранена.",
} as const;

export const actionLabels: Readonly<Record<Exclude<ChainAction, "">, string>> = {
  runNow: "Запустить сейчас",
  openModels: "Открыть модели речи",
  openConnections: "Открыть подключения",
  checkSound: "Проверить звук",
  retry: "Повторить",
  retranscribe: "Расшифровать снова",
};

/**
 * The fixes of a failed step under its hint, as text buttons (TASK-347): «Открыть подключения» when the LLM call failed
 * (the connection, the key or the quota may need a fix first), then «Повторить». The core decides what is offered.
 */
export interface RowFixes {
  /** «Открыть подключения» as a button in the status cell; false when the row already has it as its main button. */
  connections: boolean;
  retry: RowRetry | null;
}

export const fixTexts = {
  retry: "Повторить",
  connections: "Открыть подключения",
} as const;

/** The page a navigation action opens; the sound check is on «Диагностика». */
export const actionPages: Readonly<Record<Exclude<ChainAction, "" | "runNow" | "retry" | "retranscribe">, PageId>> = {
  openModels: "models",
  openConnections: "connections",
  checkSound: "diagnostics",
};

/** The core's action of the row besides Stop. */
export type RowAction =
  | { kind: "runNow"; jobId: string; label: string }
  | { kind: "retranscribe"; label: string }
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
}

/** The «Повторить» of a row: the work it starts and its name for the tooltip and screen readers. */
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
      if (r.errorCode === "noSpeech") {
        view.label = noSpeechTexts.label;
        view.kind = "wait";
        view.hint = noSpeechTexts.hint;
        break;
      }
      if (r.errorCode === "transcriptionCancelled") {
        view.label = cancelledTexts.label;
        view.kind = "neutral";
        view.icon = "stop";
        view.hint = cancelledTexts.hint;
        break;
      }
      view.hint = own(errorTexts, r.errorCode) ?? "";
      break;
  }
  return view;
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
  if (r.action === "retranscribe") {
    return { kind: "retranscribe", label: actionLabels.retranscribe };
  }
  const page = own(actionPages, r.action);
  return page === undefined ? null : { kind: "open", page, label: actionLabels[r.action as keyof typeof actionPages] };
}

/** «Повторить» after a fix of the connection: the core sets retryWork also with openConnections (TASK-086). */
export function rowSecondaryAction(r: Pick<RecordingItem, "action" | "retryWork">): RowAction | null {
  return r.action === "openConnections" ? retryAction(r.retryWork) : null;
}

/** The names of «Повторить» per work (TASK-096): the tooltip and the accessible name of the text button. */
export const retryLabels: Readonly<Record<Exclude<RetryWork, "">, string>> = {
  summary: "Повторить конспект",
  homework: "Повторить поиск заданий",
  lectureText: "Повторить текст лекции",
};

/** The retry the core offers, as the main action or after a fix of the connection. */
export function rowRetry(r: Pick<RecordingItem, "action" | "actionJobId" | "retryWork">): RowRetry | null {
  const a = r.action === "retry" ? rowAction(r) : rowSecondaryAction(r);
  return a?.kind === "retry" ? { work: a.work, label: own(retryLabels, a.work) ?? actionLabels.retry } : null;
}

/**
 * The text buttons under the hint of a failed row (TASK-347). «Открыть подключения» only for a failed chain whose
 * core action is that page, and not again when the row already shows it as its main button.
 */
export function rowFixes(
  r: Pick<RecordingItem, "stage" | "action" | "actionJobId" | "retryWork">,
  mainKey: RowItemKey | null,
): RowFixes {
  const connections = r.stage === "failed" && r.action === "openConnections" && mainKey !== "open";
  return { connections, retry: rowRetry(r) };
}

// The core's own notes (the capture switch) are Russian; its failure reasons and job errors are English for support.
// Format source: recording.withNote joins "<reason>; <note>" and values.CaptureSwitch.Note is the Russian note
// (desktop/internal/domain/recording).
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
  };
}

/** What a row button or «⋯» item does; runNow and open take the job or the page from the core's action. */
export type RowItemKey = "stop" | "runNow" | "retranscribe" | "open" | "lesson" | "summary" | "lectureText" | "transcript" | "redo";

export interface RowItem {
  key: RowItemKey;
  label: string;
}

export const rowItemLabels = {
  stop: "Остановить",
  /** The main button of a row with texts (TASK-352): the lesson screen on its own opening tab (no tab is named). */
  open: "Открыть",
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
 * The tab of the lesson screen a named text item forces (TASK-352); null for the items that name none: «lesson» (the main
 * «Открыть») lets the screen choose its opening tab, as every other entry point of the pair does.
 */
export function rowItemTab(key: RowItemKey): LessonTab | null {
  switch (key) {
    case "summary":
      return "summary";
    case "lectureText":
      return "lecture";
    case "transcript":
      return "transcript";
    default:
      return null;
  }
}

/**
 * The actions of a row (TASK-096). The main button is the first of: Stop, «Запустить сейчас», «Расшифровать снова»
 * (TASK-393: the way back after a cancel or a failed recognition must stay a visible word button, so it goes before the
 * texts, which stay in «⋯»), the texts, the core's fix link (models, connections, sound check) only when no text is
 * offered. The texts are one lesson screen (TASK-352): the
 * main one is «Открыть», with no tab named (the screen opens its own: «Мой конспект» with parts, else the summary, ...);
 * «⋯» keeps every named text as the choice of that tab. The rest goes to «⋯».
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
  const again = core?.kind === "retranscribe" ? item("retranscribe", core.label) : null;
  const open = core?.kind === "open" ? item("open", core.label) : null;
  const redo = offers.redo && r.canRedoSummary ? item("redo", rowItemLabels.redo) : null;
  const hasText = lecture !== null || sum !== null || transcript !== null;
  const lesson = hasText ? item("lesson", rowItemLabels.open) : null;
  const main = stop ?? runNow ?? again ?? lesson ?? open;
  const menu = [stop, runNow, again, lecture, sum, transcript, open, redo].filter((i): i is RowItem => i !== null && i !== main);
  return { main, menu };
}

/** The links of a row to the texts of the recording; the row uses only whether each exists (TASK-096). */
export interface RowLinks {
  /** «Открыть текст лекции», the main link once the lecture text is ready (TASK-090). */
  lectureText: boolean;
  summary: SummaryOffer;
  /** «Открыть расшифровку»: "full" without a lecture text, "source" with one. */
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
 * The tab a link to the lesson of a recording opens (TASK-352): the lecture text once it is ready, else the summary, else
 * the transcript; null when the recording offers none of them yet (the lesson is not in the base).
 */
export function rowLessonTab(r: Pick<RecordingItem, "stage" | "step" | "errorCode" | "lectureTextReady">): LessonTab | null {
  const links = rowLinks(r);
  if (links.lectureText) {
    return "lecture";
  }
  if (links.summary === "main") {
    return "summary";
  }
  return links.transcript === null ? null : "transcript";
}

/**
 * The «Открыть пару» links of an assignment (TASK-352): one per recording of its lesson that offers a text, in the order
 * of the list. The link names no tab (the lesson screen opens its own); rowLessonTab only decides that there is
 * something to open.
 */
export function pairLinks(
  recordings: readonly Pick<RecordingItem, "id" | "lessonId" | "startedAt" | "stage" | "step" | "errorCode" | "lectureTextReady">[],
  lessonId: string,
): { id: string; startedAt: string }[] {
  if (lessonId === "") {
    return [];
  }
  return recordings
    .filter((r) => r.lessonId === lessonId && rowLessonTab(r) !== null)
    .map((r) => ({ id: r.id, startedAt: r.startedAt }));
}

/**
 * A finished row goes to the folded «Завершённые» of «Записи»: the summary is ready or the chain failed. A row with
 * Stop or Run now stays in view, and so does one that offers «Расшифровать снова» (TASK-393: the student has just cancelled,
 * the way back must not fold away); a failed row keeps its link (models, connections, sound check) in the group.
 */
export function isFinishedRow(r: Pick<RecordingItem, "stage" | "stoppable" | "action">): boolean {
  return !r.stoppable && r.action !== "runNow" && r.action !== "retranscribe" && (r.stage === "done" || r.stage === "failed");
}

/** One section of the page: the rows in progress («Идут») and the finished ones («Завершённые (N)»), newest first. */
export interface PageSection<T> {
  active: T[];
  finished: T[];
}

/**
 * What «Записи» shows (TASK-098): the recordings (a recording with its whole chain) and the background processing
 * (drafts, emails, calendar sync and other jobs without a recording), each with its own groups.
 */
export interface PageGroups {
  recordings: PageSection<RecordingItem>;
  tasks: PageSection<JobItem>;
}

function timeOf(rfc3339: string): number {
  const t = new Date(rfc3339).getTime();
  return Number.isNaN(t) ? -Infinity : t;
}

// Newest first; Array.prototype.sort is stable, so equal times keep the core's order.
function newestFirst<T>(items: T[], time: (item: T) => string): T[] {
  return items.sort((a, b) => {
    const ta = timeOf(time(a));
    const tb = timeOf(time(b));
    return ta === tb ? 0 : tb > ta ? 1 : -1;
  });
}

const draftJobType = "homework_draft";

// a is newer than b: created later, then updated later, then the greater id, so the choice never depends on the order.
function newerJob(a: JobItem, b: JobItem): boolean {
  const ca = timeOf(a.createdAt);
  const cb = timeOf(b.createdAt);
  if (ca !== cb) {
    return ca > cb;
  }
  const ua = timeOf(a.updatedAt);
  const ub = timeOf(b.updatedAt);
  if (ua !== ub) {
    return ua > ub;
  }
  return a.id > b.id;
}

/**
 * Only the latest draft job of each assignment (TASK-098): an older finished one, also a failed one before a newer
 * done one, is not shown. An older draft still in progress (or offering Run now) stays, like every row in progress.
 * Other jobs and drafts without an assignment stay; the order is kept.
 */
export function latestDrafts(jobs: readonly JobItem[]): JobItem[] {
  const latest = new Map<string, JobItem>();
  for (const j of jobs) {
    if (j.type !== draftJobType || j.assignmentId === "") {
      continue;
    }
    const seen = latest.get(j.assignmentId);
    if (seen === undefined || newerJob(j, seen)) {
      latest.set(j.assignmentId, j);
    }
  }
  return jobs.filter(
    (j) => j.type !== draftJobType || j.assignmentId === "" || latest.get(j.assignmentId) === j || !isFinishedJob(j),
  );
}

/** Splits the page into its two sections; finished recordings sort by their start, finished jobs by their creation. */
export function pageGroups(recordings: readonly RecordingItem[], jobs: readonly JobItem[]): PageGroups {
  const rec = splitFinished(recordings, isFinishedRow);
  const job = splitFinished(latestDrafts(jobs), isFinishedJob);
  return {
    recordings: { active: rec.active, finished: newestFirst(rec.finished, (r) => r.startedAt) },
    tasks: { active: job.active, finished: newestFirst(job.finished, (j) => j.createdAt) },
  };
}

/** The empty «Записи» (TASK-347): where to start, and the one place that says the sound stays on this computer. */
export const emptyTexts = {
  text: "Здесь будут записи пар. Записать можно сейчас — кнопкой «Начать запись», или загрузить расписание, и paratech сам предложит запись в начале пары.",
  privacy: "Звук обрабатывается только на этом компьютере",
  schedule: "Загрузить расписание",
} as const;

/**
 * The buttons in the head of «Записи» (start, import) show with a list that has rows; an empty list offers them in its
 * empty state instead, one primary button on the screen. While the list loads they wait; a failed load keeps them.
 */
export function headActionsVisible(recordings: readonly unknown[] | null, loadFailed: boolean): boolean {
  return recordings === null ? loadFailed : recordings.length > 0;
}

/** «Фоновая обработка» (TASK-347): one folded block with the count; the jobs of a recording show on its own row. */
export const backgroundKey = "background";
export const backgroundTitle = "Фоновая обработка";
const foldedState: ListState = { collapsed: true, showAll: false };

/** The block starts folded; the state the user sets is kept across the polls. */
export function backgroundState(states: ListStates): ListState {
  return Object.hasOwn(states, backgroundKey) ? states[backgroundKey] : foldedState;
}

/** The block is shown only while there are jobs at all; an empty one stays out of the way. */
export function backgroundVisible(jobs: readonly unknown[]): boolean {
  return jobs.length > 0;
}

type JobLike = Parameters<typeof jobStatusView>[0];

const jobFailed = (j: JobLike) => jobStatusView(j).kind === "fail";

/**
 * The number in the title: the jobs that are not finished (queued, running, waiting) and the failed ones. The finished
 * ones are counted by their own «Завершённые (N)». 0 means the title has no number.
 */
export function backgroundCount(tasks: PageSection<JobLike>): number {
  return tasks.active.length + tasks.finished.filter(jobFailed).length;
}

/** The key of the inner «Завершённые» list of the block (see RecordingsPage.sectionGroups). */
export const tasksKey = "tasks";

/**
 * «С ошибкой: 2 — показать»: the text button that brings the failed jobs into view. Shown while at least one failed job
 * is hidden by the fold of the block or of its «Завершённые» (a failed job is a finished one); "" when none is hidden.
 */
export function backgroundFailedText(tasks: PageSection<JobLike>, background: ListState, finished: ListState): string {
  const failedActive = tasks.active.filter(jobFailed).length;
  const failedDone = tasks.finished.filter(jobFailed).length;
  const open = !background.collapsed;
  const hidden = (failedDone > 0 && !(open && !finished.collapsed)) || (failedActive > 0 && !open);
  return hidden ? `С ошибкой: ${failedActive + failedDone} — показать` : "";
}

/** The click on that button: the block and its «Завершённые» unfold, with every row (the failed one may be past the limit). */
export function backgroundShowFailed(states: ListStates): ListStates {
  return {
    ...states,
    [backgroundKey]: { collapsed: false, showAll: false },
    [tasksKey]: { collapsed: false, showAll: true },
  };
}

/** What a task row does: «Запустить сейчас» and «Открыть задание». */
export type JobItemKey = "runNow" | "assignment";

export interface JobRowItem {
  key: JobItemKey;
  label: string;
}

export const jobItemLabels: Readonly<Record<JobItemKey, string>> = {
  runNow: actionLabels.runNow,
  assignment: "Открыть задание",
};

/** The one main button of a task row and its «⋯» items, like a recording row (TASK-096, TASK-098). */
export function jobActions(
  j: Pick<JobItem, "runNowOffered" | "assignmentId">,
  offers: { assignment: boolean },
): { main: JobRowItem | null; menu: JobRowItem[] } {
  const items: JobRowItem[] = [];
  if (j.runNowOffered) {
    items.push({ key: "runNow", label: jobItemLabels.runNow });
  }
  if (offers.assignment && j.assignmentId !== "") {
    items.push({ key: "assignment", label: jobItemLabels.assignment });
  }
  return { main: items[0] ?? null, menu: items.slice(1) };
}

/** The chip's tooltip of a task row: the last error and the attempts, which the row itself does not show. */
export function jobDetails(j: Pick<JobItem, "lastError" | "attempts" | "maxAttempts">): string {
  const attempts = j.maxAttempts > 0 ? `Попыток: ${j.attempts} из ${j.maxAttempts}` : "";
  return [j.lastError.trim(), attempts].filter((s) => s !== "").join("\n");
}

/** The muted line of a task row: when it last changed, to the minute («Обновлена 17.09.2026, 10:12»). */
export function jobMeta(j: Pick<JobItem, "updatedAt">): string {
  const d = new Date(j.updatedAt);
  if (j.updatedAt === "" || Number.isNaN(d.getTime())) {
    return "";
  }
  const at = d.toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" });
  return `Обновлена ${at}`;
}

/** A recording is capturing: the page ticks its elapsed time every second. */
export function anyRecording(rows: readonly Pick<RecordingItem, "stage" | "status">[]): boolean {
  return rows.some((r) => r.stage === "recording" && r.status === "RECORDING");
}

/**
 * «В очереди: 2» for the status line: recordings and other jobs waiting for their turn; "" when none. No «задачи»
 * (glossary): the number is a count of things in the queue, whatever they are (TASK-348).
 */
export function queueLine(rows: readonly Pick<RecordingItem, "stage" | "queueReason">[], jobs: readonly { status: string }[]): string {
  const n =
    rows.filter((r) => r.stage === "queued" && r.queueReason === "inLine").length +
    jobs.filter((j) => j.status === "QUEUED").length;
  return n === 0 ? "" : `В очереди: ${n}`;
}
