import assert from "node:assert/strict";
import { after, before, describe, test } from "node:test";

import {
  findRule,
  lastUserContent,
  messageResponse,
  parseScript,
  type RecordedRequest,
  type Rule,
  ScriptError,
  startFakeLLM,
  type FakeLLM,
} from "../src/fakellm/index.js";

const FakeProviderKey = "e2e-fake-provider-key-SENTINEL";

describe("script validation", () => {
  test("accepts tools and plain rules", () => {
    const rules = parseScript({
      rules: [
        { kind: "tools", match: "какие у меня записи", reply: [{ name: "list_recordings", input: { limit: 5 } }], usage: { input_tokens: 900, output_tokens: 60 } },
        { kind: "plain", match: "", reply: "Готово." },
      ],
    });
    assert.equal(rules.length, 2);
    assert.deepEqual(rules[1], { kind: "plain", match: "", reply: "Готово." });
  });

  const bad: Array<[string, unknown]> = [
    ["not an object", []],
    ["no rules", {}],
    ["unknown top field", { rules: [], extra: 1 }],
    ["unknown kind", { rules: [{ kind: "stream", match: "", reply: "x" }] }],
    ["unknown rule field", { rules: [{ kind: "plain", match: "", reply: "x", delay: 5 }] }],
    ["match not a string", { rules: [{ kind: "plain", match: 3, reply: "x" }] }],
    ["plain reply not a string", { rules: [{ kind: "plain", match: "", reply: ["x"] }] }],
    ["empty tool calls", { rules: [{ kind: "tools", match: "", reply: [] }] }],
    ["bad tool name", { rules: [{ kind: "tools", match: "", reply: [{ name: "a b", input: {} }] }] }],
    ["tool input not an object", { rules: [{ kind: "tools", match: "", reply: [{ name: "a", input: "{}" }] }] }],
    ["negative usage", { rules: [{ kind: "plain", match: "", reply: "x", usage: { input_tokens: -1, output_tokens: 1 } }] }],
    ["fractional usage", { rules: [{ kind: "plain", match: "", reply: "x", usage: { input_tokens: 1.5, output_tokens: 1 } }] }],
    ["usage without output", { rules: [{ kind: "plain", match: "", reply: "x", usage: { input_tokens: 1 } }] }],
  ];
  for (const [name, body] of bad) {
    test(`rejects: ${name}`, () => assert.throws(() => parseScript(body), ScriptError));
  }
});

describe("matching", () => {
  test("last user content takes text and tool_result text of the last user message", () => {
    const body = {
      messages: [
        { role: "user", content: [{ type: "text", text: "первый вопрос" }] },
        { role: "assistant", content: [{ type: "tool_use", id: "t1", name: "list", input: {} }] },
        {
          role: "user",
          content: [
            { type: "tool_result", tool_use_id: "t1", content: "ошибка: нет доступа к файлу" },
            { type: "tool_result", tool_use_id: "t2", content: [{ type: "text", text: "Лекция 3" }] },
            { type: "text", text: "продолжай" },
          ],
        },
      ],
    };
    assert.equal(lastUserContent(body), "ошибка: нет доступа к файлу\nЛекция 3\nпродолжай");
    assert.equal(lastUserContent({ messages: [{ role: "user", content: "строкой" }] }), "строкой");
    assert.equal(lastUserContent({}), "");
  });

  test("the first remaining matching rule wins, in script order", () => {
    const rules: Rule[] = [
      { kind: "plain", match: "конспект", reply: "a" },
      { kind: "plain", match: "лекц", reply: "b" },
      { kind: "plain", match: "лекц", reply: "c" },
    ];
    assert.equal(findRule(rules, "текст лекции"), 1);
    assert.equal(findRule(rules, "конспект лекции"), 0);
    assert.equal(findRule(rules, "другое"), -1);
  });
});

describe("response shape", () => {
  test("tools rule: text, tool_use blocks with ids, stop_reason tool_use, usage", () => {
    const r = messageResponse(
      { kind: "tools", match: "", text: "Смотрю.", reply: [{ name: "open_screen", input: { screen: "lessons" } }, { name: "x", input: {}, id: "toolu_fixed" }], usage: { input_tokens: 7, output_tokens: 3, cache_read_input_tokens: 2 } },
      4,
      "claude-haiku-5-5",
    );
    assert.deepEqual(r, {
      id: "msg_e2e_4",
      type: "message",
      role: "assistant",
      model: "claude-haiku-5-5",
      content: [
        { type: "text", text: "Смотрю." },
        { type: "tool_use", id: "toolu_e2e_4_1", name: "open_screen", input: { screen: "lessons" } },
        { type: "tool_use", id: "toolu_fixed", name: "x", input: {} },
      ],
      stop_reason: "tool_use",
      stop_sequence: null,
      usage: { input_tokens: 7, output_tokens: 3, cache_creation_input_tokens: 0, cache_read_input_tokens: 2 },
    });
  });

  test("plain rule: one text block, end_turn, default usage", () => {
    const r = messageResponse({ kind: "plain", match: "", reply: "Ответ" }, 1, undefined);
    assert.deepEqual(r.content, [{ type: "text", text: "Ответ" }]);
    assert.equal(r.stop_reason, "end_turn");
    assert.deepEqual(r.usage, { input_tokens: 100, output_tokens: 20, cache_creation_input_tokens: 0, cache_read_input_tokens: 0 });
  });
});

