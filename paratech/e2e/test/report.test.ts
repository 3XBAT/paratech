import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { after, describe, mock, test } from "node:test";

import type { FullConfig, FullResult, Suite, TestCase, TestError, TestResult } from "@playwright/test/reporter";

import {
  AnnotationFeature,
  AnnotationMode,
  AnnotationRequires,
  EnvHubKey,
  EnvironmentMarker,
  EnvRunRoot,
  type RunState,
  SnapshotAttachment,
} from "../src/contract.js";
import { deepestBrokenStep, packOf, type ResultLike, type TestLike, toRecord } from "../src/report/collect.js";
import type { ReportInput, TestRecord } from "../src/report/model.js";
import { budgetWarning, cell, formatDuration, renderReport, TableHeader } from "../src/report/render.js";
import E2EReporter from "../src/report/reporter.js";
import { evaluate, firstLine } from "../src/report/rows.js";
import { ScreenshotStore } from "../src/report/screenshots.js";
import { clearRegisteredSecrets, registerSecrets, scrubSecrets } from "../src/report/scrub.js";

const tmp = mkdtempSync(path.join(tmpdir(), "e2e-report-"));
after(() => rmSync(tmp, { recursive: true, force: true }));

const png = Buffer.from("89504e470d0a1a0a-fake-png", "utf8");

function rec(over: Partial<TestRecord> = {}): TestRecord {
  return {
    title: "t",
    feature: "Запуск приложения",
    mode: "SCRIPTED",
    pack: "smoke",
    outcome: "passed",
    screenshots: [],
    durationMs: 1000,
    ...over,
  };
}

function input(tests: TestRecord[], over: Partial<ReportInput> = {}): ReportInput {
  return { tests, mode: "SCRIPTED", runnerErrors: [], elapsedMs: 60_000, ...over };
}

function state(over: Partial<RunState> = {}): RunState {
  return {
    mode: "scripted",
    startedAt: "2026-11-04T10:00:00.000Z",
    runRoot: "r",
    ctlDir: "r/ctl",
    dataDir: "r/ctl/data",
    fixturesDir: "r/ctl/fixtures",
    exePath: "r/paratech.exe",
    cdpUrl: "http://127.0.0.1:1",
    hubUrl: "http://127.0.0.1:2",
    fakeUrl: "http://127.0.0.1:3",
    ...over,
  };
}

function rowOf(text: string, feature: string): string {
  const line = text.split("\n").find((l) => l.startsWith(`| ${feature} |`));
  assert.ok(line, `no row for ${feature} in:\n${text}`);
  return line;
}

