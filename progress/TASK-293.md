# TASK-293: Windows OCR adapter (winocr)

## Feasibility (gate passed)
Windows.Media.Ocr works from Go without CGO and without a new dependency: `golang.org/x/sys/windows` + `syscall.SyscallN` over raw WinRT vtables. Verified by real runs on Windows 10 19045 (recognizers `en-US`, `ru`; `MaxImageDimension` 10000): the synthetic printed-Russian fixture is read as 4 correct lines, engine `windows-ocr/ru/10.0.19045`.
IIDs, slots and enums are in `docs/refs/windows-ocr.md` with sources. Note: the AC says «Windows SDK .winmd»; the SDK on the dev machine has only the facade `Windows.WinMD`, so the OS metadata `C:\Windows\System32\WinMetadata\*.winmd` was used (same data). Generic (pinterface) IIDs are computed by the documented UUIDv5 rule and confirmed by Windows (`QueryInterface` test).

## Done
- `adapters/winocr`: `Reader.Read(ctx, png/jpeg bytes) -> Page{Lines{Text, Box}, Engine}`, `AvailableLanguages()`, sentinels `ErrUnsupported`, `ErrNoLanguage` (message tells how to install the Russian OCR feature), `ErrBadImage`, `ErrTooLarge`, `ErrTimeout`, `ErrEngine` (+ `HResultError`).
- Only the Russian recognizer (`TryCreateFromLanguage("ru")`, tag re-checked); never user-profile languages or another language.
- Image: header-first limits (`material.MaxImageBytes`/`MaxImagePixels`), std decode, grey, box-filter downscale to `OcrEngine.MaxImageDimension` read from the API; boxes scaled back and clamped.
- Per-page timeout, context cancel -> `IAsyncInfo.Cancel`, one page at a time, panics and HRESULTs -> adapter errors (ARCH-32). Non-Windows stub -> `ErrUnsupported`.
- Tests: fake-engine unit tests everywhere; real-engine tests on Windows (skipped on GitHub Actions unless `LA_WINOCR_CI_TEST=1`, same as the WebView2 tests). Synthetic fixture `testdata/printed-ru.png`, described in `docs/refs/windows-ocr.md` (no README in `paratech/`).

## Decisions to confirm
- None. Routine: OS WinMetadata instead of SDK .winmd; polling of `IAsyncInfo.Status` (5 ms) instead of a Completed delegate; Gray8 pixels via `CryptographicBuffer` + `SoftwareBitmap.CreateCopyFromBuffer` (no BitmapDecoder/streams).

## Follow-ups
- The adapter returns its own `Page`/`Line`; the consuming use case (TASK-294) declares the port, the wiring must map types (ARCH-31).
- Security Low: a 16-bit RGBA PNG just under 40 MP decodes to about 320 MB before downscaling; consider a lower limit for 16-bit/interlaced PNGs.
- Reviewer Low: `TestReadCancelReal` (50 ms) likely cancels during decoding; the engine-cancel path is covered by `TestAwaitCancelsReal`. `TestReadDownscalesReal` keyword check for «лекция» after downscale may be flaky on other machines.
- The real-OCR tests run only locally (CI skips them).

## Changed existing tests
None. (Two helper calls in the same task's own tests changed for the new `newEngine` return value.)

## Review fixes
Reviewer: approve; fixed Lows: ctx check after `prepare`, recognizer-tag check, README moved into the refs doc. Install instructions in `ErrNoLanguage` added at the supervisor's request.

## Security
Security-auditor: no High/Medium; one Low (above). No network, no logging, no file access in the package; DLL via `NewLazySystemDLL`; text is returned as plain data.
