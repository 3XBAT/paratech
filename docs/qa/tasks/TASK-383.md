# TASK-383 — check steps (package READMEs)

No UI and nothing to run in the app: four documentation files.

1. Open `paratech/desktop/internal/input/wails/README.md`, `paratech/desktop/internal/input/agent/README.md`,
   `paratech/desktop/frontend/src/README.md`, `paratech/desktop/frontend/src/agent/README.md`.
2. Each is under 80 lines, in English, and answers: what lives where, which files a typical change touches (in order), how to
   run the focused tests, what to watch out for.
3. Pick any file name from a README and check it exists in the package.
4. The E2E line says the scenario packs are planned (ADR-0035) and that `paratech/e2e/specs` is not created yet.
