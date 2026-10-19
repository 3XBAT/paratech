// Display logic of the models page: texts, formats, which buttons are offered and the steps of the file import. What
// is installed, recommended or allowed is decided by the Go core; the page only hides buttons the core would refuse
// anyway (ARCH-44). Errors of the core are fixed Russian texts and are shown as is, as plain text.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import {
  identityErrors,
  type AvailableModel,
  type InstalledModel,
  type ModelDownload,
  type ModelFileSelection,
  type ModelImportConfirmation,
  type ModelsView,
} from "./bindings/models.js";
import type { ModelLoadEstimates } from "./bindings/load.js";
import { modelEstimateLine } from "./durationEstimate.js";

/** Formats bytes like the core: "1,5 ГБ", "300 МБ", "12 КБ" (binary units, decimal comma). */
export function sizeText(n: number): string {
  const kb = 1024;
  const mb = kb * 1024;
  const gb = mb * 1024;
  let s: string;
  if (n >= gb) {
    s = `${(n / gb).toFixed(1)} ГБ`;
  } else if (n >= mb) {
    s = `${(n / mb).toFixed(0)} МБ`;
  } else if (n >= kb) {
    s = `${(n / kb).toFixed(0)} КБ`;
  } else {
    s = `${n} Б`;
  }
  return s.replace(".", ",");
}

/** Whole percent of a download, 0 while the size is unknown. */
export function progressPercent(d: ModelDownload): number {
  if (d.totalBytes <= 0) {
    return 0;
  }
  return Math.min(100, Math.max(0, Math.floor((d.doneBytes * 100) / d.totalBytes)));
}

/** The download of the catalog model, or null when the last download was another model or none. */
export function downloadOf(m: AvailableModel, d: ModelDownload): ModelDownload | null {
  return d.phase !== "" && d.catalogId === m.catalogId ? d : null;
}

/** Status line of a download: progress, verification, outcome or the core's error text. */
export function downloadText(d: ModelDownload): string {
  switch (d.phase) {
    case "downloading":
      return d.totalBytes > 0
        ? `Скачивается: ${progressPercent(d)}% (${sizeText(d.doneBytes)} из ${sizeText(d.totalBytes)})`
        : `Скачивается: ${sizeText(d.doneBytes)}`;
    case "verifying":
      return "Проверяется SHA-256…";
    case "installed":
      return "Скачана и установлена.";
    case "failed":
      return d.error || "Скачивание не удалось.";
    case "cancelled":
      return d.error || "Скачивание остановлено.";
    default:
      return "";
  }
}

/** The download line of the status column: «25% · 50 МБ из 200 МБ» next to the «Скачивается» chip, else downloadText. */
export function progressText(d: ModelDownload): string {
  if (d.phase === "downloading" && d.totalBytes > 0) {
    return `${progressPercent(d)}% · ${sizeText(d.doneBytes)} из ${sizeText(d.totalBytes)}`;
  }
  return downloadText(d);
}

/** The chip of a download phase; null without a download or after it installed (the row says «Установлена»). */
export function downloadChip(
  d: ModelDownload,
): { kind: "run" | "fail" | "neutral"; icon: "down" | "clock" | "warn" | "x"; label: string } | null {
  switch (d.phase) {
    case "downloading":
      return { kind: "run", icon: "down", label: "Скачивается" };
    case "verifying":
      return { kind: "run", icon: "clock", label: "Проверяется" };
    case "failed":
      return { kind: "fail", icon: "warn", label: "Не скачана" };
    case "cancelled":
      return { kind: "neutral", icon: "x", label: "Остановлено" };
    default:
      return null;
  }
}

/** Buttons of a catalog row. One download runs at a time, so another running download disables "Скачать". */
export interface CatalogActions {
  download: boolean;
  cancel: boolean;
  discardPartial: boolean;
  importFile: boolean;
  openSource: boolean;
}

export function catalogActions(m: AvailableModel, d: ModelDownload): CatalogActions {
  const own = downloadOf(m, d);
  const ownRunning = own !== null && own.running;
  const stopped = own !== null && !own.running && (own.phase === "failed" || own.phase === "cancelled");
  return {
    download: !m.installed && !d.running,
    cancel: ownRunning,
    discardPartial: stopped,
    importFile: !m.installed && !ownRunning,
    openSource: m.hasSourcePage,
  };
}

