// `hub keys create / revoke / list` for the specs (TASK-377): a scenario that needs a SECOND key (the revoked-key check)
// issues one and revokes that one, so the key of the run stays valid for the later packs. The Hub CLI talks to the
// database directly, so this runs the same hub.exe the global setup built, with the database URL the setup exported to
// the workers (EnvHubDatabaseUrl: process memory only, never a file). Output and argument shapes: hub/config/config.go
// (flags) and hub/internal/input/cli/keys.go (output).
import { existsSync } from "node:fs";
import path from "node:path";

import { EnvHubDatabaseUrl } from "../contract.js";
import { scrubSecrets } from "../report/scrub.js";
import { buildHub } from "./build.js";
import { type Env, hubEnv } from "./env.js";
import { BadConfigError, HubKeyFailedError } from "./errors.js";
import { type Paths, repoPaths } from "./paths.js";
import { run, type RunResult } from "./proc.js";

/** A key as the Hub prints it once: pthub_ and url-safe characters. */
export const issuedKeyLine = /^pthub_[A-Za-z0-9_-]+$/;

// config.go: "pseudonymous label of the tester, 1-32 characters of a-z, 0-9, '-' and '_'".
const labelPattern = /^[a-z0-9_-]{1,32}$/;
// The id the Hub prints and takes back ("0123456789abcdef"); kept loose on length so a longer id format still works.
const idPattern = /^[A-Za-z0-9_-]{4,64}$/;

const keysTimeoutMs = 60_000;

export interface IssuedKey {
  id: string;
  key: string;
  label: string;
}

export interface KeyRow {
  id: string;
  label: string;
  status: string;
}

export function issueArgs(label: string): string[] {
  if (!labelPattern.test(label)) {
    throw new BadConfigError("Метка ключа: 1-32 символа a-z, 0-9, «-» и «_»");
  }
  return ["keys", "create", "-label", label];
}

export function revokeArgs(id: string): string[] {
  if (!idPattern.test(id)) {
    throw new BadConfigError("Идентификатор ключа: 4-64 символа a-z, A-Z, 0-9, «-» и «_»");
  }
  return ["keys", "revoke", "-id", id];
}

export const listArgs = (): string[] => ["keys", "list"];

/**
 * The id and the key from the output of `keys create`:
 * `API key <id> issued for "<label>" at <time>.` ... the key alone on the last line. undefined when either is missing.
 */
export function parseIssued(stdout: string): { id: string; key: string } | undefined {
  const lines = stdout.split(/\r?\n/).map((l) => l.trim());
  const key = lines.filter((l) => issuedKeyLine.test(l)).pop();
  const id = /^API key (\S+) issued for /m.exec(stdout)?.[1];
  return key !== undefined && id !== undefined && idPattern.test(id) ? { id, key } : undefined;
}

/** `API key <id> revoked at ...` or `... was already revoked at ...` (repeating the command is not an error). */
export function parseRevoked(stdout: string): { id: string; already: boolean } | undefined {
  const m = /^API key (\S+) (was already revoked|revoked) at /m.exec(stdout);
  return m?.[1] === undefined ? undefined : { id: m[1], already: m[2] === "was already revoked" };
}

/** The rows of `keys list`: ID LABEL STATUS CREATED REVOKED, columns padded with spaces; the header is skipped. */
export function parseKeyList(stdout: string): KeyRow[] {
  const rows: KeyRow[] = [];
  for (const line of stdout.split(/\r?\n/).slice(1)) {
    const cols = line.trim().split(/\s{2,}/);
    if (cols.length >= 3 && cols[0] !== undefined && cols[1] !== undefined && cols[2] !== undefined) {
      rows.push({ id: cols[0], label: cols[1], status: cols[2] });
    }
  }
  return rows;
}

export interface KeyToolDeps {
  /** Where the database URL is read (default process.env). */
  env?: Env;
  /** The Hub binary; default: paratech/dist/e2e/hub.exe the setup built, else a build. */
  hubExe?: () => Promise<string>;
  exec?: (file: string, args: string[], env: Record<string, string>) => Promise<Pick<RunResult, "code" | "stdout" | "stderr" | "timedOut">>;
}

export interface KeyTools {
  issueKey(label: string): Promise<IssuedKey>;
  revokeKey(id: string): Promise<void>;
  listKeys(): Promise<KeyRow[]>;
}

/** The existing hub.exe of the run, else a build (once per process). */
export async function defaultHubExe(paths: Paths = repoPaths(), env: Env = process.env): Promise<string> {
  const exe = path.join(paths.distDir, process.platform === "win32" ? "hub.exe" : "hub");
  return existsSync(exe) ? exe : buildHub(paths, env);
}

/** Key commands against the Hub database of this run. Messages never carry the key or the database URL. */
export function keyTools(deps: KeyToolDeps = {}): KeyTools {
  const env = deps.env ?? process.env;
  const exec = deps.exec ?? ((file, args, e) => run(file, args, { env: e, timeoutMs: keysTimeoutMs }));

  async function call(args: string[]): Promise<string> {
    const databaseUrl = env[EnvHubDatabaseUrl];
    if (!databaseUrl) {
      throw new BadConfigError(`Нет ${EnvHubDatabaseUrl}: ключи Hub доступны только из прогона, который поднял тестовый Hub`);
    }
    const file = await (deps.hubExe ?? (() => defaultHubExe()))();
    const res = await exec(file, args, hubEnv(env, { HUB_DATABASE_URL: databaseUrl, HUB_LOG_LEVEL: "error" }));
    if (res.code !== 0 || res.timedOut) {
      const tail = res.stderr.trim().split(/\r?\n/).slice(-8).join("\n");
      throw new HubKeyFailedError(scrubSecrets(`hub ${args.slice(0, 2).join(" ")} не удалась${res.timedOut ? " (истекло время)" : ""}:\n${tail}`, env));
    }
    return res.stdout;
  }

  return {
    async issueKey(label) {
      const out = await call(issueArgs(label));
      const issued = parseIssued(out);
      if (!issued) {
        throw new HubKeyFailedError("hub keys create не выдал ключ");
      }
      return { ...issued, label };
    },
    async revokeKey(id) {
      const out = await call(revokeArgs(id));
      if (parseRevoked(out) === undefined) {
        throw new HubKeyFailedError(`hub keys revoke: неожиданный ответ ${JSON.stringify(scrubSecrets(out.trim().slice(0, 120), env))}`);
      }
    },
    async listKeys() {
      return parseKeyList(await call(listArgs()));
    },
  };
}
