// Pack «Запись → конспект → задание», part 2a: the cancel icon of a running recognition and «Расшифровать снова» (TASK-393).
// 02-cancel checks the cancel through the deletion of the recording; this row checks the cancel the row itself offers:
//   - an icon «Отменить расшифровку» in the row's status cell (next to the progress) while the chip says «Расшифровка» (or «В очереди»);
//   - the first click asks «Отменить расшифровку?» («Не отменять» does nothing; «Да, отменить» + «Больше не спрашивать» cancels
//     and stores the choice in the webview's localStorage, which lives under the wiped control dir, so a run starts with the dialog);
//   - the next cancel runs without the dialog;
//   - after a cancel the row stays in the active list with the quiet chip «Расшифровка отменена» and the word button
//     «Расшифровать снова», which queues a new recognition; the third one runs to the end: «Конспект готов», three Hub calls.
// The file name puts it after 02-cancel and before 03-restart/04-privacy: 04 scans the requests kept by every earlier test.
import { readFileSync } from "node:fs";
import path from "node:path";

import type { Locator, Page } from "@playwright/test";

import { lectureRules } from "../../scripts/llm/recording/rules.js";
import { EnvCtlDir } from "../../src/contract.js";
import { ClipRel, expectedWords, foundWords } from "../../src/fixtures/audio.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { importRecording, openLesson, writeVariant } from "./driver.js";
import {
  act,
  chipOf,
  details,
  Feature,
  keepRequests,
  type Lecture,
  lectureOf,
  logLines,
  note,
  rowOf,
  showRow,
  statusWord,
  textOf,
  waitWhisper,
  whisperNow,
} from "./support.js";

const Label = "e2e-retranscribe";
const CancelName = "Отменить расшифровку";
const AgainName = "Расшифровать снова";
const Cancelled = "Расшифровка отменена";

const cancelIcon = (row: Locator): Locator => row.getByRole("button", { name: CancelName, exact: true });
const againButton = (row: Locator): Locator => row.getByRole("button", { name: AgainName, exact: true });

/** Lines of desktop.log since `from` that say a job of type stt ended as cancelled. */
const cancelledStt = (from: number): string[] =>
  logLines(/./)
    .slice(from)
    .filter((l) => /job attempt finished/.test(l) && /outcome=cancelled/.test(l) && /\btype="?stt"?(\s|$)/.test(l));

/**
 * Polls the chip of the row until its status word is `want`. The row is looked for in the ACTIVE list only (`rowOf`), unless
 * `folded` allows unfolding «Завершённые» (the finished chain). «Ошибка» ends the wait at once; `noDialog` fails the wait when
 * any dialog shows (a cancel after «Больше не спрашивать» must not ask).
 */
