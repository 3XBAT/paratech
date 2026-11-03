// TASK-370: «Подключиться» in the recording popup. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { methods, parsePromptCallSubject, promptApi } from "../.test-build/bindings/prompt.js";
import { methods as libraryMethods } from "../.test-build/bindings/library.js";
import { callLinkTexts } from "../.test-build/callLinkView.js";
import { idleJoin, JoinController, joinPromptId, joinTexts, joinView } from "../.test-build/popup/joinView.js";

const src = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");

/** A promise that the test settles by hand. */
function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/** Lets the pending microtasks run. */
const settle = () => new Promise((r) => setImmediate(r));

function setup({ subject = () => Promise.resolve("sub-1"), open = () => Promise.resolve({ problem: "" }) } = {}) {
  const reads = [];
  const opens = [];
  const states = [];
  const ctl = new JoinController(
    {
      callSubject: (id) => {
        reads.push(id);
        return subject(id);
      },
      open: (id) => {
        opens.push(id);
        return open(id);
      },
    },
    (s) => states.push(s),
  );
  return { ctl, reads, opens, states };
}

// ---- the binding ----

test("callSubject is bound to PromptService.CallSubject and returns the subject ID", async () => {
  const calls = [];
  const api = promptApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { subjectId: "sub-1" };
  });
  assert.equal(methods.callSubject, "paratech/desktop/internal/input/wails.PromptService.CallSubject");
  assert.equal(await api.callSubject("p1"), "sub-1");
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.PromptService.CallSubject", "p1"]]);
  assert.equal(await promptApi(async () => ({ subjectId: "" })).callSubject("p1"), "");
});

test("parsePromptCallSubject is strict about the shape and ignores extra fields", () => {
  assert.deepEqual(parsePromptCallSubject({ subjectId: "s" }), { subjectId: "s" });
  assert.deepEqual(parsePromptCallSubject({ subjectId: "", link: "https://x.test", host: "x.test" }), { subjectId: "" }, "extra fields are dropped");
  for (const bad of [null, undefined, "s", 1, [], {}, { subjectId: 1 }, { subjectId: null }, { subject_id: "s" }]) {
    assert.throws(() => parsePromptCallSubject(bad), undefined, JSON.stringify(bad));
  }
});

test("a malformed reply or a failed call rejects (the popup shows no button then)", async () => {
  await assert.rejects(promptApi(async () => ({ subjectId: 5 })).callSubject("p1"));
  await assert.rejects(
    promptApi(async () => {
      throw new Error("down");
    }).callSubject("p1"),
  );
  const { ctl, states } = setup({ subject: () => Promise.reject(new Error("down")) });
  ctl.offer("p1");
  await settle();
  assert.equal(ctl.state.phase, "hidden");
  assert.equal(joinView(ctl.state, false).visible, false);
  assert.ok(states.length > 0);
});

// ---- the state machine ----

test("an offered lesson with a link shows the button ready; no link or no subject shows nothing, not even a hint", async () => {
  const withLink = setup();
  withLink.ctl.offer("p1");
  await settle();
  assert.deepEqual(withLink.reads, ["p1"]);
  assert.deepEqual(withLink.ctl.state, { shown: true, phase: "ready", subjectId: "sub-1", problem: "" });
  const v = joinView(withLink.ctl.state, false);
  assert.deepEqual(v, { visible: true, busy: false, disabled: false, opened: false, title: callLinkTexts.joinTitle, confirm: "", failure: "", note: "" });

  const none = setup({ subject: () => Promise.resolve("") });
  none.ctl.offer("p1");
  await settle();
  assert.deepEqual(none.ctl.state, { shown: true, phase: "hidden", subjectId: "", problem: "" });
  assert.deepEqual(joinView(none.ctl.state, false), { visible: false, busy: false, disabled: true, opened: false, title: callLinkTexts.joinTitle, confirm: "", failure: "", note: "" });
});

