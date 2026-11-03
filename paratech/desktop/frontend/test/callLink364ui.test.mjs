// TASK-364: the link to the online class in the UI. The decisions of the card of a pair on «Сегодня», the flow of the
// buttons (save, open, delete, «Вернуть», the pair without a subject), the card and the block as markup, and the wiring
// of the pages. Runs on the modules compiled by `npm run test`; pages have no DOM harness, so their wiring is read from source.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { errorTexts, removedNameText } from "../.test-build/bindings/library.js";
import {
  CallLinkFlow,
  applySubjectLink,
  callLinkTexts,
  cardLink,
  createQuestion,
  createRefusalText,
  explainText,
  hasNoField,
  idleLink,
  joinTitle,
  linkId,
  linkKey,
  nameTakenText,
  saveLabel,
  withoutLink,
} from "../.test-build/callLinkView.js";
import { CallLinkBlock, LessonLinkButtons } from "../.test-build/pages/CallLinkParts.js";
import { LessonsSection } from "../.test-build/pages/TodayParts.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const src = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");
const clock = (d, hh, mm) => new Date(2026, 9, d, hh, mm);
const at = (d, hh = 0, mm = 0) => clock(d, hh, mm).toISOString();

const LINK = "https://meet.example.org/j/42?pwd=FakePwd42";
const HOST = "meet.example.org";

const lesson = (over = {}) => ({
  id: "l1", title: "Физика", subject: "Физика", subjectId: "s1", subjectName: "Физика", hasCallLink: false, callLinkHost: "",
  kind: "lecture", teacher: "", start: at(21, 10, 30), end: at(21, 11, 50), post: null, ...over,
});
const withLink = (over = {}) => lesson({ hasCallLink: true, callLinkHost: HOST, ...over });
const subject = (over = {}) => ({
  id: "s1", name: "Физика", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026", folders: [], version: 3,
  teachers: { lecture: null, practice: null, lab: null }, callLink: null, ...over,
});
const linked = (over = {}) => subject({ callLink: { link: LINK, host: HOST }, ...over });

// ---- The decisions of the card ----

test("the card of a pair offers what its subject allows, from the lesson fields only", () => {
  assert.deepEqual(cardLink(withLink()), { kind: "join", host: HOST });
  assert.deepEqual(cardLink(lesson()), { kind: "add" });
  assert.deepEqual(cardLink(lesson({ subjectId: "", subjectName: "", subject: " Физика " })), { kind: "create", name: "Физика" });
  assert.deepEqual(cardLink(lesson({ subjectId: "", subjectName: "", subject: "  " })), { kind: "nosubject" });
  // A link claimed for a pair with no subject is not offered: there is nowhere to open it from.
  assert.deepEqual(cardLink(lesson({ subjectId: "", subject: "", hasCallLink: true, callLinkHost: HOST })), { kind: "nosubject" });
});

test("the explanations and the labels of the pair without a subject are plain Russian with a way out", () => {
  assert.equal(createQuestion("Физика"), "Ссылка хранится у предмета «Физика». Создать предмет и сохранить ссылку?");
  assert.equal(explainText({ kind: "create", name: "Физика" }), createQuestion("Физика"));
  assert.equal(explainText({ kind: "nosubject" }), "У пары нет предмета: ссылку можно добавить на странице предмета в «Предметы».");
  assert.equal(explainText({ kind: "add" }), "");
  assert.equal(hasNoField({ kind: "nosubject" }), true);
  assert.equal(hasNoField({ kind: "create", name: "x" }), false);
  assert.equal(saveLabel({ kind: "create", name: "x" }), "Создать предмет и сохранить");
  assert.equal(saveLabel({ kind: "add" }), "Сохранить");
  assert.equal(joinTitle(HOST), "Ссылка на пару: meet.example.org");
  assert.equal(joinTitle(""), "");
  assert.equal(callLinkTexts.invalid, "Нужна ссылка вида https://… (http или https).");
  assert.equal(callLinkTexts.openFailed, "Не удалось открыть ссылку. Попробуйте ещё раз.");
});

