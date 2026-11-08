// Pack «Задания», part 6 (TASK-381; AC-14; ADR-0030). «Отправить преподавателю…» never sends by itself:
//   1. without an address of the teacher the core refuses with a reason, and the page offers the way out «Указать почту»;
//   2. with an address (a synthetic one on the subject page, the way the refusal leads) the button stops at the confirmation
//      «Отправить задание?» (version, recipient, subject, attachment) and «Отмена» sends nothing: «Письма» stays empty.
// No mail account is configured and «Отправить» is never pressed: there is no SMTP fake in Sprint 1 (ADR-0035 §7).
import type { Page } from "@playwright/test";

import { firstDraftRules } from "../../scripts/llm/assignments/rules.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { expectDraft, writeFirstDraft } from "./draft.js";
import { addAssignment, openFromList, openList, pageTitle, removeQuietly, removeTeachers } from "./driver.js";
import { act, dayFromNow, details, Feature, note, openSection, Subject, textOf, WithHub } from "./support.js";

const Teacher = { name: "Тестовый Преподаватель", email: "teacher@example.invalid" };

const sendButton = (main: Page) => main.getByRole("button", { name: "Отправить преподавателю…", exact: true });

const NoMail = "Писем по этому заданию ещё не было.";

const Refusal = "Отправка без адреса";
const RefusalDraft = { title: "Черновик письма: отправка без адреса", body: "Короткий текст для проверки отказа в отправке." };

