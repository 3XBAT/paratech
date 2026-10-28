// The sequence of «Выполнено» of a page (TASK-316), without React so it can be tested: the guard against a second click,
// the call, what the page shows after it, and «Отменить» with exactly the values the call returned. The decisions about
// what to show are in assignmentDoneView.ts; the hook (pages/useDoneRunner.ts) only maps the sink to state.

import { type ActiveIssue, type DoneAction, doneOutcome, doneTexts, type ToastModel } from "./assignmentDoneView.js";
import type { DoneResult, HomeworkApi } from "./bindings/homework.js";

/** The toast on the screen; a new id is a new toast (its timer starts again). */
export type DoneToast = ToastModel & { id: number; actionLabel: string };

/** What the page does with the steps; the runner knows nothing else about the screen. */
export interface DoneRunnerSink {
  /** The key of the running call ("" none). */
  busy(key: string): void;
  issue(issue: ActiveIssue | null): void;
  toast(toast: DoneToast | null): void;
  /** The rows take the stored state at once. */
  apply(res: DoneResult): void;
  /** The screen is read again. */
  refresh(): void | Promise<void>;
  /** The page is still on the screen. */
  alive(): boolean;
}

/** The key to report as busy: the newest call still running ("" none). A Set keeps the order of adding. */
export function newestRunning(running: ReadonlySet<string>): string {
  let last = "";
  for (const k of running) {
    last = k;
  }
  return last;
}

export class DoneRunner {
  private readonly running = new Set<string>();
  private busyKey = "";
  private seq = 0;
  private toast: DoneToast | null = null;

  constructor(
    private readonly api: Pick<HomeworkApi, "markDone" | "restoreDone">,
    private readonly sink: DoneRunnerSink,
  ) {}

  /**
   * One call. A second one with the same ids while the first is in flight starts nothing. The guard is held only for
   * the call itself: the re-read after it can take long, and a click on «Отменить» during it must not be lost.
   */
  async run(
    action: DoneAction,
    ids: string[],
    call: () => Promise<DoneResult>,
    titles: Readonly<Record<string, string>> = {},
  ): Promise<void> {
    const key = ids.join(",");
    if (this.running.has(key)) {
      return;
    }
    this.running.add(key);
    this.sink.issue(null);
    this.busyKey = key;
    this.sink.busy(key);

    let res: DoneResult;
    try {
      res = await call();
    } finally {
      this.running.delete(key);
      // A mark in flight keeps its «Сохранение…» when another call (an undo of other ids) ends first.
      const next = newestRunning(this.running);
      if (this.busyKey !== next) {
        this.busyKey = next;
        if (this.sink.alive()) {
          this.sink.busy(next);
        }
      }
    }
    if (!this.sink.alive()) {
      return;
    }

    const out = doneOutcome(action, res, titles);
    this.sink.apply(res);
    this.sink.issue(
      out.issue === null ? null : { ...out.issue, ids, retry: () => void this.run(action, ids, call, titles) },
    );
    if (out.toast !== null) {
      this.toast = { ...out.toast, id: ++this.seq, actionLabel: doneTexts.undo };
      this.sink.toast(this.toast);
    }
    if (out.refresh) {
      try {
        await this.sink.refresh();
      } catch {
        // The rows already show the stored state; a failed re-read changes nothing the student has to act on.
      }
    }
  }

  /** «Выполнено» or «Вернуть в работу» of one assignment; title is the one the student clicked (for the toast). */
  mark(id: string, done: boolean, title = ""): Promise<void> {
    return this.run(done ? "mark" : "unmark", [id], () => this.api.markDone([id], done), title === "" ? {} : { [id]: title });
  }

  /**
   * «Отменить»: sends back exactly what the call returned (the same objects, never a rebuilt list). The toast closes
   * only when the call can start; if the same assignments are still being saved, the toast stays and false comes back.
   */
  async undo(): Promise<boolean> {
    const t = this.toast;
    if (t === null) {
      return false;
    }
    const ids = t.undo.prior.map((p) => p.id);
    if (this.running.has(ids.join(","))) {
      return false;
    }
    this.closeToast();
    await this.run("undo", ids, () => this.api.restoreDone(t.undo.priorDone, t.undo.prior));
    return true;
  }

  closeToast(): void {
    this.toast = null;
    this.sink.toast(null);
  }

  dismissIssue(): void {
    this.sink.issue(null);
  }
}
