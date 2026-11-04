// Unit tests of the settings pack's pure parts (TASK-377): the synthetic week, the model cache and the corrupted copy, the
// key commands, the pick-queue guard, the text readers. No browser, no Docker, no network.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, test } from "node:test";

import { EnvHubDatabaseUrl, EnvHubKey } from "../src/contract.js";
import { findFiles } from "../src/fixtures/files.js";
import {
  ensureModel,
  flipOneByte,
  makeCorruptedCopy,
  type ModelPaths,
  ModelCatalogId,
  ModelFetchError,
  parseCatalogModel,
  placeInFixtures,
  readCatalogModel,
  sha256OfFile,
} from "../src/fixtures/model.js";
import { countPickLines, pendingPicks } from "../src/fixtures/picks.js";
import {
  buildWeekIcs,
  CallLinkUrl,
  icsUtc,
  isLocalToday,
  localTime,
  previewDayLabel,
  previewTime,
  ScheduleZone,
  startInTwoMinutes,
  wallClock,
  weekLessons,
  WeekSubjects,
} from "../src/fixtures/schedule.js";
import { amountOf, collapse, readableRussian } from "../src/fixtures/text.js";
import { BadConfigError, HubKeyFailedError } from "../src/hub/errors.js";
import { issueArgs, issuedKeyLine, keyTools, listArgs, parseIssued, parseKeyList, parseRevoked, revokeArgs } from "../src/hub/keys.js";
import { clearRegisteredSecrets, registerSecrets, scrubSecrets } from "../src/report/scrub.js";
import { databasePassword } from "../src/sandbox/run.js";

