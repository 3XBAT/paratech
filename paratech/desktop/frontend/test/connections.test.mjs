// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { helpTexts } from "../.test-build/appleHelpView.js";
import { pages } from "../.test-build/nav.js";
import { HelpView } from "../.test-build/pages/AppleCalendarHelpPage.js";
import {
  connectionsApi,
  methods,
  parseCheckResult,
  parseConnections,
  parseQuota,
  parseQuotaResult,
  parseSaveResult,
  parseSecretResult,
  quotaStatuses,
  secretStatuses,
} from "../.test-build/bindings/connections.js";
import {
  checkControls,
  failureText,
  fieldErrorText,
  hubFields,
  hubKeyLabels,
  mailConfiguredText,
  mailFields,
  mailFormDirty,
  mailFormFrom,
  mailInputFrom,
  mailPasswordLabels,
  notAPort,
  otherErrorTexts,
  parsePort,
  secretChipLabel,
  secretControls,
  secretOutcomeOk,
  secretStatusLabel,
  secretStatusNote,
  securityOptions,
  texts,
  appleCardStatus,
  googleCardStatus,
  hubCardStatus,
  mailCardStatus,
  overviewTexts,
} from "../.test-build/connectionsView.js";

// Mirrors the JSON of internal/input/wails/connections.go (TestConnectionsJSONNames).
const values = {
  unit: "tokens", remaining: 90000, reserved: 1500, limit: 100000, periodEnd: "2026-11-01T00:00:00Z",
  fetchedAt: "2026-10-15T10:00:00Z",
};
const current = { status: "current", stale: false, checkedAt: "2026-10-15T10:00:00Z", values };
const unchecked = { status: "unchecked", stale: false, checkedAt: "", values: null };
const stored = {
  hubUrl: "https://hub.example.org", hubKey: "set", quota: current, recipient: "t@school.example",
  smtpHost: "smtp.example.org", smtpPort: 465, smtpSecurity: "tls", smtpLogin: "s@example.org", mailConfigured: true,
  mailPassword: "set",
};
const fresh = { ...stored, hubUrl: "", hubKey: "unset", quota: unchecked, recipient: "", smtpHost: "", smtpPort: 0,
  smtpSecurity: "starttls", smtpLogin: "", mailConfigured: false, mailPassword: "unset" };

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("the api calls the bound Go methods with the arguments", async () => {
  const p = "paratech/desktop/internal/input/wails.ConnectionsService.";
  assert.deepEqual(Object.values(methods).sort(), [
    "CheckMail", "GetConnections", "RefreshQuota", "RemoveHubKey", "RemoveMailPassword", "SaveHubURL", "SaveMailSettings",
    "SetHubKey", "SetMailPassword",
  ].map((m) => p + m));

  const { call, calls } = fakeCall(stored);
  assert.deepEqual(await connectionsApi(call).get(), stored);

  const saved = fakeCall({ saved: true, connections: stored, errors: [] });
  const api = connectionsApi(saved.call);
  const input = { recipient: "r", smtpHost: "h", smtpPort: 0, smtpSecurity: "starttls", smtpLogin: "l" };
  assert.deepEqual(await api.saveMailSettings(input), { saved: true, connections: stored, errors: [] });
  await api.saveHubUrl("https://hub.example.org");

  const secret = fakeCall({ status: "set", message: "m" });
  await connectionsApi(secret.call).setMailPassword("pw");
  await connectionsApi(secret.call).removeMailPassword();
  const check = fakeCall({ ok: true, message: "m" });
  assert.deepEqual(await connectionsApi(check.call).checkMail(), { ok: true, message: "m" });
  const key = fakeCall({ status: "set", message: "m" });
  assert.deepEqual(await connectionsApi(key.call).setHubKey("hk_1"), { status: "set", message: "m" });
  await connectionsApi(key.call).removeHubKey();
  const refresh = fakeCall({ ok: false, message: "m", quota: { ...current, status: "offline", stale: true } });
  assert.deepEqual(await connectionsApi(refresh.call).refreshQuota(), {
    ok: false, message: "m", quota: { ...current, status: "offline", stale: true },
  });

  assert.deepEqual(calls, [[p + "GetConnections"]]);
  assert.deepEqual(saved.calls, [[p + "SaveMailSettings", input], [p + "SaveHubURL", "https://hub.example.org"]]);
  assert.deepEqual(secret.calls, [[p + "SetMailPassword", "pw"], [p + "RemoveMailPassword"]]);
  assert.deepEqual(check.calls, [[p + "CheckMail"]]);
  assert.deepEqual(key.calls, [[p + "SetHubKey", "hk_1"], [p + "RemoveHubKey"]]);
  assert.deepEqual(refresh.calls, [[p + "RefreshQuota"]]);
});

