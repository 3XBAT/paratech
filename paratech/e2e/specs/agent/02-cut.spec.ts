// Agent pack, part A, file 2: a reply that breaks off in the middle (BUG-20261104, TASK-380, TASK-399). The fake cuts the connection
// after some deltas of a long streamed reply: the Hub answered, so the failure is transient and the app retries the planner step by
// itself, 3 times under the same key after 5, 10 and 20 s (ADR-0026 addendum 2026-11-08, point 1), with the quiet line «Повторная
// попытка N из 3» next to the timer. Only when the retries are spent the request stops `failed`: the text the student has read stays,
// the line «Ответ оборван. Можно повторить.» comes with ONE retry icon (no second «Повторить»), nothing stays running, and
// «Повторить» (a new key) replaces the kept block by the new reply.
//
// The script: a fake rule is ONE-SHOT (consumed by the first request that matches) and the Hub does not retry a stream that has
// already sent deltas (a cut after the 7th delta), so every call of the app takes one rule: the first call and the 3 automatic
// retries take four cut rules, the student's «Повторить» takes the whole reply. With fewer cut rules the app would consume the whole
// reply by itself after 5 s and the scenario would never reach `failed`; the count of planner requests (4, then 5) pins this down.
// The retries add 5 + 10 + 20 s of waiting to each of the two rows (about 40 s to the final line): the timeouts are raised for it.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { filler, replyRule } from "../../scripts/llm/agent/rules.js";
import { ask, collapse, details, expectCatalogV9, Feature, note, panelOf, plannerRequestsAbout, prepare, rawLeftovers, sample, type Sample, waitThread, act } from "./support.js";

const cutLine = "Ответ оборван. Можно повторить.";
const AutoRetries = 3;
const attemptLine = (n: number): string => `Повторная попытка ${n} из ${AutoRetries}`;
// 5 + 10 + 20 s of backoff, four short streams, the Hub round trips: the final line comes after about 40 s.
const RetriesWaitMs = 100_000;

const cutStream = { chunks: 12, chunkDelayMs: 150, cutAfter: 7 };

/** Four cut streams (the call and its 3 automatic retries), then the whole reply for the student's «Повторить». */
function cutScript(question: string, long: string, full: string) {
  const cuts = Array.from({ length: AutoRetries + 1 }, () => replyRule(question, long, { stream: cutStream }));
  return [...cuts, replyRule(question, full, { stream: { chunks: 4, chunkDelayMs: 100 } })];
}

/** Sees every sample on the way to the final line: which attempt lines were shown, in order, and whether the cut line came early. */
function watchRetries() {
  const order: string[] = [];
  const early: string[] = [];
  const each = (s: Sample): void => {
    for (const a of s.attempts) {
      if (!order.includes(a)) {
        order.push(a);
      }
    }
    if (s.stopLines.some((l) => l.includes("оборван")) && !order.includes(attemptLine(AutoRetries))) {
      early.push(`строка «${s.stopLines.join(" | ")}» при показанных попытках [${order.join(", ")}]`);
    }
  };
  return { order, early, each };
}