describe("rows", () => {
  test("ready, broken and not-checked rows with the exact table header", () => {
    const text = renderReport(
      input([
        rec({ feature: "Запуск приложения", screenshots: ["screenshots/001-start.png"] }),
        rec({ feature: "Настройки", outcome: "failed", brokenStep: "Открыть «Адрес сервера»", error: "Error: expect(locator).toBeVisible()\nCall log:\n  - waiting" }),
        rec({ feature: "Запись", outcome: "skipped", requires: "Настройки" }),
      ]),
      {},
    );
    assert.ok(text.includes(`${TableHeader}\n|---|---|---|---|---|`));
    assert.equal(TableHeader, "| Функция | Режим | Итог | Шаг, где сломалось | Снимок |");
    assert.equal(rowOf(text, "Запуск приложения"), "| Запуск приложения | SCRIPTED | готово | — | [снимок](screenshots/001-start.png) |");
    assert.equal(
      rowOf(text, "Настройки"),
      "| Настройки | SCRIPTED | сломано | Открыть «Адрес сервера» — Error: expect(locator).toBeVisible() | — |",
    );
    assert.equal(rowOf(text, "Запись"), "| Запись | SCRIPTED | не проверено (нужно: Настройки) | — | — |");
  });

  test("a broken test without a step names the test; the error line is cut to about 200 characters", () => {
    const long = "x".repeat(500);
    const text = renderReport(input([rec({ feature: "F", title: "the test", outcome: "failed", error: long })]), {});
    const row = rowOf(text, "F");
    assert.ok(row.includes("the test — xxx"));
    assert.ok(row.length < 320, `row too long: ${row.length}`);
    assert.ok(row.includes("…"));
  });

  test("a feature is ready only when all its tests passed; a retry is noted", () => {
    const mixed = evaluate(input([rec({ feature: "A" }), rec({ feature: "A", outcome: "failed", error: "boom" })]));
    assert.equal(mixed.rows[0]?.verdict, "broken");
    const retried = evaluate(input([rec({ feature: "A", outcome: "flaky" })]));
    assert.equal(retried.rows[0]?.outcome, "готово после повтора");
  });

  test("test.fixme and test.skip reasons make «не проверено (нужно: reason)»", () => {
    const text = renderReport(input([rec({ feature: "Звук", outcome: "skipped", skipReason: "нужен флаг --with-sound" })]), {});
    assert.equal(rowOf(text, "Звук"), "| Звук | SCRIPTED | не проверено (нужно: нужен флаг --with-sound) | — | — |");
  });

  test("tests that never ran after a failure name the broken feature they wait for", () => {
    const e = evaluate(
      input([
        rec({ feature: "Настройки", outcome: "failed", error: "boom" }),
        rec({ feature: "Запись", outcome: "notRun" }),
        rec({ feature: "Агент", outcome: "skipped" }),
      ]),
    );
    assert.deepEqual(
      e.rows.map((r) => r.outcome),
      ["сломано", "не проверено (нужно: Настройки)", "не проверено (нужно: Настройки)"],
    );
    assert.deepEqual(e.counts, { ready: 0, broken: 1, unchecked: 2 });
  });

  test("requires: unmet prerequisite (broken, missing, unchecked) makes a failed dependent «не проверено»", () => {
    const e = evaluate(
      input([
        rec({ feature: "A", outcome: "failed", error: "boom" }),
        rec({ feature: "B", outcome: "failed", error: "b failed", requires: "A" }),
        rec({ feature: "C", outcome: "failed", error: "c failed", requires: "B" }),
        rec({ feature: "D", outcome: "failed", error: "d failed", requires: "Нет такой" }),
        rec({ feature: "E", outcome: "passed", requires: "A" }),
      ]),
    );
    assert.deepEqual(
      e.rows.map((r) => r.outcome),
      [
        "сломано",
        "не проверено (нужно: A)",
        "не проверено (нужно: B)",
        "не проверено (нужно: Нет такой (не запускалась))",
        "готово",
      ],
    );
  });

  test("a requires cycle never counts as ready", () => {
    const e = evaluate(input([rec({ feature: "A", outcome: "notRun", requires: "B" }), rec({ feature: "B", outcome: "notRun", requires: "A" })]));
    assert.ok(e.rows.every((r) => r.verdict === "unchecked"));
  });

  test("modes of a feature are listed once; LIVE is kept", () => {
    const e = evaluate(input([rec({ feature: "A", mode: "LIVE" }), rec({ feature: "A", mode: "LIVE" })]));
    assert.equal(e.rows[0]?.modes, "LIVE");
  });

  test("cells escape pipes and newlines", () => {
    assert.equal(cell("a | b\nc\r\nd"), "a \\| b c d");
    assert.equal(cell("  "), "—");
    const text = renderReport(input([rec({ feature: "F", outcome: "failed", brokenStep: "шаг | с чертой", error: "строка 1\nстрока 2" })]), {});
    const row = rowOf(text, "F");
    assert.ok(row.includes("шаг \\| с чертой — строка 1 |"), row);
    assert.equal(row.split("\n").length, 1);
  });

  test("firstLine strips colour codes and skips blank lines", () => {
    assert.equal(firstLine("\n\u001b[31mError:\u001b[39m  boom\nnext"), "Error:  boom");
    assert.equal(firstLine(undefined), "");
  });
});

