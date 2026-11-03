// View logic of the schedule_subjects card of the agent panel (TASK-324, ADR-0033 §4). The card shows the same check as
// the «Предметы» review (the rows, the choices, the words of scheduleSubjectsView) inside the chat; «Да» applies the
// student's choices through the core (AgentConfirmService.ConfirmSchedule, which runs the page's own create), «Отменить
// всё» takes the outcome back. Names, spellings and subject names are schedule text: text nodes only, never logged.
// The voice is impersonal; every state says what happened and offers the next step.

import {
  type AgentConfirmApi,
  type ConfirmCard,
  confirmErrorText,
  genericError,
  scheduleUndoGoneText,
} from "./bindings/agentConfirm.js";
import type { ScheduleChoice } from "./bindings/library.js";
import { plural } from "./plural.js";
import {
  aliasCount,
  buildChoices,
  omittedAfter,
  reviewTexts,
  type ReviewState,
  skippedLines,
  splitSkipped,
  subjectsWord,
  undoLines,
} from "./scheduleSubjectsView.js";
import type { RemovedRow } from "./subjectRemovalView.js";

export const scheduleTexts = {
  /** A small line inside the card while the core is asked. */
  loading: "Загрузка предметов…",
  /** The button after a failed read (the glossary: «Повторить»). */
  reload: "Повторить",
  no: "Нет",
  yes: "Да",
  declined: "Оставлено без изменений",
  openSubjects: "Открыть «Предметы»",
  /** The core's message is empty (an older core): the card still says why it cannot go on. */
  staleFallback: "Предложение устарело: расписание или предметы изменились.",
  partialFallback: "Создано не всё. Нажмите «Да» ещё раз — уже созданное не повторится, или «Отменить всё».",
  /** Nothing is left to choose from: every name of the schedule is already a subject. */
  noGroups: reviewTexts.allDone,
} as const;

/** «Заведено 3 предмета · Добавлено названий: 2»; "" when nothing was done. «Отменить всё» is offered exactly then. */
export function doneSummary(created: number, aliases: number): string {
  const parts: string[] = [];
  if (created > 0) {
    parts.push(`Заведено ${subjectsWord(created)}`);
  }
  if (aliases > 0) {
    parts.push(`Добавлено названий: ${aliases}`);
  }
  return parts.join(" · ");
}

/** «Отменено: убрано 2 предмета и 1 название»; «Ничего не убрано» when everything stayed. */
export function undoSummary(removed: number, unaliased: number): string {
  const parts: string[] = [];
  if (removed > 0) {
    parts.push(subjectsWord(removed));
  }
  if (unaliased > 0) {
    parts.push(`${unaliased} ${plural(unaliased, "название", "названия", "названий")}`);
  }
  return parts.length > 0 ? `Отменено: убрано ${parts.join(" и ")}` : reviewTexts.nothingRemoved;
}

/** What the card draws for each state. */
export type ScheduleModel =
  | { view: "waiting"; groups: number }
  | {
      /** done, or confirmed (stopped midway): what was created, the lines of what was left out and the rows with «Вернуть». */
      view: "done" | "confirmed";
      summary: string;
      lines: string[];
      rows: RemovedRow[];
      /** The core's message of a stop midway; "" for done. */
      message: string;
      /** «Да» again (only a stop midway). */
      yes: boolean;
      /** «Отменить всё»: something was created or added. */
      undo: boolean;
      /** The names the proposal left out wait for a choice: the way to them is «Предметы». */
      rest: boolean;
    }
  | { view: "stale"; message: string }
  | { view: "undone"; summary: string; lines: string[] }
  | { view: "declined"; text: string };

/**
 * The model of a card the core sent. `review` holds what the card showed while it waited (the groups and the edited
 * names), so a row of the outcome can be called by the name the student saw; a card read afresh has none and the rows
 * are called by the subject. `names` are the subjects by id.
 */
