// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// BUG-20261108-agent-cards (D2): the answer card shows the claims of the answer with its sources, from one read.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { answerParts, answerTexts, clampClaim, claimClampLimit, missingLine } from "../.test-build/answerView.js";
import { AnswerBodyView, ClaimLineView, startRead } from "../.test-build/agent/AnswerCard.js";
import { AnswerSourcesView } from "../.test-build/agent/AnswerSources.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const evil = "<b>x</b> <img src=x onerror=alert(1)>";

const recSource = (over = {}) => ({
  recordingId: "r1", section: "transcript", index: 4, startMs: 725000, time: "12:05", title: "Ряды", startedAt: "2026-10-19T09:00:00Z",
  noteId: "", materialId: "", locatorKind: "", locatorN: 0, label: "", removed: false, ...over,
});
const claim = (text, over = {}) => ({ text, fromLessons: true, cites: [{ quote: "q", source: 0 }], ...over });
const view = (over = {}) => ({
  id: "a1", question: "Что было на паре?", subjectId: "", createdAt: "2026-10-20T10:00:00Z",
  claims: [claim("Разбирали ряды.")], missing: [], sources: [recSource()], ...over,
});
const ready = (v = view()) => ({ kind: "ready", view: v });
const body = (state, extra = {}) => html(h("section", { className: "ag-card" }, h("h3", { className: "ag-card-h" }, "Ответ по материалам"), h(AnswerBodyView, { state, fallback: "F", onOpen: noop, ...extra })));
const firstP = (out) => out.match(/<p[^>]*>([\s\S]*?)<\/p>/)?.[1];

