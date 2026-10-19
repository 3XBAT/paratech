// Model manager bindings: calls into the Go core (internal/input/wails/models.go). The page sends only ids and the
// confirmation of a shown file; the core shows the open dialog, keeps the chosen path and owns every URL.
// Errors of the core are fixed Russian texts the page shows as plain text.

import { parseModelLoadEstimates, recognitionLoads, type ModelLoadEstimates, type RecognitionLoad } from "./load.js";
import type { CallByName } from "./settings";

/** An installed model. Name and version are checked by the core; render them as plain text anyway. */
export interface InstalledModel {
  id: string;
  name: string;
  version: string;
  sizeBytes: number;
  sha256: string;
  quantization: string;
  capabilities: string[];
  isDefault: boolean;
  fromCatalog: boolean;
  catalogId: string;
  /** RFC 3339 in UTC. */
  installedAt: string;
  /** The model a new STT job pins now (at most one); only list sets it, an import reply has false. */
  inUse: boolean;
  /** «Запись 1 ч ≈ N мин» in the current mode and in every mode (TASK-084); null when unavailable. */
  estimates: ModelLoadEstimates | null;
}

/**
 * A catalog entry. While the machine is not known the core recommends the lightest preferred entry and marks none
 * likelySlow.
 */
export interface AvailableModel {
  catalogId: string;
  name: string;
  version: string;
  sizeBytes: number;
  quantization: string;
  capabilities: string[];
  minCpuCores: number;
  minRamMb: number;
  recommended: boolean;
  likelySlow: boolean;
  installed: boolean;
  installedId: string;
  hasSourcePage: boolean;
  /** "fast", "balanced", "accurate", "max" or "" (none); the page turns it into a short description. */
  tier: string;
  englishOnly: boolean;
  /** The model size: "tiny", "base", "small", "medium", "large-v3", "large-v3-turbo" or "" (none). */
  family: string;
  /** Load on the computer, 1..5; 0 when unknown (values.LoadOf in the core). */
  load: number;
  /** The core's flags: a load that gets the "demanding model" warning, and a quantized (reduced) variant. */
  heavy: boolean;
  reduced: boolean;
  /** «Запись 1 ч ≈ N мин» in the current mode and in every mode (TASK-084); null when unavailable. */
  estimates: ModelLoadEstimates | null;
}

/** The current or last download; phase "" means none yet. */
export interface ModelDownload {
  catalogId: string;
  /** "", "downloading", "verifying", "installed", "failed" or "cancelled". */
  phase: string;
  doneBytes: number;
  totalBytes: number;
  running: boolean;
  /** The text of a failure or a stop. */
  error: string;
}

export interface ModelsView {
  installed: InstalledModel[];
  available: AvailableModel[];
  /** Several models are installed and none is the default: STT jobs wait until the user selects one. */
  needsDefault: boolean;
  /** The speech recognition component works; without it STT jobs wait until it is installed. */
  speechReady: boolean;
  machineKnown: boolean;
  download: ModelDownload;
  modelsHomePage: string;
  /** The load mode of the current estimates; "" when they are unavailable. */
  recognitionLoad: RecognitionLoad | "";
}

/** The disk space check before a download; warning is set when the space is not enough. */
export interface DownloadPlan {
  catalogId: string;
  totalBytes: number;
  haveBytes: number;
  requiredBytes: number;
  availableBytes: number;
  enough: boolean;
  warning: string;
}

/** A picked model file to confirm (AC-23). Cancelled: the user closed the dialog, every other field is empty. */
export interface ModelFileSelection {
  cancelled: boolean;
  selectionId: string;
  /** Base name of the file: render it as plain text in its own <bdi>. */
  fileName: string;
  sizeBytes: number;
  sha256: string;
  suggestedName: string;
  suggestedVersion: string;
  suggestedQuantization: string;
}

/** The confirmation of a shown file: its selection, the SHA-256 the page showed and the identity. */
export interface ModelImportConfirmation {
  selectionId: string;
  sha256: string;
  name: string;
  version: string;
  quantization: string;
}

