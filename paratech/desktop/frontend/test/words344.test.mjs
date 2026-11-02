// TASK-344: one word per thing (docs/design/glossary.md). The waiting states come from one dictionary, the old words are
// gone from the UI texts, and every deadline reads through the one dueText wording.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { jobStatusLabels, jobTypeLabels } from "../.test-build/activityView.js";
import { generationView, dueText as homeworkDueText } from "../.test-build/homeworkView.js";
import { lineModel } from "../.test-build/journalView.js";
import { queueReasonTexts, chainStatusView } from "../.test-build/recordingRowView.js";
import { statusView as ticketStatus, texts as ticketTexts } from "../.test-build/ticketsView.js";
import { headings, recordingTexts } from "../.test-build/popup/view.js";
import { problemTexts } from "../.test-build/sourcePicker/view.js";
import { texts as libraryTexts } from "../.test-build/libraryView.js";
import { lessonTabs } from "../.test-build/knowledgeView.js";
import { waitingInline, waitingWords } from "../.test-build/statusWords.js";
import { addedAssignmentText, assignmentRow, buttonLabels, texts as agentTexts } from "../.test-build/agentView.js";
import { dueText } from "../.test-build/dueText.js";
import { dueText as todayDueText } from "../.test-build/todayView.js";
import { approveLabel } from "../.test-build/planView.js";
import { turnkeyTexts as helperTexts } from "../.test-build/turnkeyView.js";
import { turnkeyTexts as blockTexts } from "../.test-build/homeworkTurnkey.js";
import { workTexts } from "../.test-build/workView.js";

// ---- The waiting dictionary ----

test("the waiting dictionary has the three words of the glossary", () => {
  assert.equal(waitingWords.network, "Ждёт интернет");
  assert.equal(waitingWords.quota, "Ждёт пополнения минут");
  assert.equal(waitingWords.power, "Ждёт зарядки");
  assert.equal(waitingInline("network"), "ждёт интернет");
  assert.equal(waitingInline("power"), "ждёт зарядки");
});

test("activity, journal, recording rows, homework and tickets say a wait with the same words", () => {
  assert.equal(jobStatusLabels.WAITING_FOR_NETWORK, waitingWords.network);
  assert.equal(jobStatusLabels.WAITING_FOR_QUOTA, waitingWords.quota);
  assert.equal(jobStatusLabels.WAITING_FOR_POWER, waitingWords.power);
  assert.equal(jobStatusLabels.RETRY_WAIT, waitingWords.retry);

  const journalLabel = (status) =>
    lineModel({ kind: "processing", at: "", updatedAt: "", subjectId: "", actionId: "", threadId: "", turn: 0, tool: "", jobId: "j", jobType: "stt", status, undoable: false, cannotUndo: false, noUndo: "" }, 0, new Map()).status;
  assert.equal(journalLabel("WAITING_FOR_NETWORK"), waitingWords.network);
  assert.equal(journalLabel("WAITING_FOR_QUOTA"), waitingWords.quota);
  assert.equal(journalLabel("WAITING_FOR_POWER"), waitingWords.power);

  assert.equal(queueReasonTexts.network, waitingInline("network"));
  assert.equal(queueReasonTexts.quota, waitingInline("quota"));
  assert.equal(queueReasonTexts.power, waitingInline("power"));

  assert.equal(generationView({ state: "waitingNetwork", message: "", percent: 0 }).label, waitingWords.network);
  assert.equal(generationView({ state: "waitingQuota", message: "", percent: 0 }).label, waitingWords.quota);

  const set = (jobStatus) => ({ status: "answering", stalled: false, jobStatus });
  assert.equal(ticketStatus(set("WAITING_FOR_NETWORK")).label, waitingWords.network);
  assert.equal(ticketStatus(set("WAITING_FOR_QUOTA")).label, waitingWords.quota);
  assert.equal(ticketStatus(set("WAITING_FOR_POWER")).label, waitingWords.power);
});