test("the prompt is read only for an offered lesson: not for ad-hoc, STARTING, other states, the recording or the advice", () => {
  const lesson = { id: "p1", kind: "lesson", state: "OFFERED" };
  assert.equal(joinPromptId(lesson, false), "p1");
  assert.equal(joinPromptId(null, false), null);
  assert.equal(joinPromptId({ ...lesson, kind: "adhoc" }, false), null);
  for (const state of ["STARTING", "STARTED", "SKIPPED", "EXPIRED"]) {
    assert.equal(joinPromptId({ ...lesson, state }, false), null, state);
  }
  assert.equal(joinPromptId(lesson, true), null, "the recording or advice view");

  const { ctl, reads } = setup();
  ctl.offer(null);
  assert.deepEqual(reads, [], "nothing is asked without an offered lesson");
  assert.equal(ctl.state.shown, false);
});

test("a click opens the meeting through the open call with the subject ID, shows busy, then stays opened (grey, disabled)", async () => {
  const open = deferred();
  const { ctl, opens } = setup({ open: () => open.promise });
  ctl.offer("p1");
  await settle();
  const click = ctl.click();
  assert.equal(ctl.state.phase, "opening");
  let v = joinView(ctl.state, false);
  assert.equal(v.busy, true);
  assert.equal(v.disabled, true);
  open.resolve({ problem: "" });
  await click;
  assert.deepEqual(opens, ["sub-1"]);
  assert.equal(ctl.state.phase, "opened");
  v = joinView(ctl.state, false);
  assert.deepEqual(
    [v.visible, v.busy, v.disabled, v.opened, v.title, v.confirm, v.failure, v.note],
    [true, false, true, true, joinTexts.openedTitle, "Ссылка открыта. Запись пока не идёт.", "", ""],
  );
  await ctl.click();
  assert.deepEqual(opens, ["sub-1"], "no second open once opened");
});

test("two quick clicks open the meeting once", async () => {
  const open = deferred();
  const { ctl, opens } = setup({ open: () => open.promise });
  ctl.offer("p1");
  await settle();
  const first = ctl.click();
  const second = ctl.click();
  open.resolve({ problem: "" });
  await Promise.all([first, second]);
  assert.equal(opens.length, 1);
});

test("a failed or thrown open shows the failure text and the button is active again; a retry works", async () => {
  for (const fail of [() => Promise.resolve({ problem: "failed" }), () => Promise.reject(new Error("x"))]) {
    let next = fail;
    const { ctl, opens } = setup({ open: () => next() });
    ctl.offer("p1");
    await settle();
    await ctl.click();
    assert.equal(ctl.state.phase, "ready");
    const v = joinView(ctl.state, false);
    assert.equal(v.visible, true);
    assert.equal(v.disabled, false);
    assert.equal(v.failure, "Не удалось открыть ссылку. Можно повторить.");
    assert.equal(v.confirm, "", "no confirmation after a failure");
    next = () => Promise.resolve({ problem: "" });
    await ctl.click();
    assert.equal(opens.length, 2);
    assert.equal(ctl.state.phase, "opened");
    assert.equal(joinView(ctl.state, false).failure, "", "the text goes with the new attempt");
  }
});

test("no_link and not_found remove the button and say the link is gone, without offering to add one", async () => {
  for (const problem of ["no_link", "not_found"]) {
    const { ctl } = setup({ open: () => Promise.resolve({ problem }) });
    ctl.offer("p1");
    await settle();
    await ctl.click();
    const v = joinView(ctl.state, false);
    assert.equal(v.visible, false);
    assert.equal(v.note, "Ссылки на пару уже нет.");
    assert.equal(v.failure, "");
    assert.equal(v.confirm, "");
    assert.doesNotMatch(v.note, /Добавить/);
    await ctl.click();
    assert.equal(ctl.state.phase, "hidden", "nothing to click");
  }
});

test("an unknown open problem counts as a failure that can be repeated", async () => {
  const { ctl } = setup({ open: () => Promise.resolve({ problem: "weird" }) });
  ctl.offer("p1");
  await settle();
  await ctl.click();
  assert.equal(ctl.state.phase, "ready");
  assert.equal(ctl.state.problem, "failed");
});

test("the popup closing resets the opened state and the texts; the next showing offers the button anew", async () => {
  const { ctl, reads, opens } = setup();
  ctl.offer("p1");
  await settle();
  await ctl.click();
  assert.equal(ctl.state.phase, "opened");
  ctl.closed();
  assert.equal(ctl.state.phase, "hidden");
  assert.equal(ctl.state.problem, "");
  ctl.refresh();
  await settle();
  assert.deepEqual(reads, ["p1", "p1"], "the core showed the popup again: read again");
  assert.equal(ctl.state.phase, "ready");
  await ctl.click();
  assert.equal(opens.length, 2);
});

