// «Вернуть» after the restore window (TASK-275, ADR-0029 addendum 2): the removed line itself turns permanent and loses
// its button, and while the delete is restorable the line carries the 3-day hint. The page has no DOM test harness:
// the logic sits in the helpers and the component tested here, the wiring is checked in the page source.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { changeOutcome, changeProblemText, changeTexts } from "../.test-build/homeworkView.js";
import { RemovedLine } from "../.test-build/pages/HomeworkDialogs.js";

const { renderToStaticMarkup } = server;

const page = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");
const noop = () => undefined;

test("the texts are exact", () => {
  assert.equal(changeTexts.expired, "Задание удалено окончательно, вернуть уже нельзя");
  assert.equal(changeTexts.restoreWindow, "Вернуть можно в течение 3 дней");
  assert.equal(changeProblemText("expired"), changeTexts.expired);
  // The generic refusal is not what an expired reply falls into any more.
  assert.notEqual(changeProblemText("expired"), changeProblemText("failed"));
});

test("an expired reply is told apart: no assignment, no reload, no «Открыть отправку»", () => {
  const out = changeOutcome({ problem: "expired", assignment: null, stopped: false });
  assert.equal(out.expired, true);
  assert.equal(out.assignment, null);
  assert.equal(out.reload, false);
  assert.equal(out.openSend, false);
  assert.equal(out.stopped, false);
  assert.equal(out.text, changeTexts.expired);
});

test("other replies are not expired", () => {
  for (const problem of ["in_use", "changed", "deleted", "not_found", "failed"]) {
    assert.equal(changeOutcome({ problem, assignment: null, stopped: false }).expired, false, problem);
  }
  const done = changeOutcome({ problem: "", assignment: { id: "a1" }, stopped: false });
  assert.equal(done.expired, false);
});

test("a restorable line shows the 3-day hint (muted, outside the status) and «Вернуть»", () => {
  const html = renderToStaticMarkup(h(RemovedLine, { busy: false, onRestore: noop }));
  assert.match(html, /role="status"[^>]*>Задание удалено</);
  assert.match(html, /class="small muted">Вернуть можно в течение 3 дней</);
  assert.match(html, />Вернуть</);
  assert.ok(!html.includes(changeTexts.expired));
  // The hint is not inside the live status: «Задание удалено» alone is announced.
  assert.doesNotMatch(html, /role="status"[^>]*>[^<]*Вернуть можно/);
});

test("the busy restore keeps «Возврат…» and the hint", () => {
  const html = renderToStaticMarkup(h(RemovedLine, { busy: true, onRestore: noop }));
  assert.match(html, /disabled/);
  assert.match(html, /Возврат…/);
  assert.ok(html.includes(changeTexts.restoreWindow));
});

test("an expired line says it is final, has no button and no hint, stays a focusable status", () => {
  const html = renderToStaticMarkup(h(RemovedLine, { busy: false, expired: true, onRestore: noop }));
  assert.match(html, /role="status"/);
  assert.match(html, /tabindex="-1"/);
  assert.ok(html.includes(changeTexts.expired));
  assert.ok(!html.includes("<button"));
  assert.ok(!html.includes("Вернуть"));
  assert.ok(!html.includes(changeTexts.restoreWindow));
  assert.ok(!html.includes(`>${changeTexts.removed}<`));
});

test("expired and stopped: the stop stays a fact of the delete, shown only when it happened", () => {
  const stopped = renderToStaticMarkup(h(RemovedLine, { busy: false, expired: true, stopped: true, onRestore: noop }));
  assert.ok(stopped.includes(`${changeTexts.expired} · ${changeTexts.stopped}`));
  const plain = renderToStaticMarkup(h(RemovedLine, { busy: false, expired: true, onRestore: noop }));
  assert.ok(!plain.includes(changeTexts.stopped));
});

test("the list and the detail page set expired on that problem; no refusal line and no reload for it", () => {
  // List: change() returns before setRowProblem / reloadItems when the reply is expired.
  assert.match(
    page,
    /if \(out\.expired && expired !== undefined\) \{[^}]*expired\(\);\s*return;\s*\}\s*setRowProblem\(\{ id: a\.id, text: out\.text, openSend: out\.openSend \}\);\s*if \(out\.reload\) \{\s*await reloadItems\(\);/,
  );
  assert.match(page, /\(\) => setExpiredIds\(\(cur\) => \(\{ \.\.\.cur, \[a\.id\]: true \}\)\)/);
  assert.match(page, /expired=\{expiredIds\[a\.id\] === true\}/);
  // A new delete of the same row starts restorable again.
  assert.match(page, /setExpiredIds\(\(cur\) => \(\{ \.\.\.cur, \[a\.id\]: false \}\)\)/);
  // Detail: changeThis() returns before setChangeProblem / reload.
  assert.match(
    page,
    /if \(out\.expired && expired !== undefined\) \{[^}]*expired\(\);\s*return;\s*\}\s*setChangeProblem\(out\.text\);\s*setChangeOpenSend\(out\.openSend\);\s*if \(out\.reload\) \{\s*await reload\(true\)/,
  );
  assert.match(page, /\(\) => setGoneExpired\(true\)/);
  assert.match(page, /expired=\{goneExpired\}/);
  assert.match(page, /setGoneExpired\(false\);\s*setGone\(next\);/);
});

test("the focus: the removed line takes it when it turns expired", () => {
  const dialogs = readFileSync(new URL("../src/pages/HomeworkDialogs.tsx", import.meta.url), "utf8");
  assert.match(dialogs, /<span role="status" tabIndex=\{-1\} ref=\{statusRef\}>/);
  assert.match(dialogs, /if \(expired\) \{\s*statusRef\.current\?\.focus\(\);\s*\}\s*\}, \[expired\]\);/);
});
