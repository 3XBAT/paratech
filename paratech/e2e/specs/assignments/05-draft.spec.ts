// Pack «Задания», part 5 (TASK-381; AC-13, PRD section 10; ADR-0035 §6). The short-answer path of the assignment page, with
// the Hub answers scripted (scripts/llm/assignments/rules.ts): «Быстрый черновик» → «Написать черновик» → the draft (title and
// text) in the fields → typing into the text and «Сохранить правку» (a new version) → «Дополнительные указания» + «Написать
// новую версию» (the Hub request CARRIES the instruction and the student's edit, a new version appears) → «Сохранить ▾»:
// «Файл .txt» writes the export of the app, «Word (.docx)» goes through the save dialog, which the E2E hook answers with
// <e2e-dir>/saved/<name> (adapters/e2ehooks/save.go). The block «Сделать под ключ» is only looked at, never started.
import { readFileSync } from "node:fs";
import path from "node:path";

import { regenerationRules, StudentClose, StudentOpen } from "../../scripts/llm/assignments/rules.js";
import { EnvCtlDir, EnvDataDir } from "../../src/contract.js";
import { findFiles } from "../../src/fixtures/files.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { docxText } from "./docx.js";
import { bodyField, expectDraft, instructionField, titleField, versionRows, versionText, writeFirstDraft } from "./draft.js";
import { addAssignment, removeQuietly } from "./driver.js";
import { act, dayFromNow, details, Feature, note, Subject, textOf, WithHub } from "./support.js";

const Title = "Составить план ответа про Смутное время";
const First = {
  title: "Черновик: план ответа про Смутное время",
  body: "## План ответа\n\nПервый абзац о причинах Смутного времени.\n\nВторой абзац о его итогах.",
};
const Second = {
  title: "Черновик, короче: ответ про Смутное время",
  body: "## Короткий ответ\n\nСмутное время — период кризиса власти. Пример: появление самозванцев.",
};
const Instruction = "Сделай ответ короче и добавь один пример";
const Typed = " Дописано вручную студентом.";

