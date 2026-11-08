// Agent pack, part A, file 1: the plain answer (TASK-380). The model plays one `reply` tool call, streamed after a delay, so the
// panel is seen in every state of a running request: the timer «Nс», the stop control, the text that grows, and at the end the
// copy and retry icons. The 04.11 bug (the sticky «Стоп» row over the reply text) is checked WHILE the reply streams, and a
// missing row is a failure here, not a skip (the layout pack's row «не проверено» is replaced by this one).
import { consoleViolations, layoutSession, watchPage } from "../../src/checks/layout.js";
import { clipboardWrites, expect, snapshot, step, test } from "../../src/steps/index.js";
import { filler, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  ask,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  lastStudentText,
  messagesOf,
  note,
  panelOf,
  plannerRequestsAbout,
  prepare,
  rawLeftovers,
  sample,
  setPanelWidth,
  waitThread,
  act,
} from "./support.js";

const timerShape = /^(\d+с|\d+м \d+с)$/;

test(Feature.reply, details(Feature.reply), async ({ main, llm, opened }) => {
  test.setTimeout(75_000);
  const question = "объясни, что такое предел";
  const answer = filler(520, 0);
  const second = "Повторный ответ тестовой модели: предел — это значение, к которому стремится функция.";

  await step("Hub: ответ модели идёт потоком после задержки; второй ответ — для «Повторить ответ»", async () => {
    await llm.script([
      replyRule(question, answer, { delayMs: 2500, stream: { chunks: 12, chunkDelayMs: 400 } }),
      replyRule(question, second),
    ]);
  });
  watchPage(main);
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));
  consoleViolations(main); // what the app wrote before the scenario is not this scenario's

  const s = layoutSession(main);
  const timers: string[] = [];
  const partials: string[] = [];
  let sawStop = false;
  let checkedBefore = 0;
  let checkedDuring = 0;
  let lastCheckedLength = -1;

  await step("вопрос ученика → таймер, «Остановить», текст растёт; строка не перекрывает текст", async () => {
    await ask(main, question);
    await waitThread(main, "ответ модели должен появиться целиком", (x) => x.replies.length === 1, {
      timeout: 45_000,
      each: async (x) => {
        if (x.think > 0 && x.stopButtons > 0) {
          sawStop = true;
        }
        for (const t of x.elapsed) {
          if (timers.at(-1) !== t) {
            timers.push(t);
          }
        }
        const draft = x.draft[0] ?? "";
        if (draft !== "" && partials.at(-1) !== draft) {
          partials.push(draft);
        }
        // The row is checked on screen before the text starts and at two places of the growing text.
        if (x.think > 0 && draft === "" && checkedBefore < 1) {
          checkedBefore++;
          const r = await s.checkAgent("Помощник · до первого текста");
          expect(r.rowSeen, "до первого текста: строка «Агент отвечает…/Стоп» должна быть видна (rowSeen), иначе проверка пуста").toBe(true);
        } else if (x.think > 0 && draft !== "" && checkedDuring < 3 && draft.length > lastCheckedLength + 30) {
          checkedDuring++;
          lastCheckedLength = draft.length;
          const r = await s.checkAgent(`Помощник · печатается ответ · ${draft.length} зн.`);
          expect(r.rowSeen, "пока ответ печатается: строка «Остановить»/таймер должна быть видна (rowSeen), иначе проверка пуста").toBe(true);
        }
      },
    });
    await snapshot("Агент: ответ готов");
  });

  await step("таймер «Nс» шёл и менялся, а «Остановить» было видно, пока ответ не пришёл", () => {
    note(`таймеры, которые видел ученик: ${timers.join(" → ") || "ни одного"}`);
    expect(sawStop, "пока шёл запрос, в строке «Агент отвечает…» должна быть кнопка «Остановить»").toBe(true);
    expect(timers.length, `таймер должен показать хотя бы два разных значения («1с», «2с»…), увидено: ${timers.join(", ") || "ничего"}`).toBeGreaterThanOrEqual(2);
    for (const t of timers) {
      expect(t, `таймер «${t}» должен быть в формате «5с» или «1м 5с»`).toMatch(timerShape);
    }
  });

  await step("текст ответа рос по частям и только вперёд", () => {
    const lengths = partials.map((p) => p.length);
    note(`длины текста, который видел ученик: ${lengths.join(" → ")}; итог ${collapse(answer).length}`);
    expect(partials.length, `текст должен вырасти хотя бы в два приёма до итога, увидено частей: ${partials.length}`).toBeGreaterThanOrEqual(2);
    for (let i = 1; i < partials.length; i++) {
      expect(partials[i]?.startsWith(partials[i - 1] ?? ""), `часть ${i + 1} ответа должна продолжать часть ${i} (текст не должен перезаписываться): «${partials[i - 1]?.slice(-30)}» → «${partials[i]?.slice(0, 60)}»`).toBe(true);
    }
    expect(partials.at(-1)?.length ?? 0, "последняя часть-черновик должна быть короче итога или равна ему").toBeLessThanOrEqual(collapse(answer).length);
  });

  await step("строка «Стоп» не перекрывает текст (все проверки, rowSeen был всегда)", () => {
    note(`проверок ленты: до текста ${checkedBefore}, во время набора ${checkedDuring}`);
    expect(checkedBefore, "ни разу не удалось проверить ленту до первого текста").toBe(1);
    expect(checkedDuring, "ни разу не удалось проверить ленту, пока текст набирается").toBeGreaterThanOrEqual(1);
    s.assertAll();
  });

  await step("итог: текст равен ответу модели, таймер и строка «Стоп» ушли, есть «Копировать» и «Повторить ответ»", async () => {
    const x = await sample(main);
    expect(x.replies[0], "итоговая карточка должна содержать ответ модели дословно").toBe(collapse(answer));
    expect(x.think, "после ответа строки «Агент отвечает…/Стоп» быть не должно").toBe(0);
    expect(x.stopButtons, "после ответа кнопки «Остановить» быть не должно").toBe(0);
    expect(x.elapsed, "после ответа таймера быть не должно").toEqual([]);
    expect(x.draft, "после ответа черновика быть не должно").toEqual([]);
    expect(x.stopLines, "после обычного ответа строк «остановлено/оборвано/ошибка» быть не должно").toEqual([]);
    expect(x.rerun, "под ответом должна быть иконка «Повторить ответ»").toBe(1);
    expect(rawLeftovers(x.text), "в ленте не должно быть «undefined»/«NaN»/сырых ошибок").toEqual([]);
    const rawPanel = await panelOf(main).innerText(act);
    expect(rawLeftovers(rawPanel), "в панели не должно быть «undefined»/«NaN»/сырых ошибок").toEqual([]);
    expect(x.users).toEqual([question]);
  });

  await step("«Копировать» кладёт в буфер ответ и вопрос (запись clipboard в opened.jsonl или в navigator.clipboard страницы)", async () => {
    // The panel copies through the shell (TranscriptService.CopyText -> the E2E hook -> opened.jsonl); the page's own
    // navigator.clipboard stub (clipboardWrites) is read too, so a change of the mechanism is not mistaken for a missing copy.
    const copied = async (since: { shell: number; page: number }): Promise<string[]> => [
      ...opened().slice(since.shell).filter((r) => r.kind === "clipboard").map((r) => r.value),
      ...(await clipboardWrites(main)).slice(since.page),
    ];
    const mark = async () => ({ shell: opened().length, page: (await clipboardWrites(main)).length });
    let since = await mark();
    await panelOf(main).locator(".ag-acts-agent").getByRole("button", { name: "Копировать", exact: true }).click(act);
    await expect.poll(() => copied(since), { timeout: 10_000, message: "приложение должно записать текст ответа в буфер" }).toEqual([collapse(answer)]);
    since = await mark();
    await panelOf(main).locator(".ag-acts-user").getByRole("button", { name: "Копировать", exact: true }).click(act);
    await expect.poll(() => copied(since), { timeout: 10_000, message: "приложение должно записать текст вопроса в буфер" }).toEqual([question]);
  });

  await step("«Повторить ответ» задаёт тот же вопрос заново и показывает новый ответ", async () => {
    await panelOf(main).locator(".ag-acts-agent").getByRole("button", { name: "Повторить ответ", exact: true }).click(act);
    const x = await waitThread(main, "после «Повторить ответ» должен появиться второй ответ модели", (y) => y.replies.includes(second), { timeout: 30_000 });
    expect(x.users.filter((u) => u === question).length, "вопрос ученика должен остаться в ленте, повтор не должен его терять").toBeGreaterThanOrEqual(1);
    await snapshot("Агент: повторный ответ");
  });

  await step("что приложение послало модели: текст вопроса последним, каталог инструментов v9, потоковый запрос", async () => {
    const asked = await plannerRequestsAbout(llm, question);
    note(`запросов к модели с этим вопросом: ${asked.length}`);
    expect(asked.length, "на вопрос и на «Повторить ответ» модель должна получить ровно два запроса").toBe(2);
    expectCatalogV9(asked);
    const first = asked[0];
    expect(first && lastStudentText(first), "последний блок последнего сообщения ученика должен быть текстом вопроса").toBe(question);
    expect(messagesOf(first as NonNullable<typeof first>).at(-1)?.role, "последнее сообщение запроса должно быть сообщением ученика").toBe("user");
    expect((first?.body as { stream?: unknown } | undefined)?.stream, "Hub должен просить у провайдера потоковый ответ").toBe(true);
    note("проверено: вопрос — последний блок запроса, 27 инструментов каталога v9, stream=true; 2 запроса (вопрос и «Повторить ответ»)");
  });

  await step("консоль окна без ошибок за сценарий", () => {
    const violations = consoleViolations(main);
    expect(violations.map((v) => `${v.element}: ${v.detail}`), "в консоли окна не должно быть ошибок").toEqual([]);
  });
});

