// View logic of the "Подключения" page: form mapping, labels and button states. The rules of the values live in the
// Go settings aggregate (ARCH-44); here only what the page needs to show and send them.

import type {
  CheckResult,
  Connections,
  FieldError,
  MailSecurity,
  MailSettingsInput,
  SecretStatus,
} from "./bindings/connections";
import type { CalendarSyncState } from "./bindings/calendar";
import type { GoogleState } from "./bindings/google";
import type { IconName } from "./ui/Icon.js";
import type { ChipKind } from "./ui/StatusChip.js";

/** The mail form as typed. There is no password here: the secret slot keeps it apart. */
export interface MailForm {
  recipient: string;
  smtpHost: string;
  /** As typed; "" is the default port of the mode. */
  smtpPort: string;
  smtpSecurity: MailSecurity;
  smtpLogin: string;
}

/** The field codes of the binding (connections.Field in Go). */
export type FieldCode = "hubUrl" | "recipient" | "smtpHost" | "smtpPort" | "smtpSecurity" | "smtpLogin";

export const hubFields: readonly FieldCode[] = ["hubUrl"];

/** The mail fields in the order of the form. */
export const mailFields: readonly { code: Exclude<FieldCode, "hubUrl">; label: string }[] = [
  { code: "recipient", label: "Адрес получателя домашки" },
  { code: "smtpHost", label: "SMTP-сервер" },
  { code: "smtpPort", label: "Порт" },
  { code: "smtpSecurity", label: "Режим TLS" },
  { code: "smtpLogin", label: "Логин (адрес почты)" },
];

/** No plaintext option: both modes are TLS. */
export const securityOptions: readonly { value: MailSecurity; label: string }[] = [
  { value: "starttls", label: "STARTTLS" },
  { value: "tls", label: "TLS" },
];

export const texts = {
  hubPlaceholder: "https://…",
  portHint: "Пусто или 0 — по умолчанию: 587 для STARTTLS, 465 для TLS.",
  mailConfigured: "Почта настроена.",
  mailNotConfigured: "Почта не настроена: нужны адрес получателя, SMTP-сервер и логин (адрес почты).",
  saved: "Сохранено.",
  checkSaveFirst: "Сначала сохраните изменения: проверяются сохранённые настройки.",
  checkNoSend: "Письмо не отправляется: только соединение и вход.",
  checking: "Проверяем…",
  loading: "Загрузка…",
  loadFailed: "Не удалось загрузить настройки подключений.",
  saveFailed: "Не удалось сохранить настройки подключений.",
  checkFailed: "Не удалось проверить почту.",
  unknownField: "Недопустимое значение.",
} as const;

/** The form of the stored values; port 0 shows as empty. */
export function mailFormFrom(c: Connections): MailForm {
  return {
    recipient: c.recipient,
    smtpHost: c.smtpHost,
    smtpPort: c.smtpPort === 0 ? "" : String(c.smtpPort),
    smtpSecurity: c.smtpSecurity,
    smtpLogin: c.smtpLogin,
  };
}

/** What a port that is not a number is sent as: the core refuses it, with the other refused fields. */
export const notAPort = -1;

/** The port to send: "" is 0 (the default of the mode), digits are the number, anything else is notAPort. */
export function parsePort(text: string): number {
  const t = text.trim();
  if (t === "") {
    return 0;
  }
  // Five digits at most keep the number an int for Go; 65536..99999 reach the core and are refused there.
  return /^\d{1,5}$/.test(t) ? Number(t) : notAPort;
}

/** The input of SaveMailSettings. Fields are picked one by one so nothing else on the form object reaches the core. */
export function mailInputFrom(form: MailForm): MailSettingsInput {
  return {
    recipient: form.recipient,
    smtpHost: form.smtpHost,
    smtpPort: parsePort(form.smtpPort),
    smtpSecurity: form.smtpSecurity,
    smtpLogin: form.smtpLogin,
  };
}

/** Whether the form differs from the stored values (a port of "" and "0" is the same). */
export function mailFormDirty(form: MailForm, saved: Connections): boolean {
  return (
    form.recipient !== saved.recipient ||
    form.smtpHost !== saved.smtpHost ||
    parsePort(form.smtpPort) !== saved.smtpPort ||
    form.smtpSecurity !== saved.smtpSecurity ||
    form.smtpLogin !== saved.smtpLogin
  );
}

/** The text of the field's error, "" when it has none. */
export function fieldErrorText(errors: readonly FieldError[], field: FieldCode): string {
  return errors
    .filter((e) => e.field === field)
    .map((e) => e.message)
    .join(" ");
}

/** Errors of fields the block does not show, so none is lost (an unknown code from a newer core). */
export function otherErrorTexts(errors: readonly FieldError[], shown: readonly FieldCode[]): string[] {
  return errors
    .filter((e) => !(shown as readonly string[]).includes(e.field))
    .map((e) => (e.message === "" ? texts.unknownField : e.message));
}

