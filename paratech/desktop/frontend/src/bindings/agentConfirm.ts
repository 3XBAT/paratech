// Bindings of the agent's confirm cards (TASK-127/128): calls into internal/input/wails/agentconfirm.go. The card
// is read from the core's proposal, never from the agent's text. Title, subject, recipient, file name and the
// message are stored or imported text: show them as text nodes only. Confirm is the only call that leads to an email
// or a calendar event, and only a click on «Да» makes it.

import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.AgentConfirmService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  card: `${service}.Card`,
  confirm: `${service}.Confirm`,
  decline: `${service}.Decline`,
  proposeDeadline: `${service}.ProposeDeadline`,
} as const;

export type CardKind = "deadline" | "send" | "delete_recording";

export const cardKinds: readonly CardKind[] = ["deadline", "send", "delete_recording"];

/** waiting: «Да» / «Нет»; confirmed: not finished, «Да» repeats it safely; stale and rejected carry a message. */
export type CardState = "waiting" | "confirmed" | "done" | "declined" | "stale" | "rejected";

export const cardStates: readonly CardState[] = ["waiting", "confirmed", "done", "declined", "stale", "rejected"];

/** The target of a confirm card as the update names it: a stored proposal or a deletion preview. */
export interface ConfirmTarget {
  kind: "proposal" | "preview";
  id: string;
}

export interface ConfirmDeletion {
  id: string;
  title: string;
  startedAt: string;
  deletable: boolean;
  refusalMessage: string;
  deletes: string[];
}

export interface ConfirmCard {
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
}

/** The fixed Russian texts of the Go binding (agentconfirm.go, homework.go); anything else gets the generic one. */
export const errorTexts: readonly string[] = [
  "Неверная карточка подтверждения: обновите панель.",
  "Предложение не найдено: возможно, оно уже обработано.",
  "Это предложение уже подтверждено или отклонено.",
  "Удаление записи подтверждается в окне удаления.",
  "Календарь не подключён: подключите Google в настройках и нажмите «Да» ещё раз.",
  "У задания нет срока: укажите его в «Домашних заданиях».",
  "Не удалось выполнить. Нажмите «Да» ещё раз — повторного действия не будет.",
  "Файл, адрес получателя или тема письма изменились после подтверждения. Проверьте и подтвердите ещё раз.",
  "Предыдущее письмо с этим заданием ещё не отправлено до конца или его результат неизвестен. Сначала разберитесь с ним в списке писем.",
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

/** Checks the shape of a card; a malformed reply is an error, not a default. */
export function parseConfirmCard(raw: unknown): ConfirmCard {
  const w = "confirm card";
  const r = record(raw, w);
  return {
    kind: oneOf(r, "kind", cardKinds, w),
    targetKind: str(r, "targetKind", w),
    targetId: str(r, "targetId", w),
    state: oneOf(r, "state", cardStates, w),
    message: str(r, "message", w),
    assignmentId: str(r, "assignmentId", w),
    title: str(r, "title", w),
    subject: str(r, "subject", w),
    due: str(r, "due", w),
    recipient: str(r, "recipient", w),
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
  /** «Да»: for a send, with the subject and format the card showed. A deletion is confirmed in the deletion window. */
  confirm(card: ConfirmCard): Promise<ConfirmCard>;
  /** «Нет»: the proposal can never run afterwards; a preview has nothing stored. */
  decline(target: ConfirmTarget): Promise<ConfirmCard>;
  /** «Да, в календарь» of «Сегодня»: the stored deadline of the assignment becomes a proposal; nothing goes out yet. */
  proposeDeadline(assignmentId: string): Promise<ConfirmCard>;
}

/** Builds the confirm API over a call function (the Wails runtime in the app, a fake in tests). */
export function agentConfirmApi(call: CallByName): AgentConfirmApi {
  return {
    card: async (t) => parseConfirmCard(await call(methods.card, t.kind, t.id)),
    confirm: async (c) =>
      parseConfirmCard(await call(methods.confirm, c.targetKind, c.targetId, c.emailSubject, c.attachmentFormat)),
    decline: async (t) => parseConfirmCard(await call(methods.decline, t.kind, t.id)),
    proposeDeadline: async (id) => parseConfirmCard(await call(methods.proposeDeadline, id)),
  };
}
