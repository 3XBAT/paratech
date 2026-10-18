; paratech installer: per-user, no admin rights, no auto-update.
;
; Adapted from the Wails v3.0.0-beta.29 NSIS templates (MIT License, Copyright (c) 2018-Present Lea Anthony):
;   github.com/wailsapp/wails/v3@v3.0.0-beta.29/internal/commands/build_assets/windows/nsis/project.nsi.tmpl
;   github.com/wailsapp/wails/v3@v3.0.0-beta.29/internal/commands/updatable_build_assets/windows/nsis/wails_tools.nsh.tmpl
; Deviations: user scope only, no desktop shortcut, a running app blocks install and uninstall, the uninstaller deletes
; only the files it installed (no RMDir /r on the install folder) and asks before deleting user data.
;
; Built by desktop/build-release.ps1 (docs/refs/packaging.md), which passes:
;   VERSION                X.Y.Z
;   APP_DIR                folder with paratech.exe and the speech recognition component
;   APP_ICON               desktop\build\windows\icon.ico (installer, shortcut and "Apps & features" icon)
;   WEBVIEW2_BOOTSTRAPPER  MicrosoftEdgeWebview2Setup.exe from the Wails module
;   OUT_FILE               installer path
; The texts are Russian, so the script runs makensis with -INPUTCHARSET UTF8.

Unicode true
ManifestDPIAware true
RequestExecutionLevel user
SetCompressor /SOLID lzma
; A locked file offers only Retry or Cancel, never "Ignore" (a half-updated install).
AllowSkipFiles off

!ifndef VERSION
  !error "VERSION (X.Y.Z) is not defined: build with desktop/build-release.ps1"
!endif
!ifndef APP_DIR
  !error "APP_DIR is not defined: build with desktop/build-release.ps1"
!endif
!ifndef WEBVIEW2_BOOTSTRAPPER
  !error "WEBVIEW2_BOOTSTRAPPER is not defined: build with desktop/build-release.ps1"
!endif
!ifndef APP_ICON
  !error "APP_ICON is not defined: build with desktop/build-release.ps1"
!endif
!ifndef OUT_FILE
  !error "OUT_FILE is not defined: build with desktop/build-release.ps1"
!endif

!define PRODUCT_NAME "paratech"
!define PRODUCT_EXE "paratech.exe"
!define PRODUCT_ICON "paratech.ico"
!define PRODUCT_PUBLISHER "paratech"
; Names of the versions before the rename (ADR-0015): an upgrade and the uninstaller remove them.
!define LEGACY_EXE "learning-assistant.exe"
!define LEGACY_SHORTCUT "Learning Assistant.lnk"
; The key keeps its old id (ADR-0015), so an upgrade finds the old install folder (InstallLocation) and replaces the entry.
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LearningAssistant"
; paratech: links («Открыть paratech» on the Google sign-in page) for this user only; the app repairs the same values
; at start (desktop/internal/adapters/urlscheme, ADR-0013).
!define URL_SCHEME_KEY "Software\Classes\paratech"
; Fixed paths of the app, not of the install folder, kept from before the rename (ADR-0015): config.dataDirName and
; config.webviewDirName (the app sets this WebView2 folder; it was the Wails default %AppData%\<exe name> for the old
; exe). A --data-dir elsewhere is not touched.
!define DATA_DIR "$LOCALAPPDATA\LearningAssistant"
!define WEBVIEW_DATA_DIR "$APPDATA\learning-assistant.exe"
; Wails single-instance mutex: "wails-app-" + UniqueID + "-sim" (pkg/application/single_instance_windows.go),
; UniqueID = instanceID in desktop/internal/app/app.go, unchanged by the rename, so an old running version is found
; too. Only the app with the UI shell holds it.
!define APP_MUTEX "wails-app-paratech.learning-assistant.desktop-sim"
; Prefixed: WinCore.nsh may define SYNCHRONIZE as well.
!define LA_SYNCHRONIZE 0x00100000
; WebView2 Evergreen Runtime client key (wails_tools.nsh.tmpl, macro wails.webview2runtime).
!define WEBVIEW2_CLIENT "Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

