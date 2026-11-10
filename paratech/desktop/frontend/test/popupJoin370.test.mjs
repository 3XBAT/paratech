// TASK-370: «Подключиться» in the recording popup. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { methods, parsePromptCallSubject, promptApi } from "../.test-build/bindings/prompt.js";
import { methods as libraryMethods } from "../.test-build/bindings/library.js";
import { callLinkTexts } from "../.test-build/callLinkView.js";
import {
  idleJoin,
  JoinController,
  joinOnOwnRow,
  joinPromptId,
  joinTexts,
  joinView,
  StartedRecording,
  startedPromptId,
} from "../.test-build/popup/joinView.js";
import { dotClass, layoutNotes, textsLayout, topClass } from "../.test-build/popup/view.js";
import { adviceTexts } from "../.test-build/sourcePicker/view.js";

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
  // TASK-406 added `label` and `live` to the view.
  assert.deepEqual(v, { visible: true, busy: false, disabled: false, opened: false, label: "Подключиться", title: callLinkTexts.joinTitle, confirm: "", failure: "", note: "", live: true });

  const none = setup({ subject: () => Promise.resolve("") });
  none.ctl.offer("p1");
  await settle();
  assert.deepEqual(none.ctl.state, { shown: true, phase: "hidden", subjectId: "", problem: "" });
  assert.deepEqual(joinView(none.ctl.state, false), { visible: false, busy: false, disabled: true, opened: false, label: "Подключиться", title: callLinkTexts.joinTitle, confirm: "", failure: "", note: "", live: false });
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

