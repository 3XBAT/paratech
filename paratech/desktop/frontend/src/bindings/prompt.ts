// Recording prompt bindings: calls into the Go core (internal/input/wails/prompt.go). The core owns the prompt and
// decides what is offered ("canSnooze"); the popup only shows it.

import type { CallByName } from "./settings";

export type PromptKind = "lesson" | "adhoc";

/** The current recording prompt, as the Go binding returns it. */
export interface PromptView {
  id: string;
  kind: PromptKind;
  /** Imported, untrusted text: render as plain text only. Empty for an ad-hoc prompt. */
  title: string;
  /** "15:04" in the lesson's time zone; empty for an ad-hoc prompt. */
  start: string;
  end: string;
  /** OFFERED, STARTING, STARTED, SNOOZED, SKIPPED, EXPIRED or WITHDRAWN. */
  state: string;
  /** Why the last start failed: "", "unavailable", "in_progress", "recovering" or "failed". */
  problem: string;
  canSnooze: boolean;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  current: "paratech/desktop/internal/input/wails.PromptService.Current",
  start: "paratech/desktop/internal/input/wails.PromptService.Start",
  snooze: "paratech/desktop/internal/input/wails.PromptService.Snooze",
  skip: "paratech/desktop/internal/input/wails.PromptService.Skip",
} as const;

/** Wails event the core emits each time it shows the popup; the page then reads the current prompt. */
export const shownEvent = "recordingprompt:shown";

/** Messages of the errors the Go binding returns; the page may match them. */
export const errorMessages = {
  invalidId: "invalid id",
  gone: "the prompt is no longer current",
  refused: "the action is not possible now",
  actionFailed: "the action failed",
} as const;

function str(r: Record<string, unknown>, key: string): string {
  const v = r[key];
  if (typeof v !== "string") {
    throw new Error(`prompt: ${key} is not a string`);
  }
  return v;
}

/** Checks the shape of a prompt; a malformed reply is an error, not a default. */
export function parsePromptView(raw: unknown): PromptView {
  if (typeof raw !== "object" || raw === null) {
    throw new Error("prompt: reply is not an object");
  }
  const r = raw as Record<string, unknown>;
  const kind = str(r, "kind");
  if (kind !== "lesson" && kind !== "adhoc") {
    throw new Error("prompt: unknown kind");
  }
  if (typeof r.canSnooze !== "boolean") {
    throw new Error("prompt: canSnooze is not a boolean");
  }
  return {
    id: str(r, "id"),
    kind,
    title: str(r, "title"),
    start: str(r, "start"),
    end: str(r, "end"),
    state: str(r, "state"),
    problem: str(r, "problem"),
    canSnooze: r.canSnooze,
  };
}

export interface PromptApi {
  /** The prompt that runs now, or null. */
  current(): Promise<PromptView | null>;
  start(id: string): Promise<PromptView>;
  snooze(id: string): Promise<PromptView>;
  skip(id: string): Promise<PromptView>;
}

/** Builds the prompt API over a call function (the Wails runtime in the app, a fake in tests). */
export function promptApi(call: CallByName): PromptApi {
  return {
    current: async () => {
      const raw = await call(methods.current);
      return raw === null || raw === undefined ? null : parsePromptView(raw);
    },
    start: async (id) => parsePromptView(await call(methods.start, id)),
    snooze: async (id) => parsePromptView(await call(methods.snooze, id)),
    skip: async (id) => parsePromptView(await call(methods.skip, id)),
  };
}
