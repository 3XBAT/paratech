// Breadcrumbs in the settings area (TASK-356): the section pages of «Подключения» and the Apple help use them instead of
// «Назад», and the over-lines that only repeat the place are gone. The pages need the Wails bindings to render, so their
// source is checked; the Apple help view and the crumbs themselves are rendered.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Breadcrumbs } from "../.test-build/ui/Breadcrumbs.js";
import { overviewTexts, sectionCrumbs } from "../.test-build/connectionsView.js";
import { helpTexts } from "../.test-build/appleHelpView.js";
import { googleTexts } from "../.test-build/googleView.js";
import { texts as diagnosticsTexts } from "../.test-build/diagnosticsView.js";
import { HelpView } from "../.test-build/pages/AppleCalendarHelpPage.js";

const read = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), "utf8");
const backButton = />\s*Назад\s*</;

test("SectionPage: «Подключения / <section>», the first crumb is the old back callback", () => {
  const page = read("pages/ConnectionsPage.tsx");
  assert.match(page, /import \{ Breadcrumbs \} from "\.\.\/ui\/Breadcrumbs"/);
  assert.match(page, /<Breadcrumbs items=\{\[\{ label: overviewTexts\.title, onClick: props\.onBack \}, \{ label: props\.crumb \}\]\} \/>/);
  // Every section page passes the same `back` (it also puts the focus back on the card), and its crumb.
  assert.equal((page.match(/<SectionPage [^>]*crumb=\{[^}]+\} onBack=\{back\}>/g) ?? []).length, 4);
  assert.match(page, /<SectionPage title=\{googleTexts\.title\} crumb=\{sectionCrumbs\.google\} onBack=\{back\}>/);
  assert.match(page, /const back = \(\) => \{\s*returnTo\.current = open;\s*setOpen\(null\);\s*\};/);
  assert.match(page, /cards\.current\[returnTo\.current\]\?\.focus\(\)/);
  // The title still takes the focus on open.
  assert.match(page, /heading\.current\?\.focus\(\)/);
  assert.equal(overviewTexts.title, "Подключения");
  // The last crumb is the name of the card and the heading.
  assert.deepEqual(sectionCrumbs, { hub: "Конспекты и задания", mail: "Почта", google: "Google Календарь" });
  assert.equal(sectionCrumbs.google, googleTexts.title);
  assert.equal(helpTexts.crumb, "Календарь на iPhone, iPad и Mac");
  assert.equal(helpTexts.crumb, helpTexts.entryTitle, "the crumb is the card title");
});

test("no «Назад» button and no over-line is left in «Подключения» and the Apple help", () => {
  for (const file of ["pages/ConnectionsPage.tsx", "pages/AppleCalendarHelpPage.tsx"]) {
    const src = read(file);
    assert.doesNotMatch(src, backButton, file);
    assert.doesNotMatch(src, /<span className="label"/, file);
    assert.doesNotMatch(src, /\.back\b/, file);
  }
  assert.ok(!("back" in overviewTexts) && !("lead" in overviewTexts));
  assert.ok(!("back" in helpTexts) && !("lead" in helpTexts));
  assert.ok(!read("styles.css").includes(".help-back"), "the style of the bottom button is gone with it");
});

test("Apple help: «Подключения / <card title>» above the title; the first crumb and the bottom button call onBack", () => {
  const calls = [];
  const html = renderToStaticMarkup(h(HelpView, { failure: null, onOpen: () => {}, onBack: () => calls.push("back") }));
  assert.match(html, /<nav class="crumbs" aria-label="Путь">/);
  assert.match(html, /<button type="button" class="crumb link" title="Подключения">Подключения<\/button>/);
  assert.match(html, /<span class="crumb cur" aria-current="page"[^>]*>Календарь на iPhone, iPad и Mac<\/span>/);
  assert.ok(html.indexOf("crumbs") < html.indexOf(helpTexts.title), "the path is above the title");
  assert.ok(!html.includes(">Назад<"));
  assert.ok(!html.includes("Apple Calendar") && !html.includes("Apple Календарь"), "one name: Календарь Apple");
  // The quiet button at the bottom (after the last card) says where it goes, not «Назад».
  assert.equal(helpTexts.backToConnections, "Вернуться к подключениям");
  assert.match(html, /<button[^>]*class="btn ghost"[^>]*>Вернуться к подключениям<\/button>/);
  assert.ok(html.indexOf(helpTexts.backToConnections) > html.indexOf(helpTexts.troubleTitle), "at the bottom");
  // The view passes onBack to the crumbs and to the bottom button as it is.
  const src = read("pages/AppleCalendarHelpPage.tsx");
  assert.match(src, /<Breadcrumbs items=\{\[\{ label: helpTexts\.parentCrumb, onClick: props\.onBack \}, \{ label: helpTexts\.crumb \}\]\} \/>/);
  assert.match(src, /<Button variant="ghost" onClick=\{props\.onBack\}>\s*\{helpTexts\.backToConnections\}/);
  assert.match(src, /import \{ Breadcrumbs \} from "\.\.\/ui\/Breadcrumbs\.js"/);
  assert.equal((src.match(/props\.onBack/g) ?? []).length, 3, "passed down once, used twice");
  // The crumbs render the same two levels in the section pages' shape.
  const crumbs = renderToStaticMarkup(
    h(Breadcrumbs, { items: [{ label: overviewTexts.title, onClick: () => {} }, { label: sectionCrumbs.mail }] }),
  );
  assert.match(crumbs, /Подключения<\/button>.*<span class="crumb cur" aria-current="page" title="Почта">Почта<\/span>/);
});

test("over-lines that only repeat the place are gone; the info-carrying line stays as a muted line", () => {
  const connections = read("pages/ConnectionsPage.tsx");
  assert.doesNotMatch(connections, /overviewTexts\.lead/);
  assert.match(connections, /<div className="head">\s*<div>\s*<h2>\{overviewTexts\.title\}<\/h2>\s*<\/div>\s*<\/div>/);

  const models = read("pages/ModelsPage.tsx");
  assert.doesNotMatch(models, /<span className="label"/);
  assert.ok(!models.includes("Расшифровка работает без интернета"));
  // The same fact is already in the line under the head.
  assert.ok(models.includes("Расшифровка идёт только на этом компьютере. Модель — файл .bin."));

  const diagnostics = read("pages/DiagnosticsPage.tsx");
  assert.doesNotMatch(diagnostics, /texts\.lead/);
  assert.ok(!("lead" in diagnosticsTexts));
  // The label of the source picker is a field label, not an over-line, and stays.
  assert.match(diagnostics, /<span className="label" id=\{labelId\}>/);

  const settings = read("pages/SettingsPage.tsx");
  assert.doesNotMatch(settings, /<span className="label"/);
  assert.ok(!settings.includes("Хранится на этом компьютере"));
  assert.match(settings, /<h2>Профиль<\/h2>\s*<p className="small muted">Для титульного листа задания<\/p>/);

  // The shared head stays as the only title of «Настройки».
  assert.match(read("pages/SettingsTabs.tsx"), /<h1>\{settingsHead\.title\}<\/h1>/);
});
