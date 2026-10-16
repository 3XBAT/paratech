// "Диагностика" bindings: calls into the Go core (internal/input/wails/diagnostics.go). The core plays the tone, picks
// the file in its own save dialog and writes the bundle; the page never sees a path, only the base name of the file.

import type { CallByName } from "./settings";

/** "ok" or the cause the check found. */
export type SoundVerdict = "ok" | "no_device" | "silence" | "unsupported" | "hands_free" | "busy" | "failed";

/** The capture modes the check tries: the capture of an app and the classic capture of the output device. */
export type SoundModeName = "include" | "system";

/** The result of one capture mode; cause is "ok" or a verdict. */
export interface SoundMode {
  mode: SoundModeName;
  ok: boolean;
  cause: string;
}

/** The answer of "Проверить звук". message and hint are fixed texts of the core; device is a hardware name. */
export interface SoundCheck {
  verdict: SoundVerdict;
  message: string;
  /** "" when unknown. */
  device: string;
  handsFree: boolean;
  /** "" when there is nothing to add. */
  hint: string;
  modes: SoundMode[];
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
  checkSound: "paratech/desktop/internal/input/wails.DiagnosticsService.CheckSound",
  saveSupportBundle: "paratech/desktop/internal/input/wails.DiagnosticsService.SaveSupportBundle",
} as const;

export const soundVerdicts: readonly SoundVerdict[] = [
  "ok", "no_device", "silence", "unsupported", "hands_free", "busy", "failed",
];

export const soundModes: readonly SoundModeName[] = ["include", "system"];

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

function parseSoundMode(raw: unknown): SoundMode {
  const w = "sound mode";
  const r = record(raw, w);
  return { mode: oneOf(r, "mode", soundModes, w), ok: bool(r, "ok", w), cause: str(r, "cause", w) };
}

/** Checks the shape of a sound check answer; a malformed reply is an error, not a default. */
export function parseSoundCheck(raw: unknown): SoundCheck {
  const w = "sound check";
  const r = record(raw, w);
  const modes = r.modes ?? []; // Go sends [], a null list is accepted as empty
  if (!Array.isArray(modes)) {
    throw new Error(`${w}: modes is not a list`);
  }
  return {
    verdict: oneOf(r, "verdict", soundVerdicts, w),
    message: str(r, "message", w),
    device: str(r, "device", w),
    handsFree: bool(r, "handsFree", w),
    hint: str(r, "hint", w),
    modes: modes.map(parseSoundMode),
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
  /** Plays a short tone on the default output device and checks that it is recorded (up to 15 s). */
  checkSound(): Promise<SoundCheck>;
  /** Asks where to save in the native dialog and writes the bundle there; nothing is sent anywhere. */
  saveSupportBundle(): Promise<BundleResult>;
}

/** Builds the API over a call function (the Wails runtime in the app, a fake in tests). */
export function diagnosticsApi(call: CallByName): DiagnosticsApi {
  return {
    checkSound: async () => parseSoundCheck(await call(methods.checkSound)),
    saveSupportBundle: async () => parseBundleResult(await call(methods.saveSupportBundle)),
  };
}
