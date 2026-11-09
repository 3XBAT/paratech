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
//
// TASK-401: the first row also reads the Hub's key table (quota_idempotency_keys, support.ts agentStepKeys): the automatic retries make
// no `:r<N>` key, «Повторить» makes `K:r1`. The third row is ONE cut then a whole answer: one automatic retry after 5 s under the same key.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { filler, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  act,
  agentStepKeys,
  agentStepRows,
  ask,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  isRetryKey,
  newKeys,
  note,
  panelOf,
  plannerRequestsAbout,
  prepare,
  rawLeftovers,
  sample,
  type Sample,
  waitThread,
} from "./support.js";

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

  // The Hub's key table lives through the whole run: what this row made is what is new after it (key evidence, TASK-401).
  let keysBefore: string[] = [];
  await step("Hub: ключи шагов агента до вопроса", async () => {
    keysBefore = await agentStepKeys();
  });

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

  // Key K of the step is `ag:<thread>:<turn>:<step>`: the automatic retries repeat it and add no `:r<N>`. The Hub keeps the row of the
  // failed key and counts its attempts: the call and 3 retries = 4.
  let failedKeys: string[] = [];
  await step("Hub: после трёх автоповторов ключ шага один, без суффикса «:r», попыток Hub 4", async () => {
    const rows = await agentStepRows();
    const made = rows.filter((r) => !keysBefore.includes(r.key));
    failedKeys = made.map((r) => r.key);
    note(`ключи шага после обрывов: ${made.map((r) => `${r.key} (попыток Hub: ${r.attempts})`).join(", ") || "нет"}`);
    expect(failedKeys.filter(isRetryKey), "автоматические повторы идут под тем же ключом: ключа с «:r<N>» быть не должно (его даёт только «Повторить»)").toEqual([]);
    expect(made.length, "один запрос — ровно один ключ шага на все четыре обращения к модели (строка ключа в таблице Hub есть)").toBe(1);
    expect(made[0]?.attempts, "Hub видел ключ 4 раза: первый вызов и три автоматических повтора").toBe(AutoRetries + 1);
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
    // «Повторить» of a failed step is a NEW key (the Hub replays a failed key for a day): K:r1
    const made = newKeys(keysBefore, await agentStepKeys());
    const retryKeys = made.filter(isRetryKey);
    note(`ключи шага после «Повторить»: ${made.join(", ")}`);
    expect(retryKeys.length, "«Повторить» даёт ровно один новый ключ с суффиксом «:r1»").toBe(1);
    expect(retryKeys[0]?.endsWith(":r1"), "первый «Повторить» — суффикс «:r1»").toBe(true);
    const base = (retryKeys[0] ?? "").replace(/:r1$/, "");
    expect(failedKeys, "ключ обрыва K не изменился: «:r1» — тот же шаг, ключ без суффикса совпадает с ключом обрыва").toEqual([base]);
    expect(retryKeys[0], "ключ «Повторить» отличается от ключа обрыва").not.toBe(failedKeys[0]);
    expect([...made].sort(), "в таблице Hub ровно два ключа шага: K обрыва и K:r1").toEqual([base, `${base}:r1`].sort());
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

// ONE cut, then a whole answer: the app retries once by itself after 5 s and the retry succeeds. The student sees «Повторная попытка 1
// из 3» and then the whole reply: never «Ответ оборван», no retry icon, no banner, the field on throughout. The retry repeats the key of
// the step (no `:r<N>`: that is only «Повторить»), so the Hub's key table gets ONE agent step key for the request, without a suffix.
test(Feature.cutRetry, details(Feature.cutRetry), async ({ main, llm }) => {
  test.setTimeout(90_000);
  const question = "расскажи подробно про сходимость числового ряда";
  const long = filler(720, 2);
  const full = filler(380, 4);

  await step("Hub: длинный ответ потоком обрывается один раз; второй ответ — целый", async () => {
    await llm.script([replyRule(question, long, { stream: cutStream }), replyRule(question, full, { stream: { chunks: 4, chunkDelayMs: 100 } })]);
  });
  await step("панель: новый пустой чат", () => prepare(main));

  let keysBefore: string[] = [];
  await step("Hub: ключи шагов агента до вопроса", async () => {
    keysBefore = await agentStepKeys();
  });

  const attempts = new Set<string>();
  const cutShown: string[] = [];
  const bannerShown: string[] = [];
  let fieldOffAt = 0;
  let samples = 0;
  let final: Sample | undefined;
  await step("вопрос → обрыв → «Повторная попытка 1 из 3» → целый ответ; «Ответ оборван» не показывается ни в одном срезе", async () => {
    await ask(main, question);
    final = await waitThread(main, "после одного обрыва и автоповтора должен появиться целый ответ", (y) => y.replies.length === 1 && y.think === 0, {
      timeout: 60_000,
      each: (y) => {
        samples++;
        y.attemptsAnywhere.forEach((a) => attempts.add(a));
        if (y.text.includes("оборван") || y.stopLines.length > 0) {
          cutShown.push(`строки «${y.stopLines.join(" | ")}»`);
        }
        if (y.offline !== "") {
          bannerShown.push(y.offline);
        }
        if (y.fieldDisabled) {
          fieldOffAt++;
        }
      },
    });
    note(`срезов ленты: ${samples}; попытки, которые видел ученик: ${[...attempts].join(" → ") || "ни одной"}`);
    expect([...attempts], "во время ожидания автоповтора рядом с таймером показано «Повторная попытка 1 из 3», других попыток нет").toEqual([attemptLine(1)]);
    expect(cutShown, `строка «${cutLine}» (и любая строка «не завершено») не должна показываться: повтор удался сам`).toEqual([]);
    expect(bannerShown, "баннера «Нет связи с paratech» не было ни в одном срезе: Hub ответил, обрыв на стороне модели").toEqual([]);
    expect(fieldOffAt, "поле запроса было включено во всех срезах").toBe(0);
    await snapshot("Агент: один обрыв, ответ после автоповтора");
  });

  await step("итог: одна карточка с целым ответом, без иконки «Повторить», без «Повторная попытка», поле включено", async () => {
    const x = await sample(main);
    expect(final?.replies, "ответ — одна карточка").toHaveLength(1);
    expect(x.replies, "одна карточка готового ответа").toEqual([collapse(full)]);
    expect(x.kept, "оставшегося обрывка нет: повтор вытеснил его целым ответом").toEqual([]);
    expect(x.draft, "черновика «печатается» нет").toEqual([]);
    expect(x.stopLines, "строк «Ответ оборван»/«не завершено» нет").toEqual([]);
    expect(x.retry, "иконки «Повторить» нет: повтор не потребовался").toBe(0);
    expect(x.rerun, "под готовым ответом «Повторить ответ»").toBe(1);
    expect(x.think + x.stopButtons + x.elapsed.length, "строки «Стоп» и таймера нет").toBe(0);
    expect(x.attemptsAnywhere, "после ответа «Повторная попытка N из 3» не осталось").toEqual([]);
    expect(x.offline, "баннера «Нет связи с paratech» нет").toBe("");
    expect(x.fieldDisabled, "поле запроса включено").toBe(false);
    expect(rawLeftovers(x.text), "в ленте нет «undefined»/«NaN»/сырых ошибок").toEqual([]);
  });

  await step("у модели ровно два запроса про этот вопрос: обрыв и один автоповтор", async () => {
    const planner = await plannerRequestsAbout(llm, question);
    note(`запросов Hub к модели про вопрос: ${planner.length}`);
    expect(planner.length, "первый вызов и один автоповтор").toBe(2);
    expectCatalogV9(planner);
  });

  await step("Hub: у запроса один ключ шага, без суффикса «:r» (автоповтор — тот же ключ)", async () => {
    const rows = await agentStepRows();
    const made = rows.filter((r) => !keysBefore.includes(r.key));
    note(`новые ключи шага: ${made.map((r) => `${r.key} (попыток Hub: ${r.attempts})`).join(", ") || "нет"}`);
    expect(made.length, "один запрос — один ключ шага (`ag:<чат>:<ход>:<шаг>`): автоповтор взял тот же ключ").toBe(1);
    expect(isRetryKey(made[0]?.key ?? ""), "автоповтор не добавляет «:r<N>» — суффикс даёт только «Повторить» ученика").toBe(false);
    expect(made[0]?.attempts, "Hub видел ключ 2 раза: первый вызов и один автоповтор").toBe(2);
    expect(newKeys(keysBefore, await agentStepKeys()).filter(isRetryKey), "ключей с «:r<N>» в таблице Hub нет").toEqual([]);
    note("проверено: один обрыв → «Повторная попытка 1 из 3» → целый ответ; «Ответ оборван» и иконки «Повторить» не было, баннера нет, поле включено; два запроса к модели под одним ключом шага");
  });
});