describe("environment and start failures", () => {
  test("a test that failed with the environment marker is «не проверено», never «сломано»", () => {
    const text = renderReport(
      input([rec({ feature: "A", outcome: "failed", error: `Error: ${EnvironmentMarker} нет переменной PARATECH_E2E_CDP_URL` })]),
      {},
    );
    const row = rowOf(text, "A");
    assert.ok(row.includes("не проверено (нужно: нет переменной PARATECH_E2E_CDP_URL)"), row);
    assert.ok(!row.includes("сломано"));
    assert.ok(text.includes("## Проблемы среды"));
  });

  test("state.environmentError: every feature is «не проверено (нужно: message)», in its own section", () => {
    const text = renderReport(
      input([rec({ feature: "A", outcome: "notRun" }), rec({ feature: "B", outcome: "failed", error: "setup died" })], {
        state: state({ environmentError: { kind: "DockerNotRunning", message: "Docker не запущен. Запустите Docker Desktop." } }),
      }),
      {},
    );
    for (const f of ["A", "B"]) {
      assert.ok(rowOf(text, f).includes("не проверено (нужно: Docker не запущен. Запустите Docker Desktop.)"));
    }
    assert.ok(!text.includes("| сломано |"));
    assert.ok(text.includes("## Проблемы среды"));
    assert.ok(text.includes("- DockerNotRunning: Docker не запущен. Запустите Docker Desktop."));
  });

  test("an environment error of the setup (onError) with no tests listed gives one «не проверено» row", () => {
    const text = renderReport(input([], { runnerErrors: [`${EnvironmentMarker} Docker не найден`] }), {});
    assert.ok(rowOf(text, "Все функции").includes("не проверено (нужно: Docker не найден)"));
    assert.ok(text.includes("## Проблемы среды"));
    assert.ok(!text.includes("## Ошибки раннера"));
  });

  test("state.startFailure is «сломано» with the step «Запуск»", () => {
    const text = renderReport(
      input([rec({ feature: "A", outcome: "notRun" })], { state: state({ startFailure: { kind: "HubNotReady", message: "Hub не ответил на /readyz за 60 с" } }) }),
      {},
    );
    assert.equal(rowOf(text, "A"), "| A | SCRIPTED | сломано | Запуск — Hub не ответил на /readyz за 60 с | — |");
    const bare = renderReport(input([], { state: state({ startFailure: { kind: "x", message: "приложение не запустилось" } }) }), {});
    assert.ok(rowOf(bare, "Запуск приложения").includes("| сломано | Запуск — приложение не запустилось |"));
  });

  test("a runner error that is not an environment problem is listed apart", () => {
    const text = renderReport(input([rec({ feature: "A", outcome: "notRun" })], { runnerErrors: ["TypeError: x is not a function\n at y"] }), {});
    assert.ok(text.includes("## Ошибки раннера"));
    assert.ok(text.includes("- TypeError: x is not a function"));
    assert.ok(rowOf(text, "A").includes("не проверено (нужно: TypeError: x is not a function)"));
  });

  test("a passed test stays «готово» next to an environment problem of another test", () => {
    const e = evaluate(
      input([rec({ feature: "A" }), rec({ feature: "B", outcome: "failed", error: `${EnvironmentMarker} popup не нашёлся` })]),
    );
    assert.deepEqual(
      e.rows.map((r) => r.verdict),
      ["ready", "unchecked"],
    );
  });
});

describe("footer and budget", () => {
  test("shows the run time, counts by outcome and the mode", () => {
    const text = renderReport(
      input([rec({ feature: "A" }), rec({ feature: "B", outcome: "failed", error: "x" }), rec({ feature: "C", outcome: "notRun" })], {
        elapsedMs: 192_000,
        mode: "LIVE",
        startedAt: "2026-11-04T10:00:00.000Z",
      }),
      {},
    );
    assert.ok(text.includes("- Режим: LIVE"));
    assert.ok(text.includes("- Время прогона: 3 мин 12 с"));
    assert.ok(text.includes("- Тесты: всего 3 — готово 1, сломано 1, не проверено 1"));
    assert.ok(text.includes("Начало прогона: 2026-11-04T10:00:00.000Z"));
    assert.ok(!text.includes("Внимание"));
  });

  test("warns over 15 minutes in SCRIPTED (30 in LIVE), not at the limit", () => {
    assert.equal(budgetWarning("SCRIPTED", 15 * 60_000), undefined);
    assert.match(budgetWarning("SCRIPTED", 15 * 60_000 + 1000) ?? "", /^Внимание: прогон занял 15 мин 1 с, бюджет режима SCRIPTED — 15 мин 0 с/);
    assert.equal(budgetWarning("LIVE", 20 * 60_000), undefined);
    assert.ok(budgetWarning("LIVE", 31 * 60_000));
    const text = renderReport(input([rec()], { elapsedMs: 16 * 60_000 }), {});
    assert.ok(text.includes("- Внимание: прогон занял 16 мин 0 с"));
  });

  test("a pack over 5 minutes is only a note", () => {
    const text = renderReport(input([rec({ pack: "agent", durationMs: 6 * 60_000 }), rec({ feature: "B", pack: "smoke" })]), {});
    assert.ok(text.includes("Примечание: пак «agent» занял 6 мин 0 с"));
    assert.ok(!text.includes("Внимание"));
  });

  test("formatDuration", () => {
    assert.equal(formatDuration(42_400), "42 с");
    assert.equal(formatDuration(60_000), "1 мин 0 с");
    assert.equal(formatDuration(-5), "0 с");
  });
});

