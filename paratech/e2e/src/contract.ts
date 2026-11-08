// Shared contract of the runner parts: sandbox setup (src/sandbox, src/app), helpers (src/steps) and the report
// (src/report). Names only, no secrets, no logic. ADR-0035 §2, §3, §6.

/** Set by the global setup (main process) for the workers; none of them is a secret except EnvHubKey and EnvHubDatabaseUrl. */
export const EnvRunRoot = "PARATECH_E2E_RUN_ROOT"; // <repo>/paratech/dist/e2e-run
export const EnvCtlDir = "PARATECH_E2E_CTL_DIR"; // <run root>/ctl = the --e2e-dir of the app
export const EnvDataDir = "PARATECH_E2E_DATA_DIR"; // <ctl>/data = the --data-dir of the app
export const EnvCdpUrl = "PARATECH_E2E_CDP_URL"; // http://127.0.0.1:<port>
export const EnvFakeUrl = "PARATECH_E2E_FAKE_URL"; // fake Messages API (control paths /__e2e/*)
export const EnvHubUrl = "PARATECH_E2E_HUB_URL";
export const EnvHubKey = "PARATECH_E2E_HUB_KEY"; // the throwaway key of the run: in memory only, scrubbed from reports
/**
 * The database URL of the throwaway Hub (user and password of the compose database, the run's schema in search_path):
 * in memory only like EnvHubKey, never in state.json, scrubbed from reports. The workers use it to run `hub keys
 * create/revoke` (src/hub/keys.ts), e.g. to revoke a second key without touching the key of the run.
 */
export const EnvHubDatabaseUrl = "PARATECH_E2E_HUB_DATABASE_URL";
export const EnvMode = "PARATECH_E2E_MODE"; // "scripted" | "live"

/** Files below the run root. */
export const StateFileName = "state.json";
export const ReportDirName = "report";
export const ReportFileName = "report.md";

/** <run root>/state.json: written by the global setup, read by the reporter. Never holds a secret. */
export interface RunState {
  mode: "scripted" | "live";
  startedAt: string; // ISO
  runRoot: string;
  ctlDir: string;
  dataDir: string;
  fixturesDir: string; // <ctl>/fixtures
  exePath: string;
  cdpUrl: string;
  hubUrl: string;
  fakeUrl: string;
  /** From the debug log of the app; the teardown deletes only the Credential Manager entries of this profile. */
  secretsProfileId?: string;
  /**
   * Pid of the app that runs now. The setup writes it; a spec that restarts the app (src/app/restart.ts) replaces it, and
   * the teardown and the exit guard stop that process too when it is not the one they started.
   */
  appPid?: number;
  /** A spec started the app again: only then do the teardown and the exit guard look for a process that is not theirs. */
  appRestarted?: boolean;
  /** The machine could not start the run (no Docker, port busy, sandbox not isolated...): features are «не проверено». */
  environmentError?: { kind: string; message: string };
  /** The product broke at start (Hub build or readiness, the app did not come up): reported as «сломано». */
  startFailure?: { kind: string; message: string };
}

/** An error whose message starts with this marker is an environment problem: «не проверено», not «сломано». */
export const EnvironmentMarker = "[среда]";

/** Annotation types of a test, read by the reporter. */
export const AnnotationFeature = "feature"; // «Запуск приложения»: the row of the report
export const AnnotationMode = "mode"; // "SCRIPTED" | "LIVE"; default SCRIPTED
export const AnnotationRequires = "requires"; // feature that must be «готово» first; else «не проверено (нужно: …)»
export const AnnotationSttSpeed = "stt-speed"; // one line about the speed of the speech recognition, printed in the report

/** Attachment name of a screenshot the report copies and links. */
export const SnapshotAttachment = "снимок";

/** Dialog names of the app (desktop/internal/adapters/e2ehooks). */
export type Dialog = "schedule" | "model" | "material" | "notes" | "recording";
export type OpenedKind = "link" | "file" | "call" | "clipboard";
export interface OpenedRecord {
  kind: OpenedKind;
  value: string;
  at: string;
}
