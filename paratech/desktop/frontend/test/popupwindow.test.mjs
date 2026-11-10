// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { popupWindowApi, windowMethods } from "../.test-build/bindings/prompt.js";
import {
  collapseAdvice,
  fitSlack,
  HeightFitter,
  layoutNotes,
  popupBaseHeight,
  textsLayout,
  wantedHeight,
  windowGrows,
} from "../.test-build/popup/view.js";
import { adviceTexts } from "../.test-build/sourcePicker/view.js";

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

// Changed in the TASK-406 review (second pass): the join button on its own row no longer lowers the collapse limit to 1
// (it was: collapseAdvice/textsLayout took joinOwnRow and one other message collapsed the advice). The window grows for
// that row instead (windowGrows), so only the original rule remains: two or more messages (the reserved join line is not one).
test("the advice collapses only by the original rule; the join row does not lower the limit", () => {
  const one = [adviceTexts.browser];
  const two = [adviceTexts.muted, adviceTexts.browser];
  assert.equal(collapseAdvice(one, ""), false, "one message keeps the advice in full");
  assert.equal(collapseAdvice([], error), false);
  assert.equal(collapseAdvice(one, error), true);
  assert.deepEqual(textsLayout(one, ""), { grows: false, clamp: true, collapsed: false });
  assert.deepEqual(textsLayout([], ""), { grows: false, clamp: true, collapsed: false });
  assert.deepEqual(textsLayout(one, error), { grows: true, clamp: false, collapsed: false });
  // Changed in the TASK-406 review (final): with the join button the reserved line is not a message, so a browser warning
  // next to it keeps the advice in full, as before TASK-406 (it was: collapsed, the reserved line counted as one message).
  assert.deepEqual(textsLayout(layoutNotes([], [], true), ""), { grows: false, clamp: true, collapsed: false });
  assert.deepEqual(textsLayout(layoutNotes(one, [], true), ""), { grows: false, clamp: true, collapsed: false });
  assert.deepEqual(textsLayout(layoutNotes(two, [], true), ""), { grows: false, clamp: true, collapsed: true }, "two real messages still collapse");
});

// TASK-406 review (second pass): the window may grow for the join button's extra row, in the offer view only.
test("windowGrows: the offer view grows for an error or for the join row; the advice and the running view never", () => {
  assert.equal(windowGrows(true, false, false), false, "plain offer: the base size, nothing measured");
  assert.equal(windowGrows(true, true, false), true, "an error");
  assert.equal(windowGrows(true, false, true), true, "the join button on its own row");
  assert.equal(windowGrows(true, true, true), true);
  assert.equal(windowGrows(false, true, true), false, "advice or running recording: the normal size");
  assert.equal(windowGrows(false, false, true), false);
});

test("with the join row the window stays at the base while the content fits (100 %), and grows when it does not (150 %)", () => {
  // The natural height is the same in ready, opening and opened (reserved line), so these inputs do not change per phase.
  assert.equal(wantedHeight(298, windowGrows(true, false, true)), popupBaseHeight, "fits 300: no resize is sent");
  assert.equal(wantedHeight(334.4, windowGrows(true, false, true)), 335 + fitSlack, "needs more: grows to the natural height");
  assert.equal(wantedHeight(334.4, windowGrows(false, false, false)), popupBaseHeight, "a plain offer is never measured");
  // The fitter sends nothing for the base height the window already has, and one call for growth.
  const calls = [];
  const f = new HeightFitter(async (h) => calls.push(h));
  f.request(wantedHeight(298, true));
  assert.deepEqual(calls, []);
  f.request(wantedHeight(334.4, true));
  assert.deepEqual(calls, [335 + fitSlack]);
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
  assert.equal(calls.length, 2, "the shell reset the size on show: growth is asked again");
  calls[1].reject(new Error("the action failed"));
  await settle();

  f.request(400);
  assert.equal(calls.length, 3, "a failed call is retried on the next request");
  calls[2].resolve(400);
  await settle();

  f.forget();
  f.request(popupBaseHeight);
  assert.equal(calls.length, 3, "after a show the base height is what the window has: nothing is sent");
});
