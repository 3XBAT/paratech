// Homework page bindings: calls into the Go core (internal/input/wails/homework.go). The core decides what is
// offered (canMarkSent, canSendAgain, the send dialog's status) and writes every text; the page only shows them.
// Titles, bodies, instructions and descriptions may be model text: show them as plain text, never as HTML.
// Sending: exportDraft or exportDocx (shown version) -> prepareSend(format) -> the confirmation dialog -> confirmSend
// (with the prepared format) or cancelSend. Changing the format in the dialog is a new export and prepareSend.
// confirmSend is the only call that leads to an email; call it only from the dialog's confirm button (AC-14).

import { type LessonSource, lessonSource } from "../lessonLabel.js";
import { choiceArgs, parseRecipientFields, type RecipientFields, type TeacherChoice } from "./recipient.js";
import type { CallByName } from "./settings";

/** One assignment, newest first. Times are RFC 3339 in UTC, "" when unset. */
export interface AssignmentItem {
  id: string;
  lessonId: string;
  title: string;
  description: string;
  /** Deadline; "" when there is none. */
  due: string;
  userEdited: boolean;
  /** The stored version: deleteAssignment, restoreAssignment and editAssignment send it back. */
  version: number;
  updatedAt: string;
  /** The lesson (TASK-092): the shown subject and teacher with their source, the kind and the lesson start. */
  subject: string;
  subjectSource: LessonSource;
  /** The shown subject is the lesson's (schedule or recording): an empty subject in the edit form keeps showing it. */
  subjectFromLesson: boolean;
  /** "lecture", "seminar", "practice", "lab" or "". */
  kind: string;
  teacher: string;
  teacherSource: LessonSource;
  lessonDate: string;
  /** The student's correction, "" when none: the values of the correction form. */
  correctedSubject: string;
  correctedTeacher: string;
  /** «Выполнено» (ADR-0033 §9), marked at doneAt ("" while open). */
  done: boolean;
  doneAt: string;
}

/** The filter of listAssignments: «Открытые / Выполненные / Все» ("all": the open ones first). */
export type AssignmentState = "open" | "done" | "all";

/** "" = the call went through and outcomes say what happened to each assignment; the others come without outcomes. */
export type DoneProblem = "" | "invalid" | "unavailable";

/** What happened to one assignment; "unchanged" (already in that state) is not an error. */
export type DoneCode = "changed" | "unchanged" | "not_found" | "deleted" | "conflict" | "failed";

/** One assignment of markDone or restoreDone; done and version are the stored state for "changed" and "unchanged". */
export interface DoneOutcome {
  id: string;
  code: DoneCode;
  done: boolean;
  version: number;
}

/** One changed assignment with the version the call saved; «Отменить» sends it back as it is. */
export interface DonePrior {
  id: string;
  version: number;
}

/**
 * The reply of markDone and restoreDone: one outcome per distinct id in the order sent. «Отменить» is
 * restoreDone(priorDone, prior) with exactly these values, offered only when prior is not empty.
 */
export interface DoneResult {
  problem: DoneProblem;
  outcomes: DoneOutcome[];
  prior: DonePrior[];
  /** The state before the call, the one «Отменить» puts back. */
  priorDone: boolean;
}

/** How a draft version was made. */
export type VersionSource = "generated" | "edited";

/** One draft version in the history list. */
export interface VersionItem {
  number: number;
  source: VersionSource;
  /** The additional instructions of a generated version; "" otherwise. */
  instruction: string;
  latest: boolean;
  createdAt: string;
}

/** One draft version with its text. */
export interface VersionText {
  number: number;
  source: VersionSource;
  title: string;
  body: string;
  instruction: string;
  latest: boolean;
  createdAt: string;
}

/** An opened assignment: its versions in order, the latest text (null: no draft yet) and the running generation. */
export interface AssignmentDetail {
  assignment: AssignmentItem;
  versions: VersionItem[];
  latest: VersionText | null;
  /** The generation that is pending or running in the core (TASK-094); null: none. */
  generation: ActiveGeneration | null;
}

/** A pending or running generation of an assignment: its job (for generationStatus) and its state. */
export interface ActiveGeneration {
  jobId: string;
  state: GenerationState;
  message: string;
  percent: number;
}

/** The cheap state of a draft to poll: latestVersion larger than the shown one means a new version (0: no draft). */
export interface DraftStatus {
  latestVersion: number;
  generation: ActiveGeneration | null;
}

/** The generation job to follow with generationStatus. */
export interface GenerateStarted {
  jobId: string;
  base: number;
}