async function waitChip(main: Page, lecture: Lecture, want: string, timeout: number, message: string, opts: { folded?: boolean; noDialog?: boolean } = {}): Promise<string[]> {
  const seen: string[] = [];
  await expect
    .poll(
      async () => {
        if (opts.noDialog && (await main.getByRole("dialog").count()) > 0) {
          throw new Error("после «Больше не спрашивать» отмена не должна открывать вопрос, а он открылся");
        }
        let word = "";
        try {
          const row = opts.folded ? await showRow(main, lecture.title) : rowOf(main, lecture.title);
          word = statusWord(await chipOf(row));
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
      { timeout, intervals: [300, 500], message },
    )
    .toBe(want);
  return seen;
}

/** whisper-cli.exe of the sandbox is running and the row says «Расшифровка» (not yet «Расшифровка отменена»). */
async function waitRecognising(main: Page, lecture: Lecture, what: string) {
  const proc = await waitWhisper(90_000, `whisper-cli.exe из папки приложения не появился (${what})`);
  await waitChip(main, lecture, "Расшифровка", 30_000, `строка записи должна показать «Расшифровка» (${what})`);
  return proc;
}

/**
 * The row is in the list of active rows «Идут»: visible and NOT inside a `.clist` (the CollapsibleList group «Завершённые»,
 * folded or unfolded: an earlier test may have left it unfolded). A row the student cancelled in this session stays in «Идут»
 * and folds only after an app restart.
 */
async function expectActiveRow(main: Page, lecture: Lecture, what: string): Promise<Locator> {
  const row = rowOf(main, lecture.title);
  await expect(row, `${what}: строка записи должна остаться в основном списке «Записей»`).toHaveCount(1, act);
  await expect(row).toBeVisible(act);
  const finished = main.locator(".page.recordings .clist").filter({ has: row });
  expect(await finished.count(), `${what}: строка не должна лежать в «Завершённые» (раскрытых или свёрнутых)`).toBe(0);
  return row;
}

/** Waits until the chip of the row no longer says «Расшифровка отменена»: the click and the refresh of the page are over. */
async function waitLeftCancelled(main: Page, lecture: Lecture, message: string): Promise<void> {
  await expect
    .poll(
      async () => {
        try {
          const word = statusWord(await chipOf(rowOf(main, lecture.title)));
          return word === Cancelled ? "" : word || "нет строки";
        } catch {
          return ""; // the list is being redrawn: look again
        }
      },
      { timeout: 30_000, intervals: [200, 300], message },
    )
    .not.toBe("");
}

test(Feature.retranscribe, details(Feature.retranscribe, { requires: Feature.import }), async ({ main, pick, llm }) => {
  test.setTimeout(10 * 60_000);
  const ctl = requireEnv(EnvCtlDir);
  const lecture = lectureOf("значок отмены");
  const clip = readFileSync(path.join(ctl, "fixtures", ...ClipRel.split("/")));
  // The speech three times in a row (~150 s of audio, as in 02-cancel): before the first real cancel the test opens a dialog,
  // answers «Не отменять» and opens it again, so the recognition must last well beyond that. Two cancelled runs are stopped
  // early; only the third one runs to the end.
  const pickRel = writeVariant(clip, "retranscribe", Label, 3);

  await step("Hub: три ответа на запросы записи (понадобятся только третьей расшифровке)", async () => {
    await llm.script(lectureRules(lecture));
  });

  await importRecording(main, pick, lecture, pickRel, "retranscribe.mp3");

  const logBefore = logLines(/./).length;
  let cancelledOnce = 0;
  let cancelledTwice = 0;

  const first = await step("расшифровка идёт: «Расшифровка», whisper-cli.exe и значок «Отменить расшифровку» в строке", async () => {
    const proc = await waitRecognising(main, lecture, "после импорта");
    const row = rowOf(main, lecture.title);
    const icon = cancelIcon(row);
    await expect(icon, "в строке с идущей расшифровкой должен быть значок «Отменить расшифровку»").toBeVisible(act);
    await expect(icon).toBeEnabled(act);
    expect(await againButton(row).count(), "пока расшифровка идёт, «Расшифровать снова» не нужна").toBe(0);
    await snapshot("Значок отмены: расшифровка идёт");
    return proc;
  });

  await step("первый клик по значку открывает вопрос «Отменить расшифровку?»; «Не отменять» ничего не меняет", async () => {
    const row = rowOf(main, lecture.title);
    await cancelIcon(row).click(act);
    const dialog = main.getByRole("dialog");
    await expect(dialog, "первый клик по значку должен открыть вопрос").toBeVisible(act);
    await expect(dialog).toContainText("Отменить расшифровку?");
    await expect(dialog.getByRole("button", { name: "Да, отменить", exact: true })).toBeVisible(act);
    await expect(dialog.getByRole("button", { name: "Не отменять", exact: true })).toBeVisible(act);
    const never = dialog.getByRole("checkbox", { name: "Больше не спрашивать", exact: true });
    await expect(never, "в вопросе должен быть флажок «Больше не спрашивать»").toBeVisible(act);
    await expect(never, "флажок «Больше не спрашивать» по умолчанию снят").not.toBeChecked(act);
    note(`вопрос об отмене: ${await textOf(dialog)}`);
    await snapshot("Значок отмены: вопрос");

    await dialog.getByRole("button", { name: "Не отменять", exact: true }).click(act);
    await expect(dialog, "после «Не отменять» вопрос должен закрыться").toBeHidden(act);

    // nothing was cancelled: the same process still recognises, the row and the icon are as before, the log is silent
    expect((await whisperNow()).map((p) => p.pid), "после «Не отменять» расшифровка (тот же whisper-cli.exe) должна идти дальше").toContain(first.pid);
    expect(statusWord(await chipOf(row)), "строка осталась «Расшифровка»").toBe("Расшифровка");
    await expect(cancelIcon(row), "значок отмены остался в строке").toBeVisible(act);
    expect(cancelledStt(logBefore), "«Не отменять» не должно отменять задание расшифровки").toEqual([]);
    await snapshot("Значок отмены: «Не отменять»");
  });

  await step("второй клик: флажок «Больше не спрашивать» + «Да, отменить» → расшифровка отменена", async () => {
    const row = rowOf(main, lecture.title);
    // A recognition that already finished is not cancelled by this click: the rest of the row would prove nothing.
    expect((await whisperNow()).map((p) => p.pid), `расшифровка (whisper-cli.exe pid ${first.pid}) успела закончиться до отмены: проверять нечего`).toContain(first.pid);
    await cancelIcon(row).click(act);
    const dialog = main.getByRole("dialog");
    await expect(dialog, "вопрос об отмене должен открываться, пока ученик не отключил его").toBeVisible(act);
    const never = dialog.getByRole("checkbox", { name: "Больше не спрашивать", exact: true });
    await never.check(act);
    await expect(never).toBeChecked(act);
    await dialog.getByRole("button", { name: "Да, отменить", exact: true }).click(act);
    await expect(dialog).toBeHidden({ timeout: 30_000 });
  });

  await step("отмена: whisper-cli.exe нет, строка «Расшифровка отменена» с «Расшифровать снова», в журнале outcome=cancelled", async () => {
    await expect
      .poll(async () => (await whisperNow()).map((p) => p.pid), { timeout: 60_000, intervals: [500], message: "после отмены в песочнице не должно остаться whisper-cli.exe" })
      .toEqual([]);
    await waitChip(main, lecture, Cancelled, 30_000, "после отмены строка должна сказать «Расшифровка отменена» (не «Ошибка»)");
    note(`whisper-cli.exe (pid ${first.pid}) исчез после «Да, отменить»`);

    // the active list, without unfolding «Завершённые»
    const row = await expectActiveRow(main, lecture, "после отмены");
    await expect(againButton(row), "у отменённой записи должна быть кнопка «Расшифровать снова»").toBeVisible(act);
    expect(await cancelIcon(row).count(), "у отменённой записи значка отмены уже нет").toBe(0);
    expect(await chipOf(row), "отмена — не ошибка").not.toContain("Ошибка");
    await snapshot("Значок отмены: расшифровка отменена");

    const lines = cancelledStt(logBefore);
    note(`строки журнала об отмене: ${lines.length > 0 ? lines.join(" | ").slice(0, 400) : "нет"}`);
    expect(lines.length, "в desktop.log должна быть строка «job attempt finished … type=stt … outcome=cancelled»").toBeGreaterThan(0);
    cancelledOnce = lines.length;
    expect(await llm.requests(), "отменённая расшифровка не должна ничего отправлять провайдеру").toEqual([]);
  });

  const second = await step("«Расшифровать снова»: новая расшифровка в очереди и идёт", async () => {
    const row = rowOf(main, lecture.title);
    await againButton(row).click(act);
    const proc = await waitRecognising(main, lecture, "после «Расшифровать снова»");
    expect(proc.pid, "это новый процесс whisper-cli.exe, не прежний").not.toBe(first.pid);
    expect(await againButton(rowOf(main, lecture.title)).count(), "пока идёт новая расшифровка, «Расшифровать снова» не показывается").toBe(0);
    await expect(cancelIcon(rowOf(main, lecture.title)), "у новой расшифровки снова есть значок отмены").toBeVisible(act);
    await snapshot("Значок отмены: расшифровка идёт снова");
    return proc;
  });

  await step("вторая отмена идёт без вопроса (выбор «Больше не спрашивать» сохранён)", async () => {
    expect((await whisperNow()).map((p) => p.pid), `вторая расшифровка (pid ${second.pid}) успела закончиться до отмены`).toContain(second.pid);
    await cancelIcon(rowOf(main, lecture.title)).click(act);
    // the dialog must not show at any moment until the cancel is visible in the row
    await waitChip(main, lecture, Cancelled, 30_000, "вторая отмена должна довести строку до «Расшифровка отменена»", { noDialog: true });
    expect(await main.getByRole("dialog").count(), "вопроса об отмене нет").toBe(0);
    await expect
      .poll(async () => (await whisperNow()).map((p) => p.pid), { timeout: 60_000, intervals: [500], message: "после второй отмены не должно остаться whisper-cli.exe" })
      .toEqual([]);

    const row = await expectActiveRow(main, lecture, "после второй отмены");
    await expect(againButton(row)).toBeVisible(act);
    await snapshot("Значок отмены: вторая отмена без вопроса");
    cancelledTwice = cancelledStt(logBefore).length;
    expect(cancelledTwice, "в журнале после второй отмены должно быть больше строк об отмене, чем после первой").toBeGreaterThan(cancelledOnce);
    expect(await llm.requests(), "и вторая отмена ничего не отправила провайдеру").toEqual([]);
  });

  await step("«Расшифровать снова» ещё раз: расшифровка доходит до «Конспект готов»", async () => {
    await againButton(rowOf(main, lecture.title)).click(act);
    // the first polls may still read the old «Расшифровка отменена» (the call and the refresh are not finished): the check
    // below is about what the row shows once the new run is accepted
    await waitLeftCancelled(main, lecture, "после «Расшифровать снова» строка должна выйти из «Расшифровка отменена»");
    const seen = await waitChip(main, lecture, "Конспект готов", 5 * 60_000, "строка записи должна дойти до «Конспект готов»", { folded: true });
    note(`статусы строки после третьего запуска: ${seen.join(" → ")}`);
    expect(seen, "после «Расшифровать снова» строка не должна оставаться в «Расшифровка отменена»").not.toContain(Cancelled);
    await snapshot("Значок отмены: конспект готов");
    expect(cancelledStt(logBefore).length, "третья расшифровка не отменялась: новых строк об отмене нет").toBe(cancelledTwice);
  });

  await step("в Hub ушли три запроса из скрипта; в расшифровке слова речи", async () => {
    await expect
      .poll(async () => (await llm.requests()).length, { timeout: 30_000, intervals: [500], message: "Hub должен был задать провайдеру три вопроса (конспект, задание, текст лекции)" })
      .toBe(3);

    const words = expectedWords(readFileSync(path.join(ctl, "fixtures", "audio", "README.md"), "utf8"));
    await openLesson(main, lecture, "Расшифровка");
    const box = main.getByLabel("Текст расшифровки");
    await expect(box, "на вкладке «Расшифровка» должен быть текст").toBeVisible({ timeout: 30_000 });
    const found = foundWords(await textOf(box), words);
    note(`слова из README после «Расшифровать снова»: ${found.join(", ") || "ничего"}`);
    expect(found.length, `в расшифровке должно быть минимум 2 слова из ${words.join(", ")}`).toBeGreaterThanOrEqual(2);
  });
});

// The fake forgets what it recorded at the next script(): the privacy row of the pack reads the file kept here.
test.afterEach(async ({ llm }) => {
  await keepRequests(llm);
});
