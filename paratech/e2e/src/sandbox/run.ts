import { mkdirSync, rmSync } from "node:fs";

import { desktopLogPath, launchApp, readSecretsProfileId, readSharedText, type RunningApp } from "../app/launch.js";
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
} from "../contract.js";
import { type FakeLLM, startFakeLLM } from "../fakellm/index.js";
import { EnvLiveAuth, EnvLiveBaseURL, EnvLiveKey } from "../hub/env.js";
import { E2EEnvironmentError, isEnvironmentError, isHubFailedError, startHub, type StartedHub } from "../hub/index.js";
import { freeLoopbackPort } from "../hub/ports.js";
import { registerSecrets } from "../report/scrub.js";
import { ensureExe } from "./build.js";
import { deleteProfileCredentials, parseSecretsProfileId } from "./cmdkey.js";
import { isStartFailure, SandboxEnvironmentError } from "./errors.js";
import { readRunOptions, type RunOptions } from "./options.js";
import { prepareControlDir, sandboxPaths, type SandboxPaths, wipeRunRoot } from "./paths.js";
import { stateFile, writeState } from "./state.js";

export interface StopReport {
  /** Cleanup steps that failed; empty on a clean teardown. */
  problems: string[];
  deletedCredentials: string[];
}

export interface RunHandle {
  readonly state: RunState;
  /** Hard stop of the app, then Hub, fake and the profile's Credential Manager entries. Idempotent. */
  stop(): Promise<StopReport>;
}

/** What the setup uses from the outside; tests replace it to run without Docker, Go and the exe. */
export interface RunDeps {
  startFakeLLM: typeof startFakeLLM;
  startHub: typeof startHub;
  ensureExe: typeof ensureExe;
  launchApp: typeof launchApp;
  freeLoopbackPort: typeof freeLoopbackPort;
  wipeRunRoot: typeof wipeRunRoot;
  prepareControlDir: typeof prepareControlDir;
}

const defaultDeps: RunDeps = { startFakeLLM, startHub, ensureExe, launchApp, freeLoopbackPort, wipeRunRoot, prepareControlDir };

const log = (message: string) => console.log(`[e2e] ${message}`);

/** Holds the handle for the global teardown, which is loaded as a separate module instance by some loaders. */
const handleKey = Symbol.for("paratech.e2e.run");
type Slot = { [handleKey]?: RunHandle };
export const currentRun = (): RunHandle | undefined => (globalThis as Slot)[handleKey];

interface Started {
  fake?: FakeLLM;
  hub?: StartedHub;
  app?: RunningApp;
}

/** The setup was stopped (teardown after Ctrl+C or the global timeout) before it came up. Neither «сломано» nor an environment problem. */
class RunInterruptedError extends Error {
  constructor() {
    super("Запуск прогона прерван остановкой раньше, чем приложение поднялось");
    this.name = "RunInterruptedError";
  }
}

/** The provider settings of the owner: the Hub gets them at start, nothing later may see them. */
const providerEnvNames = [EnvLiveKey, EnvLiveBaseURL, EnvLiveAuth] as const;

/**
 * The global setup: fresh sandbox, fake LLM, Hub, E2E exe, isolated and loopback-only. Exports the Env* names of the
 * contract to process.env (the Hub key only there). On any failure it writes state.json, stops what it started and
 * throws: a message starting with EnvironmentMarker for an environment problem, state.startFailure for a product one.
 * The handle is in the global slot from the moment it exists, so a teardown that runs while the setup is still
 * waiting (an interrupt) stops what is already up, and the setup stops what comes up afterwards.
 */