test("a queued recording row names the wait with the dictionary", () => {
  const row = (queueReason) => ({
    stage: "queued", step: "transcription", queueReason, status: "COMPLETED", progressPercent: 0, parts: 0, partsDone: 0,
    action: "", retryWork: "", errorCode: "", startedAt: "", durationSeconds: 0, hasDuration: false,
  });
  assert.equal(chainStatusView(row("power"), 0).label, "В очереди · ждёт зарядки");
  assert.equal(chainStatusView(row("quota"), 0).label, "В очереди · ждёт пополнения минут");
  assert.equal(chainStatusView(row("network"), 0).label, "В очереди · ждёт интернет");
});

// ---- The words of the glossary ----

test("speech-to-text is «Расшифровка»", () => {
  assert.equal(jobTypeLabels.stt, "Расшифровка");
});

test("the agent: «Остановить», «План», «Пропустить вопросы», «Да, выполнить», «запрос»", () => {
  assert.equal(buttonLabels.stop, "Остановить");
  assert.equal(buttonLabels.approve, "Да, выполнить");
  assert.equal(buttonLabels.restart, "Повторить");
  assert.equal(agentTexts.planCaption, "План");
  assert.equal(agentTexts.skip, "Пропустить вопросы");
  assert.equal(approveLabel(0), "Да, выполнить");
  assert.equal(approveLabel(3), "Да, выполнить 3 шага");
});

test("the turnkey helper: the same skip, the same retry, no «Ответ» caption; the block explains the cost", () => {
  assert.equal(helperTexts.skip, "Пропустить вопросы");
  assert.equal(helperTexts.retry, "Повторить");
  assert.equal("answerLabel" in helperTexts, false);
  assert.equal("answerLabel" in workTexts, false);
  assert.equal(
    blockTexts.caption,
    "paratech задаст 1–2 вопроса и напишет работу файлом (Word или PowerPoint). Разбор расходует минуты обработки.",
  );
  assert.equal(blockTexts.button, "Начать разбор");
});

test("one pair, one task, one record: popup, picker, tabs, buttons and the journal", () => {
  assert.equal(headings.lesson, "Начинается пара");
  assert.equal(recordingTexts.tip, "Окно можно закрыть — запись продолжится. Остановить можно и в окне paratech, раздел «Записи».");
  assert.equal(recordingTexts.stopFailed, "Не удалось остановить запись. Остановите её в окне paratech, раздел «Записи».");
  assert.equal(problemTexts.in_progress, "Уже идёт другая запись — её можно остановить в окне paratech, раздел «Записи».");
  assert.equal(libraryTexts.addSubject, "Добавить предмет");
  assert.equal(libraryTexts.addFolder, "Новая папка");
  assert.equal(libraryTexts.lessons, "Пары");
  assert.equal(lessonTabs[0].label, "Мой конспект");
  assert.equal(lessonTabs[1].label, "Конспект");
  assert.equal(ticketTexts.pasteAction, "Разобрать список");
  assert.equal(ticketTexts.stalledTitle, "Подбор остановлен");

  const act = (tool) =>
    lineModel({ kind: "action", at: "", updatedAt: "", subjectId: "", actionId: "a1", threadId: "t", turn: 1, tool, jobId: "", jobType: "", status: "DONE", undoable: false, cannotUndo: false, noUndo: "" }, 0, new Map()).title;
  assert.equal(act("find_homework"), "Поиск заданий в записи пары");
  assert.equal(act("triage_assignment"), "Задание разобрано");
  assert.doesNotMatch(act("generate_draft"), /ДЗ/);
});

// ---- One deadline wording ----

const now = new Date(2026, 9, 26, 12, 0, 0);
const iso = (day) => `2026-10-${String(day).padStart(2, "0")}`;

