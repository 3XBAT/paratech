// Agent pack, part B, file 6: the names of the recordings reach the model (TASK-369, ADR-0022 addendum). The owner asked «какие у меня
// записи?» and the model said it could not look; the fix is that a find_lessons row carries title, subject_name and the recording id.
// SCRIPTED: the model calls find_lessons, and the pack reads what the APP told the model in the next request (the tool_result block),
// then the reply card. The model's own wording is the LIVE set (TASK-382).
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterLessons, callRule, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  ask,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  lastStudentText,
  lectureOf,
  note,
  plannerRequests,
  PrerequisitesB,
  prepare,
  rawLeftovers,
  recordingsOfApp,
  toolResultsOf,
  waitThread,
} from "./support.js";

interface LessonRow {
  recording?: string;
  title?: string;
  subject_name?: string;
  transcribed?: boolean;
  summary?: boolean;
}

test(Feature.titles, details(Feature.titles, { requires: PrerequisitesB }), async ({ main, llm }) => {
  test.setTimeout(75_000);
  const question = "какие у меня записи?";
  const main1 = lectureOf("основная");
  const main2 = lectureOf("перезапуск");
  const gone = lectureOf("отмена");
  const answer = `Записей две: «${main1.title}» и «${main2.title}», обе по предмету «Литература».`;

  await step("Hub: модель вызывает find_lessons без аргументов, потом отвечает", async () => {
    await llm.script([callRule(question, "find_lessons", {}), replyRule(AfterLessons, answer)]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  await step("вопрос «какие у меня записи?» → карточка ответа", async () => {
    await ask(main, question);
    await waitThread(main, "ответ модели должен появиться", (x) => x.replies.length === 1 && x.think === 0, { timeout: 45_000 });
    await snapshot("Агент: какие у меня записи");
  });

  const planner = await plannerRequests(llm);
  const appRows = await recordingsOfApp(main);

  await step("первый запрос к модели несёт вопрос ученика и каталог v9", () => {
    expect(planner.length, "на этот вопрос модель должна получить два запроса: сам вопрос и результат find_lessons").toBe(2);
    expectCatalogV9(planner);
    expect(lastStudentText(planner[0] as NonNullable<(typeof planner)[0]>), "последний блок первого запроса — вопрос ученика").toBe(question);
  });

  await step("результат find_lessons в следующем запросе: названия записей, предмет и id записей", () => {
    const results = toolResultsOf(planner[1] as NonNullable<(typeof planner)[1]>);
    expect(results.length, "во втором запросе должен быть один результат инструмента").toBe(1);
    const result = results[0] as NonNullable<(typeof results)[0]>;
    expect(result.tool, "результат относится к вызову find_lessons").toBe("find_lessons");
    expect(result.isError, `find_lessons не должен вернуть ошибку: ${result.text.slice(0, 200)}`).toBe(false);
    const lessons = ((result.json as { lessons?: LessonRow[] } | undefined)?.lessons ?? []) as LessonRow[];
    note(`строки find_lessons: ${lessons.map((l) => `«${l.title ?? ""}» / ${l.subject_name ?? ""} / ${l.recording ?? "—"}`).join("; ")}`);
    for (const lecture of [main1, main2]) {
      const row = lessons.find((l) => l.title === lecture.title);
      expect(row, `в результате find_lessons должна быть запись «${lecture.title}» (поле title); есть: ${lessons.map((l) => `«${l.title ?? ""}»`).join(", ") || "ни одной строки"}`).toBeDefined();
      expect(row?.subject_name, `у записи «${lecture.title}» в результате должен быть предмет «Литература» (subject_name)`).toBe("Литература");
      const real = appRows.find((r) => r.title === lecture.title);
      expect(real, `приложение должно знать запись «${lecture.title}»`).toBeDefined();
      expect(row?.recording, `id записи «${lecture.title}» в результате должен быть id из списка «Записи»`).toBe(real?.id);
      expect(row?.transcribed, `у записи «${lecture.title}» есть расшифровка`).toBe(true);
      expect(row?.summary, `у записи «${lecture.title}» есть конспект`).toBe(true);
    }
    expect(result.text.includes(gone.title), "удалённая запись «отмена» не должна попадать в результат").toBe(false);
    const withoutName = lessons.filter((l) => l.recording !== undefined && (l.title ?? "") === "" && (l.subject_name ?? "") === "");
    expect(withoutName, "у строки с записью должно быть хотя бы название или предмет").toEqual([]);
  });

  await step("карточка ответа показывает текст модели; id и служебных слов в ней нет", async () => {
    const x = await waitThread(main, "карточка ответа", (y) => y.replies.length === 1, { timeout: 10_000 });
    expect(x.replies[0], "карточка ответа должна нести текст модели дословно").toBe(collapse(answer));
    expect(x.users).toEqual([question]);
    expect(x.stopLines, "после ответа строк «остановлено/ошибка» быть не должно").toEqual([]);
    expect(rawLeftovers(x.text), "в ленте не должно быть «undefined»/«NaN»/сырых ошибок").toEqual([]);
    expect(x.text.includes(appRows[0]?.id ?? "\u0000"), "в ленте не должно быть сырого id записи").toBe(false);
    note("проверено: вопрос — последний блок запроса; каталог v9; find_lessons вернул обе записи пака записи (title, subject_name «Литература», id как в «Записи», transcribed/summary), удалённой записи нет; карточка ответа с текстом модели");
  });
});
