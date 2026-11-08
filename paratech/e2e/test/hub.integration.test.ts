// The real Hub (built from paratech/hub) on the Postgres of the E2E compose project, in front of the fake Messages
// API. Skipped when Docker is not available, or with PARATECH_E2E_UNIT_ONLY=1 (Linux CI only type-checks and unit
// tests this package, ADR-0035 section 7).
import assert from "node:assert/strict";
import { test } from "node:test";
import { inspect } from "node:util";

import { EnvHubDatabaseUrl } from "../src/contract.js";
import { startFakeLLM } from "../src/fakellm/index.js";
import { dockerAvailable, startHub } from "../src/hub/index.js";
import { keyTools } from "../src/hub/keys.js";

const unitOnly = process.env.PARATECH_E2E_UNIT_ONLY === "1";
const canRun = !unitOnly && (await dockerAvailable());
const skipReason = unitOnly ? "PARATECH_E2E_UNIT_ONLY=1" : "Docker не запущен";

const connectJSON = (url: string, procedure: string, body: unknown, apiKey?: string) =>
  fetch(`${url}/${procedure}`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(apiKey ? { authorization: `Bearer ${apiKey}` } : {}) },
    body: JSON.stringify(body),
  });

interface StreamEnvelope {
  flags: number;
  json: Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any -- parsed Connect JSON
}

/** A Connect server-streaming call (application/connect+json): one enveloped request, a list of enveloped messages back. */
async function connectStream(url: string, procedure: string, body: unknown, apiKey: string): Promise<{ status: number; messages: StreamEnvelope[] }> {
  const payload = Buffer.from(JSON.stringify(body), "utf8");
  const header = Buffer.alloc(5);
  header.writeUInt32BE(payload.length, 1);
  const res = await fetch(`${url}/${procedure}`, {
    method: "POST",
    headers: { "content-type": "application/connect+json", authorization: `Bearer ${apiKey}` },
    body: Buffer.concat([header, payload]),
  });
  const bytes = Buffer.from(await res.arrayBuffer());
  const messages: StreamEnvelope[] = [];
  for (let at = 0; at + 5 <= bytes.length; ) {
    const size = bytes.readUInt32BE(at + 1);
    messages.push({ flags: bytes[at] ?? 0, json: JSON.parse(bytes.subarray(at + 5, at + 5 + size).toString("utf8") || "{}") });
    at += 5 + size;
  }
  return { status: res.status, messages };
}