export type GenerationState = "generating" | "waitingNetwork" | "waitingQuota" | "failed" | "done";

/** The state of a generation; message is a fixed text for the waiting and failed states, "" otherwise. */
export interface GenerationStatus {
  state: GenerationState;
  message: string;
  /** The progress the job reported, 0..100 (100 when done). */
  percent: number;
}

/** The version written to the file a send attaches. */
export interface ExportResult {
  version: number;
  fileName: string;
}

/** The attachment of a send: the Word file or the text file. */
export type SendFormat = "docx" | "txt";

export type DocxSaveStatus = "saved" | "cancelled" | "failed";

/** «Сохранить в Word (.docx)»: the core's fixed text and the saved file's base name ("" unless saved). */
export interface DocxSaveResult {
  status: DocxSaveStatus;
  message: string;
  name: string;
}

/** "ready": the dialog shows recipient, subject and attachment and offers the confirmation; "rejected": message only. */
export type SendStatus = "ready" | "rejected";

/**
 * The content of the send confirmation dialog. The recipient fields (recipientName, role, subjectId, options) are
 * described in recipient.ts: a ready send with reason "choose_teacher" has no recipient yet and options to click.
 */
export interface SendPreparation extends RecipientFields {
  proposalId: string;
  status: SendStatus;
  /**
   * The core's reason code of a rejected send ("no_teacher_email" and "no_subject" among them); "choose_teacher" of a
   * ready send that asks «Кому отправить?»; "" otherwise. Compare with recipientReason, not as free text.
   */
  reason: string;
  message: string;
  /** The address «Кому» shows; "" while the student chooses. */
  recipient: string;
  subject: string;
  attachmentName: string;
  /** Bytes. */
  attachmentSize: number;
  /** The format this preparation is for; confirmSend sends it back. */
  attachmentFormat: SendFormat;
}

/** One email of an assignment, newest first. Status is the core's code: PENDING, SENDING, SENT, FAILED, UNKNOWN. */
export interface DeliveryItem {
  id: string;
  status: string;
  recipient: string;
  subject: string;
  attempts: number;
  lastError: string;
  confirmedAt: string;
  sentAt: string;
  createdAt: string;
  /** "Отправлено" is offered (an unknown outcome). */
  canMarkSent: boolean;
  /** "Отправить ещё раз" is offered (an unknown outcome); ask for confirmation first. */
  canSendAgain: boolean;
  /** The core's explanation of an unknown outcome; "" otherwise. */
  notice: string;
}

/**
 * The add-assignment form (ADR-0029). requestId is the form's token (crypto.randomUUID when the form opens; 1..64
 * characters of [A-Za-z0-9-]): sending the same form again stores one assignment. "" in subjectId, lessonId, date and
 * time means no value; date is YYYY-MM-DD, time HH:MM (no time: the end of that day).
 */
export interface CreateAssignmentRequest {
  requestId: string;
  title: string;
  subjectId: string;
  lessonId: string;
  date: string;
  time: string;
  note: string;
}

/** "" = created; the page owns the texts of the others. */
export type CreateProblem = "" | "invalid" | "subject_not_found" | "lesson_not_found" | "failed";

/** The input an "invalid" problem names; "" when the core does not know (or the problem is not "invalid"). */
export type CreateField = "" | "requestId" | "title" | "note" | "due" | "time";

/** assignment is set exactly when problem is "". */
export interface CreateAssignmentResult {
  problem: CreateProblem;
  field: CreateField;
  assignment: AssignmentItem | null;
}

/**
 * "" = done; the page owns the texts of the others. "expired": «Вернуть» after the restore window (ADR-0029
 * addendum 2), the assignment is purged. A reply the binding cannot read is "failed".
 */
export type ChangeProblem = "" | "in_use" | "changed" | "not_found" | "expired" | "failed";

/**
 * «Удалить» and «Вернуть»: assignment (with its new version) is set exactly when problem is "". stopped is true only
 * for a delete that stopped a running work or draft of the assignment.
 */
export interface ChangeAssignmentResult {
  problem: ChangeProblem;
  assignment: AssignmentItem | null;
  stopped: boolean;
}

/** What «Удалить» would do to the assignment's work; working: a turnkey work or a draft job is running. */
export interface DeleteImpact {
  working: boolean;
}

/**
 * The edit form of an assignment of any origin (ADR-0029). version is AssignmentItem.version the form showed. "" in
 * subjectId, note, date and time means no value; date is YYYY-MM-DD, time HH:MM (no time: the end of that day).
 */
