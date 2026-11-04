// Pack `layout` (TASK-376, ADR-0035 §5): the layout invariants on every screen of the real window, in the states a
// user meets (agent panel closed / open / widest). The window size is a property of the whole run: the second size is a
// second run, `PARATECH_E2E_WINDOW=1024x640` (docs/process/e2e.md). One report row per screen and size.
//
// The first test seeds synthetic data through the UI; the others only look. A screen or tab that this build does not
// have (or that has no data) ends its test as «не проверено (причина)» in the report: a skip, never a green row. A rule
// violation is a failure that lists the screen, the state, the element and the measured values: no rule is relaxed to
// make a screen pass. Every wait has an explicit timeout (support.ts: act).
import type { Page } from "@playwright/test";

import { consoleViolations, formatViolations, layoutSession, type LayoutSession } from "../../src/checks/layout.js";
import { EnvCtlDir } from "../../src/contract.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { assignmentTitles, carry, ownSubjects, seedAssignments, seedOwnSubjects, seedSchedule, seedScheduleSubjects, stage, takeCarried } from "./seed.js";
import {
  act,
  begin,
  details,
  note,
  openSection,
  panelWidth,
  parkMouse,
  SeedFeature,
  sessionHooks,
  setAgentPanel,
  setPanelWidth,
  settle,
  sizeLabel,
  statesOf,
  tabName,
  title,
  unchecked,
  windowSize,
} from "./support.js";

interface Ctx {
  main: Page;
  s: LayoutSession;
  ctl: string;
  /** Something of this screen was not reached (no data, absent in this build): the test ends «не проверено» after the assertions. */
  missing(reason: string): void;
}

/** True when the locator becomes visible within `ms`: for the question «does it exist in this build», never a failure. */
async function appears(locator: ReturnType<Page["locator"]>, ms = 5_000): Promise<boolean> {
  return locator.waitFor({ state: "visible", timeout: ms }).then(
    () => true,
    () => false,
  );
}

/**
 * A screen test: the common frame (console watch, window size note, final assertion, snapshot) around `body`. Violations
 * fail it; parts the body could not reach (`missing`) make it «не проверено» when everything reached was clean.
 */
function screenTest(screen: string, body: (c: Ctx) => Promise<void>): void {
  test(title(screen), details(screen), async ({ main }) => {
    test.setTimeout(180_000);
    const s = await begin(main);
    const notReached: string[] = [];
    await body({ main, s, ctl: requireEnv(EnvCtlDir), missing: (reason) => notReached.push(reason) });
    await step(`раскладка «${screen}»: итог`, () => s.assertAll());
    await snapshot(`${screen} · ${sizeLabel}`);
    if (notReached.length > 0) {
      unchecked(notReached.join("; "));
    }
  });
}

// ---------- data ----------

test(
  SeedFeature,
  details(SeedFeature, { requires: false }),
  async ({ main, pick }) => {
    test.setTimeout(240_000);
    const ctl = requireEnv(EnvCtlDir);
    await begin(main);
    const seeded: string[] = [];

    if (await stage("расписание из файла (ICS с длинными названиями)", () => seedSchedule(main, pick, ctl), main)) {
      seeded.push("расписание");
    }
    if (await stage("предметы из расписания", () => seedScheduleSubjects(main, ctl), main)) {
      seeded.push("предметы из расписания");
    }
    if (await stage("два предмета с длинными названиями", () => seedOwnSubjects(main), main)) {
      seeded.push(`предметы: ${ownSubjects.length}`);
      if (await stage("два задания с длинными названиями", () => seedAssignments(main), main)) {
        seeded.push(`задания: ${assignmentTitles.length}`);
      }
    }
    await openSection(main, "Сегодня");
    // What the seeding printed to the console belongs to the seeding (its own row below), not to the first screen after it.
    carry(ctl, "seed-console", consoleViolations(main));
    note(`засеяно: ${seeded.length > 0 ? seeded.join("; ") : "ничего (экраны проверяются в пустом состоянии)"}`);
  },
);

test(title("Консоль при посеве"), details("Консоль при посеве"), async () => {
  const found = takeCarried(requireEnv(EnvCtlDir), "seed-console");
  if (found === undefined) {
    unchecked("посев не дошёл до конца: консоль за время посева не прочитана");
  }
  await step("консоль за время посева", () => {
    if (found.length > 0) {
      throw new Error(formatViolations("Посев данных", found));
    }
  });
});