test("a refused create says why: a taken name, a removed subject, anything else; each leads to «Предметы» in the flow", () => {
  assert.ok(errorTexts.includes(nameTakenText), "the text is the core's");
  assert.match(createRefusalText(new Error(nameTakenText), "Физика"), /такое название уже есть.*Ссылку можно добавить у нужного предмета в «Предметы»/);
  const removed = Object.assign(new Error(nameTakenText), { cause: { problem: "removed_name_taken", subjectId: "s9", version: 2 } });
  assert.match(createRefusalText(removed, "Физика"), /Предмет «Физика» удалён\. Вернуть его можно в «Предметы»/);
  assert.notEqual(removedNameText, "");
  assert.equal(createRefusalText(new Error("boom"), "Физика"), callLinkTexts.createFailed);
  assert.equal(createRefusalText(new Error("Название предмета: от 1 до 200 символов."), "x"), "Название предмета: от 1 до 200 символов.");
});

test("a saved link shows on every pair of its subject at once; a link that is gone leaves them offering «Добавить ссылку»", () => {
  const day = [lesson({ id: "a" }), lesson({ id: "b" }), lesson({ id: "c", subjectId: "s2", subjectName: "Химия" })];
  const out = applySubjectLink(day, "a", linked());
  assert.deepEqual(out.map((l) => [l.hasCallLink, l.callLinkHost]), [[true, HOST], [true, HOST], [false, ""]]);
  assert.equal(out[2], day[2], "another subject is the same object");
  const cleared = applySubjectLink(out, "a", subject());
  assert.deepEqual(cleared.map((l) => l.hasCallLink), [false, false, false]);
  // A pair that had no subject takes the one just made.
  const none = [lesson({ id: "n", subjectId: "", subjectName: "" })];
  const made = applySubjectLink(none, "n", subject({ id: "s7", name: "Физика" }));
  assert.deepEqual([made[0].subjectId, made[0].subjectName, made[0].hasCallLink], ["s7", "Физика", false]);
  assert.deepEqual(withoutLink(out, "s1").map((l) => l.hasCallLink), [false, false, false]);
  assert.equal(linkKey({ id: "", start: "t", title: "x" }), "t|x");
});

// ---- The flow ----

/** A page in miniature: the fake calls (results come from queues), the host notes and the states reported. */
function setup(results = {}) {
  const page = { calls: [], applied: [], gone: [], rereads: 0, states: [] };
  const queue = (name, fallback) => async (...args) => {
    page.calls.push([name, ...args]);
    const list = results[name] ?? [];
    const next = list.length > 0 ? list.shift() : fallback(args);
    if (next instanceof Error) {
      throw next;
    }
    return next;
  };
  const api = {
    createSubject: queue("create", ([name]) => subject({ id: "s-new", name })),
    setSubjectCallLink: queue("set", ([id, text]) => ({ problem: "", subject: linked({ id, callLink: { link: text, host: HOST } }) })),
    clearSubjectCallLink: queue("clear", () => ({ problem: "", subject: subject() })),
    openSubjectCallLink: queue("open", () => ({ problem: "" })),
  };
  const host = {
    applied: (s, key) => page.applied.push([s.id, key, s.callLink?.link ?? null]),
    linkGone: (id) => page.gone.push(id),
    reread: () => page.rereads++,
  };
  page.flow = new CallLinkFlow(api, host, (s) => page.states.push(s));
  return page;
}
const target = (over = {}) => ({ key: "l1", subjectId: "s1", name: "", ...over });

test("«Добавить ссылку» opens the field with the focus; typing, Escape and «Отмена» work and return the focus to the button", () => {
  const p = setup();
  p.flow.openForm("l1", "add");
  assert.deepEqual(p.flow.state.form, { key: "l1", mode: "add", text: "" });
  assert.equal(p.flow.state.focus.id, linkId("l1", "field"));
  p.flow.type("https://x");
  assert.equal(p.flow.state.form.text, "https://x");
  p.flow.cancel();
  assert.equal(p.flow.state.form, null);
  assert.equal(p.flow.state.focus.id, linkId("l1", "add"), "back to the button that opened the field");
  p.flow.openForm("l1", "change", LINK);
  assert.equal(p.flow.state.form.text, LINK, "a change starts from the current link");
  p.flow.cancel();
  assert.equal(p.flow.state.focus.id, linkId("l1", "change"));
});

