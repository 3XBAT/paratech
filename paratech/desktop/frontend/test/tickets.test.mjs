// Tickets screen (TASK-134): the binding glue, the state mapping, the cost gating, the not-covered mapping and the
// plain-text rendering. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { methods, parseSet, ticketsApi, ticketsErrorText } from "../.test-build/bindings/tickets.js";
import {
  budgetLine,
  canOpenDocx,
  confirmGate,
  openResultView,
  fileNote,
  moves,
  openingId,
  partLabel,
  partsList,
  needsCostConsent,
  needsPolling,
  percent,
  progressText,
  reportParts,
  statusView,
  ticketRow,
  ticketRows,
  undoLabel,
  usd,
  withoutResults,
  materialIdsOf,
} from "../.test-build/ticketsView.js";
import { FilesList, SetCard } from "../.test-build/pages/TicketsParts.js";

function wireSet(over = {}) {
  return {
    id: "ts-1",
    subjectId: "sub-1",
    sourceId: "",
    status: "proposed",
    tickets: [
      { number: 1, title: "Первый билет", answered: false, coverage: "", answerId: "" },
      { number: 2, title: "Второй билет", answered: false, coverage: "", answerId: "" },
    ],
    done: 0,
    total: 2,
    budget: { tickets: 2, estimateMicroUsd: 180000, capMicroUsd: 3000000, overCap: false },
    costAccepted: false,
    report: { covered: 0, partial: 0, notCovered: 0, failed: 0, pending: 2 },
    materialId: "",
    result: "",
    job: "",
    jobStatus: "",
    stalled: false,
    version: 1,
    ...over,
  };
}

const set = (over) => parseSet(wireSet(over));

test("the binding names match the Go methods and the wrappers parse and pass arguments", async () => {
  assert.equal(methods.confirm, "paratech/desktop/internal/input/wails.TicketService.Confirm");
  const calls = [];
  const api = ticketsApi(async (m, ...args) => {
    calls.push([m, ...args]);
    return m === methods.list ? [wireSet()] : wireSet();
  });
  assert.equal((await api.list("sub-1")).length, 1);
  await api.confirm("ts-1", true);
  await api.propose("sub-1", "1. a");
  await api.proposeFromMaterial("mat-1");
  await api.cancel("ts-1");
  await api.undo("ts-1");
  await api.get("ts-1");
  assert.deepEqual(calls[1], [methods.confirm, "ts-1", true]);
  assert.deepEqual(calls[2], [methods.propose, "sub-1", "1. a"]);
});

test("a null list is empty and a malformed reply is an error", async () => {
  assert.deepEqual(await ticketsApi(async () => null).list("s"), []);
  assert.equal(parseSet({ ...wireSet(), tickets: null }).tickets.length, 0);
  assert.throws(() => parseSet({ ...wireSet(), status: "paid" }));
  assert.throws(() => parseSet({ ...wireSet(), budget: null }));
});

test("files: two parts are parsed as sent", () => {
  const files = [
    { materialId: "m-1", from: 1, to: 50, name: "Билеты 1–50.docx" },
    { materialId: "m-2", from: 51, to: 100, name: "Билеты 51–100.docx" },
  ];
  assert.deepEqual(set({ status: "done", materialId: "m-1", files }).files, files);
});

test("files absent: one derived part with a material id, none without", () => {
  const { files: _drop, ...older } = wireSet({ status: "done", materialId: "m-1" });
  assert.deepEqual(parseSet(older).files, [{ materialId: "m-1", from: 0, to: 0, name: "" }]);
  const { files: _drop2, ...noMaterial } = wireSet();
  assert.deepEqual(parseSet(noMaterial).files, []);
  assert.deepEqual(parseSet({ ...noMaterial, files: undefined }).files, []);
});

test("files null is treated as absent", () => {
  assert.deepEqual(set({ status: "done", materialId: "m-1", files: null }).files, [{ materialId: "m-1", from: 0, to: 0, name: "" }]);
  assert.deepEqual(set({ files: null }).files, []);
});

test("files present are used as is: an empty list stays empty", () => {
  assert.deepEqual(set({ status: "done", materialId: "m-1", files: [] }).files, []);
});

