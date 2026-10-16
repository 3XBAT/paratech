// View logic of the remaining quota in the Hub block of "Подключения" (AC-12). The Go core decides the status, whether
// the value is stale and which values may be shown (ARCH-44); here only how the reply is put into words.

import type { Quota, QuotaStatus, SecretStatus } from "./bindings/connections";

/** The answer of the last "Обновить" on this page: the fixed text from Go. */
export interface QuotaMessage {
  text: string;
  ok: boolean;
}

export type Tone = "ok" | "error" | "warning" | "muted";

export interface QuotaLine {
  text: string;
  tone: Tone;
}

export interface QuotaDisplay {
  /** Remaining, limit and reserved with the unit; empty when the reply has no values. */
  amounts: readonly { label: string; value: string }[];
  /** "Обновлено: <local time>" of the values; "" without values. */
  updated: string;
  /** "" or why the values are an old copy. */
  stale: string;
  /** "" or when the allowance renews. */
  renews: string;
  /** That the values are a copy for display; "" without values. */
  note: string;
  /** "" or the time of the last attempt, when it gave nothing newer than what is shown. */
  checked: string;
  /** The text of the last refresh (from Go) or of the status, and where to fix a missing address or key. */
  lines: readonly QuotaLine[];
}

export const quotaTexts = {
  title: "Лимит обработки",
  refresh: "Обновить",
  refreshing: "Обновляем…",
  remaining: "Осталось",
  limit: "Лимит",
  reserved: "Зарезервировано идущими запросами",
  used: "Израсходовано",
  updated: "Обновлено: ",
  stale: "Устарело: последнее обновление не удалось, показано значение на это время.",
  renews: "Лимит обновится: ",
  note: "Это копия для показа. Хватит ли лимита, сервер обработки решает сам при каждом запросе.",
  checked: "Последняя попытка: ",
  notRequested: "Остаток ещё не запрашивался: нажмите «Обновить».",
  urlPointer: "Адрес сервера обработки не задан: укажите его в поле «Адрес сервера» выше и сохраните.",
  keyPointer: "Введите ключ в поле «Новый ключ» выше и сохраните его.",
  refreshFailed: "Не удалось получить остаток лимита.",
} as const;

/** What a status says when there is no answer of a refresh on this page (the page was opened after it). */
const statusTexts: Readonly<Record<QuotaStatus, QuotaLine | null>> = {
  unchecked: null,
  current: null,
  notConfigured: {
    text: "Адрес сервера обработки не задан или не подходит: укажите его в поле «Адрес сервера» выше.",
    tone: "warning",
  },
  noKey: { text: "Ключ доступа не задан.", tone: "warning" },
  keyRejected: { text: "Сервер обработки не принял ключ: он неверный или отозван. Введите действующий ключ.", tone: "error" },
  offline: { text: "Сервер обработки не ответил при последнем обновлении.", tone: "warning" },
  failed: { text: "Последнее обновление не удалось.", tone: "error" },
};

const unitNames: Readonly<Record<string, string>> = { tokens: "токенов" };

/** Whether the page may ask for the quota on its own: an address and a stored key. The Hub checks the rest. */
export function quotaRefreshable(c: { hubUrl: string; hubKey: SecretStatus }): boolean {
  return c.hubUrl !== "" && c.hubKey === "set";
}

/** A whole number with groups of three digits. */
export function formatAmount(n: number): string {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, " ");
}

/** The unit as a label suffix; an unknown unit from a newer core is shown as it is. */
export function unitLabel(unit: string): string {
  return Object.hasOwn(unitNames, unit) ? unitNames[unit] : unit;
}

/** An RFC 3339 time in local time; "" or an unparsable value is "—". */
export function localTime(rfc3339: string): string {
  if (rfc3339 === "") {
    return "—";
  }
  const d = new Date(rfc3339);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString("ru-RU");
}

/** Puts the quota of the last reply into words. message is the answer of a refresh made on this page, if any. */
export function quotaDisplay(
  q: Quota,
  c: { hubUrl: string; hubKey: SecretStatus },
  message: QuotaMessage | null,
): QuotaDisplay {
  const v = q.values;
  const unit = v === null ? "" : unitLabel(v.unit);
  const amount = (label: string, n: number) => ({ label: unit === "" ? label : `${label}, ${unit}`, value: formatAmount(n) });

  const lines: QuotaLine[] = [];
  if (message !== null) {
    lines.push({ text: message.text, tone: message.ok ? "ok" : "error" });
  } else if (q.status === "unchecked") {
    if (quotaRefreshable(c)) {
      lines.push({ text: quotaTexts.notRequested, tone: "muted" });
    }
  } else {
    const s = Object.hasOwn(statusTexts, q.status) ? statusTexts[q.status] : statusTexts.failed;
    if (s !== null) {
      lines.push(s);
    }
  }
  // Where to fix it, on this page. The Go text of "notConfigured" already points to the field.
  if (q.status === "unchecked" && c.hubUrl === "") {
    lines.push({ text: quotaTexts.urlPointer, tone: "warning" });
  }
  if (q.status === "noKey" || (q.status === "unchecked" && c.hubKey === "unset")) {
    lines.push({ text: quotaTexts.keyPointer, tone: "warning" });
  }

  return {
    amounts:
      v === null
        ? []
        : [amount(quotaTexts.remaining, v.remaining), amount(quotaTexts.limit, v.limit), amount(quotaTexts.reserved, v.reserved)],
    updated: v === null ? "" : quotaTexts.updated + localTime(v.fetchedAt),
    stale: v !== null && q.stale ? quotaTexts.stale : "",
    renews: v === null || v.periodEnd === "" ? "" : quotaTexts.renews + localTime(v.periodEnd),
    note: v === null ? "" : quotaTexts.note,
    checked: q.checkedAt !== "" && (v === null || q.stale) ? quotaTexts.checked + localTime(q.checkedAt) : "",
    lines,
  };
}

/** The used share of the limit in whole percent for the bar; null without values or a limit. */
export function quotaUsedPercent(q: Quota): number | null {
  const v = q.values;
  if (v === null || v.limit <= 0) {
    return null;
  }
  return Math.min(100, Math.max(0, Math.round(((v.limit - v.remaining) * 100) / v.limit)));
}

/** "Обновить" waits for a refresh in flight and for a change of the key or the address (it would be superseded). */
export function quotaControls(state: { refreshing: boolean; keyBusy: boolean; hubBusy: boolean }): {
  disabled: boolean;
  label: string;
} {
  return {
    disabled: state.refreshing || state.keyBusy || state.hubBusy,
    label: state.refreshing ? quotaTexts.refreshing : quotaTexts.refresh,
  };
}
