// Helpers of the layout pack (TASK-376): report names, navigation like a user, the agent panel, notes. Not a spec.
import type { Locator, Page } from "@playwright/test";

import { AnnotationFeature, AnnotationMode, AnnotationRequires } from "../../src/contract.js";
import { layoutSession, type LayoutSession, watchPage } from "../../src/checks/layout.js";
import { expect, step, test } from "../../src/steps/index.js";

/** The window size is a property of the whole run (PARATECH_E2E_WINDOW), not of a test; a second size is a second run. */
export const windowEnv = (process.env.PARATECH_E2E_WINDOW ?? "").trim();
export const sizeLabel = windowEnv === "" ? "обычное окно" : windowEnv;

export const SeedFeature = "Раскладка · данные для проверки";

/** Test options: tag, and the annotations the report reads (one row per screen and size). */
export function details(screen: string, opts: { requires?: boolean } = {}) {
  const feature = screen === SeedFeature ? SeedFeature : `Раскладка · ${screen} · ${sizeLabel}`;
  return {
    tag: "@layout",
    annotation: [
      { type: AnnotationFeature, description: feature },
      { type: AnnotationMode, description: "SCRIPTED" },
      ...(opts.requires === false ? [] : [{ type: AnnotationRequires, description: SeedFeature }]),
    ],
  };
}

export function title(screen: string): string {
  return `${screen} · ${sizeLabel}`;
}

export const firstLine = (err: unknown): string => (err instanceof Error ? err.message : String(err)).split("\n")[0] ?? "";

/**
 * A skipped sub-check or a missing screen is a recorded note, never a failure and never a hidden skip: an annotation of
 * the test and a line in the run output (the list reporter prints the worker's stdout).
 */
export function note(text: string): void {
  test.info().annotations.push({ type: "note", description: text });
  console.log(`[layout] ${test.info().title}: ${text}`);
}

/** Starts watching the console of the windows, records the real window size, and opens a session on the main window. */
export async function begin(main: Page, popup?: Page): Promise<LayoutSession> {
  watchPage(main);
  if (popup) {
    watchPage(popup);
  }
  const size = await main.evaluate("window.innerWidth + 'x' + window.innerHeight");
  const dpr = await main.evaluate("window.devicePixelRatio");
  note(`окно ${sizeLabel}: innerWidth x innerHeight = ${String(size)}, devicePixelRatio ${String(dpr)}`);
  await step(`размер окна: ${String(size)} (${sizeLabel})`, () => undefined);
  return layoutSession(main);
}

const railOf = (main: Page) => main.getByRole("navigation", { name: "Разделы", exact: true });

/** The pointer rests in the corner of the window, so no tooltip or hover state is left open under it. */
export async function parkMouse(main: Page): Promise<void> {
  await main.mouse.move(1, 1);
}

/** A click on a rail button (icons only: the name is the aria-label). */
export async function openSection(main: Page, name: string): Promise<void> {
  const button = railOf(main).getByRole("button", { name, exact: true });
  await button.click();
  await expect(button).toHaveAttribute("aria-current", "page");
  await parkMouse(main);
}

/** Waits on the UI: the sheet is there and «Загрузка…» is gone. A page that stays loading is measured as it is, with a note. */
export async function settle(main: Page, screen: string): Promise<void> {
  const sheet = main.locator("main.sheet");
  await expect(sheet).toBeVisible();
  try {
    await expect.poll(() => sheet.getByText("Загрузка…", { exact: true }).count(), { timeout: 8_000 }).toBe(0);
  } catch {
    note(`${screen}: «Загрузка…» не исчезла за 8 с, раскладка проверена как есть`);
  }
}

// --- the agent panel ---

const panelSeparator = (main: Page) => main.getByRole("separator", { name: "Ширина панели помощника" });
const agentPanel = (main: Page) => main.locator("aside.ag");

/** Ctrl+K opens a closed panel and closes an open one; the shell carries `agent-closed` while it is closed. */
export async function setAgentPanel(main: Page, open: boolean): Promise<void> {
  const closed = main.locator(".shell.agent-closed");
  if ((await closed.count()) === (open ? 0 : 1)) {
    return;
  }
  await main.keyboard.press("Control+K");
  await expect(closed).toHaveCount(open ? 0 : 1);
}

/** The width of the open panel in CSS px. */
export async function panelWidth(main: Page): Promise<number> {
  const box = await agentPanel(main).boundingBox();
  return box === null ? 0 : box.width;
}

// frontend/src/agent/panelWidth.ts: default 340 px, step 16 px, Home = the narrowest (260), End = the widest the window allows.
const defaultPanelWidth = 340;

/** Opens the panel and sets its width with the keyboard on the resize handle (role=separator): End = widest, Home + 5 x ArrowLeft = default. */
export async function setPanelWidth(main: Page, which: "widest" | "default"): Promise<number> {
  await setAgentPanel(main, true);
  const sep = panelSeparator(main);
  const now = async () => Number(await sep.getAttribute("aria-valuenow"));
  const max = async () => Number(await sep.getAttribute("aria-valuemax"));
  if (which === "widest") {
    if ((await now()) !== (await max())) {
      await sep.focus();
      await main.keyboard.press("End");
    }
    await expect.poll(async () => (await now()) === (await max()), { message: "the panel should reach its widest width (aria-valuenow = aria-valuemax)" }).toBe(true);
  } else if ((await now()) !== defaultPanelWidth) {
    await sep.focus();
    await main.keyboard.press("Home");
    for (let i = 0; i < 5; i++) {
      await main.keyboard.press("ArrowLeft");
    }
    await expect.poll(now, { message: `the panel should be back at ${defaultPanelWidth} px` }).toBe(defaultPanelWidth);
  }
  await parkMouse(main);
  return panelWidth(main);
}

/**
 * Checks the layout of the current screen in its panel states: «full» = panel closed, open, widest; «open» = open, widest.
 * The panel is left open at its default width.
 */
export async function statesOf(main: Page, s: LayoutSession, label: string, kind: "full" | "open" = "full"): Promise<void> {
  if (kind === "full") {
    await setAgentPanel(main, false);
    await s.check(`${label} · панель закрыта`);
  }
  await setPanelWidth(main, "default");
  await s.check(`${label} · панель открыта`);
  await setPanelWidth(main, "widest");
  await s.check(`${label} · панель шире всех`);
  await setPanelWidth(main, "default");
}

/** The text of a tab without its count. */
export async function tabName(tab: Locator): Promise<string> {
  return ((await tab.innerText()) ?? "").replace(/\s+/g, " ").trim();
}
