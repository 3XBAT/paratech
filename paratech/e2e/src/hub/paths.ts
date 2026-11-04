import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { BadConfigError } from "./errors.js";

/** Directories of the repository, found from this file (src/hub or build/src/hub), never from the cwd. */
export interface Paths {
  e2eDir: string;
  paratechDir: string;
  composeFile: string;
  distDir: string;
}

/**
 * The paratech/e2e folder, found by walking up from a module of this package. The code runs from two places (the
 * tsc output in build/src and the sources under Playwright's loader in src), so a fixed number of "../" would be wrong
 * in one of them.
 */
export function findE2EDir(moduleUrl: string): string {
  let dir = path.dirname(fileURLToPath(moduleUrl));
  for (;;) {
    if (existsSync(path.join(dir, "package.json")) && existsSync(path.join(dir, "src", "contract.ts"))) {
      return dir;
    }
    const parent = path.dirname(dir);
    if (parent === dir) {
      throw new BadConfigError("Не найден каталог paratech/e2e рядом с модулем раннера");
    }
    dir = parent;
  }
}

export function repoPaths(): Paths {
  const e2eDir = findE2EDir(import.meta.url);
  const paratechDir = path.resolve(e2eDir, "..");
  const composeFile = path.join(paratechDir, "hub", "docker-compose.yml");
  if (!existsSync(composeFile)) {
    throw new BadConfigError("Не найден paratech/hub/docker-compose.yml рядом с paratech/e2e");
  }
  return { e2eDir: path.resolve(e2eDir), paratechDir, composeFile, distDir: path.join(paratechDir, "dist", "e2e") };
}
