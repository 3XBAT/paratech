// Playwright globalSetup (main process): sandbox, fake LLM, Hub and the E2E app. Workers get the run through process.env
// and <run root>/state.json (contract.ts). The teardown is globalTeardown (teardown.ts); a failed setup stops what it
// started itself, because Playwright does not run the teardown then.
import { startRun } from "./run.js";

export default async function globalSetup(): Promise<void> {
  await startRun();
}