/** The one action button of a row: download (disabled while another download runs), cancel or delete. */
export type RowAction = "download" | "cancel" | "delete";

export function catalogAction(m: AvailableModel, d: ModelDownload): RowAction {
  if (catalogActions(m, d).cancel) {
    return "cancel";
  }
  return m.installed ? "delete" : "download";
}

/** A secondary action of a row, offered in its «⋯» menu. */
export type MenuOp = "source" | "discard" | "import" | "default";

export interface MenuItem {
  op: MenuOp;
  label: string;
}

export const menuLabels: Readonly<Record<MenuOp, string>> = {
  source: "Открыть страницу источника",
  discard: "Удалить недокачанный файл",
  import: "Импортировать скачанный файл…",
  default: "Сделать по умолчанию",
};

const item = (op: MenuOp): MenuItem => ({ op, label: menuLabels[op] });

export function catalogMenu(m: AvailableModel, d: ModelDownload): MenuItem[] {
  const a = catalogActions(m, d);
  const items: MenuItem[] = [];
  if (a.openSource) {
    items.push(item("source"));
  }
  if (a.discardPartial) {
    items.push(item("discard"));
  }
  if (a.importFile) {
    items.push(item("import"));
  }
  return items;
}

/** The menu of an installed model; entry is its catalog entry, if any (catalogEntryOf). */
export function installedMenu(m: InstalledModel, entry: AvailableModel | null): MenuItem[] {
  const items: MenuItem[] = [];
  if (!m.isDefault) {
    items.push(item("default"));
  }
  if (entry !== null && entry.hasSourcePage) {
    items.push(item("source"));
  }
  return items;
}

// The «⋯» menu logic moved to ui/menu.ts with the recordings menu (TASK-095); the models tests import it from here.
export { menuKey, menuShown } from "./ui/menu.js";

/**
 * The text of a model row, catalog or installed. The family, the load and the flags come from the core; the page only
 * words them (ARCH-44).
 */
export interface ModelRow {
  /** «Small», «Large v3 Turbo»; the catalog id for an unknown family; the name of an imported model. */
  name: string;
  /** «Q5» for a reduced (quantized) variant, "" for a full one. Its meaning is q5Hint. */
  suffix: string;
  size: string;
  /** Only «Рекомендуется для русского» and «Только английский». */
  tags: string[];
  /** The load 1..5; 0 when unknown: no scale. */
  load: number;
  /** The rest of the muted line after the scale: accuracy, language, version. */
  details: string[];
  /** The core's «below the recommended hardware» hint as a part of the muted line, or "". */
  slow: string;
  /** The core's heavy flag (load 4–5): the scale is coloured as a warning. */
  heavy: boolean;
  /** One short warning line, only with the heavy flag, or "". */
  warning: string;
  /** The technical id (and the SHA-256 of an installed model): a tooltip only, never visible text. */
  tip: string;
  /** «Запись 1 ч ≈ N мин в режиме «Бережная»» (TASK-084), "" without a number. */
  estimate: string;
  /** The estimate is above 1 h per 1 h of audio: shown as a warning. */
  estimateHeavy: boolean;
  /** The estimate in every mode, one per line: the tooltip of the estimate. */
  estimateTip: string;
}

function estimateFields(est: ModelLoadEstimates | null | undefined): Pick<ModelRow, "estimate" | "estimateHeavy" | "estimateTip"> {
  const e = modelEstimateLine(est);
  return { estimate: e.text, estimateHeavy: e.heavy, estimateTip: e.tip };
}

const familyNames: Readonly<Record<string, string>> = {
  tiny: "Tiny",
  base: "Base",
  small: "Small",
  medium: "Medium",
  "large-v3": "Large v3",
  "large-v3-turbo": "Large v3 Turbo",
};

const familyAccuracy: Readonly<Record<string, string>> = {
  tiny: "низкая",
  base: "средняя",
  small: "хорошая",
  medium: "высокая",
  "large-v3-turbo": "высокая",
  "large-v3": "самая высокая",
};

/** The words of the load levels 1..5; index 0 (unknown) has none. */
export const loadWords = ["", "лёгкая", "умеренная", "средняя", "тяжёлая", "очень тяжёлая"] as const;

/** The number of segments of the load scale. */
export const loadLevels = 5;

/** «Small», «Large v3 Turbo»; the catalog id when the family is unknown. */
export function displayName(m: { family: string; catalogId: string }): string {
  return Object.hasOwn(familyNames, m.family) ? familyNames[m.family] : m.catalogId;
}

