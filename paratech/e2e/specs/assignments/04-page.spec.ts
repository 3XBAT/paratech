// Pack «Задания», part 4 (TASK-381; BUG-20261104-e2e-layout item 5). The page of an assignment opens without a console error,
// an unhandled rejection or a failed request. The bug it guards: Wails answers HTTP 422 for every bound method that returns
// an error, and the page's own call `TurnkeyService.WorkOfAssignment` returned one for the normal state «no work yet».
// The console watch is the one of src/checks (the layout pack uses it too); it records console errors, page errors and the
// requests that failed or answered 4xx/5xx. The page keeps asking the core for a newer draft every 3 s (draftPollMs), so the
// row stays on the page until one more request has gone out.
import { drainConsole, watchPage } from "../../src/checks/layout.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { addAssignment, openFromList, openList, pageTitle, removeQuietly } from "./driver.js";
import { act, dayFromNow, details, Feature, note, OnlySubjects, Subject } from "./support.js";

const Title = "Проверить страницу задания по истории";

test(Feature.page, details(Feature.page, { requires: OnlySubjects }), async ({ main }) => {
  test.setTimeout(3 * 60_000);
  watchPage(main);
  let requests = 0;
  const count = () => {
    requests++;
  };
  try {
    await step(`добавить «${Title}»`, async () => {
      await addAssignment(main, { title: Title, subject: Subject, deadline: { date: dayFromNow(3) } });
      await openList(main);
    });

    await step("открыть страницу задания из списка и дождаться, пока она загрузится целиком", async () => {
      drainConsole(main); // what happened before this visit is not blamed on the page
      main.on("request", count);
      await openFromList(main, Title);
      await expect(pageTitle(main)).toHaveText(Title, act);
      await expect(main.getByRole("heading", { name: "Быстрый черновик", level: 2 }), "на странице есть карточка «Быстрый черновик»").toBeVisible(act);
      await expect(main.getByText("Писем по этому заданию ещё не было."), "список писем загрузился").toBeVisible({ timeout: 20_000 });
      await expect(main.getByText(/задаст 1–2 вопроса/), "блок «Сделать под ключ» загрузился").toBeVisible({ timeout: 20_000 });
      await snapshot("Страница задания");
    });

    await step("страница живёт ещё один опрос ядра (draftStatus раз в 3 с) без ошибок", async () => {
      const settled = requests;
      await expect
        .poll(() => requests, { timeout: 20_000, message: "страница должна сделать ещё один запрос к ядру: опрос черновика идёт раз в 3 с" })
        .toBeGreaterThan(settled);
      main.off("request", count);
      const records = drainConsole(main);
      note(`запросов за визит: ${requests}; записей консоли: ${records.length}`);
      expect(
        records.map((r) => `${r.kind}: ${r.text}${r.location ? ` (${r.location})` : ""}`),
        "на странице задания не должно быть ошибок консоли, необработанных отказов и неудачных запросов",
      ).toEqual([]);
    });
  } finally {
    main.off("request", count);
    const left = await removeQuietly(main, [Title]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});
