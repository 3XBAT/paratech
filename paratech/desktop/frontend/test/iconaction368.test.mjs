// TASK-368 (part 1): the shared icon button (ui/IconAction) and the agent panel's small buttons that became icons.
// Owner rule: small and secondary places have no word buttons; the words are a tooltip (hover and keyboard focus) and the
// aria-label. Words stay on the main action of a card and on dialog buttons.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { IconAction, TIP_DELAY_MS, fitTip } from "../.test-build/ui/IconAction.js";
import { IconSprite } from "../.test-build/ui/Icon.js";
import { cardButtonIcon, offerIcon, undoIcon } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { ConfirmCardPending } from "../.test-build/agent/ConfirmCard.js";
import { ScheduleCardPending } from "../.test-build/agent/ScheduleCard.js";
import { WorkBody } from "../.test-build/agent/WorkCard.js";
import { RestartNote } from "../.test-build/agent/RestartDialog.js";
import { UnderstandingCard } from "../.test-build/agent/TurnkeyCards.js";
import { ariaLabels, buttonTag, isOff, isOn, visibleText } from "./iconbtn.mjs";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const src = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const count = (text, part) => text.split(part).length - 1;
const html = (el) => renderToStaticMarkup(el);
const ia = (over = {}) => html(h(IconAction, { icon: "x", label: "Закрыть", onClick: noop, ...over }));

// ---- the component ----

test("an icon action is a button named by aria-label, with the same words as its tooltip and no visible text", () => {
  const out = ia({ label: "Отменить запрос" });
  assert.match(out, /^<button type="button" class="ia" data-place="above" aria-label="Отменить запрос">/);
  assert.match(out, /<use href="#i-x"><\/use>/);
  assert.match(out, /<span class="ia-tip" aria-hidden="true">Отменить запрос<\/span>/);
  assert.equal(visibleText(out), "", "nothing visible but the icon");
  // The tooltip is not the name: it is hidden from the accessibility tree and the label carries the words.
  assert.equal(count(out, "Отменить запрос"), 2);
});

test("tipText replaces the tooltip only; place, size, variant, role, tabIndex and expanded are options", () => {
  const out = ia({ label: "Повторить", tipText: "Повторить поиск (Enter)", place: "left", size: "sm", variant: "danger", role: "menuitem", tabIndex: -1, expanded: false });
  assert.match(out, /class="ia ia-danger ia-sm"/);
  assert.match(out, /data-place="left"/);
  assert.match(out, /role="menuitem"/);
  assert.match(out, /tabindex="-1"/);
  assert.match(out, /aria-expanded="false"/);
  assert.match(out, /aria-label="Повторить"/);
  assert.match(out, /<span class="ia-tip" aria-hidden="true">Повторить поиск \(Enter\)<\/span>/);
  assert.match(ia({ note: true }), /class="ia ia-note"/);
  // The tooltip words that differ from the name are also given to screen readers (the tooltip itself is aria-hidden).
  assert.match(out, /aria-describedby="([^"]+)"/);
  const id = out.match(/aria-describedby="([^"]+)"/)[1];
  assert.match(out, new RegExp(`<span id="${id}" class="ia-vh">Повторить поиск \\(Enter\\)</span>`));
  assert.doesNotMatch(ia({ tipText: "Закрыть" }), /aria-describedby|ia-vh/, "the same words need no description");
  assert.doesNotMatch(ia(), /aria-describedby|ia-vh/);
});

test("busy: a small spinner instead of the icon, the button is off and aria-busy", () => {
  const out = ia({ busy: true, icon: "stop", label: "Остановка…" });
  assert.match(out, /aria-busy="true"/);
  assert.match(out, / disabled=""/);
  assert.match(out, /<span class="ia-spin" aria-hidden="true"><\/span>/);
  assert.doesNotMatch(out, /<use/);
  assert.equal(isOff(out, "Остановка…"), true);
  assert.doesNotMatch(ia(), /aria-busy|ia-spin/);
});

