// TASK-238: the recipient on the send cards: «Кому: <имя> <адрес>», «Кому отправить?» with the chosen role and the shown
// address going back, «Нет почты преподавателя» with «Указать почту», «У задания не указан предмет», the agent card's
// flow, and the send dialog. Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { ConfirmFlow, confirmCardModel, confirmTexts } from "../.test-build/agentConfirmView.js";
import { ConfirmCardView } from "../.test-build/agent/ConfirmCard.js";
import { RefusalButton } from "../.test-build/pages/RecipientParts.js";
import { SendDialog } from "../.test-build/pages/HomeworkDialogs.js";
import {
  choiceOf,
  choiceOptions,
  choiceText,
  noEmailText,
  recipientText,
  recipientTexts,
  recipientView,
  refusalAction,
  roleWords,
  sendBlocked,
  teacherRequest,
} from "../.test-build/recipientView.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const evil = "<img src=x onerror=alert(1)>";

const options = [
  { role: "lecture", name: "Иванов И. И.", email: "ivanov@example.test" },
  { role: "practice", name: "", email: "petrov@example.test" },
  { role: "lab", name: "Сидоров С. С.", email: "sidorov@example.test" },
];

const prep = (over = {}) => ({
  proposalId: "p1", status: "ready", reason: "", message: "", recipient: "ivanov@example.test", recipientName: "",
  role: "", subjectId: "", options: [], subject: "ДЗ", attachmentName: "dz.txt", attachmentSize: 12, attachmentFormat: "txt",
  ...over,
});

const card = (over = {}) => ({
  kind: "send", targetKind: "proposal", targetId: "p1", state: "waiting", message: "", assignmentId: "a1", title: "Задачи",
  subject: "Физика", due: "", recipient: "ivanov@example.test", recipientName: "", role: "", reason: "", subjectId: "",
  options: [], emailSubject: "ДЗ по физике", attachmentName: "dz.txt", attachmentSize: 2048, attachmentFormat: "txt",
  draftVersion: 1, notice: "", deletion: null, ...over,
});

const choose = (over = {}) => card({ recipient: "", reason: "choose_teacher", subjectId: "s1", options, ...over });
const noEmail = (over = {}) =>
  card({
    state: "rejected", recipient: "", reason: "no_teacher_email", role: "lecture", subjectId: "s1",
    message: "Нет почты преподавателя (лектор) предмета «Физика»: укажите её на странице предмета.", ...over,
  });
const noSubject = (over = {}) =>
  card({ state: "rejected", recipient: "", reason: "no_subject", message: "У задания не указан предмет: выберите предмет, и письмо уйдёт его преподавателю.", ...over });

const nav = () => {
  const seen = [];
  return {
    seen,
    openTeacher: (subjectId, role) => seen.push(["teacher", subjectId, role]),
    openAssignment: (id) => seen.push(["assignment", id]),
    openSubjects: () => seen.push(["subjects"]),
  };
};

const cardView = (c, extra = {}) =>
  html(h(ConfirmCardView, { model: confirmCardModel(c, extra.chosenRole ?? ""), busy: "", error: "", onYes: noop, onNo: noop, ...extra }));

// ---- «Кому: <имя> <адрес>» ----

test("the recipient line is «<имя> <адрес>», or the address alone when the name is unknown", () => {
  assert.equal(recipientText("Иванов И. И.", "ivanov@example.test"), "Иванов И. И. ivanov@example.test");
  assert.equal(recipientText("", "ivanov@example.test"), "ivanov@example.test");
  assert.deepEqual(recipientView(prep({ recipientName: "Иванов И. И." })), { kind: "to", name: "Иванов И. И.", address: "ivanov@example.test" });
  assert.deepEqual(recipientView(prep()), { kind: "to", name: "", address: "ivanov@example.test" });
  assert.deepEqual(recipientView(prep({ recipient: "" })), { kind: "none" });
});

test("the agent card shows name and address as separate text nodes; without a name only the address", () => {
  const named = cardView(card({ recipientName: "Иванов И. И." }));
  assert.match(named, /<bdi>Иванов И\. И\.<\/bdi> <bdi>ivanov@example\.test<\/bdi>/);
  assert.match(named, /Кому/);
  const bare = cardView(card());
  assert.ok(bare.includes("ivanov@example.test"));
  assert.equal((bare.match(/<bdi>/g) ?? []).length, 0, "the old plain row");
  assert.match(bare, />Кому</);
});