describe("secrets", () => {
  const hubKey = "pthub_E2E_fake_hub_key_0123456789";
  const anthropic = "fake-provider-key-ABCDEFGH";
  const baseUrl = "http://gateway.fake.invalid:9999/v1";
  const auth = "bearer-fake-auth-token";
  const env = {
    [EnvHubKey]: hubKey,
    PARATECH_E2E_ANTHROPIC_KEY: anthropic,
    PARATECH_E2E_ANTHROPIC_BASE_URL: baseUrl,
    PARATECH_E2E_ANTHROPIC_AUTH: auth,
  };
  const secrets = [hubKey, anthropic, baseUrl, auth, "pthub_another_token_77", "sk-ant-api03-FAKEFAKEFAKE", "hunter2hunter2"];

  function leaky(withStartFailure: boolean): ReportInput {
    return input([
      rec({
        feature: `Функция ${hubKey}`,
        title: `title ${anthropic}`,
        outcome: "failed",
        brokenStep: `Шаг с ключом ${hubKey} и ${baseUrl}`,
        error: `Error: PARATECH_E2E_CDP_URL=hunter2hunter2 key pthub_another_token_77 provider sk-ant-api03-FAKEFAKEFAKE ${auth} Authorization: Bearer hunter2hunter2\nnext line`,
      }),
    ], {
      runnerErrors: [`setup: PARATECH_E2E_ANTHROPIC_KEY=${anthropic} failed with ${baseUrl}`],
      ...(withStartFailure ? { state: state({ startFailure: { kind: "k", message: `start ${hubKey} x-api-key: ${anthropic}` } }) } : {}),
    });
  }

  test("none of the secrets is left in the rendered report", () => {
    const text = renderReport(leaky(false), env);
    for (const s of secrets) {
      assert.ok(!text.includes(s), `leaked ${s}:\n${text}`);
    }
    assert.ok(text.includes("[скрыто]"));
    assert.ok(text.includes("PARATECH_E2E_CDP_URL=[скрыто]"));
    assert.ok(text.includes("Authorization: [скрыто]"));
  });

  test("a start failure message is scrubbed too", () => {
    const text = renderReport(leaky(true), env);
    for (const s of secrets) {
      assert.ok(!text.includes(s), `leaked ${s}:\n${text}`);
    }
    assert.ok(text.includes("| сломано | Запуск — start [скрыто] x-api-key: [скрыто] |"));
  });

  test("scrubSecrets handles patterns without env, and leaves ordinary text alone", () => {
    assert.equal(scrubSecrets("a pthub_abc-DEF_1 b", {}), "a [скрыто] b");
    assert.equal(scrubSecrets("key sk-proj-ABCDEFGH12345", {}), "key [скрыто]");
    assert.equal(scrubSecrets("disk-image task-based sk-1", {}), "disk-image task-based sk-1");
    assert.equal(scrubSecrets("PARATECH_E2E_FOO = bar baz", {}), "PARATECH_E2E_FOO = [скрыто] baz");
    assert.equal(scrubSecrets("Открыть настройки", { PARATECH_E2E_ANTHROPIC_KEY: "ab" }), "Открыть настройки");
  });

  test("registered values are scrubbed although they left the environment; short ones are ignored; clearing forgets them", () => {
    clearRegisteredSecrets();
    try {
      registerSecrets([anthropic, baseUrl, auth, "ab", undefined]);
      assert.equal(scrubSecrets(`k=${anthropic} u=${baseUrl} a=${auth}`, {}), "k=[скрыто] u=[скрыто] a=[скрыто]");
      assert.equal(scrubSecrets("ab and abc", {}), "ab and abc");
      const text = renderReport(leaky(true), {});
      for (const s of [anthropic, baseUrl, auth]) {
        assert.ok(!text.includes(s), `leaked ${s}`);
      }
      // a registered value and an env value together
      assert.equal(scrubSecrets(`${anthropic} ${hubKey}`, { [EnvHubKey]: hubKey }), "[скрыто] [скрыто]");
    } finally {
      clearRegisteredSecrets();
    }
    assert.equal(scrubSecrets(anthropic, {}), anthropic, "forgotten");
  });

  test("the written file of the reporter is scrubbed too (real process.env)", () => {
    const root = path.join(tmp, "secrets-run");
    mkdirSync(root, { recursive: true });
    const saved: Record<string, string | undefined> = {};
    for (const [k, v] of Object.entries(env)) {
      saved[k] = process.env[k];
      process.env[k] = v;
    }
    try {
      const reporter = new E2EReporter({ runRoot: root });
      const t = fakeTest({ title: `smoke ${hubKey}`, annotations: [{ type: AnnotationFeature, description: `Запуск ${anthropic}` }] });
      reporter.onBegin({} as FullConfig, suiteOf([t]));
      finish(
        reporter,
        t,
        fakeResult({
          status: "failed",
          error: { message: `boom ${hubKey} ${baseUrl}` },
          steps: [{ title: `Шаг ${auth}`, category: "test.step", error: { message: "x" }, steps: [] }],
        }),
      );
      reporter.onError({ message: `global ${anthropic}` } as TestError);
      quiet(() => reporter.onEnd({ status: "failed", startTime: new Date(), duration: 1 } as FullResult));
      const text = readFileSync(path.join(root, "report", "report.md"), "utf8");
      for (const s of [hubKey, anthropic, baseUrl, auth]) {
        assert.ok(!text.includes(s), `leaked ${s}`);
      }
      assert.ok(text.includes("| сломано |"));
    } finally {
      for (const [k, v] of Object.entries(saved)) {
        if (v === undefined) {
          delete process.env[k];
        } else {
          process.env[k] = v;
        }
      }
    }
  });
});