!define MSG_WIN10 "${PRODUCT_NAME} работает только в Windows 10 и 11."
!define MSG_ARCH "${PRODUCT_NAME} работает только на 64-битной Windows для x64 (amd64)."
!define MSG_RUNNING "${PRODUCT_NAME} сейчас запущен.$\r$\n$\r$\nЗакройте его (правый клик по значку в трее, Quit) и повторите попытку."
!define MSG_ABORT_RUNNING "Отменено: приложение запущено."
!define MSG_NO_WEBVIEW2 "Не удалось установить Microsoft Edge WebView2 Runtime (нужен интернет). Без него окна приложения не откроются.$\r$\n$\r$\nУстановите его со страницы https://developer.microsoft.com/microsoft-edge/webview2/ и запустите приложение снова."
!define MSG_LEGACY_IN_USE "Не удалось удалить ${LEGACY_EXE} прежней версии: файл занят. Закройте приложение (трей, Quit) и запустите установку снова."
!define MSG_EXE_IN_USE "Не удалось удалить ${PRODUCT_EXE}: файл занят. Закройте приложение (трей, Quit) и запустите удаление снова."
!define MSG_DELETE_DATA "Удалить и ваши данные?$\r$\n$\r$\n${DATA_DIR}$\r$\n(записи, транскрипты, модели, экспорты, логи, база данных с настройками)$\r$\n${WEBVIEW_DATA_DIR}$\r$\n(кэш окна приложения)$\r$\n$\r$\n«Нет» — данные останутся, следующая установка их подхватит. Ключ Hub, пароль почты и вход Google остаются в Диспетчере учётных данных Windows (записи paratech/...)."
!define MSG_DATA_LEFT "Часть данных удалить не удалось. Закройте приложение и удалите вручную:$\r$\n${DATA_DIR}"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WinVer.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"

Name "${PRODUCT_NAME}"
OutFile "${OUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\paratech"
; An upgrade goes into the folder of the installed version, also the LearningAssistant folder of an older one.
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
ShowInstDetails show
ShowUninstDetails show
BrandingText "${PRODUCT_NAME} ${VERSION}"

; The version information needs four parts.
VIProductVersion "${VERSION}.0"
VIFileVersion "${VERSION}.0"
VIAddVersionKey "CompanyName" "${PRODUCT_PUBLISHER}"
VIAddVersionKey "FileDescription" "${PRODUCT_NAME} Installer"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "${PRODUCT_PUBLISHER}"
VIAddVersionKey "ProductName" "${PRODUCT_NAME}"

!define MUI_ICON "${APP_ICON}"
!define MUI_UNICON "${APP_ICON}"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXE}"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "Russian"

; Sets $R9 to 1 when the app still runs and the user chose Cancel (silent mode: at once), else to 0.
!macro DEFINE_APP_RUNNING_CHECK PREFIX
Function ${PREFIX}CheckAppNotRunning
  StrCpy $R9 0
  check:
    System::Call 'kernel32::OpenMutexW(i ${LA_SYNCHRONIZE}, i 0, w "${APP_MUTEX}") p .R8'
    StrCmp $R8 0 done
    System::Call 'kernel32::CloseHandle(p R8)'
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "${MSG_RUNNING}" /SD IDCANCEL IDRETRY check
    StrCpy $R9 1
  done:
FunctionEnd
!macroend
!insertmacro DEFINE_APP_RUNNING_CHECK ""
!insertmacro DEFINE_APP_RUNNING_CHECK "un."

; Sets $R9 to 1 when the WebView2 Runtime is installed (machine-wide or for this user), else to 0.
Function IsWebView2Present
  StrCpy $R9 0
  SetRegView 64
  ReadRegStr $R8 HKLM "SOFTWARE\WOW6432Node\${WEBVIEW2_CLIENT}" "pv"
  ${If} $R8 == ""
  ${OrIf} $R8 == "0.0.0.0"
    ReadRegStr $R8 HKCU "Software\${WEBVIEW2_CLIENT}" "pv"
  ${EndIf}
  SetRegView default
  ${If} $R8 != ""
  ${AndIf} $R8 != "0.0.0.0"
    StrCpy $R9 1
  ${EndIf}