export interface EditAssignmentRequest {
  id: string;
  version: number;
  title: string;
  note: string;
  subjectId: string;
  date: string;
  time: string;
}

/** "" = saved; the page owns the texts of the others. A reply the binding cannot read is "failed". */
export type EditProblem = "" | "invalid" | "subject_not_found" | "changed" | "deleted" | "not_found" | "failed";

/** The input an "invalid" problem names; "" when the core does not know (or the problem is not "invalid"). */
export type EditField = "" | "title" | "note" | "due" | "time";

/** assignment is set exactly when problem is "". */
export interface EditAssignmentResult {
  problem: EditProblem;
  field: EditField;
  assignment: AssignmentItem | null;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  listAssignments: "paratech/desktop/internal/input/wails.HomeworkService.ListAssignments",
  openAssignment: "paratech/desktop/internal/input/wails.HomeworkService.OpenAssignment",
  draftStatus: "paratech/desktop/internal/input/wails.HomeworkService.DraftStatus",
  correctLesson: "paratech/desktop/internal/input/wails.HomeworkService.CorrectLesson",
  createAssignment: "paratech/desktop/internal/input/wails.HomeworkService.CreateAssignment",
  deleteAssignment: "paratech/desktop/internal/input/wails.HomeworkService.DeleteAssignment",
  deleteImpact: "paratech/desktop/internal/input/wails.HomeworkService.DeleteImpact",
  restoreAssignment: "paratech/desktop/internal/input/wails.HomeworkService.RestoreAssignment",
  editAssignment: "paratech/desktop/internal/input/wails.HomeworkService.EditAssignment",
  draftVersion: "paratech/desktop/internal/input/wails.HomeworkService.DraftVersion",
  generate: "paratech/desktop/internal/input/wails.HomeworkService.Generate",
  generationStatus: "paratech/desktop/internal/input/wails.HomeworkService.GenerationStatus",
  editDraft: "paratech/desktop/internal/input/wails.HomeworkService.EditDraft",
  exportDraft: "paratech/desktop/internal/input/wails.HomeworkService.ExportDraft",
  exportDocx: "paratech/desktop/internal/input/wails.HomeworkService.ExportDocx",
  saveDocx: "paratech/desktop/internal/input/wails.HomeworkService.SaveDocx",
  prepareSend: "paratech/desktop/internal/input/wails.HomeworkService.PrepareSend",
  confirmSend: "paratech/desktop/internal/input/wails.HomeworkService.ConfirmSend",
  cancelSend: "paratech/desktop/internal/input/wails.HomeworkService.CancelSend",
  deliveries: "paratech/desktop/internal/input/wails.HomeworkService.Deliveries",
  markSent: "paratech/desktop/internal/input/wails.HomeworkService.MarkSent",
  sendAgain: "paratech/desktop/internal/input/wails.HomeworkService.SendAgain",
  markDone: "paratech/desktop/internal/input/wails.HomeworkService.MarkDone",
  restoreDone: "paratech/desktop/internal/input/wails.HomeworkService.RestoreDone",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  unavailable: "Не удалось выполнить действие. Попробуйте ещё раз.",
  invalidId: "Неверный идентификатор.",
  notFound: "Задание не найдено: обновите список.",
  versionNotFound: "Такой версии черновика нет: обновите страницу.",
  draftStale: "Черновик изменился: появилась новая версия. Обновите страницу и повторите.",
  draftInvalid: "Заголовок: одна строка до 200 символов, текст: до 16000 символов; оба не пустые.",
  draftFull: "У черновика уже 100 версий: больше добавить нельзя.",
  instructionInvalid: "Инструкция слишком длинная (больше 4 КБ) или содержит недопустимые символы.",
  generateRetries: "Эта генерация уже много раз не удалась. Измените инструкцию или текст черновика.",
  /** generate refused: a generation of the assignment is pending or running. */
  generationRunning: "Черновик уже готовится. Дождитесь окончания и повторите.",
  sendFormat: "Неизвестный формат вложения: выберите .docx или .txt.",
  generationNotFound: "Генерация не найдена: обновите страницу.",
  sendNotFound: "Отправка не найдена: подготовьте её ещё раз.",
  sendCannotConfirm: "Эту отправку уже нельзя подтвердить. Подготовьте её ещё раз.",
  sendAlreadyDecided: "Эта отправка уже подтверждена или отменена.",
  sendStale: "Файл, адрес получателя или тема письма изменились после подтверждения. Проверьте и подтвердите ещё раз.",
  /** confirmSend refused: the assignment was deleted after the dialog opened (TASK-229); nothing was sent. */
  sendAssignmentDeleted: "Задание удалено: письмо не отправлено. Верните задание и подготовьте отправку ещё раз.",
  sendUnresolved:
    "Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Сначала разберитесь с ним в списке писем.",
  /** confirmSend refused: «Кому отправить?» was asked and «Отправить» came without a choice; nothing was sent. */
  sendChooseTeacher: "Выберите, кому отправить: лектору, семинаристу или преподавателю лабораторных.",
  /** confirmSend failed; repeating it with the same proposalId is safe: no second email. */
  sendIncomplete: "Не удалось завершить отправку. Повторите — второе письмо не уйдёт — или проверьте список писем ниже.",
  /** exportDraft refused: an earlier email of the assignment is unresolved; nothing was written. */
  exportLocked:
    "Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Пока оно не решено, файл задания менять нельзя.",
  deliveryNotFound: "Письмо не найдено: обновите список.",
  deliveryActionInvalid: "Это действие для письма уже недоступно: обновите список.",
  /** listAssignments refused an unknown filter. */
  listState: "Неизвестный фильтр списка заданий: обновите страницу.",
} as const;