test("a failing call rejects", async () => {
  const api = connectionsApi(async () => {
    throw new Error("connections settings are unavailable");
  });
  await assert.rejects(api.get(), /unavailable/);
  await assert.rejects(api.setMailPassword("pw"), /unavailable/);
  await assert.rejects(api.setHubKey("hk_1"), /unavailable/);
  await assert.rejects(api.refreshQuota(), /unavailable/);
});

test("replies are parsed strictly; extra fields are dropped", () => {
  assert.deepEqual(parseConnections({ ...stored, password: "x" }), stored);
  assert.deepEqual(parseConnections({ ...stored, apiKey: "x", quota: { ...current, key: "x", values: { ...values, key: "x" } } }),
    stored);
  assert.deepEqual(parseConnections(fresh), fresh);
  for (const raw of [
    null, "x", {},
    { ...stored, hubKey: "hk_secret" },
    { ...stored, hubKey: undefined },
    { ...stored, smtpPort: "465" },
    { ...stored, smtpPort: -1 },
    { ...stored, smtpPort: 1.5 },
    { ...stored, smtpSecurity: "none" },
    { ...stored, mailPassword: "hunter2" },
    { ...stored, mailConfigured: "yes" },
    { ...stored, hubUrl: null },
  ]) {
    assert.throws(() => parseConnections(raw), /connections/);
  }

  const refused = { saved: false, connections: null, errors: [{ field: "smtpPort", message: "m" }] };
  assert.deepEqual(parseSaveResult(refused), refused);
  assert.deepEqual(parseSaveResult({ saved: true, connections: stored, errors: null }).errors, []);
  for (const raw of [
    { saved: true, connections: null, errors: [] },
    { saved: false, connections: null, errors: [] },
    { saved: false, connections: null, errors: [{ field: 1, message: "m" }] },
    { saved: "true", connections: stored, errors: [] },
  ]) {
    assert.throws(() => parseSaveResult(raw), /result|connections|field error/);
  }

  assert.deepEqual(parseSecretResult({ status: "unsupported", message: "m" }), { status: "unsupported", message: "m" });
  assert.throws(() => parseSecretResult({ status: "secret", message: "m" }), /secret result/);
  assert.deepEqual(parseCheckResult({ ok: false, message: "m" }), { ok: false, message: "m" });
  assert.throws(() => parseCheckResult({ ok: 1, message: "m" }), /check result/);
});

