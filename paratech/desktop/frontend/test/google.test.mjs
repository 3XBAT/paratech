// Google sign-in block of "Подключения" (TASK-050). Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  accountStatuses,
  clientStatuses,
  googleApi,
  methods,
  parseGoogleResult,
  parseGoogleState,
} from "../.test-build/bindings/google.js";
import {
  accountStatusText,
  clientStatusText,
  googleControls,
  googleTexts,
  pollStatus,
  statusPollMs,
  statusWarns,
} from "../.test-build/googleView.js";

// Mirrors the JSON of internal/input/wails/google.go (TestGoogleStateJSON).
const signedIn = { client: "set", account: "signedIn", signingIn: false };
const ready = { client: "set", account: "signedOut", signingIn: false };
const fresh = { client: "unset", account: "signedOut", signingIn: false };
const result = (state, over = {}) => ({ ok: true, message: "m", state, ...over });

const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("the api calls the bound Go methods with the arguments", async () => {
  const p = "paratech/desktop/internal/input/wails.GoogleService.";
  assert.deepEqual(Object.values(methods).sort(), [
    "ClearGoogleClient", "GoogleCancelSignIn", "GoogleSignIn", "GoogleSignOut", "GoogleStatus", "SetGoogleClient",
  ].map((m) => p + m));

  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return method.endsWith(".GoogleStatus") ? ready : result(ready);
  };
  const api = googleApi(call);
  assert.deepEqual(await api.status(), ready);
  assert.deepEqual(await api.setClient("123-fake.apps.googleusercontent.com", "fake-secret"), result(ready));
  await api.clearClient();
  await api.signIn();
  await api.cancelSignIn();
  await api.signOut();
  assert.deepEqual(calls, [
    [p + "GoogleStatus"],
    [p + "SetGoogleClient", "123-fake.apps.googleusercontent.com", "fake-secret"],
    [p + "ClearGoogleClient"],
    [p + "GoogleSignIn"],
    [p + "GoogleCancelSignIn"],
    [p + "GoogleSignOut"],
  ]);

  await assert.rejects(googleApi(async () => { throw new Error("x"); }).signIn(), /x/);
});

test("replies are parsed strictly; extra fields are dropped", () => {
  assert.deepEqual(clientStatuses, ["set", "unset", "unsupported", "unavailable"]);
  assert.deepEqual(accountStatuses, ["signedIn", "signedOut", "unsupported", "unavailable"]);
  for (const client of clientStatuses) {
    for (const account of accountStatuses) {
      assert.deepEqual(parseGoogleState({ client, account, signingIn: true }), { client, account, signingIn: true });
    }
  }
  assert.deepEqual(parseGoogleState({ ...ready, refreshToken: "x", clientSecret: "x" }), ready);
  for (const raw of [
    undefined, null, "set", {},
    { ...ready, client: "123-fake.apps.googleusercontent.com" },
    { ...ready, client: "toString" },
    { ...ready, client: undefined },
    { ...ready, account: "set" },
    { ...ready, account: "ya29.token" },
    { ...ready, signingIn: "no" },
    { ...ready, signingIn: undefined },
  ]) {
    assert.throws(() => parseGoogleState(raw), /google state/);
    assert.throws(() => parseGoogleResult(result(raw)), /google state/);
  }

  assert.deepEqual(parseGoogleResult({ ...result(signedIn, { ok: false }), token: "x" }), result(signedIn, { ok: false }));
  for (const raw of [null, {}, result(ready, { ok: "true" }), result(ready, { message: null }), { ok: true, message: "m" }]) {
    assert.throws(() => parseGoogleResult(raw), /google (result|state)/);
  }
});

test("every status has its label; an unknown one is not shown raw", () => {
  assert.deepEqual(clientStatuses.map(clientStatusText), [
    "Клиент Google задан", "Клиент Google не задан", "Диспетчер учётных данных Windows недоступен в этой системе",
    "Диспетчер учётных данных не ответил",
  ]);
  assert.deepEqual(accountStatuses.map(accountStatusText), [
    "Вход в Google выполнен", "Вход в Google не выполнен", "Вход в Google недоступен в этой системе",
    "Не удалось узнать, выполнен ли вход в Google",
  ]);
  assert.equal(clientStatusText("toString"), "Диспетчер учётных данных не ответил");
  assert.equal(accountStatusText("constructor"), "Не удалось узнать, выполнен ли вход в Google");
  assert.deepEqual(["set", "unset", "signedIn", "signedOut", "unsupported", "unavailable"].map(statusWarns),
    [false, false, false, false, true, true]);
  assert.equal(googleTexts.title, "Google Календарь");
  assert.equal(googleTexts.signIn, "Войти через Google");
  assert.equal(googleTexts.waiting, "Ожидание входа в браузере…");
  assert.match(googleTexts.hint, /«Desktop app»/);
});

