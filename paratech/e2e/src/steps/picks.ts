// The pick queue of the native dialogs: <ctl>/picks.jsonl (written here) and consumed.jsonl (written by the app).
// Format and rules: desktop/internal/adapters/e2ehooks/doc.go. A typo is refused here, in the spec, instead of
// turning into a silent «cancelled» dialog in the app.
import { appendFileSync, readFileSync } from "node:fs";
import path from "node:path";

import type { Dialog } from "../contract.js";

export const PicksFileName = "picks.jsonl";
export const ConsumedFileName = "consumed.jsonl";
export const FixturesPrefix = "fixtures/";

export const PickDialogs: readonly Dialog[] = ["schedule", "model", "material", "notes", "recording"];

// Same limits as the app: a bigger line is refused there.
const maxPaths = 64;
const maxPathLength = 1024;
const forbiddenChars = '<>:"|?*';

export class PickError extends Error {
  constructor(message: string) {
    super(`pick: ${message}`);
    this.name = "PickError";
  }
}

/** Throws PickError unless dialog is a known dialog and every path is a safe '/'-separated path below fixtures/. */
export function validatePick(dialog: string, paths: readonly string[]): void {
  if (!(PickDialogs as readonly string[]).includes(dialog)) {
    throw new PickError(`unknown dialog ${JSON.stringify(dialog)}, want one of ${PickDialogs.join(", ")}`);
  }
  if (!Array.isArray(paths) || paths.length === 0) {
    throw new PickError(`dialog ${dialog}: no paths`);
  }
  if (paths.length > 1 && dialog !== "notes") {
    throw new PickError(`dialog ${dialog} takes one file, got ${paths.length}`);
  }
  if (paths.length > maxPaths) {
    throw new PickError(`dialog ${dialog}: more than ${maxPaths} paths`);
  }
  for (const p of paths) {
    validatePickPath(p);
  }
}

export function validatePickPath(p: unknown): void {
  if (typeof p !== "string" || p === "") {
    throw new PickError("a path must be a non-empty string");
  }
  const shown = JSON.stringify(p.length > 80 ? `${p.slice(0, 80)}…` : p);
  if (p.length > maxPathLength) {
    throw new PickError(`path ${shown} is too long`);
  }
  if (p.includes("\\")) {
    throw new PickError(`path ${shown}: use '/', not a backslash`);
  }
  if (p.startsWith("/") || /^[A-Za-z]:/.test(p)) {
    throw new PickError(`path ${shown} is absolute: want fixtures/<file>`);
  }
  if (!p.startsWith(FixturesPrefix) || p.length === FixturesPrefix.length) {
    throw new PickError(`path ${shown} must be a file below ${FixturesPrefix}`);
  }
  for (const ch of p) {
    const code = ch.codePointAt(0) ?? 0;
    if (code < 0x20 || code === 0x7f || forbiddenChars.includes(ch)) {
      throw new PickError(`path ${shown}: the character ${JSON.stringify(ch)} is not allowed`);
    }
  }
  for (const part of p.split("/")) {
    if (part === "" || part === "." || part === "..") {
      throw new PickError(`path ${shown}: empty, '.' or '..' component`);
    }
    if (part.endsWith(".") || part.endsWith(" ") || part.includes("~")) {
      throw new PickError(`path ${shown}: component ${JSON.stringify(part)} is not accepted by the app`);
    }
  }
}

/** One line of picks.jsonl, with the newline the app requires (a last line without it is ignored). */
export function buildPickLine(dialog: string, paths: readonly string[]): string {
  validatePick(dialog, paths);
  return `${JSON.stringify({ dialog, paths })}\n`;
}

/** Appends a pick to <ctl>/picks.jsonl in one write. */
export function appendPick(ctlDir: string, dialog: string, paths: readonly string[]): void {
  appendFileSync(path.join(ctlDir, PicksFileName), buildPickLine(dialog, paths), { encoding: "utf8", flag: "a" });
}

export interface ConsumedPick {
  line: number;
  dialog: string;
  result: "picked" | "cancelled";
  reason?: string;
}

/** The lines of consumed.jsonl the app has written (a missing file or a half-written last line is skipped). */
export function parseConsumed(text: string): ConsumedPick[] {
  const out: ConsumedPick[] = [];
  for (const line of text.split("\n")) {
    if (line.trim() === "") {
      continue;
    }
    try {
      const v = JSON.parse(line) as ConsumedPick;
      if (typeof v.line === "number" && typeof v.dialog === "string" && (v.result === "picked" || v.result === "cancelled")) {
        out.push(v);
      }
    } catch {
      // half-written last line
    }
  }
  return out;
}

export function consumedPicks(ctlDir: string): ConsumedPick[] {
  try {
    return parseConsumed(readFileSync(path.join(ctlDir, ConsumedFileName), "utf8"));
  } catch {
    return [];
  }
}
