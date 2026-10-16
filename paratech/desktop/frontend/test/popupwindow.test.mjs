// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { popupWindowApi, windowMethods } from "../.test-build/bindings/prompt.js";
import {
  adviceTexts,
  collapseAdvice,
  fitSlack,
  HeightFitter,
  popupBaseHeight,
  textsLayout,
  wantedHeight,
} from "../.test-build/popup/view.js";

const error = "Выбранное приложение не запущено. Откройте его или выберите другое.";

test("fit calls the bound Go method with a whole height", async () => {
  assert.equal(windowMethods.fit, "paratech/desktop/internal/input/wails.PopupWindowService.Fit");
  const calls = [];
  const api = popupWindowApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return 440;
  });
  assert.equal(await api.fit(372.4), 440);
  assert.deepEqual(calls, [[windowMethods.fit, 372]]);

  const bad = popupWindowApi(async () => "440");
  await assert.rejects(bad.fit(400));
});

test("only an error grows the window and shows the texts in full", () => {
  const two = [adviceTexts.muted, adviceTexts.browser];

  // Normal state: unchanged 300 px rules.
  assert.deepEqual(textsLayout([], ""), { grows: false, clamp: true, collapsed: false });
  assert.deepEqual(textsLayout(two, ""), { grows: false, clamp: true, collapsed: collapseAdvice(two, "") });

  // With an error nothing is clamped or collapsed, even next to two warnings.
  assert.deepEqual(textsLayout([], error), { grows: true, clamp: false, collapsed: false });
  assert.deepEqual(textsLayout(two, error), { grows: true, clamp: false, collapsed: false });
  assert.deepEqual(textsLayout([adviceTexts.browser], error), { grows: true, clamp: false, collapsed: false });
});

test("the wanted height is the base unless the growing content needs more", () => {
  assert.equal(popupBaseHeight, 300);
  assert.equal(wantedHeight(420, false), popupBaseHeight, "no error: never grows");
  assert.equal(wantedHeight(0, true), popupBaseHeight);
  assert.equal(wantedHeight(250, true), popupBaseHeight, "fits: back to the base");
  assert.equal(wantedHeight(300, true), popupBaseHeight);
  assert.equal(wantedHeight(371.2, true), 372 + fitSlack);
  assert.equal(wantedHeight(900, true), 900 + fitSlack, "the core clamps, not the page");
  assert.equal(wantedHeight(Number.NaN, true), popupBaseHeight);
  assert.equal(wantedHeight(Number.POSITIVE_INFINITY, true), popupBaseHeight);
});

function deferredFit() {
  const calls = [];
  const fit = (h) =>
    new Promise((resolve, reject) => {
      calls.push({ h, resolve, reject });
    });
  return { calls, fit };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

test("the fitter sends only changes and never overlaps calls", async () => {
  const { calls, fit } = deferredFit();
  const f = new HeightFitter(fit);

  f.request(popupBaseHeight);
  assert.equal(calls.length, 0, "the window starts at the base height");

  f.request(400);
  f.request(410);
  f.request(420);
  assert.deepEqual(calls.map((c) => c.h), [400], "one call at a time");

  calls[0].resolve(400);
  await settle();
  assert.deepEqual(calls.map((c) => c.h), [400, 420], "only the newest waiting height follows");

  calls[1].resolve(420);
  await settle();
  f.request(420);
  assert.equal(calls.length, 2, "a repeat is not sent");

  f.request(popupBaseHeight);
  assert.deepEqual(calls.map((c) => c.h), [400, 420, popupBaseHeight], "back to the base when it fits again");
  calls[2].resolve(popupBaseHeight);
  await settle();
});

test("the fitter resends after the window was shown again or a call failed", async () => {
  const { calls, fit } = deferredFit();
  const f = new HeightFitter(fit);

  f.request(400);
  calls[0].resolve(400);
  await settle();

  f.forget();
  f.request(400);
  assert.equal(calls.length, 2, "the shell reset the size on show");
  calls[1].reject(new Error("the action failed"));
  await settle();

  f.request(400);
  assert.equal(calls.length, 3, "a failed call is retried on the next request");
  calls[2].resolve(400);
  await settle();

  f.forget();
  f.request(popupBaseHeight);
  assert.equal(calls.length, 4, "after a show even the base height is sent");
});
