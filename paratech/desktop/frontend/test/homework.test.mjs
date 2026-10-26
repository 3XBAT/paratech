// Homework page (TASK-045): view helpers, the bindings glue, the send dialogs and source guards. Runs on the modules
// compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  errorMessages,
  generationStates,
  homeworkApi,
  methods,
  parseAssignmentDetail,
  parseDocxSaveResult,
  parseDraftStatus,
  sendFormats,
} from "../.test-build/bindings/homework.js";
import {
  assignmentDeliveryView,
  blockedByEmail,
  defaultSendFormat,
  deliveriesInFlight,
  deliveryLooks,
  deliveryView,
  docxSaveView,
  draftStatusStep,
  dueText,
  errorText,
  followGeneration,
  generateOffer,
  generatingLabel,
  generationGoesOn,
  generationRunning,
  generationView,
  isStale,
  regenerateBlockedReason,
  sendFormatChange,
  sendFormatLabels,
  sendRetryable,
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
    ["Готовим решение…", "Ждёт интернет", "Лимит обработки исчерпан", "Решение не готово", "Решение готово"],
  );
  const text = "Нет связи с сервисом конспектов: проверьте интернет и адрес в разделе «Подключения». Генерация продолжится сама.";
  assert.equal(generationView({ state: "waitingNetwork", message: text }).hint, text);
  assert.deepEqual(
    generationStates.filter(generationGoesOn),
    ["generating", "waitingNetwork", "waitingQuota"],
    "failed and done stop the polling",
  );
});

test("a running generation shows its progress; without one the usual time", () => {
  assert.equal(generationView({ state: "generating", message: "", percent: 0 }).hint, "Обычно до минуты.");
  assert.equal(generationView({ state: "generating", message: "", percent: 40 }).hint, "Готово 40 %.");
  assert.equal(generationView({ state: "generating", message: "", percent: 100 }).hint, "Обычно до минуты.");
  const wait = "Нет связи с сервисом конспектов.";
  assert.equal(generationView({ state: "waitingNetwork", message: wait, percent: 40 }).hint, wait, "the core's text wins");
});

// Mirrors AssignmentItem of internal/input/wails/homework.go.
const assignment = () => ({
  id: "a1", lessonId: "l1", title: "Задача 1", description: "", due: "", userEdited: false,
  updatedAt: "2026-10-19T10:00:00Z", subject: "Матанализ", subjectSource: "schedule", kind: "lecture", teacher: "",
  teacherSource: "", lessonDate: "", correctedSubject: "", correctedTeacher: "",
});

// Mirrors ActiveGeneration of internal/input/wails/homework.go.
const active = (over = {}) => ({ jobId: "j1", state: "generating", message: "", percent: 10, ...over });

test("the page follows the core's generation; a new version reloads the assignment", () => {
  assert.equal(followGeneration(null), null);
  assert.deepEqual(followGeneration(active({ state: "waitingQuota", message: "m" })), {
    jobId: "j1",
    status: { state: "waitingQuota", message: "m", percent: 10 },
  });
  // Opened again while the generation goes on: nothing followed yet, so it is picked up.
  assert.deepEqual(draftStatusStep({ latestVersion: 2, generation: active() }, 2, ""), {
    reload: false,
    follow: followGeneration(active()),
  });
  // Already followed: no second follow; a finished draft (version 3) reloads.
  assert.deepEqual(draftStatusStep({ latestVersion: 3, generation: null }, 2, "j1"), { reload: true, follow: null });
  assert.deepEqual(draftStatusStep({ latestVersion: 2, generation: active() }, 2, "j1"), { reload: false, follow: null });
  // The first draft appears on a page that had none.
  assert.equal(draftStatusStep({ latestVersion: 1, generation: null }, 0, "").reload, true);
  assert.equal(draftStatusStep({ latestVersion: 1, generation: null }, 1, "").reload, false);
  // A generation started elsewhere after the followed one ended.
  assert.equal(draftStatusStep({ latestVersion: 3, generation: active({ jobId: "j2" }) }, 3, "j1").follow.jobId, "j2");
});

