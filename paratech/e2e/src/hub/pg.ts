// A minimal Postgres client for the two statements the harness needs (CREATE SCHEMA, DROP SCHEMA): the simple query
// protocol over plain TCP with SCRAM-SHA-256, MD5 or cleartext login. It adds no dependency and works the same for the
// compose database and HUB_TEST_DATABASE_URL. No TLS: the database is local (sslmode=disable).
import { createHash, createHmac, pbkdf2Sync, randomBytes, timingSafeEqual } from "node:crypto";
import net from "node:net";

import { BadConfigError, DatabaseUnavailableError } from "./errors.js";

export interface PgTarget {
  host: string;
  port: number;
  user: string;
  password: string;
  database: string;
}

/** A statement the server refused; the message is the server's (it never contains the password). */
export class PgError extends Error {
  readonly sqlState: string;

  constructor(message: string, sqlState: string) {
    super(`Postgres: ${message} (${sqlState})`);
    this.name = "PgError";
    this.sqlState = sqlState;
  }
}

const protocolVersion = 196608; // 3.0
const maxIterations = 1_000_000;
const plainSSLModes = new Set(["", "disable", "allow", "prefer"]);

/** Parses a postgres:// URL. Messages name the variable, never the URL: it holds the password. */
export function parsePgUrl(raw: string, name: string): PgTarget {
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    throw new BadConfigError(`${name}: не является URL`);
  }
  if (u.protocol !== "postgres:" && u.protocol !== "postgresql:") {
    throw new BadConfigError(`${name}: схема должна быть postgres://`);
  }
  const database = decodeURIComponent(u.pathname.replace(/^\//, ""));
  if (!u.hostname || !database || !u.username) {
    throw new BadConfigError(`${name}: нужны пользователь, хост и имя базы`);
  }
  if (!plainSSLModes.has(u.searchParams.get("sslmode") ?? "")) {
    throw new BadConfigError(`${name}: харнесс подключается без TLS, нужен sslmode=disable`);
  }
  return {
    host: u.hostname.replace(/^\[(.*)\]$/, "$1"),
    port: u.port ? Number(u.port) : 5432,
    user: decodeURIComponent(u.username),
    password: decodeURIComponent(u.password),
    database,
  };
}

/** The URL with search_path set to schema: pgx passes it to the server, so every Hub connection uses the schema. */
export function withSearchPath(raw: string, schema: string): string {
  const u = new URL(raw);
  u.searchParams.set("search_path", schema);
  return u.toString();
}

/** Runs one SQL statement and resolves when the server has finished it. */
export function pgExec(t: PgTarget, sql: string, timeoutMs = 15_000): Promise<void> {
  return new Promise((resolve, reject) => {
    const where = `${t.host}:${t.port}`;
    const sock = net.connect({ host: t.host, port: t.port });
    let buf = Buffer.alloc(0);
    let scram: { clientFirstBare: string; serverSignature?: string } | undefined;
    let queried = false;
    let queryError: Error | undefined;
    let done = false;

    const finish = (err?: Error): void => {
      if (done) {
        return;
      }
      done = true;
      clearTimeout(timer);
      if (err) {
        sock.destroy();
        reject(err);
        return;
      }
      sock.end(message("X", Buffer.alloc(0)));
      resolve();
    };
    const timer = setTimeout(() => finish(new DatabaseUnavailableError(`Postgres на ${where} не ответил вовремя`)), timeoutMs);

    const auth = (body: Buffer): void => {
      const code = body.readInt32BE(0);
      switch (code) {
        case 0:
          return;
        case 3:
          sock.write(message("p", cstring(t.password)));
          return;
        case 5: {
          const inner = md5hex(Buffer.from(t.password + t.user));
          const outer = "md5" + md5hex(Buffer.concat([Buffer.from(inner), body.subarray(4, 8)]));
          sock.write(message("p", cstring(outer)));
          return;
        }
        case 10: {
          const mechanisms = body.subarray(4).toString("utf8").split("\0");
          if (!mechanisms.includes("SCRAM-SHA-256")) {
            throw new DatabaseUnavailableError(`Postgres на ${where} просит неподдерживаемый способ входа`);
          }
          scram = { clientFirstBare: "n=,r=" + randomBytes(18).toString("base64") };
          const first = Buffer.from("n,," + scram.clientFirstBare);
          const len = Buffer.alloc(4);
          len.writeInt32BE(first.length);
          sock.write(message("p", Buffer.concat([cstring("SCRAM-SHA-256"), len, first])));
          return;
        }
        case 11: {
          if (!scram) {
            throw new DatabaseUnavailableError("Postgres: SCRAM без начала обмена");
          }
          const step = scramClientFinal(t.password, scram.clientFirstBare, body.subarray(4).toString("utf8"));
          scram.serverSignature = step.serverSignature;
          sock.write(message("p", Buffer.from(step.clientFinal)));
          return;
        }
        case 12: {
          const got = Buffer.from(body.subarray(4).toString("utf8").replace(/^v=/, "").split(",")[0] ?? "", "base64");
          const want = Buffer.from(scram?.serverSignature ?? "", "base64");
          if (want.length === 0 || got.length !== want.length || !timingSafeEqual(got, want)) {
            throw new DatabaseUnavailableError(`Postgres на ${where}: подпись сервера SCRAM не совпала`);
          }
          return;
        }
        default:
          throw new DatabaseUnavailableError(`Postgres на ${where} просит неподдерживаемый способ входа (${code})`);
      }
    };

    const handle = (type: string, body: Buffer): void => {
      switch (type) {
        case "R":
          auth(body);
          return;
        case "E": {
          const err = errorResponse(body);
          if (!queried) {
            throw err;
          }
          queryError = err; // the server still sends ReadyForQuery
          return;
        }
        case "Z":
          if (!queried) {
            queried = true;
            sock.write(message("Q", cstring(sql)));
            return;
          }
          finish(queryError);
          return;
        default: // ParameterStatus, BackendKeyData, NoticeResponse, CommandComplete, rows: not needed
          return;
      }
    };

    sock.once("connect", () => sock.write(startup(t.user, t.database)));
    sock.on("error", () => finish(new DatabaseUnavailableError(`Postgres недоступен на ${where}`)));
    sock.on("close", () => finish(new DatabaseUnavailableError(`Postgres на ${where} закрыл соединение`)));
    sock.on("data", (chunk: Buffer) => {
      buf = Buffer.concat([buf, chunk]);
      while (!done && buf.length >= 5) {
        const len = buf.readInt32BE(1);
        if (buf.length < 1 + len) {
          break;
        }
        const type = String.fromCharCode(buf[0] ?? 0);
        const body = buf.subarray(5, 1 + len);
        buf = buf.subarray(1 + len);
        try {
          handle(type, body);
        } catch (err) {
          finish(err instanceof Error ? err : new Error(String(err)));
        }
      }
    });
  });
}

/** The client-final message of SCRAM-SHA-256 (RFC 5802/7677) and the server signature to expect. */
export function scramClientFinal(
  password: string,
  clientFirstBare: string,
  serverFirst: string,
): { clientFinal: string; serverSignature: string } {
  const attrs = new Map<string, string>();
  for (const part of serverFirst.split(",")) {
    const eq = part.indexOf("=");
    if (eq > 0) {
      attrs.set(part.slice(0, eq), part.slice(eq + 1));
    }
  }
  const clientNonce = clientFirstBare.split(",").find((p) => p.startsWith("r="))?.slice(2) ?? "";
  const nonce = attrs.get("r") ?? "";
  const salt = Buffer.from(attrs.get("s") ?? "", "base64");
  const iterations = Number(attrs.get("i"));
  if (!clientNonce || !nonce.startsWith(clientNonce) || nonce.length <= clientNonce.length || salt.length === 0) {
    throw new DatabaseUnavailableError("Postgres: неверный ответ SCRAM");
  }
  if (!Number.isInteger(iterations) || iterations < 1 || iterations > maxIterations) {
    throw new DatabaseUnavailableError("Postgres: неверное число итераций SCRAM");
  }

  const salted = pbkdf2Sync(password, salt, iterations, 32, "sha256");
  const clientKey = hmac(salted, "Client Key");
  const storedKey = createHash("sha256").update(clientKey).digest();
  const withoutProof = `c=biws,r=${nonce}`;
  const authMessage = `${clientFirstBare},${serverFirst},${withoutProof}`;
  const signature = hmac(storedKey, authMessage);
  const proof = Buffer.alloc(clientKey.length);
  for (let i = 0; i < proof.length; i++) {
    proof[i] = (clientKey[i] ?? 0) ^ (signature[i] ?? 0);
  }
  const serverSignature = hmac(hmac(salted, "Server Key"), authMessage).toString("base64");
  return { clientFinal: `${withoutProof},p=${proof.toString("base64")}`, serverSignature };
}

function hmac(key: Buffer, data: string): Buffer {
  return createHmac("sha256", key).update(data).digest();
}

function md5hex(data: Buffer): string {
  return createHash("md5").update(data).digest("hex");
}

function cstring(s: string): Buffer {
  return Buffer.from(s + "\0", "utf8");
}

function message(type: string, payload: Buffer): Buffer {
  const head = Buffer.alloc(5);
  head.write(type, 0, "ascii");
  head.writeInt32BE(payload.length + 4, 1);
  return Buffer.concat([head, payload]);
}

function startup(user: string, database: string): Buffer {
  const params = Buffer.concat([
    cstring("user"),
    cstring(user),
    cstring("database"),
    cstring(database),
    cstring("client_encoding"),
    cstring("UTF8"),
    Buffer.from([0]),
  ]);
  const head = Buffer.alloc(8);
  head.writeInt32BE(params.length + 8, 0);
  head.writeInt32BE(protocolVersion, 4);
  return Buffer.concat([head, params]);
}

function errorResponse(body: Buffer): PgError {
  const fields = new Map<string, string>();
  let i = 0;
  while (i < body.length && body[i] !== 0) {
    const code = String.fromCharCode(body[i] ?? 0);
    const end = body.indexOf(0, i + 1);
    if (end < 0) {
      break;
    }
    fields.set(code, body.subarray(i + 1, end).toString("utf8"));
    i = end + 1;
  }
  return new PgError(fields.get("M") ?? "error", fields.get("C") ?? "?????");
}