test("the send dialog shows «Кому» with the name and the address as separate text nodes", () => {
  const named = html(h(SendDialog, { api: {}, prep: prep({ recipientName: "Иванов И. И." }), version: 2, onClose: noop }));
  assert.match(named, /<dt>Кому<\/dt><dd><bdi>Иванов И\. И\.<\/bdi> <bdi>ivanov@example\.test<\/bdi><\/dd>/);
  const bare = html(h(SendDialog, { api: {}, prep: prep(), version: 2, onClose: noop }));
  assert.match(bare, /<dt>Кому<\/dt><dd><bdi>ivanov@example\.test<\/bdi><\/dd>/);
  assert.match(bare, />Отправить</);
  assert.doesNotMatch(bare, /radiogroup/, "nothing is asked");
});

test("hostile names and addresses stay text in the dialog and the card", () => {
  const dialog = html(h(SendDialog, { api: {}, prep: prep({ recipientName: evil, recipient: `${evil}@example.test` }), version: 1, onClose: noop }));
  const asked = html(h(SendDialog, { api: {}, prep: prep({ recipient: "", reason: "choose_teacher", options: options.map((o) => ({ ...o, name: evil })) }), version: 1, onClose: noop }));
  const agent = cardView(choose({ options: options.map((o) => ({ ...o, name: evil })) }));
  const refused = cardView(noEmail({ message: evil }));
  for (const out of [dialog, asked, agent, refused]) {
    assert.ok(!out.includes("<img"), out);
    assert.ok(out.includes("&lt;img"), out);
    assert.doesNotMatch(out, /<a[ >]|mailto:|dangerouslySetInnerHTML/);
  }
});

// ---- «Кому отправить?» ----

test("a question lists one option per teacher: role, name and address; a missing name shows the address", () => {
  const v = recipientView(choose());
  assert.equal(v.kind, "choose");
  assert.deepEqual(v.options.map(choiceText), [
    "Лектор — Иванов И. И. ivanov@example.test",
    "Семинарист — petrov@example.test",
    "Лабораторные — Сидоров С. С. sidorov@example.test",
  ]);
  assert.deepEqual(choiceOptions(options).map((o) => o.role), ["lecture", "practice", "lab"]);
});

test("the agent card asks «Кому отправить?» in a labelled radio group; «Да» is shown but off, with the reason", () => {
  const out = cardView(choose(), { onChoose: noop });
  assert.match(out, /<fieldset[^>]*role="radiogroup"[^>]*aria-labelledby="[^"]+"/);
  assert.match(out, /<legend id="[^"]+">Кому отправить\?<\/legend>/);
  assert.equal((out.match(/type="radio"/g) ?? []).length, 3);
  assert.doesNotMatch(out, /checked=""/, "nothing is chosen at first");
  assert.match(out, /<label class="ag-opt"><input type="radio"[^>]*value="lecture"[^>]*\/><span>Лектор — <bdi>Иванов И\. И\.<\/bdi> <bdi>ivanov@example\.test<\/bdi><\/span>/);
  assert.match(out, /<button[^>]*disabled=""[^>]*><span class="btn-labels"><span>Да</);
  assert.match(out, /Выберите, кому отправить\./);
  assert.match(out, />Нет</);
  const m = confirmCardModel(choose());
  assert.deepEqual([m.yes, m.yesBlocked], [true, recipientTexts.chooseNeeded]);
  // The question has its own title: no «Кому» row with an empty address.
  assert.deepEqual(m.rows.map((r) => r.label), ["Тема", "Задание", "Вложение", "Версия"]);
});

