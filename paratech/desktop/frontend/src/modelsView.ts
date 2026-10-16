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
  emptyCatalog:
    "Каталог моделей пока пуст: скачать в приложении нечего. Скачайте файл модели (.bin) со страницы моделей " +
    "whisper.cpp в браузере и импортируйте его, например ggml-small.bin (около 466 МБ).",
  machineUnknown:
    "Оборудование компьютера не определено: рекомендуется самая лёгкая подходящая модель, предупреждения о медленной " +
    "работе не показываются.",
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