/** The texts of the problems of markDone and restoreDone; "changed" and "unchanged" need none. */
export const doneMessages = {
  not_found: "Задание уже удалено: обновите список.",
  deleted: "Задание уже удалено: обновите список.",
  conflict:
    "Задание изменилось (например, после повторной обработки записи), поэтому отменить нельзя. Откройте задание и поставьте или снимите отметку «Выполнено» вручную.",
  failed: "Не удалось сохранить отметку. Попробуйте ещё раз.",
  invalid: "Не удалось отметить задания: обновите список и попробуйте ещё раз.",
  unavailable: "Не удалось выполнить действие. Попробуйте ещё раз.",
} as const;

export const assignmentStates: readonly AssignmentState[] = ["open", "done", "all"];

export const doneProblems: readonly DoneProblem[] = ["", "invalid", "unavailable"];

export const doneCodes: readonly DoneCode[] = ["changed", "unchanged", "not_found", "deleted", "conflict", "failed"];

export const versionSources: readonly VersionSource[] = ["generated", "edited"];

export const generationStates: readonly GenerationState[] = [
  "generating",
  "waitingNetwork",
  "waitingQuota",
  "failed",
  "done",
];

export const sendStatuses: readonly SendStatus[] = ["ready", "rejected"];

export const createProblems: readonly CreateProblem[] = ["", "invalid", "subject_not_found", "lesson_not_found", "failed"];

export const createFields: readonly CreateField[] = ["", "requestId", "title", "note", "due", "time"];

export const changeProblems: readonly ChangeProblem[] = ["", "in_use", "changed", "not_found", "expired", "failed"];

export const editProblems: readonly EditProblem[] = [
  "",
  "invalid",
  "subject_not_found",
  "changed",
  "deleted",
  "not_found",
  "failed",
];

export const editFields: readonly EditField[] = ["", "title", "note", "due", "time"];

export const sendFormats: readonly SendFormat[] = ["docx", "txt"];

export const docxSaveStatuses: readonly DocxSaveStatus[] = ["saved", "cancelled", "failed"];

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null) {
    throw new Error(`${what}: reply is not an object`);
  }
  return raw as Record<string, unknown>;
}

function str(r: Record<string, unknown>, key: string, what: string): string {
  const v = r[key];
  if (typeof v !== "string") {
    throw new Error(`${what}: ${key} is not a string`);
  }
  return v;
}

function num(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isFinite(v)) {
    throw new Error(`${what}: ${key} is not a number`);
  }
  return v;
}

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

function oneOf<T extends string>(r: Record<string, unknown>, key: string, allowed: readonly T[], what: string): T {
  const v = r[key];
  if (typeof v !== "string" || !allowed.includes(v as T)) {
    throw new Error(`${what}: ${key} is not one of ${allowed.join(", ")}`);
  }
  return v as T;
}

function list<T>(raw: unknown, what: string, parse: (item: unknown) => T): T[] {
  if (!Array.isArray(raw)) {
    throw new Error(`${what}: reply is not a list`);
  }
  return raw.map(parse);
}

