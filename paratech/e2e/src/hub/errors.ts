// Environment errors of the harness: the machine is not ready for an E2E run (no Docker, a busy port, no live key).
// They are typed so that a runner reports «не проверено», never «сломано». Messages are Russian, for the human, and
// never carry a secret, a database URL or captured output that was not scrubbed.

export type EnvironmentErrorKind =
  | "DockerMissing"
  | "DockerNotRunning"
  | "PortBusy"
  | "ForbiddenPort"
  | "MissingLiveKey"
  | "BadConfig"
  | "DatabaseUnavailable"
  | "HubBuildFailed"
  | "HubKeyFailed"
  | "HubNotReady";

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

export class HubBuildFailedError extends E2EEnvironmentError {
  constructor(message: string) {
    super("HubBuildFailed", message);
  }
}

export class HubKeyFailedError extends E2EEnvironmentError {
  constructor(message: string) {
    super("HubKeyFailed", message);
  }
}

export class HubNotReadyError extends E2EEnvironmentError {
  constructor(message: string) {
    super("HubNotReady", message);
  }
}

export function isEnvironmentError(err: unknown): err is E2EEnvironmentError {
  return err instanceof E2EEnvironmentError;
}