test("controls: sign in only with a stored client, one action at a time, cancel while waiting", () => {
  const idle = { action: null, typedId: false, typedSecret: false };

  assert.deepEqual(googleControls({ ...idle, state: fresh }), {
    inputsDisabled: false, saveDisabled: true, removeShown: false, removeDisabled: false,
    signingIn: false, signInShown: true, signInDisabled: true, signOutShown: false, signOutDisabled: false,
  });
  // Saving needs both values.
  assert.equal(googleControls({ ...idle, state: fresh, typedId: true }).saveDisabled, true);
  assert.equal(googleControls({ ...idle, state: fresh, typedSecret: true }).saveDisabled, true);
  assert.equal(googleControls({ ...idle, state: fresh, typedId: true, typedSecret: true }).saveDisabled, false);

  const r = googleControls({ ...idle, state: ready });
  assert.equal(r.signInShown, true);
  assert.equal(r.signInDisabled, false);
  assert.equal(r.removeShown, true);
  assert.equal(r.signOutShown, false);

  const s = googleControls({ ...idle, state: signedIn });
  assert.equal(s.signInShown, false);
  assert.equal(s.signOutShown, true);
  assert.equal(s.signOutDisabled, false);

  // Own sign-in call or one reported by the core: waiting, everything else disabled.
  for (const c of [
    googleControls({ ...idle, state: ready, action: "signIn" }),
    googleControls({ ...idle, state: { ...ready, signingIn: true }, typedId: true, typedSecret: true }),
  ]) {
    assert.equal(c.signingIn, true);
    assert.equal(c.signInShown, false);
    assert.equal(c.signOutShown, false);
    assert.equal(c.inputsDisabled, true);
    assert.equal(c.saveDisabled, true);
    assert.equal(c.removeDisabled, true);
  }

  for (const action of ["save", "remove", "signOut"]) {
    const c = googleControls({ state: signedIn, action, typedId: true, typedSecret: true });
    assert.equal(c.saveDisabled, true, action);
    assert.equal(c.signOutDisabled, true, action);
    assert.equal(c.removeDisabled, true, action);
    assert.equal(c.signingIn, false, action);
  }

  // No secret store: nothing to type into.
  const u = googleControls({ ...idle, state: { client: "unsupported", account: "unsupported", signingIn: false },
    typedId: true, typedSecret: true });
  assert.equal(u.inputsDisabled, true);
  assert.equal(u.saveDisabled, true);
  assert.equal(u.signInDisabled, true);
  assert.equal(u.removeShown, false);
});

test("the status is polled only for a sign-in the page did not start", () => {
  assert.equal(statusPollMs, 2000);
  assert.equal(pollStatus(null, null), false);
  assert.equal(pollStatus(ready, null), false);
  assert.equal(pollStatus({ ...ready, signingIn: true }, null), true);
  assert.equal(pollStatus({ ...ready, signingIn: true }, "signIn"), false);
});

test("the block renders text only and keeps the client id and secret write-only", () => {
  for (const file of ["../src/pages/GoogleBlock.tsx", "../src/googleView.ts", "../src/bindings/google.ts"]) {
    const s = src(file);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "console.", "localStorage", "sessionStorage", "href=",
      "fetch(", "googleapis.com", "accounts.google.com"]) {
      assert.ok(!s.includes(banned), `${file} uses ${banned}`);
    }
  }
  const block = src("../src/pages/GoogleBlock.tsx");
  assert.equal([...block.matchAll(/type="password"/g)].length, 1);
  assert.match(block, /type="password"\s+autoComplete="new-password"/);
  assert.match(block, /value=\{secret\}/);
  // The secret is cleared after the save call whatever its outcome, and goes only into that call.
  assert.match(block, /finally \{\s*setSecret\(""\);\s*\}/);
  assert.equal([...block.matchAll(/api\.setClient\(/g)].length, 1);
  assert.match(block, /api\.setClient\(clientId, secret\)/);
  assert.equal([...block.matchAll(/useState/g)].length, 9, "a new state: check that it holds no secret");
  // The page holds no Google value of its own; it renders the block.
  const page = src("../src/pages/ConnectionsPage.tsx");
  assert.match(page, /<GoogleBlock \/>/);
  assert.ok(!/googleApi|setClient|useState[^;]*[Cc]lient/.test(page), "the page handles the Google client");
});
