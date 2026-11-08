// Agent pack, part B, file 7: «дай ссылку на пару» (TASK-364, ADR-0032 addendum). The owner was once sent to «Расписание», which holds
// no links. SCRIPTED: the model calls list_subjects; the pack asserts what the APP put into the tool result (the call_link of the
// subject that has one, nothing on the others), that the system prompt of the app carries the rule about links, and that the panel
// offers nothing to open: catalog v9 has no tool that makes a navigation card or button (offer_button comes with catalog 10), so the
// way to «Подключиться» is the one the reply names, and the pack walks it like a student. The model's wording is the LIVE set.
import type { Page } from "@playwright/test";

import { CallLinkUrl, WeekSubjects } from "../../src/fixtures/schedule.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterSubjects, callRule, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  act,
  ask,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  LinkPrerequisite,
  lastStudentText,
  note,
  openSection,
  plannerRequests,
  PrerequisitesB,
  prepare,
  navigationControls,
  cardsOf,
  subjectNames,
  systemOf,
  textOf,
  toolResultsOf,
  waitThread,
} from "./support.js";

interface SubjectRow {
  id?: string;
  name?: string;
  call_link?: string;
}

// Stable parts of the rule of the planner prompt (hub/pkg/prompts/agent/panel/v9/system.md, "Questions about the app or the data" and
// "Screens of the app"). The model's wording is not checked here: only that the app sends these rules.
const linkRule = "No tool saves a link: never offer to save it, and never name «Расписание» or any place other than these two.";
const scheduleHasNoLinks = "«Расписание»: the schedule of lessons. It holds no links to online classes.";
const linkWhere = "«Предметы» → the page of the subject (always)";

async function subjectRows(requests: Awaited<ReturnType<typeof plannerRequests>>): Promise<SubjectRow[]> {
  const second = requests[1];
  expect(second, "после list_subjects модель должна получить второй запрос с результатом").toBeDefined();
  const results = toolResultsOf(second as NonNullable<typeof second>);
  expect(results.length, "во втором запросе один результат инструмента").toBe(1);
  const result = results[0] as NonNullable<(typeof results)[0]>;
  expect(result.tool).toBe("list_subjects");
  expect(result.isError, `list_subjects не должен вернуть ошибку: ${result.text.slice(0, 200)}`).toBe(false);
  return ((result.json as { subjects?: SubjectRow[] } | undefined)?.subjects ?? []) as SubjectRow[];
}

/** Opens the page of a subject from the tree of «Предметы». */
async function openSubject(main: Page, name: string): Promise<void> {
  await subjectNames(main);
  await main.locator("nav.tree .nm").filter({ hasText: name }).first().click(act);
  await expect(main.getByRole("heading", { name, exact: true }).first(), `должна открыться страница предмета «${name}»`).toBeVisible(act);
  await main.mouse.move(1, 1);
}

