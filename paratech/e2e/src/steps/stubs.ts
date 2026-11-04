// SKELETON written by the coordinator: the real file replaces it (TASK-375 part B).
import type { BrowserContext, Page } from "@playwright/test";

/** Page-side stubs for every page of the context, now and later: navigator.clipboard.writeText records, never writes. */
export async function installPageStubs(_context: BrowserContext): Promise<void> {
  throw new Error("installPageStubs: not implemented yet");
}

/** Texts the page tried to put on the clipboard through navigator.clipboard.writeText. */
export async function clipboardWrites(_page: Page): Promise<string[]> {
  throw new Error("clipboardWrites: not implemented yet");
}
