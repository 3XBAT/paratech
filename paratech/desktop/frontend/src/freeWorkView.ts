// The free-work card «Такого задания нет — могу сделать как свободную работу» (TASK-189): texts, the model of what the
// card shows and the flow of its buttons. No React here, so the tests drive it with a fake API. The topic never comes
// to the page; the card names the call only.

import { type AgentApi, freeWorkErrors, freeWorkErrorText, type FreeWorkRef, type FreeWorkState } from "./bindings/agent.js";

export const freeWorkTexts = {
  caption: "Свободная работа",
  text: "Такого задания нет — могу сделать как свободную работу (с уточняющими вопросами)",
  yes: "Сделать",
  no: "Не надо",
  dontShow: "Больше не показывать",
  declined: "Не делаем",
  undone: "Работа отменена",
  working: "Выполняется…",
  reading: "Загрузка…",
  reload: "Обновить",
} as const;

export type FreeWorkPhase =
  | "loading" // the state is being read
  | "waiting" // «Сделать» / «Не надо»
  | "done" // the work card
  | "declined" // «Не делаем»
  | "undone" // the work was removed with «Отменить»
  | "unknown" // the state could not be read
  | "failed"; // the stored state is «failed»: «Обновить», not the same call again

export interface FreeWorkFlowState {
  phase: FreeWorkPhase;
  workId: string;
  /** A call runs: both buttons are off. */
  busy: boolean;
  dontShow: boolean;
  error: string;
}

export const initialFreeWorkFlow: FreeWorkFlowState = { phase: "loading", workId: "", busy: false, dontShow: false, error: "" };

/** What the card shows for a state of the flow. */
export interface FreeWorkModel {
  kind: "loading" | "ask" | "work" | "line" | "unknown";
  /** The line of a finished card («Не делаем», «Работа отменена»). */
  line: string;
  buttonsOff: boolean;
}

export function freeWorkModel(s: FreeWorkFlowState): FreeWorkModel {
  switch (s.phase) {
    case "loading":
      return { kind: "loading", line: freeWorkTexts.reading, buttonsOff: true };
    case "waiting":
      return { kind: "ask", line: "", buttonsOff: s.busy };
    case "done":
      return { kind: s.workId !== "" ? "work" : "line", line: "", buttonsOff: true };
    case "declined":
      return { kind: "line", line: freeWorkTexts.declined, buttonsOff: true };
    case "undone":
      return { kind: "line", line: freeWorkTexts.undone, buttonsOff: true };
    case "unknown":
    case "failed":
      return { kind: "unknown", line: "", buttonsOff: false };
  }
}

type Api = Pick<AgentApi, "freeWork" | "freeWorkCard">;

/** Owns the buttons of one card. A press while a call runs is ignored (single flight), so a double click calls once. */
export class FreeWorkFlow {
  private current: FreeWorkFlowState = initialFreeWorkFlow;
  private pending = false;
  private disposed = false;

  constructor(
    private readonly api: Api,
    private readonly ref: FreeWorkRef,
    private readonly onChange: (s: FreeWorkFlowState) => void,
  ) {}

  get state(): FreeWorkFlowState {
    return this.current;
  }

  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private set(patch: Partial<FreeWorkFlowState>): void {
    this.current = { ...this.current, ...patch };
    if (!this.disposed) {
      this.onChange(this.current);
    }
  }

  private apply(s: FreeWorkState): void {
    switch (s.state) {
      case "done":
        this.set({ phase: "done", workId: s.workId, busy: false, error: "" });
        return;
      case "undone":
        this.set({ phase: "undone", workId: "", busy: false, error: "" });
        return;
      default:
        this.set({ phase: "waiting", workId: "", busy: false });
    }
  }

  /** Draws the outcome of an old thread; reads, starts nothing. */
  async load(): Promise<void> {
    if (this.pending) {
      return;
    }
    this.pending = true;
    this.set({ phase: "loading", error: "" });
    try {
      const s = await this.api.freeWorkCard(this.ref);
      if (s.state === "failed") {
        this.set({ phase: "failed", workId: "", busy: false, error: freeWorkErrors.failed });
      } else {
        this.apply(s);
      }
    } catch (err) {
      this.set({ phase: "unknown", error: freeWorkErrorText(err) });
    } finally {
      this.pending = false;
    }
  }

  setDontShow(on: boolean): void {
    if (!this.current.busy) {
      this.set({ dontShow: on });
    }
  }

  /** «Сделать» (accept) or «Не надо». The checkbox value goes along with either. */
  async answer(accept: boolean): Promise<void> {
    if (this.pending || this.current.phase !== "waiting") {
      return;
    }
    this.pending = true;
    this.set({ busy: true, error: "" });
    try {
      const s = await this.api.freeWork(this.ref, accept, this.current.dontShow);
      if (s.state === "done" || s.state === "undone") {
        this.apply(s);
      } else if (!accept && s.state === "waiting") {
        this.set({ phase: "declined", busy: false });
      } else {
        await this.reread();
      }
    } catch (err) {
      await this.reread(freeWorkErrorText(err));
    } finally {
      this.pending = false;
    }
  }

  // A failed or odd reply may be a second press that lost to the first: the card is read again before an error shows.
  private async reread(error: string = freeWorkErrors.failed): Promise<void> {
    try {
      const s = await this.api.freeWorkCard(this.ref);
      if (s.state === "done" || s.state === "undone") {
        this.apply(s);
        return;
      }
      if (s.state === "failed") {
        // The core stored the failure: «Сделать» would replay it.
        this.set({ phase: "failed", workId: "", busy: false, error });
        return;
      }
    } catch {
      // The error below stands.
    }
    this.set({ busy: false, error });
  }
}
