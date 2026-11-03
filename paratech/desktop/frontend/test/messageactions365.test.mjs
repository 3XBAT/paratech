// TASK-365: the quiet icon row under a message — «Копировать» and «Начать заново отсюда» under the student's,
// «Копировать» and «Повторить ответ» under the agent's; names as tooltip and aria-label; the one retry icon.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  copyable,
  copyNote,
  copyNoteMs,
  copyOutcome,
  messageActionTexts,
  replyCopyText,
  restartTexts,
  texts,
} from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { CopyAction } from "../.test-build/agent/MessageActions.js";
import { RestartButton } from "../.test-build/agent/RestartDialog.js";
import { IconSprite } from "../.test-build/ui/Icon.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const src = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const baseActions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const copy = () => Promise.resolve();

const reply = (text) => ({ type: "reply", text, assignments: [] });
const model = (over) => ({ key: "k", userText: "Что задали?", mentions: [], callLine: "", waiting: false, cards: [reply("Выучить §4")], rerun: false, ...over });
const target = { threadId: "a", turn: 1, text: "Что задали?", mentions: [] };
const restartBtn = h(RestartButton, { target, onOpen: noop });
const render = (m, actions = { ...baseActions, copy }, underUser = restartBtn) => renderToStaticMarkup(h(AgentRequest, { model: m, actions, underUser }));
const count = (html, part) => html.split(part).length - 1;

// ---- texts and helpers ----

test("the texts of the actions are fixed, impersonal Russian", () => {
  assert.equal(messageActionTexts.copy, "Копировать");
  assert.equal(messageActionTexts.copied, "Скопировано");
  assert.equal(messageActionTexts.copyFailed, "Не удалось скопировать");
  assert.equal(messageActionTexts.rerun, "Повторить ответ");
  assert.equal(texts.rerunTitle, "Повторить ответ");
  assert.equal(restartTexts.action, "Начать заново отсюда");
  assert.equal(copyNoteMs, 1500);
});

test("copyNote: the tooltip follows the state, idle is the action's name", () => {
  assert.equal(copyNote("idle"), "Копировать");
  assert.equal(copyNote("copied"), "Скопировано");
  assert.equal(copyNote("failed"), "Не удалось скопировать");
});

test("copyable: a message with only blanks has nothing to copy", () => {
  assert.equal(copyable("текст"), true);
  assert.equal(copyable(" \n\t"), false);
  assert.equal(copyable(""), false);
});

test("replyCopyText takes the text of the reply cards only, never a service card", () => {
  const service = [
    { type: "answer", caption: "Ответ", text: "Ответ с источниками подготовлен.", answerId: "x" },
    { type: "confirm", caption: "Подтвердите", text: "Удалить запись?", target: null },
    { type: "action", text: "Добавлено задание" },
    { type: "work", workId: "w1" },
    { type: "stopped", text: "Остановлено", buttons: [] },
    { type: "problem", text: "Не получилось", buttons: [] },
    { type: "waiting", buttons: [] },
  ];
  assert.equal(replyCopyText(service), "");
  assert.equal(replyCopyText([...service, reply(" Первый ответ \n"), reply("   "), reply("Второй")]), "Первый ответ\n\nВторой");
  assert.equal(replyCopyText([]), "");
});

test("copyOutcome: copied when the clipboard call succeeds, failed when it refuses or throws; it never throws itself", async () => {
  const seen = [];
  assert.equal(await copyOutcome((t) => (seen.push(t), Promise.resolve()), "текст"), "copied");
  assert.deepEqual(seen, ["текст"]);
  assert.equal(await copyOutcome(() => Promise.reject(new Error("clipboard")), "текст"), "failed");
  assert.equal(
    await copyOutcome(() => {
      throw new Error("sync");
    }, "текст"),
    "failed",
  );
});

// ---- the row under the student's message ----

