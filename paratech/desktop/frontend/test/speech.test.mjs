// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { methods, parseStatus, speechApi } from "../.test-build/bindings/speech.js";
import {
  installPercent,
  progressText,
  speechErrorText,
  speechNotice,
  speechTexts,
  stageLabel,
} from "../.test-build/speechView.js";

const prefix = "paratech/desktop/internal/input/wails.SpeechService.";
const mb = 1024 * 1024;

// Mirrors SpeechStatusView in internal/input/wails/speech.go.
const missing = {
  state: "missing", stage: "", doneBytes: 0, totalBytes: 0,
  message: "Компонент распознавания речи не установлен", canInstall: true, canCancel: false,
};
const installing = {
  state: "installing", stage: "downloading", doneBytes: 3 * mb, totalBytes: 12 * mb,
  message: "", canInstall: false, canCancel: true,
};
const ready = { ...missing, state: "ready", message: "", canInstall: false };

test("the bound names are the SpeechService methods", () => {
  assert.deepEqual(Object.values(methods).sort(), [
    `${prefix}CancelInstall`, `${prefix}StartInstall`, `${prefix}Status`,
  ]);
});

test("the api calls the bound names and parses the status", async () => {
  const calls = [];
  const api = speechApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return method === methods.status ? installing : null;
  });
  assert.deepEqual(await api.status(), installing);
  await api.startInstall();
  await api.cancelInstall();
  // No call carries a URL or a path.
  assert.deepEqual(calls, [[methods.status], [methods.startInstall], [methods.cancelInstall]]);
});

test("a malformed status is an error, not a default", () => {
  for (const s of ["installing", "ready", "missing", "damaged", "failed"]) {
    assert.equal(parseStatus({ ...missing, state: s }).state, s);
  }
  for (const stage of ["", "downloading", "verifying", "unpacking", "activating"]) {
    assert.equal(parseStatus({ ...installing, stage }).stage, stage);
  }
  for (const bad of [
    null, "x", 1,
    { ...missing, state: "broken" }, { ...missing, state: undefined },
    { ...installing, stage: "copying" }, { ...installing, stage: null },
    { ...installing, doneBytes: -1 }, { ...installing, totalBytes: 1.5 }, { ...installing, totalBytes: "12" },
    { ...missing, message: null }, { ...missing, canInstall: "yes" }, { ...missing, canCancel: undefined },
  ]) {
    assert.throws(() => parseStatus(bad), undefined, JSON.stringify(bad));
  }
});

test("stages have Russian labels", () => {
  assert.equal(stageLabel("downloading"), "Скачивание");
  assert.equal(stageLabel("verifying"), "Проверка");
  assert.equal(stageLabel("unpacking"), "Распаковка");
  assert.equal(stageLabel("activating"), "Включение");
  assert.equal(stageLabel(""), "Установка");
});

test("the progress shows the stage and the downloaded bytes", () => {
  assert.equal(installPercent(installing), 25);
  assert.equal(progressText(installing), "Скачивание: 25% (3 МБ из 12 МБ)");
  assert.equal(installPercent({ ...installing, totalBytes: 0 }), null);
  assert.equal(progressText({ ...installing, totalBytes: 0 }), "Скачивание: 3 МБ");
  assert.equal(installPercent({ ...installing, doneBytes: 20 * mb }), 100);
  assert.equal(progressText({ ...installing, stage: "verifying" }), "Проверка…");
  assert.equal(installPercent({ ...installing, stage: "unpacking" }), null);
  assert.equal(progressText({ ...installing, stage: "activating" }), "Включение…");
  assert.equal(progressText(missing), "");
  assert.equal(installPercent(missing), null);
});

test("the notice per state", () => {
  assert.equal(speechNotice(ready), null);

  assert.deepEqual(speechNotice(missing), {
    kind: "wait", text: missing.message, hint: speechTexts.noRestart, progress: "", percent: null,
    install: true, cancel: false,
  });
  const damaged = { ...missing, state: "damaged", message: "Компонент распознавания речи повреждён: установите его заново" };
  assert.equal(speechNotice(damaged).text, damaged.message);
  assert.equal(speechNotice(damaged).install, true);

  const failed = { ...missing, state: "failed", message: "Не удалось скачать компонент: проверьте подключение к интернету" };
  assert.equal(speechNotice(failed).kind, "fail");
  assert.equal(speechNotice(failed).text, failed.message);
  assert.equal(speechNotice(failed).install, true);

  const n = speechNotice(installing);
  assert.equal(n.text, speechTexts.installing);
  assert.equal(n.progress, "Скачивание: 25% (3 МБ из 12 МБ)");
  assert.equal(n.percent, 25);
  assert.equal(n.install, false);
  assert.equal(n.cancel, true);
  assert.equal(n.hint, "");

  // The buttons follow the core's flags only.
  assert.equal(speechNotice({ ...missing, canInstall: false }).install, false);
  assert.equal(speechNotice({ ...missing, message: "" }).text, speechTexts.unavailable);
});

test("the core's refusals are shown as is, anything else gets a generic text", () => {
  const go = readFileSync(new URL("../../internal/input/wails/speech.go", import.meta.url), "utf8");
  const texts = [...go.matchAll(/speechText = "([^"]+)"/g)].map((m) => m[1]);
  assert.ok(texts.length >= 10, "speech.go texts");
  for (const t of texts) {
    assert.equal(speechErrorText(new Error(t)), t);
  }
  assert.ok(texts.includes(missing.message));
  assert.equal(speechErrorText("Распознавание речи уже работает"), "Распознавание речи уже работает");
  assert.equal(speechErrorText(new Error("StartInstall: panic: C:\\x")), speechTexts.genericError);
  assert.equal(speechErrorText(undefined), speechTexts.genericError);
});

test("the notice is plain text and names no program files", () => {
  for (const file of ["../src/pages/SpeechComponentNotice.tsx", "../src/speechView.ts", "../src/bindings/speech.ts"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "href=", "window.open", "whisper", ".exe", "DLL", "learning-assistant"]) {
      assert.ok(!src.includes(banned), `${file} uses ${banned}`);
    }
  }
  for (const t of Object.values(speechTexts)) {
    assert.doesNotMatch(t, /whisper|\.exe|DLL|learning-assistant/);
  }
  for (const page of ["../src/pages/ModelsPage.tsx", "../src/pages/RecordingsPage.tsx"]) {
    assert.match(readFileSync(new URL(page, import.meta.url), "utf8"), /<SpeechComponentNotice /);
  }
});
