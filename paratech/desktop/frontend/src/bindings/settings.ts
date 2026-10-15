// Settings bindings: calls into the Go core (internal/input/wails/settings.go). The page never talks to the Hub
// and keeps nothing in browser storage; the core owns the values.

/** Calls a bound Go method by its full name; see runtime.ts. */
export type CallByName = (method: string, ...args: unknown[]) => Promise<unknown>;

/** The toggles of the settings page, as the Go binding returns them. */
export interface Settings {
  /** AC-02: suggest recording automatically when a lesson starts. */
  recordingPromptsEnabled: boolean;
  /** AC-22: heavy processing only on AC power. */
  processOnlyOnAcPower: boolean;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  get: "paratech/desktop/internal/input/wails.SettingsService.GetSettings",
  setRecordingPromptsEnabled: "paratech/desktop/internal/input/wails.SettingsService.SetRecordingPromptsEnabled",
  setProcessOnlyOnAcPower: "paratech/desktop/internal/input/wails.SettingsService.SetProcessOnlyOnACPower",
  setNotifyOnProcessingComplete: "paratech/desktop/internal/input/wails.SettingsService.SetNotifyOnProcessingComplete",
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
  return { recordingPromptsEnabled: r.recordingPromptsEnabled, processOnlyOnAcPower: r.processOnlyOnAcPower };
}

export interface SettingsApi {
  get(): Promise<Settings>;
  setRecordingPromptsEnabled(enabled: boolean): Promise<Settings>;
  setProcessOnlyOnAcPower(enabled: boolean): Promise<Settings>;
}

/** Builds the settings API over a call function (the Wails runtime in the app, a fake in tests). */
export function settingsApi(call: CallByName): SettingsApi {
  return {
    get: async () => parseSettings(await call(methods.get)),
    setRecordingPromptsEnabled: async (enabled) =>
      parseSettings(await call(methods.setRecordingPromptsEnabled, enabled)),
    setProcessOnlyOnAcPower: async (enabled) => parseSettings(await call(methods.setProcessOnlyOnAcPower, enabled)),
  };
}
