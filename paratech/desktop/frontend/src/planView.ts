// View logic of the plan card of the agent panel (TASK-318): how a long plan collapses, the Russian wording of the
// counters and the step markers of an executing plan. The core decides the plan and the statuses (ARCH-44); step texts
// and the summary are model output and are rendered as text nodes only, never as HTML.

import type { StepState } from "./bindings/agent.js";

/** The steps shown while a long plan is collapsed. */
export const planCollapseLimit = 8;

export const planTexts = {
  stepsLabel: "Шаги плана",
  collapse: "Свернуть",
  /** Visually hidden after a finished step. */
  done: "выполнен",
  /** Visible, in the negative colour, after a failed step. */
  failed: "не удался",
  /** Visually hidden after the step being executed. */
  running: "выполняется",
} as const;

/** 1 шаг, 2–4 шага, 5–20 шагов, 21 шаг, 22 шага, 111 шагов. */
function pluralRu(n: number, one: string, few: string, many: string): string {
  const n10 = n % 10;
  const n100 = n % 100;
  if (n10 === 1 && n100 !== 11) {
    return one;
  }
  if (n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14)) {
    return few;
  }
  return many;
}

/** «1 шаг», «2 шага», «5 шагов». */
export function stepsWord(n: number): string {
  return `${n} ${pluralRu(n, "шаг", "шага", "шагов")}`;
}

/** The text button under a collapsed list: «Ещё 22 шага». */
export function moreStepsLabel(hidden: number): string {
  return `Ещё ${stepsWord(hidden)}`;
}

/** The first button of an awaiting plan; one «Да» covers the whole plan. A plan with no steps keeps the plain «Да». */
export function approveLabel(total: number): string {
  return total > 0 ? `Да, выполнить ${stepsWord(total)}` : "Да";
}

export interface PlanWindow {
  /** Steps in the list. */
  shown: number;
  /** Steps left out of the list (0 when everything is shown). */
  hidden: number;
  /** The toggle button is offered. */
  canToggle: boolean;
  /** The whole list is shown (the student asked for it, or a failed step lies beyond the limit). */
  expanded: boolean;
  /** The list scrolls inside the card. */
  scroll: boolean;
}

/**
 * Which steps a plan of `total` steps shows. `failedIndex` is the index of a failed step, -1 for none: a failure beyond
 * the first steps forces the whole list and takes the toggle away, so the failure stays visible.
 */
export function planWindow(total: number, expanded: boolean, failedIndex: number): PlanWindow {
  const long = total > planCollapseLimit;
  if (!long) {
    return { shown: total, hidden: 0, canToggle: false, expanded: true, scroll: false };
  }
  const forced = failedIndex >= planCollapseLimit;
  if (forced || expanded) {
    return { shown: total, hidden: 0, canToggle: !forced, expanded: true, scroll: true };
  }
  return { shown: planCollapseLimit, hidden: total - planCollapseLimit, canToggle: true, expanded: false, scroll: false };
}

/** What the core said about one step; absent means it did not say. */
export type StepStates = readonly (StepState | undefined)[];

/** The core reports step states (at least one step carries the field). */
export function hasStepStates(states: StepStates): boolean {
  return states.some((s) => s !== undefined);
}

export type StepMark = "pending" | "done" | "failed" | "current";

/**
 * The marker of every step, or [] when the core sent no states (never guessed). The first unfinished step of an
 * executing plan is the current one, unless a step has failed.
 */
export function stepMarks(states: StepStates, status: string): StepMark[] {
  if (!hasStepStates(states)) {
    return [];
  }
  const marks = states.map((s): StepMark => (s === "done" ? "done" : s === "failed" ? "failed" : "pending"));
  if (status === "EXECUTING" && !marks.includes("failed")) {
    const i = marks.indexOf("pending");
    if (i >= 0) {
      marks[i] = "current";
    }
  }
  return marks;
}

/** Index of the last failed step, -1 for none. */
export function failedStepIndex(marks: readonly StepMark[]): number {
  return marks.lastIndexOf("failed");
}

export interface PlanProgress {
  /** 1-based number of the step being executed, never above `total`. */
  current: number;
  total: number;
}

/** «Шаг k из N» while the request executes and the core reports states; null otherwise. */
export function planProgress(states: StepStates, status: string): PlanProgress | null {
  if (status !== "EXECUTING" || states.length === 0 || !hasStepStates(states)) {
    return null;
  }
  const done = states.filter((s) => s === "done").length;
  return { current: Math.min(done + 1, states.length), total: states.length };
}

export function progressLabel(p: PlanProgress): string {
  return `Шаг ${p.current} из ${p.total}`;
}
