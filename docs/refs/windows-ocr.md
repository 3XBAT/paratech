# winocr: Windows OCR (Windows.Media.Ocr) through WinRT without CGO

Library: `golang.org/x/sys` v0.48.0 (`windows` package) + std `syscall.SyscallN`; no WinRT binding library (hard rule 9).
Verified on Windows 10 Pro 19045 on 2026-10-27 (TASK-293): installed recognizers `en-US`, `ru`; `MaxImageDimension` = 10000.

## Sources
- **IIDs, method order, enums, struct layout**: the Windows metadata shipped with the OS,
  `C:\Windows\System32\WinMetadata\Windows.{Media,Foundation,Globalization,Graphics,Security}.winmd` (ECMA-335).
  The Windows SDK on the dev machine has only `UnionMetadata\Facade\Windows.WinMD` (type forwards) and no headers.
  Extracted with a throwaway ECMA-335 reader (Python, not committed): IID = `GuidAttribute` of the interface TypeDef;
  method index = position in the TypeDef's MethodList (declaration order); **vtable slot = 6 + index**
  (IUnknown 0–2, IInspectable 3–5).
- **Parameterized IIDs**: Microsoft «Windows Runtime (WinRT) type system», section «Guid generation for parameterized
  types» (learn.microsoft.com/uwp/winrt-cref/winrt-type-system): UUIDv5 = SHA-1 over the namespace
  `11f47ad5-7b73-42c0-abae-878b1e16adee` (bytes big-endian) + UTF-8 signature
  `pinterface({generic iid};<arg>)`, runtime class argument `rc(<full name>;{default interface iid})`; first 16 bytes,
  version 5, RFC 4122 variant. Computed twice (Python `uuid.uuid5`, Go `pinterfaceIID` in the Windows test) and
  confirmed by Windows: every received object answers `QueryInterface` for them (`TestObjectsImplementTheInterfacesReal`).
- **Flat functions** (combase.dll, loaded with `NewLazySystemDLL`): `RoInitialize(RO_INIT_MULTITHREADED = 1)`,
  `RoUninitialize`, `RoGetActivationFactory(HSTRING, REFIID, void**)`, `WindowsCreateString(PCWSTR, UINT32, HSTRING*)`,
  `WindowsDeleteString`, `WindowsGetStringRawBuffer(HSTRING, UINT32*) → PCWSTR` — learn.microsoft.com roapi.h / winstring.h.
  x/sys/windows has none of them; HRESULT constants (`REGDB_E_CLASSNOTREG`, `E_POINTER`, …) come from x/sys.
- **ABI**: every method returns HRESULT; the return value is the last out-pointer parameter; a WinRT in-array is
  `(UINT32 length, T* items)`; enums are int32; `Windows.Foundation.Rect` = 4 × float32 (X, Y, Width, Height).

## Interfaces and slots used
| Interface (runtime class) | IID | Slots called |
|---|---|---|
| IOcrEngineStatics (`Windows.Media.Ocr.OcrEngine`) | 5bffa85a-3384-3540-9940-699120d428a8 | 6 get_MaxImageDimension, 7 get_AvailableRecognizerLanguages, 9 TryCreateFromLanguage (8 IsLanguageSupported, 10 TryCreateFromUserProfileLanguages: never called) |
| IOcrEngine (default of OcrEngine) | 5a14bc41-5b76-3140-b680-8825562683ac | 6 RecognizeAsync, 7 get_RecognizerLanguage |
| IOcrResult | 9bd235b2-175b-3d6a-92e2-388c206e2f63 | 6 get_Lines (7 TextAngle, 8 Text) |
| IOcrLine | 0043a16f-e31f-3a24-899c-d444bd088124 | 6 get_Words, 7 get_Text |
| IOcrWord | 3c2a477a-5cd9-3525-ba2a-23d1e0a68a1d | 6 get_BoundingRect, 7 get_Text |
| ILanguageFactory (`Windows.Globalization.Language`) | 9b0252ac-0c27-44f8-b792-9793fb66c63e | 6 CreateLanguage(HSTRING) |
| ILanguage | ea79a752-f7c2-4265-b1bd-c4dec4e4f080 | 6 get_LanguageTag |
| ISoftwareBitmapStatics (`Windows.Graphics.Imaging.SoftwareBitmap`) | df0385db-672f-4a9d-806e-c2442f343e86 | 9 CreateCopyFromBuffer(IBuffer, BitmapPixelFormat, int32 w, int32 h) |
| ICryptographicBufferStatics (`Windows.Security.Cryptography.CryptographicBuffer`) | 320b7e22-3cb0-4cdf-8663-1d28910065eb | 9 CreateFromByteArray(UINT32, BYTE*) |
| IAsyncInfo | 00000036-0000-0000-c000-000000000046 | 7 get_Status, 8 get_ErrorCode, 9 Cancel, 10 Close |
| IAsyncOperation\`1 (generic) | 9fc2b0bb-e446-44e2-aa61-9cab8f636af2 | 8 GetResults (6 put_Completed, 7 get_Completed: unused) |
| IVectorView\`1 (generic) | bbe1fa4c-b0e3-4583-baef-1f1b2e483e56 | 6 GetAt(uint32), 7 get_Size |
| IAsyncOperation<OcrResult> | c7d7118e-ae36-59c0-ac76-7badee711c8b | computed; QueryInterface check only |
| IVectorView<Language> | 144b0f3d-2d59-5dd2-b012-908ec3e06435 | computed; QueryInterface check only |
| IVectorView<OcrLine> | 60c76eac-8875-5ddb-a19b-65a3936279ea | computed; QueryInterface check only |
| IVectorView<OcrWord> | 805a60c7-df4f-527c-86b2-e29e439a83d2 | computed; QueryInterface check only |

Enums: `BitmapPixelFormat.Gray8 = 62` (Bgra8 = 87), `AsyncStatus` Started 0, Completed 1, Canceled 2, Error 3.
OcrEngine, OcrResult/Line/Word, Language, SoftwareBitmap: `ThreadingModel.Both`, `MarshalingBehavior.Agile`.

## Decisions
- Pixels: PNG/JPEG decoded by the std library → 8-bit grey (alpha over white) → box-filter downscale to
  `MaxImageDimension` → `CryptographicBuffer.CreateFromByteArray` → `SoftwareBitmap.CreateCopyFromBuffer(Gray8)`.
  No BitmapDecoder/streams and no classic-COM `IBufferByteAccess` (not in the .winmd). Gray8 is accepted by the engine.
- Waiting: poll `IAsyncInfo.Status` every 5 ms, `Cancel` on context done; no Completed delegate (would need a
  hand-made COM object).
- Apartment: each call locks its goroutine to the OS thread, `RoInitialize(MTA)` … `RoUninitialize`, unlocks. Calls
  are serialised by the Reader (one page at a time).
- `REGDB_E_CLASSNOTREG` from activation → `ErrUnsupported`; a null engine from `TryCreateFromLanguage(ru)` → `ErrNoLanguage`.
