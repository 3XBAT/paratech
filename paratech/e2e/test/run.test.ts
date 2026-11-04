import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { pathToFileURL } from "node:url";

import { EnvironmentMarker, EnvHubKey, EnvMode, type RunState } from "../src/contract.js";
import { sandboxPaths } from "../src/sandbox/paths.js";
import { startRun, stopRun } from "../src/sandbox/run.js";
import { readState } from "../src/sandbox/state.js";

function fakeRepo() {
  const root = mkdtempSync(path.join(os.tmpdir(), "e2e-run-"));
  const e2e = path.join(root, "paratech", "e2e");
  mkdirSync(path.join(e2e, "src"), { recursive: true });
  mkdirSync(path.join(root, "paratech", "dist"), { recursive: true });
  writeFileSync(path.join(e2e, "package.json"), "{}");
  writeFileSync(path.join(e2e, "src", "contract.ts"), "");
  const paths = sandboxPaths(pathToFileURL(path.join(e2e, "src", "sandbox", "paths.ts")).href);
  return { paths, cleanup: () => rmSync(root, { recursive: true, force: true }) };
}

test("a setup that fails on the environment wipes the old run, writes state.json, stops what started and says so", async () => {
  const { paths, cleanup } = fakeRepo();
  try {
    // Leftovers of an earlier run must be gone, the control dir recreated.
    mkdirSync(path.join(paths.runRoot, "old"), { recursive: true });
    writeFileSync(path.join(paths.runRoot, "old", "x.txt"), "x");

    // LIVE without a provider key fails in startHub, after the fake LLM started and before any Docker work.
    const env = { [EnvMode]: "live" };
    await assert.rejects(startRun(env, paths), (err: unknown) => {
      assert.ok(err instanceof Error);
      assert.ok(err.message.startsWith(EnvironmentMarker), err.message);
      assert.match(err.message, /PARATECH_E2E_ANTHROPIC_KEY/);
      return true;
    });

    assert.ok(!existsSync(path.join(paths.runRoot, "old")), "the old run is wiped");
    assert.ok(existsSync(paths.fixturesDir) && existsSync(paths.dataDir), "the control dir is prepared");

    const state = readState(paths.runRoot) as RunState;
    assert.equal(state.environmentError?.kind, "MissingLiveKey");
    assert.equal(state.startFailure, undefined, "not a product failure");
    assert.equal(state.mode, "live");
    assert.match(state.fakeUrl, /^http:\/\/127\.0\.0\.1:\d+$/, "the fake had started");
    assert.equal(state.hubUrl, "");
    assert.ok(!readFileSync(path.join(paths.runRoot, "state.json"), "utf8").includes(EnvHubKey));

    // The fake was stopped by the failed setup.
    await assert.rejects(fetch(`${state.fakeUrl}/__e2e/requests`, { signal: AbortSignal.timeout(2_000) }));
    assert.deepEqual(await stopRun(), { problems: [], deletedCredentials: [] }, "no run to tear down");
    assert.equal(process.env[EnvHubKey], undefined);
  } finally {
    cleanup();
  }
});

test("a malformed mode is refused before anything is touched", async () => {
  const { paths, cleanup } = fakeRepo();
  try {
    mkdirSync(paths.runRoot, { recursive: true });
    writeFileSync(path.join(paths.runRoot, "keep.txt"), "keep");
    await assert.rejects(startRun({ [EnvMode]: "prod" }, paths), /scripted или live/);
    assert.equal(readFileSync(path.join(paths.runRoot, "keep.txt"), "utf8"), "keep");
  } finally {
    cleanup();
  }
});
