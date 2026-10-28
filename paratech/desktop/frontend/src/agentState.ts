// State of the agent panel: the requests shown in the thread and how updates reach them. Pure, no React.
// The core publishes updates from several operations (Ask, Approve, Stop, ...) and does not order them across
// operations, and a failed Ask arrives with an empty threadId. So every update is applied to the request it names
// (threadId + turn) and does not depend on what came before, except that a finished request stays finished.

import type { AgentState, AskInput, CallProblem, ChatProblem, CurrentView, DraftView, EarlierRequestView, UpdateView } from "./bindings/agent.js";

/** One request in the thread: what the student typed (local) and the latest update of the core. */
export interface Entry {
  /** "thread:<id>:<turn>", "local:<n>" until the first update of a sent request, or "orphan:<threadId>:<scope>". */
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
  /** Unchecked answer texts being read (agent:draft), one per (thread, turn, call), the last one winning. */
  drafts: readonly DraftView[];
}

export const initialPanelState: PanelState = { entries: [], nextSeq: 1, link: null, drafts: [] };

export type PanelAction =
  | { type: "asked"; id: number; text: string; scope: AskInput }
  | { type: "askResult"; id: number; problem: CallProblem }
  | { type: "rerunAsked"; from: { threadId: string; turn: number; text: string; scope: AskInput | null } }
  | { type: "rerunRefused"; key: string }
  | { type: "update"; update: UpdateView }
  /** The current request of a chat opened after a restart (chatCurrent); `text` is what the student typed, from the history. */
  | { type: "snapshot"; update: UpdateView; text: string }
  | { type: "draft"; draft: DraftView }
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

/**
 * The oldest sent request of the scope the core has not named yet. A request asked in one chat is never named by an
 * update of another chat (`threadId` of the update; "" matches by scope only, the rule for a failed Ask).
 */
function pendingLocal(entries: readonly Entry[], scope: string, threadId = ""): Entry | undefined {
  return entries.find(
    (e) =>
      e.key.startsWith("local:") &&
      e.callProblem === "" &&
      e.update === null &&
      e.scope !== null &&
      scopeKey(e.scope) === scope &&
      (threadId === "" || (e.scope.threadId ?? "") === "" || e.scope.threadId === threadId),
  );
}

/**
 * An update that ends the draft of its request: anything but a step of progress or a problem-only update (a refused
 * button). A failed read emits no event of its own, so every other update is taken to clear.
 */
function clearsDrafts(u: UpdateView): boolean {
  return u.threadId !== "" && !u.progress && !(u.status === "" && u.cards.length === 0);
}

/** An update of an Ask the core refused: no thread, or the asked chat with turn 0 and no status. */
function isFailedAsk(u: UpdateView): boolean {
  return u.threadId === "" || (u.turn === 0 && u.status === "");
}

function applyUpdate(s: PanelState, u: UpdateView): PanelState {
  const next = applyUpdateEntries(s, u);
  if (isFailedAsk(u)) {
    return next;
  }
  // Older turns of the thread are over; the own turn's drafts end only with a clearing update.
  const drop = (d: DraftView) => d.threadId === u.threadId && (d.turn < u.turn || (clearsDrafts(u) && d.turn === u.turn));
  if (!next.drafts.some(drop)) {
    return next;
  }
  return { ...next, drafts: next.drafts.filter((d) => !drop(d)) };
}

/** The newest turn the thread has shown or been told about, -1 if none. */
function currentTurn(s: PanelState, threadId: string): number {
  return s.entries.reduce((m, e) => (e.threadId === threadId ? Math.max(m, e.turn) : m), -1);
}

function applyDraft(s: PanelState, d: DraftView): PanelState {
  const cur = currentTurn(s, d.threadId);
  // Stale: an older turn than the request's current one. Unknown threads are kept (the update may be a moment behind).
  if (d.threadId === "" || d.turn < cur) {
    return s;
  }
  const own = s.entries.find((e) => e.threadId === d.threadId && e.turn === d.turn);
  // A finished request has no draft: a late event must not outlive the end.
  // Any non-running status (done, stopped, failed, cancelled) means the same.
  if (own?.update && own.update.status !== "" && own.update.status !== "PLANNING" && own.update.status !== "EXECUTING") {
    return s;
  }
  const same = (x: DraftView) => x.threadId === d.threadId && x.turn === d.turn && x.callId === d.callId;
  // A newer turn drops the older drafts of its thread; a call already shown keeps its position.
  const kept = s.drafts.filter((x) => !(x.threadId === d.threadId && x.turn < d.turn));
  return { ...s, drafts: kept.some(same) ? kept.map((x) => (same(x) ? d : x)) : [...kept, d] };
}