// ---------- list screens ----------

screenTest("Сегодня", async ({ main, s }) => {
  await step("Сегодня", async () => {
    await openSection(main, "Сегодня");
    await settle(main, "Сегодня");
    await statesOf(main, s, "Сегодня");
  });
});

screenTest("Записи", async ({ main, s }) => {
  await step("Записи", async () => {
    await openSection(main, "Записи");
    await settle(main, "Записи");
    await statesOf(main, s, "Записи");
  });
});

screenTest("Предметы", async ({ main, s, ctl, missing }) => {
  await step("дерево предметов", async () => {
    await openSection(main, "Предметы");
    await settle(main, "Предметы");
    await statesOf(main, s, "Предметы · дерево");
  });

  const fromSeed = takeCarried(ctl, "schedule-review");
  if (fromSeed === undefined) {
    missing("проверка расписания в «Предметы» (панель «Завести предметы из расписания») не проверена: посев до неё не дошёл");
  } else {
    s.add("Предметы · проверка расписания (во время посева)", fromSeed);
  }

  const subjects = main.locator("nav.tree button[data-subject-id]");
  const count = await subjects.count();
  if (count === 0) {
    missing("предметов нет: страницы предмета и вкладок не проверены");
    return;
  }
  for (const index of [...new Set([0, count - 1])]) {
    await step(`страница предмета ${index + 1} из ${count}`, async () => {
      await subjects.nth(index).click(act);
      await expect(subjects.nth(index)).toHaveAttribute("aria-current", "page", act);
      const tabs = main.getByRole("tablist", { name: "Разделы предмета" }).getByRole("tab");
      await expect(tabs.first()).toBeVisible(act);
      await parkMouse(main);
      const n = await tabs.count();
      for (let i = 0; i < n; i++) {
        const tab = tabs.nth(i);
        const name = await tabName(tab);
        await tab.click(act);
        await expect(tab).toHaveAttribute("aria-selected", "true", act);
        await parkMouse(main);
        await settle(main, `Предмет ${index + 1} · ${name}`);
        await statesOf(main, s, `Предмет ${index + 1} · вкладка «${name}»`, "open");
      }
    });
  }
});

screenTest("Страница пары", async ({ main, s, missing }) => {
  await openSection(main, "Предметы");
  await settle(main, "Предметы");
  const subjects = main.locator("nav.tree button[data-subject-id]");
  const count = Math.min(await subjects.count(), 6);
  const rows = main.locator('ul[aria-label="Пары"] .kb-item');
  let found = false;
  for (let i = 0; i < count && !found; i++) {
    await subjects.nth(i).click(act);
    await expect(subjects.nth(i)).toHaveAttribute("aria-current", "page", act);
    const pairs = main.getByRole("tab", { name: /^Пары/ });
    if ((await pairs.count()) === 0) {
      continue; // this subject has no such tab
    }
    await pairs.click(act);
    await expect(pairs).toHaveAttribute("aria-selected", "true", act);
    await settle(main, "Пары предмета");
    found = (await rows.count()) > 0;
  }
  if (!found) {
    missing("страницы пары нет: в предметах нет ни одной пары с данными (записи, конспекты)");
    return;
  }
  await step("страница пары и её вкладки", async () => {
    await rows.first().click(act);
    const tabs = main.getByRole("tablist", { name: "Раздел пары" }).getByRole("tab");
    await expect(tabs.first()).toBeVisible({ timeout: 15_000 });
    await parkMouse(main);
    const n = await tabs.count();
    for (let i = 0; i < n; i++) {
      const tab = tabs.nth(i);
      const name = await tabName(tab);
      await tab.click(act);
      await expect(tab).toHaveAttribute("aria-selected", "true", act);
      await parkMouse(main);
      await settle(main, `Пара · ${name}`);
      await statesOf(main, s, `Пара · вкладка «${name}»`, "open");
    }
  });
});

