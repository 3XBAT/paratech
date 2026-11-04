import { existsSync, writeFileSync } from "node:fs";
import path from "node:path";

import { toolEnv } from "../hub/env.js";
import { run, SpawnError } from "../hub/proc.js";
import { SandboxEnvironmentError, StartFailureError } from "./errors.js";
import type { SandboxPaths } from "./paths.js";

const buildTimeoutMs = 15 * 60_000;

/**
 * Builds the E2E exe (tags production,e2e, with the speech component) into paratech/dist/e2e, or checks that there is
 * one to reuse. A failed build is a product failure; a missing exe with --no-build is an environment problem.
 * The caller already holds the machine lock (heavy.sh), so nothing here takes it.
 */
export async function ensureExe(p: SandboxPaths, noBuild: boolean, env: Record<string, string | undefined> = process.env): Promise<string> {
  if (noBuild) {
    if (!existsSync(p.exePath)) {
      throw new SandboxEnvironmentError("ExeMissing", `--no-build: нет ${p.exePath}; соберите: npm run e2e (без --no-build)`);
    }
    return p.exePath;
  }

  const powershell = path.join(env.SystemRoot ?? "C:\\Windows", "System32", "WindowsPowerShell", "v1.0", "powershell.exe");
  let res;
  try {
    res = await run(powershell, ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", p.buildScript, "-E2E"], {
      env: toolEnv(env), // go and the build script need the owner's toolchain setup, but never HUB_* or PARATECH_* values
      timeoutMs: buildTimeoutMs,
    });
  } catch (err) {
    if (err instanceof SpawnError) {
      throw new SandboxEnvironmentError("ToolMissing", `Не удалось запустить powershell для сборки (${err.code ?? "ошибка"})`);
    }
    throw err;
  }
  writeFileSync(p.buildLog, `${res.stdout}\n${res.stderr}`);
  if (res.code !== 0 || !existsSync(p.exePath)) {
    const tail = `${res.stdout}\n${res.stderr}`.trim().split(/\r?\n/).slice(-15).join("\n");
    throw new StartFailureError(
      "BuildFailed",
      `Сборка build-alpha.ps1 -E2E не удалась${res.timedOut ? " (истекло время)" : ""}; полный лог: ${p.buildLog}\n${tail}`,
    );
  }
  return p.exePath;
}
