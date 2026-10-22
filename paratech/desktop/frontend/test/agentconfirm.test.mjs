// The confirm card (TASK-128): binding, card contents, plain-text rendering, the click guard, «Нет», the error state
// and «Да, в календарь» of «Сегодня». Runs on the modules compiled by `npm run test` with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { agentConfirmApi, confirmErrorText, genericError, methods, parseConfirmCard } from "../.test-build/bindings/agentConfirm.js";
import {
  ConfirmFlow,
  ProposeFlow,
  calendarTexts,
  confirmCardModel,
  confirmTexts,
  isDeadlineAction,
  sizeText,
} from "../.test-build/agentConfirmView.js";
import { ConfirmCard, ConfirmCardView } from "../.test-build/agent/ConfirmCard.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { entryModel } from "../.test-build/agentView.js";
import { CalendarButton } from "../.test-build/pages/TodayCalendar.js";
import { PostsSection } from "../.test-build/pages/TodayParts.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const evil = "<img src=x onerror=alert(1)>";

function card(over = {}) {
  return {
    kind: "send", targetKind: "proposal", targetId: "p1", state: "waiting", message: "", assignmentId: "a1",
    title: "Задачи 1–7", subject: "Матанализ", due: "2026-10-24T12:00:00Z", recipient: "teacher@example.test",
    emailSubject: "ДЗ по матанализу", attachmentName: "dz.txt", attachmentSize: 2048, attachmentFormat: "txt",
    draftVersion: 3, notice: "", deletion: null, ...over,
  };
}

const deadline = (over = {}) => card({ kind: "deadline", recipient: "", emailSubject: "", attachmentName: "", attachmentSize: 0, attachmentFormat: "", draftVersion: 0, ...over });
const deletion = (over = {}) =>
  card({
    kind: "delete_recording", targetKind: "preview", targetId: "r1", title: "", subject: "", due: "", recipient: "", emailSubject: "",
    attachmentName: "", attachmentSize: 0, attachmentFormat: "", draftVersion: 0, assignmentId: "",
    deletion: { id: "r1", title: "Лекция 5", startedAt: "2026-10-20T09:00:00Z", deletable: true, refusalMessage: "", deletes: ["Звук", "Расшифровка"] },
    ...over,
  });

const view = (c, extra = {}) =>
  html(h(ConfirmCardView, { model: confirmCardModel(c), busy: "", error: "", onYes: noop, onNo: noop, ...extra }));

/** A fake core: calls are counted, a reply can be held back to model a slow call. */
function fakeApi(over = {}) {
  const calls = { card: 0, confirm: 0, decline: 0, propose: 0 };
  let release = () => undefined;
  const hold = new Promise((r) => (release = r));
  return {
    calls,
    release: () => release(),
    card: async () => (calls.card++, card()),
    confirm: async (c) => (calls.confirm++, await hold, { ...c, state: "done" }),
    decline: async (t) => (calls.decline++, card({ targetKind: t.kind, targetId: t.id, state: "declined" })),
    proposeDeadline: async () => (calls.propose++, await hold, deadline()),
    ...over,
  };
}

// ---- Binding ----

test("binding: method names, arguments and strict parsing", async () => {
  const seen = [];
  const call = async (m, ...a) => (seen.push([m, a]), card());
  const api = agentConfirmApi(call);
  await api.card({ kind: "proposal", id: "p1" });
  await api.confirm(card());
  await api.decline({ kind: "preview", id: "r1" });
  await api.proposeDeadline("a1");
  assert.deepEqual(seen, [
    [methods.card, ["proposal", "p1"]],
    [methods.confirm, ["proposal", "p1", "ДЗ по матанализу", "txt"]],
    [methods.decline, ["preview", "r1"]],
    [methods.proposeDeadline, ["a1"]],
  ]);
  assert.ok(Object.values(methods).every((m) => m.startsWith("paratech/desktop/internal/input/wails.AgentConfirmService.")));
  assert.throws(() => parseConfirmCard({ ...card(), state: "bogus" }));
  assert.throws(() => parseConfirmCard({ ...card(), kind: "mail" }));
  assert.throws(() => parseConfirmCard(null));
  assert.equal(parseConfirmCard({ ...card(), deletion: null }).deletion, null);
});

test("errors: only the core's fixed texts are shown", () => {
  assert.equal(confirmErrorText("Предложение не найдено: возможно, оно уже обработано."), "Предложение не найдено: возможно, оно уже обработано.");
  assert.equal(confirmErrorText(new Error("Это предложение уже подтверждено или отклонено.")), "Это предложение уже подтверждено или отклонено.");
  assert.equal(confirmErrorText("dial tcp 10.0.0.1: boom"), genericError);
  assert.equal(confirmErrorText(undefined), genericError);
});

