// <ctl>/opened.jsonl: what the app would have opened or copied (links, files, call links, clipboard texts).
import { readFileSync } from "node:fs";
import path from "node:path";

import type { OpenedKind, OpenedRecord } from "../contract.js";

export const OpenedFileName = "opened.jsonl";

const kinds: readonly OpenedKind[] = ["link", "file", "call", "clipboard"];

/**
 * Parses the file. The app appends whole lines in one write, so only the last line can be half-written: it is
 * skipped. A broken line elsewhere means a corrupt record and throws.
 */
export function parseOpened(text: string): OpenedRecord[] {
  const lines = text.split("\n");
  const out: OpenedRecord[] = [];
  lines.forEach((line, i) => {
    if (line.trim() === "") {
      return;
    }
    let v: unknown;
    try {
      v = JSON.parse(line);
    } catch {
      if (i === lines.length - 1) {
        return; // being written right now
      }
      throw new Error(`opened.jsonl line ${i + 1} is not JSON`);
    }
    const r = v as Partial<OpenedRecord>;
    if (typeof r.value !== "string" || typeof r.at !== "string" || !kinds.includes(r.kind as OpenedKind)) {
      throw new Error(`opened.jsonl line ${i + 1} is not an opened record`);
    }
    out.push({ kind: r.kind as OpenedKind, value: r.value, at: r.at });
  });
  return out;
}

/** The records so far; a missing file is no records. */
export function readOpened(ctlDir: string): OpenedRecord[] {
  let text: string;
  try {
    text = readFileSync(path.join(ctlDir, OpenedFileName), "utf8");
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") {
      return [];
    }
    throw err;
  }
  return parseOpened(text);
}
