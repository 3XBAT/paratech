// TASK-380: delayMs, SSE streaming and cited answers of the fake Messages API.
import assert from "node:assert/strict";
import http from "node:http";
import { after, before, describe, test } from "node:test";

import { messageResponse, type FakeLLM, type Rule, startFakeLLM } from "../src/fakellm/index.js";
import { splitPieces } from "../src/fakellm/sse.js";

interface Event {
  event: string;
  data: Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any -- test reads of parsed JSON
  at: number;
}

/** Parses `event:`/`data:` frames separated by blank lines; at = ms since start when the chunk with the frame arrived. */
function parseSse(text: string, at = 0): Event[] {
  return text
    .split("\n\n")
    .filter((f) => f.trim() !== "")
    .map((frame) => {
      const lines = frame.split("\n");
      const event = lines.find((l) => l.startsWith("event: "))?.slice("event: ".length) ?? "";
      const data = JSON.parse(lines.find((l) => l.startsWith("data: "))?.slice("data: ".length) ?? "null") as Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any
      return { event, data, at };
    });
}

describe("fake server: delay, SSE and cited", () => {
  let fake: FakeLLM;
  before(async () => {
    fake = await startFakeLLM();
  });
  after(async () => {
    await fake.stop();
  });

  const body = (text: string, extra: Record<string, unknown> = {}) => ({
    model: "claude-sonnet-5-5",
    max_tokens: 64,
    messages: [{ role: "user", content: [{ type: "text", text }] }],
    ...extra,
  });
  const call = (b: unknown) => fetch(fake.url + "/v1/messages", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(b) });

  /** Reads the whole SSE body of a response, stamping each network chunk. */
  async function readEvents(res: Response, t0: number): Promise<Event[]> {
    const events: Event[] = [];
    const reader = res.body?.getReader();
    assert.ok(reader, "a body");
    const decoder = new TextDecoder();
    let pending = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      pending += decoder.decode(value, { stream: true });
      const end = pending.lastIndexOf("\n\n");
      if (end >= 0) {
        events.push(...parseSse(pending.slice(0, end + 2), Date.now() - t0));
        pending = pending.slice(end + 2);
      }
    }
    return events;
  }

  const names = (events: Event[]) => events.map((e) => e.event);

  /** Polls a state condition; a slow runner only makes it take longer, never fail early. */
  async function until(cond: () => boolean, what: string, deadlineMs = 5_000): Promise<void> {
    const end = Date.now() + deadlineMs;
    while (!cond()) {
      if (Date.now() > end) {
        throw new Error(`timed out after ${deadlineMs} ms waiting for: ${what}`);
      }
      await new Promise((r) => setTimeout(r, 10));
    }
  }

  /** Waits for a response promise and drops its body, whatever the outcome. */
  async function settled(p: Promise<Response>): Promise<void> {
    const res = await p.catch(() => undefined);
    await res?.body?.cancel().catch(() => undefined);
  }

  describe("delayMs", () => {
    test("JSON answer comes no earlier than delayMs; the rule is consumed and recorded before the wait", async () => {
      fake.setScript([{ kind: "plain", match: "", reply: "поздно", delayMs: 250 }]);
      const t0 = Date.now();
      const pending = call(body("вопрос"));
      try {
        await until(() => fake.requests().length === 1, "the request reaches the fake");
        assert.equal(fake.remaining(), 0, "consumed at once");
        assert.equal(fake.requests()[0]?.matched, true);

        const res = await pending;
        assert.ok(Date.now() - t0 >= 245, `answered after ${Date.now() - t0} ms`);
        assert.equal(res.status, 200);
        assert.equal(((await res.json()) as { content: Array<{ text: string }> }).content[0]?.text, "поздно");
      } finally {
        await settled(pending); // a failed assertion must not leave a request in flight for the next test
      }
    });

    test("the first byte of an SSE answer waits too", async () => {
      fake.setScript([{ kind: "plain", match: "", reply: "поздно", delayMs: 200, stream: { chunks: 2 } }]);
      const t0 = Date.now();
      const pending = call(body("вопрос", { stream: true })); // fetch resolves on the headers: the first byte
      try {
        const res = await pending;
        assert.ok(Date.now() - t0 >= 195, `headers after ${Date.now() - t0} ms`);
        assert.match(res.headers.get("content-type") ?? "", /^text\/event-stream/);
        await res.text();
      } finally {
        await settled(pending);
      }
    });

    test("a client that gives up during the wait gets nothing written and the fake keeps working", async () => {
      fake.setScript([
        { kind: "plain", match: "первый", reply: "x", delayMs: 300 },
        { kind: "plain", match: "второй", reply: "ok" },
      ]);
      const ac = new AbortController();
      const first = fetch(fake.url + "/v1/messages", { method: "POST", body: JSON.stringify(body("первый")), signal: ac.signal });
      try {
        await until(() => fake.requests().length === 1, "the first request reaches the fake"); // abort only once it is inside the wait
      } catch (err) {
        ac.abort();
        await first.catch(() => undefined);
        throw err;
      }
      ac.abort();
      await assert.rejects(first);
      // The fake's 300 ms timer started before the request was seen here, so a 350 ms timer started now fires after it.
      await new Promise((r) => setTimeout(r, 350));
      const second = (await (await call(body("второй"))).json()) as { content: Array<{ text: string }> };
      assert.equal(second.content[0]?.text, "ok");
    });
  });

  describe("SSE", () => {
    test("plain rule: the documented event order, text_delta pieces join to the reply", async () => {
      const reply = "Это ответ агента на вопрос студента.";
      fake.setScript([{ kind: "plain", match: "", reply, usage: { input_tokens: 40, output_tokens: 9, cache_read_input_tokens: 3 }, stream: { chunks: 4 } }]);
      const res = await call(body("вопрос", { stream: true }));
      assert.equal(res.status, 200);
      assert.match(res.headers.get("content-type") ?? "", /^text\/event-stream/);
      const events = await readEvents(res, Date.now());

      assert.deepEqual(names(events), [
        "message_start",
        "content_block_start",
        "content_block_delta",
        "content_block_delta",
        "content_block_delta",
        "content_block_delta",
        "content_block_stop",
        "message_delta",
        "message_stop",
      ]);
      for (const e of events) {
        assert.equal(e.data.type, e.event, "data.type equals the event name");
      }
      const start = events[0]?.data.message;
      assert.equal(start.type, "message");
      assert.equal(start.role, "assistant");
      assert.equal(start.model, "claude-sonnet-5-5");
      assert.deepEqual(start.content, []);
      assert.equal(start.stop_reason, null);
      assert.equal(start.usage.input_tokens, 40);
      assert.equal(start.usage.cache_read_input_tokens, 3);
      assert.equal(typeof start.usage.output_tokens, "number");

      assert.deepEqual(events[1]?.data, { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } });
      const deltas = events.filter((e) => e.event === "content_block_delta");
      assert.ok(deltas.every((e) => e.data.index === 0 && e.data.delta.type === "text_delta"));
      assert.equal(deltas.map((e) => e.data.delta.text).join(""), reply);
      assert.deepEqual(events[6]?.data, { type: "content_block_stop", index: 0 });

      const last = events[7]?.data;
      assert.equal(last?.delta.stop_reason, "end_turn");
      assert.deepEqual(last?.usage, { input_tokens: 40, output_tokens: 9, cache_creation_input_tokens: 0, cache_read_input_tokens: 3 });
      assert.deepEqual(events[8]?.data, { type: "message_stop" });
    });

    test("tools rule: a text block, then tool_use blocks whose partial_json pieces join to the input", async () => {
      const input = { text: "Привет! Вот ответ 😀 на «вопрос»", also: [1, 2, { k: "v" }] };
      fake.setScript([
        {
          kind: "tools",
          match: "",
          text: "Сейчас отвечу.",
          reply: [{ name: "reply", input, id: "toolu_fixed" }, { name: "list_recordings", input: {} }],
          stream: { chunks: 5 },
        },
      ]);
      const events = await readEvents(await call(body("вопрос", { stream: true })), Date.now());

      assert.equal(events[0]?.event, "message_start");
      const starts = events.filter((e) => e.event === "content_block_start");
      assert.deepEqual(
        starts.map((e) => [e.data.index, e.data.content_block.type]),
        [[0, "text"], [1, "tool_use"], [2, "tool_use"]],
      );
      assert.deepEqual(starts[1]?.data.content_block, { type: "tool_use", id: "toolu_fixed", name: "reply", input: {} });
      const seq = /^msg_e2e_(\d+)$/.exec(events[0]?.data.message.id)?.[1];
      assert.ok(seq, "message id msg_e2e_<seq>");
      assert.deepEqual(starts[2]?.data.content_block, { type: "tool_use", id: `toolu_e2e_${seq}_2`, name: "list_recordings", input: {} });

      // Every block: start, deltas of one index, stop, in this order.
      let open = -1;
      for (const e of events) {
        if (e.event === "content_block_start") {
          assert.equal(open, -1, "no block is open");
          open = e.data.index;
        } else if (e.event === "content_block_delta") {
          assert.equal(e.data.index, open);
        } else if (e.event === "content_block_stop") {
          assert.equal(e.data.index, open);
          open = -1;
        }
      }
      assert.equal(open, -1);

      const joined = (index: number) =>
        events
          .filter((e) => e.event === "content_block_delta" && e.data.index === index)
          .map((e) => {
            assert.equal(e.data.delta.type, index === 0 ? "text_delta" : "input_json_delta");
            return e.data.delta[index === 0 ? "text" : "partial_json"] as string;
          });
      assert.equal(joined(0).join(""), "Сейчас отвечу.");
      assert.deepEqual(JSON.parse(joined(1).join("")), input);
      assert.equal(joined(1).length, 5);
      assert.ok(joined(1).every((p) => p === "" || encodeURIComponent(p) !== ""), "no piece cuts a surrogate pair");
      assert.deepEqual(JSON.parse(joined(2).join("")), {});

      const delta = events.at(-2)?.data;
      assert.equal(events.at(-2)?.event, "message_delta");
      assert.equal(delta?.delta.stop_reason, "tool_use");
      assert.equal(events.at(-1)?.event, "message_stop");
    });

    test("splitPieces: at most n pieces, never empty, whole code points, empty text is one piece", () => {
      assert.deepEqual(splitPieces("", 8), [""]);
      assert.deepEqual(splitPieces("abc", 8), ["a", "b", "c"]);
      assert.deepEqual(splitPieces("abcdef", 4).join(""), "abcdef");
      assert.equal(splitPieces("abcdefghij", 3).length, 3);
      const pieces = splitPieces("😀😀😀😀", 3);
      assert.equal(pieces.join(""), "😀😀😀😀");
      assert.ok(pieces.every((p) => p !== "" && encodeURIComponent(p) !== ""));
    });

    test("chunkDelayMs waits between deltas", async () => {
      fake.setScript([{ kind: "plain", match: "", reply: "abcdefgh", stream: { chunks: 4, chunkDelayMs: 60 } }]);
      const t0 = Date.now();
      const events = await readEvents(await call(body("q", { stream: true })), t0);
      const deltas = events.filter((e) => e.event === "content_block_delta");
      assert.equal(deltas.length, 4);
      const span = (deltas.at(-1)?.at ?? 0) - (deltas[0]?.at ?? 0);
      assert.ok(span >= 3 * 60 - 15, `4 deltas arrived over ${span} ms`);
      assert.equal(events.at(-1)?.event, "message_stop");
    });

    test("delayMs and chunkDelayMs together: nothing earlier than the sum", async () => {
      fake.setScript([{ kind: "plain", match: "", reply: "abcd", delayMs: 100, stream: { chunks: 2, chunkDelayMs: 100 } }]);
      const t0 = Date.now();
      await readEvents(await call(body("q", { stream: true })), t0);
      assert.ok(Date.now() - t0 >= 195, `finished after ${Date.now() - t0} ms`);
    });
  });

  describe("cutAfter", () => {
    /** Raw client: the status, the bytes that arrived and whether the body ended cleanly. */
    const rawCall = (b: unknown) =>
      new Promise<{ status: number; text: string; complete: boolean }>((resolve, reject) => {
        const u = new URL(fake.url);
        const req = http.request({ host: u.hostname, port: u.port, path: "/v1/messages", method: "POST", headers: { "content-type": "application/json" } }, (res) => {
          let text = "";
          res.setEncoding("utf8").on("data", (c: string) => (text += c));
          res.on("error", () => undefined); // 'aborted' on a cut body
          res.on("close", () => resolve({ status: res.statusCode ?? 0, text, complete: res.complete }));
        });
        req.on("error", reject);
        req.end(JSON.stringify(b));
      });

    for (const [cutAfter, deltasSeen] of [[0, 0], [2, 2], [3, 3]] as const) {
      test(`cutAfter ${cutAfter}: ${deltasSeen} deltas arrive, no message_stop, the body does not end cleanly`, async () => {
        fake.setScript([{ kind: "plain", match: "", reply: "abcdef", stream: { chunks: 6, cutAfter } }]);
        const res = await rawCall(body("q", { stream: true }));
        assert.equal(res.status, 200);
        assert.equal(res.complete, false, "the response was aborted");
        const events = parseSse(res.text.slice(0, res.text.lastIndexOf("\n\n") + 2));
        assert.equal(events.filter((e) => e.event === "content_block_delta").length, deltasSeen);
        assert.ok(!res.text.includes("message_stop"));
        assert.ok(!res.text.includes("message_delta"));
        assert.equal(names(events)[0], "message_start");
      });
    }

    test("cutAfter beyond the number of deltas still never sends message_stop", async () => {
      fake.setScript([{ kind: "plain", match: "", reply: "ab", stream: { chunks: 2, cutAfter: 50 } }]);
      const res = await rawCall(body("q", { stream: true }));
      assert.equal(res.complete, false);
      assert.ok(!res.text.includes("message_stop"));
      assert.equal(parseSse(res.text.slice(0, res.text.lastIndexOf("\n\n") + 2)).filter((e) => e.event === "content_block_delta").length, 2);
    });

    test("fetch sees a terminated body", async () => {
      fake.setScript([{ kind: "plain", match: "", reply: "abcdef", stream: { chunks: 6, cutAfter: 1 } }]);
      const res = await call(body("q", { stream: true }));
      await assert.rejects(res.text());
    });

    test("the cut consumes the rule and the next request is served", async () => {
      fake.setScript([
        { kind: "plain", match: "", reply: "abcdef", stream: { cutAfter: 1 } },
        { kind: "plain", match: "", reply: "again" },
      ]);
      await rawCall(body("q", { stream: true }));
      assert.equal(fake.remaining(), 1);
      const second = (await (await call(body("q"))).json()) as { content: Array<{ text: string }> };
      assert.equal(second.content[0]?.text, "again");
    });
  });

  describe("JSON fallback", () => {
    const rules: Rule[] = [
      { kind: "tools", match: "", text: "Смотрю.", reply: [{ name: "open_screen", input: { screen: "lessons" } }], usage: { input_tokens: 7, output_tokens: 3 } },
      { kind: "plain", match: "", reply: "Ответ" },
    ];

    test("rules without the new fields give the same body whether or not stream:true is asked", async () => {
      for (const [i, rule] of rules.entries()) {
        for (const extra of [{}, { stream: true }, { stream: false }]) {
          fake.setScript([rule]);
          const res = await call(body("q", extra));
          assert.equal(res.status, 200);
          assert.match(res.headers.get("content-type") ?? "", /^application\/json/);
          assert.deepEqual(await res.json(), messageResponse(rule as Exclude<Rule, { kind: "cited" }>, fake.requests()[0]?.seq ?? -1, "claude-sonnet-5-5"), `rule ${i} ${JSON.stringify(extra)}`);
        }
      }
    });

    test("stream:true with a rule WITHOUT stream is the JSON 200; stream:false or absent with a stream rule is JSON too", async () => {
      fake.setScript([
        { kind: "plain", match: "a", reply: "A" },
        { kind: "plain", match: "b", reply: "B", stream: { chunks: 2 } },
        { kind: "plain", match: "c", reply: "C", stream: { chunks: 2 } },
      ]);
      for (const [text, extra, want] of [["a", { stream: true }, "A"], ["b", { stream: false }, "B"], ["c", {}, "C"]] as const) {
        const res = await call(body(text, extra));
        assert.equal(res.status, 200);
        assert.match(res.headers.get("content-type") ?? "", /^application\/json/, text);
        assert.equal(((await res.json()) as { content: Array<{ text: string }> }).content[0]?.text, want);
      }
    });
  });

  describe("cited", () => {
    const hubUserMessage = (fragments: Array<{ chunk_id: string; text: string }>) => JSON.stringify({ question: "Что было на паре?", fragments });
    const fragmentText = "Сегодня разбирали  теорему Ферма\nи её доказательство, затем перешли к следствиям.";

    test("cites the first fragment with a verbatim quote of its first 6 words and the scripted claim", async () => {
      fake.setScript([{ kind: "cited", match: "Что было на паре", claim: "Разбирали теорему Ферма.", usage: { input_tokens: 500, output_tokens: 60 } }]);
      const res = await call(
        body(hubUserMessage([{ chunk_id: "lesson-1:c1", text: fragmentText }, { chunk_id: "lesson-1:c2", text: "Другой фрагмент." }])),
      );
      assert.equal(res.status, 200);
      const msg = (await res.json()) as { content: Array<{ type: string; text: string }>; stop_reason: string; usage: { input_tokens: number } };
      assert.equal(msg.stop_reason, "end_turn");
      assert.equal(msg.usage.input_tokens, 500);
      assert.equal(msg.content.length, 1);
      const answer = JSON.parse(msg.content[0]?.text ?? "") as { claims: Array<{ text: string; cites: Array<{ chunk_id: string; quote: string }> }>; missing: string[] };
      assert.deepEqual(Object.keys(answer), ["claims", "missing"]);
      assert.deepEqual(answer.missing, []);
      assert.equal(answer.claims.length, 1);
      assert.equal(answer.claims[0]?.text, "Разбирали теорему Ферма.");
      assert.equal(answer.claims[0]?.cites.length, 1);
      const cite = answer.claims[0]?.cites[0];
      assert.equal(cite?.chunk_id, "lesson-1:c1");
      assert.equal(cite?.quote, "Сегодня разбирали  теорему Ферма\nи её");
      assert.ok(fragmentText.includes(cite?.quote ?? "?"), "a verbatim substring");
      assert.equal(fake.remaining(), 0);
    });

    test("words and missing are honoured; words beyond the text quote the whole text", async () => {
      fake.setScript([
        { kind: "cited", match: "", claim: "c", words: 2, missing: ["Домашнее задание не обсуждали"] },
        { kind: "cited", match: "", claim: "c", words: 30 },
      ]);
      const first = JSON.parse(((await (await call(body(hubUserMessage([{ chunk_id: "a", text: fragmentText }])))).json()) as { content: Array<{ text: string }> }).content[0]?.text ?? "");
      assert.equal(first.claims[0].cites[0].quote, "Сегодня разбирали");
      assert.deepEqual(first.missing, ["Домашнее задание не обсуждали"]);

      const short = "  Только три слова  ";
      const second = JSON.parse(((await (await call(body(hubUserMessage([{ chunk_id: "a", text: short }])))).json()) as { content: Array<{ text: string }> }).content[0]?.text ?? "");
      assert.equal(second.claims[0].cites[0].quote, "Только три слова");
    });

    for (const [name, message] of [
      ["no fragments key", JSON.stringify({ question: "q" })],
      ["empty fragments", JSON.stringify({ question: "q", fragments: [] })],
      ["not JSON", "просто текст"],
      ["fragment without text", JSON.stringify({ question: "q", fragments: [{ chunk_id: "a" }] })],
      ["blank text", JSON.stringify({ question: "q", fragments: [{ chunk_id: "a", text: "   " }] })],
    ] as const) {
      test(`no usable fragment (${name}): HTTP 500, listed as unmatched, the rule stays`, async () => {
        fake.setScript([{ kind: "cited", match: "", claim: "c" }]);
        const res = await call(body(message));
        assert.equal(res.status, 500);
        assert.equal(((await res.json()) as { error: { type: string } }).error.type, "api_error");
        assert.equal(fake.remaining(), 1);
        assert.deepEqual(
          fake.unmatched().map((r) => [r.matched, r.reason]),
          [[false, "cited: no fragment in the request"]],
        );
        // The same rule still answers a request that has a fragment.
        const ok = await call(body(hubUserMessage([{ chunk_id: "a", text: "Один два три" }])));
        assert.equal(ok.status, 200);
        assert.equal(fake.remaining(), 0);
      });
    }

    test("a cited rule honours delayMs", async () => {
      fake.setScript([{ kind: "cited", match: "", claim: "c", delayMs: 150 }]);
      const t0 = Date.now();
      const res = await call(body(hubUserMessage([{ chunk_id: "a", text: "Один два три" }])));
      assert.ok(Date.now() - t0 >= 145, `answered after ${Date.now() - t0} ms`);
      assert.equal(res.status, 200);
    });
  });
});
