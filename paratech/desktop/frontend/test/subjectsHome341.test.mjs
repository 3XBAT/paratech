// TASK-341: «Предметы» opens the tree and a subject right away. What is shown for a route (explicit subject, «Входящие»,
// "" = no choice yet, no subjects, a subject that is gone), the remembered subject and its storage, the history around
// the "" entry, the first item of the tree («Входящие · N», quiet at 0), and the pieces of the page that are source-checked.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { initialRoute, routeToPage, routeToSubject } from "../.test-build/nav.js";
import { currentRoute, goBack, goForward, newHistory, pushRoute } from "../.test-build/navHistory.js";
import { texts } from "../.test-build/libraryView.js";
import { keepScheduleReview } from "../.test-build/scheduleSubjectsView.js";
import { keepTeacherFocus } from "../.test-build/teachersView.js";
import {
  emptyInboxText,
  firstSubjectId,
  homeSubjectId,
  inboxEntry,
  inboxFiles,
  inboxRoute,
  landingFor,
  lastSubjectKey,
  opensNewPlace,
  readLastSubject,
  rememberSubject,
  inboxFailedText,
  resolveView,
  shownSubjectId,
  showsTiles,
  viewKey,
} from "../.test-build/subjectsHome.js";
import { InboxList, LibraryActions, SubjectDetail, SubjectTree } from "../.test-build/pages/SubjectsParts.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const src = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");

function subject(id, name, semester = "2026-autumn", semesterLabel = "Осень 2026") {
  return { id, name, aliases: [], semester, semesterLabel, folders: [{ id: `${id}-f`, name: "Материалы", system: "materials" }] };
}

// By name inside a semester, as the core sends them; the spring one is older, so the tree puts it last.
const list = [subject("sp", "Алгебра", "2026-spring", "Весна 2026"), subject("b", "Сети"), subject("a", "Физика")];

function fakeStore(initial = {}) {
  const data = new Map(Object.entries(initial));
  return {
    data,
    gets: 0,
    getItem(k) {
      this.gets++;
      return data.has(k) ? data.get(k) : null;
    },
    setItem(k, v) {
      data.set(k, v);
    },
  };
}

// ---- what is shown ----

test("the first subject is the first one of the tree: the latest semester first", () => {
  assert.equal(firstSubjectId(list), "b");
  assert.equal(firstSubjectId([]), "");
});

test("a route resolves to the subject, «Входящие», the remembered or first subject, the tiles or the waiting state", () => {
  assert.deepEqual(resolveView("", null, "a"), { kind: "loading" }, "the list is not read yet");
  assert.deepEqual(resolveView("a", null, ""), { kind: "loading" });
  assert.deepEqual(resolveView("", [], "a"), { kind: "empty" }, "no subjects: the tiles");
  assert.deepEqual(resolveView(inboxRoute, [], ""), { kind: "empty" }, "«Входящие» without subjects is the empty screen with its files");
  assert.deepEqual(resolveView(inboxRoute, list, "a"), { kind: "inbox" });
  assert.deepEqual(resolveView("sp", list, "a"), { kind: "subject", id: "sp" }, "an explicit choice wins over the remembered one");
  assert.deepEqual(resolveView("", list, "a"), { kind: "subject", id: "a" }, "no choice: the remembered subject");
  assert.deepEqual(resolveView("", list, ""), { kind: "subject", id: "b" }, "nothing remembered: the first");
  assert.deepEqual(resolveView("", list, "gone"), { kind: "subject", id: "b" }, "the remembered one is not listed any more: the first");
  // A deleted subject in the route shows what "" shows; nothing is navigated (see the history test below).
  assert.deepEqual(resolveView("gone", list, "a"), { kind: "subject", id: "a" }, "unknown route: the remembered subject");
  assert.deepEqual(resolveView("gone", list, ""), { kind: "subject", id: "b" }, "unknown route, nothing remembered: the first");
  assert.deepEqual(resolveView("gone", [], "a"), { kind: "empty" });
});

test("the tiles are shown only when there are no subjects", () => {
  assert.equal(showsTiles(resolveView("", [], "")), true);
  for (const route of ["", "a", inboxRoute, "gone"]) {
    assert.equal(showsTiles(resolveView(route, list, "a")), false, route);
  }
  assert.equal(showsTiles(resolveView("", null, "")), false, "not while loading");
});

