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
  catalogGroups,
  accuracyText,
  catalogAction,
  catalogEntryOf,
  catalogMenu,
  catalogRow,
  displayName,
  installedMenu,
  installedName,
  installedRow,
  installedText,
  loadHint,
  loadLevels,
  menuKey,
  menuShown,
  progressText,
  reducedSuffix,
  pinnedByDownload,
  languageFilter,
  languageOptions,
  loadFilter,
  loadOptions,
  loadText,
  loadWords,
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
  installedAt: "2026-10-15T06:30:00Z", inUse: true, estimates: null,
};
const available = {
  catalogId: "base-q5_1", name: "base", version: "1", sizeBytes: 59707625, quantization: "q5_1",
  capabilities: ["multilingual"], minCpuCores: 2, minRamMb: 1024, recommended: false, likelySlow: false,
  installed: false, installedId: "", hasSourcePage: true, tier: "fast", englishOnly: false, family: "base", load: 1,
  heavy: false, reduced: true, estimates: null,
};
const noDownload = { catalogId: "", phase: "", doneBytes: 0, totalBytes: 0, running: false, error: "" };
const view = {
  installed: [installed], available: [available], needsDefault: false, speechReady: true, machineKnown: false,
  download: noDownload,
  modelsHomePage: "https://huggingface.co/ggerganov/whisper.cpp", recognitionLoad: "",
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
    { ...view, available: [{ ...available, family: undefined }] }, { ...view, available: [{ ...available, family: 1 }] },
    { ...view, available: [{ ...available, load: undefined }] }, { ...view, available: [{ ...available, load: 6 }] },
    { ...view, available: [{ ...available, load: -1 }] }, { ...view, available: [{ ...available, load: 2.5 }] },
    { ...view, available: [{ ...available, heavy: undefined }] }, { ...view, available: [{ ...available, heavy: "true" }] },
    { ...view, available: [{ ...available, reduced: undefined }] }, { ...view, available: [{ ...available, reduced: 1 }] },
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
  // A missing speech component has its own notice with the install button (SpeechComponentNotice).
  assert.deepEqual(statusNotes({ ...empty, speechReady: false }).map((n) => n.text), [texts.noModels]);
  for (const t of Object.values(texts)) {
    assert.doesNotMatch(t, /whisper-cli|\.exe|DLL|learning-assistant/);
  }
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
  assert.match(page, /<bdi className="mname-text">\{r\.name\}<\/bdi>/);
  assert.match(page, /<bdi className="breakable">\{installedName\(deletingModel, view\?\.available \?\? \[\]\)\}<\/bdi>/);
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

test("a catalog row is named by its family; an unknown family shows the catalog id", () => {
  const names = {
    tiny: "Tiny", base: "Base", small: "Small", medium: "Medium", "large-v3": "Large v3", "large-v3-turbo": "Large v3 Turbo",
  };
  for (const [family, name] of Object.entries(names)) {
    assert.equal(displayName({ family, catalogId: "x" }), name, family);
    assert.equal(catalogRow({ ...available, family }).name, name, family);
  }
  assert.equal(displayName({ family: "", catalogId: "base-q5_1" }), "base-q5_1");
  assert.equal(displayName({ family: "huge", catalogId: "huge-q5_0" }), "huge-q5_0");
  assert.equal(displayName({ family: "toString", catalogId: "t" }), "t", "no prototype keys");

  // Every family of the embedded catalog has a name and an accuracy.
  const catalog = JSON.parse(readFileSync(new URL("../../internal/app/catalog.json", import.meta.url), "utf8"));
  assert.equal(catalog.models.length, 20);
  for (const e of catalog.models) {
    const r = catalogRow({ ...available, catalogId: e.id, family: e.family, quantization: e.quantization });
    assert.ok(Object.values(names).includes(r.name), e.id);
    assert.match(accuracyText(e.family), /^точность /, e.id);
    assert.ok(r.details.includes(accuracyText(e.family)), e.id);
  }
});

test("the load is a 5-step scale with «Нагрузка N из 5»; the warning follows the core's heavy flag", () => {
  assert.equal(loadLevels, 5);
  assert.deepEqual(loadWords, ["", "лёгкая", "умеренная", "средняя", "тяжёлая", "очень тяжёлая"]);
  assert.equal(loadText(0), "");
  for (const n of [1, 2, 3, 4, 5]) {
    assert.equal(loadText(n), `Нагрузка ${n} из 5`);
    assert.equal(loadHint(n), `Нагрузка на компьютер: ${n} из 5 — ${loadWords[n]}`);
  }
  assert.equal(loadText(6), "");
  assert.equal(loadHint(0), "");

  assert.equal(texts.heavy, "Тяжёлая модель: распознавание будет долгим");
  for (const load of [0, 1, 2, 3, 4, 5]) {
    assert.equal(catalogRow({ ...available, load }).load, load);
  }
  assert.equal(catalogRow({ ...available, load: 7 }).load, 0, "an unknown load has no scale");
  // The page does not repeat the core's rule: only the flag (load 4–5 in the core) decides the warning.
  assert.equal(catalogRow({ ...available, load: 3, heavy: false }).warning, "");
  assert.equal(catalogRow({ ...available, load: 4, heavy: true }).warning, texts.heavy);
  assert.equal(catalogRow({ ...available, load: 5, heavy: true }).warning, texts.heavy);
  assert.equal(catalogRow({ ...available, load: 5, heavy: true }).heavy, true);
  // The core's «below the recommended hardware» hint is a part of the muted line, never a second warning line.
  assert.equal(catalogRow({ ...available, likelySlow: true }).slow, texts.likelySlow);
  assert.equal(catalogRow({ ...available, likelySlow: true }).warning, "");
  assert.equal(catalogRow({ ...available, likelySlow: true, heavy: true, load: 5 }).slow, "", "one warning only");
});

test("a catalog row: «Q5» in the name, only two tags, one muted line, the id only in the tooltip", () => {
  const at = (over) => catalogRow({ ...available, ...over });
  // The variant follows the core's reduced flag, not the quantization text.
  assert.equal(at({ reduced: true, quantization: "q5_1" }).suffix, "Q5");
  assert.equal(at({ reduced: true, quantization: "q5_0" }).suffix, "Q5");
  assert.equal(at({ reduced: false, quantization: "q5_0" }).suffix, "");
  assert.equal(at({ reduced: false, quantization: "f16" }).suffix, "");
  assert.equal(reducedSuffix("q8_0"), "Q8");
  assert.equal(texts.q5Hint, "уменьшенная: меньше и быстрее, точность чуть ниже");

  // Only «Рекомендуется для русского» and «Только английский» next to the name.
  assert.deepEqual(at({ recommended: true }).tags, ["Рекомендуется для русского"]);
  assert.deepEqual(at({ recommended: false }).tags, []);
  assert.deepEqual(at({ englishOnly: true }).tags, ["Только английский"]);
  assert.deepEqual(at({ recommended: true, englishOnly: true }).tags, ["Только английский"], "never recommended in English");
  for (const tier of ["fast", "balanced", "accurate", "max", ""]) {
    for (const t of at({ tier, recommended: true }).tags) {
      assert.ok(["Рекомендуется для русского", "Только английский"].includes(t), `${tier}: ${t}`);
    }
  }

  // The muted line: accuracy and language; an English-only model has its language as the tag.
  assert.deepEqual(at({ family: "small" }).details, ["точность хорошая", "русский и др. языки"]);
  assert.deepEqual(at({ family: "small", englishOnly: true }).details, ["точность хорошая"]);
  const accuracy = {
    tiny: "низкая", base: "средняя", small: "хорошая", medium: "высокая", "large-v3-turbo": "высокая",
    "large-v3": "самая высокая",
  };
  for (const [family, word] of Object.entries(accuracy)) {
    assert.equal(accuracyText(family), `точность ${word}`, family);
  }
  assert.equal(accuracyText(""), "");
  assert.deepEqual(at({ family: "" }).details, ["русский и др. языки"]);

  // No «Полная»/«Уменьшенная» and no quantization or id in the visible text; the id is the tooltip.
  for (const [catalogId, quantization, reduced] of [["base-q5_1", "q5_1", true], ["medium-q5_0", "q5_0", true], ["small-f16", "f16", false]]) {
    for (const over of [{}, { englishOnly: true }, { recommended: true }, { heavy: true, load: 5 }, { likelySlow: true }]) {
      const r = at({ catalogId, name: catalogId, quantization, reduced, family: catalogId.split("-")[0], ...over });
      const shown = [r.name, r.suffix, r.size, ...r.tags, loadText(r.load), ...r.details, r.slow, r.warning].join(" ");
      assert.doesNotMatch(shown, /Полная|Уменьшенная|Быстрее, но|Точнее, но/i, shown);
      assert.ok(!shown.includes(quantization), `${catalogId}: ${shown}`);
      assert.ok(!shown.includes(catalogId), `${catalogId}: ${shown}`);
      assert.equal(r.tip, catalogId);
    }
  }
  for (const t of Object.values(texts)) {
    assert.doesNotMatch(t, /Полная|Уменьшенная/, t);
  }
});

test("one action per catalog row: «Скачать», «Отменить» while its download runs or «Удалить» once installed", () => {
  const running = { catalogId: "base-q5_1", phase: "downloading", doneBytes: 1, totalBytes: 2, running: true, error: "" };
  const failed = { ...running, phase: "failed", running: false };
  assert.equal(catalogAction(available, noDownload), "download");
  assert.equal(catalogAction(available, running), "cancel");
  assert.equal(catalogAction(available, { ...running, phase: "verifying" }), "cancel");
  assert.equal(catalogAction(available, failed), "download", "a stopped download resumes with «Скачать»");
  assert.equal(catalogAction({ ...available, installed: true, installedId: "m-1" }, noDownload), "delete");
  assert.equal(catalogAction({ ...available, catalogId: "small" }, running), "download", "another model");
  assert.equal(catalogActions({ ...available, catalogId: "small" }, running).download, false, "shown disabled");
  assert.match(texts.oneDownload, /только одна/);

  assert.equal(progressText(running), "50% · 1 Б из 2 Б");
  assert.equal(progressText({ ...running, totalBytes: 0 }), downloadText({ ...running, totalBytes: 0 }));
  assert.equal(progressText(failed), downloadText(failed));
});

test("the «⋯» menu has the secondary actions; Escape closes it, arrows move, Tab leaves", () => {
  const ops = (items) => items.map((i) => i.op);
  const labels = (items) => items.map((i) => i.label);
  assert.deepEqual(labels(catalogMenu(available, noDownload)), ["Открыть страницу источника", "Импортировать скачанный файл…"]);
  const running = { catalogId: "base-q5_1", phase: "downloading", doneBytes: 1, totalBytes: 2, running: true, error: "" };
  assert.deepEqual(ops(catalogMenu(available, running)), ["source"]);
  const failed = { ...running, phase: "failed", running: false };
  assert.deepEqual(labels(catalogMenu(available, failed)), [
    "Открыть страницу источника", "Удалить недокачанный файл", "Импортировать скачанный файл…",
  ]);
  assert.deepEqual(ops(catalogMenu({ ...available, installed: true, installedId: "m-1", hasSourcePage: false }, noDownload)), []);

  const none = { ...installed, isDefault: false, inUse: false };
  assert.deepEqual(labels(installedMenu(none, null)), ["Сделать по умолчанию"]);
  assert.deepEqual(ops(installedMenu(installed, null)), []);
  assert.deepEqual(ops(installedMenu(none, available)), ["default", "source"]);
  assert.deepEqual(ops(installedMenu(installed, { ...available, hasSourcePage: false })), []);

  assert.equal(menuKey("Escape", 1, 3), "close");
  assert.equal(menuKey("Escape", -1, 0), "close");
  assert.equal(menuKey("Tab", 0, 3), "leave");
  assert.equal(menuKey("ArrowDown", -1, 3), 0);
  assert.equal(menuKey("ArrowDown", 0, 3), 1);
  assert.equal(menuKey("ArrowDown", 2, 3), 0);
  assert.equal(menuKey("ArrowUp", -1, 3), 2);
  assert.equal(menuKey("ArrowUp", 0, 3), 2);
  assert.equal(menuKey("ArrowUp", 2, 3), 1);
  assert.equal(menuKey("Home", 2, 3), 0);
  assert.equal(menuKey("End", 0, 3), 2);
  assert.equal(menuKey("ArrowDown", -1, 0), null);
  assert.equal(menuKey("a", 0, 3), null);
  assert.equal(menuKey("Enter", 0, 3), null, "Enter clicks the focused item");
});

test("the «⋯» menu is shown only while open and with items, so it never comes back open by itself", () => {
  assert.equal(menuShown(true, 2), true);
  assert.equal(menuShown(false, 2), false);
  assert.equal(menuShown(true, 0), false, "the items went away while it was open");
  assert.equal(menuShown(false, 0), false);
});

test("a disabled «Скачать» says why to a screen reader too, without visible text", () => {
  const page = readFileSync(new URL("../src/pages/ModelsPage.tsx", import.meta.url), "utf8");
  const row = page.slice(page.indexOf("function CatalogRow("));
  assert.match(row, /aria-describedby=\{a\.download \? undefined : reasonId\}/);
  assert.match(row, /<span id=\{reasonId\} className="vh">\s*\{texts\.oneDownload\}\s*<\/span>/);
  assert.ok(!row.includes("disabledReason"), "no visible reason line under the button");
});

test("an installed model has the catalog row format; an imported one its own name, no load scale", () => {
  const catalog = [
    { ...available, catalogId: "small-q5_1", family: "small", quantization: "q5_1", reduced: true, load: 2, recommended: true },
    { ...available, catalogId: "large-v3-f16", family: "large-v3", quantization: "f16", reduced: false, load: 5, heavy: true, likelySlow: true },
  ];
  const small = { ...installed, name: "small-q5_1", catalogId: "small-q5_1", quantization: "q5_1", sizeBytes: 190 * 1024 * 1024 };
  assert.equal(catalogEntryOf(small, catalog), catalog[0]);
  const r = installedRow(small, catalog);
  assert.deepEqual([r.name, r.suffix, r.size, r.load], ["Small", "Q5", "190 МБ", 2]);
  assert.deepEqual(r.tags, ["Рекомендуется для русского"]);
  assert.deepEqual(r.details, ["точность хорошая", "русский и др. языки"]);
  assert.match(r.tip, /^small-q5_1\nSHA-256: a{64}$/);
  assert.equal(installedName(small, catalog), "Small Q5", "no quantization repeated in the title");
  assert.equal(installedText(small, catalog), "Модель установлена: Small Q5.");

  const large = { ...installed, name: "large-v3-f16", catalogId: "large-v3-f16", quantization: "f16" };
  const l = installedRow(large, catalog);
  assert.deepEqual([l.name, l.suffix, l.warning, l.slow, l.heavy], ["Large v3", "", "", "", true], "no warnings once installed");
  assert.equal(installedName(large, catalog), "Large v3");

  // Imported, or from a catalog entry that is gone: the name, the version and the quantization, no fake scale.
  const own = { ...installed, fromCatalog: false, catalogId: "", name: "my-model", version: "2", quantization: "q5_0", capabilities: [] };
  const o = installedRow(own, catalog);
  assert.deepEqual([o.name, o.suffix, o.load, o.tags.length, o.warning], ["my-model", "", 0, 0, ""]);
  assert.deepEqual(o.details, ["версия 2", "квантизация q5_0"]);
  assert.deepEqual(installedRow({ ...own, quantization: "", capabilities: ["multilingual"] }, catalog).details, [
    "версия 2", "русский и др. языки",
  ]);
  assert.equal(installedName(own, catalog), "my-model 2, q5_0");
  assert.equal(installedText(own), "Модель установлена: my-model 2, q5_0.");
  const gone = { ...small, catalogId: "small-old" };
  assert.equal(catalogEntryOf(gone, catalog), null);
  assert.equal(installedRow(gone, catalog).name, "small-q5_1");
  assert.equal(catalogEntryOf({ ...small, fromCatalog: false }, catalog), null);
});

test("the page: one action button, the «⋯» menu, the id only as a tooltip, the import above the lists", () => {
  const page = readFileSync(new URL("../src/pages/ModelsPage.tsx", import.meta.url), "utf8");
  const importAt = page.indexOf("Импортировать файл модели…");
  assert.ok(importAt > 0);
  assert.equal(page.split("Импортировать файл модели…").length, 2, "one import button");
  assert.ok(importAt < page.indexOf("<CatalogList"), "above the catalog");
  assert.ok(importAt < page.indexOf("<CollapsibleList"), "above the installed models");

  const row = page.slice(page.indexOf("function CatalogRow("));
  for (const gone of ["Остановить скачивание", "Открыть страницу источника", "Импортировать скачанный файл", "Удалить недокачанный файл", "catalogLabels", "texts.likelySlow"]) {
    assert.ok(!row.includes(gone), `the catalog row has no ${gone}`);
  }
  assert.doesNotMatch(page, /<bdi>\{m\.catalogId\}|>\{m\.catalogId\}</, "the id is never visible text");
  assert.ok(!page.includes("modelTitle("), "titles come from the row text");
  assert.match(page, /<p className="t mname" title=\{props\.tip\}>/, "the id is the tooltip of the name");
  assert.match(page, /<span className="mq" title=\{texts\.q5Hint\}>/);
  assert.match(page, /<span className="vh">, \{texts\.q5Hint\}<\/span>/, "the hint for screen readers too");
  assert.match(page, /aria-haspopup="menu"/);
  assert.match(page, /aria-expanded=\{shown\}/);
  assert.match(page, /const shown = menuShown\(open, count\);/);
  assert.match(page, /if \(count === 0\) \{\s*setOpen\(false\);\s*\}\s*\}, \[count\]\);/, "a menu whose items went away closes");
  assert.match(page, /\{shown && \(\s*<div className="mmenu-list"/, "the list only while shown");
  assert.match(page, /if \(!shown\) \{\s*return;\s*\}[^]*?\}, \[shown\]\);/, "the outside listener only while shown");
  assert.match(page, /aria-label=\{label\}/);
  assert.match(page, /role="menuitem"/);
  assert.match(page, /menuKey\(e\.key,/);
  assert.match(page, /next === "close"\) \{\s*setOpen\(false\);\s*button\.current\?\.focus\(\);/, "Escape returns the focus");
  assert.match(page, /addEventListener\("pointerdown", outside\)/, "a click outside closes the menu");
  for (const r of ["function CatalogRow(", "function InstalledRow("]) {
    const body = page.slice(page.indexOf(r));
    const end = body.indexOf("\n}\n");
    const buttons = body.slice(0, end).match(/<Button\b/g) ?? [];
    assert.ok(buttons.length >= 1, r);
    assert.ok(body.slice(0, end).includes("<RowMenu"), `${r} has the «⋯» menu`);
  }
  const installedBody = page.slice(page.indexOf("function InstalledRow("));
  assert.equal(installedBody.slice(0, installedBody.indexOf("\n}\n")).match(/<Button\b/g).length, 1, "only «Удалить»");
});

