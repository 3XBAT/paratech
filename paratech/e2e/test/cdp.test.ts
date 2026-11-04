import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { classifyPageUrl } from "../src/app/cdp.js";

describe("which window a page URL belongs to", () => {
  test("main: index.html or the site root", () => {
    for (const url of ["http://wails.localhost/", "http://wails.localhost", "http://wails.localhost/index.html", "http://wails.localhost/index.html?x=1#/agent", "http://localhost:5173/", "file:///C:/app/index.html"]) {
      assert.equal(classifyPageUrl(url), "main", url);
    }
  });

  test("popup: popup.html", () => {
    for (const url of ["http://wails.localhost/popup.html", "http://wails.localhost/popup.html?lesson=1", "file:///C:/app/popup.html"]) {
      assert.equal(classifyPageUrl(url), "popup", url);
    }
  });

  test("everything else is neither", () => {
    for (const url of ["about:blank", "devtools://devtools/bundled/inspector.html", "chrome-error://chromewebdata/", "http://wails.localhost/assets/app.js", "http://wails.localhost/popup.html.bak", "", "not a url"]) {
      assert.equal(classifyPageUrl(url), "other", url);
    }
  });
});
