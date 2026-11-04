// Playwright globalSetup (main process): sandbox, fake LLM, Hub and the E2E app. Workers get the run through process.env
// and <run root>/state.json (contract.ts). The teardown is globalTeardown (teardown.ts). Playwright registers it before
// the setup runs and calls it also after a failed or interrupted setup, so startRun puts its handle into the global slot
// at once; a setup that fails by itself stops what it started and clears the slot.
import { startRun } from "./run.js";

export default async function globalSetup(): Promise<void> {
  await startRun();
}
