import { EnvMode } from "../contract.js";

// Inputs of a run, set by the cli wrapper (src/cli.ts) for the Playwright process.
export const EnvNoBuild = "PARATECH_E2E_NO_BUILD"; // "1": reuse paratech/dist/e2e/paratech.exe
export const EnvWindow = "PARATECH_E2E_WINDOW"; // "WxH": --e2e-window of the app

export interface RunOptions {
  mode: "scripted" | "live";
  noBuild: boolean;
  /** Value of --e2e-window, only when asked. */
  window?: string;
}

type Env = Record<string, string | undefined>;

/** Reads the inputs of a run; an unknown mode or a malformed window is refused, not guessed. */
export function readRunOptions(env: Env): RunOptions {
  const mode = env[EnvMode] ?? "scripted";
  if (mode !== "scripted" && mode !== "live") {
    throw new Error(`${EnvMode}: нужен scripted или live`);
  }
  const window = env[EnvWindow];
  if (window !== undefined && window !== "" && !/^\d{3,4}x\d{3,4}$/.test(window)) {
    throw new Error(`${EnvWindow}: нужен размер вида 1280x800`);
  }
  return { mode, noBuild: env[EnvNoBuild] === "1", ...(window ? { window } : {}) };
}
