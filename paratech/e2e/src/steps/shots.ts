// Screenshots of the app windows: attached to the test under SnapshotAttachment and kept in <run root>/report/screenshots/.
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import type { Page, TestInfo } from "@playwright/test";

import { EnvRunRoot, ReportDirName, SnapshotAttachment } from "../contract.js";
import { slugify, stepSlug } from "./slug.js";
import { withTimeout } from "./util.js";

const shotTimeoutMs = 5_000;
const lookupTimeoutMs = 3_000;

let counter = 0;

/** A file name that does not exist yet in dir: a new worker restarts the counter, a name must never overwrite. */
export function uniqueFile(dir: string, base: string, ext: string): string {
  let name = `${base}${ext}`;
  for (let n = 2; existsSync(path.join(dir, name)); n++) {
    name = `${base}-${n}${ext}`;
  }
  return path.join(dir, name);
}

export function reportDir(env: NodeJS.ProcessEnv = process.env): string | undefined {
  const root = env[EnvRunRoot];
  return root ? path.join(root, ReportDirName) : undefined;
}

/** Screenshots page, writes it to the report folder and attaches it. Throws if the window cannot be captured. */
export async function savePageShot(testInfo: TestInfo, page: Page, name: string, label = ""): Promise<string | undefined> {
  const png = await page.screenshot({ timeout: shotTimeoutMs });
  const dir = reportDir();
  let file: string | undefined;
  if (dir) {
    const shots = path.join(dir, "screenshots");
    mkdirSync(shots, { recursive: true });
    file = uniqueFile(shots, `${stepSlug(name, ++counter)}${label ? `-${slugify(label)}` : ""}`, ".png");
    writeFileSync(file, png);
  }
  if (file) {
    await testInfo.attach(SnapshotAttachment, { path: file, contentType: "image/png" });
  } else {
    await testInfo.attach(SnapshotAttachment, { body: png, contentType: "image/png" });
  }
  return file;
}

/** The pages to capture on failure: main, and the popup when it exists. Never throws, never waits long. */
export async function pagesToCapture(app: { main(): Promise<Page>; popup(t?: number): Promise<Page | undefined> }): Promise<Array<[string, Page]>> {
  const out: Array<[string, Page]> = [];
  try {
    out.push(["main", await withTimeout(app.main(), lookupTimeoutMs, "main page")]);
  } catch {
    // no main page to capture
  }
  try {
    const popup = await withTimeout(app.popup(0), lookupTimeoutMs, "popup page");
    if (popup) {
      out.push(["popup", popup]);
    }
  } catch {
    // no popup to capture
  }
  return out;
}