test("while a draft is prepared, generate and the instructions are off with «Готовим решение…»", () => {
  assert.equal(generatingLabel, "Готовим решение…");
  const base = { hasDraft: true, generating: false, olderShown: false, dirty: false };
  assert.deepEqual(generateOffer(base), {
    label: "Сгенерировать заново",
    disabled: false,
    reason: undefined,
    instructionDisabled: false,
  });
  assert.equal(generateOffer({ ...base, hasDraft: false }).label, "Сгенерировать ДЗ");
  const running = generateOffer({ ...base, generating: true });
  assert.equal(running.disabled, true);
  assert.equal(running.instructionDisabled, true);
  const dirty = generateOffer({ ...base, dirty: true });
  assert.equal(dirty.disabled, true);
  assert.match(dirty.reason, /сохраните/);
  assert.equal(dirty.instructionDisabled, false, "the instructions stay editable");
  assert.match(generateOffer({ ...base, olderShown: true }).reason, /из последней/);
});

test("a refused second generation is recognised and shown as the core's text", () => {
  assert.equal(generationRunning(new Error(errorMessages.generationRunning)), true);
  assert.equal(generationRunning(new Error(errorMessages.draftStale)), false);
  assert.equal(errorText(new Error(errorMessages.generationRunning)), "Решение уже готовится. Дождитесь окончания и повторите.");
  assert.equal(errorText(new Error(errorMessages.sendFormat)), errorMessages.sendFormat);
});

test("the Word save line: saved with its file, cancelled, failed", () => {
  const r = (over) => ({ status: "saved", message: "Файл Word сохранён.", name: "ДЗ.docx", ...over });
  assert.deepEqual(docxSaveView(r({})), { kind: "ok", text: "Файл Word сохранён. Файл: «ДЗ.docx»." });
  assert.deepEqual(docxSaveView(r({ status: "cancelled", message: "Сохранение отменено.", name: "" })), {
    kind: "wait",
    text: "Сохранение отменено.",
  });
  assert.equal(docxSaveView(r({ status: "failed", message: "Не удалось сохранить файл.", name: "" })).kind, "fail");
});

test("the attachment format: Word first, a change only when it differs and the dialog is free", () => {
  assert.deepEqual([...sendFormats], ["docx", "txt"]);
  assert.equal(defaultSendFormat, "docx");
  assert.match(sendFormatLabels.docx, /\.docx/);
  assert.equal(sendFormatLabels.txt, "Текст (.md)", "the text attachment is homework.md");
  assert.equal(sendFormatChange("docx", "txt", false), true);
  assert.equal(sendFormatChange("docx", "docx", false), false);
  assert.equal(sendFormatChange("docx", "txt", true), false, "not while another one is prepared");
});