test("the subject on screen, and the key of a view that resets the forms and notes", () => {
  assert.equal(shownSubjectId(resolveView("", list, "a")), "a");
  assert.equal(shownSubjectId(resolveView(inboxRoute, list, "a")), "");
  assert.equal(shownSubjectId(resolveView("", [], "")), "");
  assert.equal(viewKey(resolveView("", list, "a"), ""), "a");
  assert.equal(viewKey(resolveView(inboxRoute, list, "a"), inboxRoute), inboxRoute);
  assert.equal(viewKey(resolveView("x", null, ""), "x"), "x", "loading keeps the route's own id");
  assert.notEqual(viewKey(resolveView("a", list, ""), "a"), viewKey(resolveView("b", list, ""), "b"));
});

test("home: the remembered subject while listed, else the first", () => {
  assert.equal(homeSubjectId(list, "sp"), "sp");
  assert.equal(homeSubjectId(list, "nope"), "b");
  assert.equal(homeSubjectId(list, ""), "b");
  assert.equal(homeSubjectId([], "sp"), "");
});

// ---- the remembered subject ----

test("the last opened subject is read from the storage; missing, invalid and unusable values give nothing", () => {
  assert.equal(readLastSubject(fakeStore({ [lastSubjectKey]: "s7" })), "s7");
  assert.equal(readLastSubject(fakeStore()), "", "missing");
  assert.equal(readLastSubject(fakeStore({ [lastSubjectKey]: "" })), "");
  assert.equal(readLastSubject(fakeStore({ [lastSubjectKey]: inboxRoute })), "", "«Входящие» is not a subject");
  assert.equal(readLastSubject(fakeStore({ [lastSubjectKey]: "x".repeat(500) })), "", "too long to be an id");
  assert.equal(readLastSubject(null), "", "no storage");
  assert.equal(readLastSubject({ getItem: () => { throw new Error("blocked"); }, setItem: noop }), "", "a storage that throws");
  assert.equal(readLastSubject({ getItem: () => 42, setItem: noop }), "", "not a string");
});

test("a subject is remembered whenever it is shown, «Входящие» and the empty id never are, a failing storage is ignored", () => {
  const store = fakeStore();
  rememberSubject(store, "s1");
  assert.equal(store.data.get(lastSubjectKey), "s1");
  rememberSubject(store, "");
  rememberSubject(store, inboxRoute);
  assert.equal(store.data.get(lastSubjectKey), "s1", "the inbox keeps the last subject");
  rememberSubject(store, "s2");
  assert.equal(readLastSubject(store), "s2");
  rememberSubject(null, "s3");
  rememberSubject({ getItem: () => null, setItem: () => { throw new Error("quota"); } }, "s3");
});

test("the remembered id is read once per rail click, not at every render", () => {
  const store = fakeStore({ [lastSubjectKey]: "a" });
  const first = landingFor(null, 0, store);
  assert.deepEqual(first, { visit: 0, id: "a" });
  store.data.set(lastSubjectKey, "b"); // a subject opened later in the same visit
  assert.equal(landingFor(first, 0, store), first, "the same object: nothing is read");
  assert.equal(store.gets, 1);
  assert.deepEqual(landingFor(first, 1, store), { visit: 1, id: "b" }, "a click on the rail lands on the last opened subject");
});

// ---- the route and the history ----

test("a back to the entry without a choice shows the subject it showed, not the one just left", () => {
  const landing = "a"; // read when the section opened
  let hist = newHistory(routeToPage("subjects"));
  assert.deepEqual(resolveView(currentRoute(hist).subject, list, landing), { kind: "subject", id: "a" });
  // The student picks «Сети» (remembered meanwhile), then «Входящие».
  hist = pushRoute(hist, routeToSubject(currentRoute(hist), "b"));
  hist = pushRoute(hist, routeToSubject(currentRoute(hist), inboxRoute));
  assert.deepEqual(resolveView(currentRoute(hist).subject, list, landing), { kind: "inbox" });
  hist = goBack(hist);
  assert.deepEqual(resolveView(currentRoute(hist).subject, list, landing), { kind: "subject", id: "b" });
  hist = goBack(hist);
  assert.equal(currentRoute(hist).subject, "");
  assert.deepEqual(resolveView(currentRoute(hist).subject, list, landing), { kind: "subject", id: "a" });
  assert.deepEqual(resolveView(currentRoute(goForward(hist)).subject, list, landing), { kind: "subject", id: "b" });
});