test("the same prompt shown again: closed() then the refresh on visible brings the button back, ready and not opened", async () => {
  const { ctl, reads, opens } = setup();
  ctl.offer("p1");
  await settle();
  await ctl.click();
  assert.equal(ctl.state.phase, "opened");
  ctl.closed(); // visibilitychange: hidden
  ctl.refresh(); // visibilitychange: visible (no new offer, the prompt id is the same)
  await settle();
  assert.deepEqual(reads, ["p1", "p1"]);
  assert.deepEqual(ctl.state, { shown: true, phase: "ready", subjectId: "sub-1", problem: "" });
  assert.equal(joinView(ctl.state, false).confirm, "");
  await ctl.click();
  assert.equal(opens.length, 2, "it can be opened again in the new showing");
});

test("a read while the popup stays open does not undo the opened state, and a changed subject starts clean", async () => {
  let subject = "sub-1";
  const { ctl, opens } = setup({ subject: () => Promise.resolve(subject) });
  ctl.offer("p1");
  await settle();
  await ctl.click();
  ctl.refresh();
  await settle();
  assert.equal(ctl.state.phase, "opened");
  subject = "sub-2";
  ctl.refresh();
  await settle();
  assert.deepEqual([ctl.state.phase, ctl.state.subjectId], ["ready", "sub-2"]);
  await ctl.click();
  assert.deepEqual(opens, ["sub-1", "sub-2"]);
});

test("a new prompt id resets the state; the same offer coming back (a failed start) keeps the opened state", async () => {
  const subjects = { p1: "sub-1", p2: "sub-2" };
  const { ctl, reads } = setup({ subject: (id) => Promise.resolve(subjects[id] ?? "") });
  ctl.offer("p1");
  await settle();
  await ctl.click();
  ctl.offer(null);
  assert.equal(joinView(ctl.state, false).visible, false, "STARTING: no button");
  ctl.offer("p1");
  assert.equal(ctl.state.phase, "opened", "back at once, still opened");
  await settle();
  assert.equal(ctl.state.phase, "opened");
  ctl.offer("p2");
  assert.deepEqual(ctl.state, { shown: true, phase: "hidden", subjectId: "", problem: "" }, "clean until the read answers");
  await settle();
  assert.deepEqual([ctl.state.phase, ctl.state.subjectId], ["ready", "sub-2"]);
  assert.deepEqual(reads, ["p1", "p1", "p2"]);
});

test("only the newest read is applied", async () => {
  const slow = deferred();
  const fast = deferred();
  const replies = [slow.promise, fast.promise];
  const { ctl } = setup({ subject: () => replies.shift() });
  ctl.offer("p1");
  ctl.refresh();
  fast.resolve("sub-new");
  await settle();
  assert.equal(ctl.state.subjectId, "sub-new");
  slow.resolve("sub-old");
  await settle();
  assert.equal(ctl.state.subjectId, "sub-new", "the late older reply is dropped");

  // A reply for the previous prompt never reaches the next one.
  const old = deferred();
  const next = deferred();
  const queue = [old.promise, next.promise];
  const other = setup({ subject: () => queue.shift() });
  other.ctl.offer("p1");
  other.ctl.offer("p2");
  old.resolve("sub-p1");
  next.resolve("sub-p2");
  await settle();
  assert.equal(other.ctl.state.subjectId, "sub-p2");
});

test("an open that ends after the popup closed or the prompt changed no longer touches the state", async () => {
  const open = deferred();
  const { ctl } = setup({ open: () => open.promise });
  ctl.offer("p1");
  await settle();
  const click = ctl.click();
  ctl.closed();
  open.resolve({ problem: "" });
  await click;
  assert.equal(ctl.state.phase, "hidden");

  const open2 = deferred();
  const subjects = { p1: "sub-1", p2: "sub-2" };
  const two = setup({ subject: (id) => Promise.resolve(subjects[id]), open: () => open2.promise });
  two.ctl.offer("p1");
  await settle();
  const click2 = two.ctl.click();
  two.ctl.offer("p2");
  await settle();
  open2.resolve({ problem: "failed" });
  await click2;
  assert.deepEqual([two.ctl.state.phase, two.ctl.state.subjectId, two.ctl.state.problem], ["ready", "sub-2", ""]);
});

