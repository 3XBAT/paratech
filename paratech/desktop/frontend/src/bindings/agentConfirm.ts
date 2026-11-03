// Bindings of the agent's confirm cards (TASK-127/128): calls into internal/input/wails/agentconfirm.go. The card
// is read from the core's proposal, never from the agent's text. Title, subject, recipient, file name and the
// message are stored or imported text: show them as text nodes only. Confirm is the only call that leads to an email
// or a calendar event, and only a click on «Да» makes it.

import {
  choiceArgs,
  parseRecipientFields,
  type RecipientFields,
  type RecipientReason,
  recipientReason,
  type TeacherChoice,
} from "./recipient.js";
import {
  type CreateFromScheduleReply,
  parseCreateFromSchedule,
  parseScheduleGroup,
  parseUndoFromSchedule,
  type ScheduleChoice,
  type ScheduleGroup,
  type UndoFromScheduleReply,
} from "./library.js";
import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.AgentConfirmService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  card: `${service}.Card`,
  confirm: `${service}.Confirm`,
  decline: `${service}.Decline`,
  proposeDeadline: `${service}.ProposeDeadline`,
  proposeSendWork: `${service}.ProposeSendWork`,
  // TASK-323: the schedule_subjects card (agentconfirm_schedule.go).
  confirmSchedule: `${service}.ConfirmSchedule`,
  undoSchedule: `${service}.UndoSchedule`,
} as const;

export type CardKind = "deadline" | "send" | "delete_recording" | "delete_assignment" | "schedule_subjects";

export const cardKinds: readonly CardKind[] = ["deadline", "send", "delete_recording", "delete_assignment", "schedule_subjects"];

/**
 * waiting: «Да» / «Нет»; confirmed: not finished, «Да» repeats it safely; stale and rejected carry a message; undone: a
 * schedule_subjects card after «Отменить всё».
 */
export type CardState = "waiting" | "confirmed" | "done" | "declined" | "stale" | "rejected" | "undone";

export const cardStates: readonly CardState[] = ["waiting", "confirmed", "done", "declined", "stale", "rejected", "undone"];

/**
 * The target of a confirm card as the update names it: a stored proposal, a deletion preview, or (a delete_assignment
 * card) the assignment itself, which has nothing stored to decline. A schedule_subjects card names the token of the
 * package the desktop built.
 */
export interface ConfirmTarget {
  kind: "proposal" | "preview" | "assignment" | "schedule_subjects";
  id: string;
}

/**
 * The content of a schedule_subjects card, in the shapes the «Предметы» review reads from LibraryService: the groups to
 * choose from while waiting (the lists are empty after that), the outcome of «Да» and the outcome of «Отменить всё».
 * Every name in it is schedule text: a text node only.
 */
export interface ConfirmSchedule {
  groups: ScheduleGroup[];
  omitted: number;
  result: CreateFromScheduleReply | null;
  undo: UndoFromScheduleReply | null;
}

export interface ConfirmDeletion {
  id: string;
  title: string;
  startedAt: string;
  deletable: boolean;
  refusalMessage: string;
  deletes: string[];
}

/**
 * A send card also carries the recipient fields of recipient.ts: with reason "choose_teacher" the card asks «Кому
 * отправить?» (recipient is "" until a click, «Да» stays disabled); "no_teacher_email" and "no_subject" come with
 * state "rejected" and the message.
 */
export interface ConfirmCard extends RecipientFields {
  reason: RecipientReason;
  kind: CardKind;
  targetKind: string;
  targetId: string;
  state: CardState;
  message: string;
  assignmentId: string;
  title: string;
  subject: string;
  /** RFC 3339; "" when unknown. */
  due: string;
  recipient: string;
  emailSubject: string;
  attachmentName: string;
  attachmentSize: number;
  attachmentFormat: string;
  /** 0: not a draft version as such. */
  draftVersion: number;
  /** «В файле указан другой адрес — не используется» or "". */
  notice: string;
  deletion: ConfirmDeletion | null;
  /** A schedule_subjects card; null on every other kind. */
  schedule: ConfirmSchedule | null;
}