test("the replies with a generation, a draft status and a Word save are checked", () => {
  const detail = { assignment: assignment(), versions: [], latest: null, generation: active() };
  assert.deepEqual(parseAssignmentDetail(detail).generation, active());
  assert.equal(parseAssignmentDetail({ ...detail, generation: null }).generation, null);
  assert.throws(() => parseAssignmentDetail({ ...detail, generation: active({ state: "paused" }) }), /generation/);
  assert.throws(() => parseAssignmentDetail({ ...detail, generation: undefined }), /generation/);
  assert.deepEqual(parseDraftStatus({ latestVersion: 2, generation: null }), { latestVersion: 2, generation: null });
  assert.throws(() => parseDraftStatus({ latestVersion: "2", generation: null }), /draft status/);
  assert.throws(() => parseDraftStatus({ latestVersion: 2, generation: { jobId: "j1" } }), /draft status/);
  const saved = { status: "saved", message: "m", name: "n.docx" };
  assert.deepEqual(parseDocxSaveResult(saved), saved);
  assert.throws(() => parseDocxSaveResult({ ...saved, status: "maybe" }), /docx save result/);
  assert.throws(() => parseDocxSaveResult({ ...saved, name: null }), /docx save result/);
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

test("only an incomplete send is retried; a refused one is not", () => {
  assert.equal(sendRetryable(new Error(errorMessages.sendIncomplete)), true);
  assert.equal(sendRetryable(errorMessages.sendIncomplete), true);
  for (const key of ["sendStale", "sendUnresolved", "sendCannotConfirm", "sendAlreadyDecided", "sendNotFound", "unavailable"]) {
    assert.equal(sendRetryable(new Error(errorMessages[key])), false, key);
  }
  assert.equal(sendRetryable(null), false);
});

test("a locked export or an unresolved earlier email reloads the emails", () => {
  assert.equal(blockedByEmail(new Error(errorMessages.exportLocked)), true);
  assert.equal(blockedByEmail(new Error(errorMessages.sendUnresolved)), true);
  assert.equal(blockedByEmail(new Error(errorMessages.sendIncomplete)), false);
  assert.equal(blockedByEmail(new Error(errorMessages.versionNotFound)), false);
  assert.equal(errorText(new Error(errorMessages.exportLocked)), errorMessages.exportLocked, "shown as it is");
});

test("regenerate is off with a reason while an edit is unsaved or an older version is shown", () => {
  assert.equal(regenerateBlockedReason(false, false), undefined);
  assert.match(regenerateBlockedReason(false, true), /сохраните/);
  assert.match(regenerateBlockedReason(true, false), /из последней/);
  assert.match(regenerateBlockedReason(true, true), /сохраните/);
});

test("the bindings call the bound Go methods with the page's arguments", async () => {
  const calls = [];
  const replies = {
    [methods.generate]: { jobId: "j1", base: 2 },
    [methods.prepareSend]: {
      proposalId: "p1", status: "ready", reason: "", message: "", recipient: "t@example.org", subject: "ДЗ",
      attachmentName: "homework.docx", attachmentSize: 12, attachmentFormat: "docx",
    },
    [methods.draftStatus]: { latestVersion: 3, generation: null },
    [methods.exportDocx]: { version: 3, fileName: "a1.docx" },
    [methods.saveDocx]: { status: "cancelled", message: "Сохранение отменено.", name: "" },
    [methods.deliveries]: [delivery("UNKNOWN", { notice, canMarkSent: true, canSendAgain: true })],
    [methods.sendAgain]: delivery("PENDING"),
  };
  const api = homeworkApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return replies[method] ?? null;
  });
  assert.deepEqual(await api.generate("a1", 2, "короче"), { jobId: "j1", base: 2 });
  const prep = await api.prepareSend("a1", "docx");
  assert.equal(prep.status, "ready");
  assert.equal(prep.attachmentFormat, "docx");
  await api.confirmSend("p1", "ДЗ", prep.attachmentFormat);
  await api.cancelSend("p2");
  assert.equal((await api.deliveries("a1"))[0].notice, notice);
  assert.equal((await api.sendAgain("d1")).status, "PENDING");
  assert.deepEqual(await api.draftStatus("a1"), { latestVersion: 3, generation: null });
  assert.deepEqual(await api.exportDocx("a1", 3), { version: 3, fileName: "a1.docx" });
  assert.equal((await api.saveDocx("a1", 3)).status, "cancelled");
  assert.deepEqual(calls, [
    [methods.generate, "a1", 2, "короче"],
    [methods.prepareSend, "a1", "docx"],
    [methods.confirmSend, "p1", "ДЗ", "docx"],
    [methods.cancelSend, "p2"],
    [methods.deliveries, "a1"],
    [methods.sendAgain, "d1"],
    [methods.draftStatus, "a1"],
    [methods.exportDocx, "a1", 3],
    [methods.saveDocx, "a1", 3],
  ]);
  assert.equal(methods.draftStatus, "paratech/desktop/internal/input/wails.HomeworkService.DraftStatus");
  assert.equal(methods.exportDocx, "paratech/desktop/internal/input/wails.HomeworkService.ExportDocx");
  assert.equal(methods.saveDocx, "paratech/desktop/internal/input/wails.HomeworkService.SaveDocx");
  await assert.rejects(() => homeworkApi(async () => ({ ...replies[methods.prepareSend], attachmentFormat: "pdf" })).prepareSend("a1", "txt"));
  await assert.rejects(() => homeworkApi(async () => ({ state: "maybe", message: "" })).generationStatus("j1"));
});

