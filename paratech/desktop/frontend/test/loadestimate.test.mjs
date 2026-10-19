// Recognition load modes and time estimates (TASK-084). Runs on the modules compiled by `npm run test`
// (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { parseModelLoadEstimates, recognitionLoads } from "../.test-build/bindings/load.js";
import { parseView } from "../.test-build/bindings/models.js";
import { methods, parseSettings, settingsApi } from "../.test-build/bindings/settings.js";
import {
  durationText,
  estimatesTip,
  estimateTexts,
  estimateValue,
  loadDescriptions,
  loadLabels,
  modelEstimateLine,
  remainingText,
  settingEstimateLine,
} from "../.test-build/durationEstimate.js";
import { catalogRow, installedRow, texts } from "../.test-build/modelsView.js";
import { RecordingRow } from "../.test-build/pages/RecordingRow.js";
import { recordingRowView } from "../.test-build/recordingRowView.js";

const { renderToStaticMarkup } = server;

// Mirror the JSON of internal/input/wails/load.go.
const est = (mode, secondsPerHour, source = "table", heavy = secondsPerHour > 3600) => ({ mode, secondsPerHour, source, heavy });
const large = {
  current: est("gentle", 12000),
  modes: [est("gentle", 12000), est("normal", 8280, "measured"), est("max", 3000)],
};
const small = { current: est("normal", 1500, "measured"), modes: [est("gentle", 0, "unknown"), est("normal", 1500, "measured"), est("max", 900)] };

test("durations are whole minutes, rounded up, at least 1 мин, hours above 60 мин", () => {
  assert.equal(durationText(200 * 60), "3 ч 20 мин");
  assert.equal(durationText(45 * 60), "45 мин");
  assert.equal(durationText(12), "1 мин");
  assert.equal(durationText(0), "1 мин");
  assert.equal(durationText(61), "2 мин", "rounded up");
  assert.equal(durationText(59 * 60 + 1), "1 ч");
  assert.equal(durationText(120 * 60), "2 ч", "no «0 мин»");
  assert.equal(durationText(121 * 60), "2 ч 1 мин");
  assert.equal(durationText(25 * 3600), "25 ч");
  assert.equal(durationText(-1), "");
  assert.equal(durationText(Number.NaN), "");
});

test("the three modes have their Russian names and one-line descriptions", () => {
  assert.deepEqual(recognitionLoads, ["gentle", "normal", "max"]);
  assert.deepEqual(loadLabels, { gentle: "Бережная", normal: "Обычная", max: "Максимальная" });
  assert.deepEqual(loadDescriptions, {
    gentle: "низкий приоритет, не больше ~40 % процессора",
    normal: "низкий приоритет, без потолка",
    max: "без ограничений, быстрее всего",
  });
  assert.equal(estimateTexts.nextRun, "Применится к следующему распознаванию.");
  assert.equal(estimateTexts.setting, "Нагрузка при распознавании");
});

test("estimates are parsed strictly; null is no estimate", () => {
  assert.deepEqual(parseModelLoadEstimates(large), large);
  assert.equal(parseModelLoadEstimates(null), null);
  assert.deepEqual(parseModelLoadEstimates({ current: large.current, modes: null }).modes, []);
  for (const bad of [
    undefined, "x", {}, { ...large, current: null }, { ...large, current: { ...large.current, mode: "turbo" } },
    { ...large, current: { ...large.current, source: "guess" } }, { ...large, current: { ...large.current, secondsPerHour: -1 } },
    { ...large, current: { ...large.current, secondsPerHour: 1.5 } }, { ...large, current: { ...large.current, heavy: "no" } },
    { ...large, modes: {} },
  ]) {
    assert.throws(() => parseModelLoadEstimates(bad), undefined, JSON.stringify(bad));
  }
});

