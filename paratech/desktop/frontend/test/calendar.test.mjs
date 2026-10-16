// Google Calendar sync in the Google block of "Подключения" (TASK-051). Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  calendarApi,
  jobStates,
  methods,
  outcomes,
  parseCalendarSyncResult,
  parseCalendarSyncState,
} from "../.test-build/bindings/calendar.js";
import {
  calendarPollMs,
  calendarPolls,
  calendarSyncView,
  calendarTexts,
  lastSyncedText,
} from "../.test-build/calendarView.js";

// Mirrors the JSON of internal/input/wails/calendar.go (TestGoogleCalendarStatusJSON).
const synced = {
  signedIn: true, pending: 0, pendingMore: false, lastSyncedAt: "2026-10-16T06:30:00Z", state: "idle", lastOutcome: "ok",
  message: "Календарь «Learning Assistant» синхронизирован.",
};
const st = (over = {}) => ({ ...synced, ...over });
const result = (state, over = {}) => ({ ok: true, message: "Синхронизация запущена.", state, ...over });

const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("the api calls the bound Go methods", async () => {
  const p = "paratech/desktop/internal/input/wails.GoogleCalendarService.";
  assert.deepEqual(Object.values(methods).sort(), ["GoogleCalendarStatus", "GoogleCalendarSync"].map((m) => p + m));

  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return method.endsWith(".GoogleCalendarStatus") ? synced : result(st({ state: "queued" }));
  };
  const api = calendarApi(call);
  assert.deepEqual(await api.status(), synced);
  assert.deepEqual(await api.sync(), result(st({ state: "queued" })));
  assert.deepEqual(calls, [[p + "GoogleCalendarStatus"], [p + "GoogleCalendarSync"]]);

  await assert.rejects(calendarApi(async () => { throw new Error("x"); }).sync(), /x/);
});

test("replies are parsed strictly; extra fields are dropped", () => {
  assert.deepEqual(jobStates, ["idle", "queued", "running", "retrying", "waitingForNetwork", "unavailable"]);
  assert.deepEqual(outcomes, ["", "ok", "notSignedIn", "signInExpired", "failed"]);
  for (const state of jobStates) {
    for (const lastOutcome of outcomes) {
      assert.deepEqual(parseCalendarSyncState(st({ state, lastOutcome })), st({ state, lastOutcome }));
    }
  }
  assert.deepEqual(parseCalendarSyncState({ ...synced, token: "ya29.fake" }), synced);
  for (const raw of [
    undefined, null, "idle", {},
    st({ signedIn: "yes" }), st({ pending: -1 }), st({ pending: 1.5 }), st({ pending: "3" }), st({ pendingMore: 0 }),
    st({ lastSyncedAt: null }), st({ state: "QUEUED" }), st({ state: "toString" }), st({ lastOutcome: "maybe" }),
    st({ lastOutcome: undefined }), st({ message: 1 }),
  ]) {
    assert.throws(() => parseCalendarSyncState(raw), /calendar state/);
    assert.throws(() => parseCalendarSyncResult(result(raw)), /calendar state/);
  }
  assert.deepEqual(parseCalendarSyncResult({ ...result(synced, { ok: false }), x: 1 }), result(synced, { ok: false }));
  for (const raw of [null, {}, result(synced, { ok: "true" }), result(synced, { message: null }), { ok: true, message: "m" }]) {
    assert.throws(() => parseCalendarSyncResult(raw), /calendar (result|state)/);
  }
});

test("the last sync time is local; none or a bad one is not shown", () => {
  assert.equal(lastSyncedText(""), "");
  assert.equal(lastSyncedText("not a time"), "");
  const t = new Date("2026-10-16T06:30:00Z").toLocaleString("ru-RU");
  assert.equal(lastSyncedText("2026-10-16T06:30:00Z"), `Последняя синхронизация: ${t}`);
});

