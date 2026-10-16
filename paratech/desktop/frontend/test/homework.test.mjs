// Homework page (TASK-045): view helpers, the bindings glue, the send dialogs and source guards. Runs on the modules
// compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorMessages, generationStates, homeworkApi, methods } from "../.test-build/bindings/homework.js";
import {
  assignmentDeliveryView,
  deliveriesInFlight,
  deliveryLooks,
  deliveryView,
  dueText,
  errorText,
  generationGoesOn,
  generationView,
  isStale,
  sizeText,
  versionTitle,
} from "../.test-build/homeworkView.js";
import { SendAgainDialog, SendDialog } from "../.test-build/pages/HomeworkDialogs.js";

const { renderToStaticMarkup } = server;

const deliveryStatuses = ["PENDING", "SENDING", "SENT", "FAILED", "UNKNOWN"];
const kinds = new Set(["rec", "run", "wait", "net", "ok", "fail", "neutral"]);
// Words the student never sees (docs/design/brief.md section 7): no first person, no jargon or core codes.
const jargon = /\b(hub|llm|unknown|job|proposal|delivery|pending|sending|failed)\b|джоб|квот|(^|[^а-яё])я([^а-яё]|$)/i;
const notice =
  "Письмо отправлено, но подтверждение не получено: проверьте сами или отправьте ещё раз под свою ответственность";

// Mirrors DeliveryItem of internal/input/wails/homework.go.
const delivery = (status, over = {}) => ({
  id: "d1",
  status,
  recipient: "teacher@example.org",
  subject: "Домашнее задание",
  attempts: 1,
  lastError: "",
  confirmedAt: "2026-10-16T10:00:00Z",
  sentAt: "",
  createdAt: "2026-10-16T10:00:00Z",
  canMarkSent: false,
  canSendAgain: false,
  notice: "",
  ...over,
});

test("every email status has a chip with a Russian word, an icon and no jargon", () => {
  for (const s of deliveryStatuses) {
    const v = deliveryView(delivery(s));
    assert.ok(kinds.has(v.kind), s);
    assert.ok(v.icon, s);
    assert.match(v.label, /[а-яё]/i, s);
    assert.doesNotMatch(`${v.label} ${v.hint}`, jargon, s);
  }
  assert.deepEqual(
    deliveryStatuses.map((s) => deliveryView(delivery(s)).label),
    ["Ждёт отправки", "Отправляется", "Отправлено", "Не отправлено", "Нет подтверждения"],
  );
  assert.deepEqual(Object.keys(deliveryLooks).sort(), [...deliveryStatuses].sort());
});

test("a sent email shows its time, a failed one its last error, an unknown one the core's notice", () => {
  assert.match(deliveryView(delivery("SENT", { sentAt: "2026-10-16T10:05:00Z" })).hint, /^Отправлено \d/);
  const failed = deliveryView(delivery("FAILED", { lastError: "smtp: 550 mailbox unavailable" }));
  assert.equal(failed.problem, "smtp: 550 mailbox unavailable");
  assert.match(failed.hint, /«Подключения»/);
  const unknown = deliveryView(delivery("UNKNOWN", { notice, canMarkSent: true, canSendAgain: true }));
  assert.equal(unknown.hint, notice);
  assert.equal(unknown.problem, "");
  assert.equal(deliveryView(delivery("SENDING", { lastError: "x" })).problem, "", "only a failed email shows an error");
  assert.equal(deliveryView(delivery("LOST")).label, "LOST", "an unknown code is shown as is");
});

