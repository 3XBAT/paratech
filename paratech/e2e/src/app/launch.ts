import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { closeSync, mkdirSync, openSync, readFileSync } from "node:fs";
import path from "node:path";

import { parseSecretsProfileId } from "../sandbox/cmdkey.js";
import { StartFailureError } from "../sandbox/errors.js";
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
}

export interface RunningApp {
  pid: number;
  cdpUrl: string;
  child: ChildProcess;
  /** Hard stop of the exe and its children; idempotent. */
  stop(): void;
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

export function desktopLogPath(dataDir: string): string {
  return path.join(dataDir, "logs", "desktop.log");
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Starts the E2E exe and returns it only when it is provably isolated and reachable: all four log lines, no
 * secrets_profile=default, the debug port answers and is bound to loopback only. Any failure kills the app first.
 */
export async function launchApp(o: LaunchOptions): Promise<RunningApp> {
  mkdirSync(o.logsDir, { recursive: true });
  const out = openSync(path.join(o.logsDir, "app-stdout.log"), "a");
  let child: ChildProcess;
  try {
    child = spawn(o.exePath, [...(o.exeArgs ?? []), ...appArgs(o)], {
      cwd: o.ctlDir,
      env: appEnv(o.env ?? process.env),
      shell: false,
      windowsHide: false, // the window may appear on screen (owner OK, ADR-0035 О6)
      stdio: ["ignore", out, out],
    });
  } finally {
    closeSync(out);
  }
  let spawnError: string | undefined;
  child.on("error", (err: NodeJS.ErrnoException) => (spawnError = err.code ?? "error"));

  const pid = child.pid;
  const cdpUrl = `http://${Loopback}:${o.cdpPort}`;
  let stopped = false;
  const stop = () => {
    if (!stopped && pid !== undefined) {
      stopped = true;
      killProcessTree(pid);
    }
  };
  const fail = (kind: ConstructorParameters<typeof StartFailureError>[0], message: string): never => {
    stop();
    throw new StartFailureError(kind, message);
  };
  if (pid === undefined) {
    return fail("SandboxNotIsolated", `${NotIsolated}: приложение не запустилось (${spawnError ?? "нет pid"})`);
  }

  const log = desktopLogPath(o.dataDir);
  const deadline = Date.now() + (o.startTimeoutMs ?? startTimeoutMs);
  for (;;) {
    const verdict = evaluateStartLog(readSharedText(log));
    if (verdict.defaultProfile) {
      fail("SandboxNotIsolated", `${NotIsolated}: приложение пишет ${DefaultProfileLine}`);
    }
    if (verdict.missing.length === 0) {
      break;
    }
    const exited = child.exitCode !== null || child.signalCode !== null;
    if (exited || Date.now() > deadline) {
      const why = exited ? `приложение завершилось (код ${child.exitCode ?? child.signalCode})` : "строки не появились за отведённое время";
      fail("SandboxNotIsolated", `${NotIsolated}: ${why}; нет строк в desktop.log: ${verdict.missing.map((l) => `«${l}»`).join(", ")}`);
    }
    await sleep(pollMs);
  }

  await waitCdp(cdpUrl, child, fail);
  const listing = runNetstat();
  if (listing === undefined) {
    fail("CdpNotLoopback", `Не удалось проверить, что порт отладки ${o.cdpPort} слушает только loopback (netstat недоступен)`);
  } else {
    const check = checkLoopbackOnly(listing, o.cdpPort);
    if (!check.ok) {
      const where = check.listeners.map((l) => `${l.address}:${l.port}`).join(", ") || "слушателей нет";
      fail("CdpNotLoopback", `Порт отладки ${o.cdpPort} привязан не только к loopback (${where}); приложение остановлено`);
    }
  }
  return { pid, cdpUrl, child, stop };
}

async function waitCdp(
  cdpUrl: string,
  child: ChildProcess,
  fail: (kind: ConstructorParameters<typeof StartFailureError>[0], message: string) => never,
): Promise<void> {
  const deadline = Date.now() + cdpTimeoutMs;
  while (Date.now() < deadline) {
    if (child.exitCode !== null || child.signalCode !== null) {
      fail("CdpNotReady", `Приложение завершилось до ответа порта отладки (код ${child.exitCode ?? child.signalCode})`);
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
  fail("CdpNotReady", `Порт отладки ${cdpUrl} не ответил на /json/version за ${cdpTimeoutMs / 1000} с`);
}

/** The id of the app's isolated Credential Manager profile, from its debug log (undefined until the line is written). */
export function readSecretsProfileId(dataDir: string): string | undefined {
  return parseSecretsProfileId(readSharedText(desktopLogPath(dataDir)));
}