/** Checks the shape of one assignment; a malformed reply is an error, not a default. */
export function parseAssignment(raw: unknown): AssignmentItem {
  const w = "assignment";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    lessonId: str(r, "lessonId", w),
    title: str(r, "title", w),
    description: str(r, "description", w),
    due: str(r, "due", w),
    userEdited: bool(r, "userEdited", w),
    version: num(r, "version", w),
    updatedAt: str(r, "updatedAt", w),
    subject: str(r, "subject", w),
    subjectSource: lessonSource(str(r, "subjectSource", w)),
    subjectFromLesson: bool(r, "subjectFromLesson", w),
    kind: str(r, "kind", w),
    teacher: str(r, "teacher", w),
    teacherSource: lessonSource(str(r, "teacherSource", w)),
    lessonDate: str(r, "lessonDate", w),
    correctedSubject: str(r, "correctedSubject", w),
    correctedTeacher: str(r, "correctedTeacher", w),
    // A reply without the mark (older shape) is an open assignment; a mark of another type is an error.
    done: r.done === undefined ? false : bool(r, "done", w),
    doneAt: r.doneAt === undefined ? "" : str(r, "doneAt", w),
  };
}

/** A created assignment comes with problem ""; any other problem comes without one. */
export function parseCreateAssignmentResult(raw: unknown): CreateAssignmentResult {
  const w = "create assignment result";
  const r = record(raw, w);
  const problem = oneOf(r, "problem", createProblems, w);
  const field = oneOf(r, "field", createFields, w);
  if (problem === "") {
    return { problem, field, assignment: parseAssignment(r.assignment) };
  }
  if (r.assignment !== null) {
    throw new Error(`${w}: a problem comes with an assignment`);
  }
  return { problem, field, assignment: null };
}

/** v when it is one of allowed, else fallback: an unknown or missing code is never passed on. */
function known<T extends string>(v: unknown, allowed: readonly T[], fallback: T): T {
  return typeof v === "string" && allowed.includes(v as T) ? (v as T) : fallback;
}

/** The assignment of a reply without a problem; null when it is malformed. */
function doneAssignment(raw: unknown): AssignmentItem | null {
  try {
    return parseAssignment(raw);
  } catch {
    return null;
  }
}

/** Never throws: a malformed reply, an unknown problem or a missing assignment is "failed". */
export function parseChangeAssignmentResult(raw: unknown): ChangeAssignmentResult {
  const failed: ChangeAssignmentResult = { problem: "failed", assignment: null, stopped: false };
  if (typeof raw !== "object" || raw === null) {
    return failed;
  }
  const r = raw as Record<string, unknown>;
  const problem = known(r.problem, changeProblems, "failed");
  if (problem !== "") {
    return { problem, assignment: null, stopped: false };
  }
  const assignment = doneAssignment(r.assignment);
  // Only a literal true counts: a missing or odd flag never claims that a work was stopped.
  return assignment === null ? failed : { problem, assignment, stopped: r.stopped === true };
}

/** Never throws: anything but a literal working:true is "not working", so a failed check never blocks a delete. */
export function parseDeleteImpact(raw: unknown): DeleteImpact {
  if (typeof raw !== "object" || raw === null) {
    return { working: false };
  }
  return { working: (raw as Record<string, unknown>).working === true };
}

/** Never throws: like parseChangeAssignmentResult; field is kept only for "invalid" ("" when unknown). */
export function parseEditAssignmentResult(raw: unknown): EditAssignmentResult {
  const failed: EditAssignmentResult = { problem: "failed", field: "", assignment: null };
  if (typeof raw !== "object" || raw === null) {
    return failed;
  }
  const r = raw as Record<string, unknown>;
  const problem = known(r.problem, editProblems, "failed");
  if (problem === "invalid") {
    return { problem, field: known(r.field, editFields, ""), assignment: null };
  }
  if (problem !== "") {
    return { problem, field: "", assignment: null };
  }
  const assignment = doneAssignment(r.assignment);
  return assignment === null ? failed : { problem, field: "", assignment };
}

function doneProblem(problem: DoneProblem): DoneResult {
  return { problem, outcomes: [], prior: [], priorDone: false };
}

function isVersion(v: unknown): v is number {
  return typeof v === "number" && Number.isSafeInteger(v) && v >= 0;
}

/** One outcome; null when it names no id. An unknown code, or a stored state that is not one, is "failed". */
function parseDoneOutcome(raw: unknown): DoneOutcome | null {
  if (typeof raw !== "object" || raw === null) {
    return null;
  }
  const r = raw as Record<string, unknown>;
  if (typeof r.id !== "string") {
    return null;
  }
  const code = known(r.code, doneCodes, "failed");
  if (code !== "changed" && code !== "unchanged") {
    return { id: r.id, code, done: false, version: 0 };
  }
  if (typeof r.done !== "boolean" || !isVersion(r.version)) {
    return { id: r.id, code: "failed", done: false, version: 0 };
  }
  return { id: r.id, code, done: r.done, version: r.version };
}

