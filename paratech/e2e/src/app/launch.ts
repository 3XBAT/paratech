import { spawn, spawnSync, type ChildProcess, type SpawnOptions } from "node:child_process";
import { closeSync, mkdirSync, openSync, readFileSync } from "node:fs";
import path from "node:path";

import { parseSecretsProfileId } from "../sandbox/cmdkey.js";
import { SandboxEnvironmentError, StartFailureError, type StartFailureKind } from "../sandbox/errors.js";
import { Loopback } from "../hub/ports.js";
import { checkLoopbackOnly, runNetstat } from "./netstat.js";

/** Lines of <data>/logs/desktop.log that prove a started, isolated E2E app (as build-alpha.ps1 -E2E -Smoke checks). */
export const StartLines = ["instance lock acquired", "secrets_profile=isolated", "e2e mode on", "background work started"] as const;
export const DefaultProfileLine = "secrets_profile=default";
export const NotIsolated = "песочница не изолирована";

export const startTimeoutMs = 90_000;
const cdpTimeoutMs = 30_000;
const pollMs = 250;

export interface StartLogVerdict {
  missing: string[];
  /** The log says secrets_profile=default: the app would use the owner's Credential Manager entries. */
  defaultProfile: boolean;
}

export function evaluateStartLog(log: string): StartLogVerdict {
  return { missing: StartLines.filter((l) => !log.includes(l)), defaultProfile: log.includes(DefaultProfileLine) };
}

type Env = Record<string, string | undefined>;

// Inherited by the app, case-insensitively as Windows does. The first five are the minimal set. LOCALAPPDATA is the one
// addition the app really needs (checked by starting it without each variable, 2026-11-04): config.Load builds the
// default data dir from it, and E2E mode refuses to start when that dir is unknown (checkE2EDirs, exit code 1). The app
// writes nothing there: --data-dir and the WebView2 folder are inside the sandbox. APPDATA, USERPROFILE, ProgramData
// are not needed. Nothing else is inherited: no HUB_*, PARATECH_*, LA_* or token.
const appInherited = new Set(["path", "systemroot", "windir", "temp", "tmp", "localappdata"]);

export function appEnv(base: Env): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [name, value] of Object.entries(base)) {
    if (value !== undefined && appInherited.has(name.toLowerCase())) {
      out[name] = value;
    }
  }
  return out;
}

export interface LaunchOptions {
  exePath: string;
  dataDir: string;
  ctlDir: string;
  logsDir: string; // app-stdout.log goes here
  cdpPort: number;
  /** --e2e-window WxH, only when asked. */
  window?: string;
  env?: Env;
  startTimeoutMs?: number;
  /** Arguments before the app's own: only a test stub (node <script>) needs them. */
  exeArgs?: string[];
  /**
   * Called as soon as the child is spawned, before it is proven isolated: the caller can stop it from then on (an
   * interrupt or a timeout while the app starts). launchApp still kills it itself when the start fails.
   */
  onSpawn?: (app: RunningApp) => void;
  /**
   * A start in a data dir that already has a log (a restart, restart.ts): only the text after this length counts, so the
   * lines of the earlier start cannot prove the new one. Default 0.
   */
  logFrom?: number;
  /**
   * Spawn the app outside the job object of this process. libuv on Windows puts a non-detached child into a job with
   * KILL_ON_JOB_CLOSE, so the app dies with the process that spawned it. The app a spec restarts is spawned by a Playwright
   * worker, and every project gets a new worker: without this the restarted app is killed when its worker exits. The first
   * app is spawned by the main process, which lives for the whole run. A detached child is also unref'd so the worker can
   * exit; stop() still works by pid (taskkill /T /F). Default false.
   */
  detached?: boolean;
  /** Tests only: replaces `taskkill /T /F`. */
  kill?: (pid: number) => void;
  /** Tests only: replaces the netstat listing (undefined = netstat cannot run). */
  netstat?: () => string | undefined;
}

export interface RunningApp {
  pid: number;
  cdpUrl: string;
  child: ChildProcess;
  /** Hard stop of the exe and its children; idempotent; does nothing once the process has exited. */
  stop(): void;
}

type Exited = Pick<ChildProcess, "exitCode" | "signalCode">;

export const hasExited = (child: Exited): boolean => child.exitCode !== null || child.signalCode !== null;

/**
 * The stop of a child. The tree is killed only while the child is alive: after its exit the pid may belong to an
 * unrelated process (the owner's paratech.exe), and `taskkill /PID` would hit that one.
 */
export function createStop(child: Exited, pid: number | undefined, kill: (pid: number) => void = killProcessTree): () => void {
  let stopped = false;
  return () => {
    if (stopped || pid === undefined) {
      return;
    }
    stopped = true;
    if (!hasExited(child)) {
      kill(pid);
    }
  };
}

export function appArgs(o: Pick<LaunchOptions, "dataDir" | "ctlDir" | "cdpPort" | "window">): string[] {
  const args = ["--data-dir", o.dataDir, "--e2e-dir", o.ctlDir, "--e2e-cdp-port", String(o.cdpPort), "--log-level", "debug"];
  if (o.window) {
    args.push("--e2e-window", o.window);
  }
  return args;
}

/** `taskkill /PID <pid> /T /F`: the exe and every child (the WebView2 processes), as build-alpha.ps1 -Smoke stops it hard. */
export function killProcessTree(pid: number): void {
  const root = process.env.SystemRoot ?? "C:\\Windows";
  spawnSync(path.join(root, "System32", "taskkill.exe"), ["/PID", String(pid), "/T", "/F"], { windowsHide: true, stdio: "ignore" });
}

