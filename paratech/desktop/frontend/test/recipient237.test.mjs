// TASK-237: the recipient of «Отправить преподавателю» in the bindings: the new reply fields are parsed defensively and
// the clicked teacher goes back to the core with the confirmation. No UI here (the card «Кому отправить?» is TASK-238).
import { test } from "node:test";
import assert from "node:assert/strict";
import { agentConfirmApi, confirmErrorText, errorTexts, methods as agentMethods, parseConfirmCard } from "../.test-build/bindings/agentConfirm.js";
import { errorMessages, homeworkApi, methods as hwMethods, parseSendPreparation } from "../.test-build/bindings/homework.js";
import { choiceArgs, parseTeacherOptions, recipientReason, teacherRole } from "../.test-build/bindings/recipient.js";

const evil = "<img src=x onerror=alert(1)>";
const goText = "Выберите, кому отправить: лектору, семинаристу или преподавателю лабораторных.";

const options = [
  { role: "lecture", name: "Иванов И. И.", email: "lecture@example.test" },
  { role: "practice", name: "", email: "practice@example.test" },
];

const prep = (over = {}) => ({
  proposalId: "p1", status: "ready", reason: "", message: "", recipient: "teacher@example.test", recipientName: "",
  role: "", subjectId: "", options: null, subject: "ДЗ", attachmentName: "homework.txt", attachmentSize: 12,
  attachmentFormat: "txt", ...over,
});

const card = (over = {}) => ({
  kind: "send", targetKind: "proposal", targetId: "p1", state: "waiting", message: "", assignmentId: "a1",
  title: "Задачи", subject: "Матанализ", due: "", recipient: "teacher@example.test", recipientName: "", role: "",
  reason: "", subjectId: "", options: null, emailSubject: "ДЗ", attachmentName: "dz.txt", attachmentSize: 1,
  attachmentFormat: "txt", draftVersion: 0, notice: "", deletion: null, ...over,
});

test("homework: a send that asks «Кому отправить?» is parsed with its options", () => {
  const p = parseSendPreparation(
    prep({ recipient: "", reason: "choose_teacher", subjectId: "s1", role: "", options }),
  );
  assert.equal(p.reason, "choose_teacher");
  assert.equal(p.recipient, "");
  assert.equal(p.subjectId, "s1");
  assert.deepEqual(p.options, options);
  const one = parseSendPreparation(prep({ recipientName: "Петров П. П.", role: "lab" }));
  assert.equal(one.recipientName, "Петров П. П.");
  assert.equal(one.role, "lab");
  assert.deepEqual(one.options, []);
  assert.equal(one.recipient, "teacher@example.test");
});

test("homework: replies without the new fields keep parsing with empty ones", () => {
  const { recipientName, role, subjectId, options: _o, ...old } = prep();
  const p = parseSendPreparation(old);
  assert.deepEqual([p.recipientName, p.role, p.subjectId, p.options], ["", "", "", []]);
  assert.equal(p.recipient, "teacher@example.test");
});

test("homework: a refused send keeps its reason and the role without an address", () => {
  const p = parseSendPreparation(
    prep({ status: "rejected", reason: "no_teacher_email", message: "Нет почты", recipient: "", role: "practice", subjectId: "s1" }),
  );
  assert.deepEqual([p.status, p.reason, p.role, p.subjectId], ["rejected", "no_teacher_email", "practice", "s1"]);
  assert.equal(parseSendPreparation(prep({ status: "rejected", reason: "no_subject", recipient: "" })).reason, "no_subject");
});

test("homework: unknown role is empty, malformed options are dropped, text stays text", () => {
  const p = parseSendPreparation(
    prep({
      role: "dean",
      recipientName: evil,
      subjectId: 7,
      options: [
        { role: "lecture", name: evil, email: "a@example.test" },
        { role: "dean", name: "x", email: "b@example.test" },
        { role: "lab", name: "x", email: "" },
        { role: "lab", name: 1, email: "c@example.test" },
        { role: "lab", name: "x" },
        null,
        "lab",
        42,
        { role: "lab", name: "Лаб", email: "lab@example.test" },
      ],
    }),
  );
  assert.equal(p.role, "");
  assert.equal(p.subjectId, "");
  assert.equal(p.recipientName, evil);
  assert.deepEqual(p.options, [
    { role: "lecture", name: evil, email: "a@example.test" },
    { role: "lab", name: "Лаб", email: "lab@example.test" },
  ]);
  assert.deepEqual(parseSendPreparation(prep({ options: "lecture" })).options, []);
  assert.deepEqual(parseSendPreparation(prep({ options: { role: "lecture" } })).options, []);
  assert.deepEqual(parseSendPreparation(prep({ recipientName: 5 })).recipientName, "");
});