test("signed in: Go's message, the time and the button, disabled while queued, running or in flight", () => {
  const base = { account: "signedIn", loadFailed: false, inFlight: false };

  assert.deepEqual(calendarSyncView({ ...base, status: synced }), {
    shown: true, message: synced.message, lastSynced: lastSyncedText(synced.lastSyncedAt), syncShown: true,
    syncDisabled: false, syncDisabledReason: "",
  });
  assert.equal(calendarSyncView({ ...base, status: st({ lastSyncedAt: "" }) }).lastSynced, "");

  for (const state of ["queued", "running"]) {
    const v = calendarSyncView({ ...base, status: st({ state }) });
    assert.equal(v.syncDisabled, true, state);
    assert.equal(v.syncDisabledReason, calendarTexts.busyReason, state);
  }
  for (const state of ["idle", "retrying", "waitingForNetwork", "unavailable"]) {
    assert.equal(calendarSyncView({ ...base, status: st({ state }) }).syncDisabled, false, state);
  }
  const own = calendarSyncView({ ...base, status: st({ state: "queued" }), inFlight: true });
  assert.equal(own.syncDisabled, true);
  assert.equal(own.syncDisabledReason, "", "the busy label says it");

  const loading = calendarSyncView({ ...base, status: null });
  assert.equal(loading.message, calendarTexts.loading);
  assert.equal(loading.lastSynced, "");
  assert.equal(loading.syncShown, true);
  assert.equal(calendarSyncView({ ...base, status: null, loadFailed: true }).message, calendarTexts.loadFailed);
  // A later answer wins over an earlier failure.
  assert.equal(calendarSyncView({ ...base, status: synced, loadFailed: true }).message, synced.message);
});

test("signed out: nothing, except the line after an expired sign-in", () => {
  for (const account of ["signedOut", "unsupported", "unavailable"]) {
    const base = { account, loadFailed: false, inFlight: false };
    assert.equal(calendarSyncView({ ...base, status: null }).shown, false, account);
    assert.equal(calendarSyncView({ ...base, status: st({ signedIn: false, lastOutcome: "" }) }).shown, false, account);
    assert.equal(calendarSyncView({ ...base, status: null, loadFailed: true }).shown, false, account);

    const msg = "Вход в Google истёк: войдите снова, чтобы продолжить синхронизацию.";
    const v = calendarSyncView({ ...base, status: st({ signedIn: false, lastOutcome: "signInExpired", message: msg }) });
    assert.deepEqual(v, { shown: true, message: msg, lastSynced: "", syncShown: false, syncDisabled: true, syncDisabledReason: "" });
  }
});

test("the status is polled only while the job may change it", () => {
  assert.ok(calendarPollMs >= 3000 && calendarPollMs <= 5000);
  assert.equal(calendarPolls("signedIn", null), false);
  for (const state of jobStates) {
    const want = ["queued", "running", "retrying", "waitingForNetwork"].includes(state);
    assert.equal(calendarPolls("signedIn", st({ state })), want, state);
    assert.equal(calendarPolls("signedOut", st({ state })), false, state);
  }
});

test("the sync part renders text only, polls with a cleanup and is part of the Google block", () => {
  for (const file of ["../src/pages/CalendarSync.tsx", "../src/calendarView.ts", "../src/bindings/calendar.ts"]) {
    const s = src(file);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "console.", "localStorage", "sessionStorage", "href=",
      "fetch(", "googleapis.com", "accounts.google.com"]) {
      assert.ok(!s.includes(banned), `${file} uses ${banned}`);
    }
  }
  const part = src("../src/pages/CalendarSync.tsx");
  assert.match(part, /return \(\) => clearInterval\(timer\)/);
  assert.match(part, /\[api, props\.account\]/, "the status is read again after a sign-in or a sign-out");
  assert.match(src("../src/pages/GoogleBlock.tsx"), /<CalendarSync account=\{state\.account\} \/>/);
});