test("once an option is clicked it is checked and «Да» is on", () => {
  const out = cardView(choose(), { chosenRole: "practice", onChoose: noop });
  assert.match(out, /<input type="radio"[^>]*checked=""[^>]*value="practice"/);
  assert.equal((out.match(/checked=""/g) ?? []).length, 1);
  assert.doesNotMatch(out, /<button[^>]*disabled=""[^>]*><span class="btn-labels"><span>Да</);
  assert.match(out, /<button[^>]*><span class="btn-labels"><span>Да</, "«Да» is there and on");
  assert.doesNotMatch(out, /Выберите, кому отправить/);
  assert.equal(confirmCardModel(choose(), "practice").yesBlocked, "");
  assert.equal(confirmCardModel(choose(), "lab").yesBlocked, "");
  assert.equal(confirmCardModel(choose(), "dean").yesBlocked, recipientTexts.chooseNeeded, "an unknown role is no choice");
});

test("the send dialog asks the same question; «Отправить» is off until a choice", () => {
  const asked = html(h(SendDialog, { api: {}, prep: prep({ recipient: "", reason: "choose_teacher", subjectId: "s1", options }), version: 2, onClose: noop }));
  assert.match(asked, /role="radiogroup"/);
  assert.match(asked, /Кому отправить\?/);
  assert.equal((asked.match(/type="radio"/g) ?? []).length, 3);
  assert.match(asked, /<button type="button" class="btn primary" disabled="">Отправить</);
  assert.match(asked, /станет доступна после выбора/);
  assert.doesNotMatch(asked, /<dt>Кому<\/dt>/);
});

test("the choice that goes back is the role and the address the option shows", () => {
  assert.deepEqual(choiceOf(options, "practice"), { role: "practice", address: "petrov@example.test" });
  assert.deepEqual(choiceOf(options, "lab"), { role: "lab", address: "sidorov@example.test" });
  assert.equal(choiceOf(options, ""), null);
  assert.equal(choiceOf(options, "dean"), null);
  assert.equal(choiceOf(undefined, "lab"), null);
  const dialogs = readFileSync(new URL("../src/pages/HomeworkDialogs.tsx", import.meta.url), "utf8");
  assert.match(dialogs, /choiceOf\(props\.prep\.options, role\)/, "read from the options as shown, not from state kept apart");
  assert.match(dialogs, /confirmSend\(props\.prep\.proposalId, props\.prep\.subject, props\.prep\.attachmentFormat, choice\)/);
  assert.match(dialogs, /\|\| blocked !== ""\) \{\s*return;/, "the handler refuses a blocked confirm too");
});

function fakeCore(first, later = []) {
  const calls = { confirm: [], card: 0 };
  const queue = [...later];
  return {
    calls,
    api: {
      card: async () => (calls.card++, queue.length > 0 ? queue.shift() : first),
      confirm: async (c, chosen) => (calls.confirm.push([c.targetId, chosen]), { ...c, state: "done" }),
      decline: async () => ({ ...first, state: "declined" }),
    },
  };
}

test("«Да» of a question sends the clicked role and its address; without a click nothing is sent", async () => {
  const core = fakeCore(choose());
  const flow = new ConfirmFlow(core.api, noop);
  flow.show(choose());
  await flow.yes();
  assert.deepEqual(core.calls.confirm, [], "no choice, no call");
  flow.choose("dean");
  assert.equal(flow.state.chosenRole, "", "an option the card does not offer is ignored");
  flow.choose("practice");
  assert.equal(flow.state.chosenRole, "practice");
  assert.deepEqual(core.calls.confirm, [], "a click on an option sends nothing");
  flow.choose("lab");
  await Promise.all([flow.yes(), flow.yes()]);
  assert.deepEqual(core.calls.confirm, [["p1", { role: "lab", address: "sidorov@example.test" }]], "one call, the last click");
  assert.equal(flow.state.card.state, "done");
});

test("a card that asked nothing confirms with no choice", async () => {
  const core = fakeCore(card());
  const flow = new ConfirmFlow(core.api, noop);
  flow.show(card({ options }));
  flow.choose("lab");
  assert.equal(flow.state.chosenRole, "lab");
  await flow.yes();
  assert.deepEqual(core.calls.confirm, [["p1", null]], "options without the question are not a choice");
});