test("save: the typed text goes to the core as typed (trimmed), the card is told, the focus goes to «Подключиться»", async () => {
  const p = setup();
  p.flow.openForm("l1", "add");
  p.flow.type(`  ${LINK}  `);
  await p.flow.save(target());
  assert.deepEqual(p.calls, [["set", "s1", LINK]]);
  assert.deepEqual(p.applied, [["s1", "l1", LINK]]);
  const s = p.flow.state;
  assert.equal(s.form, null);
  assert.equal(s.busy, "");
  assert.deepEqual(s.line, { key: "l1", kind: "ok", text: callLinkTexts.saved, action: "" });
  assert.equal(s.focus.id, linkId("l1", "join"));
  assert.ok(p.states.some((x) => x.busy === "save"), "a busy state was reported");
});

test("save: an empty field and a refused link keep the field with the entered text and send the focus back to it", async () => {
  const p = setup({ set: [{ problem: "invalid", subject: null }] });
  p.flow.openForm("l1", "add");
  await p.flow.save(target());
  assert.equal(p.flow.state.fieldError, callLinkTexts.fieldEmpty);
  assert.equal(p.calls.length, 0, "nothing is sent for an empty field");
  p.flow.type("ftp://x");
  assert.equal(p.flow.state.fieldError, "", "typing clears the error");
  await p.flow.save(target());
  const s = p.flow.state;
  assert.equal(s.fieldError, "Нужна ссылка вида https://… (http или https).");
  assert.equal(s.form.text, "ftp://x");
  assert.equal(s.focus.id, linkId("l1", "field"));
  assert.equal(s.busy, "");
  assert.equal(p.applied.length, 0);
});

test("save: a failed call says so under the field and can be repeated; a lost subject closes the field and re-reads", async () => {
  const p = setup({ set: [{ problem: "failed", subject: null }, new Error("boom"), { problem: "not_found", subject: null }] });
  p.flow.openForm("l1", "add");
  p.flow.type(LINK);
  await p.flow.save(target());
  assert.deepEqual(p.flow.state.line, { key: "l1", kind: "fail", text: callLinkTexts.saveFailed, action: "" });
  assert.equal(p.flow.state.form.text, LINK);
  await p.flow.save(target());
  assert.equal(p.flow.state.line.text, callLinkTexts.saveFailed, "a rejected call reads as failed");
  await p.flow.save(target());
  assert.equal(p.flow.state.form, null);
  assert.equal(p.flow.state.line.text, callLinkTexts.subjectGone);
  assert.equal(p.rereads, 1);
});

test("a pair without a subject: the subject is made, then the link is kept; a refusal explains and leads to «Предметы»", async () => {
  const p = setup();
  p.flow.openForm("l1", "add");
  p.flow.type(LINK);
  await p.flow.save(target({ subjectId: "", name: "Физика" }));
  assert.deepEqual(p.calls, [["create", "Физика", ""], ["set", "s-new", LINK]]);
  assert.deepEqual(p.applied.map((a) => a[0]), ["s-new", "s-new"], "the card learns of the new subject, then of its link");
  assert.equal(p.flow.state.line.kind, "ok");

  const taken = setup({ create: [new Error(nameTakenText)] });
  taken.flow.openForm("l1", "add");
  taken.flow.type(LINK);
  await taken.flow.save(target({ subjectId: "", name: "Физика" }));
  assert.equal(taken.calls.filter((c) => c[0] === "set").length, 0, "no link is saved without a subject");
  assert.equal(taken.flow.state.line.action, "subjects");
  assert.match(taken.flow.state.line.text, /такое название уже есть/);
  assert.equal(taken.flow.state.form, null);
  assert.equal(taken.flow.state.focus.id, linkId("l1", "add"));

  // The subject was made but the link is refused: the card now has the subject, so the field stays and a retry only saves.
  const half = setup({ set: [{ problem: "invalid", subject: null }] });
  half.flow.openForm("l1", "add");
  half.flow.type("zzz");
  await half.flow.save(target({ subjectId: "", name: "Физика" }));
  assert.equal(half.flow.state.fieldError, callLinkTexts.invalid);
  assert.equal(half.applied.length, 1);
  half.flow.type(LINK);
  await half.flow.save(target({ subjectId: "s-new" }));
  assert.deepEqual(half.calls.map((c) => c[0]), ["create", "set", "set"], "the subject is made once");
});