test("the agent «added» line reads the deadline like «Сегодня»: «завтра до 18:30»", () => {
  const a = (over) => ({ id: "a1", title: "Эссе", subjectId: "", due: iso(27), time: "18:30", ...over });
  assert.equal(addedAssignmentText(a({}), {}, now), "Задание добавлено: Эссе · завтра до 18:30");
  assert.equal(addedAssignmentText(a({ due: iso(26), time: "20:00" }), {}, now), "Задание добавлено: Эссе · сегодня до 20:00");
  assert.equal(addedAssignmentText(a({ due: iso(30) }), {}, now), "Задание добавлено: Эссе · до пт, 30 окт., 18:30");
  assert.equal(addedAssignmentText(a({ due: iso(24) }), {}, now), "Задание добавлено: Эссе · просрочено, сб, 24 окт., 18:30");
  assert.equal(addedAssignmentText(a({ due: "", time: "" }), {}, now), "Задание добавлено: Эссе");
});

test("the agent rows and the «Сегодня» wording are the same function", () => {
  const line = (due, time) => ({ id: "a1", title: "Эссе", subjectId: "", due, time });
  const at = (day, h, m) => new Date(2026, 9, day, h, m).toISOString();
  for (const [day, time, h, m] of [[26, "20:00", 20, 0], [27, "18:30", 18, 30], [30, "18:30", 18, 30], [24, "18:30", 18, 30], [26, "09:00", 9, 0]]) {
    assert.equal(assignmentRow(line(iso(day), time), {}, now).due, todayDueText(at(day, h, m), now), `${day} ${time}`);
  }
  // A passed deadline keeps its date and is drawn as overdue.
  const late = assignmentRow(line(iso(24), "18:30"), {}, now);
  assert.equal(late.due, "просрочено, сб, 24 окт., 18:30");
  assert.equal(late.overdue, true);
});

test("«Задания» shows the deadline in the same style as «Сегодня», never the numeric date", () => {
  const due = new Date(2026, 9, 24, 18, 30).toISOString();
  assert.equal(homeworkDueText(due), "до сб, 24 окт., 18:30");
  assert.equal(homeworkDueText(due), dueText(due));
  assert.equal(homeworkDueText(""), "Без срока");
  assert.doesNotMatch(homeworkDueText(due), /\d{2}\.\d{2}\.\d{4}/);
});

// ---- The old words are gone from the UI texts ----

function sourceFiles(dir = new URL("../src/", import.meta.url)) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const url = new URL(name + (statSync(new URL(name, dir)).isDirectory() ? "/" : ""), dir);
    if (name === "bindings") {
      continue; // the fixed texts of the Go core are mirrored there, Go owns them
    }
    if (statSync(url).isDirectory()) {
      out.push(...sourceFiles(url));
    } else if (/\.tsx?$/.test(name)) {
      out.push(url);
    }
  }
  return out;
}

// Comments are English notes that may quote the old names; only code and its string literals count.
const code = (text) => text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:"'`])\/\/.*$/gm, "$1");

const oldWords = [
  /Краткое содержание/,
  /"Кратко"/,
  /Записи и задачи/,
  /Записи и задания/,
  /Записях и задачах/,
  /Домашние задания/,
  /Домашних заданиях/,
  /домашки/,
  /Понял так/,
  /"Стоп"/,
  />Стоп</,
  /Попробовать снова/,
  /Запустить снова/,
  /Найти билеты/,
  /Застряло/,
  /Сгенерировать ДЗ/,
  /Ждёт сети/,
  /Ждёт лимита/,
  /Ждёт питания/,
  /Ждёт зарядку/,
  /компонент распознавания/,
  /Начинается занятие/,
  /Пропустить — сделай/,
  /Пропустить: принять/,
  /просьб/,
  /Оценено задание/,
  /Запущен поиск ДЗ/,
];

test("no UI text uses a word the glossary replaced", () => {
  const files = sourceFiles();
  assert.ok(files.length > 100, "the scan really reads the sources");
  assert.equal(code("a // «Стоп»\nb /* «Стоп» */ c"), "a \nb  c", "comments are skipped");
  assert.match(code('x = "«Стоп»"'), /Стоп/, "string literals are kept");
  for (const url of files) {
    const text = code(readFileSync(url, "utf8"));
    for (const re of oldWords) {
      assert.doesNotMatch(text, re, `${url.pathname.split("/src/")[1]}: ${re}`);
    }
  }
});