test("a refused «Да» (the address changed) re-reads the card and drops the choice: the student clicks again", async () => {
  const changed = choose({ options: options.map((o) => (o.role === "lab" ? { ...o, email: "new@example.test" } : o)) });
  const core = fakeCore(choose(), [changed]);
  core.api.confirm = async () => {
    throw new Error("Файл, адрес получателя или тема письма изменились после подтверждения. Проверьте и подтвердите ещё раз.");
  };
  const flow = new ConfirmFlow(core.api, noop);
  flow.show(choose());
  flow.choose("lab");
  await flow.yes();
  assert.equal(flow.state.chosenRole, "");
  assert.match(flow.state.error, /изменились/);
  assert.equal(flow.state.card.options.find((o) => o.role === "lab").email, "new@example.test");
  assert.equal(confirmCardModel(flow.state.card, flow.state.chosenRole).yesBlocked, recipientTexts.chooseNeeded);
});

test("a question with fewer than two teachers is no dead end: it says why and offers «Обновить карточку»", async () => {
  for (const few of [[], [options[0]]]) {
    const c = choose({ options: few });
    const m = confirmCardModel(c);
    assert.equal(m.recipient.kind, "choice_changed");
    assert.equal(m.yes, false);
    assert.equal(m.no, true, "«Нет» stays");
    const out = cardView(c, { onRefresh: noop });
    assert.match(out, /Преподаватели предмета изменились, выбирать больше не из чего/);
    assert.match(out, />Обновить карточку</);
    assert.match(out, />Нет</);
    assert.doesNotMatch(out, />Да</);
    assert.doesNotMatch(out, /type="radio"/);
  }
  const core = fakeCore(choose({ options: [options[0]] }), [choose()]);
  const flow = new ConfirmFlow(core.api, noop);
  flow.show(choose({ options: [options[0]] }));
  await flow.refresh();
  assert.equal(core.calls.card, 1);
  assert.equal(flow.state.card.options.length, 3, "the card is read again and asks again");
  assert.equal(flow.state.busy, "");
  const dialog = html(h(SendDialog, { api: {}, prep: prep({ recipient: "", reason: "choose_teacher", options: [options[0]] }), version: 1, onRefresh: noop, onClose: noop }));
  assert.match(dialog, /выбирать больше не из чего/);
  assert.match(dialog, />Обновить</);
  assert.match(dialog, /<button type="button" class="btn primary" disabled="">Отправить</);
  assert.match(dialog, />Отмена</);
});

test("a question answered or declined no longer asks", () => {
  const done = confirmCardModel(choose({ state: "done" }));
  assert.equal(done.recipient.kind, "none");
  assert.match(cardView(choose({ state: "done" })), /Письмо отправлено/);
  assert.doesNotMatch(cardView(choose({ state: "declined" })), /radiogroup/);
});

// ---- Refusals ----

test("no address: the text names the role and the subject, «Указать почту» leads to that role's form", () => {
  for (const [role, word] of [["lecture", "лектор"], ["practice", "семинарист"], ["lab", "лабораторные"]]) {
    assert.equal(roleWords[role], word);
    assert.equal(noEmailText(role, "Физика"), `Нет почты преподавателя (${word}) предмета «Физика»: укажите её на странице предмета.`);
    const v = recipientView(card({ state: "rejected", recipient: "", reason: "no_teacher_email", role, subjectId: "s1", message: "" }));
    assert.equal(v.kind, "no_email");
    assert.match(v.text, new RegExp(`Нет почты преподавателя \\(${word}\\)`), "an empty message gets the text built here");
    assert.deepEqual(refusalAction(v, "card"), { kind: "teacher", label: "Указать почту", subjectId: "s1", role });
  }
  assert.match(noEmailText("", ""), /^Нет почты преподавателя: укажите/);
  const out = cardView(noEmail(), { nav: nav() });
  assert.match(out, /Нет почты преподавателя \(лектор\) предмета «Физика»/);
  assert.match(out, />Указать почту</);
  assert.doesNotMatch(out, />Да</);
  assert.doesNotMatch(out, />Нет</);
  assert.equal(confirmCardModel(noEmail()).yes, false);
  assert.match(confirmCardModel(noEmail()).outcome, /Нет почты преподавателя/);
});

