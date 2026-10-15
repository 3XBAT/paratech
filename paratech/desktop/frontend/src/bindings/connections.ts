// "Подключения" bindings: calls into the Go core (internal/input/wails/connections.go). The page never talks to the
// Hub or the mail server itself, and no reply carries a secret: the mail password is only a status.

import type { CallByName } from "./settings";

export type MailSecurity = "starttls" | "tls";

/** Where a secret stands; "unsupported" means there is no secret store in this system. */
export type SecretStatus = "set" | "unset" | "unsupported" | "unavailable";

/** The section as the Go binding returns it. */
export interface Connections {
  hubUrl: string;
  recipient: string;
  smtpHost: string;
  /** 0: the default port of the mode. */
  smtpPort: number;
  smtpSecurity: MailSecurity;
  smtpLogin: string;
  mailConfigured: boolean;
  mailPassword: SecretStatus;
}

/** The mail account the page submits; an empty string removes a value. */
export interface MailSettingsInput {
  recipient: string;
  smtpHost: string;
  smtpPort: number;
  smtpSecurity: string;
  smtpLogin: string;
}

/** A refused field with the rule it breaks (a fixed text, never the value). */
export interface FieldError {
  field: string;
  message: string;
}

/** Saved false: nothing was stored, errors lists every refused field and connections is null. */
export interface SaveResult {
  saved: boolean;
  connections: Connections | null;
  errors: FieldError[];
}

/** The status after a change of a secret and a fixed text for the page. */
export interface SecretResult {
  status: SecretStatus;
  message: string;
}

export interface CheckResult {
  ok: boolean;
  message: string;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  get: "paratech/desktop/internal/input/wails.ConnectionsService.GetConnections",
  saveMailSettings: "paratech/desktop/internal/input/wails.ConnectionsService.SaveMailSettings",
  saveHubUrl: "paratech/desktop/internal/input/wails.ConnectionsService.SaveHubURL",
  setMailPassword: "paratech/desktop/internal/input/wails.ConnectionsService.SetMailPassword",
  removeMailPassword: "paratech/desktop/internal/input/wails.ConnectionsService.RemoveMailPassword",
  checkMail: "paratech/desktop/internal/input/wails.ConnectionsService.CheckMail",
} as const;

/** The only error text the binding rejects with (ErrConnectionsUnavailable). */
export const errorMessages = {
  unavailable: "connections settings are unavailable",
} as const;

export const secretStatuses: readonly SecretStatus[] = ["set", "unset", "unsupported", "unavailable"];

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

function secretStatus(r: Record<string, unknown>, key: string, what: string): SecretStatus {
  const v = r[key];
  if (!secretStatuses.includes(v as SecretStatus)) {
    throw new Error(`${what}: ${key} is not a secret status`);
  }
  return v as SecretStatus;
}

/** Checks the shape of the section; a malformed reply is an error, not a default. */
export function parseConnections(raw: unknown): Connections {
  const w = "connections";
  const r = record(raw, w);
  const port = r.smtpPort;
  if (typeof port !== "number" || !Number.isInteger(port) || port < 0) {
    throw new Error(`${w}: smtpPort is not a port`);
  }
  const security = r.smtpSecurity;
  if (security !== "starttls" && security !== "tls") {
    throw new Error(`${w}: smtpSecurity is not a mode`);
  }
  return {
    hubUrl: str(r, "hubUrl", w),
    recipient: str(r, "recipient", w),
    smtpHost: str(r, "smtpHost", w),
    smtpPort: port,
    smtpSecurity: security,
    smtpLogin: str(r, "smtpLogin", w),
    mailConfigured: bool(r, "mailConfigured", w),
    mailPassword: secretStatus(r, "mailPassword", w),
  };
}

function parseFieldError(raw: unknown): FieldError {
  const w = "field error";
  const r = record(raw, w);
  return { field: str(r, "field", w), message: str(r, "message", w) };
}

/** Checks the shape of a save answer. */
export function parseSaveResult(raw: unknown): SaveResult {
  const w = "save result";
  const r = record(raw, w);
  const saved = bool(r, "saved", w);
  const errors = r.errors ?? []; // Go sends [], a null list is accepted as empty
  if (!Array.isArray(errors)) {
    throw new Error(`${w}: errors is not a list`);
  }
  const connections = saved ? parseConnections(r.connections) : null;
  if (!saved && errors.length === 0) {
    throw new Error(`${w}: not saved and no field errors`);
  }
  return { saved, connections, errors: errors.map(parseFieldError) };
}

/** Checks the shape of a secret change answer. */
export function parseSecretResult(raw: unknown): SecretResult {
  const w = "secret result";
  const r = record(raw, w);
  return { status: secretStatus(r, "status", w), message: str(r, "message", w) };
}

/** Checks the shape of a mail check answer. */
export function parseCheckResult(raw: unknown): CheckResult {
  const w = "check result";
  const r = record(raw, w);
  return { ok: bool(r, "ok", w), message: str(r, "message", w) };
}

export interface ConnectionsApi {
  get(): Promise<Connections>;
  saveMailSettings(input: MailSettingsInput): Promise<SaveResult>;
  saveHubUrl(url: string): Promise<SaveResult>;
  /** Stores or replaces the mail password; it is never read back. */
  setMailPassword(password: string): Promise<SecretResult>;
  removeMailPassword(): Promise<SecretResult>;
  /** Connects and logs in without sending a message. */
  checkMail(): Promise<CheckResult>;
}

/** Builds the API over a call function (the Wails runtime in the app, a fake in tests). */
export function connectionsApi(call: CallByName): ConnectionsApi {
  return {
    get: async () => parseConnections(await call(methods.get)),
    saveMailSettings: async (input) => parseSaveResult(await call(methods.saveMailSettings, input)),
    saveHubUrl: async (url) => parseSaveResult(await call(methods.saveHubUrl, url)),
    setMailPassword: async (password) => parseSecretResult(await call(methods.setMailPassword, password)),
    removeMailPassword: async () => parseSecretResult(await call(methods.removeMailPassword)),
    checkMail: async () => parseCheckResult(await call(methods.checkMail)),
  };
}
