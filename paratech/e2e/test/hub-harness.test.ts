import assert from "node:assert/strict";
import net from "node:net";
import path from "node:path";
import { PassThrough } from "node:stream";
import { describe, test } from "node:test";
import { fileURLToPath } from "node:url";
import { inspect } from "node:util";

import { buildHub } from "../src/hub/build.js";
import { captureStream, DummyProviderKey, hubEnv, LogCapture, providerVars, Redactor, toolEnv } from "../src/hub/env.js";
import {
  BadConfigError,
  E2EEnvironmentError,
  ForbiddenPortError,
  GoToolchainMissingError,
  guardedHubCount,
  HubBuildFailedError,
  HubFailedError,
  HubKeyFailedError,
  HubNotReadyError,
  isEnvironmentError,
  isHubFailedError,
  MissingLiveKeyError,
  startHub,
  type HubDeps,
  type StartedHub,
} from "../src/hub/index.js";
import { parseDataRow, parsePgUrl, PgError, pgQuery, pgQueryUrl, scramClientFinal, searchPathOf, searchPathStatement, withSearchPath } from "../src/hub/pg.js";
import { assertAllowedHubPort, freeLoopbackPort, loopbackPortBusy } from "../src/hub/ports.js";
import { composePort } from "../src/hub/postgres.js";

// An obviously fake provider key: the tests grep every output of the harness for it.
const Sentinel = "sk-ant-e2e-SENTINEL-0123456789abcdef";
const StubApiKey = "pthub_stub0001_s3cr3tvalue0001";
const stubHub = path.join(path.dirname(fileURLToPath(import.meta.url)), "fixtures", "stub-hub.js");

describe("environment of spawned processes", () => {
  test("the Hub gets only the minimal inherited variables plus its own", () => {
    const env = hubEnv(
      {
        Path: "C:\\bin",
        SystemRoot: "C:\\Windows",
        TEMP: "C:\\tmp",
        HUB_RATE_LIMIT_BURST: "1",
        HUB_ANTHROPIC_API_KEY: "owner-key",
        PARATECH_E2E_ANTHROPIC_KEY: Sentinel,
        OWNER_TOKEN: "x",
        GOFLAGS: "-mod=mod",
      },
      { HUB_LISTEN_ADDR: "127.0.0.1:1234" },
    );
    assert.deepEqual(Object.keys(env).sort(), ["HUB_LISTEN_ADDR", "Path", "SystemRoot", "TEMP"]);
  });

  test("tools inherit everything but HUB_* and PARATECH_*", () => {
    const env = toolEnv(
      { PATH: "/bin", GOFLAGS: "-mod=mod", hub_pg_port: "1", PARATECH_E2E_ANTHROPIC_KEY: Sentinel, HUB_X: "y" },
      { HUB_PG_PORT: "25432" },
    );
    assert.deepEqual(env, { PATH: "/bin", GOFLAGS: "-mod=mod", HUB_PG_PORT: "25432" });
  });
});

describe("provider settings", () => {
  test("scripted mode: dummy key, the fake URL, streamed provider calls (TASK-380)", () => {
    const p = providerVars("scripted", { PARATECH_E2E_ANTHROPIC_KEY: Sentinel }, "http://127.0.0.1:4100");
    assert.deepEqual(p.vars, {
      HUB_ANTHROPIC_API_KEY: DummyProviderKey,
      HUB_ANTHROPIC_BASE_URL: "http://127.0.0.1:4100",
      HUB_PROVIDER_STREAM: "true",
    });
    assert.deepEqual(p.secrets, []);
  });

  test("scripted mode needs a loopback http fake", () => {
    assert.throws(() => providerVars("scripted", {}, undefined), BadConfigError);
    assert.throws(() => providerVars("scripted", {}, "http://10.0.0.1:4100"), BadConfigError);
    assert.throws(() => providerVars("scripted", {}, "https://127.0.0.1:4100"), BadConfigError);
  });

  test("live mode without a key is a typed environment error", () => {
    assert.throws(
      () => providerVars("live", {}, undefined),
      (err: unknown) => err instanceof MissingLiveKeyError && err.kind === "MissingLiveKey" && /PARATECH_E2E_ANTHROPIC_KEY/.test(err.message),
    );
  });

  test("live mode passes key, base URL and auth and marks the secrets", () => {
    const p = providerVars(
      "live",
      { PARATECH_E2E_ANTHROPIC_KEY: Sentinel, PARATECH_E2E_ANTHROPIC_BASE_URL: "https://gw.example.invalid/anthropic", PARATECH_E2E_ANTHROPIC_AUTH: "bearer" },
      undefined,
    );
    assert.deepEqual(p.vars, {
      HUB_ANTHROPIC_API_KEY: Sentinel,
      HUB_ANTHROPIC_BASE_URL: "https://gw.example.invalid/anthropic",
      HUB_ANTHROPIC_AUTH: "bearer",
    });
    assert.ok(p.secrets.includes(Sentinel));
  });
});