/** The outcome of importing a file as a catalog model; model is null when the dialog was cancelled. */
export interface CatalogImportResult {
  cancelled: boolean;
  model: InstalledModel | null;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  list: "paratech/desktop/internal/input/wails.ModelsService.List",
  preflightDownload: "paratech/desktop/internal/input/wails.ModelsService.PreflightDownload",
  startDownload: "paratech/desktop/internal/input/wails.ModelsService.StartDownload",
  cancelDownload: "paratech/desktop/internal/input/wails.ModelsService.CancelDownload",
  discardPartial: "paratech/desktop/internal/input/wails.ModelsService.DiscardPartial",
  openSourcePage: "paratech/desktop/internal/input/wails.ModelsService.OpenSourcePage",
  openModelsHomePage: "paratech/desktop/internal/input/wails.ModelsService.OpenModelsHomePage",
  chooseImportFile: "paratech/desktop/internal/input/wails.ModelsService.ChooseImportFile",
  confirmImport: "paratech/desktop/internal/input/wails.ModelsService.ConfirmImport",
  discardImport: "paratech/desktop/internal/input/wails.ModelsService.DiscardImport",
  importCatalogFile: "paratech/desktop/internal/input/wails.ModelsService.ImportCatalogFile",
  setDefault: "paratech/desktop/internal/input/wails.ModelsService.SetDefault",
  delete: "paratech/desktop/internal/input/wails.ModelsService.Delete",
} as const;

/**
 * Errors of ConfirmImport that keep the pending selection: the identity is checked before the selection is taken
 * (ErrModelBadName, ErrModelBadVersion, ErrModelBadQuantization in models.go; a test checks the texts are there).
 */
export const identityErrors = [
  "Название модели: латинские буквы, цифры, «.», «_» и «-», до 64 символов, начинается и заканчивается буквой или цифрой",
  "Версия модели: латинские буквы, цифры, «.», «_» и «-», до 32 символов, начинается и заканчивается буквой или цифрой",
  "Квантизация: строчные латинские буквы, цифры и «_», до 16 символов, или пусто",
] as const;

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null) {
    throw new Error(`${what}: reply is not an object`);
  }
  return raw as Record<string, unknown>;
}

function str(r: Record<string, unknown>, key: string, what: string): string {
  const v = r[key];
  if (typeof v !== "string") {
    throw new Error(`${what}: ${key} is not a string`);
  }
  return v;
}

function num(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
    throw new Error(`${what}: ${key} is not a count`);
  }
  return v;
}

const maxLoad = 5;

function loadLevel(r: Record<string, unknown>, what: string): number {
  const v = num(r, "load", what);
  if (v > maxLoad) {
    throw new Error(`${what}: load is above ${maxLoad}`);
  }
  return v;
}

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

// Go encodes a nil slice as null; it is accepted as empty.
function list<T>(r: Record<string, unknown>, key: string, what: string, parse: (item: unknown) => T): T[] {
  const v = r[key] ?? [];
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v.map(parse);
}

function text(item: unknown): string {
  if (typeof item !== "string") {
    throw new Error("capability is not a string");
  }
  return item;
}

/** Checks the shape of an installed model; a malformed reply is an error, not a default. */
export function parseInstalled(raw: unknown): InstalledModel {
  const w = "installed model";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    name: str(r, "name", w),
    version: str(r, "version", w),
    sizeBytes: num(r, "sizeBytes", w),
    sha256: str(r, "sha256", w),
    quantization: str(r, "quantization", w),
    capabilities: list(r, "capabilities", w, text),
    isDefault: bool(r, "isDefault", w),
    fromCatalog: bool(r, "fromCatalog", w),
    catalogId: str(r, "catalogId", w),
    installedAt: str(r, "installedAt", w),
    inUse: bool(r, "inUse", w),
    estimates: parseModelLoadEstimates(r.estimates),
  };
}

export function parseAvailable(raw: unknown): AvailableModel {
  const w = "catalog model";
  const r = record(raw, w);
  return {
    catalogId: str(r, "catalogId", w),
    name: str(r, "name", w),
    version: str(r, "version", w),
    sizeBytes: num(r, "sizeBytes", w),
    quantization: str(r, "quantization", w),
    capabilities: list(r, "capabilities", w, text),
    minCpuCores: num(r, "minCpuCores", w),
    minRamMb: num(r, "minRamMb", w),
    recommended: bool(r, "recommended", w),
    likelySlow: bool(r, "likelySlow", w),
    installed: bool(r, "installed", w),
    installedId: str(r, "installedId", w),
    hasSourcePage: bool(r, "hasSourcePage", w),
    tier: str(r, "tier", w),
    englishOnly: bool(r, "englishOnly", w),
    family: str(r, "family", w),
    load: loadLevel(r, w),
    heavy: bool(r, "heavy", w),
    reduced: bool(r, "reduced", w),
    estimates: parseModelLoadEstimates(r.estimates),
  };
}

export function parseDownload(raw: unknown): ModelDownload {
  const w = "download";
  const r = record(raw, w);
  return {
    catalogId: str(r, "catalogId", w),
    phase: str(r, "phase", w),
    doneBytes: num(r, "doneBytes", w),
    totalBytes: num(r, "totalBytes", w),
    running: bool(r, "running", w),
    error: str(r, "error", w),
  };
}