screenTest("Задания", async ({ main, s, missing }) => {
  await step("список заданий", async () => {
    await openSection(main, "Задания");
    await settle(main, "Задания");
    await statesOf(main, s, "Задания · список");
  });
  await step("страница задания", async () => {
    // A row is a listitem (ui/controls.tsx Row); it is opened by its text button «Открыть», the row itself is not clickable.
    const list = main.locator('section.hw [role="listitem"]');
    const seeded = list.filter({ hasText: "Проверка раскладки" }).first();
    const row = (await seeded.count()) > 0 ? seeded : list.first();
    if ((await row.count()) === 0) {
      missing("заданий нет: страница задания не проверена");
      return;
    }
    const open = row.getByRole("button", { name: "Открыть", exact: true });
    if ((await open.count()) === 0) {
      missing("в строке задания нет кнопки «Открыть»: страница задания не проверена");
      return;
    }
    await open.click(act);
    await expect(main.locator("#hw-detail-title")).toBeVisible(act);
    await parkMouse(main);
    await settle(main, "Задание");
    await statesOf(main, s, "Задание · страница", "open");
  });
});

screenTest("Расписание", async ({ main, s, ctl, missing }) => {
  await step("Расписание", async () => {
    await openSection(main, "Расписание");
    await settle(main, "Расписание");
    await statesOf(main, s, "Расписание");
  });
  const preview = takeCarried(ctl, "schedule-preview");
  if (preview === undefined) {
    missing("предпросмотр файла расписания не проверен: посев не дошёл до выбора файла");
  } else {
    s.add("Расписание · предпросмотр файла (во время посева)", preview);
  }
});

screenTest("Журнал", async ({ main, s }) => {
  await step("Журнал", async () => {
    await openSection(main, "Журнал");
    await settle(main, "Журнал");
    await statesOf(main, s, "Журнал");
  });
});

// ---------- settings: every item of the left menu ----------

// The items of `settingsPages` (frontend/src/nav.ts) and «Разрешения» (TASK-361, ADR-0034 §4), in menu order.
const settingsItems = ["Основные", "Разрешения", "Подключения", "Профиль", "Оформление", "Модели речи", "Диагностика"];

async function openSettingsItem(main: Page, item: string): Promise<boolean> {
  await openSection(main, "Настройки");
  const button = main.getByRole("navigation", { name: "Разделы настроек" }).getByRole("button", { name: item, exact: true });
  if ((await button.count()) === 0) {
    return false;
  }
  await button.click(act);
  await expect(button).toHaveAttribute("aria-current", "page", act);
  await parkMouse(main);
  await settle(main, `Настройки · ${item}`);
  return true;
}

for (const item of settingsItems) {
  screenTest(`Настройки · ${item}`, async ({ main, s, missing }) => {
    if (!(await openSettingsItem(main, item))) {
      missing(`пункта «${item}» нет в этой сборке`);
      return;
    }
    await statesOf(main, s, `Настройки · ${item}`);
  });
}

screenTest("Настройки · остальные пункты", async ({ main, s }) => {
  await openSection(main, "Настройки");
  const nav = main.getByRole("navigation", { name: "Разделы настроек" });
  await expect(nav).toBeVisible(act);
  const names = (await nav.getByRole("button").allInnerTexts()).map((t) => t.replace(/\s+/g, " ").trim()).filter((t) => t !== "");
  const extra = names.filter((n) => !settingsItems.includes(n));
  if (extra.length === 0) {
    note(`пунктов, которых нет в списке пакета, нет (меню: ${names.join(", ")})`);
    return;
  }
  for (const item of extra) {
    await step(`Настройки · ${item}`, async () => {
      await openSettingsItem(main, item);
      await statesOf(main, s, `Настройки · ${item}`);
    });
  }
});

// ---------- knowledge base and tickets (reached from «Предметы») ----------

screenTest("База знаний", async ({ main, s, missing }) => {
  await openSection(main, "Предметы");
  await settle(main, "Предметы");
  const open = main.locator("nav.tree").getByRole("button", { name: "База знаний", exact: true });
  if (!(await appears(open))) {
    missing("кнопки «База знаний» в «Предметы» нет");
    return;
  }
  await open.click(act);
  await expect(main.locator("nav.tree")).toHaveCount(0, act); // the tree of «Предметы» is gone: the page of the knowledge base is open
  await parkMouse(main);
  await settle(main, "База знаний");
  await statesOf(main, s, "База знаний");
});

