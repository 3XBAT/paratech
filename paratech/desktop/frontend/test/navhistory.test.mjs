// TASK-201: in-window history, the keys and mouse buttons, the subject in the rows of «Сегодня».
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { initialRoute, routeToPage, routeToSummary, routeToAssignment } from "../.test-build/nav.js";
import {
  canGoBack,
  canGoForward,
  currentRoute,
  goBack,
  goForward,
  isEditableTarget,
  keyDirection,
  maxDepth,
  mouseDirection,
  newHistory,
  pushRoute,
} from "../.test-build/navHistory.js";
import { BurningSection, WaitingSection } from "../.test-build/pages/TodayParts.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => {};

test("going back and forward walks the entries", () => {
  let hist = newHistory(initialRoute);
  hist = pushRoute(hist, routeToSummary(initialRoute, "r1"));
  hist = pushRoute(hist, routeToPage("journal"));
  assert.equal(currentRoute(hist).page, "journal");
  hist = goBack(hist);
  assert.equal(currentRoute(hist).summary, "r1");
  hist = goBack(hist);
  assert.deepEqual(currentRoute(hist), initialRoute);
  assert.equal(canGoBack(hist), false);
  assert.equal(goBack(hist), hist);
  hist = goForward(goForward(hist));
  assert.equal(currentRoute(hist).page, "journal");
  assert.equal(canGoForward(hist), false);
  assert.equal(goForward(hist), hist);
});

test("the same route is not added twice", () => {
  let hist = newHistory(initialRoute);
  hist = pushRoute(hist, routeToPage("today"));
  assert.equal(hist.entries.length, 1);
  hist = pushRoute(hist, routeToPage("journal"));
  const again = pushRoute(hist, routeToPage("journal"));
  assert.equal(again, hist);
});

test("a new route after going back cuts off the forward entries", () => {
  let hist = newHistory(initialRoute);
  hist = pushRoute(hist, routeToPage("journal"));
  hist = pushRoute(hist, routeToPage("schedule"));
  hist = goBack(goBack(hist));
  hist = pushRoute(hist, routeToAssignment("a1"));
  assert.deepEqual(hist.entries.map((r) => r.page), ["today", "homework"]);
  assert.equal(canGoForward(hist), false);
});

test("the depth is limited and the oldest entry goes first", () => {
  let hist = newHistory(initialRoute);
  for (let i = 0; i < maxDepth + 20; i++) {
    hist = pushRoute(hist, routeToAssignment(`a${i}`));
  }
  assert.equal(hist.entries.length, maxDepth);
  assert.equal(currentRoute(hist).assignment, `a${maxDepth + 19}`);
  assert.equal(hist.entries[0].assignment, `a${20}`);
});

const key = (over) => ({ key: "ArrowLeft", altKey: true, ctrlKey: false, metaKey: false, shiftKey: false, target: { tagName: "DIV" }, ...over });

test("Alt+ArrowLeft and Alt+ArrowRight move through the history", () => {
  assert.equal(keyDirection(key({})), "back");
  assert.equal(keyDirection(key({ key: "ArrowRight" })), "forward");
  assert.equal(keyDirection(key({ altKey: false })), null);
  assert.equal(keyDirection(key({ ctrlKey: true })), null);
  assert.equal(keyDirection(key({ key: "Backspace" })), null);
});

test("Alt+Arrow is left to editable elements", () => {
  for (const target of [{ tagName: "INPUT" }, { tagName: "TEXTAREA" }, { tagName: "DIV", isContentEditable: true }]) {
    assert.equal(isEditableTarget(target), true);
    assert.equal(keyDirection(key({ target })), null);
  }
  assert.equal(isEditableTarget(null), false);
  assert.equal(isEditableTarget({ tagName: "BUTTON" }), false);
});

test("mouse buttons 3 and 4 are back and forward", () => {
  assert.equal(mouseDirection(3), "back");
  assert.equal(mouseDirection(4), "forward");
  assert.equal(mouseDirection(0), null);
  assert.equal(mouseDirection(1), null);
});

test("the hook cancels the events and acts on mouseup", () => {
  const src = readFileSync(new URL("../src/useNavHistory.ts", import.meta.url), "utf8");
  assert.match(src, /"mouseup"/);
  assert.match(src, /"auxclick"/);
  assert.match(src, /preventDefault/);
  assert.match(readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8"), /useNavHistory\(/);
});

test("the subject is shown in the rows of «Горит» and «Ждёт»; no subject, no line", () => {
  const withSubject = html(h(BurningSection, { burning: [{ assignmentId: "a", title: "Реферат", subject: "История", due: "", daysLeft: 2, urgent: false }], onOpenAssignment: noop }));
  assert.match(withSubject, /today-subject[^>]*>История</);
  const without = html(h(BurningSection, { burning: [{ assignmentId: "a", title: "Реферат", subject: "", due: "", daysLeft: 2, urgent: false }], onOpenAssignment: noop }));
  assert.doesNotMatch(without, /today-subject/);

  const w = (subject) => ({ proposalId: "p", kind: "send_work_email", assignmentId: "", title: "Реферат", subject, due: "", recipient: "", createdAt: "" });
  assert.match(html(h(WaitingSection, { waiting: [w("Физика")] })), /today-subject[^>]*>Физика</);
  assert.doesNotMatch(html(h(WaitingSection, { waiting: [w("")] })), /today-subject/);
});

test("a long subject is clamped to two lines", () => {
  const css = readFileSync(new URL("../src/pages/today.css", import.meta.url), "utf8");
  assert.match(css, /\.today-subject[^}]*line-clamp: 2/);
});