test("under the student's message: «Копировать» and «Начать заново отсюда», icons with tooltip and aria-label", () => {
  const html = render(model({ cards: [] }));
  const row = html.match(/<div class="ag-acts ag-acts-user"[^>]*>([^]*?)<\/div>/);
  assert.ok(row, html);
  const labels = [...row[1].matchAll(/<button[^>]*aria-label="([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(labels, ["Копировать", "Начать заново отсюда: Что задали?"]);
  assert.match(row[1], /<use href="#i-copy"><\/use>/);
  assert.match(row[1], /<span class="ag-act-tip" aria-hidden="true">Копировать<\/span>/);
  assert.match(row[1], /<span class="ag-act-tip" aria-hidden="true">Начать заново отсюда<\/span>/);
  assert.equal(count(row[1], '<use href="#i-retry"></use>'), 1, "the restart icon is the retry arrow");
  // The text link is gone: no visible text of the action, only the tooltip.
  assert.doesNotMatch(html, /class="ag-restart"/);
  assert.doesNotMatch(html, /title="/, "no native title next to the tooltip");
});

test("under the student's message the row is above the cards; without text there is none", () => {
  const html = render(model());
  assert.ok(html.indexOf("ag-acts-user") > html.indexOf("Что задали?"));
  assert.ok(html.indexOf("ag-acts-user") < html.indexOf("Выучить §4"));
  const none = render(model({ userText: "" }), { ...baseActions, copy }, null);
  assert.doesNotMatch(none, /ag-acts-user/);
});

test("without a copy call the student's row keeps only the restart; without both there is no row", () => {
  const onlyRestart = render(model({ cards: [] }), baseActions);
  assert.equal(count(onlyRestart, "<button"), 1);
  assert.match(onlyRestart, /ag-act-restart/);
  assert.doesNotMatch(onlyRestart, /Копировать/);
  assert.doesNotMatch(render(model({ cards: [] }), baseActions, null), /ag-acts/);
});

// ---- the row under the agent's reply ----

test("under the agent's reply: «Копировать» and «Повторить ответ», both named by tooltip and aria-label", () => {
  const html = render(model({ rerun: true }));
  const row = html.match(/<div class="ag-acts ag-acts-agent"[^>]*>([^]*?)<\/div>/);
  assert.ok(row, html);
  const labels = [...row[1].matchAll(/<button[^>]*aria-label="([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(labels, ["Копировать", "Повторить ответ"]);
  assert.match(row[1], /<span class="ag-act-tip" aria-hidden="true">Повторить ответ<\/span>/);
  assert.match(row[1], /<use href="#i-retry"><\/use>/);
  assert.ok(html.indexOf("ag-acts-agent") > html.indexOf("Выучить §4"), "the row is under the reply");
});

test("the rerun press is the existing «rerun» button call; busy turns the button off", () => {
  const presses = [];
  const html = renderToStaticMarkup(h(AgentRequest, { model: model({ rerun: true }), actions: { ...baseActions, copy, press: (id) => presses.push(id) } }));
  assert.match(html, /class="ag-act ag-act-rerun"/);
  const busy = renderToStaticMarkup(h(AgentRequest, { model: model({ rerun: true }), actions: { ...baseActions, copy, busy: true } }));
  assert.match(busy, /<button[^>]*class="ag-act ag-act-rerun"[^>]*aria-busy="true"[^>]*disabled=""/);
  assert.match(src("agent/AgentCards.tsx"), /onRerun=\{\(\) => props\.actions\.press\("rerun"\)\}/);
});

test("service cards get no copy: a request with only cards of the service kind has no copy row", () => {
  const cards = [
    { type: "confirm", caption: "Подтвердите", text: "Удалить запись?", target: null },
    { type: "action", text: "Добавлено", detail: [], actionId: "x", undo: { label: "Отменить", busyLabel: "…", enabled: false, shown: false }, undoneText: "", afterUndoOffer: null, assignment: null, offer: null },
  ];
  const html = render(model({ userText: "", cards }), { ...baseActions, copy }, null);
  assert.doesNotMatch(html, /Копировать/);
  assert.doesNotMatch(html, /ag-acts/);
});

test("a rerun alone (no reply text) still gets its row, without copy", () => {
  const html = render(model({ userText: "", cards: [], rerun: true }), { ...baseActions, copy }, null);
  assert.match(html, /ag-acts-agent/);
  assert.doesNotMatch(html, /Копировать/);
  assert.match(html, /Повторить ответ/);
});

test("the rerun row is the same as before for a panel without a copy call", () => {
  const html = render(model({ rerun: true }), baseActions, null);
  assert.equal(count(html, "<button"), 1);
  assert.match(html, /aria-label="Повторить ответ"/);
});

// ---- the copy button ----

test("CopyAction starts quiet: «Копировать», the copy icon, an empty polite status", () => {
  const html = renderToStaticMarkup(h(CopyAction, { text: "т", copy }));
  assert.match(html, /<button[^>]*class="ag-act"[^>]*aria-label="Копировать"/);
  assert.match(html, /<use href="#i-copy"><\/use>/);
  assert.match(html, /<span class="ag-vh" role="status" aria-live="polite" aria-atomic="true"><\/span>/);
  assert.doesNotMatch(html, /Скопировано|Не удалось/);
});

test("the copy confirmation is inline: the state in the component, no toast", () => {
  const code = src("agent/MessageActions.tsx");
  assert.match(code, /setTimeout\(\(\) => setState\("idle"\), copyNoteMs\)/);
  assert.match(code, /state === "copied" \? "ok" : state === "failed" \? "warn" : "copy"/);
  assert.doesNotMatch(code, /Toast|navigator\.clipboard/);
});

test("the panel copies through the shell (the same call as the transcript page), for live and earlier messages", () => {
  const panel = src("agent/AgentPanel.tsx");
  assert.match(panel, /const copyMessage = transcriptApi\(callByName\)\.copyText;/);
  assert.match(panel, /copy: copyMessage,/);
  assert.match(panel, /copy=\{copyMessage\}/);
  assert.doesNotMatch(panel, /navigator\.clipboard/);
});

// ---- the one retry icon ----

test("the icon set has one retry icon, drawn counter-clockwise; the clockwise path is gone", () => {
  const svg = renderToStaticMarkup(h(IconSprite));
  assert.equal(count(svg, 'id="i-retry"'), 1);
  assert.equal(count(svg, 'id="i-copy"'), 1);
  assert.match(svg, /<symbol id="i-retry" viewBox="0 0 24 24"><path d="M4 12a8 8 0 1 0 2\.34-5\.66M6\.34 2\.34v4h4"><\/path><\/symbol>/);
  assert.doesNotMatch(svg, /M20 12a8 8 0 1 1-2\.34-5\.66/);
});

test("every use of the retry icon goes through the set; no page draws its own arrow", () => {
  for (const f of ["agent/AgentCards.tsx", "agent/RestartDialog.tsx", "agent/MessageActions.tsx"]) {
    assert.doesNotMatch(src(f), /<path d="M2?0 12a8/);
  }
  assert.match(src("pages/OwnNotesParts.tsx"), /icon="retry"/);
});

// ---- the look ----

test("the row is quiet and visible: muted without opacity, text colour on hover and focus, 28px target, focus ring", () => {
  const css = src("agent/agent.css");
  assert.match(css, /\.ag-act \{[^}]*width: 28px; height: 28px[^}]*color: var\(--muted\)/);
  assert.doesNotMatch(css.match(/\.ag-act \{[^}]*\}/)[0], /opacity/);
  assert.match(css, /\.ag-act:hover:enabled, \.ag-act:focus-visible, \.ag-act-note \{ background: var\(--fill\); color: var\(--text\); \}/);
  assert.match(css, /\.ag-act:focus-visible \{ outline: 2px solid var\(--accent\);/);
  assert.match(css, /\.ag-act:hover \.ag-act-tip, \.ag-act:focus-visible \.ag-act-tip, \.ag-act-note \.ag-act-tip \{ opacity: 1;/);
  assert.match(css, /\.ag-act-tip \{[^}]*position: absolute[^}]*opacity: 0[^}]*pointer-events: none/);
  assert.doesNotMatch(css, /\.ag-restart \{|\.ag-rerun/);
});