FunctionEnd

; Runs the Microsoft bootstrapper (it downloads the runtime) only when the runtime is missing.
Function EnsureWebView2
  Call IsWebView2Present
  ${If} $R9 = 1
    Return
  ${EndIf}
  DetailPrint "Установка Microsoft Edge WebView2 Runtime"
  InitPluginsDir
  SetOutPath "$PLUGINSDIR\webview2"
  File "/oname=MicrosoftEdgeWebview2Setup.exe" "${WEBVIEW2_BOOTSTRAPPER}"
  ExecWait '"$PLUGINSDIR\webview2\MicrosoftEdgeWebview2Setup.exe" /silent /install' $R7
  DetailPrint "WebView2 bootstrapper exit code: $R7"
  Call IsWebView2Present
  ${If} $R9 <> 1
    MessageBox MB_OK|MB_ICONEXCLAMATION "${MSG_NO_WEBVIEW2}" /SD IDOK
  ${EndIf}
FunctionEnd

Function .onInit
  SetShellVarContext current
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_OK|MB_ICONSTOP "${MSG_WIN10}" /SD IDOK
    SetErrorLevel 64
    Abort
  ${EndIf}
  ${IfNot} ${IsNativeAMD64}
    MessageBox MB_OK|MB_ICONSTOP "${MSG_ARCH}" /SD IDOK
    SetErrorLevel 65
    Abort
  ${EndIf}
  Call CheckAppNotRunning
  ${If} $R9 = 1
    SetErrorLevel 2
    Abort
  ${EndIf}
FunctionEnd

Section "Install"
  SetShellVarContext current
  ; The app may have been started while the wizard was open.
  Call CheckAppNotRunning
  ${If} $R9 = 1
    SetErrorLevel 2
    Abort "${MSG_ABORT_RUNNING}"
  ${EndIf}

  Call EnsureWebView2

  ; An upgrade of a version before the rename (ADR-0015): the old exe and shortcut go first, so neither starts the old
  ; app (which would also point paratech: links back at itself); if the exe is in use, nothing is changed yet.
  Delete "$INSTDIR\${LEGACY_EXE}"
  ${If} ${FileExists} "$INSTDIR\${LEGACY_EXE}"
    MessageBox MB_OK|MB_ICONEXCLAMATION "${MSG_LEGACY_IN_USE}" /SD IDOK
    SetErrorLevel 2
    Abort "${MSG_ABORT_RUNNING}"
  ${EndIf}
  Delete "$SMPROGRAMS\${LEGACY_SHORTCUT}"

  SetOutPath "$INSTDIR"
  File "${APP_DIR}\${PRODUCT_EXE}"
  ; The exe has no icon resource yet (ADR-0015): the shortcut and "Apps & features" use this file.
  File "/oname=${PRODUCT_ICON}" "${APP_ICON}"
  ; The speech recognition component: the files of desktop/internal/app/speechcomponent.json (keep both lists here in
  ; sync with it). ggml loads every ggml-cpu-*.dll next to it, so none of an older set may stay.
  Delete "$INSTDIR\ggml-cpu-*.dll"
  File "${APP_DIR}\whisper-cli.exe"
  File "${APP_DIR}\whisper.dll"
  File "${APP_DIR}\ggml.dll"
  File "${APP_DIR}\ggml-base.dll"
  File "${APP_DIR}\ggml-cpu-alderlake.dll"
  File "${APP_DIR}\ggml-cpu-cannonlake.dll"
  File "${APP_DIR}\ggml-cpu-cascadelake.dll"
  File "${APP_DIR}\ggml-cpu-haswell.dll"
  File "${APP_DIR}\ggml-cpu-icelake.dll"
  File "${APP_DIR}\ggml-cpu-sandybridge.dll"
  File "${APP_DIR}\ggml-cpu-skylakex.dll"
  File "${APP_DIR}\ggml-cpu-sse42.dll"
  File "${APP_DIR}\ggml-cpu-x64.dll"

  WriteUninstaller "$INSTDIR\uninstall.exe"
  CreateShortcut "$SMPROGRAMS\${PRODUCT_NAME}.lnk" "$INSTDIR\${PRODUCT_EXE}" "" "$INSTDIR\${PRODUCT_ICON}" 0

  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_ICON}"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"

  ; Recreated, so no stale subkey (another command, a DelegateExecute) stays. Both the exe path and the link are quoted.
  DeleteRegKey HKCU "${URL_SCHEME_KEY}"
  WriteRegStr HKCU "${URL_SCHEME_KEY}" "" "URL:paratech"
  WriteRegStr HKCU "${URL_SCHEME_KEY}" "URL Protocol" ""
  WriteRegStr HKCU "${URL_SCHEME_KEY}\shell\open\command" "" '"$INSTDIR\${PRODUCT_EXE}" "%1"'