/** Whether the load is a known level 1..5. */
export function knownLoad(load: number): boolean {
  return Number.isInteger(load) && load >= 1 && load <= loadLevels;
}

/** «Нагрузка 4 из 5», shown next to the scale; "" when the load is unknown. */
export function loadText(load: number): string {
  return knownLoad(load) ? `Нагрузка ${load} из ${loadLevels}` : "";
}

/** «Нагрузка на компьютер: 4 из 5 — тяжёлая», the accessible name and tooltip of the scale; "" when unknown. */
export function loadHint(load: number): string {
  return knownLoad(load) ? `Нагрузка на компьютер: ${load} из ${loadLevels} — ${loadWords[load]}` : "";
}

/** «точность хорошая»; "" for an unknown family. */
export function accuracyText(family: string): string {
  return Object.hasOwn(familyAccuracy, family) ? `точность ${familyAccuracy[family]}` : "";
}

/** «Q5» from "q5_1" or "q5_0"; the quantization in capitals for another scheme. */
export function reducedSuffix(quantization: string): string {
  const q = /^q(\d+)/i.exec(quantization);
  if (q !== null) {
    return `Q${q[1]}`;
  }
  return quantization !== "" ? quantization.toUpperCase() : "Q";
}

export function catalogRow(m: AvailableModel): ModelRow {
  const tags: string[] = [];
  if (m.recommended && !m.englishOnly) {
    tags.push(texts.recommended);
  }
  if (m.englishOnly) {
    tags.push(texts.englishOnly);
  }
  const est = estimateFields(m.estimates);
  return {
    name: displayName(m),
    suffix: m.reduced ? reducedSuffix(m.quantization) : "",
    size: sizeText(m.sizeBytes),
    tags,
    load: knownLoad(m.load) ? m.load : 0,
    heavy: m.heavy,
    // An English-only model has its language as the tag already.
    details: [accuracyText(m.family), m.englishOnly ? "" : texts.languages].filter((s) => s !== ""),
    // The heavy warning says it is slow already.
    slow: m.likelySlow && !m.heavy ? texts.likelySlow : "",
    // With a number the estimate line is the warning: a concrete time instead of «будет долгим».
    warning: m.heavy && est.estimate === "" ? texts.heavy : "",
    tip: m.catalogId,
    ...est,
  };
}

/** The catalog entry of an installed model, or null for an imported model or one the catalog no longer has. */
export function catalogEntryOf(m: InstalledModel, catalog: readonly AvailableModel[]): AvailableModel | null {
  return m.fromCatalog ? (catalog.find((a) => a.catalogId === m.catalogId) ?? null) : null;
}

/**
 * An installed model in the catalog row format, without the warnings. An imported model has no family, so it shows its
 * own name, the version and the quantization, and no load scale.
 */
export function installedRow(m: InstalledModel, catalog: readonly AvailableModel[]): ModelRow {
  const sha = `SHA-256: ${m.sha256}`;
  const e = catalogEntryOf(m, catalog);
  if (e !== null) {
    // The installed model has its own estimates: measured runs are per installed model.
    return {
      ...catalogRow(e),
      size: sizeText(m.sizeBytes),
      slow: "",
      warning: "",
      tip: `${e.catalogId}\n${sha}`,
      ...estimateFields(m.estimates),
    };
  }
  const details = [`версия ${m.version}`];
  if (m.quantization !== "") {
    details.push(`квантизация ${m.quantization}`);
  }
  if (m.capabilities.includes("multilingual")) {
    details.push(texts.languages);
  }
  return {
    name: m.name,
    suffix: "",
    size: sizeText(m.sizeBytes),
    tags: [],
    load: 0,
    heavy: false,
    details,
    slow: "",
    warning: "",
    tip: `${m.name} ${m.version}\n${sha}`,
    ...estimateFields(m.estimates),
  };
}

/** The name of an installed model in a dialog or a message: «Small Q5»; name, version and quantization if imported. */
export function installedName(m: InstalledModel, catalog: readonly AvailableModel[]): string {
  if (catalogEntryOf(m, catalog) === null) {
    return modelTitle(m);
  }
  const r = installedRow(m, catalog);
  return r.suffix !== "" ? `${r.name} ${r.suffix}` : r.name;
}

