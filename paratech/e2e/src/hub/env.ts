import { randomBytes } from "node:crypto";

import { BadConfigError, MissingLiveKeyError } from "./errors.js";

export type Env = Record<string, string | undefined>;

export type HubMode = "scripted" | "live";

// Variables of the owner's environment the harness reads.
export const EnvLiveKey = "PARATECH_E2E_ANTHROPIC_KEY";
export const EnvLiveBaseURL = "PARATECH_E2E_ANTHROPIC_BASE_URL";
export const EnvLiveAuth = "PARATECH_E2E_ANTHROPIC_AUTH";
export const EnvE2EPgPort = "PARATECH_E2E_PG_PORT";
export const EnvHubPgPort = "HUB_PG_PORT";
export const EnvTestDatabaseURL = "HUB_TEST_DATABASE_URL";

// A value the Hub needs to start in SCRIPTED mode; the fake never checks it.
export const DummyProviderKey = "e2e-dummy-provider-key";

const redactedText = "(redacted)";

// The Hub gets only these inherited variables (names compared case-insensitively, as Windows does), plus its own
// HUB_* settings: no stray HUB_* or PARATECH_* value of the owner, no token of another tool reaches it.
const hubInherited = new Set(["path", "systemroot", "windir", "temp", "tmp", "tmpdir", "home"]);

// Tools (go, docker) need the owner's toolchain setup (GOFLAGS, GOPROXY, DOCKER_HOST, USERPROFILE...), so they inherit
// everything except the variables of the Hub and of the harness, the live provider key included.
const toolDenied = /^(HUB_|PARATECH_)/i;

function pick(base: Env, keep: (name: string) => boolean): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [name, value] of Object.entries(base)) {
    if (value !== undefined && keep(name)) {
      out[name] = value;
    }
  }
  return out;
}

/** Environment of a spawned Hub process: the minimal inherited part plus vars. */
export function hubEnv(base: Env, vars: Record<string, string>): Record<string, string> {
  return { ...pick(base, (n) => hubInherited.has(n.toLowerCase())), ...vars };
}

/** Environment of go and docker: everything inherited except HUB_* and PARATECH_*, plus vars. */
export function toolEnv(base: Env, vars: Record<string, string> = {}): Record<string, string> {
  return { ...pick(base, (n) => !toolDenied.test(n)), ...vars };
}

/** Provider settings of the Hub for a mode, and the secrets they contain. */
export interface ProviderVars {
  vars: Record<string, string>;
  secrets: string[];
}

export function providerVars(mode: HubMode, env: Env, fakeUrl: string | undefined): ProviderVars {
  if (mode === "scripted") {
    if (!fakeUrl) {
      throw new BadConfigError("Режим SCRIPTED: не задан адрес fake Messages API (fakeUrl)");
    }
    assertLoopbackHttp(fakeUrl);
    return {
      vars: {
        HUB_ANTHROPIC_API_KEY: DummyProviderKey,
        HUB_ANTHROPIC_BASE_URL: fakeUrl,
        HUB_PROVIDER_STREAM: "false",
      },
      secrets: [],
    };
  }
  if (mode !== "live") {
    throw new BadConfigError("Неизвестный режим Hub: нужен scripted или live");
  }

  const key = env[EnvLiveKey];
  if (!key) {
    throw new MissingLiveKeyError();
  }
  const vars: Record<string, string> = { HUB_ANTHROPIC_API_KEY: key };
  const baseURL = env[EnvLiveBaseURL];
  if (baseURL) {
    vars.HUB_ANTHROPIC_BASE_URL = baseURL;
  }
  const auth = env[EnvLiveAuth];
  if (auth) {
    vars.HUB_ANTHROPIC_AUTH = auth;
  }
  // The auth mode is not secret, but a key pasted into the wrong variable must not be printed either.
  return { vars, secrets: [key, ...(auth ? [auth] : [])] };
}

function assertLoopbackHttp(raw: string): void {
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    throw new BadConfigError("Адрес fake Messages API не является URL");
  }
  const loopback = u.hostname === "127.0.0.1" || u.hostname === "localhost" || u.hostname === "[::1]";
  if (u.protocol !== "http:" || !loopback) {
    throw new BadConfigError("Fake Messages API должен слушать http на 127.0.0.1");
  }
}

/** A fresh HUB_AGENT_SEAL_KEYS value: version 1 with a random 32-byte secret. */
export function randomSealKeys(): string {
  return "1:" + randomBytes(32).toString("hex");
}

/** Replaces every known secret in a text. Secrets are added as they become known. */
export class Redactor {
  private readonly secrets = new Set<string>();

  add(secret: string | undefined): void {
    if (secret) {
      this.secrets.add(secret);
    }
  }

  scrub(text: string): string {
    let out = text;
    for (const s of this.secrets) {
      out = out.split(s).join(redactedText);
    }
    return out;
  }
}

/**
 * Captures process output line by line, scrubbing each complete line, and keeps the last maxBytes. Scrubbing whole
 * lines means a secret split across chunks or cut by the size limit is never stored in part.
 */
export class LogCapture {
  private readonly lines: string[] = [];
  private size = 0;
  private pending = "";

  constructor(
    private readonly redactor: Redactor,
    private readonly maxBytes = 256 * 1024,
    private readonly maxLine = 8 * 1024,
  ) {}

  write(chunk: string | Buffer): void {
    this.pending += chunk.toString();
    let nl = this.pending.indexOf("\n");
    while (nl >= 0) {
      this.push(this.pending.slice(0, nl).replace(/\r$/, ""));
      this.pending = this.pending.slice(nl + 1);
      nl = this.pending.indexOf("\n");
    }
    // A runaway line without a newline keeps only its end, cut after scrubbing: whole secrets are gone by then, and a
    // secret still arriving sits at the very end, where the next chunk completes it.
    if (this.pending.length > 4 * this.maxLine) {
      this.pending = "…" + this.redactor.scrub(this.pending).slice(-this.maxLine);
    }
  }

  /** The captured text, scrubbed; the unfinished last line included. */
  text(): string {
    const all = this.pending ? [...this.lines, this.clean(this.pending)] : this.lines;
    return all.join("\n");
  }

  /** The last n lines, scrubbed. */
  tail(n = 30): string {
    return this.text().split("\n").slice(-n).join("\n");
  }

  private clean(line: string): string {
    const scrubbed = this.redactor.scrub(line);
    return scrubbed.length > this.maxLine ? scrubbed.slice(0, this.maxLine) + "…" : scrubbed;
  }

  private push(line: string): void {
    const clean = this.clean(line);
    this.lines.push(clean);
    this.size += clean.length + 1;
    while (this.size > this.maxBytes && this.lines.length > 1) {
      this.size -= (this.lines.shift() ?? "").length + 1;
    }
  }
}