test("opening the section pushes nothing for the resolved subject: the same route is no new entry", () => {
  const hist = newHistory(initialRoute);
  assert.equal(pushRoute(hist, initialRoute), hist);
  // Selecting the subject that is on screen is not a new place; another subject or the inbox is.
  const view = resolveView("", list, "a");
  assert.equal(opensNewPlace(view, "a"), false);
  assert.equal(opensNewPlace(view, "b"), true);
  assert.equal(opensNewPlace(view, inboxRoute), true);
  const inbox = resolveView(inboxRoute, list, "a");
  assert.equal(opensNewPlace(inbox, inboxRoute), false);
  assert.equal(opensNewPlace(inbox, "a"), true);
});

test("a route to a deleted subject shows the home subject and pushes no history entry, so «Назад» is not trapped", () => {
  let hist = newHistory(routeToPage("subjects"));
  hist = pushRoute(hist, routeToSubject(currentRoute(hist), "gone"));
  assert.deepEqual(resolveView(currentRoute(hist).subject, list, "a"), { kind: "subject", id: "a" });
  // The page no longer redirects: the shown subject is not a new place, a click on it adds nothing, «Назад» works.
  const view = resolveView(currentRoute(hist).subject, list, "a");
  assert.equal(opensNewPlace(view, "a"), false);
  hist = goBack(hist);
  assert.equal(currentRoute(hist).subject, "", "one step back leaves the stale route");
  const page = src("pages/SubjectsPage.tsx");
  assert.doesNotMatch(page, /routeIsGone|onSelect\(""\)/, "no redirect effect");
  assert.doesNotMatch(src("subjectsHome.ts"), /routeIsGone|kind: "unknown"/);
});

test("the sentinel is a route of its own: a subject and the inbox differ, and a subject leaves the one-shot requests alone", () => {
  assert.equal(routeToSubject(initialRoute, inboxRoute).subject, inboxRoute);
  assert.equal(routeToSubject(initialRoute, inboxRoute).page, "subjects");
  assert.equal(list.some((s) => s.id === inboxRoute), false);
  // The review of the schedule opens over what "" shows; a chosen subject or «Входящие» drops the request.
  assert.equal(keepScheduleReview(true, { page: "subjects", subject: "" }), true);
  assert.equal(keepScheduleReview(true, { page: "subjects", subject: inboxRoute }), false);
  // «Указать почту» names a subject explicitly.
  const focus = { subjectId: "a", role: "lecture" };
  assert.equal(keepTeacherFocus(focus, { page: "subjects", subject: "a" }), focus);
  assert.equal(keepTeacherFocus(focus, { page: "subjects", subject: inboxRoute }), null);
});

// ---- the first item of the tree ----

test("«Входящие · N»: the count of files that are not deleted; with none the item stays, quiet and without a number", () => {
  assert.equal(inboxFiles([{ removed: false }, { removed: true }, { removed: false }]), 2);
  assert.equal(inboxFiles([]), 0);
  assert.deepEqual(inboxEntry(3, false), { label: "Входящие", count: "3", quiet: false, current: false, title: "Входящие · 3" });
  assert.deepEqual(inboxEntry(0, true), { label: "Входящие", count: "", quiet: true, current: true, title: "Входящие" });
});

test("«Входящие» whose state is unknown (not read yet, or failed) claims nothing: no number, not quiet", () => {
  assert.deepEqual(inboxEntry(0, false, false), { label: "Входящие", count: "", quiet: false, current: false, title: "Входящие" });
  assert.deepEqual(inboxEntry(5, false, false), { label: "Входящие", count: "", quiet: false, current: false, title: "Входящие" }, "a stale list is not a count");
  assert.equal(inboxEntry(5, false, true).count, "5");
});