describe("redaction", () => {
  test("a secret split across chunks is scrubbed", () => {
    const r = new Redactor();
    r.add(Sentinel);
    const logs = new LogCapture(r);
    logs.write(`a ${Sentinel.slice(0, 7)}`);
    logs.write(`${Sentinel.slice(7)} b\npending ${Sentinel}`);
    assert.equal(logs.text(), "a (redacted) b\npending (redacted)");
  });

  test("a stream split inside a Cyrillic char and inside a secret is decoded and scrubbed", async () => {
    const r = new Redactor();
    r.add(Sentinel);
    const logs = new LogCapture(r);
    const stream = new PassThrough();
    captureStream(stream, logs);
    const bytes = Buffer.from(`Конспект ${Sentinel} пары\nещё`, "utf8");
    const cuts = [1, 3, Buffer.byteLength(`Конспект ${Sentinel.slice(0, 9)}`), bytes.length - 1]; // 1, 3: inside "К", "о"
    let from = 0;
    for (const to of [...cuts, bytes.length]) {
      stream.write(bytes.subarray(from, to));
      from = to;
      await new Promise((resolve) => setImmediate(resolve)); // each piece is its own 'data' chunk
    }
    assert.equal(logs.text(), "Конспект (redacted) пары\nещё");
  });

  test("the size limit drops whole lines and a runaway line keeps no part of a secret", () => {
    const r = new Redactor();
    r.add(Sentinel);
    const logs = new LogCapture(r, 200, 100);
    for (let i = 0; i < 50; i++) {
      logs.write(`line ${i} ${Sentinel}\n`);
    }
    logs.write("x".repeat(390) + Sentinel + "y".repeat(50));
    const text = logs.text();
    assert.ok(text.length < 400);
    assert.ok(!text.includes(Sentinel.slice(0, 10)) && !text.includes(Sentinel.slice(-10)));
    assert.match(text, /line 49 \(redacted\)/);
  });
});

describe("ports", () => {
  test("8080 and 58080 are refused", () => {
    for (const port of [8080, 58080]) {
      assert.throws(
        () => assertAllowedHubPort(port),
        (err: unknown) => err instanceof ForbiddenPortError && err.kind === "ForbiddenPort" && err.port === port,
      );
    }
    assertAllowedHubPort(43123);
  });

  test("a free port is on loopback, allowed, and seen as busy while held", async () => {
    const port = await freeLoopbackPort();
    assert.ok(port !== 8080 && port !== 58080);
    assert.equal(await loopbackPortBusy(port), false);

    const holder = net.createServer();
    await new Promise<void>((resolve) => holder.listen({ host: "127.0.0.1", port }, resolve));
    try {
      assert.equal(await loopbackPortBusy(port), true);
    } finally {
      await new Promise((resolve) => holder.close(resolve));
    }
  });

  test("the compose port: PARATECH_E2E_PG_PORT, else a free loopback port; HUB_PG_PORT is never read", async () => {
    const never = () => Promise.reject(new Error("must not pick a port when the variable is set"));
    assert.equal(await composePort({ PARATECH_E2E_PG_PORT: "25432", HUB_PG_PORT: "15432" }, never), 25432);

    const picked = () => Promise.resolve(40123);
    assert.equal(await composePort({ HUB_PG_PORT: "35432" }, picked), 40123);
    assert.equal(await composePort({}, picked), 40123);

    // no fixed default: the real picker gives a port nobody listens on
    const free = await composePort({});
    assert.ok(Number.isInteger(free) && free > 0 && free < 65536);
    assert.equal(await loopbackPortBusy(free), false);

    await assert.rejects(composePort({ PARATECH_E2E_PG_PORT: "abc" }, never), BadConfigError);
    await assert.rejects(composePort({ PARATECH_E2E_PG_PORT: "70000" }, never), BadConfigError);
  });
});

