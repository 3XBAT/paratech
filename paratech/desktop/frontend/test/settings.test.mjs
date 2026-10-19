// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { methods, parseSettings, settingsApi } from "../.test-build/bindings/settings.js";

// The load mode fields of every reply (TASK-084): without a model in use there are no estimates.
const load = { recognitionLoad: "gentle", loadEstimates: null, loadEstimatesModel: "" };

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

test("get calls the bound Go method and returns the toggles", async () => {
  const reply = { recordingPromptsEnabled: true, processOnlyOnAcPower: false, ...load };
  const { call, calls } = fakeCall(reply);
  const got = await settingsApi(call).get();
  assert.deepEqual(got, reply);
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.SettingsService.GetSettings"]]);
});

test("setters pass the value and return the stored toggles", async () => {
  const reply = { recordingPromptsEnabled: false, processOnlyOnAcPower: false, ...load };
  const { call, calls } = fakeCall(reply);
  const api = settingsApi(call);
  assert.deepEqual(await api.setRecordingPromptsEnabled(false), reply);
  assert.deepEqual(await api.setProcessOnlyOnAcPower(false), reply);
  assert.deepEqual(calls, [
    [methods.setRecordingPromptsEnabled, false],
    [methods.setProcessOnlyOnAcPower, false],
  ]);
});

test("a malformed reply is an error", () => {
  for (const raw of [
    null,
    "x",
    {},
    { recordingPromptsEnabled: "yes", processOnlyOnAcPower: true },
    { recordingPromptsEnabled: true },
    { recordingPromptsEnabled: true, processOnlyOnAcPower: null },
    { recordingPromptsEnabled: true, processOnlyOnAcPower: true },
  ]) {
    assert.throws(() => parseSettings(raw), /settings/);
  }
});

test("extra fields of a reply are dropped", () => {
  const got = parseSettings({
    recordingPromptsEnabled: true,
    processOnlyOnAcPower: true,
    ...load,
    // The setting removed in TASK-065: a reply that still carries it is not shown as a toggle.
    notifyOnProcessingComplete: false,
    token: "x",
  });
  assert.deepEqual(got, { recordingPromptsEnabled: true, processOnlyOnAcPower: true, ...load });
});

test("a failing call rejects", async () => {
  const api = settingsApi(async () => {
    throw new Error("settings are unavailable");
  });
  await assert.rejects(api.get(), /unavailable/);
});

// «Профиль студента» (TASK-094).
const profile = { fullName: "Иванов Иван", group: "ИВТ-21", university: "МГУ", faculty: "", city: "Москва" };

test("the profile is read and saved through the bound Go methods", async () => {
  const { call, calls } = fakeCall(profile);
  const api = settingsApi(call);
  assert.deepEqual(await api.getProfile(), profile);
  assert.deepEqual(await api.saveProfile({ ...profile, city: " Москва " }), profile);
  assert.deepEqual(calls, [[methods.getProfile], [methods.saveProfile, { ...profile, city: " Москва " }]]);
  assert.equal(methods.getProfile, "paratech/desktop/internal/input/wails.SettingsService.GetProfile");
  assert.equal(methods.saveProfile, "paratech/desktop/internal/input/wails.SettingsService.SaveProfile");
});

test("a malformed profile is an error; extra fields are dropped", async () => {
  const { parseProfile } = await import("../.test-build/bindings/settings.js");
  assert.deepEqual(parseProfile({ ...profile, token: "x" }), profile);
  for (const raw of [null, "x", {}, { ...profile, group: 1 }, { ...profile, city: undefined }]) {
    assert.throws(() => parseProfile(raw), /profile/);
  }
});

test("the profile form: labels, the change check and the core's error text", async () => {
  const { errorMessages, profileFields } = await import("../.test-build/bindings/settings.js");
  const { emptyProfile, maxProfileLength, profileChanged, profileErrorText, profileLabels } = await import(
    "../.test-build/profileView.js"
  );
  assert.deepEqual(profileFields.map((f) => profileLabels[f]), ["ФИО", "Группа", "Вуз", "Факультет или кафедра", "Город"]);
  assert.deepEqual(Object.keys(emptyProfile).sort(), [...profileFields].sort());
  assert.equal(maxProfileLength, 200);
  assert.equal(profileChanged(profile, { ...profile }), false);
  assert.equal(profileChanged({ ...profile, faculty: "ВМК" }, profile), true);
  assert.equal(profileChanged(emptyProfile, profile), true);
  assert.equal(profileErrorText(new Error(errorMessages.profileInvalid)), errorMessages.profileInvalid);
  assert.equal(profileErrorText(new Error("settings are unavailable")), "Не удалось сохранить профиль.");
  assert.equal(profileErrorText(null), "Не удалось сохранить профиль.");
});