test(Feature.cut, details(Feature.cut), async ({ main, llm }) => {
  test.setTimeout(180_000);
  const question = "расскажи подробно про непрерывность функции";
  const long = filler(720, 2);
  const full = filler(380, 4);

  await step("Hub: длинный ответ потоком обрывается четыре раза подряд (вызов и 3 автоповтора); пятый ответ — целый", async () => {
    expect([...long].length, "сценарию нужен ответ не короче 600 знаков").toBeGreaterThanOrEqual(600);
    await llm.script(cutScript(question, long, full));
  });
  await step("панель: новый пустой чат", () => prepare(main));

  let kept = "";
  await step("вопрос → ответ обрывается, приложение повторяет само («Повторная попытка 1…3 из 3»), потом текст остаётся и внизу строка «Ответ оборван»", async () => {
    const watch = watchRetries();
    await ask(main, question);
    const x = await waitThread(main, `в ленте должна появиться строка «${cutLine}» после трёх автоматических повторов`, (y) => y.stopLines.some((l) => l.includes("оборван")), {
      timeout: RetriesWaitMs,
      each: watch.each,
    });
    kept = x.kept[0] ?? "";
    note(`попытки, которые видел ученик: ${watch.order.join(" → ") || "ни одной"}`);
    note(`прочитанный до обрыва текст: ${kept.length} знаков из ${collapse(long).length}; строка: «${x.stopLines.join(" | ")}»`);
    expect(watch.order, "во время повторов рядом с таймером должны были показываться «Повторная попытка 1 из 3», «2 из 3», «3 из 3» по порядку").toEqual([1, 2, 3].map(attemptLine));
    expect(watch.early, `строка «${cutLine}» не должна появляться, пока идут автоматические повторы`).toEqual([]);
    await snapshot("Агент: ответ оборван");
    expect(kept.length, "текст, напечатанный до обрыва, должен остаться в ленте (блок над строкой)").toBeGreaterThan(80);
    expect(collapse(long).startsWith(kept), `оставшийся текст должен быть началом ответа модели, а не обрывком чужого: «${kept.slice(0, 60)}…»`).toBe(true);
    expect(kept.length, "текст должен быть оборван, то есть короче полного ответа").toBeLessThan(collapse(long).length);
    expect(x.stopLines, `строка под текстом: только «${cutLine}»`).toEqual([cutLine]);
  });

  await step("после обрыва: одна иконка «Повторить», нет второй «Повторить», нет строки «Стоп», таймера и «Повторная попытка»", async () => {
    const x = await sample(main);
    expect(x.retry, "в запросе должна быть ровно одна иконка «Повторить» (из строки обрыва)").toBe(1);
    expect(x.rerun, "«Повторить ответ» под оборванным ответом быть не должно: это был бы второй «Повторить»").toBe(0);
    expect(x.think, "после обрыва строки «Агент отвечает…/Стоп» быть не должно").toBe(0);
    expect(x.stopButtons, "после обрыва кнопки «Остановить» быть не должно").toBe(0);
    expect(x.elapsed, "после обрыва таймера быть не должно").toEqual([]);
    expect(x.attemptsAnywhere, "после последней попытки «Повторная попытка N из 3» в ленте не должно оставаться").toEqual([]);
    expect(x.draft, "после обрыва черновика-«печатается» быть не должно (текст перешёл в оставшийся блок)").toEqual([]);
    expect(x.replies, "карточки готового ответа быть не должно").toEqual([]);
    expect(rawLeftovers(x.text), "в ленте не должно быть «undefined»/«NaN»/сырых ошибок").toEqual([]);
    const words = await panelOf(main).locator(".ag-thread").getByRole("button", { name: /Повторить/ }).count();
    expect(words, "под оборванным ответом должна быть одна кнопка со словом «Повторить»").toBe(1);
    // The banner and the field after a cut are the row «после оборванного ответа можно писать дальше» below.
    note(`после обрыва панель: баннер «${x.offline || "нет"}», поле ${x.fieldDisabled ? "выключено" : "включено"}`);
    const planner = await plannerRequestsAbout(llm, question);
    note(`запросов Hub к модели за обрыв: ${planner.length}`);
    expect(planner.length, "модель получила 4 запроса: первый вызов и три автоматических повтора (под одним ключом)").toBe(AutoRetries + 1);
    expectCatalogV9(planner);
  });

  await step("«Повторить» с целым пятым ответом: оставшийся блок заменён новым текстом", async () => {
    const before = (await plannerRequestsAbout(llm, question)).length;
    await panelOf(main).locator(".ag-thread").getByRole("button", { name: "Повторить", exact: true }).click(act);
    const x = await waitThread(main, "после «Повторить» должен появиться целый второй ответ", (y) => y.replies.length === 1, { timeout: 45_000 });
    expect(x.replies[0], "новая карточка должна содержать целый ответ модели дословно").toBe(collapse(full));
    expect(x.kept, "оставшийся блок со старым обрывком должен исчезнуть").toEqual([]);
    expect(x.stopLines, "строка «Ответ оборван» должна исчезнуть").toEqual([]);
    expect(x.text.includes(kept.slice(0, 60)), "обрывок первого ответа не должен оставаться в ленте рядом с новым").toBe(false);
    expect(x.think + x.stopButtons + x.elapsed.length, "после ответа строки «Стоп» и таймера быть не должно").toBe(0);
    expect(x.attemptsAnywhere, "после ответа «Повторная попытка N из 3» в ленте не должно быть").toEqual([]);
    expect(x.rerun, "под готовым ответом снова «Повторить ответ»").toBe(1);
    const after = (await plannerRequestsAbout(llm, question)).length;
    note(`запросов к модели с этим вопросом: до «Повторить» ${before}, после ${after}`);
    expect(after, "«Повторить» должен добавить ровно один запрос к модели").toBe(before + 1);
    note("проверено: обрыв повторён приложением трижды («Повторная попытка 1…3 из 3»), после чего текст остался, строка «Ответ оборван. Можно повторить.», одна «Повторить», ни «Стоп», ни таймера; «Повторить» заменил блок новым ответом ценой одного запроса к модели");
    await snapshot("Агент: оборванный ответ заменён новым");
  });
});

