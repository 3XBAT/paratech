// «Удалить предмет» and «Вернуть» (TASK-289, ADR-0027). Pure, no React: the warning with its counts, the dialog and the
// restore line as models, the texts of every refusal. Names of subjects are typed or imported text: they are passed
// through untouched and the page renders them as text nodes.

import type { DiscardProblem, LibraryApi, LibraryMaterial, RestoreProblem, SubjectDiscard, SubjectRestore } from "./bindings/library.js";
import type { NoteView } from "./bindings/notes.js";
import { plural } from "./plural.js";

export const removalTexts = {
  /** The quiet button on the page of a subject. */
  action: "Удалить предмет",
  confirm: "Удалить",
  cancel: "Отмена",
  close: "Закрыть",
  retry: "Повторить",
  refresh: "Обновить",
  restore: "Вернуть",
  deleting: "Удаление…",
  restoring: "Возврат…",
  counting: "Подсчёт конспектов и файлов…",
  window: "Вернуть можно в течение 3 дней.",
  unknownParts: "Вместе с ним удалятся его конспекты и файлы.",
  changed: "Предмет изменился, откройте его ещё раз",
  gone: "Предмет уже удалён",
  failed: "Не удалось удалить предмет",
  /** The name a removed subject holds when the student creates a subject with it. */
  nameRemoved: "Предмет с таким названием удалён",
  restored: "Предмет возвращён",
  expired: "Предмет удалён окончательно, вернуть уже нельзя",
  restoreChanged: "Предмет изменился, вернуть его не удалось. Список предметов обновлён",
  restoreGone: "Предмет не найден: возможно, он уже удалён окончательно",
  restoreFailed: "Не удалось вернуть предмет",
} as const;

/** The attribute that tells the row of a subject in the tree (the focus goes there after «Вернуть»). */
export const subjectRowAttr = "data-subject-id";

// ---- The warning ----

export function notesText(n: number): string {
  return `${n} ${plural(n, "конспект", "конспекта", "конспектов")}`;
}

export function filesText(n: number): string {
  return `${n} ${plural(n, "файл", "файла", "файлов")}`;
}

/** What goes with the subject. */
export interface Counts {
  notes: number;
  files: number;
}

/** «2 конспекта и 1 файл»; a part that is 0 is left out, "" for nothing at all. */
export function countsPhrase(c: Counts): string {
  const parts: string[] = [];
  if (c.notes > 0) {
    parts.push(notesText(c.notes));
  }
  if (c.files > 0) {
    parts.push(filesText(c.files));
  }
  return parts.join(" и ");
}

/** «удалится 1 конспект» (one part in the singular), «удалятся 2 конспекта и 1 файл» otherwise. */
function removeVerb(c: Counts): string {
  const single = (c.notes > 0) !== (c.files > 0);
  const n = c.notes > 0 ? c.notes : c.files;
  return single && n % 10 === 1 && n % 100 !== 11 ? "удалится" : "удалятся";
}

export function removeQuestion(name: string): string {
  return `Удалить предмет «${name}»?`;
}

/**
 * The sentences under the question. `counts` null: the page does not know them (the notes or the files could not be
 * read), so the warning names no numbers but still says that both go.
 */
export function removeBody(counts: Counts | null): string {
  if (counts === null) {
    return `${removalTexts.unknownParts} ${removalTexts.window}`;
  }
  const phrase = countsPhrase(counts);
  return phrase === "" ? removalTexts.window : `Вместе с ним ${removeVerb(counts)} ${phrase}. ${removalTexts.window}`;
}

/** The whole warning as one text: «Удалить предмет «Физика»? Вместе с ним удалятся 2 конспекта и 1 файл. Вернуть …». */
export function removeWarning(name: string, counts: Counts | null): string {
  return `${removeQuestion(name)} ${removeBody(counts)}`;
}

/**
 * The counts the warning shows, from what the page already has: the active notes of the subject and its files that are no
 * part of such a note (the core counts the same way and answers with the real numbers after the call). null when either
 * list is unknown.
 */
export function knownCounts(notes: readonly NoteView[] | null, materials: readonly LibraryMaterial[] | null): Counts | null {
  if (notes === null || materials === null) {
    return null;
  }
  const active = notes.filter((n) => !n.removed);
  const inNotes = new Set(active.flatMap((n) => n.parts.map((p) => p.materialId)));
  return { notes: active.length, files: materials.filter((m) => !m.removed && !inNotes.has(m.id)).length };
}

// ---- The dialog ----

export type DiscardWay = "delete" | "refresh" | "close";

export interface DiscardProblemView {
  text: string;
  /** The button that is the way out. */
  way: DiscardWay;
  wayLabel: string;
  /** The second button («Закрыть»); false: the way is the only button. */
  canDismiss: boolean;
}

/** Why the delete did not happen, in plain words, and the button that leads on. */
export function discardProblemView(problem: Exclude<DiscardProblem, "">): DiscardProblemView {
  switch (problem) {
    case "changed":
      return { text: removalTexts.changed, way: "refresh", wayLabel: removalTexts.refresh, canDismiss: true };
    case "not_found":
      return { text: removalTexts.gone, way: "close", wayLabel: removalTexts.close, canDismiss: false };
    case "failed":
      return { text: removalTexts.failed, way: "delete", wayLabel: removalTexts.retry, canDismiss: true };
  }
}

export interface DialogInput {
  name: string;
  /** "loading" while the notes of the subject are being read. */
  counts: Counts | null | "loading";
  busy: boolean;
  problem: Exclude<DiscardProblem, ""> | "";
}

export interface DialogButton {
  label: string;
  disabled: boolean;
}