// ---- Contents ----

test("send card shows to whom, what and which version, and what «Да» does", () => {
  const out = view(card({ notice: "В файле указан другой адрес — не используется" }));
  assert.match(out, /Отправка письма/);
  for (const t of ["teacher@example.test", "ДЗ по матанализу", "Задачи 1–7", "dz.txt (TXT, 2 КБ)", "Версия 3", "В файле указан другой адрес — не используется", "только после «Да»"]) {
    assert.ok(out.includes(t), t);
  }
  assert.match(out, /Кому/);
  assert.match(out, />Нет</);
  assert.match(out, />Да</);
  assert.doesNotMatch(view(card()), /другой адрес/);
});

test("deadline card shows the calendar, the assignment and the date", () => {
  const m = confirmCardModel(deadline());
  assert.deepEqual(m.rows.map((r) => r.label), ["Куда", "Что", "Предмет", "Срок"]);
  assert.equal(m.rows[1].value, "Задачи 1–7");
  assert.match(m.rows[3].value, /24 окт\./);
  assert.match(view(deadline()), /Google Календарь/);
});

test("delete card lists what goes and says it is irreversible; a refusal has no buttons", () => {
  const out = view(deletion());
  assert.match(out, /Лекция 5/);
  assert.match(out, /Звук/);
  assert.match(out, /без возможности восстановления/);
  const refused = view(deletion({ deletion: { ...deletion().deletion, deletable: false, refusalMessage: "Запись ещё идёт: сначала остановите её." } }));
  assert.match(refused, /Запись ещё идёт/);
  assert.doesNotMatch(refused, /<button/);
  const adhoc = confirmCardModel(deletion({ deletion: { ...deletion().deletion, title: "" } }));
  assert.match(adhoc.rows[0].value, /^Запись \d\d\.\d\d/);
});

test("sizes read in Russian units", () => {
  assert.equal(sizeText(0), "");
  assert.equal(sizeText(512), "512 Б");
  assert.equal(sizeText(2048), "2 КБ");
  assert.equal(sizeText(1.5 * 1024 * 1024), "1,5 МБ");
});

test("states: waiting offers both buttons, confirmed only «Да», the rest none", () => {
  assert.match(view(card()), /Нет.*Да/s);
  const again = view(card({ state: "confirmed" }));
  assert.match(again, />Да</);
  assert.doesNotMatch(again, />Нет</);
  assert.match(again, /не создаст дубль/);
  const cases = [
    ["done", "", /Письмо отправлено/],
    ["declined", "", /Отклонено/],
    ["stale", "Файл изменился.", /Файл изменился/],
    ["rejected", "Отправить нельзя: подготовьте отправку ещё раз.", /Отправить нельзя/],
  ];
  for (const [state, message, text] of cases) {
    const out = view(card({ state, message }));
    assert.match(out, text, state);
    assert.doesNotMatch(out, /<button/, state);
  }
});

test("«Да» is never focused by default and busy turns the buttons off", () => {
  assert.doesNotMatch(view(card()), /autofocus/i);
  const busy = html(h(ConfirmCardView, { model: confirmCardModel(card()), busy: "confirm", error: "", onYes: noop, onNo: noop }));
  assert.equal((busy.match(/<button[^>]*disabled/g) ?? []).length, 2);
});

// ---- Plain text ----

test("hostile strings stay text on every card", () => {
  const send = card({ title: evil, subject: evil, recipient: evil, emailSubject: evil, attachmentName: evil, notice: evil });
  const del = deletion({ deletion: { ...deletion().deletion, title: evil, deletes: [evil] } });
  const dead = deadline({ title: evil, subject: evil });
  const msg = card({ state: "rejected", message: evil });
  for (const c of [send, del, dead, msg]) {
    const out = view(c);
    assert.ok(!out.includes("<img"), out);
    assert.ok(out.includes("&lt;img"), out);
    assert.doesNotMatch(out, /<a[ >]|mailto:|dangerouslySetInnerHTML/);
  }
});

test("an address and a url in a card are not turned into links", () => {
  const out = view(card({ recipient: "x@example.test", title: "см. https://example.test/a" }));
  assert.doesNotMatch(out, /<a[ >]/);
});

// ---- Click guard ----

test("a double click on «Да» calls the core once", async () => {
  const api = fakeApi();
  const flow = new ConfirmFlow(api, noop);
  flow.show(card());
  const clicks = [flow.yes(), flow.yes(), flow.yes()];
  api.release();
  await Promise.all(clicks);
  assert.equal(api.calls.confirm, 1);
  assert.equal(flow.state.card.state, "done");
  assert.equal(flow.state.busy, "");
  await flow.yes(); // a finished card has no «Да»
  assert.equal(api.calls.confirm, 1);
});

