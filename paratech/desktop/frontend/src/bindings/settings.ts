// Settings bindings: calls into the Go core (internal/input/wails/settings.go). The page never talks to the Hub
// and keeps nothing in browser storage; the core owns the values.

import { parseModelLoadEstimates, parseRecognitionLoad, type ModelLoadEstimates, type RecognitionLoad } from "./load.js";

/** Calls a bound Go method by its full name; see runtime.ts. */
export type CallByName = (method: string, ...args: unknown[]) => Promise<unknown>;

/** The toggles of the settings page, as the Go binding returns them. */
export interface Settings {
  /** AC-02: suggest recording automatically when a lesson starts. */
  recordingPromptsEnabled: boolean;
  /** AC-22: heavy processing only on AC power. */
  processOnlyOnAcPower: boolean;
  /** «Нагрузка при распознавании» (TASK-084); applies from the next recognition run. */
  recognitionLoad: RecognitionLoad;
  /** The estimates of the model new recordings use; null without one. */
  loadEstimates: ModelLoadEstimates | null;
  /** The name of that model; "" with null estimates. */
  loadEstimatesModel: string;
}

/** «Профиль студента» (TASK-094): the title page of a homework Word file names these; "" is not set. */
export interface StudentProfile {
  fullName: string;
  group: string;
  university: string;
  /** Faculty or department. */
  faculty: string;
  city: string;
}

export const profileFields: readonly (keyof StudentProfile)[] = ["fullName", "group", "university", "faculty", "city"];

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  profileInvalid: "Каждое поле профиля — одна строка до 200 символов без управляющих символов.",
} as const;

/** The two switches of the page. */
export type SettingsToggle = "recordingPromptsEnabled" | "processOnlyOnAcPower";

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  get: "paratech/desktop/internal/input/wails.SettingsService.GetSettings",
  setRecordingPromptsEnabled: "paratech/desktop/internal/input/wails.SettingsService.SetRecordingPromptsEnabled",
  setProcessOnlyOnAcPower: "paratech/desktop/internal/input/wails.SettingsService.SetProcessOnlyOnACPower",
  setRecognitionLoad: "paratech/desktop/internal/input/wails.SettingsService.SetRecognitionLoad",
  getProfile: "paratech/desktop/internal/input/wails.SettingsService.GetProfile",
  saveProfile: "paratech/desktop/internal/input/wails.SettingsService.SaveProfile",
} as const;

/** Checks the shape of a reply; a malformed reply is an error, not a default. */
export function parseSettings(raw: unknown): Settings {
  if (typeof raw !== "object" || raw === null) {
    throw new Error("settings: reply is not an object");
  }
  const r = raw as Record<string, unknown>;
  if (typeof r.recordingPromptsEnabled !== "boolean" || typeof r.processOnlyOnAcPower !== "boolean") {
    throw new Error("settings: reply has no toggles");
  }
  if (typeof r.loadEstimatesModel !== "string") {
    throw new Error("settings: loadEstimatesModel is not a string");
  }
  return {
    recordingPromptsEnabled: r.recordingPromptsEnabled,
    processOnlyOnAcPower: r.processOnlyOnAcPower,
    recognitionLoad: parseRecognitionLoad(r.recognitionLoad, "settings"),
    loadEstimates: parseModelLoadEstimates(r.loadEstimates),
    loadEstimatesModel: r.loadEstimatesModel,
  };
}

export function parseProfile(raw: unknown): StudentProfile {
  if (typeof raw !== "object" || raw === null) {
    throw new Error("profile: reply is not an object");
  }
  const r = raw as Record<string, unknown>;
  const out = {} as StudentProfile;
  for (const f of profileFields) {
    const v = r[f];
    if (typeof v !== "string") {
      throw new Error(`profile: ${f} is not a string`);
    }
    out[f] = v;
  }
  return out;
}

export interface SettingsApi {
  get(): Promise<Settings>;
  setRecordingPromptsEnabled(enabled: boolean): Promise<Settings>;
  setProcessOnlyOnAcPower(enabled: boolean): Promise<Settings>;
  setRecognitionLoad(load: RecognitionLoad): Promise<Settings>;
  getProfile(): Promise<StudentProfile>;
  /** Stores the profile; returns it as stored (trimmed). */
  saveProfile(profile: StudentProfile): Promise<StudentProfile>;
}

/** Builds the settings API over a call function (the Wails runtime in the app, a fake in tests). */
export function settingsApi(call: CallByName): SettingsApi {
  return {
    get: async () => parseSettings(await call(methods.get)),
    setRecordingPromptsEnabled: async (enabled) =>
      parseSettings(await call(methods.setRecordingPromptsEnabled, enabled)),
    setProcessOnlyOnAcPower: async (enabled) => parseSettings(await call(methods.setProcessOnlyOnAcPower, enabled)),
    setRecognitionLoad: async (load) => parseSettings(await call(methods.setRecognitionLoad, load)),
    getProfile: async () => parseProfile(await call(methods.getProfile)),
    saveProfile: async (profile) => parseProfile(await call(methods.saveProfile, profile)),
  };
}
