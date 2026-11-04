import { desktopLogPath, killProcessTree, launchApp, readSecretsProfileId, readSharedText, type RunningApp } from "../app/launch.js";
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
import { EnvLiveKey } from "../hub/env.js";
import { isEnvironmentError, isHubFailedError, startHub, type StartedHub } from "../hub/index.js";
import { freeLoopbackPort } from "../hub/ports.js";
import { ensureExe } from "./build.js";
import { deleteProfileCredentials, parseSecretsProfileId } from "./cmdkey.js";
import { isStartFailure } from "./errors.js";
import { readRunOptions } from "./options.js";
import { prepareControlDir, sandboxPaths, type SandboxPaths, wipeRunRoot } from "./paths.js";
import { writeState } from "./state.js";

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

/**
 * The global setup: fresh sandbox, fake LLM, Hub, E2E exe, isolated and loopback-only. Exports the Env* names of the
 * contract to process.env (the Hub key only there). On any failure it writes state.json, stops what it started and
 * throws: a message starting with EnvironmentMarker for an environment problem, state.startFailure for a product one.
 */
export async function startRun(env: Record<string, string | undefined> = process.env, p: SandboxPaths = sandboxPaths()): Promise<RunHandle> {
  const options = readRunOptions(env);

  // A crashed earlier run leaves its profile's credentials behind; the id is the same for the same folder.
  const previousId = parseSecretsProfileId(readSharedText(desktopLogPath(p.dataDir)));
  const stale = deleteProfileCredentials(previousId);
  if (stale.deleted.length > 0) {
    log(`removed ${stale.deleted.length} Credential Manager entries of the previous run`);
  }

  wipeRunRoot(p);
  prepareControlDir(p);

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
  const handle: RunHandle = { state, stop };
  // Last resort when the process dies without a teardown (Ctrl+C in the wrapper, a crash): the hard kill and the profile
  // cleanup are synchronous. The Hub child has its own exit guard; the compose project is left (README: leftovers).
  const onExit = () => {
    if (!stopping) {
      lastResort(p, state, started);
    }
  };
  process.once("exit", onExit);

  try {
    started.fake = await startFakeLLM();
    state.fakeUrl = started.fake.url;

    log(`starting the Hub (${options.mode})`);
    started.hub = await startHub({ mode: options.mode, fakeUrl: started.fake.url, env });
    state.hubUrl = started.hub.url;
    // The provider key is the Hub's alone: the workers, which inherit process.env, never see it.
    delete process.env[EnvLiveKey];

    log(options.noBuild ? `reusing ${p.exePath}` : "building the E2E exe (build-alpha.ps1 -E2E)");
    state.exePath = await ensureExe(p, options.noBuild, env);

    // Picked after the Hub and the fake are bound, so it cannot be theirs.
    const cdpPort = await freeLoopbackPort();
    state.cdpUrl = `http://127.0.0.1:${cdpPort}`;
    writeState(state);

    log(`starting the app (CDP ${state.cdpUrl})`);
    started.app = await launchApp({
      exePath: state.exePath,
      dataDir: p.dataDir,
      ctlDir: p.ctlDir,
      logsDir: p.logsDir,
      cdpPort,
      ...(options.window ? { window: options.window } : {}),
      env,
    });
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
    (globalThis as Slot)[handleKey] = handle;
    log("the app is up");
    return handle;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    if (isEnvironmentError(err)) {
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
    const cleanup = report.problems.length > 0 ? `\nОстановка тоже прошла с ошибками: ${report.problems.join("; ")}` : "";
    const wrapped = new Error(`${state.environmentError ? `${EnvironmentMarker} ` : ""}${message}${cleanup}`);
    wrapped.cause = err;
    throw wrapped;
  }
}

/** The global teardown (also called when setup failed half-way). */
export async function stopRun(handle: RunHandle | undefined = currentRun()): Promise<StopReport> {
  if (!handle) {
    return { problems: [], deletedCredentials: [] };
  }
  const report = await handle.stop();
  delete (globalThis as Slot)[handleKey];
  return report;
}

async function stopAll(p: SandboxPaths, state: RunState, started: Started): Promise<StopReport> {
  const problems: string[] = [];
  const note = (what: string, err: unknown) => problems.push(`${what}: ${err instanceof Error ? err.message : String(err)}`);

  // 1. The app first: it holds the log, the data dir and the WebView2 processes.
  try {
    started.app?.stop();
  } catch (err) {
    note("остановка приложения", err);
  }
  // 2. Hub (schema, compose project if this run started it), then the fake.
  try {
    await started.hub?.stop();
  } catch (err) {
    note("остановка Hub", err);
  }
  try {
    await started.fake?.stop();
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

function lastResort(p: SandboxPaths, state: RunState, started: Started): void {
  try {
    if (started.app) {
      killProcessTree(started.app.pid);
    }
    deleteProfileCredentials(readSecretsProfileId(p.dataDir) ?? state.secretsProfileId);
  } catch {
    // nothing more can be done at exit
  }
}