test("a model line: the number in the current mode, «примерно» for the table, a warning above 1 h", () => {
  assert.deepEqual(modelEstimateLine(large), {
    text: "Запись 1 ч ≈ 3 ч 20 мин в режиме «Бережная» (примерно) — дольше самой записи",
    heavy: true,
    tip: "Бережная: ≈ 3 ч 20 мин (примерно)\nОбычная: ≈ 2 ч 18 мин\nМаксимальная: ≈ 50 мин (примерно)",
  });
  const s = modelEstimateLine(small);
  assert.equal(s.text, "Запись 1 ч ≈ 25 мин в режиме «Обычная»", "measured: no «примерно»");
  assert.equal(s.heavy, false);
  assert.equal(s.tip, "Бережная: скорость неизвестна до первого распознавания\nОбычная: ≈ 25 мин\nМаксимальная: ≈ 15 мин (примерно)");

  const none = { text: "", heavy: false, tip: "" };
  assert.deepEqual(modelEstimateLine(null), none);
  assert.deepEqual(modelEstimateLine(undefined), none);
  assert.deepEqual(modelEstimateLine({ ...small, current: est("gentle", 0, "unknown") }), none, "unknown: nothing shown");
  assert.deepEqual(modelEstimateLine({ ...small, current: est("gentle", 0, "table") }), none, "0 is unknown");
  assert.equal(estimatesTip(null), "");
  assert.equal(estimateValue(est("max", 90, "measured")), "≈ 2 мин");
});

test("the setting shows the model in use in the selected mode", () => {
  assert.deepEqual(settingEstimateLine(large, "large-v3-f16", "gentle"), {
    text: "Модель «large-v3-f16»: запись 1 ч ≈ 3 ч 20 мин (примерно) — дольше самой записи",
    heavy: true,
  });
  assert.deepEqual(settingEstimateLine(large, "large-v3-f16", "normal"), { text: "Модель «large-v3-f16»: запись 1 ч ≈ 2 ч 18 мин — дольше самой записи", heavy: true });
  assert.deepEqual(settingEstimateLine(small, "small-q5_1", "max"), { text: "Модель «small-q5_1»: запись 1 ч ≈ 15 мин (примерно)", heavy: false });
  assert.equal(settingEstimateLine(small, "small-q5_1", "gentle"), null, "unknown speed");
  assert.equal(settingEstimateLine(null, "", "gentle"), null);
  assert.equal(settingEstimateLine(large, "", "gentle"), null);
});

test("settings: the load mode is sent by its bound method and parsed strictly", async () => {
  const reply = {
    recordingPromptsEnabled: true, processOnlyOnAcPower: false, recognitionLoad: "max",
    loadEstimates: large, loadEstimatesModel: "large-v3-f16",
  };
  const calls = [];
  const api = settingsApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  });
  assert.deepEqual(await api.setRecognitionLoad("max"), reply);
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.SettingsService.SetRecognitionLoad", "max"]]);
  assert.equal(methods.setRecognitionLoad, calls[0][0]);
  for (const bad of [
    { ...reply, recognitionLoad: "turbo" }, { ...reply, recognitionLoad: undefined }, { ...reply, loadEstimatesModel: null },
    { ...reply, loadEstimates: undefined }, { ...reply, loadEstimates: { current: null } },
  ]) {
    assert.throws(() => parseSettings(bad), /settings|load estimate/, JSON.stringify(bad));
  }
});

