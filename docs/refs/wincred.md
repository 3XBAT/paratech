# wincred: Windows Credential Manager

Library: none added. `golang.org/x/sys` v0.48.0 (already a direct dependency) has NO `Cred*` wrappers (grep of the module
source for `CredWrite`/`CredRead` finds nothing, checked 2026-10-13), so the adapter loads them lazily:
`windows.NewLazySystemDLL("advapi32.dll").NewProc("CredWriteW" | "CredReadW" | "CredDeleteW" | "CredFree")`.
`github.com/danieljoos/wincred` was not needed.

Docs (fetched 2026-10-13):
- CREDENTIALW: https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw (page updated 2022-07-27)
- CredWriteW: https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credwritew (2024-11-20)
- CredReadW: https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credreadw (2024-11-20)
- CredDeleteW: https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-creddeletew (2024-02-22)
- CredFree: https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credfree (2024-02-22)

## API facts used
- `BOOL CredWriteW(PCREDENTIALW, DWORD Flags)`: creates or replaces the credential with the same TargetName and Type.
  Flags 0 (`CRED_PRESERVE_CREDENTIAL_BLOB` = 1 is not used).
- `BOOL CredReadW(LPCWSTR TargetName, DWORD Type, DWORD Flags=0, PCREDENTIALW *Credential)`: returns ONE allocated block
  (all pointers point inside it) that must be released with `VOID CredFree(PVOID)`.
- `BOOL CredDeleteW(LPCWSTR TargetName, DWORD Type, DWORD Flags=0)`.
- Errors via GetLastError (the third result of `LazyProc.Call`, a `windows.Errno`): `ERROR_NOT_FOUND` (1168) for a missing
  credential; `ERROR_NO_SUCH_LOGON_SESSION` (1312) when the session has no credential set (network/service logons);
  `ERROR_INVALID_FLAGS` (1004); `ERROR_INVALID_PARAMETER`, `ERROR_BAD_USERNAME` on write.
- `LazyProc.Call` is `//go:uintptrescapes` (pointers converted in the call expression stay alive) and PANICS if the
  proc is missing, so the adapter calls `Find()` first.

## CREDENTIALW layout
`Flags, Type DWORD; TargetName, Comment LPWSTR; LastWritten FILETIME; CredentialBlobSize DWORD; CredentialBlob LPBYTE;
Persist, AttributeCount DWORD; Attributes PCREDENTIAL_ATTRIBUTEW; TargetAlias, UserName LPWSTR`.
Go mirrors it field by field (`windows.Filetime` for FILETIME). Sizes: 80 bytes on 64-bit (blob pointer at offset 40 after
4 bytes of padding), 52 bytes on 32-bit; checked by `TestCredentialLayout` (run on windows/amd64 and windows/386).

## Choices
- `Type = CRED_TYPE_GENERIC (1)`: application-defined blob; the docs suggest prefixing the target with the company or
  product name, so targets are `paratech/<service>/<name>`. TargetName is case-insensitive, hence names are lowercase only.
- `Persist = CRED_PERSIST_LOCAL_MACHINE (2)`: survives logoff, visible only to this user on this computer.
  `CRED_PERSIST_ENTERPRISE (3)` would roam with a roaming profile to other computers, which we do not want.
- Blob size limit: `CRED_MAX_CREDENTIAL_BLOB_SIZE` is `5*512` = 2560 bytes per the CREDENTIALW page. NOTE: older
  mingw headers (for example the one shipped with Windhawk) still define it as 512; trust the Microsoft docs and the
  real-API test, which writes and reads exactly 2560 bytes. Longer secrets are rejected with `ErrSecretTooLarge`
  (wraps `shared.ErrInvalidSecret`), never truncated.
- Blob encoding: the UTF-8 bytes of the secret, no terminator (only this adapter reads it back). Do not edit an entry
  in the Credential Manager UI: it re-encodes the password field and corrupts the blob.
- Exists has no cheap API: CredReadW is the only existence check, so Exists reads the whole blob and wipes it.
- `ERROR_NO_SUCH_LOGON_SESSION` maps to the exported `ErrNoSession` (wraps `shared.ErrStorage`). Real-API tests skip
  only on it, and fail even then when `PARATECH_REQUIRE_CREDMAN=1`.
- Comment, TargetAlias, UserName, Attributes: unset (UserName is ignored for generic credentials).
- Memory hygiene: the `[]byte` copy given to CredWriteW is zeroed after the call; on read the blob inside the CredReadW
  buffer is zeroed before `CredFree` and the Go copy is zeroed after conversion. The returned Go string cannot be wiped.
- Off Windows the adapter returns `ErrUnsupported` (wraps `errors.ErrUnsupported`) from every call: there is no
  weaker fallback. Tests use `testenv.MemCredentials`, held to the same contract by `tests/credentialstore_contract_test.go`.

## Open points
- Wiring into `app` (mail, Hub key, Google tokens) is left to TASK-038/044/050; each reading adapter declares its own
  `Get` interface.
- A Google refresh token is usually far below 2560 bytes; if a provider ever returns more, the store fails loudly and the
  value would need splitting across several targets (not implemented).
