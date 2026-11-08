// Agent pack, part A, file 2: a reply that breaks off in the middle (BUG-20261104, TASK-380). The fake cuts the connection after
// some deltas of a long streamed reply: the text the student has read must stay, the line «Ответ оборван. Можно повторить.» comes
// with ONE retry icon (no second «Повторить»), nothing stays running, and «Повторить» replaces the kept block by the new reply.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { filler, replyRule } from "../../scripts/llm/agent/rules.js";
import { ask, collapse, details, expectCatalogV9, Feature, note, panelOf, plannerRequestsAbout, prepare, rawLeftovers, sample, waitThread, act } from "./support.js";

const cutLine = "Ответ оборван. Можно повторить.";

test(Feature.cut, details(Feature.cut), async ({ main, llm }) => {
  test.setTimeout(75_000);
  const question = "расскажи подробно про непрерывность функции";
  const long = filler(720, 2);
  const full = filler(380, 4);

  await step("Hub: длинный ответ потоком обрывается после части дельт; второй ответ — целый", async () => {
    expect([...long].length, "сценарию нужен ответ не короче 600 знаков").toBeGreaterThanOrEqual(600);
    await llm.script([
      replyRule(question, long, { stream: { chunks: 12, chunkDelayMs: 150, cutAfter: 7 } }),
      replyRule(question, full, { stream: { chunks: 4, chunkDelayMs: 100 } }),
    ]);
  });
  await step("панель: новый пустой чат", () => prepare(main));

  let kept = "";
  await step("вопрос → ответ идёт потоком и обрывается: текст остаётся, внизу строка «Ответ оборван»", async () => {
    await ask(main, question);
    const x = await waitThread(main, `в ленте должна появиться строка «${cutLine}»`, (y) => y.stopLines.some((l) => l.includes("оборван")), { timeout: 45_000 });
    kept = x.kept[0] ?? "";
    note(`прочитанный до обрыва текст: ${kept.length} знаков из ${collapse(long).length}; строка: «${x.stopLines.join(" | ")}»`);
    await snapshot("Агент: ответ оборван");
    expect(kept.length, "текст, напечатанный до обрыва, должен остаться в ленте (блок над строкой)").toBeGreaterThan(80);
    expect(collapse(long).startsWith(kept), `оставшийся текст должен быть началом ответа модели, а не обрывком чужого: «${kept.slice(0, 60)}…»`).toBe(true);
    expect(kept.length, "текст должен быть оборван, то есть короче полного ответа").toBeLessThan(collapse(long).length);
    expect(x.stopLines, `строка под текстом: только «${cutLine}»`).toEqual([cutLine]);
  });

  await step("после обрыва: одна иконка «Повторить», нет второй «Повторить», нет строки «Стоп» и таймера", async () => {
    const x = await sample(main);
    expect(x.retry, "в запросе должна быть ровно одна иконка «Повторить» (из строки обрыва)").toBe(1);
    expect(x.rerun, "«Повторить ответ» под оборванным ответом быть не должно: это был бы второй «Повторить»").toBe(0);
    expect(x.think, "после обрыва строки «Агент отвечает…/Стоп» быть не должно").toBe(0);
    expect(x.stopButtons, "после обрыва кнопки «Остановить» быть не должно").toBe(0);
    expect(x.elapsed, "после обрыва таймера быть не должно").toEqual([]);
    expect(x.draft, "после обрыва черновика-«печатается» быть не должно (текст перешёл в оставшийся блок)").toEqual([]);
    expect(x.replies, "карточки готового ответа быть не должно").toEqual([]);
    expect(rawLeftovers(x.text), "в ленте не должно быть «undefined»/«NaN»/сырых ошибок").toEqual([]);
    const words = await panelOf(main).locator(".ag-thread").getByRole("button", { name: /Повторить/ }).count();
    expect(words, "под оборванным ответом должна быть одна кнопка со словом «Повторить»").toBe(1);
    // Observed, not asserted (no requirement says otherwise): a broken provider stream is mapped to «offline», so the panel shows
    // «Нет связи с paratech» and the field is off until the retry succeeds, though the Hub answered (a note for the owner).
    note(`после обрыва панель: баннер «${x.offline || "нет"}», поле ${x.fieldDisabled ? "выключено" : "включено"}`);
    const planner = await plannerRequestsAbout(llm, question);
    note(`запросов Hub к модели за обрыв: ${planner.length}`);
    expectCatalogV9(planner);
  });

  await step("«Повторить» с целым вторым ответом: оставшийся блок заменён новым текстом", async () => {
    const before = (await plannerRequestsAbout(llm, question)).length;
    await panelOf(main).locator(".ag-thread").getByRole("button", { name: "Повторить", exact: true }).click(act);
    const x = await waitThread(main, "после «Повторить» должен появиться целый второй ответ", (y) => y.replies.length === 1, { timeout: 45_000 });
    expect(x.replies[0], "новая карточка должна содержать второй ответ модели дословно").toBe(collapse(full));
    expect(x.kept, "оставшийся блок со старым обрывком должен исчезнуть").toEqual([]);
    expect(x.stopLines, "строка «Ответ оборван» должна исчезнуть").toEqual([]);
    expect(x.text.includes(kept.slice(0, 60)), "обрывок первого ответа не должен оставаться в ленте рядом с новым").toBe(false);
    expect(x.think + x.stopButtons + x.elapsed.length, "после ответа строки «Стоп» и таймера быть не должно").toBe(0);
    expect(x.rerun, "под готовым ответом снова «Повторить ответ»").toBe(1);
    const after = (await plannerRequestsAbout(llm, question)).length;
    note(`запросов к модели с этим вопросом: до «Повторить» ${before}, после ${after}`);
    expect(after, "«Повторить» должен добавить ровно один запрос к модели").toBe(before + 1);
    note("проверено: после обрыва текст остался, строка «Ответ оборван. Можно повторить.», одна «Повторить», ни «Стоп», ни таймера; «Повторить» заменил блок новым ответом ценой одного запроса к модели");
    await snapshot("Агент: оборванный ответ заменён новым");
  });
});
