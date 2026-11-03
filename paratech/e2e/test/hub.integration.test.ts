// The real Hub (built from paratech/hub) on the Postgres of the E2E compose project, in front of the fake Messages
// API. Skipped when neither Docker nor HUB_TEST_DATABASE_URL is available, or with PARATECH_E2E_UNIT_ONLY=1 (Linux CI
// only type-checks and unit tests this package, ADR-0035 section 7).
import assert from "node:assert/strict";
import { test } from "node:test";

import { startFakeLLM } from "../src/fakellm/index.js";
import { dockerAvailable, startHub } from "../src/hub/index.js";

const unitOnly = process.env.PARATECH_E2E_UNIT_ONLY === "1";
const canRun = !unitOnly && (Boolean(process.env.HUB_TEST_DATABASE_URL) || (await dockerAvailable()));
const skipReason = unitOnly ? "PARATECH_E2E_UNIT_ONLY=1" : "Docker не запущен и HUB_TEST_DATABASE_URL не задан";

const connectJSON = (url: string, procedure: string, body: unknown, apiKey?: string) =>
  fetch(`${url}/${procedure}`, {
    method: "POST",
    headers: { "content-type": "application/json", ...(apiKey ? { authorization: `Bearer ${apiKey}` } : {}) },
    body: JSON.stringify(body),
  });

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
      assert.ok(!hub.logs().includes(hub.apiKey));
    } finally {
      await hub.stop();
      await fake.stop();
    }
    await assert.rejects(fetch(`${hub.url}/readyz`), "the Hub is gone after stop");
    t.diagnostic(`total ${Date.now() - started} ms`);
  },
);
