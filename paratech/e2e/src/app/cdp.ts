// SKELETON written by the coordinator: the real file replaces it (TASK-375 part A).
import type { Browser, BrowserContext, Page } from "@playwright/test";

export interface AttachedApp {
  browser: Browser;
  context: BrowserContext;
  /** The main window page (URL index.html or "/"); waits until it exists. */
  main(): Promise<Page>;
  /** The popup page (popup.html) if it exists now or appears within timeoutMs, else undefined. */
  popup(timeoutMs?: number): Promise<Page | undefined>;
  /** Disconnects from the app; the app keeps running. */
  close(): Promise<void>;
}

/** Connects to the WebView2 of the app over CDP and installs the page stubs (steps/stubs.ts). */
export async function attachApp(_cdpUrl: string): Promise<AttachedApp> {
  throw new Error("attachApp: not implemented yet");
}