test(Feature.draft, details(Feature.draft, { requires: WithHub }), async ({ main, llm }) => {
  test.setTimeout(6 * 60_000);
  const ctl = requireEnv(EnvCtlDir);
  let latest = First;
  try {
    await step("Hub: два ответа на запросы черновика (первый и с указаниями)", async () => {
      await llm.script(regenerationRules(Title, First, Instruction, Second));
    });

    await step(`добавить «${Title}» и открыть страницу: «Черновика пока нет»`, async () => {
      await addAssignment(main, { title: Title, subject: Subject, deadline: { date: dayFromNow(3) }, note: "Ответить на двух страницах." });
      await expect(main.getByRole("heading", { name: "Быстрый черновик", level: 2 })).toBeVisible(act);
      await expect(main.getByRole("heading", { name: "Новый черновик", level: 2 }), "пока черновика нет, карточка называется «Новый черновик»").toBeVisible(act);
      await snapshot("Страница задания без черновика");
    });

    await step("«Написать черновик»: черновик (заголовок и текст) в полях, «Версия 1 · сгенерирована»", async () => {
      await writeFirstDraft(main, First);
      const line = await versionText(main);
      note(`строка версии: ${line}`);
      expect(line).toMatch(/Версия 1 · сгенерирована · .*последняя/);
      await expect(main.getByRole("heading", { name: "Доработать черновик", level: 2 }), "с черновиком карточка называется «Доработать черновик»").toBeVisible(act);
      await expect(versionRows(main)).toHaveCount(1, act);
      await snapshot("Страница задания: первый черновик");
    });

    await step("правка набором текста и «Сохранить правку»: новая версия 2 «исправлена вручную»", async () => {
      const body = bodyField(main);
      await body.click(act);
      await main.keyboard.press("Control+End");
      await body.pressSequentially(Typed, { delay: 5, timeout: 15_000 });
      await expect(body).toHaveValue(First.body + Typed, act);
      await main.getByRole("button", { name: "Сохранить правку", exact: true }).click(act);
      await expect(main.getByText("Правка сохранена: версия 2."), "после «Сохранить правку» строка сообщает версию 2").toBeVisible({ timeout: 15_000 });
      await expect(versionRows(main), "правка добавила версию, старая осталась").toHaveCount(2, { timeout: 15_000 });
      const line = await versionText(main);
      expect(line).toMatch(/Версия 2 · исправлена вручную/);
      await expect(bodyField(main)).toHaveValue(First.body + Typed, act);
      latest = { title: First.title, body: First.body + Typed };
    });

    await step("«Дополнительные указания» + «Написать новую версию»: версия 3 из ответа Hub", async () => {
      await instructionField(main).fill(Instruction, act);
      await main.getByRole("button", { name: /^Написать новую версию/ }).click(act);
      await expectDraft(main, Second);
      await expect(versionRows(main), "новая версия добавлена, старые остались").toHaveCount(3, { timeout: 15_000 });
      const line = await versionText(main);
      expect(line).toMatch(/Версия 3 · сгенерирована · .*последняя/);
      const rows = await versionRows(main).allInnerTexts();
      const instructed = rows.filter((r) => r.includes(`Указания: ${Instruction}`));
      expect(instructed, `у версии 3 в истории видны указания; строки: ${rows.join(" | ")}`).toHaveLength(1);
      await expect(instructionField(main), "после генерации поле указаний очищено").toHaveValue("", act);
      latest = Second;
      await snapshot("Страница задания: версия 3");
    });

    await step("Hub получил указание студента и его правку: что видела модель", async () => {
      const asked = (await llm.requests()).filter((r) => r.matched);
      expect(asked, "к провайдеру ушло ровно два запроса черновика").toHaveLength(2);
      const [first, second] = asked;
      expect(first?.lastUser, "первый запрос — про это задание").toContain(Title);
      expect(first?.lastUser, "в первом запросе нет указаний студента").not.toContain(StudentOpen);
      expect(second?.lastUser, "второй запрос несёт указание студента").toContain(Instruction);
      expect(second?.lastUser, "указание стоит в своём разделе").toContain(StudentOpen);
      expect(second?.lastUser).toContain(StudentClose);
      expect(second?.lastUser, "второй запрос несёт прежний черновик с правкой студента").toContain(Typed.trim());
    });

    await step("«Сохранить ▾» → «Файл .txt»: версия 3 записана в файл, текст файла равен черновику", async () => {
      await main.locator(".mmenu-save").getByRole("button", { name: "Сохранить", exact: true }).click(act);
      await main.getByRole("menuitem", { name: "Файл .txt", exact: true }).click(act);
      const status = main.getByRole("status").filter({ hasText: /сохранена в файл/ });
      await expect(status, "после «Файл .txt» строка называет файл").toBeVisible({ timeout: 15_000 });
      const text = await textOf(status);
      note(text);
      expect(text).toMatch(/Версия 3 сохранена в файл «homework\.md»/);
      const expected = `# ${latest.title}\n\n${latest.body}\n`;
      const files = findFiles(requireEnv(EnvDataDir), /^homework\.md$/).map((f) => ({ f, text: readFileSync(f, "utf8").replace(/\r\n/g, "\n") }));
      expect(
        files.some((x) => x.text === expected),
        `в данных приложения должен быть homework.md с текстом черновика; найдено файлов: ${files.length}; ${files.map((x) => JSON.stringify(x.text.slice(0, 80))).join(" | ")}`,
      ).toBe(true);
    });

    await step("«Сохранить ▾» → «Word (.docx)»: файл в <e2e-dir>/saved, в нём заголовок и текст черновика", async () => {
      await main.locator(".mmenu-save").getByRole("button", { name: "Сохранить", exact: true }).click(act);
      await main.getByRole("menuitem", { name: "Word (.docx)", exact: true }).click(act);
      const status = main.getByRole("status").filter({ hasText: /Файл Word сохранён/ });
      await expect(status, "после «Word (.docx)» строка сообщает, что файл сохранён").toBeVisible({ timeout: 20_000 });
      const text = await textOf(status);
      note(text);
      const name = /Файл: «([^»]+)»/.exec(text)?.[1];
      expect(name, `строка должна называть файл: ${text}`).toBeTruthy();
      const file = path.join(ctl, "saved", name as string);
      const docx = docxText(readFileSync(file));
      note(`текст файла: ${docx.replace(/\s+/g, " ").slice(0, 200)}`);
      expect(docx, "в файле заголовок черновика").toContain(latest.title);
      for (const line of latest.body.split("\n").filter((l) => l.trim() !== "")) {
        expect(docx, `в файле строка черновика «${line}»`).toContain(line.replace(/^#+\s*/, ""));
      }
    });

    await step("блок «Сделать под ключ»: заголовок, пояснение и «Начать разбор» на месте; разбор не запускается", async () => {
      await expect(main.getByRole("heading", { name: "Сделать под ключ", level: 2 })).toBeVisible(act);
      await expect(main.getByText(/paratech задаст 1–2 вопроса и напишет работу файлом/)).toBeVisible(act);
      await expect(main.getByRole("button", { name: "Начать разбор", exact: true })).toBeEnabled(act);
      await snapshot("Страница задания: блок «Сделать под ключ»");
    });
  } finally {
    const left = await removeQuietly(main, [Title]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});
