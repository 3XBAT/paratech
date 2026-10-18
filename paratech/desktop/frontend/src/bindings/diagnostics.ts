// "Диагностика" bindings: calls into the Go core (internal/input/wails/diagnostics.go). The core captures the sound
// for the check (nothing is played or recorded), picks the file in its own save dialog and writes the bundle; the page
// never sees a path, only the base name of the file.

import type { CallByName } from "./settings";

/** The state of the last sound check. */
export type SoundState = "idle" | "running" | "done";

/** The verdict of a finished check; "" while none is finished. */
export type SoundVerdict = "" | "ok" | "silence" | "not_running" | "no_device" | "unsupported" | "busy" | "failed";

/**
 * The state of "Проверить звук". level is the latest level and peak the loudest, both 0..1; heard tells that sound
 * was heard. message and hints are fixed texts of the core; source is the display name of what is checked (an
 * executable name of an app is untrusted: render it as text).
 */
export interface SoundCheck {
  state: SoundState;
  level: number;
  peak: number;
  heard: boolean;
  elapsedMs: number;
  remainingMs: number;
  durationMs: number;
  verdict: SoundVerdict;
  message: string;
  hints: string[];
  source: string;
  wholeComputer: boolean;
}

export type BundleStatus = "saved" | "cancelled" | "failed";

/** The answer of "Сохранить пакет для поддержки"; name is the base name of the file, set only when saved. */
export interface BundleResult {
  status: BundleStatus;
  message: string;
  name: string;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  startSoundCheck: "paratech/desktop/internal/input/wails.DiagnosticsService.StartSoundCheck",
  soundCheckStatus: "paratech/desktop/internal/input/wails.DiagnosticsService.SoundCheckStatus",
  stopSoundCheck: "paratech/desktop/internal/input/wails.DiagnosticsService.StopSoundCheck",
  saveSupportBundle: "paratech/desktop/internal/input/wails.DiagnosticsService.SaveSupportBundle",
} as const;

export const soundStates: readonly SoundState[] = ["idle", "running", "done"];

export const soundVerdicts: readonly SoundVerdict[] = [
  "", "ok", "silence", "not_running", "no_device", "unsupported", "busy", "failed",
];

export const bundleStatuses: readonly BundleStatus[] = ["saved", "cancelled", "failed"];

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null) {
    throw new Error(`${what}: reply is not an object`);
  }
  return raw as Record<string, unknown>;
}

function str(r: Record<string, unknown>, key: string, what: string): string {
  const v = r[key];
  if (typeof v !== "string") {
    throw new Error(`${what}: ${key} is not a string`);
  }
  return v;
}

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

function oneOf<T extends string>(r: Record<string, unknown>, key: string, allowed: readonly T[], what: string): T {
  const v = r[key];
  if (!allowed.includes(v as T)) {
    throw new Error(`${what}: ${key} is not a known value`);
  }
  return v as T;
}

function fraction(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isFinite(v) || v < 0 || v > 1) {
    throw new Error(`${what}: ${key} is not a level`);
  }
  return v;
}

function millis(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
    throw new Error(`${what}: ${key} is not a duration`);
  }
  return v;
}

/** Checks the shape of a sound check reply; a malformed reply is an error, not a default. */
export function parseSoundCheck(raw: unknown): SoundCheck {
  const w = "sound check";
  const r = record(raw, w);
  const state = oneOf(r, "state", soundStates, w);
  const verdict = oneOf(r, "verdict", soundVerdicts, w);
  if ((state === "done") !== (verdict !== "")) {
    throw new Error(`${w}: verdict does not match the state`);
  }
  const hints = r.hints ?? []; // Go sends [], a null list is accepted as empty
  if (!Array.isArray(hints) || !hints.every((h) => typeof h === "string")) {
    throw new Error(`${w}: hints is not a list of texts`);
  }
  return {
    state,
    level: fraction(r, "level", w),
    peak: fraction(r, "peak", w),
    heard: bool(r, "heard", w),
    elapsedMs: millis(r, "elapsedMs", w),
    remainingMs: millis(r, "remainingMs", w),
    durationMs: millis(r, "durationMs", w),
    verdict,
    message: str(r, "message", w),
    hints: hints as string[],
    source: str(r, "source", w),
    wholeComputer: bool(r, "wholeComputer", w),
  };
}

/** Checks the shape of a bundle answer: a saved bundle has a name, any other has none. */
export function parseBundleResult(raw: unknown): BundleResult {
  const w = "bundle result";
  const r = record(raw, w);
  const status = oneOf(r, "status", bundleStatuses, w);
  const name = str(r, "name", w);
  if ((status === "saved") !== (name !== "")) {
    throw new Error(`${w}: name does not match the status`);
  }
  return { status, message: str(r, "message", w), name };
}

export interface DiagnosticsApi {
  /** Starts the sound check (about 15 s): nothing is played, the chosen source is listened to; a running one is returned. */
  startSoundCheck(): Promise<SoundCheck>;
  /** The state of the last check; poll it while the check runs. */
  soundCheckStatus(): Promise<SoundCheck>;
  /** Ends a running check early. */
  stopSoundCheck(): Promise<SoundCheck>;
  /** Asks where to save in the native dialog and writes the bundle there; nothing is sent anywhere. */
  saveSupportBundle(): Promise<BundleResult>;
}

/** Builds the API over a call function (the Wails runtime in the app, a fake in tests). */
export function diagnosticsApi(call: CallByName): DiagnosticsApi {
  return {
    startSoundCheck: async () => parseSoundCheck(await call(methods.startSoundCheck)),
    soundCheckStatus: async () => parseSoundCheck(await call(methods.soundCheckStatus)),
    stopSoundCheck: async () => parseSoundCheck(await call(methods.stopSoundCheck)),
    saveSupportBundle: async () => parseBundleResult(await call(methods.saveSupportBundle)),
  };
}
