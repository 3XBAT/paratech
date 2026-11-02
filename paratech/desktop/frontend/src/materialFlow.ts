// The logic behind the file rows of the library (TASK-339), without React so that the races can be tested with a fake
// api and promises that the test resolves: the undo lines, the placing (with the create-then-place of a hint), the hints
// of «Входящие» asked one by one, and the drop of a row on a subject card. The hook useMaterialActions only wires it.

import { genericError, type LibraryApi, type LibraryMaterial, libraryErrorText, type LibrarySubject } from "./bindings/library.js";
import { type LibraryInboxApi, noHint, type PlacementHint } from "./bindings/libraryInbox.js";
import { folderTitle } from "./libraryView.js";
import {
  backOf,
  createdNote,
  hintLimit,
  hintTargets,
  type ImportOutcome,
  importOutcome,
  movedToInboxNote,
  placeLabel,
  placedNote,
  removedNote,
  targetFolderId,
  type UndoBack,
  type UndoItem,
  undoDrop,
  undoFail,
  undoNextDelay,
  undoPrune,
  undoPush,
} from "./libraryInboxView.js";

export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

const realTimers: Timers = {
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
};

export interface FlowState {
  undo: readonly UndoItem[];
  undoBusy: boolean;
}

export interface FlowDeps {
  api: Pick<LibraryApi, "createSubject" | "removeSubject">;
  inboxApi: Pick<LibraryInboxApi, "place" | "moveToInbox" | "removeMaterial" | "restoreMaterial">;
  /** The subjects the page knows now. */
  subjects(): readonly LibrarySubject[];
  /** A subject the flow created: the page adds it to its list. */
  addSubject(s: LibrarySubject): void;
  /** Re-reads the lists after a change. */
  refresh(): void;
  now?: () => number;
  timers?: Timers;
}

/**
 * Changes of files with their undo lines. Each change resolves to "" or the Russian text of the refusal. Each undo line
 * keeps its own ten seconds; only one «Вернуть» runs at a time.
 */
export class MaterialFlow {
  private state: FlowState = { undo: [], undoBusy: false };
  private readonly listeners = new Set<() => void>();
  private seq = 0;
  private undoing = false;
  /** Ids of the subjects a «create» hint made and no file was placed into yet. */
  private readonly made = new Set<string>();
  private timer: unknown = null;
  private disposed = false;
  private readonly now: () => number;
  private readonly timers: Timers;

  constructor(private readonly d: FlowDeps) {
    this.now = d.now ?? Date.now;
    this.timers = d.timers ?? realTimers;
  }

  /** For useSyncExternalStore: stable between changes. */
  getState = (): FlowState => this.state;

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  /** Starts the timer of the undo lines again after dispose (React StrictMode mounts, unmounts and mounts once more). */
  attach(): void {
    this.disposed = false;
    this.set({});
  }

  /** Stops the timer of the undo lines. */
  dispose(): void {
    this.disposed = true;
    this.stopTimer();
  }

  private stopTimer(): void {
    if (this.timer !== null) {
      this.timers.clear(this.timer);
      this.timer = null;
    }
  }

  private set(next: Partial<FlowState>): void {
    this.state = { ...this.state, ...next };
    this.stopTimer();
    const delay = undoNextDelay(this.state.undo, this.now());
    if (delay !== null && !this.disposed) {
      this.timer = this.timers.set(() => {
        this.timer = null;
        this.set({ undo: undoPrune(this.state.undo, this.now()) });
      }, delay);
    }
    this.listeners.forEach((fn) => fn());
  }

  pushUndo(materialId: string, text: string, back: UndoBack): void {
    this.seq += 1;
    this.set({ undo: undoPush(this.state.undo, { id: this.seq, materialId, text, back }, this.now()) });
  }

  async undoOne(it: UndoItem): Promise<void> {
    if (this.undoing) {
      return;
    }
    this.undoing = true;
    this.set({ undoBusy: true });
    try {
      if (it.back.kind === "inbox") {
        await this.d.inboxApi.moveToInbox(it.materialId);
        await this.removeMade(it.back.removeSubject);
      } else if (it.back.kind === "place") {
        await this.d.inboxApi.place(it.materialId, it.back.subjectId, it.back.folderId);
      } else {
        await this.d.inboxApi.restoreMaterial(it.materialId);
      }
      this.set({ undo: undoDrop(this.state.undo, it.id) });
      this.d.refresh();
    } catch (err) {
      this.set({ undo: undoFail(this.state.undo, it.id, libraryErrorText(err)) });
    } finally {
      this.undoing = false;
      this.set({ undoBusy: false });
    }
  }