test("homework: confirmSend passes the chosen role and address, or two empty strings", async () => {
  const calls = [];
  const api = homeworkApi(async (m, ...a) => (calls.push([m, ...a]), null));
  await api.confirmSend("p1", "ДЗ", "txt");
  await api.confirmSend("p1", "ДЗ", "docx", null);
  await api.confirmSend("p1", "ДЗ", "docx", { role: "practice", address: "practice@example.test" });
  assert.deepEqual(calls, [
    [hwMethods.confirmSend, "p1", "ДЗ", "txt", "", ""],
    [hwMethods.confirmSend, "p1", "ДЗ", "docx", "", ""],
    [hwMethods.confirmSend, "p1", "ДЗ", "docx", "practice", "practice@example.test"],
  ]);
});

test("agent card: the recipient fields are parsed; old cards get empty ones", () => {
  const c = parseConfirmCard(card({ recipient: "", reason: "choose_teacher", subjectId: "s1", options, recipientName: "Иванов" }));
  assert.equal(c.reason, "choose_teacher");
  assert.equal(c.recipient, "");
  assert.equal(c.recipientName, "Иванов");
  assert.equal(c.subjectId, "s1");
  assert.deepEqual(c.options, options);
  const { recipientName, role, reason, subjectId, options: _o, ...old } = card();
  const o = parseConfirmCard(old);
  assert.deepEqual([o.recipientName, o.role, o.reason, o.subjectId, o.options], ["", "", "", "", []]);
  const none = parseConfirmCard(card({ options: [] }));
  assert.deepEqual(none.options, []);
});

test("agent card: a refused send names the reason and the role; unknown codes are empty", () => {
  const c = parseConfirmCard(
    card({ state: "rejected", message: "Нет почты", recipient: "", reason: "no_teacher_email", role: "lab", subjectId: "s2" }),
  );
  assert.deepEqual([c.state, c.reason, c.role, c.subjectId], ["rejected", "no_teacher_email", "lab", "s2"]);
  assert.equal(parseConfirmCard(card({ state: "rejected", reason: "no_subject" })).reason, "no_subject");
  const odd = parseConfirmCard(card({ reason: "file_moved", role: "dean", options: [{ role: "x", name: "", email: "a@b" }] }));
  assert.deepEqual([odd.reason, odd.role, odd.options], ["", "", []]);
  assert.equal(parseConfirmCard(card({ reason: 3 })).reason, "");
});

test("agent card: confirm passes the chosen role and address, or two empty strings", async () => {
  const seen = [];
  const api = agentConfirmApi(async (m, ...a) => (seen.push([m, a]), card()));
  const c = parseConfirmCard(card({ recipient: "", reason: "choose_teacher", options }));
  await api.confirm(c);
  await api.confirm(c, { role: "lecture", address: "lecture@example.test" });
  assert.deepEqual(seen, [
    [agentMethods.confirm, ["proposal", "p1", "ДЗ", "txt", "", ""]],
    [agentMethods.confirm, ["proposal", "p1", "ДЗ", "txt", "lecture", "lecture@example.test"]],
  ]);
});

test("ErrSendChooseTeacher is listed exactly as Go has it and shown as it is", () => {
  assert.equal(errorMessages.sendChooseTeacher, goText);
  assert.ok(errorTexts.includes(goText));
  assert.equal(confirmErrorText(new Error(goText)), goText);
});

test("recipient helpers", () => {
  assert.equal(teacherRole("lecture"), "lecture");
  assert.equal(teacherRole("seminar"), "");
  assert.equal(teacherRole(undefined), "");
  assert.equal(recipientReason("choose_teacher"), "choose_teacher");
  assert.equal(recipientReason("stale"), "");
  assert.deepEqual(parseTeacherOptions(undefined), []);
  assert.deepEqual(choiceArgs(), ["", ""]);
  assert.deepEqual(choiceArgs(null), ["", ""]);
  assert.deepEqual(choiceArgs({ role: "lab", address: "l@example.test" }), ["lab", "l@example.test"]);
});