describe("collect", () => {
  function like(over: Partial<TestLike> & { result?: Partial<ResultLike> } = {}): TestLike {
    const result: ResultLike = { status: "passed", duration: 10, errors: [], steps: [], attachments: [], ...over.result };
    return {
      title: "t",
      annotations: [],
      results: [result],
      outcome: () => (result.status === "passed" ? "expected" : "unexpected"),
      location: { file: "C:\\repo\\paratech\\e2e\\specs\\smoke\\smoke.spec.ts" },
      ...over,
    };
  }

  test("feature, mode and requires come from annotations; defaults are the title and SCRIPTED", () => {
    const plain = toRecord(like(), []);
    assert.equal(plain.feature, "t");
    assert.equal(plain.mode, "SCRIPTED");
    assert.equal(plain.pack, "smoke");
    const full = toRecord(
      like({
        annotations: [
          { type: AnnotationFeature, description: "Запуск приложения" },
          { type: AnnotationMode, description: "live" },
          { type: AnnotationRequires, description: "Настройки" },
        ],
      }),
      ["screenshots/a.png"],
    );
    assert.equal(full.feature, "Запуск приложения");
    assert.equal(full.mode, "LIVE");
    assert.equal(full.requires, "Настройки");
    assert.deepEqual(full.screenshots, ["screenshots/a.png"]);
  });

  test("the deepest test.step with the error wins; expect and hook steps are skipped", () => {
    const steps = [
      { title: "ok", category: "test.step", steps: [] },
      {
        title: "outer",
        category: "test.step",
        error: { message: "e" },
        steps: [
          { title: "inner", category: "test.step", error: { message: "e" }, steps: [{ title: "expect.toBeVisible", category: "expect", error: { message: "e" }, steps: [] }] },
        ],
      },
    ];
    assert.equal(deepestBrokenStep(steps), "inner");
    assert.equal(deepestBrokenStep([{ title: "Before Hooks", category: "hook", error: { message: "e" }, steps: [] }]), undefined);
    const r = toRecord(like({ result: { status: "failed", error: { message: "Error: boom" }, steps } }), []);
    assert.equal(r.brokenStep, "inner");
    assert.equal(r.error, "Error: boom");
    assert.equal(r.outcome, "failed");
  });

  test("environment marker in any error marks the record", () => {
    const r = toRecord(like({ result: { status: "failed", error: { message: "Error: x" }, errors: [{ message: `${EnvironmentMarker} нет Docker` }] } }), []);
    assert.equal(r.environment, true);
    const plain = toRecord(like({ result: { status: "failed", error: { message: "Error: x" } } }), []);
    assert.equal(plain.environment, false);
  });

  test("skip and fixme reasons; skipped after a failure has no reason; no result is notRun; flaky; test.fail", () => {
    const fixme = toRecord(
      like({ annotations: [{ type: "fixme", description: "флак TASK-999" }], result: { status: "skipped" }, outcome: () => "skipped" }),
      [],
    );
    assert.equal(fixme.outcome, "skipped");
    assert.equal(fixme.skipReason, "флак TASK-999");
    const bare = toRecord(like({ annotations: [{ type: "skip" }], result: { status: "skipped" }, outcome: () => "skipped" }), []);
    assert.equal(bare.skipReason, "пропущен без причины");
    const serial = toRecord(like({ result: { status: "skipped" }, outcome: () => "skipped" }), []);
    assert.equal(serial.skipReason, undefined);
    const none = toRecord(like({ results: [], outcome: () => "skipped" }), []);
    assert.equal(none.outcome, "notRun");
    assert.equal(toRecord(like({ outcome: () => "flaky" }), []).outcome, "flaky");
    const expectedFail = toRecord(like({ result: { status: "failed" }, outcome: () => "expected", annotations: [{ type: "fail", description: "баг 12" }] }), []);
    assert.equal(expectedFail.outcome, "skipped");
    assert.equal(expectedFail.skipReason, "баг 12");
  });

  test("pack of a spec path", () => {
    assert.equal(packOf("C:\\x\\e2e\\specs\\agent\\a.spec.ts"), "agent");
    assert.equal(packOf("/x/e2e/specs/smoke/a.spec.ts"), "smoke");
    assert.equal(packOf("/x/e2e/test/a.ts"), "");
  });
});