test(Feature.sendRefusal, details(Feature.sendRefusal, { requires: WithHub }), async ({ main, llm }) => {
  test.setTimeout(4 * 60_000);
  try {
    await step("Hub: один ответ на запрос черновика", async () => {
      await llm.script(firstDraftRules(Refusal, RefusalDraft));
    });
    await step(`у предмета «${Subject}» нет преподавателей (убрать оставшихся через страницу предмета)`, async () => {
      // the row does not depend on the order of the rows: a teacher left by an earlier run would turn the refusal into a dialog
      const removed = await removeTeachers(main, Subject);
      note(`убрано преподавателей перед проверкой: ${removed}`);
    });
    await step(`добавить «${Refusal}» (История) и написать черновик`, async () => {
      await addAssignment(main, { title: Refusal, subject: Subject, deadline: { date: dayFromNow(3) } });
      await writeFirstDraft(main, RefusalDraft);
    });

    await step("«Отправить преподавателю…» без почты преподавателя: причина и «Указать почту», письмо не создано", async () => {
      await sendButton(main).click(act);
      const refusal = main.locator(".rcp-refusal");
      await expect(refusal, "отказ должен появиться под кнопками черновика").toBeVisible({ timeout: 30_000 });
      const text = await textOf(refusal);
      note(`отказ: ${text}`);
      expect(text, "отказ называет причину и предмет").toMatch(/Нет почты преподавателя.*предмета «История»: укажите её на странице предмета\./);
      await expect(refusal.getByRole("button", { name: "Указать почту", exact: true }), "у отказа есть выход «Указать почту»").toBeVisible(act);
      await expect(main.getByRole("dialog"), "окно подтверждения не открывается без адреса").toHaveCount(0);
      await expect(main.getByText(NoMail), "письмо не создано").toBeVisible(act);
      await snapshot("Отправка без почты преподавателя");
    });

    await step("«Указать почту» ведёт на страницу предмета с открытой формой лектора", async () => {
      await main.locator(".rcp-refusal").getByRole("button", { name: "Указать почту", exact: true }).click(act);
      const form = main.locator("form.teacher-form");
      await expect(form.first(), "после «Указать почту» открыта форма преподавателя").toBeVisible({ timeout: 15_000 });
      // one block «Преподаватели» and one open form: the student sees each once
      const blocks = await main.locator("section.teachers").count();
      note(`блоков «Преподаватели» на странице предмета: ${blocks}; открытых форм: ${await form.count()}`);
      expect(blocks, `на странице предмета блок «Преподаватели» должен быть один, а показан ${blocks} раз(а)`).toBe(1);
      await expect(form, "должна быть открыта одна форма лектора").toHaveCount(1, act);
      await expect(form.getByLabel("Почта", { exact: true })).toBeVisible(act);
      await form.getByRole("button", { name: "Отмена", exact: true }).click(act);
    });
  } finally {
    const left = await removeQuietly(main, [Refusal]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});

const Confirm = "Отправка с подтверждением";
const ConfirmDraft = { title: "Черновик письма: отправка с подтверждением", body: "Короткий текст для проверки окна подтверждения." };

test(Feature.sendDialog, details(Feature.sendDialog, { requires: WithHub }), async ({ main, llm }) => {
  test.setTimeout(5 * 60_000);
  try {
    await step("Hub: один ответ на запрос черновика", async () => {
      await llm.script(firstDraftRules(Confirm, ConfirmDraft));
    });
    await step(`добавить «${Confirm}» (История) и написать черновик`, async () => {
      await addAssignment(main, { title: Confirm, subject: Subject, deadline: { date: dayFromNow(3) } });
      await writeFirstDraft(main, ConfirmDraft);
    });

    await step("адрес преподавателя: отказ → «Указать почту» → имя и почта (синтетические) → «Сохранить»", async () => {
      await sendButton(main).click(act);
      const refusal = main.locator(".rcp-refusal");
      await expect(refusal).toBeVisible({ timeout: 30_000 });
      await refusal.getByRole("button", { name: "Указать почту", exact: true }).click(act);
      // `.first()`: the page of the subject shows the block several times (the finding of the previous row), this row goes on with the first form
      const form = main.locator("form.teacher-form");
      await expect(form.first()).toBeVisible({ timeout: 15_000 });
      await form.first().getByLabel("Имя", { exact: true }).fill(Teacher.name, act);
      await form.first().getByLabel("Почта", { exact: true }).fill(Teacher.email, act);
      await form.first().getByRole("button", { name: "Сохранить", exact: true }).click(act);
      await expect(main.getByText("Преподаватель сохранён."), "после «Сохранить» страница предмета сообщает об этом").toBeVisible({ timeout: 15_000 });
    });

    await step("«Отправить преподавателю…» останавливается на окне «Отправить задание?»", async () => {
      await openList(main);
      await openFromList(main, Confirm);
      await expectDraft(main, ConfirmDraft);
      await sendButton(main).click(act);
      const dialog = main.getByRole("dialog", { name: "Отправить задание?" });
      await expect(dialog, "с адресом кнопка открывает окно подтверждения").toBeVisible({ timeout: 30_000 });
      const text = await textOf(dialog);
      note(`окно: ${text}`);
      expect(text, "в окне версия черновика").toMatch(/Черновик\s+Версия 1/);
      expect(text, "в окне получатель").toContain(Teacher.email);
      expect(text, "в окне тема письма").toMatch(/Тема\s+\S+/);
      expect(text, "в окне вложение").toMatch(/Вложение\s+\S+/);
      expect(text, "окно говорит, когда письмо уйдёт").toContain("Письмо уйдёт сразу после подтверждения.");
      await expect(dialog.getByRole("button", { name: "Отмена", exact: true })).toBeEnabled(act);
      await snapshot("Окно «Отправить задание?»");
    });

    await step("«Отмена»: окно закрыто, «Отправка отменена: письмо не отправлено.», в «Письма» пусто", async () => {
      const dialog = main.getByRole("dialog", { name: "Отправить задание?" });
      await dialog.getByRole("button", { name: "Отмена", exact: true }).click(act);
      await expect(dialog).toBeHidden({ timeout: 15_000 });
      await expect(main.getByText("Отправка отменена: письмо не отправлено."), "после отмены страница говорит, что письмо не отправлено").toBeVisible(act);
      await expect(main.getByText(NoMail), "письмо не создано").toBeVisible({ timeout: 15_000 });
      await expect(pageTitle(main)).toHaveText(Confirm, act);
    });
  } finally {
    const left = await removeQuietly(main, [Confirm]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
    // the synthetic teacher must not stay on the subject: through the same page a student would use
    await removeTeachers(main, Subject).catch((e: unknown) => note(`не удалось убрать преподавателя после проверки: ${String(e).split("\n")[0]}`));
    await openSection(main, "Задания").catch(() => undefined);
  }
});