  /**
   * «Вернуть» after a «create» hint also removes the subject the hint made. The core removes only an empty subject; a
   * subject that got files, notes or other content stays, and that refusal is not shown (the file is back anyway).
   */
  private async removeMade(subjectId: string | undefined): Promise<void> {
    if (subjectId === undefined) {
      return;
    }
    try {
      await this.d.api.removeSubject(subjectId);
    } catch {
      // Silent on purpose, see above.
    }
  }

  /** «Обновить список» of an undo line that failed: the line has done its job. */
  refreshUndo(): void {
    this.set({ undo: this.state.undo.filter((x) => x.error === "") });
    this.d.refresh();
  }

  async change(m: LibraryMaterial, work: () => Promise<LibraryMaterial>, note: (r: LibraryMaterial) => string, back: UndoBack): Promise<string> {
    try {
      const r = await work();
      this.pushUndo(m.id, note(r), back);
      this.d.refresh();
      return "";
    } catch (err) {
      return libraryErrorText(err);
    }
  }

  placeInto(m: LibraryMaterial, subjectId: string, folderId: string): Promise<string> {
    return this.change(
      m,
      () => this.d.inboxApi.place(m.id, subjectId, folderId),
      (r) => placedNote(placeLabel(this.d.subjects(), r.subjectId, r.folderId)),
      backOf(m),
    );
  }

  /**
   * The hint's button. "existing": place. "create": create the subject (a repeat after a refusal of the placing finds the
   * one made and does not create it twice), then place into the folder of its `system` kind, else into «Материалы».
   */
  async placeHint(m: LibraryMaterial, h: PlacementHint): Promise<string> {
    if (h.kind === "existing") {
      return this.placeInto(m, h.subjectId, h.folderId);
    }
    if (h.kind !== "create") {
      return genericError;
    }
    let found = this.d.subjects().find((x) => x.name === h.createName);
    if (!found) {
      try {
        found = await this.d.api.createSubject(h.createName, "", h.createAliases);
      } catch (err) {
        return libraryErrorText(err);
      }
      this.made.add(found.id);
      this.d.addSubject(found);
    }
    const s = found;
    const folder = s.folders.find((f) => f.system === h.folderKind) ?? s.folders.find((f) => f.system === "materials");
    if (!folder) {
      return genericError;
    }

    // A subject this flow made (also in an attempt that was refused) goes with «Вернуть»; one that was there before stays.
    const fresh = this.made.has(s.id);
    const back = backOf(m);
    const text = await this.change(
      m,
      () => this.d.inboxApi.place(m.id, s.id, folder.id),
      (r) => (fresh ? createdNote(s.name, folderTitle(folder)) : placedNote(placeLabel([...this.d.subjects(), s], r.subjectId, r.folderId))),
      fresh && back.kind === "inbox" ? { ...back, removeSubject: s.id } : back,
    );
    if (text === "") {
      this.made.delete(s.id);
    }
    return text;
  }

  toInbox(m: LibraryMaterial): Promise<string> {
    return this.change(m, () => this.d.inboxApi.moveToInbox(m.id), () => movedToInboxNote, backOf(m));
  }

  remove(m: LibraryMaterial): Promise<string> {
    return this.change(m, () => this.d.inboxApi.removeMaterial(m.id), () => removedNote(m.name), { kind: "restore" });
  }
}

// ---- The hints of «Входящие» ----

export interface HintSnapshot {
  hints: ReadonlyMap<string, PlacementHint>;
  /** The files whose hint is still to come (shown as «…»). */
  pending: ReadonlySet<string>;
}

/**
 * Asks the hints of the first `limit` files of «Входящие», one request at a time. A hint is valid for the subjects it was
 * computed for: when they change the hints are forgotten and an answer on its way is ignored, as is an answer for a
 * file that left the asked ones. A failed request is a silent "no hint".
 */
export class HintScheduler {
  private key = "";
  private inbox: readonly LibraryMaterial[] = [];
  private on = false;
  private hints = new Map<string, PlacementHint>();
  private version = 0;
  private generation = 0;
  private inflight: string | null = null;
  private disposed = false;
  private lastSig = "0|";

  constructor(
    private readonly api: Pick<LibraryInboxApi, "placementHint">,
    private readonly onChange: (s: HintSnapshot) => void,
    private readonly limit: number = hintLimit,
  ) {}

