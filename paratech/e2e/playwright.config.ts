import { defineConfig } from "@playwright/test";

// The runner of ADR-0035. The app is started by the global setup (src/sandbox), not by Playwright: no browsers are
// launched or downloaded, the specs only attach over CDP (src/app/cdp.ts). One sandbox, one app, so one worker.
export default defineConfig({
  testDir: "specs",
  workers: 1,
  fullyParallel: false,
  retries: 0, // SCRIPTED has no retries: a flake is a bug (ADR-0035 §6)
  timeout: 60_000, // per test
  // Hard stop of the whole run, setup and exe build included (a full build takes up to 15 min). The SCRIPTED budget of
  // 15 min counts from the start of the tests (the reporter warns), not from here.
  globalTimeout: 45 * 60_000,
  forbidOnly: true,
  reporter: [["list"], ["./src/report/reporter.ts"]],
  globalSetup: "./src/sandbox/setup.ts",
  globalTeardown: "./src/sandbox/teardown.ts",
  // Not inside dist/e2e-run: the global setup wipes that folder, Playwright cleans this one itself at each run.
  outputDir: "../dist/e2e-pw-output",
  use: {
    trace: "retain-on-failure", // best effort: the traces of a CDP-attached page (src/steps)
  },
});
