// Pack «Запись → конспект → задание», part 1 (TASK-378; AC-06, AC-08, TASK-025). A recording of a lecture goes the whole
// real way without a restart: «Импортировать запись» + a picked MP3 -> the real whisper-cli of the exe folder with the default
// model -> the transcript -> three scripted Hub calls (summary, homework, lecture text) -> the lesson screen and the assignment.
// Rows: the import with the statuses up to «Конспект готов» and the evidence of the local recognition; the words of the speech
// in «Расшифровка»; the scripted summary in «Конспект»; the assignment with its deadline in «Задания» and «Сегодня».
import { readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";

import { AnnotationSttSpeed, EnvCtlDir } from "../../src/contract.js";
import { ClipBytesPerSecond, ClipRel, expectedWords, foundWords, modelFileInCommand, sttSpeedLine } from "../../src/fixtures/audio.js";
import { dueDayRu, dueMatcher } from "../../src/fixtures/lecture.js";
import { ModelCatalogId } from "../../src/fixtures/model.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { lectureRules } from "../../scripts/llm/recording/rules.js";
import { importRecording, openLesson } from "./driver.js";
import {
  chipOf,
  details,
  Feature,
  keepRequests,
  type Lecture,
  lectureOf,
  logLines,
  note,
  openSection,
  Prerequisites,
  readPack,
  showRow,
  statusWord,
  textOf,
  updatePack,
  watchWhisper,
} from "./support.js";

const ClipPick = `fixtures/${ClipRel}`;

const mainLecture = (): Lecture => {
  const lecture = readPack().main;
  if (!lecture) {
    throw new Error("первая запись пака не импортирована: нет данных в recording-pack.json");
  }
  return lecture;
};

test(Feature.import, details(Feature.import, { requires: Prerequisites }), async ({ main, pick, llm }) => {
  test.setTimeout(5 * 60_000);
  // The lecture of the pack is made here, once, and kept in the pack's file for the other tests.
  const lecture = lectureOf("основная");
  updatePack({ main: lecture });

  await step("Hub: три ответа на запросы записи (конспект, задание, текст лекции)", async () => {
    await llm.script(lectureRules(lecture));
  });

  const watch = watchWhisper();
  const logBefore = logLines(/./).length;
  let model = "";
  const seen: string[] = [];
  let importedAt = 0;
  let doneAt = 0;
  try {
    await importRecording(main, pick, lecture, ClipPick, path.basename(ClipRel));
    importedAt = Date.now();

    await step("строка записи проходит статусы до «Конспект готов» без перезапуска", async () => {
      await expect
        .poll(
          async () => {
            let word = "";
            try {
              word = statusWord(await chipOf(await showRow(main, lecture.title)));
            } catch {
              // the list is being redrawn: look again
            }
            if (word !== "" && seen.at(-1) !== word) {
              seen.push(word);
            }
            if (word === "Ошибка") {
              throw new Error(`цепочка записи закончилась ошибкой; статусы: ${seen.join(" → ")}`);
            }
            return word;
          },
          { timeout: 4 * 60_000, intervals: [500], message: "строка записи должна дойти до «Конспект готов»" },
        )
        .toBe("Конспект готов");
      doneAt = Date.now();
      note(`статусы строки: ${seen.join(" → ")}`);
      await snapshot("Записи: конспект готов");
      expect(seen.length, `строка должна пройти хотя бы два статуса, увидено: ${seen.join(" → ")}`).toBeGreaterThanOrEqual(2);
    });
  } finally {
    const problem = await watch.stop();
    if (problem) {
      note(`наблюдение за whisper-cli.exe прервалось: ${problem}`);
    }
  }

  await step("расшифровку сделал настоящий whisper-cli.exe из папки приложения с моделью по умолчанию", async () => {
    const sightings = [...watch.seen.values()];
    expect(sightings.length, "whisper-cli.exe из папки приложения ни разу не работал, пока шла расшифровка").toBeGreaterThan(0);
    const first = sightings[0];
    note(`whisper-cli.exe: ${first?.path}; параметры: ${first?.commandLine.slice(first.path.length).trim()}`);
    expect(first?.commandLine, `в параметрах whisper-cli.exe должен быть файл модели по умолчанию (${ModelCatalogId})`).toMatch(modelFileInCommand(ModelCatalogId));

    // only the lines written since this import started: an earlier recording's line is not this one's
    const lines = logLines(/./).slice(logBefore).filter((l) => /transcription job enqueued/.test(l));
    expect(lines.length, "в desktop.log после начала импорта нет строки «transcription job enqueued» с моделью").toBeGreaterThan(0);
    const found = /model=("[^"]+"|\S+)/.exec(lines[0] ?? "")?.[1]?.replace(/"/g, "");
    note(`модель расшифровки в desktop.log: ${found ?? "не найдена"}`);
    expect(found, "в строке «transcription job enqueued» должна быть модель").toBeTruthy();
    expect(found, `по умолчанию (после «Модель: по умолчанию») расшифровка идёт моделью ${ModelCatalogId}`).toBe(ModelCatalogId);
    model = found ?? "";
  });

  await step("скорость расшифровки для отчёта", async () => {
    const first = [...watch.seen.values()][0];
    const wall = first ? (first.lastSeen - first.firstSeen) / 1000 : (doneAt - importedAt) / 1000;
    const audioSeconds = Math.round(readFileSync(path.join(requireEnv(EnvCtlDir), "fixtures", ...ClipRel.split("/"))).length / ClipBytesPerSecond);
    const line = sttSpeedLine({ audioSeconds, wallSeconds: Math.max(0.1, Math.round(wall * 10) / 10), model, cpu: os.cpus()[0]?.model.trim() ?? "?" });
    test.info().annotations.push({ type: AnnotationSttSpeed, description: line });
    note(line);
  });
});

