// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { methods, parseSettings, settingsApi } from "../.test-build/bindings/settings.js";

// The load mode fields of every reply (TASK-084): without a model in use there are no estimates; the free-work card
// setting (TASK-188) and the photo recognition (TASK-294, off on a new install) come with them.
const load = {
  recognitionLoad: "gentle",
  loadEstimates: null,
  loadEstimatesModel: "",
  freeWorkNotice: true,
  noteRecognition: "off",
  noteRecognitionLanguage: "unknown",
};

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

// TASK-188: «Предупреждать перед свободной работой» is read with the toggles and set by its bound method.
test("the free-work card setting is read and set", async () => {
  const reply = { recordingPromptsEnabled: true, processOnlyOnAcPower: true, ...load, freeWorkNotice: false };
  const { call, calls } = fakeCall(reply);
  const api = settingsApi(call);
  assert.equal((await api.setFreeWorkNotice(false)).freeWorkNotice, false);
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.SettingsService.SetFreeWorkNotice", false]]);
  for (const bad of [{ ...reply, freeWorkNotice: undefined }, { ...reply, freeWorkNotice: "no" }]) {
    assert.throws(() => parseSettings(bad), /freeWorkNotice/);
  }
});

// TASK-294: «ИИ распознаёт фото конспектов» is read with the toggles and set by its bound method.
test("the photo recognition mode is read strictly and set by its bound method", async () => {
  const reply = { recordingPromptsEnabled: true, processOnlyOnAcPower: true, ...load, noteRecognition: "local", noteRecognitionLanguage: "missing" };
  const { call, calls } = fakeCall(reply);
  const api = settingsApi(call);
  const got = await api.setNoteRecognition("local");
  assert.equal(got.noteRecognition, "local");
  assert.equal(got.noteRecognitionLanguage, "missing");
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.SettingsService.SetNoteRecognition", "local"]]);
  assert.equal(methods.setNoteRecognition, calls[0][0]);
  for (const ok of ["off", "local"]) {
    assert.equal(parseSettings({ ...reply, noteRecognition: ok }).noteRecognition, ok);
  }
  for (const ok of ["available", "missing", "unknown"]) {
    assert.equal(parseSettings({ ...reply, noteRecognitionLanguage: ok }).noteRecognitionLanguage, ok);
  }
  for (const bad of [undefined, null, true, "", "cloud", "LOCAL"]) {
    assert.throws(() => parseSettings({ ...reply, noteRecognition: bad }), /noteRecognition/);
  }
  for (const bad of [undefined, null, false, "", "present", "Missing"]) {
    assert.throws(() => parseSettings({ ...reply, noteRecognitionLanguage: bad }), /noteRecognitionLanguage/);
  }
});

test("the photo recognition switch: off on a new install, sends local or off, hint only for a missing language", async () => {
  const { noteRecognitionLanguageHint, noteRecognitionModeFor, noteRecognitionOn, noteRecognitionTexts } = await import(
    "../.test-build/noteRecognitionView.js"
  );
  const fresh = parseSettings({ recordingPromptsEnabled: true, processOnlyOnAcPower: false, ...load });
  assert.equal(noteRecognitionOn(fresh), false);
  assert.equal(noteRecognitionModeFor(true), "local");
  assert.equal(noteRecognitionModeFor(false), "off");
  assert.equal(noteRecognitionOn({ noteRecognition: "local" }), true);

  assert.match(noteRecognitionTexts.label, /^ИИ распознаёт фото конспектов$/);
  assert.match(noteRecognitionTexts.languageMissing, /Параметры → Время и язык → Язык → Русский → Распознавание текста/);
  const on = (language) => ({ noteRecognition: "local", noteRecognitionLanguage: language });
  assert.equal(noteRecognitionLanguageHint(on("missing")), noteRecognitionTexts.languageMissing);
  assert.equal(noteRecognitionLanguageHint(on("available")), null);
  assert.equal(noteRecognitionLanguageHint(on("unknown")), null);
  assert.equal(noteRecognitionLanguageHint({ noteRecognition: "off", noteRecognitionLanguage: "missing" }), null);
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
