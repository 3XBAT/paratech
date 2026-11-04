import { E2EEnvironmentError, type EnvironmentErrorKind } from "../hub/index.js";

/** The machine cannot host the run (locked sandbox folder, no exe to reuse, no system tool): «не проверено». */
export class SandboxEnvironmentError extends E2EEnvironmentError {
  constructor(kind: Extract<EnvironmentErrorKind, "SandboxUnavailable" | "ExeMissing" | "ToolMissing" | "BadConfig">, message: string) {
    super(kind, message);
  }
}

export type StartFailureKind =
  | "BuildFailed" // build-alpha.ps1 -E2E exited nonzero
  | "SandboxNotIsolated" // the app did not log the four start lines or logged secrets_profile=default
  | "CdpNotReady" // the debug port did not answer
  | "CdpNotLoopback"; // the debug port listens on more than loopback

/** The product broke at start: a run reports it as «сломано», never «не проверено». */
export class StartFailureError extends Error {
  readonly kind: StartFailureKind;

  constructor(kind: StartFailureKind, message: string) {
    super(message);
    this.name = "StartFailureError";
    this.kind = kind;
  }
}

export function isStartFailure(err: unknown): err is StartFailureError {
  return err instanceof StartFailureError;
}
