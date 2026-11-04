import type { ChildProcess } from "node:child_process";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { pathToFileURL } from "node:url";

import type { RunningApp } from "../src/app/launch.js";
import {
  EnvCdpUrl,
  EnvCtlDir,
  EnvDataDir,
  EnvFakeUrl,
  EnvHubKey,
  EnvHubUrl,
  EnvironmentMarker,
  EnvMode,
  EnvRunRoot,
  type RunState,
} from "../src/contract.js";
import type { FakeLLM } from "../src/fakellm/index.js";
import { EnvLiveAuth, EnvLiveBaseURL, EnvLiveKey } from "../src/hub/env.js";
import type { StartedHub } from "../src/hub/index.js";
import { clearRegisteredSecrets, scrubSecrets } from "../src/report/scrub.js";
import { SandboxEnvironmentError, StartFailureError } from "../src/sandbox/errors.js";
import { prepareControlDir, sandboxPaths, type SandboxPaths, wipeRunRoot } from "../src/sandbox/paths.js";
import { currentRun, lastResort, type RunDeps, startRun, stopRun } from "../src/sandbox/run.js";
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

test("a malformed mode is refused before anything is wiped, and is classified as a configuration problem", async () => {
  const { paths, cleanup } = fakeRepo();
  try {
    mkdirSync(paths.runRoot, { recursive: true });
    writeFileSync(path.join(paths.runRoot, "keep.txt"), "keep");
    await assert.rejects(startRun({ [EnvMode]: "prod" }, paths), (err: unknown) => {
      assert.ok(err instanceof Error && err.message.startsWith(EnvironmentMarker), String(err));
      assert.match(err.message, /scripted или live/);
      return true;
    });
    assert.equal(readFileSync(path.join(paths.runRoot, "keep.txt"), "utf8"), "keep");
    assert.equal(readState(paths.runRoot)?.environmentError?.kind, "BadConfig");
    assert.equal(currentRun(), undefined);
  } finally {
    cleanup();
  }
});

// ---- the setup with its outside world replaced ----

const providerVars = [EnvLiveKey, EnvLiveBaseURL, EnvLiveAuth] as const;
const exported = [EnvRunRoot, EnvCtlDir, EnvDataDir, EnvCdpUrl, EnvFakeUrl, EnvHubUrl, EnvHubKey, EnvMode, ...providerVars];

/** Saves the process.env names the setup touches; restore() puts them back and forgets registered secrets. */
function guardEnv(): { restore(): void } {
  const saved = exported.map((name) => [name, process.env[name]] as const);
  return {
    restore() {
      for (const [name, value] of saved) {
        if (value === undefined) {
          delete process.env[name];
        } else {
          process.env[name] = value;
        }
      }
      clearRegisteredSecrets();
    },
  };
}

function stubs(paths: SandboxPaths, over: Partial<RunDeps> = {}) {
  const events: string[] = [];
  const fake = {
    url: "http://127.0.0.1:41001",
    stop: () => Promise.resolve(void events.push("fake.stop")),
  } as unknown as FakeLLM;
  const hub = {
    url: "http://127.0.0.1:41002",
    apiKey: "pthub_stubkey_0123456789",
    stop: () => Promise.resolve(void events.push("hub.stop")),
  } as unknown as StartedHub;
  const app: RunningApp = {
    pid: 4242,
    cdpUrl: "http://127.0.0.1:41003",
    child: {} as ChildProcess,
    stop: () => void events.push("app.stop"),
  };
  const deps: RunDeps = {
    startFakeLLM: () => Promise.resolve(fake),
    startHub: () => Promise.resolve(hub),
    ensureExe: () => Promise.resolve(paths.exePath),
    freeLoopbackPort: () => Promise.resolve(41003),
    launchApp: (o) => {
      o.onSpawn?.(app);
      return Promise.resolve(app);
    },
    wipeRunRoot,
    prepareControlDir,
    ...over,
  };
  return { deps, events, fake, hub, app };
}