function tree(over = {}) {
  return html(
    h(SubjectTree, { subjects: list, currentId: "b", counts: null, busy: false, onOpen: noop, onAddSubject: noop, onAddFolder: noop, inbox: inboxEntry(2, false), onOpenInbox: noop, ...over }),
  );
}

test("the tree starts with «Входящие» (before the first semester) and shows its count", () => {
  const out = tree();
  assert.ok(out.indexOf("Входящие") >= 0);
  assert.ok(out.indexOf("Входящие") < out.indexOf("Осень 2026"), "before the first semester");
  // «has»: files to look at, drawn heavier with the count as a pill (styles.css); the label and the tooltip name the count.
  assert.match(out, /<button[^>]*class="ti inbox has"[^>]*aria-label="Входящие · 2"[^>]*title="Входящие · 2"[^>]*>.*?<span class="nm">Входящие<\/span><span class="cnt num">2<\/span><\/button>/);
  assert.doesNotMatch(out, /ti inbox cur/);
});

test("the unknown state of «Входящие» in the tree: neither the emphasis nor the quiet look, no number", () => {
  const out = tree({ inbox: inboxEntry(0, false, false) });
  assert.match(out, /class="ti inbox"/);
  assert.doesNotMatch(out, /ti inbox (has|quiet)/);
  assert.match(out, /<span class="nm">Входящие<\/span><\/button>/);
});

test("the pill and the heavier name belong to «Входящие» with files only; the quiet one has neither", () => {
  assert.doesNotMatch(tree({ inbox: inboxEntry(0, false) }), /ti inbox [^"]*has/);
  const css = src("styles.css");
  assert.match(css, /\.tree \.ti\.inbox\.has \{ font-weight: 600; \}/);
  assert.match(css, /\.tree \.ti\.inbox\.has \.cnt \{[^}]*border-radius: 999px[^}]*background: var\(--fill\)[^}]*color: var\(--text\)/);
});

test("the folder counts of the tree are blank while the files are read, «—» only for a folder known to be empty", () => {
  const open = (counts) => html(h(SubjectTree, { subjects: list, currentId: "b", counts, busy: false, onOpen: noop, onAddSubject: noop, onAddFolder: noop }));
  assert.doesNotMatch(open(null), /<span class="cnt num">—<\/span>/);
  assert.match(open(null), /<span class="nm">Материалы<\/span><\/button>/, "no number after the folder");
  assert.match(open(new Map([["b-f", 0]])), /<span class="cnt num">—<\/span>/);
  assert.match(open(new Map([["b-f", 4]])), /<span class="cnt num">4<\/span>/);
});

test("with no files «Входящие» stays in the tree, quiet and without a count", () => {
  const out = tree({ inbox: inboxEntry(0, false) });
  assert.match(out, /class="ti inbox quiet"/);
  assert.match(out, /<span class="nm">Входящие<\/span><\/button>/, "no number after the name");
});

test("when «Входящие» is on screen it is the current item, no subject is, and «Новая папка» (of a subject) is not offered", () => {
  const out = tree({ currentId: "", inbox: inboxEntry(2, true) });
  assert.match(out, /class="ti inbox cur has"[^>]*aria-current="page"/);
  assert.equal((out.match(/aria-current="page"/g) ?? []).length, 1);
  assert.equal((out.match(/class="ti fold"/g) ?? []).length, 0, "no folders: no subject is open");
  assert.doesNotMatch(out, />Новая папка</);
  assert.match(out, />Добавить предмет</);
});

test("without the inbox entry the tree is as before (the old callers keep working)", () => {
  const out = tree({ inbox: undefined });
  assert.doesNotMatch(out, /inbox/);
});

test("the state of the tree and of the semester headings stays: semesters are headings, not folders", () => {
  const out = tree();
  assert.equal((out.match(/class="ti sem"/g) ?? []).length, 2);
  assert.match(out, /Осень 2026[\s\S]*Весна 2026/);
});

// ---- texts ----

test("the empty «Входящие» tells how files get there and what to do with them", () => {
  assert.equal(
    emptyInboxText,
    "Здесь собираются файлы без предмета: добавленные кнопкой «Добавить файл» или перетащенные в окно. Отсюда их можно разложить по предметам.",
  );
  assert.equal(inboxFailedText, "Не удалось загрузить «Входящие».");
});