/** The refusal no_assignment of proposeSendWork: a work not made for an assignment is never sent (TASK-180). */
export const noAssignmentText =
  "Работа сделана не по заданию из списка ДЗ: её можно сохранить, но не отправить преподавателю.";

/** The refusal of «Да» on a delete_assignment card while an email of the assignment may still go out (ErrAgentConfirmInUse). */
export const assignmentInUseText =
  "Сейчас удалить нельзя: письмо по этому заданию ещё отправляется или его судьба неизвестна. Проверьте «Письма» и нажмите «Да» ещё раз.";

/** The refusals of the schedule_subjects card (ErrAgentConfirmSchedule* in agentconfirm_schedule.go). */
export const scheduleBusyText =
  "Предметы из расписания сейчас создаются или отменяются. Подождите немного и нажмите ещё раз.";
export const scheduleNameText =
  "Это название уже занято другим предметом или строкой списка. Выберите другое и нажмите «Да».";
export const scheduleUndoGoneText =
  "Отменить отсюда уже нельзя. Лишние предметы можно убрать на странице «Предметы».";
export const scheduleUndoAgainText = "Отменилось не всё. Нажмите «Отменить всё» ещё раз — повтор безопасен.";

/** The fixed Russian texts of the Go binding (agentconfirm.go, agentconfirm_assignment.go, homework.go); anything else gets the generic one. */
export const errorTexts: readonly string[] = [
  "Неверная карточка подтверждения: обновите панель.",
  "Предложение не найдено: возможно, оно уже обработано.",
  "Это предложение уже подтверждено или отклонено.",
  "Удаление записи подтверждается в окне удаления.",
  "Календарь не подключён: подключите Google в настройках и нажмите «Да» ещё раз.",
  "У задания нет срока: укажите его в «Домашних заданиях».",
  "Не удалось выполнить. Нажмите «Да» ещё раз — повторного действия не будет.",
  "Файл, адрес получателя или тема письма изменились после подтверждения. Проверьте и подтвердите ещё раз.",
  "Неизвестный формат вложения: выберите .docx или .txt.",
  "Выберите, кому отправить: лектору, семинаристу или преподавателю лабораторных.",
  "Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Сначала разберитесь с ним в списке писем.",
  noAssignmentText,
  assignmentInUseText,
  scheduleBusyText,
  scheduleNameText,
  scheduleUndoGoneText,
  scheduleUndoAgainText,
];

export const genericError = "Не удалось выполнить действие. Попробуйте ещё раз.";

function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}

/** The core's text of a refused call when it is one of the known texts; a generic text otherwise. */
export function confirmErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return errorTexts.includes(msg) ? msg : genericError;
}

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new Error(`${what}: not an object`);
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

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

function count(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isFinite(v) || v < 0) {
    throw new Error(`${what}: ${key} is not a count`);
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

function strings(r: Record<string, unknown>, key: string, what: string): string[] {
  const v = r[key];
  if (v === null || v === undefined) {
    return [];
  }
  if (!Array.isArray(v) || v.some((s) => typeof s !== "string")) {
    throw new Error(`${what}: ${key} is not a list of strings`);
  }
  return v as string[];
}

function parseDeletion(raw: unknown): ConfirmDeletion {
  const w = "confirm deletion";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    title: str(r, "title", w),
    startedAt: str(r, "startedAt", w),
    deletable: bool(r, "deletable", w),
    refusalMessage: str(r, "refusalMessage", w),
    deletes: strings(r, "deletes", w),
  };
}

function parseSchedule(raw: unknown): ConfirmSchedule {
  const w = "confirm schedule";
  const r = record(raw, w);
  const groups = r.groups;
  if (!Array.isArray(groups)) {
    throw new Error(`${w}: groups is not a list`);
  }
  return {
    groups: groups.map(parseScheduleGroup),
    omitted: count(r, "omitted", w),
    result: r.result === null || r.result === undefined ? null : parseCreateFromSchedule(r.result),
    undo: r.undo === null || r.undo === undefined ? null : parseUndoFromSchedule(r.undo),
  };
}

