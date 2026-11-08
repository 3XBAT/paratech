// Pack «Задания», part 7 (TASK-381; ADR-0029 addendum 2026-10-27). «Журнал» and the student's own changes: adding an assignment
// and changing its deadline by hand are the student's clicks, so by design nothing is written to the agent journal
// (input/wails/homeworkchange.go, homeworkcreate.go). The journal names an agent's changes «Добавлено задание», «Изменён срок
// задания», «Изменено задание» (frontend/src/journalView.ts); this row pins that a change made by hand leaves none of those lines.
// It runs after the draft row on purpose: the draft job («Решение задания») is a line the journal does show, so the page is
// proved to list things, and the missing lines are not just an empty journal.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { addAssignment, editDeadline, pageDue, removeQuietly } from "./driver.js";
import { act, comingText, dayFromNow, details, Feature, note, OnlySubjects, openSection, Subject, textOf } from "./support.js";

const Quiet = "Тихая правка срока по истории";

// Lines of the journal that an assignment change by an agent would leave (journalView.ts: toolLabels).
const AgentLines = ["Добавлено задание", "Изменён срок задания", "Изменено задание"];

test(Feature.journal, details(Feature.journal, { requires: OnlySubjects }), async ({ main }) => {
  test.setTimeout(3 * 60_000);
  try {
    await step(`добавить «${Quiet}» и сменить срок руками`, async () => {
      await addAssignment(main, { title: Quiet, subject: Subject, deadline: { date: dayFromNow(3) } });
      await editDeadline(main, Quiet, { date: dayFromNow(5) });
      await expect.poll(() => pageDue(main), { timeout: 15_000 }).toMatch(comingText({ date: dayFromNow(5), time: "20:00" }));
    });

    await step("«Журнал»: открыт и загружен", async () => {
      await openSection(main, "Журнал");
      await expect(main.getByRole("heading", { name: "Журнал", level: 1 })).toBeVisible(act);
      await expect(main.locator(".page").getByText("Загрузка…", { exact: true }), "журнал должен загрузиться").toHaveCount(0, { timeout: 20_000 });
      await snapshot("Журнал");
    });

    await step("в журнале нет строк о правке задания, сделанной руками", async () => {
      const titles = (await main.locator(".jr-sec .row p.t").allInnerTexts()).map((t) => t.replace(/\s+/g, " ").trim());
      note(`строки журнала: ${titles.join(" | ") || "нет"}`);
      const lines = titles.filter((t) => AgentLines.includes(t));
      expect(lines, "своя правка студента не пишется в журнал агента (ADR-0029); строки об изменении заданий появляются только от агента").toEqual([]);
      expect(await textOf(main.locator(".page").first()), "в журнале нет ошибки загрузки").not.toContain("Не удалось загрузить журнал");
    });
  } finally {
    const left = await removeQuietly(main, [Quiet]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});
