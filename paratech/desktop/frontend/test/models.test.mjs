// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  identityErrors,
  methods,
  modelsApi,
  parseCatalogImport,
  parseSelection,
  parseView,
} from "../.test-build/bindings/models.js";
import {
  canConfirm,
  catalogActions,
  catalogLabels,
  confirmation,
  confirmStep,
  defaultBadge,
  downloadChip,
  downloadOf,
  downloadText,
  errorText,
  keepsSelection,
  modelTitle,
  progressPercent,
  shortSha,
  sizeText,
  statusNotes,
  texts,
} from "../.test-build/modelsView.js";

const prefix = "paratech/desktop/internal/input/wails.ModelsService.";
const sum = "a".repeat(64);

// Mirror the JSON of internal/input/wails/models.go (TestModelsListShowsInstalledAndAvailable, TestModelsJSONNames).
const installed = {
  id: "m-1", name: "base", version: "1", sizeBytes: 59707625, sha256: sum, quantization: "q5_1",
  capabilities: ["multilingual"], isDefault: true, fromCatalog: true, catalogId: "base-q5_1",
  installedAt: "2026-10-15T06:30:00Z", inUse: true,
};
const available = {
  catalogId: "base-q5_1", name: "base", version: "1", sizeBytes: 59707625, quantization: "q5_1",
  capabilities: ["multilingual"], minCpuCores: 2, minRamMb: 1024, recommended: false, likelySlow: false,
  installed: false, installedId: "", hasSourcePage: true, tier: "fast", englishOnly: false,
};
const noDownload = { catalogId: "", phase: "", doneBytes: 0, totalBytes: 0, running: false, error: "" };
const view = {
  installed: [installed], available: [available], needsDefault: false, speechReady: true, machineKnown: false,
  download: noDownload,
  modelsHomePage: "https://huggingface.co/ggerganov/whisper.cpp",
};
const selection = {
  cancelled: false, selectionId: "7", fileName: "ggml-small.bin", sizeBytes: 487601967, sha256: sum,
  suggestedName: "small", suggestedVersion: "1", suggestedQuantization: "",
};
const plan = {
  catalogId: "base-q5_1", totalBytes: 9, haveBytes: 0, requiredBytes: 9, availableBytes: 100, enough: true, warning: "",
};

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

test("every Model Manager operation calls its bound Go method with ids only", async () => {
  const cases = [
    ["list", [], view, "List", []],
    ["preflightDownload", ["base-q5_1"], plan, "PreflightDownload", ["base-q5_1"]],
    ["startDownload", ["base-q5_1"], null, "StartDownload", ["base-q5_1"]],
    ["cancelDownload", [], null, "CancelDownload", []],
    ["discardPartial", ["base-q5_1"], null, "DiscardPartial", ["base-q5_1"]],
    ["openSourcePage", ["base-q5_1"], null, "OpenSourcePage", ["base-q5_1"]],
    ["openModelsHomePage", [], null, "OpenModelsHomePage", []],
    ["chooseImportFile", [], selection, "ChooseImportFile", []],
    ["discardImport", ["7"], null, "DiscardImport", ["7"]],
    ["importCatalogFile", ["base-q5_1"], { cancelled: false, model: installed }, "ImportCatalogFile", ["base-q5_1"]],
    ["setDefault", ["m-1"], null, "SetDefault", ["m-1"]],
    ["delete", ["m-1"], null, "Delete", ["m-1"]],
  ];
  for (const [op, args, reply, method, sent] of cases) {
    const { call, calls } = fakeCall(reply);
    await modelsApi(call)[op](...args);
    assert.deepEqual(calls, [[prefix + method, ...sent]], op);
  }

  const c = { selectionId: "7", sha256: sum, name: "small", version: "1", quantization: "" };
  const { call, calls } = fakeCall(installed);
  assert.deepEqual(await modelsApi(call).confirmImport({ ...c, extra: "C:\\x.bin" }), installed);
  assert.deepEqual(calls, [[prefix + "ConfirmImport", c]], "only the five fields of the confirmation are sent");

  assert.equal(Object.keys(methods).length, 13);
  for (const name of Object.values(methods)) {
    assert.ok(name.startsWith(prefix), name);
  }
});