/** The language filter of the catalog. */
export type LanguageFilter = "all" | "multilingual" | "english";

/** The load filter: any load, or at most 2, 3 or 4 of 5. */
export type LoadFilter = "any" | "2" | "3" | "4";

export const languageOptions: readonly { value: LanguageFilter; label: string }[] = [
  { value: "all", label: "Все" },
  { value: "multilingual", label: "Русский и другие языки" },
  { value: "english", label: "Только английский" },
];

export const loadOptions: readonly { value: LoadFilter; label: string }[] = [
  { value: "any", label: "Любая" },
  { value: "2", label: "До 2 из 5 — лёгкие" },
  { value: "3", label: "До 3 из 5" },
  { value: "4", label: "До 4 из 5" },
];

/** The filter value of a <select>; an unknown value is the default (show all). */
export function languageFilter(value: string): LanguageFilter {
  return languageOptions.find((o) => o.value === value)?.value ?? "all";
}

export function loadFilter(value: string): LoadFilter {
  return loadOptions.find((o) => o.value === value)?.value ?? "any";
}

/** A heading of the catalog list ("" for none) and its models. */
export interface CatalogGroup {
  title: string;
  models: AvailableModel[];
}

/**
 * Whether the row must stay visible whatever the filters: its download runs or it has a partial to discard, so
 * hiding it would hide the progress and the cancel or discard button.
 */
export function pinnedByDownload(m: AvailableModel, d: ModelDownload): boolean {
  const a = catalogActions(m, d);
  return a.cancel || a.discardPartial;
}

/**
 * The catalog after the filters, in the core's order. With every language the list is split into the multilingual
 * and the English-only models; a model of unknown load is hidden by a load limit. Empty groups are dropped. The row
 * of the current download is always kept (pinnedByDownload).
 */
export function catalogGroups(
  list: readonly AvailableModel[],
  lang: LanguageFilter,
  load: LoadFilter,
  d: ModelDownload,
): CatalogGroup[] {
  const limit = load === "any" ? 0 : Number(load);
  const fits = (m: AvailableModel) =>
    (limit === 0 || (knownLoad(m.load) && m.load <= limit)) &&
    (lang === "all" || (lang === "english") === m.englishOnly);
  const shown = list.filter((m) => fits(m) || pinnedByDownload(m, d));
  const multilingual = shown.filter((m) => !m.englishOnly);
  const english = shown.filter((m) => m.englishOnly);
  // With one language chosen the list has no headings, unless a pinned row of the other language is in it.
  const other = lang === "english" ? multilingual : english;
  if (lang !== "all" && other.length === 0) {
    const own = lang === "english" ? english : multilingual;
    return own.length > 0 ? [{ title: "", models: own }] : [];
  }
  const groups: CatalogGroup[] = [
    { title: texts.multilingual, models: multilingual },
    { title: texts.englishOnly, models: english },
  ];
  return groups.filter((g) => g.models.length > 0);
}

/** "base 1, q5_1": name, version and quantization when there is one. */
export function modelTitle(m: { name: string; version: string; quantization: string }): string {
  return m.quantization !== "" ? `${m.name} ${m.version}, ${m.quantization}` : `${m.name} ${m.version}`;
}

/** The badge of an installed model, from the core's flags only: "По умолчанию", "Используется" or none. */
export function defaultBadge(m: InstalledModel): string {
  if (m.isDefault) {
    return "По умолчанию";
  }
  return m.inUse ? "Используется" : "";
}

/** A note of the page; warnings say why recordings are not transcribed. */
export interface Note {
  text: string;
  warning: boolean;
}