/** Checks the shape of a card; a malformed reply is an error, not a default. */
export function parseConfirmCard(raw: unknown): ConfirmCard {
  const w = "confirm card";
  const r = record(raw, w);
  const kind = oneOf(r, "kind", cardKinds, w);
  const schedule = r.schedule === null || r.schedule === undefined ? null : parseSchedule(r.schedule);
  if (kind === "schedule_subjects" && schedule === null) {
    throw new Error(`${w}: schedule is missing`);
  }
  return {
    kind,
    schedule,
    targetKind: str(r, "targetKind", w),
    targetId: str(r, "targetId", w),
    state: oneOf(r, "state", cardStates, w),
    message: str(r, "message", w),
    assignmentId: str(r, "assignmentId", w),
    title: str(r, "title", w),
    subject: str(r, "subject", w),
    due: str(r, "due", w),
    recipient: str(r, "recipient", w),
    ...parseRecipientFields(r),
    reason: recipientReason(r.reason),
    emailSubject: str(r, "emailSubject", w),
    attachmentName: str(r, "attachmentName", w),
    attachmentSize: count(r, "attachmentSize", w),
    attachmentFormat: str(r, "attachmentFormat", w),
    draftVersion: count(r, "draftVersion", w),
    notice: str(r, "notice", w),
    deletion: r.deletion === null || r.deletion === undefined ? null : parseDeletion(r.deletion),
  };
}

export interface AgentConfirmApi {
  /** The card of a proposal or of a deletion preview. */
  card(target: ConfirmTarget): Promise<ConfirmCard>;
  /**
   * «Да»: for a send, with the subject and format the card showed and, when the card asked «Кому отправить?», the
   * option the student clicked (without it such a send is refused). A deletion is confirmed in the deletion window.
   */
  confirm(card: ConfirmCard, chosen?: TeacherChoice | null): Promise<ConfirmCard>;
  /** «Нет»: the proposal can never run afterwards; a preview has nothing stored. */
  decline(target: ConfirmTarget): Promise<ConfirmCard>;
  /** «Да, в календарь» of «Сегодня»: the stored deadline of the assignment becomes a proposal; nothing goes out yet. */
  proposeDeadline(assignmentId: string): Promise<ConfirmCard>;
  /**
   * «Отправить преподавателю» of a finished work: its file becomes a send card (kind "send", .docx or .pptx); nothing
   * is sent before «Да». A work without an assignment is refused with noAssignmentText.
   */
  proposeSendWork(workId: string): Promise<ConfirmCard>;
  /**
   * «Да» of a schedule_subjects card: the choices go as the page sends them to LibraryService.CreateFromSchedule (a
   * doubtful member left alone is merged; after a «Да» that stopped midway [] is fine). The card comes back done,
   * confirmed (stopped midway), or stale; a refusal rejects with the core's text.
   */
  confirmSchedule(token: string, choices: ScheduleChoice[]): Promise<ConfirmCard>;
  /** «Отменить всё» of a schedule_subjects card; a repeated call is safe. */
  undoSchedule(token: string): Promise<ConfirmCard>;
}

/** Builds the confirm API over a call function (the Wails runtime in the app, a fake in tests). */
export function agentConfirmApi(call: CallByName): AgentConfirmApi {
  return {
    card: async (t) => parseConfirmCard(await call(methods.card, t.kind, t.id)),
    confirm: async (c, chosen) =>
      parseConfirmCard(
        await call(methods.confirm, c.targetKind, c.targetId, c.emailSubject, c.attachmentFormat, ...choiceArgs(chosen)),
      ),
    decline: async (t) => parseConfirmCard(await call(methods.decline, t.kind, t.id)),
    proposeDeadline: async (id) => parseConfirmCard(await call(methods.proposeDeadline, id)),
    proposeSendWork: async (id) => parseConfirmCard(await call(methods.proposeSendWork, id)),
    confirmSchedule: async (token, choices) => parseConfirmCard(await call(methods.confirmSchedule, token, choices)),
    undoSchedule: async (token) => parseConfirmCard(await call(methods.undoSchedule, token)),
  };
}