describe("fake server", () => {
  let fake: FakeLLM;
  before(async () => {
    fake = await startFakeLLM({ maxMessageBytes: 64 * 1024 });
  });
  after(async () => {
    await fake.stop();
  });

  const call = (body: unknown, path = "/v1/messages") =>
    fetch(fake.url + path, {
      method: "POST",
      headers: { "content-type": "application/json", "x-api-key": FakeProviderKey, authorization: `Bearer ${FakeProviderKey}`, "anthropic-version": "2023-06-01" },
      body: typeof body === "string" ? body : JSON.stringify(body),
    });
  const userTurn = (text: string, extra: Record<string, unknown> = {}) => ({
    model: "claude-sonnet-5-5",
    max_tokens: 64,
    messages: [{ role: "user", content: [{ type: "text", text }] }],
    ...extra,
  });
  const postScript = (body: unknown) =>
    fetch(fake.url + "/__e2e/script", { method: "POST", headers: { "content-type": "application/json" }, body: typeof body === "string" ? body : JSON.stringify(body) });

  test("listens on loopback only", () => {
    assert.match(fake.url, /^http:\/\/127\.0\.0\.1:\d+$/);
  });

  test("rules are consumed in order, unmatched calls get 500 api_error and are listed", async () => {
    const res = await postScript({
      rules: [
        { kind: "tools", match: "записи", reply: [{ name: "list_recordings", input: {} }], usage: { input_tokens: 900, output_tokens: 60 } },
        { kind: "plain", match: "записи", reply: "Первая" },
        { kind: "plain", match: "записи", reply: "Вторая" },
      ],
    });
    assert.equal(res.status, 200);
    assert.deepEqual(await res.json(), { rules: 3 });

    const first = await call(userTurn("какие у меня записи"));
    assert.equal(first.status, 200);
    assert.match(first.headers.get("content-type") ?? "", /^application\/json/);
    const firstBody = (await first.json()) as Record<string, unknown>;
    assert.equal(firstBody.stop_reason, "tool_use");
    assert.equal(firstBody.model, "claude-sonnet-5-5");

    const second = (await (await call(userTurn("записи?"))).json()) as { content: Array<{ text: string }> };
    assert.equal(second.content[0]?.text, "Первая");
    // A gateway prefix in the base URL ends up before /v1/messages.
    const third = (await (await call(userTurn("записи!"), "/anthropic/v1/messages")).json()) as { content: Array<{ text: string }> };
    assert.equal(third.content[0]?.text, "Вторая");
    assert.equal(fake.remaining(), 0);

    const miss = await call(userTurn("записи ещё раз"));
    assert.equal(miss.status, 500);
    const missBody = (await miss.json()) as { type: string; error: { type: string } };
    assert.equal(missBody.type, "error");
    assert.equal(missBody.error.type, "api_error");

    const all = (await (await fetch(fake.url + "/__e2e/requests")).json()) as RecordedRequest[];
    assert.deepEqual(all.map((r) => [r.matched, r.rule]), [[true, 0], [true, 1], [true, 2], [false, undefined]]);
    const unmatched = (await (await fetch(fake.url + "/__e2e/unmatched")).json()) as RecordedRequest[];
    assert.equal(unmatched.length, 1);
    assert.equal(unmatched[0]?.lastUser, "записи ещё раз");
    assert.equal(unmatched[0]?.reason, "unscripted LLM call");
    assert.deepEqual(fake.unmatched(), unmatched);

    // Recordings hold bodies only: no header, so no provider key.
    assert.ok(!JSON.stringify(all).includes(FakeProviderKey));
    assert.ok(!JSON.stringify(all).toLowerCase().includes("x-api-key"));
  });

  test("a new script clears the recordings", async () => {
    fake.setScript([{ kind: "plain", match: "", reply: "ok" }]);
    assert.deepEqual(fake.requests(), []);
    assert.equal((await call(userTurn("что угодно"))).status, 200);
    assert.equal(fake.requests().length, 1);
  });

  test("malformed scripts are refused with 400 and keep the old script", async () => {
    fake.setScript([{ kind: "plain", match: "", reply: "kept" }]);
    for (const body of ["not json", { rules: [{ kind: "plain", match: "", reply: 1 }] }, { rules: "x" }]) {
      const res = await postScript(body);
      assert.equal(res.status, 400);
      assert.equal(typeof ((await res.json()) as { error: unknown }).error, "string");
    }
    assert.equal(fake.remaining(), 1);
    assert.equal((await fetch(fake.url + "/__e2e/script")).status, 405);
    assert.throws(() => fake.setScript([{ kind: "nope" } as unknown as Rule]), ScriptError);
  });

  test("an oversized script is refused with 413", async () => {
    const res = await postScript({ rules: [{ kind: "plain", match: "", reply: "x".repeat(1100 * 1024) }] });
    assert.equal(res.status, 413);
  });

  test("streaming, non-JSON and oversized provider calls are refused and recorded as unmatched", async () => {
    fake.setScript([{ kind: "plain", match: "", reply: "never" }]);
    assert.equal((await call(userTurn("x", { stream: true }))).status, 400);
    assert.equal((await call("{oops")).status, 400);
    assert.equal((await call(userTurn("y".repeat(70 * 1024)))).status, 413);
    assert.deepEqual(
      fake.unmatched().map((r) => r.reason),
      ["stream requested", "malformed JSON", "body too large"],
    );
    assert.equal(fake.remaining(), 1);
  });

  test("unknown paths are 404", async () => {
    assert.equal((await fetch(fake.url + "/v1/other", { method: "POST", body: "{}" })).status, 404);
  });
});