test("disabled: off; with a reason the button stays focusable (aria-disabled), the reason is in the tooltip and read to screen readers", () => {
  assert.equal(isOff(ia({ disabled: true }), "Закрыть"), true);
  assert.match(ia({ disabled: true }), / disabled=""/, "no reason: natively disabled");
  assert.equal(isOn(ia(), "Закрыть"), true);
  const out = ia({ disabled: true, disabledReason: "Нечего сохранять" });
  assert.match(out, /aria-disabled="true"/);
  assert.doesNotMatch(out, / disabled=""/, "a native disabled button cannot take the focus, so its reason could not be read");
  assert.equal(isOff(out, "Закрыть"), true);
  assert.match(out, /aria-describedby="[^"]+"/);
  assert.match(out, /<span class="ia-tip" aria-hidden="true">Закрыть\. Нечего сохранять<\/span>/);
  assert.match(out, /<span id="[^"]+" class="ia-vh">Нечего сохранять<\/span>/);
  // A reason only matters while the button is off.
  assert.doesNotMatch(ia({ disabledReason: "Нечего сохранять" }), /Нечего сохранять/);
  // A running call is a native disabled + aria-busy, whatever the reason.
  assert.match(ia({ busy: true, disabled: true, disabledReason: "Нечего сохранять" }), / disabled=""/);
});

