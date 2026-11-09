// The elapsed time of a running request («12с», «1м 5с») next to the waiting dots. Pure, no React and no clock of its own:
// the moment a request started running is kept per request (by `seq`, which survives the local -> thread rename), and
// the label is always `now - start`, so a throttled timer or a hidden window never makes it drift.

import { type Entry, hasTicket, isRunning, queuedBehind } from "./agentState.js";
import { formatElapsed } from "./agentView.js";

/** When each running request started running, in ms, by `Entry.seq`. A request that is not running has no entry. */
export type RunStarts = Readonly<Record<number, number>>;

/**
 * The request is working now: sent and not yet answered (but not waiting in the queue under a ticket, nor behind another
 * request of its chat), or its status is PLANNING / EXECUTING. A plan or question wait, a stop, a failure and a finished
 * request are not running.
 */
export function isRunningEntry(entries: readonly Entry[], e: Entry): boolean {
  if (e.callProblem !== "") {
    return false;
  }
  return e.update === null ? !hasTicket(e) && queuedBehind(entries, e) === undefined : isRunning(e.update.status);
}

/**
 * The starts after the thread changed. A request that began running gets `now`; one that stopped running loses its start,
 * so a new run (Продолжить / Повторить / the answer to a plan) counts from 0 again. The same object comes back when
 * nothing changed.
 */
export function syncRunStarts(prev: RunStarts, entries: readonly Entry[], now: number): RunStarts {
  const next: Record<number, number> = {};
  for (const e of entries) {
    if (isRunningEntry(entries, e)) {
      next[e.seq] = prev[e.seq] ?? now;
    }
  }
  const keys = Object.keys(next);
  const same = keys.length === Object.keys(prev).length && keys.every((k) => prev[Number(k)] === next[Number(k)]);
  return same ? prev : next;
}

/** Whole seconds since the start; never negative (a clock set back reads 0). */
export function elapsedSeconds(startedAt: number, now: number): number {
  const s = Math.floor((now - startedAt) / 1000);
  return Number.isFinite(s) ? Math.max(0, s) : 0;
}

/** «1с», «59с», «1м 0с», «2м 5с». */
export function elapsedLabel(startedAt: number, now: number): string {
  return formatElapsed(elapsedSeconds(startedAt, now));
}
