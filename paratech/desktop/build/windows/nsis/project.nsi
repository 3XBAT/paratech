; Learning Assistant installer: per-user, no admin rights, no auto-update.
;
; Adapted from the Wails v3.0.0-beta.29 NSIS templates (MIT License, Copyright (c) 2018-Present Lea Anthony):
;   github.com/wailsapp/wails/v3@v3.0.0-beta.29/internal/commands/build_assets/windows/nsis/project.nsi.tmpl
;   github.com/wailsapp/wails/v3@v3.0.0-beta.29/internal/commands/updatable_build_assets/windows/nsis/wails_tools.nsh.tmpl
; Deviations: user scope only, no desktop shortcut, a running app blocks install and uninstall, the uninstaller deletes
; only the files it installed (no RMDir /r on the install folder) and asks before deleting user data.
;
; Built by desktop/build-release.ps1 (docs/refs/packaging.md), which passes:
;   VERSION                X.Y.Z
;   APP_DIR                folder with learning-assistant.exe (and the whisper files when WITH_WHISPER is set)
;   WEBVIEW2_BOOTSTRAPPER  MicrosoftEdgeWebview2Setup.exe from the Wails module
;   OUT_FILE               installer path
;   WITH_WHISPER           optional: also install whisper-cli.exe and its DLLs
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
!ifndef OUT_FILE
  !error "OUT_FILE is not defined: build with desktop/build-release.ps1"
!endif

!define PRODUCT_NAME "Learning Assistant"
!define PRODUCT_EXE "learning-assistant.exe"
!define PRODUCT_PUBLISHER "paratech"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LearningAssistant"
; paratech: links («Открыть Paratech» on the Google sign-in page) for this user only; the app repairs the same values
; at start (desktop/internal/adapters/urlscheme, ADR-0013).
!define URL_SCHEME_KEY "Software\Classes\paratech"
; Fixed paths of the app, not of the install folder: config.dataDirName and the Wails WebView2 default
; (%AppData%\<exe name>, internal/webview2/pkg/edge/chromium.go). A --data-dir elsewhere is not touched.
!define DATA_DIR "$LOCALAPPDATA\LearningAssistant"
!define WEBVIEW_DATA_DIR "$APPDATA\${PRODUCT_EXE}"
; Wails single-instance mutex: "wails-app-" + UniqueID + "-sim" (pkg/application/single_instance_windows.go),
; UniqueID = instanceID in desktop/internal/app/app.go. Only the app with the UI shell holds it.
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
InstallDir "$LOCALAPPDATA\Programs\LearningAssistant"
; An upgrade goes into the folder of the installed version.
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

; No MUI_ICON: the NSIS default icon until the app has its own.
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

  SetOutPath "$INSTDIR"
  File "${APP_DIR}\${PRODUCT_EXE}"
  !ifdef WITH_WHISPER
    File "${APP_DIR}\whisper-cli.exe"
    File "${APP_DIR}\whisper.dll"
    File "${APP_DIR}\ggml.dll"
    File "${APP_DIR}\ggml-base.dll"
    File "${APP_DIR}\ggml-cpu-*.dll"
  !endif

  WriteUninstaller "$INSTDIR\uninstall.exe"
  CreateShortcut "$SMPROGRAMS\${PRODUCT_NAME}.lnk" "$INSTDIR\${PRODUCT_EXE}"

  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXE}"
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
  WriteRegStr HKCU "${URL_SCHEME_KEY}" "" "URL:Paratech"
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
  Delete "$INSTDIR\whisper-cli.exe"
  Delete "$INSTDIR\whisper.dll"
  Delete "$INSTDIR\ggml.dll"
  Delete "$INSTDIR\ggml-base.dll"
  Delete "$INSTDIR\ggml-cpu-*.dll"
  Delete "$INSTDIR\uninstall.exe"
  ; Not /r (the Wails template uses /r): a folder the user chose may hold other files.
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}.lnk"
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