test("the catalog filters by language and load and groups by language", () => {
  const m = (catalogId, load, englishOnly = false) => ({ ...available, catalogId, load, englishOnly });
  const list = [
    m("tiny-q5_1", 1), m("small-f16", 3), m("large-v3-f16", 5), m("x", 0), m("tiny.en-q5_1", 1, true),
    m("medium.en-f16", 4, true),
  ];
  const ids = (groups) => groups.map((g) => [g.title, g.models.map((x) => x.catalogId)]);
  const multi = "Русский и другие языки";
  const en = "Только английский";

  assert.deepEqual(ids(catalogGroups(list, "all", "any", noDownload)), [
    [multi, ["tiny-q5_1", "small-f16", "large-v3-f16", "x"]],
    [en, ["tiny.en-q5_1", "medium.en-f16"]],
  ]);
  assert.deepEqual(ids(catalogGroups(list, "multilingual", "any", noDownload)), [["", ["tiny-q5_1", "small-f16", "large-v3-f16", "x"]]]);
  assert.deepEqual(ids(catalogGroups(list, "english", "any", noDownload)), [["", ["tiny.en-q5_1", "medium.en-f16"]]]);
  assert.deepEqual(ids(catalogGroups(list, "all", "2", noDownload)), [[multi, ["tiny-q5_1"]], [en, ["tiny.en-q5_1"]]]);
  assert.deepEqual(ids(catalogGroups(list, "all", "3", noDownload)), [[multi, ["tiny-q5_1", "small-f16"]], [en, ["tiny.en-q5_1"]]]);
  assert.deepEqual(ids(catalogGroups(list, "all", "4", noDownload)), [
    [multi, ["tiny-q5_1", "small-f16"]], [en, ["tiny.en-q5_1", "medium.en-f16"]],
  ]);
  assert.deepEqual(ids(catalogGroups(list, "english", "3", noDownload)), [["", ["tiny.en-q5_1"]]]);
  assert.deepEqual(ids(catalogGroups([m("tiny-q5_1", 1)], "all", "any", noDownload)), [[multi, ["tiny-q5_1"]]], "an empty group is dropped");
  assert.deepEqual(catalogGroups([m("large-v3-f16", 5)], "all", "2", noDownload), [], "nothing matches");
  assert.deepEqual(catalogGroups([], "all", "any", noDownload), []);

  // The row of the running download or of a partial to discard stays visible whatever the filters.
  const heavyEn = m("medium.en-f16", 4, true);
  const running = { catalogId: "medium.en-f16", phase: "downloading", doneBytes: 1, totalBytes: 2, running: true, error: "" };
  const failed = { ...running, phase: "failed", running: false };
  assert.equal(pinnedByDownload(heavyEn, running), true);
  assert.equal(pinnedByDownload(heavyEn, { ...running, phase: "verifying" }), true);
  assert.equal(pinnedByDownload(heavyEn, failed), true);
  assert.equal(pinnedByDownload(heavyEn, { ...failed, phase: "cancelled" }), true);
  assert.equal(pinnedByDownload(heavyEn, { ...failed, phase: "installed" }), false);
  assert.equal(pinnedByDownload(heavyEn, noDownload), false);
  assert.equal(pinnedByDownload(m("tiny-q5_1", 1), running), false, "another model's download");
  assert.deepEqual(ids(catalogGroups(list, "all", "2", running)), [[multi, ["tiny-q5_1"]], [en, ["tiny.en-q5_1", "medium.en-f16"]]]);
  assert.deepEqual(ids(catalogGroups(list, "english", "3", failed)), [["", ["tiny.en-q5_1", "medium.en-f16"]]]);
  assert.deepEqual(ids(catalogGroups(list, "multilingual", "2", running)), [
    [multi, ["tiny-q5_1"]], [en, ["medium.en-f16"]],
  ], "a pinned row of the other language keeps its heading");
  assert.deepEqual(ids(catalogGroups([heavyEn], "multilingual", "any", running)), [[en, ["medium.en-f16"]]]);

  assert.deepEqual(languageOptions.map((o) => o.value), ["all", "multilingual", "english"]);
  assert.deepEqual(loadOptions.map((o) => o.value), ["any", "2", "3", "4"]);
  assert.equal(languageFilter("english"), "english");
  assert.equal(languageFilter("bogus"), "all");
  assert.equal(loadFilter("3"), "3");
  assert.equal(loadFilter("5"), "any");
  assert.match(texts.noMatch, /фильтр/);
});