test("a pair that names no subject cannot be saved: nothing is made or sent", async () => {
  const p = setup();
  p.flow.openForm("l1", "add");
  p.flow.type(LINK);
  await p.flow.save(target({ subjectId: "", name: "" }));
  assert.deepEqual(p.calls, []);
});

test("«Подключиться» sends only the subject ID; a removed link switches the card, a failure offers a retry", async () => {
  const p = setup({ open: [{ problem: "" }, { problem: "no_link" }, { problem: "failed" }, { problem: "not_found" }, new Error("x")] });
  await p.flow.join(target());
  assert.deepEqual(p.calls, [["open", "s1"]]);
  assert.deepEqual(p.flow.state.line, { key: "l1", kind: "ok", text: callLinkTexts.opened, action: "" });
  assert.equal(p.flow.state.focus.id, linkId("l1", "join"));

  await p.flow.join(target());
  assert.deepEqual(p.gone, ["s1"], "the card is told the link is gone");
  assert.equal(p.flow.state.line.text, callLinkTexts.gone);
  assert.equal(p.flow.state.focus.id, linkId("l1", "add"), "the focus follows the button that replaced «Подключиться»");

  await p.flow.join(target());
  assert.deepEqual(p.flow.state.line, { key: "l1", kind: "fail", text: callLinkTexts.openFailed, action: "retry" });

  await p.flow.join(target());
  assert.equal(p.flow.state.line.text, callLinkTexts.subjectGone);
  assert.equal(p.rereads, 1);

  await p.flow.join(target());
  assert.equal(p.flow.state.line.action, "retry", "a rejected call reads as failed");
});

test("one call at a time: a second click while one runs does nothing", async () => {
  let release = noop;
  const p = setup();
  const api = {
    createSubject: async () => subject(),
    setSubjectCallLink: async () => ({ problem: "", subject: linked() }),
    clearSubjectCallLink: async () => ({ problem: "", subject: subject() }),
    openSubjectCallLink: () => {
      p.calls.push(["open"]);
      return new Promise((resolve) => {
        release = () => resolve({ problem: "" });
      });
    },
  };
  const flow = new CallLinkFlow(api, { applied: noop, linkGone: noop, reread: noop }, (s) => p.states.push(s));
  const first = flow.join(target());
  assert.equal(flow.state.busy, "open");
  assert.equal(flow.state.busyKey, "l1");
  await flow.join(target());
  flow.openForm("l1", "add");
  assert.equal(flow.state.form, null, "the field does not open meanwhile");
  release();
  await first;
  assert.equal(p.calls.length, 1);
  assert.equal(flow.state.busy, "");
});

test("delete keeps the removed link for «Вернуть»; the restore saves it again; a failure keeps the offer", async () => {
  const p = setup({ set: [{ problem: "failed", subject: null }] });
  await p.flow.clear(target({ key: "s1" }), { link: LINK, host: HOST });
  assert.deepEqual(p.calls, [["clear", "s1"]]);
  assert.deepEqual(p.flow.state.removed, { key: "s1", subjectId: "s1", link: LINK });
  assert.equal(p.flow.state.line.text, callLinkTexts.cleared);
  assert.equal(p.flow.state.focus.id, linkId("s1", "undo"));

  await p.flow.restore();
  assert.equal(p.flow.state.line.text, callLinkTexts.restoreFailed);
  assert.notEqual(p.flow.state.removed, null, "a failed call can be repeated");

  await p.flow.restore();
  assert.deepEqual(p.calls.at(-1), ["set", "s1", LINK], "the same link goes through the same check");
  assert.equal(p.flow.state.removed, null);
  assert.equal(p.flow.state.line.text, callLinkTexts.restored);
  assert.equal(p.flow.state.focus.id, linkId("s1", "join"));
});

