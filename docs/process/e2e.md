# End-to-end tests of the real app

ADR-0035. The real `paratech.exe` (build tag `e2e`), a real local Hub with its own Postgres, and a Playwright runner in
`paratech/e2e/` that drives the real window over CDP. The result is a table «готово / сломано / не проверено» per feature.

## Two modes
- **SCRIPTED** (every task, free, deterministic): the Hub talks to a fake Anthropic API with scripted answers. Checks the
  app's side: tools, cards, navigation, and what the model *was given* (the fake records every request).
- **LIVE** (before each beta, costs tokens, Hub quota 400000): real provider, tagged `@live`, loose assertions.

## Run
```
bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <abs>/paratech/e2e run e2e
bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @smoke
bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --no-build   # reuse paratech/dist/e2e/paratech.exe
```
`run e2e:live` is LIVE (needs `PARATECH_E2E_ANTHROPIC_KEY`, optionally `_BASE_URL`, `_AUTH`). Docker must be running (the
Hub's Postgres). Budget: SCRIPTED up to 15 min, a pack up to 5 min, LIVE up to 30 min, counted from the start of the tests
(the exe build and the app start are not counted); over the budget the report warns. The whole run, build included, is cut
off after 45 min.
`npm --prefix <abs>/paratech/e2e test` is the static part (unit tests, no app): `PARATECH_E2E_UNIT_ONLY=1` skips the Docker Hub test.

## Reading the report
`paratech/dist/e2e-run/report/report.md` (+ `screenshots/`, `traces/` of failed tests). One row per feature:

| Функция | Режим | Итог | Шаг, где сломалось | Снимок |
|---|---|---|---|---|

- **готово**: all tests of the feature passed. **сломано**: a test failed; the step column names the deepest `step()` and the first
  line of the error, the screenshot shows the window at that moment.
- **не проверено (нужно: …)**: not a failure. The feature did not run: a prerequisite feature is not «готово»
  (`requires`), the test is `fixme` with a reason, or the machine is not ready (Docker, port, sandbox).
  «Проблемы среды» lists machine problems; fix those first, then rerun. A start failure of the product is «сломано», step «Запуск».
- Scrubbed from the report: the secret values (the Hub key, the provider key, URL and auth of LIVE), any `pthub_…` and `sk-…`
  token, `Authorization`/`x-api-key` headers and `PARATECH_E2E_X=value` assignments. Paths and other `PARATECH_E2E_*` outputs
  are not secret and stay. Playwright traces and screenshots in `report/` and `traces/` are not scrubbed: they may show text typed
  into the UI (the throwaway Hub key). Never share them; they stay under `paratech/dist` (gitignored).

## Adding a scenario
A UI task that changes a feature adds or updates its scenario. Put it in the pack folder `paratech/e2e/specs/<pack>/<name>.spec.ts`
(packs: settings, recording, popup, assignments, agent, layout). Annotations: `feature` (the row), `mode` (SCRIPTED default | LIVE),
`requires` (feature that must be «готово» first). Tags: `@smoke`, `@agent`, `@live`...

```ts
import { test, expect, step, expectPickConsumed } from "../../src/steps/index.js";

test("импорт записи", { tag: "@recording", annotation: [
  { type: "feature", description: "Импорт записи" }, { type: "requires", description: "Запуск приложения" } ] },
  async ({ main, pick, opened }) => {
    await pick("recording", ["fixtures/clip.wav"]);            // queues the answer of the next native dialog
    await step("Импортировать запись", async () => {
      await main.getByRole("button", { name: "Импортировать запись" }).click();
      await expectPickConsumed(1);                              // the app took the pick (not «cancelled»)
    });
    expect(opened()).toEqual([]);                               // nothing was opened or copied for real
  });
```

```ts
// llm: one rule per EXPECTED provider call, consumed in order; script() also clears the recordings, so it first throws
// if an unscripted call happened since the last check (a second script() inside a step cannot hide it)
await llm.script([
  { kind: "tools", match: "какие у меня записи", reply: [{ name: "list_recordings", input: {} }] },
  { kind: "plain", match: "", reply: "У вас три записи." },
]);
// ... ask the agent in the UI ...
const sent = await llm.requests();                              // what the Hub sent to the provider
expect(JSON.stringify(sent[1]?.body)).toContain("Лекция 1");     // the agent was given the titles
```

```ts
// step: test.step + a screenshot of the windows on failure; also fails on the first unscripted LLM call
const total = await step("Открыть «Записи»", async () => {
  await main.getByRole("link", { name: "Записи" }).click();
  return main.getByTestId("recording-row").count();
});
```
Other helpers: `popup` (Page or undefined), `snapshot(name)` (screenshot of a passing scenario into the report),
`clipboardWrites(page)` (texts the page tried to copy; the system clipboard is never touched).

## Layout pack (`@layout`, TASK-376)
`paratech/e2e/specs/layout/` visits every screen (rail sections, subject and lesson tabs, assignment page, every settings item,
the agent panel open and at its widest, the popup) and runs `checkLayout` (`paratech/e2e/src/checks/`): no whole-window scroll,
no horizontal overflow, icon-only buttons with `aria-label` + tooltip (`title`, or the `.tip`/`.ia-tip`/`.ag-ib-tip` span of the
rail and `IconAction`), quiet `[data-status]`, no raw `undefined`/`NaN`/`Error:` text, no console error, no squeezed titles, no
overlap of the agent's «Стоп» row with the text. One report row per screen and size: «Раскладка · <экран> · <размер>».
The window size is a property of the run (one app start per run): the second size is a second run.
```
bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh npm --prefix <abs>/paratech/e2e run e2e -- --grep @layout
bash C:/Users/xvati/GolandProjects/paratech-ops/heavy.sh env PARATECH_E2E_WINDOW=1024x640 npm --prefix <abs>/paratech/e2e run e2e -- --grep @layout
```
A screen missing in the build is «не проверено» (skipped with a reason); a finding is a failed test with the screen and the
element named — never relax a rule to turn a row green. Do not run `playwright test --list` with the repo config from another cwd.

## Rules
- No fixed sleeps: wait on UI state (`expect(locator)...`, `expect.poll`).
- SCRIPTED has no retries. A flake is a bug: fix it, or quarantine with `test.fixme(true, "reason + task")`.
- One fake rule per expected provider call. A call no rule answers fails the scenario at once as
  «unscripted LLM call: …» (the Hub retries a 500, so it can appear several times; the first is the cause).
- Fixtures are synthetic and live in `paratech/e2e/fixtures/`; pick paths are always `fixtures/...` with `/`.
- Layout invariants (ADR-0035 §5) are checked on every screen a scenario visits.

## Safety
Everything runs in `paratech/dist/e2e-run/`: own data dir, own WebView2 profile, own Credential Manager profile
(`paratech-<profile id>/…`, deleted at teardown, never another prefix), own instance id (the owner's app keeps running),
own Hub on a free loopback port (never 8080/58080) and an own Postgres compose project `paratech-e2e`. The app refuses to
start in E2E mode with the default data dir. The exe has the `e2e` tag; the shipped exe cannot open a debug port.
The window is visible during a run (title ends with « · E2E»). Nothing audible, except the opt-in `--with-sound` scenario.

## After a crashed run
The next run wipes `paratech/dist/e2e-run/`. By hand, if needed: stop a leftover `paratech.exe` / `hub.exe` from
`paratech/dist/`, then `docker compose -p paratech-e2e -f paratech/hub/docker-compose.yml down -v`; leftover
Credential Manager entries are only those named `paratech-<profile id>/…` of `state.json` (`secretsProfileId`).
