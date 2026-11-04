// The speech model of the settings pack (TASK-377, ADR-0035 §4): ggml-base-q5_1.bin, downloaded ONCE into
// paratech/dist/cache/e2e/ and checked against the size and SHA-256 of desktop/internal/app/catalog.json, which are read
// at run time (the numbers are not copied into code). The download goes to a `.part` file that is renamed to the final
// name only after the hash matched, so a final-name file is always a verified one. Also: the placing of the file under
// <ctl>/fixtures (the app takes dialog picks only from there) and the corrupted copy (exactly one byte changed).
import { createHash } from "node:crypto";
import { copyFileSync, createReadStream, createWriteStream, existsSync, linkSync, mkdirSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

import { findE2EDir } from "../hub/paths.js";

/** The catalog entry of the model the pack imports. */
export const ModelCatalogId = "base-q5_1";

export interface CatalogModel {
  id: string;
  /** The file name of the download, the last segment of source_url: ggml-base-q5_1.bin. */
  fileName: string;
  sourceUrl: string;
  sizeBytes: number;
  /** Lowercase hex. */
  sha256: string;
}

/**
 * Why a fetch failed. Only `network` is a problem of the machine («не проверено»); `mismatch` (size or SHA-256 differ from
 * catalog.json, or more bytes than the catalog says) and `catalog` (catalog.json is unusable) are product defects and must
 * fail the row.
 */
export type ModelFetchKind = "network" | "mismatch" | "catalog";

/** A download or a cached file that is not the model of the catalog. Loud on purpose: nothing is left under the final name. */
export class ModelFetchError extends Error {
  constructor(
    message: string,
    readonly kind: ModelFetchKind,
  ) {
    super(message);
    this.name = "ModelFetchError";
  }
}

const shaPattern = /^[0-9a-f]{64}$/;

/** Parses catalog.json text and returns the entry `id`. Throws ModelFetchError on anything that is not a usable entry. */
export function parseCatalogModel(text: string, id: string = ModelCatalogId): CatalogModel {
  let doc: unknown;
  try {
    doc = JSON.parse(text);
  } catch {
    throw new ModelFetchError("catalog.json: не JSON", "catalog");
  }
  const models = (doc as { models?: unknown } | null)?.models;
  if (!Array.isArray(models)) {
    throw new ModelFetchError("catalog.json: нет списка models", "catalog");
  }
  const entry = models.find((m: unknown) => (m as { id?: unknown } | null)?.id === id) as Record<string, unknown> | undefined;
  if (entry === undefined) {
    throw new ModelFetchError(`catalog.json: нет модели ${id}`, "catalog");
  }
  const { source_url: sourceUrl, size_bytes: sizeBytes, sha256 } = entry;
  if (typeof sourceUrl !== "string" || !/^https:\/\/[^\s]+$/.test(sourceUrl)) {
    throw new ModelFetchError(`catalog.json: ${id}: source_url должен быть https-адресом`, "catalog");
  }
  if (typeof sizeBytes !== "number" || !Number.isSafeInteger(sizeBytes) || sizeBytes <= 0) {
    throw new ModelFetchError(`catalog.json: ${id}: size_bytes должен быть целым числом больше 0`, "catalog");
  }
  if (typeof sha256 !== "string" || !shaPattern.test(sha256)) {
    throw new ModelFetchError(`catalog.json: ${id}: sha256 должен быть 64 символа 0-9a-f`, "catalog");
  }
  const fileName = decodeURIComponent(new URL(sourceUrl).pathname.split("/").pop() ?? "");
  if (!/^[A-Za-z0-9._-]+\.bin$/.test(fileName)) {
    throw new ModelFetchError(`catalog.json: ${id}: из source_url не получилось имя файла .bin`, "catalog");
  }
  return { id, fileName, sourceUrl, sizeBytes, sha256 };
}

export interface ModelPaths {
  catalog: string; // paratech/desktop/internal/app/catalog.json
  cacheDir: string; // paratech/dist/cache/e2e
}

export function modelPaths(moduleUrl: string = import.meta.url): ModelPaths {
  const e2eDir = findE2EDir(moduleUrl);
  const paratechDir = path.resolve(e2eDir, "..");
  return {
    catalog: path.join(paratechDir, "desktop", "internal", "app", "catalog.json"),
    cacheDir: path.join(paratechDir, "dist", "cache", "e2e"),
  };
}

export function readCatalogModel(paths: ModelPaths = modelPaths(), id: string = ModelCatalogId): CatalogModel {
  try {
    return parseCatalogModel(readFileSync(paths.catalog, "utf8"), id);
  } catch (err) {
    if (err instanceof ModelFetchError) {
      throw err;
    }
    throw new ModelFetchError(`не читается ${paths.catalog}`, "catalog");
  }
}

export async function sha256OfFile(file: string): Promise<string> {
  const h = createHash("sha256");
  await pipeline(createReadStream(file), h);
  return h.digest("hex");
}

/** Whether `file` exists with the size and the hash of the model. */
export async function isVerified(file: string, model: Pick<CatalogModel, "sizeBytes" | "sha256">): Promise<boolean> {
  try {
    const st = statSync(file);
    return st.isFile() && st.size === model.sizeBytes && (await sha256OfFile(file)) === model.sha256;
  } catch {
    return false;
  }
}

/**
 * Writes the body of `url` to `dest` (a `.part` file). Progress goes to onProgress(done, total). `maxBytes` is the size the
 * catalog promises: a body that grows past it is cut off (kind `mismatch`), so a wrong or endless answer cannot fill the disk.
 */
export type Downloader = (url: string, dest: string, onProgress: (done: number, total: number) => void, maxBytes: number) => Promise<void>;

/** The whole download must end before the 10 minutes of the beforeAll of the model spec (specs/settings/model.spec.ts). */
export const downloadTimeoutMs = 9 * 60_000;

export const httpDownload: Downloader = async (url, dest, onProgress, maxBytes) => {
  // redirects (HuggingFace -> CDN) are followed; what arrives is trusted only after the size and SHA-256 of the catalog matched
  const res = await fetch(url, { redirect: "follow", signal: AbortSignal.timeout(downloadTimeoutMs) });
  if (!res.ok || res.body === null) {
    throw new ModelFetchError(`загрузка модели: ответ ${res.status} от ${new URL(url).host}`, "network");
  }
  const total = Number(res.headers.get("content-length") ?? 0);
  if (total > maxBytes) {
    await res.body.cancel().catch(() => undefined);
    throw new ModelFetchError(`загрузка модели: сервер обещает ${total} байт, в каталоге ${maxBytes}`, "mismatch");
  }
  let done = 0;
  let tooBig = false;
  const counted = Readable.fromWeb(res.body as import("node:stream/web").ReadableStream<Uint8Array>);
  counted.on("data", (chunk: Buffer) => {
    done += chunk.length;
    if (done > maxBytes && !tooBig) {
      tooBig = true;
      counted.destroy(new ModelFetchError(`загрузка модели: получено больше ${maxBytes} байт из каталога`, "mismatch"));
      return;
    }
    onProgress(done, total);
  });
  try {
    await pipeline(counted, createWriteStream(dest));
  } catch (err) {
    if (tooBig) {
      rmSync(dest, { force: true });
    }
    throw err;
  }
};

export interface EnsureModelOptions {
  paths?: ModelPaths;
  download?: Downloader;
  log?: (line: string) => void;
}

/**
 * The verified model file in the cache: reused when it has the catalog size and hash, else downloaded to `<name>.part`,
 * checked, renamed. A mismatch deletes the `.part`, leaves nothing under the final name and throws ModelFetchError.
 */
export async function ensureModel(opts: EnsureModelOptions = {}): Promise<{ file: string; model: CatalogModel; downloaded: boolean }> {
  const paths = opts.paths ?? modelPaths();
  const log = opts.log ?? ((line: string) => console.log(`[fetch-model] ${line}`));
  const model = readCatalogModel(paths);
  const file = path.join(paths.cacheDir, model.fileName);
  const part = `${file}.part`;
  mkdirSync(paths.cacheDir, { recursive: true });

  if (existsSync(file)) {
    if (await isVerified(file, model)) {
      log(`${model.fileName}: в кэше, размер и SHA-256 совпадают`);
      return { file, model, downloaded: false };
    }
    log(`${model.fileName}: в кэше не та модель (размер или SHA-256), загружаю заново`);
    rmSync(file, { force: true });
  }

  rmSync(part, { force: true });
  log(`загружаю ${model.fileName} (${(model.sizeBytes / 1024 / 1024).toFixed(1)} МБ) с ${new URL(model.sourceUrl).host}`);
  let lastPercent = -1;
  const progress = (done: number, total: number) => {
    const percent = Math.floor((done * 100) / (total > 0 ? total : model.sizeBytes));
    if (percent >= lastPercent + 10 || percent === 100) {
      lastPercent = percent;
      log(`${model.fileName}: ${Math.min(percent, 100)} %`);
    }
  };
  try {
    await (opts.download ?? httpDownload)(model.sourceUrl, part, progress, model.sizeBytes);
    const size = statSync(part).size;
    if (size !== model.sizeBytes) {
      throw new ModelFetchError(`${model.fileName}: размер ${size} байт, в каталоге ${model.sizeBytes}`, "mismatch");
    }
    const sha = await sha256OfFile(part);
    if (sha !== model.sha256) {
      throw new ModelFetchError(`${model.fileName}: SHA-256 загруженного файла не совпадает с каталогом (получено ${sha})`, "mismatch");
    }
    renameSync(part, file);
  } catch (err) {
    rmSync(part, { force: true });
    rmSync(file, { force: true }); // nothing of a failed download stays under the final name
    // anything that is not already classified (a reset connection, a timeout, DNS) is the network
    throw err instanceof ModelFetchError ? err : new ModelFetchError(`${model.fileName}: загрузка не удалась (${err instanceof Error ? err.message : String(err)})`, "network");
  }
  log(`${model.fileName}: загружен и проверен`);
  return { file, model, downloaded: true };
}

/** Puts `src` at `<fixturesDir>/<rel>`: a hard link when the volume allows it, else a copy. Returns the path for pick(): `fixtures/<rel>`. */
export function placeInFixtures(src: string, fixturesDir: string, rel: string): string {
  const dest = path.join(fixturesDir, ...rel.split("/"));
  mkdirSync(path.dirname(dest), { recursive: true });
  rmSync(dest, { force: true });
  try {
    linkSync(src, dest);
  } catch {
    copyFileSync(src, dest);
  }
  return `fixtures/${rel}`;
}

/** A copy of `bytes` with exactly one byte changed (XOR 0x01) at `offset`; default: the middle, behind the ggml header. */
export function flipOneByte(bytes: Uint8Array, offset: number = Math.floor(bytes.length / 2)): Buffer {
  if (!Number.isInteger(offset) || offset < 0 || offset >= bytes.length) {
    throw new RangeError(`flipOneByte: смещение ${offset} вне файла из ${bytes.length} байт`);
  }
  const copy = Buffer.from(bytes);
  copy[offset] = (copy[offset] ?? 0) ^ 0x01;
  return copy;
}

/** Writes a corrupted copy of `src` to `<fixturesDir>/<rel>` (a real copy, never a link) and returns `fixtures/<rel>`. */
export function makeCorruptedCopy(src: string, fixturesDir: string, rel: string, offset?: number): { pick: string; offset: number } {
  const bytes = readFileSync(src);
  const at = offset ?? Math.floor(bytes.length / 2);
  const dest = path.join(fixturesDir, ...rel.split("/"));
  mkdirSync(path.dirname(dest), { recursive: true });
  rmSync(dest, { force: true });
  writeFileSync(dest, flipOneByte(bytes, at));
  return { pick: `fixtures/${rel}`, offset: at };
}