function gate<T = void>() {
  let open!: (value: T) => void;
  let fail!: (err: unknown) => void;
  const promise = new Promise<T>((resolve, reject) => {
    open = resolve;
    fail = reject;
  });
  return { promise, open, fail };
}

test("a good start exports the run, drops the provider settings from process.env but still scrubs them, and the teardown stops in order", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    process.env[EnvLiveKey] = "live-key-AAAAAAAA";
    process.env[EnvLiveBaseURL] = "http://gateway.fake.invalid:9999/v1";
    process.env[EnvLiveAuth] = "bearer-auth-ZZZZZZ";
    const { deps, events, hub } = stubs(paths);

    const handle = await startRun(process.env, paths, deps);
    assert.equal(currentRun(), handle);
    assert.equal(process.env[EnvHubKey], hub.apiKey);
    assert.equal(process.env[EnvCdpUrl], "http://127.0.0.1:41003");
    assert.equal(process.env[EnvFakeUrl], "http://127.0.0.1:41001");
    assert.equal(process.env[EnvHubUrl], "http://127.0.0.1:41002");
    for (const name of providerVars) {
      assert.equal(process.env[name], undefined, `${name} must leave process.env (the workers inherit it)`);
    }
    // gone from the environment, still hidden from every report and error text of this process
    assert.equal(scrubSecrets("a live-key-AAAAAAAA b http://gateway.fake.invalid:9999/v1 c bearer-auth-ZZZZZZ", {}), "a [скрыто] b [скрыто] c [скрыто]");
    assert.ok(!readFileSync(path.join(paths.runRoot, "state.json"), "utf8").includes(hub.apiKey));

    assert.deepEqual(await stopRun(), { problems: [], deletedCredentials: [] });
    assert.deepEqual(events, ["app.stop", "hub.stop", "fake.stop"]);
    assert.equal(currentRun(), undefined);
    assert.deepEqual(await stopRun(), { problems: [], deletedCredentials: [] });
  } finally {
    env.restore();
    cleanup();
  }
});

test("the handle is in the slot while the app is still starting: a teardown then stops the spawned app, and the setup ends as interrupted", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    const hold = gate<RunningApp>();
    const spawned = gate();
    const { deps, events } = stubs(paths, {
      launchApp: (o) => {
        o.onSpawn?.({ pid: 4242, cdpUrl: "", child: {} as ChildProcess, stop: () => void events.push("app.stop") });
        spawned.open();
        return hold.promise;
      },
    });

    const starting = startRun({}, paths, deps);
    const outcome = starting.then(
      () => undefined,
      (err: unknown) => err as Error,
    );
    await spawned.promise;
    assert.ok(currentRun(), "the teardown can find the run");

    // Playwright's teardown after Ctrl+C or the global timeout
    assert.deepEqual(await stopRun(), { problems: [], deletedCredentials: [] });
    assert.deepEqual(events, ["app.stop", "hub.stop", "fake.stop"]);

    // the app dies of the kill: launchApp then reports a failed start
    hold.fail(new StartFailureError("SandboxNotIsolated", "приложение завершилось (код 1)"));
    const err = await outcome;
    assert.ok(err instanceof Error);
    assert.ok(!err.message.startsWith(EnvironmentMarker));
    const state = readState(paths.runRoot) as RunState;
    assert.equal(state.startFailure, undefined, "the consequence of the stop is not a product failure");
    assert.equal(state.environmentError, undefined);
    assert.equal(currentRun(), undefined);
    assert.deepEqual(events, ["app.stop", "hub.stop", "fake.stop"], "nothing is stopped twice");
  } finally {
    env.restore();
    cleanup();
  }
});

