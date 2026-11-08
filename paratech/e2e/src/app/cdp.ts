import { type Browser, type BrowserContext, chromium, type Page } from "@playwright/test";

import { installPageStubs } from "../steps/stubs.js";

export interface AttachedApp {
  /** The current connection; a getter, because reconnect() replaces it. */
  readonly browser: Browser;
  readonly context: BrowserContext;
  /** The main window page (URL index.html or "/"); waits until it exists. */
  main(): Promise<Page>;
  /** The popup page (popup.html) if it exists now or appears within timeoutMs, else undefined. */
  popup(timeoutMs?: number): Promise<Page | undefined>;
  /**
   * Drops the connection and connects again to the same debug port: for an app that was stopped and started again
   * (src/app/restart.ts). Pages and locators taken before are dead; ask main() / popup() again.
   */
  reconnect(): Promise<void>;
  /** Disconnects from the app; the app keeps running. */
  close(): Promise<void>;
}

export type PageKind = "main" | "popup" | "other";

/** Which window of the app a page URL belongs to: `popup.html`, or `index.html` / the site root (the main window). */
export function classifyPageUrl(rawUrl: string): PageKind {
  let url: URL;
  try {
    url = new URL(rawUrl);
  } catch {
    return "other";
  }
  if (url.protocol !== "http:" && url.protocol !== "https:" && url.protocol !== "file:") {
    return "other"; // about:blank, devtools:, chrome-error:
  }
  const file = url.pathname.split("/").pop() ?? "";
  if (file === "popup.html") {
    return "popup";
  }
  if (file === "index.html" || url.pathname === "/" || url.pathname === "") {
    return "main";
  }
  return "other";
}

const defaultAttachTimeoutMs = 30_000;
const waitMainTimeoutMs = 30_000;
const pollMs = 100;

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export interface AttachOptions {
  /** Called once for the context before any page is returned (default: the page stubs of steps/stubs.ts). */
  install?: (context: BrowserContext) => Promise<void>;
  timeoutMs?: number;
}

interface Connection {
  browser: Browser;
  context: BrowserContext;
}

/** One connection: the WebView2 context with its first page, and the page stubs installed. */
async function connect(cdpUrl: string, options: AttachOptions): Promise<Connection> {
  const timeoutMs = options.timeoutMs ?? defaultAttachTimeoutMs;
  const browser = await chromium.connectOverCDP(cdpUrl, { timeout: timeoutMs });
  try {
    // The default context is the WebView2 one (all windows share it); it may exist a moment before its first page.
    const deadline = Date.now() + timeoutMs;
    let context = browser.contexts()[0];
    while (!context || context.pages().length === 0) {
      if (Date.now() > deadline) {
        throw new Error(`CDP ${cdpUrl}: у приложения нет страниц за ${timeoutMs / 1000} с`);
      }
      await sleep(pollMs);
      context = browser.contexts()[0];
    }
    await (options.install ?? installPageStubs)(context);
    return { browser, context };
  } catch (err) {
    await browser.close().catch(() => {});
    throw err;
  }
}

/** Connects to the WebView2 of the app over CDP and installs the page stubs (steps/stubs.ts). */
export async function attachApp(cdpUrl: string, options: AttachOptions = {}): Promise<AttachedApp> {
  let conn = await connect(cdpUrl, options);

  const find = (kind: PageKind): Page | undefined =>
    conn.context.pages().find((p) => !p.isClosed() && classifyPageUrl(p.url()) === kind);
  // A page can be listed while still at about:blank; polling by URL covers pages that appear or navigate later.
  const waitFor = async (kind: PageKind, wait: number): Promise<Page | undefined> => {
    const until = Date.now() + wait;
    for (;;) {
      const page = find(kind);
      if (page || Date.now() >= until) {
        return page;
      }
      await sleep(pollMs);
    }
  };

  return {
    get browser() {
      return conn.browser;
    },
    get context() {
      return conn.context;
    },
    async main() {
      const page = await waitFor("main", waitMainTimeoutMs);
      if (!page) {
        const urls = conn.context.pages().map((p) => p.url());
        throw new Error(`Главная страница (index.html) не найдена за ${waitMainTimeoutMs / 1000} с; страницы: ${urls.join(", ") || "нет"}`);
      }
      return page;
    },
    popup: (wait = 0) => waitFor("popup", wait),
    async reconnect() {
      // The old connection belongs to a process that is gone; closing it may fail, which does not matter.
      await conn.browser.close().catch(() => {});
      conn = await connect(cdpUrl, options);
    },
    // For a browser reached by connectOverCDP this only drops the connection; the app and its windows stay.
    close: () => conn.browser.close(),
  };
}