test("a malformed quota is refused, in the section and in a refresh answer", () => {
  assert.deepEqual(quotaStatuses, ["unchecked", "current", "notConfigured", "noKey", "keyRejected", "offline", "failed"]);
  for (const status of quotaStatuses) {
    assert.equal(parseQuota({ ...unchecked, status }).status, status);
  }
  assert.deepEqual(parseQuota({ ...current, values: { ...values, periodEnd: "" } }).values.periodEnd, "");
  // Amounts up to 2^53 - 1 are exact in JavaScript.
  assert.equal(parseQuota({ ...current, values: { ...values, limit: Number.MAX_SAFE_INTEGER } }).values.limit,
    Number.MAX_SAFE_INTEGER);
  for (const raw of [
    undefined, null, "current", {},
    { ...current, status: "fine" },
    { ...current, status: "toString" },
    { ...current, stale: "no" },
    { ...current, checkedAt: null },
    { ...current, values: undefined },
    { ...current, values: "90000" },
    { ...current, values: { ...values, unit: 1 } },
    { ...current, values: { ...values, remaining: -1 } },
    { ...current, values: { ...values, remaining: 1.5 } },
    { ...current, values: { ...values, remaining: "90000" } },
    { ...current, values: { ...values, reserved: null } },
    { ...current, values: { ...values, limit: Number.MAX_SAFE_INTEGER + 1 } },
    { ...current, values: { ...values, limit: Infinity } },
    { ...current, values: { ...values, fetchedAt: undefined } },
    { ...current, values: { ...values, periodEnd: 0 } },
  ]) {
    assert.throws(() => parseQuota(raw), /quota/);
    assert.throws(() => parseConnections({ ...stored, quota: raw }), /quota/);
    assert.throws(() => parseQuotaResult({ ok: true, message: "m", quota: raw }), /quota/);
  }

  const refreshed = { ok: true, message: "Остаток обновлён.", quota: current };
  assert.deepEqual(parseQuotaResult({ ...refreshed, apiKey: "x" }), refreshed);
  for (const raw of [
    null, {},
    { ...refreshed, ok: "true" },
    { ...refreshed, message: null },
    { ok: true, message: "m" },
  ]) {
    assert.throws(() => parseQuotaResult(raw), /quota/);
  }
});

test("the form maps from the stored values and back; port 0 is empty", () => {
  assert.deepEqual(mailFormFrom(stored), {
    recipient: "t@school.example", smtpHost: "smtp.example.org", smtpPort: "465", smtpSecurity: "tls", smtpLogin: "s@example.org",
  });
  assert.equal(mailFormFrom(fresh).smtpPort, "");
  assert.deepEqual(mailInputFrom(mailFormFrom(stored)), {
    recipient: "t@school.example", smtpHost: "smtp.example.org", smtpPort: 465, smtpSecurity: "tls", smtpLogin: "s@example.org",
  });
});

test("port parsing: empty is the default, digits are sent, anything else is a port the core refuses", () => {
  assert.equal(parsePort(""), 0);
  assert.equal(parsePort("  "), 0);
  assert.equal(parsePort("0"), 0);
  assert.equal(parsePort(" 587 "), 587);
  // Out of range but a number: the Go core refuses it with its own text.
  assert.equal(parsePort("99999"), 99999);
  // The core refuses a negative port (settings checkPort) and reports it with the other refused fields.
  assert.ok(notAPort < 0);
  for (const bad of ["abc", "5.5", "-1", "1e3", "587a", "123456", "0x10"]) {
    assert.equal(parsePort(bad), notAPort, bad);
  }
  assert.equal(mailInputFrom({ ...mailFormFrom(fresh), smtpPort: "abc" }).smtpPort, notAPort);
});

test("the password is never part of the mail input", () => {
  const form = { ...mailFormFrom(stored), password: "PASSWORDMARKER", mailPassword: "PASSWORDMARKER" };
  const got = mailInputFrom(form);
  assert.deepEqual(Object.keys(got).sort(), ["recipient", "smtpHost", "smtpLogin", "smtpPort", "smtpSecurity"]);
  assert.ok(!JSON.stringify(got).includes("PASSWORDMARKER"));
  assert.ok(!Object.keys(mailFormFrom(stored)).some((k) => /password/i.test(k)));
});