test("a link that vanished while the popup was open hides the button on the next read; the note of a gone link stays until cleared", async () => {
  let subject = "sub-1";
  const { ctl } = setup({ subject: () => Promise.resolve(subject) });
  ctl.offer("p1");
  await settle();
  subject = "";
  ctl.refresh();
  await settle();
  assert.equal(joinView(ctl.state, false).visible, false);

  let linked = true;
  const gone = setup({
    subject: () => Promise.resolve(linked ? "sub-1" : ""),
    open: () => {
      linked = false;
      return Promise.resolve({ problem: "no_link" });
    },
  });
  gone.ctl.offer("p1");
  await settle();
  await gone.ctl.click();
  gone.ctl.refresh();
  await settle();
  assert.equal(joinView(gone.ctl.state, false).visible, false);
  assert.equal(joinView(gone.ctl.state, false).note, joinTexts.gone, "a read that finds no link keeps the note");
  gone.ctl.clearProblem();
  assert.equal(joinView(gone.ctl.state, false).note, "");
});

// ---- placement and disabled rules ----

test("a running start or skip disables the button but nothing here disables «Начать запись»", () => {
  const ready = { shown: true, phase: "ready", subjectId: "s", problem: "" };
  assert.equal(joinView(ready, false).disabled, false);
  assert.equal(joinView(ready, true).disabled, true);
  assert.equal(joinView(ready, true).visible, true, "still in the row");
  assert.equal(joinView(idleJoin, false).visible, false);
  // Texts are never shown without an offer.
  assert.equal(joinView({ shown: false, phase: "hidden", subjectId: "", problem: "gone" }, false).note, "");
  assert.equal(joinView({ shown: false, phase: "ready", subjectId: "s", problem: "failed" }, false).failure, "");
  assert.equal(joinView({ shown: false, phase: "opened", subjectId: "s", problem: "" }, false).confirm, "");
});

test("the confirmation is shown only while the meeting is open", () => {
  assert.equal(joinTexts.opened, "Ссылка открыта. Запись пока не идёт.");
  for (const phase of ["hidden", "ready", "opening"]) {
    assert.equal(joinView({ shown: true, phase, subjectId: "s", problem: "" }, false).confirm, "", phase);
  }
  const opened = joinView({ shown: true, phase: "opened", subjectId: "s", problem: "" }, false);
  assert.equal(opened.confirm, joinTexts.opened);
  assert.equal(opened.title, joinTexts.openedTitle, "the hover text stays");
  assert.equal(opened.disabled, true, "still grey and not clickable");
});

test("the buttons: «Начать запись» full width on top, then «Подключиться» and «Не записывать» side by side; no join keeps the old look", () => {
  const code = src("popup/Popup.tsx");
  const row = code.slice(code.lastIndexOf('<div className="pbtns">'));
  const start = row.indexOf("m.buttons.map");
  const join = row.indexOf("{id === \"start\" && joinShown && (");
  assert.ok(start >= 0 && join > start, "the join button is drawn right after the start button, before the skip one");
  assert.match(row, /callLinkTexts\.join\}\s*<\/Button>/);
  assert.match(row, /title=\{jv\.title\}/, "the hint is callLinkTexts.joinTitle (via joinView)");
  assert.match(row, /busyLabel=\{callLinkTexts\.joining\}/);
  assert.match(row, /disabled=\{jv\.disabled\}/);
  assert.match(row, /disabled=\{m\.disabled\}/, "start and skip still follow the old rule");
  assert.match(row, /className=\{id === "start" \|\| id === "close" \? "wide" : undefined\}/, "only start and close are full width, as before");
  assert.doesNotMatch(row.slice(join), /wide/, "the join button is not full width");
  assert.doesNotMatch(row, /autoFocus|\.focus\(/, "no focus is taken");
  const view = src("popup/view.ts");
  assert.match(view, /buttons: ButtonId\[\] = onOffer \? \["start", "skip"\] : \["close"\]/, "view.ts buttons are unchanged");
  // Without the join button «Не записывать» is the second and last child and spans the row, as before; with it, the
  // third child does not match and takes the second column.
  const css = src("popup/popup.css");
  assert.match(css, /\.pbtns:not\(\.two\) > \.btn:nth-child\(2\):last-child \{ grid-column: 1 \/ -1; \}/);
  assert.match(css, /\.pbtns \{ display: grid; grid-template-columns: 1fr 1fr;/);
  assert.match(css, /\.btn\.joined:disabled \{[^}]*\}/);
  assert.doesNotMatch(css.match(/\.btn\.joined:disabled \{[^}]*\}/)[0], /opacity/, "the grey button fades like every disabled one");
  assert.equal(callLinkTexts.join, "Подключиться");
  assert.equal(callLinkTexts.joining, "Открытие…");
});

