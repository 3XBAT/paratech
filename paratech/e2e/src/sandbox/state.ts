import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

import { type RunState, StateFileName } from "../contract.js";

/** <run root>/state.json. It never holds a secret (the Hub key lives in process.env only). */
export function stateFile(runRoot: string): string {
  return path.join(runRoot, StateFileName);
}

export function writeState(state: RunState): void {
  writeFileSync(stateFile(state.runRoot), JSON.stringify(state, null, 2));
}

export function readState(runRoot: string): RunState | undefined {
  try {
    return JSON.parse(readFileSync(stateFile(runRoot), "utf8")) as RunState;
  } catch {
    return undefined;
  }
}