screenTest("Билеты", async ({ main, s, missing }) => {
  await openSection(main, "Предметы");
  await settle(main, "Предметы");
  const subjects = main.locator("nav.tree button[data-subject-id]");
  if ((await subjects.count()) === 0) {
    missing("предметов нет, «Билеты к экзамену» недостижимы");
    return;
  }
  await subjects.first().click(act);
  const open = main.getByRole("button", { name: "Билеты к экзамену", exact: true });
  if (!(await appears(open))) {
    missing("кнопки «Билеты к экзамену» на странице предмета нет");
    return;
  }
  await open.click(act);
  await expect(main.locator("nav.tree")).toHaveCount(0, act);
  await parkMouse(main);
  await settle(main, "Билеты");
  await statesOf(main, s, "Билеты");
});

// ---------- the agent panel ----------

screenTest("Помощник", async ({ main, s }) => {
  await step("панель на «Сегодня»: ширина", async () => {
    await openSection(main, "Сегодня");
    await settle(main, "Сегодня");
    await setAgentPanel(main, true);
    const normal = await setPanelWidth(main, "default");
    const sep = main.getByRole("separator", { name: "Ширина панели помощника" });
    const limit = Number(await sep.getAttribute("aria-valuemax", act));
    const widest = await setPanelWidth(main, "widest");
    note(`ширина панели: ${normal} px -> самая широкая ${widest} px (предел окна ${limit} px)`);
    if (limit > normal) {
      expect(widest, "самая широкая панель должна быть шире обычной").toBeGreaterThan(normal);
    } else {
      note("окно настолько узкое, что панель уже предельной ширины: сравнение ширин пропущено");
    }
    expect(Math.abs(widest - limit), "ширина панели должна совпасть с пределом aria-valuemax").toBeLessThanOrEqual(1);
    expect(await panelWidth(main)).toBe(widest);
  });

  // The panel at its widest on top of each list screen: the owner's bugs of 04.11 came from this state.
  for (const screen of ["Сегодня", "Записи", "Предметы", "Задания", "Расписание", "Журнал"]) {
    await step(`${screen}: самая широкая панель`, async () => {
      await openSection(main, screen);
      await settle(main, screen);
      await setPanelWidth(main, "widest");
      await s.check(`${screen} · самая широкая панель`);
    });
  }
  await setPanelWidth(main, "default");
});

// The «Агент отвечает…/Стоп» row exists only while the agent works, and the fake LLM of this pack answers at once, so the
// row is not caught: its own row of the report, «не проверено», instead of a pass that looks like a check. The thread is
// sampled anyway (sticky or fixed elements over the text are found without the row).
screenTest("Помощник · строка «Стоп»", async ({ main, s, missing }) => {
  await step("лента помощника: строка «Стоп» не перекрывает текст", async () => {
    await openSection(main, "Сегодня");
    await settle(main, "Сегодня");
    let rowSeen = false;
    for (const which of ["default", "widest"] as const) {
      await setPanelWidth(main, which);
      const r = await s.checkAgent(`Помощник · лента · ${which === "widest" ? "самая широкая панель" : "обычная панель"}`);
      rowSeen = rowSeen || r.rowSeen;
      if (r.note !== undefined) {
        note(`лента помощника: ${r.note}`);
      }
    }
    await setPanelWidth(main, "default");
    if (!rowSeen) {
      missing("строка «Стоп» не поймана: нужен медленный ответ модели");
    }
  });
});

// ---------- the popup ----------

test(title("Всплывающее окно"), details("Всплывающее окно"), async ({ main, popup }) => {
  test.setTimeout(60_000);
  await begin(main, popup);
  if (popup === undefined) {
    unchecked("страницы всплывающего окна (popup.html) нет в этой сборке");
  }
  const size = await windowSize(popup);
  note(`всплывающее окно: innerWidth x innerHeight = ${size} (свой размер, окно скрыто; снимок не делается)`);
  const s = layoutSession(popup, {}, sessionHooks()); // no screenshots: the window is hidden
  await step("всплывающее окно", async () => {
    await expect(popup.locator("#root")).not.toBeEmpty(act);
    await s.check("Всплывающее окно");
  });
  await step("раскладка «Всплывающее окно»: итог", () => s.assertAll());
});
