import assert from "node:assert/strict";
import { appendFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import vm from "node:vm";
import { after, before, describe, test } from "node:test";

import { EnvironmentMarker } from "../src/contract.js";
import { type FakeLLM, startFakeLLM } from "../src/fakellm/index.js";
import { createLlm, describeCall, UnscriptedGuard, UnscriptedLlmError } from "../src/steps/llm.js";
import { parseOpened, readOpened } from "../src/steps/opened.js";
import {
  appendPick,
  buildPickLine,
  consumedPicks,
  parseConsumed,
  PickDialogs,
  PickError,
  validatePick,
} from "../src/steps/picks.js";
import { envError, requireEnv } from "../src/steps/env.js";
import { slugify, stepSlug } from "../src/steps/slug.js";
import { clipboardStubSource } from "../src/steps/stubs.js";
import { uniqueFile } from "../src/steps/shots.js";

const tmp = mkdtempSync(path.join(tmpdir(), "e2e-steps-"));
after(() => rmSync(tmp, { recursive: true, force: true }));

describe("pick", () => {
  test("builds one line with a trailing newline", () => {
    const line = buildPickLine("recording", ["fixtures/clip.wav"]);
    assert.equal(line, '{"dialog":"recording","paths":["fixtures/clip.wav"]}\n');
    assert.deepEqual(JSON.parse(line), { dialog: "recording", paths: ["fixtures/clip.wav"] });
  });

  test("every dialog of the app is known; notes may take several files, the others one", () => {
    assert.deepEqual([...PickDialogs], ["schedule", "model", "material", "notes", "recording"]);
    assert.doesNotThrow(() => validatePick("notes", ["fixtures/a.md", "fixtures/notes/b.md"]));
    assert.throws(() => validatePick("recording", ["fixtures/a.wav", "fixtures/b.wav"]), PickError);
  });

  const bad: Array<[string, string, string[]]> = [
    ["unknown dialog", "recordng", ["fixtures/a.wav"]],
    ["no paths", "recording", []],
    ["empty path", "recording", [""]],
    ["no fixtures prefix", "recording", ["clip.wav"]],
    ["only the prefix", "recording", ["fixtures/"]],
    ["wrong folder", "recording", ["fixtures2/a.wav"]],
    ["parent component", "recording", ["fixtures/../secret.wav"]],
    ["dot component", "recording", ["fixtures/./a.wav"]],
    ["doubled separator", "recording", ["fixtures//a.wav"]],
    ["trailing separator", "recording", ["fixtures/a/"]],
    ["absolute posix", "recording", ["/etc/passwd"]],
    ["absolute windows", "recording", ["C:/Windows/a.wav"]],
    ["drive relative", "recording", ["C:fixtures/a.wav"]],
    ["backslash", "recording", ["fixtures\\a.wav"]],
    ["stream", "recording", ["fixtures/a.wav:s"]],
    ["control character", "recording", ["fixtures/a\u0000.wav"]],
    ["trailing dot", "recording", ["fixtures/a."]],
    ["short name", "recording", ["fixtures/CLIP~1.WAV"]],
    ["not a string", "recording", [42 as unknown as string]],
  ];
  for (const [name, dialog, paths] of bad) {
    test(`rejects: ${name}`, () => {
      assert.throws(() => buildPickLine(dialog, paths), PickError);
    });
  }

  test("appendPick adds lines to picks.jsonl, each ending with a newline, and refuses a bad pick without writing", () => {
    const ctl = path.join(tmp, "ctl-pick");
    mkdirSync(ctl, { recursive: true });
    appendPick(ctl, "schedule", ["fixtures/schedule.ics"]);
    assert.throws(() => appendPick(ctl, "schedule", ["../x"]), PickError);
    appendPick(ctl, "notes", ["fixtures/n1.md", "fixtures/n2.md"]);
    const text = readFileSync(path.join(ctl, "picks.jsonl"), "utf8");
    assert.equal(text.endsWith("\n"), true);
    assert.deepEqual(text.split("\n").filter(Boolean).map((l) => JSON.parse(l)), [
      { dialog: "schedule", paths: ["fixtures/schedule.ics"] },
      { dialog: "notes", paths: ["fixtures/n1.md", "fixtures/n2.md"] },
    ]);
  });

  test("consumed.jsonl: lines of the app are parsed, a half-written last line and a missing file are not an error", () => {
    const ctl = path.join(tmp, "ctl-consumed");
    mkdirSync(ctl, { recursive: true });
    assert.deepEqual(consumedPicks(ctl), []);
    writeFileSync(
      path.join(ctl, "consumed.jsonl"),
      '{"line":1,"dialog":"recording","result":"picked"}\n{"line":2,"dialog":"notes","result":"cancelled","reason":"no such file"}\n{"line":3,"dia',
    );
    const lines = consumedPicks(ctl);
    assert.equal(lines.length, 2);
    assert.equal(lines[1]?.reason, "no such file");
    assert.deepEqual(parseConsumed('{"line":"x"}\n'), []);
  });
});

describe("opened", () => {
  const line = (kind: string, value: string): string => `${JSON.stringify({ kind, value, at: "2026-11-04T10:00:00Z" })}\n`;

  test("missing file is no records", () => {
    assert.deepEqual(readOpened(path.join(tmp, "nowhere")), []);
  });

  test("parses the four kinds and keeps order", () => {
    const text = line("link", "https://example.invalid/a") + line("file", "artifact-1") + line("call", "https://meet.invalid/x") + line("clipboard", "текст\nс переводом");
    const got = parseOpened(text);
    assert.deepEqual(
      got.map((r) => r.kind),
      ["link", "file", "call", "clipboard"],
    );
    assert.equal(got[3]?.value, "текст\nс переводом");
  });

  test("skips a half-written last line but refuses a corrupt line in the middle", () => {
    assert.equal(parseOpened(line("link", "https://a.invalid") + '{"kind":"link","val').length, 1);
    assert.throws(() => parseOpened('{"kind":"link"\n' + line("link", "https://a.invalid")), /line 1 is not JSON/);
    assert.throws(() => parseOpened(line("teleport", "x")), /not an opened record/);
  });

  test("readOpened reads the file written by the app", () => {
    const ctl = path.join(tmp, "ctl-opened");
    mkdirSync(ctl, { recursive: true });
    writeFileSync(path.join(ctl, "opened.jsonl"), line("link", "https://a.invalid"));
    appendFileSync(path.join(ctl, "opened.jsonl"), '{"kind":"clip');
    assert.deepEqual(readOpened(ctl).map((r) => r.value), ["https://a.invalid"]);
  });
});

describe("slug", () => {
  test("is ASCII, transliterates Russian and keeps order with the counter", () => {
    assert.equal(slugify("Открыть «Адрес сервера»"), "otkryt-adres-servera");
    assert.equal(stepSlug("Открыть «Адрес сервера»", 7), "007-otkryt-adres-servera");
    assert.equal(stepSlug("", 12), "012-step");
    assert.equal(stepSlug("%%% ???", 1), "001-step");
    assert.match(stepSlug("a/b\\c:d*e?f", 3), /^003-a-b-c-d-e-f$/);
  });

  test("is cut to a short length without a trailing dash", () => {
    const s = slugify(`${"слово ".repeat(40)}`);
    assert.ok(s.length <= 48);
    assert.ok(!s.endsWith("-"));
    assert.match(s, /^[a-z0-9-]+$/);
  });

  test("uniqueFile never returns an existing name", () => {
    const dir = path.join(tmp, "unique");
    mkdirSync(dir, { recursive: true });
    const first = uniqueFile(dir, "001-x", ".png");
    writeFileSync(first, "a");
    const second = uniqueFile(dir, "001-x", ".png");
    assert.notEqual(first, second);
    assert.equal(path.basename(second), "001-x-2.png");
  });
});

describe("env", () => {
  test("a missing variable is an environment error with the marker", () => {
    assert.throws(() => requireEnv("PARATECH_E2E_CDP_URL", {}), (e: Error) => e.message.startsWith(EnvironmentMarker) && e.message.includes("PARATECH_E2E_CDP_URL"));
    assert.equal(requireEnv("A_B", { A_B: "v" }), "v");
    assert.ok(envError("нет Docker").message.startsWith(`${EnvironmentMarker} `));
  });
});

describe("clipboard stub", () => {
  // The stub runs in another vm realm: copy its array before comparing prototypes.
  const stored = (window: Record<string, unknown>): unknown => JSON.parse(JSON.stringify(window["__e2eClipboard"] ?? null));

  function run(navigator: Record<string, unknown>): { window: Record<string, unknown> } {
    const window: Record<string, unknown> = {};
    const ctx = vm.createContext({ window, navigator, Promise, Array, String, Object });
    vm.runInContext(clipboardStubSource, ctx);
    return { window };
  }

  test("replaces writeText and write, records the text and never calls the real implementation", async () => {
    let realCalls = 0;
    const nav = {
      clipboard: {
        writeText: (_t: string): Promise<void> => (realCalls++, Promise.resolve()),
        write: (_items: unknown[]): Promise<void> => (realCalls++, Promise.resolve()),
      },
    };
    const { window } = run(nav);
    await nav.clipboard.writeText("Привет");
    await nav.clipboard.write([
      { types: ["text/plain"], getType: () => Promise.resolve({ text: () => Promise.resolve("из write") }) },
      { types: ["image/png"], getType: () => Promise.reject(new Error("must not be read")) },
    ]);
    assert.equal(realCalls, 0);
    assert.deepEqual(stored(window), ["Привет", "из write"]);
  });

  test("does not throw when navigator.clipboard is missing and defines a recording stub; running twice is harmless", async () => {
    const nav: Record<string, unknown> = {};
    const { window } = run(nav);
    const clip = nav["clipboard"] as { writeText(t: string): Promise<void> };
    await clip.writeText("x");
    assert.deepEqual(stored(window), ["x"]);
    const ctx = vm.createContext({ window, navigator: nav, Promise, Array, String, Object });
    assert.doesNotThrow(() => vm.runInContext(clipboardStubSource, ctx));
    await clip.writeText("y");
    assert.deepEqual(stored(window), ["x", "y"]);
  });

  test("a frozen clipboard object falls back to a stub on navigator and never throws", async () => {
    const nav: Record<string, unknown> = { clipboard: Object.freeze({ writeText: () => Promise.resolve() }) };
    // navigator itself is configurable here, as in a page
    const { window } = run(nav);
    const clip = nav["clipboard"] as { writeText(t: string): Promise<void> };
    await clip.writeText("z");
    assert.deepEqual(stored(window), ["z"]);
  });
});

describe("llm helper against the real fake server", () => {
  let fake: FakeLLM;
  before(async () => {
    fake = await startFakeLLM();
  });
  after(async () => {
    await fake.stop();
  });

  const ask = async (text: string, extra: Record<string, unknown> = {}): Promise<Response> =>
    fetch(`${fake.url}/v1/messages`, {
      method: "POST",
      headers: { "content-type": "application/json", "x-api-key": "e2e-fake-header-SENTINEL" },
      body: JSON.stringify({ model: "m", max_tokens: 10, messages: [{ role: "user", content: text }], ...extra }),
    });

  test("script, requests and unmatched", async () => {
    const llm = createLlm(fake.url);
    await llm.script([
      { kind: "tools", match: "какие у меня записи", reply: [{ name: "list_recordings", input: {} }] },
      { kind: "plain", match: "", reply: "Готово." },
    ]);
    assert.equal((await ask("какие у меня записи")).status, 200);
    assert.equal((await ask("что-нибудь")).status, 200);

    const requests = await llm.requests();
    assert.equal(requests.length, 2);
    assert.deepEqual(
      requests.map((r) => [r.matched, r.rule]),
      [
        [true, 0],
        [true, 1],
      ],
    );
    assert.equal(requests[0]?.lastUser, "какие у меня записи");
    assert.deepEqual(await llm.unmatched(), []);
    await llm.assertNoUnmatched();
  });

  test("an unscripted call fails assertNoUnmatched with the call named, without secrets", async () => {
    const llm = createLlm(fake.url);
    await llm.script([{ kind: "plain", match: "известно", reply: "ok" }]);
    assert.equal((await ask("известно")).status, 200);
    assert.equal((await ask("никто не ждал этот вопрос про pthub_leakedtoken123 и экзамен")).status, 500);

    await assert.rejects(llm.assertNoUnmatched(), (e: unknown) => {
      assert.ok(e instanceof UnscriptedLlmError);
      assert.match(e.message, /^unscripted LLM call: no rule matches the last user message «никто не ждал этот вопрос/);
      assert.ok(!e.message.includes("pthub_leakedtoken123"), e.message);
      assert.ok(!e.message.includes("SENTINEL"), "request headers are never recorded");
      assert.equal(e.calls.length, 1);
      return true;
    });
  });

  test("the Hub retries an unmatched 500: the error counts the records, the guard reports them once", async () => {
    const llm = createLlm(fake.url);
    await llm.script([]);
    const guard = new UnscriptedGuard(llm);
    assert.equal(await guard.check(), undefined);

    for (let i = 0; i < 3; i++) {
      assert.equal((await ask("не по сценарию")).status, 500);
    }
    const first = await guard.check();
    assert.ok(first instanceof UnscriptedLlmError);
    assert.match(first.message, /«не по сценарию».*\(3 unanswered records, the Hub retries\)/);
    assert.equal(await guard.check(), undefined, "reported once");

    assert.equal((await ask("снова другое")).status, 500);
    const second = await guard.check();
    assert.match(second?.message ?? "", /«снова другое»/);
    // the plain assertion keeps failing as long as the recordings exist
    await assert.rejects(llm.assertNoUnmatched(), UnscriptedLlmError);
  });

  test("a new script clears the recordings", async () => {
    const llm = createLlm(fake.url);
    await ask("до сценария");
    await llm.script([]);
    assert.deepEqual(await llm.requests(), []);
    await llm.assertNoUnmatched();
  });

  test("an unknown endpoint and a stream request are unscripted calls too", async () => {
    const llm = createLlm(fake.url);
    await llm.script([{ kind: "plain", match: "", reply: "ok" }]);
    await fetch(`${fake.url}/v1/messages/count_tokens`, { method: "POST", body: "{}" });
    const err = await llm.assertNoUnmatched().then(
      () => undefined,
      (e: unknown) => e as Error,
    );
    assert.match(err?.message ?? "", /unscripted LLM call: unknown endpoint POST \/v1\/messages\/count_tokens/);

    await llm.script([{ kind: "plain", match: "", reply: "ok" }]);
    assert.equal((await ask("x", { stream: true })).status, 400);
    assert.match(describeCall((await llm.unmatched())[0]!), /stream requested/);
  });

  test("a rejected script reports the fake's reason", async () => {
    const llm = createLlm(fake.url);
    await assert.rejects(
      llm.script([{ kind: "plain", match: "", reply: "x", delay: 5 } as never]),
      /fake LLM refused the script: .*unknown field "delay"/,
    );
  });

  test("a fake that does not answer is an environment error", async () => {
    const llm = createLlm("http://127.0.0.1:1", 2000);
    await assert.rejects(llm.requests(), (e: Error) => e.message.startsWith(EnvironmentMarker));
  });
});