function tmp(): { dir: string; cleanup: () => void } {
  const dir = mkdtempSync(path.join(os.tmpdir(), "e2e-settings-"));
  return { dir, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

// ---------------------------------------------------------------- the synthetic week

describe("the synthetic week", () => {
  const now = new Date("2026-11-04T10:07:31Z");

  test("the lesson starts at the first whole minute that is at least two minutes ahead", () => {
    assert.equal(startInTwoMinutes(now).toISOString(), "2026-11-04T10:10:00.000Z");
    assert.equal(startInTwoMinutes(new Date("2026-11-04T10:07:00Z")).toISOString(), "2026-11-04T10:09:00.000Z", "on a whole minute: exactly 2:00 ahead");
    assert.equal(startInTwoMinutes(new Date("2026-11-04T10:07:00.001Z")).toISOString(), "2026-11-04T10:10:00.000Z");
    for (const s of [0, 1, 29, 30, 59]) {
      const t = new Date(Date.UTC(2026, 10, 4, 10, 7, s));
      const gap = startInTwoMinutes(t).getTime() - t.getTime();
      assert.ok(gap >= 120_000 && gap <= 180_000, `${s}s: ${gap} ms`);
    }
  });

  test("three subjects, one lesson each, the first one soon; the others on the next days", () => {
    const l = weekLessons(now);
    assert.deepEqual(l.map((x) => x.subject), [WeekSubjects.analysis, WeekSubjects.literature, WeekSubjects.history]);
    assert.deepEqual(l.map((x) => x.soon), [true, false, false]);
    assert.equal(l[1]?.start.getTime(), (l[0]?.start.getTime() ?? 0) + 24 * 3_600_000);
    assert.equal(l[2]?.start.getTime(), (l[0]?.start.getTime() ?? 0) + 48 * 3_600_000);
    assert.deepEqual(l.map((x) => x.kindLabel), ["лекция", "семинар", "лекция"]);
    assert.equal(new Set(l.map((x) => x.uid)).size, 3, "unique UIDs");
    assert.equal(l.filter((x) => x.link !== undefined).map((x) => x.subject).join(), WeekSubjects.analysis, "the link belongs to the first");
    assert.equal(l[0]?.link, CallLinkUrl);
  });

  test("the file: CRLF, one calendar, UTC times, the zone of the calendar, the teacher line and the link in the event text", () => {
    const text = buildWeekIcs(now);
    const lines = text.split("\r\n");
    assert.equal(lines.at(-1), "", "ends with CRLF");
    assert.equal(lines[0], "BEGIN:VCALENDAR");
    assert.equal(lines.at(-2), "END:VCALENDAR");
    assert.ok(!/(^|[^\r])\n/.test(text), "no bare LF");
    assert.equal(lines.filter((x) => x === "BEGIN:VEVENT").length, 3);
    assert.ok(lines.includes("X-WR-TIMEZONE:Europe/Moscow"));
    assert.ok(lines.includes("SUMMARY:Матанализ (лек.)"));
    assert.ok(lines.includes("SUMMARY:Литература (сем.)"));
    assert.ok(lines.includes("SUMMARY:История (лек.)"));
    assert.ok(lines.includes("DTSTART:20261104T101000Z"));
    assert.ok(lines.includes("DTEND:20261104T114000Z"));
    assert.ok(lines.includes("DTSTART:20261105T101000Z"));
    assert.ok(lines.includes("DESCRIPTION:Преподаватель: Тестов Тест Тестович\\nСсылка на пару: https://telemost.yandex.ru/j/00000000000001"));
    assert.ok(lines.includes("LOCATION:https://telemost.yandex.ru/j/00000000000001"));
    assert.equal(lines.filter((x) => x.startsWith("LOCATION:")).length, 1, "only Матанализ has a link");
    assert.equal(text.split("telemost.yandex.ru").length - 1, 2, "the link is in the text of one event only (DESCRIPTION and LOCATION)");
  });

  test("the call link has the shape ParseCallLink accepts (calllink.go, ADR-0032)", () => {
    // A mirror of the rules of domain/schedule/values/calllink.go that matter for this URL; the Go test pins the same
    // shape: calllink_test.go has "https://telemost.yandex.ru/j/12345678901234".
    const u = CallLinkUrl;
    assert.match(u, /^https:\/\/[a-z0-9.-]+(\/[A-Za-z0-9\-._~%!$&()*+,;=:@/]*)?$/);
    assert.ok(u.length <= 2048);
    assert.ok(!/[#\s"'<>\\^{|}`]/.test(u), "no fragment, space or forbidden character");
    const host = new URL(u).host;
    assert.ok(!host.includes(":") && !host.includes("@") && host.split(".").length >= 2);
    assert.ok(host.split(".").every((x) => x !== "" && !x.startsWith("-") && !x.endsWith("-") && !x.startsWith("xn--")));
    assert.ok(!/^\d+$/.test(host.split(".").at(-1) ?? ""), "the last label is not a number");
    assert.equal(host, "telemost.yandex.ru");
  });

  test("icsUtc writes the minute in UTC", () => {
    assert.equal(icsUtc(new Date("2026-01-02T03:04:59Z")), "20260102T030400Z");
  });

  test("what the preview shows: the time in the zone of the file, not of this machine", () => {
    const [a, b, c] = weekLessons(now);
    assert.ok(a && b && c);
    assert.equal(ScheduleZone, "Europe/Moscow");
    assert.deepEqual(wallClock(a.start), { date: "04.11.2026", time: "13:10", day: "2026-11-04" });
    assert.equal(previewTime(a), "13:10–14:40");
    assert.equal(previewTime(b), "13:10–14:30");
    assert.equal(previewTime(c), "13:10–14:30");
    assert.equal(previewTime(a, "UTC"), "10:10–11:40");
    // a lesson that crosses midnight of the zone keeps the date of its end
    const late = { start: new Date("2026-11-04T20:30:00Z"), end: new Date("2026-11-04T21:30:00Z") };
    assert.equal(previewTime(late), "23:30 – 05.11.2026 00:30");
  });

  test("the day header of the preview: «Сегодня» / «Завтра» in the zone of the file, nothing for a later day", () => {
    const [a, b, c] = weekLessons(now);
    assert.ok(a && b && c);
    assert.equal(previewDayLabel(a, now), "Сегодня");
    assert.equal(previewDayLabel(b, now), "Завтра");
    assert.equal(previewDayLabel(c, now), "");
    // 23:58 in Moscow: the soon lesson is already on the next day there
    const lateNow = new Date("2026-11-04T20:58:00Z");
    const [d] = weekLessons(lateNow);
    assert.ok(d);
    assert.equal(wallClock(d.start).time, "00:00");
    assert.equal(previewDayLabel(d, lateNow), "Завтра");
  });

  test("local time and the local date of a moment (the form of «Сегодня»)", () => {
    const d = new Date(2026, 10, 4, 9, 5);
    assert.equal(localTime(d), "09:05");
    assert.equal(isLocalToday(d, new Date(2026, 10, 4, 23, 59)), true);
    assert.equal(isLocalToday(d, new Date(2026, 10, 5, 0, 0)), false);
  });
});

// ---------------------------------------------------------------- the model cache

describe("the model of the catalog", () => {
  const bytes = Buffer.from("not a real model, but with a known hash");
  const sha = createHash("sha256").update(bytes).digest("hex");
  const entry = { id: ModelCatalogId, source_url: "https://models.example.invalid/x/ggml-base-q5_1.bin", size_bytes: bytes.length, sha256: sha };
  const catalogText = JSON.stringify({ schema_version: 1, models: [{ id: "other", source_url: "https://a.invalid/o.bin", size_bytes: 1, sha256: "0".repeat(64) }, entry] });

  function setup() {
    const t = tmp();
    mkdirSync(path.join(t.dir, "app"), { recursive: true });
    const paths: ModelPaths = { catalog: path.join(t.dir, "app", "catalog.json"), cacheDir: path.join(t.dir, "cache", "e2e") };
    writeFileSync(paths.catalog, catalogText);
    return { ...t, paths, log: [] as string[] };
  }
  const writes = (body: Buffer, calls: string[] = []) => (url: string, dest: string, onProgress: (d: number, t: number) => void) => {
    calls.push(url);
    writeFileSync(dest, body);
    onProgress(body.length, body.length);
    return Promise.resolve();
  };

  test("the entry is read from catalog.json: file name from the URL, size and hash as they are", () => {
    const m = parseCatalogModel(catalogText);
    assert.deepEqual(m, { id: "base-q5_1", fileName: "ggml-base-q5_1.bin", sourceUrl: entry.source_url, sizeBytes: bytes.length, sha256: sha });
  });

  test("a bad entry is refused with a reason, not guessed", () => {
    const bad = (patch: Record<string, unknown>) => JSON.stringify({ models: [{ ...entry, ...patch }] });
    assert.throws(() => parseCatalogModel("{", ModelCatalogId), /не JSON/);
    assert.throws(() => parseCatalogModel("{}"), /нет списка models/);
    assert.throws(() => parseCatalogModel(JSON.stringify({ models: [] })), /нет модели base-q5_1/);
    assert.throws(() => parseCatalogModel(bad({ source_url: "http://a.invalid/x.bin" })), /https/);
    assert.throws(() => parseCatalogModel(bad({ size_bytes: 0 })), /size_bytes/);
    assert.throws(() => parseCatalogModel(bad({ size_bytes: "5" })), /size_bytes/);
    assert.throws(() => parseCatalogModel(bad({ sha256: "ABC" })), /sha256/);
    assert.throws(() => parseCatalogModel(bad({ sha256: "G".repeat(64) })), /sha256/);
    assert.throws(() => parseCatalogModel(bad({ source_url: "https://a.invalid/x/readme.txt" })), /\.bin/);
    assert.throws(() => parseCatalogModel(bad({ source_url: "https://a.invalid/x/..%2F..%2Fx.bin" })), /\.bin/);
  });

  test("the real catalog.json of the repository holds a usable entry for the pack", () => {
    const m = readCatalogModel();
    assert.equal(m.fileName, "ggml-base-q5_1.bin");
    assert.match(m.sha256, /^[0-9a-f]{64}$/);
    assert.ok(m.sizeBytes > 1_000_000);
    assert.match(m.sourceUrl, /^https:\/\//);
  });

  test("a missing file is downloaded to .part, verified, renamed; the numbers come from the catalog", async () => {
    const s = setup();
    try {
      const calls: string[] = [];
      const r = await ensureModel({ paths: s.paths, download: writes(bytes, calls), log: (l) => s.log.push(l) });
      assert.equal(r.downloaded, true);
      assert.equal(path.basename(r.file), "ggml-base-q5_1.bin");
      assert.deepEqual(calls, [entry.source_url]);
      assert.equal(readFileSync(r.file).equals(bytes), true);
      assert.ok(!existsSync(`${r.file}.part`), "the .part file is gone");
      assert.ok(s.log.some((l) => /100 %/.test(l)), s.log.join("\n"));
      assert.equal(statSync(r.file).size, bytes.length);
    } finally {
      s.cleanup();
    }
  });

  test("a verified file in the cache is reused: no download", async () => {
    const s = setup();
    try {
      mkdirSync(s.paths.cacheDir, { recursive: true });
      writeFileSync(path.join(s.paths.cacheDir, "ggml-base-q5_1.bin"), bytes);
      const calls: string[] = [];
      const r = await ensureModel({ paths: s.paths, download: writes(Buffer.from("never"), calls), log: () => undefined });
      assert.equal(r.downloaded, false);
      assert.deepEqual(calls, []);
    } finally {
      s.cleanup();
    }
  });

  test("a cached file with the wrong size or hash is not trusted and is downloaded again", async () => {
    const s = setup();
    try {
      mkdirSync(s.paths.cacheDir, { recursive: true });
      const file = path.join(s.paths.cacheDir, "ggml-base-q5_1.bin");
      writeFileSync(file, bytes.subarray(1)); // wrong size
      assert.equal((await ensureModel({ paths: s.paths, download: writes(bytes), log: () => undefined })).downloaded, true);
      writeFileSync(file, flipOneByte(bytes, 3)); // right size, wrong hash
      assert.equal((await ensureModel({ paths: s.paths, download: writes(bytes), log: () => undefined })).downloaded, true);
      assert.equal(await sha256OfFile(file), sha);
    } finally {
      s.cleanup();
    }
  });

  test("a download that does not match is a loud error and leaves nothing under the final name or as .part", async () => {
    const s = setup();
    try {
      await assert.rejects(ensureModel({ paths: s.paths, download: writes(flipOneByte(bytes, 5)), log: () => undefined }), (e: unknown) => e instanceof ModelFetchError && /SHA-256/.test(e.message));
      await assert.rejects(ensureModel({ paths: s.paths, download: writes(bytes.subarray(2)), log: () => undefined }), (e: unknown) => e instanceof ModelFetchError && /размер/.test(e.message));
      const failing = () => Promise.reject(new Error("connection reset"));
      await assert.rejects(ensureModel({ paths: s.paths, download: failing, log: () => undefined }), (e: unknown) => e instanceof ModelFetchError && /connection reset/.test(e.message));
      assert.deepEqual(findFiles(s.paths.cacheDir, /.*/), [], "nothing is left in the cache");
    } finally {
      s.cleanup();
    }
  });
});

describe("the corrupted copy", () => {
  test("exactly one byte differs, the size is the same, the original is untouched", () => {
    const orig = Buffer.from(Array.from({ length: 1000 }, (_, i) => i % 251));
    const copy = flipOneByte(orig, 500);
    assert.equal(copy.length, orig.length);
    const diff = [...orig].flatMap((b, i) => (b === copy[i] ? [] : [i]));
    assert.deepEqual(diff, [500]);
    assert.equal(orig[500], 500 % 251);
    assert.equal(flipOneByte(orig).equals(orig), false);
    const mid = [...orig].flatMap((b, i) => (b === flipOneByte(orig)[i] ? [] : [i]));
    assert.deepEqual(mid, [500], "default: the middle, behind any header");
  });

  test("an offset outside the file is refused", () => {
    assert.throws(() => flipOneByte(Buffer.alloc(4), 4), RangeError);
    assert.throws(() => flipOneByte(Buffer.alloc(4), -1), RangeError);
    assert.throws(() => flipOneByte(Buffer.alloc(4), 1.5), RangeError);
    assert.throws(() => flipOneByte(Buffer.alloc(0)), RangeError);
  });

  test("the file helper writes a real copy under fixtures/ (not a link) and the good one is a link or a copy of the original", () => {
    const t = tmp();
    try {
      const src = path.join(t.dir, "model.bin");
      const data = Buffer.from(Array.from({ length: 4096 }, (_, i) => (i * 7) % 256));
      writeFileSync(src, data);
      const fixtures = path.join(t.dir, "ctl", "fixtures");

      assert.equal(placeInFixtures(src, fixtures, "models/ggml-base-q5_1.bin"), "fixtures/models/ggml-base-q5_1.bin");
      assert.equal(readFileSync(path.join(fixtures, "models", "ggml-base-q5_1.bin")).equals(data), true);

      const bad = makeCorruptedCopy(src, fixtures, "models-corrupt/ggml-base-q5_1.bin");
      assert.deepEqual(bad, { pick: "fixtures/models-corrupt/ggml-base-q5_1.bin", offset: 2048 });
      const damaged = readFileSync(path.join(fixtures, "models-corrupt", "ggml-base-q5_1.bin"));
      assert.equal(damaged.length, data.length);
      assert.equal([...data].filter((b, i) => b !== damaged[i]).length, 1);
      assert.equal(readFileSync(src).equals(data), true, "the source is not changed through a link");
      assert.equal(readFileSync(path.join(fixtures, "models", "ggml-base-q5_1.bin")).equals(data), true, "the good copy is not changed either");
      // a second call replaces the file (no EEXIST, no stale bytes)
      makeCorruptedCopy(src, fixtures, "models-corrupt/ggml-base-q5_1.bin", 10);
      assert.equal([...data].filter((b, i) => b !== readFileSync(path.join(fixtures, "models-corrupt", "ggml-base-q5_1.bin"))[i]).length, 1);
    } finally {
      t.cleanup();
    }
  });
});

// ---------------------------------------------------------------- hub keys

describe("hub keys: arguments and output", () => {
  // The exact shapes of hub/internal/input/cli/keys.go.
  const created =
    'API key 0123456789abcdef issued for "e2e-rev-abc" at 2026-11-04T10:00:00Z.\n' +
    "It is shown only this once and cannot be recovered; hand it to the tester over a private channel:\n" +
    "pthub_0123456789abcdef_s3cr3tvalue-XYZ\n";

  test("create and revoke arguments follow config.go; a bad label or id is refused before anything runs", () => {
    assert.deepEqual(issueArgs("e2e-rev-1"), ["keys", "create", "-label", "e2e-rev-1"]);
    assert.deepEqual(revokeArgs("0123456789abcdef"), ["keys", "revoke", "-id", "0123456789abcdef"]);
    assert.deepEqual(listArgs(), ["keys", "list"]);
    for (const bad of ["", "UPPER", "with space", "a".repeat(33), "ключ", "x;y", "-l abel"]) {
      assert.throws(() => issueArgs(bad), BadConfigError, bad);
    }
    for (const bad of ["", "abc", "a b c d", "x".repeat(65), "id;rm", "ид-ключа"]) {
      assert.throws(() => revokeArgs(bad), BadConfigError, bad);
    }
    assert.equal(issueArgs("a".repeat(32)).length, 4);
  });

  test("parseIssued takes the id of the first line and the key line", () => {
    assert.deepEqual(parseIssued(created), { id: "0123456789abcdef", key: "pthub_0123456789abcdef_s3cr3tvalue-XYZ" });
    assert.deepEqual(parseIssued(created.replace(/\n/g, "\r\n")), { id: "0123456789abcdef", key: "pthub_0123456789abcdef_s3cr3tvalue-XYZ" });
    assert.equal(parseIssued("API key 0123456789abcdef issued for \"x\" at t.\n"), undefined, "no key line");
    assert.equal(parseIssued("pthub_abc\n"), undefined, "no id line");
    assert.equal(parseIssued(""), undefined);
    assert.ok(issuedKeyLine.test("pthub_a-b_c"));
    assert.ok(!issuedKeyLine.test("pthub_ a"));
  });

  test("parseRevoked knows a first and a repeated revoke", () => {
    assert.deepEqual(parseRevoked("API key 0123456789abcdef revoked at 2026-11-04T10:00:00Z.\n"), { id: "0123456789abcdef", already: false });
    assert.deepEqual(parseRevoked("API key 0123456789abcdef was already revoked at 2026-11-04T10:00:00Z.\n"), { id: "0123456789abcdef", already: true });
    assert.equal(parseRevoked("cli: no API key with id x"), undefined);
  });

  test("parseKeyList reads the padded table and skips the header", () => {
    const table =
      "ID                LABEL        STATUS   CREATED               REVOKED\n" +
      "aaaaaaaaaaaaaaaa  e2e-run-1    active   2026-11-04T10:00:00Z  -\n" +
      "bbbbbbbbbbbbbbbb  e2e-rev-abc  revoked  2026-11-04T10:01:00Z  2026-11-04T10:02:00Z\n";
    assert.deepEqual(parseKeyList(table), [
      { id: "aaaaaaaaaaaaaaaa", label: "e2e-run-1", status: "active" },
      { id: "bbbbbbbbbbbbbbbb", label: "e2e-rev-abc", status: "revoked" },
    ]);
    assert.deepEqual(parseKeyList("ID  LABEL  STATUS  CREATED  REVOKED\n"), []);
  });
});

describe("hub keys: the commands", () => {
  const url = "postgres://hub:s3cret-db-pass@127.0.0.1:5432/hub?sslmode=disable&search_path=e2e_x";
  const created = 'API key 0123456789abcdef issued for "l" at t.\nIt is shown only this once.\npthub_0123456789abcdef_value\n';

  function recorder(out: string, code: number | null = 0, stderr = "") {
    const calls: Array<{ file: string; args: string[]; env: Record<string, string> }> = [];
    return {
      calls,
      exec: (file: string, args: string[], env: Record<string, string>) => {
        calls.push({ file, args, env });
        return Promise.resolve({ code, stdout: out, stderr, timedOut: false });
      },
    };
  }

  test("issue runs `keys create -label` with the database URL and a minimal environment", async () => {
    const r = recorder(created);
    const tools = keyTools({
      env: { [EnvHubDatabaseUrl]: url, [EnvHubKey]: "pthub_runkey_secret", PATH: "C:\\bin", SystemRoot: "C:\\Windows", PARATECH_E2E_ANTHROPIC_KEY: "sk-leak", HUB_ANTHROPIC_API_KEY: "leak", GOFLAGS: "-x" },
      hubExe: () => Promise.resolve("C:\\hub.exe"),
      exec: r.exec,
    });
    const k = await tools.issueKey("e2e-rev-1");
    assert.deepEqual(k, { id: "0123456789abcdef", key: "pthub_0123456789abcdef_value", label: "e2e-rev-1" });
    assert.equal(r.calls.length, 1);
    assert.equal(r.calls[0]?.file, "C:\\hub.exe");
    assert.deepEqual(r.calls[0]?.args, ["keys", "create", "-label", "e2e-rev-1"]);
    assert.deepEqual(Object.keys(r.calls[0]?.env ?? {}).sort(), ["HUB_DATABASE_URL", "HUB_LOG_LEVEL", "PATH", "SystemRoot"], "no key, no provider variable, no toolchain setup reaches the Hub binary");
    assert.equal(r.calls[0]?.env.HUB_DATABASE_URL, url);
  });

  test("revoke runs `keys revoke -id`; a repeated revoke is fine; any other answer is an error", async () => {
    const env = { [EnvHubDatabaseUrl]: url };
    const ok = recorder("API key 0123456789abcdef revoked at t.\n");
    await keyTools({ env, hubExe: () => Promise.resolve("h"), exec: ok.exec }).revokeKey("0123456789abcdef");
    assert.deepEqual(ok.calls[0]?.args, ["keys", "revoke", "-id", "0123456789abcdef"]);
    const again = recorder("API key 0123456789abcdef was already revoked at t.\n");
    await keyTools({ env, hubExe: () => Promise.resolve("h"), exec: again.exec }).revokeKey("0123456789abcdef");
    const odd = recorder("something else\n");
    await assert.rejects(keyTools({ env, hubExe: () => Promise.resolve("h"), exec: odd.exec }).revokeKey("0123456789abcdef"), HubKeyFailedError);
  });

  test("without the database URL of the run the commands refuse to start", async () => {
    const r = recorder(created);
    await assert.rejects(keyTools({ env: {}, hubExe: () => Promise.resolve("h"), exec: r.exec }).issueKey("x"), (e: unknown) => e instanceof BadConfigError && e.message.includes(EnvHubDatabaseUrl));
    assert.equal(r.calls.length, 0);
  });

  test("a failing command is an error whose text carries neither the database URL nor a key", async () => {
    const key = "pthub_runkey_0123456789abc";
    const env = { [EnvHubDatabaseUrl]: url, [EnvHubKey]: key };
    const r = recorder("", 1, `cli: connect ${url} failed\nbad key ${key}`);
    await assert.rejects(keyTools({ env, hubExe: () => Promise.resolve("h"), exec: r.exec }).issueKey("x"), (e: unknown) => {
      assert.ok(e instanceof HubKeyFailedError);
      assert.ok(!e.message.includes("s3cret-db-pass"), e.message);
      assert.ok(!e.message.includes(key), e.message);
      assert.match(e.message, /hub keys create не удалась/);
      return true;
    });
    const timedOut = { exec: () => Promise.resolve({ code: null, stdout: "", stderr: "", timedOut: true }) };
    await assert.rejects(keyTools({ env, hubExe: () => Promise.resolve("h"), exec: timedOut.exec }).listKeys(), /истекло время/);
  });
});

describe("the database URL of the Hub is a secret of the run", () => {
  const url = "postgres://hub:s3cret-db-pass@127.0.0.1:5432/hub?sslmode=disable&search_path=e2e_x";

  test("it is scrubbed from texts by its environment variable, and its password as a registered value", () => {
    clearRegisteredSecrets();
    try {
      const scrubbed = scrubSecrets(`connect ${url} and PARATECH_E2E_HUB_DATABASE_URL=${url}`, { [EnvHubDatabaseUrl]: url });
      assert.ok(!scrubbed.includes("s3cret-db-pass"), scrubbed);
      assert.ok(!scrubbed.includes("postgres://"), scrubbed);
      assert.match(scrubbed, /\[скрыто\]/);

      // the setup also registers the password alone (a driver error may quote it without the rest of the URL)
      assert.ok(scrubSecrets("password s3cret-db-pass", {}).includes("s3cret-db-pass"), "not known yet");
      registerSecrets([url, databasePassword(url)]);
      assert.equal(scrubSecrets("password s3cret-db-pass", {}), "password [скрыто]");
    } finally {
      clearRegisteredSecrets();
    }
  });

  test("its password is read out of the URL for the scrubber", () => {
    assert.equal(databasePassword(url), "s3cret-db-pass");
    assert.equal(databasePassword("postgres://hub:p%40ss%3Aword@h/db"), "p@ss:word");
    assert.equal(databasePassword("postgres://hub@h/db"), undefined);
    assert.equal(databasePassword("not a url"), undefined);
  });
});

// ---------------------------------------------------------------- pick queue, files, texts

describe("the pick queue guard", () => {
  test("only complete lines count; the app has used what consumed.jsonl lists", () => {
    assert.equal(countPickLines(""), 0);
    assert.equal(countPickLines('{"dialog":"model","paths":["fixtures/a.bin"]}\n'), 1);
    assert.equal(countPickLines('{"dialog":"model","paths":["fixtures/a.bin"]}\n{"dialog":"sched'), 1, "a half-written last line is not a pick yet");
    assert.equal(countPickLines('{"a":1}\n\n{"b":2}\n'), 2);
    const t = tmp();
    try {
      assert.equal(pendingPicks(t.dir), 0, "no files: nothing pending");
      writeFileSync(path.join(t.dir, "picks.jsonl"), '{"dialog":"model","paths":["fixtures/a.bin"]}\n{"dialog":"schedule","paths":["fixtures/b.ics"]}\n');
      assert.equal(pendingPicks(t.dir), 2);
      writeFileSync(path.join(t.dir, "consumed.jsonl"), '{"line":1,"dialog":"model","result":"picked"}\n');
      assert.equal(pendingPicks(t.dir), 1);
      writeFileSync(path.join(t.dir, "consumed.jsonl"), '{"line":1,"dialog":"model","result":"picked"}\n{"line":2,"dialog":"schedule","result":"cancelled","reason":"x"}\n');
      assert.equal(pendingPicks(t.dir), 0, "a cancelled dialog used its line too");
    } finally {
      t.cleanup();
    }
  });
});

describe("findFiles", () => {
  test("finds files below a folder by base name, sorted, and none in a missing folder", () => {
    const t = tmp();
    try {
      mkdirSync(path.join(t.dir, "a", "b"), { recursive: true });
      writeFileSync(path.join(t.dir, "x.bin"), "1");
      writeFileSync(path.join(t.dir, "a", "b", "y.BIN"), "1");
      writeFileSync(path.join(t.dir, "a", "z.txt"), "1");
      assert.deepEqual(findFiles(t.dir, /\.bin$/i).map((f) => path.relative(t.dir, f)), [path.join("a", "b", "y.BIN"), "x.bin"]);
      assert.deepEqual(findFiles(path.join(t.dir, "nope"), /.*/), []);
    } finally {
      t.cleanup();
    }
  });
});

describe("text readers", () => {
  test("amounts: groups of three in any kind of space", () => {
    assert.equal(amountOf("400 000"), 400000);
    assert.equal(amountOf("400\u00a0000"), 400000);
    assert.equal(amountOf("400\u202f000"), 400000);
    assert.equal(amountOf("1 234 567"), 1234567);
    assert.equal(amountOf("0"), 0);
    assert.ok(Number.isNaN(amountOf("")));
    assert.ok(Number.isNaN(amountOf("12,5")));
    assert.ok(Number.isNaN(amountOf("NaN")));
    assert.ok(Number.isNaN(amountOf("400 000 токенов")));
  });

  test("collapse turns every space into one plain space", () => {
    assert.equal(collapse("  Лимит,\u00a0токенов\n\t400\u202f000 "), "Лимит, токенов 400 000");
  });

  test("a readable Russian message passes; empty, English, raw leftovers, hashes and paths do not", () => {
    assert.deepEqual(readableRussian("Контрольная сумма SHA-256 не совпадает: файл повреждён или это другой файл. Модель не установлена"), []);
    assert.deepEqual(readableRussian("Hub не принял ключ: он неверный или отозван. Введите действующий ключ."), []);
    assert.deepEqual(readableRussian(""), ["текст пустой", "в тексте нет русских слов"]);
    assert.ok(readableRussian("checksum mismatch").includes("в тексте нет русских слов"));
    for (const raw of ["Не удалось: undefined", "Ошибка null", "Итого NaN токенов", "Не вышло [object Object]", "Error: boom, не вышло", "TypeError в модуле", "panic: runtime error, сбой"]) {
      assert.ok(readableRussian(raw).some((p) => p.includes("остатки")), raw);
    }
    assert.deepEqual(readableRussian("Ошибка при чтении файла"), [], "an ordinary word «Ошибка» is fine");
    assert.ok(readableRussian(`Сумма ${"a1".repeat(32)} не совпала`).some((p) => p.includes("хеша")));
    assert.ok(readableRussian("Нет файла C:\\Users\\x\\model.bin").some((p) => p.includes("путь")));
    assert.ok(readableRussian("Сбой в models.go:123").some((p) => p.includes("путь")));
  });
});