test("the claims and the sources come out of one render; the first <p> is the first claim", () => {
  const out = body(ready(view({ claims: [claim("Первое."), claim("Второе.")] })));
  assert.match(out, /^<section class="ag-card"><h3 class="ag-card-h">Ответ по материалам<\/h3>/);
  assert.equal(firstP(out), "Первое.");
  assert.equal(out.match(/<p class="ag-claim"/g).length, 2);
  assert.ok(out.indexOf("Второе.") < out.indexOf('<ul class="ag-src"'));
  assert.match(out, /<button type="button"[^>]*aria-label="Открыть: Расшифровка · 12:05, /);
  assert.doesNotMatch(out, /Ответ с источниками подготовлен|Загрузка/);
});

test("claims are text nodes: markup in a claim stays visible text", () => {
  const out = body(ready(view({ claims: [claim(evil)], missing: [evil] })));
  assert.doesNotMatch(out, /<b>|<img/);
  assert.match(out, /&lt;b&gt;x&lt;\/b&gt; &lt;img src=x onerror=alert\(1\)&gt;/);
  assert.ok(firstP(out).includes("&lt;b&gt;x&lt;/b&gt;"));
});

test("a claim that is not from the lessons says so in muted text", () => {
  const out = body(ready(view({ claims: [claim("Из ваших пар.", { cites: [] }), claim("Общее знание.", { fromLessons: false, cites: [] })] })));
  const ps = [...out.matchAll(/<p class="ag-claim">([\s\S]*?)<\/p>/g)].map((m) => m[1]);
  assert.equal(ps[0], "Из ваших пар.");
  assert.equal(ps[1], '<span class="ag-claim-out muted">Не из материалов пар: </span>Общее знание.');
  // Even as the first claim the text stays inside the first <p>.
  const first = body(ready(view({ claims: [claim("Общее знание.", { fromLessons: false })] })));
  assert.ok(firstP(first).includes("Не из материалов пар:") && firstP(first).includes("Общее знание."));
});

test("the parts the lessons do not answer are one muted line, only when there are some", () => {
  const out = body(ready(view({ missing: ["про сходимость", "  ", "про признаки"] })));
  assert.match(out, /<p class="ag-claim-miss small muted">В материалах не нашлось: про сходимость; про признаки<\/p>/);
  assert.doesNotMatch(body(ready(view({ missing: [] }))), /не нашлось/);
  assert.doesNotMatch(body(ready(view({ missing: [" "] }))), /не нашлось/);
  assert.equal(missingLine([]), "");
});

test("an answer with no claims and nothing missing says so quietly; the sources list still shows", () => {
  const out = body(ready(view({ claims: [], missing: [], sources: [] })));
  assert.equal(firstP(out), "В материалах ничего не нашлось.");
  assert.equal(answerTexts.empty, "В материалах ничего не нашлось.");
  assert.doesNotMatch(out, /<ul/);
  // Nothing missing is said only when there is no claim either.
  assert.doesNotMatch(body(ready(view({ claims: [], missing: ["x"] }))), /ничего не нашлось/);
  assert.doesNotMatch(body(ready()), /ничего не нашлось/);
  // A blank claim is not a claim.
  assert.equal(answerParts(view({ claims: [claim("  ")] })).empty, true);
});

test("loading is one small muted line, a failed read one quiet line; neither draws sources or buttons", () => {
  const loading = body({ kind: "loading" });
  assert.equal(firstP(loading), "Загрузка ответа…");
  assert.match(loading, /<p class="ag-src-note small muted">/);
  const failed = body({ kind: "failed" });
  assert.equal(firstP(failed), "Ответ не загрузился.");
  assert.match(failed, /role="status"/);
  assert.doesNotMatch(loading + failed, /<ul|<button|ag-claim"/);
});

test("without a state the fixed fallback line is shown", () => {
  assert.equal(firstP(body(null)), "F");
});

test("the sources open by the handler the card was given", () => {
  const onOpen = () => undefined;
  const el = AnswerBodyView({ state: ready(), fallback: "F", onOpen });
  const kids = el.props.children.flat(3).filter((c) => c && typeof c === "object");
  const list = kids.find((c) => c.type === AnswerSourcesView);
  assert.ok(list);
  assert.equal(list.props.onOpen, onOpen);
  assert.deepEqual(list.props.state.sources, [recSource()]);
});

test("startRead makes one call per start, reports the id, and a stopped read reports nothing", async () => {
  const calls = [];
  const api = { answer: async (id) => (calls.push(id), view({ id })) };
  const got = [];
  startRead(api, "a1", (id, s) => got.push([id, s.kind]));
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(calls, ["a1"]);
  assert.deepEqual(got, [["a1", "ready"]]);

  const late = [];
  const stop = startRead(api, "a2", (id, s) => late.push([id, s.kind]));
  stop();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(late, []);

  const bad = [];
  startRead({ answer: async () => Promise.reject(new Error("boom")) }, "a3", (id, s) => bad.push([id, s.kind]));
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(bad, [["a3", "failed"]]);
});

test("the hook reads again only when the core call or the answer id changes", () => {
  const s = readFileSync(new URL("../src/agent/AnswerCard.tsx", import.meta.url), "utf8");
  assert.match(s, /\}, \[api, answerId\]\);/);
  assert.equal(s.match(/startRead\(api, answerId/g).length, 1);
  // The old second reader (one call for the sources) is gone.
  const old = readFileSync(new URL("../src/agent/AnswerSources.tsx", import.meta.url), "utf8");
  assert.ok(!old.includes("api.answer") && !old.includes("useEffect"));
});

// ---- Long claims ----

test("clampClaim leaves a short text and cuts a long one at the limit with «…»", () => {
  assert.deepEqual(clampClaim("коротко"), { text: "коротко", clamped: false });
  const edge = "я".repeat(claimClampLimit);
  assert.equal(clampClaim(edge).clamped, false);
  const long = "я".repeat(claimClampLimit + 1);
  const c = clampClaim(long);
  assert.equal(c.clamped, true);
  assert.equal(c.text, `${"я".repeat(claimClampLimit)}…`);
});

test("clampClaim never cuts a surrogate pair in half", () => {
  const text = "a".repeat(claimClampLimit - 1) + "😀" + "b".repeat(50);
  const c = clampClaim(text);
  assert.equal(c.text, `${"a".repeat(claimClampLimit - 1)}…`);
  assert.ok(![...c.text].some((ch) => ch === "�"));
});

test("a long claim shows its start and «Показать полностью»; opened it shows everything and «Свернуть»", () => {
  const long = "слово ".repeat(200).trim();
  const collapsed = html(h(ClaimLineView, { claim: claim(long), expanded: false, onToggle: noop }));
  assert.ok(!collapsed.includes(long));
  assert.match(collapsed, /…<\/p>/);
  assert.match(collapsed, /<button type="button" class="ag-plan-more ag-claim-more" aria-expanded="false">Показать полностью<\/button>/);
  const opened = html(h(ClaimLineView, { claim: claim(long), expanded: true, onToggle: noop }));
  assert.ok(opened.includes(long));
  assert.match(opened, /aria-expanded="true">Свернуть<\/button>/);
  // A short claim has no toggle.
  assert.doesNotMatch(html(h(ClaimLineView, { claim: claim("коротко"), expanded: false, onToggle: noop })), /<button/);
});

test("the toggle calls onToggle", () => {
  let n = 0;
  const el = ClaimLineView({ claim: claim("я".repeat(claimClampLimit + 5)), expanded: false, onToggle: () => n++ });
  const button = el.props.children.find((c) => c && c.type === "button");
  button.props.onClick();
  assert.equal(n, 1);
});

test("a long word wraps inside the card", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /\.ag-claim[^{]*\{[^}]*overflow-wrap:\s*anywhere/);
  assert.match(css, /\.ag-card p \{[^}]*overflow-wrap:\s*anywhere/);
});