// The fake forgets what it recorded at the next script(): the privacy row of the pack reads the file kept here.
test.afterEach(async ({ llm }) => {
  await keepRequests(llm);
});

test(Feature.words, details(Feature.words, { requires: Feature.import }), async ({ main }) => {
  const lecture = mainLecture();
  const words = expectedWords(readFileSync(path.join(requireEnv(EnvCtlDir), "fixtures", "audio", "README.md"), "utf8"));
  expect(words.length, "в fixtures/audio/README.md нет строки «Expected words:» с пятью словами и больше").toBeGreaterThanOrEqual(5);

  await step("экран пары → «Расшифровка»", async () => {
    await openLesson(main, lecture, "Расшифровка");
    await expect(main.getByLabel("Текст расшифровки"), "на вкладке «Расшифровка» должен быть текст").toBeVisible({ timeout: 30_000 });
    await snapshot("Расшифровка");
  });

  await step("в расшифровке есть слова из речи записи", async () => {
    const text = await textOf(main.getByLabel("Текст расшифровки"));
    note(`начало расшифровки: ${text.slice(0, 300)}`);
    const found = foundWords(text, words);
    note(`слова из README: ${words.join(", ")}; найдено: ${found.join(", ") || "ничего"}`);
    expect(found.length, `в расшифровке должно быть минимум 2 слова из ${words.join(", ")}; найдено: ${found.join(", ") || "ничего"}`).toBeGreaterThanOrEqual(2);
    updatePack({ words: found });
  });
});

test(Feature.summary, details(Feature.summary, { requires: Feature.import }), async ({ main }) => {
  const lecture = mainLecture();

  await step("экран пары → «Конспект»", async () => {
    await openLesson(main, lecture, "Конспект");
    await expect(main.getByLabel("Текст конспекта"), "на вкладке «Конспект» должен быть текст").toBeVisible({ timeout: 30_000 });
    await snapshot("Конспект");
  });

  await step("конспект — это ответ Hub из скрипта", async () => {
    const text = await textOf(main.getByLabel("Текст конспекта"));
    expect(text).toContain(lecture.summaryHeading);
    expect(text).toContain(lecture.summaryText);
    expect(text).toContain(lecture.summaryPoint);
    expect(await textOf(main.locator(".sm-summary"))).toContain(lecture.keyPoint);
  });
});

test(Feature.task, details(Feature.task, { requires: Feature.import }), async ({ main }) => {
  const lecture = mainLecture();

  await step("экран пары → «Задания»: найденное задание", async () => {
    await openLesson(main, lecture, "Задания");
    const item = main.locator(".sm-homework .row").filter({ hasText: lecture.assignmentTitle });
    await expect(item, "на вкладке «Задания» пары должно быть задание из ответа Hub").toHaveCount(1, { timeout: 30_000 });
    const text = await textOf(item);
    note(`задание на вкладке пары: ${text}`);
    expect(text, `у задания на вкладке пары должен быть срок «до ${dueDayRu(lecture.due)}, 20:00»`).toMatch(dueMatcher(lecture.due));
  });

  await step("«Задания»: задание есть и у него срок", async () => {
    await openSection(main, "Задания");
    const item = main.getByText(lecture.assignmentTitle).first();
    await expect(item, "в разделе «Задания» должно быть задание из ответа Hub").toBeVisible({ timeout: 30_000 });
    const row = main.locator("[role='listitem']").filter({ hasText: lecture.assignmentTitle }).first();
    const text = await textOf(row);
    note(`задание в «Задания»: ${text}`);
    await snapshot("Задания: задание из записи");
    expect(text, `у задания должен быть срок «до ${dueDayRu(lecture.due)}, 20:00»`).toMatch(dueMatcher(lecture.due, true));
  });

  await step("«Сегодня» → «Сроки на неделе»: то же задание", async () => {
    await openSection(main, "Сегодня");
    const section = main.locator("section.today-sec").filter({ has: main.getByRole("heading", { name: /^Сроки на неделе/ }) });
    await expect(section, "на «Сегодня» должен быть блок «Сроки на неделе»").toBeVisible({ timeout: 20_000 });
    const row = section.locator("[role='listitem']").filter({ hasText: lecture.assignmentTitle });
    await expect(row, "в «Сроки на неделе» должно быть задание из записи").toHaveCount(1, { timeout: 30_000 });
    const text = await textOf(row);
    note(`задание в «Сроки на неделе»: ${text}`);
    expect(text, `у задания в «Сроки на неделе» должен быть срок «до ${dueDayRu(lecture.due)}, 20:00»`).toMatch(dueMatcher(lecture.due, true));
    await snapshot("Сегодня: сроки на неделе");
  });
});