  /** The inputs changed (or not): `on` is false off the list page and while the subjects are not loaded. */
  update(p: { key: string; inbox: readonly LibraryMaterial[]; on: boolean }): void {
    if (p.key !== this.key) {
      this.key = p.key;
      this.generation += 1;
      if (this.hints.size > 0) {
        this.hints = new Map();
        this.version += 1;
      }
    }
    this.inbox = p.inbox;
    this.on = p.on;
    this.pump();
    this.publish();
  }

  /** Allows requests again after dispose (React StrictMode mounts, unmounts and mounts once more). */
  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private pending(): Set<string> {
    return this.on ? new Set(hintTargets(this.inbox, new Set(this.hints.keys()), this.limit)) : new Set();
  }

  private pump(): void {
    if (!this.on || this.disposed || this.inflight !== null) {
      return;
    }
    const id = hintTargets(this.inbox, new Set(this.hints.keys()), this.limit)[0];
    if (id === undefined) {
      return;
    }
    this.inflight = id;
    void this.ask(id, this.generation);
  }

  private async ask(id: string, generation: number): Promise<void> {
    let hint = noHint;
    try {
      hint = await this.api.placementHint(id);
    } catch {
      // Silent: the row simply has no hint.
    }
    this.inflight = null;
    if (this.disposed) {
      return;
    }
    const asked = this.inbox.filter((m) => !m.removed).slice(0, this.limit);
    if (generation === this.generation && asked.some((m) => m.id === id)) {
      this.hints.set(id, hint);
      this.version += 1;
    }
    this.pump();
    this.publish();
  }

  private publish(): void {
    const pending = this.pending();
    const sig = `${this.version}|${[...pending].join("|")}`;
    if (sig === this.lastSig) {
      return;
    }
    this.lastSig = sig;
    this.onChange({ hints: new Map(this.hints), pending });
  }
}

// ---- «Добавить файл» on the page of a subject ----

export type AddFileResult =
  | { kind: "cancelled" }
  | { kind: "done"; outcome: ImportOutcome; target: { subjectId: string; folderId: string } }
  | { kind: "refused"; text: string };

/**
 * Asks the core to import a file the student picks into a folder of a subject (folderId "" is «Материалы»). A cancelled
 * dialog is no result to show; a refusal carries its Russian text (the page offers «Повторить» / «Обновить список»).
 * `target.folderId` is the folder resolved, where «Переместить сюда» puts a file that lies elsewhere.
 */
export async function addFileInto(
  api: Pick<LibraryInboxApi, "importFileTo">,
  subjectId: string,
  folderId: string,
  subjects: readonly LibrarySubject[],
): Promise<AddFileResult> {
  try {
    const r = await api.importFileTo(subjectId, folderId);
    if (r.cancelled) {
      return { kind: "cancelled" };
    }
    return {
      kind: "done",
      outcome: importOutcome(r, { subjectId, folderId }, subjects),
      target: { subjectId, folderId: targetFolderId(subjects, subjectId, folderId) },
    };
  } catch (err) {
    return { kind: "refused", text: libraryErrorText(err) };
  }
}

// ---- The drop of a row on a subject card ----

export interface CardDropDeps {
  /** The file of «Входящие» with this ID, if the page still shows it. */
  find(materialId: string): LibraryMaterial | undefined;
  /** Places into «Материалы» of the subject (folder ""); resolves to "" or the refusal. */
  place(m: LibraryMaterial, subjectId: string, folderId: string): Promise<string>;
  setError(text: string): void;
}

/** A row dropped on a card is the same as «Разложить» into «Материалы»; a second drop during the first is ignored. */
export class CardDrop {
  private busy = false;
  private last: { subjectId: string; materialId: string } | null = null;

  constructor(private readonly d: CardDropDeps) {}

  async drop(subjectId: string, materialId: string): Promise<void> {
    const m = this.d.find(materialId);
    if (!m || this.busy) {
      return;
    }
    this.last = { subjectId, materialId };
    this.busy = true;
    this.d.setError("");
    try {
      this.d.setError(await this.d.place(m, subjectId, ""));
    } finally {
      this.busy = false;
    }
  }

  /** «Повторить» after a refusal: the last drop again. */
  again(): Promise<void> {
    return this.last ? this.drop(this.last.subjectId, this.last.materialId) : Promise.resolve();
  }
}
