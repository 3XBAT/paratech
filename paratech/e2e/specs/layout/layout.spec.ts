// Pack `layout` (TASK-376, ADR-0035 §5): the layout invariants on every screen of the real window, in the states a
// user meets (agent panel closed / open / widest). The window size is a property of the whole run: the second size is a
// second run, `PARATECH_E2E_WINDOW=1024x640` (docs/process/e2e.md). One report row per screen and size.
//
// The first test seeds synthetic data through the UI; the others only look. A screen or tab that this build does not
// have is skipped with a recorded note, never a failure. A rule violation is a failure that lists the screen, the state,
// the element and the measured values: no rule is relaxed to make a screen pass.
import type { Page } from "@playwright/test";

import { layoutSession, type LayoutSession } from "../../src/checks/layout.js";
import { EnvCtlDir } from "../../src/contract.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { assignmentTitles, carry, ownSubjects, seedAssignments, seedOwnSubjects, seedSchedule, seedScheduleSubjects, stage, takeCarried } from "./seed.js";
import {
  begin,
  details,
  note,
  openSection,
  panelWidth,
  parkMouse,
  SeedFeature,
  setAgentPanel,
  setPanelWidth,
  settle,
  sizeLabel,
  statesOf,
  tabName,
  title,
} from "./support.js";

interface Ctx {
  main: Page;
  s: LayoutSession;
  ctl: string;
}

