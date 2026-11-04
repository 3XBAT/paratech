// Playwright globalTeardown (main process): hard stop of the app, Hub, fake and the profile's Credential Manager entries.
import { EnvironmentMarker } from "../contract.js";
import { type StopReport, stopRun } from "./run.js";

/** A teardown that left something behind fails the run as an environment problem (leftovers are not a product result). */
export function throwIfProblems(report: StopReport): void {
  if (report.problems.length > 0) {
    throw new Error(`${EnvironmentMarker} Остановка прогона прошла с ошибками: ${report.problems.join("; ")}`);
  }
}

export default async function globalTeardown(): Promise<void> {
  throwIfProblems(await stopRun());
}
