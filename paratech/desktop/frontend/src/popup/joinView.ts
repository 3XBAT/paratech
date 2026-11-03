// «Подключиться» in the recording popup (TASK-370, ADR-0032): opens the online class of the lesson on offer, before the
// recording starts. Pure, no React. The page never sees the link or the host: the core says only which subject of the
// offered lesson has one (PromptApi.callSubject) and opens it by that subject ID (LibraryApi.openSubjectCallLink).
// The button opens the meeting once per showing of the popup: it stays grey until the popup closes.

import { callLinkTexts } from "../callLinkView.js";

export const joinTexts = {
  /** The link was removed meanwhile (or the subject is gone): the button goes, «Начать запись» stays. */
  gone: "Ссылки на пару больше нет.",
  /** Why the grey button does nothing. */
  openedTitle: "Ссылка уже открыта",
} as const;

/** hidden: no button; ready: «Подключиться»; opening: the call runs; opened: grey until the popup closes. */
export type JoinPhase = "hidden" | "ready" | "opening" | "opened";

export interface JoinState {
  /** A lesson is on offer in the popup now (the offered view, not the recording or the advice). */
  shown: boolean;
  phase: JoinPhase;
  /** The subject whose link «Подключиться» opens; "" while there is none. */
  subjectId: string;
  /** The last open: "failed" can be repeated, "gone" cannot (no link any more). */
  problem: "" | "failed" | "gone";
}

export const idleJoin: JoinState = { shown: false, phase: "hidden", subjectId: "", problem: "" };

/** What the controller needs from the bindings (PromptApi.callSubject and LibraryApi.openSubjectCallLink). */
export interface JoinDeps {
  /** The subject ID of the lesson prompt when it has a call link, otherwise "". May throw: that means no button. */
  callSubject(promptId: string): Promise<string>;
  open(subjectId: string): Promise<{ problem: string }>;
}

/**
 * The state machine of the button. Replies of older reads and opens are dropped: only the newest read counts, and an
 * open that ends after the popup closed or the prompt changed no longer touches the state.
 */
export class JoinController {
  private current: JoinState = idleJoin;
  private promptId = "";
  private read = 0;
  private epoch = 0;

  constructor(
    private readonly deps: JoinDeps,
    private readonly onChange: (s: JoinState) => void,
  ) {}

  get state(): JoinState {
    return this.current;
  }

  private set(next: JoinState): void {
    this.current = next;
    this.onChange(next);
  }

  /**
   * The lesson prompt on offer, or null (none, a recording or the advice is shown, the start runs). A new prompt starts
   * clean; the same one (the offer came back after a failed start) keeps the opened state.
   */
  offer(promptId: string | null): void {
    this.read++;
    if (promptId === null) {
      this.set({ ...this.current, shown: false });
      return;
    }
    if (promptId !== this.promptId) {
      this.promptId = promptId;
      this.epoch++;
      this.set({ ...idleJoin, shown: true });
    } else {
      this.set({ ...this.current, shown: true });
    }
    void this.load(promptId);
  }

  /** The core showed the popup again: the link may have changed, ask once more. */
  refresh(): void {
    if (this.current.shown) {
      void this.load(this.promptId);
    }
  }

  /** The popup closed (the page became hidden): the next showing offers the button anew. */
  closed(): void {
    this.read++;
    this.epoch++;
    this.set({ ...idleJoin, shown: this.current.shown });
  }

  /** A start was tried: the text of an earlier open goes. */
  clearProblem(): void {
    if (this.current.problem !== "") {
      this.set({ ...this.current, problem: "" });
    }
  }

  private async load(promptId: string): Promise<void> {
    const seq = ++this.read;
    let id: string;
    try {
      id = await this.deps.callSubject(promptId);
    } catch {
      id = "";
    }
    if (seq !== this.read || promptId !== this.promptId) {
      return;
    }
    const s = this.current;
    if (s.phase === "opening") {
      return;
    }
    if (id === "") {
      this.set({ ...s, phase: "hidden", subjectId: "" });
    } else if (id === s.subjectId && s.phase === "opened") {
      return;
    } else {
      this.set({ ...s, phase: "ready", subjectId: id, problem: id === s.subjectId ? s.problem : "" });
    }
  }

  /** «Подключиться»: the second click while the first runs, or after it, does nothing. */
  async click(): Promise<void> {
    const s = this.current;
    if (!s.shown || s.phase !== "ready") {
      return;
    }
    const epoch = this.epoch;
    this.set({ ...s, phase: "opening", problem: "" });
    let problem: string;
    try {
      problem = (await this.deps.open(s.subjectId)).problem;
    } catch {
      problem = "failed";
    }
    if (epoch !== this.epoch) {
      return;
    }
    const now = this.current;
    if (problem === "") {
      this.set({ ...now, phase: "opened" });
    } else if (problem === "no_link" || problem === "not_found") {
      this.set({ ...now, phase: "hidden", subjectId: "", problem: "gone" });
    } else {
      this.set({ ...now, phase: "ready", problem: "failed" });
    }
  }
}

/** What the popup draws. */
export interface JoinView {
  /** The button is in the row of the offered lesson. */
  visible: boolean;
  busy: boolean;
  disabled: boolean;
  /** The meeting is open: the button stays grey until the popup closes. */
  opened: boolean;
  title: string;
  /** Joins the popup's error line (lowest priority); "" when none. */
  error: string;
  /** A quiet line like the picker's warnings; "" when none. */
  note: string;
}

/** `blocked`: a start or skip runs; it disables this button but never «Начать запись». */
export function joinView(s: JoinState, blocked: boolean): JoinView {
  return {
    visible: s.shown && s.phase !== "hidden",
    busy: s.phase === "opening",
    disabled: s.phase !== "ready" || blocked,
    opened: s.phase === "opened",
    title: s.phase === "opened" ? joinTexts.openedTitle : callLinkTexts.joinTitle,
    error: s.shown && s.problem === "failed" ? callLinkTexts.openFailed : "",
    note: s.shown && s.problem === "gone" ? joinTexts.gone : "",
  };
}

/** The lesson prompt the button belongs to: an offered lesson while neither the recording nor the advice is shown. */
export function joinPromptId(
  view: { id: string; kind: string; state: string } | null,
  recordingOrAdvice: boolean,
): string | null {
  if (view === null || recordingOrAdvice || view.kind !== "lesson" || view.state !== "OFFERED") {
    return null;
  }
  return view.id;
}
