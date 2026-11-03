// TASK-365: the chat part of the panel header is quiet icon buttons (plus, pencil, archive box) with a tooltip and an
// aria-label; the open chat's title has no «идёт запрос»; the list marks only the other running chats, without accent.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { chatSections, chatTexts } from "../.test-build/agentView.js";
import { ChatBar, ChatItem } from "../.test-build/agent/ChatBar.js";
import { IconSprite } from "../.test-build/ui/Icon.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const src = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const chat = (over) => ({ threadId: "a", title: "Чат", archived: false, lastActive: "2026-10-27T10:00:00Z", running: false, ...over });
const sections = chatSections([chat({ threadId: "a", title: "Открытый", running: true }), chat({ threadId: "b", title: "Другой", running: true }), chat({ threadId: "c", title: "Тихий" })], []);
const bar = (over) =>
  renderToStaticMarkup(
    h(ChatBar, { openChat: sections.active[0], sections, note: "", onOpen: noop, onNew: noop, onRename: async () => "", onArchive: noop, onRestore: noop, ...over }),
  );
const buttons = (html) => [...html.matchAll(/<button\b[^>]*>[^]*?<\/button>/g)].map((m) => m[0]);

test("the header actions are icon buttons: aria-label and tooltip carry the exact words, no visible word", () => {
  const html = bar();
  const actions = html.match(/<div class="ag-chat-actions">([^]*?)<p class="ag-chat-note/)[1];
  const list = buttons(actions);
  assert.equal(list.length, 3);
  const want = [
    ["Переименовать", "#i-pencil"],
    ["В архив", "#i-archive"],
    ["Новый чат", "#i-plus"],
  ];
  want.forEach(([word, icon], i) => {
    assert.match(list[i], new RegExp(`aria-label="${word}"`));
    assert.match(list[i], new RegExp(`<use href="${icon}"></use>`));
    assert.match(list[i], new RegExp(`<span class="ag-act-tip" aria-hidden="true">${word}</span>`));
    // The only text in the button is the tooltip (hidden from the reader), never a visible word.
    assert.equal(list[i].replace(/<span class="ag-act-tip"[^>]*>[^<]*<\/span>/, "").replace(/<[^>]*>/g, ""), "");
    assert.match(list[i], /class="ag-act ag-act-head"/);
  });
  assert.equal(chatTexts.rename, "Переименовать");
  assert.equal(chatTexts.toArchive, "В архив");
  assert.equal(chatTexts.newChat, "Новый чат");
});

test("the actions sit on the title's row; an archived chat gets the «Вернуть» icon", () => {
  const html = bar();
  const line = html.match(/<div class="ag-chat-line">([^]*?)<p class="ag-chat-note/)[1];
  assert.ok(line.includes("ag-chat-toggle") && line.includes("ag-chat-actions"), "title and icons share one row");
  const archived = bar({ openChat: sections.active[0] && { ...sections.active[0], archived: true } });
  assert.match(archived, /aria-label="Вернуть"/);
  assert.match(archived, /<use href="#i-unarchive"><\/use>/);
  assert.doesNotMatch(archived, /aria-label="В архив"/);
});

test("the open chat's title has no running mark, even with a running request", () => {
  const html = bar();
  assert.doesNotMatch(html, /идёт запрос/);
  assert.doesNotMatch(html, /ag-chat-run/);
});

test("the list marks a running chat only when it is not the open one: a muted clock named for screen readers", () => {
  const row = (r, open) => renderToStaticMarkup(h("ul", null, h(ChatItem, { row: r, open, onOpen: noop, onToggle: noop })));
  const other = row(sections.active[1], false);
  assert.match(other, /<span class="ag-chat-run"><svg[^>]*aria-hidden="true"[^>]*><use href="#i-clock"><\/use><\/svg><span class="ag-vh">идёт запрос<\/span><\/span>/);
  assert.doesNotMatch(row(sections.active[1], true), /идёт запрос|ag-chat-run/, "the open chat is not marked");
  assert.doesNotMatch(row(sections.active[2], false), /идёт запрос|ag-chat-run/, "an idle chat is not marked");
});

test("a list row's archive action is an icon button with the word as tooltip and the chat in the label", () => {
  const html = renderToStaticMarkup(h("ul", null, h(ChatItem, { row: sections.active[2], open: false, onOpen: noop, onToggle: noop })));
  assert.match(html, /<button[^>]*role="menuitem"[^>]*class="ag-act ag-act-side"[^>]*aria-label="В архив: Тихий"/);
  assert.match(html, /<span class="ag-act-tip" aria-hidden="true">В архив<\/span>/);
  const archived = renderToStaticMarkup(h("ul", null, h(ChatItem, { row: { ...sections.active[2], archived: true }, open: false, onOpen: noop, onToggle: noop })));
  assert.match(archived, /aria-label="Вернуть: Тихий"/);
  assert.match(archived, /#i-unarchive/);
});

test("the new icons are in the set, drawn as plain paths", () => {
  const svg = renderToStaticMarkup(h(IconSprite));
  for (const id of ["i-pencil", "i-archive", "i-unarchive", "i-plus"]) {
    assert.equal(svg.split(`id="${id}"`).length - 1, 1, id);
  }
});

test("the look: no accent on the running mark, the tip opens inwards and is not clipped, focus ring and muted icons", () => {
  const css = src("agent/agent.css");
  const run = css.match(/\.ag-chat-run \{[^}]*\}/)[0];
  assert.match(run, /color: var\(--muted\)/);
  assert.doesNotMatch(run, /accent|background|box-shadow|font-weight/);
  assert.match(css, /\.ag-act-head \.ag-act-tip \{[^}]*top: calc\(100% \+ 4px\); right: 0; z-index: 7/);
  assert.match(css, /\.ag-act-side \.ag-act-tip \{[^}]*right: calc\(100% \+ 2px\)/);
  assert.match(css, /\.ag-chat-actions \{[^}]*margin-left: auto/);
  // The shared ActionButton rules give the 28px target, the muted icon, the focus ring and the focus tooltip.
  assert.match(css, /\.ag-act:focus-visible \{ outline: 2px solid var\(--accent\);/);
  assert.match(css, /\.ag-act:hover \.ag-act-tip, \.ag-act:focus-visible \.ag-act-tip/);
  assert.doesNotMatch(css, /\.ag-chat-act\b/, "the word button of a row is gone");
});

test("the header code has no word buttons left: no Button component, no text inside a button", () => {
  const code = src("agent/ChatBar.tsx");
  assert.doesNotMatch(code, /ui\/Button|<Button\b|className="btn /);
  assert.doesNotMatch(code, /ag-chat-act\b/);
});
