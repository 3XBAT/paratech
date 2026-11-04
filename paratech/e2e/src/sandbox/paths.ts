import { cpSync, existsSync, lstatSync, mkdirSync, rmSync, statSync } from "node:fs";
import path from "node:path";

import { findE2EDir } from "../hub/paths.js";
import { SandboxEnvironmentError } from "./errors.js";

export const RunRootName = "e2e-run";

/** Places of a run. Everything is derived from the location of this package, never from the cwd. */
export interface SandboxPaths {
  e2eDir: string; // paratech/e2e
  paratechDir: string;
  distDir: string; // paratech/dist
  runRoot: string; // paratech/dist/e2e-run
  ctlDir: string; // <runRoot>/ctl = --e2e-dir
  dataDir: string; // <ctl>/data = --data-dir
  fixturesDir: string; // <ctl>/fixtures
  logsDir: string; // <ctl>/logs
  buildLog: string; // <runRoot>/build.log
  exePath: string; // paratech/dist/e2e/paratech.exe
  buildScript: string; // paratech/desktop/build-alpha.ps1
  fixturesSource: string; // paratech/e2e/fixtures
}

export function sandboxPaths(moduleUrl: string = import.meta.url): SandboxPaths {
  const e2eDir = path.resolve(findE2EDir(moduleUrl));
  const paratechDir = path.resolve(e2eDir, "..");
  const distDir = path.join(paratechDir, "dist");
  const runRoot = path.join(distDir, RunRootName);
  const ctlDir = path.join(runRoot, "ctl");
  return {
    e2eDir,
    paratechDir,
    distDir,
    runRoot,
    ctlDir,
    dataDir: path.join(ctlDir, "data"),
    fixturesDir: path.join(ctlDir, "fixtures"),
    logsDir: path.join(ctlDir, "logs"),
    buildLog: path.join(runRoot, "build.log"),
    exePath: path.join(distDir, "e2e", "paratech.exe"),
    buildScript: path.join(paratechDir, "desktop", "build-alpha.ps1"),
    fixturesSource: path.join(e2eDir, "fixtures"),
  };
}

const same = (a: string, b: string): boolean => (process.platform === "win32" ? a.toLowerCase() === b.toLowerCase() : a === b);

/**
 * Refuses every folder but <paratech>/dist/e2e-run: the run wipes it recursively, so a wrong path must never get that far.
 * A link in its place is refused too (the wipe would remove the link, not harm, but the run would write elsewhere).
 */
export function assertWipeTarget(target: string, paratechDir: string): void {
  const resolved = path.resolve(target);
  const expectedParent = path.join(path.resolve(paratechDir), "dist");
  if (path.basename(resolved) !== RunRootName || !same(path.dirname(resolved), expectedParent)) {
    throw new SandboxEnvironmentError("BadConfig", `Отказ очищать «${resolved}»: допустима только папка ${RunRootName} в paratech/dist`);
  }
  if (existsSync(resolved) && lstatSync(resolved).isSymbolicLink()) {
    throw new SandboxEnvironmentError("BadConfig", `Отказ очищать «${resolved}»: это ссылка`);
  }
}

/** Wipes and recreates the run root. A locked file is an environment error. */
export function wipeRunRoot(p: SandboxPaths): void {
  assertWipeTarget(p.runRoot, p.paratechDir);
  try {
    rmSync(p.runRoot, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
    mkdirSync(p.runRoot, { recursive: true });
  } catch (err) {
    const code = (err as NodeJS.ErrnoException).code ?? "ошибка";
    throw new SandboxEnvironmentError(
      "SandboxUnavailable",
      `Не удалось очистить ${p.runRoot} (${code}): файл занят? Закройте paratech.exe прошлого прогона`,
    );
  }
}

/** Creates ctl/, data/, logs/ and copies paratech/e2e/fixtures into ctl/fixtures (an empty plain folder if there are none). */
export function prepareControlDir(p: SandboxPaths): void {
  mkdirSync(p.dataDir, { recursive: true });
  mkdirSync(p.logsDir, { recursive: true });
  if (existsSync(p.fixturesSource) && statSync(p.fixturesSource).isDirectory()) {
    // dereference: false and no links in the copy (the app refuses a link or junction on the way to a fixture)
    cpSync(p.fixturesSource, p.fixturesDir, { recursive: true, dereference: false });
  } else {
    mkdirSync(p.fixturesDir, { recursive: true });
  }
}