test("dirty check compares with the stored values", () => {
  const form = mailFormFrom(stored);
  assert.equal(mailFormDirty(form, stored), false);
  assert.equal(mailFormDirty({ ...form, smtpHost: "smtp.other.org" }, stored), true);
  assert.equal(mailFormDirty({ ...form, smtpSecurity: "starttls" }, stored), true);
  assert.equal(mailFormDirty({ ...form, smtpPort: "587" }, stored), true);
  assert.equal(mailFormDirty({ ...form, smtpPort: "x" }, stored), true);
  assert.equal(mailFormDirty({ ...mailFormFrom(fresh), smtpPort: "0" }, fresh), false);
  assert.equal(mailFormDirty({ ...mailFormFrom(fresh), smtpPort: "" }, fresh), false);
});

test("every field code of the Go use case has a place on the page", () => {
  const go = src("../../internal/cases/connections/errors.go");
  const codes = [...go.matchAll(/Field[A-Za-z]+\s+Field = "([a-zA-Z]+)"/g)].map((m) => m[1]);
  assert.equal(codes.length, 6);
  const placed = [...hubFields, ...mailFields.map((f) => f.code)];
  assert.deepEqual([...placed].sort(), [...codes].sort());
  assert.deepEqual(mailFields.map((f) => f.label), [
    "Адрес получателя домашки", "SMTP-сервер", "Порт", "Режим TLS", "Логин (адрес почты)",
  ]);

  const errors = [
    { field: "smtpHost", message: "Сервер: …" },
    { field: "hubUrl", message: "Адрес Hub: …" },
    { field: "future", message: "Новое правило." },
    { field: "", message: "" },
  ];
  assert.equal(fieldErrorText(errors, "smtpHost"), "Сервер: …");
  assert.equal(fieldErrorText(errors, "smtpLogin"), "");
  assert.equal(fieldErrorText(errors, "hubUrl"), "Адрес Hub: …");
  // Unplaced errors are listed so none is lost.
  assert.deepEqual(otherErrorTexts(errors, mailFields.map((f) => f.code)), ["Адрес Hub: …", "Новое правило.", texts.unknownField]);
  assert.deepEqual(otherErrorTexts(errors, hubFields), ["Сервер: …", "Новое правило.", texts.unknownField]);
});

test("the TLS mode has no plaintext option", () => {
  assert.deepEqual(securityOptions.map((o) => o.value), ["starttls", "tls"]);
  assert.match(texts.portHint, /587 для STARTTLS, 465 для TLS/);
});

test("every secret status has its label", () => {
  assert.deepEqual(secretStatuses.map((s) => secretStatusLabel(mailPasswordLabels, s)), [
    "Пароль задан", "Пароль не задан", "Хранилище паролей недоступно в этой системе", "Хранилище паролей не ответило",
  ]);
  assert.equal(secretStatusLabel(mailPasswordLabels, "toString"), "Хранилище паролей не ответило");
  assert.deepEqual(secretStatuses.map((s) => secretStatusLabel(hubKeyLabels, s)), [
    "Ключ задан", "Ключ не задан", "Хранилище ключей недоступно в этой системе", "Хранилище ключей не ответило",
  ]);
  assert.equal(hubKeyLabels.input, "Новый ключ");
  assert.match(hubKeyLabels.confirmTitle, /^Удалить сохранённый ключ доступа\?/);
  assert.equal(mailConfiguredText(true), "Почта настроена.");
  assert.match(mailConfiguredText(false), /^Почта не настроена/);
});

test("a secret chip has one to three words; a short one comes with the full label", () => {
  for (const labels of [mailPasswordLabels, hubKeyLabels]) {
    for (const s of [...secretStatuses, "toString"]) {
      const chip = secretChipLabel(labels, s);
      assert.ok(chip.split(" ").length <= 3, chip);
      const note = secretStatusNote(labels, s);
      // Either the chip is the label or the label is shown next to it: the full text is never lost.
      assert.equal(note === "" ? chip : note, secretStatusLabel(labels, s));
    }
  }
  assert.deepEqual(secretStatuses.map((s) => secretChipLabel(mailPasswordLabels, s)), [
    "Пароль задан", "Пароль не задан", "Недоступно", "Не ответило",
  ]);
  assert.equal(secretChipLabel(hubKeyLabels, "toString"), "Не ответило");
});

