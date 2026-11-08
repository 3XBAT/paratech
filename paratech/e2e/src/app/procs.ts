// Which processes of this machine are the sandbox's: by image name AND by the full path of the exe (a name alone could be
// the owner's own paratech.exe or another whisper-cli). Windows only (Win32_Process through PowerShell); the runner of the
// specs is Windows-only too. The parsing is pure and unit-tested; the query is injectable.
import { execFile, spawnSync } from "node:child_process";
import path from "node:path";

export interface ProcInfo {
  pid: number;
  /** Full path of the exe; "" when Windows would not tell. */
  path: string;
  commandLine: string;
}

/** Runs a PowerShell script and returns its stdout; undefined when PowerShell cannot be run. */
export type PowerShell = (script: string) => string | undefined;

export function runPowerShell(script: string): string | undefined {
  const root = process.env.SystemRoot ?? "C:\\Windows";
  const exe = path.join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe");
  const res = spawnSync(exe, ["-NoProfile", "-NonInteractive", "-Command", script], {
    encoding: "utf8",
    windowsHide: true,
    timeout: 20_000,
    maxBuffer: 4 << 20,
  });
  return res.error || res.status !== 0 ? undefined : res.stdout;
}

/** The same without blocking the event loop (a spec polls while Playwright keeps talking to the app). */
export type PowerShellAsync = (script: string) => Promise<string | undefined>;

export const runPowerShellAsync: PowerShellAsync = (script) =>
  new Promise((resolve) => {
    const root = process.env.SystemRoot ?? "C:\\Windows";
    const exe = path.join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe");
    execFile(
      exe,
      ["-NoProfile", "-NonInteractive", "-Command", script],
      { encoding: "utf8", windowsHide: true, timeout: 20_000, maxBuffer: 4 << 20 },
      (err, stdout) => resolve(err ? undefined : stdout),
    );
  });

/** A process image name is a plain file name: it is put into a WQL filter. */
const imageName = /^[A-Za-z0-9._-]{1,64}$/;

/** `@(...) | ConvertTo-Json` prints one object for one row and an array for several; both are read. */
export function parseProcessJson(text: string): ProcInfo[] {
  const trimmed = text.trim();
  if (trimmed === "") {
    return [];
  }
  const parsed: unknown = JSON.parse(trimmed);
  const rows = Array.isArray(parsed) ? parsed : [parsed];
  const out: ProcInfo[] = [];
  for (const r of rows as Array<Record<string, unknown>>) {
    const pid = r.ProcessId;
    if (typeof pid === "number" && Number.isInteger(pid) && pid > 0) {
      out.push({
        pid,
        path: typeof r.ExecutablePath === "string" ? r.ExecutablePath : "",
        commandLine: typeof r.CommandLine === "string" ? r.CommandLine : "",
      });
    }
  }
  return out;
}

function listScript(name: string): string {
  if (!imageName.test(name)) {
    throw new Error(`bad image name ${JSON.stringify(name)}`);
  }
  return `Get-CimInstance Win32_Process -Filter "Name='${name}'" | Select-Object ProcessId,ExecutablePath,CommandLine | ConvertTo-Json -Compress`;
}

/** The running processes with this image name. Throws when the query cannot be run: "no process" must never mean "could not look". */
export function processesNamed(name: string, run: PowerShell = runPowerShell): ProcInfo[] {
  const out = run(listScript(name));
  if (out === undefined) {
    throw new Error(`cannot list the processes named ${name} (PowerShell failed)`);
  }
  return parseProcessJson(out);
}

export async function processesNamedAsync(name: string, run: PowerShellAsync = runPowerShellAsync): Promise<ProcInfo[]> {
  const out = await run(listScript(name));
  if (out === undefined) {
    throw new Error(`cannot list the processes named ${name} (PowerShell failed)`);
  }
  return parseProcessJson(out);
}

/** The exe path of a pid, "" when no such process is running; throws when the query cannot be run. */
export function pathOfPid(pid: number, run: PowerShell = runPowerShell): string {
  if (!Number.isInteger(pid) || pid <= 0) {
    return "";
  }
  const out = run(`Get-CimInstance Win32_Process -Filter "ProcessId=${pid}" | Select-Object ProcessId,ExecutablePath,CommandLine | ConvertTo-Json -Compress`);
  if (out === undefined) {
    throw new Error(`cannot look up the process ${pid} (PowerShell failed)`);
  }
  return parseProcessJson(out)[0]?.path ?? "";
}

/**
 * Whether two Windows paths are the same file or folder: case-insensitive, `/` and `\` alike. Always win32 rules, whatever the
 * host is: the paths come from Win32_Process, and the unit tests feed Windows literals on any OS.
 */
export function sameWinPath(a: string, b: string): boolean {
  return path.win32.resolve(a).toLowerCase() === path.win32.resolve(b).toLowerCase();
}

/** Whether the exe path lies in the folder (a file directly inside it). Case-insensitive, like Windows. */
export function isInFolder(exePath: string, folder: string): boolean {
  return exePath !== "" && sameWinPath(path.win32.dirname(exePath), folder);
}

/** The processes with this image name that run from `folder` (the sandbox's exe folder): the sandbox's own. */
export function sandboxProcesses(name: string, folder: string, run?: PowerShell): ProcInfo[] {
  return processesNamed(name, run).filter((p) => isInFolder(p.path, folder));
}

export async function sandboxProcessesAsync(name: string, folder: string, run?: PowerShellAsync): Promise<ProcInfo[]> {
  return (await processesNamedAsync(name, run)).filter((p) => isInFolder(p.path, folder));
}
