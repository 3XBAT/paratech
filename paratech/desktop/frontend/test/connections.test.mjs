// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  connectionsApi,
  methods,
  parseCheckResult,
  parseConnections,
  parseSaveResult,
  parseSecretResult,
  secretStatuses,
} from "../.test-build/bindings/connections.js";
import {
  checkControls,
  failureText,
  fieldErrorText,
  hubFields,
  mailConfiguredText,
  mailFields,
  mailFormDirty,
  mailFormFrom,
  mailInputFrom,
  mailPasswordLabels,
  notAPort,
  otherErrorTexts,
  parsePort,
  secretControls,
  secretOutcomeOk,
  secretStatusLabel,
  securityOptions,
  texts,
} from "../.test-build/connectionsView.js";

// Mirrors the JSON of internal/input/wails/connections.go (TestConnectionsJSONNames).
const stored = {
  hubUrl: "https://hub.example.org", recipient: "t@school.example", smtpHost: "smtp.example.org", smtpPort: 465,
  smtpSecurity: "tls", smtpLogin: "s@example.org", mailConfigured: true, mailPassword: "set",
};
const fresh = { ...stored, hubUrl: "", recipient: "", smtpHost: "", smtpPort: 0, smtpSecurity: "starttls", smtpLogin: "",
  mailConfigured: false, mailPassword: "unset" };

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
    "CheckMail", "GetConnections", "RemoveMailPassword", "SaveHubURL", "SaveMailSettings", "SetMailPassword",
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

  assert.deepEqual(calls, [[p + "GetConnections"]]);
  assert.deepEqual(saved.calls, [[p + "SaveMailSettings", input], [p + "SaveHubURL", "https://hub.example.org"]]);
  assert.deepEqual(secret.calls, [[p + "SetMailPassword", "pw"], [p + "RemoveMailPassword"]]);
  assert.deepEqual(check.calls, [[p + "CheckMail"]]);
});

test("a failing call rejects", async () => {
  const api = connectionsApi(async () => {
    throw new Error("connections settings are unavailable");
  });
  await assert.rejects(api.get(), /unavailable/);
  await assert.rejects(api.setMailPassword("pw"), /unavailable/);
});

test("replies are parsed strictly; extra fields are dropped", () => {
  assert.deepEqual(parseConnections({ ...stored, password: "x" }), stored);
  for (const raw of [
    null, "x", {},
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
  assert.equal(mailConfiguredText(true), "Почта настроена.");
  assert.match(mailConfiguredText(false), /^Почта не настроена/);
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

test("the page renders text only and keeps the password write-only", () => {
  for (const file of ["../src/pages/ConnectionsPage.tsx", "../src/pages/SecretSlot.tsx", "../src/connectionsView.ts",
    "../src/bindings/connections.ts"]) {
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
});
