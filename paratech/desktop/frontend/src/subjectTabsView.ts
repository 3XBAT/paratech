// View logic of the subject page (TASK-353, ADR-0034 §2): the tabs «Пары · Файлы · Задания», which one opens by default,
// the lessons of a subject from the knowledge base, the rows of its open assignments, and the focus after «Добавить
// файл». Pure, no React. Titles and names are the student's or the model's text: the page renders them as text nodes.

import type { KnowledgeSubject } from "./bindings/knowledgeBase.js";
import type { LibrarySubject, SubjectAssignment } from "./bindings/library.js";
import { dayMonth, type DaysChip, daysChip, weekdayOf } from "./libraryView.js";

export type SubjectTab = "pairs" | "files" | "tasks";

/** The tabs in the order of the bar. */
export const subjectTabs: readonly SubjectTab[] = ["pairs", "files", "tasks"];

export const tabTexts = {
  bar: "Разделы предмета",
  pairs: "Пары",
  files: "Файлы",
  tasks: "Задания",
  loading: "Загрузка…",
} as const;

export const headTexts = {
  tickets: "Билеты к экзамену",
  more: "Ещё действия с предметом",
  moreTitle: "Ещё",
  busy: "Идёт другое действие: дождитесь его окончания.",
} as const;

export const pairsTexts = {
  empty: "Записей по предмету пока нет.",
  export: "Сохранить все конспекты одним файлом",
  exporting: "Сохранение…",
  exportHint: "Сохранить все пары предмета в один файл .md — по датам, с конспектом и текстом лекции.",
  exportEmpty: "Экспортировать нечего: у предмета пока нет пар с конспектом.",
  more: "Ещё действия с парами",
} as const;

export const tasksTexts = {
  add: "Добавить задание",
  empty: "Открытых заданий по предмету нет.",
  overdue: "Просрочено",
  noDue: "Без срока",
  failed: "Не удалось загрузить задания предмета.",
  list: "Открытые задания",
} as const;

/** The tab named by the route; "" for none (or a value no tab has): the page decides. */
export function validTab(raw: string): SubjectTab | "" {
  return subjectTabs.find((t) => t === raw) ?? "";
}

/** «Пары · 3» / «Задания · 3»: the number only when there is something; the page draws it as `num` text. */
export function tabSuffix(count: number): string {
  return count > 0 ? ` · ${count}` : "";
}

/** The state of reading the lessons of the subject (see SubjectTabs). */
export interface PairsRead {
  status: "loading" | "ready" | "failed";
  /** The rows of the tab (recorded lessons and the student's notes); 0 unless ready. */
  lessons: number;
}

/**
 * The tab a subject opens on when the route names none: «Пары» when the subject has lessons, else «Файлы» (also after a
 * failed read: the files do not depend on it). "" while the read is on, so nothing is guessed. The page asks once per
 * opened subject and puts the answer into the route, so a tab the student chose is never flipped.
 */
export function defaultTab(read: PairsRead): SubjectTab | "" {
  if (read.status === "loading") {
    return "";
  }
  return read.status === "ready" && read.lessons > 0 ? "pairs" : "files";
}

/** The knowledge-base group of a library subject (by `subjectId`, not by a key built here); null: no lessons, no notes. */
export function knowledgeEntryFor(list: readonly KnowledgeSubject[], subjectId: string): KnowledgeSubject | null {
  if (subjectId === "") {
    return null;
  }
  return list.find((s) => s.subjectId === subjectId) ?? null;
}

/**
 * The subject «Добавить конспект прошедшей пары» files its note under. A subject without lessons and notes has no group in
 * the knowledge base yet: the library subject stands in (the key only has to be unique, the core files by `subjectId`).
 */
export function pairsSubject(entry: KnowledgeSubject | null, subject: Pick<LibrarySubject, "id" | "name">): KnowledgeSubject {
  return entry ?? { key: `library:${subject.id}`, name: subject.name, lessons: 0, latest: "", noSubject: false, subjectId: subject.id };
}

/** The «⋯» item of the tab «Пары»: off with a visible reason when there is nothing to save. */
export function exportItem(
  state: { loaded: boolean; hasEntry: boolean; nothingToExport: boolean; exporting: boolean },
): { disabled: boolean; hint: string } {
  if (state.exporting || !state.loaded) {
    return { disabled: true, hint: "" };
  }
  return state.hasEntry && !state.nothingToExport ? { disabled: false, hint: "" } : { disabled: true, hint: pairsTexts.exportEmpty };
}

// ---- «Задания» ----

export interface TaskRow {
  id: string;
  title: string;
  /** "" without a deadline. */
  weekday: string;
  day: string;
  /** null: no deadline (the row says «Без срока» in quiet text). */
  chip: DaysChip | null;
}

/** The chip of a deadline: overdue is the warning look, the rest as in the lists of «Сегодня» (today, tomorrow, N дней). */
export function taskChip(a: Pick<SubjectAssignment, "due" | "daysLeft">): DaysChip | null {
  if (a.due === "") {
    return null;
  }
  return a.daysLeft < 0 ? { kind: "fail", label: tasksTexts.overdue } : daysChip(a.daysLeft);
}

/** The rows in the order the core sent them (by deadline, overdue first, undated last): the page does not sort. */
export function taskRows(list: readonly SubjectAssignment[]): TaskRow[] {
  return list.map((a) => ({
    id: a.assignmentId,
    title: a.title,
    weekday: a.due === "" ? "" : weekdayOf(a.due),
    day: a.due === "" ? "" : dayMonth(a.due),
    chip: taskChip(a),
  }));
}

/** The id of the button of a row; the focus goes to it after «Добавить задание». */
export function taskRowId(assignmentId: string): string {
  return `task-open-${assignmentId}`;
}

export const addTaskId = "subject-add-task";

// ---- The focus after «Добавить файл» ----

/** «Добавить файл» of the tab «Файлы». */
export const addFileId = "subject-add-file";

/**
 * The «+» of a folder, in its section header or, while the folder is empty, in the line «Пусто: …». A folder has exactly
 * one of the two at a time, so one id finds the button wherever the folder is drawn after its first file came.
 */
export function folderAddId(folderId: string): string {
  return `folder-add-${folderId}`;
}

/**
 * The focus goes back to the button that started the add only when it is lost (on the page body) or still on that button:
 * a student who has moved on to something else keeps the place they chose.
 */
export function refocusAllowed(active: { id: string; isBody: boolean } | null, targetId: string): boolean {
  return active === null || active.isBody || active.id === targetId;
}
