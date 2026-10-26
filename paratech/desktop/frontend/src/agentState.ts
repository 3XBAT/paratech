// State of the agent panel: the requests shown in the thread and how updates reach them. Pure, no React.
// The core publishes updates from several operations (Ask, Approve, Stop, ...) and does not order them across
// operations, and a failed Ask arrives with an empty threadId. So every update is applied to the request it names
// (threadId + turn) and does not depend on what came before, except that a finished request stays finished.

import type { AgentState, AskInput, CallProblem, UpdateView } from "./bindings/agent.js";

/** One request in the thread: what the student typed (local) and the latest update of the core. */
export interface Entry {
  /** "thread:<id>:<turn>", "local:<n>" until the first update of a sent request, or "orphan:<scope>". */
  key: string;
  /** Insertion order, for the stable order of the thread. */
  seq: number;
  /** "" until the core names the thread. */
  threadId: string;
  turn: number;
  /** What the student typed here; "" for requests that came from the history. */
  text: string;
  /** The context the request was asked in. */
  scope: AskInput | null;
  /** The call failed before the core accepted it. */
  callProblem: CallProblem;
  update: UpdateView | null;
}

export interface PanelState {
  entries: readonly Entry[];
  nextSeq: number;
  /** The Hub link; null until State answered. */
  link: AgentState | null;
}

export const initialPanelState: PanelState = { entries: [], nextSeq: 1, link: null };

export type PanelAction =
  | { type: "asked"; id: number; text: string; scope: AskInput }
  | { type: "askResult"; id: number; problem: CallProblem }
  | { type: "rerunAsked"; from: { threadId: string; turn: number; text: string; scope: AskInput | null } }
  | { type: "rerunRefused"; key: string }
  | { type: "update"; update: UpdateView }
  | { type: "history"; requests: readonly UpdateView[] }
  | { type: "link"; link: AgentState };

/** Finished for good: no later operation changes a done or cancelled request. */
const final = new Set(["DONE", "CANCELLED"]);

export function scopeKey(s: { subjectId: string; lessonId: string; assignmentId: string; screen: string }): string {
  return [s.subjectId, s.lessonId, s.assignmentId, s.screen].join("|");
}

function threadKey(threadId: string, turn: number): string {
  return `thread:${threadId}:${turn}`;
}

function localKey(id: number): string {
  return `local:${id}`;
}

function replace(entries: readonly Entry[], key: string, next: Entry): Entry[] {
  return entries.map((e) => (e.key === key ? next : e));
}

/** The oldest sent request of the scope the core has not named yet. */
function pendingLocal(entries: readonly Entry[], scope: string): Entry | undefined {
  return entries.find(
    (e) => e.key.startsWith("local:") && e.callProblem === "" && e.update === null && e.scope !== null && scopeKey(e.scope) === scope,
  );
}

function applyUpdate(s: PanelState, u: UpdateView): PanelState {
  if (u.threadId === "") {
    // A failed Ask: the core never named a thread, so the scope is all there is to match.
    const sc = scopeKey(u);
    const local = pendingLocal(s.entries, sc);
    if (local) {
      return { ...s, entries: replace(s.entries, local.key, { ...local, update: u }) };
    }
    const key = `orphan:${sc}`;
    const old = s.entries.find((e) => e.key === key);
    const orphan: Entry = { key, seq: old?.seq ?? s.nextSeq, threadId: "", turn: 0, text: "", scope: null, callProblem: "", update: u };
    return old
      ? { ...s, entries: replace(s.entries, key, orphan) }
      : { ...s, entries: [...s.entries, orphan], nextSeq: s.nextSeq + 1 };
  }

  const key = threadKey(u.threadId, u.turn);
  const known = s.entries.find((e) => e.key === key);
  if (known) {
    // A problem-only update (a refused button) is no status: it must reach a finished request (a refused rerun).
    if (known.update && final.has(known.update.status) && !final.has(u.status) && u.status !== "") {
      return s;
    }
    // A step went to the Hub: only the status and the stop reason move, the cards shown stay.
    if (u.progress && known.update) {
      return { ...s, entries: replace(s.entries, key, { ...known, update: { ...known.update, status: u.status, stopReason: u.stopReason, problem: "" } }) };
    }
    // A failed button operation: no status, no cards, only a problem. The request itself is unchanged, so keep its
    // cards and buttons and attach the problem; the next real update replaces it.
    if (known.update && u.status === "" && u.cards.length === 0) {
      // A refused rerun comes this way too (the problem lands on the old request): take the shown new one back.
      const next = threadKey(u.threadId, u.turn + 1);
      const entries = replace(s.entries, key, { ...known, update: { ...known.update, problem: u.problem } });
      return { ...s, entries: entries.filter((x) => !(x.key === next && x.update === null)) };
    }
    const replaced = replace(s.entries, key, { ...known, update: u });
    const pending = threadKey(u.threadId, u.turn + 1);
    return { ...s, entries: u.problem === "" ? replaced : replaced.filter((x) => !(x.key === pending && x.update === null)) };
  }
  const local = pendingLocal(s.entries, scopeKey(u));
  if (local) {
    return { ...s, entries: replace(s.entries, local.key, { ...local, key, threadId: u.threadId, turn: u.turn, update: u }) };
  }
  const entry: Entry = { key, seq: s.nextSeq, threadId: u.threadId, turn: u.turn, text: "", scope: null, callProblem: "", update: u };
  return { ...s, entries: [...s.entries, entry], nextSeq: s.nextSeq + 1 };
}

