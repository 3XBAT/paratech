// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { methods, parseSettings, settingsApi } from "../.test-build/bindings/settings.js";

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

test("get calls the bound Go method and returns the toggles", async () => {
  const { call, calls } = fakeCall({ recordingPromptsEnabled: true, processOnlyOnAcPower: false });
  const got = await settingsApi(call).get();
  assert.deepEqual(got, { recordingPromptsEnabled: true, processOnlyOnAcPower: false });
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.SettingsService.GetSettings"]]);
});

test("setters pass the value and return the stored toggles", async () => {
  const { call, calls } = fakeCall({ recordingPromptsEnabled: false, processOnlyOnAcPower: false });
  const api = settingsApi(call);
  assert.deepEqual(await api.setRecordingPromptsEnabled(false), { recordingPromptsEnabled: false, processOnlyOnAcPower: false });
  await api.setProcessOnlyOnAcPower(false);
  assert.deepEqual(calls, [
    [methods.setRecordingPromptsEnabled, false],
    [methods.setProcessOnlyOnAcPower, false],
  ]);
});

test("a malformed reply is an error", () => {
  for (const raw of [null, "x", {}, { recordingPromptsEnabled: "yes", processOnlyOnAcPower: true }]) {
    assert.throws(() => parseSettings(raw), /settings/);
  }
});

test("extra fields of a reply are dropped", () => {
  const got = parseSettings({ recordingPromptsEnabled: true, processOnlyOnAcPower: true, token: "x" });
  assert.deepEqual(got, { recordingPromptsEnabled: true, processOnlyOnAcPower: true });
});

test("a failing call rejects", async () => {
  const api = settingsApi(async () => {
    throw new Error("settings are unavailable");
  });
  await assert.rejects(api.get(), /unavailable/);
});