test("a teardown while the Hub starts: what comes up afterwards is stopped by the setup itself", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    const hold = gate<StartedHub>();
    const asked = gate();
    const { deps, events, hub } = stubs(paths, {
      startHub: () => {
        asked.open();
        return hold.promise;
      },
    });

    const outcome = startRun({}, paths, deps).then(
      () => undefined,
      (err: unknown) => err as Error,
    );
    await asked.promise;
    await stopRun();
    assert.deepEqual(events, ["fake.stop"], "the Hub is not known yet");

    hold.open(hub);
    const err = await outcome;
    assert.ok(err instanceof Error && /прерван/.test(err.message), String(err));
    assert.ok(events.includes("hub.stop"), `the late Hub must be stopped: ${events.join(",")}`);
    assert.notEqual(process.env[EnvHubKey], hub.apiKey, "the key of an interrupted run is never exported");
    assert.deepEqual(events, ["fake.stop", "hub.stop"], "each piece is stopped exactly once");
    assert.equal(readState(paths.runRoot)?.startFailure, undefined);
  } finally {
    env.restore();
    cleanup();
  }
});

test("problems of the setup's own late stop (the second pass after a teardown) reach the failure message", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    const hold = gate<StartedHub>();
    const asked = gate();
    const { deps, hub } = stubs(paths, {
      startHub: () => {
        asked.open();
        return hold.promise;
      },
    });
    const late = { ...hub, stop: () => Promise.reject(new Error("compose down failed")) } as unknown as StartedHub;

    const outcome = startRun({}, paths, deps).then(
      () => undefined,
      (err: unknown) => err as Error,
    );
    await asked.promise;
    await stopRun(); // the first pass: the Hub is not known yet, so it has no problems
    hold.open(late);

    const err = await outcome;
    assert.ok(err instanceof Error, String(err));
    assert.match(err.message, /Остановка тоже прошла с ошибками: .*остановка Hub: compose down failed/);
    assert.ok(!err.message.startsWith(EnvironmentMarker));
  } finally {
    env.restore();
    cleanup();
  }
});

test("the exit guard also runs after a teardown: an app spawned once the teardown has passed is still killed", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  const before = new Set(process.listeners("exit"));
  try {
    const portAsked = gate();
    const portHold = gate<number>();
    const spawned = gate();
    const hold = gate<RunningApp>();
    const { deps, events } = stubs(paths, {
      freeLoopbackPort: () => {
        portAsked.open();
        return portHold.promise;
      },
      launchApp: (o) => {
        o.onSpawn?.({ pid: 4242, cdpUrl: "", child: {} as ChildProcess, stop: () => void events.push("app.stop") });
        spawned.open();
        return hold.promise;
      },
    });

    const outcome = startRun({}, paths, deps).then(
      () => undefined,
      (err: unknown) => err as Error,
    );
    const onExit = process.listeners("exit").find((l) => !before.has(l));
    assert.ok(onExit, "the exit guard is registered");

    await portAsked.promise;
    await stopRun(); // nothing is running yet: only the fake and the Hub stop
    assert.deepEqual(events, ["hub.stop", "fake.stop"]);

    portHold.open(41003);
    await spawned.promise; // spawned after the teardown, before the next checkpoint
    (onExit as () => void)();
    assert.deepEqual(events, ["hub.stop", "fake.stop", "app.stop"], "the guard does not skip a run that is stopping");

    hold.fail(new Error("killed"));
    await outcome;
  } finally {
    for (const l of process.listeners("exit")) {
      if (!before.has(l)) {
        process.removeListener("exit", l);
      }
    }
    env.restore();
    cleanup();
  }
});

