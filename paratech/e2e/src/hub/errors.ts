// Typed errors of the harness, in two separate hierarchies:
//   - E2EEnvironmentError: the machine is not ready for an E2E run (no Docker, a busy port, no live key, no Go). A
//     runner reports it as «не проверено».
//   - HubFailedError: the Hub itself failed (it does not build, `keys create` fails, it does not become ready or exits
//     at startup). This can be a real Hub regression, so a runner reports it as «сломано», never as «не проверено».
// Messages are Russian, for the human, and never carry a secret, a database URL or captured output that was not scrubbed.

export type EnvironmentErrorKind =
  | "DockerMissing"
  | "DockerNotRunning"
  | "PortBusy"
  | "ForbiddenPort"
  | "MissingLiveKey"
  | "BadConfig"
  | "DatabaseUnavailable"
  | "GoToolchainMissing";

export class E2EEnvironmentError extends Error {
  readonly kind: EnvironmentErrorKind;

  constructor(kind: EnvironmentErrorKind, message: string) {
    super(message);
    this.name = new.target.name;
    this.kind = kind;
  }
}

export class DockerMissingError extends E2EEnvironmentError {
  constructor() {
    super("DockerMissing", "Docker не установлен (команда docker не найдена)");
  }
}

export class DockerNotRunningError extends E2EEnvironmentError {
  constructor() {
    super("DockerNotRunning", "Docker не запущен: запустите Docker Desktop или задайте HUB_TEST_DATABASE_URL");
  }
}

export class PortBusyError extends E2EEnvironmentError {
  readonly port: number;

  constructor(port: number) {
    super(
      "PortBusy",
      `Порт ${port} занят другим процессом или зарезервирован Windows; задайте другой порт в PARATECH_E2E_PG_PORT`,
    );
    this.port = port;
  }
}

export class ForbiddenPortError extends E2EEnvironmentError {
  readonly port: number;

  constructor(port: number) {
    super("ForbiddenPort", `Порт ${port} запрещён для тестового Hub (8080 и 58080 принадлежат рабочему Hub)`);
    this.port = port;
  }
}

export class MissingLiveKeyError extends E2EEnvironmentError {
  constructor() {
    super("MissingLiveKey", "Для режима LIVE нужен ключ провайдера в переменной PARATECH_E2E_ANTHROPIC_KEY");
  }
}

export class BadConfigError extends E2EEnvironmentError {
  constructor(message: string) {
    super("BadConfig", message);
  }
}

export class DatabaseUnavailableError extends E2EEnvironmentError {
  constructor(message: string) {
    super("DatabaseUnavailable", message);
  }
}

/** The go command cannot be started at all (not in PATH): nothing about the Hub is known yet. */
export class GoToolchainMissingError extends E2EEnvironmentError {
  constructor(message: string) {
    super("GoToolchainMissing", message);
  }
}

export type HubFailedKind = "HubBuildFailed" | "HubKeyFailed" | "HubNotReady";

export class HubFailedError extends Error {
  readonly kind: HubFailedKind;

  constructor(kind: HubFailedKind, message: string) {
    super(message);
    this.name = new.target.name;
    this.kind = kind;
  }
}

/** go build ran and finished with a nonzero exit (or was killed by the build timeout). */
export class HubBuildFailedError extends HubFailedError {
  constructor(message: string) {
    super("HubBuildFailed", message);
  }
}

export class HubKeyFailedError extends HubFailedError {
  constructor(message: string) {
    super("HubKeyFailed", message);
  }
}

/** The Hub did not answer /readyz in time, exited at startup or its binary could not be started. */
export class HubNotReadyError extends HubFailedError {
  constructor(message: string) {
    super("HubNotReady", message);
  }
}

/** True for «не проверено» errors (the environment), false for Hub failures and anything else. */
export function isEnvironmentError(err: unknown): err is E2EEnvironmentError {
  return err instanceof E2EEnvironmentError;
}

/** True for errors the Hub itself caused: a runner reports them as «сломано». */
export function isHubFailedError(err: unknown): err is HubFailedError {
  return err instanceof HubFailedError;
}
