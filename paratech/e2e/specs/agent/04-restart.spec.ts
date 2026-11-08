// Agent pack, part A, file 4: «Начать заново отсюда» (TASK-249, TASK-271, TASK-380). «Скрыть» puts the message into the field and
// the next request carries `restartFrom`: what the app then sends to the model must not hold the hidden answers. «Форк» opens a
// new chat «… (fork)» with the message text in the field, sends nothing, and tells where the original is.
import type { Locator, Page } from "@playwright/test";

import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { filler, replyRule } from "../../scripts/llm/agent/rules.js";
import { act, ask, collapse, details, expectCatalogV9, Feature, messagesOf, messagesText, note, panelOf, plannerRequests, prepare, sample, waitThread } from "./support.js";

const restartButtons = (main: Page): Locator => panelOf(main).locator(".ag-thread").getByRole("button", { name: /^Начать заново отсюда: / });
const dialogOf = (main: Page): Locator => main.getByRole("dialog", { name: "Начать заново отсюда?" });

test(Feature.restart, details(Feature.restart), async ({ main, llm }) => {
  test.setTimeout(90_000);
  const q1 = "что такое производная функции";
  const q2 = "а что такое интеграл функции";
  const edited = "объясни коротко и по делу, что такое производная"; // does not contain q1, so q1 in the history means "not hidden"
  const q3 = "как связаны производная и интеграл";
  const a1 = filler(200, 1);
  const a2 = filler(210, 2);
  const a1b = filler(190, 3);
  const a3 = filler(220, 4);

  await step("Hub: пять ответов по порядку (две пары, правка, вопрос для форка)", async () => {
    await llm.script([replyRule(q1, a1), replyRule(q2, a2), replyRule("коротко и по делу", a1b), replyRule(q3, a3)]);
  });
  await step("панель: новый пустой чат", () => prepare(main));

  await step("два вопроса с ответами", async () => {
    await ask(main, q1);
    await waitThread(main, "ответ на первый вопрос", (x) => x.replies.includes(collapse(a1)), { timeout: 30_000 });
    await ask(main, q2);
    await waitThread(main, "ответ на второй вопрос", (x) => x.replies.includes(collapse(a2)) && x.think === 0, { timeout: 30_000 });
    await snapshot("Агент: два вопроса");
  });

  await step("«Начать заново отсюда» под первым вопросом → окно → «Скрыть»: текст вопроса в поле", async () => {
    const icons = restartButtons(main);
    await expect(icons, "под каждым из двух вопросов должна быть иконка «Начать заново отсюда»").toHaveCount(2, act);
    await icons.first().click(act);
    const dialog = dialogOf(main);
    await expect(dialog, "после иконки должно открыться окно «Начать заново отсюда?»").toBeVisible(act);
    await expect(dialog.getByRole("button", { name: "Скрыть", exact: true }), "в окне должна быть кнопка «Скрыть»").toBeEnabled(act);
    await expect(dialog.getByRole("button", { name: "Форк", exact: true }), "в окне должна быть кнопка «Форк»").toBeEnabled(act);
    await snapshot("Агент: окно «Начать заново отсюда»");
    await dialog.getByRole("button", { name: "Скрыть", exact: true }).click(act);
    await expect(dialog, "после «Скрыть» окно должно закрыться").toBeHidden(act);
    const x = await sample(main);
    expect(x.field, "после «Скрыть» в поле должен быть текст первого вопроса").toBe(q1);
    await expect(main.getByText("Правка сообщения. После отправки всё, что ниже него, пропадёт из чата.", { exact: false }), "рядом с полем должна быть заметка о правке").toBeVisible(act);
  });

  await step("правка отправлена: скрытые запросы ушли из ленты, модель получила правку без скрытых ответов", async () => {
    await ask(main, edited);
    const x = await waitThread(main, "ответ на правку", (y) => y.replies.includes(collapse(a1b)) && y.think === 0, { timeout: 30_000 });
    note(`после правки в ленте вопросы: ${x.users.map((u) => `«${u.slice(0, 40)}»`).join(", ")}; ответы: ${x.replies.length}`);
    expect(x.users, "в ленте остался только исправленный вопрос").toEqual([edited]);
    expect(x.replies, "в ленте только ответ на исправленный вопрос").toEqual([collapse(a1b)]);

    const planner = await plannerRequests(llm);
    const sent = planner.at(-1);
    expect(planner.length, "до правки модель получила два запроса, на правку — третий").toBe(3);
    expect(sent?.lastUser.includes("коротко и по делу"), "последний запрос к модели — запрос правки").toBe(true);
    expectCatalogV9(planner);
    const history = sent ? messagesText(sent) : "";
    for (const [what, text] of [["первый вопрос", q1], ["ответ на него", a1.slice(0, 80)], ["второй вопрос", q2], ["ответ на второй", a2.slice(0, 80)]] as const) {
      expect(history.includes(text), `запрос к модели после «Скрыть» не должен содержать скрытое: ${what}`).toBe(false);
    }
    expect(history.includes(edited), "запрос к модели должен содержать исправленный текст").toBe(true);
    expect(sent ? messagesOf(sent).filter((m) => m.role === "assistant") : [], "в истории запроса не должно быть ответов агента: всё скрыто").toEqual([]);
    note("проверено: после «Скрыть» запрос к модели = только правка; ни вопросов, ни ответов скрытых запросов в истории");
    await snapshot("Агент: после «Скрыть»");
  });

  await step("ещё вопрос в том же чате (для форка)", async () => {
    await ask(main, q3);
    await waitThread(main, "ответ на третий вопрос", (x) => x.replies.includes(collapse(a3)) && x.think === 0, { timeout: 30_000 });
  });

  const before = (await plannerRequests(llm)).length;
  const original = (await sample(main)).chat;
  await step("«Начать заново отсюда» под последним вопросом → «Форк»: новый чат «… (fork)», текст в поле, ничего не отправлено", async () => {
    const icons = restartButtons(main);
    await expect(icons).toHaveCount(2, act);
    await icons.last().click(act);
    const dialog = dialogOf(main);
    await expect(dialog).toBeVisible(act);
    await dialog.getByRole("button", { name: "Форк", exact: true }).click(act);
    await expect(dialog, "после «Форк» окно должно закрыться").toBeHidden(act);
    const x = await waitThread(main, "должен открыться новый чат «… (fork)»", (y) => y.chat.includes("(fork)"), { timeout: 20_000 });
    note(`чат до форка: «${original}»; после: «${x.chat}»; заметка: «${x.chatNote}»; поле: ${x.field.length} знаков`);
    expect(x.chat, "заголовок нового чата должен кончаться на «(fork)»").toMatch(/\(fork\)$/);
    expect(x.field, "в поле нового чата должен быть текст сообщения, от которого сделан форк, и он не отправлен").toBe(q3);
    expect(x.chatNote, "под заголовком должна быть заметка о новом чате").toMatch(/^Открыт новый чат\. Исходный «.+» не изменился\.$/);
    await expect(
      panelOf(main).getByRole("button", { name: "Открыть исходный чат", exact: true }),
      "рядом с заметкой должна быть иконка «Открыть исходный чат»",
    ).toBeVisible(act);
    expect(x.requests, "в новом чате нет живых запросов: форк ничего не отправляет").toBe(0);
    expect(x.think, "в новом чате не должно быть бегущей строки").toBe(0);
    const earlier = await panelOf(main).locator(".ag-earlier").innerText(act);
    expect(collapse(earlier), "в форк скопирована история до сообщения: исправленный вопрос и его ответ").toContain(edited);
    expect((await plannerRequests(llm)).length, "форк не должен ничего отправлять модели").toBe(before);
    await snapshot("Агент: форк");
  });

  await step("«Открыть исходный чат»: исходный чат не изменился", async () => {
    await panelOf(main).getByRole("button", { name: "Открыть исходный чат", exact: true }).click(act);
    const x = await waitThread(main, "должен открыться исходный чат", (y) => !y.chat.includes("(fork)"), { timeout: 20_000 });
    expect(x.chat, "должен открыться тот же чат, что был до форка").toBe(original);
    const text = collapse(await panelOf(main).locator(".ag-thread").innerText(act));
    expect(text, "в исходном чате остался исправленный вопрос").toContain(edited);
    expect(text, "в исходном чате остался последний вопрос, от которого сделан форк").toContain(q3);
    expect(text, "в исходном чате остался ответ на последний вопрос").toContain(collapse(a3));
    expect((await plannerRequests(llm)).length, "возврат в исходный чат ничего не отправляет модели").toBe(before);
  });
});
