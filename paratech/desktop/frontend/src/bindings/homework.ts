// Homework page bindings: calls into the Go core (internal/input/wails/homework.go). The core decides what is
// offered (canMarkSent, canSendAgain, the send dialog's status) and writes every text; the page only shows them.
// Titles, bodies, instructions and descriptions may be model text: show them as plain text, never as HTML.
// Sending: exportDraft(shown version) -> prepareSend -> the confirmation dialog -> confirmSend or cancelSend.
// confirmSend is the only call that leads to an email; call it only from the dialog's confirm button (AC-14).

import { type LessonSource, lessonSource } from "../lessonLabel.js";
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
  updatedAt: string;
  /** The lesson (TASK-092): the shown subject and teacher with their source, the kind and the lesson start. */
  subject: string;
  subjectSource: LessonSource;
  /** "lecture", "seminar", "practice", "lab" or "". */
  kind: string;
  teacher: string;
  teacherSource: LessonSource;
  lessonDate: string;
  /** The student's correction, "" when none: the values of the correction form. */
  correctedSubject: string;
  correctedTeacher: string;
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

/** An opened assignment: its versions in order and the latest text (null: no draft yet). */
export interface AssignmentDetail {
  assignment: AssignmentItem;
  versions: VersionItem[];
  latest: VersionText | null;
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
}

/** The version written to the file a send attaches. */
export interface ExportResult {
  version: number;
  fileName: string;
}

/** "ready": the dialog shows recipient, subject and attachment and offers the confirmation; "rejected": message only. */
export type SendStatus = "ready" | "rejected";

/** The content of the send confirmation dialog. */
export interface SendPreparation {
  proposalId: string;
  status: SendStatus;
  /** The core's reason code of a rejected send; "" when ready. */
  reason: string;
  message: string;
  recipient: string;
  subject: string;
  attachmentName: string;
  /** Bytes. */
  attachmentSize: number;
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

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  listAssignments: "paratech/desktop/internal/input/wails.HomeworkService.ListAssignments",
  openAssignment: "paratech/desktop/internal/input/wails.HomeworkService.OpenAssignment",
  correctLesson: "paratech/desktop/internal/input/wails.HomeworkService.CorrectLesson",
  draftVersion: "paratech/desktop/internal/input/wails.HomeworkService.DraftVersion",
  generate: "paratech/desktop/internal/input/wails.HomeworkService.Generate",
  generationStatus: "paratech/desktop/internal/input/wails.HomeworkService.GenerationStatus",
  editDraft: "paratech/desktop/internal/input/wails.HomeworkService.EditDraft",
  exportDraft: "paratech/desktop/internal/input/wails.HomeworkService.ExportDraft",
  prepareSend: "paratech/desktop/internal/input/wails.HomeworkService.PrepareSend",
  confirmSend: "paratech/desktop/internal/input/wails.HomeworkService.ConfirmSend",
  cancelSend: "paratech/desktop/internal/input/wails.HomeworkService.CancelSend",
  deliveries: "paratech/desktop/internal/input/wails.HomeworkService.Deliveries",
  markSent: "paratech/desktop/internal/input/wails.HomeworkService.MarkSent",
  sendAgain: "paratech/desktop/internal/input/wails.HomeworkService.SendAgain",
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
  generationNotFound: "Генерация не найдена: обновите страницу.",
  sendNotFound: "Отправка не найдена: подготовьте её ещё раз.",
  sendCannotConfirm: "Эту отправку уже нельзя подтвердить. Подготовьте её ещё раз.",
  sendAlreadyDecided: "Эта отправка уже подтверждена или отменена.",
  sendStale: "Файл, адрес получателя или тема письма изменились после подтверждения. Проверьте и подтвердите ещё раз.",
  sendUnresolved:
    "Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Сначала разберитесь с ним в списке писем.",
  /** confirmSend failed; repeating it with the same proposalId is safe: no second email. */
  sendIncomplete: "Не удалось завершить отправку. Повторите — второе письмо не уйдёт — или проверьте список писем ниже.",
  /** exportDraft refused: an earlier email of the assignment is unresolved; nothing was written. */
  exportLocked:
    "Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Пока оно не решено, файл задания менять нельзя.",
  deliveryNotFound: "Письмо не найдено: обновите список.",
  deliveryActionInvalid: "Это действие для письма уже недоступно: обновите список.",
} as const;

export const versionSources: readonly VersionSource[] = ["generated", "edited"];

export const generationStates: readonly GenerationState[] = [
  "generating",
  "waitingNetwork",
  "waitingQuota",
  "failed",
  "done",
];