test("secret buttons: nothing without a store, remove only for a stored secret", () => {
  assert.deepEqual(secretControls("unsupported", false, true), {
    inputDisabled: true, saveDisabled: true, removeShown: false, removeDisabled: false,
  });
  assert.deepEqual(secretControls("unset", false, false), {
    inputDisabled: false, saveDisabled: true, removeShown: false, removeDisabled: false,
  });
  assert.deepEqual(secretControls("unset", false, true).saveDisabled, false);
  assert.deepEqual(secretControls("set", false, true), {
    inputDisabled: false, saveDisabled: false, removeShown: true, removeDisabled: false,
  });
  assert.deepEqual(secretControls("set", true, true), {
    inputDisabled: true, saveDisabled: true, removeShown: true, removeDisabled: true,
  });
  // A store that did not answer may be retried.
  assert.equal(secretControls("unavailable", false, true).saveDisabled, false);

  assert.equal(secretOutcomeOk("store", "set"), true);
  assert.equal(secretOutcomeOk("store", "unset"), false);
  assert.equal(secretOutcomeOk("store", "unavailable"), false);
  assert.equal(secretOutcomeOk("remove", "unset"), true);
  assert.equal(secretOutcomeOk("remove", "set"), false);
});

test("the mail check waits for unsaved changes and for calls in flight", () => {
  const idle = { dirty: false, checking: false, saving: false, secretBusy: false };
  assert.deepEqual(checkControls(idle), { disabled: false, hint: "" });
  assert.deepEqual(checkControls({ ...idle, dirty: true }), {
    disabled: true, hint: "Сначала сохраните изменения: проверяются сохранённые настройки.",
  });
  assert.equal(checkControls({ ...idle, checking: true }).disabled, true);
  assert.equal(checkControls({ ...idle, saving: true }).disabled, true);
  // A password being stored or removed: the check would race it and log in with the old one.
  assert.deepEqual(checkControls({ ...idle, secretBusy: true }), { disabled: true, hint: "" });
  assert.equal(texts.checkNoSend, "Письмо не отправляется: только соединение и вход.");
});

test("rejected calls show fixed Russian texts", () => {
  assert.equal(failureText("load"), "Не удалось загрузить настройки подключений.");
  assert.equal(failureText("save"), "Не удалось сохранить настройки подключений.");
  assert.equal(failureText("check"), "Не удалось проверить почту.");
});

test("the page renders text only and keeps the password and the Hub key write-only", () => {
  for (const file of ["../src/pages/ConnectionsPage.tsx", "../src/pages/SecretSlot.tsx", "../src/connectionsView.ts",
    "../src/quotaView.ts", "../src/bindings/connections.ts"]) {
    const s = src(file);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "console.", "localStorage", "sessionStorage", "href="]) {
      assert.ok(!s.includes(banned), `${file} uses ${banned}`);
    }
  }
  const slot = src("../src/pages/SecretSlot.tsx");
  assert.match(slot, /type="password"/);
  assert.match(slot, /autoComplete="new-password"/);
  // The typed value is cleared after the call whatever its outcome.
  assert.match(slot, /finally \{\s*setValue\(""\);/);
  // Only the slot holds the typed password; the page forwards it straight to the call.
  const page = src("../src/pages/ConnectionsPage.tsx");
  assert.ok(!/useState[^;]*[Pp]assword/.test(page), "the page keeps the password in its state");
  assert.match(page, /onStore=\{\(value\) => changeSecret\("store", \(\) => api\.setMailPassword\(value\)\)\}/);
  // The Hub key uses the same slot and goes the same way; the page has no state of its own for it.
  assert.ok(!/useState[^;]*[Kk]ey/.test(page), "the page keeps the Hub key in its state");
  assert.match(page, /onStore=\{\(value\) => changeHubKey\("store", \(\) => api\.setHubKey\(value\)\)\}/);
  assert.equal([...page.matchAll(/<SecretSlot\b/g)].length, 2);
  assert.equal([...page.matchAll(/api\.setHubKey\(/g)].length, 1);
  // The re-read and the refresh after a key change are not awaited inside the change: the slot clears the field as
  // soon as the store answers.
  assert.match(page, /\}\);\s*void reloadHub\(\);\s*\}/);
});

