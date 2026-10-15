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
  const reply = { recordingPromptsEnabled: true, processOnlyOnAcPower: false, notifyOnProcessingComplete: true };
  const { call, calls } = fakeCall(reply);
  const got = await settingsApi(call).get();
  assert.deepEqual(got, reply);
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.SettingsService.GetSettings"]]);
});

test("setters pass the value and return the stored toggles", async () => {
  const reply = { recordingPromptsEnabled: false, processOnlyOnAcPower: false, notifyOnProcessingComplete: false };
  const { call, calls } = fakeCall(reply);
  const api = settingsApi(call);
  assert.deepEqual(await api.setRecordingPromptsEnabled(false), reply);
  await api.setProcessOnlyOnAcPower(false);
  assert.deepEqual(await api.setNotifyOnProcessingComplete(false), reply);
  assert.deepEqual(calls, [
    [methods.setRecordingPromptsEnabled, false],
    [methods.setProcessOnlyOnAcPower, false],
    [methods.setNotifyOnProcessingComplete, false],
  ]);
});

test("the notification setter calls the bound Go method with the value", async () => {
  const { call, calls } = fakeCall({
    recordingPromptsEnabled: true,
    processOnlyOnAcPower: true,
    notifyOnProcessingComplete: true,
  });
  const got = await settingsApi(call).setNotifyOnProcessingComplete(true);
  assert.equal(got.notifyOnProcessingComplete, true);
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.SettingsService.SetNotifyOnProcessingComplete", true]]);
});

test("a malformed reply is an error", () => {
  for (const raw of [
    null,
    "x",
    {},
    { recordingPromptsEnabled: "yes", processOnlyOnAcPower: true, notifyOnProcessingComplete: true },
    // Without the notification toggle (an older core) the reply is malformed too.
    { recordingPromptsEnabled: true, processOnlyOnAcPower: true },
    { recordingPromptsEnabled: true, processOnlyOnAcPower: true, notifyOnProcessingComplete: "true" },
    { recordingPromptsEnabled: true, processOnlyOnAcPower: true, notifyOnProcessingComplete: null },
  ]) {
    assert.throws(() => parseSettings(raw), /settings/);
  }
});

test("extra fields of a reply are dropped", () => {
  const got = parseSettings({
    recordingPromptsEnabled: true,
    processOnlyOnAcPower: true,
    notifyOnProcessingComplete: false,
    token: "x",
  });
  assert.deepEqual(got, { recordingPromptsEnabled: true, processOnlyOnAcPower: true, notifyOnProcessingComplete: false });
});

test("a failing call rejects", async () => {
  const api = settingsApi(async () => {
    throw new Error("settings are unavailable");
  });
  await assert.rejects(api.get(), /unavailable/);
});