export interface DialogView {
  title: string;
  body: string;
  /** A small inline status (counting, deleting); "" for none. */
  status: string;
  /** The refusal; "" for none. */
  problem: string;
  /** The call runs: both buttons are off and Esc does nothing. */
  busy: boolean;
  /** The right button: «Удалить» (the only red one), or the way out of a refusal. */
  confirm: DialogButton & { action: DiscardWay; danger: boolean };
  /** The left button, which takes the focus; null when the way out is the only button. */
  cancel: DialogButton | null;
}

/** The dialog for any state: asking, counting, deleting, refused. */
export function dialogView(i: DialogInput): DialogView {
  const title = removeQuestion(i.name);
  if (i.problem !== "") {
    const p = discardProblemView(i.problem);
    return {
      title,
      body: removeBody(i.counts === "loading" ? null : i.counts),
      status: "",
      problem: p.text,
      busy: i.busy,
      confirm: { label: i.busy ? removalTexts.deleting : p.wayLabel, disabled: i.busy, action: p.way, danger: false },
      cancel: p.canDismiss ? { label: removalTexts.close, disabled: i.busy } : null,
    };
  }
  const loading = i.counts === "loading";
  return {
    title,
    body: i.counts === "loading" ? removalTexts.window : removeBody(i.counts),
    status: i.busy ? removalTexts.deleting : loading ? removalTexts.counting : "",
    problem: "",
    busy: i.busy,
    confirm: { label: i.busy ? removalTexts.deleting : removalTexts.confirm, disabled: i.busy || loading, action: "delete", danger: true },
    cancel: { label: removalTexts.cancel, disabled: i.busy },
  };
}

export type DiscardOutcome = { kind: "done"; discard: SubjectDiscard } | { kind: "problem"; problem: Exclude<DiscardProblem, ""> };

/** Runs «Удалить»; never throws. A reply with no problem but no usable version is a failure (nothing to restore with). */
export async function runDiscard(api: Pick<LibraryApi, "discardSubject">, subjectId: string, version: number): Promise<DiscardOutcome> {
  try {
    const d = await api.discardSubject(subjectId, version);
    return d.problem === "" ? { kind: "done", discard: d } : { kind: "problem", problem: d.problem };
  } catch {
    return { kind: "problem", problem: "failed" };
  }
}

// ---- «Вернуть» ----

export type RestoreState =
  | { phase: "idle" }
  | { phase: "busy" }
  | { phase: "done" }
  | { phase: "problem"; problem: Exclude<RestoreProblem, ""> };

export const idleRestore: RestoreState = { phase: "idle" };
export const busyRestore: RestoreState = { phase: "busy" };

/** The state after the call: restored, or why not. */
export function restoreState(r: SubjectRestore): RestoreState {
  return r.problem === "" ? { phase: "done" } : { phase: "problem", problem: r.problem };
}

/** The call of a restore: the state it ended in and the subject that is back (null unless it is `done`). */
export interface RestoreCall {
  state: RestoreState;
  subject: SubjectRestore["subject"];
}

/** Runs «Вернуть»; never throws. */
export async function runRestore(api: Pick<LibraryApi, "restoreSubject">, subjectId: string, version: number): Promise<RestoreCall> {
  try {
    const r = await api.restoreSubject(subjectId, version);
    return { state: restoreState(r), subject: r.subject };
  } catch {
    return { state: { phase: "problem", problem: "failed" }, subject: null };
  }
}

/** After which refusal the lists are out of date (the page reads them again). */
export function restoreNeedsReload(s: RestoreState): boolean {
  return s.phase === "problem" && (s.problem === "changed" || s.problem === "not_found" || s.problem === "expired");
}

export interface RestoreLineView {
  text: string;
  tone: "info" | "ok" | "fail";
  /** «Вернуть» or, after a failed call, «Повторить». */
  button: "restore" | "retry" | null;
  busy: boolean;
  /** «Закрыть» is offered (only where the line has no other way out). */
  close: boolean;
}

/** The line of a removed subject for any state; `idle` and `done` are the texts of the place the line is shown in. */
export function restoreLineView(idle: string, done: string, s: RestoreState, canClose: boolean): RestoreLineView {
  switch (s.phase) {
    case "idle":
      return { text: idle, tone: "info", button: "restore", busy: false, close: canClose };
    case "busy":
      return { text: idle, tone: "info", button: "restore", busy: true, close: canClose };
    case "done":
      return { text: done, tone: "ok", button: null, busy: false, close: false };
    case "problem":
      switch (s.problem) {
        case "expired":
          return { text: removalTexts.expired, tone: "info", button: null, busy: false, close: canClose };
        case "changed":
          return { text: removalTexts.restoreChanged, tone: "fail", button: null, busy: false, close: canClose };
        case "not_found":
          return { text: removalTexts.restoreGone, tone: "fail", button: null, busy: false, close: canClose };
        case "failed":
          return { text: removalTexts.restoreFailed, tone: "fail", button: "retry", busy: false, close: canClose };
      }
  }
}

/** «Предмет «Физика» удалён»: the line that replaces the list after «Удалить». */
export function removedLineText(name: string): string {
  return `Предмет «${name}» удалён`;
}

/** A skipped row of the schedule review whose name a removed subject holds (the name is "" when the row has none). */
export interface RemovedRow {
  key: string;
  name: string;
  subjectId: string;
  version: number;
}

/** The text of such a row before «Вернуть»: the name first when it is known. */
export function removedRowText(row: RemovedRow): string {
  return row.name === "" ? removalTexts.nameRemoved : `«${row.name}» · ${removalTexts.nameRemoved}`;
}

/** The text of the row once the subject is back. */
export function restoredRowText(row: RemovedRow): string {
  return row.name === "" ? removalTexts.restored : `«${row.name}» · ${removalTexts.restored}`;
}
