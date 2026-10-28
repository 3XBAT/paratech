# TASK-293: check steps

Nothing is visible in the app yet (no caller until TASK-294).
1. From `paratech/` on Windows: `go test -race -count=1 -v ./desktop/internal/adapters/winocr/...` passes. The log shows `installed OCR recognizers: [... ru]` and 4 recognised Russian lines of the fixture (the real tests are not skipped).
2. Without the Russian recognizer (Settings > Time & language > Language), `TestAvailableLanguagesReal`/the fixture test fail or skip with `ErrNoLanguage`, whose message explains how to install it.
3. `GOOS=windows go vet ./desktop/...` and `GOOS=linux go vet ./desktop/internal/adapters/winocr/...` are clean.
