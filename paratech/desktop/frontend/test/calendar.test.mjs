// Google Calendar sync in the Google block of "Подключения" (TASK-051). Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  attentions,
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
  lastRunResult,
  lastSyncedText,
} from "../.test-build/calendarView.js";

// Mirrors the JSON of internal/input/wails/calendar.go (TestGoogleCalendarStatusJSON).
const synced = {
  signedIn: true, pending: 0, pendingMore: false, lastSyncedAt: "2026-10-16T06:30:00Z", state: "idle", lastOutcome: "ok",
  attention: "", message: "Календарь «paratech» синхронизирован.",
  lastRunAt: "2026-10-16T06:31:00Z", lastRunAttempts: 1, lastRunOutcome: "ok", lastRunText: "Успешно.",
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
  assert.deepEqual(outcomes, ["", "ok", "notSignedIn", "signInExpired", "noAccess", "failed"]);
  for (const state of jobStates) {
    for (const lastOutcome of outcomes) {
      assert.deepEqual(parseCalendarSyncState(st({ state, lastOutcome })), st({ state, lastOutcome }));
    }
  }
  for (const lastRunOutcome of outcomes) {
    assert.deepEqual(parseCalendarSyncState(st({ lastRunOutcome })), st({ lastRunOutcome }));
  }
  assert.deepEqual(attentions, ["", "signIn", "noAccess"]);
  for (const attention of attentions) {
    assert.deepEqual(parseCalendarSyncState(st({ attention })), st({ attention }));
  }
  // The removed field of an older core is dropped like any extra one.
  assert.deepEqual(parseCalendarSyncState({ ...synced, lastRunOk: true }), synced);
  const never = st({ lastRunAt: "", lastRunAttempts: 0, lastRunOutcome: "", lastRunText: "" });
  assert.deepEqual(parseCalendarSyncState(never), never);
  assert.deepEqual(parseCalendarSyncState({ ...synced, token: "ya29.fake" }), synced);
  for (const raw of [
    undefined, null, "idle", {},
    st({ signedIn: "yes" }), st({ pending: -1 }), st({ pending: 1.5 }), st({ pending: "3" }), st({ pendingMore: 0 }),
    st({ lastSyncedAt: null }), st({ state: "QUEUED" }), st({ state: "toString" }), st({ lastOutcome: "maybe" }),
    st({ lastOutcome: undefined }), st({ message: 1 }), st({ lastOutcome: "toString" }),
    st({ lastRunAt: null }), st({ lastRunAt: undefined }),
    st({ attention: undefined }), st({ attention: null }), st({ attention: "SignIn" }), st({ attention: "signInExpired" }),
    st({ attention: "toString" }), st({ attention: 0 }),
    st({ lastRunAttempts: -1 }), st({ lastRunAttempts: 1.5 }), st({ lastRunAttempts: "2" }),
    st({ lastRunAttempts: undefined }), st({ lastRunOutcome: "OK" }), st({ lastRunOutcome: undefined }),
    st({ lastRunText: 0 }), st({ lastRunText: undefined }),
  ]) {
    assert.throws(() => parseCalendarSyncState(raw), /calendar state/);
    assert.throws(() => parseCalendarSyncResult(result(raw)), /calendar state/);
  }
  assert.deepEqual(parseCalendarSyncResult({ ...result(synced, { ok: false }), x: 1 }), result(synced, { ok: false }));
  for (const raw of [null, {}, result(synced, { ok: "true" }), result(synced, { message: null }), { ok: true, message: "m" }]) {
    assert.throws(() => parseCalendarSyncResult(raw), /calendar (result|state)/);
  }
});

test("the last sync time is local with the attempts beyond one; none or a bad one is not shown", () => {
  assert.equal(lastSyncedText(""), "");
  assert.equal(lastSyncedText("", 3), "");
  assert.equal(lastSyncedText("not a time"), "");
  const t = new Date("2026-10-16T06:30:00Z").toLocaleString("ru-RU");
  assert.equal(lastSyncedText("2026-10-16T06:30:00Z"), `Последняя синхронизация: ${t}`);
  for (const n of [0, 1]) {
    assert.equal(lastSyncedText("2026-10-16T06:30:00Z", n), `Последняя синхронизация: ${t}`, String(n));
  }
  for (const [n, word] of [[2, "попытки"], [4, "попытки"], [5, "попыток"], [11, "попыток"], [21, "попытка"], [22, "попытки"]]) {
    assert.equal(lastSyncedText("2026-10-16T06:30:00Z", n), `Последняя синхронизация: ${t} · ${n} ${word}`, String(n));
  }
});

test("the result of the last finished sync is Go's text, styled by its outcome", () => {
  const kinds = { ok: "ok", notSignedIn: "wait", signInExpired: "fail", noAccess: "fail", failed: "fail" };
  for (const [lastRunOutcome, kind] of Object.entries(kinds)) {
    assert.deepEqual(lastRunResult(st({ lastRunOutcome, lastRunText: "текст" })), { text: "текст", kind }, lastRunOutcome);
  }
  // An unknown result shows the time only.
  assert.equal(lastRunResult(st({ lastRunOutcome: "", lastRunText: "" })), null);
  assert.equal(lastRunResult(st({ lastRunOutcome: "", lastRunText: "текст" })), null);
  assert.equal(lastRunResult(st({ lastRunText: "" })), null);
  assert.equal(lastRunResult(st({ lastRunAt: "" })), null);

});