test("replies are parsed strictly", () => {
  assert.deepEqual(parseView(view), view);
  assert.deepEqual(parseView({ ...view, installed: null, available: null }).installed, []);
  assert.deepEqual(parseView({ ...view, installed: [{ ...installed, capabilities: null }] }).installed[0].capabilities, []);
  assert.deepEqual(parseSelection(selection), selection);
  assert.deepEqual(parseCatalogImport({ cancelled: true, model: null }), { cancelled: true, model: null });
  assert.deepEqual(parseCatalogImport({ cancelled: false, model: installed }).model, installed);

  for (const bad of [
    null, "x", { ...view, speechReady: "yes" }, { ...view, download: null }, { ...view, installed: [{ id: "m" }] },
    { ...view, available: [{ ...available, sizeBytes: -1 }] }, { ...view, available: [{ ...available, minRamMb: 1.5 }] },
    { ...view, installed: [{ ...installed, capabilities: [1] }] },
    { ...view, needsDefault: undefined }, { ...view, needsDefault: "true" },
    { ...view, installed: [{ ...installed, inUse: undefined }] }, { ...view, installed: [{ ...installed, inUse: 1 }] },
    { ...view, available: [{ ...available, tier: undefined }] }, { ...view, available: [{ ...available, tier: 1 }] },
    { ...view, available: [{ ...available, englishOnly: undefined }] },
    { ...view, available: [{ ...available, englishOnly: "false" }] },
  ]) {
    assert.throws(() => parseView(bad), undefined, JSON.stringify(bad));
  }
  assert.throws(() => parseSelection({ ...selection, sha256: null }));
  assert.throws(() => parseCatalogImport({ cancelled: false, model: null }), undefined, "an import without a model");
});

test("sizes are in binary units with a decimal comma, like the core", () => {
  assert.equal(sizeText(0), "0 Б");
  assert.equal(sizeText(1023), "1023 Б");
  assert.equal(sizeText(12 * 1024), "12 КБ");
  assert.equal(sizeText(300 * 1024 * 1024), "300 МБ");
  assert.equal(sizeText(487601967), "465 МБ");
  assert.equal(sizeText(1.5 * 1024 * 1024 * 1024), "1,5 ГБ");
  assert.equal(sizeText(1024 * 1024 * 1024), "1,0 ГБ");
});

test("download progress, status and buttons follow the core's state", () => {
  const running = { catalogId: "base-q5_1", phase: "downloading", doneBytes: 50 * 1024 * 1024, totalBytes: 200 * 1024 * 1024, running: true, error: "" };
  assert.equal(progressPercent(running), 25);
  assert.equal(progressPercent({ ...running, totalBytes: 0 }), 0);
  assert.equal(progressPercent({ ...running, doneBytes: running.totalBytes * 2 }), 100);
  assert.equal(downloadText(running), "Скачивается: 25% (50 МБ из 200 МБ)");
  assert.equal(downloadText({ ...running, phase: "verifying" }), "Проверяется SHA-256…");
  const failed = { ...running, phase: "failed", running: false, error: "Контрольная сумма SHA-256 не совпадает: файл повреждён или это другой файл. Модель не установлена" };
  assert.equal(downloadText(failed), failed.error, "the core's text is shown as is");
  assert.equal(downloadText(noDownload), "");

  assert.equal(downloadOf(available, running), running);
  assert.equal(downloadOf(available, { ...running, catalogId: "small" }), null);
  assert.equal(downloadOf(available, noDownload), null);

  assert.deepEqual(catalogActions(available, noDownload), {
    download: true, cancel: false, discardPartial: false, importFile: true, openSource: true,
  });
  assert.deepEqual(catalogActions(available, running), {
    download: false, cancel: true, discardPartial: false, importFile: false, openSource: true,
  });
  const other = catalogActions({ ...available, catalogId: "small" }, running);
  assert.equal(other.download, false, "one download at a time");
  assert.equal(other.cancel, false);
  assert.equal(other.importFile, true);
  assert.equal(catalogActions(available, failed).discardPartial, true);
  assert.equal(catalogActions(available, { ...failed, phase: "cancelled" }).discardPartial, true);
  assert.equal(catalogActions(available, { ...failed, phase: "installed" }).discardPartial, false);
  const done = catalogActions({ ...available, installed: true, installedId: "m-1", hasSourcePage: false }, noDownload);
  assert.equal(done.download, false);
  assert.equal(done.importFile, false);
  assert.equal(done.openSource, false);
});