/**
 * Never throws: a malformed reply or an unknown problem is "unavailable". The prior is kept only when every entry
 * is well formed and names a "changed" outcome: otherwise no «Отменить» is offered rather than a partial one.
 */
export function parseDoneResult(raw: unknown): DoneResult {
  if (typeof raw !== "object" || raw === null) {
    return doneProblem("unavailable");
  }
  const r = raw as Record<string, unknown>;
  const problem = known(r.problem, doneProblems, "unavailable");
  if (problem !== "") {
    return doneProblem(problem);
  }
  if (!Array.isArray(r.outcomes) || !Array.isArray(r.prior) || typeof r.priorDone !== "boolean") {
    return doneProblem("unavailable");
  }
  const outcomes: DoneOutcome[] = [];
  for (const item of r.outcomes) {
    const o = parseDoneOutcome(item);
    if (o === null) {
      return doneProblem("unavailable");
    }
    outcomes.push(o);
  }
  const changed = new Set(outcomes.filter((o) => o.code === "changed").map((o) => o.id));
  const prior: DonePrior[] = [];
  for (const item of r.prior) {
    const p = item as Record<string, unknown> | null;
    if (typeof p !== "object" || p === null || typeof p.id !== "string" || !changed.has(p.id) || !isVersion(p.version)) {
      return { problem, outcomes, prior: [], priorDone: r.priorDone };
    }
    prior.push({ id: p.id, version: p.version });
  }
  return { problem, outcomes, prior, priorDone: r.priorDone };
}

/** What the page shows after markDone or restoreDone. */
export interface DoneSummary {
  changed: number;
  unchanged: number;
  /** The distinct texts of the problems, in the order they first came; empty when everything went through. */
  messages: string[];
  /** «Отменить» is offered: something changed and the reply says how to put it back. */
  undo: boolean;
}

/** Counts the outcomes and collects the texts of the problems once each (not_found and deleted share one). */
export function doneSummary(res: DoneResult): DoneSummary {
  if (res.problem !== "") {
    return { changed: 0, unchanged: 0, messages: [doneMessages[res.problem]], undo: false };
  }
  const out: DoneSummary = { changed: 0, unchanged: 0, messages: [], undo: res.prior.length > 0 };
  for (const o of res.outcomes) {
    if (o.code === "changed") {
      out.changed++;
    } else if (o.code === "unchanged") {
      out.unchanged++;
    } else if (!out.messages.includes(doneMessages[o.code])) {
      out.messages.push(doneMessages[o.code]);
    }
  }
  return out;
}

export function parseVersionItem(raw: unknown): VersionItem {
  const w = "draft version";
  const r = record(raw, w);
  return {
    number: num(r, "number", w),
    source: oneOf(r, "source", versionSources, w),
    instruction: str(r, "instruction", w),
    latest: bool(r, "latest", w),
    createdAt: str(r, "createdAt", w),
  };
}

export function parseVersionText(raw: unknown): VersionText {
  const w = "draft version text";
  const r = record(raw, w);
  return {
    number: num(r, "number", w),
    source: oneOf(r, "source", versionSources, w),
    title: str(r, "title", w),
    body: str(r, "body", w),
    instruction: str(r, "instruction", w),
    latest: bool(r, "latest", w),
    createdAt: str(r, "createdAt", w),
  };
}

export function parseAssignmentDetail(raw: unknown): AssignmentDetail {
  const w = "assignment detail";
  const r = record(raw, w);
  return {
    assignment: parseAssignment(r.assignment),
    versions: list(r.versions, `${w} versions`, parseVersionItem),
    latest: r.latest === null ? null : parseVersionText(r.latest),
    generation: parseActiveGeneration(r.generation, w),
  };
}

/** null stays null: no generation is running. */
export function parseActiveGeneration(raw: unknown, what: string): ActiveGeneration | null {
  if (raw === null) {
    return null;
  }
  const w = `${what} generation`;
  const r = record(raw, w);
  return {
    jobId: str(r, "jobId", w),
    state: oneOf(r, "state", generationStates, w),
    message: str(r, "message", w),
    percent: num(r, "percent", w),
  };
}

export function parseDraftStatus(raw: unknown): DraftStatus {
  const w = "draft status";
  const r = record(raw, w);
  return { latestVersion: num(r, "latestVersion", w), generation: parseActiveGeneration(r.generation, w) };
}

