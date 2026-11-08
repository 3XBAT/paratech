// Agent pack, part B, file 10: «добавь предмет по последней записи» (TASK-369). Before the fix the planner asked the name back because
// the recordings had none in find_lessons. SCRIPTED: the model looks first (find_lessons, no question card), then calls
// create_subject (class U: it runs at once, the panel shows the action line with «Отменить»); the pack asserts the names the app
// gave the model, that the subject is in the tree of «Предметы», and that «Отменить» takes it back without a leftover.
//
// The model's final `reply` is delayed (25 s) so the action line can be used while the request is still running: the app replaces the
// cards of a request with the last one, the model's reply (the row «карточки остаются после ответа модели», file 13).
//
// The name: the app checks a name of create_subject by its shape only (values.ParseName: 1..80 letters, digits, spaces, .,-();
// there is no check that it occurs in the request or in a tool result, the prompt asks for a copy of subject_name only as
// a rule for the model). The last recording of the pack belongs to «Литература», which is already in «Предметы», so the scripted
// model picks a new name that is valid; this checks the plumbing (look first, action line, tree, undo), not the choice of the name.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterLessons, AfterWrite, callRule, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  act,
  ask,
  cardsOf,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  lectureOf,
  note,
  openSection,
  plannerRequests,
  PrerequisitesB,
  prepare,
  subjectNames,
  toolResultsOf,
  waitCards,
  waitThread,
} from "./support.js";

const replyDelayMs = 25_000;

