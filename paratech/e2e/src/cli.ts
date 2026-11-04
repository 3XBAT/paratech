// `npm run e2e [-- <playwright args>]`: reads the runner flags, then runs `playwright test` with everything else.
//   --no-build  reuse paratech/dist/e2e/paratech.exe instead of building it
//   --live      mode LIVE (the real provider; plumbing only, ADR-0035 §1)
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

import { EnvMode } from "./contract.js";
import { findE2EDir } from "./hub/paths.js";
import { EnvNoBuild } from "./sandbox/options.js";

export interface CliArgs {
  noBuild: boolean;
  live: boolean;
  /** Everything but the runner flags, in order: handed to `playwright test` (--grep, spec paths...). */
  rest: string[];
}

export function parseCliArgs(argv: readonly string[]): CliArgs {
  const out: CliArgs = { noBuild: false, live: false, rest: [] };
  for (const arg of argv) {
    if (arg === "--no-build") {
      out.noBuild = true;
    } else if (arg === "--live") {
      out.live = true;
    } else {
      out.rest.push(arg);
    }
  }
  return out;
}

/** The environment of the Playwright process: the inherited one plus the run flags (the inputs of sandbox/options.ts). */
export function playwrightEnv(base: Record<string, string | undefined>, args: CliArgs): Record<string, string | undefined> {
  return { ...base, [EnvMode]: args.live ? "live" : "scripted", [EnvNoBuild]: args.noBuild ? "1" : "0" };
}

function main(): void {
  const args = parseCliArgs(process.argv.slice(2));
  const playwright = createRequire(import.meta.url).resolve("@playwright/test/cli");
  const child = spawn(process.execPath, [playwright, "test", ...args.rest], {
    cwd: findE2EDir(import.meta.url),
    env: playwrightEnv(process.env, args),
    stdio: "inherit",
    shell: false,
  });
  // Ctrl+C reaches Playwright through the shared console; it tears the run down, so this wrapper just waits for it.
  process.on("SIGINT", () => {});
  child.on("error", (err) => {
    console.error(`[e2e] не удалось запустить playwright: ${err.message}`);
    process.exit(2);
  });
  child.on("exit", (code, signal) => process.exit(code ?? (signal ? 1 : 0)));
}

if (process.argv[1] !== undefined && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
