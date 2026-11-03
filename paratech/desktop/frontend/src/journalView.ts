// View logic of «Журнал»: the Russian wording of journal lines, filters and the refusals of «Отменить». Pure, no React.
// The core sends ids and enums only; every phrase here is fixed, so a line never shows model text. A subject name is
// typed text and is rendered as a text node by the page.

import type { CallProblem, HistoryFilter, JournalLine, UndoView } from "./bindings/agent.js";
import { formatTime } from "./activityView.js";
import { waitingWords } from "./statusWords.js";

export const texts = {
  title: "Журнал",
  lead: "Что paratech сделал за последние дни и что обрабатывалось. Часть действий можно отменить.",
  empty: "За выбранный период ничего нет.",
  loading: "Загрузка…",
  loadFailed: "Не удалось загрузить журнал.",
  undo: "Отменить",
  undoBusy: "Отмена…",
  /** «Нельзя отменить: письмо уже отправлено»: the reason follows, in lower case. */
  cannotUndo: "Нельзя отменить",
  /** The reason when the core named none: the line still says why in plain words. */
  cannotUndoGeneric: "действие уже нельзя вернуть",
  undone: "Действие отменено.",
  undoFailed: "Не удалось отменить действие.",
  kindLabel: "Что показывать",
  subjectLabel: "Предмет",
  statusLabel: "Состояние",
  periodLabel: "Период",
  onlyUndoable: "Только отменяемые",
  moreFilters: "Ещё фильтры",
  noSubject: "Без предмета",
  /** Next to the load error: the way out. */
  retry: "Повторить",
} as const;

export type KindChoice = "" | "action" | "processing";

export const kindOptions: readonly { value: KindChoice; label: string }[] = [
  { value: "", label: "Всё" },
  { value: "action", label: "Действия" },
  { value: "processing", label: "Обработка" },
];

export const periodOptions: readonly { value: number; label: string }[] = [
  { value: 7, label: "7 дней" },
  { value: 30, label: "30 дней" },
  { value: 90, label: "90 дней" },
];

export const defaultDays = 30;

const statusLabels: Readonly<Record<string, string>> = {
  PENDING: "Выполняется",
  DONE: "Выполнено",
  FAILED: "Ошибка",
  UNKNOWN: "Итог неизвестен",
  UNDOING: "Отменяется",
  UNDONE: "Отменено",
  UNDO_REFUSED: "Отменить не удалось",
  PROPOSED: "Предложено",
  QUEUED: "В очереди",
  RUNNING: "Выполняется",
  COMPLETED: "Завершено",
  RETRY_WAIT: waitingWords.retry,
  CANCELLED: "Остановлено",
  WAITING_FOR_NETWORK: waitingWords.network,
  WAITING_FOR_QUOTA: waitingWords.quota,
  WAITING_FOR_POWER: waitingWords.power,
};

/** The states the status filter offers: "" is any. */
export const statusOptions: readonly { value: string; label: string }[] = [
  { value: "", label: "Любое" },
  ...["DONE", "COMPLETED", "UNDONE", "UNDO_REFUSED", "FAILED", "RUNNING", "QUEUED"].map((value) => ({
    value,
    // RUNNING matches jobs only; PENDING actions are «Выполняется» too, so the filter says which it means.
    label: value === "RUNNING" ? "Обработка идёт" : statusLabels[value],
  })),
];

const toolLabels: Readonly<Record<string, string>> = {
  create_assignment: "Добавлено задание",
  create_subject: "Создан предмет",
  create_folder: "Создана папка",
  place_upload: "Файл положен в папку",
  link_material: "Материал привязан к паре",
  set_lesson_info: "Изменены данные пары",
  make_summary: "Запущен конспект",
  make_lecture_text: "Запущен текст лекции",
  find_homework: "Поиск заданий в записи пары",
  generate_draft: "Запущено решение задания",
  edit_due: "Изменён срок задания",
  triage_assignment: "Задание разобрано",
  write_work: "Запущена работа по заданию",
  propose_deadline: "Предложен срок в календарь",
  propose_send: "Предложена отправка письма",
  propose_delete_recording: "Предложено удаление записи",
};

const jobLabels: Readonly<Record<string, string>> = {
  stt: "Расшифровка",
  summarize: "Конспект",
  extract_homework: "Поиск заданий",
  lecture_text: "Текст лекции",
  homework_draft: "Решение задания",
  send_email: "Отправка письма",
};

