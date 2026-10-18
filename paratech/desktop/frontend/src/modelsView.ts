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

/**
 * The Russian description of a catalog entry. The name comes from the family; the load level, the family and the
 * flags come from the core, the page only words them. Every model can be downloaded: «Рекомендуется» is a hint.
 */
export interface CatalogLabels {
  /** «Small», «Large v3 Turbo»; the catalog id for an unknown family. */
  name: string;
  /** «Рекомендуется для русского» or "". */
  recommended: string;
  /** A short secondary description from the tier, or "". */
  description: string;
  language: string;
  /** «Полная» or «Уменьшенная — …», and what it means (a tooltip). */
  variant: string;
  variantHint: string;
  /** «Точность: хорошая», or "" for an unknown family. */
  accuracy: string;
  /** «Нагрузка на компьютер: 4 из 5 — тяжёлая», or "" when the load is unknown. */
  load: string;
  /** The warning of a heavy model, or "". */
  warning: string;
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

// No line for "max": the load scale and the heavy warning already say it is slow, and the accuracy line says how
// accurate it is (turbo is not the most accurate one).
const tierLabels: Readonly<Record<string, string>> = {
  fast: "Быстрее, но менее точная",
  accurate: "Точнее, но медленнее",
};

/** The words of the load levels 1..5; index 0 (unknown) has none. */
export const loadWords = ["", "лёгкая", "умеренная", "средняя", "тяжёлая", "очень тяжёлая"] as const;

/** The number of segments of the load scale. */
export const loadLevels = 5;

// Levels 4 and 5 are heavy (ModelLoad.Heavy in the core, docs/refs/whisper.md).
const heavyFrom = 4;

/** «Small», «Large v3 Turbo»; the catalog id when the family is unknown. */
export function displayName(m: { family: string; catalogId: string }): string {
  return Object.hasOwn(familyNames, m.family) ? familyNames[m.family] : m.catalogId;
}

/** Whether the load is a known level 1..5. */
export function knownLoad(load: number): boolean {
  return Number.isInteger(load) && load >= 1 && load <= loadLevels;
}

/** «Нагрузка на компьютер: 4 из 5 — тяжёлая»; "" when the load is unknown. */
export function loadText(load: number): string {
  return knownLoad(load) ? `Нагрузка на компьютер: ${load} из ${loadLevels} — ${loadWords[load]}` : "";
}

/** Whether the page warns that the model is demanding (load 4 or 5). */
export function isHeavy(load: number): boolean {
  return knownLoad(load) && load >= heavyFrom;
}

/** The reduced (quantized, "q…") variant of a model; f16 and anything else is the full one. */
export function isReduced(m: { quantization: string }): boolean {
  return /^q/i.test(m.quantization);
}

export function catalogLabels(m: AvailableModel): CatalogLabels {
  return {
    name: displayName(m),
    recommended: m.recommended && !m.englishOnly ? texts.recommended : "",
    description: Object.hasOwn(tierLabels, m.tier) ? tierLabels[m.tier] : "",
    language: m.englishOnly ? texts.englishOnly : texts.multilingual,
    variant: isReduced(m) ? texts.reduced : texts.full,
    variantHint: isReduced(m) ? texts.reducedHint : texts.fullHint,
    accuracy: Object.hasOwn(familyAccuracy, m.family) ? `Точность: ${familyAccuracy[m.family]}` : "",
    load: loadText(m.load),
    warning: isHeavy(m.load) ? texts.heavy : "",
  };
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
 * The catalog after the filters, in the core's order. With every language the list is split into the multilingual
 * and the English-only models; a model of unknown load is hidden by a load limit. Empty groups are dropped.
 */
export function catalogGroups(list: readonly AvailableModel[], lang: LanguageFilter, load: LoadFilter): CatalogGroup[] {
  const limit = load === "any" ? 0 : Number(load);
  const shown = list.filter((m) => limit === 0 || (knownLoad(m.load) && m.load <= limit));
  const multilingual = shown.filter((m) => !m.englishOnly);
  const english = shown.filter((m) => m.englishOnly);
  let groups: CatalogGroup[];
  if (lang === "all") {
    groups = [
      { title: texts.multilingual, models: multilingual },
      { title: texts.englishOnly, models: english },
    ];
  } else {
    groups = [{ title: "", models: lang === "english" ? english : multilingual }];
  }
  return groups.filter((g) => g.models.length > 0);
}

/** "base 1, q5_1": name, version and quantization when there is one. */
export function modelTitle(m: { name: string; version: string; quantization: string }): string {
  return m.quantization !== "" ? `${m.name} ${m.version}, ${m.quantization}` : `${m.name} ${m.version}`;
}

/** The first 12 hex digits of a SHA-256; the page shows the full one on expand. */
export function shortSha(sha: string): string {
  return sha.length > 12 ? `${sha.slice(0, 12)}…` : sha;
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
  speechMissing:
    "whisper-cli не найден рядом с программой: распознавание недоступно. Положите whisper-cli.exe и его DLL рядом с " +
    "learning-assistant.exe и перезапустите приложение.",
  noModels: "Модель не установлена: записи не распознаются, пока вы не импортируете модель.",
  afterImport: "После установки модели ожидающие записи начнут распознаваться без перезапуска.",
  needDefault: "Установлено несколько моделей, но ни одна не выбрана по умолчанию: распознавание ждёт выбора.",
  emptyCatalog: "Список моделей пуст. Модель можно установить из файла (см. «Другие модели»).",
  machineUnknown: "Оборудование компьютера не определено: нагрузка указана примерно, для обычного компьютера.",
  catalogTitle: "Модели для скачивания",
  variantsNote: "Уменьшенная — сжатая копия той же модели: файл меньше, работает быстрее, точность чуть ниже.",
  full: "Полная",
  fullHint: "Полная модель: исходный файл, самая высокая точность этого размера.",
  reduced: "Уменьшенная — меньше и быстрее, точность чуть ниже",
  reducedHint: "Сжатая копия той же модели: файл в 2–3 раза меньше, распознаёт быстрее, ошибок чуть больше.",
  multilingual: "Русский и другие языки",
  englishOnly: "Только английский",
  recommended: "Рекомендуется для русского",
  heavy: "Модель требовательна к ресурсам вашего устройства: распознавание будет долгим на обычном процессоре",
  languageFilter: "Язык",
  loadFilter: "Нагрузка на компьютер",
  noMatch: "Нет моделей с такими условиями. Измените фильтры.",
  otherModels:
    "Другие модели whisper.cpp лежат на Hugging Face. Скачайте файл .bin в браузере и установите его кнопкой " +
    "«Импортировать файл модели…».",
  likelySlow: "Может работать медленно на этом компьютере.",
  compareSha:
    "Сравните SHA-256 с указанной на официальной странице модели (Hugging Face: страница файла, строка «SHA256»). " +
    "Устанавливайте, только если они совпадают.",
  choosing: "Выбор файла… После выбора считается SHA-256: для файла 0,5–1,5 ГБ это несколько секунд.",
  installing: "Установка: файл копируется в папку приложения и проверяется ещё раз…",
  deleteTitle: "Удалить модель?",
  deleteConfirm: "Её файл будет удалён из папки приложения.",
} as const;

/** The notes above the lists, warnings first. */
export function statusNotes(v: ModelsView): Note[] {
  const notes: Note[] = [];
  if (!v.speechReady) {
    notes.push({ text: texts.speechMissing, warning: true });
  }
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

/** "Модель установлена: base 1, q5_1." */
export function installedText(m: InstalledModel): string {
  return `Модель установлена: ${modelTitle(m)}.`;
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