export function scheduleModel(card: ConfirmCard, review: ReviewState, names: ReadonlyMap<string, string>): ScheduleModel {
  const data = card.schedule;
  switch (card.state) {
    case "done":
    case "confirmed": {
      const result = data?.result ?? null;
      const created = result?.created.length ?? 0;
      const aliases = result === null ? 0 : aliasCount(result.aliased);
      const { rows, plain } = splitSkipped(review, result?.skipped ?? []);
      const rest = review.omitted > 0;
      const nothing = created + aliases === 0;
      const summary = doneSummary(created, aliases) || (nothing && (result?.skipped.length ?? 0) > 0 ? reviewTexts.nothingCreated : "");
      const confirmed = card.state === "confirmed";
      return {
        view: card.state,
        summary,
        lines: [...skippedLines(review, plain, names), ...(rest ? [omittedAfter(review.omitted)] : [])],
        rows,
        message: confirmed ? card.message || scheduleTexts.partialFallback : "",
        yes: confirmed,
        undo: !nothing,
        rest,
      };
    }
    case "undone": {
      const undo = data?.undo ?? null;
      if (undo === null) {
        return { view: "undone", summary: undoSummary(0, 0), lines: [] };
      }
      return {
        view: "undone",
        summary: undoSummary(undo.removed.length, aliasCount(undo.unaliased)),
        lines: undoLines({ reply: undo, remaining: [], thrown: false, names, invalid: 0, tried: 1 }),
      };
    }
    case "stale":
    case "rejected":
      return { view: "stale", message: card.message || scheduleTexts.staleFallback };
    case "declined":
      return { view: "declined", text: scheduleTexts.declined };
    case "waiting":
      return { view: "waiting", groups: data?.groups.length ?? 0 };
  }
}

/** The choices «Да» sends: the page's own (buildChoices); [] when the card shows no groups (a repeat after a stop midway). */
export function choicesOf(review: ReviewState): ScheduleChoice[] {
  return review.groups.length === 0 ? [] : buildChoices(review.groups, review.selection);
}

/** Whether a refusal of the core leaves the student with nothing to press but the page: it names «Предметы» as the way. */
export function refusalOpensSubjects(error: string): boolean {
  return error === scheduleUndoGoneText;
}

// ---- Flow ----

export interface ScheduleFlowState {
  card: ConfirmCard | null;
  /** A call runs: the buttons are off. */
  busy: "" | "confirm" | "decline" | "undo";
  /** A fixed Russian text of a refused call or of a failed read; "" when none. */
  error: string;
}

export const initialScheduleFlow: ScheduleFlowState = { card: null, busy: "", error: "" };

/**
 * The calls of one schedule_subjects card. Keeps a guard outside the rendering: a second click arriving before the page
 * re-rendered finds the first call still running and does nothing, so one click is one action. A refusal keeps the card
 * and its buttons as they were and shows the core's text beside them.
 */
export class ScheduleCardFlow {
  private current: ScheduleFlowState = initialScheduleFlow;
  private pending = false;
  private disposed = false;

  constructor(
    private readonly api: AgentConfirmApi,
    private readonly token: string,
    private readonly onChange: (s: ScheduleFlowState) => void,
  ) {}

  get state(): ScheduleFlowState {
    return this.current;
  }

  /** The panel shows the card again (a strict-mode remount re-attaches the same flow). */
  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private set(patch: Partial<ScheduleFlowState>): void {
    this.current = { ...this.current, ...patch };
    if (!this.disposed) {
      this.onChange(this.current);
    }
  }

  /** The card of the token; a card that cannot be read keeps what is shown and says why. */
  async load(): Promise<void> {
    try {
      const card = await this.api.card({ kind: "schedule_subjects", id: this.token });
      if (card.kind !== "schedule_subjects" || card.schedule === null) {
        this.set({ error: genericError });
        return;
      }
      this.set({ card, error: "" });
    } catch (err) {
      this.set({ error: confirmErrorText(err) });
    }
  }

  private async call(busy: "confirm" | "decline" | "undo", make: () => Promise<ConfirmCard>): Promise<void> {
    if (this.pending) {
      return;
    }
    this.pending = true;
    this.set({ busy, error: "" });
    try {
      const card = await make();
      this.set({ card, busy: "" });
    } catch (err) {
      this.set({ busy: "", error: confirmErrorText(err) });
    } finally {
      this.pending = false;
    }
  }

  /** «Да» (waiting) or «Да» again (stopped midway): only these two states may go to the core. */
  async yes(choices: ScheduleChoice[]): Promise<void> {
    const state = this.current.card?.state;
    if (state !== "waiting" && state !== "confirmed") {
      return;
    }
    await this.call("confirm", () => this.api.confirmSchedule(this.token, choices));
  }

  /** «Нет»: nothing is created. */
  async no(): Promise<void> {
    if (this.current.card?.state !== "waiting") {
      return;
    }
    await this.call("decline", () => this.api.decline({ kind: "schedule_subjects", id: this.token }));
  }

  /** «Отменить всё» after a create (also one that stopped midway). */
  async undo(): Promise<void> {
    const state = this.current.card?.state;
    if (state !== "done" && state !== "confirmed") {
      return;
    }
    await this.call("undo", () => this.api.undoSchedule(this.token));
  }
}