export function mailConfiguredText(configured: boolean): string {
  return configured ? texts.mailConfigured : texts.mailNotConfigured;
}

/** The texts of one secret slot (the mail password, the Hub key). */
export interface SecretLabels {
  title: string;
  status: Readonly<Record<SecretStatus, string>>;
  input: string;
  save: string;
  replaceHint: string;
  remove: string;
  /** The question of the confirmation dialog. */
  confirmTitle: string;
  confirmRemove: string;
  confirmYes: string;
  confirmNo: string;
}

/** Under a slot whose store answers: both secrets are kept in Windows Credential Manager by the core. */
export const secretStoreCaption = "Хранится в диспетчере учётных данных Windows, а не в файлах приложения.";

export const mailPasswordLabels: SecretLabels = {
  title: "Пароль почты",
  status: {
    set: "Пароль задан",
    unset: "Пароль не задан",
    unsupported: "Хранилище паролей недоступно в этой системе",
    unavailable: "Хранилище паролей не ответило",
  },
  input: "Новый пароль",
  save: "Сохранить пароль",
  replaceHint: "Новый пароль заменит сохранённый.",
  remove: "Удалить пароль",
  confirmTitle: "Удалить сохранённый пароль почты?",
  confirmRemove: "Письма не будут отправляться, пока не зададите новый.",
  confirmYes: "Удалить пароль",
  confirmNo: "Отмена",
};

export const hubKeyLabels: SecretLabels = {
  title: "Ключ доступа",
  status: {
    set: "Ключ задан",
    unset: "Ключ не задан",
    unsupported: "Хранилище ключей недоступно в этой системе",
    unavailable: "Хранилище ключей не ответило",
  },
  input: "Новый ключ",
  save: "Сохранить ключ",
  replaceHint: "Новый ключ заменит сохранённый.",
  remove: "Удалить ключ",
  confirmTitle: "Удалить сохранённый ключ доступа?",
  confirmRemove: "Конспекты и поиск домашних заданий будут ждать, пока не зададите новый.",
  confirmYes: "Удалить ключ",
  confirmNo: "Отмена",
};

/** The label of a status; an unknown status is shown as "не ответило" rather than as raw text. */
export function secretStatusLabel(labels: SecretLabels, status: string): string {
  return Object.hasOwn(labels.status, status) ? labels.status[status as SecretStatus] : labels.status.unavailable;
}

/**
 * The chip of a status (one to three words): the label of a stored or missing secret, a short word otherwise. Such a
 * word comes with the full label next to it (secretStatusNote).
 */
export function secretChipLabel(labels: SecretLabels, status: string): string {
  if (status === "set" || status === "unset") {
    return labels.status[status];
  }
  return status === "unsupported" ? "Недоступно" : "Не ответило";
}

/** The full status shown next to a short chip; "" when the chip is the label. */
export function secretStatusNote(labels: SecretLabels, status: string): string {
  return status === "set" || status === "unset" ? "" : secretStatusLabel(labels, status);
}

export interface SecretControls {
  inputDisabled: boolean;
  saveDisabled: boolean;
  /** "Удалить" is offered only for a stored secret. */
  removeShown: boolean;
  removeDisabled: boolean;
}

/** What the slot allows: nothing without a store; saving needs a typed value. */
export function secretControls(status: SecretStatus, busy: boolean, typed: boolean): SecretControls {
  const unsupported = status === "unsupported";
  return {
    inputDisabled: unsupported || busy,
    saveDisabled: unsupported || busy || !typed,
    removeShown: status === "set",
    removeDisabled: busy,
  };
}

/** Whether a secret change answer is a success (for its style): stored is "set", removed is "unset". */
export function secretOutcomeOk(action: "store" | "remove", status: SecretStatus): boolean {
  return action === "store" ? status === "set" : status === "unset";
}

export interface CheckControls {
  disabled: boolean;
  /** "" or why the check waits for a save. */
  hint: string;
}

/**
 * The check uses the stored settings and password, so it waits for unsaved changes, for a save or check in flight and
 * for a password change in flight.
 */
export function checkControls(state: {
  dirty: boolean;
  checking: boolean;
  saving: boolean;
  secretBusy: boolean;
}): CheckControls {
  return {
    disabled: state.dirty || state.checking || state.saving || state.secretBusy,
    hint: state.dirty ? texts.checkSaveFirst : "",
  };
}

/**
 * The text of a rejected call. The binding rejects only with one fixed error (or the reply is malformed), so the
 * text depends on the action alone and the raw error text is never shown.
 */
