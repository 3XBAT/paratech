// Display mapping for the homework page: labels, chips and formats only. What can be sent, marked as sent or sent
// again is decided by the Go core (the send dialog's status, canMarkSent, canSendAgain), never here (ARCH-44).
// Model and student text is never formatted here: the page shows it as plain text.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import { formatTime } from "./activityView.js";
import {
  type DeliveryItem,
  errorMessages,
  type GenerationState,
  type GenerationStatus,
  type VersionSource,
} from "./bindings/homework.js";
import { plural } from "./plural.js";
import type { IconName } from "./ui/Icon.js";
import type { ChipKind } from "./ui/StatusChip.js";

/** A chip with its one-sentence hint ("" for none). */
export interface ChipView {
  kind: ChipKind;
  icon: IconName;
  label: string;
  hint: string;
}

/** How often the page asks for a running generation and for emails on their way. */
export const generationPollMs = 2000;
export const deliveryPollMs = 3000;

interface Look {
  kind: ChipKind;
  icon: IconName;
  label: string;
}

const generationLooks: Readonly<Record<GenerationState, Look>> = {
  generating: { kind: "run", icon: "wave", label: "Готовится черновик" },
  waitingNetwork: { kind: "net", icon: "off", label: "Ждёт интернет" },
  waitingQuota: { kind: "wait", icon: "clock", label: "Лимит обработки исчерпан" },
  failed: { kind: "fail", icon: "warn", label: "Черновик не готов" },
  done: { kind: "ok", icon: "ok", label: "Черновик готов" },
};

/** The chip of a generation; the hint is the core's fixed text for the waiting and failed states. */
export function generationView(s: GenerationStatus): ChipView {
  const look = generationLooks[s.state];
  const hint = s.message !== "" ? s.message : s.state === "generating" ? "Обычно до минуты." : "";
  return { ...look, hint };
}

/** A generation in these states goes on in the core: the page keeps asking. */
export function generationGoesOn(state: GenerationState): boolean {
  return state === "generating" || state === "waitingNetwork" || state === "waitingQuota";
}

/** Labels of the delivery statuses (domain/delivery/delivery). */
export const deliveryLooks: Readonly<Record<string, Look>> = {
  PENDING: { kind: "neutral", icon: "clock", label: "Ждёт отправки" },
  SENDING: { kind: "run", icon: "mail", label: "Отправляется" },
  SENT: { kind: "ok", icon: "ok", label: "Отправлено" },
  FAILED: { kind: "fail", icon: "warn", label: "Не отправлено" },
  UNKNOWN: { kind: "wait", icon: "warn", label: "Нет подтверждения" },
};

const pendingHint = "Письмо уйдёт само, как только будет связь с почтой.";

/** The chip of an email; problem is the core's last error of a failed one ("" otherwise). */
export interface DeliveryView extends ChipView {
  problem: string;
}

const failedHint = "Проверьте почту в разделе «Подключения» и отправьте ещё раз.";

/**
 * The chip of an email. The hint: the time it was sent, what to do about a failed one, the core's notice of an
 * unknown outcome. An unknown status is shown as is.
 */
export function deliveryView(d: DeliveryItem): DeliveryView {
  const look = Object.hasOwn(deliveryLooks, d.status)
    ? deliveryLooks[d.status]
    : { kind: "neutral" as const, icon: "mail" as const, label: d.status };
  let hint = "";
  switch (d.status) {
    case "PENDING":
      hint = pendingHint;
      break;
    case "SENT":
      hint = d.sentAt === "" ? "" : `Отправлено ${formatTime(d.sentAt)}`;
      break;
    case "FAILED":
      hint = failedHint;
      break;
    case "UNKNOWN":
      hint = d.notice;
      break;
  }
  return { ...look, hint, problem: d.status === "FAILED" ? d.lastError : "" };
}

const noDelivery: ChipView = { kind: "neutral", icon: "mail", label: "Не отправлялось", hint: "" };

/** The chip of an assignment in the list: its newest email (the core lists them newest first). */
export function assignmentDeliveryView(ds: readonly DeliveryItem[] | undefined): ChipView | null {
  if (ds === undefined) {
    return null;
  }
  if (ds.length === 0) {
    return noDelivery;
  }
  const { kind, icon, label } = deliveryView(ds[0]);
  return { kind, icon, label, hint: "" };
}

/** Emails on their way change by themselves: the page keeps asking while there is one. */
export function deliveriesInFlight(ds: readonly DeliveryItem[]): boolean {
  return ds.some((d) => d.status === "PENDING" || d.status === "SENDING");
}

const sourceLabels: Readonly<Record<VersionSource, string>> = {
  generated: "сгенерирована",
  edited: "исправлена вручную",
};

/** "Версия 3 · исправлена вручную". */
export function versionTitle(v: { number: number; source: VersionSource }): string {
  return `Версия ${v.number} · ${sourceLabels[v.source]}`;
}

/** "до 18.09.2026, 18:00" or "Без срока". */
export function dueText(due: string): string {
  return due === "" ? "Без срока" : `до ${formatTime(due)}`;
}

/** An attachment size for people: "800 байт", "12,5 КБ", "1,2 МБ". */
export function sizeText(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return "—";
  }
  if (bytes < 1024) {
    const n = Math.trunc(bytes);
    return `${n} ${plural(n, "байт", "байта", "байт")}`;
  }
  const kb = bytes / 1024;
  if (kb < 1024) {
    return `${oneDecimal(kb)} КБ`;
  }
  return `${oneDecimal(kb / 1024)} МБ`;
}

function oneDecimal(n: number): string {
  return (Math.round(n * 10) / 10).toLocaleString("ru-RU", { maximumFractionDigits: 1 });
}

const knownErrors: ReadonlySet<string> = new Set(Object.values(errorMessages));

/** The core's fixed text of a failed call as it is; anything else (a malformed reply) gets the generic text. */
export function errorText(err: unknown): string {
  const msg = errorMessage(err);
  return knownErrors.has(msg) ? msg : errorMessages.unavailable;
}

/** Whether a failed call means the draft changed meanwhile and the page should reload it. */
export function isStale(err: unknown): boolean {
  return errorMessage(err) === errorMessages.draftStale;
}

/** After sendIncomplete the dialog offers «Повторить» with the same send: the core sends no second email. */
export function sendRetryable(err: unknown): boolean {
  return errorMessage(err) === errorMessages.sendIncomplete;
}

/** The call was refused because of an unresolved email: the page reloads the emails so the student sees it. */
export function blockedByEmail(err: unknown): boolean {
  const msg = errorMessage(err);
  return msg === errorMessages.exportLocked || msg === errorMessages.sendUnresolved;
}

/**
 * Why «Сгенерировать заново» is off, undefined when it is on. A new version always continues the latest one, so it is
 * off while an older version is shown: the student picks «К последней версии» or «Взять этот текст» first.
 */
export function regenerateBlockedReason(olderShown: boolean, dirty: boolean): string | undefined {
  if (dirty) {
    return "Сначала сохраните или отмените правку.";
  }
  if (olderShown) {
    return "Новая версия делается из последней: откройте её или возьмите этот текст.";
  }
  return undefined;
}

// Wails rejects a call with an Error-like object carrying the Go error text.
function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}