test("the notes say why recordings are not transcribed", () => {
  const empty = { ...view, installed: [], available: [] };
  assert.deepEqual(statusNotes(empty).map((n) => n.text), [texts.noModels, texts.afterImport]);
  assert.deepEqual(statusNotes({ ...empty, speechReady: false }).map((n) => n.text), [texts.speechMissing, texts.noModels]);
  assert.match(texts.speechMissing, /whisper-cli/);
  assert.match(texts.speechMissing, /перезапустите/);
  assert.deepEqual(statusNotes(view), []);
  const none = { ...installed, isDefault: false, inUse: false };
  const two = { ...view, installed: [none, { ...none, id: "m-2" }], needsDefault: true };
  assert.deepEqual(statusNotes(two).map((n) => n.text), [texts.needDefault]);
  assert.match(texts.machineUnknown, /не определено/);

  assert.equal(defaultBadge(installed), "По умолчанию");
  assert.equal(defaultBadge({ ...installed, inUse: false }), "По умолчанию");
  assert.equal(defaultBadge({ ...installed, isDefault: false }), "Используется");
  assert.equal(defaultBadge(none), "");
  assert.equal(modelTitle(installed), "base 1, q5_1");
  assert.equal(modelTitle({ ...installed, quantization: "" }), "base 1");
  assert.equal(shortSha(sum), "aaaaaaaaaaaa…");
});

test("the badge and the default note follow the core's flags, not the count of models (ARCH-44)", () => {
  const none = { ...installed, isDefault: false, inUse: false };
  // Several models and no default, but the core does not ask for one: no note.
  assert.deepEqual(statusNotes({ ...view, installed: [none, { ...none, id: "m-2" }], needsDefault: false }), []);
  // One model, yet the core asks for a default: the note is shown.
  assert.deepEqual(statusNotes({ ...view, installed: [none], needsDefault: true }).map((n) => n.text), [texts.needDefault]);
  // The only model is not in use: no badge; one of several is in use: it has the badge.
  assert.equal(defaultBadge(none), "");
  const several = [none, { ...none, id: "m-2", inUse: true }, { ...none, id: "m-3" }];
  assert.deepEqual(several.map((m) => defaultBadge(m)), ["", "Используется", ""]);
});

test("the import confirms exactly the SHA-256 the page showed", () => {
  const s = confirmStep(selection);
  assert.deepEqual([s.name, s.version, s.quantization], ["small", "1", ""]);
  assert.equal(canConfirm(s), true);
  assert.deepEqual(confirmation({ ...s, name: " small ", quantization: " q5_0 " }), {
    selectionId: "7", sha256: sum, name: "small", version: "1", quantization: "q5_0",
  });
  assert.equal(confirmation(s).sha256, selection.sha256);

  assert.equal(canConfirm({ step: "idle" }), false);
  assert.equal(canConfirm({ step: "choosing" }), false);
  assert.equal(canConfirm({ step: "installing", selection }), false);
  assert.equal(canConfirm({ ...s, name: "  " }), false, "no name");
  assert.equal(canConfirm({ ...s, version: "" }), false, "no version");
  assert.equal(canConfirm(confirmStep({ ...selection, selectionId: "" })), false, "no selection");
  assert.equal(canConfirm(confirmStep({ ...selection, sha256: "abc" })), false, "no full SHA-256");
  assert.equal(canConfirm(confirmStep({ ...selection, cancelled: true })), false, "a cancelled dialog");
});

test("errors of the core are shown as is, other errors as a generic text", () => {
  const space = "Недостаточно места на диске: нужно 1,5 ГБ, свободно 300 МБ";
  assert.equal(errorText(new Error(space)), space);
  assert.equal(errorText({ message: "Модель с таким названием и версией уже установлена" }), "Модель с таким названием и версией уже установлена");
  const generic = "Не удалось выполнить действие с моделью";
  assert.equal(errorText(new Error(`${prefix}Delete: panic: open C:\\Users\\Иван\\x`)), generic);
  assert.equal(errorText(new Error("could not parse argument #0")), generic);
  assert.equal(errorText(undefined), generic);

  for (const msg of identityErrors) {
    assert.equal(keepsSelection(new Error(msg)), true, msg);
  }
  assert.equal(keepsSelection(new Error("Подтверждённая SHA-256 не совпадает с показанной: выберите файл заново")), false);
});