describe("typed errors", () => {
  test("every environment error has a kind and a Russian message", () => {
    const err = new MissingLiveKeyError();
    assert.ok(err instanceof E2EEnvironmentError && isEnvironmentError(err));
    assert.equal(err.name, "MissingLiveKeyError");
    assert.match(err.message, /[а-я]/);
    assert.equal(isEnvironmentError(new Error("x")), false);
  });

  test("Hub failures are a separate hierarchy: never environment errors", () => {
    const failures = [
      new HubBuildFailedError("Сборка hub.exe не удалась"),
      new HubKeyFailedError("hub keys create не выдал ключ"),
      new HubNotReadyError("Hub не ответил на /readyz"),
    ];
    assert.deepEqual(failures.map((e) => e.kind), ["HubBuildFailed", "HubKeyFailed", "HubNotReady"]);
    for (const err of failures) {
      assert.ok(err instanceof HubFailedError && isHubFailedError(err));
      assert.ok(!(err instanceof E2EEnvironmentError) && !isEnvironmentError(err), err.name);
      assert.match(err.message, /[а-я]/);
    }
    const goMissing = new GoToolchainMissingError("Go не найден в PATH");
    assert.ok(isEnvironmentError(goMissing) && !isHubFailedError(goMissing));
    assert.equal(goMissing.kind, "GoToolchainMissing");
  });

  test("go missing from PATH is an environment error, not a failed build", async () => {
    // Under build/ (compiled tests live there): nothing outside the package is created.
    const dir = path.join(path.dirname(fileURLToPath(import.meta.url)), "nogo");
    const paths = { e2eDir: dir, paratechDir: dir, composeFile: path.join(dir, "none.yml"), distDir: dir };
    await assert.rejects(
      buildHub(paths, { PATH: dir }),
      (err: unknown) => err instanceof GoToolchainMissingError && isEnvironmentError(err) && /Go не найден/.test(err.message),
    );
  });
});