test("an aria-disabled button does nothing on click or mouse down", () => {
  const code = src("ui/IconAction.tsx");
  assert.match(code, /const click = \(e: MouseEvent<HTMLButtonElement>\) => \{\s*if \(soft\) \{\s*e\.preventDefault\(\);\s*return;\s*\}\s*props\.onClick\(e\);/);
  assert.match(code, /const down = [^]*if \(!soft\) \{\s*props\.onMouseDown\?\.\(e\);/);
  assert.match(code, /onClick=\{click\}/);
  assert.match(code, /onMouseDown=\{down\}/);
});

test("fitTip keeps a tooltip inside the clipping box: nudges sideways, flips when there is no room", () => {
  const clip = { left: 0, right: 340, top: 0, bottom: 600 };
  const tip = (left, top, w = 100, h2 = 24) => ({ left, right: left + w, top, bottom: top + h2 });
  assert.deepEqual(fitTip(tip(100, 100), clip, "above"), { dx: 0, flip: false });
  assert.deepEqual(fitTip(tip(-20, 100), clip, "above"), { dx: 24, flip: false }, "past the left edge: moves right, 4px margin");
  assert.deepEqual(fitTip(tip(300, 100), clip, "above"), { dx: -64, flip: false }, "past the right edge: moves left");
  assert.deepEqual(fitTip(tip(100, -10), clip, "above"), { dx: 0, flip: true }, "no room above: opens below");
  assert.deepEqual(fitTip(tip(100, 590), clip, "below"), { dx: 0, flip: true }, "no room below: opens above");
  assert.deepEqual(fitTip(tip(-60, 100), clip, "left"), { dx: 0, flip: true }, "no room on the left: opens right");
  assert.deepEqual(fitTip(tip(300, 100), clip, "right"), { dx: 0, flip: true });
  assert.deepEqual(fitTip(tip(100, 100), clip, "right"), { dx: 0, flip: false });
});

test("the tooltip opens after a hover pause and at once on keyboard focus; it is measured after it shows; Esc hides it (WCAG 1.4.13)", () => {
  const code = src("ui/IconAction.tsx");
  assert.equal(TIP_DELAY_MS, 350, "the hover pause");
  assert.match(code, /window\.setTimeout\(\(\) => \{\s*setDismissed\(false\);\s*setHovered\(true\);\s*\}, TIP_DELAY_MS\)/);
  assert.match(code, /onMouseEnter=\{enter\}/);
  assert.match(code, /onMouseLeave=\{leave\}/);
  assert.match(code, /onFocus=\{focus\}/);
  assert.match(code, /onBlur=\{\(\) => setFocused\(false\)\}/);
  // Keyboard focus: no timer around the state change.
  assert.match(code, /matches\(":focus-visible"\)\) \{\s*setDismissed\(false\);\s*setFocused\(true\);/);
  // Measured in a layout effect once it is open (it is display: none before), so there is no flicker.
  assert.match(code, /useLayoutEffect\(\(\) => \{\s*const t = tip\.current;\s*if \(!open/);
  assert.match(code, /data-open=\{open \? "" : undefined\}/);
  assert.match(code, /e\.key === "Escape"[^}]*setDismissed\(true\)/);
  assert.match(code, /const open = !dismissed && \(hovered \|\| focused \|\| props\.note === true\)/);
  // Esc is not swallowed: a menu or a dialog around the button still closes. (preventDefault is only in the aria-disabled click.)
  assert.doesNotMatch(code, /stopPropagation/);
  assert.equal(count(code, "preventDefault"), 1);
});

test("a closed tooltip is not rendered open: no data-open on a plain button, a note stays open", () => {
  assert.doesNotMatch(ia(), /data-open/);
  assert.match(ia({ note: true }), /data-open=""/);
});

// ---- the look ----

const css = src("ui/iconAction.css");
const rules = [...css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({ selector: m[1].trim(), decls: m[2].trim() }));
const rule = (selector) => rules.find((r) => r.selector === selector)?.decls ?? "";

test("the target is at least 32px, 28px only for the dense size", () => {
  assert.match(rule(".ia"), /width: 32px; height: 32px/);
  assert.match(rule(".ia-sm"), /width: 28px; height: 28px/);
  assert.match(rule(".ia"), /color: var\(--muted\)/);
  assert.doesNotMatch(rule(".ia"), /opacity/);
});

test("keyboard focus is visible; the tooltip is not laid out while hidden and shows when the script opens it", () => {
  assert.match(rule(".ia:focus-visible"), /outline: 2px solid var\(--accent\)/);
  assert.match(rule('.ia:hover:enabled:not([aria-disabled="true"]), .ia:focus-visible, .ia-note'), /background: var\(--fill\); color: var\(--text\)/);
  // display: none while hidden: a tooltip near the edge of the chat or a list never adds scroll space.
  assert.match(rule(".ia-tip"), /display: none; position: absolute[^}]*pointer-events: none/);
  assert.doesNotMatch(rule(".ia-tip"), /opacity: 0|transition/, "hiding is display, not an invisible box that still takes room");
  assert.match(rule(".ia[data-open] .ia-tip"), /display: block/);
  // Hover and focus do not show it by CSS: the hover pause is the script's.
  assert.doesNotMatch(css, /\.ia:hover \.ia-tip|\.ia:focus-visible \.ia-tip/);
});

test("a disabled button with a reason looks disabled and does not light up on hover", () => {
  assert.match(rule('.ia:disabled, .ia[aria-disabled="true"]'), /opacity: \.5; cursor: not-allowed/);
  assert.match(rule('.ia-danger:hover:enabled:not([aria-disabled="true"]), .ia-danger:focus-visible'), /color: var\(--rec-t\)/);
});

test("the tooltip opens above, below, left or right and flips; a long text wraps instead of running off", () => {
  for (const place of ["above", "below", "left", "right"]) {
    assert.match(css, new RegExp(`\\.ia\\[data-place="${place}"\\] \\.ia-tip \\{`));
    assert.match(css, new RegExp(`\\.ia\\[data-place="${place}"\\] \\.ia-tip\\[data-flip\\] \\{`));
  }
  assert.match(rule(".ia-tip"), /max-width: min\(240px, 70vw\)/);
  assert.match(rule(".ia-tip"), /white-space: normal/);
  assert.match(rule('.ia[data-place="above"] .ia-tip'), /translateX\(calc\(-50% \+ var\(--ia-dx, 0px\)\)\)/);
});

test("danger turns red on hover and focus only; a busy button shows the spinner at full strength", () => {
  assert.match(rule('.ia-danger:hover:enabled:not([aria-disabled="true"]), .ia-danger:focus-visible'), /color: var\(--rec-t\)/);
  assert.match(rule('.ia:disabled, .ia[aria-disabled="true"]'), /opacity: \.5/);
  assert.match(rule('.ia[aria-busy="true"]:disabled'), /opacity: 1/);
  assert.match(rule(".ia-spin"), /animation: ia-spin 1s linear infinite/);
});

test("reduced motion and forced colours have their rules", () => {
  assert.match(css, /@media \(prefers-reduced-motion: reduce\) \{ \.ia\[data-open\] \.ia-tip \{ animation: none; \} \.ia-spin \{ animation: none; \} \}/);
  assert.match(css, /@media \(forced-colors: active\) \{ \.ia \{ color: ButtonText; \} \.ia:disabled, \.ia\[aria-disabled="true"\] \{ color: GrayText; \} \.ia-tip \{ border: 1px solid CanvasText; \} \}/);
});

test("the stylesheet is loaded by the main window and by the popup, which has no components.css", () => {
  assert.match(src("main.tsx"), /import "\.\/ui\/iconAction\.css";/);
  assert.match(src("popup/main.tsx"), /import "\.\.\/ui\/iconAction\.css";/);
  assert.doesNotMatch(css, /\.btn|\.ag-/, "needs tokens.css only");
});

// ---- the icon set ----

test("the new icons are in the sprite once each; every name of the type has a symbol", () => {
  const svg = html(h(IconSprite));
  // «open» (an arrow out of a box) is only for what opens outside the app; «skip» and «save» went with the word buttons.
  for (const name of ["open", "undo", "chevl", "arrowup", "arrowdown", "swap", "paste", "insert"]) {
    assert.equal(count(svg, `id="i-${name}"`), 1, name);
  }
  for (const name of ["skip", "save"]) {
    assert.equal(count(svg, `id="i-${name}"`), 0, `${name} is no longer drawn`);
  }
  const type = src("ui/Icon.tsx").match(/export type IconName =([^;]+);/)[1];
  const names = [...type.matchAll(/"([a-z]+)"/g)].map((m) => m[1]);
  for (const name of names) {
    assert.equal(count(svg, `id="i-${name}"`), 1, `symbol of ${name}`);
  }
  assert.equal(count(svg, "<symbol"), names.length);
});

// ---- which button is an icon ----

test("cardButtonIcon: the secondary buttons are icons, the main actions keep words", () => {
  assert.equal(cardButtonIcon("revise", "plan"), "pencil");
  assert.equal(cardButtonIcon("stop", "progress"), "stop");
  assert.equal(cardButtonIcon("stop", "waiting"), "stop");
  assert.equal(cardButtonIcon("cancel", "stopped"), "x");
  // Yes / no of the plan and the forward buttons of a stopped request are words.
  assert.equal(cardButtonIcon("cancel", "plan"), null);
  for (const id of ["approve", "continue", "resume", "retry", "restart", "resend", "rerun"]) {
    assert.equal(cardButtonIcon(id, "stopped"), null, id);
  }
  // Keyed on what the button does, never on its Russian words.
  assert.equal(undoIcon("undo"), "undo");
  assert.equal(undoIcon("stop"), "stop");
  const source = src("agentView.ts");
  assert.doesNotMatch(source.slice(source.indexOf("export function undoIcon")), /^[^}]*(?:=== |!== )resultTexts/, "no comparison of labels");
  assert.match(source, /kind: started \? "stop" : "undo",/, "the action line says what its button does");
  assert.equal(offerIcon({ action: "undo_action", target: null }), "undo");
  for (const action of ["start_recording", "open_summary", "google_sync", "sound_check", "open_material"]) {
    assert.equal(offerIcon({ action, target: null }), null, action);
  }
});

// ---- the cards ----

const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const request = (cards, over = {}) => html(h(AgentRequest, { model: { key: "k", userText: "", mentions: [], callLine: "", waiting: false, cards, rerun: false }, actions: { ...actions, ...over } }));

test("plan card: «Да, выполнить» and «Не выполнять» keep their words, «Изменить» is a pencil", () => {
  const out = request([
    {
      type: "plan", caption: "План", summary: "Сводка", steps: ["Шаг"], notes: [], total: 1, hidden: 0, marks: [], progress: null,
      buttons: [{ id: "approve", label: "Да, выполнить" }, { id: "revise", label: "Изменить" }, { id: "cancel", label: "Не выполнять" }],
    },
  ]);
  assert.deepEqual(ariaLabels(out).sort(), ["Изменить"]);
  const words = visibleText(out);
  assert.match(words, /Да, выполнить/);
  assert.match(words, /Не выполнять/);
  assert.doesNotMatch(words, /Изменить/);
  assert.match(buttonTag(out, "Изменить"), /class="ia"/);
  assert.match(out, /<use href="#i-pencil"><\/use><\/svg><span class="ia-tip" aria-hidden="true">Изменить<\/span>/);
  // The first word button is the primary one, as before.
  assert.match(out, /class="btn primary sm"[^>]*>Да, выполнить</);
  assert.match(out, /class="btn sm"[^>]*>Не выполнять</);
  // The words first, then the icon.
  assert.ok(out.indexOf(">Не выполнять<") < out.indexOf('aria-label="Изменить"'));
});

test("question card: «Ответить» and «Пропустить вопросы» are both words (a form's own two buttons)", () => {
  const out = request([{ type: "questions", caption: "Вопросы", questions: [], active: true, skip: "Пропустить вопросы", submit: "Ответить" }]);
  assert.deepEqual(ariaLabels(out), []);
  assert.match(out, /class="btn primary sm"[^>]*>Ответить</);
  assert.match(out, /class="btn ghost sm"[^>]*>Пропустить вопросы</);
});

test("waiting and progress rows: «Остановить» is the stop icon", () => {
  const waiting = request([{ type: "waiting", buttons: [{ id: "stop", label: "Остановить" }] }]);
  assert.ok(isOn(waiting, "Остановить"));
  assert.equal(visibleText(waiting).includes("Остановить"), false);
  const progress = request([{ type: "progress", caption: "Работа идёт", detail: "", buttons: [{ id: "stop", label: "Остановить" }] }]);
  assert.ok(isOn(progress, "Остановить"));
  assert.equal(visibleText(progress).includes("Остановить"), false);
  assert.ok(isOff(request([{ type: "waiting", buttons: [{ id: "stop", label: "Остановить" }] }], { busy: true }), "Остановить"));
});

test("stopped line: «Продолжить» keeps its word, «Отменить запрос» is a cross", () => {
  const out = request([{ type: "stopped", text: "Остановлено", buttons: [{ id: "resume", label: "Продолжить" }, { id: "cancel", label: "Отменить запрос" }] }]);
  assert.deepEqual(ariaLabels(out), ["Отменить запрос"]);
  assert.match(visibleText(out), /Остановлено Продолжить/);
  assert.doesNotMatch(visibleText(out), /Отменить запрос/);
  assert.match(out, /class="ia ia-danger"/);
  // A request refused as busy: stopping the blocker is an icon, sending again is the word.
  const problem = request([{ type: "problem", text: "Занято", buttons: [{ id: "stop", label: "Остановить" }, { id: "resend", label: "Отправить ещё раз" }] }]);
  assert.deepEqual(ariaLabels(problem), ["Остановить"]);
  assert.match(visibleText(problem), /Отправить ещё раз/);
});

test("action line: «Открыть» (in the app: a chevron that names the task) and «Отменить» are icons; «Отменить всё» under several lines is a word", () => {
  const line = (id) => ({
    type: "action", text: "Готово", detail: [], actionId: id, undo: { kind: "undo", label: "Отменить", busyLabel: "Отмена…", enabled: true, shown: true },
    undoneText: "Отменено", afterUndoOffer: null, assignment: { id: "a1", title: "Задача", subjectId: "", due: "", time: "" }, offer: null,
  });
  const one = request([line("x1")], { openAssignment: noop });
  assert.deepEqual(ariaLabels(one), ["Открыть задание: Задача", "Отменить"]);
  assert.match(one, /<use href="#i-chevr"><\/use>/, "opening something inside the app is a chevron, not the arrow out of a box");
  assert.doesNotMatch(one, /#i-open/);
  assert.equal(visibleText(one).includes("Отменить"), false);
  assert.equal(visibleText(one).includes("Открыть"), false);
  // An undo is the default colour: red is for deleting, stopping a recording and cancelling a download.
  assert.doesNotMatch(buttonTag(one, "Отменить"), /ia-danger/);
  assert.doesNotMatch(one, /ia-danger/);
  const two = request([line("x1"), line("x2")], { openAssignment: noop });
  assert.match(two, /class="btn ghost sm"[^>]*>Отменить всё</, "the bulk undo spends several results: a word");
  assert.equal(buttonTag(two, "Отменить всё"), null);
  // A started recording: the same place is a «Остановить» square, red.
  const started = line("x1");
  started.undo = { kind: "stop", label: "Остановить", busyLabel: "Остановка…", enabled: true, shown: true };
  const stop = request([started]);
  assert.match(stop, /<use href="#i-stop"><\/use>/);
  assert.match(buttonTag(stop, "Остановить"), /ia-danger/);
});

test("an undo offer is an icon in the default colour; the other offers are the card's one main action and keep their words", () => {
  const offer = (label, action) => ({ type: "offer", offer: { label, offer: { action, target: null } } });
  const undo = request([offer("Отменить", "undo_action")], { offer: noop });
  assert.ok(isOn(undo, "Отменить"));
  assert.doesNotMatch(buttonTag(undo, "Отменить"), /ia-danger/);
  assert.equal(visibleText(undo).includes("Отменить"), false);
  const record = request([offer("Начать запись", "start_recording")], { offer: noop });
  assert.deepEqual(ariaLabels(record), []);
  assert.match(visibleText(record), /Начать запись/);
});

test("a card that could not be read: the only way out is a word button, as before the icons", () => {
  const confirm = html(h(ConfirmCardPending, { error: "Не удалось загрузить", onReload: noop }));
  assert.deepEqual(ariaLabels(confirm), []);
  assert.match(confirm, /class="btn sm"[^>]*>Повторить</);
  const schedule = html(h(ScheduleCardPending, { error: "Не удалось загрузить", onReload: noop }));
  assert.deepEqual(ariaLabels(schedule), []);
  assert.match(schedule, /class="btn sm"[^>]*>Повторить</);
  // The error text stays as the reason; without a reload there is no button at all.
  assert.match(visibleText(schedule), /Не удалось загрузить/);
  assert.equal(count(html(h(ScheduleCardPending, { error: "Не удалось загрузить" })), "<button"), 0);
});

test("the note of a pending edit: «Вернуть текст сообщения» puts text into the field (insert), «Отменить правку» is a plain undo", () => {
  const out = html(h(RestartNote, { id: "n1", problem: "", onCancel: noop, onRestoreText: noop }));
  assert.deepEqual(ariaLabels(out), ["Вернуть текст сообщения", "Отменить правку"]);
  assert.match(out, /<use href="#i-insert"><\/use>/);
  assert.match(out, /<use href="#i-undo"><\/use>/);
  assert.doesNotMatch(out, /ia-danger|#i-x"/, "an undo is neither red nor a cross");
  assert.equal(html(h(RestartNote, { id: "n1", problem: "", onCancel: noop })).includes("insert"), false, "no restore, no insert glyph");
});

// ---- the work card and the turnkey cards ----

const writing = (over) => ({
  step: "none", generation: 1, topic: "Тема", sections: [], done: 0, total: 0, subjectId: "", materialId: "m1", result: "ok", job: "",
  presentation: false, template: "", questions: [], talkMaterialId: "", talkResult: "", lab: false, manual: "", placeholders: [], ...over,
});
const work = (over) => ({ id: "w1", status: "writing", version: 1, writing: null, problem: "", message: "", workType: "referat", ...over });
const library = { openMaterial: async () => ({ status: "opened", message: "" }) };
const api = {};
const body = (w, extra = {}) => html(h(WorkBody, { work: w, api, onWork: noop, library, ...extra }));

test("work card, done: «Доработать» and «Открыть в Word» keep words; the rest of the toolbar is icons", () => {
  const out = body(work({ writing: writing({ pdfMaterialId: "" }) }));
  assert.deepEqual(ariaLabels(out), ["Изменить план", "Скачать PDF"]);
  const words = visibleText(out);
  assert.match(words, /Доработать/);
  assert.match(words, /Открыть в Word/);
  assert.doesNotMatch(words, /Изменить план|Скачать PDF/);
  assert.match(out, /<use href="#i-pencil"><\/use>/);
  assert.match(out, /<use href="#i-down"><\/use>/);
  // A presentation has the text of the talk (and no PDF).
  const deck = body(work({ workType: "presentation", writing: writing({ presentation: true, talkMaterialId: "t1", talkResult: "ok" }) }));
  assert.deepEqual(ariaLabels(deck), ["Изменить план", "Открыть текст выступления"]);
  assert.match(visibleText(deck), /Открыть в PowerPoint/);
  assert.equal(visibleText(deck).includes("Открыть текст выступления"), false);
  assert.match(deck, /<use href="#i-doc"><\/use><\/svg><span class="ia-tip"[^>]*>Открыть текст выступления</);
  // A saved PDF is opened with its own icon.
  const saved = body(work({ writing: writing({ pdfMaterialId: "p1" }) }));
  assert.ok(isOn(saved, "Открыть PDF"));
  assert.match(saved, /<use href="#i-open"><\/use>/, "the PDF opens outside the app: the one place of the arrow out of a box");
  assert.equal(buttonTag(saved, "Скачать PDF"), null);
});

test("work card: the plan editor adds with an icon and saves with a word; a running step stops with an icon that spins while stopping", () => {
  const approve = body(work({ writing: writing({ step: "approve", result: "", sections: [{ number: 1, title: "Введение", words: 300, note: "", written: false, paragraphs: [], sources: [], kind: "", part: "", bullets: [], columns: [] }] }) }));
  assert.ok(ariaLabels(approve).includes("Добавить раздел"));
  assert.ok(!ariaLabels(approve).includes("Сохранить план"));
  assert.match(visibleText(approve), /Утвердить план/);
  assert.match(visibleText(approve), /Сохранить план/, "the form's own commit button keeps its words");
  assert.doesNotMatch(visibleText(approve), /Добавить раздел/);
  const running = work({ writing: writing({ step: "section", result: "", total: 2, active: true }) });
  assert.ok(isOn(body(running), "Остановить"));
  assert.equal(visibleText(body(running)).includes("Остановить"), false);
  const stopping = body(running, { stopping: true });
  assert.ok(isOff(stopping, "Остановка…"));
  assert.match(stopping, /ia-spin/);
});

test("work card, revise and edit modes end with the word «Отмена» (an inline form's own button)", () => {
  const code = src("agent/WorkCard.tsx");
  assert.equal(count(code, "{workTexts.cancel}"), 2);
  assert.doesNotMatch(code, /<IconAction[^>]*workTexts\.cancel/);
});

test("turnkey cards: the edit of the understanding is a pencil, its save and cancel and the questions skip are words", () => {
  const card = { workType: "referat", volume: "10 страниц", fileFormat: "docx", formatting: "ГОСТ", variant: "", deadline: "", sources: "" };
  const w = { id: "w1", status: "review", version: 1, writing: null, workType: "referat", problem: "", message: "", questionRound: 0, questions: [], hasCard: true, card, edited: [], feasibility: "", feasibilityReason: "", partial: false, assumptions: [], helper: null, answered: [] };
  const ctx = { work: w, api: {}, busy: false, run: async () => undefined, step: async () => undefined, onWork: noop, errorLine: null };
  const view = html(h(UnderstandingCard, { ctx }));
  assert.ok(isOn(view, "Изменить"));
  assert.match(visibleText(view), /Подтвердить/);
  assert.doesNotMatch(visibleText(view), /Изменить/);
  const editing = html(h(UnderstandingCard, { ctx, startEditing: true }));
  assert.deepEqual(ariaLabels(editing), []);
  assert.match(editing, /class="btn sm"[^>]*>Сохранить</);
  assert.match(editing, /class="btn ghost sm"[^>]*>Отмена</);
  const questions = html(h(WorkBody, { work: { ...w, status: "questions", questionRound: 1, questions: [{ kind: "text", text: "Тема?", options: [], default: 0, defaultText: "", why: "" }] }, api: {}, onWork: noop }));
  assert.equal(buttonTag(questions, "Пропустить вопросы"), null);
  assert.match(questions, /class="btn ghost sm"[^>]*>Пропустить вопросы</);
  assert.match(visibleText(questions), /Ответить/);
  const helper = html(h(WorkBody, { work: { ...w, status: "helper", helper: { steps: ["Шаг"], submit: "", pitfalls: [], readyParts: [], placeholders: [] } }, api: {}, onWork: noop }));
  assert.ok(isOn(helper, "Спросить по шагу"));
  assert.match(buttonTag(helper, "Спросить по шагу"), /aria-expanded="false"/);
  assert.equal(visibleText(helper).includes("Спросить по шагу"), false);
});

// ---- the source: no word buttons left in the small places of the panel ----

// The word buttons that remain in src/agent, each with the reason: the main action of its card, a yes/no pair, a dialog button,
// an inline form's own buttons («Пропустить», «Сохранить», «Отмена»), the only way out of an error («Повторить» of a card that
// could not be read), and the bulk «Отменить всё» (it spends several results).
const wordButtons = new Map([
  ["agent/AgentCards.tsx", ["offer.label", "b.label", "texts.reviseSend", "card.submit", "card.skip", "b.label", "texts.undoAll"]],
  ["agent/ConfirmCard.tsx", ["confirmTexts.openAssignment", "confirmTexts.openTasks", "m.noLabel", "m.yesLabel", "confirmTexts.reload"]],
  ["agent/ScheduleCard.tsx", ["scheduleTexts.reload", "scheduleTexts.openSubjects", "scheduleTexts.no", "main.label", "scheduleTexts.retry"]],
  [
    "agent/TurnkeyCards.tsx",
    ["props.problem === \"\" ? turnkeyTexts.triageStart : turnkeyTexts.retry", "turnkeyTexts.answer", "turnkeyTexts.skip", "turnkeyTexts.confirm", "turnkeyTexts.save", "turnkeyTexts.cancel", "turnkeyTexts.askSend"],
  ],
  [
    "agent/WorkCard.tsx",
    ["model.button", "workTexts.retry", "model.button", "workTexts.approve", "workTexts.savePlan", "model.button", "workTexts.revise", "openLabel(work)", "workTexts.reviseSubmit", "workTexts.cancel", "workTexts.rewrite", "workTexts.cancel"],
  ],
  ["agent/WorkSend.tsx", ["note.sent ? sendWorkTexts.again : sendWorkTexts.button"]],
  ["agent/RestartDialog.tsx", ["restartTexts.hide", "props.fixBusy ? (block.fix.kind === \"stop\" ? restartTexts.stopping : restartTexts.cancelling) : block.fix.label"]],
  ["agent/ContextRing.tsx", ["ringTexts.hintButton"]],
]);

test("every <Button> that still shows words in src/agent is a main action, a yes/no half or a dialog button (the list is the decision)", () => {
  const found = new Map();
  for (const file of readdirSync(new URL("../src/agent/", import.meta.url)).filter((f) => f.endsWith(".tsx"))) {
    const code = src(`agent/${file}`);
    const exprs = [...code.matchAll(/<Button\b[\s\S]*?\{([^{}]+)\}\s*<\/Button>/g)].map((m) => m[1].trim());
    if (exprs.length > 0) found.set(`agent/${file}`, exprs);
  }
  assert.deepEqual(Object.fromEntries(found), Object.fromEntries(wordButtons));
});

test("the converted places draw no raw <button> with words: the secondary buttons go through IconAction", () => {
  for (const file of ["AgentCards.tsx", "WorkCard.tsx", "TurnkeyCards.tsx", "ScheduleCard.tsx", "MentionParts.tsx", "RestartDialog.tsx"]) {
    const code = src(`agent/${file}`);
    assert.ok(count(code, "<IconAction") > 0, file);
  }
  // The words of the old buttons are not hard-coded in the markup: they come from the *View.ts text modules.
  for (const file of readdirSync(new URL("../src/agent/", import.meta.url)).filter((f) => f.endsWith(".tsx"))) {
    const code = src(`agent/${file}`);
    for (const m of code.matchAll(/<IconAction\b[\s\S]*?\/>/g)) {
      assert.doesNotMatch(m[0], /label="/, `${file}: the label of an icon button comes from a text module`);
    }
  }
});
