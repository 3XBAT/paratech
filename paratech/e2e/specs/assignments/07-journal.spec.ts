// Pack «Задания», part 7 (TASK-381; ADR-0029 addendum 2026-10-27). «Журнал» and the student's own changes: adding an assignment
// and changing its deadline by hand are the student's clicks, so by design nothing is written to the agent journal
// (input/wails/homeworkchange.go, homeworkcreate.go). The journal names an agent's changes «Добавлено задание», «Изменён срок
// задания», «Изменено задание» (frontend/src/journalView.ts); this row pins that a change made by hand leaves none of those lines.
// The row makes its own proof that the journal lists things: it writes a draft itself, and the draft job («Решение задания») is
// a line the journal shows. Without that line the row fails: a journal that is empty or not read proves nothing.
import { firstDraftRules } from "../../scripts/llm/assignments/rules.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { writeFirstDraft } from "./draft.js";
import { addAssignment, editDeadline, pageDue, removeQuietly } from "./driver.js";
import { act, comingText, dayFromNow, details, Feature, note, openSection, Subject, textOf, WithHub } from "./support.js";

const Quiet = "Тихая правка срока по истории";
const QuietDraft = { title: "Черновик для журнала: тихая правка", body: "Короткий текст, чтобы в журнале была строка." };

// Lines of the journal that an assignment change by an agent would leave (journalView.ts: toolLabels).
const AgentLines = ["Добавлено задание", "Изменён срок задания", "Изменено задание"];

// The line of the draft job (journalView.ts: jobLabels.homework_draft): proof that the page lists and the selector reads.
const DraftLine = "Решение задания";

test(Feature.journal, details(Feature.journal, { requires: WithHub }), async ({ main, llm }) => {
  test.setTimeout(4 * 60_000);
  const first = dayFromNow(3);
  const later = dayFromNow(5);
  try {
    await step("Hub: один ответ на запрос черновика", async () => {
      await llm.script(firstDraftRules(Quiet, QuietDraft));
    });

    await step(`добавить «${Quiet}», написать черновик и сменить срок руками`, async () => {
      await addAssignment(main, { title: Quiet, subject: Subject, deadline: { date: first } });
      await writeFirstDraft(main, QuietDraft);
      await editDeadline(main, Quiet, { date: later });
      await expect.poll(() => pageDue(main), { timeout: 15_000 }).toMatch(comingText({ date: later, time: "20:00" }));
    });

    await step("«Журнал»: открыт и загружен", async () => {
      await openSection(main, "Журнал");
      await expect(main.getByRole("heading", { name: "Журнал", level: 1 })).toBeVisible(act);
      await expect(main.locator(".page").getByText("Загрузка…", { exact: true }), "журнал должен загрузиться").toHaveCount(0, { timeout: 20_000 });
      await snapshot("Журнал");
    });

    await step("в журнале нет строк о правке задания, сделанной руками", async () => {
      const lines = () => main.locator(".jr-sec .row p.t").allInnerTexts().then((all) => all.map((t) => t.replace(/\s+/g, " ").trim()));
      // the control: the draft job is listed, so the selector reads real lines and an empty result below means «none»
      await expect.poll(lines, { timeout: 20_000, message: `в журнале должна быть строка «${DraftLine}» (черновик из этой строки отчёта)` }).toContain(DraftLine);
      const titles = await lines();
      note(`строки журнала: ${titles.join(" | ")}`);
      const found = titles.filter((t) => AgentLines.includes(t));
      expect(found, "своя правка студента не пишется в журнал агента (ADR-0029); строки об изменении заданий появляются только от агента").toEqual([]);
      expect(await textOf(main.locator(".page").first()), "в журнале нет ошибки загрузки").not.toContain("Не удалось загрузить журнал");
    });
  } finally {
    const left = await removeQuietly(main, [Quiet]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});
