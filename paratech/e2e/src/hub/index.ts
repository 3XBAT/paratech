import { spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { inspect } from "node:util";

import { buildHub } from "./build.js";
import { captureStream, type Env, type HubMode, hubEnv, LogCapture, providerVars, randomSealKeys, Redactor } from "./env.js";
import { BadConfigError, DatabaseUnavailableError, HubKeyFailedError, HubNotReadyError } from "./errors.js";
import { repoPaths } from "./paths.js";
import { pgExec, PgError, type PgTarget, withSearchPath } from "./pg.js";
import { assertAllowedHubPort, freeLoopbackPort, Loopback } from "./ports.js";
import { acquireDatabase, type DatabaseLease } from "./postgres.js";
import { killTree, run, waitExit } from "./proc.js";

export * from "./errors.js";
export { type Env, type HubMode } from "./env.js";
export { dockerAvailable } from "./postgres.js";

export const DefaultQuotaLimit = 400_000;

const keyTimeoutMs = 60_000;
const defaultReadyTimeoutMs = 60_000;
const readyPollMs = 100;
const exitWaitMs = 10_000;
const issuedKeyLine = /^pthub_[A-Za-z0-9_-]+$/;
const logLevels = new Set(["debug", "info", "warn", "error"]);

export interface StartHubOptions {
  mode: HubMode;
  /** Base URL of the fake Messages API; required in SCRIPTED mode. */
  fakeUrl?: string;
  /** HUB_QUOTA_LIMIT of the run in tokens (default 400000). */
  quotaLimit?: number;
  logLevel?: "debug" | "info" | "warn" | "error";
  /** Where the PARATECH_E2E_* variables are read (default process.env). */
  env?: Env;
  readyTimeoutMs?: number;
}

export interface StartedHub {
  readonly url: string;
  /** The API key of the run; the only secret the caller gets. Not enumerable and masked in JSON. */
  readonly apiKey: string;
  readonly schema: string;
  readonly mode: HubMode;
  /** Captured Hub output, scrubbed of every secret of the run. */
  logs(): string;
  /** Ends the Hub process tree, drops the schema, stops compose if this process started it. Idempotent. */
  stop(): Promise<void>;
}

/** How the harness reaches the database and the Hub binary; tests replace it to run without Docker and Go. */
export interface HubDeps {
  database(env: Env): Promise<DatabaseLease>;
  exec(target: PgTarget, sql: string): Promise<void>;
  /** The Hub binary and the arguments that come before the Hub's own. */
  hubCommand(env: Env): Promise<{ file: string; args: string[] }>;
  freePort(): Promise<number>;
}

export function defaultDeps(): HubDeps {
  const paths = repoPaths();
  return {
    database: (env) => acquireDatabase(env, paths.composeFile),
    exec: (target, sql) => pgExec(target, sql),
    hubCommand: async (env) => ({ file: await buildHub(paths, env), args: [] }),
    freePort: freeLoopbackPort,
  };
}

/** Starts a throwaway real Hub: own schema, own key, free loopback port, provider = the fake or the live one. */
export async function startHub(options: StartHubOptions, deps: HubDeps = defaultDeps()): Promise<StartedHub> {
  const env = options.env ?? process.env;
  const quotaLimit = options.quotaLimit ?? DefaultQuotaLimit;
  if (!Number.isSafeInteger(quotaLimit) || quotaLimit <= 0) {
    throw new BadConfigError("quotaLimit: нужно целое число токенов больше 0");
  }
  const logLevel = options.logLevel ?? "info";
  if (!logLevels.has(logLevel)) {
    throw new BadConfigError("logLevel: нужен debug, info, warn или error");
  }
  const provider = providerVars(options.mode, env, options.fakeUrl); // a missing live key fails before Docker

  const redactor = new Redactor();
  provider.secrets.forEach((s) => redactor.add(s));
  const logs = new LogCapture(redactor);
  const runId = randomBytes(8).toString("hex");
  const schema = `e2e_${runId}`;
  const cleanups: Array<() => Promise<void>> = [];
  let stopping: Promise<void> | undefined;
  const teardown = (): Promise<void> => (stopping ??= runCleanups(cleanups, redactor));

  try {
    const db = await deps.database(env);
    cleanups.push(() => db.release());
    redactor.add(db.target.password);

    await sql(deps, db.target, `CREATE SCHEMA ${schema}`);
    cleanups.push(() => sql(deps, db.target, `DROP SCHEMA IF EXISTS ${schema} CASCADE`));

    const cmd = await deps.hubCommand(env);
    const base = { HUB_DATABASE_URL: withSearchPath(db.adminUrl, schema), HUB_LOG_LEVEL: logLevel };

    const issued = await run(cmd.file, [...cmd.args, "keys", "create", "-label", `e2e-${runId}`], {
      env: hubEnv(env, base),
      timeoutMs: keyTimeoutMs,
    });
    const apiKey = issued.stdout.split(/\r?\n/).map((l) => l.trim()).filter((l) => issuedKeyLine.test(l)).pop();
    redactor.add(apiKey);
    if (issued.code !== 0 || !apiKey) {
      throw new HubKeyFailedError(`hub keys create не выдал ключ:\n${redactor.scrub(lastLines(issued.stderr, 15))}`);
    }

    const port = await deps.freePort();
    assertAllowedHubPort(port);
    const sealKeys = randomSealKeys();
    redactor.add(sealKeys);
    const url = `http://${Loopback}:${port}`;
    const child = spawn(cmd.file, cmd.args, {
      env: hubEnv(env, {
        ...base,
        ...provider.vars,
        HUB_LISTEN_ADDR: `${Loopback}:${port}`,
        HUB_AGENT_SEAL_KEYS: sealKeys,
        HUB_QUOTA_LIMIT: String(quotaLimit),
      }),
      shell: false,
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    child.on("error", () => {}); // reported by waitReady; later errors must not crash the runner
    guardOrphan(child);
    cleanups.push(async () => {
      killTree(child);
      await waitExit(child, exitWaitMs);
      runningHubs.delete(child);
    });
    captureStream(child.stdout, logs);
    captureStream(child.stderr, logs);

    await waitReady(url, child, options.readyTimeoutMs ?? defaultReadyTimeoutMs, logs);

    const hub = {
      url,
      schema,
      mode: options.mode,
      logs: () => logs.text(),
      stop: teardown,
      toJSON: () => ({ url, schema, mode: options.mode, apiKey: "(redacted)" }),
      [inspect.custom]: () => `StartedHub { url: '${url}', schema: '${schema}', mode: '${options.mode}' }`,
    };
    Object.defineProperty(hub, "apiKey", { value: apiKey, enumerable: false });
    return hub as typeof hub & { apiKey: string };
  } catch (err) {
    await teardown().catch(() => {}); // the original failure matters more than a cleanup one
    throw scrubError(err, redactor);
  }
}

// Hub processes not stopped yet. One process 'exit' listener for all of them: a test runner that exits without stop()
// leaves no Hub behind, and many Hubs do not pile up listeners (no MaxListeners warning).
const runningHubs = new Set<ChildProcess>();
let exitGuardInstalled = false;

function guardOrphan(child: ChildProcess): void {
  runningHubs.add(child);
  child.once("exit", () => runningHubs.delete(child));
  if (!exitGuardInstalled) {
    exitGuardInstalled = true;
    process.on("exit", () => runningHubs.forEach((c) => killTree(c)));
  }
}

/** How many Hub processes the shared exit guard still watches (for tests). */
export function guardedHubCount(): number {
  return runningHubs.size;
}

async function sql(deps: HubDeps, target: PgTarget, statement: string): Promise<void> {
  try {
    await deps.exec(target, statement);
  } catch (err) {
    if (err instanceof PgError) {
      throw new DatabaseUnavailableError(`Postgres отклонил «${statement}»: ${err.message}`);
    }
    throw err;
  }
}

/** Polls /readyz until 200; a Hub that exits or stays unready is HubNotReady with the scrubbed log tail. */
async function waitReady(url: string, child: ChildProcess, timeoutMs: number, logs: LogCapture): Promise<void> {
  let spawnError: string | undefined;
  child.once("error", (err: NodeJS.ErrnoException) => (spawnError = err.code ?? "error"));
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (spawnError) {
      throw new HubNotReadyError(`Hub не запустился (${spawnError})`);
    }
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new HubNotReadyError(`Hub завершился при запуске (код ${child.exitCode ?? child.signalCode}):\n${logs.tail()}`);
    }
    try {
      const res = await fetch(`${url}/readyz`, { signal: AbortSignal.timeout(2_000) });
      await res.body?.cancel();
      if (res.status === 200) {
        return;
      }
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, readyPollMs));
  }
  throw new HubNotReadyError(`Hub не ответил на /readyz за ${Math.round(timeoutMs / 1000)} с:\n${logs.tail()}`);
}

async function runCleanups(cleanups: Array<() => Promise<void>>, redactor: Redactor): Promise<void> {
  const failures: string[] = [];
  for (const step of cleanups.splice(0).reverse()) {
    try {
      await step();
    } catch (err) {
      failures.push(redactor.scrub(err instanceof Error ? err.message : String(err)));
    }
  }
  if (failures.length > 0) {
    throw new Error(`Остановка тестового Hub прошла с ошибками:\n${failures.join("\n")}`);
  }
}

function scrubError(err: unknown, redactor: Redactor): unknown {
  if (err instanceof Error) {
    err.message = redactor.scrub(err.message);
    if (err.stack) {
      err.stack = redactor.scrub(err.stack);
    }
  }
  return err;
}

function lastLines(text: string, n: number): string {
  return text.trim().split(/\r?\n/).slice(-n).join("\n");
}
