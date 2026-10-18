// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  bundleStatuses,
  diagnosticsApi,
  methods,
  parseBundleResult,
  parseSoundCheck,
  soundStates,
  soundVerdicts,
} from "../.test-build/bindings/diagnostics.js";
import {
  bundleButton,
  bundleFailedLines,
  bundleLines,
  checkButton,
  pollMs,
  soundDisplay,
  soundFailedDisplay,
  texts,
} from "../.test-build/diagnosticsView.js";

// Mirrors the JSON of internal/input/wails/diagnostics.go (diagnostics_test.go).
const running = {
  state: "running", level: 0.123, peak: 0.5, heard: true, elapsedMs: 4200, remainingMs: 10800, durationMs: 15000,
  verdict: "", message: "Звук слышен.", hints: [], source: "zoom.exe", wholeComputer: false,
};
const silence = {
  ...running, state: "done", level: 0, peak: 0.001, heard: false, elapsedMs: 15000, remainingMs: 0, verdict: "silence",
  message: "Звук не слышен.", hints: ["Проверьте микшер."], source: "Весь звук компьютера", wholeComputer: true,
};
const idle = { ...running, state: "idle", level: 0, peak: 0, heard: false, elapsedMs: 0, remainingMs: 15000, message: "",
  source: "" };
const saved = { status: "saved", message: "Пакет для поддержки сохранён.", name: "la-support-20261016.zip" };

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("the api calls the bound Go methods without arguments", async () => {
  const p = "paratech/desktop/internal/input/wails.DiagnosticsService.";
  assert.deepEqual(Object.values(methods).sort(),
    ["SaveSupportBundle", "SoundCheckStatus", "StartSoundCheck", "StopSoundCheck"].map((m) => p + m));

  const sound = fakeCall(running);
  const api = diagnosticsApi(sound.call);
  assert.deepEqual(await api.startSoundCheck(), running);
  assert.deepEqual(await api.soundCheckStatus(), running);
  assert.deepEqual(await api.stopSoundCheck(), running);
  const bundle = fakeCall(saved);
  assert.deepEqual(await diagnosticsApi(bundle.call).saveSupportBundle(), saved);

  // The page never passes a path: the core asks for it in its own dialog.
  assert.deepEqual(sound.calls, [[p + "StartSoundCheck"], [p + "SoundCheckStatus"], [p + "StopSoundCheck"]]);
  assert.deepEqual(bundle.calls, [[p + "SaveSupportBundle"]]);
});

test("a failing call rejects", async () => {
  const api = diagnosticsApi(async () => {
    throw new Error("boom");
  });
  await assert.rejects(api.startSoundCheck(), /boom/);
  await assert.rejects(api.soundCheckStatus(), /boom/);
  await assert.rejects(api.stopSoundCheck(), /boom/);
  await assert.rejects(api.saveSupportBundle(), /boom/);
});

test("sound check replies are parsed strictly; extra fields are dropped", () => {
  assert.deepEqual(soundStates, ["idle", "running", "done"]);
  assert.deepEqual(soundVerdicts, ["", "ok", "silence", "not_running", "no_device", "unsupported", "busy", "failed"]);
  for (const verdict of soundVerdicts.filter((v) => v !== "")) {
    assert.equal(parseSoundCheck({ ...silence, verdict }).verdict, verdict);
  }
  assert.deepEqual(parseSoundCheck({ ...running, path: "C:\\x" }), running);
  assert.deepEqual(parseSoundCheck({ ...silence, hints: null }).hints, []);
  assert.deepEqual(parseSoundCheck(idle), idle);
  for (const raw of [
    null, "ok", {},
    { ...running, state: "paused" },
    { ...running, verdict: "ok" },
    { ...silence, verdict: "" },
    { ...silence, verdict: "toString" },
    { ...running, level: 1.5 },
    { ...running, level: -0.1 },
    { ...running, peak: "0.5" },
    { ...running, level: Number.NaN },
    { ...running, elapsedMs: -1 },
    { ...running, remainingMs: 1.5 },
    { ...running, durationMs: undefined },
    { ...running, heard: "yes" },
    { ...running, message: null },
    { ...running, hints: "x" },
    { ...running, hints: [1] },
    { ...running, source: undefined },
    { ...running, wholeComputer: 0 },
  ]) {
    assert.throws(() => parseSoundCheck(raw), /sound check/);
  }
});

