import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, test } from "node:test";

import { isEnvironmentError } from "../src/hub/index.js";
import { ensureExe } from "../src/sandbox/build.js";
import { isStartFailure } from "../src/sandbox/errors.js";
import type { SandboxPaths } from "../src/sandbox/paths.js";

function fake(script?: string): { p: SandboxPaths; cleanup(): void } {
  const root = mkdtempSync(path.join(os.tmpdir(), "e2e-build-"));
  const p = {
    runRoot: path.join(root, "run"),
    buildLog: path.join(root, "run", "build.log"),
    exePath: path.join(root, "dist", "e2e", "paratech.exe"),
    buildScript: path.join(root, "build-alpha.ps1"),
  } as SandboxPaths;
  mkdirSync(p.runRoot, { recursive: true });
  if (script !== undefined) {
    writeFileSync(p.buildScript, script.replaceAll("<EXE>", p.exePath));
  }
  return { p, cleanup: () => rmSync(root, { recursive: true, force: true }) };
}

describe("--no-build", () => {
  test("reuses an existing exe", async () => {
    const { p, cleanup } = fake();
    try {
      mkdirSync(path.dirname(p.exePath), { recursive: true });
      writeFileSync(p.exePath, "exe");
      assert.equal(await ensureExe(p, true), p.exePath);
    } finally {
      cleanup();
    }
  });

  test("a missing exe is an environment error, not a product failure", async () => {
    const { p, cleanup } = fake();
    try {
      await assert.rejects(ensureExe(p, true), (e: unknown) => isEnvironmentError(e) && e.kind === "ExeMissing" && /--no-build/.test(e.message));
    } finally {
      cleanup();
    }
  });
});

// The stand-ins are PowerShell scripts, run by the same powershell.exe command line as the real build.
describe("build through build-alpha.ps1 -E2E", { skip: process.platform === "win32" ? false : "Windows only (powershell)" }, () => {
  test("passes -E2E, logs the output and returns the exe path", async () => {
    const { p, cleanup } = fake(`param([switch]$E2E)
if (-not $E2E) { exit 2 }
Write-Host 'building the stub'
New-Item -ItemType File -Force -Path '<EXE>' | Out-Null
exit 0
`);
    try {
      assert.equal(await ensureExe(p, false), p.exePath);
      assert.match(readFileSync(p.buildLog, "utf8"), /building the stub/);
      assert.ok(existsSync(p.exePath));
    } finally {
      cleanup();
    }
  });

  test("a failed build is a product failure with the tail of the output", async () => {
    const { p, cleanup } = fake("Write-Host 'compile error: boom'\nexit 1\n");
    try {
      await assert.rejects(ensureExe(p, false), (e: unknown) => {
        assert.ok(isStartFailure(e) && e.kind === "BuildFailed" && !isEnvironmentError(e));
        assert.match(e.message, /compile error: boom/);
        assert.ok(e.message.includes(p.buildLog));
        return true;
      });
    } finally {
      cleanup();
    }
  });

  test("a build that exits 0 without an exe is also a failure", async () => {
    const { p, cleanup } = fake("exit 0\n");
    try {
      await assert.rejects(ensureExe(p, false), (e: unknown) => isStartFailure(e) && e.kind === "BuildFailed");
    } finally {
      cleanup();
    }
  });

  test("the build does not see HUB_*, PARATECH_* values", async () => {
    const { p, cleanup } = fake(`$names = (Get-ChildItem Env: | ForEach-Object { $_.Name }) -join ','
Write-Host "names=$names"
New-Item -ItemType File -Force -Path '<EXE>' | Out-Null
`);
    try {
      await ensureExe(p, false, { ...process.env, HUB_ANTHROPIC_API_KEY: "x", PARATECH_E2E_HUB_KEY: "y" });
      const names = /names=(.*)/.exec(readFileSync(p.buildLog, "utf8"))?.[1] ?? "";
      assert.ok(names.length > 0 && !/(^|,)(HUB_|PARATECH_)/i.test(names), names);
    } finally {
      cleanup();
    }
  });
});