// TASK-068: the overview cards. Each status comes from what the page already holds; the kind picks the chip colour.
const chip = (s) => [s.kind, s.text];

test("the Hub card says what is missing before it says connected", () => {
  const hub = (saved, more = {}) => chip(hubCardStatus({ saved, loadFailed: false, refreshing: false, ...more }));
  assert.deepEqual(hub(null), ["neutral", "Загрузка…"]);
  assert.deepEqual(hub(null, { loadFailed: true }), ["fail", "Не загрузилось"]);
  assert.deepEqual(hub(fresh), ["wait", "Не настроено"]);
  assert.deepEqual(hub({ ...stored, hubKey: "unset" }), ["wait", "Нет ключа"]);
  assert.deepEqual(hub({ ...stored, hubKey: "unsupported" }), ["fail", "Ключ недоступен"]);
  assert.deepEqual(hub({ ...stored, hubKey: "unavailable" }), ["fail", "Ключ недоступен"]);
  assert.deepEqual(hub(stored, { refreshing: true }), ["run", "Проверяем…"]);
  assert.deepEqual(hub(stored), ["ok", "Подключено"]);
  const q = (status) => hub({ ...stored, quota: { ...current, status } });
  assert.deepEqual(q("unchecked"), ["neutral", "Не проверено"]);
  assert.deepEqual(q("notConfigured"), ["wait", "Не настроено"]);
  assert.deepEqual(q("noKey"), ["wait", "Нет ключа"]);
  assert.deepEqual(q("keyRejected"), ["fail", "Ключ не принят"]);
  assert.deepEqual(q("offline"), ["net", "Нет связи"]);
  assert.deepEqual(q("failed"), ["fail", "Ошибка проверки"]);
  assert.deepEqual(q("newerStatus"), ["fail", "Ошибка проверки"]);
});

test("the mail card follows the settings, the password and a check made on the page", () => {
  const mail = (saved, check = null) => chip(mailCardStatus({ saved, loadFailed: false, check }));
  assert.deepEqual(mail(null), ["neutral", "Загрузка…"]);
  assert.deepEqual(chip(mailCardStatus({ saved: null, loadFailed: true, check: null })), ["fail", "Не загрузилось"]);
  assert.deepEqual(mail(fresh), ["wait", "Не настроено"]);
  assert.deepEqual(mail({ ...stored, mailPassword: "unset" }), ["wait", "Нет пароля"]);
  assert.deepEqual(mail({ ...stored, mailPassword: "unavailable" }), ["fail", "Пароль недоступен"]);
  // Saved but not checked: no green until «Проверить почту» passes.
  assert.deepEqual(mail(stored), ["neutral", "Настроено"]);
  assert.deepEqual(mail(stored, { ok: true, message: "" }), ["ok", "Проверено"]);
  assert.deepEqual(mail(stored, { ok: false, message: "" }), ["fail", "Проверка не прошла"]);
});

