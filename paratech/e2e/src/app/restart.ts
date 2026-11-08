// A hard stop of the running E2E app and a new start on the SAME sandbox (AC-09: a job survives a crash of the app). The
// Hub and the fake LLM stay up; the data dir, the WebView2 profile, the Credential Manager profile and the debug port are the
// same. The setup (main process) owns the first app; this module runs in a worker, so the new pid goes into state.json
// (RunState.appPid) and the teardown / exit guard of the main process stop it from there (killRestartedApp).
import path from "node:path";

import type { RunState } from "../contract.js";
import { EnvWindow } from "../sandbox/options.js";
import { readState, writeState } from "../sandbox/state.js";
import { desktopLogPath, killProcessTree, launchApp, type LaunchOptions, readSharedText, type RunningApp } from "./launch.js";
import { pathOfPid, sameWinPath } from "./procs.js";

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

export interface RestartDeps {
  launch: (o: LaunchOptions) => Promise<RunningApp>;
  /** The exe path of a pid, "" when no such process runs. */
  pathOfPid: (pid: number) => string;
  kill: (pid: number) => void;
  /** Whether the debug port still answers. */
  portAnswers: (cdpUrl: string) => Promise<boolean>;
  sleep: (ms: number) => Promise<void>;
}

const defaultDeps: RestartDeps = {
  launch: launchApp,
  pathOfPid: (pid) => pathOfPid(pid),
  kill: killProcessTree,
  portAnswers: async (cdpUrl) => {
    try {
      const res = await fetch(`${cdpUrl}/json/version`, { signal: AbortSignal.timeout(1_500) });
      return res.status === 200;
    } catch {
      return false;
    }
  },
  sleep,
};

export class RestartError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RestartError";
  }
}

export interface RestartOptions {
  /** <run root>: where state.json is. */
  runRoot: string;
  /** --e2e-window WxH, as the setup passed it. */
  window?: string;
  env?: Record<string, string | undefined>;
  startTimeoutMs?: number;
  /** How long the old app may take to disappear (default 30 s). */
  goneTimeoutMs?: number;
  /** Tests only: a stub app is started with node and a script. */
  exeArgs?: string[];
  /** Tests only: replaces the exe of state.json. */
  exePath?: string;
  deps?: Partial<RestartDeps>;
}

const goneTimeoutMs = 30_000;
const pollMs = 250;

/** state.json of the run; throws when it is missing or has no app pid (nothing to restart). */
function runningState(runRoot: string): RunState & { appPid: number } {
  const state = readState(runRoot);
  if (!state || typeof state.appPid !== "number") {
    throw new RestartError("state.json не знает pid приложения: перезапускать нечего");
  }
  return { ...state, appPid: state.appPid };
}

/**
 * `taskkill /T /F` of the app tree (the exe, the WebView2 processes and the whisper-cli child), waits until the exe is gone and
 * its debug port is closed, starts the same exe with the same flags on the same data dir and port, waits for the four log
 * lines of the NEW start, and records the new pid. Refuses when the pid in state.json now belongs to another exe.
 */
export async function restartApp(o: RestartOptions): Promise<RunningApp> {
  const d: RestartDeps = { ...defaultDeps, ...o.deps };
  const state = runningState(o.runRoot);
  const exePath = o.exePath ?? state.exePath;

  const running = d.pathOfPid(state.appPid);
  if (running !== "") {
    if (!sameWinPath(running, exePath)) {
      throw new RestartError(`pid ${state.appPid} из state.json теперь чужой процесс (${running}): не трогаю`);
    }
    d.kill(state.appPid);
  }
  const limit = o.goneTimeoutMs ?? goneTimeoutMs;
  const deadline = Date.now() + limit;
  // Gone is: no such process, or the pid was reused by another exe (never ours to wait for, still less to kill).
  const stillRunning = (): boolean => {
    const now = d.pathOfPid(state.appPid);
    return now !== "" && sameWinPath(now, exePath);
  };
  while (stillRunning() || (await d.portAnswers(state.cdpUrl))) {
    if (Date.now() > deadline) {
      throw new RestartError(`приложение (pid ${state.appPid}) не остановилось за ${limit / 1000} с`);
    }
    await d.sleep(pollMs);
  }

  const port = Number(new URL(state.cdpUrl).port);
  // The same window size as the first start (the setup read it from the same variable).
  const wanted = o.window ?? process.env[EnvWindow];
  const window = wanted !== undefined && /^\d{3,4}x\d{3,4}$/.test(wanted) ? wanted : undefined;
  const launch: LaunchOptions = {
    exePath,
    dataDir: state.dataDir,
    ctlDir: state.ctlDir,
    logsDir: path.join(state.ctlDir, "logs"),
    cdpPort: port,
    logFrom: readSharedText(desktopLogPath(state.dataDir)).length,
    env: o.env ?? process.env,
    ...(window ? { window } : {}),
    ...(o.startTimeoutMs ? { startTimeoutMs: o.startTimeoutMs } : {}),
    ...(o.exeArgs ? { exeArgs: o.exeArgs } : {}),
    // This runs in a Playwright worker, and the next project gets a new worker. A non-detached child of the worker is in
    // its job object (KILL_ON_JOB_CLOSE on Windows) and dies when the worker exits, which killed the app before the next
    // pack. Detached, it outlives the worker; the main process stops it by pid (killRestartedApp).
    detached: true,
    // The new process is the teardown's from the moment it exists, even if its start then fails.
    onSpawn: (app) => record(o.runRoot, app.pid),
  };
  const app = await d.launch(launch);
  record(o.runRoot, app.pid);
  return app;
}

function record(runRoot: string, pid: number): void {
  const state = readState(runRoot);
  if (state) {
    writeState({ ...state, appPid: pid, appRestarted: true });
  }
}

/**
 * For the teardown and the exit guard of the main process (synchronous): stops the app a spec started by restartApp, i.e. the
 * pid of state.json when it is not the one the setup started. It is killed only while it still runs from the sandbox's exe.
 * The restarted app is detached from its worker (see restartApp), so nothing but this kill (by pid, the whole tree) ends it
 * when the worker exits; the teardown and the exit guard must therefore always call this.
 */
export function killRestartedApp(
  runRoot: string,
  exePath: string,
  startedPid: number | undefined,
  deps: Pick<RestartDeps, "pathOfPid" | "kill"> = defaultDeps,
): boolean {
  const state = readState(runRoot);
  const pid = state?.appPid;
  if (state?.appRestarted !== true || pid === undefined || pid === startedPid) {
    return false;
  }
  const running = deps.pathOfPid(pid);
  if (running === "" || !sameWinPath(running, exePath)) {
    return false;
  }
  deps.kill(pid);
  return true;
}