test("the settings page offers the three modes, says when a change applies and warns with the number", () => {
  const page = readFileSync(new URL("../src/pages/SettingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /<Segmented\s+label=\{estimateTexts\.setting\}/);
  assert.match(page, /\{estimateTexts\.nextRun\}/);
  assert.match(page, /loadDescriptions\[l\]/);
  assert.match(page, /<span className="small slow">\s*<b>\{estimate\.text\}<\/b>/, "a heavy estimate is a bold warning");
  assert.match(page, /api\.setRecognitionLoad\(load\)/);
});

const catalogEntry = {
  catalogId: "large-v3-f16", name: "large", version: "3", sizeBytes: 3_000_000_000, quantization: "", capabilities: ["multilingual"],
  minCpuCores: 4, minRamMb: 4096, recommended: false, likelySlow: false, installed: true, installedId: "m-1", hasSourcePage: true,
  tier: "max", englishOnly: false, family: "large-v3", load: 5, heavy: true, reduced: false, estimates: large,
};
const installedModel = {
  id: "m-1", name: "large", version: "3", sizeBytes: 3_000_000_000, sha256: "a".repeat(64), quantization: "",
  capabilities: ["multilingual"], isDefault: true, fromCatalog: true, catalogId: "large-v3-f16",
  installedAt: "2026-10-15T06:30:00Z", inUse: true, estimates: small,
};

test("models: every row shows the estimate; the number replaces the vague heavy warning", () => {
  const r = catalogRow(catalogEntry);
  assert.equal(r.estimate, "Запись 1 ч ≈ 3 ч 20 мин в режиме «Бережная» (примерно) — дольше самой записи");
  assert.equal(r.estimateHeavy, true);
  assert.match(r.estimateTip, /^Бережная: ≈ 3 ч 20 мин \(примерно\)\nОбычная: /);
  assert.equal(r.warning, "", "the number is the warning");
  assert.equal(catalogRow({ ...catalogEntry, estimates: null }).warning, texts.heavy, "without a number the old warning stays");
  assert.equal(catalogRow({ ...catalogEntry, estimates: null }).estimate, "");

  const i = installedRow(installedModel, [catalogEntry]);
  assert.equal(i.estimate, "Запись 1 ч ≈ 25 мин в режиме «Обычная»", "an installed model has its own (measured) estimates");
  assert.equal(i.estimateHeavy, false);
  const own = installedRow({ ...installedModel, fromCatalog: false, catalogId: "", estimates: null }, []);
  assert.deepEqual([own.estimate, own.estimateTip], ["", ""]);

  const v = parseView({
    installed: [installedModel], available: [catalogEntry], needsDefault: false, speechReady: true, machineKnown: true,
    download: { catalogId: "", phase: "", doneBytes: 0, totalBytes: 0, running: false, error: "" }, modelsHomePage: "x",
    recognitionLoad: "gentle",
  });
  assert.equal(v.recognitionLoad, "gentle");
  assert.deepEqual(v.available[0].estimates, large);
  assert.throws(() => parseView({ ...v, recognitionLoad: "turbo" }));
  assert.throws(() => parseView({ ...v, recognitionLoad: undefined }));
  assert.throws(() => parseView({ ...v, available: [{ ...catalogEntry, estimates: undefined }] }));

  const page = readFileSync(new URL("../src/pages/ModelsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /<p className=\{r\.estimateHeavy \? "mest small slow" : "mest small muted"\} title=\{r\.estimateTip\}>/);
});

const recording = {
  id: "r1", lessonId: "l1", title: "Матанализ", status: "COMPLETED", startedAt: "2026-10-18T17:19:35Z", stoppedAt: "",
  durationSeconds: 3600, hasDuration: true, problem: "", stoppable: false, stage: "transcribing", step: "",
  progressPercent: 40, queueReason: "", errorCode: "", jobError: "", action: "", actionJobId: "",
  remainingSeconds: 1490, hasRemaining: true,
};

test("a recording row shows the time left as its own element, only when the core has one", () => {
  assert.equal(remainingText(recording), "≈ 25 мин осталось");
  assert.equal(remainingText({ remainingSeconds: 12_000, hasRemaining: true }), "≈ 3 ч 20 мин осталось");
  assert.equal(remainingText({ remainingSeconds: 5, hasRemaining: true }), "≈ 1 мин осталось");
  assert.equal(remainingText({ remainingSeconds: 1490, hasRemaining: false }), "");
  assert.equal(remainingText({}), "");
  assert.equal(recordingRowView(recording, 0).remaining, "≈ 25 мин осталось");
  assert.equal(recordingRowView({ ...recording, hasRemaining: false }, 0).remaining, "");

  const props = { now: 0, stopping: false, starting: false, onStop: () => undefined, onRunNow: () => undefined, onOpen: () => undefined };
  const html = renderToStaticMarkup(h(RecordingRow, { item: recording, ...props }));
  assert.ok(html.includes('<span class="small muted num row-remaining">≈ 25 мин осталось</span>'), html);
  const none = renderToStaticMarkup(h(RecordingRow, { item: { ...recording, hasRemaining: false }, ...props }));
  assert.ok(!none.includes("осталось"));
});