describe("screenshots", () => {
  test("copies body and path attachments named «снимок», links once per content, never overwrites", () => {
    const report = path.join(tmp, "shots", "report");
    const store = new ScreenshotStore(report);
    const outside = path.join(tmp, "shots", "outside.png");
    mkdirSync(path.dirname(outside), { recursive: true });
    writeFileSync(outside, "other-bytes");

    const links = store.importFrom(
      [
        { name: SnapshotAttachment, body: png },
        { name: "trace", body: png },
        { name: SnapshotAttachment, path: outside },
        { name: SnapshotAttachment, path: path.join(tmp, "missing.png") },
        { name: SnapshotAttachment, body: png },
      ],
      "Запуск приложения",
    );
    assert.deepEqual(links, ["screenshots/zapusk-prilozheniya.png", "screenshots/zapusk-prilozheniya-2.png"]);
    assert.equal(readFileSync(path.join(report, "screenshots", "zapusk-prilozheniya.png")).toString(), png.toString());
    // same content again, from another test: the existing file is linked
    assert.deepEqual(store.importFrom([{ name: SnapshotAttachment, body: png }], "другой"), ["screenshots/zapusk-prilozheniya.png"]);
    // a file the step helper wrote into the folder after the store started is recognised by content
    writeFileSync(path.join(report, "screenshots", "001-step.png"), "step-helper-shot");
    assert.deepEqual(
      store.importFrom([{ name: SnapshotAttachment, body: Buffer.from("step-helper-shot") }], "x"),
      ["screenshots/001-step.png"],
    );
    // different content with the same hint gets a new name
    assert.deepEqual(store.importFrom([{ name: SnapshotAttachment, body: Buffer.from("new") }], "Запуск приложения"), ["screenshots/zapusk-prilozheniya-3.png"]);
  });
});

// The clock of the tests that measure time: Date only (timers still run), reset after each use.
function withClock<T>(fn: (tick: (ms: number) => void) => T): T {
  mock.timers.enable({ apis: ["Date"], now: Date.parse("2026-11-04T10:00:00.000Z") });
  try {
    return fn((ms) => mock.timers.tick(ms));
  } finally {
    mock.timers.reset();
  }
}