/** Why Undo is refused, in lower case: the line reads «Нельзя отменить: <reason>». */
const noUndoRefusals: Readonly<Record<string, string>> = {
  changed: "изменено после этого действия",
  result_ready: "результат уже готов",
  cannot_stop: "задачу уже не остановить",
  sent: "письмо уже отправлено",
};

/** Lines without «Отменить» that are no refusal (nothing to undo, or not up to the student): the state, in plain words. */
const noUndoStates: Readonly<Record<string, string>> = {
  running: "Выполняется",
  failed: "Не выполнено — отменять нечего",
  unknown: "Итог неизвестен",
  undone: "Уже отменено",
  irreversible: "Решение остаётся за вами",
};

/**
 * What a line without «Отменить» says in its place; "" says nothing. A refusal reads «Нельзя отменить: <reason>»; a
 * line the core marks cannotUndo without a reason it knows still says why, in general words.
 */
export function noUndoText(noUndo: string, cannotUndo: boolean): string {
  if (Object.hasOwn(noUndoRefusals, noUndo)) {
    return `${texts.cannotUndo}: ${noUndoRefusals[noUndo]}`;
  }
  if (Object.hasOwn(noUndoStates, noUndo)) {
    return noUndoStates[noUndo];
  }
  return cannotUndo ? `${texts.cannotUndo}: ${texts.cannotUndoGeneric}` : "";
}

const busyText = "Действие уже меняется, попробуйте ещё раз.";

/** The refusals of Undo as the Go binding names them. */
const refusalTexts: Readonly<Record<string, string>> = {
  changed: "Изменено после — отменить нельзя.",
  result_ready: "Результат уже готов — отменить нельзя.",
  cannot_stop: "Задачу сейчас не остановить.",
  name_taken: "Вернуть нельзя: предмет или папка с таким названием уже есть.",
  not_found: "Запись журнала не найдена: обновите список.",
  not_undoable: "Это действие отменить нельзя.",
  busy: busyText,
};

const problemTexts: Readonly<Record<string, string>> = {
  invalid: "Неверный идентификатор: обновите список.",
  unavailable: "Пока недоступно.",
  closed: "Приложение закрывается.",
  busy: busyText,
  failed: texts.undoFailed,
};

/** The line under the journal after «Отменить»: the outcome in words; ok is true when the action was undone. */
export function undoOutcome(v: UndoView): { ok: boolean; text: string } {
  if (v.problem !== "") {
    return { ok: false, text: problemTexts[v.problem] ?? texts.undoFailed };
  }
  if (v.refusal !== "") {
    return { ok: false, text: refusalTexts[v.refusal] ?? texts.undoFailed };
  }
  if (v.status === "UNDONE") {
    return { ok: true, text: texts.undone };
  }
  return { ok: false, text: texts.undoFailed };
}

/** The wording of a problem of a call (shared with the panel). */
export function problemText(p: CallProblem): string {
  return p === "" ? "" : (problemTexts[p] ?? texts.undoFailed);
}

export function refusalText(refusal: string): string {
  return refusalTexts[refusal] ?? texts.undoFailed;
}

/** What the filter form holds. */
export interface FilterState {
  kind: KindChoice;
  subjectId: string;
  status: string;
  days: number;
  undoable: boolean;
}

export const initialFilter: FilterState = { kind: "", subjectId: "", status: "", days: defaultDays, undoable: false };

/** The binding's filter for the form (processing lines have no subject, so a subject filter leaves actions only). */
export function toFilter(f: FilterState): HistoryFilter {
  return {
    kind: f.kind,
    subjectId: f.subjectId,
    status: f.status,
    days: f.days,
    undoable: f.undoable,
  };
}

export interface LineModel {
  key: string;
  /** "action" or "processing". */
  kind: string;
  title: string;
  /** Plain text of the state; "" when unknown. */
  status: string;
  when: string;
  /** The typed name of the subject; "" for none. */
  subject: string;
  /** The student's chat title of an action line (plain text); "" for none. */
  chat: string;
  undoActionId: string;
  /** The id of an action line whether or not it can be undone now (where its outcome shows); "" for a processing line. */
  actionId: string;
  /** The core marks the line as one that cannot be undone. */
  cannotUndo: boolean;
  /** What stands in place of «Отменить»: «Нельзя отменить: <reason>» or a plain state ("" when it is offered or nothing is to be said). */
  reason: string;
}

export const groupTitles = { now: "Сейчас", today: "Сегодня", noDate: "Без даты" } as const;

