// «Подключиться» in the recording popup (TASK-370, ADR-0032): opens the online class of the lesson, before the recording
// starts and (TASK-406, owner 2026-11-08) while the recording started from this popup runs. Pure, no React. The page
// never sees the link or the host: the core says only which subject of the lesson has one (PromptApi.callSubject) and
// opens it by that subject ID (LibraryApi.openSubjectCallLink).
// Once the meeting is open the button is not grey: it reads «Открыть ещё раз» and opens the same link again.

import type { PromptView } from "../bindings/prompt.js";
import { callLinkTexts } from "../callLinkView.js";

export const joinTexts = {
  /** The link was removed meanwhile (or the subject is gone): the button goes, «Начать запись» stays. */
  gone: "Ссылки на пару уже нет.",
  /** The visible confirmation before the recording starts: the open does not start it. */
  opened: "Ссылка открыта. Запись пока не идёт.",
  /** While the recording runs the confirmation must not say it is not going. */
  openedRunning: callLinkTexts.opened,
} as const;

/** hidden: no button; ready: «Подключиться»; opening: the call runs; opened: «Открыть ещё раз», clickable. */
export type JoinPhase = "hidden" | "ready" | "opening" | "opened";

export interface JoinState {
  /** The button has a place in the popup now: the offered lesson, or the running recording started from the popup. */
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
 * open that ends after the prompt changed no longer touches the state.
 *
 * What was opened is remembered per prompt ID: the same prompt keeps it over the start of the recording (the offer goes
 * away, then the running view asks again with the same ID) and over a closing and re-showing of the popup (the core
 * hides it right after the start). A different prompt ID starts clean.
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
   * The prompt the button belongs to, or null (nothing to offer it for: the advice, the start runs, no recording from
   * here). It is the offered lesson, or the prompt the recording on screen was started from; the core answers for both.
   * A new prompt starts clean; the same one keeps the state.
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

  /**
   * The popup closed (the page became hidden). What was opened stays: the core hides the popup right after the start,
   * and the running view must still say «Открыть ещё раз». Only the text of a failed open goes; the next showing asks
   * the core again (refresh), so a link removed meanwhile takes the button away.
   */
  closed(): void {
    this.read++;
    this.clearFailure();
  }

  /** A start was tried: the text of an earlier open goes. */
  clearProblem(): void {
    if (this.current.problem !== "") {
      this.set({ ...this.current, problem: "" });
    }
  }