export const texts = {
  noModels: "Модель не установлена: записи не распознаются, пока вы не импортируете модель.",
  afterImport: "После установки модели ожидающие записи начнут распознаваться без перезапуска.",
  needDefault: "Установлено несколько моделей, но ни одна не выбрана по умолчанию: распознавание ждёт выбора.",
  emptyCatalog: "Список моделей пуст. Модель можно установить из файла (см. «Другие модели»).",
  machineUnknown: "Оборудование компьютера не определено: нагрузка указана примерно, для обычного компьютера.",
  catalogTitle: "Модели для скачивания",
  variantsNote: "Q5 в названии — уменьшенная копия модели: файл меньше, работает быстрее, точность чуть ниже.",
  /** The tooltip of the «Q5» suffix. */
  q5Hint: "уменьшенная: меньше и быстрее, точность чуть ниже",
  multilingual: "Русский и другие языки",
  /** The language in the muted line of a row; short, so the longest line fits one line in the default window. */
  languages: "русский и др. языки",
  englishOnly: "Только английский",
  recommended: "Рекомендуется для русского",
  heavy: "Тяжёлая модель: распознавание будет долгим",
  languageFilter: "Язык",
  loadFilter: "Нагрузка на компьютер",
  noMatch: "Нет моделей с такими условиями. Измените фильтры.",
  otherModels:
    "Другие модели для распознавания лежат на Hugging Face. Скачайте файл .bin в браузере и установите его кнопкой " +
    "«Импортировать файл модели…».",
  likelySlow: "может работать медленно на этом компьютере",
  more: "Ещё действия",
  oneDownload: "Скачивается другая модель: одновременно скачивается только одна",
  compareSha:
    "Сравните SHA-256 с указанной на официальной странице модели (Hugging Face: страница файла, строка «SHA256»). " +
    "Устанавливайте, только если они совпадают.",
  choosing: "Выбор файла… После выбора считается SHA-256: для файла 0,5–1,5 ГБ это несколько секунд.",
  installing: "Установка: файл копируется в папку приложения и проверяется ещё раз…",
  deleteTitle: "Удалить модель?",
  deleteConfirm: "Её файл будет удалён из папки приложения.",
} as const;

/** The notes above the lists, warnings first. A missing speech component has its own notice (SpeechComponentNotice). */
export function statusNotes(v: ModelsView): Note[] {
  const notes: Note[] = [];
  if (v.installed.length === 0) {
    notes.push({ text: texts.noModels, warning: true });
    if (v.speechReady) {
      notes.push({ text: texts.afterImport, warning: false });
    }
  }
  if (v.needsDefault) {
    notes.push({ text: texts.needDefault, warning: true });
  }
  return notes;
}

/** "Модель установлена: Small Q5." or, for an imported model, "Модель установлена: base 1, q5_1." */
export function installedText(m: InstalledModel, catalog: readonly AvailableModel[] = []): string {
  return `Модель установлена: ${installedName(m, catalog)}.`;
}

/** The steps of importing a file outside the catalog (AC-23). */
export type ImportStep =
  | { step: "idle" }
  | { step: "choosing" }
  | ConfirmStep
  | { step: "installing"; selection: ModelFileSelection };

export interface ConfirmStep {
  step: "confirm";
  selection: ModelFileSelection;
  name: string;
  version: string;
  quantization: string;
}

const shaPattern = /^[0-9a-f]{64}$/i;

/** The confirm step of a picked file, the identity prefilled from the core's suggestions. */
export function confirmStep(selection: ModelFileSelection): ConfirmStep {
  return {
    step: "confirm",
    selection,
    name: selection.suggestedName,
    version: selection.suggestedVersion,
    quantization: selection.suggestedQuantization,
  };
}

/** Whether "SHA-256 совпадает, установить" is offered: a received selection with a SHA-256 and an identity. */
export function canConfirm(s: ImportStep): boolean {
  return (
    s.step === "confirm" &&
    !s.selection.cancelled &&
    s.selection.selectionId !== "" &&
    shaPattern.test(s.selection.sha256) &&
    s.name.trim() !== "" &&
    s.version.trim() !== ""
  );
}

/** The confirmation sent to the core: always the SHA-256 the page showed, never one typed by the user. */
export function confirmation(s: ConfirmStep): ModelImportConfirmation {
  return {
    selectionId: s.selection.selectionId,
    sha256: s.selection.sha256,
    name: s.name.trim(),
    version: s.version.trim(),
    quantization: s.quantization.trim(),
  };
}

const genericError = "Не удалось выполнить действие с моделью";

/**
 * The text of an error of the core. Its errors are fixed Russian sentences and are shown as is; anything else (an
 * error of the runtime such as "<method>: panic: ...", which may name internals or a path) gets a generic text.
 */
export function errorText(err: unknown): string {
  const msg = errorMessage(err);
  return /^[А-ЯЁ]/.test(msg) ? msg : genericError;
}

/** Whether a failed ConfirmImport kept the selection: only a bad name, version or quantization does. */
export function keepsSelection(err: unknown): boolean {
  return (identityErrors as readonly string[]).includes(errorMessage(err));
}

// Wails rejects a call with an Error-like object carrying the Go error text.
function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}
