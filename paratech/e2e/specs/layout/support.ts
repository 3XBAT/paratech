// Helpers of the layout pack (TASK-376): report names, navigation like a user, the agent panel, notes. Not a spec.
//
// Every wait here has an explicit timeout (actionMs): the Playwright config has none for actions, so a renamed or missing
// locator would hang until the 240 s test timeout instead of failing (or being skipped) in 10 s.
import type { Locator, Page } from "@playwright/test";

import { AnnotationFeature, AnnotationMode, AnnotationRequires } from "../../src/contract.js";
import { evalInPage, formatCounts, layoutSession, type LayoutSession, type SessionHooks, watchPage } from "../../src/checks/layout.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";

/** The window size is a property of the whole run (PARATECH_E2E_WINDOW), not of a test; a second size is a second run. */
export const windowEnv = (process.env.PARATECH_E2E_WINDOW ?? "").trim();
export const sizeLabel = windowEnv === "" ? "обычное окно" : windowEnv;

export const SeedFeature = "Раскладка · данные для проверки";

/** The timeout of every click, fill, focus, read and expect of the pack. */
export const actionMs = 10_000;
export const act = { timeout: actionMs } as const;

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
 * A sub-check that did not run is a recorded note (an annotation of the test and a line in the run output; the list
 * reporter prints the worker's stdout). A note alone looks green in the report: when a whole screen or tab was not
 * reached, end the test with unchecked() instead.
 */
export function note(text: string): void {
  test.info().annotations.push({ type: "note", description: text });
  console.log(`[layout] ${test.info().title}: ${text}`);
}

/**
 * Ends the test as «не проверено (reason)» in the report: a skip annotation, which the report reads as unchecked. Records
 * the note first. Call it AFTER the assertions of the test: a skip throws, so nothing after it runs.
 */
export function unchecked(reason: string): never {
  note(`не проверено: ${reason}`);
  return test.skip(true, reason) as never;
}

/** One line per checked state: what the probe really examined, so a pass cannot be vacuous. */
function countsLine(name: string, counts: string): void {
  test.info().annotations.push({ type: "note", description: `${name}: ${counts}` });
  console.log(`[layout] ${name}: ${counts}`);
}

/**
 * The hooks of a session on a window: the counts line of every state, and one screenshot of a state that has violations
 * (the report then shows the offending state, not only the last one). `shotPage` undefined = no screenshots (the hidden popup).
 */
export function sessionHooks(shotPage?: Page): SessionHooks {
  const shot = new Set<string>();
  return {
    async onChecked(name, violations, counts) {
      if (counts) {
        countsLine(name, formatCounts(counts));
      }
      if (violations.length > 0 && shotPage !== undefined && !shot.has(name)) {
        shot.add(name);
        await snapshot(`${name} · нарушение`, shotPage);
      }
    },
  };
}

/** The window size as the page sees it, read through CDP (the page CSP blocks Playwright's own evaluate). */
export async function windowSize(page: Page): Promise<string> {
  return evalInPage<string>(page, "() => window.innerWidth + 'x' + window.innerHeight");
}

/** Starts watching the console of the windows, records the real window size, and opens a session on the main window. */
export async function begin(main: Page, popup?: Page): Promise<LayoutSession> {
  watchPage(main);
  if (popup) {
    watchPage(popup);
  }
  const size = await windowSize(main);
  const dpr = await evalInPage<number>(main, "() => window.devicePixelRatio");
  note(`окно ${sizeLabel}: innerWidth x innerHeight = ${size}, devicePixelRatio ${String(dpr)}`);
  await step(`размер окна: ${size} (${sizeLabel})`, () => undefined);
  return layoutSession(main, {}, sessionHooks(main));
}

const railOf = (main: Page) => main.getByRole("navigation", { name: "Разделы", exact: true });

/** The pointer rests in the corner of the window, so no tooltip or hover state is left open under it. */
export async function parkMouse(main: Page): Promise<void> {
  await main.mouse.move(1, 1);
}

/** A click on a rail button (icons only: the name is the aria-label). */
export async function openSection(main: Page, name: string): Promise<void> {
  const button = railOf(main).getByRole("button", { name, exact: true });
  await button.click(act);
  await expect(button).toHaveAttribute("aria-current", "page", act);
  await parkMouse(main);
}

/** Waits on the UI: the sheet is there and «Загрузка…» is gone. A page that stays loading is measured as it is, with a note. */
export async function settle(main: Page, screen: string): Promise<void> {
  const sheet = main.locator("main.sheet");
  await expect(sheet).toBeVisible(act);
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
  await expect(closed).toHaveCount(open ? 0 : 1, act);
}

/** The width of the open panel in CSS px. */
export async function panelWidth(main: Page): Promise<number> {
  const box = await agentPanel(main).boundingBox(act);
  return box === null ? 0 : box.width;
}

// frontend/src/agent/panelWidth.ts: default 340 px, step 16 px, Home = the narrowest (260), End = the widest the window allows.
const defaultPanelWidth = 340;

/** Opens the panel and sets its width with the keyboard on the resize handle (role=separator): End = widest, Home + 5 x ArrowLeft = default. */
export async function setPanelWidth(main: Page, which: "widest" | "default"): Promise<number> {
  await setAgentPanel(main, true);
  const sep = panelSeparator(main);
  const read = async (attr: string) => Number(await sep.getAttribute(attr, act));
  const now = () => read("aria-valuenow");
  const max = () => read("aria-valuemax");
  if (which === "widest") {
    if ((await now()) !== (await max())) {
      await sep.focus(act);
      await main.keyboard.press("End");
    }
    await expect
      .poll(async () => (await now()) === (await max()), { timeout: actionMs, message: "the panel should reach its widest width (aria-valuenow = aria-valuemax)" })
      .toBe(true);
  } else if ((await now()) !== defaultPanelWidth) {
    await sep.focus(act);
    await main.keyboard.press("Home");
    for (let i = 0; i < 5; i++) {
      await main.keyboard.press("ArrowLeft");
    }
    await expect.poll(now, { timeout: actionMs, message: `the panel should be back at ${defaultPanelWidth} px` }).toBe(defaultPanelWidth);
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
  return ((await tab.innerText(act)) ?? "").replace(/\s+/g, " ").trim();
}
