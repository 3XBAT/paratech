// Reads <run root>/state.json written by the global setup. Shared by the reporter and the helpers.
import { readFileSync } from "node:fs";
import path from "node:path";

import { type RunState, StateFileName } from "../contract.js";

/** The state of the run, or undefined when the file is missing or not valid JSON (the setup may not have got that far). */
export function readRunState(runRoot: string): RunState | undefined {
  try {
    const raw: unknown = JSON.parse(readFileSync(path.join(runRoot, StateFileName), "utf8"));
    return typeof raw === "object" && raw !== null && !Array.isArray(raw) ? (raw as RunState) : undefined;
  } catch {
    return undefined;
  }
}