test("the texts the page matches are the core's texts", () => {
  const go = readFileSync(new URL("../../internal/input/wails/models.go", import.meta.url), "utf8");
  for (const msg of identityErrors) {
    assert.ok(go.includes(`"${msg}"`), `models.go has no error ${msg}`);
  }
  assert.ok(go.includes(`= "${view.modelsHomePage}"`), "the models home page");
});

test("file and catalog text is plain text, isolated in <bdi>", () => {
  const page = readFileSync(new URL("../src/pages/ModelsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /<bdi>\{step\.selection\.fileName\}<\/bdi>/);
  assert.match(page, /<code className="sha">\s*<bdi>\{step\.selection\.sha256\}<\/bdi>/);
  assert.match(page, /<bdi>\{modelTitle\(m\)\}<\/bdi>/);
  assert.match(page, /SHA-256 совпадает, установить/);
  for (const file of ["../src/pages/ModelsPage.tsx", "../src/modelsView.ts", "../src/bindings/models.ts"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "outerHTML", "insertAdjacentHTML", "href=", "window.open"]) {
      assert.ok(!src.includes(banned), `${file} uses ${banned}`);
    }
  }
});

test("a download phase has its chip; none without a download or once installed", () => {
  const d = { catalogId: "base-q5_1", phase: "downloading", doneBytes: 1, totalBytes: 2, running: true, error: "" };
  assert.deepEqual(downloadChip(d), { kind: "run", icon: "down", label: "Скачивается" });
  assert.equal(downloadChip({ ...d, phase: "verifying" }).label, "Проверяется");
  assert.equal(downloadChip({ ...d, phase: "failed", running: false }).kind, "fail");
  assert.equal(downloadChip({ ...d, phase: "cancelled", running: false }).kind, "neutral");
  assert.equal(downloadChip({ ...d, phase: "installed", running: false }), null);
  assert.equal(downloadChip(noDownload), null);
});

test("catalog entries get Russian labels and a qualitative CPU speed", () => {
  const mb = 1024 * 1024;
  const at = (over) => catalogLabels({ ...available, ...over });

  assert.deepEqual(at({ tier: "balanced", recommended: true, sizeBytes: 190 * mb }), {
    label: "Рекомендуется для русского", speed: "Скорость на CPU: средняя", warning: "",
  });
  assert.deepEqual(at({ tier: "fast" }), { label: "Быстрее, но менее точная", speed: "Скорость на CPU: высокая", warning: "" });
  assert.deepEqual(at({ tier: "accurate" }), { label: "Точнее, но медленнее", speed: "Скорость на CPU: низкая", warning: "" });
  assert.deepEqual(at({ tier: "balanced" }), { label: "", speed: "Скорость на CPU: средняя", warning: "" });

  const max = at({ tier: "max", sizeBytes: 574 * mb });
  assert.equal(max.label, "Самая точная, но очень медленная на обычном компьютере");
  assert.equal(max.speed, "Скорость на CPU: очень низкая");
  assert.match(max.warning, /574 МБ/, "the warning names the size");
  assert.match(max.warning, /CPU/, "and the CPU speed");
  assert.notEqual(at({ tier: "max", recommended: true }).warning, "", "a recommended max model still warns");

  // English-only wins over the tier and over recommended.
  for (const tier of ["fast", "balanced", "accurate", "max", ""]) {
    assert.equal(at({ tier, englishOnly: true }).label, "Только английский", tier);
    assert.equal(at({ tier, englishOnly: true, recommended: true }).label, "Только английский", tier);
  }
  assert.equal(at({ tier: "balanced", englishOnly: true }).speed, "Скорость на CPU: средняя");

  // No tier or an unknown one: no label and no speed line.
  assert.deepEqual(at({ tier: "" }), { label: "", speed: "", warning: "" });
  assert.deepEqual(at({ tier: "toString" }), { label: "", speed: "", warning: "" });
  assert.equal(at({ tier: "", recommended: true }).label, "Рекомендуется для русского");
});