export function failureText(action: "load" | "save" | "check"): string {
  return action === "load" ? texts.loadFailed : action === "save" ? texts.saveFailed : texts.checkFailed;
}

/** A section of the page: a card on the overview, its own page when opened. */
export type ConnSection = "hub" | "mail" | "google" | "apple";

/** The chip of an overview card. */
export interface CardStatus {
  kind: ChipKind;
  icon: IconName | null;
  text: string;
}

export const overviewTexts = {
  lead: "Конспекты и задания, почта и Google Календарь",
  title: "Подключения",
  back: "Назад",
  hubTitle: "Конспекты и задания",
  hubHint:
    "Ключ доступа выдаёт автор приложения. Это не ключ Claude или ChatGPT. Распознавание речи всегда идёт на этом компьютере.",
  hubDesc: "Адрес, ключ доступа и лимит обработки",
  mailTitle: "Почта",
  mailDesc: "Отправка домашних заданий письмом",
  googleDesc: "Вход и синхронизация расписания",
} as const;

const status = (kind: ChipKind, text: string): CardStatus => ({
  kind,
  icon: kind === "ok" ? "ok" : kind === "fail" || kind === "wait" || kind === "net" ? "warn" : kind === "run" ? "clock" : null,
  text,
});

const loading = status("neutral", "Загрузка…");
const loadFailed = status("fail", "Не загрузилось");

/**
 * The card of the Hub from what the page already has: the address, the key status and the quota of the last refresh
 * (the page asks for it on open). "Подключено" only after the Hub answered with the quota.
 */
export function hubCardStatus(s: { saved: Connections | null; loadFailed: boolean; refreshing: boolean }): CardStatus {
  const c = s.saved;
  if (c === null) {
    return s.loadFailed ? loadFailed : loading;
  }
  if (c.hubUrl === "") {
    return status("wait", "Не настроено");
  }
  if (c.hubKey === "unset") {
    return status("wait", "Нет ключа");
  }
  if (c.hubKey !== "set") {
    return status("fail", "Ключ недоступен");
  }
  if (s.refreshing) {
    return status("run", "Проверяем…");
  }
  switch (c.quota.status) {
    case "current":
      return status("ok", "Подключено");
    case "unchecked":
      return status("neutral", "Не проверено");
    case "notConfigured":
      return status("wait", "Не настроено");
    case "noKey":
      return status("wait", "Нет ключа");
    case "keyRejected":
      return status("fail", "Ключ не принят");
    case "offline":
      return status("net", "Нет связи");
    default:
      return status("fail", "Ошибка проверки");
  }
}

/** The card of the mail: the saved settings, the password status and the check made on this page, if any. */
export function mailCardStatus(s: { saved: Connections | null; loadFailed: boolean; check: CheckResult | null }): CardStatus {
  const c = s.saved;
  if (c === null) {
    return s.loadFailed ? loadFailed : loading;
  }
  if (!c.mailConfigured) {
    return status("wait", "Не настроено");
  }
  if (c.mailPassword === "unset") {
    return status("wait", "Нет пароля");
  }
  if (c.mailPassword !== "set") {
    return status("fail", "Пароль недоступен");
  }
  if (s.check !== null) {
    return s.check.ok ? status("ok", "Проверено") : status("fail", "Проверка не прошла");
  }
  // Without a check nothing says the server accepts the login, so no green.
  return status("neutral", "Настроено");
}

/**
 * The card of Google from GoogleStatus (the client, the account and a sign-in waiting for the browser) and the
 * calendar attention Go reports: what only the user can fix turns the card red.
 */
export function googleCardStatus(s: {
  state: GoogleState | null;
  loadFailed: boolean;
  calendar?: CalendarSyncState | null;
}): CardStatus {
  const g = s.state;
  if (g === null) {
    return s.loadFailed ? status("fail", "Не удалось проверить") : loading;
  }
  if (g.signingIn) {
    return status("run", "Ожидание входа");
  }
  if (g.client === "unset") {
    return status("wait", "Не настроено");
  }
  if (g.client !== "set") {
    return status("fail", "Недоступно");
  }
  if (g.account === "signedIn" || g.account === "signedOut") {
    // "signIn" comes while signed out, "noAccess" while signed in: Go decides, the card only names it.
    switch (s.calendar?.attention) {
      case "signIn":
        return status("fail", "Нужен вход");
      case "noAccess":
        return status("fail", "Нет доступа");
    }
  }
  switch (g.account) {
    case "signedIn":
      return status("ok", "Подключено");
    case "signedOut":
      return status("wait", "Вход не выполнен");
    case "unsupported":
      return status("fail", "Недоступно");
    default:
      return status("fail", "Не удалось проверить");
  }
}

/** The Apple card is a help screen: there is nothing to connect, so its chip only says what it is. */
export const appleCardStatus: CardStatus = status("neutral", "Инструкция");