export const sendStatuses: readonly SendStatus[] = ["ready", "rejected"];

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
    updatedAt: str(r, "updatedAt", w),
    subject: str(r, "subject", w),
    subjectSource: lessonSource(str(r, "subjectSource", w)),
    kind: str(r, "kind", w),
    teacher: str(r, "teacher", w),
    teacherSource: lessonSource(str(r, "teacherSource", w)),
    lessonDate: str(r, "lessonDate", w),
    correctedSubject: str(r, "correctedSubject", w),
    correctedTeacher: str(r, "correctedTeacher", w),
  };
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
  };
}

export function parseGenerateStarted(raw: unknown): GenerateStarted {
  const w = "generation started";
  const r = record(raw, w);
  return { jobId: str(r, "jobId", w), base: num(r, "base", w) };
}

export function parseGenerationStatus(raw: unknown): GenerationStatus {
  const w = "generation status";
  const r = record(raw, w);
  return { state: oneOf(r, "state", generationStates, w), message: str(r, "message", w) };
}

export function parseExportResult(raw: unknown): ExportResult {
  const w = "export result";
  const r = record(raw, w);
  return { version: num(r, "version", w), fileName: str(r, "fileName", w) };
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
    subject: str(r, "subject", w),
    attachmentName: str(r, "attachmentName", w),
    attachmentSize: num(r, "attachmentSize", w),
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
  listAssignments(): Promise<AssignmentItem[]>;
  openAssignment(id: string): Promise<AssignmentDetail>;
  /** Stores the student's subject and teacher of the assignment; "" removes that correction. Returns it as shown. */
  correctLesson(id: string, subject: string, teacher: string): Promise<AssignmentItem>;
  draftVersion(id: string, version: number): Promise<VersionText>;
  /** Generates from version base, the one the page shows (0: no draft yet). */
  generate(id: string, base: number, instruction: string): Promise<GenerateStarted>;
  generationStatus(jobId: string): Promise<GenerationStatus>;
  /** Stores the student's text over version base as a new version. */
  editDraft(id: string, base: number, title: string, body: string): Promise<VersionText>;
  exportDraft(id: string, version: number): Promise<ExportResult>;
  /** Builds the confirmation dialog; sends nothing. */
  prepareSend(id: string): Promise<SendPreparation>;
  /** The dialog's confirm button only: confirms the shown send and subject, creates the email (AC-14). */
  confirmSend(proposalId: string, subject: string): Promise<void>;
  /** The dialog's cancel button: the send can never happen afterwards. */
  cancelSend(proposalId: string): Promise<void>;
  deliveries(id: string): Promise<DeliveryItem[]>;
  markSent(deliveryId: string): Promise<DeliveryItem>;
  /** Only after the student confirmed a possible duplicate email. */
  sendAgain(deliveryId: string): Promise<DeliveryItem>;
}

/** Builds the homework API over a call function (the Wails runtime in the app, a fake in tests). */
export function homeworkApi(call: CallByName): HomeworkApi {
  return {
    listAssignments: async () => list(await call(methods.listAssignments), "assignments", parseAssignment),
    openAssignment: async (id) => parseAssignmentDetail(await call(methods.openAssignment, id)),
    correctLesson: async (id, subject, teacher) =>
      parseAssignment(await call(methods.correctLesson, id, subject, teacher)),
    draftVersion: async (id, version) => parseVersionText(await call(methods.draftVersion, id, version)),
    generate: async (id, base, instruction) =>
      parseGenerateStarted(await call(methods.generate, id, base, instruction)),
    generationStatus: async (jobId) => parseGenerationStatus(await call(methods.generationStatus, jobId)),
    editDraft: async (id, base, title, body) => parseVersionText(await call(methods.editDraft, id, base, title, body)),
    exportDraft: async (id, version) => parseExportResult(await call(methods.exportDraft, id, version)),
    prepareSend: async (id) => parseSendPreparation(await call(methods.prepareSend, id)),
    confirmSend: async (proposalId, subject) => {
      await call(methods.confirmSend, proposalId, subject);
    },
    cancelSend: async (proposalId) => {
      await call(methods.cancelSend, proposalId);
    },
    deliveries: async (id) => list(await call(methods.deliveries, id), "deliveries", parseDelivery),
    markSent: async (deliveryId) => parseDelivery(await call(methods.markSent, deliveryId)),
    sendAgain: async (deliveryId) => parseDelivery(await call(methods.sendAgain, deliveryId)),
  };
}
