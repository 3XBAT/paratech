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
import { IconAction } from "../.test-build/ui/IconAction.js";

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

test("an answer with no claims, nothing missing and no source says what to do next", () => {
  const out = body(ready(view({ claims: [], missing: [], sources: [] })));
  const text = "В материалах пар ответа нет. Попробуйте уточнить вопрос или выбрать другой предмет.";
  assert.equal(firstP(out), text);
  assert.equal(answerTexts.empty, text);
  assert.doesNotMatch(out, /<ul|упоминается/);
  // Nothing found is said only when there is no claim and nothing missing.
  assert.doesNotMatch(body(ready(view({ claims: [], missing: ["x"], sources: [] }))), /ответа нет/);
  assert.doesNotMatch(body(ready()), /ответа нет|упоминается/);
  // A blank claim is not a claim.
  assert.equal(answerParts(view({ claims: [claim("  ")] })).empty, true);
});

test("no claims but some sources: the sources are only mentions, never «nothing found»", () => {
  const out = body(ready(view({ claims: [], missing: [], sources: [recSource()] })));
  assert.equal(firstP(out), "Прямого ответа нет, но вопрос упоминается здесь:");
  assert.doesNotMatch(out, /ответа нет\./);
  assert.ok(out.indexOf("упоминается здесь:") < out.indexOf('<ul class="ag-src"'));
  assert.match(out, /<button type="button"[^>]*aria-label="Открыть: /);
  assert.equal(answerParts(view({ claims: [], sources: [recSource()] })).emptyNote, answerTexts.mentioned);
  assert.equal(answerParts(view({ claims: [], sources: [] })).emptyNote, answerTexts.empty);
  assert.equal(answerParts(view()).emptyNote, "");
});

test("loading is one small muted line; a failed read is one quiet line with a retry icon", () => {
  const loading = body({ kind: "loading" });
  assert.equal(firstP(loading), "Загрузка ответа…");
  assert.match(loading, /<p class="ag-src-note small muted">/);
  assert.doesNotMatch(loading, /<ul|<button|ag-claim"/);
  // Without a retry handler there is no button.
  const failed = body({ kind: "failed" });
  assert.match(failed, /<p class="ag-src-note small muted" role="status">Ответ не открылся\. Попробуйте ещё раз\.<\/p>/);
  assert.doesNotMatch(failed, /<ul|<button|ag-claim"/);
  assert.equal(answerTexts.failed, "Ответ не открылся. Попробуйте ещё раз.");
});

test("a failed read offers «Повторить» as an icon action with a tooltip and a name", () => {
  const out = body({ kind: "failed" }, { onRetry: noop });
  assert.match(out, /^<section class="ag-card"><h3 class="ag-card-h">Ответ по материалам<\/h3><div[^>]*><p class="ag-src-note small muted" role="status">Ответ не открылся\. Попробуйте ещё раз\./);
  assert.equal(out.match(/<button/g).length, 1);
  assert.match(out, /<button type="button" class="ia ia-sm ag-inline-ia"[^>]*aria-label="Повторить"/);
  assert.match(out, /<span class="ia-tip" aria-hidden="true">Повторить<\/span>/);
  // The retry button is inside the quiet line, which stays the first <p>.
  assert.ok(firstP(out).startsWith("Ответ не открылся."));
});

test("the retry button calls onRetry", () => {
  let n = 0;
  const el = AnswerBodyView({ state: { kind: "failed" }, fallback: "F", onRetry: () => n++ });
  const p = el.props.children;
  const kids = [p.props.children].flat();
  const button = kids.find((c) => c && typeof c === "object" && c.type === IconAction);
  assert.ok(button);
  assert.equal(button.props.label, "Повторить");
  button.props.onClick();
  assert.equal(n, 1);
});

test("the body is a polite live region from loading on, and the fallback is not wrapped", () => {
  for (const state of [{ kind: "loading" }, { kind: "failed" }, ready()]) {
    assert.match(body(state), /<div class="ag-answer-body" aria-live="polite">/);
  }
  assert.equal(html(h(AnswerBodyView, { state: null, fallback: "F" })), "<p>F</p>");
});

test("the retry bumps the attempt in the hook deps, and still one call per attempt", async () => {
  const s = readFileSync(new URL("../src/agent/AnswerCard.tsx", import.meta.url), "utf8");
  assert.match(s, /\}, \[api, answerId, attempt\]\);/);
  assert.match(s, /setAttempt\(\(n\) => n \+ 1\)/);
  assert.equal(s.match(/startRead\(api, answerId/g).length, 1);
  // A failed start and its retry are two separate reads, one api call each.
  let calls = 0;
  const api = { answer: async () => (++calls === 1 ? Promise.reject(new Error("boom")) : view()) };
  const got = [];
  startRead(api, "a1", (id, st) => got.push(st.kind));
  await new Promise((r) => setTimeout(r, 0));
  startRead(api, "a1", (id, st) => got.push(st.kind));
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(calls, 2);
  assert.deepEqual(got, ["failed", "ready"]);
});

test("a hovered source row gets a fill that differs from the card background", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /\.ag-src-btn:hover \{ background: var\(--fill\); \}/);
  assert.match(css, /\.ag-card \{[^}]*background: var\(--raised\)/);
  const tokens = readFileSync(new URL("../src/ui/tokens.css", import.meta.url), "utf8");
  // Light and dark (both dark blocks): the fill differs from the raised surface in every theme.
  const pick = (name) => [...tokens.matchAll(new RegExp(`--${name}:\\s*(#[0-9A-Fa-f]{6})`, "g"))].map((m) => m[1].toLowerCase());
  const raised = pick("raised");
  const fill = pick("fill");
  assert.ok(raised.length >= 2);
  assert.equal(raised.length, fill.length);
  raised.forEach((r, i) => assert.notEqual(r, fill[i]));
});