test("«Да» sends back what the card showed", async () => {
  let got = null;
  const api = fakeApi({ confirm: async (c) => ((got = c), { ...c, state: "done" }) });
  const flow = new ConfirmFlow(api, noop);
  flow.show(card());
  await flow.yes();
  assert.equal(got.emailSubject, "ДЗ по матанализу");
  assert.equal(got.attachmentFormat, "txt");
});

test("nothing confirms by itself: loading a card calls neither confirm nor decline", async () => {
  const api = fakeApi();
  const flow = new ConfirmFlow(api, noop);
  await flow.load({ kind: "proposal", id: "p1" });
  assert.equal(flow.state.card.state, "waiting");
  assert.equal(api.calls.confirm, 0);
  assert.equal(api.calls.decline, 0);
});

// ---- «Нет» ----

test("«Нет» declines the proposal and the card shows it; a second «Нет» does nothing", async () => {
  const api = fakeApi();
  const flow = new ConfirmFlow(api, noop);
  flow.show(card());
  await Promise.all([flow.no(), flow.no()]);
  assert.equal(api.calls.decline, 1);
  assert.equal(flow.state.card.state, "declined");
  await flow.no();
  await flow.yes();
  assert.equal(api.calls.decline, 1);
  assert.equal(api.calls.confirm, 0);
});

test("«Да» on a deletion preview opens the deletion window; «Нет» only changes the card", async () => {
  const api = fakeApi();
  api.decline = async (t) => (api.calls.decline++, deletion({ state: "declined", targetId: t.id }));
  const flow = new ConfirmFlow(api, noop);
  flow.show(deletion());
  await flow.yes();
  assert.equal(flow.state.deleteOpen, true);
  assert.equal(api.calls.confirm, 0);
  await flow.yes();
  assert.equal(flow.state.deleteOpen, true);
  flow.closeDelete();
  assert.equal(flow.state.card.state, "waiting");
  await flow.no();
  assert.equal(flow.state.card.state, "declined");
  assert.equal(api.calls.confirm, 0);

  const done = new ConfirmFlow(fakeApi(), noop);
  done.show(deletion());
  await done.yes();
  done.deleted();
  assert.equal(done.state.card.state, "done");
  assert.match(view(done.state.card), /Запись удалена/);
});

// ---- Error state ----

test("a refused «Да» shows the core's text, keeps «Да» available and does not claim success", async () => {
  let n = 0;
  const api = fakeApi({
    confirm: async (c) => {
      if (n++ === 0) {
        throw "Календарь не подключён: подключите Google в настройках и нажмите «Да» ещё раз.";
      }
      return { ...c, state: "done" };
    },
  });
  const flow = new ConfirmFlow(api, noop);
  flow.show(deadline());
  await flow.yes();
  assert.match(flow.state.error, /Календарь не подключён/);
  assert.equal(flow.state.card.state, "waiting");
  assert.equal(flow.state.busy, "");
  const out = html(h(ConfirmCardView, { model: confirmCardModel(flow.state.card), busy: "", error: flow.state.error, onYes: noop, onNo: noop }));
  assert.match(out, /role="alert"/);
  assert.match(out, />Да</);
  await flow.yes();
  assert.equal(flow.state.error, "");
  assert.equal(flow.state.card.state, "done");
});

test("an unknown error text becomes the generic one; a failed load can be retried", async () => {
  let fail = true;
  const api = fakeApi({
    card: async () => {
      if (fail) {
        throw new Error("sqlite: disk I/O error");
      }
      return card();
    },
  });
  const flow = new ConfirmFlow(api, noop);
  await flow.load({ kind: "proposal", id: "p1" });
  assert.equal(flow.state.error, genericError);
  assert.equal(flow.state.card, null);
  fail = false;
  await flow.load({ kind: "proposal", id: "p1" });
  assert.equal(flow.state.error, "");
  assert.equal(flow.state.card.kind, "send");
});

test("an unloaded card shows loading and no buttons", () => {
  const out = html(h(ConfirmCard, { api: fakeApi(), deleteApi: {}, target: { kind: "proposal", id: "p1" } }));
  assert.match(out, /Загрузка карточки/);
  assert.doesNotMatch(out, /<button/);
});

// ---- In the panel ----

