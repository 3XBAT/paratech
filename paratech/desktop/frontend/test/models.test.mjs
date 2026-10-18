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
  catalogLabels,
  displayName,
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
  installed: false, installedId: "", hasSourcePage: true, tier: "fast", englishOnly: false, family: "base", load: 1,
  heavy: false, reduced: true,
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

test("a catalog row is named by its family; an unknown family shows the catalog id", () => {
  const names = {
    tiny: "Tiny", base: "Base", small: "Small", medium: "Medium", "large-v3": "Large v3", "large-v3-turbo": "Large v3 Turbo",
  };
  for (const [family, name] of Object.entries(names)) {
    assert.equal(displayName({ family, catalogId: "x" }), name, family);
    assert.equal(catalogLabels({ ...available, family }).name, name, family);
  }
  assert.equal(displayName({ family: "", catalogId: "base-q5_1" }), "base-q5_1");
  assert.equal(displayName({ family: "huge", catalogId: "huge-q5_0" }), "huge-q5_0");
  assert.equal(displayName({ family: "toString", catalogId: "t" }), "t", "no prototype keys");

  // Every family of the embedded catalog has a name and an accuracy.
  const catalog = JSON.parse(readFileSync(new URL("../../internal/app/catalog.json", import.meta.url), "utf8"));
  assert.equal(catalog.models.length, 20);
  for (const e of catalog.models) {
    const l = catalogLabels({ ...available, catalogId: e.id, family: e.family, quantization: e.quantization });
    assert.ok(Object.values(names).includes(l.name), e.id);
    assert.match(l.accuracy, /^Точность: /, e.id);
  }
});

test("the load is a 5-step scale with words; the warning follows the core's heavy flag", () => {
  assert.deepEqual(loadWords, ["", "лёгкая", "умеренная", "средняя", "тяжёлая", "очень тяжёлая"]);
  assert.equal(loadText(0), "");
  assert.equal(loadText(1), "Нагрузка на компьютер: 1 из 5 — лёгкая");
  assert.equal(loadText(2), "Нагрузка на компьютер: 2 из 5 — умеренная");
  assert.equal(loadText(3), "Нагрузка на компьютер: 3 из 5 — средняя");
  assert.equal(loadText(4), "Нагрузка на компьютер: 4 из 5 — тяжёлая");
  assert.equal(loadText(5), "Нагрузка на компьютер: 5 из 5 — очень тяжёлая");
  assert.equal(loadText(6), "");

  const heavy = "Модель требовательна к ресурсам вашего устройства: распознавание будет долгим на обычном процессоре";
  assert.equal(texts.heavy, heavy);
  for (const load of [0, 1, 2, 3, 4, 5]) {
    assert.equal(catalogLabels({ ...available, load }).load, loadText(load));
  }
  // The page does not repeat the core's rule: only the flag decides the warning, whatever the load.
  assert.equal(catalogLabels({ ...available, load: 5, heavy: false }).warning, "");
  assert.equal(catalogLabels({ ...available, load: 1, heavy: true }).warning, heavy);
});

test("language, variant, accuracy and the recommended label", () => {
  const at = (over) => catalogLabels({ ...available, ...over });
  assert.equal(at({}).language, "Русский и другие языки");
  assert.equal(at({ englishOnly: true }).language, "Только английский");

  // The variant follows the core's reduced flag, not the quantization text.
  assert.equal(at({ reduced: true }).variant, "Уменьшенная — меньше и быстрее, точность чуть ниже");
  assert.equal(at({ reduced: false }).variant, "Полная");
  assert.equal(at({ quantization: "q5_0", reduced: false }).variant, "Полная");
  assert.equal(at({ quantization: "f16", reduced: true }).variant, "Уменьшенная — меньше и быстрее, точность чуть ниже");
  assert.notEqual(at({ reduced: true }).variantHint, "");
  assert.notEqual(at({ reduced: false }).variantHint, "");
  // The visible page line explains both variants (the row's title is only extra).
  assert.equal(
    texts.variantsNote,
    "Полная — исходная модель. Уменьшенная — сжатая копия той же модели: файл меньше, работает быстрее, точность чуть ниже.",
  );

  const accuracy = {
    tiny: "низкая", base: "средняя", small: "хорошая", medium: "высокая", "large-v3-turbo": "высокая",
    "large-v3": "самая высокая",
  };
  for (const [family, word] of Object.entries(accuracy)) {
    assert.equal(at({ family }).accuracy, `Точность: ${word}`, family);
  }
  assert.equal(at({ family: "" }).accuracy, "");

  assert.equal(at({ recommended: true }).recommended, "Рекомендуется для русского");
  assert.equal(at({ recommended: false }).recommended, "");
  assert.equal(at({ recommended: true, englishOnly: true }).recommended, "", "never on an English-only model");

  // The tier is only a secondary description; the heaviest tier has none (the load and the warning say it).
  assert.equal(at({ tier: "fast" }).description, "Быстрее, но менее точная");
  assert.equal(at({ tier: "accurate" }).description, "Точнее, но медленнее");
  assert.equal(at({ tier: "balanced" }).description, "");
  assert.equal(at({ tier: "max" }).description, "");
  assert.equal(at({ tier: "toString" }).description, "");
});

test("a catalog row shows the quantization only in the technical id", () => {
  for (const [catalogId, quantization] of [["base-q5_1", "q5_1"], ["medium-q5_0", "q5_0"], ["small-f16", "f16"]]) {
    const l = catalogLabels({ ...available, catalogId, name: catalogId, quantization, family: catalogId.split("-")[0] });
    const shown = Object.values(l).join(" ");
    assert.ok(!shown.includes(quantization), `${catalogId}: ${shown}`);
  }
  const page = readFileSync(new URL("../src/pages/ModelsPage.tsx", import.meta.url), "utf8");
  const row = page.slice(page.indexOf("function CatalogRow("));
  assert.ok(!row.includes("modelTitle("), "the catalog row does not use name + version + quantization");
  assert.equal(row.match(/<bdi>\{m\.catalogId\}<\/bdi>/g).length, 1, "the id once, as plain text");
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
  assert.match(page, /role="img" aria-label=\{props\.text\}/);
  assert.match(page, /useState<LanguageFilter>\("all"\)/, "every language by default");
  assert.match(page, /useState<LoadFilter>\("any"\)/, "any load by default");
  assert.match(page, /<Select id=\{id\} value=\{lang\}/);
  assert.match(page, /<Select id=\{id\} value=\{load\}/);
  assert.match(page, /<bdi>\{l\.name\}<\/bdi>/);
  assert.ok(!page.includes("style={{"), "no inline styles");
  assert.match(page, /<LoadScale load=\{m\.load\} heavy=\{m\.heavy\}/, "the heavy colour comes from the core's flag");
  assert.match(page, /<span className="msize muted num">/, "the size is as large as the name (AC6)");
  assert.match(page, /catalogGroups\(v\.available, lang, load, v\.download\)/, "the download row is never filtered out");
});