test("without a state the fixed fallback line is shown", () => {
  assert.equal(firstP(body(null)), "F");
});

test("the sources open by the handler the card was given", () => {
  const onOpen = () => undefined;
  const el = AnswerBodyView({ state: ready(), fallback: "F", onOpen });
  // The container div holds the content fragment: claims, missing, note, then the sources list.
  const kids = [el.props.children.props.children].flat(3).filter((c) => c && typeof c === "object");
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
  assert.match(s, /\}, \[api, answerId, attempt\]\);/);
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

test("clampClaim cuts at the last space within 80 units of the limit, else hard", () => {
  // A word straddles the limit: the cut backs up to the space before it.
  const near = "a".repeat(550) + " " + "w".repeat(100);
  assert.equal(clampClaim(near).text, `${"a".repeat(550)}…`);
  // The space is exactly 80 units before the limit: still used.
  const edge = "a".repeat(claimClampLimit - 80) + " " + "w".repeat(200);
  assert.equal(clampClaim(edge).text, `${"a".repeat(claimClampLimit - 80)}…`);
  // The nearest space is farther than 80: hard cut at the limit.
  const far = "a".repeat(claimClampLimit - 81) + " " + "w".repeat(200);
  assert.equal(clampClaim(far).text, `${far.slice(0, claimClampLimit)}…`);
  // The limit already falls on a space: nothing more is dropped.
  const clean = "слово ".repeat(120);
  const c = clampClaim(clean);
  assert.equal(c.clamped, true);
  assert.ok(!/\s…$/.test(c.text));
  assert.ok(c.text.endsWith("слово…"));
  // Never ends in half a word of this text.
  const words = "альфа бета гамма дельта ".repeat(60);
  assert.match(clampClaim(words).text, /(альфа|бета|гамма|дельта)…$/);
  // A newline counts as whitespace.
  assert.equal(clampClaim("a".repeat(560) + "\n" + "w".repeat(100)).text, `${"a".repeat(560)}…`);
  // A surrogate pair at the limit with no space near is still not split.
  const emoji = "a".repeat(claimClampLimit - 1) + "😀".repeat(5);
  assert.equal(clampClaim(emoji).text, `${"a".repeat(claimClampLimit - 1)}…`);
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
