// Speech recognition component bindings: calls into the Go core (internal/input/wails/speech.go). The page never sends
// a URL or a path: the core owns the source of the component. Messages are fixed Russian texts shown as plain text.

import type { CallByName } from "./settings";

export type SpeechState = "ready" | "missing" | "damaged" | "installing" | "failed";

/** "" unless installing. */
export type SpeechStage = "" | "downloading" | "verifying" | "unpacking" | "activating";

export interface SpeechStatus {
  state: SpeechState;
  stage: SpeechStage;
  doneBytes: number;
  totalBytes: number;
  /** The text to show; "" when ready or installing. */
  message: string;
  /** Offer «Установить компонент распознавания». */
  canInstall: boolean;
  /** Offer the cancel of the running install. */
  canCancel: boolean;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  status: "paratech/desktop/internal/input/wails.SpeechService.Status",
  startInstall: "paratech/desktop/internal/input/wails.SpeechService.StartInstall",
  cancelInstall: "paratech/desktop/internal/input/wails.SpeechService.CancelInstall",
} as const;

const states: readonly string[] = ["ready", "missing", "damaged", "installing", "failed"];
const stages: readonly string[] = ["", "downloading", "verifying", "unpacking", "activating"];

function field<T>(r: Record<string, unknown>, key: string, ok: (v: unknown) => v is T): T {
  const v = r[key];
  if (!ok(v)) {
    throw new Error(`speech status: ${key} is not valid`);
  }
  return v;
}

const isString = (v: unknown): v is string => typeof v === "string";
const isBool = (v: unknown): v is boolean => typeof v === "boolean";
const isCount = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= 0;
const isState = (v: unknown): v is SpeechState => typeof v === "string" && states.includes(v);
const isStage = (v: unknown): v is SpeechStage => typeof v === "string" && stages.includes(v);

/** Checks the shape of a reply; a malformed reply or an unknown state or stage is an error, not a default. */
export function parseStatus(raw: unknown): SpeechStatus {
  if (typeof raw !== "object" || raw === null) {
    throw new Error("speech status: reply is not an object");
  }
  const r = raw as Record<string, unknown>;
  return {
    state: field(r, "state", isState),
    stage: field(r, "stage", isStage),
    doneBytes: field(r, "doneBytes", isCount),
    totalBytes: field(r, "totalBytes", isCount),
    message: field(r, "message", isString),
    canInstall: field(r, "canInstall", isBool),
    canCancel: field(r, "canCancel", isBool),
  };
}

export interface SpeechApi {
  status(): Promise<SpeechStatus>;
  /** Starts the install in the background; status shows its progress. */
  startInstall(): Promise<void>;
  cancelInstall(): Promise<void>;
}

/** Builds the speech component API over a call function (the Wails runtime in the app, a fake in tests). */
export function speechApi(call: CallByName): SpeechApi {
  return {
    status: async () => parseStatus(await call(methods.status)),
    startInstall: async () => {
      await call(methods.startInstall);
    },
    cancelInstall: async () => {
      await call(methods.cancelInstall);
    },
  };
}
