import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import path from "node:path";

export interface RunOptions {
  env: Record<string, string>;
  cwd?: string;
  timeoutMs: number;
}

export interface RunResult {
  code: number | null;
  stdout: string;
  stderr: string;
  timedOut: boolean;
}

/** Thrown when the command itself cannot be started (ENOENT and the like); carries no environment. */
export class SpawnError extends Error {
  readonly code: string | undefined;

  constructor(file: string, code: string | undefined) {
    super(`cannot start ${path.basename(file)}: ${code ?? "unknown error"}`);
    this.name = "SpawnError";
    this.code = code;
  }
}

/** Runs a command without a shell (argument array) and collects its output; a timeout kills its process tree. */
export function run(file: string, args: string[], opts: RunOptions): Promise<RunResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(file, args, { env: opts.env, cwd: opts.cwd, shell: false, windowsHide: true });
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    child.stdout.setEncoding("utf8").on("data", (d: string) => (stdout += d));
    child.stderr.setEncoding("utf8").on("data", (d: string) => (stderr += d));
    child.stdin.end();

    const timer = setTimeout(() => {
      timedOut = true;
      killTree(child);
    }, opts.timeoutMs);
    child.on("error", (err: NodeJS.ErrnoException) => {
      clearTimeout(timer);
      reject(new SpawnError(file, err.code));
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      resolve({ code, stdout, stderr, timedOut });
    });
  });
}

/** Ends a process and everything it started. On Windows a console child has no signals, so taskkill /T /F. */
export function killTree(child: ChildProcess): void {
  if (child.pid === undefined || child.exitCode !== null || child.signalCode !== null) {
    return;
  }
  if (process.platform === "win32") {
    const root = process.env.SystemRoot ?? "C:\\Windows";
    spawnSync(path.join(root, "System32", "taskkill.exe"), ["/PID", String(child.pid), "/T", "/F"], {
      windowsHide: true,
      stdio: "ignore",
    });
    return;
  }
  child.kill("SIGKILL");
}

/** Resolves when the child has exited (at once if it already has), or after timeoutMs. */
export function waitExit(child: ChildProcess, timeoutMs: number): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, timeoutMs);
    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}