test("the load scale is not colour only and the filters are native selects", () => {
  const page = readFileSync(new URL("../src/pages/ModelsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /role="img" aria-label=\{hint\} title=\{hint\}/);
  assert.match(page, /useState<LanguageFilter>\("all"\)/, "every language by default");
  assert.match(page, /useState<LoadFilter>\("any"\)/, "any load by default");
  assert.match(page, /<Select id=\{id\} value=\{lang\}/);
  assert.match(page, /<Select id=\{id\} value=\{load\}/);
  assert.ok(!page.includes("style={{"), "no inline styles");
  assert.match(page, /<LoadScale load=\{r\.load\} heavy=\{r\.heavy\}/, "the heavy colour comes from the core's flag");
  assert.match(page, /<span className="msize muted num">\{r\.size\}<\/span>/, "the size is as large as the name (AC6)");
  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  assert.match(css, /\.mname-text \{[^}]*text-overflow: ellipsis/, "a long name ends with an ellipsis");
  assert.match(css, /\.mrows \{[^}]*grid-template-columns: minmax\(0, 1fr\) fit-content\(200px\) /, "aligned columns, a narrow status, the name shrinks first");
  assert.match(css, /@container app \(max-width: 880px\) \{\s*\.mrows \{ grid-template-columns: minmax\(0, 1fr\) max-content max-content; \}\s*\.mrow > \.row-main \{ grid-column: 1 \/ -1; \}/,
    "at 800 px the model spans the row, the status and the actions go below in shared columns");
  assert.match(css, /\.mline > \* \{[^}]*white-space: nowrap/, "a part of the muted line never breaks inside");
  assert.match(css, /\.mline > \* \+ \*::before \{ content: "·" \/ "";/, "the dots are not read aloud");
  assert.match(page, /catalogGroups\(v\.available, lang, load, v\.download\)/, "the download row is never filtered out");
});

test("a model row has four cells: text, status (nowrap), button, «⋯» in its own content-sized column", () => {
  const page = readFileSync(new URL("../src/pages/ModelsPage.tsx", import.meta.url), "utf8");
  for (const fn of ["function InstalledRow(", "function CatalogRow("]) {
    const start = page.indexOf(fn);
    assert.ok(start > 0, fn);
    const body = page.slice(start, page.indexOf("\n}\n", start));
    const row = body.slice(body.indexOf('<div className="mrow" role="listitem">'));
    // The direct children of the row, in order: each one is a column of the .mrows grid.
    const cells = [...row.matchAll(/^ {6}<(\w+)(?: className="([^"]*)")?/gm)].map((m) => `${m[1]}${m[2] ? "." + m[2] : ""}`);
    assert.deepEqual(cells, ["ModelText", "div.st mst", "div.acts", "RowMenu"], fn);
  }
  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  const tracks = [...css.matchAll(/\.mrows \{[^}]*grid-template-columns: ([^;]+);/g)].map((m) => m[1]);
  assert.deepEqual(tracks, [
    "minmax(0, 1fr) fit-content(200px) max-content max-content",
    "minmax(0, 1fr) max-content max-content",
  ]);
  assert.match(css, /\.mrow \.mst, \.mrow \.mst \.chip-text \{ white-space: nowrap; \}/);
  assert.match(css, /\.mrow > \.acts > \.btn \{ flex: 1 0 auto; min-width: 120px; \}/);
  assert.ok(!/\.mmenu[^{]*\{[^}]*position: absolute/.test(css.replace(/\.mmenu-list \{[^}]*\}/, "")), "«⋯» is not an overlay");
});
