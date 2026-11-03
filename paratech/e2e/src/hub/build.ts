import { mkdirSync } from "node:fs";
import path from "node:path";

import { type Env, toolEnv } from "./env.js";
import { HubBuildFailedError } from "./errors.js";
import type { Paths } from "./paths.js";
import { run, SpawnError } from "./proc.js";

const buildTimeoutMs = 600_000;

let built: Promise<string> | undefined;

/** Builds paratech/hub/cmd/hub into paratech/dist/e2e once per process; later calls share the result. */
export function buildHub(paths: Paths, env: Env = process.env): Promise<string> {
  if (!built) {
    built = doBuild(paths, env);
    built.catch(() => {
      built = undefined; // a failed build is retried by the next caller
    });
  }
  return built;
}

async function doBuild(paths: Paths, env: Env): Promise<string> {
  mkdirSync(paths.distDir, { recursive: true });
  const exe = path.join(paths.distDir, process.platform === "win32" ? "hub.exe" : "hub");
  let res;
  try {
    // GOFLAGS, GOPROXY and the rest of the owner's toolchain setup come from the inherited environment.
    res = await run("go", ["-C", paths.paratechDir, "build", "-o", exe, "./hub/cmd/hub"], {
      env: toolEnv(env),
      timeoutMs: buildTimeoutMs,
    });
  } catch (err) {
    if (err instanceof SpawnError && err.code === "ENOENT") {
      throw new HubBuildFailedError("Go не найден в PATH: hub.exe не собрать");
    }
    throw new HubBuildFailedError("Не удалось запустить go build");
  }
  if (res.code !== 0) {
    const tail = res.stderr.trim().split("\n").slice(-15).join("\n");
    throw new HubBuildFailedError(`Сборка hub.exe не удалась${res.timedOut ? " (истекло время)" : ""}:\n${tail}`);
  }
  return exe;
}