export function reducePanel(s: PanelState, a: PanelAction): PanelState {
  switch (a.type) {
    case "asked": {
      const e: Entry = {
        key: localKey(a.id),
        seq: s.nextSeq,
        threadId: "",
        turn: 0,
        text: a.text,
        scope: a.scope,
        callProblem: "",
        update: null,
      };
      return { ...s, entries: [...s.entries, e], nextSeq: s.nextSeq + 1 };
    }
    case "askResult": {
      const e = s.entries.find((x) => x.key === localKey(a.id));
      // An accepted call needs nothing; a request the core already named has its own updates.
      if (!e || a.problem === "") {
        return s;
      }
      return { ...s, entries: replace(s.entries, e.key, { ...e, callProblem: a.problem }) };
    }
    case "rerunAsked": {
      // The new request is shown at once with the same text; the core's update for the next turn fills it by key.
      const key = threadKey(a.from.threadId, a.from.turn + 1);
      if (s.entries.some((x) => x.key === key)) {
        return s;
      }
      // A stale rerun_blocked/busy text of the source must not outlive a successful rerun.
      const src = threadKey(a.from.threadId, a.from.turn);
      const entries = s.entries.map((x) => (x.key === src && x.update ? { ...x, update: { ...x.update, problem: "" } } : x));
      const e: Entry = {
        key,
        seq: s.nextSeq,
        threadId: a.from.threadId,
        turn: a.from.turn + 1,
        text: a.from.text,
        scope: a.from.scope,
        callProblem: "",
        update: null,
      };
      return { ...s, entries: [...entries, e], nextSeq: s.nextSeq + 1 };
    }
    case "rerunRefused":
      // Only a request the core never answered for is taken back.
      return { ...s, entries: s.entries.filter((x) => !(x.key === a.key && x.update === null)) };
    case "update":
      return applyUpdate(s, a.update);
    case "history":
      return a.requests.reduce(
        (acc, u) => (acc.entries.some((e) => e.threadId === u.threadId && e.turn === u.turn && e.update !== null) ? acc : applyUpdate(acc, u)),
        s,
      );
    case "link":
      return { ...s, link: a.link };
  }
}

/** The thread in display order: a thread's requests together by turn, threads by first appearance. */
export function orderedEntries(entries: readonly Entry[]): Entry[] {
  const first = new Map<string, number>();
  for (const e of entries) {
    const k = e.threadId || e.key;
    first.set(k, Math.min(first.get(k) ?? e.seq, e.seq));
  }
  return [...entries].sort((a, b) => {
    const fa = first.get(a.threadId || a.key)!;
    const fb = first.get(b.threadId || b.key)!;
    return fa - fb || a.turn - b.turn || a.seq - b.seq;
  });
}

/** A request the core is working on: «Стоп» applies to it. */
export function isRunning(status: string): boolean {
  return status === "PLANNING" || status === "EXECUTING";
}

/** The last request of the thread: the only one that offers «Повторить». */
export function lastEntry(entries: readonly Entry[]): Entry | undefined {
  return orderedEntries(entries).at(-1);
}

/**
 * The request that made the core refuse a new one as busy: the latest running request of the same subject. The refused
 * request has no thread of its own, so «Стоп» on it stops this one.
 */
export function blockingEntry(entries: readonly Entry[], refused: Entry): Entry | undefined {
  const u = refused.update;
  if (!u) {
    return undefined;
  }
  return orderedEntries(entries)
    .filter((e) => e.threadId !== "" && e.update !== null && isRunning(e.update.status) && e.update.subjectId === u.subjectId)
    .at(-1);
}

/** A refused-as-busy request that never got a thread, with the text it can be sent with again. */
export function isBusyRefused(e: Entry): boolean {
  return e.threadId === "" && e.update?.problem === "busy";
}

/**
 * Double-click guard for buttons: a request is marked on the update it was pressed on and stays off until a newer
 * update arrives or the call is refused. Synchronous, so two clicks before the next render make one call.
 */
export class PressTracker {
  private marks = new Map<string, UpdateView | null>();

  busy(key: string, update: UpdateView | null): boolean {
    return this.marks.has(key) && this.marks.get(key) === update;
  }

  /** False when this press is a repeat. */
  mark(key: string, update: UpdateView | null): boolean {
    if (this.busy(key, update)) {
      return false;
    }
    this.marks.set(key, update);
    return true;
  }

  release(key: string): void {
    this.marks.delete(key);
  }
}

export interface PanelMode {
  /** The input accepts a request. */
  inputEnabled: boolean;
  /** The Hub cannot be reached or is not set up: «Нет связи с paratech». */
  offline: boolean;
  placeholder: string;
  /** «нет связи — может быть неактуально» over the last thread, "" otherwise. */
  staleNote: string;
}

/**
 * Offline has two sources: the Hub is not set up (State, read-only) and a request that stopped for lack of a
 * connection (stopReason "offline" on its latest update).
 */
export function panelMode(s: PanelState): PanelMode {
  if (s.link === null) {
    return { inputEnabled: false, offline: false, placeholder: "Загрузка…", staleNote: "" };
  }
  const latest = orderedEntries(s.entries).at(-1);
  // A local "offline" refusal does not lock the input: the panel re-reads State and link.readOnly decides.
  const stoppedOffline = latest?.update?.stopReason === "offline";
  const offline = s.link.readOnly || stoppedOffline;
  if (offline) {
    return {
      inputEnabled: false,
      offline: true,
      placeholder: "Нет связи с paratech",
      staleNote: s.entries.length > 0 ? "нет связи — может быть неактуально" : "",
    };
  }
  return { inputEnabled: true, offline: false, placeholder: "Спросить paratech", staleNote: "" };
}