export function parseGenerateStarted(raw: unknown): GenerateStarted {
  const w = "generation started";
  const r = record(raw, w);
  return { jobId: str(r, "jobId", w), base: num(r, "base", w) };
}

export function parseGenerationStatus(raw: unknown): GenerationStatus {
  const w = "generation status";
  const r = record(raw, w);
  return { state: oneOf(r, "state", generationStates, w), message: str(r, "message", w), percent: num(r, "percent", w) };
}

export function parseExportResult(raw: unknown): ExportResult {
  const w = "export result";
  const r = record(raw, w);
  return { version: num(r, "version", w), fileName: str(r, "fileName", w) };
}

export function parseDocxSaveResult(raw: unknown): DocxSaveResult {
  const w = "docx save result";
  const r = record(raw, w);
  return {
    status: oneOf(r, "status", docxSaveStatuses, w),
    message: str(r, "message", w),
    name: str(r, "name", w),
  };
}

export function parseSendPreparation(raw: unknown): SendPreparation {
  const w = "send preparation";
  const r = record(raw, w);
  return {
    proposalId: str(r, "proposalId", w),
    status: oneOf(r, "status", sendStatuses, w),
    reason: str(r, "reason", w),
    message: str(r, "message", w),
    recipient: str(r, "recipient", w),
    ...parseRecipientFields(r),
    subject: str(r, "subject", w),
    attachmentName: str(r, "attachmentName", w),
    attachmentSize: num(r, "attachmentSize", w),
    attachmentFormat: oneOf(r, "attachmentFormat", sendFormats, w),
  };
}

export function parseDelivery(raw: unknown): DeliveryItem {
  const w = "delivery";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    status: str(r, "status", w),
    recipient: str(r, "recipient", w),
    subject: str(r, "subject", w),
    attempts: num(r, "attempts", w),
    lastError: str(r, "lastError", w),
    confirmedAt: str(r, "confirmedAt", w),
    sentAt: str(r, "sentAt", w),
    createdAt: str(r, "createdAt", w),
    canMarkSent: bool(r, "canMarkSent", w),
    canSendAgain: bool(r, "canSendAgain", w),
    notice: str(r, "notice", w),
  };
}

export interface HomeworkApi {
  /** The filter «Открытые / Выполненные / Все»; the open ones when none is given. */
  listAssignments(state?: AssignmentState): Promise<AssignmentItem[]>;
  /**
   * «Выполнено» (done) or «Вернуть в работу» (not done) of 1..50 assignments; never rejects: a failure is a problem
   * or a per-assignment code. Offer «Отменить» only when doneSummary(result).undo.
   */
  markDone(ids: string[], done: boolean): Promise<DoneResult>;
  /** «Отменить»: exactly priorDone and prior of the previous markDone or restoreDone; never rejects. */
  restoreDone(priorDone: boolean, prior: DonePrior[]): Promise<DoneResult>;
  openAssignment(id: string): Promise<AssignmentDetail>;
  /** The cheap poll of an open assignment: a larger latestVersion than shown means reload with openAssignment. */
  draftStatus(id: string): Promise<DraftStatus>;
  /** Stores the student's subject and teacher of the assignment; "" removes that correction. Returns it as shown. */
  correctLesson(id: string, subject: string, teacher: string): Promise<AssignmentItem>;
  /** Adds an assignment or a reminder the student typed in; a problem is a code, not a thrown error. */
  createAssignment(req: CreateAssignmentRequest): Promise<CreateAssignmentResult>;
  /** «Удалить» at the shown version; never rejects: any failure is a problem code. */
  deleteAssignment(id: string, version: number): Promise<ChangeAssignmentResult>;
  /** Whether «Удалить» would stop a running work or draft: asked before the delete; never rejects (failure: not working). */
  deleteImpact(id: string): Promise<DeleteImpact>;
  /** «Вернуть» at the version deleteAssignment returned; never rejects. */
  restoreAssignment(id: string, version: number): Promise<ChangeAssignmentResult>;
  /** Stores the edit form; never rejects: any failure is a problem code. */
  editAssignment(req: EditAssignmentRequest): Promise<EditAssignmentResult>;
  draftVersion(id: string, version: number): Promise<VersionText>;
  /** Generates from version base, the one the page shows (0: no draft yet). */
  generate(id: string, base: number, instruction: string): Promise<GenerateStarted>;
  generationStatus(jobId: string): Promise<GenerationStatus>;
  /** Stores the student's text over version base as a new version. */
  editDraft(id: string, base: number, title: string, body: string): Promise<VersionText>;
  exportDraft(id: string, version: number): Promise<ExportResult>;
  /** Writes the version as the Word file a send in .docx attaches. */
  exportDocx(id: string, version: number): Promise<ExportResult>;
  /** «Сохранить в Word (.docx)»: the core asks where in the native save dialog. */
  saveDocx(id: string, version: number): Promise<DocxSaveResult>;
  /** Builds the confirmation dialog for the attachment in format; sends nothing. */
  prepareSend(id: string, format: SendFormat): Promise<SendPreparation>;
  /**
   * The dialog's confirm button only: confirms the shown send, subject and attachment format, creates the email
   * (AC-14). chosen is the option the student clicked when the dialog asked «Кому отправить?» (reason
   * "choose_teacher"); without it such a send is refused with errorMessages.sendChooseTeacher.
   */
  confirmSend(proposalId: string, subject: string, format: SendFormat, chosen?: TeacherChoice | null): Promise<void>;
  /** The dialog's cancel button: the send can never happen afterwards. */
  cancelSend(proposalId: string): Promise<void>;
  deliveries(id: string): Promise<DeliveryItem[]>;
  markSent(deliveryId: string): Promise<DeliveryItem>;
  /** Only after the student confirmed a possible duplicate email. */
  sendAgain(deliveryId: string): Promise<DeliveryItem>;
}