/** The drafts shown under a request, in arrival order. */
export function draftsFor(s: PanelState, e: Entry): DraftView[] {
  if (e.threadId === "") {
    return [];
  }
  return s.drafts.filter((d) => d.threadId === e.threadId && d.turn === e.turn && d.texts.length > 0);
}

function applyUpdateEntries(s: PanelState, u: UpdateView): PanelState {
  if (isFailedAsk(u)) {
    // A failed Ask: the core never named a request (threadId "", or the asked chat with turn 0 and no status; real
    // turns start at 1). The entry keeps threadId "": it stays unnamed, so its buttons act on the blocker, not on turn 0.
    const sc = scopeKey(u);
    const local = pendingLocal(s.entries, sc, u.threadId);
    if (local) {
      return { ...s, entries: replace(s.entries, local.key, { ...local, update: u }) };
    }
    // The thread is part of the key: failures of two chats in the same scope must not overwrite each other.
    const key = `orphan:${u.threadId}:${sc}`;
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
  const local = pendingLocal(s.entries, scopeKey(u), u.threadId);
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
    case "snapshot": {
      const u = a.update;
      // A live update wins: the session already knows the chat, so a late snapshot is dropped, never merged.
      if (isFailedAsk(u) || hasLiveRequest(s.entries, u.threadId)) {
        return s;
      }
      // Under the same key as a live update, so the next update of this request replaces the card instead of adding one.
      const entry: Entry = { key: threadKey(u.threadId, u.turn), seq: s.nextSeq, threadId: u.threadId, turn: u.turn, text: a.text, scope: null, callProblem: "", update: u };
      return { ...s, entries: [...s.entries, entry], nextSeq: s.nextSeq + 1 };
    }
    case "draft":
      return applyDraft(s, a.draft);
    case "history":
      return a.requests.reduce(
        (acc, u) => (acc.entries.some((e) => e.threadId === u.threadId && e.turn === u.turn && e.update !== null) ? acc : applyUpdate(acc, u)),
        s,
      );
    case "link":
      return { ...s, link: a.link };
  }
}

// ---- Snapshot of a chat opened after a restart (TASK-273) ----

/**
 * The panel already knows a request of the chat in this session (an update arrived, a rerun is shown, or a request sent
 * into the chat is waiting for its first update): the live state is newer than any snapshot.
 */
export function hasLiveRequest(entries: readonly Entry[], threadId: string): boolean {
  return entries.some(
    (e) =>
      (e.threadId === threadId && threadId !== "") ||
      (e.threadId === "" && e.callProblem === "" && e.update === null && e.scope !== null && (e.scope.threadId ?? "") === threadId),
  );
}

/** What the student typed in the request of `turn`, from the chat history; "" when the history does not have it. */
export function snapshotText(requests: readonly EarlierRequestView[], turn: number): string {
  return requests.find((r) => r.turn === turn)?.text ?? "";
}

export interface SnapshotDeps {
  threadId: string;
  /** The entries of the panel right now. */
  entries(): readonly Entry[];
  /** The chat is still the open one. */
  stillOpen(): boolean;
  /** chatCurrent of the chat. */
  current(): Promise<CurrentView>;
  /** The earlier requests of the chat (empty on failure); the snapshot waits for them to take the student's text. */
  earlier: Promise<readonly EarlierRequestView[]>;
  dispatch(a: PanelAction): void;
  /** A problem of the call; "failed" when it threw. */
  onProblem(p: ChatProblem): void;
}

/**
 * Restores the current request of the opened chat as a live card. Nothing happens when the session already shows the
 * chat; a snapshot that arrives after a switch of chat is dropped (the reducer drops one that a live update beat).
 */
export async function loadSnapshot(d: SnapshotDeps): Promise<void> {
  if (d.threadId === "" || hasLiveRequest(d.entries(), d.threadId)) {
    return;
  }
  let v: CurrentView;
  try {
    v = await d.current();
  } catch {
    if (d.stillOpen()) {
      d.onProblem("failed");
    }
    return;
  }
  if (!d.stillOpen()) {
    return;
  }
  if (v.problem !== "") {
    d.onProblem(v.problem);
    return;
  }
  // No request or an ended one: the text line of the history stays. An update of another chat is never taken.
  if (v.update === null || v.update.threadId !== d.threadId) {
    return;
  }
  const requests = await d.earlier.catch((): readonly EarlierRequestView[] => []);
  if (!d.stillOpen()) {
    return;
  }
  d.dispatch({ type: "snapshot", update: v.update, text: snapshotText(requests, v.update.turn) });
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
  // Busy is per chat: the blocker is in the chat the refused request was asked in; subject only when that is unknown.
  const chat = refused.scope?.threadId || u.threadId;
  return orderedEntries(entries)
    .filter(
      (e) =>
        e.threadId !== "" &&
        e.update !== null &&
        isRunning(e.update.status) &&
        (chat !== "" ? e.threadId === chat : e.update.subjectId === u.subjectId),
    )
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