test("the Google card reads the client, the account and a waiting sign-in", () => {
  const g = (state, loadFailed = false) => chip(googleCardStatus({ state, loadFailed }));
  const ready = { client: "set", account: "signedIn", signingIn: false };
  assert.deepEqual(g(null), ["neutral", "Загрузка…"]);
  assert.deepEqual(g(null, true), ["fail", "Не удалось проверить"]);
  assert.deepEqual(g(ready), ["ok", "Подключено"]);
  assert.deepEqual(g({ ...ready, account: "signedOut" }), ["wait", "Вход не выполнен"]);
  assert.deepEqual(g({ ...ready, account: "signedOut", signingIn: true }), ["run", "Ожидание входа"]);
  assert.deepEqual(g({ client: "unset", account: "signedOut", signingIn: false }), ["wait", "Не настроено"]);
  assert.deepEqual(g({ ...ready, client: "unsupported" }), ["fail", "Недоступно"]);
  assert.deepEqual(g({ ...ready, account: "unavailable" }), ["fail", "Не удалось проверить"]);
  assert.deepEqual(chip(appleCardStatus), ["neutral", "Инструкция"]);
});

test("the Google card shows what Go's calendar attention asks of the user", () => {
  const ready = { client: "set", account: "signedIn", signingIn: false };
  const out = { ...ready, account: "signedOut" };
  const cal = (attention, outcome = "") => ({
    signedIn: true, pending: 0, pendingMore: false, lastSyncedAt: "", state: "idle", lastOutcome: outcome, attention,
    message: "m", lastRunAt: "2026-10-16T06:31:00Z", lastRunAttempts: 1, lastRunOutcome: outcome, lastRunText: "",
  });
  const g = (state, calendar) => chip(googleCardStatus({ state, loadFailed: false, calendar }));
  for (const c of [null, undefined, cal("")]) {
    assert.deepEqual(g(ready, c), ["ok", "Подключено"]);
    assert.deepEqual(g(out, c), ["wait", "Вход не выполнен"]);
  }
  // Go decides; the card does not need signedIn for "signIn" (it comes while signed out).
  for (const state of [ready, out]) {
    assert.deepEqual(g(state, cal("signIn")), ["fail", "Нужен вход"], state.account);
    assert.deepEqual(g(state, cal("noAccess")), ["fail", "Нет доступа"], state.account);
  }
  // The outcomes alone do not turn the card red: that rule is Go's (ARCH-44).
  for (const o of ["ok", "notSignedIn", "signInExpired", "noAccess", "failed"]) {
    assert.deepEqual(g(ready, cal("", o)), ["ok", "Подключено"], o);
    assert.deepEqual(g(out, cal("", o)), ["wait", "Вход не выполнен"], o);
  }
  // A sign-in waiting for the browser still wins, and so do a missing or broken client and an unknown account.
  for (const a of ["signIn", "noAccess"]) {
    assert.deepEqual(g({ ...out, signingIn: true }, cal(a)), ["run", "Ожидание входа"], a);
    assert.deepEqual(g({ ...ready, signingIn: true }, cal(a)), ["run", "Ожидание входа"], a);
    assert.deepEqual(g({ ...out, client: "unset" }, cal(a)), ["wait", "Не настроено"], a);
    assert.deepEqual(g({ ...ready, client: "unavailable" }, cal(a)), ["fail", "Недоступно"], a);
    assert.deepEqual(g({ ...ready, account: "unsupported" }, cal(a)), ["fail", "Недоступно"], a);
    assert.deepEqual(g({ ...ready, account: "unavailable" }, cal(a)), ["fail", "Не удалось проверить"], a);
  }
  for (const s of [g(ready, cal("noAccess")), g(out, cal("signIn"))]) {
    assert.ok(s[1].length <= 20, s[1]);
  }
});

test("every card chip pairs its colour with a word and an icon where it warns", () => {
  const all = [
    appleCardStatus,
    ...["current", "offline", "keyRejected", "unchecked"].map((status) =>
      hubCardStatus({ saved: { ...stored, quota: { ...current, status } }, loadFailed: false, refreshing: false })),
    hubCardStatus({ saved: stored, loadFailed: false, refreshing: true }),
    mailCardStatus({ saved: fresh, loadFailed: false, check: null }),
  ];
  for (const s of all) {
    assert.ok(s.text.length > 0 && s.text.length <= 20, s.text);
    assert.equal(s.icon === null, s.kind === "neutral", s.text);
  }
  for (const d of [overviewTexts.hubDesc, overviewTexts.mailDesc, overviewTexts.googleDesc]) {
    assert.ok(d.length <= 40, `${d}: one line in a card`);
  }
});