/** A screen test: the common frame (console watch, window size note, final assertion, snapshot) around `body`. */
function screenTest(screen: string, body: (c: Ctx) => Promise<void>): void {
  test(title(screen), details(screen), async ({ main }) => {
    test.setTimeout(180_000);
    const s = await begin(main);
    await body({ main, s, ctl: requireEnv(EnvCtlDir) });
    await step(`раскладка «${screen}»: итог`, () => s.assertAll());
    await snapshot(`${screen} · ${sizeLabel}`);
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
    note(`засеяно: ${seeded.length > 0 ? seeded.join("; ") : "ничего (экраны проверяются в пустом состоянии)"}`);
  },
);

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

screenTest("Предметы", async ({ main, s, ctl }) => {
  await step("дерево предметов", async () => {
    await openSection(main, "Предметы");
    await settle(main, "Предметы");
    await statesOf(main, s, "Предметы · дерево");
  });

  const fromSeed = takeCarried(ctl, "schedule-review");
  if (fromSeed === undefined) {
    note("проверка расписания в «Предметы» (панель «Завести предметы из расписания») не проверена: посев до неё не дошёл");
  } else {
    s.add("Предметы · проверка расписания (во время посева)", fromSeed);
  }

  const subjects = main.locator("nav.tree button[data-subject-id]");
  const count = await subjects.count();
  if (count === 0) {
    note("предметов нет: проверено пустое состояние, страницы предмета и вкладок нет");
    return;
  }
  for (const index of [...new Set([0, count - 1])]) {
    await step(`страница предмета ${index + 1} из ${count}`, async () => {
      await subjects.nth(index).click();
      await expect(subjects.nth(index)).toHaveAttribute("aria-current", "page");
      const tabs = main.getByRole("tablist", { name: "Разделы предмета" }).getByRole("tab");
      await expect(tabs.first()).toBeVisible();
      await parkMouse(main);
      const n = await tabs.count();
      for (let i = 0; i < n; i++) {
        const tab = tabs.nth(i);
        const name = await tabName(tab);
        await tab.click();
        await expect(tab).toHaveAttribute("aria-selected", "true");
        await parkMouse(main);
        await settle(main, `Предмет ${index + 1} · ${name}`);
        await statesOf(main, s, `Предмет ${index + 1} · вкладка «${name}»`, "open");
      }
    });
  }
});

screenTest("Страница пары", async ({ main, s }) => {
  await openSection(main, "Предметы");
  await settle(main, "Предметы");
  const subjects = main.locator("nav.tree button[data-subject-id]");
  const count = Math.min(await subjects.count(), 6);
  const rows = main.locator('ul[aria-label="Пары"] .kb-item');
  let found = false;
  for (let i = 0; i < count && !found; i++) {
    await subjects.nth(i).click();
    await expect(subjects.nth(i)).toHaveAttribute("aria-current", "page");
    const pairs = main.getByRole("tab", { name: /^Пары/ });
    await pairs.click();
    await expect(pairs).toHaveAttribute("aria-selected", "true");
    await settle(main, "Пары предмета");
    found = (await rows.count()) > 0;
  }
  if (!found) {
    note("страницы пары нет: в предметах нет ни одной пары с данными (записи, конспекты); экран не проверен");
    return;
  }
  await step("страница пары и её вкладки", async () => {
    await rows.first().click();
    const tabs = main.getByRole("tablist", { name: "Раздел пары" }).getByRole("tab");
    await expect(tabs.first()).toBeVisible({ timeout: 15_000 });
    await parkMouse(main);
    const n = await tabs.count();
    for (let i = 0; i < n; i++) {
      const tab = tabs.nth(i);
      const name = await tabName(tab);
      await tab.click();
      await expect(tab).toHaveAttribute("aria-selected", "true");
      await parkMouse(main);
      await settle(main, `Пара · ${name}`);
      await statesOf(main, s, `Пара · вкладка «${name}»`, "open");
    }
  });
});

screenTest("Задания", async ({ main, s }) => {
  await step("список заданий", async () => {
    await openSection(main, "Задания");
    await settle(main, "Задания");
    await statesOf(main, s, "Задания · список");
  });
  await step("страница задания", async () => {
    const seeded = main.locator("section.hw").getByText(/^Проверка раскладки/).first();
    const row = (await seeded.count()) > 0 ? seeded : main.locator("section.hw button.drow-open").first();
    if ((await row.count()) === 0) {
      note("заданий нет: проверено пустое состояние списка, страницы задания нет");
      return;
    }
    await row.click();
    await expect(main.locator("#hw-detail-title")).toBeVisible({ timeout: 10_000 });
    await parkMouse(main);
    await settle(main, "Задание");
    await statesOf(main, s, "Задание · страница", "open");
  });
});

screenTest("Расписание", async ({ main, s, ctl }) => {
  await step("Расписание", async () => {
    await openSection(main, "Расписание");
    await settle(main, "Расписание");
    await statesOf(main, s, "Расписание");
  });
  const preview = takeCarried(ctl, "schedule-preview");
  if (preview === undefined) {
    note("предпросмотр файла расписания не проверен: посев не дошёл до выбора файла");
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
  await button.click();
  await expect(button).toHaveAttribute("aria-current", "page");
  await parkMouse(main);
  await settle(main, `Настройки · ${item}`);
  return true;
}

for (const item of settingsItems) {
  screenTest(`Настройки · ${item}`, async ({ main, s }) => {
    if (!(await openSettingsItem(main, item))) {
      note(`пункта «${item}» нет в этой сборке: экран пропущен`);
      return;
    }
    await statesOf(main, s, `Настройки · ${item}`);
  });
}

screenTest("Настройки · остальные пункты", async ({ main, s }) => {
  await openSection(main, "Настройки");
  const nav = main.getByRole("navigation", { name: "Разделы настроек" });
  await expect(nav).toBeVisible();
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

screenTest("База знаний", async ({ main, s }) => {
  await openSection(main, "Предметы");
  await settle(main, "Предметы");
  const open = main.locator("nav.tree").getByRole("button", { name: "База знаний", exact: true });
  if ((await open.count()) === 0) {
    note("кнопки «База знаний» в «Предметы» нет: экран не проверен");
    return;
  }
  await open.click();
  await expect(main.locator("nav.tree")).toHaveCount(0); // the tree of «Предметы» is gone: the page of the knowledge base is open
  await parkMouse(main);
  await settle(main, "База знаний");
  await statesOf(main, s, "База знаний");
});

screenTest("Билеты", async ({ main, s }) => {
  await openSection(main, "Предметы");
  await settle(main, "Предметы");
  const subjects = main.locator("nav.tree button[data-subject-id]");
  if ((await subjects.count()) === 0) {
    note("предметов нет, «Билеты к экзамену» недостижимы: экран не проверен");
    return;
  }
  await subjects.first().click();
  const open = main.getByRole("button", { name: "Билеты к экзамену", exact: true });
  await expect(open).toBeVisible();
  await open.click();
  await expect(main.locator("nav.tree")).toHaveCount(0);
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
    const limit = Number(await sep.getAttribute("aria-valuemax"));
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

  await step("лента помощника: строка «Стоп» не перекрывает текст", async () => {
    let rowSeen = false;
    for (const which of ["default", "widest"] as const) {
      await setPanelWidth(main, which);
      const r = await s.checkAgent(`Помощник · лента · ${which === "widest" ? "самая широкая панель" : "обычная панель"}`);
      rowSeen = rowSeen || r.rowSeen;
      if (r.note !== undefined) {
        note(`лента помощника: ${r.note}`);
      }
    }
    if (!rowSeen) {
      note("не проверено: строки «Агент отвечает…/Стоп» нет, пока помощник не работает (запрос к модели в этом пакете не отправляется)");
    }
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

// ---------- the popup ----------

test(title("Всплывающее окно"), details("Всплывающее окно"), async ({ main, popup }) => {
  test.setTimeout(60_000);
  await begin(main, popup);
  if (popup === undefined) {
    note("страницы всплывающего окна (popup.html) нет: экран не проверен");
    return;
  }
  const size = await popup.evaluate("window.innerWidth + 'x' + window.innerHeight");
  note(`всплывающее окно: innerWidth x innerHeight = ${String(size)} (свой размер, окно скрыто; снимок не делается)`);
  const s = layoutSession(popup);
  await step("всплывающее окно", async () => {
    await expect(popup.locator("#root")).not.toBeEmpty();
    await s.check("Всплывающее окно");
  });
  await step("раскладка «Всплывающее окно»: итог", () => s.assertAll());
});