test("a malformed file item is an error", () => {
  const ok = { materialId: "m-1", from: 1, to: 50, name: "a.docx" };
  assert.throws(() => set({ files: [{ from: 1, to: 50, name: "a.docx" }] }));
  assert.throws(() => set({ files: [{ ...ok, materialId: "" }] }));
  assert.throws(() => set({ files: [{ ...ok, from: "1" }] }));
  assert.throws(() => set({ files: [{ ...ok, to: -1 }] }));
  assert.throws(() => set({ files: [{ ...ok, name: 5 }] }));
  assert.throws(() => set({ files: [null] }));
  assert.throws(() => set({ files: "m-1" }));
});

test("list keeps working with sets that carry files", async () => {
  const files = [{ materialId: "m-1", from: 1, to: 50, name: "a.docx" }];
  const api = ticketsApi(async () => [wireSet({ status: "done", materialId: "m-1", files }), wireSet({ id: "ts-2" })]);
  const sets = await api.list("sub-1");
  assert.equal(sets.length, 2);
  assert.deepEqual(sets[0].files, files);
  assert.deepEqual(sets[1].files, []);
});

test("error texts: known ones as they are, others generic", () => {
  assert.equal(ticketsErrorText(new Error("Прогон дороже обычного: подтвердите стоимость.")), "Прогон дороже обычного: подтвердите стоимость.");
  assert.equal(ticketsErrorText("Слишком много билетов: не больше 100 за один раз."), "Слишком много билетов: не больше 100 за один раз.");
  assert.equal(ticketsErrorText(new Error("rpc failed: secret detail")), "Не удалось выполнить. Попробуйте ещё раз.");
});

test("money is shown in USD from micro-USD", () => {
  assert.equal(usd(2970000), "$2,97");
  assert.equal(usd(3000000), "$3,00");
  assert.equal(usd(90000), "$0,09");
  assert.equal(usd(1), "$0,01");
  assert.equal(usd(0), "$0,00");
  assert.equal(usd(-1), "—");
  assert.match(budgetLine(set()), /\$0,18 за 2 билета; предел обычного прогона — \$3,00/);
});

test("status mapping: proposed, running, waits, stalled, done, failed, cancelled", () => {
  assert.equal(statusView(set()).label, "Ждёт подтверждения");
  assert.equal(statusView(set({ status: "answering", jobStatus: "RUNNING" })).kind, "run");
  assert.equal(statusView(set({ status: "confirmed", jobStatus: "QUEUED" })).label, "В очереди");
  assert.equal(statusView(set({ status: "answering", jobStatus: "WAITING_FOR_NETWORK" })).kind, "net");
  assert.equal(statusView(set({ status: "answering", jobStatus: "WAITING_FOR_QUOTA" })).label, "Ждёт пополнения минут");
  assert.equal(statusView(set({ status: "answering", jobStatus: "WAITING_FOR_POWER" })).label, "Ждёт зарядки");
  assert.equal(statusView(set({ status: "answering", jobStatus: "RETRY_WAIT" })).label, "Ждёт повтора");
  const stalled = statusView(set({ status: "answering", jobStatus: "FAILED", stalled: true }));
  assert.equal(stalled.label, "Подбор остановлен");
  assert.equal(stalled.kind, "fail");
  assert.equal(statusView(set({ status: "done" })).kind, "ok");
  assert.equal(statusView(set({ status: "failed" })).kind, "fail");
  assert.equal(statusView(set({ status: "cancelled" })).kind, "neutral");
});

test("progress is answered of total", () => {
  const s = set({ status: "answering", done: 3, total: 12 });
  assert.equal(progressText(s), "Готово 3 из 12");
  assert.equal(percent(s), 25);
  assert.equal(percent(set({ total: 0 })), 0);
});

test("moves: confirm only for a proposal or a stalled run, cancel while it lives, undo after a filed result", () => {
  assert.deepEqual(moves(set()), { confirm: "start", cancel: true, undo: false });
  assert.deepEqual(moves(set({ status: "answering", jobStatus: "RUNNING" })), { confirm: "none", cancel: true, undo: false });
  assert.deepEqual(moves(set({ status: "answering", stalled: true })), { confirm: "restart", cancel: true, undo: false });
  assert.deepEqual(moves(set({ status: "done", materialId: "m-1" })), { confirm: "none", cancel: false, undo: true });
  assert.deepEqual(moves(set({ status: "done" })), { confirm: "none", cancel: false, undo: false });
  assert.deepEqual(moves(set({ status: "cancelled" })), { confirm: "none", cancel: false, undo: false });
});

