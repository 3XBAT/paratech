// TASK-201: in-window history, the keys and mouse buttons, the subject in the rows of «Сегодня».
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { initialRoute, routeToPage, routeToSummary, routeToAssignment, routeToSubject, routeToOpenedAssignment } from "../.test-build/nav.js";
import {
  canGoBack,
  canGoForward,
  currentRoute,
  goBack,
  goForward,
  handleKey,
  handleMouse,
  isEditableTarget,
  keyDirection,
  maxDepth,
  mouseDirection,
  needsRemount,
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

test("an open subject is an entry: back leaves it for the list, forward opens it again (TASK-202)", () => {
  let hist = newHistory(routeToPage("subjects"));
  hist = pushRoute(hist, routeToSubject(currentRoute(hist), "s1"));
  hist = pushRoute(hist, routeToSubject(currentRoute(hist), "s2"));
  assert.deepEqual(hist.entries.map((r) => r.subject), ["", "s1", "s2"]);
  hist = goBack(hist);
  assert.equal(currentRoute(hist).subject, "s1");
  hist = goBack(hist);
  assert.equal(currentRoute(hist).subject, "");
  assert.equal(currentRoute(hist).page, "subjects");
  assert.equal(currentRoute(goForward(hist)).subject, "s1");
  // Another section forgets the subject; «Назад» brings the section back with it.
  const away = pushRoute(goForward(hist), routeToPage("journal"));
  assert.equal(currentRoute(away).subject, "");
  assert.equal(currentRoute(goBack(away)).subject, "s1");
});

test("a card opened from the list of «Задания» is an entry; the back to the list is one too", () => {
  let hist = newHistory(routeToPage("homework"));
  hist = pushRoute(hist, routeToOpenedAssignment(currentRoute(hist), "a1"));
  assert.equal(currentRoute(hist).assignment, "a1");
  assert.equal(currentRoute(hist).page, "homework");
  assert.equal(currentRoute(goBack(hist)).assignment, "");
  // The page's own «Назад» to the list adds an entry, as in a browser.
  hist = pushRoute(hist, routeToOpenedAssignment(currentRoute(hist), ""));
  assert.deepEqual(hist.entries.map((r) => r.assignment), ["", "a1", ""]);
  // Opening the card again from history needs the page remounted on the card; leaving it does not.
  assert.equal(needsRemount(hist.entries[1], hist.entries[0]), true);
  assert.equal(needsRemount(hist.entries[0], hist.entries[1]), false);
});

test("a subject does not leak into other routes: a summary over «Предметы» keeps it, a jump to an assignment drops it", () => {
  const r = routeToSubject(routeToPage("subjects"), "s1");
  assert.equal(routeToSummary(r, "r1").subject, "s1");
  assert.equal(routeToAssignment("a1").subject, "");
  assert.equal(routeToOpenedAssignment(r, "a1").page, "homework");
});

test("App wires the subject and the card through the route, not through page state", () => {
  const app = readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.match(app, /routeToSubject\(r, id\)/);
  assert.match(app, /routeToOpenedAssignment\(r, id\)/);
  assert.match(app, /onSelect=\{props\.onSelectAssignment\}/);
  const page = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");
  assert.match(page, /props\.onSelect \? \(props\.openAssignment \?\? ""\) : own/);
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

const twoEntries = () => goBack(pushRoute(newHistory(initialRoute), routeToPage("journal")));

test("mousedown and auxclick on buttons 3/4 are cancelled without navigating", () => {
  const hist = twoEntries();
  for (const type of ["mousedown", "auxclick"]) {
    assert.deepEqual(handleMouse({ button: 4, type }, hist), { prevent: true, next: null });
    assert.deepEqual(handleMouse({ button: 3, type }, hist), { prevent: true, next: null });
  }
});

test("mouseup on a side button navigates", () => {
  const hist = twoEntries();
  const fwd = handleMouse({ button: 4, type: "mouseup" }, hist);
  assert.equal(fwd.prevent, true);
  assert.equal(currentRoute(fwd.next).page, "journal");
  // «back» at the start: cancelled, nowhere to go.
  assert.deepEqual(handleMouse({ button: 3, type: "mouseup" }, hist), { prevent: true, next: null });
  assert.deepEqual(handleMouse({ button: 0, type: "mouseup" }, hist), { prevent: false, next: null });
});

test("a prevented or repeated keydown is ignored", () => {
  const hist = twoEntries();
  const ok = handleKey(key({ key: "ArrowRight" }), hist);
  assert.equal(ok.prevent, true);
  assert.notEqual(ok.next, null);
  assert.deepEqual(handleKey(key({ key: "ArrowRight", defaultPrevented: true }), hist), { prevent: false, next: null });
  assert.deepEqual(handleKey(key({ key: "ArrowRight", repeat: true }), hist), { prevent: false, next: null });
});

test("a keydown in an editable target is ignored", () => {
  const a = handleKey(key({ key: "ArrowRight", target: { tagName: "INPUT" } }), twoEntries());
  assert.deepEqual(a, { prevent: false, next: null });
});

test("going back from a summary opened over an assignment does not remount the page", () => {
  const over = routeToSummary(routeToAssignment("a1"), "r1");
  assert.equal(needsRemount(routeToAssignment("a1"), over), false);
  assert.equal(needsRemount(routeToAssignment("a2"), routeToAssignment("a1")), true);
  assert.equal(needsRemount(routeToAssignment("a1"), routeToPage("journal")), true);
  assert.equal(needsRemount(routeToPage("journal"), over), false);
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
