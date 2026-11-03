import { DockerMissingError, DockerNotRunningError, DatabaseUnavailableError, PortBusyError } from "./errors.js";
import { type Env, EnvE2EPgPort, EnvHubPgPort, EnvTestDatabaseURL, toolEnv } from "./env.js";
import { parsePgUrl, type PgTarget } from "./pg.js";
import { Loopback, loopbackPortBusy, parsePort } from "./ports.js";
import { run, SpawnError } from "./proc.js";

// Own compose project: own container, network and volume, so the owner's stack (project "hub") is never touched.
export const ComposeProject = "paratech-e2e";
export const DefaultPgPort = 55432;

// Credentials of the dev-only database of hub/docker-compose.yml (reachable from localhost only).
const composeUser = "hub";
const composePassword = "hub-dev-only";
const composeDatabase = "hub";

const dockerCheckTimeoutMs = 30_000;
const composeUpTimeoutMs = 300_000; // the first run may pull the image
const composeTimeoutMs = 120_000;

/** A database the harness may create schemas in; release() stops what the harness started. */
export interface DatabaseLease {
  adminUrl: string;
  target: PgTarget;
  release(): Promise<void>;
}

/** The Postgres port of the E2E compose project: PARATECH_E2E_PG_PORT, else HUB_PG_PORT, else 55432. */
export function composePort(env: Env): number {
  const own = env[EnvE2EPgPort];
  if (own) {
    return parsePort(own, EnvE2EPgPort);
  }
  const hub = env[EnvHubPgPort];
  return hub ? parsePort(hub, EnvHubPgPort) : DefaultPgPort;
}

/** Whether the Docker engine answers; false when docker is missing or not running. */
export async function dockerAvailable(env: Env = process.env): Promise<boolean> {
  try {
    await checkDocker(env);
    return true;
  } catch {
    return false;
  }
}

async function checkDocker(env: Env): Promise<void> {
  let res;
  try {
    res = await run("docker", ["info", "--format", "{{.ServerVersion}}"], {
      env: toolEnv(env),
      timeoutMs: dockerCheckTimeoutMs,
    });
  } catch (err) {
    if (err instanceof SpawnError && err.code === "ENOENT") {
      throw new DockerMissingError();
    }
    throw new DockerNotRunningError();
  }
  if (res.code !== 0 || res.stdout.trim() === "") {
    throw new DockerNotRunningError();
  }
}

/** HUB_TEST_DATABASE_URL when set (compose is skipped), else the postgres service of the E2E compose project. */
export async function acquireDatabase(env: Env, composeFile: string): Promise<DatabaseLease> {
  const url = env[EnvTestDatabaseURL];
  if (url) {
    return { adminUrl: url, target: parsePgUrl(url, EnvTestDatabaseURL), release: async () => {} };
  }
  return acquireCompose(env, composeFile);
}

// One compose stack per Node process, shared by every Hub of the process and stopped with the last one.
let shared: { ready: Promise<{ port: number; started: boolean }>; refs: number; composeFile: string; env: Env } | undefined;

async function acquireCompose(env: Env, composeFile: string): Promise<DatabaseLease> {
  if (!shared) {
    shared = { ready: startCompose(env, composeFile), refs: 0, composeFile, env };
  }
  const state = shared;
  state.refs++;
  let port: number;
  try {
    ({ port } = await state.ready);
  } catch (err) {
    state.refs--;
    if (shared === state && state.refs === 0) {
      shared = undefined;
    }
    throw err;
  }

  const adminUrl = `postgres://${composeUser}:${composePassword}@${Loopback}:${port}/${composeDatabase}?sslmode=disable`;
  let released = false;
  return {
    adminUrl,
    target: { host: Loopback, port, user: composeUser, password: composePassword, database: composeDatabase },
    release: async () => {
      if (released) {
        return;
      }
      released = true;
      state.refs--;
      if (state.refs > 0) {
        return;
      }
      if (shared === state) {
        shared = undefined;
      }
      if ((await state.ready).started) {
        await composeDown(state.env, state.composeFile);
      }
    },
  };
}

function compose(env: Env, composeFile: string, args: string[], timeoutMs: number, vars: Record<string, string> = {}) {
  return run("docker", ["compose", "-p", ComposeProject, "-f", composeFile, ...args], {
    env: toolEnv(env, vars),
    timeoutMs,
  });
}

/** Reuses a running postgres of the E2E project (it is ours), else starts it on a port that must be free. */
async function startCompose(env: Env, composeFile: string): Promise<{ port: number; started: boolean }> {
  await checkDocker(env);
  const wanted = composePort(env);

  const ps = await compose(env, composeFile, ["ps", "--status", "running", "--quiet", "postgres"], composeTimeoutMs);
  if (ps.code === 0 && ps.stdout.trim() !== "") {
    const mapped = await compose(env, composeFile, ["port", "postgres", "5432"], composeTimeoutMs);
    const port = Number(mapped.stdout.trim().split(":").pop());
    if (mapped.code === 0 && Number.isInteger(port) && port > 0) {
      return { port, started: false };
    }
  }

  if (await loopbackPortBusy(wanted)) {
    throw new PortBusyError(wanted);
  }
  const up = await compose(env, composeFile, ["up", "-d", "--wait", "postgres"], composeUpTimeoutMs, {
    [EnvHubPgPort]: String(wanted),
  });
  if (up.code !== 0) {
    await composeDown(env, composeFile).catch(() => {});
    const tail = up.stderr.trim().split("\n").slice(-5).join("\n");
    throw new DatabaseUnavailableError(
      `Не удалось запустить Postgres (docker compose -p ${ComposeProject})${up.timedOut ? ": истекло время" : ""}\n${tail}`,
    );
  }
  return { port: wanted, started: true };
}

/** Stops and removes the E2E project only (its container, network and volume); nothing else is ever stopped. */
async function composeDown(env: Env, composeFile: string): Promise<void> {
  const res = await compose(env, composeFile, ["down", "-v"], composeTimeoutMs);
  if (res.code !== 0) {
    throw new DatabaseUnavailableError(`Не удалось остановить docker compose -p ${ComposeProject}`);
  }
}