test("bundle replies are parsed strictly: only a saved bundle has a name", () => {
  assert.deepEqual(bundleStatuses, ["saved", "cancelled", "failed"]);
  assert.deepEqual(parseBundleResult({ ...saved, path: "C:\\Users\\x\\la.zip" }), saved);
  const cancelled = { status: "cancelled", message: "Сохранение отменено.", name: "" };
  assert.deepEqual(parseBundleResult(cancelled), cancelled);
  for (const raw of [
    null, {},
    { ...saved, status: "ok" },
    { ...saved, name: "" },
    { ...cancelled, name: "la.zip" },
    { status: "failed", message: "m", name: "la.zip" },
    { ...saved, message: undefined },
    { ...saved, name: null },
  ]) {
    assert.throws(() => parseBundleResult(raw), /bundle result/);
  }
});

test("a check shows the source, the level while it runs, then the verdict and its hints", () => {
  assert.deepEqual(soundDisplay(running), {
    running: true, level: 0.123, source: "Источник: zoom.exe", lines: [{ text: "Звук слышен.", tone: "ok" }],
  });
  assert.deepEqual(soundDisplay({ ...running, heard: false, message: "Включите звук." }).lines,
    [{ text: "Включите звук.", tone: "muted" }]);
  assert.deepEqual(soundDisplay(silence), {
    running: false, level: 0, source: "Источник: Весь звук компьютера",
    lines: [{ text: "Звук не слышен.", tone: "error" }, { text: "Проверьте микшер.", tone: "muted" }],
  });
  assert.equal(soundDisplay({ ...silence, verdict: "ok", message: "Звук слышен.", hints: [] }).lines[0].tone, "ok");
  assert.equal(soundDisplay({ ...silence, verdict: "busy" }).lines[0].tone, "warning");
  assert.deepEqual(soundDisplay(idle), { running: false, level: 0, source: "", lines: [] });
  assert.deepEqual(soundFailedDisplay(),
    { running: false, level: 0, source: "", lines: [{ text: "Не удалось проверить звук.", tone: "error" }] });
});

test("the bundle result: the name when saved, a neutral line when cancelled", () => {
  assert.deepEqual(bundleLines(saved), [
    { text: "Сохранено: la-support-20261016.zip", tone: "ok" },
    { text: "Пакет для поддержки сохранён.", tone: "muted" },
  ]);
  assert.deepEqual(bundleLines({ status: "cancelled", message: "Сохранение отменено.", name: "" }), [
    { text: "Сохранение отменено.", tone: "muted" },
  ]);
  assert.deepEqual(bundleLines({ status: "failed", message: "Не удалось.", name: "" }), [
    { text: "Не удалось.", tone: "error" },
  ]);
  assert.deepEqual(bundleFailedLines(), [{ text: "Не удалось сохранить пакет для поддержки.", tone: "error" }]);
});

test("the sound button starts or stops; buttons are disabled while their call runs", () => {
  assert.deepEqual(checkButton(false, false), { label: "Проверить звук", disabled: false });
  assert.deepEqual(checkButton(true, false), { label: "Остановить проверку", disabled: false });
  assert.deepEqual(checkButton(false, true), { label: "Проверить звук", disabled: true });
  assert.deepEqual(bundleButton(false), { label: "Сохранить пакет для поддержки…", disabled: false });
  assert.deepEqual(bundleButton(true), { label: "Сохраняем…", disabled: true });
  // 10 reads a second, as fast as the core reports levels at most (20 a second).
  assert.equal(pollMs, 100);
});

test("the explanations say what is checked and what the bundle holds", () => {
  assert.match(texts.soundExplain, /Ничего не проигрывается/);
  assert.match(texts.bundleExplain, /без паролей, ключей, аудио, расшифровок и текстов уроков/);
  assert.match(texts.bundleLocal, /никуда не отправляется/);
});

test("the page renders text only and never handles a path", () => {
  for (const file of ["../src/pages/DiagnosticsPage.tsx", "../src/diagnosticsView.ts", "../src/bindings/diagnostics.ts"]) {
    const s = src(file);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "console.", "localStorage", "sessionStorage", "href="]) {
      assert.ok(!s.includes(banned), `${file} uses ${banned}`);
    }
  }
  const page = src("../src/pages/DiagnosticsPage.tsx");
  assert.match(page, /api\.startSoundCheck\(\)/);
  assert.match(page, /api\.soundCheckStatus\(\)/);
  assert.match(page, /api\.stopSoundCheck\(\)/);
  assert.match(page, /api\.saveSupportBundle\(\)/);
  assert.ok(!/<input\b/.test(page), "the page asks for a path itself");
});
