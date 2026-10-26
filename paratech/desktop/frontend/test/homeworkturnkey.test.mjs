// «Сделать под ключ» on the assignment page (TASK-186): the block per state, the single start, the errors, the renames.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorTexts, notFoundText } from "../.test-build/bindings/turnkey.js";
import { WorkWritingCard } from "../.test-build/agent/WorkCard.js";
import { HomeworkTurnkey, TurnkeyBlockView } from "../.test-build/pages/HomeworkTurnkey.js";
import { loadTurnkeyBlock, startTurnkeyOnce, turnkeyTexts } from "../.test-build/homeworkTurnkey.js";
import { generatingLabel, generationView } from "../.test-build/homeworkView.js";
import { singleFlight } from "../.test-build/workView.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const src = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");

const noSource = errorTexts.find((t) => t.startsWith("Задание не найдено"));

function view(state, busy = false) {
  return h(TurnkeyBlockView, { api: {}, state, busy, onStart: noop });
}

// Looks for a component type in the element tree a function component returns (SSR does not run effects, so the
// card of a loaded work renders nothing there).
function hasType(el, type) {
  if (el === null || typeof el !== "object") {
    return false;
  }
  if (Array.isArray(el)) {
    return el.some((e) => hasType(e, type));
  }
  return el.type === type || hasType(el.props?.children, type);
}

test("without a work the block shows the caption and the button, and no card", () => {
  const out = renderToStaticMarkup(view({ phase: "none", error: "" }));
  assert.match(out, /Сделать под ключ/);
  assert.match(out, /Разбор задания и вопросы, затем работа или презентация файлом/);
  assert.match(out, /<button/);
  assert.doesNotMatch(out, /disabled/);
  assert.doesNotMatch(out, /платн/i);
  assert.ok(!hasType(TurnkeyBlockView({ api: {}, state: { phase: "none", error: "" }, busy: false, onStart: noop }), WorkWritingCard));
});

test("an existing work shows its card, not the button", () => {
  const el = TurnkeyBlockView({ api: {}, state: { phase: "work", workId: "w1" }, busy: false, onStart: noop });
  assert.ok(hasType(el, WorkWritingCard));
  const out = renderToStaticMarkup(view({ phase: "work", workId: "w1" }));
  assert.doesNotMatch(out, /<button/);
  assert.doesNotMatch(out, /Разбор задания и вопросы/);
});

test("loading shows no button yet; a pending start disables it", () => {
  const loading = renderToStaticMarkup(view({ phase: "loading" }));
  assert.match(loading, /Загрузка…/);
  assert.doesNotMatch(loading, /<button/);
  assert.match(renderToStaticMarkup(view({ phase: "none", error: "" }, true)), /disabled/);
});

test("an error is shown as the core's text, including the no-source text", () => {
  assert.ok(noSource);
  const out = renderToStaticMarkup(view({ phase: "none", error: noSource }));
  assert.ok(out.includes(noSource));
  assert.match(out, /role="alert"/);
  assert.match(out, /<button/, "the button stays for another try");
});

test("page open: a work gives its card, a missing work the button, another failure its text", async () => {
  const found = { workOfAssignment: async () => ({ id: "w9" }) };
  assert.deepEqual(await loadTurnkeyBlock(found, "a1"), { phase: "work", workId: "w9" });
  const missing = { workOfAssignment: async () => Promise.reject(new Error(notFoundText)) };
  assert.deepEqual(await loadTurnkeyBlock(missing, "a1"), { phase: "none", error: "" });
  const broken = { workOfAssignment: async () => Promise.reject(new Error("raw internal text")) };
  const s = await loadTurnkeyBlock(broken, "a1");
  assert.equal(s.phase, "none");
  assert.ok(errorTexts.includes(s.error) && s.error !== "");
});

test("a start shows the card of the work it returns", async () => {
  const calls = [];
  const api = {
    startForAssignment: async (id) => {
      calls.push(id);
      return { id: "w2" };
    },
  };
  // The state carries the reply too: it may hold the problem of a reader call that did not go through (TASK-193).
  assert.deepEqual(await startTurnkeyOnce(singleFlight(), api, "a1"), { phase: "work", workId: "w2", work: { id: "w2" } });
  assert.deepEqual(calls, ["a1"]);
});

test("a double click makes exactly one call", async () => {
  let calls = 0;
  let release = noop;
  const api = {
    startForAssignment: () => {
      calls++;
      return new Promise((resolve) => {
        release = () => resolve({ id: "w2" });
      });
    },
  };
  const flight = singleFlight();
  const first = startTurnkeyOnce(flight, api, "a1");
  const second = startTurnkeyOnce(flight, api, "a1");
  assert.equal(await second, null, "the second click is ignored");
  release();
  assert.deepEqual(await first, { phase: "work", workId: "w2", work: { id: "w2" } });
  assert.equal(calls, 1);
  // After the answer a new start is allowed again (a failed one can be repeated).
  const again = startTurnkeyOnce(flight, api, "a1");
  release();
  await again;
  assert.equal(calls, 2);
});

test("a failed start comes back as the core's text and lets the button start again", async () => {
  const api = { startForAssignment: async () => Promise.reject(new Error(noSource)) };
  assert.deepEqual(await startTurnkeyOnce(singleFlight(), api, "a1"), { phase: "none", error: noSource });
  const unknown = { startForAssignment: async () => Promise.reject(new Error("boom")) };
  const s = await startTurnkeyOnce(singleFlight(), unknown, "a1");
  assert.ok(errorTexts.includes(s.error));
  assert.ok(!s.error.includes("boom"));
});

test("the stateful block starts in loading", () => {
  const out = renderToStaticMarkup(h(HomeworkTurnkey, { api: {}, assignmentId: "a1" }));
  assert.match(out, /Загрузка…/);
});

test("the page names the blocks «Быстрый черновик» and «Сделать под ключ»", () => {
  const page = src("pages/HomeworkPage.tsx");
  assert.match(page, /<h2>Быстрый черновик<\/h2>/);
  assert.match(page, /"Новый черновик" : "Доработать черновик"/);
  assert.doesNotMatch(page, /Новое решение|Доработать решение|<h2>Решение<\/h2>/);
  assert.match(page, /<HomeworkTurnkey api=\{turnkey\} assignmentId=\{id\} \/>/);
  assert.equal(turnkeyTexts.title, "Сделать под ключ");
});

test("the generation chips and the busy label say «черновик»", () => {
  assert.equal(generatingLabel, "Готовим черновик…");
  assert.equal(generationView({ state: "failed", message: "", percent: 0 }).label, "Черновик не готов");
  assert.equal(generationView({ state: "done", message: "", percent: 0 }).label, "Черновик готов");
});

test("the block marks nothing as paid and renders no HTML", () => {
  for (const rel of ["pages/HomeworkTurnkey.tsx", "homeworkTurnkey.ts"]) {
    const text = src(rel);
    assert.doesNotMatch(text, /платн|dangerouslySetInnerHTML|innerHTML/i, rel);
  }
});