test("a run over the cap is never allowed without the ticked consent; acceptCost follows the tick only", () => {
  const cheap = set();
  assert.deepEqual(confirmGate(cheap, false), { allowed: true, acceptCost: false });
  assert.deepEqual(confirmGate(cheap, true), { allowed: true, acceptCost: false });

  const over = set({ budget: { tickets: 40, estimateMicroUsd: 3600000, capMicroUsd: 3000000, overCap: true }, total: 40 });
  assert.equal(needsCostConsent(over), true);
  assert.deepEqual(confirmGate(over, false), { allowed: false, acceptCost: false });
  assert.deepEqual(confirmGate(over, true), { allowed: true, acceptCost: true });

  // Only the core's flag decides, not the ticket count.
  const many = set({ total: 34, budget: { tickets: 34, estimateMicroUsd: 1, capMicroUsd: 3000000, overCap: false } });
  assert.equal(needsCostConsent(many), false);
  assert.equal(confirmGate(many, false).allowed, true);

  // A stalled restart never asks for the cost again.
  const stalled = set({ status: "answering", stalled: true, budget: over.budget });
  assert.deepEqual(confirmGate(stalled, false), { allowed: true, acceptCost: false });
  assert.equal(needsPolling(stalled), false);
  assert.equal(needsPolling(set({ status: "answering" })), true);
});

test("not-covered mapping: a missing answer is «Не покрыто», never green", () => {
  const done = set({ status: "done" });
  const t = (over) => ({ number: 1, title: "T", answered: true, coverage: "covered", answerId: "a-1", ...over });
  assert.equal(ticketRow(t(), done).chip.label, "Покрыто");
  assert.equal(ticketRow(t({ coverage: "partial" }), done).chip.label, "Покрыто частично");
  assert.equal(ticketRow(t({ coverage: "not_covered" }), done).chip.label, "Не покрыто");
  // Recording deleted: the answer is gone but the coverage still says covered.
  assert.equal(ticketRow(t({ answered: false, answerId: "" }), done).chip.label, "Не покрыто");
  assert.equal(ticketRow(t({ answerId: "" }), done).chip.label, "Не покрыто");
  // Never answered in a finished set.
  assert.equal(ticketRow(t({ answered: false, coverage: "", answerId: "" }), done).chip.label, "Не покрыто");
  // Not yet answered in a running set is only waiting.
  const running = set({ status: "answering" });
  assert.equal(ticketRow(t({ answered: false, coverage: "", answerId: "" }), running).chip.label, "Ждёт ответа");
  assert.equal(ticketRow(t({ coverage: "failed" }), running).chip.kind, "fail");
  assert.equal(ticketRows(done).length, 2);
});

test("report parts leave out zeros", () => {
  assert.deepEqual(reportParts({ covered: 5, partial: 0, notCovered: 2, failed: 0, pending: 0 }), ["5 покрыто", "2 не покрыто"]);
});

test("the file note names the folder «Билеты» and the subject", () => {
  assert.equal(fileNote("Физика"), "Файл лежит в папке «Билеты» предмета «Физика».");
});

test("titles are rendered as plain text, not markup or links", () => {
  const evil = '<img src=x onerror=alert(1)> <a href="https://evil.example">x</a> **bold**';
  const s = set({
    status: "done",
    done: 1,
    materialId: "m-1",
    tickets: [{ number: 1, title: evil, answered: true, coverage: "covered", answerId: "a-1" }],
  });
  const html = server.renderToStaticMarkup(
    h(SetCard, { set: s, subjectName: "Физика", busy: false, error: "", onConfirm() {}, onCancel() {}, onUndo() {} }),
  );
  assert.ok(!html.includes("<img"), "no element made of the title");
  assert.ok(!html.includes("<a "), "no link made of the title");
  assert.ok(html.includes("&lt;img src=x"), "the title is escaped text");
  assert.ok(html.includes("**bold**"), "no markdown");
  assert.ok(html.includes("Билеты"), "the folder is named");
  assert.ok(html.includes("Вернуть файл во «Входящие»"));
  assert.ok(html.includes("Открыть .docx"), "the open button is there");
});

const card = (s, over = {}) =>
  server.renderToStaticMarkup(
    h(SetCard, { set: s, subjectName: "Физика", busy: false, blocked: false, error: "", openBusy: false, openResult: null, onConfirm() {}, onCancel() {}, onUndo() {}, onOpen() {}, ...over }),
  );

