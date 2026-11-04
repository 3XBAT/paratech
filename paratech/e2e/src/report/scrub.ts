// Removes secrets from any text the runner writes or throws (report, step errors). Pure: the environment is a parameter.
import { EnvHubKey } from "../contract.js";

export const Hidden = "[скрыто]";

/** Variables of the owner whose values must never appear anywhere (ADR-0035 §4). */
export const SecretEnvNames = [
  EnvHubKey,
  "PARATECH_E2E_ANTHROPIC_KEY",
  "PARATECH_E2E_ANTHROPIC_BASE_URL",
  "PARATECH_E2E_ANTHROPIC_AUTH",
] as const;

// Shorter values would hide ordinary words; a real key or URL is longer.
const minSecretLength = 6;

const tokenPatterns: RegExp[] = [/pthub_[A-Za-z0-9_-]+/g, /(?<![A-Za-z0-9])sk-[A-Za-z0-9_-]{8,}/g];

const authHeader = /\b(authorization|x-api-key)(\s*[:=]\s*)(bearer\s+)?[^\s,;"']+/gi;

const assignment = /\b(PARATECH_E2E_[A-Z0-9_]+)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;]+)/g;

/** The secret values currently in env, longest first so a value that contains another is replaced whole. */
export function secretValues(env: NodeJS.ProcessEnv = process.env): string[] {
  const values: string[] = [];
  for (const name of SecretEnvNames) {
    const v = env[name];
    if (v && v.length >= minSecretLength) {
      values.push(v);
    }
  }
  return values.sort((a, b) => b.length - a.length);
}

/** Replaces secrets in text: the env values, pthub_ keys, sk- provider keys, auth headers, PARATECH_E2E_*=value. */
export function scrubSecrets(text: string, env: NodeJS.ProcessEnv = process.env): string {
  let out = text;
  for (const v of secretValues(env)) {
    out = out.split(v).join(Hidden);
  }
  out = out.replace(assignment, (_m, name: string, sep: string) => `${name}${sep}${Hidden}`);
  out = out.replace(authHeader, (_m, name: string, sep: string) => `${name}${sep}${Hidden}`);
  for (const re of tokenPatterns) {
    out = out.replace(re, Hidden);
  }
  return out;
}