const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const confirmUpdate = (target) => ({
  threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "AWAITING_CONFIRM", stopReason: "", problem: "",
  cards: [{ kind: "confirm", plan: null, questions: null, jobId: "", artifact: "", target, actionId: "", answerId: "", cannotDo: null, text: evil, stopReason: "" }],
});
const modelOf = (u) => entryModel({ key: "k", seq: 1, threadId: "t1", turn: 1, text: "", scope: null, callProblem: "", update: u });

test("the panel's confirm card reads its target from the core and ignores the agent's text", () => {
  const withCalls = { ...actions, confirm: { api: fakeApi(), deleteApi: {} } };
  const out = html(h(AgentRequest, { model: modelOf(confirmUpdate({ kind: "proposal", id: "p1" })), actions: withCalls }));
  assert.match(out, /Нужно подтверждение/);
  assert.match(out, /Загрузка карточки/);
  assert.ok(!out.includes("<img"));
  // A target of another kind has no card to load.
  const odd = html(h(AgentRequest, { model: modelOf(confirmUpdate({ kind: "work", id: "w1" })), actions: withCalls }));
  assert.match(odd, /пока недоступно/);
  assert.doesNotMatch(odd, /<button/);
});

// ---- «Сегодня» ----

const post = (calendar) => ({
  recordingId: "r1", transcriptId: "t1", headline: "Конспект готов", summaryReady: true, homework: 1, earliestDue: "2026-10-24T12:00:00Z",
  shownAt: "2026-10-22T12:00:00Z", calendar,
  lesson: { lessonId: "l1", title: "Матан", subject: "", subjectId: "", subjectName: "Матанализ", kind: "", start: "2026-10-22T09:00:00Z" },
});

test("posts offer «Да, в календарь» for a deadline action only, and only when the calls are given", () => {
  const cal = [{ kind: "create_deadline_event", assignmentId: "a1", due: "2026-10-24T12:00:00Z" }, { kind: "other", assignmentId: "a2", due: "" }];
  const calls = { api: fakeApi(), deleteApi: {} };
  const withCalls = html(h(PostsSection, { posts: [post(cal)], onOpenSummary: noop, calendar: calls }));
  assert.equal((withCalls.match(/Да, в календарь/g) ?? []).length, 1);
  assert.match(withCalls, /24 окт\./);
  assert.doesNotMatch(html(h(PostsSection, { posts: [post(cal)], onOpenSummary: noop })), /Да, в календарь/);
  assert.doesNotMatch(html(h(PostsSection, { posts: [post([])], onOpenSummary: noop, calendar: calls })), /Да, в календарь/);
  assert.equal(isDeadlineAction("create_deadline_event", ""), false);
  assert.equal(isDeadlineAction("create_deadline_event", "a1"), true);
});

test("«Да, в календарь» makes one proposal, then the card asks «Да»; the calendar is written by Confirm only", async () => {
  const api = fakeApi();
  const flow = new ProposeFlow(api, "a1", noop);
  const clicks = [flow.propose(), flow.propose(), flow.propose()];
  assert.equal(flow.state.busy, true);
  api.release();
  await Promise.all(clicks);
  assert.equal(api.calls.propose, 1);
  assert.equal(api.calls.confirm, 0);
  assert.equal(flow.state.card.state, "waiting");
  await flow.propose();
  assert.equal(api.calls.propose, 1);

  const conf = new ConfirmFlow(api, noop);
  conf.show(flow.state.card);
  await conf.yes();
  assert.equal(api.calls.confirm, 1);
  assert.equal(conf.state.card.state, "done");
  assert.match(view(conf.state.card), /Срок добавлен в календарь/);
});

test("a refused proposal shows the text and the button can be pressed again", async () => {
  let n = 0;
  const api = fakeApi({
    proposeDeadline: async () => {
      if (n++ === 0) {
        throw "У задания нет срока: укажите его в «Домашних заданиях».";
      }
      return deadline();
    },
  });
  const flow = new ProposeFlow(api, "a1", noop);
  await flow.propose();
  assert.match(flow.state.error, /нет срока/);
  assert.equal(flow.state.card, null);
  const out = html(h(CalendarButton, { due: "", busy: false, error: flow.state.error, onClick: noop }));
  assert.match(out, /role="alert"/);
  assert.match(out, /Да, в календарь/);
  await flow.propose();
  assert.equal(flow.state.error, "");
  assert.equal(flow.state.card.kind, "deadline");
  assert.equal(calendarTexts.button, "Да, в календарь");
  assert.equal(confirmTexts.yes, "Да");
});

test("texts are impersonal", () => {
  const all = JSON.stringify([confirmTexts, calendarTexts]);
  assert.doesNotMatch(all, /(^|[^А-Яа-я])(Я|я|Вы|вы|Ваш\w*|ваш\w*)([^А-Яа-я]|$)/);
});