describe("postgres client", () => {
  test("SCRAM-SHA-256 matches the RFC 7677 example", () => {
    const step = scramClientFinal(
      "pencil",
      "n=user,r=rOprNGfwEbeRWgbNEkqO",
      "r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096",
    );
    assert.equal(
      step.clientFinal,
      "c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,p=dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ=",
    );
    assert.equal(step.serverSignature, "6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4=");
  });

  test("a server nonce that does not extend the client one is refused", () => {
    assert.throws(() => scramClientFinal("p", "n=,r=abc", "r=xyz123,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"), E2EEnvironmentError);
  });

  test("URL parsing never echoes the password", () => {
    const t = parsePgUrl("postgres://hub:p%40ss@127.0.0.1:25432/hub?sslmode=disable", "database url");
    assert.deepEqual(t, { host: "127.0.0.1", port: 25432, user: "hub", password: "p@ss", database: "hub" });
    assert.throws(
      () => parsePgUrl("postgres://hub:secret-pw@127.0.0.1/hub?sslmode=require", "database url"),
      (err: unknown) => err instanceof BadConfigError && !err.message.includes("secret-pw"),
    );
    assert.match(withSearchPath("postgres://hub:x@127.0.0.1:1/hub?sslmode=disable", "e2e_ab"), /sslmode=disable&search_path=e2e_ab$/);
  });

  // The body of a DataRow ('D'): Int16 column count, then Int32 length (-1 = NULL) and the bytes of each column.
  const dataRowBody = (cells: Array<string | null>): Buffer => {
    const parts: Buffer[] = [Buffer.alloc(2)];
    parts[0]?.writeInt16BE(cells.length);
    for (const c of cells) {
      const bytes = c === null ? Buffer.alloc(0) : Buffer.from(c, "utf8");
      const len = Buffer.alloc(4);
      len.writeInt32BE(c === null ? -1 : bytes.length);
      parts.push(len, bytes);
    }
    return Buffer.concat(parts);
  };

  test("a DataRow is parsed into text cells: UTF-8, empty string, NULL as \"\"", () => {
    assert.deepEqual(parseDataRow(dataRowBody(["ag:t1:1:1", "2", "Привет", "", null])), ["ag:t1:1:1", "2", "Привет", "", ""]);
    assert.deepEqual(parseDataRow(dataRowBody([])), []);
    assert.throws(() => parseDataRow(dataRowBody(["abcdef"]).subarray(0, 8)), E2EEnvironmentError);
  });

  test("the search_path of a database URL is read and quoted as an identifier", () => {
    assert.equal(searchPathOf("postgres://hub:x@127.0.0.1:1/hub?sslmode=disable&search_path=e2e_ab12"), "e2e_ab12");
    assert.equal(searchPathOf("postgres://hub:x@127.0.0.1:1/hub?sslmode=disable"), undefined);
    assert.equal(searchPathOf("not a url"), undefined);
    assert.equal(searchPathStatement("e2e_ab12"), 'SET search_path TO "e2e_ab12"');
    assert.equal(searchPathStatement('a"; DROP SCHEMA x; --'), 'SET search_path TO "a""; DROP SCHEMA x; --"');
  });

  test("pgQuery logs in, sets the search_path, and returns the rows of the SELECT as text", async () => {
    const frame = (type: string, payload: Buffer): Buffer => {
      const head = Buffer.alloc(5);
      head.write(type, 0, "ascii");
      head.writeInt32BE(payload.length + 4, 1);
      return Buffer.concat([head, payload]);
    };
    const authOk = Buffer.alloc(4); // AuthenticationOk
    const received: string[] = [];
    const server = net.createServer((sock) => {
      let buf = Buffer.alloc(0);
      let started = false;
      sock.on("error", () => {});
      sock.on("data", (chunk: Buffer) => {
        buf = Buffer.concat([buf, chunk]);
        while (buf.length >= (started ? 5 : 4)) {
          const len = started ? buf.readInt32BE(1) + 1 : buf.readInt32BE(0);
          if (buf.length < len) {
            return;
          }
          const type = started ? String.fromCharCode(buf[0] ?? 0) : "";
          const body = buf.subarray(started ? 5 : 4, len);
          buf = buf.subarray(len);
          if (!started) {
            started = true;
            sock.write(Buffer.concat([frame("R", authOk), frame("Z", Buffer.from("I"))]));
          } else if (type === "Q") {
            received.push(body.toString("utf8").replace(/\0$/, ""));
            sock.write(
              Buffer.concat([
                frame("C", Buffer.from("SET\0")),
                frame("D", dataRowBody(["ag:t1:1:1", "1"])),
                frame("D", dataRowBody(["ag:t1:1:1:r1", null])),
                frame("C", Buffer.from("SELECT 2\0")),
                frame("Z", Buffer.from("I")),
              ]),
            );
          } else if (type === "X") {
            sock.end();
          }
        }
      });
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    try {
      const port = (server.address() as net.AddressInfo).port;
      const url = `postgres://hub:never-printed@127.0.0.1:${port}/hub?sslmode=disable&search_path=e2e_ab12`;
      const rows = await pgQueryUrl(url, "database url", "SELECT idempotency_key, attempts FROM quota_idempotency_keys");
      assert.deepEqual(rows, [
        ["ag:t1:1:1", "1"],
        ["ag:t1:1:1:r1", ""],
      ]);
      assert.deepEqual(received, ['SET search_path TO "e2e_ab12"; SELECT idempotency_key, attempts FROM quota_idempotency_keys']);

      const plain = await pgQuery({ host: "127.0.0.1", port, user: "hub", password: "x", database: "hub" }, "SELECT 1");
      assert.equal(plain.length, 2);
      assert.equal(received[1], "SELECT 1", "no search_path option: the SQL is sent as is");
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  test("a query ErrorResponse (E, then ReadyForQuery) rejects with PgError and returns no rows", async () => {
    const frame = (type: string, payload: Buffer): Buffer => {
      const head = Buffer.alloc(5);
      head.write(type, 0, "ascii");
      head.writeInt32BE(payload.length + 4, 1);
      return Buffer.concat([head, payload]);
    };
    // ErrorResponse fields: a code byte, a C string, ...; a zero byte ends the list
    const errorBody = Buffer.concat([
      Buffer.from("SERROR\0", "latin1"),
      Buffer.from("C42P01\0", "latin1"),
      Buffer.from('Mrelation "quota_idempotency_keys" does not exist\0', "utf8"),
      Buffer.from([0]),
    ]);
    const server = net.createServer((sock) => {
      let buf = Buffer.alloc(0);
      let started = false;
      sock.on("error", () => {});
      sock.on("data", (chunk: Buffer) => {
        buf = Buffer.concat([buf, chunk]);
        while (buf.length >= (started ? 5 : 4)) {
          const len = started ? buf.readInt32BE(1) + 1 : buf.readInt32BE(0);
          if (buf.length < len) {
            return;
          }
          const type = started ? String.fromCharCode(buf[0] ?? 0) : "";
          buf = buf.subarray(len);
          if (!started) {
            started = true;
            sock.write(Buffer.concat([frame("R", Buffer.alloc(4)), frame("Z", Buffer.from("I"))]));
          } else if (type === "Q") {
            sock.write(Buffer.concat([frame("E", errorBody), frame("Z", Buffer.from("I"))]));
          } else if (type === "X") {
            sock.end();
          }
        }
      });
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    try {
      const port = (server.address() as net.AddressInfo).port;
      let rows: string[][] | undefined;
      await assert.rejects(
        async () => {
          rows = await pgQuery({ host: "127.0.0.1", port, user: "hub", password: "never-printed", database: "hub" }, "SELECT * FROM quota_idempotency_keys");
        },
        (err: unknown) => err instanceof PgError && err.sqlState === "42P01" && /does not exist/.test(err.message) && !err.message.includes("never-printed"),
      );
      assert.equal(rows, undefined, "a failed query returns no rows");
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  test("a bad database URL is named by the variable, never echoed", async () => {
    await assert.rejects(
      pgQueryUrl("postgres://hub:secret-pw@127.0.0.1/hub?sslmode=require", "DB_VAR", "SELECT 1"),
      (err: unknown) => err instanceof BadConfigError && err.message.includes("DB_VAR") && !err.message.includes("secret-pw"),
    );
  });
});

// startHub with the stub hub (a Node script that prints every secret it gets) and a recording fake database.
function stubDeps(behaviour: "ok" | "crash", port?: number) {
  const calls: string[] = [];
  const deps: HubDeps = {
    database: async () => {
      calls.push("database");
      return {
        adminUrl: "postgres://hub:db-secret-pw@127.0.0.1:1/hub?sslmode=disable",
        target: { host: "127.0.0.1", port: 1, user: "hub", password: "db-secret-pw", database: "hub" },
        release: async () => void calls.push("release"),
      };
    },
    exec: async (_t, sql) => void calls.push(sql),
    hubCommand: async () => ({ file: process.execPath, args: [stubHub, behaviour] }),
    freePort: async () => port ?? freeLoopbackPort(),
  };
  return { deps, calls };
}

/** The stub's startup output once it is all captured: /readyz can answer before the pipe delivers the last lines. */
async function logsWhenComplete(hub: StartedHub, timeoutMs = 10_000): Promise<string> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const logs = hub.logs();
    if (/^end-of-startup$/m.test(logs)) {
      return logs;
    }
    if (Date.now() > deadline) {
      assert.fail(`the stub's startup output was not captured in ${timeoutMs} ms:\n${logs}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

const ownerEnv = {
  ...process.env,
  PARATECH_E2E_ANTHROPIC_KEY: Sentinel,
  OWNER_STRAY_TOKEN: "owner-stray-value",
  HUB_RATE_LIMIT_BURST: "1",
};

describe("startHub with a stub hub", () => {
  test("live key reaches only the Hub and never the caller, the logs or JSON", async () => {
    const { deps, calls } = stubDeps("ok");
    const hub = await startHub({ mode: "live", env: ownerEnv, quotaLimit: 1234 }, deps);
    try {
      assert.equal(hub.apiKey, StubApiKey);
      assert.match(hub.url, /^http:\/\/127\.0\.0\.1:\d+$/);
      assert.match(hub.schema, /^e2e_[0-9a-f]{16}$/);

      const logs = await logsWhenComplete(hub);
      assert.match(logs, /provider key=\(redacted\)/, "the stub did print the key and it was scrubbed");
      assert.match(logs, /split=\(redacted\)/);
      assert.match(logs, /^русский \(redacted\) текст$/m, "bytes of a Cyrillic char split across chunks stay one char");
      assert.ok(!logs.includes("�"));
      assert.match(logs, /quota=1234 /);
      const names = /env-names=(.*)/.exec(logs)?.[1]?.split(",") ?? [];
      assert.ok(names.includes("HUB_ANTHROPIC_API_KEY") && names.includes("HUB_DATABASE_URL"));
      assert.ok(!names.includes("OWNER_STRAY_TOKEN") && !names.includes("HUB_RATE_LIMIT_BURST"), names.join(","));
      assert.ok(!names.some((n) => n.startsWith("PARATECH_")));

      const visible = [logs, JSON.stringify(hub), inspect(hub), Object.keys(hub).join(",")].join("\n");
      for (const secret of [Sentinel, StubApiKey, "db-secret-pw"]) {
        assert.ok(!visible.includes(secret), `leaked ${secret.slice(0, 8)}…`);
      }
      assert.match(JSON.stringify(hub), /"apiKey":"\(redacted\)"/);
      assert.deepEqual(calls.slice(0, 2), ["database", `CREATE SCHEMA ${hub.schema}`]);
    } finally {
      await hub.stop();
      await hub.stop();
    }
    assert.deepEqual(calls.slice(2), [`DROP SCHEMA IF EXISTS ${hub.schema} CASCADE`, "release"]);
  });

  test("scripted mode sets the dummy key, the fake and streamed calls", async () => {
    const { deps } = stubDeps("ok");
    const hub = await startHub({ mode: "scripted", fakeUrl: "http://127.0.0.1:4999", env: ownerEnv }, deps);
    try {
      const logs = await logsWhenComplete(hub);
      assert.match(logs, /quota=400000 stream=true base=http:\/\/127\.0\.0\.1:4999/);
      assert.match(logs, new RegExp(`provider key=${DummyProviderKey}`));
      assert.ok(!logs.includes(Sentinel));
    } finally {
      await hub.stop();
    }
  });

  test("a Hub that exits at start is a Hub failure (not an environment one), scrubbed, and cleaned up", async () => {
    const { deps, calls } = stubDeps("crash");
    const err = await startHub({ mode: "live", env: ownerEnv }, deps).then(
      () => assert.fail("startHub should fail"),
      (e: unknown) => e,
    );
    assert.ok(err instanceof HubNotReadyError && err.kind === "HubNotReady");
    assert.ok(isHubFailedError(err) && !isEnvironmentError(err), "a crashing Hub is «сломано», never «не проверено»");
    assert.match(err.message, /код 3/);
    const visible = [err.message, err.stack ?? "", JSON.stringify(err), inspect(err)].join("\n");
    for (const secret of [Sentinel, StubApiKey, "db-secret-pw"]) {
      assert.ok(!visible.includes(secret), `leaked ${secret.slice(0, 8)}…`);
    }
    assert.equal(calls.at(-1), "release");
    assert.ok(calls.some((c) => c.startsWith("DROP SCHEMA")));
  });

  test("all running Hubs share one process exit guard", async () => {
    const before = process.listenerCount("exit");
    const hubs = await Promise.all(
      [1, 2, 3].map(() => startHub({ mode: "scripted", fakeUrl: "http://127.0.0.1:4999", env: ownerEnv }, stubDeps("ok").deps)),
    );
    try {
      assert.equal(guardedHubCount(), 3);
      assert.ok(process.listenerCount("exit") - before <= 1, "at most the one shared listener is added");
    } finally {
      await Promise.all(hubs.map((h) => h.stop()));
    }
    assert.equal(guardedHubCount(), 0);
    const after = process.listenerCount("exit");
    const more = await startHub({ mode: "scripted", fakeUrl: "http://127.0.0.1:4999", env: ownerEnv }, stubDeps("ok").deps);
    await more.stop();
    assert.equal(process.listenerCount("exit"), after, "a later Hub reuses the installed guard");
  });

  test("a forbidden port fails the start and cleans up", async () => {
    const { deps, calls } = stubDeps("ok", 8080);
    await assert.rejects(startHub({ mode: "scripted", fakeUrl: "http://127.0.0.1:4999", env: ownerEnv }, deps), ForbiddenPortError);
    assert.equal(calls.at(-1), "release");
  });

  test("a missing live key fails before any database work", async () => {
    const { deps, calls } = stubDeps("ok");
    await assert.rejects(startHub({ mode: "live", env: {} }, deps), MissingLiveKeyError);
    assert.deepEqual(calls, []);
  });
});