function viewLoad(r: Record<string, unknown>, what: string): RecognitionLoad | "" {
  const v = str(r, "recognitionLoad", what);
  if (v !== "" && !(recognitionLoads as readonly string[]).includes(v)) {
    throw new Error(`${what}: unknown recognitionLoad`);
  }
  return v as RecognitionLoad | "";
}

export function parseView(raw: unknown): ModelsView {
  const w = "models";
  const r = record(raw, w);
  return {
    installed: list(r, "installed", w, parseInstalled),
    available: list(r, "available", w, parseAvailable),
    needsDefault: bool(r, "needsDefault", w),
    speechReady: bool(r, "speechReady", w),
    machineKnown: bool(r, "machineKnown", w),
    download: parseDownload(r.download),
    modelsHomePage: str(r, "modelsHomePage", w),
    recognitionLoad: viewLoad(r, w),
  };
}

export function parsePlan(raw: unknown): DownloadPlan {
  const w = "download plan";
  const r = record(raw, w);
  return {
    catalogId: str(r, "catalogId", w),
    totalBytes: num(r, "totalBytes", w),
    haveBytes: num(r, "haveBytes", w),
    requiredBytes: num(r, "requiredBytes", w),
    availableBytes: num(r, "availableBytes", w),
    enough: bool(r, "enough", w),
    warning: str(r, "warning", w),
  };
}

export function parseSelection(raw: unknown): ModelFileSelection {
  const w = "model file";
  const r = record(raw, w);
  return {
    cancelled: bool(r, "cancelled", w),
    selectionId: str(r, "selectionId", w),
    fileName: str(r, "fileName", w),
    sizeBytes: num(r, "sizeBytes", w),
    sha256: str(r, "sha256", w),
    suggestedName: str(r, "suggestedName", w),
    suggestedVersion: str(r, "suggestedVersion", w),
    suggestedQuantization: str(r, "suggestedQuantization", w),
  };
}

export function parseCatalogImport(raw: unknown): CatalogImportResult {
  const w = "catalog import";
  const r = record(raw, w);
  const cancelled = bool(r, "cancelled", w);
  const model = r.model === null || r.model === undefined ? null : parseInstalled(r.model);
  if (!cancelled && model === null) {
    throw new Error(`${w}: no model`);
  }
  return { cancelled, model };
}

export interface ModelsApi {
  list(): Promise<ModelsView>;
  preflightDownload(catalogId: string): Promise<DownloadPlan>;
  startDownload(catalogId: string): Promise<void>;
  cancelDownload(): Promise<void>;
  discardPartial(catalogId: string): Promise<void>;
  openSourcePage(catalogId: string): Promise<void>;
  openModelsHomePage(): Promise<void>;
  /** Opens the dialog in the core, which then hashes the file: this takes seconds for a large file. */
  chooseImportFile(): Promise<ModelFileSelection>;
  confirmImport(c: ModelImportConfirmation): Promise<InstalledModel>;
  discardImport(selectionId: string): Promise<void>;
  /** Opens the dialog in the core and installs the file as the catalog model when it matches the catalog. */
  importCatalogFile(catalogId: string): Promise<CatalogImportResult>;
  setDefault(modelId: string): Promise<void>;
  delete(modelId: string): Promise<void>;
}

/** Builds the model manager API over a call function (the Wails runtime in the app, a fake in tests). */
export function modelsApi(call: CallByName): ModelsApi {
  const run = async (method: string, ...args: unknown[]) => {
    await call(method, ...args);
  };
  return {
    list: async () => parseView(await call(methods.list)),
    preflightDownload: async (catalogId) => parsePlan(await call(methods.preflightDownload, catalogId)),
    startDownload: (catalogId) => run(methods.startDownload, catalogId),
    cancelDownload: () => run(methods.cancelDownload),
    discardPartial: (catalogId) => run(methods.discardPartial, catalogId),
    openSourcePage: (catalogId) => run(methods.openSourcePage, catalogId),
    openModelsHomePage: () => run(methods.openModelsHomePage),
    chooseImportFile: async () => parseSelection(await call(methods.chooseImportFile)),
    confirmImport: async (c) =>
      parseInstalled(
        await call(methods.confirmImport, {
          selectionId: c.selectionId,
          sha256: c.sha256,
          name: c.name,
          version: c.version,
          quantization: c.quantization,
        }),
      ),
    discardImport: (selectionId) => run(methods.discardImport, selectionId),
    importCatalogFile: async (catalogId) => parseCatalogImport(await call(methods.importCatalogFile, catalogId)),
    setDefault: (modelId) => run(methods.setDefault, modelId),
    delete: (modelId) => run(methods.delete, modelId),
  };
}