test("a failed «Входящие» is not drawn as an empty one; the files of a subject keep no empty claim while unknown", () => {
  const inboxHtml = (props) => html(h(InboxList, { materials: [], bare: true, ...props }));
  assert.match(inboxHtml({}), /Здесь собираются файлы без предмета/);
  assert.match(inboxHtml({ loading: true }), /Загрузка…/);
  assert.equal(inboxHtml({ failed: true }), "", "failed: no «empty» text");
  const detail = (props) => html(h(SubjectDetail, { subject: list[1], folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop, ...props }));
  const loading = detail({ materials: null });
  assert.doesNotMatch(loading, /пока нет файлов/);
  assert.doesNotMatch(loading, /Загрузка…/, "no flashing line per folder while loading");
  assert.doesNotMatch(loading, /empty-folder/);
  assert.match(loading, /Материалы/, "the folders themselves stay");
  assert.doesNotMatch(loading, /· 0 файлов/, "no count while unknown");
  const failed = detail({ materials: null, failed: true });
  assert.doesNotMatch(failed, /пока нет файлов|Загрузка…/);
  assert.match(detail({ materials: [] }), /В этой папке пока нет файлов\./);
  assert.match(detail({ materials: [] }), /· 0 файлов/);
});

test("the head of the library without subjects has no «База знаний» and «Билеты» (both are empty there)", () => {
  const head = (props) => html(h(LibraryActions, { onOpen: noop, onAddFile: noop, addTip: "", busy: false, ...props }));
  assert.match(head({}), /База знаний/);
  assert.match(head({}), /Билеты/);
  assert.doesNotMatch(head({ links: false }), /База знаний|Билеты/);
  assert.match(head({ links: false }), /Добавить файл/);
});

// ---- the page and the window (source checks: no DOM harness) ----

test("the page shows the tree for any route with subjects and the tiles only for none; no «Все предметы» button is left", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.doesNotMatch(page, /Все предметы|texts\.back|icon="back"/);
  assert.match(page, /showsTiles\(view\)/);
  assert.doesNotMatch(page, /view\.kind === "empty" *\)/, "the tiles are decided by showsTiles");
  assert.match(page, /<EmptySubjects /);
  assert.equal((page.match(/<EmptySubjects /g) ?? []).length, 1, "the tiles are drawn in one place only");
  assert.match(page, /resolveView\(openId, subjects,/);
});

test("the empty screen has «Добавить предмет» of EmptySubjects only: no «Предмет» button and no library links in its head", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /busy=\{busy\} links=\{false\} \/>/);
  assert.doesNotMatch(page, /addSubjectVariant|<Button/);
});

test("the schedule review keeps its state when the first subjects are made: one return, the same slot in both views", () => {
  const page = src("pages/SubjectsPage.tsx");
  // The empty view and the tree view are the last child of one <section>; the review is its 3rd child (after the
  // optional head and the load error) in both, so React does not remount it when view.kind goes empty -> subject.
  assert.equal((page.match(/\{reviewPanel\}/g) ?? []).length, 1, "drawn in one place only");
  assert.equal((page.match(/<section className="page">/g) ?? []).length, 2, "the loading return and the one return of the views");
  assert.match(page, /<section className="page">\s*\{tilesHead\}\s*\{loadFailed\}\s*\{reviewPanel\}\s*\{scheduleLines\}\s*\{tiles \? tilesBody : treeView\}\s*<\/section>/);
  assert.match(page, /<ScheduleSubjectsReview\s+key="schedule-review"/, "and an explicit key besides");
});

test("a failed read of the files of a subject is one line under the head with «Повторить»; the load of «Входящие» likewise", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /setFilesFailed\(true\)/);
  assert.doesNotMatch(page, /setError\(texts\.filesFailed\)/, "not the shared error line");
  assert.match(page, /<ErrorLine text=\{texts\.filesFailed\} onRefresh=\{refresh\} onRetry=\{refresh\} \/>/);
  assert.match(page, /materials=\{materials\}\s+failed=\{filesFailed\}/, "null stays null: nothing is claimed about the folders");
  assert.doesNotMatch(page, /materials \?\? \[\]/);
  assert.match(page, /setInboxError\(true\)/);
  assert.match(page, /<ErrorLine text=\{inboxFailedText\}/);
  assert.match(page, /inboxEntry\(inboxFiles\(inbox\), view\.kind === "inbox", inboxLoaded && !inboxError\)/);
  assert.equal(texts.filesFailed, "Не удалось загрузить файлы предмета.");
});

