import { spawnSync } from "node:child_process";
import path from "node:path";

export interface Listener {
  address: string; // without brackets
  port: number;
  pid: number;
}

/** Splits "127.0.0.1:9222", "[::1]:9222" or "[fe80::1%4]:9222" into address and port; undefined if it is neither. */
function splitEndpoint(token: string): { address: string; port: number } | undefined {
  const colon = token.lastIndexOf(":");
  if (colon <= 0 || !/^\d+$/.test(token.slice(colon + 1))) {
    return undefined;
  }
  const address = token.slice(0, colon).replace(/^\[|\]$/g, "");
  return address === "" ? undefined : { address, port: Number(token.slice(colon + 1)) };
}

/**
 * The listening TCP sockets in the output of `netstat -ano -p tcp`. The header and the state word are localized
 * («LISTENING», «ОЖИДАНИЕ»), so a row is recognised by its numbers: "TCP <local addr:port> <foreign addr:port> [state] <pid>",
 * and it listens when the foreign port is 0.
 */
export function parseNetstatListeners(output: string): Listener[] {
  const listeners: Listener[] = [];
  for (const line of output.split(/\r?\n/)) {
    const cols = line.trim().split(/\s+/);
    if (cols.length < 4 || !/^tcp/i.test(cols[0] ?? "")) {
      continue;
    }
    const local = splitEndpoint(cols[1] ?? "");
    const foreign = splitEndpoint(cols[2] ?? "");
    const pidText = cols[cols.length - 1] ?? "";
    if (!local || !foreign || foreign.port !== 0 || !/^\d+$/.test(pidText)) {
      continue;
    }
    listeners.push({ address: local.address, port: local.port, pid: Number(pidText) });
  }
  return listeners;
}

export function isLoopbackAddress(address: string): boolean {
  return address === "127.0.0.1" || address === "::1";
}

export type PortCheck =
  | { ok: true; listeners: Listener[] }
  | { ok: false; reason: "not-listening" | "not-loopback"; listeners: Listener[] };

/**
 * Every listener on `port` must be on 127.0.0.1 or ::1. No listener at all is a failure too: the port answered, so
 * the check cannot say where it is bound. The pid is not compared with the exe's: Chromium opens the debug port in the
 * msedgewebview2 browser process, a child of the app.
 */
export function checkLoopbackOnly(output: string, port: number): PortCheck {
  const listeners = parseNetstatListeners(output).filter((l) => l.port === port);
  if (listeners.length === 0) {
    return { ok: false, reason: "not-listening", listeners };
  }
  if (!listeners.every((l) => isLoopbackAddress(l.address))) {
    return { ok: false, reason: "not-loopback", listeners };
  }
  return { ok: true, listeners };
}

/**
 * Runs netstat (system folder, no shell) for IPv4 and IPv6 sockets (`-p tcp` alone lists IPv4 only, so a listener on
 * [::] would be missed); undefined when it cannot be run.
 */
export function runNetstat(): string | undefined {
  const root = process.env.SystemRoot ?? "C:\\Windows";
  const exe = path.join(root, "System32", "netstat.exe");
  const outputs: string[] = [];
  for (const proto of ["tcp", "tcpv6"]) {
    const res = spawnSync(exe, ["-ano", "-p", proto], { windowsHide: true, encoding: "latin1", timeout: 30_000 });
    if (res.status !== 0) {
      return undefined;
    }
    outputs.push(res.stdout);
  }
  return outputs.join("\n");
}