test("a refused delete and a restore the core refuses say so; the offer goes with a refusal that cannot be repeated", async () => {
  const p = setup({ clear: [{ problem: "failed", subject: null }], set: [{ problem: "invalid", subject: null }] });
  await p.flow.clear(target({ key: "s1" }), { link: LINK, host: HOST });
  assert.equal(p.flow.state.line.text, callLinkTexts.clearFailed);
  assert.equal(p.flow.state.removed, null);
  assert.equal(p.flow.state.focus.id, linkId("s1", "clear"));
  await p.flow.clear(target({ key: "s1" }), { link: LINK, host: HOST });
  await p.flow.restore();
  assert.equal(p.flow.state.removed, null, "an invalid link will not become valid by repeating");
  assert.equal(p.flow.state.focus.id, linkId("s1", "add"));
});

test("opening a field drops the undo offer; reset() clears everything and a late call no longer touches the state", async () => {
  const p = setup();
  await p.flow.clear(target({ key: "s1" }), { link: LINK, host: HOST });
  p.flow.openForm("s1", "add");
  assert.equal(p.flow.state.removed, null);
  p.flow.reset();
  assert.equal(p.flow.state.form, null);
  assert.equal(p.flow.state.line, null);

  let release = noop;
  const slow = new CallLinkFlow(
    {
      createSubject: noop,
      setSubjectCallLink: noop,
      clearSubjectCallLink: noop,
      openSubjectCallLink: () => new Promise((r) => (release = () => r({ problem: "no_link" }))),
    },
    { applied: noop, linkGone: (id) => p.gone.push(id), reread: noop },
    noop,
  );
  const call = slow.join(target());
  slow.reset();
  release();
  await call;
  assert.deepEqual(p.gone, ["s1"], "the host still learns what the late call found");
  assert.equal(slow.state.line, null, "the state of the new page is not touched");
});

// ---- The card of «Сегодня» as markup ----

const controls = (state = {}) => ({ state: { ...idleLink, ...state }, on: new Proxy({}, { get: () => noop }), onOpenSubjects: noop });
const section = (lessons, links, extra = {}) =>
  html(h(LessonsSection, { lessons, recordings: [], now: clock(21, 10, 25), onOpenLesson: noop, onRecord: noop, links, ...extra }));

