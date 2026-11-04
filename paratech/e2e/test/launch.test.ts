import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, test } from "node:test";
import { fileURLToPath } from "node:url";

import { appArgs, appEnv, evaluateStartLog, launchApp, type LaunchOptions, NotIsolated, StartLines } from "../src/app/launch.js";
import { freeLoopbackPort } from "../src/hub/ports.js";
import { isStartFailure } from "../src/sandbox/errors.js";

const stubApp = path.join(path.dirname(fileURLToPath(import.meta.url)), "fixtures", "stub-app.js");
const all = StartLines.join("\n");

describe("start log verdict", () => {
  test("all four lines present", () => {
    assert.deepEqual(evaluateStartLog(all), { missing: [], defaultProfile: false });
  });

  test("names every missing line", () => {
    const v = evaluateStartLog("instance lock acquired\nsecrets_profile=isolated");
    assert.deepEqual(v.missing, ["e2e mode on", "background work started"]);
  });

  test("secrets_profile=default is flagged even when the other lines are there", () => {
    assert.equal(evaluateStartLog(`${all}\nsecrets_profile=default`).defaultProfile, true);
    assert.deepEqual(evaluateStartLog("secrets_profile=default").missing.length, 4);
  });
});

describe("app arguments and environment", () => {
  test("the flags of ADR-0035 §3, the window only when asked", () => {
    const base = { dataDir: "D:\\d", ctlDir: "D:\\c", cdpPort: 9222 };
    assert.deepEqual(appArgs(base), ["--data-dir", "D:\\d", "--e2e-dir", "D:\\c", "--e2e-cdp-port", "9222", "--log-level", "debug"]);
    assert.deepEqual(appArgs({ ...base, window: "1280x800" }).slice(-2), ["--e2e-window", "1280x800"]);
  });

  test("the app inherits the minimal set only: no HUB_*, PARATECH_*, LA_* or tokens", () => {
    const env = appEnv({
      Path: "C:\\bin",
      SystemRoot: "C:\\Windows",
      windir: "C:\\Windows",
      TEMP: "C:\\t",
      TMP: "C:\\t",
      LOCALAPPDATA: "C:\\u\\AppData\\Local",
      APPDATA: "C:\\u\\AppData\\Roaming",
      USERPROFILE: "C:\\u",
      HUB_ANTHROPIC_API_KEY: "x",
      PARATECH_E2E_HUB_KEY: "pthub_x",
      PARATECH_E2E_ANTHROPIC_KEY: "x",
      LA_DATA_DIR: "C:\\owner",
      LA_HEADLESS: "1",
      GITHUB_TOKEN: "x",
      GOFLAGS: "-p=4",
    });
    assert.deepEqual(Object.keys(env).sort(), ["LOCALAPPDATA", "Path", "SystemRoot", "TEMP", "TMP", "windir"]);
  });
});

// The stub is a node script, started like the exe; launchApp's netstat and taskkill checks are Windows tools.
const onWindows = process.platform === "win32";

describe("launchApp with a stub app", { skip: onWindows ? false : "Windows only (netstat, taskkill)" }, () => {
  async function options(behaviour: string): Promise<{ o: LaunchOptions; root: string; cleanup(): void }> {
    const root = mkdtempSync(path.join(os.tmpdir(), "e2e-launch-"));
    const o: LaunchOptions = {
      exePath: process.execPath,
      exeArgs: [stubApp, behaviour],
      dataDir: path.join(root, "ctl", "data"),
      ctlDir: path.join(root, "ctl"),
      logsDir: path.join(root, "ctl", "logs"),
      cdpPort: await freeLoopbackPort(),
      startTimeoutMs: 3_000,
      env: {
        ...process.env,
        HUB_ANTHROPIC_API_KEY: "must-not-reach-the-app",
        PARATECH_E2E_HUB_KEY: "must-not-reach-the-app",
        LA_DATA_DIR: "C:\\owner-data",
      },
    };
    return { o, root, cleanup: () => rmSync(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 }) };
  }

  const alive = (pid: number): boolean => {
    try {
      process.kill(pid, 0);
      return true;
    } catch {
      return false;
    }
  };
  const gone = async (pid: number): Promise<boolean> => {
    for (let i = 0; i < 50; i++) {
      if (!alive(pid)) {
        return true;
      }
      await new Promise((r) => setTimeout(r, 100));
    }
    return false;
  };

  test("an isolated app with a loopback debug port starts; it got a minimal environment and the flags", async () => {
    const { o, root, cleanup } = await options("ok");
    const app = await launchApp(o);
    try {
      assert.equal(app.cdpUrl, `http://127.0.0.1:${o.cdpPort}`);
      const res = await fetch(`${app.cdpUrl}/json/version`);
      assert.equal(res.status, 200);

      const out = readFileSync(path.join(root, "ctl", "logs", "app-stdout.log"), "utf8");
      const names = /env-names=(.*)/.exec(out)?.[1]?.split(",") ?? [];
      assert.ok(names.length > 0 && !names.some((n) => /^(HUB_|PARATECH_|LA_)/i.test(n)), names.join(","));
      assert.ok(!out.includes("must-not-reach-the-app"));
      assert.deepEqual(JSON.parse(/argv=(.*)/.exec(out)?.[1] ?? "[]"), appArgs(o));
    } finally {
      app.stop();
      assert.ok(await gone(app.pid), "stop() ends the process");
      app.stop(); // idempotent
      cleanup();
    }
  });

  test("a crash before the lines is «песочница не изолирована», names the missing lines, and is a start failure", async () => {
    const { o, cleanup } = await options("crash");
    try {
      await assert.rejects(launchApp(o), (err: unknown) => {
        assert.ok(isStartFailure(err) && err.kind === "SandboxNotIsolated");
        assert.ok(err.message.startsWith(NotIsolated), err.message);
        assert.match(err.message, /код 3/);
        assert.match(err.message, /«e2e mode on»/);
        assert.match(err.message, /«background work started»/);
        assert.ok(!err.message.includes("«instance lock acquired»"));
        return true;
      });
    } finally {
      cleanup();
    }
  });

  test("lines that never come: the app is killed at the timeout", async () => {
    const { o, root, cleanup } = await options("hang");
    o.startTimeoutMs = 1_500;
    try {
      await assert.rejects(launchApp(o), (err: unknown) => isStartFailure(err) && /не появились/.test(err.message));
      const out = readFileSync(path.join(root, "ctl", "logs", "app-stdout.log"), "utf8");
      assert.match(out, /env-names=/);
    } finally {
      cleanup();
    }
  });

  test("secrets_profile=default kills the app at once", async () => {
    const { o, cleanup } = await options("default-profile");
    try {
      await assert.rejects(launchApp(o), (err: unknown) => {
        assert.ok(isStartFailure(err) && err.kind === "SandboxNotIsolated");
        assert.match(err.message, /secrets_profile=default/);
        return true;
      });
    } finally {
      cleanup();
    }
  });

  test("a debug port bound to 0.0.0.0 kills the app and aborts", async () => {
    const { o, cleanup } = await options("wildcard");
    try {
      await assert.rejects(launchApp(o), (err: unknown) => {
        assert.ok(isStartFailure(err) && err.kind === "CdpNotLoopback");
        assert.match(err.message, /0\.0\.0\.0/);
        return true;
      });
      const res = await fetch(`http://127.0.0.1:${o.cdpPort}/json/version`, { signal: AbortSignal.timeout(2_000) }).catch(() => undefined);
      assert.equal(res, undefined, "the stub app is gone, the port no longer answers");
    } finally {
      cleanup();
    }
  });
});