  private clearFailure(): void {
    if (this.current.problem === "failed") {
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

  /**
   * «Подключиться» / «Открыть ещё раз»: opens the link. A second click while the call runs does nothing, so a double
   * click opens once; after it the button can be clicked again.
   */
  async click(): Promise<void> {
    const s = this.current;
    if (!s.shown || (s.phase !== "ready" && s.phase !== "opened")) {
      return;
    }
    const before = s.phase;
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
      // A failed second open leaves the first one standing: the button keeps reading «Открыть ещё раз».
      this.set({ ...now, phase: before, problem: "failed" });
    }
  }
}

/** What the popup draws. */
export interface JoinView {
  /** The button is drawn (in the row of the offered lesson, or of the running recording). */
  visible: boolean;
  busy: boolean;
  /** Only while a call runs, the link is gone or a start/skip runs: never because the meeting is already open. */
  disabled: boolean;
  /** The meeting was opened already: the label is «Открыть ещё раз». */
  opened: boolean;
  label: string;
  title: string;
  /** A quiet line while the meeting is open: «…Запись пока не идёт.» before the recording starts, «Ссылка открыта.» while it runs. */
  confirm: string;
  /** The amber line of a failed open (never the red banner, it must not grow the window); "" when none. */
  failure: string;
  /** A quiet line: the link is gone; "" when none. */
  note: string;
  /**
   * The live regions of the texts area exist: they are drawn (empty) as soon as the button is, so a screen reader has
   * them registered before a text appears in them. They stay for the gone note, which comes as the button goes.
   */
  live: boolean;
}

/**
 * `blocked`: a start or skip runs; it disables this button but never «Начать запись». `running`: the recording runs, so
 * the hover text does not promise that it will not start and the confirmation does not say «Запись пока не идёт».
 * While a repeated open runs the confirmation is off for a moment and comes back with its end: that tells the open
 * happened again, and a screen reader says it again.
 */
export function joinView(s: JoinState, blocked: boolean, running = false): JoinView {
  const opened = s.phase === "opened";
  const clickable = s.phase === "ready" || opened;
  let title: string;
  if (running) {
    title = opened ? callLinkTexts.joinAgainRunningTitle : callLinkTexts.joinRunningTitle;
  } else {
    title = opened ? callLinkTexts.joinAgainTitle : callLinkTexts.joinTitle;
  }
  return {
    visible: s.shown && s.phase !== "hidden",
    busy: s.phase === "opening",
    disabled: !clickable || blocked,
    opened,
    label: opened ? callLinkTexts.joinAgain : callLinkTexts.join,
    title,
    confirm: s.shown && opened && s.problem === "" ? (running ? joinTexts.openedRunning : joinTexts.opened) : "",
    failure: s.shown && s.problem === "failed" ? callLinkTexts.openFailed : "",
    note: s.shown && s.problem === "gone" ? joinTexts.gone : "",
    live: s.shown && (s.phase !== "hidden" || s.problem !== ""),
  };
}

/**
 * Whenever the join button is shown in the offer view it takes its own row: «Начать запись», the join button and
 * «Не записывать» are all full width, in every phase (ready, opening, opened). The layout must not change between phases:
 * the rows are pinned to the bottom, so a row that moves puts another button under the pointer, and the second click of a
 * double click starts the recording or skips the lesson. This deliberately reverses the TASK-370 choice («Подключиться»
 * shares a row with «Не записывать»).
 */
export function joinOnOwnRow(v: Pick<JoinView, "visible">): boolean {
  return v.visible;
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

/** The lesson prompt a successful start from the popup began a recording for; "" for a failed start or an ad-hoc offer. */
export function startedPromptId(
  sent: Pick<PromptView, "id" | "kind"> | null,
  result: Pick<PromptView, "id" | "state">,
): string {
  if (sent === null || sent.kind !== "lesson" || result.id !== sent.id || result.state !== "STARTED") {
    return "";
  }
  return sent.id;
}

/**
 * The prompt the popup started a recording from, kept so the running view can keep asking the core for its call link
 * (PromptService.CallSubject answers for it while that recording runs, although the prompt is no longer current).
 * The prompt is tied to the first recording the popup sees running after the start; it is dropped when that recording
 * ends or another one is shown. A recording that was not started from here never gets an ID: the core answers "".
 */
export class StartedRecording {
  private promptId = "";
  private recordingId = "";

  /** A start from the popup succeeded for this lesson prompt; its recording is not seen yet. */
  started(promptId: string): void {
    this.promptId = promptId;
    this.recordingId = "";
  }

  /**
   * `recordingId`: the recording that captures now, "" when none does, null when the sound could not be read (nothing
   * is learned: a failed read must not make the popup forget its recording). Returns the prompt ID to ask the core
   * with while that recording is the one this popup started; "" otherwise.
   */
  track(recordingId: string | null): string {
    if (this.promptId === "") {
      return "";
    }
    if (recordingId === null) {
      return this.recordingId === "" ? "" : this.promptId;
    }
    if (recordingId === "") {
      // The recording is over: forget it. Before it was seen (not capturing yet) the start still waits for it.
      if (this.recordingId !== "") {
        this.forget();
      }
      return "";
    }
    if (this.recordingId === "") {
      this.recordingId = recordingId;
    } else if (this.recordingId !== recordingId) {
      this.forget();
      return "";
    }
    return this.promptId;
  }

  private forget(): void {
    this.promptId = "";
    this.recordingId = "";
  }
}