test("a pair with a link: «Подключиться» next to «Записать», the host only, never the link; «Изменить» is quiet", () => {
  const out = section([withLink()], controls());
  assert.match(out, /<button[^>]*aria-label="Подключиться: Физика"[^>]*>.*Подключиться<\/span>/);
  assert.match(out, /Записать/);
  assert.ok(out.indexOf("Подключиться") < out.indexOf(">Записать<"), "joining comes before recording");
  assert.match(out, /<bdi>meet\.example\.org<\/bdi>/);
  assert.match(out, /aria-label="Изменить: Физика"/);
  assert.doesNotMatch(out, /Добавить ссылку/);
  assert.doesNotMatch(out, /https?:\/\//, "no link text on the card");
  assert.doesNotMatch(out, /FakePwd42/);
  // «Подключиться» is not the accent: the one accent stays where it was.
  assert.doesNotMatch(out.match(/<button[^>]*Подключиться: Физика[^>]*>/)[0], /primary/);
});

test("the link controls do not depend on the recording window: an earlier and a finished pair have them too", () => {
  const early = section([withLink()], controls(), { now: clock(21, 7, 30) });
  assert.match(early, /Подключиться<\/span>/);
  assert.doesNotMatch(early, />Записать</, "«Записать» keeps its own window");
  const late = section([lesson()], controls(), { now: clock(21, 15, 0) });
  assert.match(late, /Добавить ссылку<\/button>/);
});

test("a pair without a link offers «Добавить ссылку» as a quiet button; the field opens in place with a label, a placeholder and no error", () => {
  const plain = section([lesson()], controls());
  assert.match(plain, /<button[^>]*aria-label="Добавить ссылку: Физика"[^>]*class="btn ghost sm"|class="btn ghost sm"[^>]*aria-label="Добавить ссылку: Физика"/);
  assert.doesNotMatch(plain, /<input/);
  const open = section([lesson()], controls({ form: { key: "l1", mode: "add", text: "https://x" } }));
  assert.match(open, /<form[^>]*aria-label="Ссылка на пару"/);
  assert.match(open, /<input[^>]*aria-label="Ссылка на пару"/);
  assert.match(open, /placeholder="https:\/\/…"/);
  assert.match(open, /value="https:\/\/x"/);
  assert.match(open, />Сохранить</);
  assert.match(open, />Отмена</);
  assert.doesNotMatch(open, /Добавить ссылку<\/button>/, "the opener gives way to the field's own buttons");
  assert.doesNotMatch(open, /field-error/);
});

test("a refused link shows the plain rule under the field, marks it invalid and keeps the typed text", () => {
  const out = section([lesson()], controls({ form: { key: "l1", mode: "add", text: "ftp://x" }, fieldError: callLinkTexts.invalid }));
  assert.match(out, /Нужна ссылка вида https:\/\/… \(http или https\)\./);
  assert.match(out, /aria-invalid="true"/);
  assert.match(out, /value="ftp:\/\/x"/);
  assert.match(out, /role="alert"/);
});

test("while a call runs the controls are off and the busy label is small and inline", () => {
  const out = section([withLink()], controls({ busy: "open", busyKey: "l1" }));
  assert.match(out, /<button[^>]*disabled=""[^>]*aria-label="Подключиться: Физика"|<button[^>]*aria-label="Подключиться: Физика"[^>]*disabled=""/);
  assert.match(out, /Открытие…/);
  const saving = section([lesson()], controls({ form: { key: "l1", mode: "add", text: LINK }, busy: "save", busyKey: "l1" }));
  assert.match(saving, /<input[^>]*disabled=""/);
  assert.match(saving, /Сохранение…/);
});

test("the pair without a subject: the question with the creating button, or the explanation with the way to «Предметы»", () => {
  const nameless = lesson({ subjectId: "", subjectName: "", subject: "Физика (лк)" });
  const closed = section([nameless], controls());
  assert.match(closed, /Добавить ссылку<\/button>/, "the action is there for a pair without a subject too");
  const open = section([nameless], controls({ form: { key: "l1", mode: "add", text: "" } }));
  assert.match(open, /Ссылка хранится у предмета «Физика \(лк\)»\. Создать предмет и сохранить ссылку\?/);
  assert.match(open, /Создать предмет и сохранить/);
  assert.match(open, /<input/);

  const none = lesson({ subjectId: "", subjectName: "", subject: "" });
  const explained = section([none], controls({ form: { key: "l1", mode: "add", text: "" } }));
  assert.match(explained, /У пары нет предмета: ссылку можно добавить на странице предмета в «Предметы»\./);
  assert.match(explained, /Открыть «Предметы»/);
  assert.match(explained, />Закрыть</);
  assert.doesNotMatch(explained, /<input/, "nowhere to keep a link: no field");
});

test("results: a refused create leads to «Предметы», a failed open to «Повторить»; texts of files and models stay text", () => {
  const refused = section(
    [lesson({ subjectId: "", subjectName: "", subject: "Физика" })],
    controls({ line: { key: "l1", kind: "fail", text: createRefusalText(new Error(nameTakenText), "Физика"), action: "subjects" } }),
  );
  assert.match(refused, /Открыть «Предметы»/);
  assert.match(refused, /role="alert"/);
  const failed = section([withLink()], controls({ line: { key: "l1", kind: "fail", text: callLinkTexts.openFailed, action: "retry" } }));
  assert.match(failed, /Не удалось открыть ссылку\. Попробуйте ещё раз\./);
  assert.match(failed, />Повторить</);
  const ok = section([withLink()], controls({ line: { key: "l1", kind: "ok", text: callLinkTexts.opened, action: "" } }));
  assert.match(ok, /role="status"[^>]*>Ссылка открыта в браузере\./);
  const evil = section([withLink({ callLinkHost: "<img src=x onerror=1>", title: "<b>x</b>" })], controls());
  assert.doesNotMatch(evil, /<img|<b>/);
  // A result of another pair is not drawn here.
  const other = section([withLink()], controls({ line: { key: "zzz", kind: "fail", text: "Чужое", action: "" } }));
  assert.doesNotMatch(other, /Чужое/);
});

test("without the link controls the rows are as they were; LessonLinkButtons alone offers one button per card kind", () => {
  const before = section([lesson()], undefined);
  assert.doesNotMatch(before, /ссылк|Подключиться/i);
  const one = (l) => html(h(LessonLinkButtons, { lesson: l, title: "Физика", links: controls() }));
  assert.match(one(withLink()), /Подключиться/);
  assert.match(one(lesson()), /Добавить ссылку/);
  assert.match(one(lesson({ subjectId: "", subject: "" })), /Добавить ссылку/);
});

// ---- The block on the page of a subject ----

const block = (s, state = {}) => html(h(CallLinkBlock, { subject: s, links: controls(state) }));

test("the block: the host, the full link as selectable text, «Подключиться», «Изменить», «Удалить»; no link says so and offers «Добавить ссылку»", () => {
  const out = block(linked());
  assert.match(out, /Ссылка на пару/);
  assert.match(out, /<bdi>meet\.example\.org<\/bdi>/);
  assert.ok(out.includes(`<bdi>${LINK}</bdi>`), "the full link is a text node");
  assert.ok(out.includes(`title="${LINK}"`));
  for (const word of ["Подключиться", "Изменить", "Удалить"]) {
    assert.match(out, new RegExp(`>${word}<`), word);
  }
  assert.doesNotMatch(out, /<a /, "never a link element");
  const empty = block(subject());
  assert.match(empty, /Ссылки на пару нет/);
  assert.match(empty, />Добавить ссылку</);
  assert.doesNotMatch(empty, /Подключиться|Удалить/);
});

test("the block: the change form is filled with the current link and labelled; «Вернуть» follows a delete", () => {
  const form = block(linked(), { form: { key: "s1", mode: "change", text: LINK } });
  assert.match(form, /<label[^>]*>Ссылка на пару<\/label>/);
  assert.ok(form.includes(`value="${LINK}"`));
  assert.match(form, />Сохранить</);
  const cleared = block(subject(), {
    line: { key: "s1", kind: "ok", text: callLinkTexts.cleared, action: "" },
    removed: { key: "s1", subjectId: "s1", link: LINK },
  });
  assert.match(cleared, /Ссылка на пару удалена\./);
  assert.match(cleared, />Вернуть</);
  assert.match(cleared, /Ссылки на пару нет/);
  const busy = block(subject(), { line: { key: "s1", kind: "ok", text: callLinkTexts.cleared, action: "" }, removed: { key: "s1", subjectId: "s1", link: LINK }, busy: "restore", busyKey: "s1" });
  assert.match(busy, /Возврат…/);
  // The offer of another subject is not drawn.
  const other = block(subject({ id: "s2" }), { line: { key: "s1", kind: "ok", text: "Чужое", action: "" }, removed: { key: "s1", subjectId: "s1", link: LINK } });
  assert.doesNotMatch(other, /Чужое|Вернуть/);
});

// ---- The wiring of the pages ----

test("the pages are wired: «Сегодня» passes the controls to the lessons, the subject page shows the block, the popup is untouched", () => {
  const today = src("pages/TodayPage.tsx");
  assert.match(today, /new CallLinkFlow\(/);
  assert.match(today, /links=\{links\}/);
  assert.match(today, /applySubjectLink/);
  assert.match(today, /requests\.current\.next\(\);/, "an older read must not bring the old link back");
  const subjects = src("pages/SubjectsPage.tsx");
  assert.match(subjects, /<CallLinkBlock /);
  assert.match(subjects, /linkFlow\.reset\(\)/, "another subject drops the form and the offer");
  const parts = src("pages/CallLinkParts.tsx");
  assert.match(parts, /e\.key === "Escape"/);
  assert.match(parts, /aria-label=\{props\.labelled \? undefined : callLinkTexts\.field\}/);
  assert.doesNotMatch(parts, /dangerouslySetInnerHTML|<a\b/);
  for (const file of ["popup/Popup.tsx", "popup/main.tsx"]) {
    assert.equal(existsSync(new URL(`../src/${file}`, import.meta.url)), true);
    assert.doesNotMatch(src(file), /callLink|CallLink/i, file);
  }
});
