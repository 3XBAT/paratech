// Google sign-in bindings of "Подключения": calls into the Go core (internal/input/wails/google.go). The page never
// talks to Google itself and no reply carries a secret or a token: the client and the account are only statuses.

import type { CallByName } from "./settings";

/** Where the client id and secret stand; "unsupported" means there is no secret store in this system. */
export type GoogleClientStatus = "set" | "unset" | "unsupported" | "unavailable";

/** Whether a refresh token is stored. */
export type GoogleAccountStatus = "signedIn" | "signedOut" | "unsupported" | "unavailable";

/** The section as the Go binding returns it. */
export interface GoogleState {
  client: GoogleClientStatus;
  account: GoogleAccountStatus;
  /** A sign-in waits for the browser (possibly started before the page was opened). */
  signingIn: boolean;
}

/** The answer to an action: a fixed text from Go and the state afterwards. */
export interface GoogleResult {
  ok: boolean;
  message: string;
  state: GoogleState;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  status: "paratech/desktop/internal/input/wails.GoogleService.GoogleStatus",
  setClient: "paratech/desktop/internal/input/wails.GoogleService.SetGoogleClient",
  clearClient: "paratech/desktop/internal/input/wails.GoogleService.ClearGoogleClient",
  signIn: "paratech/desktop/internal/input/wails.GoogleService.GoogleSignIn",
  cancelSignIn: "paratech/desktop/internal/input/wails.GoogleService.GoogleCancelSignIn",
  signOut: "paratech/desktop/internal/input/wails.GoogleService.GoogleSignOut",
} as const;

export const clientStatuses: readonly GoogleClientStatus[] = ["set", "unset", "unsupported", "unavailable"];

export const accountStatuses: readonly GoogleAccountStatus[] = ["signedIn", "signedOut", "unsupported", "unavailable"];

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

/** Checks the shape of the section; a malformed reply is an error, not a default. */
export function parseGoogleState(raw: unknown): GoogleState {
  const w = "google state";
  const r = record(raw, w);
  if (!clientStatuses.includes(r.client as GoogleClientStatus)) {
    throw new Error(`${w}: client is not a client status`);
  }
  if (!accountStatuses.includes(r.account as GoogleAccountStatus)) {
    throw new Error(`${w}: account is not an account status`);
  }
  return {
    client: r.client as GoogleClientStatus,
    account: r.account as GoogleAccountStatus,
    signingIn: bool(r, "signingIn", w),
  };
}

/** Checks the shape of an action answer. */
export function parseGoogleResult(raw: unknown): GoogleResult {
  const w = "google result";
  const r = record(raw, w);
  return { ok: bool(r, "ok", w), message: str(r, "message", w), state: parseGoogleState(r.state) };
}

export interface GoogleApi {
  status(): Promise<GoogleState>;
  /** Stores the client id and secret; neither is ever read back. */
  setClient(clientId: string, clientSecret: string): Promise<GoogleResult>;
  clearClient(): Promise<GoogleResult>;
  /** Opens Google's page in the browser and answers when the sign-in ends (up to 5 minutes). */
  signIn(): Promise<GoogleResult>;
  /** Stops a waiting sign-in; its signIn call then answers "cancelled". */
  cancelSignIn(): Promise<GoogleResult>;
  /** Revokes the access at Google and removes the stored token. */
  signOut(): Promise<GoogleResult>;
}

/** Builds the API over a call function (the Wails runtime in the app, a fake in tests). */
export function googleApi(call: CallByName): GoogleApi {
  return {
    status: async () => parseGoogleState(await call(methods.status)),
    setClient: async (clientId, clientSecret) =>
      parseGoogleResult(await call(methods.setClient, clientId, clientSecret)),
    clearClient: async () => parseGoogleResult(await call(methods.clearClient)),
    signIn: async () => parseGoogleResult(await call(methods.signIn)),
    cancelSignIn: async () => parseGoogleResult(await call(methods.cancelSignIn)),
    signOut: async () => parseGoogleResult(await call(methods.signOut)),
  };
}
