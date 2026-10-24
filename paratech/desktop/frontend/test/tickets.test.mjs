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
  confirmGate,
  fileNote,
  moves,
  needsCostConsent,
  percent,
  progressText,
  reportParts,
  statusView,
  ticketRow,
  ticketRows,
  usd,
} from "../.test-build/ticketsView.js";
import { SetCard } from "../.test-build/pages/TicketsParts.js";

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
  assert.equal(statusView(set({ status: "answering", jobStatus: "WAITING_NETWORK" })).kind, "net");
  assert.equal(statusView(set({ status: "answering", jobStatus: "WAITING_QUOTA" })).kind, "wait");
  const stalled = statusView(set({ status: "answering", jobStatus: "FAILED", stalled: true }));
  assert.equal(stalled.label, "Застряло");
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

  // The ticket count is a second guard when the core's flag is absent.
  const many = set({ total: 34, budget: { tickets: 34, estimateMicroUsd: 1, capMicroUsd: 3000000, overCap: false } });
  assert.equal(needsCostConsent(many), true);
  assert.equal(confirmGate(many, false).allowed, false);
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
  assert.ok(!html.includes("Открыть .docx</button>"), "there is no open button");
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

test("the stalled card shows «Застряло» and the restart button", () => {
  const s = set({ status: "answering", stalled: true, jobStatus: "FAILED" });
  const html = server.renderToStaticMarkup(
    h(SetCard, { set: s, subjectName: "", busy: false, error: "", onConfirm() {}, onCancel() {}, onUndo() {} }),
  );
  assert.ok(html.includes("Застряло"));
  assert.ok(html.includes("Запустить снова"));
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