test("which email actions are offered is the core's decision, not the view's", () => {
  // The view returns no actions; the page offers «Письмо дошло» and «Отправить ещё раз…» only from canMarkSent and
  // canSendAgain and never looks at the status code itself.
  for (const s of deliveryStatuses) {
    const v = deliveryView(delivery(s, { canMarkSent: true, canSendAgain: true }));
    assert.deepEqual(Object.keys(v).sort(), ["hint", "icon", "kind", "label", "problem"]);
  }
  const page = code("pages/HomeworkPage.tsx");
  assert.match(page, /\{d\.canMarkSent && \(/);
  assert.match(page, /\{d\.canSendAgain && \(/);
  for (const s of deliveryStatuses) {
    assert.ok(!page.includes(`"${s}"`), `the page does not branch on ${s}`);
  }
});

test("every generation state has a chip; waiting and failed ones show the core's text", () => {
  for (const state of generationStates) {
    const v = generationView({ state, message: "" });
    assert.ok(kinds.has(v.kind), state);
    assert.match(v.label, /[а-яё]/i, state);
    assert.doesNotMatch(`${v.label} ${v.hint}`, jargon, state);
  }
  assert.deepEqual(
    generationStates.map((state) => generationView({ state, message: "" }).label),
    ["Готовится черновик", "Ждёт интернет", "Лимит обработки исчерпан", "Черновик не готов", "Черновик готов"],
  );
  const text = "Нет связи с сервером обработки: проверьте интернет и адрес в разделе «Подключения». Генерация продолжится сама.";
  assert.equal(generationView({ state: "waitingNetwork", message: text }).hint, text);
  assert.deepEqual(
    generationStates.filter(generationGoesOn),
    ["generating", "waitingNetwork", "waitingQuota"],
    "failed and done stop the polling",
  );
});

test("emails on their way keep the page asking; settled ones do not", () => {
  assert.equal(deliveriesInFlight([]), false);
  assert.equal(deliveriesInFlight([delivery("SENT"), delivery("PENDING")]), true);
  assert.equal(deliveriesInFlight([delivery("SENDING")]), true);
  assert.equal(deliveriesInFlight([delivery("SENT"), delivery("FAILED"), delivery("UNKNOWN")]), false);
});

test("the list chip is the newest email, or none sent yet", () => {
  assert.equal(assignmentDeliveryView(undefined), null);
  assert.equal(assignmentDeliveryView([]).label, "Не отправлялось");
  assert.equal(assignmentDeliveryView([delivery("UNKNOWN", { notice }), delivery("SENT")]).label, "Нет подтверждения");
  assert.equal(assignmentDeliveryView([delivery("UNKNOWN", { notice })]).hint, "");
});

test("formats: versions, deadlines, sizes", () => {
  assert.equal(versionTitle({ number: 3, source: "edited" }), "Версия 3 · исправлена вручную");
  assert.equal(versionTitle({ number: 1, source: "generated" }), "Версия 1 · сгенерирована");
  assert.equal(dueText(""), "Без срока");
  assert.match(dueText("2026-09-18T15:00:00Z"), /^до \d/);
  assert.equal(sizeText(1), "1 байт");
  assert.equal(sizeText(3), "3 байта");
  assert.equal(sizeText(800), "800 байт");
  assert.equal(sizeText(12800), "12,5 КБ");
  assert.equal(sizeText(1024 * 1024 * 1.25), "1,3 МБ");
  assert.equal(sizeText(-1), "—");
});

test("errors: the core's fixed texts as they are, anything else generic", () => {
  for (const text of Object.values(errorMessages)) {
    assert.equal(errorText(new Error(text)), text);
  }
  assert.equal(errorText(new Error("send preparation: status is not one of ready, rejected")), errorMessages.unavailable);
  assert.equal(errorText(null), errorMessages.unavailable);
  assert.equal(isStale(new Error(errorMessages.draftStale)), true);
  assert.equal(isStale(new Error(errorMessages.draftInvalid)), false);
});

test("the bindings call the bound Go methods with the page's arguments", async () => {
  const calls = [];
  const replies = {
    [methods.generate]: { jobId: "j1", base: 2 },
    [methods.prepareSend]: {
      proposalId: "p1", status: "ready", reason: "", message: "", recipient: "t@example.org", subject: "ДЗ",
      attachmentName: "homework.md", attachmentSize: 12,
    },
    [methods.deliveries]: [delivery("UNKNOWN", { notice, canMarkSent: true, canSendAgain: true })],
    [methods.sendAgain]: delivery("PENDING"),
  };
  const api = homeworkApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return replies[method] ?? null;
  });
  assert.deepEqual(await api.generate("a1", 2, "короче"), { jobId: "j1", base: 2 });
  assert.equal((await api.prepareSend("a1")).status, "ready");
  await api.confirmSend("p1");
  await api.cancelSend("p2");
  assert.equal((await api.deliveries("a1"))[0].notice, notice);
  assert.equal((await api.sendAgain("d1")).status, "PENDING");
  assert.deepEqual(calls, [
    [methods.generate, "a1", 2, "короче"],
    [methods.prepareSend, "a1"],
    [methods.confirmSend, "p1"],
    [methods.cancelSend, "p2"],
    [methods.deliveries, "a1"],
    [methods.sendAgain, "d1"],
  ]);
  await assert.rejects(() => homeworkApi(async () => ({ state: "maybe", message: "" })).generationStatus("j1"));
});

// Rendering creates no calls: an email needs the student's click on the dialog's confirm button.
function noCallApi() {
  const calls = [];
  const api = new Proxy({}, { get: (_, name) => (...args) => calls.push([name, ...args]) });
  return { api, calls };
}

test("the send dialog shows recipient, subject and attachment as text and sends nothing by itself", () => {
  const { api, calls } = noCallApi();
  const prep = {
    proposalId: "p1", status: "ready", reason: "", message: "", recipient: "<b>t</b>@example.org",
    subject: "ДЗ <i>1</i>", attachmentName: "homework <script>.md", attachmentSize: 12800,
  };
  const html = renderToStaticMarkup(h(SendDialog, { api, prep, onClose: () => undefined }));
  assert.match(html, /Кому/);
  assert.match(html, /&lt;b&gt;t&lt;\/b&gt;@example.org/);
  assert.match(html, /ДЗ &lt;i&gt;1&lt;\/i&gt;/);
  assert.match(html, /homework &lt;script&gt;.md/);
  assert.match(html, /12,5 КБ/);
  assert.match(html, />Отправить</);
  assert.match(html, />Отмена</);
  assert.ok(!html.includes("<b>") && !html.includes("<script"));
  assert.deepEqual(calls, []);
});

test("the send-again dialog warns about a duplicate email and sends nothing by itself", () => {
  const { api, calls } = noCallApi();
  const html = renderToStaticMarkup(
    h(SendAgainDialog, { api, delivery: delivery("UNKNOWN", { notice, canSendAgain: true }), onClose: () => undefined }),
  );
  assert.match(html, /дважды/);
  assert.match(html, />Отправить ещё раз</);
  assert.match(html, /teacher@example.org/);
  assert.deepEqual(calls, []);
});

// Source guards (test step 2 of TASK-045): read the TypeScript sources, not the build.
const srcRoot = new URL("../src/", import.meta.url);

function source(rel) {
  return readFileSync(new URL(rel, srcRoot), "utf8");
}

// The source without comments: a call mentioned or commented out in a comment is not a call.
function code(rel) {
  return source(rel)
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

function sourceFiles(dir = "") {
  const out = [];
  for (const name of readdirSync(new URL(dir, srcRoot))) {
    const rel = dir + name;
    if (statSync(new URL(rel, srcRoot)).isDirectory()) {
      out.push(...sourceFiles(`${rel}/`));
    } else if (/\.(ts|tsx)$/.test(name)) {
      out.push(rel);
    }
  }
  return out;
}

const count = (text, s) => text.split(s).length - 1;

// The body of the function declared at `head`: from its first "{" to the matching "}".
function functionBody(text, head) {
  const start = text.indexOf(head);
  assert.ok(start >= 0, `no ${head}`);
  const open = text.indexOf("{", start + head.length);
  let depth = 0;
  for (let i = open; i < text.length; i++) {
    if (text[i] === "{") {
      depth++;
    } else if (text[i] === "}" && --depth === 0) {
      return { start: open, end: i };
    }
  }
  assert.fail(`unbalanced ${head}`);
}

for (const [binding, component, handler, nextComponent] of [
  ["confirmSend", "export function SendDialog(", "async function onConfirmSend()", "export function SendAgainDialog("],
  ["sendAgain", "export function SendAgainDialog(", "async function onConfirmSendAgain()", null],
]) {
  test(`${binding} is called from one place only: the confirm button handler of its dialog`, () => {
    const goName = `HomeworkService.${binding[0].toUpperCase()}${binding.slice(1)}"`;
    for (const rel of sourceFiles()) {
      const text = code(rel);
      if (rel === "bindings/homework.ts") {
        assert.equal(count(text, goName), 1, "the bound name is listed once");
        assert.equal(count(text, `methods.${binding})`), 0);
        assert.equal(count(text, `methods.${binding},`), 1, "the glue calls it once");
        continue;
      }
      assert.equal(count(text, goName), 0, `${rel} names the bound method`);
      assert.equal(count(text, `methods.${binding}`), 0, `${rel} calls the method by name`);
      if (rel !== "pages/HomeworkDialogs.tsx") {
        assert.equal(count(text, `.${binding}(`), 0, `${rel} calls ${binding}`);
      }
    }

    const dialogs = code("pages/HomeworkDialogs.tsx");
    assert.equal(count(dialogs, `.${binding}(`), 1);
    const at = dialogs.indexOf(`.${binding}(`);
    const body = functionBody(dialogs, handler);
    assert.ok(at > body.start && at < body.end, `${binding} is inside ${handler}`);

    const from = dialogs.indexOf(component);
    const to = nextComponent === null ? dialogs.length : dialogs.indexOf(nextComponent);
    assert.ok(from >= 0 && from < body.start && body.end < to, `${handler} belongs to ${component}`);
    const name = handler.replace("async function ", "").replace("()", "");
    assert.equal(dialogs.match(new RegExp(`\\b${name}\\b`, "g")).length, 2, `${name} is declared once and used once`);
    assert.equal(count(dialogs, `onConfirm={() => void ${name}()}`), 1, `${name} is the dialog's confirm handler`);
  });
}

test("the homework page renders model text as plain text only", () => {
  for (const rel of sourceFiles()) {
    const text = source(rel);
    assert.ok(!text.includes("dangerouslySetInnerHTML"), `${rel} uses dangerouslySetInnerHTML`);
    assert.ok(!/\binnerHTML\b|\bouterHTML\b|insertAdjacentHTML/.test(text), `${rel} writes HTML`);
  }
  for (const rel of ["pages/HomeworkPage.tsx", "pages/HomeworkDialogs.tsx", "homeworkView.ts", "bindings/homework.ts"]) {
    assert.doesNotMatch(code(rel), /markdown|marked|DOMParser|createContextualFragment/i, rel);
  }
});