test(Feature.addSubject, details(Feature.addSubject, { requires: PrerequisitesB }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const question = "добавь предмет по последней записи";
  const name = "Теория литературы";
  const answer = `Предмет «${name}» добавлен.`;
  const last = lectureOf("перезапуск");

  await step("Hub: модель сначала вызывает find_lessons, потом create_subject, потом (через 25 с) отвечает", async () => {
    await llm.script([
      callRule(question, "find_lessons", {}),
      callRule(AfterLessons, "create_subject", { name }),
      replyRule(AfterWrite, answer, { delayMs: replyDelayMs }),
    ]);
  });
  let before: string[] = [];
  await step("«Предметы»: такого предмета ещё нет", async () => {
    before = await subjectNames(main);
    expect(before, `до просьбы предмета «${name}» в «Предметы» нет`).not.toContain(name);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  let asked = 0;
  await step("вопрос → модель сначала смотрит записи, предмет создаётся, строка действия с «Отменить»; карточки вопросов нет ни разу", async () => {
    await ask(main, question);
    const cards = await waitCards(
      main,
      "строка действия должна появиться",
      (c) => {
        asked = Math.max(asked, c.askCards);
        return c.actionLines.length > 0;
      },
      60_000,
    );
    note(`строка действия: ${cards.actionLines.join(" | ")}; кнопки: ${cards.actionButtons.join(" | ")}`);
    expect(asked, "ученика не спрашивали об имени: карточки «ask_questions» не было").toBe(0);
    expect(cards.actionLines.length, "одна строка действия: предмет создан").toBe(1);
    // the line of create_subject is the generic «Действие выполнено» (no wording of its own yet): only that it says something
    expect((cards.actionLines[0] ?? "").length, "у строки действия есть текст").toBeGreaterThan(0);
    expect(cards.actionButtons, "у строки действия есть «Отменить»").toContain("Отменить");
    await snapshot("Агент: предмет добавлен, строка действия");
  });

  await step("модель увидела названия записей, а create_subject приложение выполнило (ok)", async () => {
    await expect
      .poll(async () => (await plannerRequests(llm)).length, { timeout: 15_000, intervals: [100, 200], message: "после create_subject модель должна получить третий запрос" })
      .toBe(3);
    const planner = await plannerRequests(llm);
    expectCatalogV9(planner);
    const looked = toolResultsOf(planner[1] as NonNullable<(typeof planner)[1]>);
    expect(looked.length).toBe(1);
    expect(looked[0]?.tool).toBe("find_lessons");
    expect(looked[0]?.isError).toBe(false);
    expect(looked[0]?.text.includes(last.title), `find_lessons дал модели название записи «${last.title}»`).toBe(true);
    expect(looked[0]?.text.includes("Литература"), "и предмет записи").toBe(true);
    const wrote = toolResultsOf(planner[2] as NonNullable<(typeof planner)[2]>);
    expect(wrote.length).toBe(1);
    const w = wrote[0] as NonNullable<(typeof wrote)[0]>;
    note(`результат create_subject для модели: is_error=${w.isError} ${w.text}`);
    expect(w.tool).toBe("create_subject");
    expect(w.isError, `create_subject не должен вернуть ошибку: ${w.text.slice(0, 200)}`).toBe(false);
    const j = w.json as { done?: boolean; changed?: boolean; id?: string };
    expect(j.done, "результат: done").toBe(true);
    expect(j.changed, "результат: предмет создан (changed)").toBe(true);
    expect((j.id ?? "") !== "", "в результате id нового предмета").toBe(true);
  });

  await step("«Предметы»: новый предмет в дереве", async () => {
    const names = await subjectNames(main);
    note(`дерево до: ${before.join(", ")}; после: ${names.join(", ")}`);
    expect(names, `предмет «${name}» должен появиться в дереве «Предметы»`).toContain(name);
    expect(names.length, "добавлен ровно один предмет").toBe(before.length + 1);
    await snapshot("Предметы: новый предмет от помощника");
  });

  await step("«Отменить» на строке действия: предмета в дереве нет, остальное как было", async () => {
    const button = main.locator("aside.ag .ag-act").getByRole("button", { name: "Отменить", exact: true });
    await expect(button, "кнопка «Отменить» на строке действия").toBeVisible(act);
    await button.click(act);
    await waitCards(main, "строка действия должна стать «отменено»", (c) => !c.actionButtons.includes("Отменить"), 20_000);
    const cards = await cardsOf(main);
    note(`строка действия после отмены: ${cards.actionLines.join(" | ")}`);
    expect(cards.problems, "отмена прошла без сообщения об ошибке").toEqual([]);
    // What the student sees on the page that is open: does the tree follow the undo by itself? (a note; the check is after re-entering)
    const treeNames = async (): Promise<string[]> => (await main.locator("nav.tree .nm").allInnerTexts()).map((n) => collapse(n));
    let liveGone = false;
    await expect
      .poll(async () => (liveGone = !(await treeNames()).includes(name)), { timeout: 3_000, intervals: [250, 500] })
      .toBe(true)
      .catch(() => undefined);
    note(liveGone ? "дерево открытой страницы «Предметы» обновилось само после «Отменить»" : "дерево открытой страницы «Предметы» само не обновилось после «Отменить» (предмет виден, пока страницу не открыть заново)");
    // the page reads the library again when the section is opened
    await openSection(main, "Сегодня");
    await expect
      .poll(async () => (await subjectNames(main)).includes(name), { timeout: 20_000, intervals: [300, 500], message: `после «Отменить» предмет «${name}» не должен остаться в дереве` })
      .toBe(false);
    const after = await subjectNames(main);
    expect(after, "после отмены дерево предметов такое же, как до просьбы").toEqual(before);
    await snapshot("Предметы: после отмены");
    note(`проверено: look-first (find_lessons до create_subject, вопросов нет), create_subject ok, строка действия, предмет «${name}» в дереве и пропал после «Отменить» (дерево как до)`);
  });

  await step("ответ модели приходит и показан как есть (строка действия после него — в строке «карточки остаются после ответа»)", async () => {
    const x = await waitThread(main, "ответ модели должен появиться", (y) => y.replies.includes(collapse(answer)) && y.think === 0, { timeout: 60_000 });
    expect(x.replies).toEqual([collapse(answer)]);
  });
});