/** The reply of a call, or null when the runtime rejects it: its text is never shown. */
async function settled(reply: Promise<unknown>): Promise<unknown> {
  try {
    return await reply;
  } catch {
    return null;
  }
}

/** Builds the homework API over a call function (the Wails runtime in the app, a fake in tests). */
export function homeworkApi(call: CallByName): HomeworkApi {
  return {
    listAssignments: async (state = "open") =>
      list(await call(methods.listAssignments, state), "assignments", parseAssignment),
    markDone: async (ids, done) => parseDoneResult(await settled(call(methods.markDone, ids, done))),
    restoreDone: async (priorDone, prior) =>
      parseDoneResult(
        await settled(call(methods.restoreDone, priorDone, prior.map((p) => ({ id: p.id, version: p.version })))),
      ),
    openAssignment: async (id) => parseAssignmentDetail(await call(methods.openAssignment, id)),
    draftStatus: async (id) => parseDraftStatus(await call(methods.draftStatus, id)),
    correctLesson: async (id, subject, teacher) =>
      parseAssignment(await call(methods.correctLesson, id, subject, teacher)),
    createAssignment: async (req) => parseCreateAssignmentResult(await call(methods.createAssignment, req)),
    deleteAssignment: async (id, version) =>
      parseChangeAssignmentResult(await settled(call(methods.deleteAssignment, id, version))),
    deleteImpact: async (id) => parseDeleteImpact(await settled(call(methods.deleteImpact, id))),
    restoreAssignment: async (id, version) =>
      parseChangeAssignmentResult(await settled(call(methods.restoreAssignment, id, version))),
    editAssignment: async (req) => parseEditAssignmentResult(await settled(call(methods.editAssignment, req))),
    draftVersion: async (id, version) => parseVersionText(await call(methods.draftVersion, id, version)),
    generate: async (id, base, instruction) =>
      parseGenerateStarted(await call(methods.generate, id, base, instruction)),
    generationStatus: async (jobId) => parseGenerationStatus(await call(methods.generationStatus, jobId)),
    editDraft: async (id, base, title, body) => parseVersionText(await call(methods.editDraft, id, base, title, body)),
    exportDraft: async (id, version) => parseExportResult(await call(methods.exportDraft, id, version)),
    exportDocx: async (id, version) => parseExportResult(await call(methods.exportDocx, id, version)),
    saveDocx: async (id, version) => parseDocxSaveResult(await call(methods.saveDocx, id, version)),
    prepareSend: async (id, format) => parseSendPreparation(await call(methods.prepareSend, id, format)),
    confirmSend: async (proposalId, subject, format, chosen) => {
      await call(methods.confirmSend, proposalId, subject, format, ...choiceArgs(chosen));
    },
    cancelSend: async (proposalId) => {
      await call(methods.cancelSend, proposalId);
    },
    deliveries: async (id) => list(await call(methods.deliveries, id), "deliveries", parseDelivery),
    markSent: async (deliveryId) => parseDelivery(await call(methods.markSent, deliveryId)),
    sendAgain: async (deliveryId) => parseDelivery(await call(methods.sendAgain, deliveryId)),
  };
}
