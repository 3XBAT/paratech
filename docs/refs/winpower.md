# winpower: Windows power status

Library: `golang.org/x/sys` v0.48.0 (module `windows` package), verified in the module source on 2026-10-12.
Docs: https://learn.microsoft.com/windows/win32/api/winbase/nf-winbase-getsystempowerstatus

- `x/sys/windows` has NO wrapper for `GetSystemPowerStatus` (grep for "PowerStatus" finds nothing). The adapter loads it with
  `windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")` and calls `proc.Call(uintptr(unsafe.Pointer(&s)))`;
  `r1 == 0` means failure and the error is the third result.
- `SYSTEM_POWER_STATUS`: `ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag byte`, then `BatteryLifeTime, BatteryFullLifeTime uint32` (12 bytes).
- `ACLineStatus`: 0 offline (battery), 1 online (AC), 255 unknown. Adapter rule: only 0 means battery, anything else is AC (port contract: unknown = AC).
- Change notifications are done by polling (5 s). `RegisterPowerSettingNotification` needs a window or a service handle, so it would
  require a message-only window with a locked OS thread and a message loop; polling a cheap call is simpler and robust.
