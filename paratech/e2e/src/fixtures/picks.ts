// Guards for the pick queue (<ctl>/picks.jsonl, consumed.jsonl; format: src/steps/picks.ts). A pick queued for a dialog
// that never opened would be taken by the NEXT dialog, which then imports the wrong file: the pack checks the queue is
// drained before it queues anything, and that one click used exactly one line.
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

import { consumedPicks, PicksFileName } from "../steps/picks.js";

/** Complete lines of a picks.jsonl text (the app ignores a last line without its newline until it is complete). */
export function countPickLines(text: string): number {
  return text.split("\n").slice(0, -1).filter((l) => l.trim() !== "").length;
}

/** Lines of picks.jsonl the app has not used yet. */
export function pendingPicks(ctlDir: string): number {
  const file = path.join(ctlDir, PicksFileName);
  const queued = existsSync(file) ? countPickLines(readFileSync(file, "utf8")) : 0;
  return Math.max(0, queued - consumedPicks(ctlDir).length);
}

/** The lines of consumed.jsonl so far (used and cancelled alike). */
export const usedPicks = (ctlDir: string): number => consumedPicks(ctlDir).length;
