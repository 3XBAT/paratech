// View logic of the calendar sync in the «Google Календарь» block of "Подключения". The sync rules and the status
// texts live in the Go core (ARCH-44); the page shows Go's message, the last sync time and «Синхронизировать».

import type { CalendarSyncState } from "./bindings/calendar";
// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { formatTime } from "./activityView.js";

export const calendarTexts = {
  title: "Синхронизация",
  sync: "Синхронизировать",
  syncing: "Запускаем…",
  busyReason: "Дождитесь окончания синхронизации.",
  lastSynced: "Последняя синхронизация:",
  loading: "Загрузка…",
  loadFailed: "Не удалось получить состояние синхронизации.",
  actionFailed: "Не удалось запустить синхронизацию.",
} as const;

export interface CalendarSyncView {
  /** The section is shown at all. */
  shown: boolean;
  /** The status line: Go's message, or the loading/failure text before the first answer. */
  message: string;
  /** "Последняя синхронизация: <local time>"; "" if there is none. */
  lastSynced: string;
  syncShown: boolean;
  syncDisabled: boolean;
  /** Why the button is disabled by the job; "" while the page's own call runs (the button says it). */
  syncDisabledReason: string;
}

const hidden: CalendarSyncView = {
  shown: false, message: "", lastSynced: "", syncShown: false, syncDisabled: true, syncDisabledReason: "",
};

/** The last sync time in local time; "" for none or an unparsable value. */
export function lastSyncedText(lastSyncedAt: string): string {
  const t = formatTime(lastSyncedAt);
  return t === "—" ? "" : `${calendarTexts.lastSynced} ${t}`;
}

function jobBusy(status: CalendarSyncState | null): boolean {
  return status !== null && (status.state === "queued" || status.state === "running");
}

/**
 * Signed in: the status, the last sync time and the button. Signed out: nothing, except Go's "sign in again" line
 * after an expired sign-in, so the reason the calendar stopped is visible.
 */
export function calendarSyncView(input: {
  account: string;
  status: CalendarSyncState | null;
  loadFailed: boolean;
  inFlight: boolean;
}): CalendarSyncView {
  const { account, status, loadFailed, inFlight } = input;
  if (account !== "signedIn") {
    if (status !== null && !status.signedIn && status.lastOutcome === "signInExpired") {
      return { ...hidden, shown: true, message: status.message };
    }
    return hidden;
  }

  const busy = jobBusy(status);
  return {
    shown: true,
    message: status !== null ? status.message : loadFailed ? calendarTexts.loadFailed : calendarTexts.loading,
    lastSynced: status !== null ? lastSyncedText(status.lastSyncedAt) : "",
    syncShown: true,
    syncDisabled: inFlight || busy,
    syncDisabledReason: !inFlight && busy ? calendarTexts.busyReason : "",
  };
}

/** How often the status is read while a sync is queued, running or waiting; reading it does not call Google. */
export const calendarPollMs = 4000;

/** The status is followed while the job may still change it by itself. */
export function calendarPolls(account: string, status: CalendarSyncState | null): boolean {
  if (account !== "signedIn" || status === null) {
    return false;
  }
  return ["queued", "running", "retrying", "waitingForNetwork"].includes(status.state);
}