test("the overview opens each section in place and returns the focus to its card", () => {
  const page = src("../src/pages/ConnectionsPage.tsx");
  // Four cards, each a real button (Tab, Enter and Space come with it).
  assert.equal([...page.matchAll(/<SectionCard\s/g)].length, 4);
  assert.match(page, /<button type="button" className="card box conn-card"/);
  for (const id of ["hub", "mail", "google", "apple"]) {
    assert.ok(page.includes(`{...cardProps("${id}")}`), id);
  }
  assert.match(page, /cards\.current\[returnTo\.current\]\?\.focus\(\)/);
  assert.match(page, /<Button onClick=\{props\.onBack\}>\{overviewTexts\.back\}<\/Button>/);
  assert.equal(overviewTexts.back, "Назад");
  // The overview reads the Google status only; nothing on it signs in, syncs or refreshes.
  const hook = src("../src/pages/useGoogleStatus.ts");
  assert.deepEqual([...hook.matchAll(/api\.(\w+)\(/g)].map((m) => m[1]), ["status", "status"]);
  // The sections stay off the rail.
  assert.deepEqual(
    pages.map((p) => p.id).filter((id) => ["hub", "mail", "google", "apple"].includes(id)),
    [],
  );
  assert.ok(pages.some((p) => p.id === "connections"));
  // A click on «Подключения» in the rail while a section is open brings the overview back.
  assert.match(src("../src/App.tsx"), /<ConnectionsPage visit=\{props\.visit\} \/>/);
  assert.match(page, /if \(props\.visit !== firstVisit\.current\) \{\s*setOpen\(null\);/);
});

test("the Apple help page keeps the entry text as its lead", () => {
  const markup = server.renderToStaticMarkup(h(HelpView, { failure: null, onOpen: () => {}, onBack: () => {} }));
  assert.ok(markup.includes(helpTexts.entryText), helpTexts.entryText);
});

test("section heading rule is scoped to h2.sub so dialog titles keep their size", () => {
  const css = src("../src/styles.css");
  assert.match(css, /\.conn-page \.box h2\.sub \{/);
  assert.doesNotMatch(css, /\.conn-page \.box h2 \{/);
  for (const file of ["ConnectionsPage.tsx", "GoogleBlock.tsx", "CalendarSync.tsx", "SecretSlot.tsx"]) {
    assert.match(src(`../src/pages/${file}`), /<h2 className="sub">/, file);
  }
});

test("the Hub section is «Конспекты и задания» with the key hint under its title (TASK-069)", () => {
  assert.equal(overviewTexts.hubTitle, "Конспекты и задания");
  assert.equal(overviewTexts.lead, "Конспекты и задания, почта и Google Календарь");
  assert.match(overviewTexts.hubHint, /выдаёт автор приложения/);
  assert.match(overviewTexts.hubHint, /не ключ Claude или ChatGPT/);
  assert.match(overviewTexts.hubHint, /Распознавание речи всегда идёт на этом компьютере/);
  const page = src("../src/pages/ConnectionsPage.tsx");
  // The card of the section page carries the title, and the hint is its first line.
  assert.match(
    page,
    /<Card className="box" label=\{overviewTexts\.hubTitle\}>\s*<span className="small muted box-desc">\{overviewTexts\.hubHint\}<\/span>/,
  );
  assert.ok(!/Сервер обработки|сервер(а|ом)? обработки/i.test(page + src("../src/connectionsView.ts") + src("../src/quotaView.ts")));
});