test(Feature.widths, details(Feature.widths), async ({ main, llm }) => {
  test.setTimeout(75_000);
  const s = layoutSession(main);
  const runs = [
    { which: "default" as const, label: "обычная панель", question: "что такое производная функции в точке" },
    { which: "widest" as const, label: "самая широкая панель", question: "что такое первообразная функции" },
  ];
  await step("Hub: два медленных ответа потоком", async () => {
    await llm.script(runs.map((r, i) => replyRule(r.question, filler(450, i + 1), { delayMs: 1500, stream: { chunks: 14, chunkDelayMs: 350 } })));
  });
  await step("панель: новый пустой чат", () => prepare(main));

  for (const run of runs) {
    await step(`${run.label}: строка «Стоп» не перекрывает печатающийся ответ`, async () => {
      const width = await setPanelWidth(main, run.which);
      note(`${run.label}: ширина панели ${Math.round(width)} px`);
      await ask(main, run.question);
      let checked = 0;
      await waitThread(main, `${run.label}: ответ модели должен появиться целиком`, (x) => x.replies.length === (run.which === "default" ? 1 : 2), {
        timeout: 45_000,
        each: async (x) => {
          const draft = x.draft[0] ?? "";
          if (x.think > 0 && draft !== "" && checked < 2 && draft.length > checked * 120) {
            checked++;
            const r = await s.checkAgent(`Помощник · ${run.label} · печатается ответ · ${draft.length} зн.`);
            expect(r.rowSeen, `${run.label}: строка «Стоп»/таймер должна быть видна (rowSeen), иначе проверка пуста`).toBe(true);
          }
        },
      });
      expect(checked, `${run.label}: ни разу не удалось проверить ленту, пока текст печатается`).toBeGreaterThanOrEqual(1);
      await snapshot(`Агент: ответ на ${run.label}`);
    });
  }
  await step("ни одного перекрытия на обеих ширинах", async () => {
    await setPanelWidth(main, "default");
    s.assertAll();
  });
});