test("the empty screen shows the failed read of «Входящие» above its list (the bare list is empty then)", () => {
  const page = src("pages/SubjectsPage.tsx");
  const body = page.slice(page.indexOf("const tilesBody"), page.indexOf("const tree = ("));
  assert.match(body, /\{inboxError && <ErrorLine text=\{inboxFailedText\} onRefresh=\{load\} onRetry=\{load\} \/>\}\s*<InboxList /);
});

test("the tree gets the drop (and so the reserved cue line) only in the view «Входящие»", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /drop=\{\s*(\/\/[^\n]*\n\s*)?view\.kind === "inbox"\s*\?\s*\{[^}]*inboxIds,[^}]*\}\s*:\s*undefined\s*\}/);
});

test("the tree is sticky with its own scroll, off in the narrow layout, and the .sheet is its only scroll ancestor", () => {
  const css = src("styles.css");
  assert.match(css, /\.tree \{ position: sticky; top: 16px; max-height: calc\(100vh - [^;]*; overflow: auto;/);
  assert.match(css, /@container app \(max-width: 900px\) \{ \.tree \{ position: static;/);
  assert.match(css, /\.subj \{[^}]*align-items: start;/, "the grid cell is as tall as the tree, so it can stick");
  assert.match(css, /\.tree-cue \{ height: 36px;/, "the cue line has a fixed height");
  assert.match(css, /\.tree \{[^}]*padding: 4px; margin: -4px;/, "room for the 2px drop ring and the outline offset");
  assert.match(css, /@media \(forced-colors: active\) \{\s*\.tree \.ti\.cur \{ outline: 2px solid Highlight; \}\s*\.tree \.ti\.drop \{ outline: 2px solid Highlight;/);
});

test("the page remembers the shown subject, opens «Входящие» by the route and shows its hints there", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /rememberSubject\(browserStore\(\), shownId\)/);
  assert.match(page, /onOpenInbox=\{openInbox\}/);
  assert.match(page, /openSubject\(inboxRoute\)/);
  assert.match(page, /hintsOn: view\.kind === "inbox" \|\| view\.kind === "empty"/);
  assert.doesNotMatch(page, /inboxScroll|scrollToInbox/);
});

test("«Добавить файл» in the head of a subject puts the file into that subject, in «Входящие» into the inbox", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /onAddFile=\{\(\) => void addFileTo\(""\)\} addTip=\{fileTexts\.addToMaterialsTip\}/);
  assert.match(page, /onAddFile=\{\(\) => void addFile\(\)\} addTip=\{fileTexts\.addToInboxTip\}/);
  assert.match(page, /addFileInto\(inboxApi, shownId, folderId, subjects \?\? \[\]\)/);
});

test("the schedule lines, the review and the load error sit above the tree, so they stay reachable", () => {
  const page = src("pages/SubjectsPage.tsx");
  const layout = page.indexOf("{tiles ? tilesBody : treeView}");
  assert.ok(layout > 0);
  for (const piece of ["{loadFailed}", "{reviewPanel}", "{scheduleLines}"]) {
    const at = page.indexOf(piece);
    assert.ok(at > 0 && at < layout, `${piece} before the layout`);
  }
  assert.match(page, /<ScheduleOfferLine /);
  assert.match(page, /<KeptUndoLine /);
  assert.match(page, /<ScheduleCheckFailedLine /);
});

test("the load error says what to do: «Повторить» through ErrorLine", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /<ErrorLine\s+text=\{loadError\}/);
  assert.match(page, /onRetry=\{\(\) => \{\s*setLoadError\(""\);\s*load\(\);/);
});

test("App passes the rail clicks to the section, so a click lands on the last opened subject", () => {
  const app = src("App.tsx");
  assert.match(app, /<SubjectsPage[^>]*visit=\{props\.visit\}/);
  assert.match(app, /openSubjects: \(\) => \{\s*setRoute\(routeToPage\("subjects"\)\);\s*setVisit/);
});