describe("reporter end to end", () => {
  test("writes report.md with links, copies screenshots, reads state.json and prints the path", () => {
    const root = path.join(tmp, "run");
    mkdirSync(root, { recursive: true });
    const attach = path.join(tmp, "run-attach.png");
    writeFileSync(attach, png);

    const a = fakeTest({ id: "a", title: "smoke", annotations: [{ type: AnnotationFeature, description: "Запуск приложения" }] });
    const b = fakeTest({ id: "b", title: "settings", annotations: [{ type: AnnotationFeature, description: "Настройки" }, { type: AnnotationRequires, description: "Запуск приложения" }] });
    const c = fakeTest({ id: "c", title: "never", annotations: [{ type: AnnotationFeature, description: "Запись" }] });
    const logs: string[] = [];
    const log = console.log;
    console.log = (...args: unknown[]) => void logs.push(args.join(" "));
    try {
      withClock((tick) => {
        // as in Playwright: the reporter exists first, then the setup writes the state, then the tests begin
        const reporter = new E2EReporter({ runRoot: root });
        tick(1000);
        writeFileSync(path.join(root, "state.json"), JSON.stringify(state({ startedAt: new Date().toISOString() })));
        tick(5 * 60_000); // the exe build and the app start: not part of the test time
        reporter.onBegin({} as FullConfig, suiteOf([a, b, c]));
        tick(90_000);
        finish(reporter, a, fakeResult({ status: "passed", attachments: [{ name: SnapshotAttachment, contentType: "image/png", path: attach }] }));
        finish(reporter, b, fakeResult({ status: "failed", error: { message: "Error: нет кнопки" }, steps: [{ title: "Открыть настройки", category: "test.step", error: { message: "x" }, steps: [] }] }));
        reporter.onEnd({ status: "failed", startTime: new Date(), duration: 1 } as FullResult);
      });
    } finally {
      console.log = log;
    }
    const report = path.join(root, "report", "report.md");
    const text = readFileSync(report, "utf8");
    assert.ok(rowOf(text, "Запуск приложения").includes("| готово | — | [снимок](screenshots/smoke.png) |"));
    assert.ok(rowOf(text, "Настройки").includes("| сломано | Открыть настройки — Error: нет кнопки | — |"));
    assert.ok(rowOf(text, "Запись").includes("не проверено (нужно: Настройки)"));
    assert.ok(existsSync(path.join(root, "report", "screenshots", "smoke.png")));
    assert.ok(text.includes("- Время прогона: 1 мин 30 с"), "counted from onBegin, not from the setup");
    assert.ok(text.includes("Начало прогона: 2026-11-04T10:00:01.000Z"), "the start comes from state.json");
    assert.ok(!text.includes("Внимание"), "the 5 min build is not counted against the budget");
    assert.ok(logs.some((l) => l.includes("report.md")), `printed: ${logs.join("|")}`);
  });

  test("onEnd without onBegin (the setup failed first) still writes the report with the state's problem", () => {
    const root = path.join(tmp, "run-early");
    mkdirSync(root, { recursive: true });
    const prev = process.env[EnvRunRoot];
    process.env[EnvRunRoot] = root;
    try {
      const reporter = new E2EReporter();
      // the setup writes the state after the reporter was created
      writeFileSync(path.join(root, "state.json"), JSON.stringify(state({ startedAt: new Date().toISOString(), environmentError: { kind: "PortBusy", message: "порт 55432 занят" } })));
      reporter.onError({ message: "global setup failed" } as TestError);
      quiet(() => reporter.onEnd({ status: "failed", startTime: new Date(), duration: 1 } as FullResult));
    } finally {
      if (prev === undefined) {
        delete process.env[EnvRunRoot];
      } else {
        process.env[EnvRunRoot] = prev;
      }
    }
    const text = readFileSync(path.join(root, "report", "report.md"), "utf8");
    assert.ok(rowOf(text, "Все функции").includes("не проверено (нужно: порт 55432 занят)"));
    assert.ok(text.includes("- PortBusy: порт 55432 занят"));
  });

  test("an interrupted run lists the cut as a runner error and unrun tests as not checked", () => {
    const root = path.join(tmp, "run-cut");
    mkdirSync(root, { recursive: true });
    const a = fakeTest({ id: "a", title: "only" });
    const reporter = new E2EReporter({ runRoot: root });
    reporter.onBegin({} as FullConfig, suiteOf([a]));
    quiet(() => reporter.onEnd({ status: "interrupted", startTime: new Date(), duration: 1 } as FullResult));
    const text = readFileSync(path.join(root, "report", "report.md"), "utf8");
    assert.ok(rowOf(text, "only").includes("не проверено (нужно: Прогон прерван.)"));
    assert.ok(text.includes("## Ошибки раннера"));
  });

  test("a run over 15 minutes prints the budget warning to stdout and into the report", () => {
    const root = path.join(tmp, "run-slow");
    mkdirSync(root, { recursive: true });
    const a = fakeTest({ id: "a", title: "slow" });
    const warnings: string[] = [];
    const { log, warn } = console;
    console.log = () => undefined;
    console.warn = (...args: unknown[]) => void warnings.push(args.join(" "));
    try {
      withClock((tick) => {
        const reporter = new E2EReporter({ runRoot: root });
        writeFileSync(path.join(root, "state.json"), JSON.stringify(state({ startedAt: new Date().toISOString() })));
        tick(10 * 60_000); // setup and build: not counted
        reporter.onBegin({} as FullConfig, suiteOf([a]));
        tick(16 * 60_000);
        finish(reporter, a, fakeResult({ status: "passed" }));
        reporter.onEnd({ status: "passed", startTime: new Date(), duration: 1 } as FullResult);
      });
    } finally {
      console.log = log;
      console.warn = warn;
    }
    assert.equal(warnings.length, 1);
    assert.match(warnings[0] ?? "", /^Внимание: прогон занял 16 мин/);
    assert.match(readFileSync(path.join(root, "report", "report.md"), "utf8"), /- Внимание: прогон занял 16 мин/);
  });

  test("a slow setup does not count: 14 min of tests after a 20 min build is within the budget", () => {
    const root = path.join(tmp, "run-slow-setup");
    mkdirSync(root, { recursive: true });
    const a = fakeTest({ id: "a", title: "ok" });
    const warnings: string[] = [];
    const { log, warn } = console;
    console.log = () => undefined;
    console.warn = (...args: unknown[]) => void warnings.push(args.join(" "));
    try {
      withClock((tick) => {
        const reporter = new E2EReporter({ runRoot: root });
        writeFileSync(path.join(root, "state.json"), JSON.stringify(state({ startedAt: new Date().toISOString() })));
        tick(20 * 60_000);
        reporter.onBegin({} as FullConfig, suiteOf([a]));
        tick(14 * 60_000);
        finish(reporter, a, fakeResult({ status: "passed" }));
        reporter.onEnd({ status: "passed", startTime: new Date(), duration: 1 } as FullResult);
      });
    } finally {
      console.log = log;
      console.warn = warn;
    }
    assert.deepEqual(warnings, []);
    assert.match(readFileSync(path.join(root, "report", "report.md"), "utf8"), /- Время прогона: 14 мин 0 с/);
  });

  test("a state.json of the previous run (older than the reporter) is ignored: no old startFailure shows up as «сломано»", () => {
    const root = path.join(tmp, "run-stale");
    mkdirSync(root, { recursive: true });
    const old = state({
      startedAt: new Date(Date.now() - 60_000).toISOString(),
      startFailure: { kind: "BuildFailed", message: "старая сборка не удалась" },
      environmentError: { kind: "DockerNotRunning", message: "старый Docker" },
    });
    writeFileSync(path.join(root, "state.json"), JSON.stringify(old));
    const a = fakeTest({ id: "a", title: "t", annotations: [{ type: AnnotationFeature, description: "Запуск приложения" }] });
    const reporter = new E2EReporter({ runRoot: root });
    reporter.onBegin({} as FullConfig, suiteOf([a]));
    finish(reporter, a, fakeResult({ status: "passed" }));
    quiet(() => reporter.onEnd({ status: "passed", startTime: new Date(), duration: 1 } as FullResult));
    const text = readFileSync(path.join(root, "report", "report.md"), "utf8");
    assert.ok(rowOf(text, "Запуск приложения").includes("| готово |"), text);
    assert.ok(!text.includes("старая сборка") && !text.includes("старый Docker"), text);
    assert.ok(!text.includes("Начало прогона"), "the old start time is not shown either");

    // the same file written after the reporter is this run's
    const fresh = new E2EReporter({ runRoot: root });
    writeFileSync(path.join(root, "state.json"), JSON.stringify({ ...old, startedAt: new Date().toISOString() }));
    fresh.onBegin({} as FullConfig, suiteOf([a]));
    finish(fresh, a, fakeResult({ status: "passed" }));
    quiet(() => fresh.onEnd({ status: "passed", startTime: new Date(), duration: 1 } as FullResult));
    assert.ok(readFileSync(path.join(root, "report", "report.md"), "utf8").includes("старый Docker"));
  });
});

