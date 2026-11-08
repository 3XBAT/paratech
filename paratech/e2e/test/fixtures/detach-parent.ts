// A stand-in for a Playwright worker, for the test that a detached app outlives the process that spawned it.
// argv: <stub-app.js> <data dir> <ctl dir> <port> <detached|attached>. Prints the app's pid and exits at once.
import path from "node:path";

import { launchApp } from "../../src/app/launch.js";

const [, , stubApp, dataDir, ctlDir, port, mode] = process.argv;
if (!stubApp || !dataDir || !ctlDir || !port) {
  throw new Error("usage: detach-parent <stub-app.js> <data dir> <ctl dir> <port> <detached|attached>");
}
const app = await launchApp({
  exePath: process.execPath,
  exeArgs: [stubApp, "ok"],
  dataDir,
  ctlDir,
  logsDir: path.join(ctlDir, "logs"),
  cdpPort: Number(port),
  startTimeoutMs: 10_000,
  ...(mode === "detached" ? { detached: true } : {}),
});
process.stdout.write(`pid=${app.pid}\n`);
process.exit(0);