test("signed in: Go's message, the time and the button, disabled while queued, running or in flight", () => {
  const base = { account: "signedIn", loadFailed: false, inFlight: false };

  assert.deepEqual(calendarSyncView({ ...base, status: synced }), {
    shown: true, message: synced.message, messageAlert: false, lastSynced: lastSyncedText(synced.lastRunAt),
    lastRunResult: { text: "Успешно.", kind: "ok" }, syncShown: true, syncDisabled: false, syncDisabledReason: "",
  });
  // One time line: the last finished job, not the time the last lesson was written.
  assert.equal(calendarSyncView({ ...base, status: st({ lastRunAt: "" }) }).lastSynced, "");
  assert.equal(calendarSyncView({ ...base, status: st({ lastSyncedAt: "" }) }).lastSynced, lastSyncedText(synced.lastRunAt));
  assert.equal(
    calendarSyncView({ ...base, status: st({ lastRunAttempts: 3 }) }).lastSynced, lastSyncedText(synced.lastRunAt, 3));

  const expired = "Ошибка: вход в Google истёк.";
  const failedRun = calendarSyncView({ ...base, status: st({ lastRunOutcome: "signInExpired", lastRunText: expired }) });
  assert.deepEqual(failedRun.lastRunResult, { text: expired, kind: "fail" });
  assert.equal(failedRun.messageAlert, false, "no attention from Go: the result line carries the error");
  const unknown = calendarSyncView({ ...base, status: st({ lastRunOutcome: "", lastRunText: "" }) });
  assert.equal(unknown.lastSynced, lastSyncedText(synced.lastRunAt));
  assert.equal(unknown.lastRunResult, null);

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
  assert.equal(loading.lastRunResult, null);
  assert.equal(loading.syncShown, true);
  assert.equal(calendarSyncView({ ...base, status: null, loadFailed: true }).message, calendarTexts.loadFailed);
  // A later answer wins over an earlier failure.
  assert.equal(calendarSyncView({ ...base, status: synced, loadFailed: true }).message, synced.message);
});

test("signed in: the status line is an alert exactly when Go asks the user to act", () => {
  const base = { account: "signedIn", loadFailed: false, inFlight: false };
  for (const attention of attentions) {
    // The outcomes do not decide it: Go already did (ARCH-44).
    for (const lastOutcome of outcomes) {
      const v = calendarSyncView({ ...base, status: st({ attention, lastOutcome, lastRunOutcome: lastOutcome }) });
      assert.equal(v.messageAlert, attention !== "", `${attention}/${lastOutcome}`);
      assert.equal(v.message, synced.message);
      assert.equal(v.syncShown, true);
    }
  }
  assert.equal(calendarSyncView({ ...base, status: null }).messageAlert, false);
  assert.equal(calendarSyncView({ ...base, status: null, loadFailed: true }).messageAlert, false);
});

test("signed out: nothing, except Go's line when it asks to sign in again", () => {
  for (const account of ["signedOut", "unsupported", "unavailable"]) {
    const base = { account, loadFailed: false, inFlight: false };
    assert.equal(calendarSyncView({ ...base, status: null }).shown, false, account);
    assert.equal(calendarSyncView({ ...base, status: st({ signedIn: false, lastOutcome: "" }) }).shown, false, account);
    assert.equal(calendarSyncView({ ...base, status: null, loadFailed: true }).shown, false, account);

    const msg = "Вход в Google истёк: войдите снова, чтобы продолжить синхронизацию.";
    const want = {
      shown: true, message: msg, messageAlert: true, lastSynced: "", lastRunResult: null, syncShown: false,
      syncDisabled: true, syncDisabledReason: "",
    };
    const v = calendarSyncView({ ...base, status: st({ signedIn: false, attention: "signIn", message: msg }) });
    assert.deepEqual(v, want);
    // The outcomes alone do not show it: whether to ask is Go's decision.
    for (const o of outcomes) {
      const s = st({ signedIn: false, attention: "", lastOutcome: o, lastRunOutcome: o, message: msg });
      assert.equal(calendarSyncView({ ...base, status: s }).shown, false, `${account}/${o}`);
    }
    // "noAccess" is about a signed-in account; signed out the sign-in part already asks.
    assert.equal(calendarSyncView({ ...base, status: st({ signedIn: false, attention: "noAccess" }) }).shown, false);
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
  assert.match(part, /role="alert"/, "an error the user must fix is announced");
  const hook = src("../src/pages/useGoogleStatus.ts");
  assert.match(hook, /calendarApi\(callByName\)/, "the overview card reads the calendar attention");
  assert.match(hook, /\[active, account, signingIn, cal\]/,
    "the calendar is read again after a sign-in or a sign-out, and when a sign-in ends");
  assert.match(hook, /const signingIn = state\?\.signingIn \?\? false;/);
  assert.doesNotMatch(hook, /\.sync\(/, "the overview never starts a sync");
  assert.equal((hook.match(/return \(\) => clearInterval\(timer\)/g) ?? []).length, 2);
  assert.match(src("../src/pages/GoogleBlock.tsx"), /<CalendarSync account=\{state\.account\} \/>/);
});
