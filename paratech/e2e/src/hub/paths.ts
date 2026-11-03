import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { BadConfigError } from "./errors.js";

/** Directories of the repository, found from this file (build/src/hub/paths.js), never from the cwd. */
export interface Paths {
  e2eDir: string;
  paratechDir: string;
  composeFile: string;
  distDir: string;
}

export function repoPaths(): Paths {
  const e2eDir = fileURLToPath(new URL("../../../", import.meta.url));
  const paratechDir = path.resolve(e2eDir, "..");
  const composeFile = path.join(paratechDir, "hub", "docker-compose.yml");
  if (!existsSync(composeFile)) {
    throw new BadConfigError("Не найден paratech/hub/docker-compose.yml рядом с paratech/e2e");
  }
  return { e2eDir: path.resolve(e2eDir), paratechDir, composeFile, distDir: path.join(paratechDir, "dist", "e2e") };
}