test("«Указать почту» asks the window for the subject and the role; the role the core did not name is the lecturer", () => {
  const n = nav();
  const v = recipientView(noEmail({ role: "practice", subjectId: "s7" }));
  const button = RefusalButton({ action: refusalAction(v, "card"), nav: n });
  button.props.onClick();
  assert.deepEqual(n.seen, [["teacher", "s7", "practice"]]);
  assert.deepEqual(teacherRequest(recipientView(noEmail({ role: "", subjectId: "s7" }))), { subjectId: "s7", role: "lecture" });
  assert.equal(teacherRequest(recipientView(noEmail({ subjectId: "" }))), null);
  const lost = refusalAction(recipientView(noEmail({ subjectId: "" })), "card");
  assert.deepEqual(lost, { kind: "subjects", label: recipientTexts.openSubjects }, "no subject known: still a way out");
  RefusalButton({ action: lost, nav: n }).props.onClick();
  assert.deepEqual(n.seen[1], ["subjects"]);
});

test("no subject: the text and a way to choose it; on the assignment page the subject is chosen in place", () => {
  const out = cardView(noSubject(), { nav: nav() });
  assert.match(out, /У задания не указан предмет/);
  assert.match(out, />Открыть задание</);
  assert.doesNotMatch(out, />Да</);
  const v = recipientView(noSubject());
  assert.deepEqual(refusalAction(v, "card"), { kind: "assignment", label: "Открыть задание", assignmentId: "a1" });
  assert.deepEqual(refusalAction(v, "page"), { kind: "choose_subject", label: "Выбрать предмет" });
  assert.deepEqual(refusalAction(recipientView(noSubject({ assignmentId: "" })), "card"), { kind: "subjects", label: recipientTexts.openSubjects });
  const n = nav();
  RefusalButton({ action: refusalAction(v, "card"), nav: n }).props.onClick();
  assert.deepEqual(n.seen, [["assignment", "a1"]]);
  let chosen = 0;
  RefusalButton({ action: refusalAction(v, "page"), nav: n, onChooseSubject: () => chosen++ }).props.onClick();
  assert.equal(chosen, 1);
  assert.equal(recipientView({ reason: "no_subject", message: "" }).text, recipientTexts.noSubject);
  assert.match(recipientTexts.noSubject, /^У задания не указан предмет/);
});

test("a refusal can never be confirmed: no «Да»/«Отправить» for no address and no subject", () => {
  for (const c of [noEmail(), noSubject(), noEmail({ state: "waiting" })]) {
    const m = confirmCardModel(c);
    assert.equal(m.yes, false, c.reason);
  }
  assert.equal(sendBlocked(recipientView(noEmail()), null), recipientTexts.noEmailBlocked);
  assert.equal(sendBlocked(recipientView(noSubject()), null), recipientTexts.noSubjectBlocked);
  const dialog = html(h(SendDialog, { api: {}, prep: prep({ status: "rejected", recipient: "", reason: "no_teacher_email", role: "lab", message: noEmail().message }), version: 1, onClose: noop }));
  assert.match(dialog, /<button type="button" class="btn primary" disabled="">Отправить</);
  assert.match(dialog, /Нет почты преподавателя/);
});

test("the old kinds of rejected cards stay buttonless and keep their message", () => {
  const out = cardView(card({ state: "rejected", message: "Отправить нельзя: подготовьте отправку ещё раз." }), { nav: nav() });
  assert.match(out, /Отправить нельзя/);
  assert.doesNotMatch(out, /<button/);
});

// ---- Texts ----

test("the send note no longer points to the settings", () => {
  assert.equal(confirmTexts.sendNote, "Письмо уйдёт только после «Да» — на указанный адрес.");
  assert.doesNotMatch(confirmTexts.sendNote, /настро/);
  assert.match(cardView(card()), /только после «Да»/);
  const homework = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");
  assert.match(homework, /refusalAction\(refusal, "page"\)/, "the assignment page leads out of a refusal too");
  assert.match(homework, /setEditing\(true\)/);
});

test("the agent card and the work card share one confirm card, so both get the question and the refusals", () => {
  const workSend = readFileSync(new URL("../src/agent/WorkSend.tsx", import.meta.url), "utf8");
  assert.match(workSend, /<ConfirmCard /);
  const confirm = readFileSync(new URL("../src/agent/ConfirmCard.tsx", import.meta.url), "utf8");
  assert.match(confirm, /useRecipientNav\(\)/);
  assert.match(confirm, /flow\.choose\(role\)/);
});