test("«Открыть .docx» is shown only for a done set with a material, with the copy note", () => {
  const done = card(set({ status: "done", done: 2, materialId: "m-1" }));
  assert.ok(done.includes("Открыть .docx"));
  assert.ok(done.includes("Открывается копия файла: правки в Word в библиотеку не попадают."));
  for (const over of [{ status: "proposed" }, { status: "answering" }, { status: "confirmed" }, { status: "done", materialId: "" }]) {
    const html = card(set(over));
    assert.ok(!html.includes("Открыть .docx"), JSON.stringify(over));
    assert.ok(!html.includes("копия файла"), JSON.stringify(over));
  }
});

test("the open button is blocked while an action runs and shows the busy label", () => {
  const s = set({ status: "done", done: 2, materialId: "m-1" });
  assert.match(card(s, { blocked: true }), /<button[^>]*disabled=""[^>]*>[^]*?Открыть \.docx/);
  assert.ok(card(s, { openingId: "m-1", blocked: true }).includes("Открывается…"));
});

test("the open result line: success says a copy was opened, a failure shows the binding text", () => {
  assert.deepEqual(openResultView({ status: "opened", message: "Файл открывается в программе по умолчанию." }), {
    kind: "ok",
    text: "Открыта копия файла: правки в Word в библиотеку не попадают.",
  });
  assert.deepEqual(openResultView({ status: "not_found", message: "Файл не найден: возможно, его удалили." }), {
    kind: "fail",
    text: "Файл не найден: возможно, его удалили.",
  });
  const s = set({ status: "done", done: 2, materialId: "m-1" });
  assert.ok(card(s, { openResults: { "m-1": { kind: "ok", text: "Открыта копия файла: x" } } }).includes("Открыта копия файла: x"));
});

test("canOpenDocx needs a done set with a material id", () => {
  assert.equal(canOpenDocx(set({ status: "done", materialId: "m-1" })), true);
  assert.equal(canOpenDocx(set({ status: "done", materialId: "" })), false);
  assert.equal(canOpenDocx(set({ status: "failed", materialId: "m-1" })), false);
});

