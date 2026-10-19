// Display mapping for the homework page: labels, chips and formats only. What can be sent, marked as sent or sent
// again is decided by the Go core (the send dialog's status, canMarkSent, canSendAgain), never here (ARCH-44).
// Model and student text is never formatted here: the page shows it as plain text.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import { formatTime } from "./activityView.js";
import {
  type ActiveGeneration,
  type DeliveryItem,
  type DocxSaveResult,
  type DraftStatus,
  errorMessages,
  type GenerationState,
  type GenerationStatus,
  type SendFormat,
  type VersionSource,
} from "./bindings/homework.js";
import { plural } from "./plural.js";
import type { ResultKind } from "./ui/controls.js";
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
/** How often an open assignment asks the core for a new version or a generation started elsewhere (TASK-094). */
export const draftPollMs = 3000;

interface Look {
  kind: ChipKind;
  icon: IconName;
  label: string;
}

const generationLooks: Readonly<Record<GenerationState, Look>> = {
  generating: { kind: "run", icon: "wave", label: "Готовим черновик…" },
  waitingNetwork: { kind: "net", icon: "off", label: "Ждёт интернет" },
  waitingQuota: { kind: "wait", icon: "clock", label: "Лимит обработки исчерпан" },
  failed: { kind: "fail", icon: "warn", label: "Черновик не готов" },
  done: { kind: "ok", icon: "ok", label: "Черновик готов" },
};

/**
 * The chip of a generation; the hint is the core's fixed text for the waiting and failed states, the progress of a
 * running one.
 */
export function generationView(s: GenerationStatus): ChipView {
  const look = generationLooks[s.state];
  let hint = s.message;
  if (hint === "" && s.state === "generating") {
    hint = s.percent > 0 && s.percent < 100 ? `Готово ${Math.round(s.percent)} %.` : "Обычно до минуты.";
  }
  return { ...look, hint };
}

/** The generation the page follows: its job and its last state. */
export interface FollowedGeneration {
  jobId: string;
  status: GenerationStatus;
}

/** The core's running generation (from openAssignment or draftStatus) as the page follows it. */
export function followGeneration(g: ActiveGeneration | null): FollowedGeneration | null {
  return g === null ? null : { jobId: g.jobId, status: { state: g.state, message: g.message, percent: g.percent } };
}

/**
 * What a draftStatus answer asks of the page: reload the assignment when a newer version than the latest one it
 * knows appeared, and follow a generation it does not follow yet (started before the page opened or elsewhere).
 */
export function draftStatusStep(
  st: DraftStatus,
  knownLatest: number,
  followedJob: string,
): { reload: boolean; follow: FollowedGeneration | null } {
  const follow = st.generation !== null && st.generation.jobId !== followedJob ? followGeneration(st.generation) : null;
  return { reload: st.latestVersion > knownLatest, follow };
}

/** The busy label of the generate button while the core prepares a draft. */
export const generatingLabel = "Готовим черновик…";

/** The generate button and the instructions field: off while a generation goes on or the shown text forbids one. */
export interface GenerateOffer {
  label: string;
  disabled: boolean;
  reason: string | undefined;
  instructionDisabled: boolean;
}

export function generateOffer(o: { hasDraft: boolean; generating: boolean; olderShown: boolean; dirty: boolean }): GenerateOffer {
  const label = o.hasDraft ? "Сгенерировать заново" : "Сгенерировать черновик";
  if (o.generating) {
    // The button's busy label says why.
    return { label, disabled: true, reason: undefined, instructionDisabled: true };
  }
  const reason = regenerateBlockedReason(o.olderShown, o.dirty);
  return { label, disabled: reason !== undefined, reason, instructionDisabled: false };
}

/** generate was refused because a generation of the assignment already goes on: the page picks it up. */
export function generationRunning(err: unknown): boolean {
  return errorMessage(err) === errorMessages.generationRunning;
}

/** The line under the draft after «Сохранить в Word (.docx)»: the core's text and the saved file's name. */
export function docxSaveView(r: DocxSaveResult): { kind: ResultKind; text: string } {
  switch (r.status) {
    case "saved": {
      const text = r.name === "" ? r.message : `${r.message} Файл: «${r.name}».`;
      // Saved, but the file a send in .docx attaches is not updated: the message says why.
      return { kind: r.attachmentName === "" ? "wait" : "ok", text };
    }
    case "cancelled":
      return { kind: "wait", text: r.message };
    case "failed":
      return { kind: "fail", text: r.message };
  }
}

/** The attachment a send dialog starts with; the student's last choice on the page wins. */
export const defaultSendFormat: SendFormat = "docx";

export const sendFormatLabels: Readonly<Record<SendFormat, string>> = {
  docx: "Word (.docx)",
  txt: "Текст (.txt)",
};

/** A new choice in the dialog needs a new export and preparation, unless nothing changes or the dialog is busy. */
export function sendFormatChange(current: SendFormat, next: SendFormat, busy: boolean): boolean {
  return !busy && next !== current;
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