// The states of a line that is still going on: the core has not finished it yet.
const inProgress: ReadonlySet<string> = new Set([
  "PENDING",
  "UNDOING",
  "QUEUED",
  "RUNNING",
  "RETRY_WAIT",
  "WAITING_FOR_NETWORK",
  "WAITING_FOR_QUOTA",
  "WAITING_FOR_POWER",
]);

const monthsGen = [
  "января",
  "февраля",
  "марта",
  "апреля",
  "мая",
  "июня",
  "июля",
  "августа",
  "сентября",
  "октября",
  "ноября",
  "декабря",
] as const;

export interface JournalGroup {
  key: string;
  title: string;
  /** The lines in the order the core sent them (newest first), with their index in the whole list. */
  items: { line: JournalLine; index: number }[];
}

/**
 * Splits the lines into «Сейчас» (still going on), «Сегодня» and one group per earlier day, in local time. The order of
 * the lines is kept; «Сейчас» comes first, the days follow in the order they first appear, «Без даты» is last.
 */
export function groupLines(lines: readonly JournalLine[], now: Date): JournalGroup[] {
  const groups = new Map<string, JournalGroup>();
  const put = (key: string, title: string, line: JournalLine, index: number) => {
    const g = groups.get(key) ?? { key, title, items: [] };
    g.items.push({ line, index });
    groups.set(key, g);
  };
  lines.forEach((line, index) => {
    if (inProgress.has(line.status)) {
      put("now", groupTitles.now, line, index);
      return;
    }
    const at = new Date(line.at);
    if (line.at === "" || Number.isNaN(at.getTime())) {
      put("none", groupTitles.noDate, line, index);
      return;
    }
    const day = `${at.getFullYear()}-${at.getMonth()}-${at.getDate()}`;
    if (at.getFullYear() === now.getFullYear() && at.getMonth() === now.getMonth() && at.getDate() === now.getDate()) {
      put("today", groupTitles.today, line, index);
      return;
    }
    const year = at.getFullYear() === now.getFullYear() ? "" : ` ${at.getFullYear()}`;
    put(day, `${at.getDate()} ${monthsGen[at.getMonth()]}${year}`, line, index);
  });
  const order = (g: JournalGroup) => (g.key === "now" ? 0 : g.key === "none" ? 2 : 1);
  return [...groups.values()].sort((a, b) => order(a) - order(b));
}

/** "14:05": the day is already in the heading of a dated group. */
function clockTime(rfc3339: string): string {
  const d = new Date(rfc3339);
  return Number.isNaN(d.getTime()) ? formatTime(rfc3339) : d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
}

/** Builds the line from the ids of the core; subjects maps ids to names. timeOnly drops the date (dated groups). */
export function lineModel(l: JournalLine, index: number, subjects: ReadonlyMap<string, string>, timeOnly = false): LineModel {
  const processing = l.kind === "processing";
  const title = processing ? (jobLabels[l.jobType] ?? "Обработка") : (toolLabels[l.tool] ?? "Действие");
  const key = processing ? `job:${l.jobId}:${index}` : `act:${l.actionId}:${index}`;
  const undoable = !processing && l.undoable && l.actionId !== "";
  return {
    key,
    kind: processing ? "processing" : "action",
    title,
    status: statusLabels[l.status] ?? "",
    when: timeOnly ? clockTime(l.at) : formatTime(l.at),
    subject: subjects.get(l.subjectId) ?? "",
    chat: processing ? "" : (l.chatTitle ?? "").trim(),
    undoActionId: undoable ? l.actionId : "",
    actionId: processing ? "" : l.actionId,
    cannotUndo: l.cannotUndo,
    reason: undoable ? "" : noUndoText(l.noUndo, l.cannotUndo),
  };
}

/** The outcome of the last «Отменить», kept for the row of that action. */
export interface UndoOutcome {
  actionId: string;
  ok: boolean;
  text: string;
}

/** Whether the row of the action is in the list: if not (a filter dropped it), the page says the outcome itself. */
export function outcomeHasRow(lines: readonly JournalLine[], actionId: string): boolean {
  return lines.some((l) => l.kind !== "processing" && l.actionId === actionId);
}

/** How many of the hidden filters («Что показывать», «Предмет», «Состояние») are not at their default. */
export function extraFilterCount(f: FilterState): number {
  return [f.kind !== initialFilter.kind, f.subjectId !== initialFilter.subjectId, f.status !== initialFilter.status].filter(
    Boolean,
  ).length;
}
