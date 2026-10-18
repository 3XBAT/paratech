// View logic of the calendar sync in the «Google Календарь» block of "Подключения". The sync rules and the status
// texts live in the Go core (ARCH-44); the page shows Go's message, the last finished sync and «Синхронизировать».

import type { CalendarSyncOutcome, CalendarSyncState } from "./bindings/calendar";
// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { formatTime } from "./activityView.js";
import { plural } from "./plural.js";

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

/** The result line of the last finished sync: Go's text and its style. */
export interface LastRunResult {
  text: string;
  kind: "ok" | "wait" | "fail";
}

export interface CalendarSyncView {
  /** The section is shown at all. */
  shown: boolean;
  /** The status line: Go's message, or the loading/failure text before the first answer. */
  message: string;
  /** Go asks the user to act (attention), so the status line is an alert. */
  messageAlert: boolean;
  /** "Последняя синхронизация: <local time>" of the last finished job, with its attempts if more than one; "" if none. */
  lastSynced: string;
  /** Go's result of that job; null if Go gives none. */
  lastRunResult: LastRunResult | null;
  syncShown: boolean;
  syncDisabled: boolean;
  /** Why the button is disabled by the job; "" while the page's own call runs (the button says it). */
  syncDisabledReason: string;
}

const hidden: CalendarSyncView = {
  shown: false, message: "", messageAlert: false, lastSynced: "", lastRunResult: null, syncShown: false,
  syncDisabled: true, syncDisabledReason: "",
};

/**
 * The last finished sync in local time, with "· N попытки" when it took more than one; "" for none or an unparsable
 * time.
 */
export function lastSyncedText(lastRunAt: string, attempts = 0): string {
  const t = formatTime(lastRunAt);
  if (t === "—") {
    return "";
  }
  const tries = attempts > 1 ? ` · ${attempts} ${plural(attempts, "попытка", "попытки", "попыток")}` : "";
  return `${calendarTexts.lastSynced} ${t}${tries}`;
}

const resultKinds: Readonly<Record<CalendarSyncOutcome, LastRunResult["kind"] | null>> = {
  "": null, ok: "ok", notSignedIn: "wait", signInExpired: "fail", noAccess: "fail", failed: "fail",
};

/** Go's line of the last finished job, styled by its outcome; null when Go has no text for it. */
export function lastRunResult(status: CalendarSyncState): LastRunResult | null {
  const kind = resultKinds[status.lastRunOutcome];
  if (status.lastRunAt === "" || status.lastRunText === "" || kind === null) {
    return null;
  }
  return { text: status.lastRunText, kind };
}

function jobBusy(status: CalendarSyncState | null): boolean {
  return status !== null && (status.state === "queued" || status.state === "running");
}

/**
 * Signed in: the status, the last finished sync with its result and the button. Signed out: nothing, except Go's
 * line when Go asks to sign in again (an expired sign-in), so the reason the calendar stopped is visible.
 */
export function calendarSyncView(input: {
  account: string;
  status: CalendarSyncState | null;
  loadFailed: boolean;
  inFlight: boolean;
}): CalendarSyncView {
  const { account, status, loadFailed, inFlight } = input;
  if (account !== "signedIn") {
    if (status !== null && status.attention === "signIn") {
      return { ...hidden, shown: true, message: status.message, messageAlert: true };
    }
    return hidden;
  }

  const busy = jobBusy(status);
  return {
    shown: true,
    message: status !== null ? status.message : loadFailed ? calendarTexts.loadFailed : calendarTexts.loading,
    messageAlert: status !== null && status.attention !== "",
    lastSynced: status !== null ? lastSyncedText(status.lastRunAt, status.lastRunAttempts) : "",
    lastRunResult: status !== null ? lastRunResult(status) : null,
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