export async function startRun(
  env: Record<string, string | undefined> = process.env,
  p: SandboxPaths = sandboxPaths(),
  deps: RunDeps = defaultDeps,
): Promise<RunHandle> {
  const options = prepareSandbox(env, p, deps);

  const state: RunState = {
    mode: options.mode,
    startedAt: new Date().toISOString(),
    runRoot: p.runRoot,
    ctlDir: p.ctlDir,
    dataDir: p.dataDir,
    fixturesDir: p.fixturesDir,
    exePath: p.exePath,
    cdpUrl: "",
    hubUrl: "",
    fakeUrl: "",
  };
  writeState(state);

  const started: Started = {};
  let stopping: Promise<StopReport> | undefined;
  const stop = () => (stopping ??= stopAll(p, state, started));
  const isStopping = () => stopping !== undefined;
  const handle: RunHandle = { state, stop };
  (globalThis as Slot)[handleKey] = handle;
  // Last resort when the process dies without a teardown (a crash, a kill of the wrapper): the hard kill and the profile
  // cleanup are synchronous. The Hub child has its own exit guard; the compose project is left (README: leftovers).
  const onExit = () => {
    if (!stopping) {
      lastResort(p, state, started);
    }
  };
  process.once("exit", onExit);

  // After every wait: a teardown that ran meanwhile did not know what came up since.
  const checkpoint = async (): Promise<void> => {
    if (stopping) {
      await stopping;
      await stopAll(p, state, started);
      throw new RunInterruptedError();
    }
  };

  try {
    try {
      started.fake = await deps.startFakeLLM();
    } catch (err) {
      const code = (err as NodeJS.ErrnoException).code ?? "ошибка";
      throw new E2EEnvironmentError("PortBusy", `Не удалось запустить fake LLM на 127.0.0.1 (${code}): порт занят или зарезервирован Windows`);
    }
    await checkpoint();
    state.fakeUrl = started.fake.url;

    // The provider settings leave process.env once the Hub has them: the workers inherit it and must never see the key.
    // The reporter (this process) still scrubs the values.
    registerSecrets(providerEnvNames.flatMap((name) => [env[name], process.env[name]]));

    log(`starting the Hub (${options.mode})`);
    started.hub = await deps.startHub({ mode: options.mode, fakeUrl: started.fake.url, env });
    for (const name of providerEnvNames) {
      delete process.env[name];
    }
    await checkpoint();
    state.hubUrl = started.hub.url;

    log(options.noBuild ? `reusing ${p.exePath}` : "building the E2E exe (build-alpha.ps1 -E2E)");
    state.exePath = await deps.ensureExe(p, options.noBuild, env);
    await checkpoint();

    // Picked after the Hub and the fake are bound, so it cannot be theirs.
    const cdpPort = await deps.freeLoopbackPort();
    state.cdpUrl = `http://127.0.0.1:${cdpPort}`;
    writeState(state);

    log(`starting the app (CDP ${state.cdpUrl})`);
    started.app = await deps.launchApp({
      exePath: state.exePath,
      dataDir: p.dataDir,
      ctlDir: p.ctlDir,
      logsDir: p.logsDir,
      cdpPort,
      ...(options.window ? { window: options.window } : {}),
      env,
      onSpawn: (app) => {
        started.app = app; // stoppable from the spawn on, not only once it is proven isolated
      },
    });
    await checkpoint();
    const id = readSecretsProfileId(p.dataDir);
    if (id) {
      state.secretsProfileId = id;
    }
    writeState(state);

    process.env[EnvRunRoot] = p.runRoot;
    process.env[EnvCtlDir] = p.ctlDir;
    process.env[EnvDataDir] = p.dataDir;
    process.env[EnvCdpUrl] = state.cdpUrl;
    process.env[EnvFakeUrl] = state.fakeUrl;
    process.env[EnvHubUrl] = state.hubUrl;
    process.env[EnvHubKey] = started.hub.apiKey; // memory only: never written to state.json, a file or a log
    process.env[EnvMode] = options.mode;
    log("the app is up");
    return handle;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    // An error that arrives after a teardown stopped the run is most likely its consequence (the killed app "crashed").
    const interrupted = err instanceof RunInterruptedError || isStopping();
    if (interrupted) {
      // the teardown reports the stop; state.json keeps what was known
    } else if (isEnvironmentError(err)) {
      state.environmentError = { kind: err.kind, message };
    } else if (isStartFailure(err) || isHubFailedError(err)) {
      state.startFailure = { kind: err.kind, message };
    } else {
      state.startFailure = { kind: "Unexpected", message };
    }
    const id = readSecretsProfileId(p.dataDir);
    if (id) {
      state.secretsProfileId = id;
    }
    writeState(state);
    const report = await stop();
    process.removeListener("exit", onExit);
    if ((globalThis as Slot)[handleKey] === handle) {
      delete (globalThis as Slot)[handleKey]; // stopped and reported here: a teardown has nothing left to do
    }
    const cleanup = report.problems.length > 0 ? `\nОстановка тоже прошла с ошибками: ${report.problems.join("; ")}` : "";
    const wrapped = new Error(`${state.environmentError && !interrupted ? `${EnvironmentMarker} ` : ""}${message}${cleanup}`);
    wrapped.cause = err;
    throw wrapped;
  }
}