test("the page opens the file through the library binding with the material id only", () => {
  const page = readFileSync(new URL("../src/pages/TicketsPage.tsx", import.meta.url), "utf8");
  assert.equal((page.match(/lib\.openMaterial\(/g) ?? []).length, 1);
  assert.ok(page.includes("lib.openMaterial(materialId)"), "only the id is passed");
});

// ---- TASK-403: the parts of a split tickets file ----

const part = (from, to, id) => ({ materialId: id, from, to, name: `${id}.docx` });
const threeParts = [part(1, 50, "m-1"), part(51, 100, "m-2"), part(101, 120, "m-3")];
const splitSet = (files = threeParts, over = {}) => set({ status: "done", done: 2, materialId: files[0]?.materialId ?? "", files, ...over });
const partNote = "Файл разделён на части: не помещается в один документ.";
const count = (html, text) => html.split(text).length - 1;

test("partLabel: a range, one ticket, an unknown range by position", () => {
  assert.equal(partLabel(part(1, 50, "a"), 0), "Билеты 1–50");
  assert.equal(partLabel(part(51, 100, "a"), 1), "Билеты 51–100");
  assert.equal(partLabel(part(7, 7, "a"), 2), "Билет 7");
  assert.equal(partLabel(part(0, 0, "a"), 0), "Часть 1");
  assert.equal(partLabel(part(0, 0, "a"), 1), "Часть 2");
  assert.deepEqual(partsList(splitSet()).map((p) => p.openLabel), ["Открыть «Билеты 1–50»", "Открыть «Билеты 51–100»", "Открыть «Билеты 101–120»"]);
});

test("one part: no list, no split note, the word button and the singular file note as before", () => {
  const html = card(splitSet([part(1, 50, "m-1")]));
  assert.ok(html.includes("Открыть .docx"));
  assert.ok(!html.includes("tk-files"));
  assert.ok(!html.includes(partNote));
  assert.ok(!html.includes("Билеты 1–50"));
  assert.ok(html.includes("Файл лежит в папке «Билеты» предмета «Физика»."));
  assert.ok(html.includes("Вернуть файл во «Входящие»"));
  assert.equal(fileNote("Физика"), "Файл лежит в папке «Билеты» предмета «Физика».");
});

// The elements of a rendered-by-hand tree (a plain function component's result) and the rendered tag of a button by its name.
const walk = (n, out = []) => {
  if (Array.isArray(n)) {
    n.forEach((c) => walk(c, out));
  } else if (n && typeof n === "object" && n.props) {
    out.push(n);
    walk(n.props.children, out);
  }
  return out;
};
const byLabel = (tree, label) => walk(tree).find((e) => e.props.label === label);
const buttonTag = (html, label) => (html.match(/<button[^>]*>/g) ?? []).find((t) => t.includes(`aria-label="${label}"`));
const labels = ["Билеты 1–50", "Билеты 51–100", "Билеты 101–120"];
const undoThree = "Вернуть все 3 файла во «Входящие»";

test("three parts: labels in order, an open icon per part, the note once, the plural file note, one undo", () => {
  const html = card(splitSet());
  const at = labels.map((l) => html.indexOf(`>${l}<`));
  assert.ok(at.every((i) => i > 0) && at[0] < at[1] && at[1] < at[2], "the labels are listed in order");
  for (const l of labels) {
    assert.ok(buttonTag(html, `Открыть «${l}»`), l);
  }
  assert.ok(!html.includes("Открыть .docx"), "no word button with several parts");
  assert.equal(count(html, partNote), 1);
  assert.ok(html.includes("Файлы лежат в папке «Билеты» предмета «Физика»."));
  assert.ok(buttonTag(html, undoThree), "one undo icon for all parts, with the count");
  assert.ok(!html.includes("Вернуть файл во"));
  assert.equal(count(html, "Открывается копия файла: правки в Word в библиотеку не попадают."), 1, "the copy note is said once");
  assert.equal(fileNote("", 2), "Файлы лежат в папке «Билеты» предмета.");
});

test("done block order: several parts = split note, file note, the list, then the muted notes; one part as before", () => {
  const many = card(splitSet());
  const at = [partNote, "Файлы лежат в папке", 'class="tk-files"', "Открывается копия файла", "Ответы сгенерированы по лекциям"].map((t) => many.indexOf(t));
  assert.ok(at.every((i) => i > 0), JSON.stringify(at));
  assert.ok(at[0] < at[1] && at[1] < at[2], "split note, file note, then the list");
  assert.ok(at[2] < at[3] && at[3] < at[4], "the notes of the whole file sit under the list");
  const one = card(splitSet([part(1, 50, "m-1")]));
  const o = ["Файл лежит в папке", "Открывается копия файла", "Ответы сгенерированы по лекциям", "Открыть .docx"].map((t) => one.indexOf(t));
  assert.ok(o.every((i) => i > 0) && o[0] < o[1] && o[1] < o[2] && o[2] < o[3], "single card: file note, notes, then the button");
});

test("the open callback of a part gets that part's material id (and only it)", () => {
  const clicked = [];
  const list = FilesList({ parts: partsList(splitSet()), blocked: false, openingId: "", results: {}, onOpen: (id) => clicked.push(id) });
  for (const l of labels) {
    byLabel(list, `Открыть «${l}»`).props.onClick();
  }
  assert.deepEqual(clicked, ["m-1", "m-2", "m-3"]);
});

test("undo tooltip names the count of files with the right plural; one file keeps its text", () => {
  assert.equal(undoLabel(1), "Вернуть файл во «Входящие»");
  assert.equal(undoLabel(2), "Вернуть все 2 файла во «Входящие»");
  assert.equal(undoLabel(5), "Вернуть все 5 файлов во «Входящие»");
  assert.equal(undoLabel(21), "Вернуть все 21 файл во «Входящие»");
  assert.equal(undoLabel(12), "Вернуть все 12 файлов во «Входящие»");
});

test("a reply without files: parseSet derives one part, the card is the single-part card", () => {
  const s = set({ status: "done", done: 2, materialId: "m-9" });
  assert.deepEqual(s.files, [{ materialId: "m-9", from: 0, to: 0, name: "" }]);
  const html = card(s);
  assert.ok(html.includes("Открыть .docx"));
  assert.ok(!html.includes("tk-files") && !html.includes(partNote));
  assert.equal(canOpenDocx(s), true);
  assert.equal(moves(s).undo, true);
});

test("unknown ranges of several parts are «Часть 1», «Часть 2»; the file name is never shown", () => {
  const html = card(splitSet([part(0, 0, "m-1"), part(0, 0, "m-2")]));
  assert.ok(html.includes(">Часть 1<") && html.includes(">Часть 2<"));
  assert.ok(html.includes("Открыть «Часть 2»"));
  assert.ok(!html.includes("m-1.docx"));
});

test("a part of one ticket is «Билет N»", () => {
  const html = card(splitSet([part(1, 50, "m-1"), part(51, 51, "m-2")]));
  assert.ok(html.includes(">Билет 51<"));
});

test("canOpenDocx and undo count the parts; a done set without files shows no open controls", () => {
  assert.equal(canOpenDocx(splitSet()), true);
  assert.equal(moves(splitSet()).undo, true);
  assert.equal(canOpenDocx(set({ status: "done", files: [] })), false);
  assert.equal(moves(set({ status: "done", files: [] })).undo, false);
  assert.equal(canOpenDocx(set({ status: "failed", files: threeParts, materialId: "m-1" })), false);
  const html = card(set({ status: "done", files: [] }));
  assert.ok(!html.includes("Открыть") && !html.includes("tk-files") && !html.includes("Вернуть"));
  assert.ok(html.includes("Ответы сгенерированы по лекциям"));
});

test("per-part state: the busy part spins, the others and the undo are off with a reason, each part has its own result line", () => {
  const busy = card(splitSet(), { openingId: "m-2", blocked: true });
  assert.equal(count(busy, 'aria-busy="true"'), 1);
  assert.ok(buttonTag(busy, "Открыть «Билеты 51–100»").includes('aria-busy="true"'));
  for (const l of ["Билеты 1–50", "Билеты 101–120"]) {
    const tag = buttonTag(busy, `Открыть «${l}»`);
    assert.ok(tag.includes('aria-disabled="true"'), `${l} is off`);
    assert.ok(tag.includes("aria-describedby"), `${l} says why to a screen reader`);
  }
  const undo = buttonTag(busy, undoThree);
  assert.ok(undo.includes('aria-disabled="true"') && undo.includes("aria-describedby"), "undo is off with a reason");
  assert.equal(count(busy, "Открывается другой файл."), 6, "tooltip and screen reader text of the two parts and the undo");
  const idle = card(splitSet());
  assert.equal(count(idle, 'aria-busy="true"'), 0);
  assert.ok(!idle.includes("Открывается другой файл."), "no reason while nothing opens");
  for (const l of labels) {
    assert.ok(!buttonTag(idle, `Открыть «${l}»`).includes("disabled"), `${l} is on`);
  }
  assert.ok(!buttonTag(idle, undoThree).includes("disabled"));
});

test("another action that is not an open disables the icons without the «other file» reason", () => {
  const html = card(splitSet(), { blocked: true });
  for (const l of labels) {
    assert.ok(buttonTag(html, `Открыть «${l}»`).includes('disabled=""'), l);
  }
  assert.ok(!html.includes("Открывается другой файл."));
});

test("a part's own line: the short success «Открыта копия.» and the binding's refusal; the note is not repeated", () => {
  const results = {
    "m-1": { kind: "ok", text: "Открыта копия файла: правки в Word в библиотеку не попадают." },
    "m-3": { kind: "fail", text: "Файл не найден: возможно, его удалили." },
  };
  const html = card(splitSet(), { openResults: results });
  assert.equal(count(html, "Открыта копия."), 1);
  assert.equal(count(html, "Открыта копия файла"), 0, "the long success text is not in the list");
  assert.equal(count(html, "Открывается копия файла: правки в Word в библиотеку не попадают."), 1, "the note stays once, above the list");
  assert.equal(count(html, "Файл не найден: возможно, его удалили."), 1);
  assert.ok(html.indexOf("Открыта копия.") < html.indexOf(">Билеты 51–100<"), "the line sits under its own part");
  assert.ok(html.indexOf("Файл не найден") > html.indexOf(">Билеты 101–120<"));
  assert.ok(html.includes('role="alert"'), "a failed part says why");
  // The single-part card keeps the full text.
  const one = card(splitSet([part(1, 50, "m-1")]), { openResults: { "m-1": results["m-1"] } });
  assert.equal(count(one, "Открыта копия файла: правки в Word в библиотеку не попадают."), 1);
  assert.ok(!one.includes("Открыта копия."));
});

test("the busy key names the material, so each part has its own state", () => {
  assert.equal(openingId("open:m-2"), "m-2");
  assert.equal(openingId("propose"), "");
  assert.equal(openingId(""), "");
});

test("a moved or confirmed set loses its open lines: the ids of every part are dropped, others stay", () => {
  assert.deepEqual(materialIdsOf(splitSet()), ["m-1", "m-2", "m-3"]);
  assert.deepEqual(materialIdsOf(set({ status: "done", materialId: "m-9" })), ["m-9"]);
  const results = { "m-1": { kind: "ok", text: "a" }, "m-2": { kind: "fail", text: "b" }, "x-1": { kind: "ok", text: "c" } };
  assert.deepEqual(withoutResults(results, ["m-1", "m-2", "m-3"]), { "x-1": { kind: "ok", text: "c" } });
  assert.deepEqual(withoutResults(results, []), results);
  assert.equal(results["m-1"].text, "a", "the input is not changed");
});

test("the page drops the lines of a set after its action (undo, confirm, cancel all go through one place)", () => {
  const page = readFileSync(new URL("../src/pages/TicketsPage.tsx", import.meta.url), "utf8");
  assert.ok(page.includes("withoutResults("), "the page uses the helper");
});

test("student text of the parts is plain text: a hostile file name is not shown at all", () => {
  const evil = '<img src=x onerror=alert(1)>.docx';
  const html = card(splitSet([{ ...part(1, 50, "m-1"), name: evil }, { ...part(51, 100, "m-2"), name: evil }]));
  assert.ok(!html.includes("<img") && !html.includes("onerror"));
});

test("the confirm card of a run over the cap shows the estimate and a disabled button until the consent", () => {
  const over = set({ total: 40, budget: { tickets: 40, estimateMicroUsd: 3600000, capMicroUsd: 3000000, overCap: true } });
  const html = server.renderToStaticMarkup(
    h(SetCard, { set: over, subjectName: "Физика", busy: false, error: "", onConfirm() {}, onCancel() {}, onUndo() {} }),
  );
  assert.ok(html.includes("$3,60"));
  assert.ok(html.includes('type="checkbox"'));
  assert.match(html, /<button[^>]*disabled=""[^>]*>[^]*?Подтвердить и запустить/);
});

test("the stalled card shows «Подбор остановлен» and the restart button", () => {
  const s = set({ status: "answering", stalled: true, jobStatus: "FAILED" });
  const html = server.renderToStaticMarkup(
    h(SetCard, { set: s, subjectName: "", busy: false, error: "", onConfirm() {}, onCancel() {}, onUndo() {} }),
  );
  assert.ok(html.includes("Подбор остановлен"));
  assert.ok(html.includes("Повторить"));
  assert.ok(!html.includes('type="checkbox"'), "no cost checkbox on a restart");
});

test("no source of the tickets screen uses raw HTML, and Confirm is only called from the page", () => {
  const files = ["pages/TicketsPage.tsx", "pages/TicketsParts.tsx", "ticketsView.ts", "bindings/tickets.ts"];
  for (const f of files) {
    const src = readFileSync(new URL(`../src/${f}`, import.meta.url), "utf8");
    assert.ok(!/dangerouslySetInnerHTML|innerHTML|insertAdjacentHTML/.test(src), `${f} uses raw HTML`);
  }
  const page = readFileSync(new URL("../src/pages/TicketsPage.tsx", import.meta.url), "utf8");
  assert.equal((page.match(/api\.confirm\(/g) ?? []).length, 1, "one Confirm call, behind the card's button");
  // Nothing outside the page and the binding touches the tickets binding.
  const root = new URL("../src/", import.meta.url);
  const walk = (dir) =>
    readdirSync(dir).flatMap((n) => {
      const p = new URL(n + (statSync(new URL(n, dir)).isDirectory() ? "/" : ""), dir);
      return statSync(p).isDirectory() ? walk(p) : [p];
    });
  for (const p of walk(root)) {
    if (!/\.tsx?$/.test(p.pathname) || /tickets|Tickets/.test(p.pathname)) continue;
    assert.ok(!/ticketsApi|bindings\/tickets/.test(readFileSync(p, "utf8")), `${p.pathname} uses the tickets binding`);
  }
});