SectionEnd

Function un.onInit
  SetShellVarContext current
  Call un.CheckAppNotRunning
  ${If} $R9 = 1
    SetErrorLevel 2
    Abort
  ${EndIf}
FunctionEnd

Section "Uninstall"
  SetShellVarContext current

  ; The exe goes first: if it is still in use, stop before anything else is removed.
  Delete "$INSTDIR\${PRODUCT_EXE}"
  ${If} ${FileExists} "$INSTDIR\${PRODUCT_EXE}"
    MessageBox MB_OK|MB_ICONEXCLAMATION "${MSG_EXE_IN_USE}" /SD IDOK
    SetErrorLevel 2
    Abort "${MSG_ABORT_RUNNING}"
  ${EndIf}
  ; Left only if an upgrade over a version before the rename did not run.
  Delete "$INSTDIR\${LEGACY_EXE}"
  Delete "$INSTDIR\${PRODUCT_ICON}"
  Delete "$INSTDIR\whisper-cli.exe"
  Delete "$INSTDIR\whisper.dll"
  Delete "$INSTDIR\ggml.dll"
  Delete "$INSTDIR\ggml-base.dll"
  Delete "$INSTDIR\ggml-cpu-alderlake.dll"
  Delete "$INSTDIR\ggml-cpu-cannonlake.dll"
  Delete "$INSTDIR\ggml-cpu-cascadelake.dll"
  Delete "$INSTDIR\ggml-cpu-haswell.dll"
  Delete "$INSTDIR\ggml-cpu-icelake.dll"
  Delete "$INSTDIR\ggml-cpu-sandybridge.dll"
  Delete "$INSTDIR\ggml-cpu-skylakex.dll"
  Delete "$INSTDIR\ggml-cpu-sse42.dll"
  Delete "$INSTDIR\ggml-cpu-x64.dll"
  ; Leftovers of an older whisper.cpp set.
  Delete "$INSTDIR\ggml-cpu-*.dll"
  Delete "$INSTDIR\uninstall.exe"
  ; Not /r (the Wails template uses /r): a folder the user chose may hold other files.
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}.lnk"
  Delete "$SMPROGRAMS\${LEGACY_SHORTCUT}"
  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegKey HKCU "${URL_SCHEME_KEY}"

  ; Default and silent answer: keep the data.
  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "${MSG_DELETE_DATA}" /SD IDNO IDNO keep_data
  ${If} $LOCALAPPDATA != ""
    RMDir /r "${DATA_DIR}"
    ${If} ${FileExists} "${DATA_DIR}\*.*"
      MessageBox MB_OK|MB_ICONEXCLAMATION "${MSG_DATA_LEFT}" /SD IDOK
    ${EndIf}
  ${EndIf}
  ${If} $APPDATA != ""
    RMDir /r "${WEBVIEW_DATA_DIR}"
  ${EndIf}
  keep_data:
SectionEnd
