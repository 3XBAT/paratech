// Wrapper of the bound Go GoogleCalendarService (internal/input/wails/calendar.go): the Google Calendar sync of the
// «Подключения» page. The texts come from Go; the page only shows them.

import type { CallByName } from "./settings";

/** The sync job as the page reads it; "unavailable" means the state could not be read. */
export type CalendarSyncJobState = "idle" | "queued" | "running" | "retrying" | "waitingForNetwork" | "unavailable";

/** The last run since the app started; "" if none. */
export type CalendarSyncOutcome = "" | "ok" | "notSignedIn" | "signInExpired" | "failed";

/** The sync state as the Go binding returns it. */
export interface CalendarSyncState {
  signedIn: boolean;
  /** Lessons the calendar does not show as they are (at most 999; pendingMore says there are more). */
  pending: number;
  pendingMore: boolean;
  /** RFC 3339 UTC of the last lesson written; "" if none. */
  lastSyncedAt: string;
  state: CalendarSyncJobState;
  lastOutcome: CalendarSyncOutcome;
  /** One fixed line from Go that sums the state up. */
  message: string;
}

/** The answer to «Синхронизировать». */
export interface CalendarSyncResult {
  ok: boolean;
  message: string;
  state: CalendarSyncState;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  status: "paratech/desktop/internal/input/wails.GoogleCalendarService.GoogleCalendarStatus",
  sync: "paratech/desktop/internal/input/wails.GoogleCalendarService.GoogleCalendarSync",
} as const;

export const jobStates: readonly CalendarSyncJobState[] = [
  "idle", "queued", "running", "retrying", "waitingForNetwork", "unavailable",
];

export const outcomes: readonly CalendarSyncOutcome[] = ["", "ok", "notSignedIn", "signInExpired", "failed"];

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

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

/** Checks the shape of the sync state; a malformed reply is an error, not a default. */
export function parseCalendarSyncState(raw: unknown): CalendarSyncState {
  const w = "calendar state";
  const r = record(raw, w);
  const pending = r.pending;
  if (typeof pending !== "number" || !Number.isInteger(pending) || pending < 0) {
    throw new Error(`${w}: pending is not a count`);
  }
  if (!jobStates.includes(r.state as CalendarSyncJobState)) {
    throw new Error(`${w}: state is not a sync state`);
  }
  if (!outcomes.includes(r.lastOutcome as CalendarSyncOutcome)) {
    throw new Error(`${w}: lastOutcome is not a sync outcome`);
  }
  return {
    signedIn: bool(r, "signedIn", w),
    pending,
    pendingMore: bool(r, "pendingMore", w),
    lastSyncedAt: str(r, "lastSyncedAt", w),
    state: r.state as CalendarSyncJobState,
    lastOutcome: r.lastOutcome as CalendarSyncOutcome,
    message: str(r, "message", w),
  };
}

/** Checks the shape of the «Синхронизировать» answer. */
export function parseCalendarSyncResult(raw: unknown): CalendarSyncResult {
  const w = "calendar result";
  const r = record(raw, w);
  return { ok: bool(r, "ok", w), message: str(r, "message", w), state: parseCalendarSyncState(r.state) };
}

export interface CalendarApi {
  /** Reads the sync state; it does not call Google. */
  status(): Promise<CalendarSyncState>;
  /** Asks for a sync now; one that has not started yet already covers it. */
  sync(): Promise<CalendarSyncResult>;
}

/** Builds the API over a call function (the Wails runtime in the app, a fake in tests). */
export function calendarApi(call: CallByName): CalendarApi {
  return {
    status: async () => parseCalendarSyncState(await call(methods.status)),
    sync: async () => parseCalendarSyncResult(await call(methods.sync)),
  };
}