/**
 * Everything before the first state: the options, the previous run's leftovers, the wipe. A failure here is an
 * environment problem too (a locked file, a malformed option): it is classified, and state.json is written when the
 * folder allows it. The previous run's state.json is deleted first, so a half-done wipe cannot leave it for the reporter.
 */
function prepareSandbox(env: Record<string, string | undefined>, p: SandboxPaths, deps: RunDeps): RunOptions {
  try {
    rmSync(stateFile(p.runRoot), { force: true });
  } catch {
    // best effort: the reporter also ignores a state older than itself
  }
  try {
    let options: RunOptions;
    try {
      options = readRunOptions(env);
    } catch (err) {
      throw new SandboxEnvironmentError("BadConfig", err instanceof Error ? err.message : String(err));
    }

    // A crashed earlier run leaves its profile's credentials behind; the id is the same for the same folder.
    const previousId = parseSecretsProfileId(readSharedText(desktopLogPath(p.dataDir)));
    const stale = deleteProfileCredentials(previousId);
    if (stale.deleted.length > 0) {
      log(`removed ${stale.deleted.length} Credential Manager entries of the previous run`);
    }

    deps.wipeRunRoot(p);
    deps.prepareControlDir(p);
    return options;
  } catch (err) {
    throw earlyFailure(err, env, p);
  }
}

function earlyFailure(err: unknown, env: Record<string, string | undefined>, p: SandboxPaths): Error {
  const reason = err instanceof Error ? err.message : String(err);
  const kind = isEnvironmentError(err) ? err.kind : "SandboxUnavailable";
  const message = isEnvironmentError(err) ? reason : `Не удалось подготовить песочницу ${p.runRoot}: ${reason}`;
  try {
    mkdirSync(p.runRoot, { recursive: true });
    writeState({
      mode: env[EnvMode] === "live" ? "live" : "scripted",
      startedAt: new Date().toISOString(),
      runRoot: p.runRoot,
      ctlDir: p.ctlDir,
      dataDir: p.dataDir,
      fixturesDir: p.fixturesDir,
      exePath: p.exePath,
      cdpUrl: "",
      hubUrl: "",
      fakeUrl: "",
      environmentError: { kind, message },
    });
  } catch {
    // the folder is the problem; the thrown message still carries it
  }
  const wrapped = new Error(`${EnvironmentMarker} ${message}`);
  wrapped.cause = err;
  return wrapped;
}

/** The global teardown (also called when setup failed half-way). */
export async function stopRun(handle: RunHandle | undefined = currentRun()): Promise<StopReport> {
  if (!handle) {
    return { problems: [], deletedCredentials: [] };
  }
  const report = await handle.stop();
  if ((globalThis as Slot)[handleKey] === handle) {
    delete (globalThis as Slot)[handleKey];
  }
  return report;
}

async function stopAll(p: SandboxPaths, state: RunState, started: Started): Promise<StopReport> {
  const problems: string[] = [];
  const note = (what: string, err: unknown) => problems.push(`${what}: ${err instanceof Error ? err.message : String(err)}`);

  // Taken out of `started`: a second pass (the setup finding more that came up after a teardown) stops only newcomers.
  const { app, hub, fake } = started;
  delete started.app;
  delete started.hub;
  delete started.fake;

  // 1. The app first: it holds the log, the data dir and the WebView2 processes.
  try {
    app?.stop();
  } catch (err) {
    note("остановка приложения", err);
  }
  // 2. Hub (schema, compose project if this run started it), then the fake.
  try {
    await hub?.stop();
  } catch (err) {
    note("остановка Hub", err);
  }
  try {
    await fake?.stop();
  } catch (err) {
    note("остановка fake LLM", err);
  }
  // 3. Only the Credential Manager entries of THIS profile (the id of the app's own log, validated).
  const id = readSecretsProfileId(p.dataDir) ?? state.secretsProfileId;
  const creds = deleteProfileCredentials(id);
  creds.failed.forEach((t) => problems.push(`не удалена запись Credential Manager: ${t}`));
  if (creds.deleted.length > 0) {
    log(`removed ${creds.deleted.length} Credential Manager entries of the profile`);
  }
  return { problems, deletedCredentials: creds.deleted };
}

/** Exported for tests. The kill goes through app.stop(): it kills only a child that is still alive. */
export function lastResort(p: SandboxPaths, state: RunState, started: Pick<Started, "app">): void {
  try {
    started.app?.stop();
    deleteProfileCredentials(readSecretsProfileId(p.dataDir) ?? state.secretsProfileId);
  } catch {
    // nothing more can be done at exit
  }
}