test(Feature.link, details(Feature.link, { requires: `${PrerequisitesB} + ${LinkPrerequisite}` }), async ({ main, llm, opened }) => {
  test.setTimeout(90_000);
  const question = "дай ссылку на пару по матанализу";
  const answer = `Ссылка на пару по матанализу: ${CallLinkUrl}`;

  await step("Hub: модель вызывает list_subjects и отвечает ссылкой из результата", async () => {
    await llm.script([callRule(question, "list_subjects", {}), replyRule(AfterSubjects, answer)]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  await step("вопрос «дай ссылку на пару по матанализу» → ответ модели", async () => {
    await ask(main, question);
    await waitThread(main, "ответ модели должен появиться", (x) => x.replies.length === 1 && x.think === 0, { timeout: 45_000 });
    await snapshot("Агент: ссылка на пару");
  });

  const planner = await plannerRequests(llm);
  await step("результат list_subjects: call_link только у «Матанализ», у остальных предметов ссылки нет", async () => {
    expect(planner.length, "модель получила два запроса: вопрос и результат list_subjects").toBe(2);
    expectCatalogV9(planner);
    expect(lastStudentText(planner[0] as NonNullable<(typeof planner)[0]>)).toBe(question);
    const rows = await subjectRows(planner);
    note(`предметы в результате: ${rows.map((r) => `${r.name}${r.call_link ? " (есть call_link)" : ""}`).join(", ")}`);
    for (const name of Object.values(WeekSubjects)) {
      expect(rows.some((r) => r.name === name), `в результате list_subjects должен быть предмет «${name}»; есть: ${rows.map((r) => r.name).join(", ")}`).toBe(true);
    }
    const withLink = rows.filter((r) => r.call_link !== undefined && r.call_link !== "");
    expect(withLink.map((r) => r.name), "ссылка должна быть только у «Матанализ»").toEqual([WeekSubjects.analysis]);
    expect(withLink[0]?.call_link, "ссылка в результате — ровно сохранённая ссылка на пару").toBe(CallLinkUrl);
  });

  await step("ответ показывает ссылку обычным текстом; ни кнопки, ни ссылки для перехода в нём нет", async () => {
    const x = await waitThread(main, "карточка ответа", (y) => y.replies.length === 1, { timeout: 10_000 });
    expect(x.replies[0], "карточка ответа несёт текст модели дословно").toBe(collapse(answer));
    const cards = await cardsOf(main);
    note(`кнопки ленты: ${cards.controls.join(" | ")}; заголовки карточек: ${cards.captions.join(" | ") || "нет"}`);
    expect(cards.hrefs, "ссылка в ответе — не гиперссылка: приложение ничего не открывает само").toBe(0);
    expect(
      navigationControls(cards),
      "в ответе нет кнопок перехода (в каталоге v9 их делает только offer_button из каталога 10); любая такая кнопка должна вести на экран с «Подключиться» для «Матанализ»",
    ).toEqual([]);
    expect(cards.captions, "у ответа нет карточек с заголовками (план, подтверждение, ответ по материалам)").toEqual([]);
    note("в каталоге v9 у ответа нет кнопок и карточек перехода: проверено, что их нет; путь к «Подключиться» — тот, что называет ответ («Сегодня»)");
  });

  await step("«Сегодня»: у пары «Матанализ» есть «Подключиться»; нажатие записывает ссылку в opened.jsonl (ничего не открывается)", async () => {
    await openSection(main, "Сегодня");
    const row = main.locator(".today-lesson").filter({ hasText: WeekSubjects.analysis });
    await expect(row, "на «Сегодня» должна быть пара «Матанализ»").toHaveCount(1, { timeout: 20_000 });
    const join = row.getByRole("button", { name: /^Подключиться/ });
    await expect(join, "у пары «Матанализ» должна быть кнопка «Подключиться»").toBeVisible(act);
    await expect(row.locator(".today-host")).toContainText(new URL(CallLinkUrl).host, act);
    const before = opened().length;
    await join.click(act);
    await expect
      .poll(() => opened().slice(before).filter((r) => r.kind === "call").map((r) => r.value), {
        timeout: 15_000,
        message: "«Подключиться» должна записать ссылку на пару в opened.jsonl",
      })
      .toEqual([CallLinkUrl]);
    expect(opened().slice(before).filter((r) => r.kind !== "call"), "кроме ссылки на пару ничего не открывалось").toEqual([]);
    await snapshot("Сегодня: Подключиться записало ссылку");
    note(`«Подключиться» записало в opened.jsonl: ${opened().at(-1)?.kind} ${new URL(CallLinkUrl).host}`);
  });

  await step("страница предмета «Матанализ» (второе место, которое называет подсказка) показывает ссылку", async () => {
    await openSubject(main, WeekSubjects.analysis);
    const block = main.locator("section.calllink");
    await expect(block, "на странице предмета должен быть блок «Ссылка на пару»").toBeVisible(act);
    expect(await textOf(block), "в блоке должен быть хост ссылки").toContain(new URL(CallLinkUrl).host);
  });
});

test(Feature.linkNone, details(Feature.linkNone, { requires: `${PrerequisitesB} + ${LinkPrerequisite}` }), async ({ main, llm }) => {
  test.setTimeout(90_000);
  const question = "дай ссылку на пару по литературе";
  const answer = "Ссылка на пару по литературе не сохранена. Добавить её можно на странице предмета в «Предметы», а в день пары — на карточке пары в «Сегодня».";

  await step("Hub: модель вызывает list_subjects и отвечает, что ссылки нет и где её добавить", async () => {
    await llm.script([callRule(question, "list_subjects", {}), replyRule(AfterSubjects, answer)]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  await step("вопрос «дай ссылку на пару по литературе» → ответ модели", async () => {
    await ask(main, question);
    await waitThread(main, "ответ модели должен появиться", (x) => x.replies.length === 1 && x.think === 0, { timeout: 45_000 });
    await snapshot("Агент: ссылки на пару нет");
  });

  const planner = await plannerRequests(llm);
  await step("результат list_subjects: у «Литература» нет call_link", async () => {
    expect(planner.length, "модель получила два запроса").toBe(2);
    expectCatalogV9(planner);
    const rows = await subjectRows(planner);
    const literature = rows.find((r) => r.name === WeekSubjects.literature);
    expect(literature, "в результате должен быть предмет «Литература»").toBeDefined();
    expect(literature?.call_link, "у предмета без ссылки поле call_link в результате отсутствует (ссылку нельзя выдумать)").toBeUndefined();
    const others = rows.filter((r) => r.name === WeekSubjects.literature || r.name === WeekSubjects.history);
    expect(others.map((r) => r.name).sort(), "в результате оба предмета без ссылки: «Литература» и «История»").toEqual([WeekSubjects.history, WeekSubjects.literature].sort());
    expect(others.map((r) => r.call_link), "ни у «Литература», ни у «История» ссылки нет").toEqual([undefined, undefined]);
  });

  await step("панель не предлагает никуда переходить: ни «Расписание», ни другой кнопки", async () => {
    const cards = await cardsOf(main);
    note(`кнопки ленты: ${cards.controls.join(" | ")}`);
    expect(
      navigationControls(cards),
      "в ответе не должно быть кнопок перехода (владелец уже встретил отправку в «Расписание», где ссылок нет)",
    ).toEqual([]);
    expect(cards.captions, "у ответа нет карточек (план, подтверждение, ответ по материалам, предложение)").toEqual([]);
    expect(cards.hrefs).toBe(0);
    const x = await waitThread(main, "карточка ответа", (y) => y.replies.length === 1, { timeout: 10_000 });
    expect(x.replies[0]).toBe(collapse(answer));
  });

  await step("приложение отправило модели правило о ссылках (TASK-364): стабильные куски system.md v9", () => {
    const first = planner[0] as NonNullable<(typeof planner)[0]>;
    const system = systemOf(first).prompt;
    expect(system.includes(linkRule), `в системном промпте запроса должно быть правило о ссылках: «${linkRule}»`).toBe(true);
    expect(system.includes(scheduleHasNoLinks), `в промпте должно быть: «${scheduleHasNoLinks}»`).toBe(true);
    expect(system.includes(linkWhere), `в промпте должно быть, где лежит ссылка: «${linkWhere}»`).toBe(true);
    note("проверено: в промпте запроса есть правило TASK-364 («No tool saves a link… never name «Расписание»…», «Расписание … holds no links», где лежит ссылка); как модель это формулирует — LIVE-набор (TASK-382)");
  });

  await step("место, которое называет ответ, существует: на странице предмета «Литература» есть блок ссылки и «Добавить ссылку»", async () => {
    await openSubject(main, WeekSubjects.literature);
    const block = main.locator("section.calllink");
    await expect(block, "на странице предмета должен быть блок «Ссылка на пару»").toBeVisible(act);
    const text = await textOf(block);
    expect(text, "у предмета без ссылки блок говорит, что ссылки нет").toContain("Ссылки на пару нет");
    await expect(block.getByRole("button", { name: /^Добавить ссылку/ }), "в блоке должна быть кнопка «Добавить ссылку»").toBeVisible(act);
    await snapshot("Предмет без ссылки: блок «Ссылка на пару»");
  });
});
