// «Отменить расшифровку» and «Расшифровать снова» on a recording row (TASK-393): texts, the «ask first» preference, the
// outcome of each core answer and the one-call guard. Pure, no React; the core decides what is offered (ARCH-44).

import type { CancelTranscriptionResult, RecordingItem, RetranscribeResult } from "./bindings/activity.js";
import type { KeyValueStore } from "./subjectsHome.js";

export const cancelTexts = {
  /** The tooltip and the accessible name of the cancel icon. */
  action: "Отменить расшифровку",
  title: "Отменить расшифровку?",
  body: "Запись останется, расшифровать её можно будет снова.",
  confirm: "Да, отменить",
  keep: "Не отменять",
  skip: "Больше не спрашивать",
} as const;

export const retranscribeTexts = {
  busy: "Запуск…",
  openModels: "Открыть модели",
} as const;

export const noteTexts = {
  cancelEnding: "Расшифровка уже завершается. Если она ещё идёт, отмените её ещё раз.",
  cancelFailed: "Не удалось отменить расшифровку.",
  noModel: "Нет модели распознавания.",
  retranscribeFailed: "Не удалось запустить расшифровку.",
} as const;

/** The cancel icon sits on a row that shows the progress or waits for recognition and that the core lets cancel. */
export function rowOffersCancel(r: Pick<RecordingItem, "stage" | "canCancel">): boolean {
  return r.canCancel && (r.stage === "transcribing" || r.stage === "queued");
}

/** The open confirmation closes by itself once its row stops offering the cancel (it ended or was cancelled elsewhere). */
export function cancelAskOpen(askId: string | null, rows: readonly Pick<RecordingItem, "id" | "stage" | "canCancel">[]): boolean {
  return askId !== null && rows.some((r) => r.id === askId && rowOffersCancel(r));
}

/** Where «Больше не спрашивать» is kept: a view preference of the window, like the theme (not a secret). */
export const cancelSkipKey = "paratech.confirm.cancelTranscription.skip";

const skipValue = "1";

/** True once the student chose not to be asked; a missing or failing storage means: ask. */
export function cancelAskSkipped(store: KeyValueStore | null): boolean {
  if (store === null) {
    return false;
  }
  try {
    return store.getItem(cancelSkipKey) === skipValue;
  } catch {
    return false;
  }
}

/** The first cancel asks; after «Больше не спрашивать» it runs at once. */
export function cancelDecision(store: KeyValueStore | null): "ask" | "run" {
  return cancelAskSkipped(store) ? "run" : "ask";
}

/** «Да, отменить»: remembers the choice only when the box is checked; a failing storage is ignored (the dialog asks again). */
export function confirmCancel(store: KeyValueStore | null, skipNext: boolean): void {
  if (store === null || !skipNext) {
    return;
  }
  try {
    store.setItem(cancelSkipKey, skipValue);
  } catch {
    // Quota or a blocked storage: the next cancel simply asks again.
  }
}

/** A short line under the row's status after an action that did not simply work; keyed by recording on the page. */
export interface RowNote {
  /** The action it answers: it shows only while the row still offers that action. */
  kind: "cancel" | "retranscribe";
  text: string;
  /** The way out: the small «Открыть модели» button next to the text. */
  openModels: boolean;
}

/** The note after a cancel. «cancelled» and «not_running» only refresh (the row already tells the state). */
export function cancelNote(result: CancelTranscriptionResult): RowNote | null {
  switch (result) {
    case "ending":
      return { kind: "cancel", text: noteTexts.cancelEnding, openModels: false };
    case "failed":
      return { kind: "cancel", text: noteTexts.cancelFailed, openModels: false };
    default:
      return null;
  }
}

/** The note after «Расшифровать снова». «started» and «not_allowed» only refresh: the row then shows the real state. */
export function retranscribeNote(result: RetranscribeResult): RowNote | null {
  switch (result) {
    case "no_model":
      return { kind: "retranscribe", text: noteTexts.noModel, openModels: true };
    case "failed":
      return { kind: "retranscribe", text: noteTexts.retranscribeFailed, openModels: false };
    default:
      return null;
  }
}

/** A note is shown only while it still matches the row: a stale one (the state moved on) is not. */
export function noteVisible(note: RowNote | null | undefined, r: Pick<RecordingItem, "stage" | "canCancel" | "action">): boolean {
  if (note === null || note === undefined) {
    return false;
  }
  return note.kind === "cancel" ? rowOffersCancel(r) : r.action === "retranscribe";
}

export type FlightKind = "cancel" | "retranscribe";

/** The key of a call in flight: per recording and per kind. It is also the page's busy key. */
export function flightKey(kind: FlightKind, recordingId: string): string {
  return `${kind}:${recordingId}`;
}

/**
 * The calls in flight. A ref holds it, not React state: two clicks before the next render must start one call.
 * begin() is true for the call that may run.
 */
export class Flights {
  private readonly keys = new Set<string>();

  begin(key: string): boolean {
    if (this.keys.has(key)) {
      return false;
    }
    this.keys.add(key);
    return true;
  }

  end(key: string): void {
    this.keys.delete(key);
  }
}
