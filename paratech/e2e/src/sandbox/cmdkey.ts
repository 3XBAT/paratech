import { spawnSync } from "node:child_process";
import path from "node:path";

// The app (desktop/internal/adapters/wincred) names the credentials of an isolated profile "paratech-<id>/<service>/<name>",
// and of the owner's default profile "paratech/<service>/<name>". Only the first, for the id of THIS run, is ever deleted.

const profileIdPattern = /^[A-Za-z0-9_-]{6,}$/;

export function isValidProfileId(id: string | undefined): id is string {
  return id !== undefined && profileIdPattern.test(id);
}

/** The exact prefix of this profile's targets: "paratech-<id>/". Throws on a malformed id. */
export function profileTargetPrefix(id: string): string {
  if (!isValidProfileId(id)) {
    throw new Error("secrets_profile_id has an unexpected form");
  }
  return `paratech-${id}/`;
}

/**
 * Targets in the output of `cmdkey /list`. The labels are localized («Target:», «Объект:»...), the `target=` token of the
 * value ("LegacyGeneric:target=name") is not, so the value is taken after it. A line without that token is read as
 * «label: value» (an older or differently localized cmdkey); the exact-prefix filter of profileTargets keeps that safe.
 */
export function parseCmdkeyTargets(listing: string): string[] {
  const targets: string[] = [];
  for (const raw of listing.split(/\r?\n/)) {
    const line = raw.trim();
    if (line === "") {
      continue;
    }
    const at = line.toLowerCase().indexOf("target=");
    if (at >= 0) {
      targets.push(line.slice(at + "target=".length).trim());
      continue;
    }
    const colon = line.indexOf(":");
    if (colon > 0) {
      targets.push(line.slice(colon + 1).trim());
    }
  }
  return targets;
}

/**
 * The targets that belong to profile `id` and nothing else: they start EXACTLY with "paratech-<id>/" and carry a name
 * after it. Another id, the owner's "paratech/…" entries and look-alikes ("paratech-<id>x/", "xparatech-<id>/") are left
 * alone. A target with a quote or a control character is skipped, so it cannot bend the cmdkey command line.
 */
export function profileTargets(listing: string, id: string): string[] {
  const prefix = profileTargetPrefix(id);
  const unique = new Set<string>();
  for (const target of parseCmdkeyTargets(listing)) {
    if (target.startsWith(prefix) && target.length > prefix.length && !/["\u0000-\u001f]/.test(target)) {
      unique.add(target);
    }
  }
  return [...unique];
}

export interface CmdkeyResult {
  code: number | null;
  stdout: string;
}

/** Runs cmdkey with arguments (no shell). Injected in tests. */
export type CmdkeyRunner = (args: string[]) => CmdkeyResult;

export const runCmdkey: CmdkeyRunner = (args) => {
  const root = process.env.SystemRoot ?? "C:\\Windows";
  const res = spawnSync(path.join(root, "System32", "cmdkey.exe"), args, { windowsHide: true, encoding: "latin1", timeout: 30_000 });
  return { code: res.status, stdout: res.stdout ?? "" };
};

export interface CredentialCleanup {
  deleted: string[];
  failed: string[];
}

/**
 * Deletes the Credential Manager entries of one isolated profile. An unknown or malformed id deletes nothing.
 * Returns what was deleted and what could not be (the caller reports it; the names carry no secret).
 */
export function deleteProfileCredentials(id: string | undefined, run: CmdkeyRunner = runCmdkey): CredentialCleanup {
  const result: CredentialCleanup = { deleted: [], failed: [] };
  if (!isValidProfileId(id)) {
    return result;
  }
  const listing = run(["/list"]);
  if (listing.code !== 0) {
    result.failed.push("cmdkey /list");
    return result;
  }
  for (const target of profileTargets(listing.stdout, id)) {
    if (run([`/delete:${target}`]).code === 0) {
      result.deleted.push(target);
    } else {
      result.failed.push(target);
    }
  }
  return result;
}

/** The secrets_profile_id of the app's debug log; undefined if the line is absent or the id is malformed. */
export function parseSecretsProfileId(log: string): string | undefined {
  const m = /secrets_profile_id="?([A-Za-z0-9_-]+)"?/.exec(log);
  return m && isValidProfileId(m[1]) ? m[1] : undefined;
}