/** Reads a file another process still writes to (Node opens with shared access on Windows); "" if absent or busy. */
export function readSharedText(file: string): string {
  try {
    return readFileSync(file, "utf8");
  } catch {
    return "";
  }
}

/** The part of the log written after `from` characters; a log that got shorter (rotated) is read whole. */
export function freshLog(text: string, from: number): string {
  return from > 0 && text.length >= from ? text.slice(from) : text;
}

export function desktopLogPath(dataDir: string): string {
  return path.join(dataDir, "logs", "desktop.log");
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** Exported for tests: how the app is spawned. */
export function spawnOptions(o: Pick<LaunchOptions, "ctlDir" | "env" | "detached">, out: number): SpawnOptions {
  return {
    cwd: o.ctlDir,
    env: appEnv(o.env ?? process.env),
    shell: false,
    windowsHide: false, // the window may appear on screen (owner OK, ADR-0035 О6)
    stdio: ["ignore", out, out],
    detached: o.detached ?? false,
  };
}

/**
 * Starts the E2E exe and returns it only when it is provably isolated and reachable: all four log lines, no
 * secrets_profile=default, the debug port answers and is bound to loopback only. Any failure kills the app first.
 */
export async function launchApp(o: LaunchOptions): Promise<RunningApp> {
  mkdirSync(o.logsDir, { recursive: true });
  const out = openSync(path.join(o.logsDir, "app-stdout.log"), "a");
  let child: ChildProcess;
  try {
    child = spawn(o.exePath, [...(o.exeArgs ?? []), ...appArgs(o)], spawnOptions(o, out));
  } finally {
    closeSync(out);
  }
  if (o.detached) {
    child.unref(); // the spawning process must be able to exit while the app runs
  }
  let spawnError: string | undefined;
  child.on("error", (err: NodeJS.ErrnoException) => (spawnError = err.code ?? "error"));

  const pid = child.pid;
  const cdpUrl = `http://${Loopback}:${o.cdpPort}`;
  const stop = createStop(child, pid, o.kill);
  const fail = (error: Error): never => {
    stop();
    throw error;
  };
  const startFailure = (kind: StartFailureKind, message: string): StartFailureError => new StartFailureError(kind, message);
  if (pid === undefined) {
    return fail(startFailure("SandboxNotIsolated", `${NotIsolated}: приложение не запустилось (${spawnError ?? "нет pid"})`));
  }
  const app: RunningApp = { pid, cdpUrl, child, stop };
  o.onSpawn?.(app);

  const log = desktopLogPath(o.dataDir);
  const deadline = Date.now() + (o.startTimeoutMs ?? startTimeoutMs);
  for (;;) {
    const verdict = evaluateStartLog(freshLog(readSharedText(log), o.logFrom ?? 0));
    if (verdict.defaultProfile) {
      fail(startFailure("SandboxNotIsolated", `${NotIsolated}: приложение пишет ${DefaultProfileLine}`));
    }
    if (verdict.missing.length === 0) {
      break;
    }
    const exited = hasExited(child);
    if (exited || Date.now() > deadline) {
      const why = exited ? `приложение завершилось (код ${child.exitCode ?? child.signalCode})` : "строки не появились за отведённое время";
      fail(startFailure("SandboxNotIsolated", `${NotIsolated}: ${why}; нет строк в desktop.log: ${verdict.missing.map((l) => `«${l}»`).join(", ")}`));
    }
    await sleep(pollMs);
  }

  await waitCdp(cdpUrl, child, fail);
  const listing = (o.netstat ?? runNetstat)();
  if (listing === undefined) {
    // The machine cannot run its own tool: not a product result.
    fail(new SandboxEnvironmentError("ToolMissing", `Не удалось проверить, что порт отладки ${o.cdpPort} слушает только loopback (netstat недоступен)`));
  } else {
    const check = checkLoopbackOnly(listing, o.cdpPort);
    if (!check.ok) {
      const where = check.listeners.map((l) => `${l.address}:${l.port}`).join(", ") || "слушателей нет";
      fail(startFailure("CdpNotLoopback", `Порт отладки ${o.cdpPort} привязан не только к loopback (${where}); приложение остановлено`));
    }
  }
  return app;
}

async function waitCdp(cdpUrl: string, child: ChildProcess, fail: (error: Error) => never): Promise<void> {
  const deadline = Date.now() + cdpTimeoutMs;
  while (Date.now() < deadline) {
    if (hasExited(child)) {
      fail(new StartFailureError("CdpNotReady", `Приложение завершилось до ответа порта отладки (код ${child.exitCode ?? child.signalCode})`));
    }
    try {
      const res = await fetch(`${cdpUrl}/json/version`, { signal: AbortSignal.timeout(2_000) });
      const body = (await res.json()) as { webSocketDebuggerUrl?: string };
      if (res.status === 200 && typeof body.webSocketDebuggerUrl === "string") {
        return;
      }
    } catch {
      // not listening yet
    }
    await sleep(pollMs);
  }
  fail(new StartFailureError("CdpNotReady", `Порт отладки ${cdpUrl} не ответил на /json/version за ${cdpTimeoutMs / 1000} с`));
}

/** The id of the app's isolated Credential Manager profile, from its debug log (undefined until the line is written). */
export function readSecretsProfileId(dataDir: string): string | undefined {
  return parseSecretsProfileId(readSharedText(desktopLogPath(dataDir)));
}