// Rendering creates no calls: an email needs the student's click on the dialog's confirm button.
function noCallApi() {
  const calls = [];
  const api = new Proxy({}, { get: (_, name) => (...args) => calls.push([name, ...args]) });
  return { api, calls };
}

test("the send dialog shows the version, recipient, subject and attachment as text and sends nothing by itself", () => {
  const { api, calls } = noCallApi();
  const prep = {
    proposalId: "p1", status: "ready", reason: "", message: "", recipient: "<b>t</b>@example.org",
    subject: "ДЗ <i>1</i>", attachmentName: "homework <script>.md", attachmentSize: 12800, attachmentFormat: "txt",
  };
  const html = renderToStaticMarkup(h(SendDialog, { api, prep, version: 3, onClose: () => undefined }));
  assert.match(html, /Версия 3</);
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

test("the send dialog offers .docx and .txt with the prepared one chosen; a switch confirms nothing", () => {
  const { api, calls } = noCallApi();
  const prep = {
    proposalId: "p1", status: "ready", reason: "", message: "", recipient: "t@example.org", subject: "ДЗ",
    attachmentName: "a1.docx", attachmentSize: 100, attachmentFormat: "docx",
  };
  const html = renderToStaticMarkup(h(SendDialog, { api, prep, version: 2, onFormat: () => undefined, onClose: () => undefined }));
  assert.match(html, /Формат вложения/);
  // A segmented control (TASK-100) in place of the radio buttons.
  assert.match(html, /<div class="seg" role="group" aria-label="Формат вложения">/);
  assert.match(html, /<button type="button" aria-pressed="true">Word \(\.docx\)<\/button>/);
  assert.match(html, /<button type="button" aria-pressed="false">Текст \(\.md\)<\/button>/);
  assert.ok(!html.includes('type="radio"'));
  assert.match(html, /<fieldset class="hw-format">/, "the choice is open");
  const switching = renderToStaticMarkup(
    h(SendDialog, { api, prep, version: 2, switching: true, problem: "Нет файла Word", onFormat: () => undefined, onClose: () => undefined }),
  );
  assert.match(switching, /<fieldset class="hw-format" disabled="">/);
  assert.match(switching, /Подготовка…/);
  assert.match(switching, /Нет файла Word/);
  assert.match(switching, /<button type="button" class="btn primary" disabled="">Отправить</, "nothing to confirm while switching");
  const locked = renderToStaticMarkup(h(SendDialog, { api, prep, version: 2, onClose: () => undefined }));
  assert.match(locked, /<fieldset class="hw-format" disabled="">/, "no handler, no choice");
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

test("a retry repeats the same send; a refused export opens no send dialog", () => {
  const dialogs = code("pages/HomeworkDialogs.tsx");
  assert.equal(count(dialogs, ".prepareSend("), 0, "the dialog never prepares a new send");
  assert.equal(
    count(dialogs, ".confirmSend(props.prep.proposalId, props.prep.subject, props.prep.attachmentFormat)"),
    1,
    "confirmSend takes the dialog's own send, the subject and the format it shows",
  );
  assert.match(dialogs, /retry \? "Повторить" : "Отправить"/);

  const page = code("pages/HomeworkPage.tsx");
  // The export of the chosen format comes first and its failure stops the preparation.
  const helper = functionBody(page, "async function prepareSendAs(");
  const prepare = page.slice(helper.start, helper.end);
  const docx = prepare.indexOf("await api.exportDocx(");
  const txt = prepare.indexOf("await api.exportDraft(");
  const prep = prepare.indexOf("await api.prepareSend(id, format)");
  assert.ok(docx >= 0 && txt >= 0 && prep > docx && prep > txt, "the export comes first");
  assert.ok(!prepare.includes("try {") && !prepare.includes(".catch("), "no export failure is swallowed");
  assert.match(prepare, /version: exported\.version/, "the dialog shows the exported version");

  const body = functionBody(page, "const startSend = () =>");
  const start = page.slice(body.start, body.end);
  const call = start.indexOf("await prepareSendAs(");
  assert.ok(call >= 0, "the send is prepared through the export");
  assert.ok(!start.includes("try {"), "no export failure is swallowed");
  assert.ok(!start.slice(0, call).includes(".catch("), "no export failure is swallowed");
  assert.ok(start.indexOf("setSendOpen(") > call, "the dialog opens only after both");

  // A format switch prepares anew; only a ready preparation replaces the shown send, which is then cancelled.
  const sw = functionBody(page, "async function switchFormat(");
  const switching = page.slice(sw.start, sw.end);
  const again = switching.indexOf("await prepareSendAs(");
  const replace = switching.indexOf("pendingSend.current = next.prep.proposalId");
  assert.ok(again >= 0 && replace > again, "the new send is prepared before it replaces the old one");
  assert.ok(switching.indexOf("api.cancelSend(old)") > replace, "the old send is cancelled after the replacement");
  assert.equal(count(switching, ".confirmSend("), 0);
});

test("the open assignment asks the core for its draft until the page goes away", () => {
  const page = code("pages/HomeworkPage.tsx");
  assert.equal(count(page, "api.draftStatus(id)"), 1);
  assert.match(page, /draftStatusStep\(st, k\.latest, k\.job\)/);
  assert.match(page, /setTimeout\(\(\) => void tick\(\), draftPollMs\)/);
  assert.match(page, /pollDraftNow\.current = \(\) => undefined;/, "the poll stops on unmount");
  assert.match(page, /disabled=\{offer\.instructionDisabled\}/, "the instructions follow the core's generation");
  assert.match(page, /busyLabel=\{generatingLabel\}/);
  assert.match(page, />\s*Сохранить в Word \(\.docx\)\s*</);
  assert.equal(count(page, "api.saveDocx(id, shown.number)"), 1, "Word saves the shown version");
});

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

// Go interpreted ("…" with escapes) and raw (`…`) string literals.
const goStrings = (text) => text.match(/"(?:[^"\\\n]|\\.)*"|`[^`]*`/g) ?? [];

test("the Go literal extractor keeps each literal whole", () => {
  const sample = 'a := "unavailable\\n" // черновик\nb := "say \\"ok\\"" + `raw`\n';
  assert.deepEqual(goStrings(sample), ['"unavailable\\n"', '"say \\"ok\\""', "`raw`"]);
});

// TASK-102: the student sees «решение», never «черновик»; internal names (draft…) and comments may keep it.
test("no user-facing text says «черновик»", () => {
  for (const rel of sourceFiles()) {
    // The one exception: the caption of the agent's answer being written (TASK-148, ADR-0026), not the homework draft.
    const text = rel === "agentView.ts" ? code(rel).replace("Ответ пишется · черновик", "") : code(rel);
    assert.doesNotMatch(text, /чернов/i, rel);
  }
  // The core's texts the page shows as they are: the string literals of the bindings.
  for (const go of ["homework.go", "activity.go"]) {
    const text = readFileSync(new URL(`../../internal/input/wails/${go}`, import.meta.url), "utf8");
    for (const lit of goStrings(text)) {
      assert.doesNotMatch(lit, /чернов/i, `${go}: ${lit}`);
    }
  }
});
