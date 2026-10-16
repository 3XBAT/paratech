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
  soundModes,
  soundVerdicts,
} from "../.test-build/bindings/diagnostics.js";
import {
  bundleButton,
  bundleFailedLines,
  bundleLines,
  causeText,
  checkButton,
  modeLabels,
  soundDisplay,
  soundFailedDisplay,
  texts,
} from "../.test-build/diagnosticsView.js";

// Mirrors the JSON of internal/input/wails/diagnostics.go (diagnostics_test.go).
const checked = {
  verdict: "ok", message: "Звук записывается: проверочный сигнал слышен.", device: "Динамики (Realtek Audio)",
  handsFree: false, hint: "",
  modes: [{ mode: "include", ok: true, cause: "ok" }, { mode: "system", ok: false, cause: "silence" }],
};
const busy = { verdict: "busy", message: "Проверка звука уже идёт.", device: "", handsFree: false, hint: "", modes: [] };
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
  assert.deepEqual(Object.values(methods).sort(), ["CheckSound", "SaveSupportBundle"].map((m) => p + m));

  const sound = fakeCall(checked);
  assert.deepEqual(await diagnosticsApi(sound.call).checkSound(), checked);
  const bundle = fakeCall(saved);
  assert.deepEqual(await diagnosticsApi(bundle.call).saveSupportBundle(), saved);

  // The page never passes a path: the core asks for it in its own dialog.
  assert.deepEqual(sound.calls, [[p + "CheckSound"]]);
  assert.deepEqual(bundle.calls, [[p + "SaveSupportBundle"]]);
});

test("a failing call rejects", async () => {
  const api = diagnosticsApi(async () => {
    throw new Error("boom");
  });
  await assert.rejects(api.checkSound(), /boom/);
  await assert.rejects(api.saveSupportBundle(), /boom/);
});

test("sound check replies are parsed strictly; extra fields are dropped", () => {
  assert.deepEqual(soundVerdicts, ["ok", "no_device", "silence", "unsupported", "hands_free", "busy", "failed"]);
  assert.deepEqual(soundModes, ["include", "system"]);
  for (const verdict of soundVerdicts) {
    assert.equal(parseSoundCheck({ ...checked, verdict }).verdict, verdict);
  }
  assert.deepEqual(parseSoundCheck({ ...checked, path: "C:\\x", modes: [{ ...checked.modes[0], extra: 1 }] }).modes,
    [checked.modes[0]]);
  assert.deepEqual(parseSoundCheck({ ...busy, modes: null }).modes, []);
  for (const raw of [
    null, "ok", {},
    { ...checked, verdict: "fine" },
    { ...checked, verdict: "toString" },
    { ...checked, message: null },
    { ...checked, device: undefined },
    { ...checked, handsFree: "no" },
    { ...checked, hint: 0 },
    { ...checked, modes: "include" },
    { ...checked, modes: [null] },
    { ...checked, modes: [{ mode: "exclude", ok: true, cause: "ok" }] },
    { ...checked, modes: [{ mode: "include", ok: "true", cause: "ok" }] },
    { ...checked, modes: [{ mode: "include", ok: true }] },
  ]) {
    assert.throws(() => parseSoundCheck(raw), /sound/);
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

test("the check result shows the verdict, the device, the hint and every mode", () => {
  assert.deepEqual(soundDisplay(checked), {
    lines: [
      { text: "Звук записывается: проверочный сигнал слышен.", tone: "ok" },
      { text: "Устройство вывода: Динамики (Realtek Audio)", tone: "muted" },
    ],
    modes: [
      { label: "Захват приложения", text: "Работает", ok: true },
      { label: "Весь звук компьютера (классический)", text: "Сигнал не слышен.", ok: false },
    ],
  });

  const headset = { ...checked, verdict: "silence", message: "Не слышен.", hint: "Гарнитура.", handsFree: true };
  assert.deepEqual(soundDisplay(headset).lines, [
    { text: "Не слышен.", tone: "error" },
    { text: "Устройство вывода: Динамики (Realtek Audio)", tone: "muted" },
    { text: "Гарнитура.", tone: "warning" },
  ]);
  // Busy is a wait, not a fault; no device and no modes means one line.
  assert.deepEqual(soundDisplay(busy), { lines: [{ text: "Проверка звука уже идёт.", tone: "warning" }], modes: [] });
  assert.deepEqual(soundFailedDisplay(), { lines: [{ text: "Не удалось проверить звук.", tone: "error" }], modes: [] });

  assert.deepEqual(modeLabels, { include: "Захват приложения", system: "Весь звук компьютера (классический)" });
  for (const cause of ["no_device", "silence", "unsupported", "hands_free", "busy", "failed"]) {
    assert.notEqual(causeText(cause), "", cause);
  }
  // An unknown cause is not shown raw.
  assert.equal(causeText("toString"), causeText("failed"));
  assert.equal(causeText("<b>x</b>"), "Не удалось проверить.");
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

test("buttons are disabled while their call runs", () => {
  assert.deepEqual(checkButton(false), { label: "Проверить звук", disabled: false });
  assert.deepEqual(checkButton(true), { label: "Проверяем…", disabled: true });
  assert.deepEqual(bundleButton(false), { label: "Сохранить пакет для поддержки…", disabled: false });
  assert.deepEqual(bundleButton(true), { label: "Сохраняем…", disabled: true });
});

test("the explanations say what is checked and what the bundle holds", () => {
  assert.match(texts.soundExplain, /устройство вывода по умолчанию/);
  assert.match(texts.soundExplain, /Тихая громкость/);
  assert.match(texts.soundScope, /не что слышно выбранное приложение/);
  assert.match(texts.bundleExplain, /без паролей, ключей, аудио, расшифровок и текстов уроков/);
  assert.match(texts.bundleLocal, /никуда не отправляется/);
  // DefaultSelfTestTimeout of internal/adapters/audiocapture/selftest.go is at most 15 s.
  assert.equal(texts.checkTakes, "Проверка занимает до 15 секунд.");
});

test("the page renders text only and never handles a path", () => {
  for (const file of ["../src/pages/DiagnosticsPage.tsx", "../src/diagnosticsView.ts", "../src/bindings/diagnostics.ts"]) {
    const s = src(file);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "console.", "localStorage", "sessionStorage", "href="]) {
      assert.ok(!s.includes(banned), `${file} uses ${banned}`);
    }
  }
  const page = src("../src/pages/DiagnosticsPage.tsx");
  assert.match(page, /api\.checkSound\(\)/);
  assert.match(page, /api\.saveSupportBundle\(\)/);
  assert.ok(!/<input\b/.test(page), "the page asks for a path itself");
});