// TASK-406 (owner 2026-11-08, answer 11): once the meeting is open the button is «Открыть ещё раз», active, not grey.
test("a click opens the meeting through the open call with the subject ID, shows busy, then «Открыть ещё раз» stays clickable and opens again", async () => {
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
    [v.visible, v.busy, v.disabled, v.opened, v.label, v.title, v.confirm, v.failure, v.note],
    [true, false, false, true, "Открыть ещё раз", callLinkTexts.joinAgainTitle, "Ссылка открыта. Запись пока не идёт.", "", ""],
  );
  assert.equal(joinView(ctl.state, true).disabled, true, "a start that runs still blocks it");

  // Again: the same path (the subject ID); the busy state shows at once, then it is opened once more.
  const second = ctl.click();
  assert.equal(ctl.state.phase, "opening");
  assert.equal(joinView(ctl.state, false).busy, true);
  await second;
  assert.deepEqual(opens, ["sub-1", "sub-1"], "the same subject ID is opened the second time");
  assert.equal(ctl.state.phase, "opened");
  assert.equal(joinView(ctl.state, false).label, "Открыть ещё раз");
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

// TASK-406: the core hides the popup right after the start, so a closing must not forget what was opened for the same
// prompt: the running view says «Открыть ещё раз». Changed from "closing resets the opened state" (TASK-370).
test("the popup closing keeps what was opened for the same prompt and drops only the failure text; the next showing asks again", async () => {
  const { ctl, reads, opens } = setup();
  ctl.offer("p1");
  await settle();
  await ctl.click();
  assert.equal(ctl.state.phase, "opened");
  ctl.closed();
  assert.equal(ctl.state.phase, "opened");
  ctl.refresh();
  await settle();
  assert.deepEqual(reads, ["p1", "p1"], "the core showed the popup again: read again");
  assert.equal(ctl.state.phase, "opened");
  await ctl.click();
  assert.equal(opens.length, 2, "and it can be opened again");

  const failing = setup({ open: () => Promise.resolve({ problem: "failed" }) });
  failing.ctl.offer("p1");
  await settle();
  await failing.ctl.click();
  assert.equal(joinView(failing.ctl.state, false).failure, callLinkTexts.openFailed);
  failing.ctl.closed();
  assert.equal(failing.ctl.state.problem, "");
  assert.equal(failing.ctl.state.phase, "ready", "the button is still there");
});

test("the same prompt shown again: closed() then the refresh on visible brings the button back, still «Открыть ещё раз»; a removed link takes it away", async () => {
  let subject = "sub-1";
  const { ctl, reads, opens } = setup({ subject: () => Promise.resolve(subject) });
  ctl.offer("p1");
  await settle();
  await ctl.click();
  ctl.closed(); // visibilitychange: hidden
  ctl.refresh(); // visibilitychange: visible (no new offer, the prompt id is the same)
  await settle();
  assert.deepEqual(reads, ["p1", "p1"]);
  assert.deepEqual(ctl.state, { shown: true, phase: "opened", subjectId: "sub-1", problem: "" });
  assert.equal(joinView(ctl.state, false).label, "Открыть ещё раз");
  await ctl.click();
  assert.equal(opens.length, 2, "it can be opened again in the new showing");

  subject = "";
  ctl.closed();
  ctl.refresh();
  await settle();
  assert.equal(joinView(ctl.state, false).visible, false, "the link was removed meanwhile: no button");
});

test("«Открыть ещё раз» keeps its label when the second open fails, and double click on it opens once", async () => {
  let reply = () => Promise.resolve({ problem: "" });
  const { ctl, opens } = setup({ open: () => reply() });
  ctl.offer("p1");
  await settle();
  await ctl.click();
  reply = () => Promise.resolve({ problem: "failed" });
  await ctl.click();
  assert.equal(ctl.state.phase, "opened", "the first open still stands");
  const v = joinView(ctl.state, false);
  assert.deepEqual([v.label, v.disabled, v.failure, v.confirm], ["Открыть ещё раз", false, callLinkTexts.openFailed, ""], "active, the failure line, no stale confirmation");
  assert.equal(joinView(ctl.state, false).live, true);

  const slow = deferred();
  reply = () => slow.promise;
  const a = ctl.click();
  const b = ctl.click();
  slow.resolve({ problem: "" });
  await Promise.all([a, b]);
  assert.equal(opens.length, 3, "one open for the double click");
  assert.equal(ctl.state.phase, "opened");
  assert.equal(joinView(ctl.state, false).failure, "");

  // The link is gone on the second open: the button goes with a note.
  reply = () => Promise.resolve({ problem: "no_link" });
  await ctl.click();
  const gone = joinView(ctl.state, false);
  assert.deepEqual([gone.visible, gone.note, gone.live], [false, joinTexts.gone, true], "the note comes inside the live region that was there");
});

// Changed in the TASK-406 review: while the recording runs the confirmation is the short «Ссылка открыта.» (it was "").
test("while the recording runs the confirmation does not say it is not going; the hover texts do not say that the recording will not start", async () => {
  const { ctl } = setup();
  ctl.offer("p1");
  await settle();
  await ctl.click();
  const before = joinView(ctl.state, false, false);
  const during = joinView(ctl.state, false, true);
  assert.equal(before.confirm, joinTexts.opened);
  assert.equal(during.confirm, "Ссылка открыта.");
  assert.equal(during.confirm, callLinkTexts.opened);
  assert.doesNotMatch(during.confirm, /Запись пока не идёт/);
  assert.equal(before.title, callLinkTexts.joinAgainTitle);
  assert.equal(during.title, "Открыть ссылку на пару ещё раз");
  assert.doesNotMatch(during.title, /запись не начнётся/);
  assert.equal(joinView({ shown: true, phase: "ready", subjectId: "s", problem: "" }, false, true).title, "Открыть ссылку на пару");
  assert.doesNotMatch(joinView({ shown: true, phase: "ready", subjectId: "s", problem: "" }, false, true).title, /запись не начнётся/);
  assert.equal(joinView({ shown: true, phase: "ready", subjectId: "s", problem: "" }, false, true).label, "Подключиться");
  assert.equal(during.label, "Открыть ещё раз");
});

test("opened for a prompt, the recording starts from it: the opened state survives the start; another prompt starts clean", async () => {
  const subjects = { p1: "sub-1", p2: "sub-2" };
  const { ctl, reads } = setup({ subject: (id) => Promise.resolve(subjects[id] ?? "") });
  ctl.offer("p1");
  await settle();
  await ctl.click();
  // «Начать запись»: no offer any more (STARTED), the popup closes, then the running view asks with the same ID.
  ctl.offer(null);
  ctl.closed();
  assert.equal(joinView(ctl.state, false, true).visible, false, "nothing shown until the recording is seen");
  ctl.offer("p1");
  ctl.refresh();
  await settle();
  assert.deepEqual(reads, ["p1", "p1", "p1"], "the core is asked about the started prompt");
  const v = joinView(ctl.state, false, true);
  // Changed in the TASK-406 review: the running view confirms the open with «Ссылка открыта.» (it was "").
  assert.deepEqual([v.visible, v.label, v.confirm, v.disabled], [true, "Открыть ещё раз", "Ссылка открыта.", false]);

  // The recording is over: nothing is asked for it.
  ctl.offer(null);
  assert.equal(joinView(ctl.state, false, true).visible, false);
  ctl.offer("p2");
  assert.equal(ctl.state.phase, "hidden", "a different prompt starts clean");
  await settle();
  assert.deepEqual([ctl.state.phase, ctl.state.subjectId], ["ready", "sub-2"]);
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

// TASK-406: an open that ends after the popup closed still counts (the core hides the popup right after the start, and the
// meeting did open); only a changed prompt drops it. Changed from "closed: hidden" (TASK-370).
test("an open that ends after the popup closed is kept; one that ends after the prompt changed no longer touches the state", async () => {
  const open = deferred();
  const { ctl } = setup({ open: () => open.promise });
  ctl.offer("p1");
  await settle();
  const click = ctl.click();
  ctl.closed();
  open.resolve({ problem: "" });
  await click;
  assert.equal(ctl.state.phase, "opened");

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

// Changed in TASK-406: the opened button is no longer grey (disabled) and its hover text is «Открыть ссылку на пару ещё раз
// (запись не начнётся)» instead of «Ссылка уже открыта».
test("the confirmation is shown only while the meeting is open and the recording has not started", () => {
  assert.equal(joinTexts.opened, "Ссылка открыта. Запись пока не идёт.");
  for (const phase of ["hidden", "ready", "opening"]) {
    assert.equal(joinView({ shown: true, phase, subjectId: "s", problem: "" }, false).confirm, "", phase);
  }
  const opened = joinView({ shown: true, phase: "opened", subjectId: "s", problem: "" }, false);
  assert.equal(opened.confirm, joinTexts.opened);
  assert.equal(opened.title, callLinkTexts.joinAgainTitle);
  assert.equal(opened.disabled, false, "active, not a grey dead end");
  assert.equal(opened.label, "Открыть ещё раз");
  // Changed in the TASK-406 review: never «Запись пока не идёт» while the recording runs, but «Ссылка открыта.».
  const running = (s) => joinView({ shown: true, subjectId: "s", problem: "", ...s }, false, true);
  assert.equal(running({ phase: "opened" }).confirm, "Ссылка открыта.");
  assert.equal(running({ phase: "opened", problem: "failed" }).confirm, "", "a failed open keeps only the amber line");
  assert.equal(running({ phase: "ready" }).confirm, "", "nothing opened, nothing confirmed");
  assert.equal(running({ phase: "opened", shown: false }).confirm, "");
  assert.equal(joinTexts.openedTitle, undefined, "the grey-button hover text is gone");
});

test("the live regions exist as soon as the button does, and stay for the gone note", () => {
  const live = (s) => joinView({ shown: true, subjectId: "s", problem: "", ...s }, false).live;
  assert.equal(live({ phase: "ready" }), true, "drawn (empty) before any text");
  assert.equal(live({ phase: "opening" }), true);
  assert.equal(live({ phase: "opened" }), true);
  assert.equal(live({ phase: "hidden" }), false, "no link, never a button: nothing to announce");
  assert.equal(live({ phase: "hidden", problem: "gone" }), true, "the gone note arrives inside the region");
  assert.equal(live({ phase: "ready", shown: false }), false);
});

test("the popup texts: «Открыть ещё раз» and its hover texts", () => {
  assert.equal(callLinkTexts.joinAgain, "Открыть ещё раз");
  assert.equal(callLinkTexts.joinAgainTitle, "Открыть ссылку на пару ещё раз (запись не начнётся)");
  assert.equal(callLinkTexts.joinRunningTitle, "Открыть ссылку на пару");
  assert.equal(callLinkTexts.joinAgainRunningTitle, "Открыть ссылку на пару ещё раз");
  assert.equal(callLinkTexts.joinTitle, "Открыть ссылку на пару (запись не начнётся)", "the first open before the recording is unchanged");
});

// ---- the prompt the recording was started from ----

test("startedPromptId: only a successful start of a lesson prompt counts", () => {
  const sent = { id: "p1", kind: "lesson" };
  assert.equal(startedPromptId(sent, { id: "p1", state: "STARTED" }), "p1");
  assert.equal(startedPromptId(sent, { id: "p1", state: "OFFERED" }), "", "a failed start leaves the offer");
  assert.equal(startedPromptId(sent, { id: "p1", state: "STARTING" }), "");
  assert.equal(startedPromptId(sent, { id: "p2", state: "STARTED" }), "", "another prompt answered");
  assert.equal(startedPromptId({ id: "a1", kind: "adhoc" }, { id: "a1", state: "STARTED" }), "", "an ad-hoc recording has no lesson link");
  assert.equal(startedPromptId(null, { id: "p1", state: "STARTED" }), "");
});

test("StartedRecording keeps the started prompt for callSubject while its recording runs and drops it after", () => {
  const rec = new StartedRecording();
  assert.equal(rec.track("r1"), "", "nothing started from the popup: a recording from the main window or ad-hoc gets no ID");
  rec.started("p1");
  assert.equal(rec.track(""), "", "not capturing yet: nothing to ask for, the start still waits");
  assert.equal(rec.track("r1"), "p1", "the recording is seen: its prompt is asked for, though it is no longer current");
  assert.equal(rec.track("r1"), "p1");
  assert.equal(rec.track(null), "p1", "a failed sound read changes nothing");
  assert.equal(rec.track(""), "", "the recording ended");
  assert.equal(rec.track("r1"), "", "and the prompt is forgotten");
  assert.equal(rec.track("r2"), "");

  rec.started("p2");
  assert.equal(rec.track("r3"), "p2");
  assert.equal(rec.track("r4"), "", "a different recording is shown: dropped");
  assert.equal(rec.track("r3"), "", "and not taken back");

  rec.started("p5");
  assert.equal(rec.track(null), "", "unread sound before the recording was seen");
  rec.started("p6");
  assert.equal(rec.track("r5"), "p6", "a newer start replaces the older prompt");
});

// TASK-406 review: the sound poll may report the recording before the start reply comes; started() after that still binds.
test("StartedRecording: started() after the sound poll already saw the recording binds at the next track", () => {
  const rec = new StartedRecording();
  assert.equal(rec.track("r1"), "", "the poll saw the recording, the start reply is pending");
  rec.started("p1");
  assert.equal(rec.track("r1"), "p1", "re-evaluated on the registration: the button shows");
  assert.equal(rec.track("r1"), "p1");
});

test("Popup.tsx re-runs the started-prompt binding when a started prompt is registered", () => {
  const code = src("popup/Popup.tsx");
  assert.match(code, /const \[startedSeq, setStartedSeq\] = useState\(0\);/);
  assert.match(code, /startedRec\.started\(started\);\s*setStartedSeq\(\(n\) => n \+ 1\);/);
  assert.match(code, /setRunningFrom\(startedRec\.track\(recordingNow\)\);\s*\}, \[startedRec, recordingNow, startedSeq\]\);/);
});

// Changed in the TASK-406 review: the join button takes its own row in EVERY phase while it is visible (it was: only when
// opened), because a row that moves between phases puts another button under the pointer of a double click.
test("joinOnOwnRow: the visible join button takes its own row in every phase; hidden or not shown: no", () => {
  for (const phase of ["ready", "opening", "opened"]) {
    assert.equal(joinOnOwnRow(joinView({ shown: true, phase, subjectId: "s", problem: "" }, false)), true, phase);
  }
  assert.equal(joinOnOwnRow(joinView({ shown: true, phase: "hidden", subjectId: "s", problem: "" }, false)), false);
  assert.equal(joinOnOwnRow(joinView({ shown: false, phase: "opened", subjectId: "s", problem: "" }, false)), false);
});

test("the offer layout is identical for ready, opening and opened: same row structure, same texts layout (TASK-406 review)", () => {
  const views = ["ready", "opening", "opened"].map((phase) => joinView({ shown: true, phase, subjectId: "s", problem: "" }, false));
  assert.deepEqual(views.map((v) => joinOnOwnRow(v)), [true, true, true], "the same wide classes: start, join and skip");
  // The confirmation text exists only in the opened phase, but must not change what the layout counts.
  assert.deepEqual(views.map((v) => v.confirm !== ""), [false, false, true]);
  const failed = joinView({ shown: true, phase: "ready", subjectId: "s", problem: "failed" }, false);
  const gone = joinView({ shown: true, phase: "hidden", subjectId: "s", problem: "gone" }, false);
  const bases = [[], [adviceTexts.browser], [adviceTexts.muted, adviceTexts.browser]];
  for (const base of bases) {
    const layouts = views.map((v) => {
      const own = joinOnOwnRow(v);
      const texts = [v.confirm, v.note, v.failure].filter((n) => n !== "");
      return textsLayout(layoutNotes(base, texts, v.visible), "");
    });
    assert.deepEqual(layouts[1], layouts[0], `opening == ready for ${base.length} other messages`);
    assert.deepEqual(layouts[2], layouts[0], `opened == ready for ${base.length} other messages`);
    // Changed in the TASK-406 review (second pass): the original rule again (two or more messages, the reserved line
    // included), it was: collapsed for every base because the join row lowered the limit to one.
    assert.equal(layouts[0].collapsed, base.length >= 1, "the advice collapses only by the original rule");
    // The failure line appearing does not toggle it either.
    const withFailure = textsLayout(layoutNotes(base, [failed.failure], failed.visible), "");
    assert.deepEqual(withFailure, layouts[0]);
  }
  // Without the join button the layout is the old one: the join texts count as they are (gone: no button, the note shows).
  assert.equal(gone.visible, false);
  assert.deepEqual(layoutNotes([], [gone.note], gone.visible), [gone.note]);
  assert.deepEqual(layoutNotes([adviceTexts.browser], [], false), [adviceTexts.browser]);
  assert.deepEqual(textsLayout(layoutNotes([adviceTexts.browser], [], false), ""), { grows: false, clamp: true, collapsed: false });
});

test("popup.css: a long label never spills out of a popup button, and the join button and «Не записывать» get full rows", () => {
  const css = src("popup/popup.css");
  const guard = css.match(/\.popup \.pbtns \.btn-labels \{[^}]*\}/)?.[0] ?? "";
  assert.match(guard, /min-width: 0/);
  assert.match(guard, /overflow: hidden/);
  const label = css.match(/\.popup \.pbtns \.btn-labels > \* \{[^}]*\}/)?.[0] ?? "";
  assert.match(label, /overflow: hidden/);
  assert.match(label, /text-overflow: ellipsis/);
  const code = src("popup/Popup.tsx");
  assert.match(code, /id === "skip" && joinOwnRow/);
  assert.match(css, /\.pbtns \.wide,/);
});

test("view.ts: noPromptNote is written like the other constants", () => {
  assert.match(src("popup/view.ts"), /export const noPromptNote = "/);
});

test("Popup.tsx remembers the prompt of a successful start and asks the core with it while the recording is on screen", () => {
  const code = src("popup/Popup.tsx");
  assert.match(code, /const startedRec = useMemo\(\(\) => new StartedRecording\(\), \[\]\);/);
  assert.match(code, /const started = action === "start" \? startedPromptId\(view, v\) : "";\s*if \(started !== ""\) \{\s*startedRec\.started\(started\);/);
  assert.match(code, /const recordingNow = sound === null \? null : sound\.recording \? sound\.recordingId : "";/);
  assert.match(code, /setRunningFrom\(startedRec\.track\(recordingNow\)\)/);
  assert.match(code, /const joinId = live \? \(runningFrom === "" \? null : runningFrom\) : offeredId;/);
  assert.match(code, /joinCtl\.offer\(joinId\)/);
  assert.match(code, /joinView\(join, live \? false : m\.disabled, live\)/);
});

test("the buttons: «Начать запись», the join button and «Не записывать» each on a full-width row; no join keeps the old look", () => {
  const code = src("popup/Popup.tsx");
  const row = code.slice(code.lastIndexOf('<div className="pbtns">'));
  const start = row.indexOf("m.buttons.map");
  const join = row.indexOf("{id === \"start\" && joinShown && (");
  assert.ok(start >= 0 && join > start, "the join button is drawn right after the start button, before the skip one");
  // TASK-406: the label is «Подключиться» or «Открыть ещё раз» (jv.label, from callLinkTexts via joinView).
  assert.match(row, /\{jv\.label\}\s*<\/Button>/);
  assert.match(row, /title=\{jv\.title\}/, "the hint is callLinkTexts.joinTitle or joinAgainTitle (via joinView)");
  assert.match(row, /busyLabel=\{callLinkTexts\.joining\}/);
  assert.match(row, /disabled=\{jv\.disabled\}/);
  assert.match(row, /disabled=\{m\.disabled\}/, "start and skip still follow the old rule");
  // Changed in the TASK-406 review: the join button is full width in every phase (it was: only once opened, as «Открыть
  // ещё раз»; before that it shared a row with «Не записывать»), so no row moves under the pointer between phases.
  assert.match(row, /className=\{id === "start" \|\| id === "close" \|\| \(id === "skip" && joinOwnRow\) \? "wide" : undefined\}/);
  assert.match(row.slice(join), /className="wide"/, "the join button is wide in every phase");
  assert.doesNotMatch(row.slice(join), /className=\{joinOwnRow/, "its width does not depend on the phase");
  assert.match(code, /const joinOwnRow = joinShown && joinOnOwnRow\(jv\);/);
  // Changed in the TASK-406 review (second pass): the layout no longer takes joinOwnRow; the window grows for the row.
  assert.match(code, /textsLayout\(notes, errorLine\)/);
  assert.match(code, /growsRef\.current = windowGrows\(advice === null && !live, layout\.grows, joinOwnRow\);/, "the join row lets the window grow");
  assert.doesNotMatch(row, /autoFocus|\.focus\(/, "no focus is taken");
  const view = src("popup/view.ts");
  assert.match(view, /buttons: ButtonId\[\] = onOffer \? \["start", "skip"\] : \["close"\]/, "view.ts buttons are unchanged");
  // Without the join button «Не записывать» is the second and last child and spans the row, as before; with it, the
  // third child does not match and takes the second column.
  const css = src("popup/popup.css");
  assert.match(css, /\.pbtns:not\(\.two\) > \.btn:nth-child\(2\):last-child \{ grid-column: 1 \/ -1; \}/);
  assert.match(css, /\.pbtns \{ display: grid; grid-template-columns: 1fr 1fr;/);
  // TASK-406: no grey «joined» look any more; the opened button is an ordinary active one.
  assert.doesNotMatch(css, /\.joined/);
  assert.doesNotMatch(code, /joined/);
  assert.equal(callLinkTexts.join, "Подключиться");
  assert.equal(callLinkTexts.joining, "Открытие…");
});

test("the red banner carries only the page error and the start problem; the join texts are quiet lines in the texts area", () => {
  const code = src("popup/Popup.tsx");
  assert.match(code, /const errorLine = error \|\| m\.problem;/);
  assert.doesNotMatch(code, /jv\.error/);
  // The notes count in the layout, the failure and the opened note too, so the buttons stay uncut.
  // Changed in the TASK-406 review: the join texts go through layoutNotes, which counts one reserved line while the
  // button is shown (it was: the notes list held the join texts directly, so the count changed with the phase).
  assert.match(code, /\[picker\.autoNote, \.\.\.picker\.warnings\]/);
  assert.match(code, /layoutNotes\(baseNotes, \[\.\.\.joinQuiet, joinFailure\]\.filter\(\(n\) => n !== ""\), joinShown\)/);
  assert.match(code, /textsLayout\(notes, errorLine\)/);
  assert.match(code, /const joinQuiet = showPicker \? \[jv\.confirm, jv\.note\]/);
  // Quiet: p-note (muted); the failure is the amber warning, last in the area. Both are live regions (TASK-406): drawn
  // as soon as the button is, text changes inside them.
  const area = code.slice(code.indexOf('<div className="texts">\n          {errorLine'), code.indexOf('<div className="pbtns">\n        {m.buttons'));
  const quiet = area.indexOf("{showPicker && jv.live && (");
  assert.ok(quiet > area.indexOf("picker.warnings.map"), "the quiet line follows the warnings");
  assert.match(area.slice(quiet), /"p-note join-note clamp" : "p-note join-note"/);
  assert.match(area.slice(quiet), /\{joinQuiet\[0\]\}/);
  const failure = area.lastIndexOf("{showPicker && jv.live && (");
  assert.ok(failure > area.indexOf("picker.advice"), "the failure is the last item, below the advice");
  assert.match(area.slice(failure), /"warning join-fail clamp" : "warning join-fail"/);
  assert.match(area.slice(failure), /\{joinFailure\}/);
  assert.doesNotMatch(area.slice(quiet, area.indexOf("{showPicker && (\n            <p\n              className={layout.collapsed")), /warning/, "the quiet lines are not amber");
  assert.doesNotMatch(area.slice(failure), /Banner|alert/);
  assert.equal(area.indexOf("Banner kind=\"fail\"") < quiet, true);
  assert.doesNotMatch(area.slice(area.indexOf("Banner kind"), area.indexOf("</Banner>")), /join/i, "nothing of the join reaches the banner");
});

test("a11y: the confirmation, the gone note and the failure line are persistent role=status regions in both views (TASK-370 follow-up)", () => {
  const code = src("popup/Popup.tsx");
  // Offer view: the two regions are mounted with jv.live, not with their text, and carry role="status".
  const offer = code.slice(code.indexOf('<div className="texts">\n          {errorLine'), code.indexOf('<div className="pbtns">\n        {m.buttons'));
  assert.equal([...offer.matchAll(/\{showPicker && jv\.live && \(/g)].length, 2);
  assert.equal([...offer.matchAll(/role="status"/g)].length, 2);
  assert.doesNotMatch(offer, /\{joinFailure !== "" && \(/, "the failure line is no longer mounted together with its text");
  assert.doesNotMatch(offer, /role=\{n === jv\.confirm/, "the confirmation is no longer mounted together with its text");
  // Running view: the gone note and the failure line, again drawn with jv.live.
  const running = code.slice(code.indexOf("{stopProblem !== \"\" && <p className=\"warning\">"), code.indexOf('<div className="pbtns two">'));
  assert.equal([...running.matchAll(/\{jv\.live && \(/g)].length, 2);
  assert.equal([...running.matchAll(/role="status"/g)].length, 2);
  // Changed in the TASK-406 review: the quiet region says the note or the confirmation (it said only the note).
  assert.match(running, /\{jv\.note \|\| jv\.confirm\}/, "«Ссылка открыта.» (jv.confirm is never «Запись пока не идёт» while it runs)");
  assert.match(running, /\{jv\.failure\}/);
  // An empty region takes no room: the negative margin takes back the gap of the column.
  const css = src("popup/popup.css");
  assert.match(css, /\.join-note:empty, \.join-fail:empty \{ margin-top: -4px; \}/);
  assert.doesNotMatch(css, /\.join-(note|fail)[^{]*\{[^}]*display:\s*none/, "display:none would unregister the live region");
});

// TASK-406 review (second pass): the quiet line is one line high while the join button is shown, so the confirmation
// appearing does not move the texts; the region still exists (empty) before its text, so it stays announced.
test("the offer view's quiet join line keeps one label line while the join button is shown", () => {
  const code = src("popup/Popup.tsx");
  const quiet = code.slice(code.indexOf('"p-note join-note clamp"') - 200, code.indexOf("{joinQuiet[0]}"));
  assert.match(quiet, /role="status"/, "still a persistent live region");
  assert.match(quiet, /\(joinShown \? " reserved" : ""\)/, "reserved only while the button is shown");
  const css = src("popup/popup.css");
  assert.match(css, /\.p-note\.join-note\.reserved \{ min-height: var\(--lh-label\); \}/);
  assert.match(css, /\.join-note\.reserved:empty \{ margin-top: 0; \}/, "the empty margin trick does not shrink the reserved line");
  assert.doesNotMatch(css, /\.join-(note|fail)[^{]*\{[^}]*display:\s*none/);
  // The reserved line height is the line height of the label text, so the text fills it exactly.
  assert.match(css, /\.p-note\.join-note \{ font-size: var\(--fs-label\); line-height: var\(--lh-label\); \}/);
});

test("the running view: one extra full-width row with «Подключиться» / «Открыть ещё раз», only when the core gave a link", () => {
  const code = src("popup/Popup.tsx");
  const run = code.slice(code.indexOf('<div className="pbtns two">'), code.indexOf("  return (\n    <div className=\"popup\" ref={popupRef}>\n      <IconSprite />\n      <div className={topClass(\"offer\")}"));
  assert.match(run, /\{jv\.visible && \(/, "no link: the button is not drawn and nothing else changes");
  assert.equal([...run.matchAll(/<Button/g)].length, 3, "«Остановить», «Скрыть» and the join button: no more rows");
  const join = run.slice(run.indexOf("{jv.visible && ("));
  assert.match(join, /className="wide"/);
  assert.match(join, /busyLabel=\{callLinkTexts\.joining\}/);
  assert.match(join, /disabled=\{jv\.disabled\}/);
  assert.match(join, /title=\{jv\.title\}/);
  assert.match(join, /\{jv\.label\}/);
  assert.doesNotMatch(join, /variant=|danger|primary/, "a secondary button, as «Скрыть»");
  assert.doesNotMatch(join, /autoFocus|\.focus\(/);
  // The row stays inside the two-column grid: «wide» spans it, the popup width holds.
  const css = src("popup/popup.css");
  assert.match(css, /\.pbtns \.wide, \.pbtns:not\(\.two\) > \.btn:nth-child\(2\):last-child \{ grid-column: 1 \/ -1; \}/);
});

test("the dot: neutral in the offer view, red and pulsing only while a recording runs (running view and the advice about it)", () => {
  assert.equal(dotClass("offer"), "dot");
  assert.equal(dotClass("recording"), "dot rec");
  assert.equal(dotClass("advice"), "dot rec");
  assert.equal(topClass("offer"), "p-top", "the heading of an offer is not red either");
  assert.equal(topClass("recording"), "p-top live");
  assert.equal(topClass("advice"), "p-top live");
  const css = src("popup/popup.css");
  assert.match(css, /\n\.dot \{[^}]*background: var\(--muted\);[^}]*\}/);
  assert.doesNotMatch(css.match(/\n\.dot \{[^}]*\}/)[0], /animation|--rec/, "the base dot neither pulses nor is red");
  assert.match(css, /\n\.dot\.rec \{ background: var\(--rec\); animation: breath var\(--t-breath\) ease-in-out infinite; \}/);
  // Each view uses the function: no literal dot or top classes are left in the page.
  const code = src("popup/Popup.tsx");
  assert.match(code, /className=\{dotClass\("advice"\)\}/);
  assert.match(code, /className=\{dotClass\("recording"\)\}/);
  assert.match(code, /className=\{dotClass\("offer"\)\}/);
  assert.match(code, /className=\{topClass\("advice"\)\}/);
  assert.match(code, /className=\{topClass\("recording"\)\}/);
  assert.match(code, /className=\{topClass\("offer"\)\}/);
  assert.doesNotMatch(code, /className="dot"|className="p-top/);
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