test(
  "a throwaway Hub answers GetRemaining with its key and calls the scripted fake",
  { skip: canRun ? false : skipReason, timeout: 900_000 },
  async (t) => {
    const fake = await startFakeLLM();
    fake.setScript([{ kind: "plain", match: "e2e-ping", reply: "Конспект пары", usage: { input_tokens: 100, output_tokens: 20 } }]);
    const started = Date.now();
    const hub = await startHub({ mode: "scripted", fakeUrl: fake.url, quotaLimit: 400_000 });
    t.diagnostic(`hub ready in ${Date.now() - started} ms at ${hub.url}, schema ${hub.schema}`);
    try {
      const anonymous = await connectJSON(hub.url, "paratech.hub.v1.QuotaService/GetRemaining", {});
      assert.equal(anonymous.status, 401);

      const remaining = await connectJSON(hub.url, "paratech.hub.v1.QuotaService/GetRemaining", {}, hub.apiKey);
      assert.equal(remaining.status, 200, await remaining.clone().text());
      const quota = ((await remaining.json()) as { quota: { limit: { value: string }; remaining: { value: string } } }).quota;
      assert.equal(quota.limit.value, "400000");
      assert.equal(quota.remaining.value, "400000");

      // One unary provider call through the fake: the reply comes back and the usage is charged.
      const complete = await connectJSON(
        hub.url,
        "paratech.hub.v1.GatewayService/Complete",
        {
          spec: {
            idempotencyKey: "e2e-ping-1",
            operationType: "OPERATION_TYPE_SUMMARY",
            modelAlias: "default",
            instructions: "Сделай конспект пары.",
            untrustedData: "e2e-ping: текст пары",
            maxOutputTokens: 64,
          },
        },
        hub.apiKey,
      );
      assert.equal(complete.status, 200, await complete.clone().text());
      const result = ((await complete.json()) as {
        result: { output: string; usage: { inputTokens: string; outputTokens: string }; quota: { remaining: { value: string } } };
      }).result;
      assert.equal(result.output, "Конспект пары");
      assert.deepEqual(result.usage, { inputTokens: "100", outputTokens: "20" });
      assert.equal(result.quota.remaining.value, String(400_000 - 120));

      const calls = fake.requests();
      assert.equal(calls.length, 1);
      assert.equal(calls[0]?.matched, true);
      const body = calls[0]?.body as { stream?: boolean; model?: string };
      assert.equal(body.stream, undefined, "SCRIPTED mode calls the provider unary");
      assert.deepEqual(fake.unmatched(), []);
      // The client's Hub key must not travel to the provider (recordings hold the bodies the Hub sent).
      assert.ok(!JSON.stringify(calls).includes(hub.apiKey), "the run's API key reached the provider");
      // The capture is scrubbed, so the key itself can never show up there; the marker shows the Hub printed a secret
      // of the run (API key, seal keys, database password) at all, which it must not.
      assert.ok(hub.logs().length > 0, "the Hub output is captured");
      assert.ok(!hub.logs().includes("(redacted)"), `the Hub logged a secret of the run:\n${hub.logs()}`);

      // TASK-377: a second key is issued and revoked through src/hub/keys.ts; the key of the run stays valid
      const tools = keyTools({ env: { [EnvHubDatabaseUrl]: hub.databaseUrl } });
      const second = await tools.issueKey("e2e-second");
      assert.match(second.key, /^pthub_/);
      assert.notEqual(second.key, hub.apiKey);
      assert.equal((await connectJSON(hub.url, "paratech.hub.v1.QuotaService/GetRemaining", {}, second.key)).status, 200, "the second key works");
      const before = await tools.listKeys();
      assert.deepEqual(before.map((k) => k.status).sort(), ["active", "active"]);
      assert.ok(before.some((k) => k.id === second.id && k.label === "e2e-second"));

      await tools.revokeKey(second.id);
      await tools.revokeKey(second.id); // repeating is not an error
      const rejected = await connectJSON(hub.url, "paratech.hub.v1.QuotaService/GetRemaining", {}, second.key);
      assert.equal(rejected.status, 401, "a revoked key is refused at once");
      assert.equal((await connectJSON(hub.url, "paratech.hub.v1.QuotaService/GetRemaining", {}, hub.apiKey)).status, 200, "the key of the run still works");
      const after = await tools.listKeys();
      assert.equal(after.find((k) => k.id === second.id)?.status, "revoked");
      assert.equal(after.filter((k) => k.status === "active").length, 1);
      assert.ok(!JSON.stringify(hub).includes("postgres://") && !inspect(hub).includes("postgres://"), "the database URL is a secret of the run");

      // TASK-380: the Hub runs with HUB_PROVIDER_STREAM=true. Only the ANSWER operation is streamed (CompleteStream),
      // with the server prompt: an SSE rule, a `cited` rule (no `stream`: the JSON fallback) and a cut stream.
      const fragment = "Сегодня разбирали теорему Ферма и её доказательство";
      const answerSpec = (key: string, question: string) => ({
        spec: {
          idempotencyKey: key,
          operationType: "OPERATION_TYPE_ANSWER",
          untrustedData: JSON.stringify({ question, fragments: [{ chunk_id: "lesson-1-c1", text: fragment }] }),
          maxOutputTokens: 256,
        },
      });
      const answerJSON = JSON.stringify({ claims: [{ text: "Разбирали теорему Ферма.", cites: [{ chunk_id: "lesson-1-c1", quote: "разбирали теорему Ферма" }] }], missing: [] });
      const stream = (key: string, question: string) => connectStream(hub.url, "paratech.hub.v1.GatewayService/CompleteStream", answerSpec(key, question), hub.apiKey);
      const resultOf = (r: { messages: StreamEnvelope[] }) => r.messages.find((m) => m.flags === 0 && m.json.result)?.json.result;

      fake.setScript([{ kind: "plain", match: "e2e-sse", reply: answerJSON, usage: { input_tokens: 50, output_tokens: 12 }, stream: { chunks: 5 } }]);
      const sse = await stream("e2e-sse-1", "e2e-sse: что было на паре?");
      assert.equal(sse.status, 200);
      const deltas = sse.messages.filter((m) => m.flags === 0 && typeof m.json.delta === "string").map((m) => m.json.delta as string);
      assert.ok(deltas.length >= 2, `the Hub passed ${deltas.length} deltas on: ${JSON.stringify(sse)}\n${hub.logs().slice(-1500)}`);
      assert.equal(deltas.join(""), answerJSON);
      assert.deepEqual(JSON.parse(resultOf(sse)?.output), JSON.parse(answerJSON));
      assert.deepEqual(resultOf(sse)?.usage, { inputTokens: "50", outputTokens: "12" });
      assert.equal((fake.requests()[0]?.body as { stream?: boolean }).stream, true, "CompleteStream asks the provider for SSE");

      // `cited` builds the answer from the request: the Hub keeps the claim, so the chunk id and the quote were valid.
      fake.setScript([{ kind: "cited", match: "e2e-cited", claim: "Разбирали теорему Ферма.", words: 3, missing: ["Про домашнее задание не сказано"] }]);
      const cited = await stream("e2e-cited-1", "e2e-cited: что было на паре?");
      assert.equal(cited.status, 200);
      const citedOutput = JSON.parse(resultOf(cited)?.output) as { claims: Array<{ text: string; cites: Array<{ chunk_id: string; quote: string }> }>; missing: string[] };
      assert.deepEqual(citedOutput.claims, [{ text: "Разбирали теорему Ферма.", cites: [{ chunk_id: "lesson-1-c1", quote: "Сегодня разбирали теорему" }] }]);
      assert.deepEqual(citedOutput.missing, ["Про домашнее задание не сказано"]);
      assert.equal((fake.requests()[0]?.body as { stream?: boolean }).stream, true, "the JSON 200 answers a stream:true call");
      assert.deepEqual(fake.unmatched(), []);

      fake.setScript([{ kind: "plain", match: "e2e-cut", reply: answerJSON, stream: { chunks: 6, cutAfter: 2 } }]);
      const cut = await stream("e2e-cut-1", "e2e-cut: что было на паре?");
      t.diagnostic(`cut stream: ${JSON.stringify(cut.messages)}`);
      assert.ok(!resultOf(cut), "a cut stream has no result");
      assert.ok(cut.messages.some((m) => m.flags === 2 && m.json.error), "a cut stream ends with an error");
      assert.equal(fake.remaining(), 0);
    } finally {
      await hub.stop();
      await fake.stop();
    }
    await assert.rejects(fetch(`${hub.url}/readyz`), "the Hub is gone after stop");
    t.diagnostic(`total ${Date.now() - started} ms`);
  },
);