test("a fake LLM that cannot bind its port is an environment problem, not a start failure", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    const { deps } = stubs(paths, {
      startFakeLLM: () => Promise.reject(Object.assign(new Error("listen EACCES"), { code: "EACCES" })),
    });
    await assert.rejects(startRun({}, paths, deps), (err: unknown) => {
      assert.ok(err instanceof Error && err.message.startsWith(EnvironmentMarker), String(err));
      assert.match(err.message, /fake LLM.*EACCES/);
      return true;
    });
    const state = readState(paths.runRoot) as RunState;
    assert.equal(state.environmentError?.kind, "PortBusy");
    assert.equal(state.startFailure, undefined);
  } finally {
    env.restore();
    cleanup();
  }
});

test("a wipe that fails (a locked file) is classified, and the previous run's state.json does not survive", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    mkdirSync(paths.runRoot, { recursive: true });
    const old: RunState = {
      mode: "scripted",
      startedAt: "2026-01-01T00:00:00.000Z",
      runRoot: paths.runRoot,
      ctlDir: paths.ctlDir,
      dataDir: paths.dataDir,
      fixturesDir: paths.fixturesDir,
      exePath: paths.exePath,
      cdpUrl: "",
      hubUrl: "",
      fakeUrl: "",
      startFailure: { kind: "BuildFailed", message: "старая сборка не удалась" },
    };
    writeFileSync(path.join(paths.runRoot, "state.json"), JSON.stringify(old));

    const { deps } = stubs(paths, {
      wipeRunRoot: () => {
        throw new SandboxEnvironmentError("SandboxUnavailable", "Не удалось очистить (EBUSY): файл занят");
      },
    });
    await assert.rejects(startRun({}, paths, deps), (err: unknown) => {
      assert.ok(err instanceof Error && err.message.startsWith(EnvironmentMarker), String(err));
      assert.match(err.message, /EBUSY/);
      return true;
    });
    const state = readState(paths.runRoot) as RunState;
    assert.equal(state.environmentError?.kind, "SandboxUnavailable");
    assert.equal(state.startFailure, undefined, "the old state is gone");
    assert.equal(currentRun(), undefined);
  } finally {
    env.restore();
    cleanup();
  }
});

test("a failure of the control dir preparation (not an environment error itself) is classified as SandboxUnavailable", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    const { deps } = stubs(paths, {
      prepareControlDir: () => {
        throw Object.assign(new Error("EPERM: operation not permitted, copyfile"), { code: "EPERM" });
      },
    });
    await assert.rejects(startRun({}, paths, deps), (err: unknown) => {
      assert.ok(err instanceof Error && err.message.startsWith(EnvironmentMarker), String(err));
      assert.match(err.message, /EPERM/);
      return true;
    });
    assert.equal(readState(paths.runRoot)?.environmentError?.kind, "SandboxUnavailable");
  } finally {
    env.restore();
    cleanup();
  }
});

test("when even state.json cannot be written the failure is still classified in the thrown message", async () => {
  const { paths, cleanup } = fakeRepo();
  const env = guardEnv();
  try {
    // a folder in place of the file: it can be neither deleted without recursion nor written
    mkdirSync(path.join(paths.runRoot, "state.json"), { recursive: true });
    writeFileSync(path.join(paths.runRoot, "state.json", "x"), "x");
    const { deps } = stubs(paths, {
      wipeRunRoot: () => {
        throw new SandboxEnvironmentError("SandboxUnavailable", "папка занята");
      },
    });
    await assert.rejects(startRun({}, paths, deps), (err: unknown) => err instanceof Error && err.message.startsWith(`${EnvironmentMarker} папка занята`));
  } finally {
    env.restore();
    cleanup();
  }
});

test("the exit guard stops the spawned app through its own stop() (which kills only a live child)", () => {
  const { paths, cleanup } = fakeRepo();
  try {
    const events: string[] = [];
    const app = { stop: () => void events.push("app.stop") } as unknown as RunningApp;
    const state = { secretsProfileId: undefined } as unknown as RunState;
    lastResort(paths, state, { app });
    lastResort(paths, state, {});
    assert.deepEqual(events, ["app.stop"]);
  } finally {
    cleanup();
  }
});