// A cut reply is a provider-side break, not a lost connection: the Hub answered, so the student is not offline and must be able to
// write on at once (no dead end until a retry succeeds), also while the app retries by itself (ADR-0026 addendum, point 4: «Нет связи
// с paratech» and a disabled field come only from the connection state). The check «field enabled, no banner» is made on the sample
// after the FINAL line, and the retries are watched for the same two signs on the way.
test(Feature.cutNext, details(Feature.cutNext), async ({ main, llm }) => {
  test.setTimeout(180_000);
  const question = "расскажи подробно про предел функции";
  const long = filler(720, 2);
  const full = filler(380, 4);

  await step("Hub: длинный ответ потоком обрывается четыре раза подряд (вызов и 3 автоповтора); пятый ответ — целый", async () => {
    await llm.script(cutScript(question, long, full));
  });
  await step("панель: новый пустой чат", () => prepare(main));

  const duringRetries: { worst?: Sample } = {}; // the first sample with a banner or a disabled field while the app retried
  await step("вопрос → автоповторы → ответ оборван: строка «Ответ оборван»", async () => {
    const watch = watchRetries();
    await ask(main, question);
    await waitThread(main, `в ленте должна появиться строка «${cutLine}» после трёх автоматических повторов`, (y) => y.stopLines.some((l) => l.includes("оборван")), {
      timeout: RetriesWaitMs,
      each: (y) => {
        watch.each(y);
        if (duringRetries.worst === undefined && (y.offline !== "" || y.fieldDisabled)) {
          duringRetries.worst = y;
        }
      },
    });
    expect(watch.order, "во время повторов должны были показываться «Повторная попытка 1…3 из 3»").toEqual([1, 2, 3].map(attemptLine));
    expect(watch.early, `строка «${cutLine}» не должна появляться, пока идут автоматические повторы`).toEqual([]);
  });

  let mainFailed = true;
  try {
    await step("сразу после обрыва: баннера «Нет связи с paratech» нет, поле запроса включено и принимает текст", async () => {
      // the banner may come a moment after the cut line: look for 1.5 s, keep the worst sample (a poll callback must not throw)
      const from = Date.now();
      let x = duringRetries.worst ?? (await sample(main));
      await waitThread(main, "окно наблюдения после обрыва", (y) => {
        if (y.offline !== "" || y.fieldDisabled) {
          x = y;
        }
        return Date.now() - from > 1_500;
      });
      await snapshot("Агент: сразу после обрыва ответа");
      note(`после обрыва: баннер «${x.offline || "нет"}», поле ${x.fieldDisabled ? "выключено" : "включено"}`);
      // both defects are named in one message; the typing is tried only when none is found
      const wrong: string[] = [];
      if (x.offline !== "") {
        wrong.push(`панель помощника, над полем: ожидалось без баннера, показано «${x.offline}» (Hub ответил, обрыв на стороне модели, ученик не офлайн)`);
      }
      if (x.fieldDisabled) {
        wrong.push("панель помощника, поле запроса (textarea.ag-input): ожидалось включено, оно выключено — ученик не может написать, пока не нажмёт «Повторить» (тупик)");
      }
      expect(wrong, `при автоповторах и после обрыва ответа: ${wrong.join("; ")}`).toEqual([]);
      const field = panelOf(main).locator("textarea.ag-input");
      await field.fill("и ещё вопрос", act);
      await expect(field, "поле должно принять текст после обрыва").toHaveValue("и ещё вопрос", act);
      await expect(panelOf(main).locator("button.ag-send"), "«Отправить» должна включиться, когда в поле есть текст").toBeEnabled(act);
    });
    mainFailed = false;
  } finally {
    // leave the chat clean for the next rows: drop the typed text, then the retry replaces the kept block by the scripted reply
    const cleanup = async (): Promise<void> => {
      await step("уборка: черновик стёрт, «Повторить» заменяет оборванный блок целым ответом", async () => {
        const field = panelOf(main).locator("textarea.ag-input");
        if (!(await field.isDisabled())) {
          await field.fill("", act);
        }
        await panelOf(main).locator(".ag-thread").getByRole("button", { name: "Повторить", exact: true }).click(act);
        const x = await waitThread(main, "после «Повторить» должен появиться целый второй ответ", (y) => y.replies.length === 1 && y.think === 0, { timeout: 45_000 });
        expect(x.replies[0]).toBe(collapse(full));
      });
    };
    if (mainFailed) {
      await cleanup().catch(() => undefined);
    } else {
      await cleanup();
    }
  }
});