// Minimal stand-ins of Playwright's objects; the reporter only reads the members set here.
function fakeTest(over: { id?: string; title?: string; annotations?: Array<{ type: string; description?: string }> } = {}): TestCase {
  const results: TestResult[] = [];
  const t = {
    id: over.id ?? "id",
    title: over.title ?? "t",
    annotations: over.annotations ?? [],
    results,
    location: { file: "C:\\repo\\paratech\\e2e\\specs\\smoke\\smoke.spec.ts", line: 1, column: 1 },
    outcome: () => {
      const last = results[results.length - 1];
      return last === undefined ? "skipped" : last.status === "passed" ? "expected" : last.status === "skipped" ? "skipped" : "unexpected";
    },
  };
  return t as unknown as TestCase;
}

function fakeResult(over: Partial<ResultLike> & { error?: { message: string } } = {}): TestResult {
  return { status: "passed", duration: 5, errors: over.error ? [over.error] : [], steps: [], attachments: [], annotations: [], ...over } as unknown as TestResult;
}

function quiet(fn: () => void): void {
  const { log, warn } = console;
  console.log = () => undefined;
  console.warn = () => undefined;
  try {
    fn();
  } finally {
    console.log = log;
    console.warn = warn;
  }
}

function finish(reporter: E2EReporter, test: TestCase, result: TestResult): void {
  test.results.push(result);
  reporter.onTestEnd(test, result);
}

function suiteOf(tests: TestCase[]): Suite {
  return { allTests: () => tests } as unknown as Suite;
}