test("the red banner carries only the page error and the start problem; the join texts are quiet lines in the texts area", () => {
  const code = src("popup/Popup.tsx");
  assert.match(code, /const errorLine = error \|\| m\.problem;/);
  assert.doesNotMatch(code, /jv\.error/);
  // The notes count in the layout, the failure and the opened note too, so the buttons stay uncut.
  assert.match(code, /\[picker\.autoNote, \.\.\.picker\.warnings, \.\.\.joinQuiet, joinFailure\]/);
  assert.match(code, /textsLayout\(notes, errorLine\)/);
  assert.match(code, /const joinQuiet = showPicker \? \[jv\.confirm, jv\.note\]/);
  // Quiet: p-note (muted), the confirmation is a status; the failure is the amber warning, last in the area.
  const area = code.slice(code.indexOf('<div className="texts">\n          {errorLine'), code.indexOf('<div className="pbtns">\n        {m.buttons'));
  assert.match(area, /joinQuiet\.map/);
  assert.match(area, /"p-note join-note clamp" : "p-note join-note"/);
  assert.match(area, /role=\{n === jv\.confirm \? "status" : undefined\}/);
  assert.doesNotMatch(area.slice(area.indexOf("joinQuiet.map"), area.indexOf("{showPicker && (\n            <p\n              className={layout.collapsed")), /warning/, "the quiet lines are not amber");
  const failure = area.indexOf("{joinFailure !== \"\" && (");
  assert.ok(failure > area.indexOf("picker.advice"), "the failure is the last item, below the advice");
  assert.match(area.slice(failure), /className=\{layout\.clamp \? "warning clamp" : "warning"\}/);
  assert.doesNotMatch(area.slice(failure), /Banner|alert/);
  assert.equal(area.indexOf("Banner kind=\"fail\"") < area.indexOf("joinQuiet.map"), true);
  assert.doesNotMatch(area.slice(area.indexOf("Banner kind"), area.indexOf("</Banner>")), /join/i, "nothing of the join reaches the banner");
});

test("the visibility handler: hidden closes the join state, visible reads the subject again", () => {
  const code = src("popup/Popup.tsx");
  assert.match(
    code,
    /if \(document\.visibilityState === "hidden"\) \{\s*joinCtl\.closed\(\);\s*\} else \{\s*joinCtl\.refresh\(\);\s*\}/,
  );
});

// ---- the core is never given a URL, the page opens none ----

test("the page opens the meeting only through the library binding by subject ID; Popup.tsx opens no URL", () => {
  assert.equal(libraryMethods.openSubjectCallLink.endsWith(".OpenSubjectCallLink"), true);
  const code = src("popup/Popup.tsx");
  assert.match(code, /open: library\.openSubjectCallLink/);
  assert.match(code, /callSubject: api\.callSubject/);
  for (const bad of [/window\.open/, /<a[\s>]/, /href=/, /location\./, /\bfetch\(/, /openUrl|OpenURL|BrowserOpen/]) {
    assert.doesNotMatch(code, bad, String(bad));
  }
  const logic = src("popup/joinView.ts");
  for (const bad of [/window\./, /document\./, /https?:/, /href/]) {
    assert.doesNotMatch(logic.replace(/\/\/.*$/gm, ""), bad, String(bad));
  }
  // The binding of the popup sends only the prompt id and reads only a subject ID.
  const bindings = src("bindings/prompt.ts");
  assert.match(bindings, /call\(methods\.callSubject, id\)/);
});
